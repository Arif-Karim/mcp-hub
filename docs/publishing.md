# Publish an MCP server to mcp-hub

This guide is for teams who want their MCP server in the hub catalogue so any
engineer can enable it from the UI or from Claude Code.

There are two kinds of entry:

| Kind | What the hub does | Use it when |
|---|---|---|
| **local** | Downloads your release, checks its sha256, runs it on the engineer's laptop behind the proxy, and injects a short-lived token naming the user | Your server calls internal APIs on the user's behalf |
| **remote** | Points Claude Code at your hosted HTTPS endpoint through the proxy, with **no** hub token forwarded | You already run a hosted MCP service |

> **Demo vs production.** In this repo, GitHub Releases stands in for the
> bank's artifact registry (Artifactory). The catalogue is a YAML file built
> into the binary or loaded from `MCP_HUB_CATALOGUE`, and GitHub PR review
> stands in for the platform team's approval. Production is described at the
> end.

## 1. Build it (local servers)

Copy [`examples/mcp-server-template`](../examples/mcp-server-template). Its
README is **the standard**:

- streamable HTTP at `/mcp`
- bind 127.0.0.1 only
- verify the hub JWT: `EdDSA`, `kid` from `HUB_JWKS_URL`, `iss`, `aud` = your catalogue id
- `GET /healthz` returns 200
- configuration from `PORT`, `HUB_JWKS_URL`, `HUB_AUDIENCE` and `HUB_ISSUER`
- clean exit on `SIGTERM`
- never log tokens, tool arguments or results

Replace the tools in `internal/directory` and keep the rest.

Use the verified `sub` (the engineer's identity) when you call your own
APIs. Your API makes its own access decisions, for example by AD group, so
the hub does not need to know your permissions model.

Test locally without the hub: `cmd/devtoken` serves a throwaway JWKS and
prints a token. See the template README.

## 2. Release it

A release is **one `.tar.gz` per platform, with an executable named
`mcp-server` at the archive root**, plus `checksums.txt` (sha256).

Platforms are `darwin-arm64`, `darwin-amd64` and `linux-amd64`.

[`.github/workflows/publish-mcp-server.yml`](../.github/workflows/publish-mcp-server.yml)
does this for the reference server. Push a tag `service-directory-v<semver>`,
or run it manually from `main`. It works like this:

1. A **build** job with read-only permissions runs lint, race tests and
   `govulncheck`, cross-compiles, and packages the archives.
2. A **release** job is the only one with write access. It runs no repository
   code; it only publishes the built files.

For your own server, copy the workflow and change the tag prefix and the
working directory.

## 3. Add a catalogue entry (pull request)

Open a PR that adds your entry to `internal/catalogue/default.yaml`.

Start with `approval: pending`. Engineers see it as **"coming soon"**, which
lets other teams know it's on the way. They can't enable it yet.

```yaml
  - id: my-service              # ^[a-z0-9][a-z0-9-]{1,62}$, also the token audience
    name: My Service
    description: One sentence on what it lets Claude do.
    owner: my-team
    version: 0.1.0
    kind: local
    classification: internal    # public | internal | confidential (highest data it can return)
    approval: pending           # platform team flips this to approved
    auth: hub-jwt               # local servers verify the hub token
    tags: [my-domain]
    tools:
      - name: do_thing
        description: What the tool does.
    docsUrl: https://github.com/<org>/<repo>      # https only
    artifacts:
      darwin-arm64:
        url: https://github.com/<org>/<repo>/releases/download/my-service-v0.1.0/my-service_darwin_arm64.tar.gz
        sha256: "<from checksums.txt>"
      # darwin-amd64, linux-amd64 likewise
```

A **remote** entry has `kind: remote` and `url:`, an `https://` URL without
credentials. It has no `artifacts`, and `auth` must be `none`. The hub never
forwards its token to a server it doesn't run.

The catalogue is validated strictly, and an invalid catalogue stops the daemon
with every problem listed. The rules:

- Unknown fields are errors.
- `id` must be unique.
- `docsUrl` and remote `url` must be https.
- Artifact URLs must be https.
- `sha256` must be 64 hex characters.
- An approved local entry must list at least one artifact.

CI (`scripts/verify.sh`) runs the catalogue tests on your PR.

## 4. Approval and lifecycle

| State | Engineers see | Who sets it |
|---|---|---|
| `pending` | "Coming soon"; enable is refused (`not_approved`) | Your team, in the PR |
| `approved` | Can enable it | Platform team, after review |
| `revoked` | Hidden from enable; already-enabled copies are disabled when the daemon restarts | Platform team (incident, end of life) |

The platform team's review is a human check on these points:

- ownership is clear;
- the data classification is honest;
- the tool list matches what the server does;
- the server follows the template standard;
- the checksums match the release.

**New version:** publish a new release, then open a PR that bumps `version`
and replaces the artifact URLs and sha256s. Engineers get the new version the next time it
is enabled or the daemon restarts. Each version installs into its own folder,
`~/.mcp-hub/servers/<id>/<version>/`.

## 5. Try it before the PR merges

```sh
scripts/dev-registry.sh                        # builds the template, serves it on 127.0.0.1:7421,
                                               # writes dist/dev/catalogue.yaml with the real sha256
MCP_HUB_CATALOGUE=dist/dev/catalogue.yaml ./bin/mcp-hub serve
```

Then enable it in the UI at http://127.0.0.1:7420, or ask Claude Code. Run
`/reload-plugins` in Claude Code, or restart it, to load the new server.

## In production (not built here)

These pieces would replace the demo stand-ins:

- **Catalogue service:** a central catalogue service, fed by PRs to a catalogue
  repo, with CODEOWNERS approval by platform engineering. Daemons poll it, so
  approvals and revocations reach laptops without a new hub release.
- **Artifact registry:** Artifactory instead of GitHub Releases. Artifacts
  would be signed, and the hub would check the signature as well as the
  sha256.
- **Identity provider:** the bank's IdP issues the token instead of the hub's
  dev issuer. Servers would verify it in the same way.
- **Self-service and gates:** a self-service "submit a server" form that opens
  the catalogue PR from a release. Automated gates (SAST, dependency scan,
  template-conformance test) would run before human review.
