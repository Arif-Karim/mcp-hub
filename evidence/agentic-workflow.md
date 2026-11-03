# Agentic workflow

## Tools
Claude Code (Opus 5.5) as orchestrator + builder/reviewer subagents. Human: reviews, tests, merges.

## Log
- **Pre-timebox planning:** read the brief with Claude Code, scoped down from the original
  idea (MCP + plugin marketplace + devcontainers) to one vertical slice; agreed guarded-failure list,
  user-scope config with a `hub__` marker + ledger, skill-driven UX, prebuilt-binary install for assessors.
  Saved the multi-agent pipeline as global agent instructions.
- **Phase 0 (orchestrator, sequential):** contract (`api/openapi.yaml`), domain model, sample catalogue,
  health endpoint test-first (red → green commits), `verify.sh` harness + CI, `CLAUDE.md` rules for builders.
  Intervention: two pinned tools (golangci-lint, gitleaks) had incompatible shared deps in one tools module
  → moved gitleaks to `go run …@version`. Coverage gate caught 26% on first run → scoped gate to `internal/...`
  (cmd is wiring, covered by e2e) and added tests.

### Lane B (catalogue + registry + catalogue API)
- Builder subagent in an isolated git worktree; three red → green pairs (catalogue, registry, server) visible in history.
- Validation reports **every** problem at once (`errors.Join`, each naming entry id + field), so a catalogue PR
  reviewer sees the whole list; `sample-data/invalid-catalogue.yaml` demos it and a test keeps it invalid.
- Security calls made in-lane: remotes must be `auth: none` (no hub JWTs to third parties), sha256 checked
  before anything is unpacked, tar entries limited to plain files/dirs, unpacked size capped (decompression bomb).
- Mutation gate found 5 surviving registry mutants (size-budget arithmetic, nil-client default) → added
  boundary tests; efficacy 84.9% → 95.4%.
- Intervention: gremlins' default timeout marked 26/34 catalogue mutants "timed out" (not counted), inflating
  efficacy to a meaningless 100%. Re-ran with `--timeout-coefficient 20` to get honest numbers (see verify.md).
- Gotcha: parallel lane agents share a scratchpad; a `verify.log` was overwritten by another lane's run. Use
  lane-unique filenames for logs.

### Lane A (web UI)
- **Built:** Vue 3 + Vite + TS UI in `web/` — catalogue (debounced search, kind / enabled-only chips, approval-aware
  cards), focus-managed detail drawer (`hub__{id}` + proxy URL with copy, restart note), Activity tab (5s polling,
  privacy-explaining empty state), daemon status pill, Claude Code callout, aria-live toasts. Hand-written CSS
  tokens, light/dark, responsive to 380px. No component library.
- **Contract-first:** `src/api/types.ts` mirrors `openapi.yaml`; one `ApiClient` interface with `httpClient`
  (typed `ApiError {status, code, message}`) and an in-memory `mockClient` (`npm run dev:mock`) that follows the
  contract's approval/kind rules. `?demoFail=checksum|conflict` makes one enable fail to demo error UX.
  The mock is tree-shaken out of the production bundle.
- **TDD:** two red → green cycles (API/lib, then components); 56 Vitest tests. A manual Playwright run against the
  mock found a real bug the unit tests missed: Esc didn't close the drawer once the toggle button disabled itself
  mid-request (focus fell to `<body>`). Fixed with a window-level listener plus a regression test.
- **Review round (one batched review, no must-fix):** six should-fixes taken as one more red → green cycle. The
  notable ones: a 404 from a route that isn't wired yet no longer claims "server no longer in the catalogue"; the
  header tells "daemon unreachable" (no response) apart from "status unavailable" (HTTP error); the checksum message
  no longer promises a publisher notification that nothing sends; publisher-supplied `docsUrl` is only linked if
  https (or loopback http). `verify.sh` now also builds the UI and checks `internal/webui/dist` stays clean.
- `npm audit` reports moderate advisories in the dev-only eslint toolchain (minimatch); the only runtime dep is `vue`.

### Lane C (token issuer, MCP proxy, audit log)
- Builder subagent, own worktree/branch `lane-c-proxy-token-audit`. One red commit with the full security test
  matrix (alg-confusion HS256-with-public-key, `alg:none`, tampered payload, expired/nbf with injected clock,
  insecure key perms, Origin/Host DNS-rebinding cases, credential stripping, SSE flush gated on a channel), then
  green commits per package; all tests passed on the first implementation run.
