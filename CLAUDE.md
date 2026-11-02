# mcp-hub — agent instructions

Governed MCP marketplace for Claude Code. A single Go binary (`mcp-hub`) runs a
loopback-only daemon that serves:

- `/api/v1/*` control plane (contract: `api/openapi.yaml` — the source of truth)
- `/mcp/{id}` auth-injecting proxy in front of every enabled MCP server
- `/.well-known/jwks.json` dev token issuer keys (stands in for the enterprise IdP)
- `/` embedded Vue UI (built from `web/` into `internal/webui/dist`)

Enabling an entry: verify approval → (local) download artifact, check sha256, unpack, start →
write `hub__{id}` into user-scope `~/.claude.json` `mcpServers` pointing at the proxy →
record it in the ledger `~/.mcp-hub/state.json`. Claude Code config never contains secrets.

## Layout

| Path | Owner lane | Purpose |
|---|---|---|
| `cmd/mcp-hub` | integration | entrypoint: `serve`, `version`, CLI subcommands |
| `internal/server` | shared | root mux; each lane adds one `Options` field + its route registration |
| `internal/httpx` | shared | `WriteJSON` / `WriteError` + error codes. **All API errors go through it.** |
| `internal/catalogue` | B | model (exists), load + validate + search, embedded default catalogue |
| `internal/registry` | B | download artifact, verify sha256, unpack safely |
| `internal/token` | C | Ed25519 JWT issuer + JWKS handler |
| `internal/proxy` | C | `/mcp/{id}` reverse proxy, token injection |
| `internal/audit` | C | JSONL audit log + recent-events reader |
| `internal/claudeconfig` | D | atomic, marker-scoped edits of `~/.claude.json` + ledger |
| `web/` | A | Vue 3 + Vite + TS UI |
| `examples/mcp-server-template` | E | reference MCP server (own go.mod) |
| `internal/skillfiles` | F/G | Claude Code skill (`mcp-hub/SKILL.md`, embedded) + `install-skill` writer |
| `evidence/` | everyone | evidence pack — append notes relevant to your lane |

## Commands

- `scripts/verify.sh` — everything CI runs. **Must pass before you open a PR.**
- `scripts/verify.sh --mutation` — adds gremlins on Go packages changed vs `origin/main`.
- `make build` / `make run` — build UI + binary, run the daemon on 127.0.0.1:7420.
- Dev tools are pinned in `tools/go.mod`; run them as `go tool -modfile=tools/go.mod <tool>`. Do not install tools globally.

## Engineering rules

1. **TDD.** Write the failing test, commit it (`test(x): ... (red)`), then implement and commit (`feat(x): ... (green)`). Keep this visible in history.
2. Coverage gate is 70% over `internal/...`; mutation efficacy gate is 60% on changed packages. Write tests that assert behaviour (values, error codes), not just "no error" — that's what kills mutants.
3. Standard library first. Approved deps: `gopkg.in/yaml.v3`, `github.com/golang-jwt/jwt/v5`, `github.com/modelcontextprotocol/go-sdk`. Ask the orchestrator before adding anything else.
4. Filesystem paths come from config (`MCP_HUB_HOME`, default `~/.mcp-hub`; `CLAUDE_CONFIG_PATH`, default `~/.claude.json`). **Tests must use `t.TempDir()` and must never touch the real home directory.**
5. Errors map to the contract's error codes via sentinel errors (`errors.Is`) — never string matching.
6. Logging: `log/slog` JSON. Never log tokens, Authorization headers, tool arguments or tool results.
7. Synthetic data only. No real customer, staff or bank data anywhere.
8. Match surrounding code style; package doc comment on every package; small files.

## Git / PR rules (we share one GitHub account — avoid secondary rate limits)

- Branch `lane-<letter>-<slug>` from latest `origin/main`. Conventional commits. End every commit message with
  `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Commit locally as you go; **push once when the lane is done**, then open one PR with `gh pr create`.
- PR body: what/why, how verified (paste the tail of `scripts/verify.sh`), known gaps. End with
  `🤖 Generated with [Claude Code](https://claude.com/claude-code)`.
- Reviewers post **one** review (`gh pr review --comment --body-file ...`) with all findings batched. No per-line comment storms.
- After fixing review findings, post **one** PR comment headed `Review findings → fixes` mapping each finding to its fix (commit sha) or why not, plus final verify/mutation numbers.
- Merge gate: `scripts/pr-ready.sh <n>` (checks green, 0 code-scanning alerts on the PR ref, 0 bot comments, final findings comment). Branch protection on `main` requires verify, mutation and both CodeQL analyses.
- Hooks in `.claude/settings.json` enforce these rules: builders cannot `gh pr merge`, force-push, push to `main`, push tags, cut releases or change repo settings (`scripts/hooks/guard-bash.sh`); `.go` files are gofmt'd after every edit.
- Never merge, force-push to `main`, or rewrite history. The human merges (merge commits only).
- If `main` moved, `git rebase origin/main` on your branch before pushing (your branch only).
