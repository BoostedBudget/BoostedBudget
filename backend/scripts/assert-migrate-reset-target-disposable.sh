#!/usr/bin/env bash
# Entry-point guard for `make migrate-reset` (backend/Makefile), which runs
# `migrate ... down -all` — the ONE command in this repo that unconditionally rolls back every
# migration, including the foundational `DROP TABLE`s in 000007 (accounts), 000010 (transactions),
# 000018 and 000022. #1569 round 12 added 12 per-file rollback refusal guards, and Codex then
# defeated one of them: `migrate down -all` runs in REVERSE order, so 000152's down.sql (an
# unconditional `DELETE FROM account_status_history;`) empties the table BEFORE 000151's down.sql
# counts rows and refuses. Every guard passed in isolation; nobody tested the sequence.
#
# THE RULING (#1569 round 12 task 7): do not add a refusal to every destructive down-migration —
# there is no way to enumerate all 179 files and be sure the next one is covered too. Instead, gate
# the one command that can ever run the destructive sequence at all. One gate here covers the whole
# class, including the four foundational DROP TABLEs above, which carry no per-file guard and never
# will.
#
# WHAT THIS SCRIPT DOES: refuses to let `migrate-reset` proceed against a database that (a) holds
# real user data and (b) has not been explicitly told it is disposable. Two ways to satisfy that,
# by design — see the refusal message below for the exact commands:
#
#   1. STAMP IT ONCE. `COMMENT ON DATABASE ... IS '...'` marks a database as a disposable local dev
#      target permanently. Every `migrate-reset` against it afterwards runs with no ceremony. This
#      is the path a developer resetting their own local dev database takes, and they take it once.
#   2. CONFIRM THIS ONE RUN. `MIGRATE_RESET_CONFIRM=<dbname> make migrate-reset` — the value must
#      equal the database DATABASE_URL actually names. This is deliberately NOT a bare boolean like
#      `MIGRATE_RESET_CONFIRM=1`: a boolean opt-out exported once into a shell profile silently
#      disarms the guard forever, for every database that shell ever points at (the same failure
#      mode this guard exists to catch — a stale DATABASE_URL export left over from pointing at a
#      Fly proxy or a shared database). Requiring the name forces the operator to look at what they
#      are about to destroy on every single invocation.
#
# An EMPTY database (no relations that can hold rows — see the relkind enumeration below) is
# auto-stamped and allowed through with no ceremony: there is nothing in it to lose, and refusing a
# `migrate-reset` against a database that was just `createdb`'d would be the guard the next
# engineer deletes out of frustration.
#
# ⚠ NOT overridable by a plain unqualified environment variable, and not read from any file the
# guard itself did not write (the COMMENT ON DATABASE marker below) — a workalike env-var opt-out
# left in ~/.zshrc is exactly the hazard this file exists to close. MIGRATE_RESET_CONFIRM is the one
# exception, and it is safe only because it is compared against the actual database name — a stale
# export pointing at a DIFFERENT database that also happens to be named the same is vanishingly
# unlikely, and a stale export naming an OLD database does nothing when DATABASE_URL now points
# somewhere else.
#
# Usage: DATABASE_URL=... assert-migrate-reset-target-disposable.sh
# Exit 0: proceed. Exit 3: refused (distinct from 1 so a caller can tell "refused" from "could not
# connect" or "psql missing", matching scripts/test-migrations.sh's GUARD_REFUSED_EXIT convention).
set -euo pipefail

GUARD_REFUSED_EXIT=3

if [ -z "${DATABASE_URL:-}" ]; then
    echo "ERROR: DATABASE_URL environment variable is not set" >&2
    exit 1
fi

# Same marker family as scripts/test-migrations.sh, but a DISTINCT string. Stamping a database as
# disposable for the automated migration-test suite (#1589) is a separate consent from stamping it
# disposable for a human's interactive `make migrate-reset` — conflating them would let one round of
# CI-only test tooling silently unlock a destructive command a developer runs by hand, or vice
# versa.
DISPOSABLE_DB_MARKER='boostedbudget:disposable-migrate-reset-target'

