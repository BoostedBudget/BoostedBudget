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
# An EMPTY database (no relations that can hold rows — see the relkind enumeration below,
# EXCLUDING golang-migrate's own schema_migrations bookkeeping table) is auto-stamped and allowed
# through with no ceremony: there is nothing in it to lose, and refusing a `migrate-reset` against
# a database that was just `createdb`'d — or one that just had every migration rolled back,
# leaving only schema_migrations behind — would be the guard the next engineer deletes out of
# frustration. ⚠ Re-derived and fixed in #1569 round 12 task 7 fix round 1: `down -all` does NOT
# drop schema_migrations, so without excluding it BY NAME, this "no ceremony" path is unreachable
# for any database that has ever run `migrate up` — which is every database this command is ever
# actually run against. See the exclusion comment below for the empirical proof.
#
# ⚠ NOT overridable by a plain unqualified environment variable, and not read from any file the
# guard itself did not write (the COMMENT ON DATABASE marker below) — a workalike env-var opt-out
# left in ~/.zshrc is exactly the hazard this file exists to close. MIGRATE_RESET_CONFIRM is the one
# exception, and it is safe only because it is compared against the actual database name — a stale
# export pointing at a DIFFERENT database that also happens to be named the same is vanishingly
# unlikely, and a stale export naming an OLD database does nothing when DATABASE_URL now points
# somewhere else.
#
# Usage: DATABASE_URL=... assert-migrate-reset-target-disposable.sh [migrate-reset|migrate-down]
# Exit 0: proceed. Exit 3: refused (distinct from 1 so a caller can tell "refused" from "could not
# connect" or "psql missing", matching scripts/test-migrations.sh's GUARD_REFUSED_EXIT convention).
#
# ⚠ #1569 round 13 task 1: this guard now also covers `backend/Makefile`'s `migrate-down` target
# (`migrate ... down 1`), not just `migrate-reset` (`migrate ... down -all`). The confirmed hole:
# `migrate-down` checked only that DATABASE_URL was set and never called this script at all — a
# developer (or a script) could roll all the way back to any migration, one `make migrate-down` at
# a time, without ever touching the ONE command (`down -all`) this guard used to be scoped to. Since
# a rollback to a specific version can land on a migration with an unconditional destructive
# down.sql (e.g. 000035's unguarded `DELETE FROM transactions WHERE account_id IS NULL`) exactly as
# easily via repeated `down 1` as via `down -all`, the same disposability question — "is this
# database's data OK to lose?" — applies to both, so both share this one script rather than growing
# a second copy of the same logic. The optional first argument selects which command's wording goes
# in the refusal message and which confirmation environment variable unlocks a single run;
# everything else (the marker, the emptiness check, the database-name derivation) is identical for
# both and stays shared.
set -euo pipefail

GUARD_REFUSED_EXIT=3

TARGET_MODE="${1:-migrate-reset}"
case "$TARGET_MODE" in
    migrate-reset)
        MIGRATE_COMMAND_DESC="migrate down -all"
        MAKE_TARGET_DESC="make migrate-reset"
        CONFIRM_VAR_NAME="MIGRATE_RESET_CONFIRM"
        ;;
    migrate-down)
        MIGRATE_COMMAND_DESC="migrate down 1"
        MAKE_TARGET_DESC="make migrate-down"
        CONFIRM_VAR_NAME="MIGRATE_DOWN_CONFIRM"
        ;;
    *)
        echo "ERROR: unrecognized target mode '$TARGET_MODE' (expected 'migrate-reset' or 'migrate-down')" >&2
        exit 1
        ;;
esac

if [ -z "${DATABASE_URL:-}" ]; then
    echo "ERROR: DATABASE_URL environment variable is not set" >&2
    exit 1
fi

# Same marker family as scripts/test-migrations.sh, but a DISTINCT string. Stamping a database as
# disposable for the automated migration-test suite (#1589) is a separate consent from stamping it
# disposable for a human's interactive `make migrate-reset` — conflating them would let one round of
# CI-only test tooling silently unlock a destructive command a developer runs by hand, or vice
# versa.
#
# ⚠ THE STAMP DOES NOT EXPIRE OR RE-VALIDATE, AND THAT IS DELIBERATE (#1569 round 12 task 7 fix
# round 1 — reviewed and decided, not an oversight). Once a database carries this marker, EVERY
# future `migrate-reset` against it proceeds with no ceremony, forever, regardless of what gets
# put into it later. A database stamped empty today, then seeded with real data tomorrow, is
# silently exempt from every check this guard has.
#
# Two other shapes were considered and rejected:
#   - Re-validate emptiness on every run even when the stamp is present: this makes the stamp
#     meaningless — a stamped, ACTIVELY USED dev database (the normal case a developer keeps
#     resetting) would fail the re-check the moment it holds anything, forcing
#     MIGRATE_RESET_CONFIRM on every single reset. That is the exact "guard the next engineer
#     deletes out of frustration" failure mode this file exists to avoid, just moved to a
#     different trigger.
#   - Expire the stamp after N days: adds real state (a timestamp embedded in the marker) for a
#     narrower, harder-to-reason-about safety property — an operator who stamped a database six
#     months ago and now has it silently start refusing gets a surprise refusal with no signal
#     that anything about the database changed, which is its own "guard nobody can diagnose"
#     failure per this file's own design note above.
#
# The stamp means exactly one thing: "I am telling this guard, permanently, that I will never put
# anything into this database that I am not willing to lose to a rollback." That is the same
# contract `scripts/test-migrations.sh`'s sibling marker has always made for its own automated
# databases, and this guard makes the identical contract for a human's dev database, not a
# stronger one. Putting real data into an already-stamped database is an operator error against a
# promise the operator made, not a gap in this guard — the guard's job is to catch UNMARKED
# databases, not to protect an operator from contradicting their own earlier declaration.
DISPOSABLE_DB_MARKER='boostedbudget:disposable-migrate-reset-target'

