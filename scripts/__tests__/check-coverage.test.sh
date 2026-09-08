#!/usr/bin/env bash
# Falsification suite for scripts/check-coverage.sh (#1622).
#
# check-coverage.sh is a CI gate: it reads a Go coverage profile and fails the build when
# statement coverage is under threshold. Its friendly "could not extract coverage" error was
# DEAD CODE — `VAR=$(... | grep total)` under `set -euo pipefail` exits the script at the
# assignment when `go tool cover` fails, so a corrupt profile produced a bare one-line parse
# error naming neither the artifact nor the way to regenerate it. This suite pins the fix: a
# malformed profile must be reported AS malformed, distinguishably from "below threshold".
#
# Every assertion here is constructed to fail against the pre-fix script. Test 1 in particular
# reproduces the exact defect: pre-fix it printed `cover: ...` and nothing else.
#
# Run: bash scripts/__tests__/check-coverage.test.sh
# Exit 0 on all assertions passing, non-zero on any failure.
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SCRIPT="$ROOT/scripts/check-coverage.sh"
PASS=0
FAIL=0

pass() { echo "  ✅ PASS: $1"; PASS=$((PASS + 1)); }
fail() { echo "  ❌ FAIL: $1"; FAIL=$((FAIL + 1)); }

# A skip is not a pass. Locally a missing Go toolchain is a legitimate reason to skip; under CI
# it is not, because this suite is wired into a CI step and a silent exit 0 there would
# manufacture the appearance of coverage. GitHub Actions always sets CI=true.
if ! command -v go >/dev/null 2>&1; then
  if [ -n "${CI:-}" ]; then
    echo "❌ go not found on PATH, but CI is set — this suite must not silently pass in CI." >&2
    exit 1
  fi
  echo "⚠️  go not installed — skipping locally (CI fails closed instead)."
  exit 0
fi

if [ ! -f "$SCRIPT" ]; then
  echo "❌ $SCRIPT not found — the suite's subject has moved or been deleted." >&2
  exit 1
fi

SANDBOX=$(mktemp -d)
trap 'rm -rf "$SANDBOX"' EXIT

echo "Test 1: a MALFORMED profile is reported as malformed, with the file and a regenerate hint"
printf 'mode: set\nthis is not a valid coverage line\n' > "$SANDBOX/bad.out"
OUT_BAD=$(cd "$SANDBOX" && bash "$SCRIPT" bad.out 74 2>&1)
CODE_BAD=$?
if [ "$CODE_BAD" -ne 0 ] \
  && printf '%s' "$OUT_BAD" | grep -q "could not read the coverage profile" \
  && printf '%s' "$OUT_BAD" | grep -q "bad.out" \
  && printf '%s' "$OUT_BAD" | grep -q "coverprofile"; then
  pass "malformed profile names the file, the cause, and how to regenerate it"
else
  fail "expected a malformed-profile diagnostic naming bad.out and a regenerate hint, got exit=$CODE_BAD output=$OUT_BAD"
fi

echo ""
echo "Test 2: the malformed-profile diagnostic surfaces go tool cover's own stderr"
# Pre-fix, stderr went to the terminal unlabelled and the script died before it could be
# attributed to anything. It must now appear INSIDE the diagnostic.
if printf '%s' "$OUT_BAD" | grep -q "go tool cover said:" \
  && printf '%s' "$OUT_BAD" | grep -q "doesn't match expected format"; then
  pass "go tool cover's stderr is captured and attributed"
else
  fail "expected go tool cover's stderr inside the diagnostic, got: $OUT_BAD"
fi

echo ""
echo "Test 3: a malformed profile is NOT reported as a threshold failure"
# The whole point of the fix: 'corrupt' and 'below threshold' are different operator actions.
# Regenerate the artifact vs. write more tests.
# Matched on the threshold failure's OWN line ("Coverage X% is below threshold Y%"), not on the
# bare words: the malformed diagnostic legitimately contains the phrase "not merely below
# threshold", and a looser grep here failed against the CORRECT implementation.
if ! printf '%s' "$OUT_BAD" | grep -q "is below threshold"; then
  pass "malformed profile is distinguishable from below-threshold"
else
  fail "malformed profile was reported as a threshold failure: $OUT_BAD"
fi

echo ""
echo "Test 4: the stale-artifact warning names the .gitignore wildcard that hides the profile"
# .gitignore's '*.out' keeps coverage artifacts out of `git status`, so a stale or half-written
# profile can sit in the tree unnoticed. The operator has to be told to check the timestamp.
if printf '%s' "$OUT_BAD" | grep -q "gitignore" \
  && printf '%s' "$OUT_BAD" | grep -q "last modified"; then
  pass "stale-artifact warning present, with the profile's mtime"
else
  fail "expected a stale-artifact warning with the profile mtime, got: $OUT_BAD"
fi

echo ""
echo "Test 5: a VALID profile under threshold still fails, as a threshold failure"
# Guards against 'fixing' the diagnostic by swallowing real gate failures. go tool cover -func
# always emits a total: line, so an empty-but-valid profile reads as 0.0% — loudly under
# threshold, NOT as a corruption.
printf 'mode: set\n' > "$SANDBOX/empty.out"
OUT_LOW=$(cd "$SANDBOX" && bash "$SCRIPT" empty.out 74 2>&1)
CODE_LOW=$?
if [ "$CODE_LOW" -ne 0 ] \
  && printf '%s' "$OUT_LOW" | grep -q "below threshold" \
  && ! printf '%s' "$OUT_LOW" | grep -q "could not read the coverage profile"; then
  pass "a parseable profile under threshold fails as a threshold failure"
else
  fail "expected a threshold failure, got exit=$CODE_LOW output=$OUT_LOW"
fi

echo ""
echo "Test 6: a VALID profile at or above threshold still passes"
# The gate must still be able to say yes. Threshold 0 against the same 0.0% profile.
OUT_OK=$(cd "$SANDBOX" && bash "$SCRIPT" empty.out 0 2>&1)
CODE_OK=$?
if [ "$CODE_OK" -eq 0 ]; then
  pass "a profile meeting the threshold exits 0"
else
  fail "expected exit 0 for a profile meeting the threshold, got exit=$CODE_OK output=$OUT_OK"
fi

echo ""
echo "Test 7: a MISSING profile is still reported as missing, not as malformed"
OUT_MISSING=$(cd "$SANDBOX" && bash "$SCRIPT" nope.out 74 2>&1)
CODE_MISSING=$?
if [ "$CODE_MISSING" -ne 0 ] && printf '%s' "$OUT_MISSING" | grep -q "not found"; then
  pass "missing profile keeps its own distinct diagnostic"
else
  fail "expected a not-found diagnostic, got exit=$CODE_MISSING output=$OUT_MISSING"
fi

echo ""
echo "========================================"
echo "Results: $PASS passed, $FAIL failed"
echo "========================================"
if [ "$FAIL" -gt 0 ]; then
  exit 1
fi
exit 0
