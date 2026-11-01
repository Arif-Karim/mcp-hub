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
