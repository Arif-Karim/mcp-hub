package daemon_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Arif-Karim/mcp-hub/internal/catalogue"
	"github.com/Arif-Karim/mcp-hub/internal/daemon"
	"github.com/Arif-Karim/mcp-hub/internal/token"
)

// upstream is a stand-in MCP server that records the Authorization it got.
type upstream struct {
	mu    sync.Mutex
	authz []string
}

func (u *upstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	u.mu.Lock()
	u.authz = append(u.authz, r.Header.Get("Authorization"))
	u.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
}

func (u *upstream) last() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.authz[len(u.authz)-1]
}

func remote(id, url string, auth catalogue.AuthMode, approval catalogue.Approval) catalogue.Entry {
	return catalogue.Entry{
		ID: id, Name: id, Description: "d", Owner: "o", Version: "1", Kind: catalogue.KindRemote,
		Classification: catalogue.ClassPublic, Approval: approval, Auth: auth, URL: url,
	}
}

type fixture struct {
	home, claude string
	up           *upstream
	cfg          daemon.Config
}

func newFixture(t *testing.T, approval catalogue.Approval) *fixture {
	t.Helper()
	dir := t.TempDir()
	up := &upstream{}
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	f := &fixture{home: filepath.Join(dir, "hub"), claude: filepath.Join(dir, "claude.json"), up: up}
	f.cfg = daemon.Config{
		Home: f.home, ClaudeConfigPath: f.claude, Addr: "127.0.0.1:7420", Version: "9.9.9",
		Catalogue: catalogue.NewStore(&catalogue.Catalogue{Version: 1, Entries: []catalogue.Entry{
			remote("open", srv.URL+"/mcp", catalogue.AuthNone, approval),
			remote("inhouse", srv.URL+"/mcp", catalogue.AuthHubJWT, approval),
		}}),
		CatalogueSource: "test.yaml", User: "dev-user", Logger: slog.New(slog.DiscardHandler),
		UI: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ui")) }),
	}
	return f
}

func (f *fixture) start(t *testing.T) *daemon.Daemon {
	t.Helper()
	d, err := daemon.New(f.cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func call(t *testing.T, h http.Handler, method, path, body string) (int, string) {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Host = "127.0.0.1:7420"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	b, _ := io.ReadAll(rec.Body)
	return rec.Code, string(b)
}

func TestDaemonWiring(t *testing.T) {
	f := newFixture(t, catalogue.ApprovalApproved)
	d := f.start(t)
	h := d.Handler()

	for _, sub := range []string{"", "servers", "logs"} {
		fi, err := os.Stat(filepath.Join(f.home, sub))
		if err != nil || fi.Mode().Perm() != 0o700 {
			t.Fatalf("%s: %v mode %v, want 0700", sub, err, fi.Mode().Perm())
		}
	}

	code, body := call(t, h, http.MethodGet, "/api/v1/status", "")
	var st map[string]any
	_ = json.Unmarshal([]byte(body), &st)
	if code != 200 || st["version"] != "9.9.9" || st["addr"] != "127.0.0.1:7420" || st["claudeConfigPath"] != f.claude ||
		st["catalogueSource"] != "test.yaml" || st["user"] != "dev-user" || st["enabledCount"] != float64(0) {
		t.Fatalf("status = %d %s", code, body)
	}

	if code, body := call(t, h, http.MethodGet, "/", ""); code != 200 || body != "ui" {
		t.Fatalf("ui = %d %q", code, body)
	}
	if code, body := call(t, h, http.MethodGet, "/.well-known/jwks.json", ""); code != 200 || !strings.Contains(body, `"OKP"`) {
		t.Fatalf("jwks = %d %s", code, body)
	}

	// Not enabled yet: the proxy refuses.
	if code, _ := call(t, h, http.MethodPost, "/mcp/open", `{"jsonrpc":"2.0","id":1,"method":"ping"}`); code != 404 {
		t.Fatalf("proxy before enable = %d, want 404", code)
	}

	for _, id := range []string{"open", "inhouse"} {
		if code, body := call(t, h, http.MethodPost, "/api/v1/servers/"+id+"/enable", ""); code != 200 || !strings.Contains(body, `"status":"ready"`) {
			t.Fatalf("enable %s = %d %s", id, code, body)
		}
	}
	cfg, err := os.ReadFile(f.claude)
	if err != nil || !strings.Contains(string(cfg), `"url": "http://127.0.0.1:7420/mcp/inhouse"`) {
		t.Fatalf("claude config = %s %v", cfg, err)
	}
	if _, body := call(t, h, http.MethodGet, "/api/v1/catalogue/open", ""); !strings.Contains(body, `"enabled":true`) {
		t.Fatalf("catalogue state not wired: %s", body)
	}
	if _, body := call(t, h, http.MethodGet, "/api/v1/status", ""); !strings.Contains(body, `"enabledCount":2`) {
		t.Fatalf("enabledCount not wired: %s", body)
	}

	// auth none: no credentials upstream.
	if code, _ := call(t, h, http.MethodPost, "/mcp/open", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"t1"}}`); code != 200 {
		t.Fatalf("proxy open = %d", code)
	}
	if got := f.up.last(); got != "" {
		t.Fatalf("auth none sent Authorization %q", got)
	}
	// hub-jwt: a token for this user and audience, signed by the daemon's key.
	if code, _ := call(t, h, http.MethodPost, "/mcp/inhouse", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"t2"}}`); code != 200 {
		t.Fatalf("proxy inhouse = %d", code)
	}
	verifier, err := token.NewIssuer(filepath.Join(f.home, "keys", "signing.pem"), daemon.Issuer, nil)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := verifier.Verify(strings.TrimPrefix(f.up.last(), "Bearer "), "inhouse")
	if err != nil || claims.Subject != "dev-user" || claims.Issuer != "mcp-hub" {
		t.Fatalf("injected token: %+v %v", claims, err)
	}

	_, body = call(t, h, http.MethodGet, "/api/v1/audit", "")
	if !strings.Contains(body, `"tool":"t2"`) || !strings.Contains(body, `"user":"dev-user"`) {
		t.Fatalf("audit = %s", body)
	}
	if _, err := os.Stat(filepath.Join(f.home, "audit.jsonl")); err != nil {
		t.Fatalf("audit file: %v", err)
	}
}