# ⚠ Derived from the LIVE connection's own current_database(), not by string-splitting
# DATABASE_URL (#1569 round 12 task 7 fix round 1, Codex finding). A string split trusts what the
# operator typed as if it were what the server actually opened — the exact gap MIGRATE_RESET_CONFIRM
# exists to close would reopen the moment the two disagree (a URL alias, a connection default, or
# any other reason the path segment and the server's own idea of its name diverge). Asking the
# server is one round-trip we already pay for the marker check immediately below, so there is no
# cost to asking it first and reusing the answer everywhere else in this file.
db=$(psql "$DATABASE_URL" -tAc "SELECT current_database()" 2>/dev/null || true)
db="${db//[[:space:]]/}"
if [ -z "$db" ]; then
    echo "ERROR: could not connect to the database named by DATABASE_URL to determine its actual name" >&2
    exit 1
fi

comment=$(psql "$DATABASE_URL" -tAc \
    "SELECT coalesce(shobj_description(oid, 'pg_database'), '') FROM pg_database WHERE datname = current_database()" 2>/dev/null || true)
if [[ "$comment" == *"$DISPOSABLE_DB_MARKER"* ]]; then
    echo "$TARGET_MODE target accepted: '$db' is stamped as a disposable migrate-reset target."
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
#
# ⚠ public.schema_migrations is EXCLUDED BY NAME, and this is load-bearing, not cosmetic
# (#1569 round 12 task 7 fix round 1, Codex finding). golang-migrate creates it as an ordinary
# 'r' table and `down -all` does NOT drop it — verified empirically: `migrate up` to 180 then
# `migrate down -all` on a throwaway database leaves exactly one relation, `schema_migrations`,
# with `version` reset to 0. Without this exclusion, EVERY database that has ever run `migrate
# up` — including one this very guard just finished rolling back — reports >=1 user relation
# FOREVER, so the "empty database, no ceremony" path this guard advertises as the common case is
# actually unreachable for any database in the normal migrate-up/migrate-reset loop. It holds
# golang-migrate's own bookkeeping (a version number and a dirty flag), never user data, so
# excluding it is not a special case carved out of the "has user data" definition — it never
# qualified for that definition in the first place.
user_relations=$(psql "$DATABASE_URL" -tAc \
    "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE c.relkind IN ('r','p','m') AND n.nspname NOT IN ('pg_catalog','information_schema') AND n.nspname !~ '^pg_toast' AND NOT (n.nspname = 'public' AND c.relname = 'schema_migrations')" 2>/dev/null || true)
user_relations="${user_relations//[[:space:]]/}"

if [ "$user_relations" = "0" ]; then
    if psql "$DATABASE_URL" -c "COMMENT ON DATABASE \"$db\" IS '$DISPOSABLE_DB_MARKER'" > /dev/null 2>&1; then
        echo "$TARGET_MODE target accepted: '$db' was empty; stamped it as a disposable migrate-reset target."
    else
        echo "$TARGET_MODE target accepted: '$db' is empty, but it could not be stamped (not the owner?). It will be re-checked for emptiness every run."
    fi
    exit 0
fi

confirm_value="${!CONFIRM_VAR_NAME:-}"
if [ -n "$confirm_value" ] && [ "$confirm_value" = "$db" ]; then
    echo "$TARGET_MODE target accepted: '$db' confirmed for this run via $CONFIRM_VAR_NAME."
    exit 0
fi

echo "Refusing to run '$MIGRATE_COMMAND_DESC': '$db' holds $user_relations user relation(s) and carries no disposability marker." >&2
echo "" >&2
if [ "$TARGET_MODE" = "migrate-reset" ]; then
    echo "This command rolls EVERY migration back, in reverse, including foundational DROP TABLEs" >&2
    echo "(accounts, transactions, and others) that carry no per-file rollback guard at all — the" >&2
    echo "guard has to live here, at the one command that can run the whole sequence." >&2
else
    echo "This command rolls back the single most recently applied migration. Run it repeatedly and" >&2
    echo "it walks all the way down through every down.sql in reverse, exactly like 'migrate down" >&2
    echo "-all' does in one shot — including any down.sql with an unconditional, unguarded DELETE" >&2
    echo "(e.g. 000035's). The same disposability question applies either way." >&2
fi
echo "" >&2
echo "Two ways to proceed, both deliberate:" >&2
echo "" >&2
echo "  1. If '$db' really is a disposable local dev database you intend to keep resetting," >&2
echo "     stamp it once and every future migrate-down/migrate-reset against it will run with no" >&2
echo "     ceremony (the marker is shared across both):" >&2
echo "       psql \"\$DATABASE_URL\" -c \"COMMENT ON DATABASE \\\"$db\\\" IS '$DISPOSABLE_DB_MARKER'\"" >&2
echo "" >&2
echo "  2. If you want to roll '$db' back just this once, name it explicitly on this invocation:" >&2
echo "       $CONFIRM_VAR_NAME=$db DATABASE_URL=\$DATABASE_URL $MAKE_TARGET_DESC" >&2
echo "" >&2
echo "A plain on/off environment variable is deliberately not accepted here: one left exported in" >&2
echo "a shell profile would silently disarm this guard forever, for every database that shell ever" >&2
echo "points at — which is the exact stale-DATABASE_URL hazard this guard exists to catch." >&2
exit $GUARD_REFUSED_EXIT
