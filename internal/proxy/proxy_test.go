package proxy_test

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Arif-Karim/mcp-hub/internal/audit"
	"github.com/Arif-Karim/mcp-hub/internal/catalogue"
	"github.com/Arif-Karim/mcp-hub/internal/proxy"
	"github.com/Arif-Karim/mcp-hub/internal/token"
)

const user = "dev@example.test"

type resolver map[string]proxy.Upstream

func (r resolver) Resolve(id string) (proxy.Upstream, bool) { u, ok := r[id]; return u, ok }

type recorder struct {
	mu     sync.Mutex
	events []audit.Event
}

func (r *recorder) Record(e audit.Event) { r.mu.Lock(); r.events = append(r.events, e); r.mu.Unlock() }
func (r *recorder) all() []audit.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]audit.Event(nil), r.events...)
}
func (r *recorder) only(t *testing.T) audit.Event {
	t.Helper()
	ev := r.all()
	if len(ev) != 1 {
		t.Fatalf("audit events = %d (%+v), want 1", len(ev), ev)
	}
	return ev[0]
}

type failingTokens struct{}

func (failingTokens) Issue(string, string, time.Duration) (string, error) {
	return "", errors.New("hsm offline")
}

// seen captures what an upstream received.
type seen struct {
	mu     sync.Mutex
	header http.Header
	host   string
	path   string
	query  string
	body   string
	calls  int
}

func (s *seen) get() seen {
	s.mu.Lock()
	defer s.mu.Unlock()
	return seen{header: s.header.Clone(), host: s.host, path: s.path, query: s.query, body: s.body, calls: s.calls}
}

func upstream(t *testing.T, h http.HandlerFunc) (*httptest.Server, *seen) {
	t.Helper()
	s := &seen{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.header, s.host, s.path, s.query, s.body = r.Header.Clone(), r.Host, r.URL.Path, r.URL.RawQuery, string(b)
		s.calls++
		s.mu.Unlock()
		if h != nil {
			h(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{}}`)
	}))
	t.Cleanup(srv.Close)
	return srv, s
}

type fixture struct {
	h      http.Handler
	issuer *token.Issuer
	audit  *recorder
}

func newFixture(t *testing.T, res resolver, mut func(*proxy.Config)) fixture {
	t.Helper()
	is, err := token.NewIssuer(filepath.Join(t.TempDir(), "k.pem"), "http://127.0.0.1:7420", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	cfg := proxy.Config{Resolver: res, Tokens: is, Audit: rec, User: user}
	if mut != nil {
		mut(&cfg)
	}
	return fixture{h: proxy.New(cfg), issuer: is, audit: rec}
}

func req(method, target, body string) *http.Request {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.Host = "127.0.0.1:7420"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	return r
}

func serve(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func errCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var b struct {
		Error struct{ Code, Message string } `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&b); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	return b.Error.Code
}

const callBody = `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"find_service","arguments":{"query":"SECRET-ARG-VALUE"}}}`

func TestHubJWTInjectsFreshTokenAndStripsClientCredentials(t *testing.T) {
	up, got := upstream(t, nil)
	f := newFixture(t, resolver{"svc-a": {URL: up.URL + "/mcp", Auth: catalogue.AuthHubJWT}}, nil)

	r := req(http.MethodPost, "/mcp/svc-a", callBody)
	r.Header.Set("Authorization", "Bearer client-secret")
	r.Header.Set("Cookie", "session=abc")
	r.Header.Set("Proxy-Authorization", "Basic Zm9vOmJhcg==")
	r.Header.Set("X-Api-Key", "leak-me")
	r.Header.Set("Mcp-Protocol-Version", "2025-06-18")
	rec := serve(f.h, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body)
	}
	g := got.get()
	auth := g.header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") || strings.Contains(auth, "client-secret") {
		t.Fatalf("upstream Authorization = %q", auth)
	}
	claims, err := f.issuer.Verify(strings.TrimPrefix(auth, "Bearer "), "svc-a")
	if err != nil {
		t.Fatalf("injected token invalid: %v", err)
	}
	if claims.Subject != user {
		t.Fatalf("sub = %q", claims.Subject)
	}
	if ttl := claims.ExpiresAt.Sub(claims.IssuedAt.Time); ttl != proxy.DefaultTokenTTL {
		t.Fatalf("ttl = %v, want %v", ttl, proxy.DefaultTokenTTL)
	}
	for _, h := range []string{"Cookie", "Proxy-Authorization", "X-Api-Key"} {
		if v := g.header.Get(h); v != "" {
			t.Errorf("upstream received %s = %q", h, v)
		}
	}
	if g.header.Get("Mcp-Protocol-Version") != "2025-06-18" || g.header.Get("Content-Type") != "application/json" ||
		g.header.Get("Accept") != "application/json, text/event-stream" {
		t.Fatalf("MCP headers not forwarded: %v", g.header)
	}
	if g.path != "/mcp" || g.body != callBody {
		t.Fatalf("path=%q body=%q", g.path, g.body)
	}
	if g.host != strings.TrimPrefix(up.URL, "http://") {
		t.Fatalf("upstream Host = %q, want %q", g.host, up.URL)
	}
}

