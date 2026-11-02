// Package skillfiles carries mcp-hub's Claude Code skills inside the binary
// and installs them into the user's Claude Code skills directory, so a
// developer gets them from `mcp-hub install-skill` or the web UI without a
// separate download. Each <name>/SKILL.md here is the only copy in the repo:
// mcp-hub (find and enable servers), mcp-hub-build-server and
// mcp-hub-publish-server (build and publish your own server).
package skillfiles

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
)

//go:embed */SKILL.md
var embedded embed.FS

// mainSkill is the skill whose path the SkillStatus contract reports.
const mainSkill = "mcp-hub"

const (
	dirMode  = 0o755
	fileMode = 0o644
)

// Skill is one embedded Claude Code skill.
type Skill struct {
	Name    string // directory name; matches the frontmatter name
	Content []byte
}

// State is the contract's SkillStatus schema.
type State struct {
	Installed bool   `json:"installed"`
	UpToDate  bool   `json:"upToDate"`
	Path      string `json:"path"`
}

var skills = loadSkills(embedded)

func loadSkills(fsys fs.FS) []Skill {
	files, err := fs.Glob(fsys, "*/SKILL.md")
	if err != nil {
		panic(err) // constant, valid pattern
	}
	out := make([]Skill, 0, len(files))
	for _, p := range files {
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			panic(err) // embedded at build time
		}
		out = append(out, Skill{Name: path.Dir(p), Content: b})
	}
	// Main skill first, then the rest by name.
	sort.SliceStable(out, func(i, j int) bool {
		if mi, mj := out[i].Name == mainSkill, out[j].Name == mainSkill; mi != mj {
			return mi
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Skills returns every embedded skill, the main mcp-hub skill first.
func Skills() []Skill {
	out := make([]Skill, len(skills))
	for i, s := range skills {
		out[i] = Skill{Name: s.Name, Content: bytes.Clone(s.Content)}
	}
	return out
}

// Content returns the embedded main mcp-hub SKILL.md.
func Content() []byte { return bytes.Clone(skills[0].Content) }

// DefaultClaudeDir is $CLAUDE_HOME_DIR (mcp-hub override), else Claude Code's
// own $CLAUDE_CONFIG_DIR, else ~/.claude. Relative paths are rejected: under
// launchd the working directory is /, so they would point somewhere unexpected.
func DefaultClaudeDir() (string, error) {
	for _, key := range []string{"CLAUDE_HOME_DIR", "CLAUDE_CONFIG_DIR"} {
		if d := os.Getenv(key); d != "" {
			if !filepath.IsAbs(d) {
				return "", fmt.Errorf("%s must be an absolute path, got %q", key, d)
			}
			return d, nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude"), nil
}

// SkillsDir is Claude Code's user skills directory under claudeDir.
func SkillsDir(claudeDir string) string { return filepath.Join(claudeDir, "skills") }

// SkillPath is where the named skill lives under claudeDir.
func SkillPath(claudeDir, name string) string {
	return filepath.Join(SkillsDir(claudeDir), name, "SKILL.md")
}

// Path is where the main mcp-hub skill lives under claudeDir.
func Path(claudeDir string) string { return SkillPath(claudeDir, mainSkill) }

// Install writes every embedded skill under SkillsDir(claudeDir).
func Install(claudeDir string) error {
	for _, s := range skills {
		if err := installOne(SkillPath(claudeDir, s.Name), s.Content); err != nil {
			return err
		}
	}
	return nil
}

// installOne replaces target atomically (temp file + rename) so Claude Code
// never reads half a file, and a symlink at target is replaced, not followed.
func installOne(target string, content []byte) error {
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".SKILL.md.*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // no-op after a successful rename
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(fileMode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		return fmt.Errorf("installing %s: %w", target, err)
	}
	return nil
}

// Status reports whether every skill is installed, and whether every copy
// matches the embedded one. Any missing skill means not installed.
func Status(claudeDir string) (installed, upToDate bool) {
	upToDate = true
	for _, s := range skills {
		got, err := os.ReadFile(SkillPath(claudeDir, s.Name))
		if err != nil {
			return false, false
		}
		upToDate = upToDate && bytes.Equal(got, s.Content)
	}
	return true, upToDate
}

// Manager binds the functions above to one Claude directory (for the daemon).
type Manager struct{ ClaudeDir string }

// State reports the skills' install state; Path is the main skill's path.
func (m Manager) State() State {
	installed, upToDate := Status(m.ClaudeDir)
	return State{Installed: installed, UpToDate: upToDate, Path: Path(m.ClaudeDir)}
}

// Install installs every skill and returns the new state.
func (m Manager) Install() (State, error) {
	if err := Install(m.ClaudeDir); err != nil {
		return State{}, err
	}
	return m.State(), nil
}
