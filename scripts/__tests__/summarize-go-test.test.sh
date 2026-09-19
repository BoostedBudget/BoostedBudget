#!/usr/bin/env bash
# Falsification suite for scripts/summarize-go-test.sh (#1569 round 14 task 3).
#
# summarize-go-test.sh wraps a `go test` invocation and prints a PASS/FAIL/SKIP tally so a
# run with skipped tests never reads as an indistinguishable clean pass — the exact defect
# that let a real regression hide in a silently-skipped test for two rounds. This suite
# falsifies it against a tiny throwaway Go module rather than the real backend module, so it
# runs in well under a second and does not depend on Postgres/Redis being up.
#
# Run: bash scripts/__tests__/summarize-go-test.test.sh
# Exit 0 on all assertions passing, non-zero on any failure.
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SCRIPT="$ROOT/scripts/summarize-go-test.sh"
PASS=0
FAIL=0

pass() { echo "  ✅ PASS: $1"; PASS=$((PASS + 1)); }
fail() { echo "  ❌ FAIL: $1"; FAIL=$((FAIL + 1)); }

# A skip is not a pass — the very defect scripts/summarize-go-test.sh exists to make visible, so
# this suite must not commit it either. Locally a missing Go toolchain is a legitimate reason to
# skip; under CI it is not, because this suite is now wired into a CI job (#1569 round 15 task 1)
# and a silent exit 0 there would manufacture the appearance of coverage. GitHub Actions always
# sets CI=true, so CI fails closed while a local run keeps the courtesy skip.
if ! command -v go >/dev/null 2>&1; then
  if [ -n "${CI:-}" ]; then
    echo "❌ go not found on PATH, but CI is set — this suite must not silently pass in CI." >&2
    exit 1
  fi
  echo "⚠️  go not installed — skipping locally (CI fails closed instead)."
  exit 0
fi

SANDBOX=$(mktemp -d)
trap 'rm -rf "$SANDBOX"' EXIT

cat > "$SANDBOX/go.mod" <<'EOF'
module github.com/example/summarizetestfixture

go 1.21
EOF

cat > "$SANDBOX/fixture_test.go" <<'EOF'
package summarizetestfixture

import "testing"

func TestAlwaysPasses(t *testing.T) {}

func TestAlwaysSkips(t *testing.T) {
	t.Skip("deliberately skipped for the summarize-go-test.sh fixture")
}

func TestAlwaysSkipsToo(t *testing.T) {
	t.Skip("deliberately skipped for the summarize-go-test.sh fixture")
}
EOF

echo "Test 1: a run with 1 pass + 2 skips reports PASS=1 FAIL=0 SKIP=2 and exits 0"
OUT=$(cd "$SANDBOX" && bash "$SCRIPT" go test ./... 2>&1)
CODE=$?
if [ "$CODE" -eq 0 ] && printf '%s' "$OUT" | grep -q "PASS=1 FAIL=0 SKIP=2"; then
  pass "reports PASS=1 FAIL=0 SKIP=2 and exits 0"
else
  fail "expected exit 0 and 'PASS=1 FAIL=0 SKIP=2', got exit=$CODE output=$OUT"
fi

echo ""
echo "Test 2: skip names are listed by name, not just counted"
if printf '%s' "$OUT" | grep -q "TestAlwaysSkips$" && printf '%s' "$OUT" | grep -q "TestAlwaysSkipsToo"; then
  pass "lists both skipped test names"
else
  fail "expected both skipped test names in output, got: $OUT"
fi

echo ""
echo "Test 3: -v is added automatically (caller need not remember it) — without this,"
echo "         the script would have nothing to count and would silently under-report"
OUT_NO_V=$(cd "$SANDBOX" && bash "$SCRIPT" go test ./... 2>&1)
if printf '%s' "$OUT_NO_V" | grep -q '^--- SKIP:'; then
  pass "-v was injected: --- SKIP lines are present even though the caller didn't pass -v"
else
  fail "expected --- SKIP lines (implying -v was auto-injected), got: $OUT_NO_V"
fi

echo ""
echo "Test 4: a real test failure's exit code is propagated, not swallowed by the summary"
cat > "$SANDBOX/fixture_test.go" <<'EOF'
package summarizetestfixture

import "testing"

func TestThisOneFails(t *testing.T) {
	t.Fatal("deliberate failure for the summarize-go-test.sh fixture")
}
EOF
OUT_FAIL=$(cd "$SANDBOX" && bash "$SCRIPT" go test ./... 2>&1)
CODE_FAIL=$?
if [ "$CODE_FAIL" -ne 0 ] && printf '%s' "$OUT_FAIL" | grep -q "PASS=0 FAIL=1 SKIP=0"; then
  pass "propagates go test's non-zero exit and reports FAIL=1"
else
  fail "expected non-zero exit and 'PASS=0 FAIL=1 SKIP=0', got exit=$CODE_FAIL output=$OUT_FAIL"
fi

echo ""
echo "Test 5: a clean run with zero skips does NOT print the skip-warning block"
cat > "$SANDBOX/fixture_test.go" <<'EOF'
package summarizetestfixture

import "testing"

func TestCleanPass(t *testing.T) {}
EOF
OUT_CLEAN=$(cd "$SANDBOX" && bash "$SCRIPT" go test ./... 2>&1)
if ! printf '%s' "$OUT_CLEAN" | grep -q "SKIPPED"; then
  pass "no skip-warning block when SKIP=0"
else
  fail "expected no skip-warning block on a clean run, got: $OUT_CLEAN"
fi

echo ""
echo "Test 6: a clean run reports PKG_FAIL=0 and prints no build-failure warning"
if printf '%s' "$OUT_CLEAN" | grep -q "PKG_FAIL=0" \
   && ! printf '%s' "$OUT_CLEAN" | grep -q "package(s) FAILED at the package level"; then
  pass "clean run reports PKG_FAIL=0 with no build-failure warning"
else
  fail "expected PKG_FAIL=0 and no build-failure warning, got: $OUT_CLEAN"
fi

echo ""
echo "Test 7: a Go BUILD failure is surfaced, not a false green (G22)"
# A build failure emits a PACKAGE-level 'FAIL <pkg> [build failed]' line and never a
# '--- FAIL:' line, so the pre-G22 summary printed PASS=0 FAIL=0 SKIP=0 while the command
# exited non-zero. The fixture module path is github.com/... so the script's package-level
# regex sees it, exactly as it sees the real backend module.
cat > "$SANDBOX/fixture_test.go" <<'EOF'
package summarizetestfixture

import "testing"

func TestBrokenBuild(t *testing.T) { undefinedSymbolForBuildFailure() }
EOF
OUT_BUILD=$(cd "$SANDBOX" && bash "$SCRIPT" go test ./... 2>&1)
CODE_BUILD=$?
if [ "$CODE_BUILD" -ne 0 ] \
   && printf '%s' "$OUT_BUILD" | grep -q "PKG_FAIL=1" \
   && printf '%s' "$OUT_BUILD" | grep -q "package(s) FAILED at the package level"; then
  pass "build failure exits non-zero and is surfaced as PKG_FAIL=1 with a warning"
else
  fail "expected non-zero exit + PKG_FAIL=1 + warning, got exit=$CODE_BUILD output=$OUT_BUILD"
fi

echo ""
echo "========================================"
echo "Results: $PASS passed, $FAIL failed"
echo "========================================"
if [ "$FAIL" -gt 0 ]; then
  exit 1
fi
exit 0
