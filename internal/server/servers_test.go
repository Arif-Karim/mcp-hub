package server_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/Arif-Karim/mcp-hub/internal/catalogue"
	"github.com/Arif-Karim/mcp-hub/internal/claudeconfig"
	"github.com/Arif-Karim/mcp-hub/internal/httpx"
	"github.com/Arif-Karim/mcp-hub/internal/registry"
	"github.com/Arif-Karim/mcp-hub/internal/runner"
	"github.com/Arif-Karim/mcp-hub/internal/server"
	"github.com/Arif-Karim/mcp-hub/internal/skillfiles"
)

type fakeEnabler struct {
	err   error
	calls []string
}

func (f *fakeEnabler) Enable(_ context.Context, id string) (catalogue.InstallState, error) {
	f.calls = append(f.calls, "enable "+id)
	return catalogue.InstallState{
		Enabled: true, Status: catalogue.StatusRunning, ConfigName: "hub__" + id,
		ProxyURL: "http://127.0.0.1:7420/mcp/" + id, EnabledAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}, f.err
}

func (f *fakeEnabler) Disable(_ context.Context, id string) (catalogue.InstallState, error) {
	f.calls = append(f.calls, "disable "+id)
	return catalogue.InstallState{Status: catalogue.StatusDisabled, ConfigName: "hub__" + id}, f.err
}

func TestEnableDisableRoutes(t *testing.T) {
	f := &fakeEnabler{}
	h := server.New(server.Options{Catalogue: testCatalogue(), Servers: f})

	rec, body := do(t, h, http.MethodPost, "/api/v1/servers/svc/enable")
	if rec.Code != http.StatusOK {
		t.Fatalf("enable = %d %v", rec.Code, body)
	}
	if body["id"] != "svc" || body["name"] != "Service Directory" || body["kind"] != "local" {
		t.Fatalf("item = %v", body)
	}
	want := map[string]any{
		"enabled": true, "status": "running", "configName": "hub__svc",
		"proxyUrl": "http://127.0.0.1:7420/mcp/svc", "enabledAt": "2026-01-02T03:04:05Z",
	}
	if !reflect.DeepEqual(body["state"], want) {
		t.Fatalf("state = %v\nwant    %v", body["state"], want)
	}
	if _, leaked := body["artifacts"]; leaked {
		t.Fatal("artifacts leaked into the API")
	}

	rec, body = do(t, h, http.MethodPost, "/api/v1/servers/wiki/disable")
	if rec.Code != http.StatusOK || body["id"] != "wiki" {
		t.Fatalf("disable = %d %v", rec.Code, body)
	}
	if st := body["state"].(map[string]any); st["status"] != "disabled" || st["enabled"] != false {
		t.Fatalf("disable state = %v", st)
	}
	if got := fmt.Sprint(f.calls); got != "[enable svc disable wiki]" {
		t.Fatalf("calls = %s", got)
	}
}

func TestEnableUnknownID(t *testing.T) {
	f := &fakeEnabler{}
	h := server.New(server.Options{Catalogue: testCatalogue(), Servers: f})
	for _, action := range []string{"enable", "disable"} {
		rec, body := do(t, h, http.MethodPost, "/api/v1/servers/nope/"+action)
		if rec.Code != http.StatusNotFound || errCode(body) != httpx.CodeNotFound {
			t.Fatalf("%s unknown = %d %v", action, rec.Code, body)
		}
	}
	if len(f.calls) != 0 {
		t.Fatalf("service called for an unknown id: %v", f.calls)
	}
}

