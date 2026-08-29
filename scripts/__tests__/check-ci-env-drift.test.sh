#!/usr/bin/env bash
# Falsification suite for scripts/check-ci-env-drift.sh (#1569 round 14 task 3).
#
# check-ci-env-drift.sh derives its list of load-bearing integration-test env vars from CI's
# own guard step in .github/workflows/test.yml (rather than a hand-maintained duplicate list)
# and fails when any of those vars is unset locally. This suite falsifies:
#   1. the real workflow + all three vars set -> passes (proves it isn't a guard that can't pass)
#   2. the real workflow + one var missing    -> fails, names exactly the missing one
#   3. the real workflow + all vars missing   -> fails, names all three
#   4. a fixture workflow whose guard step name changed -> fails CLOSED ("could not locate"),
#      never silently reports success when it can't find what it's supposed to check
#   5. a fixture workflow whose guard step body has zero `[ -z "$VAR" ]` checks -> fails CLOSED
#   6. a fixture workflow whose guard step checks a FOURTH var -> the fourth var is picked up
#      automatically (proves this isn't a hand-maintained duplicate: it observes CI, not a
#      copy of CI)
#   7. it must not fire on a CI-only var (e.g. GO_VERSION) that is unset locally but is NOT
#      part of the guard step -> proves the narrow scope claim, not just the wide one
#
# Run: bash scripts/__tests__/check-ci-env-drift.test.sh
# Exit 0 on all assertions passing, non-zero on any failure.
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SCRIPT="$ROOT/scripts/check-ci-env-drift.sh"
PASS=0
FAIL=0

pass() { echo "  ✅ PASS: $1"; PASS=$((PASS + 1)); }
fail() { echo "  ❌ FAIL: $1"; FAIL=$((FAIL + 1)); }

SANDBOX=$(mktemp -d)
trap 'rm -rf "$SANDBOX"' EXIT

echo "Test 1: real workflow, all three vars set -> exit 0"
OUT=$(env -i PATH="$PATH" \
  DATABASE_URL="postgres://x" CACHE_REDIS_TEST_URL="redis://x" REDIS_TEST_URL="redis://x" \
  bash "$SCRIPT" 2>&1)
CODE=$?
if [ "$CODE" -eq 0 ] && printf '%s' "$OUT" | grep -q "all env var(s)"; then
  pass "exits 0 and confirms all vars set"
else
  fail "expected exit 0 with confirmation, got exit=$CODE output=$OUT"
fi

echo ""
echo "Test 2: real workflow, REDIS_TEST_URL missing -> exit 1 naming it"
OUT=$(env -i PATH="$PATH" \
  DATABASE_URL="postgres://x" CACHE_REDIS_TEST_URL="redis://x" \
  bash "$SCRIPT" 2>&1)
CODE=$?
if [ "$CODE" -ne 0 ] && printf '%s' "$OUT" | grep -q "REDIS_TEST_URL" && ! printf '%s' "$OUT" | grep -q "DATABASE_URL.*CACHE_REDIS_TEST_URL.*REDIS_TEST_URL"; then
  pass "fails and names exactly REDIS_TEST_URL as missing"
else
  fail "expected failure naming only REDIS_TEST_URL, got exit=$CODE output=$OUT"
fi

echo ""
echo "Test 3: real workflow, all three vars missing -> exit 1 naming all three"
OUT=$(env -i PATH="$PATH" bash "$SCRIPT" 2>&1)
CODE=$?
if [ "$CODE" -ne 0 ] && printf '%s' "$OUT" | grep -q "DATABASE_URL" && printf '%s' "$OUT" | grep -q "CACHE_REDIS_TEST_URL" && printf '%s' "$OUT" | grep -q "REDIS_TEST_URL"; then
  pass "fails and names all three missing vars"
else
  fail "expected failure naming all three, got exit=$CODE output=$OUT"
fi

echo ""
echo "Test 4: fixture workflow with the guard step renamed -> fails CLOSED"
FIXTURE_RENAMED="$SANDBOX/renamed.yml"
sed 's/Guard — DATABASE_URL, CACHE_REDIS_TEST_URL, REDIS_TEST_URL must be set/Guard — renamed step/' \
  "$ROOT/.github/workflows/test.yml" > "$FIXTURE_RENAMED"
