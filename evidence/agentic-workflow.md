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
| #17 | Catalogue: real service-directory v0.1.0 checksums | yes |
| #15 | F1 hub service, runner, daemon, e2e | open at time of writing (review fixes done) |

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
| Tested the slice by hand in Claude Code via the skill | enabled remote DeepWiki and local service-directory, called tools; the server verified the hub token and the audit log showed `find_service` |

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
  server). The e2e first failed because redirecting `HOME` before `go build` moved the module cache into `t.TempDir()`.
