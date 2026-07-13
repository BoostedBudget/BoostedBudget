#!/usr/bin/env bash
# check-frontend-coverage.sh — merge frontend lcov groups, filter generated code, enforce threshold
#
# Usage: ./scripts/check-frontend-coverage.sh <web-dir> <lines-threshold>
#   web-dir          Path to apps/web (relative or absolute)
#   lines-threshold  Minimum lines coverage % (integer or decimal, e.g. 82)
#
# Example: ./scripts/check-frontend-coverage.sh apps/web 82
#
# ⚠  DENOMINATOR CAVEAT (bun 1.3.x limitation, tracked in #1286):
#    Bun's built-in test runner only instruments source files that are imported
#    during test execution — there is no --coverage-include / --coverage-all flag.
#    Production files that no test file imports are ABSENT from the LCOV data and
#    therefore absent from this gate's denominator. The reported ~84% lines coverage
#    applies only to the "measured" subset, not to all of apps/web/src/. Files
#    known to be outside the denominator include (non-exhaustive): ErrorBoundary.tsx,
#    PlaidLinkButton.tsx, CookieConsent.tsx, DataTable.tsx, and similar leaf components.
#    See GitHub issue #1345 (FE coverage honesty follow-up) for the full list and the
#    plan to add an all-source include once bun supports it or via vitest migration.

set -euo pipefail

# Locale: lcov panics on non-C locales on some macOS configurations
export LC_ALL=C
export LANG=C

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

print_error()   { echo -e "${RED}❌ $1${NC}"; }
print_success() { echo -e "${GREEN}✅ $1${NC}"; }
print_info()    { echo -e "${YELLOW}ℹ  $1${NC}"; }

WEB_DIR="${1:?Usage: check-frontend-coverage.sh <web-dir> <threshold>}"
THRESHOLD="${2:?Usage: check-frontend-coverage.sh <web-dir> <threshold>}"

# Ensure lcov is available
if ! command -v lcov >/dev/null 2>&1; then
  print_error "lcov is required but not installed. Install with: brew install lcov"
  exit 1
fi

# Intermediate merge goes to /tmp; filtered result goes to $WEB_DIR/coverage/combined.lcov.
# Writing the final file in-tree lets CI upload it as an artifact.
COMBINED="/tmp/bbudget-frontend-coverage-$$.lcov"

# Ensure the coverage directory exists (bun test creates it, but guard for safety)
mkdir -p "$WEB_DIR/coverage"
FILTERED="$WEB_DIR/coverage/combined.lcov"

# Directory for SF-normalized copies of each input lcov file (see add_lcov below).
NORMALIZED_DIR="/tmp/bbudget-frontend-coverage-normalized-$$"
mkdir -p "$NORMALIZED_DIR"

# Clean up the temp intermediate + normalized-copy dir on exit
trap "rm -f '$COMBINED'; rm -rf '$NORMALIZED_DIR'" EXIT

# --- Merge all lcov groups ---
# SF-path normalization (issue #1370 exposed this; root cause is #1323):
# apps/web/scripts/test-regular.sh cd's into apps/web before running bun, so its
# lcov.info records SF paths relative to apps/web (e.g. "SF:src/foo.ts").
# apps/web/scripts/test-conflict-files.sh deliberately cd's into /tmp first
# (issue #1323, to dodge bunfig.toml autoload), so ITS lcov.info records the
# SAME logical file under a /tmp-relative path (e.g.
# "SF:../../Users/.../apps/web/src/foo.ts"). `lcov -a` keys strictly on the
# literal SF string, so any file imported by both a regular test and a
# conflict-group test was counted as TWO separate "files" in the merge, and
# the (usually narrower) conflict-group copy dragged the aggregate down.
# Adding issue #1370's new conflict-group test widened the duplicate set past
# the coverage threshold and surfaced this as a false failure.
# Fix: rewrite every input lcov file's SF paths onto the same apps/web-relative
# convention (anchored on the full "/apps/web/src/" segment, not a bare
# "/apps/web/", so this never touches packages/* paths) before merging.
# The anchor is a literal path segment, not derived from $WEB_DIR: every input
# here always originates from this same repo's own apps/web (test-regular.sh
# and test-conflict-files.sh both only ever run against it), so a second,
# unrelated "apps/web/src" tree can't appear among these inputs.
LCOV_ARGS=""
add_lcov() {
  local src="$1"
  [ -f "$src" ] || return 0
  local normalized="$NORMALIZED_DIR/$(echo "$src" | tr '/' '_').lcov"
  sed -E 's#^SF:.*/apps/web/src/#SF:src/#' "$src" > "$normalized"
  LCOV_ARGS="$LCOV_ARGS -a $normalized"
}

add_lcov "$WEB_DIR/coverage/lcov.info"
for group in a b c d e; do
  add_lcov "$WEB_DIR/coverage-conflict/group-$group/lcov.info"
done

if [ -z "$LCOV_ARGS" ]; then
  print_error "No lcov.info files found under $WEB_DIR/coverage/ or coverage-conflict/. Run bun run test:ci first."
  exit 1
fi

print_info "Merging lcov groups..."
# shellcheck disable=SC2086
lcov $LCOV_ARGS -o "$COMBINED"

# --- Filter generated packages/api-client code ---
# packages/api-client is orval-generated — not unit-testable, should not be in denominator.
# packages/shared-types/matching.ts is hand-written and IS counted (do not exclude it).
print_info "Filtering generated packages/api-client/** from denominator..."
lcov --remove "$COMBINED" '*/packages/api-client/*' -o "$FILTERED"

# --- Parse Lines coverage ---
SUMMARY=$(lcov --summary "$FILTERED" 2>&1)
print_info "Post-filter coverage summary:"
echo "$SUMMARY" | grep -E "(lines|source files)" || true

LINES=$(echo "$SUMMARY" | grep "lines" | grep -oE '[0-9]+([.][0-9]+)?%' | head -1 | sed 's/%//' || true)

if [ -z "$LINES" ]; then
  print_error "Could not parse Lines coverage from lcov summary. Raw output:"
  echo "$SUMMARY"
  exit 1
fi

print_info "Lines coverage: ${LINES}% (threshold: ${THRESHOLD}%)"

# Decimal-safe comparison
if awk -v actual="$LINES" -v threshold="$THRESHOLD" \
       'BEGIN { exit !(actual + 0 < threshold + 0) }'; then
  print_error "Frontend Lines coverage ${LINES}% is below threshold ${THRESHOLD}%"
  exit 1
fi

print_success "Frontend Lines coverage ${LINES}% meets threshold ${THRESHOLD}%"
