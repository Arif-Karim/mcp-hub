# design

_To be completed during the build._

## Security controls (proxy/token)

- **Credentials in, credentials out.** The proxy forwards only an allowlist of request headers upstream
  (`Accept`, `Content-Type`, `Mcp-Session-Id`, `Mcp-Protocol-Version`, `Last-Event-ID`). Client
  `Authorization`, `Cookie`, `Proxy-Authorization` and any other header are always dropped. Upstream
  `Set-Cookie` and `WWW-Authenticate` are removed (no cookies planted on the hub origin; no OAuth flow
  started against an upstream-controlled URL). Upstream 401/403 → 502 `start_failed` "upstream rejected hub
  credentials" (audited with the upstream status).
- **Upstream content is inert on the hub origin.** Every proxied response carries `X-Content-Type-Options: nosniff`
  and `Content-Security-Policy: sandbox; default-src 'none'`; only `application/json` and `text/event-stream`
  (or an empty body) pass, anything else (e.g. `text/html`) → 502 `start_failed`. Without this a malicious
  upstream could serve HTML that runs same-origin with `/api/v1` and enables servers.
- **Injected token.** For `auth: hub-jwt`, every request gets a fresh EdDSA (Ed25519) JWT:
  `iss` = hub, `sub` = local user, `aud` = the catalogue entry id (a token for one server is useless at
  another), `iat`/`nbf`/`exp` with a 5-minute default TTL (hard cap 15 min), random `jti`, `kid` header.
  `auth: none` sends no credentials. Token failure, an unknown auth mode, or a `hub-jwt` upstream on plaintext
  `http://` to a non-loopback host → 502, upstream never called.
- **Keys.** Ed25519 PKCS8 PEM at a configured path, created with dir 0700 / file 0600; a key file readable by
  group/other is refused at startup. Public key published at `/.well-known/jwks.json` (`Cache-Control: max-age=300`).
  The verifier accepts only `EdDSA` (rejects `none` and HS* alg-confusion), checks `kid`, `iss`, `aud`, `exp`, `nbf`,
  `iat` with 30s leeway.
- **Who can call the daemon.** Binds loopback only. A server-wide guard (`internal/server/guard.go`) rejects (403)
  any request whose `Host` is not `127.0.0.1`/`localhost`/`[::1]` (DNS rebinding — protects `/api/v1/*` too) and
  any non-GET/HEAD request with a present non-loopback `Origin`, including `null` (CSRF against enable/disable).
  The proxy is stricter: a non-loopback Origin is refused on every method (GET opens an SSE stream). Claude Code
  and the CLI send no Origin, so they are unaffected.
- **Limits.** Request bodies capped at 4 MiB (413). SSE responses stream unbuffered (`FlushInterval: -1`).
- **Audit / logging.** One event per proxied request (server, JSON-RPC method, tool name for `tools/call`,
  upstream status, duration, user) to an append-only 0600 JSONL file + in-memory ring served at
  `GET /api/v1/audit`. Tool arguments, results, tokens and headers are never decoded into the event or logged;
  client-controlled method/tool strings are truncated to 128 chars. Unknown server ids are rejected (404) and
  not audited, since nothing was proxied. Client disconnects are audited as 499. File write failures are counted
  (`Log.WriteErrors()`, for a future `degraded` status flag) and logged without flooding.
- **What a real IdP would replace.** The local issuer stands in for an enterprise IdP: in production the proxy
  would perform OAuth 2.0 token exchange (RFC 8693) of the developer's SSO session for a per-server audience
  token, keys would live in an HSM/KMS with rotation (JWKS already supports multiple keys by `kid`), and audit
  events would ship to the SIEM rather than a local file.

## Claude config safety

`internal/claudeconfig` is the only code that writes `~/.claude.json` (path: `CLAUDE_CONFIG_PATH`, default `~/.claude.json`).

- **Marker + ledger.** Entries are named `hub__<id>`; `~/.mcp-hub/state.json` (`{"version":1,"servers":{id:{...}}}`,
  0600, dir 0700) records each one with a sha256 of its canonical JSON (sorted keys, compact — so Claude Code
  re-serialising the file is not mistaken for an edit). Ids must match `^[a-z0-9][a-z0-9-]{1,62}$`.
- **Conflict rules (`ErrConfigConflict` → `config_conflict`).** Add/Remove refuse a `hub__<id>` that is not in the
  ledger (the user made it) or whose hash differs from the ledger (the user edited it). Re-adding an unchanged owned
  entry is a no-op. Remove of an owned entry already gone just drops the ledger record. `RemoveAll` (uninstall) removes
  every clean owned entry in one write and returns `*PartialRemoveError{Skipped}` (unwraps to `ErrConfigConflict`)
  listing the edited ones it left behind. Non-`hub__` keys are never touched.
- **Invalid input (`ErrConfigInvalid` → `config_invalid`).** Unparseable JSON, non-object top level, or a non-object /
  null `mcpServers` → error, nothing written (file bytes, ledger and backups unchanged).
- **Preservation.** Both levels decode into `map[string]json.RawMessage`, so values we do not own pass through
  (no HTML escaping, numbers untouched). Key order is not preserved; output is 2-space indented like Claude Code.
- **Atomic write.** Backup current bytes to `~/.mcp-hub/backups/claude.json.<UTC ns timestamp>[-NNN]` (0600, `O_EXCL`,
  fsync'd, named to sort after the newest even if the clock repeats; newest 5 kept; only files matching that exact layout
  are pruned), write a temp file in the target's directory, fsync, keep the original mode (new file 0600) and owner
  (best effort, so `sudo` cannot leave it root-owned), rename, fsync the dir.
  A symlinked `~/.claude.json` is resolved and the real file replaced, so the link survives; a dangling link is an error.
