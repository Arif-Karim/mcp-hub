// Package e2e drives the whole product in-process: a real daemon, the real
// example MCP server built from source and installed through a fake registry,
// the official MCP Go SDK client talking through the hub's proxy, and the
// real Claude config writer — all under t.TempDir().
package e2e

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Arif-Karim/mcp-hub/internal/catalogue"
	"github.com/Arif-Karim/mcp-hub/internal/daemon"
	"github.com/Arif-Karim/mcp-hub/internal/registry"
)

const (
	localID   = "service-directory"
	revokedID = "payments-sandbox"
)

func TestEnableCallDisable(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: builds the example server and starts processes")
	}
	example, err := filepath.Abs(filepath.Join("..", "examples", "mcp-server-template"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(example, "go.mod")); err != nil {
		t.Skipf("e2e: %s not present on this branch (lane E not merged yet)", example)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	tmp := t.TempDir()
	home, hubHome, claudePath := filepath.Join(tmp, "home"), filepath.Join(tmp, "home", ".mcp-hub"), filepath.Join(tmp, "home", ".claude.json")
	// 1. Build and package the example server like the release pipeline does
	// (before HOME is redirected, so the real Go module cache is used).
	archive, sum := buildArtifact(ctx, t, example, filepath.Join(tmp, "build"))
	t.Setenv("HOME", home) // the runner passes HOME to the server; never the real one
	t.Setenv("MCP_HUB_HOME", hubHome)
	t.Setenv("CLAUDE_CONFIG_PATH", claudePath)

	reg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/service-directory.tar.gz" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(archive)
	}))
	defer reg.Close()

	// 2. Start the daemon on a random loopback port.
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	base := "http://" + addr
	d, err := daemon.New(daemon.Config{
		Home: hubHome, ClaudeConfigPath: claudePath, Addr: addr, Version: "e2e",
		Catalogue: catalogue.NewStore(&catalogue.Catalogue{Version: 1, Entries: []catalogue.Entry{
			{
				ID: localID, Name: "Service Directory", Description: "d", Owner: "platform", Version: "0.1.0",
				Kind: catalogue.KindLocal, Classification: catalogue.ClassInternal, Approval: catalogue.ApprovalApproved,
				Auth:      catalogue.AuthHubJWT,
				Artifacts: map[string]catalogue.Artifact{registry.PlatformKey(): {URL: reg.URL + "/service-directory.tar.gz", SHA256: sum}},
			},
			{
				ID: revokedID, Name: "Payments Sandbox", Description: "d", Owner: "payments", Version: "0.3.2",
				Kind: catalogue.KindLocal, Classification: catalogue.ClassConfidential, Approval: catalogue.ApprovalRevoked,
				Auth: catalogue.AuthHubJWT,
			},
		}}),
		CatalogueSource: "e2e", User: "e2e-dev", Logger: slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: d.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		_ = srv.Shutdown(context.Background())
		_ = d.Close()
	}()

	// 3. Enable: downloaded, verified, unpacked, started, registered.
	code, item := post(ctx, t, base+"/api/v1/servers/"+localID+"/enable")
	if code != http.StatusOK {
		t.Fatalf("enable = %d %v", code, item)
	}
	state, _ := item["state"].(map[string]any)
	if state["status"] != "running" || state["enabled"] != true {
		t.Fatalf("enable state = %v", state)
	}
	proxyURL := base + "/mcp/" + localID
	if got := claudeServers(t, claudePath)["hub__"+localID]; got["type"] != "http" || got["url"] != proxyURL {
		t.Fatalf("~/.claude.json hub__%s = %v, want http → %s", localID, got, proxyURL)
	}

	// 4. Call a tool through the proxy with the official SDK client. The server
	// only answers if the hub injected a token it can verify via the JWKS.
	client := mcp.NewClient(&mcp.Implementation{Name: "e2e", Version: "1.0.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: proxyURL, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatalf("MCP connect via proxy: %v", err)
	}
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "find_service", Arguments: map[string]any{"tag": "payments"}})
	if err != nil {
		t.Fatalf("find_service: %v", err)
	}
	text := toolText(res)
	if res.IsError || !strings.Contains(text, `"services"`) || !strings.Contains(text, "payments") {
		t.Fatalf("find_service result (isError=%v): %s", res.IsError, text)
	}
	_ = session.Close()

	// 5. The call is audited with the tool name and user, never the arguments.
	code, audit := get(ctx, t, base+"/api/v1/audit")
	events, _ := audit["events"].([]any)
	found := false
	for _, e := range events {
		ev := e.(map[string]any)
		if ev["method"] == "tools/call" && ev["tool"] == "find_service" && ev["server"] == localID &&
			ev["user"] == "e2e-dev" && ev["status"] == float64(200) {
			found = true
		}
	}
	if code != http.StatusOK || !found {
		t.Fatalf("audit has no tools/call find_service event: %v", audit)
	}

	// 6. Governance: a revoked entry cannot be enabled.
	code, body := post(ctx, t, base+"/api/v1/servers/"+revokedID+"/enable")
	if errBody, _ := body["error"].(map[string]any); code != http.StatusForbidden || errBody["code"] != "not_approved" {
		t.Fatalf("enable revoked = %d %v", code, body)
	}
	if _, ok := claudeServers(t, claudePath)["hub__"+revokedID]; ok {
		t.Fatal("revoked entry written to claude config")
	}

	// 7. Disable: config entry removed, process stopped, proxy no longer routes.
	up, ok := d.Hub.Resolve(localID)
	if !ok {
		t.Fatal("enabled server does not resolve")
	}
	upstreamHealth := strings.TrimSuffix(up.URL, "/mcp") + "/healthz"
	code, item = post(ctx, t, base+"/api/v1/servers/"+localID+"/disable")
	if state, _ := item["state"].(map[string]any); code != http.StatusOK || state["status"] != "disabled" {
		t.Fatalf("disable = %d %v", code, item)
	}
	if _, ok := claudeServers(t, claudePath)["hub__"+localID]; ok {
		t.Fatal("config entry still present after disable")
	}
	if err := httpGet(ctx, upstreamHealth); err == nil {
		t.Fatal("server process still answering after disable")
	}
	if code, _ := post(ctx, t, proxyURL); code != http.StatusNotFound {
		t.Fatalf("proxy after disable = %d, want 404", code)
	}
}

func buildArtifact(ctx context.Context, t *testing.T, example, dir string) ([]byte, string) {
	t.Helper()
	exe := filepath.Join(dir, "mcp-server")
	cmd := exec.CommandContext(ctx, "go", "build", "-o", exe, "./cmd/mcp-server")
	cmd.Dir = example
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build example server: %v\n%s", err, out)
	}
	bin, err := os.ReadFile(exe) //nolint:gosec // test build output
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "mcp-server", Mode: 0o755, Size: int64(len(bin)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(bin); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(buf.Bytes())
	return buf.Bytes(), hex.EncodeToString(sum[:])
}

func post(ctx context.Context, t *testing.T, url string) (int, map[string]any) {
	t.Helper()
	return do(ctx, t, http.MethodPost, url)
}

func get(ctx context.Context, t *testing.T, url string) (int, map[string]any) {
	t.Helper()
	return do(ctx, t, http.MethodGet, url)
}

func do(ctx context.Context, t *testing.T, method, url string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body map[string]any
	raw, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(raw, &body)
	return resp.StatusCode, body
}

func httpGet(ctx context.Context, url string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New(resp.Status)
	}
	return nil
}

func claudeServers(t *testing.T, path string) map[string]map[string]any {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // test temp path
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		MCPServers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.MCPServers
}

func toolText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}
