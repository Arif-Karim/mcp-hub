# Agentic workflow

## Tools

- **Claude Code** with **Opus 5.5** as the orchestrator. It planned the work, wrote Phase 0 and spawned subagents.
- **Builder subagents**, one per lane, each in its own **git worktree** and branch (`lane-<letter>-<slug>`). They
  followed `CLAUDE.md`: TDD with visible red → green commits, `scripts/verify.sh` must pass, push once, one PR.
- **Reviewer subagents**, independent of the builder, posted **one** batched review per PR (must-fix / should-fix /
  nice-to-have) after reproducing issues where they could.
- **GitHub**: Actions CI, CodeQL, Dependabot. The human read every review, checked CI and bot comments, and merged.

## How the work was decomposed

1. **Pre-timebox planning.** Scoped the original idea (MCP + plugin marketplace + devcontainers) down to one governed
   vertical slice. Agreed the guarded-failure list, the `hub__` marker + ledger approach, skill-driven UX and a
   prebuilt-binary install for assessors.
2. **Phase 0, contract first (orchestrator, sequential).** `api/openapi.yaml`, domain model, sample catalogue, health
   endpoint test-first, the `verify.sh` harness + CI, and `CLAUDE.md` rules for builders. Every lane built against
   the contract, so lanes could run in parallel without waiting on each other.
3. **Parallel lanes A–F**, each owning disjoint packages (table in `CLAUDE.md`):
   A web UI · B catalogue + registry · C token, proxy, audit · D Claude config writer · E reference MCP server +
   publish pipeline · F2 CLI, skill, installer, release.