func TestTokensAreFreshPerRequestAndUseConfiguredTTL(t *testing.T) {
	up, got := upstream(t, nil)
	f := newFixture(t, resolver{"svc-a": {URL: up.URL, Auth: catalogue.AuthHubJWT}},
		func(c *proxy.Config) { c.TokenTTL = 2 * time.Minute })

	serve(f.h, req(http.MethodPost, "/mcp/svc-a", callBody))
	first := got.get().header.Get("Authorization")
	serve(f.h, req(http.MethodPost, "/mcp/svc-a", callBody))
	second := got.get().header.Get("Authorization")
	if first == second {
		t.Fatal("token reused across requests")
	}
	c, err := f.issuer.Verify(strings.TrimPrefix(second, "Bearer "), "svc-a")
	if err != nil {
		t.Fatal(err)
	}
	if ttl := c.ExpiresAt.Sub(c.IssuedAt.Time); ttl != 2*time.Minute {
		t.Fatalf("ttl = %v", ttl)
	}
}

func TestAuthNoneSendsNoCredentials(t *testing.T) {
	up, got := upstream(t, nil)
	f := newFixture(t, resolver{"pub": {URL: up.URL, Auth: catalogue.AuthNone}}, nil)

	r := req(http.MethodPost, "/mcp/pub", callBody)
	r.Header.Set("Authorization", "Bearer client-secret")
	r.Header.Set("Cookie", "a=b")
	if rec := serve(f.h, r); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	g := got.get()
	if g.header.Get("Authorization") != "" || g.header.Get("Cookie") != "" {
		t.Fatalf("credentials leaked: %v", g.header)
	}
}

func TestUnknownAuthModeFailsClosed(t *testing.T) {
	up, got := upstream(t, nil)
	f := newFixture(t, resolver{"odd": {URL: up.URL, Auth: "basic"}}, nil)
	rec := serve(f.h, req(http.MethodPost, "/mcp/odd", callBody))
	if rec.Code != http.StatusBadGateway || errCode(t, rec) != "internal" {
		t.Fatalf("status = %d", rec.Code)
	}
	if got.get().calls != 0 {
		t.Fatal("upstream called with unknown auth mode")
	}
}

func TestUnknownOrInvalidIDIs404(t *testing.T) {
	up, got := upstream(t, nil)
	f := newFixture(t, resolver{"svc-a": {URL: up.URL, Auth: catalogue.AuthNone}}, nil)
	for _, p := range []string{"/mcp/nope", "/mcp/", "/mcp", "/mcp/UPPER", "/mcp/svc-a/../x"} {
		rec := serve(f.h, req(http.MethodPost, p, callBody))
		if rec.Code != http.StatusNotFound || errCode(t, rec) != "not_found" {
			t.Errorf("%s: status = %d", p, rec.Code)
		}
	}
	if got.get().calls != 0 {
		t.Fatal("upstream called")
	}
	if n := len(f.audit.all()); n != 0 {
		t.Fatalf("unknown ids should not be audited, got %d", n)
	}
}

