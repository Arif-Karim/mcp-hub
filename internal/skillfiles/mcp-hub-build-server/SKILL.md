---
name: mcp-hub-build-server
description: Build a new MCP server for mcp-hub from the company template. Use when the user wants to build, create, write, scaffold or start an MCP server for their team's service or API, or turn an internal API into MCP tools that Claude can call, so it can later be published to the mcp-hub catalogue.
---

# Build an MCP server for mcp-hub

The hub runs **local** servers on the engineer's laptop behind its proxy. Every server starts from
the reference template and keeps its standard; only the tools change. Details:
- Template README (the standard): https://github.com/Arif-Karim/mcp-hub/blob/main/examples/mcp-server-template/README.md
- Publishing guide: https://github.com/Arif-Karim/mcp-hub/blob/main/docs/publishing.md

If the user already runs a hosted HTTPS MCP service, they do not need this; it can be a **remote**
catalogue entry (see the `mcp-hub-publish-server` skill).

## 1. Agree the basics with the user

Ask, and confirm before writing files:
- **Catalogue id**: lowercase, matches `^[a-z0-9][a-z0-9-]{1,62}$`, e.g. `team-billing`. It is also the
  token audience (`HUB_AUDIENCE`), so pick it once.
- **Go module path**, e.g. `github.com/<org>/<repo>` (ask; do not guess an org).
- **Which repo**: the server lives in **your team's own repo** (a new one, or a directory in an existing team
  repo), never inside the mcp-hub repo. Its release workflow lives there too; only the catalogue entry PR goes
  to the hub repo later.
- **The tools**: name, input, output, and which endpoint of their API each one calls.

## 2. Get the template

- If an mcp-hub checkout is at hand (`examples/mcp-server-template` exists): copy that directory into the
  team's repo.
- Otherwise: `git clone --depth 1 https://github.com/Arif-Karim/mcp-hub` into a temporary directory, copy
  `examples/mcp-server-template` into the team's repo, and delete the clone.

Layout: `cmd/mcp-server` (entrypoint, SIGTERM), `cmd/devtoken` (local test issuer), `internal/app` (HTTP,
`/mcp`, `/healthz`), `internal/config` (env), `internal/hubauth` (JWT verification), `internal/directory`
(the tools; replace this), `scripts/package.sh` (release archives).

## 3. Rename

- `go.mod` `module` line and every import of `github.com/Arif-Karim/mcp-hub/examples/mcp-server-template`.
- `Name` (and reset `Version` to `0.1.0`) in the tools package; `Name` is what MCP clients see.
- `name=` in `scripts/package.sh`: it names the archives and the release tag (`<id>-v<version>`), so use the
  catalogue id. Keep the executable inside the archive named `mcp-server`; the hub execs that fixed name.
- The default `-aud` in `cmd/devtoken` and the `service-directory` audience in tests: use the catalogue id.
- Rewrite the README for the new server (tools table, env, run steps).

## 4. Replace the tools (test first)

Replace `internal/directory` (rename the package if you like) with the user's tools:
1. Write the tests first: each tool through a real SDK client, input validation, and failure paths. Run them and
   see them fail.
2. Implement. Each tool calls the user's API **on behalf of the verified `sub`** (`req.Extra.TokenInfo.UserID`);
   the API makes its own access decisions. Take the API base URL from an extra env var and fail fast if it is
   missing, the same way `internal/config` does.
3. Failures (bad input, API errors, not found) are tool results with `isError: true`, not protocol errors.
4. Synthetic data only in tests, fixtures and examples: fictional names and `example.test` identities, never
   real customer, staff or bank data.

## 5. Keep the standard (non-negotiable)

Do not remove or weaken these when editing:
- hub JWT verification on every `/mcp` request (`EdDSA`, `kid` from `HUB_JWKS_URL`, `iss`, `aud` = catalogue id, `exp`, `sub`);
- bind `127.0.0.1` only;
- `GET /healthz` returns 200 without auth;
- env contract `PORT`, `HUB_JWKS_URL`, `HUB_AUDIENCE`, `HUB_ISSUER` (optional), failing fast with the variable name;
- clean, prompt exit on `SIGTERM`;
- log `sub` and tool name only; **never** log tokens, tool arguments or results.

## 6. Verify

From the server directory:
- `go test -race ./...`
- the linter: `golangci-lint run ./...` (the hub repo pins it in `tools/go.mod`; pin it in the team's repo the
  same way, e.g. a `tools/go.mod` used with `go tool -modfile=tools/go.mod golangci-lint run ./...`)
- `scripts/package.sh 0.1.0` builds `dist/` and prints the catalogue `artifacts:` snippet.

## 7. Try it locally

`cmd/devtoken` stands in for the hub (throwaway JWKS on 127.0.0.1:7499 plus a token):

```sh
go run ./cmd/devtoken -aud <id> > /tmp/<id>-token &
PORT=7501 HUB_JWKS_URL=http://127.0.0.1:7499/.well-known/jwks.json HUB_AUDIENCE=<id> go run ./cmd/mcp-server &
curl -s http://127.0.0.1:7501/healthz        # {"status":"ok"}
curl -s -XPOST http://127.0.0.1:7501/mcp     # 401 {"error":"unauthorized"}
```

Then the authorised `initialize` call from the template README, or the MCP Inspector at
`http://127.0.0.1:7501/mcp` with the bearer token. Stop the background processes afterwards.

When it works, offer the `mcp-hub-publish-server` skill to release it and add it to the catalogue.
