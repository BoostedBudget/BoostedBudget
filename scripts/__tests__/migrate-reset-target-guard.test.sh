#!/usr/bin/env bash
# Regression test for backend/scripts/assert-migrate-reset-target-disposable.sh (#1569 round 12
# task 7), the entry-point guard on `make migrate-reset` (backend/Makefile:~82's
# `migrate ... down -all`).
#
# WHY THIS GUARD EXISTS, and why it lives at the ENTRY POINT rather than in another per-migration
# refusal: #1569 round 12 added 12 per-file rollback refusal guards (one of them
# 000151_create_account_status_history.down.sql, which refuses to DROP TABLE while the table holds
# rows). Codex then found the whole class defeated: `migrate down -all` runs migrations in REVERSE
# order, so 000152's down.sql (an unconditional `DELETE FROM account_status_history;`) empties the
# table BEFORE 000151's guard ever counts a row. Every one of the 12 guards passed in isolation
# (seeded directly); nobody tested the SEQUENCE. The user's ruling was to guard the ONE command that
# can run the destructive sequence at all — this file — rather than add a refusal to every
# destructive down-migration (including the four foundational DROP TABLEs in 000007/000010/000018/
# 000022, which carry no per-file guard and never will).
#
# Run: bash scripts/__tests__/migrate-reset-target-guard.test.sh
# Exit 0 on all assertions passing, non-zero on any failure. Needs a live local Postgres (this
# suite creates its own throwaway `mrguard_*` databases and drops them in the EXIT trap); it is
# SKIPPED with a visible warning by `make test-scripts` when none is reachable locally. CI always
# has a live Postgres and runs it unconditionally.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
GUARD="$REPO_ROOT/backend/scripts/assert-migrate-reset-target-disposable.sh"
PASS=0
FAIL=0

pass() { echo "  ✅ PASS: $1"; PASS=$((PASS + 1)); }
fail() { echo "  ❌ FAIL: $1"; FAIL=$((FAIL + 1)); }

if [ ! -x "$GUARD" ]; then
  fail "guard script not found or not executable at $GUARD"
  echo ""
  echo "=== Results: $PASS passed, $FAIL failed ==="
  exit 1
fi

PROBE_ADMIN_URL=""
PROBE_HOSTPORT=""

# Same discovery order as scripts/__tests__/test-migrations-target-guard.test.sh: an explicit
# GUARD_TEST_ADMIN_URL, the caller's own DATABASE_URL rewritten to the `postgres` maintenance
# database, then the two ports this repo actually uses locally.
discover_probe_cluster() {
  local candidate rewritten
  local -a candidates=()
  [ -n "${GUARD_TEST_ADMIN_URL:-}" ] && candidates+=("$GUARD_TEST_ADMIN_URL")
  if [ -n "${DATABASE_URL:-}" ]; then
    rewritten="${DATABASE_URL%%\?*}"
    rewritten="${rewritten%/*}/postgres?sslmode=disable"
    candidates+=("$rewritten")
  fi
  candidates+=("postgres://devuser:devpass@localhost:5434/postgres?sslmode=disable")
  candidates+=("postgres://devuser:devpass@localhost:5432/postgres?sslmode=disable")
  for candidate in "${candidates[@]}"; do
    if psql "$candidate" -tAc 'SELECT 1' > /dev/null 2>&1; then
      PROBE_ADMIN_URL="$candidate"
      local stripped="${candidate#*://}"
      PROBE_HOSTPORT="${stripped%%/*}"
      return 0
    fi
  done
  return 1
}

probe_url() { printf 'postgres://%s/%s?sslmode=disable' "$PROBE_HOSTPORT" "$1"; }

PROBE_DBS=(mrguard_empty mrguard_marked mrguard_populated mrguard_matview mrguard_confirm mrguard_sequence mrguard_wiring_mutant mrguard_roundtrip)

