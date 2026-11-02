#!/usr/bin/env bash
# mcp-hub installer (macOS). Installs the mcp-hub binary, the Claude Code skill
# (embedded in the binary; written by `mcp-hub install-skill`) and a per-user
# daemon, then opens the marketplace UI.
#
#   curl -fsSL https://raw.githubusercontent.com/Arif-Karim/mcp-hub/main/install.sh | bash
#   ./install.sh [options]
#
# Options:
#   --version vX.Y.Z        install this release instead of the latest (pin / roll back)
#   --local-artifact <dir>  use <dir>/mcp-hub_<os>_<arch>.tar.gz + <dir>/checksums.txt instead of downloading
#   --yes                   answer yes to prompts (installing Claude Code)
#   --skip-claude           do not check for / install Claude Code
#   --no-launchd            start the daemon with nohup instead of a LaunchAgent (CI)
#   --no-open               do not open the UI in a browser
#   --uninstall             stop the daemon, remove hub-owned Claude config entries, binary and skill
#   --purge                 with --uninstall: also delete ~/.mcp-hub (state, logs, audit log)
#
# Nothing here needs sudo. Every download is sha256-verified before anything is installed.
set -euo pipefail

REPO="${MCP_HUB_REPO:-Arif-Karim/mcp-hub}"
ADDR="${MCP_HUB_ADDR:-127.0.0.1:7420}"
BIN_DIR="${HOME}/.local/bin"
BIN="${BIN_DIR}/mcp-hub"
CLAUDE_DIR="${HOME}/.claude"
SKILL_DIR="${CLAUDE_DIR}/skills/mcp-hub"
HUB_HOME="${MCP_HUB_HOME:-${HOME}/.mcp-hub}"
LOG_DIR="${HUB_HOME}/logs"
PID_FILE="${HUB_HOME}/daemon.pid"
LABEL="com.mcp-hub.daemon"
PLIST="${HOME}/Library/LaunchAgents/${LABEL}.plist"

VERSION=""
LOCAL_ARTIFACT=""
YES=0
SKIP_CLAUDE=0
NO_LAUNCHD=0
NO_OPEN=0
UNINSTALL=0
PURGE=0
TMP=""

log()  { printf '\033[1m[mcp-hub]\033[0m %s\n' "$*"; }
step() { printf '\n\033[1;34m==> %s\033[0m\n' "$*"; }
warn() { printf '\033[1;33m[mcp-hub] warning:\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31m[mcp-hub] error:\033[0m %s\n' "$*" >&2; exit 1; }

usage() { sed -n '2,19p' "$0" 2>/dev/null | sed 's/^# \{0,1\}//' || true; }

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --version)        [[ $# -ge 2 ]] || die "--version needs a value"; VERSION="$2"; shift 2 ;;
      --local-artifact) [[ $# -ge 2 ]] || die "--local-artifact needs a directory"; LOCAL_ARTIFACT="$2"; shift 2 ;;
      --yes|-y)         YES=1; shift ;;
      --skip-claude)    SKIP_CLAUDE=1; shift ;;
      --no-launchd)     NO_LAUNCHD=1; shift ;;
      --no-open)        NO_OPEN=1; shift ;;
      --uninstall)      UNINSTALL=1; shift ;;
      --purge)          PURGE=1; shift ;;
      -h|--help)        usage; exit 0 ;;
      *)                die "unknown option: $1 (see --help)" ;;
    esac
  done
}

detect_platform() {
  OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "$OS" in
    darwin) ;;
    linux) [[ "${MCP_HUB_ALLOW_LINUX:-}" == "1" ]] || die "mcp-hub's installer supports macOS only (set MCP_HUB_ALLOW_LINUX=1 to try Linux)" ;;
    *) die "unsupported OS: $OS (macOS only)" ;;
  esac
  case "$(uname -m)" in
    arm64|aarch64) ARCH=arm64 ;;
    x86_64|amd64)  ARCH=amd64 ;;
    *) die "unsupported CPU architecture: $(uname -m)" ;;
  esac
  ASSET="mcp-hub_${OS}_${ARCH}.tar.gz"
  log "platform: ${OS}/${ARCH}"
}

