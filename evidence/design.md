# Design

## Overview

One Go binary, `mcp-hub`, runs a per-user daemon on `127.0.0.1:7420`. Claude Code never talks to an MCP server
directly. Each enabled server becomes a `hub__<id>` entry in `~/.claude.json` whose URL is the hub's own proxy, so
every call goes through one place that adds credentials and writes the audit log.

```
 developer laptop (macOS)
 ─────────────────────────────────────────────────────────────────────────────
  Claude Code ── skill ──▶ mcp-hub CLI ─┐
      │                                 │  /api/v1 (enable, search, status)
      │ hub__<id> = http://127.0.0.1:7420/mcp/<id>
      ▼                                 ▼
  ┌─────────────── mcp-hub daemon (127.0.0.1:7420) ────────────────┐   Browser UI
  │ guard: loopback Host + Origin checks                           │◀── (Activity tab,
  │ /api/v1/*   catalogue · enable/disable · status · audit        │     enable/disable)
  │ /mcp/<id>   proxy: strip client creds, inject JWT, audit event │
  │ /.well-known/jwks.json   dev token issuer (IdP stand-in)       │
  └───┬──────────────────────┬──────────────────────────┬──────────┘
      │ writes               │ starts                   │ proxies
      ▼                      ▼                          ▼
  ~/.claude.json         local MCP server           remote MCP server
  (hub__ entries)        127.0.0.1:<port>,          (https, auth: none)
  ~/.mcp-hub/            own process group,
  state.json, logs/,     verifies JWT via JWKS
  audit.jsonl, servers/        ▲
                               │ tar.gz, sha256 checked before unpack
                         artifact registry (GitHub Releases; prod: Artifactory)
```

## Components

| Package | Responsibility | Boundary |
|---|---|---|
| `internal/catalogue` | strict YAML load, validation (all problems at once), search; default catalogue embedded | pure, no I/O beyond reading the file |
| `internal/registry` | pick the platform artifact, download, **sha256 before unpack**, safe unpack, cache | writes only under `~/.mcp-hub/servers` |
| `internal/runner` | start/stop local servers: free loopback port, minimal env, own process group, health wait | the only code that spawns processes |
| `internal/hub` | enable/disable/restore orchestration, per-id serialisation, rollback, state for the API | composes the other packages |
| `internal/claudeconfig` | atomic, marker + ledger edits of `~/.claude.json` | the only code that writes Claude config |
| `internal/token` | Ed25519 JWT issuer + JWKS | stand-in for the enterprise IdP |
| `internal/proxy` | `/mcp/{id}` reverse proxy, credential stripping/injection, response hardening | reads catalogue + hub state via a resolver |
| `internal/audit` | append-only JSONL + in-memory ring for `GET /api/v1/audit` | never sees arguments or results |
| `internal/server`, `internal/httpx` | root mux, Host/Origin guard, contract error codes | contract: `api/openapi.yaml` |
| `internal/daemon` | builds the whole graph from `MCP_HUB_HOME`; used by `serve` **and** the e2e test | |
| `internal/cli`, `skill/` | CLI over the API; Claude Code skill that drives the CLI | |
| `web/` | Vue 3 UI (catalogue, detail drawer, Activity tab), embedded in the binary | talks to `/api/v1` only |
| `examples/mcp-server-template` | reference `service-directory` server + publish workflow | separate Go module |

The enable/disable/runner/daemon/e2e pieces land in PR #15.

## Enable flow

