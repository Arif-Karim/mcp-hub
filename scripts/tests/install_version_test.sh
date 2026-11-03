#!/usr/bin/env bash
# install.sh must pick the newest stable mcp-hub (exact vX.Y.Z) release, not whatever GitHub
# calls "latest": the same repo also publishes service-directory-v* releases, and pre-release
# or malformed tags must be skipped in favour of the next valid release.
set -euo pipefail
cd "$(dirname "$0")/../.."
# shellcheck source=/dev/null
MCP_HUB_INSTALL_SOURCE_ONLY=1 source ./install.sh
set +e # sourcing applies install.sh's `set -e`; we check each result explicitly

fails=0
expect() { # expect <name> <want> <json>
  local got
  got="$(printf '%s' "$3" | pick_hub_version)"
  if [[ "$got" == "$2" ]]; then echo "  ok    $1"; else echo "  FAIL  $1: picked '$got', want '$2'"; fails=$((fails + 1)); fi
}

expect "pretty-printed: skips newer service-directory release" "v0.1.1" '[
  {"tag_name": "service-directory-v0.2.0", "draft": false, "prerelease": false},
  {"tag_name": "v0.1.1", "draft": false, "prerelease": false},
  {"tag_name": "v0.1.0", "draft": false, "prerelease": false}
]'
expect "no hub releases: prints nothing" "" '[{"tag_name": "service-directory-v0.1.0"}]'
expect "minified API shape: nested author/assets, body with fake tag_name, rc skipped" "v0.1.0" \
  "$(cat scripts/tests/testdata/releases_minified.json)"
expect "pre-release skipped, falls through to stable" "v1.1.0" '[{"tag_name":"v1.2.0-rc.1"},{"tag_name":"v1.2.0+build.5"},{"tag_name":"v1.1.0"}]'
expect "stable tag picked when it is newest" "v1.2.0" '[{"tag_name":"v1.2.0"},{"tag_name":"v1.2.0-rc.1"}]'
expect "malformed v1.2.3.4 skipped" "v1.2.2" '[{"tag_name":"v1.2.3.4"},{"tag_name":"v1.2.2"}]'
expect "malformed v1.2.3foo / v1.2 / 1.2.3 skipped" "v0.0.1" '[{"tag_name":"v1.2.3foo"},{"tag_name":"v1.2"},{"tag_name":"1.2.3"},{"tag_name":"v0.0.1"}]'

# The source-only switch must not turn a real run into a silent no-op.
out="$(MCP_HUB_INSTALL_SOURCE_ONLY=1 bash ./install.sh --no-open 2>&1)"; rc=$?
if [[ "$rc" -ne 0 && "$out" == *"nothing installed"* ]]; then echo "  ok    executed with MCP_HUB_INSTALL_SOURCE_ONLY set: fails loudly"
else echo "  FAIL  executed with MCP_HUB_INSTALL_SOURCE_ONLY set: rc=$rc out='$out'"; fails=$((fails + 1)); fi
out="$(MCP_HUB_INSTALL_SOURCE_ONLY=1 bash < ./install.sh 2>&1)"; rc=$?
if [[ "$rc" -ne 0 && "$out" == *"nothing installed"* ]]; then echo "  ok    piped (curl | bash) with MCP_HUB_INSTALL_SOURCE_ONLY set: fails loudly"
else echo "  FAIL  piped with MCP_HUB_INSTALL_SOURCE_ONLY set: rc=$rc out='$out'"; fails=$((fails + 1)); fi

[[ "$fails" -eq 0 ]] || { echo "install.sh version pick: $fails case(s) failed"; exit 1; }
echo "install.sh version pick: all cases pass"
