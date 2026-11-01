# decisions

## Write user-scope `~/.claude.json` directly (marker + ledger) vs `claude mcp add`

**Context.** Enabling a catalogue entry must register an HTTP MCP server with Claude Code at user scope, and
disabling/uninstalling must remove exactly what we added. Claude Code ships `claude mcp add/remove`, which edits the
same file. `~/.claude.json` also holds a lot of other state (projects, history, flags) that Claude Code rewrites often.

**Decision.** `internal/claudeconfig` edits `mcpServers` in `~/.claude.json` directly. Every entry we write is named
`hub__<id>` (marker) and recorded with a sha256 of its canonical JSON in `~/.mcp-hub/state.json` (ledger). We only
modify or remove an entry when both the marker and an unchanged ledger hash say it is ours.

**Consequences.**
- Testable without the CLI: tests run against temp files, no dependency on a `claude` binary or its version/output format.
- Ownership is provable: `claude mcp add` leaves no marker, so we could not tell our entry from one the user made with
  the same name, nor detect that the user edited it. The ledger gives us that, making uninstall safe and reversible.
- We own the safety of the write: atomic temp+fsync+rename, backups (newest 5), symlink-following, mode preservation,
  and a lost-update re-check. These are implemented and tested.
- Coupled to the file format: if Claude Code moves user-scope servers elsewhere we must follow. The surface we touch is
  tiny (one top-level key) and everything else is passed through as raw JSON.
- No cross-process lock with Claude Code is possible; see design.md "Claude config safety".
