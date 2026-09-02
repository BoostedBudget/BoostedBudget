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
# ⚠ TWO CLASSES OF CASE LIVE HERE, and the split is the whole point of #1569's Codex finding 1.
#
#   A. PRE-CONNECT cases (Tests 1-10, 14-15) point at 127.0.0.1 port 1, which nothing listens on.
#      That is deliberate: they exercise gates that run BEFORE any connection is opened, so if such
#      a gate regresses to a no-op the case fails at the connectivity preflight instead of
#      destroying anything. The guard must run BEFORE the preflight or these cases could not tell
#      "refused the target" from "could not connect"; Test 1 asserts the refusal exit code (3) and
#      the refusal text, neither of which a connectivity failure (exit 1) produces.
#
#   B. REACHABLE cases (Tests 11-13, 16) point at a REAL, LIVE Postgres cluster — because the
#      original version of this file had only class A, and a suite that can never reach a live
#      server can never observe the path where the guard says YES to a real database. That blind
#      spot was load-bearing: the name-only guard admitted `production_test`, `foo_ci` and
#      `test_backup`, and nothing here could see it. Class B closes it by CREATING its own
#      throwaway databases (guardprobe_*) on a discovered local cluster, asserting the refusal, and
#      then asserting the probe's own table SURVIVED — i.e. that nothing was dropped.
#
# ⚠ Class B must NEVER point at a database it did not create. Every reachable case runs against a
# `guardprobe_*` database created in setup_probe_cluster and dropped in the EXIT trap.
set -euo pipefail

# PROSE-ASSERTION AUDIT (#1569 task 7.1). The repeated "Refusing to run" checks, plus "does not
# match", "boostedbudget-db.internal", and "unknown argument", are kept verbatim. Decision:
# CONTRACT — "Refusing to run" is the guard's entire purpose statement (scripts/test-migrations.sh
# log_error calls); a reader who greps CI output for that exact phrase must keep finding it. The
# hostname and flag-name checks are literal identifiers, not descriptive prose about what a
# number/scope means, so there's no "honest correction" risk of the typecheck-gate kind. Left
# unchanged.

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SCRIPT="$REPO_ROOT/scripts/test-migrations.sh"
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

# ─────────────────────────────────────────────────────────────────────────────
# Class B: cases against a REAL, LIVE cluster.
#
# Codex finding 1 (#1569): the guard trusted the database NAME and nothing else. A REACHABLE
# production database called `production_test`, `foo_ci` or `test_backup` passed the guard and
# proceeded to `DROP SCHEMA public CASCADE`. The suite above could not detect that, because every
# case in it pointed at a dead port — it could observe refusals, never acceptances.
# ─────────────────────────────────────────────────────────────────────────────

PROBE_ADMIN_URL=""
PROBE_HOSTPORT=""

# Finds a live local cluster to create throwaway databases on. Candidates, in order: an explicit
# GUARD_TEST_ADMIN_URL, the caller's own DATABASE_URL rewritten to the `postgres` maintenance
# database, then the two ports this repo actually uses locally (5434 is what the running
# boostedbudget container publishes; docker-compose.yml declares 5432).
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
      # user:pass@host:port, reused to build the probe databases' URLs.
      local stripped="${candidate#*://}"
      PROBE_HOSTPORT="${stripped%%/*}"
      return 0
    fi
  done
  return 1
}

probe_url() { printf 'postgres://%s/%s?sslmode=disable' "$PROBE_HOSTPORT" "$1"; }

drop_probe_dbs() {
  [ -n "$PROBE_ADMIN_URL" ] || return 0
  local db
  for db in guardprobe_test guardprobe_scratch; do
    psql "$PROBE_ADMIN_URL" -c "DROP DATABASE IF EXISTS $db WITH (FORCE)" > /dev/null 2>&1 || true
  done
}

trap 'drop_probe_dbs; rm -rf "$SANDBOX"' EXIT

echo ""
echo "── Class B: gates that can only be observed against a LIVE cluster ──"

if ! command -v psql > /dev/null 2>&1; then
  fail "psql is not on PATH — the reachable-path cases below CANNOT run, and they are the only ones that can observe the guard saying YES to a real database. Install a PostgreSQL client."
elif ! discover_probe_cluster; then
  fail "no live Postgres found (tried GUARD_TEST_ADMIN_URL, DATABASE_URL, localhost:5434, localhost:5432). The reachable-path cases are the only ones that can observe the guard saying YES to a real database; a suite that skips them is the exact blind spot #1569 closed. Start the dev database (make dev) or set GUARD_TEST_ADMIN_URL."
