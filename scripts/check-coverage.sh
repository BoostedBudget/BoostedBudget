#!/usr/bin/env bash

# check-coverage.sh - Check Go test coverage meets minimum threshold
# Usage: ./scripts/check-coverage.sh [coverage-file] [threshold]
#
# Examples:
#   ./scripts/check-coverage.sh coverage.out 80
#   ./scripts/check-coverage.sh backend/coverage.out 75

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
COVERAGE_FILE="${1:-coverage.out}"
THRESHOLD="${2:-80}"

# Function to print colored output
print_error() {
  echo -e "${RED}❌ $1${NC}"
}

print_success() {
  echo -e "${GREEN}✅ $1${NC}"
}

print_info() {
  echo -e "${YELLOW}ℹ $1${NC}"
}

# Check if coverage file exists
if [ ! -f "$COVERAGE_FILE" ]; then
  print_error "Coverage file not found: $COVERAGE_FILE"
  exit 1
fi

# Extract coverage percentage from go tool cover
# Must run from the directory containing go.mod (backend/) so go tool cover
# can resolve package paths in the coverage file.
COVERAGE_DIR=$(dirname "$COVERAGE_FILE")
COVERAGE_BASENAME=$(basename "$COVERAGE_FILE")

# Issue #1622. `COVERAGE_OUTPUT=$(... | grep total)` under `set -euo pipefail` exits the script
# AT THIS LINE when `go tool cover` fails, so the friendly error below could never print: a
# corrupt profile produced a bare one-line `cover: ...` parse error naming neither the file nor
# the target, and the operator had no idea which artifact to regenerate. Demonstrated, not
# assumed -- a profile with one malformed line reproduces it exactly.
#
# The `|| true` and the explicit status capture are what keep the failure reportable here. Do
# NOT collapse this back into a single assignment.
#
# ⚠ Corrected while fixing: the path was dead ONE way, not two. `grep total` matching nothing is
# NOT reachable for a profile go tool cover can parse -- `-func` always emits a `total:` line,
# so a valid-but-empty profile reports 0.0% and fails the threshold loudly rather than falling
# into the -z branch. Only the parse failure was silent.
set +e
COVERAGE_RAW=$(cd "$COVERAGE_DIR" && go tool cover -func="$COVERAGE_BASENAME" 2>&1)
COVER_STATUS=$?
set -e

if [ "$COVER_STATUS" -ne 0 ]; then
  print_error "go tool cover could not read the coverage profile: $COVERAGE_FILE"
  echo "  go tool cover said:"
  echo "$COVERAGE_RAW" | sed 's/^/    /'
  echo ""
  echo "  The profile is malformed, not merely below threshold. Regenerate it:"
  echo "    cd backend && go test -coverprofile=coverage.out -coverpkg=./... ./..."
  echo ""
  echo "  Profile on disk: $(wc -c < "$COVERAGE_FILE" | tr -d ' ') bytes, last modified $(date -r "$COVERAGE_FILE" 2>/dev/null || echo unknown)"
  echo "  ⚠ .gitignore's '*.out' wildcard hides coverage artifacts from 'git status', so a stale"
  echo "    or half-written profile from an earlier run can sit in the tree unnoticed. Check the"
  echo "    timestamp above against the test run you expected to produce it."
  exit 1
fi

COVERAGE_OUTPUT=$(echo "$COVERAGE_RAW" | grep total || true)

if [ -z "$COVERAGE_OUTPUT" ]; then
  print_error "Could not extract a total line from $COVERAGE_FILE"
  echo "  go tool cover parsed the profile but emitted no 'total:' line, which it normally"
  echo "  always does. Regenerate the profile:"
  echo "    cd backend && go test -coverprofile=coverage.out -coverpkg=./... ./..."
  exit 1
fi

# Parse coverage percentage (format: "total: (statements) XX.X%")
COVERAGE=$(echo "$COVERAGE_OUTPUT" | awk '{print $3}' | sed 's/%//')

print_info "Coverage file: $COVERAGE_FILE"
print_info "Current coverage: ${COVERAGE}%"
print_info "Required threshold: ${THRESHOLD}%"

# Decimal-safe comparison via awk (no external dependency)
if awk -v actual="$COVERAGE" -v threshold="$THRESHOLD" \
       'BEGIN { exit !(actual + 0 < threshold + 0) }'; then
  print_error "Coverage ${COVERAGE}% is below threshold ${THRESHOLD}%"
  echo ""
  echo "Coverage breakdown:"
  (cd "$COVERAGE_DIR" && go tool cover -func="$COVERAGE_BASENAME") | grep -v "^mode:" | tail -n 10
  exit 1
fi

print_success "Coverage ${COVERAGE}% meets threshold ${THRESHOLD}%"

# Show detailed breakdown in verbose mode
if [ "${VERBOSE:-false}" = "true" ]; then
  echo ""
  echo "Detailed coverage breakdown:"
  (cd "$COVERAGE_DIR" && go tool cover -func="$COVERAGE_BASENAME") | grep -v "^mode:"
fi

exit 0
