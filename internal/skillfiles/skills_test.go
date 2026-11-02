package skillfiles_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Arif-Karim/mcp-hub/internal/skillfiles"
)

var wantSkills = []string{"mcp-hub", "mcp-hub-build-server", "mcp-hub-publish-server"}

// frontmatter returns the key: value lines between the leading --- markers.
func frontmatter(t *testing.T, name string, c []byte) map[string]string {
	t.Helper()
	s := string(c)
	if !strings.HasPrefix(s, "---\n") {
		t.Fatalf("%s: SKILL.md must start with --- frontmatter", name)
	}
	body, _, ok := strings.Cut(s[4:], "\n---\n")
	if !ok {
		t.Fatalf("%s: unterminated frontmatter", name)
	}
	fm := map[string]string{}
	for _, line := range strings.Split(body, "\n") {
		k, v, ok := strings.Cut(line, ": ")
		if !ok {
			t.Fatalf("%s: frontmatter line %q is not key: value", name, line)
		}
		fm[k] = v
	}
	return fm
}

func TestEmbeddedSkillsAreValid(t *testing.T) {
	skills := skillfiles.Skills()
	if len(skills) != len(wantSkills) {
		t.Fatalf("Skills() = %d skills, want %v", len(skills), wantSkills)
	}
	for i, s := range skills {
		if s.Name != wantSkills[i] {
			t.Errorf("skill %d = %q, want %q (sorted, main skill first)", i, s.Name, wantSkills[i])
		}
		fm := frontmatter(t, s.Name, s.Content)
		if fm["name"] != s.Name {
			t.Errorf("%s: frontmatter name = %q, want the directory name", s.Name, fm["name"])
		}
		if len(fm["description"]) < 80 {
			t.Errorf("%s: description too short to trigger reliably: %q", s.Name, fm["description"])
		}
		if s.Name != "mcp-hub" {
			if _, ok := fm["allowed-tools"]; ok {
				t.Errorf("%s: must not pre-approve tools; edits and commands go through normal prompts", s.Name)
			}
		}
	}
	if !bytes.Equal(skills[0].Content, skillfiles.Content()) {
		t.Error("Content() must be the main mcp-hub skill")
	}
}

func skill(t *testing.T, name string) string {
	t.Helper()
	for _, s := range skillfiles.Skills() {
		if s.Name == name {
			return string(s.Content)
		}
	}
	t.Fatalf("no embedded skill %q", name)
	return ""
}

func TestSkillsStateTheEssentials(t *testing.T) {
	cases := map[string][]string{
		"mcp-hub": {"mcp-hub-build-server", "mcp-hub-publish-server"},
		"mcp-hub-build-server": {
			"examples/mcp-server-template", "git clone --depth 1 https://github.com/Arif-Karim/mcp-hub",
			"^[a-z0-9][a-z0-9-]{1,62}$", "HUB_AUDIENCE", "scripts/package.sh", "internal/directory",
			"cmd/devtoken", "go test -race ./...", "/healthz", "SIGTERM", "isError", "127.0.0.1",
			"Synthetic data only", "docs/publishing.md",
		},
		"mcp-hub-publish-server": {
			"publish-mcp-server.yml", "<id>-v<semver>", "approval: pending", "auth: hub-jwt", "auth: none",
			"checksums.txt", "gh pr create", "internal/catalogue/default.yaml", "Never invent", "next mcp-hub release",
			"Ask the user", "docs/publishing.md",
		},
	}
	for name, want := range cases {
		c := skill(t, name)
		for _, w := range want {
			if !strings.Contains(c, w) {
				t.Errorf("%s: SKILL.md does not mention %q", name, w)
			}
		}
	}
}

func TestInstallWritesEverySkill(t *testing.T) {
	dir := t.TempDir()
	if err := skillfiles.Install(dir); err != nil {
		t.Fatal(err)
	}
	for _, s := range skillfiles.Skills() {
		p := skillfiles.SkillPath(dir, s.Name)
		if p != filepath.Join(dir, "skills", s.Name, "SKILL.md") {
			t.Errorf("SkillPath(%s) = %q", s.Name, p)
		}
		got, err := os.ReadFile(p)
		if err != nil || !bytes.Equal(got, s.Content) {
			t.Errorf("%s: installed copy differs (err %v)", s.Name, err)
		}
	}
	if got, want := skillfiles.SkillsDir(dir), filepath.Join(dir, "skills"); got != want {
		t.Errorf("SkillsDir = %q, want %q", got, want)
	}
}

func TestStatusRequiresEverySkill(t *testing.T) {
	for _, s := range skillfiles.Skills() {
		t.Run(s.Name+"/missing", func(t *testing.T) {
			dir := t.TempDir()
			if err := skillfiles.Install(dir); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(skillfiles.SkillPath(dir, s.Name)); err != nil {
				t.Fatal(err)
			}
			if i, u := skillfiles.Status(dir); i || u {
				t.Fatalf("Status = %v,%v with %s missing; want false,false", i, u, s.Name)
			}
		})
		t.Run(s.Name+"/stale", func(t *testing.T) {
			dir := t.TempDir()
			if err := skillfiles.Install(dir); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(skillfiles.SkillPath(dir, s.Name), []byte("old"), 0o644); err != nil {
				t.Fatal(err)
			}
			if i, u := skillfiles.Status(dir); !i || u {
				t.Fatalf("Status = %v,%v with %s stale; want true,false", i, u, s.Name)
			}
		})
	}
}