- **Lost-update guard.** Immediately before rename the file is re-read and compared to the bytes we parsed. If it changed,
  the edit is re-applied once on the fresh content; if it changed again we return `ErrConfigConflict` ("retry").
  An in-process mutex plus an exclusive advisory `flock` on `<MCP_HUB_HOME>/claudeconfig.lock` (0600, unix) serialise
  every read-modify-write across the daemon and the CLI.
- **Ledger ordering (ledger → config → ledger).** Before the config write the ledger is saved as old ∪ new, where a
  record that replaces an existing entry carries `PrevHash` (the hash it replaces). After the rename the ledger is saved
  as new, and `PrevHash` is cleared once the config entry matches `Hash`. Any error before the rename lands restores the
  previous ledger (one deferred restore), so a failed call never claims an entry it did not write (PR #4 review must-fix:
  previously a failed retry could leave the ledger claiming a user-created `hub__x`, which a later Remove would delete).
  A crash leaves the ledger claiming an entry that is absent or still at `PrevHash`; both count as ours, so the next call
  self-heals. Config-first ordering was rejected: a crash would leave an unrecorded `hub__` entry locked as "user-owned".
- **Known gap.** There is no lock shared with Claude Code itself (its own locking, if any, is not a published contract), so a write landing
  between our re-check and `rename` can still be lost. The window is microseconds and mitigated by the re-check plus
  the backups; it cannot be closed from outside Claude Code.

## Enable flow (lane F1: `internal/hub`, `internal/runner`, `internal/daemon`)

```
UI / CLI / skill            daemon (127.0.0.1:7420)                                    disk / processes
POST /servers/{id}/enable ─▶ hub.Service.Enable (per-id mutex)
                             ├─ catalogue.Get(id)            ── 404 not_found
                             ├─ catalogue.CanEnable          ── 403 not_approved (nothing installed or written)
                             ├─ local only:
                             │   ├─ registry.Select(platform) ── 422 unsupported_platform
                             │   ├─ Installer.Install          ── download → sha256 → unpack ~/.mcp-hub/servers/<id>/<ver>
                             │   │                                (422 checksum_mismatch / artifact_rejected, 502 registry_unavailable)
                             │   └─ runner.Start               ── free loopback port, env {PORT, HUB_JWKS_URL, HUB_AUDIENCE=id,
                             │                                    HUB_ISSUER}, own pgid, ~/.mcp-hub/logs/<id>.log, wait GET /healthz
                             │                                    (502 start_failed + log tail; process killed)
                             ├─ claudeconfig.Add(hub__<id> → http://127.0.0.1:7420/mcp/<id>)
                             │                                 ── 409 config_conflict / 422 config_invalid
                             │                                    → a process started by THIS call is stopped (rollback)
                             └─ record enabled → Resolve(id) now answers the proxy
Claude Code ── /mcp/<id> ──▶ proxy: Resolve → strip client creds → inject hub JWT (aud=id) → upstream → audit event
```

- **Disable** removes the config entry first; if that conflicts (user edited `hub__<id>`) the server keeps running, so
  Claude Code is never left pointing at a dead proxy route the hub can't clean up. Then the process gets SIGTERM to its
  process group, SIGKILL after 5s.
- **Restart (Restore).** For every ledger-owned id: delisted, revoked or no-longer-approved entries are **auto-disabled**
  (config entry removed, WARN log, state `lastError: "disabled: revoked by platform team"` shown in the UI) — revocation
  in the central catalogue reaches every laptop on its next daemon start without trusting the developer to act.
  Approved local servers are restarted from the cached install (re-downloaded if missing), approved remotes are ready.
  A server that fails to come back is `status: error` with the reason and does not resolve.
- **State.** `running` / `stopped` (clean exit) / `error` (crash, with the exit reason) for local servers, `ready` for
  remotes, `disabled` otherwise. Only enabled + running/ready entries resolve in the proxy. A failed re-enable of an
  already healthy server leaves it serving.
- **Process hygiene.** The child environment is minimal (`PATH`, `HOME`, the hub variables, `PORT`) — the daemon's own
  environment, which may hold credentials, is not inherited. Logs are 0600 in a 0700 dir.
- **Known gaps.** The free port is picked then released before the server binds (a race another process could win; the
  health check then fails cleanly). No automatic restart of a crashed server — the UI shows `error`, Enable restarts it.
  A second daemon on the same `MCP_HUB_HOME` would start duplicate processes (config writes are flock-safe; processes are not).

## Identity tokens

The hub token is an identity assertion, not just a "came through the hub" stamp: `sub` is the developer, `aud` the
server. That lets each MCP server call its own backend APIs on the user's behalf and make its own access decisions
(e.g. "is `sub` in the on-call group for this service?"); the example server logs `sub` per tool call. In production the
hub's local issuer is replaced by the bank's IdP (token exchange of the developer's SSO session for a per-server
audience token) and every server, local or remote, verifies against the IdP's JWKS — exactly the code path the example
server uses today against `HUB_JWKS_URL`. There is no central token database to protect or revoke from.
Responsibilities split cleanly: **revoking a server** is a catalogue concern (approval → `revoked`, auto-disabled on
restart, refused on enable); **revoking a user's access** is the IdP's and each server's concern.
