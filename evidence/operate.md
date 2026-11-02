# operate

How a developer (and the platform team supporting them) runs, debugs, rolls back and upgrades mcp-hub.

## Run

| What | How |
|---|---|
| Install / upgrade | `curl -fsSL https://raw.githubusercontent.com/Arif-Karim/mcp-hub/main/install.sh \| bash` (re-run to upgrade in place; needs a published `v*` release, otherwise use `--local-artifact`) |
| Daemon | Per-user LaunchAgent `~/Library/LaunchAgents/com.mcp-hub.daemon.plist`: RunAtLoad and KeepAlive, listens on `127.0.0.1:7420` (loopback only) |
| Restart daemon | `launchctl kickstart -k gui/$(id -u)/com.mcp-hub.daemon` |
| Stop daemon | `launchctl bootout gui/$(id -u)/com.mcp-hub.daemon` (`install.sh` loads it again) |
| Foreground (debug) | stop the agent, then `mcp-hub serve` |
| Health | `curl http://127.0.0.1:7420/api/v1/health` · `mcp-hub status` |
| CI / headless | `install.sh --no-launchd --no-open --skip-claude`. This uses `nohup`, writes the pid to `~/.mcp-hub/daemon.pid`, and the uninstaller stops it |
| Uninstall | `install.sh --uninstall [--purge]` |

## Logs and audit

- `~/.mcp-hub/logs/daemon.log`: daemon stdout/stderr as JSON lines (`log/slog`). Tokens, Authorization headers,
  tool arguments and tool results are never logged.
- `~/.mcp-hub/logs/<id>.log`: stdout/stderr of each local MCP server (0600). A failed start returns its tail in the error.
- `~/.mcp-hub/audit.jsonl`: one line per proxied MCP request (time, server, method, tool, status, duration, user).
  It never records arguments or results. The UI's **Activity** tab and `GET /api/v1/audit` read it.
- `~/.mcp-hub/state.json`: the ledger of the `hub__*` entries mcp-hub wrote to `~/.claude.json`. `~/.mcp-hub/backups/`
  holds rotated copies of `~/.claude.json` taken before each edit.

## After enabling or disabling

Claude Code reads MCP servers when it loads, so a newly enabled server's tools appear only after **`/reload-plugins`
(or restarting Claude Code)**. Check with `/mcp`: enabled servers are listed as `hub__<id>`. The same applies after a
disable. A single gateway entry would remove this step (decisions.md ADR 4).

## Config and secrets

- Settings come from environment variables only. `MCP_HUB_ADDR` (loopback only, enforced by both the daemon and the CLI),
  `MCP_HUB_HOME` (default `~/.mcp-hub`), `CLAUDE_CONFIG_PATH` (default `~/.claude.json`) and `MCP_HUB_CATALOGUE`
  (catalogue override). The LaunchAgent sets `MCP_HUB_HOME` and `MCP_HUB_ADDR`.
- **No secrets in Claude Code config.** Each `hub__<id>` entry is just an HTTP URL to the local proxy. The proxy adds a
  short-lived JWT (audience = server id) for each request. The signing key stays under `~/.mcp-hub` (owner-only) and
  stands in for the enterprise IdP.
- The installer needs no credentials and no sudo. Release assets are sha256-verified before anything is written.

## Failure modes

| Symptom | Cause | What happens / what to do |
|---|---|---|
| `mcp-hub daemon is not running on 127.0.0.1:7420` (CLI exit 3, the skill tells the user) | agent not loaded, crashed, or not installed | `launchctl kickstart -k gui/$(id -u)/com.mcp-hub.daemon`, or re-run `install.sh`, then check `daemon.log` |
| Installer: `sha256 mismatch ... nothing was installed` | tampered or corrupted download, or wrong `checksums.txt` | Aborts before touching the system. Re-run. If it repeats, report it to platform engineering |
| Installer: `daemon did not become healthy within 15s` | port 7420 in use, bad catalogue override, crash at startup | read `~/.mcp-hub/logs/daemon.log`; `lsof -iTCP:7420` |
| Installer warns that the daemon reports a different version | another `mcp-hub` is holding the port | stop the other process and re-run |
| `enable` → `not_approved` (exit 2) | entry is pending or revoked | expected; pending entries are coming soon, revoked ones are withdrawn |
| `enable` → `checksum_mismatch` / `artifact_rejected` | registry artifact does not match the catalogue | the install is blocked for safety; tell the server owner |
| `enable` → `config_conflict` | the user already has their own `hub__<id>` entry, or edited ours | mcp-hub will not overwrite it. Remove or rename the entry, then retry |
| `enable` → `config_invalid` | `~/.claude.json` is not valid JSON | mcp-hub does not write to it. Fix the file, or restore from `~/.mcp-hub/backups/` |
| `enable` → `registry_unavailable` / `start_failed` | network or registry outage, or the server crashed on start | retry. The `state.lastError` field (`mcp-hub info <id>`) has details |
| Enabled, but Claude Code doesn't show the tools | Claude Code has not reloaded its MCP config | `/reload-plugins` or restart Claude Code; `/mcp` should list `hub__<id>` |
| `enable` → `start_failed` for a local server | the server crashed or never answered `/healthz` | read `~/.mcp-hub/logs/<id>.log` |
| Tool calls fail after a revoke | the catalogue revoked the entry; the daemon auto-disables it on its next start | `mcp-hub disable <id>` now, then `/reload-plugins` |
| Uninstall prints `Kept hub__x` | the user edited that entry | kept on purpose; remove it by hand if you no longer want it |

## Rollback

- **Binary:** `install.sh --version vX.Y.Z` installs the release you name (checksum-verified) and restarts the agent.
  The data in `~/.mcp-hub` and `~/.claude.json` stays compatible: entries are plain URLs.
- **A bad MCP server:** `mcp-hub disable <id>` (or **Disable** in the UI) removes `hub__<id>` from Claude config and
  stops it. For everyone: revoke it in the catalogue. Revoked entries cannot be enabled.
- **Claude config:** every edit is backed up first to `~/.mcp-hub/backups/claude.json.<timestamp>`.
- **Everything:** `install.sh --uninstall` removes only what mcp-hub created.

## Upgrade path

1. Merge to `main` (CI: verify + mutation on PRs; the macOS `installer` job runs on `main`).
2. Tag `vX.Y.Z` on a commit that is on `main` (or run the `release` workflow from `main`). The build job tests and runs
   `scripts/package-release.sh` for darwin-arm64, darwin-amd64 and linux-amd64 with the version in the ldflags. The release
   job then publishes the tarballs and `checksums.txt`. The release job runs no repo code.
3. Developers re-run `install.sh`. It installs the latest `v*`, replaces the binary atomically, refreshes the skill and
   restarts the LaunchAgent. In the bank this would be a managed push (Jamf or similar) of the same script with `--version` pinned.

## Dependency routine

Dependabot opens weekly PRs for Go modules, `web/` npm and GitHub Actions. The release and installer workflows pin actions by SHA with a version comment.
Each Dependabot PR goes through the same `scripts/verify.sh` gates (govulncheck, `npm audit`, gitleaks, tests, coverage)
plus mutation testing. Merge it when green. Security updates go in the next release tag. Dev tools are pinned in `tools/go.mod`
and get bumped the same way.
