# Decisions

Short ADRs: context, decision, consequences.

## 1. Write user-scope `~/.claude.json` directly (marker + ledger), not `claude mcp add`

**Context.** Enabling must register an HTTP MCP server with Claude Code at user scope, and disable/uninstall must
remove exactly what we added. `claude mcp add/remove` edits the same file, which also holds lots of other state.

**Decision.** `internal/claudeconfig` edits `mcpServers` directly. Every entry is named `hub__<id>` (marker) and
recorded with a hash of its canonical JSON in `~/.mcp-hub/state.json` (ledger). We change or remove an entry only when
the marker and an unchanged ledger hash both say it is ours.

**Consequences.** Testable against temp files with no `claude` binary. Ownership is provable, so uninstall is safe:
`claude mcp add` leaves no marker, so we could not tell our entry from one the user made or edited. We own write
safety (atomic rename, backups, flock, lost-update re-check). We are coupled to the file format, but the surface is
one top-level key and everything else passes through as raw JSON.

## 2. Write ordering: ledger → config → ledger

**Context.** A crash can land between the ledger write and the config write.

**Decision.** Save the ledger as old ∪ new first (a replacing record carries `PrevHash`), then rename the config, then
save the final ledger. Any error before the rename restores the previous ledger.

**Consequences.** A crash leaves the ledger claiming an entry that is absent or still at `PrevHash`; both count as
ours, so the next call self-heals. Config-first was rejected: a crash would leave an unrecorded `hub__` entry that we
would then treat as user-owned and refuse to touch forever. The review of PR #4 found the earlier version could leave
the ledger claiming a user-created entry after a failed retry (so Remove would delete it); fixed with one deferred restore.

## 3. Enforcement belongs in Claude Code managed settings, not the skill

**Context.** Found in real use: before the skill was installed, asking Claude for an MCP server made it run
`claude mcp add` and bypass the hub entirely. The skill now says "never edit `~/.claude.json` or run `claude mcp add`",
but that is guidance to a model, not a control.

**Decision.** The catalogue and skill **guide**; they do not **enforce**. In production, enforcement is a managed
Claude Code settings file deployed by MDM that allows only MCP servers whose URL is the hub proxy
(`http://127.0.0.1:7420/mcp/*`), so a hand-added server is not loaded.

**Consequences.** This take-home does not enforce: a developer can still add servers by hand. The skill also only
pre-approves read-only commands (`search`, `info`, `status`), so `enable`/`disable` still hit Claude Code's own
permission prompt (a PR #14 review catch).

## 4. Per-server `hub__` entries now, a single gateway later

**Context.** Claude Code reads MCP servers from config at load time, so after enabling a server the user must run
`/reload-plugins` (or restart Claude Code) before its tools appear. Hands-on testing showed `/reload-plugins` adds
servers but does not remove them, so after a disable the user must restart Claude Code (PR #25).

**Decision.** One `hub__<id>` entry per enabled server, each pointing at `/mcp/<id>`. Simple, maps one-to-one to audit
and token audience, and disable is a config delete.

**Consequences.** The reload and restart steps stay. A disabled id is refused by the proxy, so stale tools fail closed. The fix is a single gateway MCP entry that aggregates tools from all enabled
servers and sends `notifications/tools/list_changed` when one is enabled. Deferred: it means a tool-namespacing and
session-multiplexing layer, too much for the timebox.

## 5. Claude Code only

**Context.** The brief allows any assistant. The hiring manager confirmed Claude Code is the adopted assistant.

**Decision.** Target Claude Code only: its user-scope config, its skill format, its permission prompts.

**Consequences.** Simpler, testable integration. The proxy, catalogue and tokens are assistant-agnostic, so another
client would need only a config writer and a skill equivalent.

## 6. Local MCP servers run as processes, not Docker containers

**Context.** Local servers need isolation from each other and from the developer's credentials.

**Decision.** Run them as plain processes: verified binary, minimal environment (the daemon's env is not inherited),
loopback port, own process group, per-server audience-scoped token, 0600 log.

**Consequences.** No Docker Desktop dependency or licence on developer laptops, and fast start. Weaker isolation than
a container: a server runs as the user and can read their files. Acceptable for internally built, reviewed,
checksum-verified servers; a sandbox (macOS `sandbox-exec`, containers) is a later hardening step.

## 7. Public DeepWiki / Context7 stand in for internal remote servers

**Context.** The bank's internally hosted remote MCP servers are not reachable from a take-home.

**Decision.** Use public, unauthenticated DeepWiki and Context7 as `kind: remote` examples. Remote entries must be
`https` and `auth: none`, so no hub token or other credential is sent to a third party.

**Consequences.** The remote path (proxy, audit, response hardening) is real and testable. The "remote server with an
IdP token" path is not exercised; the local `service-directory` server demonstrates token verification instead.

## 8. Skills ship inside the binary today; a Claude Code plugin in an internal marketplace in production

**Context.** Engineers should be able to ask Claude Code to "build an MCP server for my team's service" or "publish
my MCP server to the hub". That needs two more skills (`mcp-hub-build-server`, `mcp-hub-publish-server`) next to the
`mcp-hub` skill, all kept in step with the template, `docs/publishing.md` and the daemon.

**Decision.** All three live in `internal/skillfiles/<name>/SKILL.md`, embedded with `go:embed`. `mcp-hub
install-skill`, `install.sh` and the UI banner write each one to `~/.claude/skills/<name>/`. In production they would be
packaged as a Claude Code plugin in an internal plugin marketplace. The single binary is this demo's one delivery
unit, and the exercise has nowhere to host a marketplace.

**Consequences.** One install path and one version, so the skills always match the binary; tests fail if a skill's
name or `install.sh`'s uninstall list drifts. Updating a skill needs an mcp-hub release. A marketplace would let the
platform team ship skill fixes independently and let Claude Code manage enable, disable and update. The builder and
publisher skills pre-approve no tools, so edits, tag pushes and `gh pr create` all go through Claude Code's permission
prompts.

## 9. What we intentionally did not build

| Not built | Why / what would replace it |
|---|---|
| Claude Code plugin marketplace | ADR 8: skills ship in the binary for now |
| Real IdP / SSO | Local Ed25519 issuer with the same JWKS contract; see design.md "Identity tokens" |
| Code signing / notarisation | sha256 against the catalogue only; production adds signed artifacts |
| Devcontainers | Brief scoped to local macOS laptops |
| Windows / Linux packaging | Release builds linux-amd64 but the installer and LaunchAgent are macOS only |
| Gateway MCP entry | ADR 4 |
| Central catalogue service | Catalogue is embedded or a file; production shape in design.md |
