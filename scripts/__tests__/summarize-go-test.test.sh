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

if ! command -v go >/dev/null 2>&1; then
  echo "⚠️  go not installed — skipping (CI always has go)."
  exit 0
fi

SANDBOX=$(mktemp -d)
trap 'rm -rf "$SANDBOX"' EXIT

cat > "$SANDBOX/go.mod" <<'EOF'
module summarizetestfixture

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
echo "========================================"
echo "Results: $PASS passed, $FAIL failed"
echo "========================================"
if [ "$FAIL" -gt 0 ]; then
  exit 1
fi
exit 0
