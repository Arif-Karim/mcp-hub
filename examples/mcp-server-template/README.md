# Build an MCP server for mcp-hub

This directory is **the standard**: a small, working MCP server (`service-directory`)
that mcp-hub can install, start and front. Copy it, replace the tools in
`internal/directory`, keep everything else.

To release it and add it to the catalogue, see [docs/publishing.md](../../docs/publishing.md).

All data here is synthetic (fictional services, `example.test` identities).

## The standard

| Rule | Why | Where |
|---|---|---|
| MCP over **streamable HTTP** at `/mcp`, using the official Go SDK | One transport the hub proxy understands | `internal/app` |
| Bind **127.0.0.1 only** | Only the local hub may reach it | `internal/config` |
| Every `/mcp` request carries a **hub JWT**; verify it | The hub is the policy point; the server trusts nothing else | `internal/hubauth` |
| `GET /healthz` → `200 {"status":"ok"}`, no auth | Hub readiness probe after start | `internal/app` |
| Config from env, **fail fast** with the variable name | Hub owns the process contract | `internal/config` |
| Exit cleanly and promptly on `SIGTERM`, even with open SSE streams (cancel request contexts on shutdown) | Hub stops servers on disable/shutdown | `cmd/mcp-server` |
| Close idle MCP sessions (30m) | Clients that die without `DELETE` must not leak sessions | `internal/app` |
| Log `sub` + tool name; **never** tokens, args or results | Tool I/O may contain sensitive data | `internal/directory` |
| Tool failures are **tool errors** (`isError: true`), not protocol errors | The model can see and recover from them | `internal/directory` |
| Release = one archive per platform, `mcp-server` at the root | Hub unpacks and execs a fixed name | `scripts/` |

### Token verification

The hub injects `Authorization: Bearer <JWT>` on every proxied request. Reject with
`401 {"error":"unauthorized"}` unless all of these hold:

- `alg` is `EdDSA` (Ed25519). `none`, `HS*`, `RS*` are rejected.
- `kid` resolves in the JWKS at `HUB_JWKS_URL`
  (`{"keys":[{"kty":"OKP","crv":"Ed25519","x":"<base64url>","kid":"…","alg":"EdDSA","use":"sig"}]}`).
  Keys are cached for 10 minutes, then refetched and replaced (retired keys stop working).
  An unknown `kid` triggers at most one refetch per 30s (5s after a failed fetch). The
  fetch runs outside the lock and is shared by concurrent requests. If the hub is
  unreachable, cached keys keep working. Plain `http` is only allowed to loopback, and
  redirects to another host are refused.
- `iss` == `HUB_ISSUER`, `aud` == `HUB_AUDIENCE` (your catalogue id).
- `exp` is present; `exp`/`nbf` checked with 30s leeway. `sub` is present.

The verified `sub` is available to tools as `req.Extra.TokenInfo.UserID`, and the SDK
binds each MCP session to it.

### Environment contract

| Variable | Required | Meaning |
|---|---|---|
| `PORT` | yes | Port to listen on (the hub picks a free one) |
| `HUB_JWKS_URL` | yes | Hub public keys, e.g. `http://127.0.0.1:7420/.well-known/jwks.json` |
| `HUB_AUDIENCE` | yes | Expected `aud` = catalogue entry id, e.g. `service-directory` |
| `HUB_ISSUER` | no | Expected `iss`, default `mcp-hub` |

## Tools

| Tool | Input | Output |
|---|---|---|
| `find_service` | `query?` (substring of name/team), `tag?` (exact) — at least one | `services[]`: name, team, tags, channel, tier |
| `get_runbook` | `service` (exact name) | markdown runbook; unknown → tool error `unknown service` |

## Run locally

`cmd/devtoken` stands in for the hub: it serves a throwaway JWKS and prints a token.

```sh
cd examples/mcp-server-template
go run ./cmd/devtoken -aud service-directory > /tmp/sd-token &   # JWKS on 127.0.0.1:7499

PORT=7501 \
HUB_JWKS_URL=http://127.0.0.1:7499/.well-known/jwks.json \
HUB_AUDIENCE=service-directory \
go run ./cmd/mcp-server &

curl -s http://127.0.0.1:7501/healthz            # {"status":"ok"}
curl -s -XPOST http://127.0.0.1:7501/mcp         # 401 {"error":"unauthorized"}
curl -s -XPOST http://127.0.0.1:7501/mcp \
  -H "Authorization: Bearer $(cat /tmp/sd-token)" \
  -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"curl","version":"0"}}}'
```

Or point the MCP Inspector at `http://127.0.0.1:7501/mcp` with the bearer token.

Tests: `go test -race ./...` (auth cases, tools through a real SDK client, config, archive layout).

## Publish

1. Bump `Version` in `internal/directory/directory.go`.
2. After the bump is merged, tag the merge commit on `main` and push:
   `git tag service-directory-v0.2.0 && git push origin service-directory-v0.2.0`
   (or run the `publish-mcp-server` workflow manually on `main` with a version that has no tag yet).
3. `.github/workflows/publish-mcp-server.yml` has two jobs. `build` (read-only token, no
   persisted credentials) checks the version and that the commit is on `main`, lints, runs
   `go test -race` and `govulncheck`, and packages. `release` (write token, runs no repo
   code) creates the GitHub Release from the uploaded archives and `checksums.txt`. In the
   bank this job pushes to Artifactory and opens the catalogue PR. Actions are pinned to
   commit SHAs.

To build locally: `scripts/package.sh 0.1.0` → `dist/` (gitignored):

```
dist/service-directory_darwin_arm64.tar.gz   # contains only ./mcp-server (0755)
dist/service-directory_darwin_amd64.tar.gz
dist/service-directory_linux_amd64.tar.gz
dist/checksums.txt
```

Builds are reproducible (toolchain pinned in `go.mod`, CGO off, `-trimpath`, empty
build id, fixed archive metadata via `scripts/mktar`), so anyone can rebuild a tag and
match the sha256s. Release URLs use `$GITHUB_REPOSITORY`; set it when packaging a copy locally.

## Catalogue entry to submit

`package.sh` prints the `artifacts:` block; paste it into your catalogue PR:

```yaml
- id: service-directory
  name: Service Directory
  description: Look up internal services, their owning team, on-call channel and runbook.
  owner: platform-engineering
  version: 0.1.0
  kind: local
  classification: internal
  approval: pending          # platform engineering flips this on review
  auth: hub-jwt
  tags: [platform, ownership, runbooks]
  tools:
    - name: find_service
      description: Search services by name or tag.
    - name: get_runbook
      description: Return the runbook for a service.
  docsUrl: https://github.com/Arif-Karim/mcp-hub/tree/main/examples/mcp-server-template
  artifacts:
    darwin-arm64:
      url: https://github.com/Arif-Karim/mcp-hub/releases/download/service-directory-v0.1.0/service-directory_darwin_arm64.tar.gz
      sha256: "<from package.sh>"
    # darwin-amd64, linux-amd64 likewise
```
