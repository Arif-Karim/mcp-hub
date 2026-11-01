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
