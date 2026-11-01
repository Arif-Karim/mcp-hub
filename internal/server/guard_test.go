package server_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Arif-Karim/mcp-hub/internal/audit"
	"github.com/Arif-Karim/mcp-hub/internal/server"
)

// localReq builds a request as Claude Code / the UI would send it: Host is the loopback daemon.
func localReq(method, target string, body io.Reader) *http.Request {
	r := httptest.NewRequest(method, target, body)
	r.Host = "127.0.0.1:7420"
	return r
}

func guarded(t *testing.T) http.Handler {
	t.Helper()
	log, err := audit.NewLog(filepath.Join(t.TempDir(), "a.jsonl"), 5)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	return server.New(server.Options{Version: "x", Audit: log, Proxy: ok})
}

func TestGuardRejectsRebindingHost(t *testing.T) {
	h := guarded(t)
	for _, host := range []string{"evil.example:7420", "localhost.evil.example", "10.0.0.5:7420", ""} {
		r := localReq(http.MethodGet, "/api/v1/audit", nil)
		r.Host = host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != http.StatusForbidden {
			t.Errorf("host %q: status = %d, want 403", host, rec.Code)
			continue
		}
		var body struct {
			Error struct{ Code string } `json:"error"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil || body.Error.Code != "bad_request" {
			t.Errorf("host %q: body code = %q (%v)", host, body.Error.Code, err)
		}
	}
	for _, host := range []string{"127.0.0.1:7420", "localhost:7420", "[::1]:7420", "127.0.0.1"} {
		r := localReq(http.MethodGet, "/api/v1/audit", nil)
		r.Host = host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != http.StatusOK {
			t.Errorf("host %q: status = %d, want 200", host, rec.Code)
		}
	}
}

func TestGuardRejectsCrossSiteStateChangingRequests(t *testing.T) {
	h := guarded(t)
	cases := []struct {
		method, origin string
		want           int
	}{
		{http.MethodPost, "https://evil.example", http.StatusForbidden},
		{http.MethodPost, "null", http.StatusForbidden},
		{http.MethodDelete, "http://evil.example:7420", http.StatusForbidden},
		{http.MethodPut, "http://127.0.0.1.evil.example", http.StatusForbidden},
		{http.MethodPost, "http://127.0.0.1:7420", http.StatusNoContent}, // same origin (UI)
		{http.MethodPost, "http://localhost:5173", http.StatusNoContent}, // vite dev server
		{http.MethodPost, "", http.StatusNoContent},                      // CLI / curl / Claude Code
		{http.MethodGet, "https://evil.example", http.StatusNoContent},   // safe method; Host check still applies
		{http.MethodHead, "null", http.StatusNoContent},
	}
	for _, tc := range cases {
		r := localReq(tc.method, "/mcp/svc", nil)
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != tc.want {
			t.Errorf("%s origin=%q: status = %d, want %d", tc.method, tc.origin, rec.Code, tc.want)
		}
	}
}
