package cli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Arif-Karim/mcp-hub/internal/skillfiles"
)

func skillEnv(dir string, err error) Env {
	return Env{ClaudeDir: func() (string, error) { return dir, err }}
}

func TestInstallSkillWritesSkillOffline(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".claude")
	// No daemon: Addr points nowhere, install-skill must not need it.
	env := skillEnv(dir, nil)
	env.Addr = "127.0.0.1:1"
	r := runWith(t, env, "install-skill")
	if r.code != ExitOK {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	mustContain(t, r.stdout, "Installed 3 mcp-hub Claude Code skills to "+skillfiles.SkillsDir(dir)+":",
		"Restart Claude Code or run /reload-plugins to load them.")
	for _, s := range skillfiles.Skills() {
		mustContain(t, r.stdout, "  "+s.Name+"  "+skillfiles.SkillPath(dir, s.Name)+"\n")
	}
	if installed, upToDate := skillfiles.Status(dir); !installed || !upToDate {
		t.Fatalf("Status = %v,%v after install-skill", installed, upToDate)
	}
}

func TestInstallSkillErrors(t *testing.T) {
	r := runWith(t, skillEnv("", errors.New("no home")), "install-skill")
	if r.code != ExitError {
		t.Fatalf("exit %d, want %d", r.code, ExitError)
	}
	mustContain(t, r.stderr, "locating Claude Code directory: no home")

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "skills"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	r = runWith(t, skillEnv(dir, nil), "install-skill")
	if r.code != ExitError || r.stdout != "" {
		t.Fatalf("exit %d stdout %q, want %d and nothing printed", r.code, r.stdout, ExitError)
	}
	mustContain(t, r.stderr, "installing skill:")

	r = runWith(t, skillEnv(dir, nil), "install-skill", "extra")
	if r.code != ExitUsage {
		t.Fatalf("extra arg: exit %d, want %d", r.code, ExitUsage)
	}
}