4. **Thin vertical slice (F1).** Hub service, runner, daemon wiring and the e2e test that joins everything (PR #15).
5. **Hands-on iteration.** The human used the slice in Claude Code. What they found became lanes G–M and fix PRs
   (below), and the harness was hardened (#22).

**Direct commits to main (lesson learned).** `main`'s first-parent history has 8 direct commits. Six are the Phase 0
contract-first commits, made one after another before any lane started: scaffold + API contract, the server red and
green commits, `verify.sh` + CI, `CLAUDE.md` + the evidence skeleton, and a small evidence tweak. The other two are early
CI fixes to the mutation gate (timed-out mutants were passing it), pushed directly in the middle of Phase 1. Every
other change (23+ PRs) merged through a PR with a merge commit. Branch protection, added later, now blocks direct
pushes. It should have been switched on at the end of Phase 0, so the gate fix would have been reviewed like
everything else.

## Pipeline per lane

```
builder (worktree) ── red → green commits ── verify.sh ── push once ── PR
   └─▶ reviewer agent: one batched review ──▶ builder fixes (red → green) ──▶ CI + CodeQL + bot comments checked ──▶ human merges
```

| PR | Lane | Merged |
|---|---|---|
| #1 | B catalogue, registry, catalogue API | yes |
| #2 | A Vue UI (mock + HTTP clients) | yes |
| #3 | C JWT issuer, proxy, audit | yes |
| #4 | D `~/.claude.json` writer + ledger | yes |
| #5 | CodeQL, Dependabot, npm audit (vuln scan layer) | yes |
| #6 | E reference server + publish workflow | yes |
| #7–#10, #12 | Dependabot bumps | yes |
| #11 | Dependabot TypeScript 7 | closed: failed verify |
| #13 | CodeQL log-injection fixes | yes |
| #14 | F2 CLI, skill, installer, release | yes |
| #16 | CI builds the shippable binary | yes |
| #15 | F1 hub service, runner, daemon, e2e | yes |
| #17 | Catalogue: real service-directory v0.1.0 checksums | yes |
| #18 | H evidence pack + README (this) | open |
| #19 | G skill embedded in the binary, Install-skill banner, `/reload-plugins` wording | yes |
| #20 | Proxy: accept Context7's chunked 202 with no content type | yes |
| #21 | Publishing guide for MCP server authors (`docs/publishing.md`) | yes |
| #22 | Harness: hook-enforced agent rules, `pr-ready.sh` merge gate, PR template | yes |
| #25 | Disable needs a Claude Code restart; servers live in team repos | yes |
| #23 | I "publish your own MCP server" UI | yes |
| #24 | J skills to build and publish MCP servers | yes |
| #26 | Installer picks the newest `vX.Y.Z` release, not GitHub's "latest" | yes |
| #27 | K Activity tab hides connection traffic; proxy passes upstream 405 through | open at time of writing |
| #28 | M dev registry: try any local or remote server before publishing | yes |

## Representative steering prompts (excerpts)

- To builders (`CLAUDE.md`): *"Write the failing test, commit it (red), then implement and commit (green)… Tests must
  use `t.TempDir()` and must never touch the real home directory."*
- To reviewers (`CLAUDE.md`): *"Reviewers post one review with all findings batched. No per-line comment storms."*
- Mid-build, from the human: *"they can't test anything end to end yet"* → *"simplest complete slice first"*.
- To this docs lane: *"Accurate over impressive — only claim what the code/PRs show."*

## Where the human intervened

| Intervention | Effect |
|---|---|
| Demanded a thin vertical slice when nothing was testable end to end | F2 shipped CLI + skill + minimal installer as a draft PR first; F1 shipped hub + daemon + `dev-registry.sh` before its unit tests. TDD pairing is weaker for `internal/hub` as a result |
| Caught CodeQL alerts the orchestrator had not looked at | PR #13 (10 `go/log-injection` alerts → `httpx.LogSafe`); "check CodeQL and bot comments" added to the merge checklist |
| Required CI waits to run in the background | the orchestrator kept working instead of blocking on `gh pr checks` |
| Asked for a vulnerability-scanning layer | PR #5: CodeQL (PR, main, weekly), Dependabot, `npm audit` alongside govulncheck and gosec |
| Corrected the token model | the hub JWT carries user identity (`sub`) so servers can call their own APIs on the user's behalf; see design.md |
| Found Claude bypassing the hub with `claude mcp add` | skill wording, plus decisions.md ADR 3 (managed settings) |
| Tested the slice by hand in Claude Code via the skill; confirmed `/reload-plugins` loads newly enabled servers | enabled remote DeepWiki and local service-directory, called tools; the server verified the hub token and the audit log showed `find_service` |
| Hands-on: the skill was not picked up after install, and "run `/mcp`" did not load new servers | Lane G (#19): skill embedded in the binary, Install-skill banner in the UI, all wording changed to `/reload-plugins` |
| Hands-on: `/reload-plugins` still listed disabled servers | #25: disable says "restart Claude Code"; the hub already refuses disabled ids, so stale tools fail closed |
| Hands-on: Activity tab showed handshake/listing rows that looked like tool calls; DeepWiki `GET` showed 502 | #27 (Lane K): tool calls only by default with a "show connection activity" toggle; upstream 405 (DeepWiki refuses the optional SSE GET with `text/plain`) passed through with the body dropped |
| Cutting v0.1.0: the default install failed because GitHub's "latest" release was the example server's | #26: installer picks the newest `vX.Y.Z` tag |
| Product steer: the hub served consumers only; teams should be encouraged to publish | publishing guide (#21), Lane I publish UI (#23), Lane J build/publish skills (#24), Lane M dev registry (#28) |
| Asked for the agent rules to be enforced, not just written down | #22 harness (see below) |
| Hands-on test found `hub__context7` failing with `start_failed` | Context7 answers notifications with a chunked 202 and no content type, which the proxy's content-type guard blocked; fixed in PR #20 (202 passed through with its body dropped) |

## Notable reviewer catches

| PR | Finding | Fix |
|---|---|---|
| #1 (B) | Concurrent install of the same server failed: a probe of 8 goroutines × 30 rounds gave **162/240 failures** (`rename: directory not empty`), surfacing as 500. Also no entry-count cap (20k empty files unpacked) | per-id lock + marker re-check; 10k entry cap |
| #3 (C) | Upstream responses were served as-is on the hub UI origin: an upstream returning `text/html` could run script that calls `/api/v1` (**XSS on the hub origin**) | content-type allowlist, CSP sandbox, nosniff; Origin/Host check promoted to a server-wide guard against **CSRF and DNS rebinding** |
| #4 (D) | A failed concurrent-change retry left the ledger claiming a user-created `hub__x`, so a later Remove would **delete the user's entry** | single deferred ledger restore + regression test; added flock, `PrevHash` |
| #6 (E) | **SIGTERM hung 10s** and exited 1 with an open `GET /mcp` SSE stream (Claude Code's default). The builder's smoke test never held a stream open | base context cancelled on shutdown + a test that holds SSE open |
| #14 (F2) | `allowed-tools: Bash(mcp-hub:*)` **pre-approved `enable`**, so only model obedience stood between a prompt and a config change | pre-approve only `search`, `info`, `status` |
| #15 (F1) | Runner "never-healthy" fake deadlock-panicked at once, so the **timeout path was never exercised**; SIGKILL skipped surviving group members, orphaning a child that ignores SIGTERM | fake now stays alive and tests assert its pid is gone; reaper SIGKILLs the whole group when the leader exits (tested with a SIGTERM-ignoring grandchild). The red run itself left such orphans on the dev machine |

Lesson repeated across lanes: builders tested the request path they had in mind; reviewers tested the real client's
connection pattern, concurrency, and what a hostile counterpart could send back.

## Harness catches

- **Mutation gate passed vacuously.** Under load (5 lanes in parallel, load avg ~39) gremlins timed out every mutant,
  reported 0 killed / 0 lived and the 60% gate still passed. Fixed in `scripts/mutation.sh`: timeout coefficient 20,
  and fail when mutants existed but none were tested. Lanes re-ran single-worker for honest numbers.
- **A `k++` → `k--` mutant OOM-killed CI** by turning an index loop into an unbounded one. F2's argument parser was
  then written as a single `range` pass with no index arithmetic.
- **Dependabot TypeScript 7** (PR #11) failed lint/typecheck in `verify.sh` and was closed rather than forced through.
- Coverage gate caught 26% on the first run (Phase 0) → gate scoped to `internal/...` and tests added.
- Parallel lanes shared a scratchpad and overwrote each other's `verify.log` → lane-prefixed log names.

## Harness (PR #22)

`CLAUDE.md` rules were text that agents read. #22 made the important ones enforced:

- **Hooks** (`.claude/settings.json`): a PreToolUse guard on Bash (`scripts/hooks/guard-bash.py`, parses each command
  segment with `shlex`, fails closed) blocks builder agents from `gh pr merge`, force-pushing (except
  `--force-with-lease` on their own branch), pushing to `main`, pushing tags, cutting releases and changing repo
  settings. `.go` files are gofmt'd after every edit. A table test runs in `verify.sh`.
- **Merge gate** `scripts/pr-ready.sh <n>`: CI green, 0 code-scanning alerts on the PR ref, 0 bot comments, and a
  final "Review findings → fixes" comment posted after the last review.
- **Branch protection** on `main`: requires verify, mutation and both CodeQL analyses; no force-push or deletion;
  enforced for admins. This is the hard control; the hook guards against agent mistakes and is not a security boundary.
- PR template with a TDD / verify / evidence checklist.

## Per-lane notes worth keeping

- **B:** validation reports every problem at once (entry id + field) so a catalogue PR reviewer sees the whole list.
- **A:** a manual Playwright pass against the mock found Esc not closing the drawer after focus fell to `<body>`;
  fixed with a regression test. The mock client is tree-shaken out of the production bundle.
- **C:** chose a request-header **allowlist** over the brief's blocklist, so `X-Api-Key` and friends never reach upstream.
- **D:** the green step showed one wrong test, not wrong code (a concurrent create merges correctly); the test was
  changed and the reason recorded in the commit.
- **E:** checked the MCP Go SDK API in the module cache rather than guessing; packaging is reproducible (identical
  sha256 across two runs).
- **F2:** running the installer in a throwaway `HOME` found launchd deferring the spawn (`ProcessType Background`);
  fixed with `launchctl kickstart`.
- **F1:** writing hub tests after the slice still found a real bug (failed re-enable stopped routing to a healthy
  server). Lesson from its review: a fake that exits for an unrelated reason makes a timeout test pass for the wrong
  reason, so assert the intermediate state (child alive before, gone after). Also caught: macOS `tar` added an
  AppleDouble `._mcp-server` to the dev archive, so it is now built with the example's `mktar`. The e2e first failed because redirecting `HOME` before `go build` moved the module cache into `t.TempDir()`.
- **G:** the skill now lives in `internal/skillfiles` (`go:embed`), so it always matches the daemon version. A test fails
  if a second copy reappears. Review of #19 caught that Claude Code reads `CLAUDE_CONFIG_DIR`, not our `CLAUDE_HOME_DIR`;
  with it set, the banner would have said "installed" while Claude could not see the skill.
- **K, M:** Activity filtering + 405 pass-through (#27, open at time of writing); dev registry for any local or remote
  server (#28, merged). Each was built test-first, with red → green commits named in its PR.

### Lane J (builder and publisher skills)
- This lane came from the human's steer that engineers should just talk to Claude Code to build and publish an MCP
  server, not read a guide: two new embedded skills, `mcp-hub-build-server` (template copy, rename, tools test-first,
  devtoken smoke test) and `mcp-hub-publish-server` (workflow adaptation, tag, catalogue PR), both written against
  `docs/publishing.md` and the template README; tests pin the facts each must state.
- `skillfiles` now embeds `*/SKILL.md` and installs all of them; `Status` needs every skill present and current
  (red → green), `install-skill` lists each path, and `install.sh --uninstall` plus the macOS installer CI job cover all three.
- Two corrections from the human's testing mid-lane: teams keep their server and release workflow in their own repo
  (only the catalogue PR goes to the hub), and `/reload-plugins` loads newly enabled servers but does not remove
  disabled ones, so disable now says "restart Claude Code" (skill + CLI, red → green).

### Lane I (publish your own MCP server, in the UI)
- This lane came from the human's product steer, not the original plan: the hub should encourage engineering teams to
  build MCP servers for their own services, but the UI only served people consuming them. It adds a "Publish your
  server" header link, a `/publish` guide condensed from `docs/publishing.md` (static TS, no markdown or `v-html`), a
  "Your service here" invite card at the end of the catalogue grid, and a "publish yours too" hint on pending entries.
- UI only, no new deps or API changes: routing is a small `lib/route.ts` on `history.pushState` (the daemon's SPA fallback
  already serves `index.html` for `/publish`). Tests were committed red then green, covering the header link, the 5 steps,
  `rel="noopener noreferrer"` on external links, Copy success, missing-clipboard and rejected-clipboard fallbacks via an
  `aria-live` status, the invite card, and the hint on pending entries only.

### Lane K (Activity tab: tool calls vs connection traffic; DeepWiki GET 502)
- Found in the human's hands-on testing: after `/mcp`, with no tools called, the Activity tab filled with what looked like
  tool calls. They were Claude Code's connection handshake (`initialize`, `notifications/initialized`, `tools/list`,
  `resources/list`, `prompts/list`, SSE `GET`). The tab now shows only `tools/call` by default, with a "Show connection
  activity (handshake, listing, streams)" toggle and friendly labels ("Connected", "Listed tools", "Event stream"). The
  audit file still records everything.
- The same testing showed `deepwiki` `GET` as 502. `curl` showed DeepWiki answers the optional GET SSE stream with
  `405 Method Not Allowed`, `text/plain` (allowed by the MCP spec), and our content-type guard turned it into 502. The
  proxy now passes 405 through with the body dropped (same path as the untyped 202). Review and an assessor's bare
  `curl -X POST` showed the same pattern for 404 (expired session, so the client must re-initialize) and go-sdk's 400/406
  `text/plain`. Any upstream 4xx except 401/403 with an untyped body now keeps its status with the body dropped. The
  content-type rule is unchanged for 2xx, 3xx and 5xx (table test, red → green in `security_test.go`).
