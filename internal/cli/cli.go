// Package cli implements the mcp-hub client subcommands (search, info, enable,
// disable, status, uninstall-config). They talk to the local daemon's
// /api/v1 control plane; uninstall-config works offline on the Claude config.
package cli

import (
	"context"
	"io"

	"github.com/Arif-Karim/mcp-hub/internal/claudeconfig"
)

// ConfigRemover is the slice of claudeconfig.Writer that uninstall-config needs.
type ConfigRemover interface {
	Owned() (map[string]claudeconfig.Owned, error)
	RemoveAll() error
}

// Env carries everything a command touches, so tests can inject it.
type Env struct {
	Stdout, Stderr io.Writer
	Addr           string                         // daemon host:port; empty means the default
	Config         func() (ConfigRemover, error) // lazily opens the Claude config writer
}

// Handles reports whether cmd is a CLI subcommand implemented here.
func Handles(string) bool { return false }

// DefaultEnv wires the process environment.
func DefaultEnv() Env { return Env{} }

func (Env) addr() string { return "" }

// Run executes one subcommand and returns the process exit code.
func Run(context.Context, []string, Env) int { return 1 }
