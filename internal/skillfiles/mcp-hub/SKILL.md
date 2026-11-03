---
name: mcp-hub
description: Find, enable and disable approved MCP servers from the company's governed mcp-hub catalogue. Use when the user asks to find, get, install, add or connect an (existing, approved) MCP server or tool, wants to connect Claude to an internal system (service directory, Jira, payments, docs, ...), asks what MCP servers are available or which ones they have enabled, or wants to remove/disable one. To build or publish your own server, use mcp-hub-build-server or mcp-hub-publish-server instead.
allowed-tools: Bash(mcp-hub search:*), Bash(mcp-hub info:*), Bash(mcp-hub status:*)
---

# mcp-hub: governed MCP servers for Claude Code

`mcp-hub` is the only approved way to add MCP servers here. It installs only catalogue entries that
governance approved, verifies artifacts, injects short-lived credentials and audits every call.
Never edit `~/.claude.json` or run `claude mcp add` yourself to work around it.

To build your own MCP server, use the `mcp-hub-build-server` skill; to publish one to the catalogue, use the
`mcp-hub-publish-server` skill.

## Find servers

1. Run `mcp-hub search <a few keywords> --json` (omit keywords to list everything; add `--kind local|remote` if asked).
2. Present matches briefly, one line each: **name** (`id`) — what it does, owner, classification, approval,
   and its tool names. Mention if it is already enabled (`state.enabled`). If nothing matches, say so and
   suggest broader terms; do not invent servers.
3. For more detail on one entry: `mcp-hub info <id> --json`.

Approval rules — check `approval` before offering to enable:
- `approved`: can be enabled.
- `pending`: the owning team announced it and it is coming soon, but it is not approved yet. Do not try to enable it.
- `revoked`: withdrawn by governance. Do not try to enable it; suggest an approved alternative if one exists.

Point out `confidential` classification so the user knows what data it can return.

## Enable

1. **Ask the user to confirm** the exact server (name + id) before enabling. Never enable without a yes.
2. Run `mcp-hub enable <id> --json`. Claude Code will also show its own permission prompt for this command
   (the skill pre-approves only the read-only commands); that is expected.
3. On success tell them it was added as `hub__<id>`, then: **Run `/reload-plugins` in Claude Code (or restart it) to load
   the new server.**

## What do I have enabled?

Run `mcp-hub status` and report the enabled servers (or `mcp-hub search --json` and filter `state.enabled`).

## Disable

Confirm first, then `mcp-hub disable <id> --json`, then tell them: Restart Claude Code to remove it
(`/reload-plugins` adds servers but does not remove them; until the restart its tools fail because the hub no
longer routes them).

## Errors (exit code non-zero; `--json` prints `{"error":{"code","message"}}`)

Explain in plain words, never retry blindly:
- `not_approved` — not approved for use (pending or revoked); see approval rules above.
- `not_found` — no such catalogue entry; search again.
- `checksum_mismatch` / `artifact_rejected` — the download did not match what was approved, so the install was
  blocked for safety. Nothing was installed. Report it to the server's owner.
- `config_conflict` — they already have their own `hub__<id>` entry (or edited it); mcp-hub will not overwrite it.
  They can remove or rename that entry in `~/.claude.json` and try again.
- `config_invalid` — `~/.claude.json` is not valid JSON; mcp-hub will not touch it until it is fixed.
- `registry_unavailable` / `start_failed` — temporary problem downloading or starting it; try again later.
- `unsupported_platform` — no build for this machine.
- Exit code 3 / "daemon is not running" — mcp-hub's background service is down. Tell them to run `mcp-hub serve`
  in a terminal or re-run the installer (`install.sh`).
