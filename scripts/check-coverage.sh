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
COVERAGE_OUTPUT=$(go tool cover -func="$COVERAGE_FILE" | grep total)

if [ -z "$COVERAGE_OUTPUT" ]; then
  print_error "Could not extract coverage from $COVERAGE_FILE"
  exit 1
fi

# Parse coverage percentage (format: "total: (statements) XX.X%")
COVERAGE=$(echo "$COVERAGE_OUTPUT" | awk '{print $3}' | sed 's/%//')

print_info "Coverage file: $COVERAGE_FILE"
print_info "Current coverage: ${COVERAGE}%"
print_info "Required threshold: ${THRESHOLD}%"

# Compare coverage with threshold using bc for float comparison
if command -v bc >/dev/null 2>&1; then
  # Use bc for precise comparison if available
  if (( $(echo "$COVERAGE < $THRESHOLD" | bc -l) )); then
    print_error "Coverage ${COVERAGE}% is below threshold ${THRESHOLD}%"
    echo ""
    echo "Coverage breakdown:"
    go tool cover -func="$COVERAGE_FILE" | grep -v "^mode:" | tail -n 10
    exit 1
  fi
else
  # Fallback to integer comparison
  COVERAGE_INT=$(echo "$COVERAGE" | cut -d. -f1)
  THRESHOLD_INT=$(echo "$THRESHOLD" | cut -d. -f1)

  if [ "$COVERAGE_INT" -lt "$THRESHOLD_INT" ]; then
    print_error "Coverage ${COVERAGE}% is below threshold ${THRESHOLD}%"
    echo ""
    echo "Coverage breakdown:"
    go tool cover -func="$COVERAGE_FILE" | grep -v "^mode:" | tail -n 10
    exit 1
  fi
fi

print_success "Coverage ${COVERAGE}% meets threshold ${THRESHOLD}%"

# Show detailed breakdown in verbose mode
if [ "${VERBOSE:-false}" = "true" ]; then
  echo ""
  echo "Detailed coverage breakdown:"
  go tool cover -func="$COVERAGE_FILE" | grep -v "^mode:"
fi

exit 0
