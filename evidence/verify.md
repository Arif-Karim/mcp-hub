# Verify

## Checks

`scripts/verify.sh` is the single entry point; CI runs the same script.

| Check | Tool | Where it runs |
|---|---|---|
| Module tidiness | `go mod tidy -diff` | verify.sh (local + every PR + main) |
| Lint + static security | golangci-lint (gofmt, goimports, govet, staticcheck, **gosec**, revive, errcheck) | verify.sh |
| Unit + integration + e2e tests, race detector | `go test -race ./...` | verify.sh |
| Coverage gate ≥ 70% over `internal/...` | `go tool cover` | verify.sh |
| Known-vulnerable Go deps | govulncheck | verify.sh |
| Known-vulnerable shipped npm deps (high+) | `npm audit --omit=dev` | verify.sh |
| Secret scan over git history | gitleaks | verify.sh |
| Example server lint + race tests | golangci-lint, `go test` | verify.sh |
| UI lint, typecheck, unit tests, build; `dist` stays clean | eslint, vue-tsc, Vitest, Vite | verify.sh |
| Shippable binary builds with embedded UI; `mcp-hub version` smoke | `go build` | verify.sh |
| Mutation testing, efficacy ≥ 60% on changed packages; fails if no mutant was actually tested | gremlins (`scripts/mutation.sh`) | `verify.sh --mutation` locally; PR CI |
| SAST with cross-function taint tracking (Go + TS) | CodeQL `security-extended` | PR CI, main, **weekly** |
| Real install on macOS: package → `install.sh` → health → `search` → uninstall | `install.sh --local-artifact` | main only (`installer` job) |
| Dependency updates (Go, npm, Actions) | Dependabot | weekly PRs, same gates |
| Release: tag must be on main; build job read-only, publish job runs no repo code | `release.yml` | on `v*` tag |

## Latest results

Taken from the PR descriptions (single-worker gremlins, timeout coefficient 20).

| Package | Mutation efficacy | Source |
|---|---|---|
| `internal/catalogue` | 100% (37 killed) | PR #1 |
| `internal/registry` | 96.6% (56 killed / 2 lived, equivalent) | PR #1 |
| `internal/token` | 100% (21/21) | PR #3 |
| `internal/proxy` | 95.5% (42/44) | PR #3 |
| `internal/audit` | 95.7% (22/23) | PR #3 |
| `internal/claudeconfig` | 91.6% (87 killed / 8 lived: unobservable fsync/close error branches) | PR #4 |
| `internal/cli` | 94.0% (63 killed / 4 lived) | PR #14 |
| `internal/hub`, `internal/daemon`, `internal/server`, `cmd/mcp-hub` | 100% (35, 9, 25, 18 killed) | PR #15 |
| `internal/runner` | 80.8% (21 killed / 5 lived / 2 timed out: timing boundaries) | PR #15 (before review fixes) |