use_launchd() { [[ "$OS" == "darwin" && "$NO_LAUNCHD" == "0" ]]; }

sha256_of() {
  if command -v shasum >/dev/null 2>&1; then shasum -a 256 "$1" | awk '{print $1}'
  else sha256sum "$1" | awk '{print $1}'; fi
}

resolve_version() {
  if [[ -n "$VERSION" ]]; then
    :
  else
    log "looking up the latest release of ${REPO}"
    VERSION="$(curl -fsSL --max-time 20 "https://api.github.com/repos/${REPO}/releases/latest" \
      | sed -n 's/^ *"tag_name": *"\(v[^"]*\)".*/\1/p' | head -n 1)" \
      || die "could not query GitHub releases for ${REPO}"
    [[ -n "$VERSION" ]] || die "no v* release found for ${REPO}; use --local-artifact <dir> to install a local build"
  fi
  [[ "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.-]+)?$ ]] || die "invalid version: $VERSION (expected vX.Y.Z)"
  log "version: ${VERSION}"
}

fetch_artifact() {
  step "[1/6] Download and verify ${ASSET}"
  if [[ -n "$LOCAL_ARTIFACT" ]]; then
    [[ -f "${LOCAL_ARTIFACT}/${ASSET}" && -f "${LOCAL_ARTIFACT}/checksums.txt" ]] \
      || die "${LOCAL_ARTIFACT} must contain ${ASSET} and checksums.txt (build them with scripts/package-release.sh)"
    cp "${LOCAL_ARTIFACT}/${ASSET}" "${LOCAL_ARTIFACT}/checksums.txt" "$TMP/"
    log "using local artifact from ${LOCAL_ARTIFACT}"
  else
    resolve_version
    local base="https://github.com/${REPO}/releases/download/${VERSION}"
    curl -fsSL --max-time 300 -o "$TMP/${ASSET}" "${base}/${ASSET}" || die "download failed: ${base}/${ASSET}"
    curl -fsSL --max-time 60 -o "$TMP/checksums.txt" "${base}/checksums.txt" || die "download failed: ${base}/checksums.txt"
  fi

  local want got
  want="$(awk -v f="$ASSET" '$2 == f || $2 == "*" f { print $1 }' "$TMP/checksums.txt" | head -n 1)"
  [[ -n "$want" ]] || die "checksums.txt has no entry for ${ASSET}; refusing to install"
  got="$(sha256_of "$TMP/${ASSET}")"
  if [[ "$got" != "$want" ]]; then
    die "sha256 mismatch for ${ASSET} (expected ${want}, got ${got}). Aborting: nothing was installed."
  fi
  log "sha256 verified: ${got}"

  # Reject absolute paths and '..' before unpacking (defence in depth; the archive is already verified).
  if tar -tzf "$TMP/${ASSET}" | grep -Eq '(^/|(^|/)\.\.(/|$))'; then
    die "archive contains unsafe paths; aborting"
  fi
  mkdir -p "$TMP/x"
  tar -xzf "$TMP/${ASSET}" -C "$TMP/x"
  [[ -f "$TMP/x/mcp-hub" ]] || die "archive is missing the mcp-hub binary"
}

install_binary() {
  step "[2/6] Install binary to ${BIN}"
  mkdir -p "$BIN_DIR"
  # Write next to the target then rename, so a running daemon's binary is replaced atomically.
  install -m 0755 "$TMP/x/mcp-hub" "${BIN}.new"
  mv -f "${BIN}.new" "$BIN"
  log "installed $("$BIN" version)"
  case ":${PATH}:" in
    *":${BIN_DIR}:"*) ;;
    *) warn "${BIN_DIR} is not on your PATH. Add this line to ~/.zshrc (then open a new terminal):"
       printf '\n    export PATH="$HOME/.local/bin:$PATH"\n\n' >&2 ;;
  esac
}

install_skill() {
  step "[3/6] Install Claude Code skill"
  # The skill ships inside the binary, so the installed skill always matches it.
  CLAUDE_HOME_DIR="$CLAUDE_DIR" "$BIN" install-skill || die "could not install the Claude Code skill"
}

