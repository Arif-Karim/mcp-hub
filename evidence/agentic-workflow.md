# Agentic workflow

## Tools
Claude Code (Opus 5.5) as orchestrator + builder/reviewer subagents. Human: reviews, tests, merges.

## Log
- **Pre-timebox planning (~1h25m, Toggl):** read the brief with Claude Code, scoped down from the original
  idea (MCP + plugin marketplace + devcontainers) to one vertical slice; agreed guarded-failure list,
  user-scope config with a `hub__` marker + ledger, skill-driven UX, prebuilt-binary install for assessors.
  Saved the multi-agent pipeline as global agent instructions.
- **Phase 0 (orchestrator, sequential):** contract (`api/openapi.yaml`), domain model, sample catalogue,
  health endpoint test-first (red → green commits), `verify.sh` harness + CI, `CLAUDE.md` rules for builders.
  Intervention: two pinned tools (golangci-lint, gitleaks) had incompatible shared deps in one tools module
  → moved gitleaks to `go run …@version`. Coverage gate caught 26% on first run → scoped gate to `internal/...`
  (cmd is wiring, covered by e2e) and added tests.
