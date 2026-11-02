package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Arif-Karim/mcp-hub/internal/claudeconfig"
)

func tempWriter(t *testing.T) *claudeconfig.Writer {
	t.Helper()
	dir := t.TempDir()
	return &claudeconfig.Writer{Paths: claudeconfig.Paths{
		ClaudeConfig: filepath.Join(dir, ".claude.json"),
		Ledger:       filepath.Join(dir, "hub", "state.json"),
		BackupDir:    filepath.Join(dir, "hub", "backups"),
	}}
}

func configEnv(w ConfigRemover, err error) Env {
	return Env{Config: func() (ConfigRemover, error) { return w, err }}
}

func TestUninstallConfigRemovesHubEntriesOnly(t *testing.T) {
	w := tempWriter(t)
	if err := os.WriteFile(w.ClaudeConfig, []byte(`{"mcpServers":{"mine":{"type":"http","url":"http://x"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"alpha", "beta"} {
		if err := w.Add(id, claudeconfig.ServerEntry{Type: "http", URL: "http://127.0.0.1:7420/mcp/" + id}); err != nil {
			t.Fatal(err)
		}
	}
	r := runWith(t, configEnv(w, nil), "uninstall-config")
	if r.code != 0 {
		t.Fatalf("code %d %s", r.code, r.stderr)
	}
	mustContain(t, r.stdout, "Removed hub__alpha", "hub__beta", w.ClaudeConfig)
	raw, err := os.ReadFile(w.ClaudeConfig)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "hub__") || !strings.Contains(string(raw), `"mine"`) {
		t.Errorf("config after uninstall: %s", raw)
	}
}

func TestUninstallConfigNothingToDo(t *testing.T) {
	r := runWith(t, configEnv(tempWriter(t), nil), "uninstall-config")
	if r.code != 0 || !strings.Contains(r.stdout, "No mcp-hub entries") {
		t.Fatalf("%d %q", r.code, r.stdout)
	}
}

func TestUninstallConfigKeepsUserModified(t *testing.T) {
	w := tempWriter(t)
	for _, id := range []string{"alpha", "beta"} {
		if err := w.Add(id, claudeconfig.ServerEntry{Type: "http", URL: "http://127.0.0.1:7420/mcp/" + id}); err != nil {
			t.Fatal(err)
		}
	}
	raw, _ := os.ReadFile(w.ClaudeConfig)
	edited := strings.Replace(string(raw), "/mcp/beta", "/mcp/beta-edited", 1)
	if err := os.WriteFile(w.ClaudeConfig, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	r := runWith(t, configEnv(w, nil), "uninstall-config")
	if r.code != 0 {
		t.Fatalf("partial removal is expected and safe; code %d %s", r.code, r.stderr)
	}
	mustContain(t, r.stdout, "Removed hub__alpha")
	if strings.Contains(r.stdout, "Removed hub__alpha, hub__beta") {
		t.Errorf("beta must not be reported as removed:\n%s", r.stdout)
	}
	mustContain(t, r.stderr, "Kept hub__beta", "modified outside mcp-hub")
}

type failingRemover struct{ ownedErr, removeErr error }

func (f failingRemover) Owned() (map[string]claudeconfig.Owned, error) {
	return map[string]claudeconfig.Owned{}, f.ownedErr
}
func (f failingRemover) RemoveAll() error { return f.removeErr }

func TestUninstallConfigErrors(t *testing.T) {
	cases := []Env{
		configEnv(nil, errors.New("no home dir")),
		configEnv(failingRemover{ownedErr: errors.New("ledger unreadable")}, nil),
		configEnv(failingRemover{removeErr: claudeconfig.ErrConfigInvalid}, nil),
	}
	for i, env := range cases {
		r := runWith(t, env, "uninstall-config")
		if r.code != 1 {
			t.Errorf("case %d: exit %d, want 1", i, r.code)
		}
		if !strings.Contains(r.stderr, "mcp-hub:") {
			t.Errorf("case %d: stderr %q", i, r.stderr)
		}
	}
}