drop_probe_dbs() {
  [ -n "$PROBE_ADMIN_URL" ] || return 0
  local db
  for db in "${PROBE_DBS[@]}"; do
    psql "$PROBE_ADMIN_URL" -c "DROP DATABASE IF EXISTS $db WITH (FORCE)" > /dev/null 2>&1 || true
  done
}

trap 'drop_probe_dbs' EXIT

echo "=== migrate-reset entry-point guard test suite (#1569 round 12 task 7) ==="
echo ""

if ! command -v psql > /dev/null 2>&1; then
  fail "psql is not on PATH — this suite cannot run without it."
  echo ""
  echo "=== Results: $PASS passed, $FAIL failed ==="
  exit 1
fi

# Tests 10-12 below invoke the REAL `make migrate-reset` / real `migrate` binary end to end — this
# is the fix for #1569 round 12 task 7 fix round 1 finding 3 (Codex): every earlier test in this
# file called the guard SCRIPT directly, so no automated test would have noticed if a future
# Makefile edit reordered or dropped the guard invocation line — exactly the class of defect that
# let the original 000152->000151 bypass survive round 12. `migrate` is not needed for anything
# else in this file, so it is checked here rather than at the top.
if ! command -v migrate > /dev/null 2>&1; then
  echo "⚠️  SKIPPED — golang-migrate ('migrate') is not on PATH; Tests 10-12 (real end-to-end wiring) cannot run without it."
  echo "    brew install golang-migrate to run them locally. CI always installs it before this step."
  MIGRATE_AVAILABLE=false
else
  MIGRATE_AVAILABLE=true
fi

if ! discover_probe_cluster; then
  echo "⚠️  SKIPPED — no live Postgres reachable (tried GUARD_TEST_ADMIN_URL, DATABASE_URL, localhost:5434, localhost:5432)."
  echo "    Start the dev database (make dev) to run this suite locally. CI always has a live Postgres."
  exit 0
fi
echo "(probe cluster: $PROBE_HOSTPORT)"
drop_probe_dbs

# ── Test 1: DATABASE_URL unset -> exit 1 (not the guard-refusal exit 3) ──
echo "Test 1: refuses to run at all without DATABASE_URL, distinctly from a guard refusal"
set +e
OUT=$(env -u DATABASE_URL bash "$GUARD" 2>&1)
STATUS=$?
set -e
if [ "$STATUS" -eq 1 ]; then
  pass "missing DATABASE_URL exits 1, distinct from guard-refusal exit 3"
else
  fail "expected exit 1 for missing DATABASE_URL, got $STATUS — output: $OUT"
fi

# ── Test 2: an empty database is accepted and stamped ──
echo "Test 2: accepts and stamps a genuinely empty database"
psql "$PROBE_ADMIN_URL" -c 'CREATE DATABASE mrguard_empty' > /dev/null
set +e
OUT=$(DATABASE_URL="$(probe_url mrguard_empty)" bash "$GUARD" 2>&1)
STATUS=$?
set -e
if [ "$STATUS" -ne 0 ]; then
  fail "expected exit 0 for an empty database, got $STATUS — output: $OUT"
elif ! echo "$OUT" | grep -q "stamped it as a disposable"; then
  fail "accepted but did not report stamping — output: $OUT"
else
  comment=$(psql "$(probe_url mrguard_empty)" -tAc "SELECT shobj_description(oid, 'pg_database') FROM pg_database WHERE datname = current_database()")
  if [[ "$comment" == *"disposable-migrate-reset-target"* ]]; then
    pass "empty database accepted and stamped with the disposable marker"
  else
    fail "guard reported stamping but the marker is not actually on the database: '$comment'"
  fi
fi

# ── Test 3: a database already carrying the marker is accepted with no re-derivation ──
echo "Test 3: accepts a database already marked disposable"
set +e
OUT=$(DATABASE_URL="$(probe_url mrguard_empty)" bash "$GUARD" 2>&1)
STATUS=$?
set -e
if [ "$STATUS" -ne 0 ]; then
  fail "expected exit 0 for an already-marked database, got $STATUS — output: $OUT"
