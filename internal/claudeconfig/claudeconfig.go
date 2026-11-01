// Package claudeconfig makes atomic, ownership-scoped edits to Claude Code's
// user-scope config (~/.claude.json). It only ever touches mcpServers entries
// named hub__<id> that its ledger (~/.mcp-hub/state.json) proves it created and
// that the user has not edited since; everything else in the file is preserved.
package claudeconfig

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// Sentinel errors; callers map them to API error codes with errors.Is.
var (
	// ErrConfigInvalid: the config is not a JSON object, or mcpServers is not an object (config_invalid).
	ErrConfigInvalid = errors.New("claude config is invalid")
	// ErrConfigConflict: the entry is not hub-owned, was edited by the user, or the
	// file kept changing while we wrote it (config_conflict).
	ErrConfigConflict = errors.New("claude config conflict")
	// ErrInvalidID: the server id does not match idPattern (bad_request).
	ErrInvalidID = errors.New("invalid server id")
)

// NamePrefix marks every mcpServers key the hub writes.
const NamePrefix = "hub__"

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}$`)

// ConfigName is the mcpServers key used for a catalogue id.
func ConfigName(id string) string { return NamePrefix + id }

// Paths locates the files the writer reads and writes.
type Paths struct {
	ClaudeConfig string // ~/.claude.json (may be a symlink)
	Ledger       string // ~/.mcp-hub/state.json
	BackupDir    string // ~/.mcp-hub/backups
}

// DefaultPaths honours CLAUDE_CONFIG_PATH and MCP_HUB_HOME, falling back to the home directory.
func DefaultPaths() (Paths, error) {
	cfg, hub := os.Getenv("CLAUDE_CONFIG_PATH"), os.Getenv("MCP_HUB_HOME")
	if cfg == "" || hub == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, err
		}
		if cfg == "" {
			cfg = filepath.Join(home, ".claude.json")
		}
		if hub == "" {
			hub = filepath.Join(home, ".mcp-hub")
		}
	}
	return Paths{
		ClaudeConfig: cfg,
		Ledger:       filepath.Join(hub, "state.json"),
		BackupDir:    filepath.Join(hub, "backups"),
	}, nil
}

// ServerEntry is the mcpServers value for an HTTP MCP server.
type ServerEntry struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// Owned is a ledger record: proof the hub created an entry, and its hash at write time.
type Owned struct {
	ID         string    `json:"id"`
	ConfigName string    `json:"configName"`
	URL        string    `json:"url"`
	Hash       string    `json:"hash"` // sha256 of the canonical entry JSON
	EnabledAt  time.Time `json:"enabledAt"`
}

// Writer edits the Claude config. The zero Now uses time.Now.
type Writer struct {
	Paths
	Now func() time.Time

	beforeRename func() // test hook: runs after the temp file is written, before the lost-update check
}

func (w *Writer) now() time.Time {
	if w.Now == nil {
		return time.Now()
	}
	return w.Now()
}

func validateID(id string) error {
	if !idPattern.MatchString(id) {
		return ErrInvalidID
	}
	return nil
}
