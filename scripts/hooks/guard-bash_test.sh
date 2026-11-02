#!/usr/bin/env bash
# Table test for guard-bash.sh: feeds Claude Code PreToolUse JSON and checks
# the exit code (2 = blocked, 0 = allowed).
set -euo pipefail
cd "$(dirname "$0")"

fail=0
check() {
  local want="$1" cmd="$2" got=0
  jq -n --arg c "$cmd" '{tool_name:"Bash",tool_input:{command:$c}}' | ./guard-bash.sh >/dev/null 2>&1 || got=$?
  if [[ "$got" != "$want" ]]; then
    echo "FAIL: want exit $want, got $got: $cmd"; fail=1
  fi
}

# Blocked: only the human (or the orchestrator, explicitly) merges and touches main.
check 2 'gh pr merge 12 --merge'
check 2 'cd x && gh pr merge 3'
check 2 'git push --force origin lane-a'
check 2 'git push -f origin lane-a'
check 2 'git push origin main'
check 2 'git push origin HEAD:main'
check 2 'git push --delete origin main'
check 2 'gh api -X PUT repos/o/r/branches/main/protection'
check 2 'gh release create v1.0.0'
check 2 'git tag v1.0.0 && git push origin v1.0.0'
# Allowed: normal lane work.
check 0 'git push -u origin lane-a-web-ui'
check 0 'git push --force-with-lease origin lane-a-web-ui'
check 0 'gh pr create --base main --title x'
check 0 'gh pr view 12 --json state'
check 0 'go test ./...'
check 0 'grep -n "gh pr merge" docs/x.md'
# Orchestrator override is explicit and visible in the transcript.
check 0 'MCP_HUB_ORCHESTRATOR=1 gh pr merge 12 --merge'

[[ "$fail" == 0 ]] && echo "guard-bash: all cases pass"
exit "$fail"