func TestOriginAndHostChecks(t *testing.T) {
	up, got := upstream(t, nil)
	f := newFixture(t, resolver{"svc-a": {URL: up.URL, Auth: catalogue.AuthHubJWT}}, nil)

	bad := []struct{ origin, host string }{
		{"https://evil.example", "127.0.0.1:7420"},
		{"http://127.0.0.1.evil.example:7420", "127.0.0.1:7420"},
		{"null", "127.0.0.1:7420"},
		{"", "evil.example:7420"},  // DNS rebinding: name resolves to 127.0.0.1
		{"", "localhost.evil:7420"},
	}
	for _, b := range bad {
		r := req(http.MethodPost, "/mcp/svc-a", callBody)
		r.Host = b.host
		if b.origin != "" {
			r.Header.Set("Origin", b.origin)
		}
		rec := serve(f.h, r)
		if rec.Code != http.StatusForbidden || errCode(t, rec) != "bad_request" {
			t.Errorf("origin=%q host=%q: status = %d", b.origin, b.host, rec.Code)
		}
	}
	if got.get().calls != 0 {
		t.Fatal("upstream called on rejected request")
	}

	good := []struct{ origin, host string }{
		{"", "127.0.0.1:7420"},
		{"http://localhost:5173", "localhost:7420"},
		{"http://127.0.0.1:7420", "127.0.0.1"},
		{"http://[::1]:7420", "[::1]:7420"},
	}
	for _, g := range good {
		r := req(http.MethodPost, "/mcp/svc-a", callBody)
		r.Host = g.host
		if g.origin != "" {
			r.Header.Set("Origin", g.origin)
		}
		if rec := serve(f.h, r); rec.Code != http.StatusOK {
			t.Errorf("origin=%q host=%q: status = %d", g.origin, g.host, rec.Code)
		}
	}
}

func TestBodyTooLarge(t *testing.T) {
	up, got := upstream(t, nil)
	f := newFixture(t, resolver{"svc-a": {URL: up.URL, Auth: catalogue.AuthNone}},
		func(c *proxy.Config) { c.MaxBodyBytes = 16 })

	rec := serve(f.h, req(http.MethodPost, "/mcp/svc-a", strings.Repeat("x", 17)))
	if rec.Code != http.StatusRequestEntityTooLarge || errCode(t, rec) != "bad_request" {
		t.Fatalf("status = %d", rec.Code)
	}
	if got.get().calls != 0 {
		t.Fatal("upstream called")
	}
	if e := f.audit.only(t); e.Status != http.StatusRequestEntityTooLarge || e.Server != "svc-a" {
		t.Fatalf("audit = %+v", e)
	}

	// Exactly at the limit is fine.
	if rec := serve(f.h, req(http.MethodPost, "/mcp/svc-a", strings.Repeat("x", 16))); rec.Code != http.StatusOK {
		t.Fatalf("at limit: status = %d", rec.Code)
	}
}

