package proxy_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Arif-Karim/mcp-hub/internal/catalogue"
	"github.com/Arif-Karim/mcp-hub/internal/proxy"
)

func TestProxiedResponsesCarrySecurityHeaders(t *testing.T) {
	up, _ := upstream(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src *")
		w.Header().Set("X-Content-Type-Options", "whatever")
		_, _ = io.WriteString(w, `{}`)
	})
	f := newFixture(t, resolver{"svc-a": {URL: up.URL, Auth: catalogue.AuthNone}}, nil)
	rec := serve(f.h, req(http.MethodPost, "/mcp/svc-a", callBody))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	assertSecurityHeaders(t, rec.Header())
}

func assertSecurityHeaders(t *testing.T, h http.Header) {
	t.Helper()
	if v := h.Values("X-Content-Type-Options"); len(v) != 1 || v[0] != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", v)
	}
	if v := h.Values("Content-Security-Policy"); len(v) != 1 || v[0] != "sandbox; default-src 'none'" {
		t.Errorf("Content-Security-Policy = %q", v)
	}
}

func TestUnexpectedUpstreamContentTypeIsBlocked(t *testing.T) {
	for _, ct := range []string{"text/html", "text/html; charset=utf-8", "image/svg+xml", "text/plain", "application/javascript", "application/jsonx"} {
		up, _ := upstream(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", ct)
			_, _ = io.WriteString(w, `<script>fetch('/api/v1/servers/x/enable',{method:'POST'})</script>`)
		})
		f := newFixture(t, resolver{"svc-a": {URL: up.URL, Auth: catalogue.AuthNone}}, nil)
		rec := serve(f.h, req(http.MethodGet, "/mcp/svc-a/x", ""))
		if rec.Code != http.StatusBadGateway {
			t.Errorf("%s: status = %d, want 502", ct, rec.Code)
			continue
		}
		if strings.Contains(rec.Body.String(), "<script>") {
			t.Errorf("%s: upstream body leaked", ct)
		}
		if got := rec.Header().Get("Content-Type"); got != "application/json" {
			t.Errorf("%s: content-type = %q", ct, got)
		}
		assertSecurityHeaders(t, rec.Header())
		if code := errCode(t, rec); code != "start_failed" {
			t.Errorf("%s: code = %q", ct, code)
		}
		if e := f.audit.only(t); e.Status != http.StatusBadGateway {
			t.Errorf("%s: audit = %+v", ct, e)
		}
	}
}

func TestEmptyBodyWithoutContentTypeIsAllowed(t *testing.T) {
	for _, code := range []int{http.StatusOK, http.StatusAccepted, http.StatusNoContent} {
		up, _ := upstream(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) })
		f := newFixture(t, resolver{"svc-a": {URL: up.URL, Auth: catalogue.AuthNone}}, nil)
		if rec := serve(f.h, req(http.MethodPost, "/mcp/svc-a", callBody)); rec.Code != code {
			t.Errorf("status = %d, want %d", rec.Code, code)
		}
	}
	// A body with no content type is not trusted.
	up, _ := upstream(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header()["Content-Type"] = nil // stop the test server sniffing one
		_, _ = io.WriteString(w, "<html>")
	})
	f := newFixture(t, resolver{"svc-a": {URL: up.URL, Auth: catalogue.AuthNone}}, nil)
	if rec := serve(f.h, req(http.MethodGet, "/mcp/svc-a", "")); rec.Code != http.StatusBadGateway {
		t.Fatalf("untyped body: status = %d, want 502", rec.Code)
	}
}

// MCP notifications are answered with 202 and no body. Some servers (e.g.
// Context7) send that chunked, so the length is unknown rather than zero.
func TestAcceptedWithoutContentTypeIsAllowedWhenChunked(t *testing.T) {
	up, _ := upstream(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		w.(http.Flusher).Flush() // forces chunked encoding: no Content-Length
	})
	f := newFixture(t, resolver{"svc-a": {URL: up.URL, Auth: catalogue.AuthNone}}, nil)
	if rec := serve(f.h, req(http.MethodPost, "/mcp/svc-a", callBody)); rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rec.Code)
	}
	if e := f.audit.only(t); e.Status != http.StatusAccepted {
		t.Errorf("audit = %+v", e)
	}
}

// A 202 carries no content per the MCP spec, so any untyped body is dropped
// rather than forwarded.
func TestAcceptedUntypedBodyIsDropped(t *testing.T) {
	up, _ := upstream(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header()["Content-Type"] = nil
		w.WriteHeader(http.StatusAccepted)
		w.(http.Flusher).Flush() // chunked, with a body following
		_, _ = io.WriteString(w, "<script>alert(1)</script>")
	})
	f := newFixture(t, resolver{"svc-a": {URL: up.URL, Auth: catalogue.AuthNone}}, nil)
	rec := serve(f.h, req(http.MethodPost, "/mcp/svc-a", callBody))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want empty", rec.Body.String())
	}
	assertSecurityHeaders(t, rec.Header())
	if e := f.audit.only(t); e.Status != http.StatusAccepted {
		t.Errorf("audit = %+v", e)
	}
}

