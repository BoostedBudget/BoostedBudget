#!/usr/bin/env bash
# Regression test for the destructive-target guard in scripts/test-migrations.sh (#1589).
# Run: bash scripts/__tests__/test-migrations-target-guard.test.sh
# Exit 0 on all assertions passing, non-zero on any failure.
#
# What this pins, and why it is constructed rather than asserted:
#
# scripts/test-migrations.sh runs `DROP SCHEMA public CASCADE` and `migrate down -all` against
# whatever DATABASE_URL names. Before #1589 the ONLY thing it checked about that database was that
# the variable was non-empty, so one wrong `export` destroyed the developer's dev database before
# any test output appeared. The guard added by #1589 refuses unless the database name is
# recognisably disposable, or the caller passes `--yes-destroy <name>` that EQUALS the name in
# DATABASE_URL.
#
# ⚠ Every DATABASE_URL used below points at 127.0.0.1 port 1, which nothing listens on. That is
# deliberate: if the guard regresses to a no-op, these cases must not be able to destroy a real
# database — they fail at the connectivity preflight instead, which is exactly what the mutant
# case at the bottom observes. Never point a case in this file at a reachable server.
#
# ⚠ The guard must run BEFORE the connectivity preflight, or these cases cannot distinguish
# "refused the target" from "could not connect". Case 1 asserts the refusal exit code (3) and the
# refusal text, neither of which a connectivity failure (exit 1) produces.
set -euo pipefail

SCRIPT="$(cd "$(dirname "$0")/.." && pwd)/test-migrations.sh"
PASS=0
FAIL=0

pass() { echo "  ✅ PASS: $1"; PASS=$((PASS + 1)); }
fail() { echo "  ❌ FAIL: $1"; FAIL=$((FAIL + 1)); }

SANDBOX=$(mktemp -d)
trap 'rm -rf "$SANDBOX"' EXIT

# An unreachable server. Nothing this file runs can touch a real database.
url_for() { printf 'postgres://nobody:nobody@127.0.0.1:1/%s?sslmode=disable' "$1"; }

# Runs the suite and records exit status + combined output in RUN_STATUS / RUN_OUTPUT.
run_suite() {
  local db="$1"; shift
  set +e
  RUN_OUTPUT=$(DATABASE_URL="$(url_for "$db")" bash "${SUITE:-$SCRIPT}" "$@" 2>&1)
  RUN_STATUS=$?
  set -e
}

echo "=== test-migrations.sh destructive-target guard test suite ==="
echo ""

# ── Test 1: a non-disposable database is REFUSED, before anything can connect ──
echo "Test 1: refuses a non-disposable target (boostedbudget)"
run_suite boostedbudget
if [ "$RUN_STATUS" -ne 3 ]; then
  fail "expected exit 3 (guard refusal), got $RUN_STATUS — output: $RUN_OUTPUT"
elif ! echo "$RUN_OUTPUT" | grep -q "Refusing to run"; then
  fail "exit 3 but no refusal message — output: $RUN_OUTPUT"
elif ! echo "$RUN_OUTPUT" | grep -q "boostedbudget"; then
  fail "refusal does not name the database it refused — output: $RUN_OUTPUT"
elif echo "$RUN_OUTPUT" | grep -q "Prerequisites checked"; then
  fail "guard let the run reach the preflight — output: $RUN_OUTPUT"
else
  pass "exits 3, names boostedbudget, never reaches the preflight"
fi

# ── Test 2: the allowlisted scratch database is permitted past the guard ──
echo "Test 2: permits the allowlisted scratch database (freshcheck)"
run_suite freshcheck
if echo "$RUN_OUTPUT" | grep -q "Refusing to run"; then
  fail "guard refused an allowlisted database — output: $RUN_OUTPUT"
elif [ "$RUN_STATUS" -eq 3 ]; then
  fail "exited with the guard's refusal code for an allowlisted database"
else
  pass "freshcheck passes the guard (run then fails later on the unreachable server, as designed)"
fi

# ── Test 3: CI's database name (`test`) is permitted ──
# .github/workflows/test.yml builds DATABASE_URL as .../test?sslmode=disable and runs this suite
# with no arguments. If `test` were not allowlisted the guard would break CI, so pin it here.
echo "Test 3: permits CI's database name (test)"
run_suite test
if echo "$RUN_OUTPUT" | grep -q "Refusing to run"; then
  fail "guard would break CI: refused the database name CI uses — output: $RUN_OUTPUT"
else
  pass "CI's 'test' database passes the guard"
fi

# ── Test 4: --yes-destroy naming the SAME database unlocks a non-disposable target ──
echo "Test 4: --yes-destroy <matching name> unlocks a non-disposable target"
run_suite boostedbudget --yes-destroy boostedbudget
if echo "$RUN_OUTPUT" | grep -q "Refusing to run"; then
  fail "matching --yes-destroy was still refused — output: $RUN_OUTPUT"
