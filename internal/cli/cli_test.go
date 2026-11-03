package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const itemJSON = `{"id":"service-directory","name":"Service Directory","description":"Find who owns a service.",
"owner":"platform-engineering","version":"0.1.0","kind":"local","classification":"internal","approval":"approved",
"auth":"hub-jwt","tags":["ownership","services"],"tools":[{"name":"find_service","description":"Look up a service by name."}],
"docsUrl":"https://example.test/docs","state":{"enabled":true,"status":"running","configName":"hub__service-directory",
"proxyUrl":"http://127.0.0.1:7420/mcp/service-directory","lastError":"previous start timed out"}}`

const remoteJSON = `{"id":"deepwiki","name":"DeepWiki","description":"Docs for public repos.","owner":"dev-experience",
"version":"1.0.0","kind":"remote","classification":"public","approval":"pending","auth":"none","tags":[],"tools":[],
"upstreamUrl":"https://mcp.deepwiki.test/mcp","state":{"enabled":false,"status":"disabled","configName":"hub__deepwiki"}}`

const statusJSON = `{"version":"1.2.3","addr":"127.0.0.1:7420","claudeConfigPath":"/tmp/x/.claude.json","enabledCount":1,
"catalogueSource":"embedded","user":"dev-user"}`

// fakeDaemon records requests and serves canned contract responses.
type fakeDaemon struct {
	mu   sync.Mutex
	reqs []string
}

func (f *fakeDaemon) record(r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, r.Method+" "+r.URL.RequestURI())
}

func (f *fakeDaemon) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.reqs...)
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, `{"error":{"code":"`+code+`","message":"`+msg+`"}}`)
}

func (f *fakeDaemon) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.record(r)
	ok := func(body string) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/catalogue":
		if r.URL.Query().Get("q") == "explode" {
			writeErr(w, http.StatusInternalServerError, "internal", "catalogue unavailable")
			return
		}
		if r.URL.Query().Get("q") == "nothing" {
			ok(`{"items":[]}`)
			return
		}
		ok(`{"items":[` + itemJSON + `,` + remoteJSON + `]}`)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/catalogue/service-directory":
		ok(itemJSON)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/catalogue/deepwiki":
		ok(remoteJSON)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/status":
		ok(statusJSON)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/servers/service-directory/enable":
		ok(itemJSON)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/servers/service-directory/disable":
		ok(strings.Replace(itemJSON, `"enabled":true,"status":"running"`, `"enabled":false,"status":"disabled"`, 1))
	case r.URL.Path == "/api/v1/servers/deepwiki/enable":
		writeErr(w, http.StatusForbidden, "not_approved", "deepwiki is pending approval")
	case r.URL.Path == "/api/v1/servers/bad-sum/enable":
		writeErr(w, http.StatusUnprocessableEntity, "checksum_mismatch", "artifact sha256 does not match the catalogue")
	case r.URL.Path == "/api/v1/servers/bad-req/enable":
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid id")
	case r.URL.Path == "/api/v1/servers/plain/enable":
		http.Error(w, "boom", http.StatusInternalServerError)
	case r.URL.Path == "/api/v1/catalogue/garbled":
		ok(`{not json`)
	default:
		writeErr(w, http.StatusNotFound, "not_found", "no such entry")
	}
}

type result struct {
	code   int
	stdout string
	stderr string
}

func runWith(t *testing.T, env Env, args ...string) result {
	t.Helper()
	var out, errOut bytes.Buffer
	env.Stdout, env.Stderr = &out, &errOut
	code := Run(context.Background(), args, env)
	return result{code, out.String(), errOut.String()}
}

func startFake(t *testing.T) (*fakeDaemon, Env) {
	t.Helper()
	f := &fakeDaemon{}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, Env{Addr: strings.TrimPrefix(srv.URL, "http://")}
}

func mustContain(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("output missing %q:\n%s", w, got)
		}
	}
}

func TestHandles(t *testing.T) {
	for _, c := range []string{"search", "info", "enable", "disable", "status", "uninstall-config", "install-skill", "help"} {
		if !Handles(c) {
			t.Errorf("Handles(%q) = false", c)
		}
	}
	for _, c := range []string{"serve", "version", "", "rm"} {
		if Handles(c) {
			t.Errorf("Handles(%q) = true", c)
		}
	}
}