confirm() {
  [[ "$YES" == "1" ]] && return 0
  local reply=""
  # Works under `curl | bash`: stdin is the script, so ask the terminal directly.
  if [[ -r /dev/tty ]] && { exec 3</dev/tty; } 2>/dev/null; then
    printf '%s [y/N] ' "$1" >/dev/tty
    read -r reply <&3 || true
    exec 3<&-
  fi
  [[ "$reply" =~ ^[Yy]([Ee][Ss])?$ ]]
}

ensure_claude() {
  step "[4/6] Claude Code"
  if [[ "$SKIP_CLAUDE" == "1" ]]; then
    log "skipped (--skip-claude)"
  elif command -v claude >/dev/null 2>&1; then
    log "found $(command -v claude)"
  elif confirm "Claude Code is not installed. Install it now with the official installer (https://claude.ai/install.sh)?"; then
    curl -fsSL https://claude.ai/install.sh | bash || die "Claude Code install failed"
    log "Claude Code installed. Run \`claude\` and log in with your own account."
  else
    warn "Claude Code not installed. Install it later: curl -fsSL https://claude.ai/install.sh | bash"
  fi
}

xml_escape() {
  local s="$1"
  s="${s//&/&amp;}"; s="${s//</&lt;}"; s="${s//>/&gt;}"
  printf '%s' "$s"
}

agent_loaded() { launchctl print "gui/$(id -u)/${LABEL}" >/dev/null 2>&1; }

stop_daemon() {
  if [[ "$OS" == "darwin" ]] && agent_loaded; then
    launchctl bootout "gui/$(id -u)/${LABEL}" 2>/dev/null || true
    # bootout returns before launchd has fully removed the service; bootstrapping
    # too early fails with "Bootstrap failed: 5". Wait for it to go (bounded: 5s).
    local _
    for _ in $(seq 1 10); do agent_loaded || break; sleep 0.5; done
    log "stopped LaunchAgent ${LABEL}"
  fi
  if [[ -f "$PID_FILE" ]]; then
    local pid
    pid="$(cat "$PID_FILE" 2>/dev/null || true)"
    # Only signal it if it is really our daemon (pid files go stale).
    if [[ "$pid" =~ ^[0-9]+$ ]] && ps -p "$pid" -o command= 2>/dev/null | grep -q 'mcp-hub serve'; then
      kill "$pid" 2>/dev/null || true
      # Let it release the port before a new one starts (bounded: 5s).
      local _
      for _ in $(seq 1 10); do kill -0 "$pid" 2>/dev/null || break; sleep 0.5; done
      log "stopped daemon (pid ${pid})"
    fi
    rm -f "$PID_FILE"
  fi
}

start_daemon() {
  step "[5/6] Start the mcp-hub daemon on ${ADDR}"
  mkdir -p "$LOG_DIR"
  chmod 700 "$HUB_HOME"
  stop_daemon
  if use_launchd; then
    mkdir -p "$(dirname "$PLIST")"
    local extra_env=""
    if [[ -n "${CLAUDE_CONFIG_PATH:-}" ]]; then
      extra_env="<key>CLAUDE_CONFIG_PATH</key><string>$(xml_escape "$CLAUDE_CONFIG_PATH")</string>"
    fi
    cat > "$PLIST" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>${LABEL}</string>
  <key>ProgramArguments</key>
  <array><string>$(xml_escape "$BIN")</string><string>serve</string></array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>MCP_HUB_HOME</key><string>$(xml_escape "$HUB_HOME")</string>
    <key>MCP_HUB_ADDR</key><string>$(xml_escape "$ADDR")</string>
    ${extra_env}
  </dict>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>$(xml_escape "$LOG_DIR/daemon.log")</string>
  <key>StandardErrorPath</key><string>$(xml_escape "$LOG_DIR/daemon.log")</string>
</dict>
</plist>
EOF
    chmod 644 "$PLIST"
    if ! launchctl bootstrap "gui/$(id -u)" "$PLIST"; then
      # Error 5 (I/O error) usually means the previous instance was still being torn down: retry once.
      warn "launchctl bootstrap failed; retrying once"
      sleep 2
      launchctl bootstrap "gui/$(id -u)" "$PLIST" || die "launchctl bootstrap failed; see ${LOG_DIR}/daemon.log"
    fi
    # launchd may defer a RunAtLoad spawn; start it now.
    launchctl kickstart "gui/$(id -u)/${LABEL}" || warn "launchctl kickstart failed; the daemon will start at next login"
    log "LaunchAgent ${LABEL} loaded (starts at login, restarts if it exits)"
  else
    # A plist left by an earlier launchd install would start a second daemon at next login.
    if [[ -f "$PLIST" ]]; then rm -f "$PLIST"; log "removed old ${PLIST} (--no-launchd)"; fi
    MCP_HUB_HOME="$HUB_HOME" MCP_HUB_ADDR="$ADDR" nohup "$BIN" serve >>"$LOG_DIR/daemon.log" 2>&1 &
    echo $! > "$PID_FILE"
    log "daemon started in the background (pid $!, --no-launchd)"
  fi
}

