package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Arif-Karim/mcp-hub/internal/server"
)

func TestHealthReturnsOKAndVersion(t *testing.T) {
	h := server.New(server.Options{Version: "1.2.3"})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, localReq(http.MethodGet, "/api/v1/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q, want application/json", ct)
	}
	var body struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "ok" || body.Version != "1.2.3" {
		t.Fatalf("body = %+v, want status ok version 1.2.3", body)
	}
}

func TestHealthRejectsNonGET(t *testing.T) {
	h := server.New(server.Options{Version: "x"})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, localReq(http.MethodPost, "/api/v1/health", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestUnknownAPIRouteReturnsJSON404(t *testing.T) {
	h := server.New(server.Options{Version: "x"})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, localReq(http.MethodGet, "/api/v1/nope", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error.Code != "not_found" {
		t.Fatalf("error code = %q, want not_found", body.Error.Code)
	}
}