func TestDaemonRestoreAutoDisablesRevoked(t *testing.T) {
	f := newFixture(t, catalogue.ApprovalApproved)
	d := f.start(t)
	if code, body := call(t, d.Handler(), http.MethodPost, "/api/v1/servers/open/enable", ""); code != 200 {
		t.Fatalf("enable = %d %s", code, body)
	}
	_ = d.Close()

	revoked := newFixture(t, catalogue.ApprovalRevoked)
	f.cfg.Catalogue = revoked.cfg.Catalogue
	d2 := f.start(t)
	d2.Restore(context.Background())
	cfg, _ := os.ReadFile(f.claude)
	if strings.Contains(string(cfg), "hub__open") {
		t.Fatalf("revoked server still in claude config: %s", cfg)
	}
	_, body := call(t, d2.Handler(), http.MethodGet, "/api/v1/catalogue/open", "")
	if !strings.Contains(body, `"lastError":"disabled: revoked by platform team"`) {
		t.Fatalf("catalogue state = %s", body)
	}
}

func TestDaemonRestoreLogsFailures(t *testing.T) {
	f := newFixture(t, catalogue.ApprovalApproved)
	if err := os.MkdirAll(f.home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.home, "state.json"), []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	var logs strings.Builder
	f.cfg.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	f.start(t).Restore(context.Background())
	if !strings.Contains(logs.String(), "restore incomplete") {
		t.Fatalf("logs = %s", logs.String())
	}
}

func TestDaemonNewErrors(t *testing.T) {
	f := newFixture(t, catalogue.ApprovalApproved)
	for name, mutate := range map[string]func(*daemon.Config){
		"no home":      func(c *daemon.Config) { c.Home = "" },
		"no config":    func(c *daemon.Config) { c.ClaudeConfigPath = "" },
		"no catalogue": func(c *daemon.Config) { c.Catalogue = nil },
		"home is a file": func(c *daemon.Config) {
			p := filepath.Join(t.TempDir(), "file")
			_ = os.WriteFile(p, nil, 0o600)
			c.Home = p
		},
	} {
		cfg := f.cfg
		mutate(&cfg)
		if d, err := daemon.New(cfg); err == nil {
			_ = d.Close()
			t.Errorf("%s: New succeeded", name)
		}
	}
	// A key file readable by others is refused.
	cfg := f.cfg
	cfg.Home = filepath.Join(t.TempDir(), "hub")
	if err := os.MkdirAll(filepath.Join(cfg.Home, "keys"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.Home, "keys", "signing.pem"), []byte("x"), 0o644); err != nil { //nolint:gosec // deliberately insecure fixture
		t.Fatal(err)
	}
	if _, err := daemon.New(cfg); err == nil {
		t.Fatal("insecure key accepted")
	}
	// Default logger.
	cfg = f.cfg
	cfg.Home, cfg.Logger = filepath.Join(t.TempDir(), "hub"), nil
	d, err := daemon.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	_ = d.Close()
}