- Chose a request-header **allowlist** over the brief's blocklist (strip Authorization/Cookie/Proxy-Authorization):
  anything not explicitly MCP-related (e.g. `X-Api-Key`) never reaches an upstream. Also strips upstream
  `Set-Cookie`, since the proxy shares an origin with the hub UI.
- Fail-closed choices: unknown `auth` mode and token-issue failure both return 502 without calling the upstream;
  upstream URL must be http(s) with a host; `..` in trailing paths is refused.
- Mutation gate caught a surviving mutant (`target.Host == ""` check untested) → added host-less/`ftp:`/`file:`
  upstream cases. Remaining survivors are equivalent mutants (`len > N` vs `>= N` on truncation).
- Intervention: a shared scratchpad meant another lane's verify log overwrote mine mid-read; switched to
  lane-prefixed log names. `cmd/mcp-hub/main.go` deliberately left unwired to avoid conflicts with integration.
- Mutation results (serial run, `--workers 1`): token 100% (21/21), proxy 95.5% (42/44), audit 95.7% (22/23),
  server 100%. **Gate weakness found:** under heavy machine load (5 lanes in parallel, load avg ~39) gremlins timed
  out every mutant, reported 0 killed / 0 lived and the 60% gate still *passed*. Recommend failing verify.sh when
  `Killed + Lived == 0` or running gremlins with `--workers 1` in CI.
