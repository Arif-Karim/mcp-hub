package skillfiles_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Arif-Karim/mcp-hub/internal/skillfiles"
)

func TestEmbeddedSkillIsValid(t *testing.T) {
	c := string(skillfiles.Content())
	if !strings.HasPrefix(c, "---\nname: mcp-hub\ndescription: ") {
		t.Fatalf("SKILL.md must start with frontmatter naming the skill, got %.60q", c)
	}
	if !strings.Contains(c, "/reload-plugins") {
		t.Error("SKILL.md must tell users to run /reload-plugins")
	}
	if strings.Contains(c, "run `/mcp`") {
		t.Error("SKILL.md still uses the old /mcp wording")
	}
}

// The embedded file is the only copy in the repo; a stray copy under skill/ would drift.
func TestNoDriftingCopyUnderSkillDir(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	stray := filepath.Join(filepath.Dir(file), "..", "..", "skill", "mcp-hub", "SKILL.md")
	if _, err := os.Stat(stray); err == nil {
		t.Fatalf("%s exists; the canonical skill is internal/skillfiles/mcp-hub/SKILL.md", stray)
	}
}

func TestPath(t *testing.T) {
	if got, want := skillfiles.Path("/c"), filepath.Join("/c", "skills", "mcp-hub", "SKILL.md"); got != want {
		t.Fatalf("Path = %q, want %q", got, want)
	}
}

func TestDefaultClaudeDir(t *testing.T) {
	t.Setenv("CLAUDE_HOME_DIR", "/override/.claude")
	if d, err := skillfiles.DefaultClaudeDir(); err != nil || d != "/override/.claude" {
		t.Fatalf("DefaultClaudeDir = %q, %v; want the override", d, err)
	}
	home := t.TempDir()
	t.Setenv("CLAUDE_HOME_DIR", "")
	t.Setenv("HOME", home)
	if d, err := skillfiles.DefaultClaudeDir(); err != nil || d != filepath.Join(home, ".claude") {
		t.Fatalf("DefaultClaudeDir = %q, %v; want %s/.claude", d, err, home)
	}
}

func TestInstallWritesSkillWithModes(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".claude")
	if err := skillfiles.Install(dir); err != nil {
		t.Fatal(err)
	}
	p := skillfiles.Path(dir)
	got, err := os.ReadFile(p)
	if err != nil || !bytes.Equal(got, skillfiles.Content()) {
		t.Fatalf("installed file differs from embedded (err %v)", err)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o644 {
		t.Errorf("file mode = %v, want 0644", fi.Mode().Perm())
	}
	di, err := os.Stat(filepath.Dir(p))
	if err != nil {
		t.Fatal(err)
	}
	if di.Mode().Perm() != 0o755 {
		t.Errorf("dir mode = %v, want 0755", di.Mode().Perm())
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	if len(entries) != 1 {
		t.Errorf("skill dir has %d entries, want only SKILL.md (temp file left behind?)", len(entries))
	}
}

func TestStatusAndUpdate(t *testing.T) {
	dir := t.TempDir()
	if i, u := skillfiles.Status(dir); i || u {
		t.Fatalf("fresh dir: Status = %v,%v; want false,false", i, u)
	}
	if err := skillfiles.Install(dir); err != nil {
		t.Fatal(err)
	}
	if i, u := skillfiles.Status(dir); !i || !u {
		t.Fatalf("after install: Status = %v,%v; want true,true", i, u)
	}
	if err := os.WriteFile(skillfiles.Path(dir), []byte("old skill"), 0o644); err != nil {
		t.Fatal(err)
	}
	if i, u := skillfiles.Status(dir); !i || u {
		t.Fatalf("stale: Status = %v,%v; want true,false", i, u)
	}
	// Re-installing overwrites the stale copy.
	if err := skillfiles.Install(dir); err != nil {
		t.Fatal(err)
	}
	if i, u := skillfiles.Status(dir); !i || !u {
		t.Fatalf("after update: Status = %v,%v; want true,true", i, u)
	}
}

func TestInstallFailsWhenSkillsIsAFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "skills"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := skillfiles.Install(dir); err == nil {
		t.Fatal("Install succeeded although skills/ is a file")
	}
}

func TestManager(t *testing.T) {
	m := skillfiles.Manager{ClaudeDir: t.TempDir()}
	want := skillfiles.State{Path: skillfiles.Path(m.ClaudeDir)}
	if got := m.State(); got != want {
		t.Fatalf("State = %+v, want %+v", got, want)
	}
	got, err := m.Install()
	want.Installed, want.UpToDate = true, true
	if err != nil || got != want {
		t.Fatalf("Install = %+v, %v; want %+v", got, err, want)
	}
	if got := m.State(); got != want {
		t.Fatalf("State after install = %+v, want %+v", got, want)
	}
	bad := skillfiles.Manager{ClaudeDir: filepath.Join(skillfiles.Path(m.ClaudeDir), "x")}
	if _, err := bad.Install(); err == nil {
		t.Fatal("Install under a file path succeeded")
	}
}