```
POST /api/v1/servers/{id}/enable  (UI, CLI or skill → CLI)
 hub.Enable (per-id mutex)
 ├─ catalogue.Get / CanEnable        404 not_found · 403 not_approved (nothing installed or written)
 ├─ local only:
 │   ├─ registry.Select(platform)    422 unsupported_platform
 │   ├─ registry.Install             download → sha256 → unpack → ~/.mcp-hub/servers/<id>/<ver>
 │   │                               422 checksum_mismatch / artifact_rejected · 502 registry_unavailable
 │   └─ runner.Start                 env {PATH, HOME, PORT, HUB_JWKS_URL, HUB_AUDIENCE=id, HUB_ISSUER},
 │                                   log ~/.mcp-hub/logs/<id>.log, wait GET /healthz (502 start_failed + log tail)
 ├─ claudeconfig.Add(hub__<id> → http://127.0.0.1:7420/mcp/<id>)
 │                                   409 config_conflict · 422 config_invalid → stop the process THIS call started
 └─ mark enabled → proxy now resolves /mcp/<id>
Claude Code → /mcp/<id> → proxy: resolve → strip client creds → inject JWT (aud=id) → upstream → audit event
```

- **Disable** removes the config entry first. If that conflicts (the user edited `hub__<id>`), the server keeps
  running so Claude Code is never left pointing at a dead route. Then SIGTERM to the process group, SIGKILL after 5s.
- **Restore on daemon start.** For every ledger-owned id: revoked, delisted or no-longer-approved entries are
  auto-disabled (`lastError: "disabled: revoked by platform team"`); approved local servers restart from the cache.
  Revocation in the catalogue reaches every laptop on its next daemon start, without relying on the developer.
- **State.** `running` / `stopped` / `error` (with reason) for local, `ready` for remote, `disabled` otherwise. Only
  enabled and running/ready entries resolve in the proxy. A failed re-enable leaves a healthy server serving.

## Identity tokens

The hub token is an identity assertion, not just a "came through the hub" stamp. `sub` is the developer and `aud`
is the server id, so a token for one server is useless at another. That lets each MCP server call its own backend
APIs on the user's behalf and make its own access decisions (for example "is `sub` in the on-call group for this
service?"). The example server verifies the token against `HUB_JWKS_URL` and binds each MCP session to `sub`.

- Fresh EdDSA (Ed25519) JWT per request: `iss`, `sub`, `aud`, `iat`/`nbf`/`exp` (5 min default, 15 min cap), random
  `jti`, `kid`. Verifiers accept only `EdDSA` (no `none`, no HS* alg confusion) with 30s leeway.
- Key is PKCS8 PEM under `~/.mcp-hub` (dir 0700, file 0600); a group/other-readable key is refused at startup.
- **Production:** the local issuer is replaced by the bank's IdP (OAuth 2.0 token exchange, RFC 8693, of the
  developer's SSO session for a per-server audience token). Every server, local or remote, verifies against the IdP's
  JWKS, which is the same code path the example server uses today. There is **no central token database** to
  protect or revoke from. **Revoking a server** is a catalogue concern (approval → `revoked`: refused on enable,
  auto-disabled on restart). **Controlling a user's access** is the IdP's and each server's concern.

## Security and privacy controls

- **Loopback only.** `serve` refuses a non-loopback `MCP_HUB_ADDR`. A server-wide guard returns 403 for any `Host`
  other than `127.0.0.1`/`localhost`/`[::1]` (DNS rebinding, protects `/api/v1/audit` too) and for any non-GET/HEAD
  request with a non-loopback `Origin`, including `null` (CSRF on enable/disable). The proxy refuses a foreign
  `Origin` on every method. Claude Code and the CLI send no `Origin`.
- **Credentials in.** The proxy forwards an allowlist of request headers only (`Accept`, `Content-Type`,
  `Mcp-Session-Id`, `Mcp-Protocol-Version`, `Last-Event-ID`); client `Authorization`, `Cookie` and anything else are
  dropped. Unknown auth mode, token failure, or a hub JWT bound for plaintext `http://` on a non-loopback host → 502,
  upstream never called. Remote catalogue entries must be `https` and `auth: none`, so no hub JWT goes to a third party.