- **Review round (PR #3, reviewer subagent):** caught a real gap the builder's threat model missed — upstream
  responses were served unmodified on the hub UI origin, so an upstream returning `text/html` could run script that
  calls `/api/v1`. Fixed test-first (red → green) with a sandbox CSP + nosniff + content-type allowlist, and the
  loopback/Origin check was promoted from the proxy to a server-wide guard (DNS rebinding on `/api/v1/audit`,
  CSRF on state-changing routes). Also: no hub token over plaintext http to remote hosts, upstream 401/403 →
  502 without `WWW-Authenticate`, 499 for client disconnects, audit write-error counter, key-read TOCTOU check.
  Lesson: proxying third-party bytes onto a privileged origin needs response-side controls, not just request-side.

### Lane D (Claude Code config writer + ledger)
- Builder subagent, single worktree, branch `lane-d-claude-config`. Red commit specified 20+ behaviours first
  (preserve unrelated keys, invalid file untouched, ownership conflicts, symlinks, mode, backups, lost-update guard, id validation).
- Green surfaced one wrong test, not wrong code: "Claude Code creates the file mid-write" expected a conflict, but the
  re-read-and-re-apply path correctly merged into the new file. Test changed to assert the merge (noted in the commit).
- Lint (revive) caught shadowing the `real` builtin; renamed.
- Mutation: 62 killed / 6 lived (91% efficacy). Survivors are fsync/dir-sync/close error branches and an `attempt++`→`--`
  mutant that yields identical retry behaviour — accepted as equivalent rather than adding fault-injection seams.
- Tests use `t.TempDir()` for config, ledger and backups and set `HOME` to a temp dir for `DefaultPaths`; the real
  `~/.claude.json` is never read or written.
- Review round (PR #4, one batched review): must-fix was a real data-loss path — a failed concurrent-change retry left
  the ledger claiming a user-created `hub__x`, so a later Remove would delete it. Reproduced as a red regression test,
  fixed with a single deferred ledger restore. The same round added a cross-process `flock`, `PrevHash` for crash-safe
  URL changes, a typed `PartialRemoveError`, and `O_EXCL` + fsync backups.
- The new frozen-clock backup test caught a second bug in the first fix attempt: after pruning, the unsuffixed name was
  reused and sorted *first*, so the newest backup got pruned next. Fixed by always naming a backup to sort after the newest.
- Mutation after review: `verify.sh --mutation` reported 77 killed / 0 lived / **21 timed out** (100%) under load —
  not trusted. Re-ran single-worker: 88 killed / 8 lived / 2 timed out → **91.7%**. The 2 timeouts are negated
  lock-error checks (deadlock = detected); the 8 survivors are unobservable fsync/close/chmod/rename/dir-sync error
  branches plus the equivalent retry-counter mutant.

### Lane E (reference MCP server template + publish pipeline)
- Builder subagent in an isolated worktree; two red → green pairs (server: config/auth/tools; packaging: archive
  writer) visible in history. Tool tests drive a **real SDK MCP client** over HTTP through the auth middleware.
- Checked the Go SDK v1.8.0 API in the module cache (`go doc`, source) rather than guessing. Found the SDK's
  `RequireBearerToken` returns plain-text 401s, so hub verification lives in our own middleware (JSON 401,
  logged reason, never the token) and an SDK adapter only lifts the verified `sub` into `TokenInfo` — tools see
  the caller and the SDK binds each MCP session to that user.
- Auth tests cover missing/non-bearer/malformed, HS256, `alg:none`, wrong aud/iss, expired/nbf beyond 30s leeway,
  missing exp/sub, unknown kid, wrong key; JWKS refetch on unknown kid is rate-limited (clock-injected test).
- Packaging is reproducible: CGO off, `-trimpath`, empty build id, Go tar writer with fixed metadata — two local
  runs of `package.sh 0.1.0` produced identical sha256s. Smoke-tested the packaged binary with `cmd/devtoken`
  (healthz 200, no token 401, initialize OK, SIGTERM → clean shutdown).
- Intervention: gosec G703/G704 taint findings on the JWKS fetch (operator-configured URL) and the build-time
  archiver; suppressed inline with justification rather than weakening the lint config.
- **Review round (PR #6):** reviewer reproduced a bug my smoke test missed: SIGTERM with an open `GET /mcp` SSE
  stream (Claude Code's default) hung 10s and exited 1. My test closed no stream, so it passed. Fixed with a base
  context cancelled on `Shutdown`, plus a test that holds an SSE stream open. Also from review: JWKS fetch moved
  outside the lock (single-flight, detached timeout, 5s failure backoff, 10m key max age, cross-host redirects
  refused), idle MCP sessions reaped after 30m, and the publish workflow split into read-only build and
  write-only release jobs with SHA-pinned actions. Lesson: smoke-test with the real client's connection
  pattern, not just one request/response.

### Lane F2 (CLI, Claude Code skill, install.sh, release workflow)
- Builder subagent, single worktree, branch `lane-f2-cli-skill-installer`. The red commit specified the CLI against an
  `httptest` fake daemon: table and detail text, raw `--json` passthrough, contract `message (code)` errors, exit codes
  (2 for `not_approved`/validation, 3 when the daemon is unreachable) and offline `uninstall-config` against a real
  `claudeconfig.Writer` in `t.TempDir()`. `cli.Run` returns an exit code and writes to injected writers, so no `os.Exit` runs under test.
- During the build the human changed the priority: "simplest complete slice first". The order became CLI + skill, then a minimal
  `install.sh --local-artifact --no-launchd`, then a **draft PR** with try-it steps. Polish (launchd, release workflow,
  macOS CI job, docs) went onto the same PR afterwards.
- The argument parser is a single `range` pass with no index arithmetic. A previous lane's mutant turned an
  index increment into an unbounded loop and OOM-killed CI, so this design means no mutant can make it loop forever.
- Running the installer in a throwaway `HOME` on a spare port found a real bug that no test covered. launchd accepted
  the LaunchAgent but deferred the spawn (`pended nondemand spawn = speculative`, caused by `ProcessType Background`),
  so the health wait timed out. Fixed by dropping `ProcessType` and running `launchctl kickstart`. Re-verified install, re-install,
  and `--uninstall --purge` through launchd.
- A tampered `checksums.txt` was checked by hand: the installer aborts at the verify step and `~/.local/bin` is never created.
- `uninstall-config` exits 0 when it keeps user-edited `hub__*` entries (`PartialRemoveError`). Leaving them in place is the
  safe outcome, and failing would block `install.sh --uninstall` from removing everything else.

### Lane F1 (hub service: runner, enable/disable orchestration, API, daemon wiring, e2e)
- Builder subagent, branch `lane-f1-hub-service`. Runner went red → green first (TestMain re-exec: the test binary is its
  own fake server — healthy, never-healthy, crash-after, ignores SIGTERM — so no network or example module needed).
- **Mid-lane re-prioritisation from the human:** "they can't test anything end to end yet" → ship the thinnest complete
  vertical slice first. Hub service, routes, daemon wiring and `scripts/dev-registry.sh` landed as one commit,
  smoke-tested by hand (DeepWiki through the proxy; local service-directory `find_service` through the proxy with the
  injected JWT verified by the server) and pushed as a draft PR with try-it steps before the unit tests. Tests followed in
  the same PR; the TDD pairing is therefore weaker for `internal/hub` than for the runner (called out honestly here).
- Writing the hub tests afterwards still found a real bug, fixed red → green: a failed re-enable (config conflict) of an
  already-running server marked it `error` and the proxy stopped routing to it.
- `serve` construction moved into `internal/daemon` so the e2e test and the binary build the exact same graph.
- The e2e initially failed because redirecting `HOME` before `go build` moved the Go module cache into `t.TempDir()`
  (read-only module files broke cleanup) → build first, then redirect `HOME`.
- Dev-catalogue: catalogue validation rejects `file://` artifact URLs (only https or loopback http) — kept that rule and
  serve the dev artifact from a loopback `python3 -m http.server` instead of widening the registry.
- `verify.sh --mutation` (gremlins, timeout coefficient 20): cmd/mcp-hub 18/0, daemon 9/0, hub 35/0, server 25/0 (all
  100%); runner 21 killed / 5 lived / 2 timed out → 80.8%. Runner survivors are timing boundaries (log-tail size cut,
  default-timeout `> 0` checks, poll interval) that change no observable outcome within the test timeouts. Total
  coverage 92.8%. The e2e test (real example server + MCP SDK client) passed 3× under `-race` and now runs in `verify.sh` (lane E
  merged); it skips only with `-short` or if `examples/` is absent.
- **Review round (PR #15).** Must-fix 1: the runner's "never-healthy" fake used `select {}`, which Go's deadlock detector
  kills immediately — so the health-timeout, ctx-cancel and kill-a-live-unhealthy-child paths had never run against a live
  child. Fixed (fake sleeps; tests now assert the child pid is gone). Must-fix 2: Stop could orphan grandchildren; the
  reaper now SIGKILLs the whole group whenever the leader exits — proven with a SIGTERM-ignoring grandchild fake. That red
  run itself left SIGTERM-ignoring orphans on the dev machine (needed `pkill -9`), which is exactly the bug. Should-fix:
  per-id runner locks (a slow health check no longer blocks other ids), hub `Close` + runner `StopAll` drain in-flight
  starts before killing processes, Restore in the background after the listener serves, openapi 409 on disable,
  crash test driven by `/crash` + `Proc.Done()` instead of a 300ms timer, dev archive built with the example's `mktar`
  (macOS `tar` had added an AppleDouble `._mcp-server`). Each as a red → green pair.
- Lesson: a fake that exits for an unrelated reason makes a timeout test pass for the wrong reason — assert the
  intermediate state (child alive before, gone after), not only the final error.
- After the review round (rebased on #13/#14/#16): `verify.sh --mutation` passes. cmd/mcp-hub 18/0, daemon 9/1 (90%;
  the survivor is the drain-timeout warning log), hub 39/0, runner 25 killed / 4 lived / 3 timed out (86.2%, up from
  80.8%), server 25/0. Coverage 92.8%; e2e runs inside verify.

### Lane G (skill ships with the app, UX wording)
- This lane came from the human's hands-on testing, not the original plan. Two findings: after installing, Claude Code
  did not pick up the skill (nothing told the user it was missing or how to load it), and our "run `/mcp`" advice did
  not load newly enabled servers; `/reload-plugins` (or a restart) does. Every user-facing string now says that.
  Follow-up (PR #25): `/reload-plugins` loads added servers but does not remove disabled ones, so disable now says
  "restart Claude Code"; until then the hub refuses the disabled id, so stale tools fail closed.
- The skill moved into the binary (`internal/skillfiles`, `go:embed`), so the skill always matches the daemon version and
  the release tarball is just the binary. `install.sh`, `mcp-hub install-skill` and the UI banner all write it the same
  way (temp file + rename). A test fails if a second copy reappears under `skill/`, so the copies cannot drift.
- The UI reads `Status.skill` and shows an Install/Update banner; the "use mcp-hub from Claude Code" tip only appears once
  the skill is there. `POST /api/v1/skill/install` writes under `~/.claude`, so a test proves a cross-site Origin gets 403.
- Lane G avoided `internal/daemon`/`hub` while #15 was open, then rebased once it merged and wired `Status.skill` through
  `daemon.Config.ClaudeDir` (red → green). Review of #19 caught that Claude Code reads `CLAUDE_CONFIG_DIR`, not our
  `CLAUDE_HOME_DIR`: with it set, the banner would have said "installed" while Claude still could not see the skill, which
  is the very failure this lane fixes. Now resolved in the same order in Go and `install.sh` and forwarded to launchd.
