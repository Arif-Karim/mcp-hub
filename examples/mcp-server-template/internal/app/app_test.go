package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Arif-Karim/mcp-hub/examples/mcp-server-template/internal/hubauth"
)

const (
	kid = "k1"
	aud = "service-directory"
	sub = "dev.user@example.test"
)

// syncBuffer is a goroutine-safe log sink (the SDK handles requests concurrently).
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

type bearer struct {
	token string
	base  http.RoundTripper
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.base.RoundTrip(r)
}

type env struct {
	srv   *httptest.Server
	token string
	logs  *syncBuffer
}

func newEnv(t *testing.T) *env {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	jwks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "OKP", "crv": "Ed25519", "alg": "EdDSA", "use": "sig", "kid": kid,
			"x": base64.RawURLEncoding.EncodeToString(pub),
		}}})
	}))
	t.Cleanup(jwks.Close)

	tok := jwt.NewWithClaims(jwt.SigningMethodEdDSA, jwt.MapClaims{
		"sub": sub, "iss": "mcp-hub", "aud": aud,
		"iat": time.Now().Unix(), "exp": time.Now().Add(5 * time.Minute).Unix(),
	})
	tok.Header["kid"] = kid
	signed, err := tok.SignedString(priv)
	if err != nil {
		t.Fatal(err)
	}

	logs := &syncBuffer{}
	v := &hubauth.Verifier{Keys: hubauth.NewJWKS(jwks.URL, nil), Issuer: "mcp-hub", Audience: aud}
	srv := httptest.NewServer(Handler(v, slog.New(slog.NewJSONHandler(logs, nil))))
	t.Cleanup(srv.Close)
	return &env{srv: srv, token: signed, logs: logs}
}

func (e *env) connect(t *testing.T) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:             e.srv.URL + MCPPath,
		HTTPClient:           &http.Client{Transport: bearer{token: e.token, base: http.DefaultTransport}},
		DisableStandaloneSSE: true,
		MaxRetries:           -1,
	}
	cs, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	return res
}

func text(res *mcp.CallToolResult) string {
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

// names decodes find_service structured output into the list of service names.
func names(t *testing.T, res *mcp.CallToolResult) []string {
	t.Helper()
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Services []struct {
			Name    string   `json:"name"`
			Team    string   `json:"team"`
			Tags    []string `json:"tags"`
			Channel string   `json:"channel"`
			Tier    int      `json:"tier"`
			Runbook string   `json:"runbook"`
		} `json:"services"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	var n []string
	for _, s := range out.Services {
		if s.Team == "" || s.Channel == "" || s.Tier == 0 || len(s.Tags) == 0 {
			t.Errorf("incomplete summary: %+v", s)
		}
		if s.Runbook != "" {
			t.Errorf("find_service should not return runbooks")
		}
		n = append(n, s.Name)
	}
	slices.Sort(n)
	return n
}

func TestHealthzNoAuth(t *testing.T) {
	e := newEnv(t)
	resp, err := http.Get(e.srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != `{"status":"ok"}` {
		t.Fatalf("got %d %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type = %q", ct)
	}
}

func TestMCPRequiresAuth(t *testing.T) {
	e := newEnv(t)
	resp, err := http.Post(e.srv.URL+MCPPath, "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestToolsList(t *testing.T) {
	cs := newEnv(t).connect(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tool := range res.Tools {
		got = append(got, tool.Name)
		if tool.Description == "" {
			t.Errorf("%s has no description", tool.Name)
		}
	}
	slices.Sort(got)
	if !slices.Equal(got, []string{"find_service", "get_runbook"}) {
		t.Fatalf("tools = %v", got)
	}
}

func TestFindService(t *testing.T) {
	e := newEnv(t)
	cs := e.connect(t)
	cases := []struct {
		name string
		args map[string]any
		want []string
	}{
		{"by name substring", map[string]any{"query": "ledger"}, []string{"ledger-api"}},
		{"case insensitive", map[string]any{"query": "FX"}, []string{"fx-rates"}},
		{"by team", map[string]any{"query": "financial-crime"}, []string{"fraud-scoring"}},
		{"by tag", map[string]any{"tag": "payments"}, []string{"fraud-scoring", "ledger-api"}},
		{"tag is exact", map[string]any{"tag": "pay"}, nil},
		{"query and tag", map[string]any{"query": "fraud", "tag": "payments"}, []string{"fraud-scoring"}},
		{"no match", map[string]any{"query": "does-not-exist"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := call(t, cs, "find_service", tc.args)
			if res.IsError {
				t.Fatalf("IsError: %s", text(res))
			}
			if got := names(t, res); !slices.Equal(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}

	logs := e.logs.String()
	if !strings.Contains(logs, `"tool":"find_service"`) || !strings.Contains(logs, `"sub":"`+sub+`"`) {
		t.Errorf("tool call not logged with sub + tool: %s", logs)
	}
	if strings.Contains(logs, "financial-crime") || strings.Contains(logs, "does-not-exist") {
		t.Errorf("tool arguments leaked into logs: %s", logs)
	}
}

func TestFindServiceEmptyIsValidationError(t *testing.T) {
	cs := newEnv(t).connect(t)
	res := call(t, cs, "find_service", map[string]any{"query": " ", "tag": ""})
	if !res.IsError || !strings.Contains(text(res), "query or tag is required") {
		t.Fatalf("got IsError=%v %q", res.IsError, text(res))
	}
}

func TestGetRunbook(t *testing.T) {
	cs := newEnv(t).connect(t)
	res := call(t, cs, "get_runbook", map[string]any{"service": "ledger-api"})
	if res.IsError {
		t.Fatalf("IsError: %s", text(res))
	}
	if !strings.HasPrefix(text(res), "# ledger-api runbook") {
		t.Fatalf("runbook = %q", text(res))
	}
}

func TestGetRunbookUnknownIsToolError(t *testing.T) {
	cs := newEnv(t).connect(t)
	res := call(t, cs, "get_runbook", map[string]any{"service": "no-such-service"})
	if !res.IsError || !strings.Contains(text(res), "unknown service") {
		t.Fatalf("got IsError=%v %q", res.IsError, text(res))
	}
}