wait_healthy() {
  step "[6/6] Wait for the daemon"
  local url="http://${ADDR}/api/v1/health" body="" i
  for i in $(seq 1 30); do
    if body="$(curl -fsS --max-time 2 "$url" 2>/dev/null)"; then
      log "healthy: ${body}"
      if [[ "$body" != *"\"$("$BIN" version)\""* ]]; then
        warn "the daemon on ${ADDR} reports a different version than ${BIN}; another mcp-hub may be holding the port"
      fi
      return 0
    fi
    sleep 0.5
  done
  die "daemon did not become healthy within 15s (attempts: ${i}); see ${LOG_DIR}/daemon.log"
}

next_steps() {
  local ui="http://${ADDR}"
  if [[ "$NO_OPEN" == "0" ]] && command -v open >/dev/null 2>&1; then
    open "$ui" || true
  fi
  cat <<EOF

mcp-hub is installed and running.
  Marketplace UI:  ${ui}
  CLI:             mcp-hub search | info <id> | enable <id> | status
  Logs:            ${LOG_DIR}/daemon.log

Next: start Claude Code (\`claude\`) and try:
  find me an MCP server for service ownership

Uninstall: ./install.sh --uninstall   (or: curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | bash -s -- --uninstall)
EOF
}

uninstall() {
  step "Uninstall mcp-hub"
  stop_daemon
  if [[ -f "$PLIST" ]]; then rm -f "$PLIST"; log "removed ${PLIST}"; fi
  if [[ -x "$BIN" ]]; then
    MCP_HUB_HOME="$HUB_HOME" "$BIN" uninstall-config || warn "could not clean Claude Code config; hub__* entries may remain in ~/.claude.json"
    rm -f "$BIN"
    log "removed ${BIN}"
  fi
  if [[ -d "$SKILL_DIR" ]]; then rm -rf "$SKILL_DIR"; log "removed ${SKILL_DIR}"; fi
  if [[ "$PURGE" == "1" ]]; then
    case "$HUB_HOME" in
      ""|"/"|"$HOME"|"$HOME/") die "refusing to purge ${HUB_HOME}" ;;
    esac
    rm -rf "$HUB_HOME"
    log "purged ${HUB_HOME}"
  else
    log "kept ${HUB_HOME} (state, logs, audit log); use --purge to delete it"
  fi
  log "uninstalled"
}

main() {
  parse_args "$@"
  detect_platform
  if [[ "$UNINSTALL" == "1" ]]; then
    uninstall
    return 0
  fi
  command -v curl >/dev/null 2>&1 || die "curl is required"
  TMP="$(mktemp -d)"
  trap 'rm -rf "$TMP"' EXIT
  fetch_artifact
  install_binary
  install_skill
  ensure_claude
  start_daemon
  wait_healthy
  next_steps
}

# Everything runs from main() so a truncated `curl | bash` download cannot execute a partial script.
main "$@"