Total Go coverage over `internal/...`: **92.8%** (PR #15; 91.3% on main after PR #14). UI: 56 Vitest tests (PR #2).

## End-to-end test

`e2e/e2e_test.go` `TestEnableCallDisable` (PR #15) runs inside `verify.sh`, all under `t.TempDir()` on a random port:

1. Builds `examples/mcp-server-template`, packages it as tar.gz and serves it from a fake registry with its real sha256.
2. Starts the real daemon (same `internal/daemon` graph as `serve`).
3. Enable `service-directory` → `running`; `~/.claude.json` has `hub__service-directory` → proxy URL.
4. The official MCP Go SDK client calls `find_service` through `/mcp/service-directory` and gets results. This proves
   the hub injected a JWT and the server verified it against the hub's JWKS.
5. `/api/v1/audit` contains the `tools/call` with tool `find_service` and the user.
6. Enabling the revoked entry → 403 `not_approved`.
7. Disable → config entry removed, process gone, proxy returns 404.

Not covered by the e2e: the UI click path and Claude Code itself (checked by hand, see agentic-workflow.md).

## Guarded failures

| Failure | Behaviour | Proving test(s) |
|---|---|---|
| Pending/revoked server enabled | 403 `not_approved`, nothing installed or written | `TestCanEnable`, `TestEnableRefusesUnapproved`, e2e step 6 |
| Revoked after enabling | auto-disabled on daemon start, reason shown | `TestRestore` |
| Tampered artifact | sha256 checked before unpack → 422 `checksum_mismatch`, nothing left on disk | `TestInstallChecksumMismatch`, `TestInstallVerifiesChecksumBeforeUnpacking` |
| Unsafe archive (traversal, absolute path, links, devices, bomb, 10k+ entries) | 422 `artifact_rejected`, scratch dir removed | `TestInstallRejectsUnsafeArchives`, `TestInstallRejectsDecompressionBomb`, `TestInstallRejectsTooManyEntries` |
| https registry redirecting to http | refused → `registry_unavailable` | `TestInstallRefusesSchemeDowngradeRedirect` |
| Local failure after or during start | process stopped, nothing written | `TestEnableLocalFailuresWriteNothing`, `TestEnableConfigConflictStopsProcess` |
| User-owned or user-edited `hub__` entry | 409 `config_conflict`, entry untouched | `TestAddConflictsWithUserOwnedHubEntry`, `TestUserEditedOwnedEntryConflicts`, `TestRemoveAllLeavesUserAndEditedEntries` |
| Malformed `~/.claude.json` | 422 `config_invalid`, file, ledger and backups unchanged | `TestInvalidConfigIsNeverWritten`, `TestEnableConfigInvalid` |
| Failed retry claiming a user entry in the ledger | ledger restored | `TestRetryFailureRollsBackLedger` |
| Concurrent enable (double-click, UI + skill) | one download/start | `TestInstallConcurrentCallersShareOneInstall`, `TestConcurrentEnableStartsOnce` |
| Non-loopback bind | `serve` refuses to start | `TestRequireLoopback`, `TestServeRejectsBadInput` |
| Cross-site request / DNS rebinding | 403 | `TestGuardRejectsCrossSiteStateChangingRequests`, `TestGuardRejectsRebindingHost`, `TestOriginAndHostChecks` |
| Hostile upstream returns `text/html` | 502, body never served on hub origin; CSP sandbox + nosniff on all | `TestUnexpectedUpstreamContentTypeIsBlocked`, `TestProxiedResponsesCarrySecurityHeaders` |
| Client credentials forwarded upstream | stripped; fresh hub JWT injected | `TestHubJWTInjectsFreshTokenAndStripsClientCredentials`, `TestAuthNoneSendsNoCredentials` |
| Hub JWT over plaintext to a remote host | 502, upstream not called | `TestHubJWTNeverSentOverPlaintextToRemoteHost` |
| Token alg confusion / `none` / wrong aud, iss, kid / expired / tampered | rejected | `TestVerifyRejectsAlgConfusionAndNone`, `TestVerifyRejectsWrongAudience`, `TestVerifyRejectsWrongIssuerAndKID`, `TestVerifyRejectsExpiredToken`, `TestVerifyRejectsTamperedPayload` (plus the example server's `hubauth` tests) |
| Signing key readable by others | daemon refuses to start | `TestInsecureKeyFileRejected` |
| Audit leaking arguments | only method + tool name recorded | `TestAuditRecordsMethodAndToolButNotArguments` |
| Invalid catalogue | startup refused, every problem listed | `TestValidateReportsEveryProblem`, `TestLoadCatalogue`, `TestSampleInvalidCatalogueIsRejected` |
| Installer checksum mismatch | aborts, nothing installed | **manual** (tampered `checksums.txt`, PR #14); the macOS CI job covers only the happy path |

## Known gaps

- Installer checksum failure is checked by hand, not in CI.
- No automated test drives the UI against a real daemon or Claude Code itself.
- Runner (PR #15): the reviewer found the health-timeout path untested and a group-member SIGKILL gap; fixes are
  tracked on that PR. A crashed local server is not auto-restarted.
- No lock shared with Claude Code for `~/.claude.json` (microsecond window, backups mitigate).
- A second daemon on the same `MCP_HUB_HOME` is refused by the port bind, not by a file lock.
- Mutation numbers are per-PR snapshots; later changes are re-gated only for the packages they touch.