func TestDefaultMaxBody(t *testing.T) {
	if proxy.DefaultMaxBodyBytes != 4<<20 {
		t.Fatalf("DefaultMaxBodyBytes = %d", proxy.DefaultMaxBodyBytes)
	}
	up, _ := upstream(t, nil)
	f := newFixture(t, resolver{"svc-a": {URL: up.URL, Auth: catalogue.AuthNone}}, nil)
	rec := serve(f.h, req(http.MethodPost, "/mcp/svc-a", strings.Repeat(" ", 4<<20+1)))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestAuditRecordsMethodAndToolButNotArguments(t *testing.T) {
	up, _ := upstream(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) })
	f := newFixture(t, resolver{"svc-a": {URL: up.URL, Auth: catalogue.AuthHubJWT}}, nil)

	serve(f.h, req(http.MethodPost, "/mcp/svc-a", callBody))
	e := f.audit.only(t)
	if e.Server != "svc-a" || e.Method != "tools/call" || e.Tool != "find_service" ||
		e.Status != http.StatusAccepted || e.User != user || e.Time.IsZero() || e.DurationMs < 0 {
		t.Fatalf("audit = %+v", e)
	}
	b, _ := json.Marshal(e)
	if strings.Contains(string(b), "SECRET-ARG-VALUE") || strings.Contains(string(b), "Bearer") {
		t.Fatalf("audit leaks: %s", b)
	}
}

func TestAuditMethodVariants(t *testing.T) {
	up, _ := upstream(t, nil)
	f := newFixture(t, resolver{"svc-a": {URL: up.URL, Auth: catalogue.AuthNone}}, nil)

	cases := []struct{ method, body, wantMethod, wantTool string }{
		{http.MethodPost, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, "tools/list", ""},
		{http.MethodPost, `[{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"a"}},{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"b"}}]`, "tools/call", "a,b"},
		{http.MethodPost, `[{"jsonrpc":"2.0","id":1,"method":"ping"},{"jsonrpc":"2.0","id":2,"method":"tools/list"}]`, "ping,tools/list", ""},
		{http.MethodPost, `not json`, "POST", ""},
		{http.MethodPost, `{"jsonrpc":"2.0","id":1,"result":{}}`, "POST", ""},
		{http.MethodGet, ``, "GET", ""},
		{http.MethodDelete, ``, "DELETE", ""},
	}
	for _, tc := range cases {
		f.audit.events = nil
		serve(f.h, req(tc.method, "/mcp/svc-a", tc.body))
		e := f.audit.only(t)
		if e.Method != tc.wantMethod || e.Tool != tc.wantTool {
			t.Errorf("%s %q: method=%q tool=%q, want %q %q", tc.method, tc.body, e.Method, e.Tool, tc.wantMethod, tc.wantTool)
		}
	}
}

func TestAuditTruncatesLongNames(t *testing.T) {
	up, _ := upstream(t, nil)
	f := newFixture(t, resolver{"svc-a": {URL: up.URL, Auth: catalogue.AuthNone}}, nil)
	long := strings.Repeat("n", 500)
	serve(f.h, req(http.MethodPost, "/mcp/svc-a", `{"method":"tools/call","params":{"name":"`+long+`"}}`))
	if e := f.audit.only(t); len(e.Tool) != proxy.MaxAuditField {
		t.Fatalf("tool len = %d, want %d", len(e.Tool), proxy.MaxAuditField)
	}
}

func TestSessionHeadersRoundTripAndSetCookieStripped(t *testing.T) {
	up, got := upstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Mcp-Session-Id", "sess-"+r.Header.Get("Mcp-Session-Id"))
		w.Header().Set("Set-Cookie", "tracker=1")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	})
	f := newFixture(t, resolver{"svc-a": {URL: up.URL, Auth: catalogue.AuthNone}}, nil)

	r := req(http.MethodGet, "/mcp/svc-a/sub/path?x=1", "")
	r.Header.Set("Mcp-Session-Id", "abc")
	r.Header.Set("Last-Event-ID", "42")
	rec := serve(f.h, r)

	if rec.Header().Get("Mcp-Session-Id") != "sess-abc" {
		t.Fatalf("response session = %q", rec.Header().Get("Mcp-Session-Id"))
	}
	if rec.Header().Get("Set-Cookie") != "" {
		t.Fatal("upstream Set-Cookie passed to client")
	}
	g := got.get()
	if g.header.Get("Last-Event-ID") != "42" || g.path != "/sub/path" || g.query != "x=1" {
		t.Fatalf("upstream saw path=%q query=%q header=%v", g.path, g.query, g.header)
	}
}

