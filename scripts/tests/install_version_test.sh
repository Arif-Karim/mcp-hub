#!/usr/bin/env bash
# install.sh must pick the newest mcp-hub (vX.Y.Z) release, not whatever GitHub
# calls "latest": the same repo also publishes service-directory-v* releases.
set -euo pipefail
cd "$(dirname "$0")/../.."
# shellcheck source=/dev/null
MCP_HUB_INSTALL_SOURCE_ONLY=1 source ./install.sh

fixture='[
  {"tag_name": "service-directory-v0.2.0", "draft": false, "prerelease": false},
  {"tag_name": "v0.1.1", "draft": false, "prerelease": false},
  {"tag_name": "v0.1.0", "draft": false, "prerelease": false}
]'
got="$(printf '%s' "$fixture" | pick_hub_version)"
[[ "$got" == "v0.1.1" ]] || { echo "FAIL: picked '$got', want v0.1.1"; exit 1; }

got="$(printf '%s' '[{"tag_name": "service-directory-v0.1.0"}]' | pick_hub_version)"
[[ -z "$got" ]] || { echo "FAIL: picked '$got' from a list with no hub releases"; exit 1; }
echo "install.sh version pick: all cases pass"
