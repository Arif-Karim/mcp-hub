#!/usr/bin/env bash
# Builds release tarballs + checksums.txt into dist/. CI (release.yml, ci.yml
# installer job) and humans run the same script.
#
# Usage: VERSION=v0.1.0 scripts/package-release.sh [os/arch ...]
#   default target: this machine (e.g. darwin/arm64)
#   SKIP_WEB=1  reuse an already-built UI in internal/webui/dist
#
# Each archive mcp-hub_<os>_<arch>.tar.gz contains:
#   mcp-hub                      the binary (UI embedded)
#   skill/mcp-hub/SKILL.md       the Claude Code skill install.sh copies to ~/.claude/skills/
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION="${VERSION:-dev}"
if [[ $# -eq 0 ]]; then
  set -- "$(go env GOOS)/$(go env GOARCH)"
fi
LDFLAGS="-s -w -X github.com/Arif-Karim/mcp-hub/internal/version.Version=${VERSION}"

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$@"; else shasum -a 256 "$@"; fi
}

if [[ "${SKIP_WEB:-}" != "1" ]]; then
  echo "==> building web UI"
  make web
fi

rm -rf dist
mkdir -p dist
stage="$(mktemp -d)"
trap 'rm -rf "$stage"' EXIT

for target in "$@"; do
  os="${target%/*}"
  arch="${target#*/}"
  [[ "$os" =~ ^(darwin|linux)$ && "$arch" =~ ^(arm64|amd64)$ ]] || { echo "unsupported target: $target" >&2; exit 1; }
  name="mcp-hub_${os}_${arch}"
  echo "==> building ${name} (${VERSION})"
  dir="${stage}/${name}"
  mkdir -p "${dir}/skill/mcp-hub"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags "$LDFLAGS" -o "${dir}/mcp-hub" ./cmd/mcp-hub
  cp skill/mcp-hub/SKILL.md "${dir}/skill/mcp-hub/SKILL.md"
  tar -czf "dist/${name}.tar.gz" -C "$dir" mcp-hub skill
done

(cd dist && sha256 ./*.tar.gz | sed 's# \./# #; s#\*\./#*#' > checksums.txt)
echo "==> dist/"
cat dist/checksums.txt
