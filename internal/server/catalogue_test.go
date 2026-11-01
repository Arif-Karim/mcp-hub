package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/Arif-Karim/mcp-hub/internal/catalogue"
	"github.com/Arif-Karim/mcp-hub/internal/server"
)

func testCatalogue() *catalogue.Store {
	return catalogue.NewStore(&catalogue.Catalogue{Version: 1, Entries: []catalogue.Entry{
		{
			ID: "svc", Name: "Service Directory", Description: "find services", Owner: "o", Version: "0.1.0",
			Kind: catalogue.KindLocal, Classification: catalogue.ClassInternal, Approval: catalogue.ApprovalApproved,
			Auth: catalogue.AuthHubJWT, Tags: []string{"platform"},
			Tools:     []catalogue.Tool{{Name: "find_service", Description: "d"}},
			Artifacts: map[string]catalogue.Artifact{"linux-amd64": {URL: "https://secret.test/a", SHA256: "ab"}},
		},
		{
			ID: "wiki", Name: "Wiki", Description: "docs", Owner: "o", Version: "1",
			Kind: catalogue.KindRemote, Classification: catalogue.ClassPublic, Approval: catalogue.ApprovalPending,
			Auth: catalogue.AuthNone, URL: "https://mcp.example.test/mcp",
		},
	}})
}

func do(t *testing.T, h http.Handler, method, target string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("%s %s content-type = %q", method, target, ct)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", rec.Body, err)
	}
	return rec, body
}

func errCode(body map[string]any) string {
	e, _ := body["error"].(map[string]any)
	c, _ := e["code"].(string)
	return c
}

func itemIDs(body map[string]any) []string {
	out := []string{}
	items, _ := body["items"].([]any)
	for _, it := range items {
		out = append(out, it.(map[string]any)["id"].(string))
	}
	return out
}

func TestListCatalogue(t *testing.T) {
	h := server.New(server.Options{Catalogue: testCatalogue()})
	cases := []struct {
		target string
		want   []string
	}{
		{"/api/v1/catalogue", []string{"svc", "wiki"}},
		{"/api/v1/catalogue?kind=remote", []string{"wiki"}},
		{"/api/v1/catalogue?kind=local", []string{"svc"}},
		{"/api/v1/catalogue?q=FIND_", []string{"svc"}},
		{"/api/v1/catalogue?q=nomatch", []string{}},
	}
	for _, tc := range cases {
		rec, body := do(t, h, http.MethodGet, tc.target)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d", tc.target, rec.Code)
		}
		if got := itemIDs(body); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: ids = %v, want %v", tc.target, got, tc.want)
		}
	}
	// Empty result is [] not null, per the contract (items is required).
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/catalogue?q=nomatch", nil))
	if got := strings.TrimSpace(rec.Body.String()); got != `{"items":[]}` {
		t.Fatalf("empty body = %s", got)
	}
}

func TestListCatalogueRejectsBadKind(t *testing.T) {
	h := server.New(server.Options{Catalogue: testCatalogue()})
	rec, body := do(t, h, http.MethodGet, "/api/v1/catalogue?kind=cloud")
	if rec.Code != http.StatusBadRequest || errCode(body) != "bad_request" {
		t.Fatalf("status = %d code = %q, want 400 bad_request", rec.Code, errCode(body))
	}
}

func TestGetCatalogueItemShape(t *testing.T) {
	h := server.New(server.Options{Catalogue: testCatalogue()})
	rec, body := do(t, h, http.MethodGet, "/api/v1/catalogue/svc")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	for _, k := range []string{"id", "name", "description", "owner", "version", "kind", "classification", "approval", "auth", "tags", "tools", "state"} {
		if _, ok := body[k]; !ok {
			t.Errorf("missing required field %q in %v", k, body)
		}
	}
	if _, ok := body["artifacts"]; ok || strings.Contains(rec.Body.String(), "secret.test") {
		t.Fatalf("artifacts leaked: %s", rec.Body)
	}
	want := map[string]any{"enabled": false, "status": "disabled", "configName": "hub__svc"}
	if !reflect.DeepEqual(body["state"], want) {
		t.Fatalf("state = %v, want %v", body["state"], want)
	}

	// Remote entry: upstreamUrl present, nil tags/tools serialised as [].
	_, body = do(t, h, http.MethodGet, "/api/v1/catalogue/wiki")
	if body["upstreamUrl"] != "https://mcp.example.test/mcp" {
		t.Errorf("upstreamUrl = %v", body["upstreamUrl"])
	}
	if !reflect.DeepEqual(body["tags"], []any{}) || !reflect.DeepEqual(body["tools"], []any{}) {
		t.Errorf("tags = %v tools = %v, want []", body["tags"], body["tools"])
	}
}

func TestGetCatalogueItemNotFound(t *testing.T) {
	h := server.New(server.Options{Catalogue: testCatalogue()})
	rec, body := do(t, h, http.MethodGet, "/api/v1/catalogue/nope")
	if rec.Code != http.StatusNotFound || errCode(body) != "not_found" {
		t.Fatalf("status = %d code = %q, want 404 not_found", rec.Code, errCode(body))
	}
}

func TestCatalogueUsesStateFunc(t *testing.T) {
	h := server.New(server.Options{
		Catalogue: testCatalogue(),
		State: func(id string) catalogue.InstallState {
			return catalogue.InstallState{Enabled: true, Status: catalogue.StatusReady, ConfigName: catalogue.ConfigName(id), ProxyURL: "http://127.0.0.1:7420/mcp/" + id}
		},
	})
	_, body := do(t, h, http.MethodGet, "/api/v1/catalogue")
	items := body["items"].([]any)
	state := items[1].(map[string]any)["state"].(map[string]any)
	if state["enabled"] != true || state["status"] != "ready" || state["proxyUrl"] != "http://127.0.0.1:7420/mcp/wiki" || state["configName"] != "hub__wiki" {
		t.Fatalf("state = %v", state)
	}
}

func TestCatalogueRejectsWrongMethod(t *testing.T) {
	h := server.New(server.Options{Catalogue: testCatalogue()})
	for _, target := range []string{"/api/v1/catalogue", "/api/v1/catalogue/svc"} {
		rec, body := do(t, h, http.MethodPost, target)
		if rec.Code != http.StatusMethodNotAllowed || errCode(body) != "bad_request" {
			t.Errorf("POST %s: status = %d code = %q, want 405 bad_request", target, rec.Code, errCode(body))
		}
	}
}

func TestCatalogueRoutesAbsentWithoutStore(t *testing.T) {
	h := server.New(server.Options{})
	rec, body := do(t, h, http.MethodGet, "/api/v1/catalogue")
	if rec.Code != http.StatusNotFound || errCode(body) != "not_found" {
		t.Fatalf("status = %d code = %q, want 404 not_found", rec.Code, errCode(body))
	}
}
