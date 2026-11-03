// Package cli implements the mcp-hub client subcommands (search, info, enable,
// disable, status, uninstall-config, install-skill). They talk to the local
// daemon's /api/v1 control plane; uninstall-config and install-skill work offline.
//
// Commands never call os.Exit: Run returns the exit code so tests can assert it.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Arif-Karim/mcp-hub/internal/claudeconfig"
	"github.com/Arif-Karim/mcp-hub/internal/skillfiles"
)

// Exit codes.
const (
	ExitOK          = 0
	ExitError       = 1 // daemon or config error
	ExitUsage       = 2 // bad usage, validation, or not_approved
	ExitUnreachable = 3 // daemon not running
)

// DefaultAddr is where the daemon listens unless MCP_HUB_ADDR says otherwise.
const DefaultAddr = "127.0.0.1:7420"

// ConfigRemover is the slice of claudeconfig.Writer that uninstall-config needs.
type ConfigRemover interface {
	Owned() (map[string]claudeconfig.Owned, error)
	RemoveAll() error
}

// Env carries everything a command touches, so tests can inject it.
type Env struct {
	Stdout, Stderr io.Writer
	Addr           string // daemon host:port; empty means DefaultAddr
	// Config opens the Claude config writer and returns the config path (for messages).
	Config func() (ConfigRemover, string, error)
	// ClaudeDir returns the Claude Code directory (~/.claude) install-skill writes into.
	ClaudeDir func() (string, error)
}

// DefaultEnv wires the process environment.
func DefaultEnv() Env {
	return Env{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Addr:   os.Getenv("MCP_HUB_ADDR"),
		Config: func() (ConfigRemover, string, error) {
			p, err := claudeconfig.DefaultPaths()
			if err != nil {
				return nil, "", err
			}
			return &claudeconfig.Writer{Paths: p}, p.ClaudeConfig, nil
		},
		ClaudeDir: skillfiles.DefaultClaudeDir,
	}
}

func (e Env) addr() string {
	if e.Addr == "" {
		return DefaultAddr
	}
	return e.Addr
}

type command struct {
	run      func(ctx context.Context, env Env, o opts) error
	kindFlag bool // accepts --kind
	jsonFlag bool // accepts --json
	minArgs  int
	maxArgs  int // -1: any number (joined with spaces)
}

var commands = map[string]command{
	"search":           {run: search, kindFlag: true, jsonFlag: true, maxArgs: -1},
	"info":             {run: info, jsonFlag: true, minArgs: 1, maxArgs: 1},
	"enable":           {run: enable, jsonFlag: true, minArgs: 1, maxArgs: 1},
	"disable":          {run: disable, jsonFlag: true, minArgs: 1, maxArgs: 1},
	"status":           {run: status, jsonFlag: true},
	"uninstall-config": {run: uninstallConfig},
	"install-skill":    {run: installSkill},
}

// Handles reports whether cmd is a CLI subcommand implemented here.
func Handles(cmd string) bool {
	_, ok := commands[cmd]
	return ok || cmd == "help"
}

// Run executes one subcommand and returns the process exit code.
func Run(ctx context.Context, args []string, env Env) int {
	if env.Stdout == nil {
		env.Stdout = io.Discard
	}
	if env.Stderr == nil {
		env.Stderr = io.Discard
	}
	if len(args) == 0 || args[0] == "help" {
		fmt.Fprint(env.Stdout, helpText)
		return ExitOK
	}
	cmd, ok := commands[args[0]]
	if !ok {
		return report(env, usageErr("unknown command %q", args[0]))
	}
	o, err := parse(args[1:], cmd)
	if o.help {
		fmt.Fprint(env.Stdout, helpText)
		return ExitOK
	}
	if err != nil {
		return report(env, err)
	}
	return report(env, cmd.run(ctx, env, o))
}