func TestUpstreamDownIs502AndAudited(t *testing.T) {
	up, _ := upstream(t, nil)
	url := up.URL
	up.Close()
	f := newFixture(t, resolver{"svc-a": {URL: url, Auth: catalogue.AuthNone}}, nil)

	rec := serve(f.h, req(http.MethodPost, "/mcp/svc-a", callBody))
	if rec.Code != http.StatusBadGateway || errCode(t, rec) != "start_failed" {
		t.Fatalf("status = %d", rec.Code)
	}
	if e := f.audit.only(t); e.Status != http.StatusBadGateway || e.Method != "tools/call" {
		t.Fatalf("audit = %+v", e)
	}
}

func TestTokenFailureIs502AndUpstreamNotCalled(t *testing.T) {
	up, got := upstream(t, nil)
	f := newFixture(t, resolver{"svc-a": {URL: up.URL, Auth: catalogue.AuthHubJWT}},
		func(c *proxy.Config) { c.Tokens = failingTokens{} })

	rec := serve(f.h, req(http.MethodPost, "/mcp/svc-a", callBody))
	if rec.Code != http.StatusBadGateway || errCode(t, rec) != "internal" {
		t.Fatalf("status = %d", rec.Code)
	}
	if got.get().calls != 0 {
		t.Fatal("upstream called without credentials")
	}
	if e := f.audit.only(t); e.Status != http.StatusBadGateway {
		t.Fatalf("audit = %+v", e)
	}
}

func TestInvalidUpstreamURLFailsClosed(t *testing.T) {
	f := newFixture(t, resolver{"svc-a": {URL: "://bad", Auth: catalogue.AuthNone}}, nil)
	rec := serve(f.h, req(http.MethodPost, "/mcp/svc-a", callBody))
	if rec.Code != http.StatusBadGateway || errCode(t, rec) != "internal" {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestCustomTransportIsUsed(t *testing.T) {
	var calls atomic.Int32
	rt := roundTripper(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: 418, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	f := newFixture(t, resolver{"svc-a": {URL: "http://upstream.invalid", Auth: catalogue.AuthNone}},
		func(c *proxy.Config) { c.Transport = rt })
	rec := serve(f.h, req(http.MethodGet, "/mcp/svc-a", ""))
	if rec.Code != 418 || calls.Load() != 1 {
		t.Fatalf("status=%d calls=%d", rec.Code, calls.Load())
	}
}

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSSEIsStreamedBeforeUpstreamFinishes(t *testing.T) {
	release := make(chan struct{})
	up, _ := upstream(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: one\n\n")
		w.(http.Flusher).Flush()
		<-release
		_, _ = io.WriteString(w, "data: two\n\n")
	})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	f := newFixture(t, resolver{"svc-a": {URL: up.URL, Auth: catalogue.AuthHubJWT}}, nil)
	hub := httptest.NewServer(f.h)
	defer hub.Close()

	r, _ := http.NewRequest(http.MethodGet, hub.URL+"/mcp/svc-a", nil)
	r.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type = %q", ct)
	}

	lines := make(chan string, 4)
	go func() {
		s := bufio.NewScanner(resp.Body)
		for s.Scan() {
			lines <- s.Text()
		}
		close(lines)
	}()
	select {
	case l := <-lines:
		if l != "data: one" {
			t.Fatalf("first line = %q", l)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("first SSE event not flushed while upstream still open")
	}
	close(release)
	var rest []string
	for l := range lines {
		rest = append(rest, l)
	}
	if !strings.Contains(strings.Join(rest, "\n"), "data: two") {
		t.Fatalf("rest = %q", rest)
	}
	// Audit happens after the stream completes.
	deadline := time.Now().Add(2 * time.Second)
	for len(f.audit.all()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if e := f.audit.only(t); e.Method != "GET" || e.Status != 200 {
		t.Fatalf("audit = %+v", e)
	}
}
