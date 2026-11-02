// Package skillfiles carries the mcp-hub Claude Code skill inside the binary
// and installs it into the user's Claude Code skills directory, so a
// developer gets the skill from `mcp-hub install-skill` or the web UI without
// a separate download. mcp-hub/SKILL.md here is the only copy in the repo.
package skillfiles

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed mcp-hub/SKILL.md
var skillMD []byte

const (
	dirMode  = 0o755
	fileMode = 0o644
)

// State is the contract's SkillStatus schema.
type State struct {
	Installed bool   `json:"installed"`
	UpToDate  bool   `json:"upToDate"`
	Path      string `json:"path"`
}

// Content returns the embedded SKILL.md.
func Content() []byte { return bytes.Clone(skillMD) }

// DefaultClaudeDir is $CLAUDE_HOME_DIR, or ~/.claude.
func DefaultClaudeDir() (string, error) {
	if d := os.Getenv("CLAUDE_HOME_DIR"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude"), nil
}

// Path is where the skill lives under claudeDir.
func Path(claudeDir string) string {
	return filepath.Join(claudeDir, "skills", "mcp-hub", "SKILL.md")
}

// Install writes the embedded skill to Path(claudeDir), replacing any older
// copy atomically (temp file + rename) so Claude Code never reads half a file.
func Install(claudeDir string) error {
	target := Path(claudeDir)
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".SKILL.md.*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // no-op after a successful rename
	if _, err := tmp.Write(skillMD); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(fileMode); err != nil {
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

// Status reports whether the skill is installed and matches the embedded copy.
func Status(claudeDir string) (installed, upToDate bool) {
	got, err := os.ReadFile(Path(claudeDir))
	if err != nil {
		return false, false
	}
	return true, bytes.Equal(got, skillMD)
}

// Manager binds the functions above to one Claude directory (for the daemon).
type Manager struct{ ClaudeDir string }

// State reports the skill's install state.
func (m Manager) State() State {
	installed, upToDate := Status(m.ClaudeDir)
	return State{Installed: installed, UpToDate: upToDate, Path: Path(m.ClaudeDir)}
}

// Install installs the skill and returns the new state.
func (m Manager) Install() (State, error) {
	if err := Install(m.ClaudeDir); err != nil {
		return State{}, err
	}
	return m.State(), nil
}
