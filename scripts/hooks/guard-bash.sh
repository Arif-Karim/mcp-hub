#!/usr/bin/env bash
# Claude Code PreToolUse hook (Bash): stops builder agents doing things only the
# human or the orchestrator may do — merging PRs, pushing to or force-pushing
# branches, changing repo settings, cutting releases. CLAUDE.md states these
# rules; this enforces them. Branch protection on main is the server-side backstop.
#
# The orchestrator opts in per command with a visible MCP_HUB_ORCHESTRATOR=1 prefix.
# Exit 2 blocks the call and shows the reason to the agent.
set -euo pipefail

cmd="$(jq -r '.tool_input.command // ""')"
[[ "$cmd" == MCP_HUB_ORCHESTRATOR=1\ * ]] && exit 0

# Ignore quoted text so `grep "gh pr merge"` or commit messages don't trip it.
bare="$(printf '%s' "$cmd" | sed -E "s/\"[^\"]*\"//g; s/'[^']*'//g")"

block() { echo "Blocked by mcp-hub harness: $1. Ask the orchestrator." >&2; exit 2; }

[[ "$bare" =~ gh[[:space:]]+pr[[:space:]]+merge ]] && block "only the human or orchestrator merges PRs"
[[ "$bare" =~ git[[:space:]]+push.*(--force([[:space:]]|$)|[[:space:]]-f([[:space:]]|$)) ]] && block "force-push (use --force-with-lease on your own branch)"
[[ "$bare" =~ git[[:space:]]+push.*([[:space:]:]main([[:space:]]|$)) ]] && block "pushing to main"
[[ "$bare" =~ git[[:space:]]+push.*[[:space:]]v[0-9] ]] && block "pushing release tags"
[[ "$bare" =~ gh[[:space:]]+release[[:space:]]+create ]] && block "cutting releases"
[[ "$bare" =~ gh[[:space:]]+api.*-X[[:space:]]*(PUT|PATCH|DELETE).*(protection|settings|rulesets) ]] && block "changing repo settings"
exit 0
