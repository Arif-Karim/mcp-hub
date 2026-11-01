package server_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Arif-Karim/mcp-hub/internal/audit"
	"github.com/Arif-Karim/mcp-hub/internal/server"
)

func TestProxyJWKSAndAuditRoutes(t *testing.T) {
	log, err := audit.NewLog(filepath.Join(t.TempDir(), "audit.jsonl"), 10)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = log.Close() }()
	log.Record(audit.Event{Server: "svc", Method: "tools/list", Status: 200, User: "u"})

	tag := func(name string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(name + ":" + r.URL.Path))
		})
	}
	h := server.New(server.Options{Version: "x", Proxy: tag("proxy"), JWKS: tag("jwks"), Audit: log})

	cases := []struct{ method, path, want string }{
		{http.MethodPost, "/mcp/svc-a", "proxy:/mcp/svc-a"},
		{http.MethodGet, "/mcp/svc-a/stream", "proxy:/mcp/svc-a/stream"},
		{http.MethodDelete, "/mcp/svc-a", "proxy:/mcp/svc-a"},
		{http.MethodGet, "/.well-known/jwks.json", "jwks:/.well-known/jwks.json"},
		{http.MethodGet, "/api/v1/audit?limit=5", `"method":"tools/list"`},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), tc.want) {
			t.Errorf("%s %s: %d %q, want %q", tc.method, tc.path, rec.Code, rec.Body.String(), tc.want)
		}
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/audit", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST audit = %d, want 405", rec.Code)
	}
}

func TestLaneCRoutesAbsentWhenNotConfigured(t *testing.T) {
	h := server.New(server.Options{Version: "x"})
	for _, p := range []string{"/mcp/svc-a", "/.well-known/jwks.json", "/api/v1/audit"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", p, rec.Code)
		}
	}
}