db="${DATABASE_URL%%\?*}"
db="${db##*/}"
if [ -z "$db" ]; then
    echo "ERROR: could not extract a database name from DATABASE_URL" >&2
    exit 1
fi

comment=$(psql "$DATABASE_URL" -tAc \
    "SELECT coalesce(shobj_description(oid, 'pg_database'), '') FROM pg_database WHERE datname = current_database()" 2>/dev/null || true)
if [[ "$comment" == *"$DISPOSABLE_DB_MARKER"* ]]; then
    echo "migrate-reset target accepted: '$db' is stamped as a disposable migrate-reset target."
    exit 0
fi

# The ONE condition under which this guard stamps on the operator's behalf: a database with no
# relation that can hold a row. An empty database is provably not anybody's data.
#
# Enumerated deliberately (#1569 round 12 task 7, Codex finding: a sibling check in
# scripts/test-migrations.sh checked relkind IN ('r','p') only, so a database holding ONLY a
# materialized view was judged empty and destroyed):
#   r (ordinary table)    — HAS DATA: rows live here directly.
#   p (partitioned table) — HAS DATA (parent presence counts even with zero partitions so far).
#   m (materialized view) — HAS DATA: REFRESH writes real heap pages, unlike a plain view's query.
#   v (view)               — no data: a stored query.
#   S (sequence)           — no data: a counter, not user content.
#   f (foreign table)      — no LOCAL data: the rows live on the remote side.
#   i / I (index, part. index) — no data: derivable from the table it indexes.
#   c (composite type)     — no data: a type definition.
#   t (TOAST)               — excluded by the pg_toast namespace filter below regardless.
user_relations=$(psql "$DATABASE_URL" -tAc \
    "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE c.relkind IN ('r','p','m') AND n.nspname NOT IN ('pg_catalog','information_schema') AND n.nspname !~ '^pg_toast'" 2>/dev/null || true)
user_relations="${user_relations//[[:space:]]/}"

if [ "$user_relations" = "0" ]; then
    if psql "$DATABASE_URL" -c "COMMENT ON DATABASE \"$db\" IS '$DISPOSABLE_DB_MARKER'" > /dev/null 2>&1; then
        echo "migrate-reset target accepted: '$db' was empty; stamped it as a disposable migrate-reset target."
    else
        echo "migrate-reset target accepted: '$db' is empty, but it could not be stamped (not the owner?). It will be re-checked for emptiness every run."
    fi
    exit 0
fi

if [ -n "${MIGRATE_RESET_CONFIRM:-}" ] && [ "$MIGRATE_RESET_CONFIRM" = "$db" ]; then
    echo "migrate-reset target accepted: '$db' confirmed for this run via MIGRATE_RESET_CONFIRM."
    exit 0
fi

echo "Refusing to run 'migrate down -all': '$db' holds $user_relations user relation(s) and carries no disposability marker." >&2
echo "" >&2
echo "This command rolls EVERY migration back, in reverse, including foundational DROP TABLEs" >&2
echo "(accounts, transactions, and others) that carry no per-file rollback guard at all — the" >&2
echo "guard has to live here, at the one command that can run the whole sequence." >&2
echo "" >&2
echo "Two ways to proceed, both deliberate:" >&2
echo "" >&2
echo "  1. If '$db' really is a disposable local dev database you intend to keep resetting," >&2
echo "     stamp it once and every future migrate-reset against it will run with no ceremony:" >&2
echo "       psql \"\$DATABASE_URL\" -c \"COMMENT ON DATABASE \\\"$db\\\" IS '$DISPOSABLE_DB_MARKER'\"" >&2
echo "" >&2
echo "  2. If you want to destroy '$db' just this once, name it explicitly on this invocation:" >&2
echo "       MIGRATE_RESET_CONFIRM=$db DATABASE_URL=\$DATABASE_URL make migrate-reset" >&2
echo "" >&2
echo "A plain on/off environment variable is deliberately not accepted here: one left exported in" >&2
echo "a shell profile would silently disarm this guard forever, for every database that shell ever" >&2
echo "points at — which is the exact stale-DATABASE_URL hazard this guard exists to catch." >&2
exit $GUARD_REFUSED_EXIT
