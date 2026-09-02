#!/usr/bin/env bash
# Regression test for the rollback clean-state check in scripts/test-migrations.sh
# (extract_object_identities / get_full_object_inventory's pure text transform / assert_clean_rollback_state
# / is_allowlisted_residue / allowlist_reason_for).
#
# Run: bash scripts/__tests__/rollback-residue.test.sh
# Exit 0 on all assertions passing, non-zero on any failure.
#
# This sources scripts/test-migrations.sh (which requires the BASH_SOURCE guard at that file's
# bottom to avoid running main() on source) to call its pure, no-database functions directly with
# SYNTHETIC inventory strings. No Docker, no Postgres, no migrate CLI involved — this is testing the
# residue-comparison LOGIC in isolation, not the real migration set.
#
# Why synthetic inventories instead of a real database: assert_clean_rollback_state's whole point is
# to behave correctly under TWO provenances for an allowlisted item (absent before the first `up`,
# vs. already installed before it) — see that function's own comment in test-migrations.sh. The
# live suite's clean_database() always does `DROP SCHEMA public CASCADE`, which (verified
# empirically while building this) always cascade-drops any pre-existing extension too — so the
# "already installed" provenance can never actually arise from a real run of this suite. Testing it
# here, directly against the pure comparison function, is the only way to exercise that path at all.
set -uo pipefail

# PROSE-ASSERTION AUDIT (#1569 task 7.1). "unexpected residue not on the allowlist", "is stale",
# and the "FUNCTION update_updated_at_column()" / "EXTENSION pg_trgm" object-name checks below are
# kept verbatim. Decision: CONTRACT for the first two — the guard's entire job is to say exactly
# "unexpected residue" / "is stale" when it rejects, so that text IS the thing under test, not
# incidental narration. The object-name strings are literal Postgres identifiers pulled from a
# real inventory diff, not prose the author could word differently — nothing to loosen. None of
# these describe a computed scope the way the typecheck gate's "in test files" did, so none carry
# that failure mode. Left unchanged.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCRIPT="$SCRIPT_DIR/test-migrations.sh"

PASS=0
FAIL=0
pass() { echo "  ✅ PASS: $1"; PASS=$((PASS + 1)); }
fail() { echo "  ❌ FAIL: $1"; FAIL=$((FAIL + 1)); }

echo "=== rollback-residue test suite ==="
echo ""

# Dummy DATABASE_URL so sourcing test-migrations.sh doesn't abort at its top-level
# `DATABASE_URL="${DATABASE_URL:?...}"` guard. Never actually connected to — every function
# exercised below is pure text/array manipulation, no psql/pg_dump/migrate call happens here.
export DATABASE_URL="postgres://unused:unused@127.0.0.1:1/unused?sslmode=disable"

# shellcheck source=/dev/null
source "$SCRIPT"

echo "--- extract_object_identities (pure text transform) ---"
echo ""

SAMPLE_DUMP='--
-- Name: public; Type: SCHEMA; Schema: -; Owner: -
--

--
-- Name: pg_trgm; Type: EXTENSION; Schema: -; Owner: -
--

--
-- Name: EXTENSION pg_trgm; Type: COMMENT; Schema: -; Owner: -
--

--
-- Name: update_updated_at_column(); Type: FUNCTION; Schema: public; Owner: -
--

--
-- Name: users; Type: TABLE; Schema: public; Owner: -
--
'

identities="$(extract_object_identities "$SAMPLE_DUMP")"

check_contains() {
  local desc="$1" needle="$2"
  # Here-string, not `printf | grep -q`: under `pipefail` (inherited from sourcing
  # test-migrations.sh), a grep -q that exits on an early match can SIGPIPE a still-writing
  # producer and make pipefail report the whole pipeline as failed even though grep found it.
  if grep -qxF "$needle" <<< "$identities"; then
    pass "$desc"
  else
    fail "$desc — expected '$needle' in: $identities"
  fi
}
check_not_contains() {
  local desc="$1" needle="$2"
  if grep -qxF "$needle" <<< "$identities"; then
    fail "$desc — did NOT expect '$needle' in: $identities"
  else
    pass "$desc"
  fi
}

check_contains "EXTENSION pg_trgm is captured" "EXTENSION pg_trgm"
check_contains "FUNCTION update_updated_at_column() is captured" "FUNCTION update_updated_at_column()"
check_contains "TABLE users is captured" "TABLE users"
check_not_contains "COMMENT-type header (EXTENSION pg_trgm's auto-comment) is excluded" "COMMENT EXTENSION pg_trgm"
check_not_contains "SCHEMA public header is not mistaken for a table/extension identity" "TABLE public"

echo ""
echo "--- assert_clean_rollback_state (provenance cases) ---"
echo ""

