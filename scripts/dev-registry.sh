#!/usr/bin/env bash
# Try MCP servers in the hub before publishing them. Writes dist/dev/catalogue.yaml
# = the built-in catalogue (internal/catalogue/default.yaml) plus your dev entries
# (an entry with an existing id replaces it), validates it, then serves local
# builds as a loopback "registry" on 127.0.0.1:${DEV_REGISTRY_PORT:-7421}.
#
#   scripts/dev-registry.sh
#       builds the reference server (examples/mcp-server-template) as service-directory
#   scripts/dev-registry.sh --src <dir> --id <id> [--version <v>]
#       builds the Go server in <dir> (needs <dir>/cmd/mcp-server, as the template has)
#   scripts/dev-registry.sh --remote <id> <https-url> [--remote <id> <https-url>]...
#       adds remote entries (kind: remote, auth: none, classification: public)
#
# Options combine. Then run the MCP_HUB_CATALOGUE=... mcp-hub serve line it prints.
# The registry only has to run while a server is installed; installs are cached
# under ~/.mcp-hub/servers/<id>/<version>.
#
# Env: DEV_REGISTRY_PORT (7421), DEV_REGISTRY_OUT (dist/dev),
#      DEV_REGISTRY_DRY_RUN=1 writes the catalogue but does not serve.
set -euo pipefail
caller="$PWD" # relative --src / DEV_REGISTRY_OUT resolve against where the user ran us
cd "$(dirname "$0")/.."
root="$PWD"
marker=".dev-registry" # we only ever delete an output dir that contains this

usage() { sed -n '2,19p' "$root/scripts/dev-registry.sh" | sed 's/^# \{0,1\}//' >&2; exit 2; }
fail() { echo "dev-registry: $*" >&2; exit 2; }
id_re='^[a-z0-9][a-z0-9-]{1,62}$'
check_id() { [[ "$1" =~ $id_re ]] || fail "invalid id '$1': must match $id_re"; }