- **Upstream bytes are inert.** The proxy shares an origin with the UI, so a hostile upstream returning `text/html`
  could script `/api/v1`. Every proxied response gets `nosniff` and `CSP: sandbox; default-src 'none'`; only
  `application/json`, `text/event-stream` or empty bodies pass. Upstream `Set-Cookie` and `WWW-Authenticate` are
  stripped; upstream 401/403 → 502.
- **Supply chain.** sha256 is checked before an archive is opened. Unpack allows only plain files and dirs (no
  traversal, absolute paths, links or devices), caps download and unpacked size at 100 MiB and entries at 10k, and
  refuses https→http redirects. Release archives and the installer use the same checksum rule.
- **Processes.** Local servers get a minimal env (the daemon's environment, which may hold credentials, is not
  inherited), a loopback port, and their own process group. Logs are 0600 in a 0700 dir.
- **Audit / logging.** One event per proxied request: time, server, JSON-RPC method, tool name, status, duration,
  user. Arguments, results, tokens and headers are never decoded or logged. Client-controlled strings are truncated
  and CR/LF-stripped. Request bodies are capped at 4 MiB. SSE streams unbuffered.
- **Privacy.** Synthetic data only. No secret is ever written to Claude Code config: a `hub__` entry is just a URL.

## Claude config safety

`internal/claudeconfig` is the only writer of `~/.claude.json` (`CLAUDE_CONFIG_PATH`).

- **Marker + ledger.** Entries are named `hub__<id>`. `~/.mcp-hub/state.json` records each one with a sha256 of its
  canonical JSON, so Claude Code re-serialising the file is not mistaken for an edit. We modify or remove an entry
  only when the marker **and** an unchanged ledger hash say it is ours; otherwise `config_conflict`. Uninstall
  removes clean owned entries and lists edited ones it kept.
- **Never corrupt.** Unparseable JSON or a non-object `mcpServers` → `config_invalid`, nothing written. Unknown keys
  pass through as raw JSON. Writes are backup (newest 5) → temp file → fsync → rename → dir fsync, keeping mode,
  owner and symlinks. A cross-process `flock` serialises the daemon and CLI; the file is re-read just before rename
  and the edit re-applied once if Claude Code changed it.
- **Ordering ledger → config → ledger** for crash safety (see decisions.md).
- **Known gap.** There is no lock shared with Claude Code itself. A write landing between our re-check and rename can
  still be lost; the window is microseconds and backups cover it.

## Tech choices

- **Go**, standard library first; deps limited to `yaml.v3`, `golang-jwt/jwt/v5` and the official MCP Go SDK (example
  server and e2e). One static binary is easy to ship to laptops and to sign later.
- **Vue 3 + Vite + TS**, hand-written CSS, embedded with `go:embed`. No component library.
- **Bash installer** plus a per-user LaunchAgent; no sudo.
- Contract-first: `api/openapi.yaml` is the source of truth for the UI types, CLI and server.

## Production / cloud shape

| Here (take-home) | In the bank |
|---|---|
| Catalogue YAML embedded in the binary or `MCP_HUB_CATALOGUE` | Central catalogue service; entries change by **PR** reviewed by platform/security, then published. Daemons poll it, so revocation is fleet-wide |
| GitHub Releases + sha256 | **Artifactory** (internal mirror, retention, scanning); signed artifacts and notarised binaries |
| Local Ed25519 issuer | Bank **IdP**: token exchange per server audience; keys in HSM/KMS with rotation (JWKS already supports `kid`) |
| Skill + catalogue guide Claude | **Managed Claude Code settings** allow only MCP servers whose URL is the hub proxy (see decisions.md) |
| `curl … install.sh` | Fleet distribution via **MDM** (Jamf or similar) running the same installer with `--version` pinned |
| `audit.jsonl` on the laptop | Audit events shipped to the SIEM |
| Remote servers: public DeepWiki/Context7 | Internally hosted remote servers behind the same proxy, receiving IdP tokens |