func TestHelp(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"search", "--help"}, {"enable", "-h"}} {
		r := runWith(t, Env{}, args...)
		if r.code != 0 {
			t.Fatalf("%v: code %d", args, r.code)
		}
		mustContain(t, r.stdout, "mcp-hub search", "mcp-hub enable <id>", "mcp-hub uninstall-config", "MCP_HUB_ADDR")
	}
}

func TestSearchTable(t *testing.T) {
	f, env := startFake(t)
	r := runWith(t, env, "search", "service", "owner", "--kind", "local")
	if r.code != 0 {
		t.Fatalf("code %d stderr %s", r.code, r.stderr)
	}
	mustContain(t, r.stdout, "ID", "NAME", "KIND", "APPROVAL", "CLASSIFICATION", "ENABLED", "OWNER",
		"service-directory", "Service Directory", "platform-engineering", "deepwiki", "pending")
	lines := strings.Split(strings.TrimSpace(r.stdout), "\n")
	if len(lines) != 3 {
		t.Fatalf("want header + 2 rows, got %d:\n%s", len(lines), r.stdout)
	}
	if !strings.Contains(lines[1], "yes") || !strings.Contains(lines[2], "no") {
		t.Errorf("ENABLED column wrong:\n%s", r.stdout)
	}
	if got := f.requests(); len(got) != 1 || got[0] != "GET /api/v1/catalogue?kind=local&q=service+owner" {
		t.Errorf("requests = %v", got)
	}
}

func TestSearchNoQueryAndEmpty(t *testing.T) {
	f, env := startFake(t)
	if r := runWith(t, env, "search", "--kind=remote"); r.code != 0 {
		t.Fatalf("code %d", r.code)
	}
	if got := f.requests(); got[0] != "GET /api/v1/catalogue?kind=remote" {
		t.Errorf("request = %v", got)
	}
	r := runWith(t, env, "search", "nothing")
	if r.code != 0 || !strings.Contains(r.stdout, `No catalogue entries match "nothing"`) {
		t.Errorf("empty search: %d %q", r.code, r.stdout)
	}
	if r := runWith(t, env, "search"); r.code != 0 {
		t.Fatalf("code %d", r.code)
	}
	if got := f.requests(); got[2] != "GET /api/v1/catalogue" {
		t.Errorf("request = %v", got)
	}
}

func TestSearchJSONPassthrough(t *testing.T) {
	_, env := startFake(t)
	r := runWith(t, env, "search", "--json", "svc")
	if r.code != 0 {
		t.Fatalf("code %d", r.code)
	}
	var v struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &v); err != nil || len(v.Items) != 2 {
		t.Fatalf("not raw JSON (%v): %s", err, r.stdout)
	}
	if !strings.HasSuffix(r.stdout, "\n") {
		t.Error("JSON output should end with a newline")
	}
}

func TestUsageErrorsExit2(t *testing.T) {
	_, env := startFake(t)
	cases := [][]string{
		{"search", "--kind", "cloud"},
		{"search", "--kind"},
		{"search", "--bogus"},
		{"info"},
		{"info", "a", "b"},
		{"enable"},
		{"disable", "Not_An_Id"},
		{"status", "extra"},
		{"status", "--kind", "local"},
		{"uninstall-config", "--json"},
		{"frobnicate"},
	}
	for _, args := range cases {
		r := runWith(t, env, args...)
		if r.code != 2 {
			t.Errorf("%v: code %d, want 2 (stderr %q)", args, r.code, r.stderr)
		}
		if !strings.Contains(r.stderr, "mcp-hub help") {
			t.Errorf("%v: stderr should point at help: %q", args, r.stderr)
		}
	}
}

func TestInfoLocal(t *testing.T) {
	_, env := startFake(t)
	r := runWith(t, env, "info", "service-directory")
	if r.code != 0 {
		t.Fatalf("code %d %s", r.code, r.stderr)
	}
	mustContain(t, r.stdout,
		"Service Directory (service-directory) v0.1.0",
		"Find who owns a service.",
		"platform-engineering",
		"short-lived", // hub-jwt explained in words
		"find_service", "Look up a service by name.",
		"hub__service-directory",
		"http://127.0.0.1:7420/mcp/service-directory",
		"running",
		"previous start timed out",
		"https://example.test/docs",
		"ownership, services",
	)
}

