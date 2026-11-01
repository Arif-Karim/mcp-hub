# verify

## Guarded failures (catalogue/registry)

| Failure | Guard | Proving test(s) |
|---|---|---|
| Malformed / unknown-field catalogue | strict YAML (`KnownFields`), `ErrInvalid` | `TestParseIsStrict`, `TestParseValidates` |
| Bad entry (id, enums, required fields, duplicate id) | `Validate` reports all problems with id + field | `TestValidateRejects`, `TestValidateReportsEveryProblem`, `TestSampleInvalidCatalogueIsRejected` |
| Hub token sent to a third-party remote | remotes must be `https://` and `auth: none` | `TestValidateRejects/remote_hub-jwt`, `.../remote_url_http` |
| Invalid catalogue at startup | `serve` refuses to start, lists every problem | `TestLoadCatalogue` (cmd/mcp-hub) |
| Pending/revoked server enabled | `CanEnable` → `ErrNotApproved` | `TestCanEnable` |
| No build for this OS/arch | `Select` → `ErrUnsupportedPlatform` | `TestSelect` |
| Registry down / 4xx / 5xx / cancelled | `ErrRegistryUnavailable` (wraps ctx error) | `TestInstallRegistryUnavailable`, `TestInstallClosedServer`, `TestInstallHonoursContext` |
| Credentials in remote url / `javascript:` docs link (stored XSS) | remote `url` without userinfo; `docsUrl` https-only | `TestValidateRejects/remote_url_userinfo`, `.../docsUrl_javascript` |
| Tampered artifact | sha256 checked before unpack → `ErrChecksumMismatch`, nothing left on disk | `TestInstallChecksumMismatch`, `TestInstallVerifiesChecksumBeforeUnpacking` (unsafe archive + wrong sha → checksum error, archive never opened) |
| Path traversal / absolute path / symlink / hardlink / fifo in archive | `ErrUnsafeArchive` (→ `artifact_rejected`), scratch dir removed | `TestInstallRejectsUnsafeArchives`, `TestInstallRejectsCorruptArchive` |
| Oversize download / decompression bomb / inode exhaustion | 100 MiB cap on download and unpacked total, 10k entry cap → `ErrTooLarge` (→ `artifact_rejected`) | `TestInstallRejectsOversizeDownload`, `TestInstallRejectsDecompressionBomb`, `TestInstallUnpackedSizeBudget`, `TestInstallRejectsTooManyEntries` |
| https registry redirecting to plain http | redirect downgrade refused → `ErrRegistryUnavailable` | `TestInstallRefusesSchemeDowngradeRedirect` |
| Unsafe id/version used as a path | catalogue `version` pattern; installer refuses before any request | `TestValidateRejects/entry_version_path`, `TestInstallRejectsUnsafeIDOrVersion` |
| Concurrent enable of the same server (double-click, UI + skill) | per-id/version lock + marker re-check | `TestInstallConcurrentCallersShareOneInstall` (8 callers → 1 download, same dir) |
| Re-enable re-downloads / stale install / crash mid-install | `.sha256` marker: match → no request; mismatch → old renamed aside, new renamed in; scratch swept next time | `TestInstallIsIdempotent`, `TestInstallReplacesStaleInstall`, `TestInstallSweepsLeftoversFromCrashedInstalls` |
| Sentinel → contract code drift | `server.ErrorStatus` table | `TestErrorStatusMapsSentinelsToContractCodes` |
| Artifact URLs leaking via API | `json:"-"` on `Artifacts` | `TestArtifactsNeverSerialised`, `TestGetCatalogueItemShape` |
| Bad `kind`, unknown id, wrong method | 400 `bad_request`, 404 `not_found`, 405 | `TestListCatalogueRejectsBadKind`, `TestGetCatalogueItemNotFound`, `TestCatalogueRejectsWrongMethod` |

Mutation efficacy (`scripts/mutation.sh`, timeout coefficient 20, 0 timed out): catalogue 100% (37 killed),
registry 96.6% (56 killed, 2 lived — equivalent: HTTP 299 boundary and the default client timeout constant),
server 100% (9 killed), cmd/mcp-hub 100% (6 killed).

Known gap: the install lock is in-process only; two daemons sharing one `MCP_HUB_HOME` are not coordinated
(the loopback daemon is a per-user singleton, so this needs a file lock only if that assumption changes).