elif ! echo "$OUT" | grep -q "is stamped as a disposable"; then
  fail "accepted but did not report the existing stamp — output: $OUT"
else
  pass "a marked database is accepted without re-deriving emptiness"
fi

# ── Test 4: a populated, unmarked database is REFUSED ──
echo "Test 4: refuses a populated database with no disposability marker"
psql "$PROBE_ADMIN_URL" -c 'CREATE DATABASE mrguard_populated' > /dev/null
psql "$(probe_url mrguard_populated)" -c 'CREATE TABLE precious (id int); INSERT INTO precious VALUES (1)' > /dev/null
set +e
OUT=$(DATABASE_URL="$(probe_url mrguard_populated)" bash "$GUARD" 2>&1)
STATUS=$?
set -e
precious_count=$(psql "$(probe_url mrguard_populated)" -tAc 'SELECT count(*) FROM precious' | tr -d '[:space:]')
if [ "$STATUS" -ne 3 ]; then
  fail "expected exit 3 (guard refusal) for a populated database, got $STATUS — output: $OUT"
elif ! echo "$OUT" | grep -q "Refusing to run"; then
  fail "exit 3 but no refusal message — output: $OUT"
elif [ "$precious_count" != "1" ]; then
  fail "THE GUARD ITSELF ALTERED THE PROBE DATABASE — 'precious' no longer holds its row"
else
  pass "a populated, unmarked database is refused, and nothing in it is touched"
fi

# ── Test 5: the refusal names BOTH remediation paths ──
echo "Test 5: refusal message names both the stamp path and the MIGRATE_RESET_CONFIRM path"
if ! echo "$OUT" | grep -q "COMMENT ON DATABASE"; then
  fail "refusal does not mention the stamp-it-once remediation — output: $OUT"
elif ! echo "$OUT" | grep -q "MIGRATE_RESET_CONFIRM=mrguard_populated"; then
  fail "refusal does not name the exact MIGRATE_RESET_CONFIRM invocation for this database — output: $OUT"
else
  pass "refusal message tells the operator exactly how to proceed, both ways"
fi

# ── Test 6: MIGRATE_RESET_CONFIRM naming a DIFFERENT database does not unlock this one ──
echo "Test 6: a MIGRATE_RESET_CONFIRM for a different database does not unlock this one"
set +e
OUT=$(MIGRATE_RESET_CONFIRM=some_other_db DATABASE_URL="$(probe_url mrguard_populated)" bash "$GUARD" 2>&1)
STATUS=$?
set -e
if [ "$STATUS" -ne 3 ]; then
  fail "a mismatched MIGRATE_RESET_CONFIRM unlocked the guard (exit $STATUS) — output: $OUT"
else
  pass "MIGRATE_RESET_CONFIRM must name THIS database, not just be set"
fi

# ── Test 7: MIGRATE_RESET_CONFIRM naming THIS database unlocks it, this run only ──
echo "Test 7: MIGRATE_RESET_CONFIRM=<this db> unlocks a populated database for one run"
set +e
OUT=$(MIGRATE_RESET_CONFIRM=mrguard_populated DATABASE_URL="$(probe_url mrguard_populated)" bash "$GUARD" 2>&1)
STATUS=$?
set -e
if [ "$STATUS" -ne 0 ]; then
  fail "expected exit 0 with a matching MIGRATE_RESET_CONFIRM, got $STATUS — output: $OUT"
else
  # It must NOT have stamped the database — this is a one-run confirmation, not a permanent one.
  comment=$(psql "$(probe_url mrguard_populated)" -tAc "SELECT coalesce(shobj_description(oid, 'pg_database'), '') FROM pg_database WHERE datname = current_database()")
  if [[ "$comment" == *"disposable-migrate-reset-target"* ]]; then
    fail "MIGRATE_RESET_CONFIRM permanently stamped the database — it must only cover the one invocation"
  else
    pass "MIGRATE_RESET_CONFIRM unlocks exactly one run and does not stamp the database"
  fi