func TestEnableErrorsMapToContract(t *testing.T) {
	cases := []struct {
		err     error
		status  int
		code    string
		message string
	}{
		{fmt.Errorf("%w: svc is revoked", catalogue.ErrNotApproved), 403, httpx.CodeNotApproved, "catalogue entry not approved: svc is revoked"},
		{fmt.Errorf("%w: edited", claudeconfig.ErrConfigConflict), 409, httpx.CodeConfigConflict, "claude config conflict: edited"},
		{claudeconfig.ErrConfigInvalid, 422, httpx.CodeConfigInvalid, "claude config is invalid"},
		{fmt.Errorf("%w: boom", runner.ErrStartFailed), 502, httpx.CodeStartFailed, "mcp server failed to start: boom"},
		{registry.ErrChecksumMismatch, 422, httpx.CodeChecksumMismatch, "artifact checksum mismatch"},
		{errors.New("open /Users/secret/path: denied"), 500, httpx.CodeInternal, "internal error"},
	}
	for _, tc := range cases {
		h := server.New(server.Options{Catalogue: testCatalogue(), Servers: &fakeEnabler{err: tc.err}})
		rec, body := do(t, h, http.MethodPost, "/api/v1/servers/svc/enable")
		msg, _ := body["error"].(map[string]any)["message"].(string)
		if rec.Code != tc.status || errCode(body) != tc.code || msg != tc.message {
			t.Errorf("%v → %d %s %q, want %d %s %q", tc.err, rec.Code, errCode(body), msg, tc.status, tc.code, tc.message)
		}
	}
}

func TestEnableRequiresPOST(t *testing.T) {
	h := server.New(server.Options{Catalogue: testCatalogue(), Servers: &fakeEnabler{}})
	for _, target := range []string{"/api/v1/servers/svc/enable", "/api/v1/servers/svc/disable"} {
		rec, body := do(t, h, http.MethodGet, target)
		if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "POST" || errCode(body) != httpx.CodeBadRequest {
			t.Fatalf("GET %s = %d allow=%q %v", target, rec.Code, rec.Header().Get("Allow"), body)
		}
	}
}

func TestEnableRejectsCrossSite(t *testing.T) {
	f := &fakeEnabler{}
	h := server.New(server.Options{Catalogue: testCatalogue(), Servers: f})
	r := localReq(http.MethodPost, "/api/v1/servers/svc/enable", nil)
	r.Header.Set("Origin", "https://evil.test")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden || len(f.calls) != 0 {
		t.Fatalf("cross-site enable = %d, calls %v", rec.Code, f.calls)
	}
}

func TestServerRoutesDisabledWithoutService(t *testing.T) {
	h := server.New(server.Options{Catalogue: testCatalogue()})
	rec, body := do(t, h, http.MethodPost, "/api/v1/servers/svc/enable")
	if rec.Code != http.StatusNotFound || errCode(body) != httpx.CodeNotFound {
		t.Fatalf("enable without service = %d %v", rec.Code, body)
	}
	rec, _ = do(t, h, http.MethodGet, "/api/v1/status")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status without provider = %d", rec.Code)
	}
}

func TestStatusRoute(t *testing.T) {
	h := server.New(server.Options{Status: func() server.Status {
		return server.Status{
			Version: "1.0.0", Addr: "127.0.0.1:7420", ClaudeConfigPath: "/home/dev/.claude.json",
			EnabledCount: 2, CatalogueSource: "embedded", User: "dev",
		}
	}})
	rec, body := do(t, h, http.MethodGet, "/api/v1/status")
	want := map[string]any{
		"version": "1.0.0", "addr": "127.0.0.1:7420", "claudeConfigPath": "/home/dev/.claude.json",
		"enabledCount": float64(2), "catalogueSource": "embedded", "user": "dev",
	}
	if rec.Code != http.StatusOK || !reflect.DeepEqual(body, want) {
		t.Fatalf("status = %d %v", rec.Code, body)
	}
	rec, _ = do(t, h, http.MethodPost, "/api/v1/status")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d", rec.Code)
	}
}

func TestStatusIncludesSkill(t *testing.T) {
	sk := &fakeSkill{state: skillfiles.State{Installed: true, UpToDate: false, Path: "/home/dev/.claude/skills/mcp-hub/SKILL.md"}}
	h := server.New(server.Options{Status: func() server.Status { return server.Status{Version: "1"} }, Skill: sk})
	rec, body := do(t, h, http.MethodGet, "/api/v1/status")
	want := map[string]any{"installed": true, "upToDate": false, "path": "/home/dev/.claude/skills/mcp-hub/SKILL.md"}
	if rec.Code != http.StatusOK || !reflect.DeepEqual(body["skill"], want) {
		t.Fatalf("status = %d skill=%v, want %v", rec.Code, body["skill"], want)
	}
}