elif [ "$RUN_STATUS" -eq 3 ]; then
  fail "matching --yes-destroy still exited with the guard refusal code"
else
  pass "explicit matching confirmation is honoured"
fi

# ── Test 5: --yes-destroy naming a DIFFERENT database is refused ──
# This is the typo case the flag exists to catch: the developer means to destroy the scratch
# database, has DATABASE_URL still pointing at the dev one, and types the name they intended.
echo "Test 5: --yes-destroy naming a different database is refused"
run_suite boostedbudget --yes-destroy freshcheck
if [ "$RUN_STATUS" -ne 3 ]; then
  fail "expected exit 3 for a mismatched --yes-destroy, got $RUN_STATUS — output: $RUN_OUTPUT"
elif ! echo "$RUN_OUTPUT" | grep -q "does not match"; then
  fail "refusal does not explain the mismatch — output: $RUN_OUTPUT"
else
  pass "mismatched confirmation is refused, not silently accepted"
fi

# ── Test 6: --yes-destroy=<name> form works too ──
echo "Test 6: --yes-destroy=<name> equals form is accepted"
run_suite boostedbudget --yes-destroy=boostedbudget
if echo "$RUN_OUTPUT" | grep -q "Refusing to run"; then
  fail "--yes-destroy=<name> form was refused — output: $RUN_OUTPUT"
else
  pass "--yes-destroy=<name> form honoured"
fi

# ── Test 7: an unrecognised argument is an error, not silently ignored ──
# A silently-ignored argument is how `--yes-destroy` could be mistyped into a no-op that leaves the
# guard armed but the developer believing they had confirmed.
echo "Test 7: unknown argument is rejected"
run_suite freshcheck --yolo
if [ "$RUN_STATUS" -eq 0 ]; then
  fail "unknown argument was accepted"
elif ! echo "$RUN_OUTPUT" | grep -qi "unknown argument"; then
  fail "unknown argument rejected but not explained — output: $RUN_OUTPUT"
else
  pass "unknown argument rejected with an explanation"
fi

# ── Test 8: DATABASE_URL with no database name is refused ──
echo "Test 8: refuses a DATABASE_URL with no database name"
set +e
RUN_OUTPUT=$(DATABASE_URL="postgres://nobody:nobody@127.0.0.1:1/?sslmode=disable" bash "$SCRIPT" 2>&1)
RUN_STATUS=$?
set -e
if [ "$RUN_STATUS" -ne 3 ]; then
  fail "expected exit 3 for a nameless DATABASE_URL, got $RUN_STATUS — output: $RUN_OUTPUT"
else
  pass "a DATABASE_URL naming no database is refused"
fi

# ── Test 9 (mutant): removing the guard call makes Test 1 stop refusing ──
# Per feedback_validate_the_mutant_not_just_the_red: a green suite proves nothing until the
# mutation is shown to (a) actually change behaviour and (b) fail Test 1's assertion. The mutation
# deletes the single call site, leaving the function defined but never invoked — the exact
# regression a careless refactor produces.
echo "Test 9 (mutant): deleting the guard call re-opens the hazard"
MUTANT="$SANDBOX/test-migrations.mutant.sh"
grep -v '^[[:space:]]*assert_target_is_disposable$' "$SCRIPT" > "$MUTANT"
if ! diff -q "$SCRIPT" "$MUTANT" > /dev/null 2>&1; then
  SUITE="$MUTANT" run_suite boostedbudget
  if [ "$RUN_STATUS" -eq 3 ] || echo "$RUN_OUTPUT" | grep -q "Refusing to run"; then
    fail "mutant still refuses — Test 1 is not actually bound to the guard call"
  else
    pass "mutant reaches past the guard (exit $RUN_STATUS), so Test 1's refusal is load-bearing"
  fi
else
  fail "mutation was a no-op: no bare 'assert_target_is_disposable' call line found in $SCRIPT"
fi

# ── Test 10: the allowlist pattern cannot be widened from the environment ──
# An env-overridable pattern is the same defect as an opt-out env var: one `export` in a shell
# profile disarms the guard for every future run from that shell, invisibly and permanently. The
# only widening path must be editing this repository, where it is reviewable.
echo "Test 10: DISPOSABLE_DB_PATTERN is not environment-overridable"
set +e
RUN_OUTPUT=$(DISPOSABLE_DB_PATTERN='.' DATABASE_URL="$(url_for boostedbudget)" bash "$SCRIPT" 2>&1)
RUN_STATUS=$?
set -e
if [ "$RUN_STATUS" -ne 3 ]; then
  fail "an exported DISPOSABLE_DB_PATTERN disarmed the guard (exit $RUN_STATUS) — output: $RUN_OUTPUT"
else
  pass "exported DISPOSABLE_DB_PATTERN is ignored; boostedbudget still refused"
fi

echo ""
echo "=== Results: $PASS passed, $FAIL failed ==="
[ $FAIL -eq 0 ]
