#!/usr/bin/env bash
# Runs the same checks as CI. Usage:
#   scripts/verify.sh              # lint, security, tests, coverage, UI checks
#   scripts/verify.sh --mutation   # also mutation-test Go packages changed vs origin/main
set -euo pipefail
cd "$(dirname "$0")/.."

COVERAGE_MIN="${COVERAGE_MIN:-70}"
tool() { go tool -modfile=tools/go.mod "$@"; }
step() { printf '\n\033[1m==> %s\033[0m\n' "$*"; }

step "go mod tidy check"
go mod tidy -diff

step "golangci-lint (gofmt, goimports, govet, staticcheck, gosec, revive, errcheck, ...)"
tool golangci-lint run ./...

step "go test -race with coverage (min ${COVERAGE_MIN}%)"
go test -race -covermode=atomic -coverpkg=./internal/... -coverprofile=coverage.out ./...
total=$(go tool cover -func=coverage.out | awk '/^total:/ {sub("%","",$3); print $3}')
echo "total coverage: ${total}%"
awk -v t="$total" -v m="$COVERAGE_MIN" 'BEGIN { exit (t+0 >= m+0) ? 0 : 1 }' \
  || { echo "coverage ${total}% is below ${COVERAGE_MIN}%"; exit 1; }

step "harness hooks: guard table test"
scripts/hooks/guard-bash_test.sh

step "govulncheck"
tool govulncheck ./...

if [[ -f web/package-lock.json ]]; then
  step "npm audit (shipped dependencies, high+)"
  (cd web && npm audit --omit=dev --audit-level=high)
fi

step "gitleaks (secret scan)"
go run github.com/zricethezav/gitleaks/v8@v8.30.1 git . --no-banner --redact

if [[ -f examples/mcp-server-template/go.mod ]]; then
  step "example server: lint + test"
  (cd examples/mcp-server-template && go tool -modfile=../../tools/go.mod golangci-lint run ./... && go test -race ./...)
fi

if [[ -f web/package.json ]]; then
  step "web: install, lint, typecheck, unit tests, build"
  (cd web && { [[ -d node_modules ]] || npm ci; } && npm run lint && npm run typecheck && npm test && npm run build)
  # The build empties internal/webui/dist and restores the tracked .gitkeep; the tree must stay clean.
  if [[ -n "$(git status --porcelain -- internal/webui/dist)" ]]; then
    echo "web build left internal/webui/dist dirty:"; git status --short -- internal/webui/dist; exit 1
  fi
fi

step "build: shippable binary with embedded UI + smoke test"
go build -trimpath -o bin/mcp-hub ./cmd/mcp-hub
bin/mcp-hub version
if [[ -f examples/mcp-server-template/go.mod ]]; then
  (cd examples/mcp-server-template && go build -trimpath -o ../../bin/example-mcp-server ./cmd/mcp-server)
  step "dev registry script test (dry run)"
  scripts/tests/dev_registry_test.sh
fi

if [[ "${1:-}" == "--mutation" ]]; then
  step "mutation testing (changed packages)"
  scripts/mutation.sh
fi

step "all checks passed"