func TestInfoRemotePending(t *testing.T) {
	_, env := startFake(t)
	r := runWith(t, env, "info", "deepwiki")
	if r.code != 0 {
		t.Fatalf("code %d", r.code)
	}
	mustContain(t, r.stdout, "pending", "not yet approved", "No credentials", "https://mcp.deepwiki.test/mcp",
		"not enabled", "(none listed)")
}

func TestInfoJSONAndNotFound(t *testing.T) {
	_, env := startFake(t)
	r := runWith(t, env, "info", "deepwiki", "--json")
	if r.code != 0 || !json.Valid([]byte(r.stdout)) || !strings.Contains(r.stdout, `"upstreamUrl"`) {
		t.Fatalf("json info: %d %s", r.code, r.stdout)
	}
	r = runWith(t, env, "info", "missing-one")
	if r.code != 1 {
		t.Errorf("not_found code = %d, want 1", r.code)
	}
	mustContain(t, r.stderr, "no such entry", "not_found")
}

func TestEnableSuccess(t *testing.T) {
	f, env := startFake(t)
	r := runWith(t, env, "enable", "service-directory")
	if r.code != 0 {
		t.Fatalf("code %d %s", r.code, r.stderr)
	}
	mustContain(t, r.stdout, "hub__service-directory", "Run /reload-plugins in Claude Code (or restart it) to load the new server.",
		"http://127.0.0.1:7420/mcp/service-directory")
	if got := f.requests(); got[0] != "POST /api/v1/servers/service-directory/enable" {
		t.Errorf("request = %v", got)
	}
}

func TestDisableSuccess(t *testing.T) {
	f, env := startFake(t)
	r := runWith(t, env, "disable", "service-directory")
	if r.code != 0 {
		t.Fatalf("code %d %s", r.code, r.stderr)
	}
	mustContain(t, r.stdout, "Disabled", "hub__service-directory",
		"Restart Claude Code to unload the server (/reload-plugins does not remove disabled servers).")
	if strings.Contains(r.stdout, "Run /reload-plugins") {
		t.Error("disable must not suggest /reload-plugins to unload a server")
	}
	if got := f.requests(); got[0] != "POST /api/v1/servers/service-directory/disable" {
		t.Errorf("request = %v", got)
	}
}

func TestEnableErrors(t *testing.T) {
	_, env := startFake(t)
	cases := []struct {
		id, code, msg string
		exit          int
	}{
		{"deepwiki", "not_approved", "deepwiki is pending approval", 2},
		{"bad-req", "bad_request", "invalid id", 2},
		{"bad-sum", "checksum_mismatch", "artifact sha256 does not match the catalogue", 1},
		{"nope", "not_found", "no such entry", 1},
	}
	for _, c := range cases {
		r := runWith(t, env, "enable", c.id)
		if r.code != c.exit {
			t.Errorf("%s: exit %d, want %d", c.id, r.code, c.exit)
		}
		mustContain(t, r.stderr, c.msg, "("+c.code+")")
		if r.stdout != "" {
			t.Errorf("%s: human error should not write stdout: %q", c.id, r.stdout)
		}
	}
}

