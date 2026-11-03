package skillfiles_test

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
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
	// Teams keep their server in their own repo; only the catalogue PR goes to the hub repo.
	cases["mcp-hub-build-server"] = append(cases["mcp-hub-build-server"], "your team's own repo",
		// package.sh's release URLs fall back to the hub repo; the local snippet is a check only.
		"GITHUB_REPOSITORY", "for checking only")
	cases["mcp-hub-publish-server"] = append(cases["mcp-hub-publish-server"], "your team's own repo",
		// No write access to the hub: fork; a manual release run is as outward as pushing a tag.
		"gh repo fork", "gh workflow run publish-mcp-server.yml -f version=")
	// /reload-plugins loads new servers but does not remove disabled ones (same wording as #25).
	cases["mcp-hub"] = append(cases["mcp-hub"], "Restart Claude Code to remove it\n(`/reload-plugins` adds servers but does not remove them")
	for name, want := range cases {
		c := skill(t, name)
		for _, w := range want {
			if !strings.Contains(c, w) {
				t.Errorf("%s: SKILL.md does not mention %q", name, w)
			}
		}
	}
	// Show artifacts in the release snippet's block style so users paste rather than retype.
	if c := skill(t, "mcp-hub-publish-server"); strings.Contains(c, "{ url:") {
		t.Error("publish skill shows artifacts in flow style")
	}
}

// The main skill's trigger must not swallow "add my MCP server to the hub".
func TestMainSkillDescriptionPointsAtBuildAndPublish(t *testing.T) {
	d := frontmatter(t, "mcp-hub", skillfiles.Content())["description"]
	for _, w := range []string{"existing, approved", "mcp-hub-build-server", "mcp-hub-publish-server"} {
		if !strings.Contains(d, w) {
			t.Errorf("mcp-hub description does not mention %q", w)
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

// install.sh --uninstall removes skills by name after the binary is gone, so its list must match.
func TestInstallShUninstallsEverySkill(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	b, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^SKILLS=\(([^)]*)\)`).FindSubmatch(b)
	if m == nil {
		t.Fatal("install.sh must declare SKILLS=(...)")
	}
	if got := strings.Fields(string(m[1])); !slices.Equal(got, wantSkills) {
		t.Fatalf("install.sh SKILLS = %v, want %v", got, wantSkills)
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
