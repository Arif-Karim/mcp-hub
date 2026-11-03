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

## Builder and publisher skills ship inside the binary today; in production, package mcp-hub's skills as a Claude Code plugin in an internal plugin marketplace

**Context.** Engineers should be able to ask Claude Code to "build an MCP server for my team's service" or "publish
my MCP server to the hub". That needs two more skills (`mcp-hub-build-server`, `mcp-hub-publish-server`) next to
the `mcp-hub` skill, and they must stay in step with the template, `docs/publishing.md` and the daemon.

**Decision.** All three live in `internal/skillfiles/<name>/SKILL.md`, embedded with `go:embed`, and
`mcp-hub install-skill` / `install.sh` / the UI write every one to `~/.claude/skills/<name>/`. Status is
"installed and up to date" only when all three are present and current. In production we would package them as a
Claude Code plugin in an internal plugin marketplace instead. We don't do that today because the single binary is
the demo's one delivery unit, and an internal marketplace is infrastructure the exercise has no place to host.

**Consequences.**
- One install path and one version: the skills always match the binary, and a test fails if a skill's frontmatter
  name drifts from its directory or `install.sh`'s uninstall list drifts from the embedded set.
- Updating a skill needs an mcp-hub release; a plugin marketplace would let the platform team ship skill fixes
  independently, scope them per team, and let Claude Code manage enable/disable/update.
- The builder and publisher skills pre-approve no tools, so file edits, `git push` of tags and `gh pr create` all go
  through Claude Code's normal permission prompts, and the skills also ask the user first.
- A skill directory dropped in a later release is not cleaned up by `install-skill` (only `install.sh --uninstall`
  removes skills, by name). Fine while the set only grows; a plugin would handle removal for us.
