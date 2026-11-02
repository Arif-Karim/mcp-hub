#!/usr/bin/env bash
# Is PR <n> ready to merge? One command for the merge gate the orchestrator
# applies (see CLAUDE.md):
#   - every check on the latest head passed
#   - 0 open code-scanning alerts on refs/pull/<n>/head
#   - 0 inline comments from bots
#   - if the PR was reviewed, a final "Review findings → fixes" comment was posted after the last review
# Usage: scripts/pr-ready.sh <pr-number>
set -euo pipefail
n="${1:?usage: scripts/pr-ready.sh <pr-number>}"
repo="$(gh repo view --json nameWithOwner -q .nameWithOwner)"
ok=1
pass() { printf '  ok    %s\n' "$*"; }
miss() { printf '  FAIL  %s\n' "$*"; ok=0; }

echo "PR #$n"
if gh pr checks "$n" >/dev/null 2>&1; then pass "all checks passed"; else miss "checks not all passing (gh pr checks $n)"; fi

alerts="$(gh api "repos/$repo/code-scanning/alerts?ref=refs/pull/$n/head&state=open" --jq length 2>/dev/null || echo "?")"
[[ "$alerts" == 0 ]] && pass "0 open code-scanning alerts" || miss "$alerts open code-scanning alerts"

bots="$(gh api "repos/$repo/pulls/$n/comments" --jq '[.[] | select(.user.type == "Bot")] | length')"
[[ "$bots" == 0 ]] && pass "0 bot inline comments" || miss "$bots bot inline comments"

last_review="$(gh pr view "$n" --json reviews --jq '[.reviews[].submittedAt] | max // ""')"
if [[ -z "$last_review" ]]; then
  miss "no review posted"
else
  final="$(gh pr view "$n" --json comments | jq --arg t "$last_review" \
    '[.comments[] | select(.createdAt > $t and (.body | contains("Review findings → fixes")))] | length')"
  [[ "$final" -gt 0 ]] && pass "final findings → fixes comment after last review" \
    || miss "no 'Review findings → fixes' comment after the last review"
fi

[[ "$ok" == 1 ]] && echo "READY" || { echo "NOT READY"; exit 1; }