src="" id="" version="0.0.0-dev" remotes=()
if [[ $# -eq 0 ]]; then
  src="$root/examples/mcp-server-template" id="service-directory"
fi
while [[ $# -gt 0 ]]; do
  case "$1" in
    --src) [[ $# -ge 2 ]] || fail "--src needs a directory"; src="$2"; shift 2 ;;
    --id) [[ $# -ge 2 ]] || fail "--id needs a value"; id="$2"; shift 2 ;;
    --version) [[ $# -ge 2 ]] || fail "--version needs a value"; version="$2"; shift 2 ;;
    --remote)
      [[ $# -ge 3 ]] || fail "--remote needs <id> <https-url>"
      check_id "$2"
      [[ "$3" == https://?* ]] || fail "remote url for '$2' must start with https:// (got '$3')"
      remotes+=("-remote" "$2=$3"); shift 3 ;;
    -h|--help) usage ;;
    *) fail "unknown argument '$1' (see --help)" ;;
  esac
done
if [[ -n "$src" ]]; then
  [[ -n "$id" ]] || fail "--src needs --id <id>"
  check_id "$id"
  [[ "$src" == /* ]] || src="$caller/$src"
  [[ -d "$src/cmd/mcp-server" ]] || fail "$src has no cmd/mcp-server: --src must be a Go server built from examples/mcp-server-template"
  src="$(cd "$src" && pwd)"
elif [[ -n "$id" ]]; then
  fail "--id needs --src <dir>"
fi

port="${DEV_REGISTRY_PORT:-7421}"
if [[ -n "${DEV_REGISTRY_OUT:-}" ]]; then
  out="$DEV_REGISTRY_OUT"
  [[ "$out" == /* ]] || out="$caller/$out"
else
  out="$root/dist/dev"
fi
mkdir -p "$(dirname "$out")"
out="$(cd "$(dirname "$out")" && pwd -P)/$(basename "$out")"
if [[ -d "$out" ]]; then out="$(cd "$out" && pwd -P)"; fi # collapses ., .. and symlinks
# Never wipe a directory we didn't create: refuse /, $HOME and the repo root
# outright, and anything non-empty that lacks our marker.
home="$(cd "$HOME" 2>/dev/null && pwd -P || echo "$HOME")"
case "$out" in
  /|"$home"|"$(pwd -P)") fail "refusing to use $out as the output dir" ;;
esac
ours() { # our marker, or a catalogue written by an older version of this script
  [[ -f "$1/$marker" ]] || head -1 "$1/catalogue.yaml" 2>/dev/null | grep -q '^# Dev catalogue written by scripts/dev-registry.sh'
}
if [[ -d "$out" && -n "$(ls -A "$out")" ]] && ! ours "$out"; then
  fail "refusing to replace $out: not empty and not created by dev-registry.sh (no $marker)"
fi
serve=1
if [[ "${DEV_REGISTRY_DRY_RUN:-}" == 1 || -z "$src" ]]; then serve=0; fi
if [[ "$serve" == 1 ]] && ! command -v python3 >/dev/null; then
  fail "python3 is needed to serve the build (install the Xcode Command Line Tools: xcode-select --install)"
fi

# Build into a staging dir next to the output; it replaces the old output only on success.
stage="$(mktemp -d "$out.staging.XXXXXX")"
trap 'rm -rf "$stage"' EXIT
touch "$stage/$marker"

sha256() { if command -v sha256sum >/dev/null; then sha256sum "$1"; else shasum -a 256 "$1"; fi | awk '{print $1}'; }

local_args=()
if [[ -n "$src" ]]; then
  platform="$(go env GOOS)-$(go env GOARCH)"
  echo "building $id from $src for $platform" >&2
  tgz="$stage/$id.tar.gz"
  (cd "$src" && CGO_ENABLED=0 go build -trimpath -buildvcs=false -o "$stage/build/mcp-server" ./cmd/mcp-server)
  # Package like the release pipeline: the template's own archiver writes one
  # `mcp-server` entry only (macOS tar would add AppleDouble `._mcp-server` files).
  (cd "$root/examples/mcp-server-template" && go run ./scripts/mktar "$stage/build/mcp-server" "$tgz")
  rm -rf "$stage/build"
  local_args=(-local-id "$id" -local-version "$version" -platform "$platform"
    -local-url "http://127.0.0.1:${port}/${id}.tar.gz" -local-sha256 "$(sha256 "$tgz")")
fi

# Merges into the built-in catalogue and validates with catalogue.Parse before writing.
go run ./scripts/devcatalogue -out "$stage/catalogue.yaml" ${local_args[@]+"${local_args[@]}"} ${remotes[@]+"${remotes[@]}"}

# Swap in. The old output is missing, empty, or carries our marker (checked above).
rm -rf "$out"
mv "$stage" "$out"
trap - EXIT

cat >&2 <<EOF

Next steps:
  1. If the hub LaunchAgent is running, stop it:
       launchctl bootout gui/\$(id -u)/com.mcp-hub.daemon
  2. Run the hub on the dev catalogue (it replaces the built-in one, but contains a copy of it):
       MCP_HUB_CATALOGUE=$out/catalogue.yaml mcp-hub serve
  3. Enable the server at http://127.0.0.1:7420 or ask Claude Code, then /reload-plugins.
  4. Afterwards, Ctrl-C the hub and restore the normal one:
       launchctl bootstrap gui/\$(id -u) ~/Library/LaunchAgents/com.mcp-hub.daemon.plist
     Dev-only entries are then auto-disabled; restart Claude Code to drop their hub__* servers.
EOF

if [[ "$serve" == 0 ]]; then
  echo "not serving (dry run, or no local build); run the hub as above" >&2
  exit 0
fi
echo "serving $out on http://127.0.0.1:${port} — Ctrl-C to stop" >&2
exec python3 -m http.server "$port" --bind 127.0.0.1 --directory "$out"