func TestUpstreamAuthRejectionIsMappedTo502(t *testing.T) {
	for _, code := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		up, _ := upstream(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="https://evil.example/.well-known/oauth-protected-resource"`)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(code)
			_, _ = io.WriteString(w, `{}`)
		})
		f := newFixture(t, resolver{"svc-a": {URL: up.URL, Auth: catalogue.AuthHubJWT}}, nil)
		rec := serve(f.h, req(http.MethodPost, "/mcp/svc-a", callBody))
		if rec.Code != http.StatusBadGateway || errCode(t, rec) != "start_failed" {
			t.Errorf("%d: status = %d", code, rec.Code)
		}
		if v := rec.Header().Get("WWW-Authenticate"); v != "" {
			t.Errorf("%d: WWW-Authenticate leaked: %q", code, v)
		}
		if e := f.audit.only(t); e.Status != code {
			t.Errorf("%d: audit status = %d, want upstream status", code, e.Status)
		}
	}
}

func TestWWWAuthenticateStrippedOnOtherStatuses(t *testing.T) {
	up, _ := upstream(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="x"`)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	})
	f := newFixture(t, resolver{"svc-a": {URL: up.URL, Auth: catalogue.AuthNone}}, nil)
	rec := serve(f.h, req(http.MethodPost, "/mcp/svc-a", callBody))
	if rec.Code != http.StatusOK || rec.Header().Get("WWW-Authenticate") != "" {
		t.Fatalf("status = %d www-auth = %q", rec.Code, rec.Header().Get("WWW-Authenticate"))
	}
}

func TestHubJWTNeverSentOverPlaintextToRemoteHost(t *testing.T) {
	var calls atomic.Int32
	rt := roundTripper(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader("{}")), Request: r}, nil
	})
	cfg := func(c *proxy.Config) { c.Transport = rt }

	f := newFixture(t, resolver{"svc-a": {URL: "http://mcp.example.test/mcp", Auth: catalogue.AuthHubJWT}}, cfg)
	rec := serve(f.h, req(http.MethodPost, "/mcp/svc-a", callBody))
	if rec.Code != http.StatusBadGateway || errCode(t, rec) != "internal" || calls.Load() != 0 {
		t.Fatalf("plaintext remote: status = %d calls = %d", rec.Code, calls.Load())
	}
	if e := f.audit.only(t); e.Status != http.StatusBadGateway {
		t.Fatalf("audit = %+v", e)
	}

	// https remote and http loopback are fine; auth none over http remote is fine.
	ok := resolver{
		"tls":   {URL: "https://mcp.example.test/mcp", Auth: catalogue.AuthHubJWT},
		"local": {URL: "http://localhost:9999/mcp", Auth: catalogue.AuthHubJWT},
		"pub":   {URL: "http://mcp.example.test/mcp", Auth: catalogue.AuthNone},
	}
	f = newFixture(t, ok, cfg)
	for id := range ok {
		if rec := serve(f.h, req(http.MethodPost, "/mcp/"+id, callBody)); rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d", id, rec.Code)
		}
	}
}

func TestClientDisconnectIsAudited499(t *testing.T) {
	started := make(chan struct{})
	up, _ := upstream(t, func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	})
	f := newFixture(t, resolver{"svc-a": {URL: up.URL, Auth: catalogue.AuthNone}}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	r := req(http.MethodPost, "/mcp/svc-a", callBody).WithContext(ctx)
	done := make(chan struct{})
	go func() { serve(f.h, r); close(done) }()
	<-started
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("proxy did not return after client cancel")
	}
	if proxy.StatusClientClosed != 499 {
		t.Fatalf("StatusClientClosed = %d", proxy.StatusClientClosed)
	}
	if e := f.audit.only(t); e.Status != 499 {
		t.Fatalf("audit status = %d, want 499", e.Status)
	}
}

func TestBackslashPathSegmentsRejected(t *testing.T) {
	up, got := upstream(t, nil)
	f := newFixture(t, resolver{"svc-a": {URL: up.URL, Auth: catalogue.AuthNone}}, nil)
	rec := serve(f.h, req(http.MethodGet, `/mcp/svc-a/..%5Cx`, ""))
	if rec.Code != http.StatusNotFound || got.get().calls != 0 {
		t.Fatalf("status = %d calls = %d", rec.Code, got.get().calls)
	}
}
