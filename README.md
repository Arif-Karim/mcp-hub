# mcp-hub

A governed MCP marketplace for Claude Code: browse an approved catalogue, enable a server in one click
(or by asking Claude Code), and get a verified install, short-lived injected credentials and an audit log —
with no secrets in your Claude config.

> Work in progress — built in a 3-hour timebox. See [`evidence/`](evidence/) for intent, design and decisions.

## Quick start (macOS)

```sh
curl -fsSL https://raw.githubusercontent.com/Arif-Karim/mcp-hub/main/install.sh | bash
```

What it does (each step is logged; no sudo):

1. Downloads `mcp-hub_darwin_<arch>.tar.gz` for the latest `v*` release and **verifies its sha256** against
   `checksums.txt`. If it does not match, it stops and installs nothing.
2. Installs the binary to `~/.local/bin/mcp-hub`. If that folder is not on your `PATH`, it prints the line to add.
3. Installs the Claude Code skill to `~/.claude/skills/mcp-hub/`.
4. If Claude Code is missing, it offers to install it with the official installer. You log in with your own account.
5. Starts the daemon as a per-user LaunchAgent (`com.mcp-hub.daemon`, on `127.0.0.1:7420` only). It starts at login
   and restarts if it exits. Logs go to `~/.mcp-hub/logs/daemon.log`.
6. Waits for `/api/v1/health` and opens the marketplace at <http://127.0.0.1:7420>.

Then in Claude Code, try: *"find me an MCP server for service ownership"*. You can also use the CLI:
`mcp-hub search`, `mcp-hub info <id>`, `mcp-hub enable <id>`, `mcp-hub status` (`mcp-hub help` lists them all).

Options: `--version vX.Y.Z` (pin or roll back), `--yes`, `--skip-claude`, `--no-launchd`, `--no-open`.
Re-running the installer upgrades in place.

To install from a checkout without a release:

```sh
VERSION=v0.0.0-dev scripts/package-release.sh      # writes dist/mcp-hub_<os>_<arch>.tar.gz and checksums.txt
./install.sh --local-artifact dist
```

### Uninstall

```sh
./install.sh --uninstall            # or: curl -fsSL .../install.sh | bash -s -- --uninstall
```

This stops and removes the LaunchAgent. It removes only the `hub__*` entries that mcp-hub created from
`~/.claude.json`. Entries you edited yourself are kept and listed. It also removes the binary and the skill.
`~/.mcp-hub` (state, logs, audit log) is kept unless you add `--purge`.

See [`evidence/operate.md`](evidence/operate.md) for logs, failure modes, rollback and upgrades.

## Verify

```sh
scripts/verify.sh            # lint, gosec, race tests + coverage gate, govulncheck, gitleaks, UI checks
scripts/verify.sh --mutation # + mutation testing on changed packages
```
