#!/usr/bin/env bash
# Mutation-tests Go packages changed vs BASE_REF (default origin/main). Used by
# verify.sh --mutation and the CI mutation job so both apply the same gate.
set -euo pipefail
cd "$(dirname "$0")/.."

BASE_REF="${BASE_REF:-origin/main}"
MUTATION_EFFICACY_MIN="${MUTATION_EFFICACY_MIN:-60}"
# A generous timeout: under load gremlins otherwise marks mutants TIMED OUT,
# which are excluded from efficacy, so the gate could pass having tested nothing.
TIMEOUT_COEFFICIENT="${TIMEOUT_COEFFICIENT:-20}"

pkgs=$(git diff --name-only "${BASE_REF}"...HEAD -- '*.go' 2>/dev/null \
  | grep -v '_test.go$' | grep -v '^examples/' | grep -v '^web/' | xargs -r -n1 dirname | sort -u || true)
if [[ -z "$pkgs" ]]; then
  echo "no changed Go packages vs ${BASE_REF}; skipping"
  exit 0
fi
for p in $pkgs; do
  echo "--- mutation: $p (efficacy >= ${MUTATION_EFFICACY_MIN}%)"
  go tool -modfile=tools/go.mod gremlins unleash \
    --timeout-coefficient "$TIMEOUT_COEFFICIENT" \
    --threshold-efficacy "$MUTATION_EFFICACY_MIN" "./$p"
done
