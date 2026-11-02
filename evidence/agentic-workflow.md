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
