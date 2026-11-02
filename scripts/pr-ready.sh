#!/usr/bin/env bash
# Is PR <n> ready to merge? One command for the orchestrator's merge gate (see CLAUDE.md):
#   - every check on the latest head passed
#   - 0 open code-scanning alerts on the PR (CodeQL files PR results under refs/pull/<n>/merge)
#   - 0 unresolved review threads started by bots
#   - if the PR was reviewed, a "Review findings → fixes" comment was posted after the last review
# Any gh/API failure prints NOT READY rather than passing silently.
# Usage: scripts/pr-ready.sh <pr-number>
set -euo pipefail
n="${1:?usage: scripts/pr-ready.sh <pr-number>}"
trap 'echo "  FAIL  a gh/API call failed (line $LINENO)"; echo "NOT READY"; exit 1' ERR

repo="$(gh repo view --json nameWithOwner -q .nameWithOwner)"
owner="${repo%/*}" name="${repo#*/}"
ok=1
pass() { printf '  ok    %s\n' "$*"; }
miss() { printf '  FAIL  %s\n' "$*"; ok=0; }

echo "PR #$n"
if gh pr checks "$n" >/dev/null 2>&1; then pass "all checks passed"; else miss "checks not all passing (gh pr checks $n)"; fi

alerts=0
for ref in "refs/pull/$n/merge" "refs/pull/$n/head"; do
  c="$(gh api --paginate "repos/$repo/code-scanning/alerts?ref=$ref&state=open&per_page=100" --jq length | awk '{s+=$1} END {print s+0}')"
  alerts=$((alerts + c))
done
[[ "$alerts" == 0 ]] && pass "0 open code-scanning alerts" || miss "$alerts open code-scanning alerts"

# shellcheck disable=SC2016 # GraphQL variables, not shell
bots="$(gh api graphql -F owner="$owner" -F name="$name" -F n="$n" -f query='
  query($owner:String!,$name:String!,$n:Int!){repository(owner:$owner,name:$name){pullRequest(number:$n){
    reviewThreads(first:100){nodes{isResolved comments(first:1){nodes{author{__typename}}}}}}}}' \
  --jq '[.data.repository.pullRequest.reviewThreads.nodes[]
         | select(.isResolved | not) | select(.comments.nodes[0].author.__typename == "Bot")] | length')"
[[ "$bots" == 0 ]] && pass "0 unresolved bot review threads" || miss "$bots unresolved bot review threads"

last_review="$(gh pr view "$n" --json reviews --jq '[.reviews[].submittedAt] | max // ""')"
if [[ -z "$last_review" ]]; then
  miss "no review posted"
else
  final="$(gh pr view "$n" --json comments | jq --arg t "$last_review" \
    '[.comments[] | select(.createdAt > $t and (.body | test("^## Review (findings → fixes|follow-up)")))] | length')"
  [[ "$final" -gt 0 ]] && pass "final findings → fixes comment after last review" \
    || miss "no '## Review findings → fixes' comment after the last review"
fi

[[ "$ok" == 1 ]] && echo "READY" || { echo "NOT READY"; exit 1; }
