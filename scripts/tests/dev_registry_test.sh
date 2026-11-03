#!/usr/bin/env bash
# Tests scripts/dev-registry.sh in dry-run mode (writes the catalogue, never serves).
set -euo pipefail
cd "$(dirname "$0")/../.."

root="$PWD"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
export DEV_REGISTRY_DRY_RUN=1 DEV_REGISTRY_OUT="$tmp/out" DEV_REGISTRY_PORT=7499
fail=0
die() { echo "FAIL: $*"; fail=1; }
sha256() { if command -v sha256sum >/dev/null; then sha256sum "$1"; else shasum -a 256 "$1"; fi | awk '{print $1}'; }

# A team's server lives outside the repo, copied from the template.
mkdir -p "$tmp/leave-balance"
(cd examples/mcp-server-template && tar -cf - --exclude dist .) | tar -xf - -C "$tmp/leave-balance"

# 1. --src with a custom id: tarball has only mcp-server, catalogue = default ids + new one.
if scripts/dev-registry.sh --src "$tmp/leave-balance" --id leave-balance --remote cf-docs https://docs.example.com/mcp >"$tmp/log" 2>&1; then
  tgz="$tmp/out/leave-balance.tar.gz"
  listing="$(tar -tzf "$tgz")"
  [[ "$listing" == "mcp-server" ]] || die "tarball contents: $listing"
  cat="$tmp/out/catalogue.yaml"
  grep -q "sha256: \"\?$(sha256 "$tgz")" "$cat" || die "catalogue lacks the tarball sha256"
  grep -q "url: http://127.0.0.1:7499/leave-balance.tar.gz" "$cat" || die "catalogue lacks artifact url"
  grep -q "version: 0.0.0-dev" "$cat" || die "default version not 0.0.0-dev"
  for id in $(awk '/^  - id:/ {print $3}' internal/catalogue/default.yaml) leave-balance cf-docs; do
    [[ "$(grep -c "^  *- id: $id\$" "$cat")" == 1 ]] || die "catalogue should list $id exactly once"
  done
  grep -q "url: https://docs.example.com/mcp" "$cat" || die "remote url missing"
  grep -q "MCP_HUB_CATALOGUE=" "$tmp/log" || die "next steps not printed"
  grep -q "launchctl bootout gui/" "$tmp/log" || die "launchctl hint not printed"
else
  cat "$tmp/log"; die "--src build failed"
fi

# 2. Rejections: exit non-zero, clear message, nothing written.
reject() {
  local want="$1"; shift
  rm -rf "$tmp/out"
  if scripts/dev-registry.sh "$@" >"$tmp/log" 2>&1; then
    die "accepted: $*"
  elif ! grep -q -e "$want" "$tmp/log"; then
    die "message for '$*' lacks '$want': $(cat "$tmp/log")"
  fi
  [[ ! -e "$tmp/out/catalogue.yaml" ]] || die "catalogue written for: $*"
}
reject "https://" --remote cf-docs http://docs.example.com/mcp
reject "invalid id" --remote Bad_ID https://docs.example.com/mcp
reject "invalid id" --src "$tmp/leave-balance" --id Bad_ID
reject "cmd/mcp-server" --src "$tmp" --id leave-balance
reject "--id" --src "$tmp/leave-balance"
reject "unknown argument" --bogus

cd "$root"
[[ "$fail" == 0 ]] && echo "dev_registry_test: ok"
exit "$fail"
