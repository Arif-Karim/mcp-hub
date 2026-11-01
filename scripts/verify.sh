#!/usr/bin/env bash
# Runs the same checks as CI. Usage:
#   scripts/verify.sh              # lint, security, tests, coverage, UI checks
#   scripts/verify.sh --mutation   # also mutation-test Go packages changed vs origin/main
set -euo pipefail
cd "$(dirname "$0")/.."

COVERAGE_MIN="${COVERAGE_MIN:-70}"
MUTATION_EFFICACY_MIN="${MUTATION_EFFICACY_MIN:-60}"
BASE_REF="${BASE_REF:-origin/main}"
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

step "govulncheck"
tool govulncheck ./...

step "gitleaks (secret scan)"
go run github.com/zricethezav/gitleaks/v8@v8.30.1 git . --no-banner --redact

if [[ -f examples/mcp-server-template/go.mod ]]; then
  step "example server: lint + test"
  (cd examples/mcp-server-template && go tool -modfile=../../tools/go.mod golangci-lint run ./... && go test -race ./...)
fi

if [[ -f web/package.json ]]; then
  step "web: install, lint, typecheck, unit tests"
  (cd web && { [[ -d node_modules ]] || npm ci; } && npm run lint && npm run typecheck && npm test)
fi

if [[ "${1:-}" == "--mutation" ]]; then
  step "mutation testing (changed packages vs ${BASE_REF}, efficacy >= ${MUTATION_EFFICACY_MIN}%)"
  pkgs=$(git diff --name-only "${BASE_REF}"...HEAD -- '*.go' 2>/dev/null \
    | grep -v '_test.go$' | grep -v '^examples/' | xargs -r -n1 dirname | sort -u || true)
  if [[ -z "$pkgs" ]]; then
    echo "no changed Go packages; skipping"
  else
    for p in $pkgs; do
      echo "--- $p"
      tool gremlins unleash --threshold-efficacy "$MUTATION_EFFICACY_MIN" "./$p"
    done
  fi
fi

step "all checks passed"
