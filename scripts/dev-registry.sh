#!/usr/bin/env bash
# Try the LOCAL server flow without a release: builds the example MCP server
# (examples/mcp-server-template, or from origin/lane-e-example-server if it is
# not merged yet), packages it like the release pipeline does (tar.gz with one
# executable `mcp-server`), writes a dev catalogue pointing at it with the real
# sha256, then serves it as a loopback "registry" on 127.0.0.1:${DEV_REGISTRY_PORT:-7421}.
#
#   terminal 1:  scripts/dev-registry.sh
#   terminal 2:  MCP_HUB_CATALOGUE=dist/dev/catalogue.yaml ./bin/mcp-hub serve
#
# The registry only has to run while a server is installed; installs are
# cached under ~/.mcp-hub/servers/<id>/<version>.
set -euo pipefail
cd "$(dirname "$0")/.."

port="${DEV_REGISTRY_PORT:-7421}"
out="dist/dev"
src="examples/mcp-server-template"
rm -rf "$out"
mkdir -p "$out/build"

if [[ ! -f "$src/go.mod" ]]; then
  echo "examples/ not on this branch; using origin/lane-e-example-server" >&2
  git fetch -q origin lane-e-example-server || true
  tmp="$out/build/src"
  mkdir -p "$tmp"
  git archive origin/lane-e-example-server "$src" | tar -x -C "$tmp"
  src="$tmp/$src"
fi

sha256() { if command -v sha256sum >/dev/null; then sha256sum "$1"; else shasum -a 256 "$1"; fi | awk '{print $1}'; }

platform="$(go env GOOS)-$(go env GOARCH)"
echo "building service-directory for $platform" >&2
root="$PWD"
# Package with the example's own archiver (same as the release pipeline): one
# `mcp-server` entry only — macOS tar would add AppleDouble `._mcp-server` files.
(cd "$src" && CGO_ENABLED=0 go build -trimpath -o "$root/$out/build/mcp-server" ./cmd/mcp-server \
  && go run ./scripts/mktar "$root/$out/build/mcp-server" "$root/$out/service-directory.tar.gz")
rm -rf "$out/build"
sum="$(sha256 "$out/service-directory.tar.gz")"

cat > "$out/catalogue.yaml" <<EOF
# Dev catalogue written by scripts/dev-registry.sh — not for production.
version: 1
entries:
  - id: service-directory
    name: Service Directory
    description: Look up internal services, their owning team, on-call channel and runbook. Built from examples/mcp-server-template (dev build).
    owner: platform-engineering
    version: 0.1.0-dev
    kind: local
    classification: internal
    approval: approved
    auth: hub-jwt
    tags: [platform, ownership, runbooks]
    tools:
      - name: find_service
        description: Search services by name or tag.
      - name: get_runbook
        description: Return the runbook for a service.
    artifacts:
      ${platform}:
        url: http://127.0.0.1:${port}/service-directory.tar.gz
        sha256: "${sum}"

  - id: deepwiki
    name: Engineering Docs Q&A (DeepWiki)
    description: >-
      Ask questions about repositories' documentation. In production this would be a company-hosted
      DeepWiki over internal repos; this demo points at the public instance (no credentials sent).
    owner: external (Cognition)
    version: "1"
    kind: remote
    classification: public
    approval: approved
    auth: none
    tags: [docs, github, external]
    tools:
      - name: read_wiki_structure
        description: List documentation topics for a repository.
      - name: ask_question
        description: Ask a question about a repository.
    url: https://mcp.deepwiki.com/mcp

  - id: payments-sandbox
    name: Payments Sandbox (legacy)
    description: Revoked after it was found returning unmasked account numbers. Kept listed so users know why it disappeared.
    owner: payments-platform
    version: 0.3.2
    kind: local
    classification: confidential
    approval: revoked
    auth: hub-jwt
    tags: [payments, legacy]
    tools:
      - name: create_test_payment
        description: Create a sandbox payment.
    artifacts: {}
EOF

echo "wrote $out/catalogue.yaml (sha256 $sum)" >&2
echo "serving $out on http://127.0.0.1:${port} — Ctrl-C to stop" >&2
echo "now run:  MCP_HUB_CATALOGUE=$out/catalogue.yaml ./bin/mcp-hub serve" >&2
exec python3 -m http.server "$port" --bind 127.0.0.1 --directory "$out"