fi

# ── Test 8: a database holding ONLY a materialized view is REFUSED (the Codex-found gap) ──
# A sibling check in scripts/test-migrations.sh used to count relkind IN ('r','p') only, so a
# database holding just a materialized view was judged empty and destroyed. This guard's relkind
# list includes 'm' from the start; this case pins that it stays that way.
echo "Test 8: refuses a database holding only a materialized view (Codex finding)"
psql "$PROBE_ADMIN_URL" -c 'CREATE DATABASE mrguard_matview' > /dev/null
psql "$(probe_url mrguard_matview)" -c 'CREATE MATERIALIZED VIEW mv AS SELECT 1 AS x' > /dev/null
set +e
OUT=$(DATABASE_URL="$(probe_url mrguard_matview)" bash "$GUARD" 2>&1)
STATUS=$?
set -e
if [ "$STATUS" -ne 3 ]; then
  fail "a database holding only a materialized view was judged empty (exit $STATUS) — output: $OUT"
else
  pass "a materialized-view-only database counts as holding user data and is refused"
fi

# ── Test 9 (mutant): narrowing the relkind list back to ('r','p') re-opens Test 8's hazard ──
# Per feedback_validate_the_mutant_not_just_the_red: prove Test 8 is bound to the relkind list, not
# to some incidental property of the probe database.
echo "Test 9 (mutant): narrowing relkind back to ('r','p') re-opens the materialized-view hazard"
MUTANT="$REPO_ROOT/scripts/__tests__/.migrate-reset-guard-mutant.sh"
sed "s/relkind IN ('r','p','m')/relkind IN ('r','p')/" "$GUARD" > "$MUTANT"
chmod +x "$MUTANT"
if diff -q "$GUARD" "$MUTANT" > /dev/null 2>&1; then
  fail "mutation was a no-op: relkind list not found in $GUARD"
else
  set +e
  MOUT=$(DATABASE_URL="$(probe_url mrguard_matview)" bash "$MUTANT" 2>&1)
  MSTATUS=$?
  set -e
  rm -f "$MUTANT"
  if [ "$MSTATUS" -eq 0 ]; then
    pass "mutant wrongly accepts the materialized-view-only database — Test 8 is bound to the 'm' relkind"
  else
    fail "mutant still refused (exit $MSTATUS) — Test 8 is not actually bound to the relkind list narrowing"
  fi
fi

# ── Test 10: a fully round-tripped database (migrate up, then down -all) is auto-accepted ──
# #1569 round 12 task 7 fix round 1 finding 2 (Codex): golang-migrate's schema_migrations table is
# an ordinary 'r' table and `down -all` does NOT drop it. Before excluding it BY NAME from the
# relation count, this meant ANY database that had ever run `migrate up` reported >=1 user relation
# forever — including one that had just been fully rolled back — so the "empty database, no
# ceremony" path this guard's own header presents as the common case was actually unreachable in
# the normal migrate-up/migrate-reset loop. This proves it is reachable now, with the real binary.
echo "Test 10: a database that ran migrate up then migrate down -all is judged empty (schema_migrations exclusion)"
if [ "$MIGRATE_AVAILABLE" = true ]; then
  psql "$PROBE_ADMIN_URL" -c 'CREATE DATABASE mrguard_roundtrip' > /dev/null
  migrate -path "$REPO_ROOT/backend/migrations" -database "$(probe_url mrguard_roundtrip)" up > /dev/null 2>&1
  migrate -path "$REPO_ROOT/backend/migrations" -database "$(probe_url mrguard_roundtrip)" down -all > /dev/null 2>&1
  set +e
  OUT=$(DATABASE_URL="$(probe_url mrguard_roundtrip)" bash "$GUARD" 2>&1)
  STATUS=$?
  set -e
  if [ "$STATUS" -ne 0 ]; then
    fail "a database holding only schema_migrations after a full round trip was refused (exit $STATUS) — output: $OUT"
  elif ! echo "$OUT" | grep -q "stamped it as a disposable"; then
    fail "accepted but did not report stamping — output: $OUT"
  else
    pass "schema_migrations alone does not block the empty-database no-ceremony path"
  fi