// report prints err (if any) and maps it to an exit code.
func report(env Env, err error) int {
	if err == nil {
		return ExitOK
	}
	var (
		usage *usageError
		api   *apiError
		down  *unreachableError
	)
	switch {
	case errors.As(err, &usage):
		fmt.Fprintf(env.Stderr, "mcp-hub: %s\nRun `mcp-hub help` for usage.\n", usage.msg)
		return ExitUsage
	case errors.As(err, &down):
		fmt.Fprintf(env.Stderr, "mcp-hub daemon is not running on %s. Start it with `mcp-hub serve` (or re-run install.sh).\n", down.addr)
		return ExitUnreachable
	case errors.As(err, &api):
		fmt.Fprintf(env.Stderr, "mcp-hub: %s (%s)\n", api.Message, api.Code)
		if api.Code == "not_approved" || api.Code == "bad_request" {
			return ExitUsage
		}
		return ExitError
	default:
		fmt.Fprintf(env.Stderr, "mcp-hub: %v\n", err)
		return ExitError
	}
}

type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

func usageErr(format string, a ...any) error {
	return &usageError{msg: fmt.Sprintf(format, a...)}
}

type opts struct {
	json bool
	kind string
	help bool
	args []string
}

// parse accepts flags anywhere among the positional arguments. It is a single
// bounded pass over args (no index arithmetic) so it always terminates.
func parse(args []string, cmd command) (opts, error) {
	var o opts
	wantKind := false
	for _, a := range args {
		if wantKind {
			o.kind, wantKind = a, false
			continue
		}
		switch {
		case a == "-h" || a == "--help":
			o.help = true
		case cmd.jsonFlag && (a == "--json" || a == "-json"):
			o.json = true
		case cmd.kindFlag && (a == "--kind" || a == "-kind"):
			wantKind = true
		case cmd.kindFlag && strings.HasPrefix(a, "--kind="):
			o.kind = strings.TrimPrefix(a, "--kind=")
		case strings.HasPrefix(a, "-"):
			return o, usageErr("unknown flag %q", a)
		default:
			o.args = append(o.args, a)
		}
	}
	switch {
	case wantKind:
		return o, usageErr("--kind needs a value: local or remote")
	case o.kind != "" && o.kind != "local" && o.kind != "remote":
		return o, usageErr("--kind must be local or remote, got %q", o.kind)
	case len(o.args) < cmd.minArgs:
		return o, usageErr("missing <id>")
	case cmd.maxArgs >= 0 && len(o.args) > cmd.maxArgs:
		return o, usageErr("too many arguments: %s", strings.Join(o.args, " "))
	}
	return o, nil
}

const helpText = `mcp-hub — governed MCP marketplace for Claude Code

Usage:
  mcp-hub serve                      run the daemon (API, proxy, web UI) on MCP_HUB_ADDR
  mcp-hub search [query] [--kind local|remote] [--json]
                                     list catalogue entries matching query
  mcp-hub info <id> [--json]         show an entry: owner, auth, tools, install state
  mcp-hub enable <id> [--json]       install (local) and add hub__<id> to Claude Code
  mcp-hub disable <id> [--json]      stop and remove hub__<id> from Claude Code
  mcp-hub status [--json]            daemon status and enabled servers
  mcp-hub uninstall-config           remove every mcp-hub entry from Claude Code config (offline)
  mcp-hub install-skill              install/update the Claude Code skills in ~/.claude/skills (offline)
  mcp-hub version                    print the version
  mcp-hub help                       this help

--json prints the daemon's raw API response (for scripts and the Claude Code skill).

Environment:
  MCP_HUB_ADDR        daemon address (default 127.0.0.1:7420, loopback only)
  MCP_HUB_HOME        state, logs and audit directory (default ~/.mcp-hub)
  CLAUDE_CONFIG_PATH  Claude Code user config (default ~/.claude.json)
  CLAUDE_HOME_DIR     Claude Code directory the skill is installed under
                      (default $CLAUDE_CONFIG_DIR, else ~/.claude)

Exit codes: 0 ok, 1 error, 2 usage/validation/not approved, 3 daemon not running.
`
