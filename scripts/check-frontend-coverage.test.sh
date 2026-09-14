#!/usr/bin/env bash
# check-frontend-coverage.test.sh — regression test for the coverage-merge tooling
# in check-frontend-coverage.sh.
#
# Reproduces (with tiny hand-crafted LCOV fixtures) the two bugs found while
# investigating issue #1370's PR (#1379) coverage-gate failure:
#
#   Bug 1 — SF path double-counting: apps/web/scripts/test-regular.sh cd's into
#           apps/web before running bun, so its lcov.info records SF paths like
#           "SF:src/foo.ts". apps/web/scripts/test-conflict-files.sh deliberately
#           cd's into /tmp first (issue #1323, to dodge bunfig.toml autoload), so
#           ITS lcov.info records the same logical file under a path like
#           "SF:../../Users/.../apps/web/src/foo.ts". `lcov -a` keys strictly on
#           the literal SF string, so a file imported by both a regular test and
#           a conflict-group test is counted as TWO separate "files" in the merge,
#           and the (usually narrower) conflict-group copy drags the aggregate down.
#
#   Bug 2 — Group E omitted: test-conflict-files.sh runs five isolated groups
#           (a, b, c, d, e), but check-frontend-coverage.sh's merge loop only
#           iterated `for group in a b c d` — group e's lcov.info was silently
#           never merged into the gate at all.
#
# This test builds a fake <web-dir> with three tiny LCOV fixtures reproducing
# both bugs simultaneously, runs the real check-frontend-coverage.sh against it,
# and asserts the merged+filtered output at <web-dir>/coverage/combined.lcov
# reflects a correct union: exactly 2 unique source files (foo.ts, bar.ts), with
# bar.ts (group e) present, and foo.ts's two partial coverage sets unioned into
# full coverage.
#
# Usage: ./scripts/check-frontend-coverage.test.sh

set -uo pipefail

# lcov is Perl-backed on macOS and can abort when the parent shell exports an
# unavailable UTF-8 locale. The subject script already pins the portable C
# locale; this suite invokes lcov directly for its post-run assertions too.
export LC_ALL=C
export LANG=C

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

print_error()   { echo -e "${RED}❌ $1${NC}"; }
print_success() { echo -e "${GREEN}✅ $1${NC}"; }
print_info()    { echo -e "${YELLOW}ℹ  $1${NC}"; }

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TARGET="$SCRIPT_DIR/check-frontend-coverage.sh"

FAILURES=0
fail() { print_error "$1"; FAILURES=$((FAILURES + 1)); }
pass() { print_success "$1"; }

# --- Build a fake <web-dir> with fixture LCOV files ---
TMP_DIR="$(mktemp -d)" || { print_error "mktemp -d failed — cannot build fixture web-dir"; exit 1; }
trap 'rm -rf "$TMP_DIR"' EXIT

mkdir -p "$TMP_DIR/coverage" \
         "$TMP_DIR/coverage-conflict/group-a" \
         "$TMP_DIR/coverage-conflict/group-e"

# Regular-test copy of foo.ts (as produced by test-regular.sh, which cd's into
# apps/web): lines 1,2 hit; lines 3,4 NOT hit.
#
# Also includes a dummy packages/api-client entry, fully covered. This isn't
# part of either real bug under test — it exists solely so the script's
# existing `lcov --remove ... '*/packages/api-client/*'` filter step has
# something to actually match. Without it, this lcov version (2.4) treats an
# "unused" --remove pattern as a hard error and aborts before the threshold
# check ever runs — a real repo checkout always has api-client files in the
# denominator, so this only bites synthetic fixtures like this one.
#
# Also includes packages/shared-types/matching.ts, fully covered, under its
# real (non-apps/web) relative path — this file is hand-written and MUST stay
# counted after normalization + filtering (see check-frontend-coverage.sh's
# own "packages/shared-types/matching.ts is hand-written and IS counted"
# comment). It never matches the SF-normalization regex (it doesn't contain
# "/apps/web/src/"), so this asserts that stays true.
cat > "$TMP_DIR/coverage/lcov.info" <<'EOF'
SF:src/foo.ts
DA:1,1
DA:2,1
DA:3,0
DA:4,0
LF:4
LH:2
end_of_record
SF:../packages/api-client/generated.ts
DA:1,1
DA:2,1
LF:2
LH:2
end_of_record
SF:../packages/shared-types/src/matching.ts
DA:1,1
LF:1
LH:1
end_of_record
EOF

# Conflict-group-a copy of the SAME logical foo.ts (as produced by
# test-conflict-files.sh, which cd's into /tmp): COMPLEMENTARY lines 3,4 hit;
# lines 1,2 NOT hit. Path ends in /apps/web/src/foo.ts per the real convention.
cat > "$TMP_DIR/coverage-conflict/group-a/lcov.info" <<'EOF'
SF:../../Users/fake/worktree/apps/web/src/foo.ts
DA:1,0
DA:2,0
DA:3,1
DA:4,1
LF:4
LH:2
end_of_record
EOF

