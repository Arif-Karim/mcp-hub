// Package skillfiles carries the mcp-hub Claude Code skill inside the binary
// and installs it into the user's Claude Code skills directory, so a
// developer gets the skill from `mcp-hub install-skill` or the web UI without
// a separate download.
package skillfiles

import (
	_ "embed"
	"errors"
)

//go:embed mcp-hub/SKILL.md
var skillMD []byte

// State is the contract's SkillStatus schema.
type State struct {
	Installed bool   `json:"installed"`
	UpToDate  bool   `json:"upToDate"`
	Path      string `json:"path"`
}

// Content returns the embedded SKILL.md.
func Content() []byte { return nil }

// DefaultClaudeDir is $CLAUDE_HOME_DIR, or ~/.claude.
func DefaultClaudeDir() (string, error) { return "", errors.New("todo") }

// Path is where the skill lives under claudeDir.
func Path(string) string { return "" }

// Install writes the embedded skill under claudeDir.
func Install(string) error { return errors.New("todo") }

// Status reports whether the skill is installed and matches the embedded copy.
func Status(string) (installed, upToDate bool) { return false, false }

// Manager binds the functions above to one Claude directory (for the daemon).
type Manager struct{ ClaudeDir string }

// State reports the skill's install state.
func (m Manager) State() State { return State{} }

// Install installs the skill and returns the new state.
func (m Manager) Install() (State, error) { return State{}, errors.New("todo") }