# Case 1: pg_trgm absent before the first `up`, present after rollback (the normal, deliberate
# residue case — 000101_merchant_name_handling.down.sql doesn't drop it).
if assert_clean_rollback_state "" "EXTENSION pg_trgm"; then
  pass "Case 1 (pg_trgm absent before, present after): passes — matches the allowlist"
else
  fail "Case 1 (pg_trgm absent before, present after): should pass, but failed"
fi

# Case 2: pg_trgm ALREADY installed before the first `up` (ambient state on a shared DB), still
# present after rollback. The diff (post - pre) is EMPTY here — nothing NEW was left behind — so a
# naive "diff must equal the allowlist" check would wrongly fail this; the two-check design must not.
if assert_clean_rollback_state "EXTENSION pg_trgm" "EXTENSION pg_trgm"; then
  pass "Case 2 (pg_trgm already present before AND after): passes — not new residue"
else
  fail "Case 2 (pg_trgm already present before AND after): should pass (ambient, not new), but failed"
fi

echo ""
echo "--- assert_clean_rollback_state (RED: catches real residue) ---"
echo ""

# This is the exact scenario Task S3-1/S3-2 exist for: update_updated_at_column() surviving a
# rollback (000001_create_users_table.down.sql, before it was fixed to drop it) — NOT allowlisted,
# so this must fail.
#
# NOTE: `if output=$(cmd); then ... else status=$?; fi` (non-negated, NOT a bare `output=$(cmd)`)
# is used deliberately here, not out of style preference — sourcing test-migrations.sh above pulled
# its top-level `set -euo pipefail` into THIS shell too, and a BARE `output=$(assert_clean_rollback_state
# ...)` assignment expected to return non-zero would abort this entire test script right there
# (the exact Task S3-6 masking/dead-code trap this batch fixes elsewhere — caught by writing this
# test file, not just by inspection).
if output=$(assert_clean_rollback_state "" "FUNCTION update_updated_at_column()" 2>&1); then
  status=0
else
  status=$?
fi
if [ "$status" -ne 0 ] && printf '%s' "$output" | grep -qi "unexpected residue not on the allowlist"; then
  pass "unallowlisted new residue (the update_updated_at_column bug) is caught"
else
  fail "expected rejection of unallowlisted residue, got exit=$status output=$output"
fi

# Combined: one allowlisted (fine) + one not (must still fail overall, and name the right one).
if output=$(assert_clean_rollback_state "" "$(printf 'EXTENSION pg_trgm\nFUNCTION update_updated_at_column()')" 2>&1); then
  status=0
else
  status=$?
fi
if [ "$status" -ne 0 ] && printf '%s' "$output" | grep -q "FUNCTION update_updated_at_column()" && ! printf '%s' "$output" | grep -q "EXTENSION pg_trgm.*allowlist\|unexpected.*EXTENSION pg_trgm"; then
  pass "mixed allowlisted+unallowlisted residue: fails overall, flags only the unallowlisted one"
else
  fail "expected failure naming FUNCTION update_updated_at_column() without flagging EXTENSION pg_trgm as unexpected, got exit=$status output=$output"
fi

echo ""
echo "--- assert_clean_rollback_state (RED: catches a stale allowlist entry) ---"
echo ""

# pg_trgm allowlisted but NOT actually present after rollback in either provenance — the allowlist
# entry no longer reflects reality (e.g. if 000101's down.sql were later fixed to drop it) and must
# be flagged as stale rather than silently accepted.
if output=$(assert_clean_rollback_state "" "" 2>&1); then
  status=0
else
  status=$?
fi
if [ "$status" -ne 0 ] && printf '%s' "$output" | grep -qi "is stale"; then
  pass "a stale allowlist entry (pg_trgm allowlisted but absent post-rollback) is caught"
else
  fail "expected a 'stale' rejection, got exit=$status output=$output"
fi

echo ""
echo "--- is_allowlisted_residue / allowlist_reason_for ---"
echo ""

if is_allowlisted_residue "EXTENSION pg_trgm"; then
  pass "EXTENSION pg_trgm is allowlisted"
else
  fail "EXTENSION pg_trgm should be allowlisted"
fi

if is_allowlisted_residue "FUNCTION update_updated_at_column()"; then
  fail "FUNCTION update_updated_at_column() must NOT be allowlisted (Task S3-1/S3-2's whole point)"
else
  pass "FUNCTION update_updated_at_column() is correctly NOT allowlisted"
fi

reason="$(allowlist_reason_for "EXTENSION pg_trgm")"
if [ -n "$reason" ]; then
  pass "allowlist_reason_for returns a non-empty reason: $reason"
else
  fail "allowlist_reason_for returned an empty reason for an allowlisted entry"
fi

echo ""
echo "=== Results: $PASS passed, $FAIL failed ==="
[ "$FAIL" -eq 0 ]
