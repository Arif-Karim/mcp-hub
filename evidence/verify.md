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
| Tampered artifact | sha256 checked before unpack → `ErrChecksumMismatch`, nothing left on disk | `TestInstallChecksumMismatch` |
| Path traversal / absolute path / symlink / hardlink / fifo in archive | `ErrUnsafeArchive`, staging dir removed | `TestInstallRejectsUnsafeArchives`, `TestInstallRejectsCorruptArchive` |
| Oversize download / decompression bomb | 100 MiB cap on download and unpacked total → `ErrTooLarge` | `TestInstallRejectsOversizeDownload`, `TestInstallRejectsDecompressionBomb`, `TestInstallUnpackedSizeBudget` |
| Unsafe id/version used as a path | `ErrUnsafeArchive` before any request | `TestInstallRejectsUnsafeIDOrVersion` |
| Re-enable re-downloads / stale install | `.sha256` marker: match → no request; mismatch → replaced atomically | `TestInstallIsIdempotent`, `TestInstallReplacesStaleInstall` |
| Artifact URLs leaking via API | `json:"-"` on `Artifacts` | `TestArtifactsNeverSerialised`, `TestGetCatalogueItemShape` |
| Bad `kind`, unknown id, wrong method | 400 `bad_request`, 404 `not_found`, 405 | `TestListCatalogueRejectsBadKind`, `TestGetCatalogueItemNotFound`, `TestCatalogueRejectsWrongMethod` |

Mutation efficacy (gremlins, `--timeout-coefficient 20` so slow-but-killed mutants aren't discarded as timeouts):
catalogue 100% (28 killed, 0 lived), registry 95.4% (41 killed, 2 lived — equivalent: HTTP 299 boundary and the
default client timeout constant), server 100% (7 killed, 0 lived).