else
  echo "  (probe cluster: $PROBE_HOSTPORT)"
  drop_probe_dbs
  psql "$PROBE_ADMIN_URL" -c 'CREATE DATABASE guardprobe_test' > /dev/null
  psql "$PROBE_ADMIN_URL" -c 'CREATE DATABASE guardprobe_scratch' > /dev/null
  # guardprobe_test stands in for the reachable `production_test` Codex used: a name that PASSES
  # the disposable-name pattern, on a live server, holding data that must not be destroyed.
  psql "$(probe_url guardprobe_test)" -c 'CREATE TABLE precious (id int); INSERT INTO precious VALUES (1)' > /dev/null

  # ⚠ A pg_dump VERSION stub, and only that. Tests 13 and 16 need the run to get PAST main()'s
  # pg_dump-vs-server major-version preflight, which fails on any machine whose host client is
  # older than the container (measured here: client 14 vs server 17). Both cases kill the run at
  # or before clean_database, long before any dump is taken, so the stub's dump output is never
  # consumed — it exists purely so the preflight is not what decides those two assertions.
  PG_DUMP_STUB="$SANDBOX/pg_dump-stub"
  cat > "$PG_DUMP_STUB" <<'STUB'
#!/bin/bash
if [ "${1:-}" = "--version" ]; then echo "pg_dump (PostgreSQL) 99.0"; exit 0; fi
echo "-- guard-test stub: no dump is ever consumed by this suite"
STUB
  chmod +x "$PG_DUMP_STUB"

  probe_survived() {
    local n
    n=$(psql "$(probe_url guardprobe_test)" -tAc 'SELECT count(*) FROM precious' 2>/dev/null | tr -d '[:space:]')
    [ "$n" = "1" ]
  }

  # ── Test 11: a REACHABLE, name-passing database holding real data is refused ──
  echo "Test 11: refuses a live, name-passing database that holds data (the Codex exploit)"
  set +e
  RUN_OUTPUT=$(DATABASE_URL="$(probe_url guardprobe_test)" bash "${SUITE:-$SCRIPT}" 2>&1)
  RUN_STATUS=$?
  set -e
  if [ "$RUN_STATUS" -ne 3 ]; then
    fail "expected exit 3 (guard refusal) for a live database holding data, got $RUN_STATUS — output: $RUN_OUTPUT"
  elif ! echo "$RUN_OUTPUT" | grep -q "Refusing to run"; then
    fail "exit 3 but no refusal message — output: $RUN_OUTPUT"
  elif ! probe_survived; then
    fail "THE GUARD DESTROYED THE PROBE DATABASE. 'precious' no longer holds its row."
  else
    pass "a live name-passing database with data is refused, and its data survives"
  fi

  # ── Test 12: --yes-destroy does NOT unlock a live database holding data ──
  # A name is not evidence of safety, and neither is a developer typing that same name back. The
  # cluster check is unconditional: it applies even to an explicit confirmation.
  echo "Test 12: --yes-destroy does not unlock a live database that holds data"
  set +e
  RUN_OUTPUT=$(DATABASE_URL="$(probe_url guardprobe_test)" bash "${SUITE:-$SCRIPT}" --yes-destroy guardprobe_test 2>&1)
  RUN_STATUS=$?
  set -e
  if [ "$RUN_STATUS" -ne 3 ]; then
    fail "--yes-destroy bypassed the cluster check (exit $RUN_STATUS) — output: $RUN_OUTPUT"
  elif ! probe_survived; then
    fail "THE GUARD DESTROYED THE PROBE DATABASE under --yes-destroy."
  else
    pass "--yes-destroy is refused too; the cluster check is unconditional"
  fi

  # ── Test 13: a genuinely empty live database IS accepted (the YES path) ──
  # Without this the suite could be satisfied by a gate that refuses everything, which would break
  # CI and the local verification path while looking maximally safe.
  echo "Test 13: accepts a genuinely empty live database"
  ACCEPT_LOG="$SANDBOX/accept.log"
  : > "$ACCEPT_LOG"
  set +e
  PG_DUMP="$PG_DUMP_STUB" DATABASE_URL="$(probe_url guardprobe_scratch)" bash "${SUITE:-$SCRIPT}" > "$ACCEPT_LOG" 2>&1 &
  ACCEPT_PID=$!
  ACCEPTED=false
  for _ in $(seq 1 120); do
    if grep -q "Target cluster accepted" "$ACCEPT_LOG" 2>/dev/null; then ACCEPTED=true; break; fi
    if grep -q "Refusing to run" "$ACCEPT_LOG" 2>/dev/null; then break; fi
    kill -0 "$ACCEPT_PID" 2>/dev/null || break
    sleep 0.5
  done
  pkill -P "$ACCEPT_PID" 2>/dev/null || true
  kill "$ACCEPT_PID" 2>/dev/null || true
  wait "$ACCEPT_PID" 2>/dev/null
  set -e
  if [ "$ACCEPTED" != true ]; then
    fail "an EMPTY live database was not accepted past the cluster check — the gate refuses everything, which would break CI. Output: $(cat "$ACCEPT_LOG")"
  else
    pass "an empty live database is accepted (and stamped) — the YES path is reachable"
  fi
fi