else
  echo "  (skipped — migrate not available)"
fi

# Seeds a database with the REAL migration chain up to HEAD, then a real user/linked_item/account
# row so the account_status_history insert trigger (000151/000152) leaves exactly one live row —
# the same precondition the 000152->000151 bypass needed to be observable at all.
seed_full_chain_probe_db() {
  local name="$1"
  psql "$PROBE_ADMIN_URL" -c "CREATE DATABASE $name" > /dev/null
  migrate -path "$REPO_ROOT/backend/migrations" -database "$(probe_url "$name")" up > /dev/null 2>&1
  psql "$(probe_url "$name")" -v ON_ERROR_STOP=1 > /dev/null <<SQL
INSERT INTO users (id, clerk_user_id, email, subscription_tier)
  VALUES ('11111111-1111-1111-1111-111111111111', 'clerk_$name', '$name@example.com', 'premium');
INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
  VALUES ('22222222-2222-2222-2222-222222222222', '11111111-1111-1111-1111-111111111111', 'item_$name', 'enc_$name');
INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type)
  VALUES ('33333333-3333-3333-3333-333333333333', '22222222-2222-2222-2222-222222222222',
          '11111111-1111-1111-1111-111111111111', 'plaidacct_$name', 'Seq Test Checking', 'depository');
SQL
}

# Counts relations using the SAME "has user data" definition the guard itself uses
# (relkind IN ('r','p','m'), excluding public.schema_migrations) — not every pg_class row. An
# earlier version of this helper counted ALL relkinds, which included
# schema_migrations_pkey (relkind 'i', the primary key index ON schema_migrations) as "remaining
# data" even though the guard's own emptiness check — and this test's whole point — has never
# treated an index as user data. Mirroring the guard's definition here means this check can only
# ever disagree with the guard about what "clean" means by actually diverging in behavior, not by
# counting something the guard was never asked to count.
count_non_bookkeeping_relations() {
  psql "$(probe_url "$1")" -tAc \
    "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE c.relkind IN ('r','p','m') AND n.nspname = 'public' AND NOT (c.relname = 'schema_migrations')" \
    2>/dev/null | tr -d '[:space:]'
}

