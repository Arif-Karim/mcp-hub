---
name: mcp-hub-publish-server
description: Publish an MCP server to the company's mcp-hub catalogue. Use when the user wants to publish, release, ship or list their MCP server in mcp-hub, add it to the hub catalogue, make it available to other engineers, cut a new version of it, or open the catalogue pull request for it.
---

# Publish an MCP server to mcp-hub

Source of truth: https://github.com/Arif-Karim/mcp-hub/blob/main/docs/publishing.md (read it if unsure).
Two kinds of entry:
- **local**: the hub downloads the release, checks its sha256, runs it on the engineer's laptop and injects a
  short-lived hub token. The server must follow the template standard (see the `mcp-hub-build-server` skill).
- **remote**: an existing hosted `https://` MCP endpoint. No release, no artifacts, and the hub forwards **no**
  token to it. Skip to step 3.

## 1. Release workflow (local servers)

The server, its release workflow, its tags and its releases all live in **your team's own repo** on GitHub
(GitHub Releases stands in for the bank's artifact registry). The only change to the hub repo is the catalogue
entry PR in step 4. Copy `.github/workflows/publish-mcp-server.yml` from the hub repo into the team's repo and change:
- the tag prefix: `service-directory-v*` trigger, the `${REF_NAME#service-directory-v}` strip, `tag=` in the
  guard, and the `gh release create` tag;
- the working directory (`examples/mcp-server-template` becomes the server's path in the team's repo, also in
  `go-version-file`, `cache-dependency-path` and the upload `path`);
- the `Version` file path in the guard step (`internal/directory/directory.go`);
- the archive `name=` in `scripts/package.sh` (use the catalogue id);
- the concurrency group (`publish-service-directory`);
- the release title (`service-directory v...`).
Also: the lint and govulncheck steps run tools via `-modfile=../../tools/go.mod`, which exists only in the hub
repo; pin those tools in the new repo instead. Keep the two-job split (read-only `build`, `release` with write
access that runs no repo code) and the SHA-pinned actions. Show the user the diff before committing.

## 2. Release it

All of this happens in the team's repo.

1. Bump `Version` in the tools package and get it merged to `main`. The build fails if it does not match the tag.
2. Tag the merge commit on `main` as `<id>-v<semver>` (e.g. `team-billing-v0.1.0`). **Ask the user before
   pushing any tag**, and show the exact `git tag` / `git push origin <tag>` commands. The build refuses
   commits that are not on `main`, so the team repo's default branch must be `main`.
   Alternative: run the workflow manually on `main` with an untagged version. That also creates a tag and a
   release, so **ask first** in the same way and show `gh workflow run publish-mcp-server.yml -f version=<v>`.
3. When the release exists, copy the `artifacts:` snippet from its release notes
   (`gh release view <id>-v<semver>`), or build it from the release's `checksums.txt`.

**Never invent, guess or hand-type sha256 values.** They must come from the release notes or `checksums.txt`.
If you cannot get them, stop and say so.

## 3. Catalogue entry

Add the entry to `internal/catalogue/default.yaml` in the hub repo (https://github.com/Arif-Karim/mcp-hub):

```yaml
  - id: my-service              # ^[a-z0-9][a-z0-9-]{1,62}$; same as HUB_AUDIENCE and the tag prefix
    name: My Service
    description: One sentence on what it lets Claude do.
    owner: my-team
    version: 0.1.0              # matches the release
    kind: local
    classification: internal    # public | internal | confidential
    approval: pending           # always pending; the platform team approves
    auth: hub-jwt               # local servers verify the hub token
    tags: [my-domain]
    tools:
      - name: do_thing
        description: What the tool does.
    docsUrl: https://github.com/<org>/<repo>      # https only
    artifacts:                  # pasted from the release notes
      darwin-arm64:
        url: https://github.com/<org>/<repo>/releases/download/my-service-v0.1.0/my-service_darwin_arm64.tar.gz
        sha256: "<from the release notes or checksums.txt>"
      # darwin-amd64, linux-amd64 likewise
```

- Choose `classification` **with the user, honestly**: the most sensitive data any tool can return. Do not
  pick a lower one to ease approval.
- The `tools` list must match what the server really exposes.
- Remote entries: `kind: remote`, `url: https://...` (no credentials in it), `auth: none`, no `artifacts`.
- Rules (strict; unknown fields are errors): `version` matches `^[0-9A-Za-z][0-9A-Za-z.+-]*$`; `docsUrl` and
  URLs are https; platform keys are `darwin-arm64`, `darwin-amd64`, `linux-amd64`, `linux-arm64`; sha256 is 64
  lowercase hex.
- In a hub checkout, run `scripts/verify.sh` (or at least `go test ./internal/catalogue/...`) before the PR.

## 4. Open the catalogue PR

Unless the user has write access to `Arif-Karim/mcp-hub`, fork it (`gh repo fork Arif-Karim/mcp-hub --clone`).
Branch, commit the entry, then **ask the user before pushing the branch** to the fork. **Ask the user before
opening the PR**, showing the title and body, then `gh pr create --repo Arif-Karim/mcp-hub --base main`. The body should say what the server does, who owns it,
the classification and why, and link the release.

## 5. What happens next (tell the user)

- `pending`: engineers see "Pending security review" and cannot enable it yet.
- The platform team reviews ownership, honest classification, the tool list, the template standard and that
  the checksums match the release, then sets `approved` (in this demo by PR review; production would use
  CODEOWNERS). `revoked` withdraws it later.
- The catalogue is built into the mcp-hub binary in this demo, so a merged entry reaches engineers with the
  next mcp-hub release. (In production a catalogue service would deliver it without a hub release.)
- New version: release again, then a PR bumping `version` and replacing the artifact URLs and sha256s.
- To try it before the PR merges, see "Try it locally before the PR merges" in `docs/publishing.md`
  (`MCP_HUB_CATALOGUE` pointing at a local catalogue file).