# ── Test 14: a REMOTE host is refused, even with --yes-destroy ──
# Pre-connect, so no live server is needed. A production database is normally not on loopback at
# all; refusing every non-local host removes that entire class before a connection is even opened.
echo "Test 14: refuses a non-local host even with --yes-destroy"
set +e
RUN_OUTPUT=$(DATABASE_URL='postgres://u:p@boostedbudget-db.internal:5432/test?sslmode=disable' bash "$SCRIPT" --yes-destroy test 2>&1)
RUN_STATUS=$?
set -e
if [ "$RUN_STATUS" -ne 3 ]; then
  fail "expected exit 3 for a remote host, got $RUN_STATUS — output: $RUN_OUTPUT"
elif ! echo "$RUN_OUTPUT" | grep -q "boostedbudget-db.internal"; then
  fail "refusal does not name the host it refused — output: $RUN_OUTPUT"
else
  pass "a non-local host is refused, and --yes-destroy does not unlock it"
fi

# ── Test 15: the host allowlist cannot be widened from the environment ──
# Same property Test 10 pins for the name pattern: the only widening path is editing this
# repository, where it is reviewable.
echo "Test 15: DISPOSABLE_DB_HOST_PATTERN is not environment-overridable"
set +e
RUN_OUTPUT=$(DISPOSABLE_DB_HOST_PATTERN='.' DATABASE_URL='postgres://u:p@boostedbudget-db.internal:5432/test?sslmode=disable' bash "$SCRIPT" 2>&1)
RUN_STATUS=$?
set -e
if [ "$RUN_STATUS" -ne 3 ]; then
  fail "an exported DISPOSABLE_DB_HOST_PATTERN disarmed the host gate (exit $RUN_STATUS) — output: $RUN_OUTPUT"
else
  pass "exported DISPOSABLE_DB_HOST_PATTERN is ignored; the remote host is still refused"
fi

# ── Test 16 (mutant): deleting the cluster check re-opens the Codex exploit ──
# Per feedback_validate_the_mutant_not_just_the_red. The mutation deletes the single call site of
# the post-connect cluster check, leaving the function defined but never invoked. Under the mutant
# the live, name-passing, data-holding database must stop being refused — proving Test 11's
# refusal is bound to THAT gate and not to the name gate that already existed.
echo "Test 16 (mutant): deleting the cluster check re-opens the live-database hazard"
if [ -z "$PROBE_ADMIN_URL" ]; then
  fail "cannot run the cluster-check mutant without a live cluster (see the Class B message above)"
else
  CMUTANT="$SANDBOX/test-migrations.cluster-mutant.sh"
  grep -v '^[[:space:]]*assert_target_cluster_is_disposable$' "$SCRIPT" > "$CMUTANT"
  if diff -q "$SCRIPT" "$CMUTANT" > /dev/null 2>&1; then
    fail "mutation was a no-op: no bare 'assert_target_cluster_is_disposable' call line found in $SCRIPT"
  else
    MUT_LOG="$SANDBOX/cluster-mutant.log"
    : > "$MUT_LOG"
    set +e
    # MIGRATIONS_DIR must be absolute here: the mutant lives in $SANDBOX, and the script derives
    # its project root from its OWN location, so the default relative ./backend/migrations would
    # resolve under /tmp and abort at the migrations-directory check before reaching the gate the
    # mutation removed — a mutant that proves nothing.
    PG_DUMP="$PG_DUMP_STUB" MIGRATIONS_DIR="$REPO_ROOT/backend/migrations" \
      DATABASE_URL="$(probe_url guardprobe_test)" bash "$CMUTANT" > "$MUT_LOG" 2>&1 &
    MUT_PID=$!
    MUT_REFUSED=unknown
    for _ in $(seq 1 120); do
      if grep -q "Refusing to run" "$MUT_LOG" 2>/dev/null; then MUT_REFUSED=yes; break; fi
      if grep -q "Cleaning database" "$MUT_LOG" 2>/dev/null; then MUT_REFUSED=no; break; fi
      kill -0 "$MUT_PID" 2>/dev/null || { MUT_REFUSED=exited; break; }
      sleep 0.5
    done
    pkill -P "$MUT_PID" 2>/dev/null || true
    kill "$MUT_PID" 2>/dev/null || true
    wait "$MUT_PID" 2>/dev/null
    set -e
    if [ "$MUT_REFUSED" = yes ]; then
      fail "mutant still refuses — Test 11 is not actually bound to the cluster check"
    elif [ "$MUT_REFUSED" = no ]; then
      pass "mutant reaches clean_database, so Test 11's refusal is load-bearing"
    else
      fail "mutant neither refused nor reached clean_database ($MUT_REFUSED) — the mutation proved nothing. Output: $(cat "$MUT_LOG")"
    fi
    # The mutant just dropped guardprobe_test's schema. Rebuild the fixture so nothing after this
    # point sees a half-destroyed probe.
    psql "$(probe_url guardprobe_test)" -c 'DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public; CREATE TABLE precious (id int); INSERT INTO precious VALUES (1)' > /dev/null 2>&1 || true
  fi
fi


echo ""
echo "=== Results: $PASS passed, $FAIL failed ==="
[ $FAIL -eq 0 ]