# Conflict-group-e copy of a DIFFERENT source file, bar.ts, fully covered, that
# appears in no other group. Also under a /tmp-style path (group e is produced
# by the same test-conflict-files.sh cd-into-/tmp mechanism as group a).
cat > "$TMP_DIR/coverage-conflict/group-e/lcov.info" <<'EOF'
SF:../../Users/fake/worktree/apps/web/src/bar.ts
DA:1,1
DA:2,1
DA:3,1
LF:3
LH:3
end_of_record
EOF

# --- Run the real script under test ---
# Threshold of 95 is deliberately unreachable unless BOTH bugs are fixed:
#   - current (buggy) behavior: foo.ts double-counted, bar.ts entirely missing -> 55.6% (5/9)
#   - partial fix (group e added, no SF normalization): 4 "files" -> 66.7% (8/12)
#   - full fix (normalization + group e): 3 files, union of foo.ts -> 100% (9/9)
THRESHOLD=95
print_info "Running $TARGET against fixture web-dir (threshold ${THRESHOLD})..."
set +e
SCRIPT_OUTPUT="$("$TARGET" "$TMP_DIR" "$THRESHOLD" 2>&1)"
SCRIPT_EXIT=$?
set -e
echo "$SCRIPT_OUTPUT"

COMBINED="$TMP_DIR/coverage/combined.lcov"

if [ ! -f "$COMBINED" ]; then
  fail "Expected merged+filtered output at $COMBINED, but it does not exist"
  echo
  print_error "check-frontend-coverage.test.sh: FAIL ($FAILURES assertion(s) failed)"
  exit 1
fi

LIST_OUTPUT="$(lcov --list "$COMBINED" 2>&1)"
print_info "lcov --list of merged+filtered output:"
echo "$LIST_OUTPUT"

# --- Assertion 1: exactly 3 unique source files (foo.ts, bar.ts, matching.ts) ---
SF_COUNT=$(grep -c '^SF:' "$COMBINED" || true)
if [ "$SF_COUNT" -eq 3 ]; then
  pass "Exactly 3 unique source files in merged output (got $SF_COUNT)"
else
  fail "Expected exactly 3 unique source files in merged output, got $SF_COUNT (foo.ts is still double-counted under two SF paths, and/or bar.ts is missing/duplicated)"
fi

# --- Assertion 2: bar.ts (group e) is present at all ---
if grep -q '^SF:src/bar.ts$' "$COMBINED"; then
  pass "bar.ts (group e) is present in merged output"
else
  fail "bar.ts (group e) is MISSING from merged output — group e is not being merged"
fi

# --- Assertion 3: foo.ts is present exactly once, under the normalized path ---
FOO_SF_COUNT=$(grep -c '^SF:.*foo\.ts$' "$COMBINED" || true)
if [ "$FOO_SF_COUNT" -eq 1 ] && grep -q '^SF:src/foo.ts$' "$COMBINED"; then
  pass "foo.ts appears exactly once, under normalized SF path"
else
  fail "foo.ts does not appear exactly once under normalized 'SF:src/foo.ts' (found $FOO_SF_COUNT foo.ts SF record(s))"
fi

# --- Assertion 4: foo.ts's two complementary partial-coverage sets were unioned ---
# (lines 1-4 all hit: 2 from the regular-test copy + 2 complementary from group-a)
FOO_BLOCK=$(awk '/^SF:src\/foo\.ts$/,/^end_of_record$/' "$COMBINED")
FOO_LH=$(echo "$FOO_BLOCK" | grep '^LH:' | head -1 | cut -d: -f2)
FOO_LF=$(echo "$FOO_BLOCK" | grep '^LF:' | head -1 | cut -d: -f2)
if [ "${FOO_LH:-0}" -eq 4 ] && [ "${FOO_LF:-0}" -eq 4 ]; then
  pass "foo.ts shows 4/4 lines hit (union of both partial-coverage copies)"
else
  fail "foo.ts expected 4/4 lines hit (LH=4, LF=4) after union, got LH=${FOO_LH:-<missing>} LF=${FOO_LF:-<missing>}"
fi

# --- Assertion 5: the script itself reports success (threshold met) ---
if [ "$SCRIPT_EXIT" -eq 0 ]; then
  pass "check-frontend-coverage.sh exited 0 (threshold ${THRESHOLD}% met)"
else
  fail "check-frontend-coverage.sh exited $SCRIPT_EXIT (expected 0 — threshold ${THRESHOLD}% should be met once both bugs are fixed)"
fi

# --- Assertion 6: packages/shared-types/matching.ts survives untouched ---
# (proves the SF-normalization regex, anchored on "/apps/web/src/", never
# touches non-apps/web paths — this file must stay counted, per
# check-frontend-coverage.sh's own "IS counted" comment)
if grep -q '^SF:\.\./packages/shared-types/src/matching\.ts$' "$COMBINED"; then
  pass "packages/shared-types/matching.ts survives normalization+filtering under its real path"
else
  fail "packages/shared-types/matching.ts is missing or its SF path was altered by normalization — it must never match the /apps/web/src/ regex"
fi

echo
if [ "$FAILURES" -eq 0 ]; then
  print_success "check-frontend-coverage.test.sh: PASS (all assertions passed)"
  exit 0
else
  print_error "check-frontend-coverage.test.sh: FAIL ($FAILURES assertion(s) failed)"
  exit 1
fi