if [ "$MIGRATE_AVAILABLE" = true ]; then
  seed_full_chain_probe_db mrguard_sequence

  # ── Test 11: the REAL `make migrate-reset` refuses — not the script called directly ──
  # This is the automated proof the manual verification in the round's report stood in for: the
  # 000152->000151 bypass class is closed through the ACTUAL Makefile wiring, not just through the
  # guard script in isolation. A future edit that reorders or deletes the guard invocation line in
  # backend/Makefile's migrate-reset recipe fails THIS test (and Test 13's mutant proves it).
  echo "Test 11: the real 'make migrate-reset' refuses against a live migrated database holding account_status_history rows"
  ash_before=$(psql "$(probe_url mrguard_sequence)" -tAc 'SELECT count(*) FROM account_status_history' | tr -d '[:space:]')
  version_before=$(psql "$(probe_url mrguard_sequence)" -tAc 'SELECT version FROM schema_migrations' | tr -d '[:space:]')
  set +e
  MOUT=$(DATABASE_URL="$(probe_url mrguard_sequence)" make -C "$REPO_ROOT/backend" migrate-reset 2>&1)
  MSTATUS=$?
  set -e
  ash_after=$(psql "$(probe_url mrguard_sequence)" -tAc 'SELECT count(*) FROM account_status_history' | tr -d '[:space:]')
  version_after=$(psql "$(probe_url mrguard_sequence)" -tAc 'SELECT version FROM schema_migrations' | tr -d '[:space:]')
  if [ "$MSTATUS" -eq 0 ]; then
    fail "real 'make migrate-reset' proceeded (exit 0) against an unmarked, populated database — output: $MOUT"
  elif ! echo "$MOUT" | grep -q "Refusing to run"; then
    fail "make migrate-reset failed but not with the guard's refusal message — output: $MOUT"
  elif [ "$ash_before" != "$ash_after" ] || [ "$version_before" != "$version_after" ]; then
    fail "THE REAL 'make migrate-reset' RAN ANYWAY — account_status_history $ash_before -> $ash_after row(s), schema version $version_before -> $version_after"
  else
    pass "the real Makefile recipe refuses end-to-end and touches nothing"
  fi

  # ── Test 12: MIGRATE_RESET_CONFIRM unlocks the real recipe, and the full chain — including
  # 000152 then 000151 in their actual reverse order — runs to completion ──
  echo "Test 12: MIGRATE_RESET_CONFIRM unlocks the real 'make migrate-reset', running the full chain (incl. 000152->000151) to completion"
  set +e
  MOUT=$(MIGRATE_RESET_CONFIRM=mrguard_sequence DATABASE_URL="$(probe_url mrguard_sequence)" make -C "$REPO_ROOT/backend" migrate-reset 2>&1)
  MSTATUS=$?
  set -e
  remaining=$(count_non_bookkeeping_relations mrguard_sequence)
  if [ "$MSTATUS" -ne 0 ]; then
    fail "real 'make migrate-reset' failed even with a matching MIGRATE_RESET_CONFIRM (exit $MSTATUS) — output: $MOUT"
  elif [ "$remaining" != "0" ]; then
    fail "the confirmed run did not roll back every migration — $remaining relation(s) besides schema_migrations remain"
  else
    pass "the confirmed run executes the real migrate binary end-to-end, including 000152 then 000151, down to a clean schema"
  fi

  # ── Test 13 (mutant): removing the guard invocation from backend/Makefile reopens Test 11's hazard ──
  # Per feedback_validate_the_mutant_not_just_the_red: prove Test 11 is bound to the guard line
  # actually being IN the recipe, not to some incidental property of the probe database or the
  # `make` invocation.
  echo "Test 13 (mutant): removing the guard invocation from backend/Makefile's migrate-reset recipe re-opens the bypass"
  MUTANT_MAKEFILE="$REPO_ROOT/scripts/__tests__/.migrate-reset-guard-makefile-mutant"
  grep -v "assert-migrate-reset-target-disposable.sh" "$REPO_ROOT/backend/Makefile" > "$MUTANT_MAKEFILE"
  if diff -q "$REPO_ROOT/backend/Makefile" "$MUTANT_MAKEFILE" > /dev/null 2>&1; then
    fail "mutation was a no-op: guard invocation line not found in backend/Makefile"
  else
    seed_full_chain_probe_db mrguard_wiring_mutant
    set +e
    MOUT=$(DATABASE_URL="$(probe_url mrguard_wiring_mutant)" make -C "$REPO_ROOT/backend" -f "$MUTANT_MAKEFILE" migrate-reset 2>&1)
    MSTATUS=$?
    set -e
    rm -f "$MUTANT_MAKEFILE"
    remaining=$(count_non_bookkeeping_relations mrguard_wiring_mutant)
    if [ "$MSTATUS" -eq 0 ] && [ "$remaining" = "0" ]; then
      pass "with the guard line removed, the same populated database is destroyed unimpeded — Test 11's refusal is bound to that line being present"
    else
      fail "mutant Makefile still refused or left data behind (exit $MSTATUS, remaining=$remaining) — Test 11 is not actually bound to the guard invocation line"
    fi
  fi
else
  echo "Tests 11-13 (real end-to-end wiring): skipped — migrate not available"
fi

echo ""
echo "=== Results: $PASS passed, $FAIL failed ==="
[ $FAIL -eq 0 ]
