# Intent

## Problem
Developers want their AI coding assistant (Claude Code) to reach internal systems — service ownership,
runbooks, Jira, docs. Today each developer hand-edits MCP config, copies tokens into JSON files, and
installs unvetted binaries. In a regulated bank that means: no inventory of what's connected, long-lived
credentials on disk, no audit of what the agent called, and no way to revoke a bad server.

## Outcome
A developer opens mcp-hub (UI or by asking Claude Code), finds an approved MCP server, clicks **Enable**,
and it works in every Claude Code project — with no secrets in their config, a verified binary, and every
tool call audited. Platform teams get a governed catalogue and a standard way to publish servers.

## Users
- **Developers** using Claude Code on macOS laptops — enable/disable servers.
- **MCP server authors** (internal teams) — build from the template, CI publishes an artifact + checksum.
- **Platform / security** — curate the catalogue (approval, classification), read the audit log.

## Assumptions
- Claude Code is the adopted assistant (confirmed with the hiring manager).
- Devs work on local macOS machines (no devcontainers for this slice).
- The enterprise has an IdP that can issue short-lived user JWTs and an artifact store (Artifactory).
  Both are **stubbed**: a local Ed25519 issuer and GitHub Releases / a test HTTP server.

## Constraints
3-hour build timebox; runnable without private credentials or paid services; synthetic data only.

## Out of scope
Real SSO/IdP, real Artifactory, code signing/notarisation, devcontainers, Windows/Linux packaging,
OAuth-based remote servers (e.g. Rovo — shown as *pending approval*), multi-user/central catalogue service.
Claude Code plugin marketplace and a single gateway MCP entry (not built; see decisions.md §8).

## Success criteria
1. `install.sh` on a clean Mac installs the binary + skill and opens the UI — no Go/Node needed.
2. Enabling `service-directory` downloads, verifies and runs it, and Claude Code can call its tools
   through the proxy with an injected, short-lived token; the call shows in the audit log.
3. Enabling a remote public server (`deepwiki`) works with **no** token forwarded upstream.
4. Guarded failures are demonstrated and tested: unapproved/revoked entry refused, checksum mismatch
   refused, malformed `~/.claude.json` not overwritten, user-owned config entries never touched,
   non-loopback bind refused.
5. `scripts/verify.sh` passes locally and in CI on every PR.
