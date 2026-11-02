#!/usr/bin/env bash
# Builds release archives for the service-directory MCP server.
#
#   scripts/package.sh <version>
#
# Produces, under dist/:
#   service-directory_<os>_<arch>.tar.gz   one executable `mcp-server` at the root (0755)
#   checksums.txt                          sha256 of every archive
# and prints the catalogue `artifacts:` snippet to paste into the catalogue PR.
# Builds are reproducible: pinned toolchain (go.mod), CGO off, -trimpath, no build id,
# fixed archive mtime.
set -euo pipefail
cd "$(dirname "$0")/.."

version="${1:-}"
if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.-]+)?$ ]]; then
  echo "usage: scripts/package.sh <semver, e.g. 0.1.0>" >&2
  exit 2
fi

name="service-directory"
tag="${name}-v${version}"
# Teams copying this template: GITHUB_REPOSITORY is set in Actions; set it locally too.
repo="${GITHUB_REPOSITORY:-Arif-Karim/mcp-hub}"
base_url="https://github.com/${repo}/releases/download/${tag}"
platforms=(darwin/arm64 darwin/amd64 linux/amd64)
export SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:-0}"

rm -rf dist
mkdir -p dist/build

sha256() { if command -v sha256sum >/dev/null; then sha256sum "$1"; else shasum -a 256 "$1"; fi | awk '{print $1}'; }

for p in "${platforms[@]}"; do
  os="${p%/*}"; arch="${p#*/}"
  bin="dist/build/${os}_${arch}/mcp-server"
  echo "building ${os}/${arch}" >&2
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -buildvcs=false \
    -ldflags="-s -w -buildid=" -o "$bin" ./cmd/mcp-server
  go run ./scripts/mktar "$bin" "dist/${name}_${os}_${arch}.tar.gz"
done
rm -rf dist/build

(cd dist && for f in *.tar.gz; do printf '%s  %s\n' "$(sha256 "$f")" "$f"; done | LC_ALL=C sort -k2 > checksums.txt)

echo "# catalogue entry '${name}' version: ${version}"
echo "artifacts:"
for p in "${platforms[@]}"; do
  os="${p%/*}"; arch="${p#*/}"
  file="${name}_${os}_${arch}.tar.gz"
  echo "  ${os}-${arch}:"
  echo "    url: ${base_url}/${file}"
  echo "    sha256: \"$(sha256 "dist/${file}")\""
done
