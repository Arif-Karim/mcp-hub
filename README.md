# mcp-hub

A governed MCP marketplace for Claude Code. Developers find an approved MCP server in a catalogue (web UI, CLI, or by
asking Claude) and enable it in one step. mcp-hub verifies the artifact's checksum, runs it locally or proxies a remote
one, injects a short-lived identity token on calls to internal servers (never to third-party remotes) and writes an
audit log. No secrets ever land in Claude Code config.

Built in a 3-hour timebox for a take-home. See the [evidence pack](#evidence-pack) and [time and tools](#time-spent-and-tools).

## Prerequisites

| Who | Needs |
|---|---|
| Assessors / developers | macOS (arm64 or amd64), Claude Code. No Go or Node needed for the release install |
| Contributors | Go 1.27, Node 24 (for the UI), `python3` (only for `scripts/dev-registry.sh`) |

## Quick start (macOS)

```sh
curl -fsSL https://raw.githubusercontent.com/Arif-Karim/mcp-hub/main/install.sh | bash
```

> This installs the published **v0.1.0** release. To install from a checkout instead (needs Go 1.27 + Node 24):
>
> ```sh
> VERSION=v0.0.0-dev scripts/package-release.sh   # writes dist/mcp-hub_<os>_<arch>.tar.gz and checksums.txt
> ./install.sh --local-artifact dist
> ```

What the installer does (each step is logged; no sudo):

1. Downloads `mcp-hub_darwin_<arch>.tar.gz` and **verifies its sha256** against `checksums.txt`. On a mismatch it
   stops and installs nothing.
2. Installs the binary to `~/.local/bin/mcp-hub` (prints the `PATH` line to add if needed).
3. Installs the Claude Code skill (embedded in the binary) to `~/.claude/skills/mcp-hub/` via `mcp-hub install-skill`.
   If the UI shows "Claude Code skill not installed", click **Install skill** there.
4. If Claude Code is missing, offers to install it with the official installer. You log in with your own account.
5. Starts the daemon as a per-user LaunchAgent (`com.mcp-hub.daemon`, `127.0.0.1:7420` only, restarts if it exits).
6. Waits for `/api/v1/health` and opens the marketplace at <http://127.0.0.1:7420>.

Options: `--version vX.Y.Z` (pin or roll back), `--yes`, `--skip-claude`, `--no-launchd`, `--no-open`. Re-running
upgrades in place. **Uninstall:** `./install.sh --uninstall [--purge]` removes the LaunchAgent, binary, skill and only
the `hub__*` entries mcp-hub created (entries you edited are kept and listed). `~/.mcp-hub` is kept unless `--purge`.

## Run from source

```sh
make build && ./bin/mcp-hub serve         # UI + API + proxy on http://127.0.0.1:7420
```

The default catalogue includes two public remote servers that need no setup: DeepWiki, and Context7 (which needs the
proxy fix in PR #20). To try the
**local** server flow (download → checksum → run → JWT verified by the server) without a release:

```sh
scripts/dev-registry.sh                                        # terminal 1: builds the example server, serves it on 127.0.0.1:7421
MCP_HUB_CATALOGUE=dist/dev/catalogue.yaml ./bin/mcp-hub serve  # terminal 2
```

CLI: `mcp-hub search [query]`, `info <id>`, `enable <id>`, `disable <id>`, `status` (`--json` on each; `mcp-hub help`).

## Try it without touching your setup

Everything mcp-hub writes is behind three environment variables, so a sandbox leaves your real `~/.claude.json`,
`~/.mcp-hub` and port 7420 alone:

```sh
tmp=$(mktemp -d)
MCP_HUB_HOME=$tmp/hub CLAUDE_CONFIG_PATH=$tmp/claude.json MCP_HUB_ADDR=127.0.0.1:7499 mcp-hub serve
# in another terminal, point the CLI at the same address
MCP_HUB_ADDR=127.0.0.1:7499 mcp-hub enable deepwiki && cat $tmp/claude.json
```

The installer can run in a throwaway `HOME` with no LaunchAgent and no browser (this is what the macOS CI job does):

```sh
HOME=$(mktemp -d) ./install.sh --local-artifact dist --no-launchd --no-open --skip-claude
```

## Verify

```sh
scripts/verify.sh              # lint + gosec, race tests incl. e2e, coverage gate, govulncheck, npm audit, gitleaks, UI, build
scripts/verify.sh --mutation   # + mutation testing on changed Go packages
```

CI runs the same script on every PR, plus mutation testing and CodeQL. See [`evidence/verify.md`](evidence/verify.md).

## Example workflow

1. In Claude Code: *"get me an MCP server for service ownership"*. The skill runs `mcp-hub search`:

   ```
   $ mcp-hub search ownership
   ID                 NAME               KIND   APPROVAL  CLASSIFICATION  ENABLED  OWNER
   service-directory  Service Directory  local  approved  internal        no       platform-engineering
   ```

2. Claude describes it (owner, classification, tools) and **asks you to confirm**. Claude Code then shows its own
   permission prompt for `mcp-hub enable service-directory`, because the skill pre-approves only read-only commands.
3. mcp-hub downloads and checksums the server, starts it, and adds `hub__service-directory` →
   `http://127.0.0.1:7420/mcp/service-directory` to `~/.claude.json`.
4. Run **`/reload-plugins`** (or restart Claude Code) so it loads the new server.
5. Ask *"which services are tagged payments?"*. Claude calls `find_service` with `{"tag": "payments"}` through the
   proxy, which injects a JWT (`sub` = you, `aud` = `service-directory`). The server verifies it and returns (from the
   synthetic `services.json`):

   ```json
   {"services":[
     {"name":"ledger-api","team":"core-ledger","tags":["payments","ledger","grpc"],"channel":"#oncall-core-ledger","tier":1},
     {"name":"fraud-scoring","team":"financial-crime","tags":["payments","ml","grpc"],"channel":"#oncall-financial-crime","tier":1}
   ]}
   ```

   `tag` is an exact match; `query` is a substring of the service or team name (`{"query":"ledger"}` finds
   `ledger-api`; `{"query":"payments"}` finds nothing, because no name or team contains it).
6. The UI's **Activity** tab (and `~/.mcp-hub/audit.jsonl`) shows the call. Arguments and results are never recorded.
   Illustrative line (shape from `internal/audit`; values synthetic):

   ```json
   {"time":"2026-11-02T01:30:12.345Z","server":"service-directory","method":"tools/call","tool":"find_service","status":200,"durationMs":14,"user":"alice"}
   ```

For *Payments Sandbox*, the skill tells Claude to explain that it was revoked and not to try enabling it;
`mcp-hub enable payments-sandbox` returns `not_approved` (exit 2) regardless.

## Core path / what we'd cut

The core is one thin slice: **enable → verified install → proxy injects an identity token → audited tool call**
(`e2e/e2e_test.go` proves it). Everything else is additive and could be cut without breaking it: the web UI (the CLI
and skill do the same), the LaunchAgent installer, remote servers, the publish workflow and the hardening layers
beyond checksum, loopback and token audience.

## Known limitations

- macOS only (no Windows/Linux installer).
- After enable, Claude Code needs `/reload-plugins` (or a restart). After disable it needs a **restart**:
  `/reload-plugins` does not remove servers (the hub refuses the disabled id meanwhile, so stale tools fail closed).
  A gateway entry would remove both steps (ADR 4).
- Nothing **enforces** use of the hub: a developer can still `claude mcp add` by hand. Production would use managed
  Claude Code settings (ADR 3).
- The token issuer is local, standing in for the bank's IdP; artifacts are checksummed but not signed.
- Local servers run as the user's processes (no container sandbox); a crashed server is not auto-restarted.
- The catalogue is embedded or a local file, not a central service: a revoke takes effect once the laptop has the
  updated catalogue (new release or `MCP_HUB_CATALOGUE` file) and the daemon restarts. Fleet-wide push is not built.

More in [`evidence/verify.md`](evidence/verify.md#known-gaps) and [`evidence/design.md`](evidence/design.md).

## Evidence pack

| File | What it covers |
|---|---|
| [intent](evidence/intent.md) | problem, users, assumptions, constraints, out of scope, success criteria |
| [design](evidence/design.md) | architecture, data flow, identity tokens, security, path to production |
| [decisions](evidence/decisions.md) | 8 numbered ADRs and what we did not build |
| [verify](evidence/verify.md) | checks, results, e2e, guarded failures, known gaps |
| [operate](evidence/operate.md) | run, logs, failure modes, rollback, upgrades |
| [agentic workflow](evidence/agentic-workflow.md) | how Claude Code agents built it, where the human intervened |
| [publishing guide](docs/publishing.md) | how a team publishes its own MCP server to the hub |

## Time spent and tools

- **Time:** a 3-hour timeboxed build (tracked in Toggl), plus about 15 minutes of packaging.
- **Tools:** Claude Code with an orchestrator session and parallel builder and reviewer subagents, each in its own
  worktree (see [agentic workflow](evidence/agentic-workflow.md)). Go, Vue 3 + Vite + TypeScript, GitHub Actions
  (CI, CodeQL, Dependabot, releases).