OUT=$(env -i PATH="$PATH" CI_ENV_DRIFT_WORKFLOW_OVERRIDE="$FIXTURE_RENAMED" \
  DATABASE_URL="x" CACHE_REDIS_TEST_URL="x" REDIS_TEST_URL="x" \
  bash "$SCRIPT" 2>&1)
CODE=$?
if [ "$CODE" -ne 0 ] && printf '%s' "$OUT" | grep -q "could not locate"; then
  pass "fails closed with 'could not locate' when the guard step is renamed"
else
  fail "expected fail-closed 'could not locate', got exit=$CODE output=$OUT"
fi

echo ""
echo "Test 5: fixture workflow whose guard step body checks nothing -> fails CLOSED"
FIXTURE_EMPTY="$SANDBOX/empty-body.yml"
awk '
  /^      - name: Guard — DATABASE_URL, CACHE_REDIS_TEST_URL, REDIS_TEST_URL must be set/ {
    print
    print "        run: echo no-op"
    skip = 1
    next
  }
  skip && /^      - name:/ { skip = 0 }
  skip { next }
  { print }
' "$ROOT/.github/workflows/test.yml" > "$FIXTURE_EMPTY"
OUT=$(env -i PATH="$PATH" CI_ENV_DRIFT_WORKFLOW_OVERRIDE="$FIXTURE_EMPTY" \
  DATABASE_URL="x" CACHE_REDIS_TEST_URL="x" REDIS_TEST_URL="x" \
  bash "$SCRIPT" 2>&1)
CODE=$?
if [ "$CODE" -ne 0 ] && printf '%s' "$OUT" | grep -q "extracted zero variable names"; then
  pass "fails closed with 'extracted zero variable names' when the guard step body is empty"
else
  fail "expected fail-closed 'extracted zero variable names', got exit=$CODE output=$OUT"
fi

echo ""
echo "Test 6: fixture workflow whose guard step checks a FOURTH var -> picked up automatically"
FIXTURE_FOURTH="$SANDBOX/fourth-var.yml"
sed 's/if \[ -z "\$REDIS_TEST_URL" \]; then/if [ -z "$REDIS_TEST_URL" ]; then\n            MISSING="${MISSING}PLAID_TEST_TOKEN "\n          fi\n          if [ -z "$PLAID_TEST_TOKEN" ]; then/' \
  "$ROOT/.github/workflows/test.yml" > "$FIXTURE_FOURTH"
OUT=$(env -i PATH="$PATH" CI_ENV_DRIFT_WORKFLOW_OVERRIDE="$FIXTURE_FOURTH" \
  DATABASE_URL="x" CACHE_REDIS_TEST_URL="x" REDIS_TEST_URL="x" \
  bash "$SCRIPT" 2>&1)
CODE=$?
if [ "$CODE" -ne 0 ] && printf '%s' "$OUT" | grep -q "PLAID_TEST_TOKEN"; then
  pass "picks up a fourth var added to CI's guard step without any edit to this script"
else
  fail "expected failure naming the injected fourth var PLAID_TEST_TOKEN, got exit=$CODE output=$OUT"
fi

echo ""
echo "Test 7: a CI-only var (GO_VERSION) unset locally must NOT trigger a failure"
OUT=$(env -i PATH="$PATH" \
  DATABASE_URL="postgres://x" CACHE_REDIS_TEST_URL="redis://x" REDIS_TEST_URL="redis://x" \
  bash "$SCRIPT" 2>&1)
CODE=$?
if [ "$CODE" -eq 0 ] && ! printf '%s' "$OUT" | grep -q "GO_VERSION"; then
  pass "does not fire on GO_VERSION (a legitimately CI-only var), even though it is unset here"
else
  fail "expected exit 0 with no mention of GO_VERSION, got exit=$CODE output=$OUT"
fi

echo ""
echo "========================================"
echo "Results: $PASS passed, $FAIL failed"
echo "========================================"
if [ "$FAIL" -gt 0 ]; then
  exit 1
fi
exit 0