func TestEnableErrorJSON(t *testing.T) {
	_, env := startFake(t)
	r := runWith(t, env, "enable", "deepwiki", "--json")
	if r.code != 2 {
		t.Fatalf("exit %d", r.code)
	}
	var e struct {
		Error struct{ Code string } `json:"error"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &e); err != nil || e.Error.Code != "not_approved" {
		t.Fatalf("stdout should carry the raw error JSON: %q (%v)", r.stdout, err)
	}
}

func TestNonContractResponses(t *testing.T) {
	_, env := startFake(t)
	r := runWith(t, env, "enable", "plain")
	if r.code != 1 {
		t.Errorf("exit %d", r.code)
	}
	mustContain(t, r.stderr, "HTTP 500")
	r = runWith(t, env, "info", "garbled")
	if r.code != 1 {
		t.Errorf("garbled exit %d", r.code)
	}
	mustContain(t, r.stderr, "unexpected response")
}

func TestStatus(t *testing.T) {
	f, env := startFake(t)
	r := runWith(t, env, "status")
	if r.code != 0 {
		t.Fatalf("code %d %s", r.code, r.stderr)
	}
	mustContain(t, r.stdout, "1.2.3", "127.0.0.1:7420", "dev-user", "/tmp/x/.claude.json", "embedded",
		"Enabled servers: 1", "hub__service-directory", "Service Directory")
	if strings.Contains(r.stdout, "deepwiki") {
		t.Errorf("status lists a server that is not enabled:\n%s", r.stdout)
	}
	if got := f.requests(); len(got) != 2 || got[0] != "GET /api/v1/status" || got[1] != "GET /api/v1/catalogue" {
		t.Errorf("requests = %v", got)
	}
	r = runWith(t, env, "status", "--json")
	if r.code != 0 || strings.TrimSpace(r.stdout) != strings.TrimSpace(statusJSON) {
		t.Errorf("status --json should be the raw body: %q", r.stdout)
	}
}

func TestDaemonUnreachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	for _, args := range [][]string{{"search"}, {"status"}, {"enable", "service-directory"}, {"info", "x1", "--json"}} {
		r := runWith(t, Env{Addr: addr}, args...)
		if r.code != 3 {
			t.Errorf("%v: exit %d, want 3", args, r.code)
		}
		mustContain(t, r.stderr, "mcp-hub daemon is not running on "+addr+".",
			"Start it with `mcp-hub serve` (or re-run install.sh).")
	}
}

func TestDefaultAndNonLoopbackAddr(t *testing.T) {
	if got := (Env{}).addr(); got != "127.0.0.1:7420" {
		t.Errorf("default addr = %q", got)
	}
	r := runWith(t, Env{Addr: "203.0.113.5:7420"}, "status")
	if r.code != 2 {
		t.Errorf("non-loopback addr exit %d, want 2", r.code)
	}
	mustContain(t, r.stderr, "loopback")
	r = runWith(t, Env{Addr: "localhost"}, "status")
	if r.code != 2 {
		t.Errorf("addr without port exit %d, want 2", r.code)
	}
}

func TestDefaultEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MCP_HUB_ADDR", "127.0.0.1:9999")
	t.Setenv("MCP_HUB_HOME", filepath.Join(home, "hub"))
	t.Setenv("CLAUDE_CONFIG_PATH", filepath.Join(home, "claude.json"))
	t.Setenv("CLAUDE_HOME_DIR", filepath.Join(home, "cl"))
	env := DefaultEnv()
	if env.Addr != "127.0.0.1:9999" || env.Stdout == nil || env.Stderr == nil {
		t.Fatalf("DefaultEnv = %+v", env)
	}
	cfg, path, err := env.Config()
	if err != nil || path != filepath.Join(home, "claude.json") {
		t.Fatalf("Config: %q %v", path, err)
	}
	if _, err := cfg.Owned(); err != nil {
		t.Fatalf("Owned on empty temp home: %v", err)
	}
	if d, err := env.ClaudeDir(); err != nil || d != filepath.Join(home, "cl") {
		t.Fatalf("ClaudeDir = %q, %v", d, err)
	}
}

func TestReadCommandsJSONErrors(t *testing.T) {
	_, env := startFake(t)
	cases := []struct {
		args []string
		code string
	}{
		{[]string{"info", "missing-one", "--json"}, "not_found"},
		{[]string{"search", "explode", "--json"}, "internal"},
	}
	for _, c := range cases {
		r := runWith(t, env, c.args...)
		if r.code != 1 {
			t.Errorf("%v: exit %d, want 1", c.args, r.code)
		}
		var e struct {
			Error struct{ Code string } `json:"error"`
		}
		if err := json.Unmarshal([]byte(r.stdout), &e); err != nil || e.Error.Code != c.code {
			t.Errorf("%v: stdout should carry the raw error JSON: %q (%v)", c.args, r.stdout, err)
		}
		mustContain(t, r.stderr, "("+c.code+")")
	}
}
