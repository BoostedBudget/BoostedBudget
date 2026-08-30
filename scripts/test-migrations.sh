#!/bin/bash
# Migration Testing Framework for BoostedBudget
# Issue #326: Comprehensive migration testing
# Issue #348: Rollback capability testing (.up/.down migrations)
# Issue #631: Migrated from Atlas to golang-migrate + Squawk
#
# Tests:
#   1. Idempotency (migrations can be run multiple times safely)
#   2. Performance (migrations complete within acceptable timeframes)
#   3. Data Integrity (migrations preserve existing data)
#   4. Schema Constraints (foreign keys, indexes, constraints exist)
#   5. Rollback Testing (up→down→up cycle produces identical schema)

set -euo pipefail

# =============================================================================
# Configuration
# =============================================================================

DATABASE_URL="${DATABASE_URL:?ERROR: DATABASE_URL environment variable is required}"
MIGRATIONS_DIR="${MIGRATIONS_DIR:-./backend/migrations}"
PERF_WARN_THRESHOLD=${PERF_WARN_THRESHOLD:-60}    # seconds
PERF_FAIL_THRESHOLD=${PERF_FAIL_THRESHOLD:-300}   # seconds
PERF_REPORT_FILE="${PERF_REPORT_FILE:-migration-performance.json}"

# Resolve MIGRATIONS_DIR to an absolute path ONCE, here, and use that resolved value ($ABS_MIGRATIONS_DIR)
# everywhere a migrate/psql -f call needs a path. Previously three call sites (apply_migrations,
# rollback_migrations, and the 000072 backfill invocation inside test_data_integrity) each
# recomputed their own project_root and then hardcoded "$project_root/backend/migrations",
# ignoring MIGRATIONS_DIR entirely — so the suite could log one directory (this variable, via
# main's "Migrations: $MIGRATIONS_DIR" line) while actually migrating a completely different one
# if a caller ever set MIGRATIONS_DIR to something else. Resolving MIGRATIONS_DIR itself into an
# absolute path, once, and using that resolved value everywhere, makes the logged path and the
# migrated path the same value by construction instead of by coincidence.
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
case "$MIGRATIONS_DIR" in
    /*) ABS_MIGRATIONS_DIR="$MIGRATIONS_DIR" ;;
    *) ABS_MIGRATIONS_DIR="$PROJECT_ROOT/${MIGRATIONS_DIR#./}" ;;
esac

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Test results tracking
TESTS_PASSED=0
TESTS_FAILED=0
WARNINGS=0

# =============================================================================
# Helper Functions
# =============================================================================

log_info() {
    echo -e "${BLUE}ℹ️  $1${NC}"
}

log_success() {
    echo -e "${GREEN}✅ $1${NC}"
}

log_warning() {
    echo -e "${YELLOW}⚠️  $1${NC}"
    WARNINGS=$((WARNINGS + 1))
}

log_error() {
    echo -e "${RED}❌ $1${NC}"
}

log_section() {
    echo ""
    echo -e "${BLUE}═══════════════════════════════════════════════════════════${NC}"
    echo -e "${BLUE}  $1${NC}"
    echo -e "${BLUE}═══════════════════════════════════════════════════════════${NC}"
    echo ""
}

pass_test() {
    ((TESTS_PASSED++))
    log_success "$1"
}

fail_test() {
    ((TESTS_FAILED++))
    log_error "$1"
}

# Run psql command against database
run_psql() {
    psql "$DATABASE_URL" -t -c "$1" 2>/dev/null | tr -d ' \n' || echo ""
}

# capture_all_table_counts prints "<table> <rowcount>" for EVERY ordinary table in the public
# schema, one per line, sorted by table name (#1569 round 14 task 8).
#
# WHY EVERY TABLE, NOT A LIST. test_data_integrity's original row-count check names five tables by
# hand (users, privacy_settings, linked_items, accounts, transactions). Those five are the ORIGINAL
# seed set and none of this branch's own migrations INSERT into any of them, so a duplicate-row or
# mis-update defect introduced by a migration in the replay range was invisible to it — and equally
# invisible to test_idempotency's schema hash, which is `pg_dump --schema-only` and never contains
# data. A hand-maintained table list has no way to notice the table nobody added to it, which is the
# exact failure mode that produced that gap; enumerating pg_class instead cannot go stale.
#
# query_to_xml is the standard way to run COUNT(*) over a dynamically-named table from plain SQL
# (no PL/pgSQL block, no server-side function to create and clean up). `format('%I')` quotes the
# identifier, so a table name needing quoting cannot break the query or inject into it.
#
# schema_migrations is excluded deliberately: it is golang-migrate's own tracking table, and a psql
# replay never touches it (that is precisely why the replay mechanism exists). Partitions and views
# are excluded by relkind='r'.
capture_all_table_counts() {
    psql "$DATABASE_URL" -At -F' ' -c "
        SELECT c.relname,
               (xpath('/row/cnt/text()',
                      query_to_xml(format('SELECT COUNT(*) AS cnt FROM public.%I', c.relname),
                                   false, true, '')))[1]::text::bigint
        FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = 'public'
          AND c.relkind = 'r'
          AND c.relname <> 'schema_migrations'
        ORDER BY c.relname
    " 2>/dev/null
}

# Get schema hash for idempotency comparison
# Uses --schema=public to dump only application tables, excluding:
#   - golang-migrate tracking table (schema_migrations)
#   - System schemas (pg_catalog, information_schema, etc.)
# Also filters pg_dump 15.14+ security tokens (CVE-2025-8714):
#   - \restrict and \unrestrict with random keys (change on each dump)
#   - Filtering is safe and standard practice (Rails, GitLab, etc.)
# PG_DUMP lets a caller substitute the pg_dump binary. pg_dump REFUSES to dump a server whose major
# version is newer than its own, so the client must be >= the server. Where the host has no matching
# client, point this at the server container's own binary, e.g.
#   PG_DUMP="$PWD/scripts/pg_dump-via-docker" ./scripts/test-migrations.sh
# CI does exactly that (see the migration-tests job) because the runner's newest client is older than
# the postgres:17 container it starts. psql is deliberately NOT indirected: it tolerates a
# newer server for the queries used here, and one call site passes `-f <host path>` which a
# containerised psql could not read.
PG_DUMP="${PG_DUMP:-pg_dump}"

# =============================================================================
# Destructive-target guard (#1589)
# =============================================================================
#
# This suite is not a read-only checker. clean_database() runs `DROP SCHEMA public CASCADE` and
# rollback_migrations() runs `migrate down -all`, both against whatever DATABASE_URL names, and
# both before any test output appears. Until #1589 the only thing checked about that database was
# that the variable was non-empty (`${DATABASE_URL:?...}` above), so a shell that still had
# DATABASE_URL exported for `make -C backend migrate-up`, a `make dev` session, or a Fly proxy
# destroyed whatever it pointed at. There is no Makefile target wrapping this script, so it is run
# by hand from exactly such shells.
#
# The guard below is deliberately shaped so the SAFE path is the default and the destructive path
# is explicit *per invocation*:
#
#   - A database whose name is recognisably disposable runs with no ceremony. That covers the two
#     names this repository actually uses: `freshcheck` (the local scratch database) and `test`
#     (what .github/workflows/test.yml builds its DATABASE_URL around).
#   - Anything else requires `--yes-destroy <name>` on the command line, and that name must EQUAL
#     the database in DATABASE_URL. Naming the database you *think* you are pointing at is what
#     catches the stale-export case: the flag and the URL disagree, and the run stops.
#
# ⚠ Deliberately NOT an environment variable. An opt-out like `I_KNOW_THIS_DESTROYS_THE_DB=1` is
# exported once into a shell profile and then silently disarms the guard for every future run from
# that shell — which is the same failure mode as the stale DATABASE_URL export the guard exists to
# catch. A command-line flag cannot be left behind in an environment.
#
# ⚠ Widening DISPOSABLE_DB_PATTERN is how this guard dies quietly. `boostedbudget` must never
# match it; scripts/__tests__/test-migrations-target-guard.test.sh constructs that refusal, and
# also runs a mutant with the call site deleted to prove the refusal is bound to the guard rather
# than to the unreachable host the test points at.
#
# ⚠ NOT overridable from the environment (no `${DISPOSABLE_DB_PATTERN:-...}` indirection). An
# environment-overridable pattern is an opt-out env var wearing a different hat: `export
# DISPOSABLE_DB_PATTERN=.` in a profile disarms the guard for every future run from that shell.
DISPOSABLE_DB_PATTERN='^(test|freshcheck|[A-Za-z0-9]+_test|test_[A-Za-z0-9_]+|[A-Za-z0-9_]*scratch[A-Za-z0-9_]*|[A-Za-z0-9_]+_ci)$'

# ⚠ A NAME IS NOT EVIDENCE OF SAFETY (#1569, Codex finding 1). The pattern above admits
# `production_test`, `foo_ci` and `test_backup`, and until this comment existed the script checked
# NOTHING else — not the host, not the cluster, not the contents. A reachable production database
# with one of those names passed the guard and proceeded to DROP SCHEMA public CASCADE. Two further
# gates close that, and BOTH apply even under --yes-destroy, because the failure being guarded
# against is a developer who is confident and wrong:
#
#   HOST (pre-connect, DISPOSABLE_DB_HOST_PATTERN below). A database worth protecting is almost
#   never on this machine's loopback. Refusing every non-local host removes the entire remote class
#   — Fly, RDS, a staging proxy, a tunnelled port on a named host — before a socket is opened.
#
#   CLUSTER (post-connect, assert_target_cluster_is_disposable below). Loopback is not enough:
#   `docker-compose` publishes the dev database on loopback too, and Codex's exploit URL was
#   127.0.0.1. So the database itself must positively declare that it is a throwaway migration
#   target. It declares that by carrying DISPOSABLE_DB_MARKER as its COMMENT ON DATABASE. That
#   marker is stamped automatically the first time the suite sees a database with NO user tables at
#   all — an empty database is provably not anybody's production data, which is why that is the one
#   condition under which the suite is willing to stamp on the user's behalf. Anything else refuses
#   and prints the exact statement to run.
#
# Why a database comment rather than a file or an env var: it lives WITH the database it describes,
# survives DROP SCHEMA (it is stored in pg_shdescription, not in the schema), cannot be exported
# once into a shell profile and forgotten, and cannot travel with a copied DATABASE_URL to a
# different cluster.

# ⚠ NOT overridable from the environment, for the same reason DISPOSABLE_DB_PATTERN is not.
# ⚠ No empty alternation branch here: an EMPTY host (a local unix socket, local by construction)
# is accepted by an explicit -z test in assert_host_is_local, because `^(|localhost|...)$` is not
# a compilable ERE in every shell that might run this — it fails with "empty (sub)expression",
# which under `[[ =~ ]]` returns non-zero and would have made the gate refuse EVERY host,
# 127.0.0.1 included. Observed while writing this; the test suite caught it.
DISPOSABLE_DB_HOST_PATTERN='^(localhost|localhost\.localdomain|127\.[0-9]+\.[0-9]+\.[0-9]+|::1|\[::1\]|host\.docker\.internal|postgres|db)$'

# The self-declaration a database must carry to be a legal target. Matched as a substring so a
# team can keep their own explanatory text in the same comment.
DISPOSABLE_DB_MARKER='boostedbudget:disposable-migration-test-target'

# Exit status used for every guard refusal. Distinct from 1 (a normal failure, including the
# connectivity preflight) so the test suite can tell "refused this target" apart from "could not
# reach it" — the two are indistinguishable by message alone when the target is unreachable.
GUARD_REFUSED_EXIT=3

# Set only by --yes-destroy on the command line. Never read from the environment; see above.
YES_DESTROY_DB=""

# Extracts the database name from a libpq URL: strip any query string, then take the last path
# segment. `postgres://u:p@h:5432/boostedbudget?sslmode=disable` -> `boostedbudget`.
database_name_from_url() {
    local url="$1"
    url="${url%%\?*}"
    url="${url##*/}"
    printf '%s' "$url"
}

# Extracts the host from a libpq URL, without the port or the userinfo.
# `postgres://u:p@127.0.0.1:5434/freshcheck?sslmode=disable` -> `127.0.0.1`
# `postgres://u@[::1]:5432/test`                             -> `[::1]`
# `postgres:///freshcheck`                                   -> `` (local unix socket)
database_host_from_url() {
    local url="$1" hostport
    url="${url#*://}"     # strip scheme
    url="${url%%\?*}"     # strip query
    url="${url%%/*}"      # strip path -> [userinfo@]host[:port]
    hostport="${url##*@}" # strip userinfo
    case "$hostport" in
        \[*\]*) printf '%s' "${hostport%%\]*}]" ;;
        *)      printf '%s' "${hostport%%:*}" ;;
    esac
}

# Pre-connect. Unconditional: --yes-destroy cannot unlock a remote host, because the flag only
# proves the developer knows which NAME they typed, not which machine it lives on.
assert_host_is_local() {
    local host
    host="$(database_host_from_url "$DATABASE_URL")"

    # Empty host = a local unix socket (postgres:///freshcheck). Local by construction.
    if [ -z "$host" ] || [[ "$host" =~ $DISPOSABLE_DB_HOST_PATTERN ]]; then
        return 0
    fi

    log_error "Refusing to run: '$host' is not a local host."
    log_error "  DATABASE_URL=$DATABASE_URL"
    log_error ""
    log_error "This suite runs 'DROP SCHEMA public CASCADE' and 'migrate down -all'. It will only"
    log_error "do that against a database on this machine's loopback (or a local unix socket)."
    log_error "A remote host — Fly, RDS, a staging proxy, a tunnel to a named host — is refused"
    log_error "outright, and --yes-destroy does NOT unlock it: naming the database you meant proves"
    log_error "nothing about which server it is on."
    exit $GUARD_REFUSED_EXIT
}

assert_target_is_disposable() {
    local db
    db="$(database_name_from_url "$DATABASE_URL")"

    if [ -z "$db" ]; then
        log_error "Refusing to run: DATABASE_URL names no database."
        log_error "  DATABASE_URL=$DATABASE_URL"
        log_error "This suite DROPs the schema it is pointed at; it will not guess a target."
        exit $GUARD_REFUSED_EXIT
    fi

    if [ -n "$YES_DESTROY_DB" ]; then
        if [ "$YES_DESTROY_DB" = "$db" ]; then
            log_warning "--yes-destroy '$db' matches DATABASE_URL. This run WILL drop and rebuild that database."
            return 0
        fi
        log_error "Refusing to run: --yes-destroy '$YES_DESTROY_DB' does not match the database in DATABASE_URL ('$db')."
        log_error "  DATABASE_URL=$DATABASE_URL"
        log_error "You named one database and pointed at another. Fix whichever is wrong."
        exit $GUARD_REFUSED_EXIT
    fi

    if [[ "$db" =~ $DISPOSABLE_DB_PATTERN ]]; then
        return 0
    fi

    log_error "Refusing to run: '$db' is not a recognisably disposable database."
    log_error "  DATABASE_URL=$DATABASE_URL"
    log_error ""
    log_error "This suite runs 'DROP SCHEMA public CASCADE' and 'migrate down -all' against that"
    log_error "database. Everything in it would be gone, unrecoverably, before the first test ran."
    log_error ""
    log_error "Point it at a scratch database instead:"
    log_error "  createdb freshcheck   # or any name matching $DISPOSABLE_DB_PATTERN"
    log_error "  DATABASE_URL=postgres://devuser:devpass@localhost:5434/freshcheck?sslmode=disable $0"
    log_error ""
    log_error "If destroying '$db' really is what you want, say so explicitly on this invocation:"
    log_error "  $0 --yes-destroy $db"
    exit $GUARD_REFUSED_EXIT
}

# Post-connect. Unconditional, --yes-destroy included. See the DISPOSABLE_DB_MARKER commentary
# above for why the database has to declare itself rather than be recognised by name or host.
#
# ⚠ Ordering: this runs immediately after the connectivity check in main() and BEFORE the pg_dump
# version preflight, so a wrong target is refused at the earliest moment a connection makes the
# question answerable — and well before clean_database().
assert_target_cluster_is_disposable() {
    local db comment user_tables
    db="$(database_name_from_url "$DATABASE_URL")"

    comment=$(psql "$DATABASE_URL" -tAc \
        "SELECT coalesce(shobj_description(oid, 'pg_database'), '') FROM pg_database WHERE datname = current_database()" 2>/dev/null || true)
    if [[ "$comment" == *"$DISPOSABLE_DB_MARKER"* ]]; then
        log_info "Target cluster accepted: '$db' is stamped as a disposable migration-test target."
        return 0
    fi

    # No stamp. The ONE condition under which this suite stamps on the user's behalf is a database
    # with no user DATA at all: an empty database cannot be anybody's data.
    #
    # ⚠ #1569 round 12 task 7 (Codex finding): this used to check relkind IN ('r','p') only —
    # ordinary and partitioned tables. A database holding ONLY a materialized view ('m') has real,
    # queryable, materialized ROWS (not a live query like a plain view) and was judged empty by that
    # list, silently stamped disposable, and then destroyed by DROP SCHEMA public CASCADE /
    # migrate down -all. Enumerated deliberately, not copied forward again:
    #   r (ordinary table)      — HAS DATA: rows live here directly.
    #   p (partitioned table)   — HAS DATA (parent): its partitions are relkind 'r' and are already
    #                             counted on their own, but the parent is included too so a
    #                             partitioned table with zero partitions still counts as user schema.
    #   m (materialized view)   — HAS DATA: REFRESH writes real heap pages; this is the gap above.
    #   v (plain view)          — NO DATA: a stored query, nothing to lose if dropped.
    #   S (sequence)            — NO DATA: a counter, not user-authored content.
    #   f (foreign table)       — NO LOCAL DATA: the rows live on the remote side; dropping the
    #                             mapping here does not destroy them.
    #   i / I (index, part. index) — NO DATA: derivable from the table it indexes, already counted.
    #   c (composite type)      — NO DATA: a type definition, not a row.
    #   t (TOAST table)         — excluded by the `nspname !~ '^pg_toast'` filter below regardless.
    #
    # ⚠ public.schema_migrations is EXCLUDED BY NAME (#1569 round 12 task 7 fix round 1, same
    # Codex finding as the sibling guard in backend/scripts/assert-migrate-reset-target-disposable.sh):
    # golang-migrate creates it as an ordinary 'r' table and neither `down -all` nor a failed run
    # drops it, so without this exclusion a database that has EVER run `migrate up` reports >=1
    # user relation forever and never qualifies for the auto-stamp path. It holds only a version
    # number and a dirty flag — golang-migrate's own bookkeeping, never user data.
    user_tables=$(psql "$DATABASE_URL" -tAc \
        "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE c.relkind IN ('r','p','m') AND n.nspname NOT IN ('pg_catalog','information_schema') AND n.nspname !~ '^pg_toast' AND NOT (n.nspname = 'public' AND c.relname = 'schema_migrations')" 2>/dev/null || true)
    user_tables="${user_tables//[[:space:]]/}"

    if [ "$user_tables" = "0" ]; then
        # Best-effort. If the caller does not own the database the stamp fails, and that is fine:
        # the emptiness that justified this run is re-derived on every subsequent run anyway.
        if psql "$DATABASE_URL" -c "COMMENT ON DATABASE \"$db\" IS '$DISPOSABLE_DB_MARKER'" > /dev/null 2>&1; then
            log_info "Target cluster accepted: '$db' was empty; stamped it as a disposable migration-test target."
        else
            log_warning "Target cluster accepted: '$db' is empty, but it could not be stamped (not the owner?). It will be re-checked for emptiness every run."
        fi
        return 0
    fi

    log_error "Refusing to run: '$db' is a live database, not a disposable migration target."
    log_error "  DATABASE_URL=$DATABASE_URL"
    log_error "  It holds $user_tables user table(s) and carries no disposability marker."
    log_error ""
    log_error "Its NAME passed the disposable-name check, but a name is not evidence: 'production_test',"
    log_error "'foo_ci' and 'test_backup' all pass that pattern, and this suite would have run"
    log_error "'DROP SCHEMA public CASCADE' against any of them. So the database itself has to say so."
    log_error ""
    log_error "Either point at a fresh database (an EMPTY one is stamped automatically):"
    log_error "  createdb ${db}_scratch"
    log_error ""
    log_error "or, if you are certain this database is throwaway, stamp it yourself:"
    log_error "  psql \"\$DATABASE_URL\" -c \"COMMENT ON DATABASE \\\"$db\\\" IS '$DISPOSABLE_DB_MARKER'\""
    log_error ""
    log_error "⚠ --yes-destroy does NOT skip this check."
    exit $GUARD_REFUSED_EXIT
}

parse_args() {
    while [ $# -gt 0 ]; do
        case "$1" in
            --yes-destroy)
                if [ $# -lt 2 ]; then
                    log_error "--yes-destroy requires the database name it is confirming"
                    exit 2
                fi
                YES_DESTROY_DB="$2"
                shift 2
                ;;
            --yes-destroy=*)
                YES_DESTROY_DB="${1#*=}"
                shift
                ;;
            -h|--help)
                echo "Usage: DATABASE_URL=... $0 [--yes-destroy <dbname>]"
                echo ""
                echo "Runs the migration test suites against DATABASE_URL. The suite DROPS and"
                echo "rebuilds that database, so the target must clear THREE independent checks:"
                echo ""
                echo "  1. HOST      — must be local (loopback or a unix socket). Not unlockable."
                echo "  2. NAME      — must look disposable, or pass --yes-destroy <that exact name>."
                echo "  3. CLUSTER   — the database must carry the disposability marker as its"
                echo "                 COMMENT ON DATABASE. A database with NO user tables is"
                echo "                 stamped automatically; anything else is refused with the"
                echo "                 exact statement to run. Not unlockable by --yes-destroy."
                echo ""
                echo "Checks 1 and 3 apply even with --yes-destroy: a name is not evidence of safety."
                exit 0
                ;;
            *)
                # Never ignored: a mistyped --yes-destroy that was silently dropped would leave the
                # developer believing they had confirmed while the guard was still armed (or, worse,
                # a future flag silently doing nothing).
                log_error "Unknown argument: $1"
                log_error "Usage: DATABASE_URL=... $0 [--yes-destroy <dbname>]"
                exit 2
                ;;
        esac
    done
}

get_schema_hash() {
    # Capture pg_dump's raw output SEPARATELY from the restrict/unrestrict filter below, and check
    # ITS exit status directly via `|| dump_exit=$?` — NOT `local raw_dump=$(...)`, which would mask
    # a real pg_dump failure behind `local`'s own (always-0) exit status (see the root-cause note on
    # PG_DUMP above `apply_migrations`). This is what fixes R3-15: previously a `pg_dump` that wrote
    # SOME output and then died mid-stream (a broken pipe, a killed connection, a partial dump) was
    # non-empty and got hashed as a valid schema — the empty-dump guard below only ever caught TOTAL
    # emptiness, never a truncated-but-non-empty dump.
    local raw_dump
    local dump_exit=0
    raw_dump=$("$PG_DUMP" "$DATABASE_URL" --schema-only --no-owner --no-privileges \
        --schema=public 2>/dev/null) || dump_exit=$?

    if [ "$dump_exit" -ne 0 ]; then
        echo "❌ get_schema_hash: pg_dump FAILED (exit code $dump_exit) — it failed to connect, read the" >&2
        echo "   schema, or died mid-dump (a truncated dump is a failure, not a smaller-but-valid schema)." >&2
        echo "   Re-run without 2>/dev/null to see the real error. Common causes:" >&2
        echo "     - a DATABASE_URL missing '?sslmode=disable'" >&2
        echo "     - pg_dump older than the server (it refuses; see check_pg_dump_version below)" >&2
        exit 1
    fi

    # Require the raw completion trailer BEFORE any filtering (checked on raw_dump, not on the
    # \restrict/\unrestrict-filtered `dump` below). A pg_dump that exits 0 and writes SOME non-empty
    # output but dies mid-stream (a stub binary, a killed connection, a broken pipe) is a truncated
    # dump, not a smaller-but-valid schema — the exit-code check above and the empty-dump check
    # below only ever caught total failure or total emptiness, never "exited 0 with partial,
    # non-empty output". Checked on the UNFILTERED raw_dump specifically so a future change to the
    # \restrict/\unrestrict filter can never be the reason this trailer check passes or fails.
    #
    # Uses a here-string (<<<), NOT `printf ... | grep -q`: grep -q exits the moment it finds a
    # match, closing its read end of the pipe — if the producer (printf, writing a ~250KB/7000+
    # line dump) is still mid-write when that happens, it can be killed by SIGPIPE, and under
    # `pipefail` that makes the WHOLE pipeline report failure even though grep already found the
    # trailer. Reproduced for real while building this: the exact same content, byte-for-byte
    # (confirmed via length + last-line comparison), intermittently tripped this check when piped
    # but never once did across 20+ repeated calls using a here-string or a full-scan `grep -c`
    # instead. A here-string has no live producer process to race against.
    if ! grep -q '^-- PostgreSQL database dump complete$' <<< "$raw_dump"; then
        echo "❌ get_schema_hash: pg_dump output has no completion trailer — it produced SOME output" >&2
        echo "   but died mid-stream. A truncated dump must never be hashed as if it were a" >&2
        echo "   smaller-but-valid schema; that would silently verify nothing." >&2
        exit 1
    fi

    # Filter out pg_dump 15.14+ security tokens (CVE-2025-8714): \restrict / \unrestrict lines whose
    # random keys change on every dump. This filter's own exit status is DELIBERATELY not checked
    # (kept under `local dump=$(...)`, which masks it): grep -v exits 1 if it filters out every line,
    # which would be a false-positive "failure" here, not a real one — in practice it can't happen
    # (pg_dump --schema-only always emits a real schema body after the header), but treating a
    # fully-filtered pipeline as a hard error would conflate "the filter matched everything" with "pg_dump
    # failed". The empty-dump guard immediately below already catches that case, with a clearer message
    # than a bare pipefail abort would give.
    local dump=$(printf '%s\n' "$raw_dump" | grep -v '^\\restrict ' | grep -v '^\\unrestrict ')

    # FAIL CLOSED on an empty dump. pg_dump emits a header (SET statements, comments) even for a
    # schema containing zero objects, so an EMPTY result never means "no tables" — it means either
    # pg_dump failed in a way not already caught above, or the restrict/unrestrict filter matched
    # every line. Without this check the callers hash "" twice, find the two equal, and print
    # "IDEMPOTENCY TEST PASSED - Schema unchanged after re-applying migrations" having applied and
    # compared nothing at all. Observed for real: a DATABASE_URL without sslmode=disable produced md5
    # 68b329da9893e34099c7d8ad5cb9c940 (the hash of an empty string) from both calls and two green
    # PASSED lines.
    if [ -z "$dump" ]; then
        echo "❌ get_schema_hash: pg_dump produced NO output after filtering — it failed to connect or read the schema." >&2
        echo "   This is not an empty schema (pg_dump emits a header even with zero objects); it is a" >&2
        echo "   pg_dump failure or the restrict/unrestrict filter matched every line. Re-run without" >&2
        echo "   2>/dev/null to see the real error. Common causes:" >&2
        echo "     - a DATABASE_URL missing '?sslmode=disable'" >&2
        echo "     - pg_dump older than the server (it refuses; see check_pg_dump_version below)" >&2
        exit 1
    fi

    # Save to timestamped file for debugging
    local timestamp=$(date +%s%N)
    local dump_file="/tmp/schema_dump_${timestamp}.sql"
    echo "$dump" > "$dump_file"

    # Log file location (visible in CI logs for debugging)
    echo "📝 Schema dump saved: $dump_file" >&2

    # Show first 50 lines for quick inspection
    echo "=== Schema Dump Preview (first 50 lines) ===" >&2
    echo "$dump" | head -50 >&2
    echo "..." >&2

    # Calculate and return hash. This hash is comparable ONLY across calls within the same script
    # run: it is produced by one fixed $PG_DUMP binary dumping one server, so every comparison this
    # suite makes (test_idempotency's hash1/hash2; test_rollback's hash_baseline/hash_reapplied) is
    # between two dumps from that SAME client/server pair. It is never persisted or compared across
    # separate invocations, PostgreSQL versions, or pg_dump client versions, and must not be treated
    # as stable if it ever is. `$dump`'s line order is exactly pg_dump's own output order — never
    # sorted — since re-ordering could mask a genuine ordering-sensitive schema difference (e.g.
    # column order).
    echo "$dump" | md5sum | cut -d' ' -f1
}

# =============================================================================
# Full-schema object inventory — for test_rollback's clean-state check
# =============================================================================
#
# get_schema_hash above intentionally scopes pg_dump to --schema=public, which is correct for
# comparing the public application schema across two `up` runs (Test 1) or across up->down->up
# (Test 5's baseline/reapplied hash) — but --schema=public makes PostgreSQL's own pg_dump silently
# omit non-schema-scoped objects entirely. Extensions are the clearest case: `CREATE EXTENSION
# pg_trgm` never appears in a --schema=public dump even though 000101_merchant_name_handling.up.sql
# creates it and its own down.sql deliberately does not drop it (comment there: "pg_trgm extension
# is not dropped here since other features may use it"). That made the OLD clean-state check
# (`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='public' AND table_name !=
# 'schema_migrations'`) blind on two separate axes: it only ever looked at TABLES (never functions,
# extensions, types, triggers, ...), and even a full pg_dump-based check scoped to --schema=public
# would still miss extensions specifically, for the reason above.
#
# get_full_object_inventory therefore drops --schema=public entirely and instead excludes exactly
# the schema_migrations object via pg_dump's own --exclude-table=public.schema_migrations (a
# dependency-aware exclusion of one named object, not a text-grep that could also eat unrelated
# lines that happen to match).
#
# What this covers, stated honestly: every object pg_dump emits one of its own
# "-- Name: X; Type: Y; Schema: Z; Owner: -" TOC header comments for, which — per direct inspection
# of real dumps taken while building this, not just documentation — includes tables, sequences,
# functions, types, triggers, indexes, constraints, extensions, and comments-on-objects; pg_dump's
# TOC header format is generic across every object kind it supports, so this is NOT a hand-picked
# list of object types we maintain (which the earlier table-only check effectively was) — it is
# whatever pg_dump itself considers a distinct, nameable object. This was NOT independently
# verified against pg_dump's own source for completeness across every object kind (policies,
# materialized views, domains, collations, operator classes were not each individually produced
# and confirmed in a test dump here) — only extensions, tables, sequences, functions, types,
# triggers, indexes, and constraints were. It does not cover cluster-level objects (roles,
# tablespaces), which pg_dump never dumps by default and which this suite has never claimed to
# check.
extract_object_identities() {
    printf '%s\n' "$1" \
        | grep -oE '^-- Name: .+; Type: [A-Z ]+; Schema: [^;]*; Owner: -$' \
        | sed -E 's/^-- Name: (.+); Type: ([A-Z ]+); Schema: [^;]*; Owner: -$/\2 \1/' \
        | grep -v '^COMMENT ' \
        | sort -u
}

get_full_object_inventory() {
    local raw_dump
    local dump_exit=0
    raw_dump=$("$PG_DUMP" "$DATABASE_URL" --schema-only --no-owner --no-privileges \
        --exclude-table=public.schema_migrations 2>/dev/null) || dump_exit=$?

    if [ "$dump_exit" -ne 0 ]; then
        echo "❌ get_full_object_inventory: pg_dump FAILED (exit code $dump_exit)." >&2
        exit 1
    fi

    # Here-string, not `printf | grep -q` — see get_schema_hash's identical check above for why
    # (a piped grep -q that exits early on a match can SIGPIPE a still-writing large producer,
    # which pipefail then reports as this check's own failure; reproduced for real on this dump).
    if ! grep -q '^-- PostgreSQL database dump complete$' <<< "$raw_dump"; then
        echo "❌ get_full_object_inventory: pg_dump output has no completion trailer — it died" >&2
        echo "   mid-stream. A truncated dump must never be treated as 'the schema has fewer" >&2
        echo "   objects'; it means pg_dump itself failed partway through." >&2
        exit 1
    fi

    extract_object_identities "$raw_dump"
}

# Allowlist of residue that a migration DELIBERATELY retains across a full rollback, each entry
# tagged with why. Two parallel indexed arrays, not `declare -A`: this repo's devs and CI runner
# use macOS's shipped bash 3.2 (see scripts/pg_dump-via-docker's own comment on the same
# constraint), which has no associative arrays.
ROLLBACK_ALLOWLIST_KEYS=(
    "EXTENSION pg_trgm"
)
ROLLBACK_ALLOWLIST_REASONS=(
    "000101_merchant_name_handling.down.sql deliberately does not drop pg_trgm (comment: other features may use it)"
)

is_allowlisted_residue() {
    local key="$1" i
    for i in "${!ROLLBACK_ALLOWLIST_KEYS[@]}"; do
        [ "${ROLLBACK_ALLOWLIST_KEYS[$i]}" = "$key" ] && return 0
    done
    return 1
}

allowlist_reason_for() {
    local key="$1" i
    for i in "${!ROLLBACK_ALLOWLIST_KEYS[@]}"; do
        if [ "${ROLLBACK_ALLOWLIST_KEYS[$i]}" = "$key" ]; then
            printf '%s' "${ROLLBACK_ALLOWLIST_REASONS[$i]}"
            return 0
        fi
    done
    return 1
}

# assert_clean_rollback_state PRE_INVENTORY POST_INVENTORY
#
# PRE_INVENTORY: get_full_object_inventory captured right after clean_database, BEFORE the first
# `up` in this run — i.e. whatever was already in the database, if anything, before this suite
# touched it. In this suite's actual flow that is always empty, because clean_database's
# `DROP SCHEMA public CASCADE` always cascade-drops any pre-existing extension too (verified
# empirically: CREATE EXTENSION pg_trgm, then DROP SCHEMA public CASCADE, leaves pg_extension with
# zero pg_trgm rows) — but the comparison below is written to be correct regardless, and is
# exercised against BOTH provenances by scripts/__tests__/rollback-residue.test.sh with synthetic
# inventories, since the live DB path can only ever exercise the "absent before" case.
# POST_INVENTORY: get_full_object_inventory captured after `down -all`.
#
# This is deliberately TWO checks, not one "the diff must equal the allowlist" set-equality —
# because a single strict-equality check on the pre/post DIFF breaks depending on whether the
# retained object already existed before the first `up`:
#   Case 1 (pg_trgm absent before the first up): pg_trgm appears only in POST -> it IS residue
#     caused by this run's up/down cycle -> the diff is {EXTENSION pg_trgm}, correctly equal to
#     the allowlist.
#   Case 2 (pg_trgm already installed before the first up, e.g. ambient state on a shared DB):
#     pg_trgm appears in BOTH PRE and POST -> the diff is EMPTY (this run left nothing new behind)
#     -> a strict "diff == allowlist" check would wrongly FAIL here, because {} != {EXTENSION
#     pg_trgm}, even though nothing is actually wrong.
#
# Check A (unexpected new residue): every object in POST but not in PRE — i.e. every object this
# run's up/down cycle actually left behind — must be on the allowlist. Anything else fails. This is
# what catches update_updated_at_column() surviving 000001's rollback (before that was fixed), or
# any future stray object.
#
# Check B (stale allowlist entries): every allowlisted object must still actually be present in
# POST. This deliberately does NOT use the PRE/POST diff (which would reintroduce the Case-2
# problem above) — it only asks "is the allowlisted object still there after rollback," which is
# true in both provenance cases whenever the allowlist entry is still accurate. If some future fix
# makes a migration's .down.sql actually drop what it used to leave behind, the allowlist entry
# becomes stale and this check fails, forcing the comment to be pruned rather than silently
# rotting into a false permission for residue that no longer exists.
assert_clean_rollback_state() {
    local pre_inventory="$1" post_inventory="$2"
    local new_residue line ok=true

    new_residue=$(comm -13 <(printf '%s\n' "$pre_inventory") <(printf '%s\n' "$post_inventory"))
    if [ -n "$new_residue" ]; then
        while IFS= read -r line; do
            [ -n "$line" ] || continue
            if is_allowlisted_residue "$line"; then
                log_warning "Rollback left allowlisted residue: $line ($(allowlist_reason_for "$line"))"
            else
                fail_test "Rollback left unexpected residue not on the allowlist: $line"
                ok=false
            fi
        done <<< "$new_residue"
    fi

    local i entry
    for i in "${!ROLLBACK_ALLOWLIST_KEYS[@]}"; do
        entry="${ROLLBACK_ALLOWLIST_KEYS[$i]}"
        # Here-string, not `printf | grep -qxF` — see get_schema_hash's trailer check for why a
        # piped grep that can exit early is a pipefail/SIGPIPE race against a live producer.
        if ! grep -qxF "$entry" <<< "$post_inventory"; then
            fail_test "Allowlist entry '$entry' is stale — not actually present after rollback (${ROLLBACK_ALLOWLIST_REASONS[$i]}); remove it from ROLLBACK_ALLOWLIST_KEYS or fix whatever stopped retaining it"
            ok=false
        fi
    done

    [ "$ok" = true ]
}

# Clean database (drop all tables and golang-migrate tracking)
clean_database() {
    log_info "Cleaning database..."
    # Drop public schema (all tables including schema_migrations)
    # NOTE: Errors visible for debugging - previously silenced with > /dev/null 2>&1
    psql "$DATABASE_URL" -c "DROP SCHEMA public CASCADE; CREATE SCHEMA public;"
}

# Apply migrations using golang-migrate
apply_migrations() {
    local start_time=$(date +%s)

    # NOTE: Errors visible for debugging - previously silenced with > /dev/null 2>&1
    # golang-migrate requires -database and -path flags
    #
    # FAIL CLOSED on a non-zero exit. This was previously unchecked, so a migrate failure
    # (unreachable server, a DATABASE_URL missing sslmode=disable, dirty state, a genuinely broken
    # migration) left the suite running against a schema where NOTHING had been applied — and the
    # idempotency and performance suites then reported PASSED, because comparing two
    # never-populated schemas trivially succeeds. A migration safety net that goes green when the
    # migrations did not run is worse than no net.
    if ! migrate -database "$DATABASE_URL" -path "$ABS_MIGRATIONS_DIR" up; then
        echo "❌ apply_migrations: 'migrate up' FAILED — aborting rather than testing an unmigrated schema." >&2
        echo "   A common cause is a DATABASE_URL missing '?sslmode=disable'." >&2
        exit 1
    fi

    local end_time=$(date +%s)
    local duration=$((end_time - start_time))
    echo "$duration"
}

# =============================================================================
# Genuine second-execution replay (#1569 round 12 task 1)
# =============================================================================
#
# THE HOLE THIS CLOSES: `apply_migrations` above calls `migrate ... up`. Once schema_migrations
# is already at the latest version, that is a no-op ("no change", 0s) — golang-migrate only ever
# runs a migration file whose version is not yet recorded as applied. Both test_idempotency's
# "2nd time" application and test_data_integrity's "re-apply migrations with data present" step
# called exactly that no-op as their entire idempotency check. Verified live on this branch before
# writing this: both printed "no change" / "0s" and their hash/row-count comparisons therefore
# compared two identical, unmodified snapshots — passing having executed zero migration SQL a
# second time. CLAUDE.md's "Migrations MUST be idempotent" rule had no gate behind it.
#
# THE FIX: actually re-execute each targeted migration's .up.sql a second time, by running it
# directly through psql — NOT through golang-migrate, which will never re-run a version it has
# already recorded. This is not a new mechanism: test_data_integrity already does exactly this for
# migration 000072 (`psql -f ".../000072_....up.sql"`, a few dozen lines below) specifically
# because "golang-migrate marks them applied so the second apply_migrations won't re-run them."
# What follows generalises that one-off call into a loop over a range of migrations instead of
# inventing a parallel technique.
#
# WHY A BOUNDED RANGE, NOT ALL 178: replaying an old migration's .up.sql against TODAY's final
# schema is only sound if nothing between that migration and HEAD renamed or dropped what it
# depends on — otherwise a replay failure would mean "the schema evolved," not "this migration is
# non-idempotent," which is a false positive, not a real finding. That failure mode is not
# hypothetical: it already happened. Migration 000070 targeted the `rules` table, migration 000095
# renamed it to `boosts`, and replaying 000070's own file against the current schema fails outright
# — which is why test_data_integrity below hand-writes an "000070-equivalent" query instead of
# replaying the file. Blindly replaying migrations 000001-000148 would reproduce that exact
# failure dozens of times over, for schema-drift reasons that have nothing to do with idempotency.
#
# So this replays only REPLAY_RANGE_START_MIGRATION..HEAD: the migrations added on this branch
# (#1569; 000149 is the first file this branch's `git diff --diff-filter=A` against its merge-base
# with staging shows as newly added, as of the commit this comment was written against). Replaying
# them in their own original order against the final schema reproduces the exact preconditions
# each one saw when it was first applied for real, AS LONG AS none of them tears down or narrows
# something an EARLIER in-range file created or relies on.
#
# ⚠ DROP TABLE / DROP COLUMN / RENAME are the hazards assert_replay_range_has_no_destructive_ddl
# below actually checks for, NOT a claimed-exhaustive list of everything that could invalidate this
# assumption. They are the ones found by direct inspection when this range was chosen (000149-000179,
# checked #1569 round 12 fix round 1) and the ones this specific branch's migration style (additive:
# new tables/columns/backfills) could plausibly produce. An in-range `ALTER COLUMN ... TYPE`, or a
# new `NOT NULL`/`CHECK` added to a column an EARLIER in-range file already depends on, would equally
# break the assumption and this guard does NOT catch it — none exists in the migrations added on
# this branch (000149 through 000180 as of #1569 round 12 task 7; REPLAY_RANGE_START_MIGRATION's
# upper bound is UNBOUNDED — see list_replay_range_up_files — so this citation drifts as new
# migrations land and MUST be re-derived, not trusted, on the next round that touches this file),
# but that is a fact about today's migrations, not a guarantee the guard enforces. Widen the guard
# (and this comment) the day a real case like that appears, rather than trusting the list is complete.

#
# Migrations 000001-000148 are SKIPPED for this specific check, on purpose, for the reason above —
# NOT silently: every run logs the exact skipped range so "covered everything" is never implied by
# omission. Older migrations are not left completely unchecked: they still go through the empty-DB
# idempotency path (`apply_migrations` called twice from a clean database) that this suite already
# ran before #1569, via the initial no-op-detecting call in test_idempotency; what was missing was
# ANY re-execution against non-empty state, which is what this section adds.
#
# ⚠⚠ WHAT THIS MECHANISM DOES NOT PROVE (#1569 round 12 fix round 1 — stated precisely so this gate
# is never over-read the way the ORIGINAL no-op gate was). Both replay_migrations_up_sql_range call
# sites detect non-idempotency ONLY when re-executing a migration's .up.sql a second time ERRORS.
# They do NOT detect semantic non-idempotency that runs to completion without error:
#   - test_idempotency's hash comparison is `pg_dump --schema-only` (get_schema_hash). DATA IS NEVER
#     IN THE HASH. An unguarded `INSERT` that duplicates a row on replay, or an `UPDATE` that is not
#     a no-op the second time (writes a DIFFERENT value, not the same one), changes zero DDL and
#     therefore changes NOTHING this hash can see. It would pass silently.
#   - test_data_integrity's NAMED row-count check (a few hundred lines below) covers exactly FIVE
#     pre-branch tables: users, privacy_settings, linked_items, accounts, transactions. NONE of
#     this branch's own migrations (000149 through 000180 as of #1569 round 14 — re-derive
#     this against the current migration set, do not trust the number) INSERT into any of those
#     five — they add columns/tables/triggers and backfill EXISTING rows in OTHER tables
#     (account_status_history, household_account_shares, user_accounts, etc.). A duplicate-row
#     defect introduced by one of THIS branch's own migrations was therefore invisible to that
#     check too, for the same reason it is invisible to the schema hash: nobody counted those
#     tables.
#   - Both of round 12's real findings (the 000178 CREATE-TEMP-TABLE replay artefact, and the real
#     000177 trigger defect) happen to THROW on replay, which is exactly why this gate looked
#     complete when it was not — a defect that silently duplicates a row without erroring sailed
#     through both suites unnoticed.
#
# ✅ THE ROW-COUNT HALF OF THAT GAP IS CLOSED (#1569 round 14 task 8). test_data_integrity now also
# runs a WHOLE-SCHEMA replay-stability check: it replays the in-range migrations a SECOND time and
# compares COUNT(*) for every ordinary table in the public schema (enumerated from pg_class, not
# from a hand-written list that could go stale) across that second replay. See
# capture_all_table_counts and the block that calls it for why the comparison has to straddle the
# SECOND replay rather than the first, and for a demonstration that it can actually go RED.
# ⚠ STILL OUT OF SCOPE, and named so it stays deliberate rather than forgotten: row-CONTENT diffs.
# An UPDATE that rewrites the same rows with a different value on each execution changes no counts
# and passes every check in this file.
REPLAY_RANGE_START_MIGRATION="000149"

# Lists this run's ABS_MIGRATIONS_DIR *.up.sql files whose zero-padded numeric prefix is >=
# REPLAY_RANGE_START_MIGRATION, sorted ascending. String comparison (not arithmetic) deliberately:
# these prefixes are fixed-width, zero-padded 6-digit strings, and bash arithmetic treats a
# leading-zero literal like 000149 as octal, which would misparse a range boundary silently.
list_replay_range_up_files() {
    local f base num
    for f in "$ABS_MIGRATIONS_DIR"/*.up.sql; do
        base="$(basename "$f")"
        num="${base%%_*}"
        if [[ "$num" > "$REPLAY_RANGE_START_MIGRATION" || "$num" == "$REPLAY_RANGE_START_MIGRATION" ]]; then
            printf '%s\n' "$f"
        fi
    done | sort
}

list_replay_range_skipped_up_files() {
    local f base num
    for f in "$ABS_MIGRATIONS_DIR"/*.up.sql; do
        base="$(basename "$f")"
        num="${base%%_*}"
        if [[ "$num" < "$REPLAY_RANGE_START_MIGRATION" ]]; then
            printf '%s\n' "$f"
        fi
    done | sort
}

# Re-verifies, on every run, that no migration inside the replay range contains one of the KNOWN
# hazard shapes for "replaying this range in order against the final schema reproduces original
# preconditions" — see the big comment above for why that assumption is load-bearing, and for why
# this list is a set of hazards FOUND, not a claimed-exhaustive enumeration of every way the
# assumption could break. Strips whole-line `--` comments before scanning so prose (e.g. 000164's
# "Never updated when the boost is renamed", inside a COMMENT ON COLUMN string) cannot trip it.
#
# The RENAME alternation covers all three legal PostgreSQL forms, checked by direct inspection of
# `ALTER ... RENAME` syntax and confirmed by mutation testing (#1569 round 12 fix round 1: a scratch
# in-range migration using each shorthand form was added, confirmed to trip this guard, then
# removed):
#   - `RENAME TO new_name`            (table/index/etc. rename)                 -> RENAME\s+TO\b
#   - `RENAME COLUMN old TO new`      (explicit column rename)                  -> RENAME\s+COLUMN\b
#   - `RENAME old_col TO new_col`     (column-rename shorthand — COLUMN is OPTIONAL per the
#     PostgreSQL grammar; the FIRST regex draft here missed exactly this form) -> RENAME\s+<ident>\s+TO\b
# All three require "RENAME" to be followed by whitespace, which "renamed" (as prose) never is
# (immediately followed by "d"), so SQL-shaped adjacency still cannot fire on past-tense prose on
# the same line as real SQL.
assert_replay_range_has_no_destructive_ddl() {
    local f hits all_hits=""
    for f in $(list_replay_range_up_files); do
        hits=$(/usr/bin/grep -viE '^[[:space:]]*--' "$f" \
            | /usr/bin/grep -inE 'DROP[[:space:]]+TABLE|DROP[[:space:]]+COLUMN|RENAME[[:space:]]+(TO|COLUMN)[[:space:]]|RENAME[[:space:]]+[A-Za-z_][A-Za-z0-9_]*[[:space:]]+TO[[:space:]]' || true)
        if [ -n "$hits" ]; then
            all_hits="${all_hits}--- $f ---"$'\n'"${hits}"$'\n'
        fi
    done

    if [ -n "$all_hits" ]; then
        log_error "Replay-range safety assumption violated: a migration between $REPLAY_RANGE_START_MIGRATION and HEAD contains DROP TABLE / DROP COLUMN / RENAME DDL."
        log_error "Replaying this range against the final schema is only sound because none of these files"
        log_error "tear down what an earlier one in the SAME range created. That is no longer true:"
        echo "$all_hits" >&2
        log_error "Fix: either make the offending migration idempotent-and-driftless, or move"
        log_error "REPLAY_RANGE_START_MIGRATION forward past it (and re-verify everything after the new"
        log_error "start still has no destructive DDL) — do not silently ignore this."
        return 1
    fi
    return 0
}

# Actually re-executes every .up.sql in the replay range, directly via psql (never through
# golang-migrate — see the big comment above for why). ON_ERROR_STOP makes a real SQL error abort
# immediately with a non-zero psql exit code instead of continuing past it silently. Logs every
# file it runs and the skipped range, so a reader never has to guess what this call covered.
#
# ⚠ `-1` / `--single-transaction` is REQUIRED here, not optional. golang-migrate's postgres driver
# wraps an entire .up.sql file in ONE transaction (confirmed by 000074's own header comment: "Plain
# CREATE INDEX (not CONCURRENTLY) because golang-migrate wraps .up.sql in a transaction block").
# Plain `psql -f` autocommits each statement separately. That difference is not cosmetic: replaying
# 000178 without `-1` produced a false RED — `CREATE TEMP TABLE ... ON COMMIT DROP` autocommitted
# immediately (dropping the temp table right after creating it) instead of staying alive until the
# file's real end, so the very next statement's INSERT into that table failed with "relation does
# not exist." That is a fidelity gap in THIS replay mechanism, not a defect in the migration: under
# migrate's real single-transaction execution (both in production and in this suite's own first
# `apply_migrations` pass) ON COMMIT DROP never fires until the migration's actual COMMIT, by which
# point the temp table is no longer needed. `-1` reproduces that same single-transaction semantics
# for the replay, closing the gap rather than papering over the symptom.
# replay_file_runs_outside_a_transaction: does this .up.sql have to be replayed WITHOUT `-1`?
#
# ⚠ THIS EXISTS BECAUSE `-1` IS FIDELITY FOR MULTI-STATEMENT FILES AND INFIDELITY FOR THIS ONE CASE,
# and the composition was never tested until #1569 round 15 task 9 fix 1 put the branch's first
# CONCURRENTLY migration inside the replay range. golang-migrate's postgres driver sends a file's
# whole contents as ONE simple query. Postgres wraps a MULTI-statement simple query in an implicit
# transaction (which is what `-1` above reproduces, and what 000178's ON COMMIT DROP finding is
# about) but does NOT wrap a SINGLE-statement one — which is the only reason
# `CREATE INDEX CONCURRENTLY`, illegal inside any transaction block, can be a migration at all.
# 000077/000082/000138/000144 have relied on that since long before this range existed.
#
# So for a file whose statement is CONCURRENTLY, `-1` does not reproduce golang-migrate — it
# contradicts it, and deterministically:
#
#     ERROR:  CREATE INDEX CONCURRENTLY cannot run inside a transaction block
#
# Verified by running `psql -1 -f` against a migrated database, on both the up and the down file.
# Dropping `-1` for exactly these files makes the replay MATCH production instead of failing it.
#
# WHY MATCHING ON THE KEYWORD IS SOUND rather than a heuristic: a file containing CONCURRENTLY
# cannot be a multi-statement file in the first place — golang-migrate would send it as a
# multi-statement simple query, Postgres would wrap it, and the migration would fail in PRODUCTION
# with the same error. Single-statement is therefore not an assumption this makes about such a file;
# it is a property the file must already have to work at all.
#
# ⚠ "THE KEYWORD" MEANS THE KEYWORD IN EXECUTABLE SQL, AND GETTING THAT WRONG IS UNSAFE IN THE
# DIRECTION THAT MATTERS (#1569 round 16 task 9, D-1). This used to be
#
#     /usr/bin/grep -viE '^[[:space:]]*--' "$f" | /usr/bin/grep -qiE '\bCONCURRENTLY\b'
#
# which strips only FULL-LINE comments. A file whose only occurrence of the word is a TRAILING
# comment, a /* block comment */, or SQL string data was classified as needing no transaction, and
# was then replayed WITHOUT `-1` — silently removing single-transaction semantics from an unrelated
# MULTI-statement migration, which is exactly the 000178 ON COMMIT DROP class of false RED (or worse,
# a false GREEN) the `-1` above exists to prevent. No file in the tree has that shape today, so this
# is a latent defect, not an active one; it is fixed because the shape is one comment away.
#
# ⚠⚠ THE RULE BELOW IS A PORT, NOT A NEW REGEX. This is the same lexical class that
# blankSQLCommentsAndLiterals (backend/internal/repository/history_delete_sql_guard_test.go) was
# rewritten FOUR times to get right for the G4 guards, and each rewrite was forced by a hole a fresh
# regex had. The awk below is a line-for-line port of that Go scanner's state machine — read its doc
# comment there for WHY each rule is what it is; the summary is:
#
#   * ONE left-to-right pass, state has priority. Once a `'` opens a string, `--` and `/*` inside it
#     are data, not comment starts (000164's RAISE EXCEPTION message contains a literal `--`).
#   * `''` inside a string is an escape, not a close.
#   * A `$tag$` dollar-quoted region's INTERIOR is lexed independently and BOUNDED to its own
#     matching close tag, so a prose apostrophe inside it cannot swallow the rest of the file, and a
#     `$1$`/`$word$`-shaped substring inside an ORDINARY string is never inspected as a tag at all
#     (tags are only ever recognised in default state). Dollar-quoted bodies are NOT blanked — they
#     are procedural code, and a CONCURRENTLY inside one would be real.
#   * A tag name may not start with a digit (Postgres's own rule).
#   * Block comments are non-nested; an unterminated string blanks to EOF, an unterminated dollar
#     region keeps being scanned normally (fail toward SEEING more, not less).
#
# Blanked bytes become SPACES, never nothing, so `CONCURR/*x*/ENTLY` cannot be spliced into a false
# match. The keyword test then runs per line (the keyword cannot span one) with explicit
# non-word-character boundaries, awk having no \b.
#
# scripts/__tests__/replay-concurrently-transaction.test.sh exercises every one of these cases and
# pins the whole migrations corpus's classification explicitly.
replay_file_runs_outside_a_transaction() {
    local f="$1"
    /usr/bin/awk '
        function isIdentStart(c) { return c != "" && index(ALPHA, c) > 0 }
        function isIdentCont(c)  { return c != "" && (index(ALPHA, c) > 0 || index(DIGIT, c) > 0) }

        # The full `$tag$` delimiter starting at i, or "" if none starts there. Only ever called
        # from default state — that restriction, not anything here, is what keeps a $..$-shaped
        # substring inside an ordinary string from being treated as a tag.
        function dollarTagAt(i,   j) {
            if (i > N || substr(S, i, 1) != "$") return ""
            j = i + 1
            if (j <= N && isIdentStart(substr(S, j, 1)))
                while (j <= N && isIdentCont(substr(S, j, 1))) j++
            if (j <= N && substr(S, j, 1) == "$") return substr(S, i, j - i + 1)
            return ""
        }

        # First literal occurrence of tag in S[from, hi), or -1. Bounded to hi so a match past the
        # enclosing region is never found.
        function indexOfTag(tag, from, hi,   k, L) {
            L = length(tag)
            for (k = from; k + L <= hi; k++)
                if (substr(S, k, L) == tag) return k
            return -1
        }

        function blank(from, to,   k) {
            for (k = from; k < to; k++)
                if (substr(S, k, 1) != "\n") BL[k] = 1
        }

        # The port of blankSQLRange: hi is EXCLUSIVE.
        function scanRange(lo, hi,   i, j, e, c, c2, tag, body, endTag) {
            i = lo
            while (i < hi) {
                c  = substr(S, i, 1)
                c2 = (i + 1 < hi) ? substr(S, i + 1, 1) : ""
                if (i + 1 < hi && c == "-" && c2 == "-") {
                    j = i
                    while (j < hi && substr(S, j, 1) != "\n") j++
                    blank(i, j); i = j
                } else if (i + 1 < hi && c == "/" && c2 == "*") {
                    j = i + 2
                    while (j + 1 < hi && (substr(S, j, 1) != "*" || substr(S, j + 1, 1) != "/")) j++
                    e = j + 2
                    if (e > hi) e = hi
                    blank(i, e); i = e
                } else if ((tag = dollarTagAt(i)) != "") {
                    body = i + length(tag)
                    endTag = indexOfTag(tag, body, hi)
                    if (endTag < 0) { scanRange(body, hi); return }
                    scanRange(body, endTag)     # interior lexed INDEPENDENTLY AND BOUNDED
                    i = endTag + length(tag)
                } else if (c == "'"'"'") {
                    j = i + 1
                    while (j < hi) {
                        if (substr(S, j, 1) == "'"'"'") {
                            if (j + 1 < hi && substr(S, j + 1, 1) == "'"'"'") { j += 2; continue }
                            j++; break
                        }
                        j++   # a $ in here is DATA — never inspected as a possible tag
                    }
                    blank(i, j); i = j
                } else {
                    i++
                }
            }
        }

        BEGIN {
            ALPHA = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ_"
            DIGIT = "0123456789"
            S = ""
        }
        { S = S $0 "\n" }
        END {
            N = length(S)
            scanRange(1, N + 1)
            line = ""
            for (k = 1; k <= N; k++) {
                c = substr(S, k, 1)
                if (c == "\n") {
                    if (tolower(line) ~ /(^|[^a-z0-9_])concurrently([^a-z0-9_]|$)/) exit 0
                    line = ""
                } else {
                    line = line (BL[k] ? " " : c)
                }
            }
            if (tolower(line) ~ /(^|[^a-z0-9_])concurrently([^a-z0-9_]|$)/) exit 0
            exit 1
        }
    ' "$f"
}

replay_migrations_up_sql_range() {
    local f
    local skipped_count replay_files replayed_count
    skipped_count=$(list_replay_range_skipped_up_files | wc -l | tr -d ' ')
    replay_files=$(list_replay_range_up_files)

    log_info "Re-executing migrations ${REPLAY_RANGE_START_MIGRATION} through HEAD directly via psql (genuine second execution, not golang-migrate no-op)..."
    log_info "SKIPPED: $skipped_count migration(s) before ${REPLAY_RANGE_START_MIGRATION} — replaying them against the final schema is unsound (see assert_replay_range_has_no_destructive_ddl / the comment above replay_migrations_up_sql_range for why); they are still exercised by the empty-DB first+second apply_migrations pass."

    replayed_count=0
    while IFS= read -r f; do
        [ -z "$f" ] && continue
        local txn_flag="-1" txn_desc="as a single transaction"
        if replay_file_runs_outside_a_transaction "$f"; then
            txn_flag=""
            txn_desc="outside a transaction, because it uses CONCURRENTLY"
        fi
        log_info "  replaying $(basename "$f") ($txn_desc)"
        # shellcheck disable=SC2086 # $txn_flag is deliberately word-split: it is a flag or nothing.
        if ! psql "$DATABASE_URL" -v ON_ERROR_STOP=1 $txn_flag -f "$f" > /dev/null; then
            log_error "Migration $(basename "$f") is NOT idempotent: re-executing its .up.sql ($txn_desc, matching golang-migrate's own execution semantics) against the current (already-migrated) schema failed."
            return 1
        fi
        ((replayed_count++))
    done <<< "$replay_files"

    log_success "Replayed $replayed_count migration(s) a second time with no error"
    return 0
}

# Rollback all migrations using golang-migrate
# Returns: "duration:error_type" where error_type is "none" or "other"
rollback_migrations() {
    local start_time=$(date +%s)

    # Rollback all migrations using -all flag
    # golang-migrate executes SQL directly without static analysis limitations
    local rollback_output
    local error_type="none"
    rollback_output=$(migrate -database "$DATABASE_URL" -path "$ABS_MIGRATIONS_DIR" down -all 2>&1) || {
        error_type="other"
        echo "$rollback_output" >&2
    }

    local end_time=$(date +%s)
    local duration=$((end_time - start_time))
    echo "${duration}:${error_type}"
}

# =============================================================================
# Test 1: Idempotency Testing
# =============================================================================

test_idempotency() {
    log_section "TEST 1: IDEMPOTENCY"

    log_info "Testing that migrations can be applied multiple times safely..."
    log_info "This is critical to prevent Issue #279-style failures"
    echo ""

    # Clean start
    clean_database

    # First application
    log_info "Applying migrations (1st time)..."
    # `local duration1=$(apply_migrations)` would mask apply_migrations' real exit status behind
    # `local`'s own (always-0) status — confirmed empirically: `set -e; local f() { local x=$(false); }`
    # never aborts, vs. a bare `x=$(false)` which does. Declaring `local` on its own line first,
    # then assigning in a plain `if !` test, makes the check live: bash treats that assignment's
    # own exit status as testable.
    local duration1
    if ! duration1=$(apply_migrations); then
        fail_test "First migration application failed"
        return 1
    fi
    log_success "First application completed in ${duration1}s"

    # Capture schema hash
    log_info "Capturing schema hash..."
    local hash1
    if ! hash1=$(get_schema_hash); then
        fail_test "Failed to capture schema hash after first application"
        return 1
    fi
    log_info "Schema hash (1st): $hash1"

    # Second application (idempotency test)
    #
    # `apply_migrations` here is a golang-migrate no-op ("no change") once schema_migrations is
    # already at the latest version — kept as a cheap sanity check that migrate itself still exits
    # 0 against an up-to-date tracking table, but it executes ZERO migration SQL a second time, so
    # it alone proves nothing about idempotency (confirmed live: "no change" / 0s, every run,
    # regardless of whether any migration has an IF NOT EXISTS guard). The real second execution is
    # replay_migrations_up_sql_range below, which runs each in-range migration's actual .up.sql a
    # second time via psql directly.
    log_info "Applying migrations (2nd time - golang-migrate no-op sanity check, NOT the idempotency test)..."
    # Same masked-local fix as duration1 above.
    local duration2
    if ! duration2=$(apply_migrations); then
        fail_test "Second migration application failed - migrations are NOT idempotent!"
        log_error "This likely means migrations lack IF NOT EXISTS guards"
        log_error "See migrations 000013, 000015 for good idempotency patterns"
        return 1
    fi
    log_success "Second (no-op) application completed in ${duration2}s"

    # This is the actual idempotency test: re-execute each in-range migration's .up.sql a second
    # time, for real, via psql (see replay_migrations_up_sql_range's header comment for the full
    # rationale and why the range is bounded).
    if ! replay_migrations_up_sql_range; then
        fail_test "IDEMPOTENCY TEST FAILED - a migration in the replay range is not safe to re-execute"
        return 1
    fi

    # Capture schema hash again
    log_info "Capturing schema hash (2nd)..."
    local hash2
    if ! hash2=$(get_schema_hash); then
        fail_test "Failed to capture schema hash after second application"
        return 1
    fi
    log_info "Schema hash (2nd): $hash2"

    # Compare hashes
    echo ""
    if [ "$hash1" = "$hash2" ]; then
        pass_test "IDEMPOTENCY TEST PASSED - Schema unchanged after re-applying migrations"
        log_success "Migrations are safe to re-run (critical for deployment reliability)"
        return 0
    else
        fail_test "IDEMPOTENCY TEST FAILED - Schema changed after re-applying migrations!"
        log_error "Schema hash mismatch:"
        log_error "  1st application: $hash1"
        log_error "  2nd application: $hash2"
        log_error ""

        # Show diff between the two dumps for debugging
        echo "🔍 Analyzing differences between schema dumps..." >&2
        DUMP1=$(ls -t /tmp/schema_dump_*.sql 2>/dev/null | sed -n '2p')  # Second newest
        DUMP2=$(ls -t /tmp/schema_dump_*.sql 2>/dev/null | sed -n '1p')  # Newest

        if [ -f "$DUMP1" ] && [ -f "$DUMP2" ]; then
            echo "Comparing:" >&2
            echo "  First run:  $DUMP1" >&2
            echo "  Second run: $DUMP2" >&2
            echo "" >&2
            echo "=== DIFF OUTPUT (first 100 lines) ===" >&2
            diff -u "$DUMP1" "$DUMP2" | head -100 >&2 || true
            echo "===" >&2
        else
            log_error "Could not find dump files for comparison"
            log_error "  DUMP1: ${DUMP1:-not found}"
            log_error "  DUMP2: ${DUMP2:-not found}"
        fi
        echo "" >&2

        log_error "This indicates migrations are NOT idempotent"
        log_error "Check for missing IF NOT EXISTS guards on:"
        log_error "  - CREATE TABLE"
        log_error "  - CREATE INDEX"
        log_error "  - ALTER TABLE ADD COLUMN"
        return 1
    fi
}

# =============================================================================
# Test 2: Performance Benchmarking
# =============================================================================

test_performance() {
    log_section "TEST 2: PERFORMANCE BENCHMARKING"

    log_info "Measuring migration performance..."
    log_info "Thresholds: WARN > ${PERF_WARN_THRESHOLD}s, FAIL > ${PERF_FAIL_THRESHOLD}s"
    echo ""

    # Clean start
    clean_database

    # Time the migration application
    #
    # This used to be `duration_output=$(apply_migrations 2>&1)` — a BARE assignment (no `local`
    # on the same line) — followed by `local apply_exit_code=$?` and an `if [ $apply_exit_code -ne
    # 0 ]` check. That check was dead code: under `set -euo pipefail`, a bare `var=$(cmd)` whose
    # command substitution fails aborts the WHOLE SCRIPT right there (confirmed empirically:
    # `set -e; x=$(false); echo reached` never prints "reached", exits 1 immediately) — so
    # `apply_exit_code=$?` could never be reached on the failure path it existed to check. The
    # non-negated `if cmd; then ... else ... fi` form below is what actually makes this live: bash
    # evaluates the assignment's exit status as the `if`'s condition, which suppresses `set -e`
    # for exactly this one command, and `$?` inside the `else` branch is the real exit code.
    local start_time=$(date +%s)
    local duration_output
    local apply_exit_code
    if duration_output=$(apply_migrations 2>&1); then
        apply_exit_code=0
    else
        apply_exit_code=$?
    fi
    local end_time=$(date +%s)
    local total_duration=$((end_time - start_time))

    if [ $apply_exit_code -ne 0 ]; then
        fail_test "MIGRATION APPLICATION FAILED during performance test"
        log_error "migrate up returned exit code: $apply_exit_code"
        log_error "Output: $duration_output"
        return 1
    fi

    # Check against thresholds
    local perf_status="PASS"
    if [ $total_duration -gt $PERF_FAIL_THRESHOLD ]; then
        fail_test "PERFORMANCE TEST FAILED - Migrations took ${total_duration}s (threshold: ${PERF_FAIL_THRESHOLD}s)"
        log_error "Migrations are too slow - consider optimization or splitting"
        perf_status="FAIL"
    elif [ $total_duration -gt $PERF_WARN_THRESHOLD ]; then
        log_warning "PERFORMANCE WARNING - Migrations took ${total_duration}s (warning threshold: ${PERF_WARN_THRESHOLD}s)"
        log_warning "Consider optimizing slow migrations"
        pass_test "Performance test passed with warnings"
        perf_status="WARN"
    else
        pass_test "PERFORMANCE TEST PASSED - Migrations completed in ${total_duration}s"
        perf_status="PASS"
    fi

    # Generate JSON performance report
    # NOTE: Count only .up.sql files (forward migrations), not .down.sql
    cat > "$PERF_REPORT_FILE" <<EOF
{
  "timestamp": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "total_duration_seconds": $total_duration,
  "threshold_warn_seconds": $PERF_WARN_THRESHOLD,
  "threshold_fail_seconds": $PERF_FAIL_THRESHOLD,
  "status": "$perf_status",
  "migrations_count": $(ls -1 "$ABS_MIGRATIONS_DIR"/*.up.sql 2>/dev/null | wc -l | tr -d ' ')
}
EOF

    log_info "Performance report saved to: $PERF_REPORT_FILE"

    if [ "$perf_status" = "FAIL" ]; then
        return 1
    fi
    return 0
}

# =============================================================================
# Test 3: Data Integrity Testing
# =============================================================================

test_data_integrity() {
    log_section "TEST 3: DATA INTEGRITY"

    log_info "Testing that idempotent migrations preserve existing data..."
    echo ""

    # Clean start and apply migrations
    clean_database
    # apply_migrations is called directly here (no command substitution / subshell), so a
    # `migrate up` failure calls its own `exit 1` in THIS shell and the whole script aborts right
    # there with apply_migrations' own diagnostic — there is no "after" for a check to run. Only
    # stdout (the duration, unused here) is discarded so that diagnostic reaches the console
    # (previously `2>&1` sent it to /dev/null too, so a failure here died with no explanation).
    apply_migrations > /dev/null

    # Seed test data
    log_info "Seeding test data..."

    # Insert test users.
    # `if !` + `<<EOF > /dev/null`: three-way fix for set -e + ON_ERROR_STOP interaction:
    #   1. `if !` prevents set -e from killing the script when psql exits non-zero (code 3).
    #   2. `> /dev/null` suppresses verbose INSERT success noise (stdout only).
    #   3. stderr is NOT redirected — SQL error messages appear in CI logs for diagnosis.
    # `\set ON_ERROR_STOP on` makes psql exit 3 immediately on the first SQL error
    # instead of continuing and silently masking failures with a 0 exit code.
    # Heredoc is QUOTED (<<'EOF'), so bash performs no expansion on the SQL below. Unquoted, the
    # backticks in the "`category` column dropped in migration 000065" comment were command-
    # substituted, printing "category: command not found" on every run — harmless there because it
    # sits in a SQL comment, but it meant any `$` or backtick in this block would be silently
    # mangled before Postgres ever saw it, and it added a spurious error line that could mask a real
    # one. Nothing in this block needs shell expansion; the sibling backfill heredoc below already
    # uses the quoted form.
    if ! psql "$DATABASE_URL" <<'EOF' > /dev/null
\set ON_ERROR_STOP on
-- primary_region must be 'iad' after migrations 000005 and 000059.
-- subscription_tier is the column name (not 'tier'); valid values post-migration-000040:
--   premium, household_2, household_3, household_4, household_5
-- Keep subscription_tier explicit here because the legacy column default is still 'free',
-- which no longer satisfies the final valid_subscription_tier CHECK constraint.
INSERT INTO users (id, clerk_user_id, email, primary_region, subscription_tier, created_at, updated_at)
VALUES (
    '00000000-0000-0000-0000-000000000001'::uuid,
    'user_test_1',
    'user1@test.com',
    'iad',
    'premium',
    NOW(),
    NOW()
);

INSERT INTO users (id, clerk_user_id, email, primary_region, subscription_tier, created_at, updated_at)
VALUES (
    '00000000-0000-0000-0000-000000000002'::uuid,
    'user_test_2',
    'user2@test.com',
    'iad',
    'premium',
    NOW(),
    NOW()
);

INSERT INTO users (id, clerk_user_id, email, primary_region, subscription_tier, created_at, updated_at)
VALUES (
    '00000000-0000-0000-0000-000000000003'::uuid,
    'user_test_3',
    'user3@test.com',
    'iad',
    'household_5',
    NOW(),
    NOW()
);

-- Privacy settings (no data_retention_days column exists in the final schema)
INSERT INTO privacy_settings (user_id, created_at, updated_at)
VALUES
    ('00000000-0000-0000-0000-000000000001'::uuid, NOW(), NOW()),
    ('00000000-0000-0000-0000-000000000002'::uuid, NOW(), NOW()),
    ('00000000-0000-0000-0000-000000000003'::uuid, NOW(), NOW());

-- Linked items (access_token_encrypted is the column name per migration 000006)
INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted, provider, status, created_at, updated_at)
VALUES
    ('00000000-0000-0000-0000-000000000011'::uuid, '00000000-0000-0000-0000-000000000001'::uuid, 'item-001', 'encrypted-token-1', 'plaid', 'active', NOW(), NOW()),
    ('00000000-0000-0000-0000-000000000012'::uuid, '00000000-0000-0000-0000-000000000002'::uuid, 'item-002', 'encrypted-token-2', 'plaid', 'active', NOW(), NOW()),
    ('00000000-0000-0000-0000-000000000013'::uuid, '00000000-0000-0000-0000-000000000003'::uuid, 'item-003', 'encrypted-token-3', 'plaid', 'active', NOW(), NOW());

-- Accounts (user_id is NOT NULL — each account belongs to the same-numbered user)
INSERT INTO accounts (id, user_id, linked_item_id, plaid_account_id, name, official_name, type, subtype, created_at, updated_at)
VALUES
    ('00000000-0000-0000-0000-000000000021'::uuid, '00000000-0000-0000-0000-000000000001'::uuid, '00000000-0000-0000-0000-000000000011'::uuid, 'plaid-acc-001', 'Checking', 'Main Checking', 'depository', 'checking', NOW(), NOW()),
    ('00000000-0000-0000-0000-000000000022'::uuid, '00000000-0000-0000-0000-000000000002'::uuid, '00000000-0000-0000-0000-000000000012'::uuid, 'plaid-acc-002', 'Savings', 'High Yield Savings', 'depository', 'savings', NOW(), NOW()),
    ('00000000-0000-0000-0000-000000000023'::uuid, '00000000-0000-0000-0000-000000000003'::uuid, '00000000-0000-0000-0000-000000000013'::uuid, 'plaid-acc-003', 'Credit Card', 'Premium Rewards Card', 'credit', 'credit card', NOW(), NOW());

-- Transactions (PFC v2 canonical, #1073 — `category` column dropped in migration 000065)
-- user_id and source remain required in the final schema; linked_item_id is nullable but
-- included here so the seed preserves the account/item chain.
INSERT INTO transactions (
    id, user_id, account_id, linked_item_id, source, external_transaction_id,
    amount, date, name, pfc_primary, pfc_detailed, pending, created_at, updated_at
)
VALUES
    (gen_random_uuid(), '00000000-0000-0000-0000-000000000001'::uuid, '00000000-0000-0000-0000-000000000021'::uuid, '00000000-0000-0000-0000-000000000011'::uuid, 'plaid', 'txn-001', -50.00, '2025-11-01', 'Grocery Store', 'GENERAL_MERCHANDISE', 'GENERAL_MERCHANDISE_GROCERIES', false, NOW(), NOW()),
    (gen_random_uuid(), '00000000-0000-0000-0000-000000000001'::uuid, '00000000-0000-0000-0000-000000000021'::uuid, '00000000-0000-0000-0000-000000000011'::uuid, 'plaid', 'txn-002', -25.50, '2025-11-02', 'Gas Station', 'TRANSPORTATION', 'TRANSPORTATION_GAS', false, NOW(), NOW()),
    (gen_random_uuid(), '00000000-0000-0000-0000-000000000001'::uuid, '00000000-0000-0000-0000-000000000021'::uuid, '00000000-0000-0000-0000-000000000011'::uuid, 'plaid', 'txn-003', 2500.00, '2025-11-05', 'Paycheck', 'INCOME', 'INCOME_WAGES', false, NOW(), NOW()),
    (gen_random_uuid(), '00000000-0000-0000-0000-000000000002'::uuid, '00000000-0000-0000-0000-000000000022'::uuid, '00000000-0000-0000-0000-000000000012'::uuid, 'plaid', 'txn-004', -100.00, '2025-11-03', 'Online Shopping', 'GENERAL_MERCHANDISE', 'GENERAL_MERCHANDISE_ONLINE_MARKETPLACES', false, NOW(), NOW()),
    (gen_random_uuid(), '00000000-0000-0000-0000-000000000002'::uuid, '00000000-0000-0000-0000-000000000022'::uuid, '00000000-0000-0000-0000-000000000012'::uuid, 'plaid', 'txn-005', 100.00, '2025-11-01', 'Transfer from Checking', 'TRANSFER_IN', 'TRANSFER_IN_ACCOUNT_TRANSFER', false, NOW(), NOW()),
    (gen_random_uuid(), '00000000-0000-0000-0000-000000000003'::uuid, '00000000-0000-0000-0000-000000000023'::uuid, '00000000-0000-0000-0000-000000000013'::uuid, 'plaid', 'txn-006', -200.00, '2025-11-04', 'Restaurant', 'FOOD_AND_DRINK', 'FOOD_AND_DRINK_RESTAURANTS', false, NOW(), NOW()),
    (gen_random_uuid(), '00000000-0000-0000-0000-000000000003'::uuid, '00000000-0000-0000-0000-000000000023'::uuid, '00000000-0000-0000-0000-000000000013'::uuid, 'plaid', 'txn-007', -75.00, '2025-11-06', 'Utilities', 'RENT_AND_UTILITIES', 'RENT_AND_UTILITIES_GAS_AND_ELECTRICITY', false, NOW(), NOW());

-- User accounts (chain transparency, #1109)
INSERT INTO user_accounts (id, user_id, institution_id, mask, primary_type, created_at, updated_at)
VALUES
    ('00000000-0000-0000-0000-000000000031'::uuid, '00000000-0000-0000-0000-000000000001'::uuid, 'ins-test-001', '1234', 'depository', NOW(), NOW()),
    ('00000000-0000-0000-0000-000000000032'::uuid, '00000000-0000-0000-0000-000000000002'::uuid, 'ins-test-002', '5678', 'depository', NOW(), NOW());

-- Link accounts for migration 000070/000072 backfill
UPDATE accounts SET user_account_id = '00000000-0000-0000-0000-000000000031'::uuid WHERE id = '00000000-0000-0000-0000-000000000021'::uuid;
UPDATE accounts SET user_account_id = '00000000-0000-0000-0000-000000000032'::uuid WHERE id = '00000000-0000-0000-0000-000000000022'::uuid;

-- Migration 000072: string-valued account_id condition
-- Uses target_kind/target_pfc_primary (target_category was dropped in migration 000065_pfc_v2_canonical)
INSERT INTO categorization_rules (id, user_id, name, conditions, target_kind, target_pfc_primary, created_at, updated_at)
VALUES (
    '00000000-0000-0000-0000-000000000041'::uuid,
    '00000000-0000-0000-0000-000000000001'::uuid,
    'Test Rule String Condition',
    '[{"type": "account_id", "operator": "equals", "value": "00000000-0000-0000-0000-000000000021"}]'::jsonb,
    'pfc',
    'GENERAL_MERCHANDISE',
    NOW(), NOW()
);

-- Migration 000072: array-valued account_id condition
INSERT INTO categorization_rules (id, user_id, name, conditions, target_kind, target_pfc_primary, created_at, updated_at)
VALUES (
    '00000000-0000-0000-0000-000000000042'::uuid,
    '00000000-0000-0000-0000-000000000001'::uuid,
    'Test Rule Array Condition',
    '[{"type": "account_id", "operator": "in", "value": ["00000000-0000-0000-0000-000000000021"]}]'::jsonb,
    'pfc',
    'GENERAL_MERCHANDISE',
    NOW(), NOW()
);

-- Migration 000070: transfer_suggestion boost with raw account IDs
-- Table/column renamed rules→boosts, rule_type→boost_type in migration 000095.
INSERT INTO boosts (id, user_id, name, boost_type, trigger_config, actions, created_at, updated_at)
VALUES (
    '00000000-0000-0000-0000-000000000051'::uuid,
    '00000000-0000-0000-0000-000000000001'::uuid,
    'Test Transfer Suggestion Rule',
    'transfer_suggestion',
    '{"type": "event", "event": "transaction_created", "filters": {"account_id": "00000000-0000-0000-0000-000000000021"}}'::jsonb,
    '{"transfer": {"from_account_id": "00000000-0000-0000-0000-000000000021", "to_account_id": "00000000-0000-0000-0000-000000000022", "amount_type": "fixed", "amount": 100}}'::jsonb,
    NOW(), NOW()
);
EOF
    then
        fail_test "Failed to seed test data: check stderr above for the specific SQL error"
        return 1
    fi

    log_success "Test data seeded successfully"

    # Run backfill migration SQL directly against seeded data (#1109 W3-G3).
    # Migrations 000070 and 000072 ran during initial apply_migrations when the DB was empty
    # (no-op). golang-migrate marks them applied so the second apply_migrations won't re-run
    # them. We re-execute their SQL directly here so the JSONB assertions below have live data
    # to verify. Both migrations are idempotent (they check for missing keys before updating).
    #
    # This hand-written re-execution is the ORIGINAL instance of the same technique that
    # replay_migrations_up_sql_range (above, #1569 round 12 task 1) generalises into a loop over
    # migrations 000149+: run a migration's real .up.sql (or, when that is not possible, an
    # equivalent query) directly via psql because golang-migrate itself will never re-run a version
    # it has already recorded as applied. 000070/000072 are handled by hand here, outside that
    # loop, precisely because they demonstrate why the loop cannot start at 000001: see the NOTE
    # immediately below.
    #
    # NOTE (#1283): Migration 000070 originally targeted the `rules` table, which was renamed
    # to `boosts` (column rule_type → boost_type) by migration 000095. We cannot replay the
    # historical file against the renamed schema; instead we run an equivalent inline query.
    log_info "Applying backfill migrations against seeded data (000070-equivalent, 000072)..."

    # 000070-equivalent: backfill user_account_id keys into boosts JSONB columns.
    # Mirrors the original three UPDATE steps from 000070_backfill_user_account_id_in_rules.up.sql
    # but targets the renamed table (boosts) and column (boost_type). Idempotent.
    #
    # `\set ON_ERROR_STOP on` + `if !`: without ON_ERROR_STOP, psql's own exit code does NOT
    # reflect a failed SQL statement inside a script/heredoc (verified empirically: psql exits 0
    # here even when every statement inside errors). Without this guard a failed backfill UPDATE
    # was invisible at this step — it would usually still surface later via the JSONB assertions
    # below, but only by accident, not because this step was actually checked.
    if ! psql "$DATABASE_URL" <<'BACKFILL_SQL'
\set ON_ERROR_STOP on
UPDATE boosts r
SET actions = jsonb_set(
    r.actions,
    '{transfer,from_user_account_id}',
    to_jsonb(a.user_account_id::text)
)
FROM accounts a
WHERE r.boost_type = 'transfer_suggestion'
  AND r.actions->'transfer' ? 'from_account_id'
  AND NOT (r.actions->'transfer' ? 'from_user_account_id')
  AND a.id::text = r.actions->'transfer'->>'from_account_id'
  AND a.user_account_id IS NOT NULL;

UPDATE boosts r
SET actions = jsonb_set(
    r.actions,
    '{transfer,to_user_account_id}',
    to_jsonb(a.user_account_id::text)
)
FROM accounts a
WHERE r.boost_type = 'transfer_suggestion'
  AND r.actions->'transfer' ? 'to_account_id'
  AND NOT (r.actions->'transfer' ? 'to_user_account_id')
  AND a.id::text = r.actions->'transfer'->>'to_account_id'
  AND a.user_account_id IS NOT NULL;

UPDATE boosts r
SET trigger_config = jsonb_set(
    r.trigger_config,
    '{filters}',
    r.trigger_config->'filters' || jsonb_build_object('user_account_id', a.user_account_id::text)
)
FROM accounts a
WHERE r.trigger_config->>'type' = 'event'
  AND r.trigger_config->'filters' IS NOT NULL
  AND r.trigger_config->'filters' ? 'account_id'
  AND NOT (r.trigger_config->'filters' ? 'user_account_id')
  AND a.id::text = r.trigger_config->'filters'->>'account_id'
  AND a.user_account_id IS NOT NULL;
BACKFILL_SQL
    then
        fail_test "Failed to apply 000070-equivalent backfill: check stderr above for the specific SQL error"
        return 1
    fi

    # Same ON_ERROR_STOP gap as above applies to -f script invocations; -v ON_ERROR_STOP=1 is the
    # command-line equivalent (the migration file itself is a versioned artifact, not edited here).
    if ! psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f "$ABS_MIGRATIONS_DIR/000072_backfill_user_account_id_in_categorization_rule_conditions.up.sql"; then
        fail_test "Failed to apply migration 000072 backfill against seeded data: check stderr above for the specific SQL error"
        return 1
    fi
    log_success "Backfill migrations applied against seeded data"

    # Record row counts before re-running migrations
    log_info "Recording row counts..."
    local users_count_before=$(run_psql "SELECT COUNT(*) FROM users")
    local privacy_count_before=$(run_psql "SELECT COUNT(*) FROM privacy_settings")
    local linked_items_count_before=$(run_psql "SELECT COUNT(*) FROM linked_items")
    local accounts_count_before=$(run_psql "SELECT COUNT(*) FROM accounts")
    local transactions_count_before=$(run_psql "SELECT COUNT(*) FROM transactions")

    log_info "Row counts before re-applying migrations:"
    log_info "  users: $users_count_before"
    log_info "  privacy_settings: $privacy_count_before"
    log_info "  linked_items: $linked_items_count_before"
    log_info "  accounts: $accounts_count_before"
    log_info "  transactions: $transactions_count_before"

    # Re-apply migrations (idempotency test with data present)
    #
    # `apply_migrations` is a golang-migrate no-op here too, for the identical reason as
    # test_idempotency's second application — schema_migrations is already at the latest version,
    # so `migrate up` runs zero migration SQL. Kept as a cheap sanity check only. The real
    # data-integrity check is replay_migrations_up_sql_range immediately below, which re-executes
    # each in-range migration's actual .up.sql against THIS populated database, for real.
    echo ""
    log_info "Re-applying migrations with data present (golang-migrate no-op sanity check)..."
    # apply_migrations is called directly (no subshell): a `migrate up` failure calls its own
    # `exit 1` in THIS shell and the whole script aborts right there with its own diagnostic — the
    # `if [ $? -ne 0 ]` that used to follow this line could never run, so it has been removed.
    # Only stdout is discarded (`2>&1` previously sent the diagnostic to /dev/null too).
    apply_migrations > /dev/null
    log_success "Migrations re-applied (no-op) successfully"

    log_info "Re-executing in-range migrations' .up.sql directly against the populated database (the real data-integrity idempotency check)..."
    if ! replay_migrations_up_sql_range; then
        fail_test "DATA INTEGRITY TEST FAILED - a migration in the replay range is not safe to re-execute against populated data"
        return 1
    fi

    # Check row counts after
    log_info "Verifying data integrity..."
    local users_count_after=$(run_psql "SELECT COUNT(*) FROM users")
    local privacy_count_after=$(run_psql "SELECT COUNT(*) FROM privacy_settings")
    local linked_items_count_after=$(run_psql "SELECT COUNT(*) FROM linked_items")
    local accounts_count_after=$(run_psql "SELECT COUNT(*) FROM accounts")
    local transactions_count_after=$(run_psql "SELECT COUNT(*) FROM transactions")

    log_info "Row counts after re-applying migrations:"
    log_info "  users: $users_count_after"
    log_info "  privacy_settings: $privacy_count_after"
    log_info "  linked_items: $linked_items_count_after"
    log_info "  accounts: $accounts_count_after"
    log_info "  transactions: $transactions_count_after"

    # Compare counts
    echo ""
    local data_preserved=true

    # ⚠ These five tables are the ORIGINAL seed-data set (predating #1569's replay mechanism), not
    # a table list chosen to cover this branch's own migrations. None of this branch's migrations
    # INSERT into any of these five. That gap is no longer carried: the whole-schema
    # replay-stability check immediately after this loop covers EVERY table in the public schema.
    # This named list is kept because it asserts something the whole-schema check deliberately does
    # not — that the seeded rows survive the FIRST replay, i.e. that an in-range migration does not
    # destroy pre-existing data on its first real execution against a populated database.
    # Check each table
    local tables_before=("users" "privacy_settings" "linked_items" "accounts" "transactions")
    local counts_before=("$users_count_before" "$privacy_count_before" "$linked_items_count_before" "$accounts_count_before" "$transactions_count_before")
    local counts_after=("$users_count_after" "$privacy_count_after" "$linked_items_count_after" "$accounts_count_after" "$transactions_count_after")

    for i in "${!tables_before[@]}"; do
        if [ "${counts_before[$i]}" != "${counts_after[$i]}" ]; then
            fail_test "Data loss detected in ${tables_before[$i]} table (${counts_before[$i]} → ${counts_after[$i]})"
            data_preserved=false
        fi
    done

    # -------------------------------------------------------------------------------------------
    # WHOLE-SCHEMA REPLAY-STABILITY CHECK (#1569 round 14 task 8 item 3)
    #
    # THE GAP THIS CLOSES. Everything above this point is blind to a data-level defect in one of
    # THIS branch's own migrations: the schema hash is `pg_dump --schema-only` (no data at all), and
    # the row-count comparison covers five tables none of the in-range migrations write to. A
    # migration in the replay range that duplicated a row, or wrote a different value on a second
    # execution, ran to completion without error and passed both.
    #
    # ⚠ WHY A **SECOND** REPLAY, AND WHY THE OBVIOUS EXTENSION WOULD HAVE BEEN A FALSE RED. The
    # naive fix — add this branch's tables (account_status_history, user_accounts,
    # household_account_shares, ...) to the five-table list above — is WRONG, and measurably so.
    # Look at the order of this function: clean_database, apply_migrations against an EMPTY
    # database (so every backfill in the range is a no-op), THEN seed, THEN count, THEN replay. The
    # replay above is therefore the FIRST time those migrations execute against data, and a
    # backfill legitimately inserting rows there would fail a before/after comparison for doing
    # exactly its job. Idempotency is a property of the SECOND execution, so that is what this
    # compares: counts after replay #1 vs counts after replay #2, over every table in the schema.
    #
    # WHAT A FAILURE HERE MEANS: some in-range migration is not row-count idempotent — an INSERT
    # without ON CONFLICT / NOT EXISTS, or a trigger firing on a re-run UPDATE. It does NOT prove
    # the converse: an UPDATE that rewrites the same rows with a DIFFERENT value on each execution
    # changes no counts and still passes. Row CONTENT diffs remain out of scope; this is the
    # count-level half, and it is named as such so it is never over-read the way the original
    # no-op gate was.
    echo ""
    log_info "Capturing whole-schema row counts, then replaying the in-range migrations a SECOND time..."
    local counts_after_first_replay counts_after_second_replay
    counts_after_first_replay=$(capture_all_table_counts)
    if [ -z "$counts_after_first_replay" ]; then
        fail_test "capture_all_table_counts returned nothing — the whole-schema replay-stability check cannot run"
        data_preserved=false
    else
        if ! replay_migrations_up_sql_range; then
            fail_test "DATA INTEGRITY TEST FAILED - an in-range migration errored on its THIRD execution (second replay)"
            data_preserved=false
        else
            counts_after_second_replay=$(capture_all_table_counts)
            if [ "$counts_after_first_replay" != "$counts_after_second_replay" ]; then
                fail_test "Replay-stability violation: re-executing the in-range migrations changed row counts. Diff (< after 1st replay, > after 2nd):"
                diff <(printf '%s\n' "$counts_after_first_replay") \
                     <(printf '%s\n' "$counts_after_second_replay") >&2 || true
                data_preserved=false
            else
                log_success "Whole-schema row counts are stable across a second replay ($(printf '%s\n' "$counts_after_first_replay" | wc -l | tr -d ' ') tables compared)"
            fi
        fi
    fi

    # Verify migrations 000070 and 000072 backfilled user_account_id into JSONB columns (#1109)
    echo ""
    log_info "Verifying JSONB backfill in migrations 000070/000072..."
    local ua_id_1="00000000-0000-0000-0000-000000000031"
    local ua_id_2="00000000-0000-0000-0000-000000000032"

    # Migration 000072: categorization_rules conditions (string & array)
    local cat_rule_string=$(run_psql "SELECT conditions->0->>'value' FROM categorization_rules WHERE id = '00000000-0000-0000-0000-000000000041'")
    if [ "$cat_rule_string" != "$ua_id_1" ]; then
        fail_test "Migration 000072 Step 1 failed: string condition value should be '$ua_id_1', got '$cat_rule_string'"
        data_preserved=false
    else
        log_success "Migration 000072 Step 1: string condition backfilled"
    fi

    local cat_rule_array=$(run_psql "SELECT conditions->0->'value'->>0 FROM categorization_rules WHERE id = '00000000-0000-0000-0000-000000000042'")
    if [ "$cat_rule_array" != "$ua_id_1" ]; then
        fail_test "Migration 000072 Step 2 failed: array condition should be '$ua_id_1', got '$cat_rule_array'"
        data_preserved=false
    else
        log_success "Migration 000072 Step 2: array condition backfilled"
    fi

    # Migration 000070: boosts actions and trigger_config
    # (#1283: table renamed rules→boosts, column rule_type→boost_type by migration 000095)
    local transfer_from=$(run_psql "SELECT actions->'transfer'->>'from_user_account_id' FROM boosts WHERE id = '00000000-0000-0000-0000-000000000051'")
    if [ "$transfer_from" != "$ua_id_1" ]; then
        fail_test "Migration 000070 Step 1 failed: from_user_account_id should be '$ua_id_1', got '$transfer_from'"
        data_preserved=false
    else
        log_success "Migration 000070 Step 1: from_user_account_id backfilled"
    fi

    local transfer_to=$(run_psql "SELECT actions->'transfer'->>'to_user_account_id' FROM boosts WHERE id = '00000000-0000-0000-0000-000000000051'")
    if [ "$transfer_to" != "$ua_id_2" ]; then
        fail_test "Migration 000070 Step 2 failed: to_user_account_id should be '$ua_id_2', got '$transfer_to'"
        data_preserved=false
    else
        log_success "Migration 000070 Step 2: to_user_account_id backfilled"
    fi

    local trigger_ua=$(run_psql "SELECT trigger_config->'filters'->>'user_account_id' FROM boosts WHERE id = '00000000-0000-0000-0000-000000000051'")
    if [ "$trigger_ua" != "$ua_id_1" ]; then
        fail_test "Migration 000070 Step 3 failed: trigger_config user_account_id should be '$ua_id_1', got '$trigger_ua'"
        data_preserved=false
    else
        log_success "Migration 000070 Step 3: trigger_config user_account_id backfilled"
    fi

    if [ "$data_preserved" = true ]; then
        pass_test "DATA INTEGRITY TEST PASSED - All data preserved and JSONB backfills verified"
        return 0
    else
        fail_test "DATA INTEGRITY TEST FAILED - Data was lost, corrupted, or JSONB backfill assertions failed"
        return 1
    fi
}

# =============================================================================
# Test 4: Schema Constraint Validation
# =============================================================================

test_schema_constraints() {
    log_section "TEST 4: SCHEMA CONSTRAINT VALIDATION"

    log_info "Validating database schema constraints, indexes, and foreign keys..."
    echo ""

    # Ensure migrations are applied
    clean_database
    log_info "Applying migrations for schema validation..."
    # apply_migrations is called directly (no subshell): a `migrate up` failure calls its own
    # `exit 1` in THIS shell and the whole script aborts right there with its own diagnostic — the
    # `if [ $? -ne 0 ]` that used to follow this line could never run, so it has been removed.
    # Only stdout is discarded (`2>&1` previously sent the diagnostic to /dev/null too).
    apply_migrations > /dev/null
    log_success "Migrations applied successfully"
    echo ""

    # Expected tables
    local expected_tables=(
        "users"
        "privacy_settings"
        "audit_logs"
        "user_subscription_overrides"
        "linked_items"
        "accounts"
        "user_identities"
        "transactions"
        "webhook_events"
        "notifications"
    )

    log_info "Checking for expected tables..."
    local tables_ok=true
    for table in "${expected_tables[@]}"; do
        local exists=$(run_psql "SELECT COUNT(*) FROM information_schema.tables WHERE table_name='$table'")
        if [ "$exists" = "1" ]; then
            log_success "Table '$table' exists"
        else
            fail_test "Table '$table' is missing!"
            tables_ok=false
        fi
    done

    if [ "$tables_ok" = false ]; then
        fail_test "SCHEMA VALIDATION FAILED - Missing expected tables"
        return 1
    fi

    # Check critical foreign keys
    echo ""
    log_info "Checking critical foreign key constraints..."

    local fk_count=$(run_psql "SELECT COUNT(*) FROM information_schema.table_constraints WHERE constraint_type='FOREIGN KEY'")
    log_info "Total foreign keys found: $fk_count"

    if [ -z "$fk_count" ] || [ "$fk_count" -lt 5 ]; then
        log_warning "Expected more foreign key constraints (found: $fk_count)"
    else
        log_success "Foreign key constraints present ($fk_count found)"
    fi

    # Check critical indexes
    echo ""
    log_info "Checking for critical indexes..."

    local index_count=$(run_psql "SELECT COUNT(*) FROM pg_indexes WHERE schemaname='public'")
    log_info "Total indexes found: $index_count"

    if [ -z "$index_count" ] || [ "$index_count" -lt 10 ]; then
        log_warning "Expected more indexes (found: $index_count)"
    else
        log_success "Indexes present ($index_count found)"
    fi

    # Check unique constraints
    echo ""
    log_info "Checking unique constraints..."

    local unique_count=$(run_psql "SELECT COUNT(*) FROM information_schema.table_constraints WHERE constraint_type='UNIQUE'")
    log_info "Total unique constraints found: $unique_count"

    if [ -z "$unique_count" ] || [ "$unique_count" -lt 3 ]; then
        log_warning "Expected more unique constraints (found: $unique_count)"
    else
        log_success "Unique constraints present ($unique_count found)"
    fi

    echo ""
    pass_test "SCHEMA CONSTRAINT VALIDATION PASSED"
    return 0
}

# =============================================================================
# Test 5: Rollback Testing
# =============================================================================
# Purpose: Check that a full up→down→up round-trip is schema-neutral (the public schema hash
#          before rollback matches the hash after re-applying), and that rollback leaves no
#          residue beyond a fixed, named allowlist.
#
# What this does NOT prove, stated explicitly because it is easy to over-read the PASSED message:
#   - It does NOT prove each individual .down.sql correctly reverses its own .up.sql. A LATER
#     migration's .down.sql can remove residue an EARLIER one left behind (or vice versa mask it),
#     and a full down→up→up cycle would still land on the same final schema. Proving per-pair
#     reversal would mean comparing schema(N-1) against schema(apply N, then down N) for every N —
#     not implemented here (130 migrations × a full cycle each is a lot of runtime for CI to pay on
#     every PR); this is a real gap, not a hedge.
#   - It does NOT prove DATA survives a rollback. Every check in this test is schema-only
#     (pg_dump --schema-only / object-identity inventories); no row data is compared before and
#     after. A .down.sql that DROPs a table and its .up.sql recreates it empty would pass this test
#     while losing every row.

test_rollback() {
    log_section "TEST 5: ROLLBACK TESTING"

    log_info "Testing migration rollback capability (up → down → up cycle)..."
    log_info "This validates Issue #348: Add .up/.down Migration Support"
    echo ""

    # Clean start
    clean_database

    # Capture the full-schema object inventory BEFORE the first `up` in this run — i.e. whatever
    # was already in the database, if anything, right after clean_database. Used by
    # assert_clean_rollback_state below to tell "residue THIS run's up/down cycle left behind" apart
    # from "state that was already there before we touched anything" (see that function's comment
    # for why the distinction matters).
    log_info "Capturing pre-migration object inventory (rollback baseline)..."
    local pre_up_inventory
    if ! pre_up_inventory=$(get_full_object_inventory); then
        fail_test "Failed to capture pre-migration object inventory for rollback test"
        return 1
    fi

    # Step 1: Apply all migrations (forward)
    log_info "Step 1: Applying all migrations (forward)..."
    # `local apply_duration=$(apply_migrations)` would mask apply_migrations' real exit status
    # behind `local`'s own (always-0) status — see test_idempotency's duration1 comment for the
    # empirical confirmation. Declaring `local` separately, then checking the plain assignment in
    # `if !`, makes this live.
    local apply_duration
    if ! apply_duration=$(apply_migrations); then
        fail_test "Migration application failed during rollback test (forward step)"
        return 1
    fi
    log_success "Migrations applied in ${apply_duration}s"

    # Capture baseline schema hash
    log_info "Capturing baseline schema hash..."
    local hash_baseline
    if ! hash_baseline=$(get_schema_hash); then
        fail_test "Failed to capture baseline schema hash for rollback test"
        return 1
    fi
    log_info "Baseline schema hash: $hash_baseline"
    echo ""

    # Step 2: Rollback all migrations (reverse)
    log_info "Step 2: Rolling back all migrations (reverse)..."
    local rollback_result=$(rollback_migrations)
    local rollback_duration="${rollback_result%%:*}"
    local rollback_error_type="${rollback_result##*:}"

    # Check for rollback errors
    if [ "$rollback_error_type" = "other" ]; then
        fail_test "Rollback failed with unexpected error"
        return 1
    fi

    log_success "Migrations rolled back in ${rollback_duration}s"

    # Verify clean state after rollback: every object THIS cycle left behind that wasn't already
    # there before Step 1 must be on the fixed allowlist above (see assert_clean_rollback_state for
    # the two-check design and why it isn't a single "diff == allowlist" set-equality).
    log_info "Verifying clean state after rollback (full object inventory, not just tables)..."
    local post_rollback_inventory
    if ! post_rollback_inventory=$(get_full_object_inventory); then
        fail_test "Failed to capture post-rollback object inventory for rollback test"
        return 1
    fi

    if ! assert_clean_rollback_state "$pre_up_inventory" "$post_rollback_inventory"; then
        fail_test "Rollback left residue outside the allowlist — see errors above"
        return 1
    fi

    log_success "Clean state verified (no unallowlisted residue across the full object inventory)"
    echo ""

    # Step 3: Re-apply all migrations (forward again)
    log_info "Step 3: Re-applying all migrations (forward)..."
    local reapply_duration
    if ! reapply_duration=$(apply_migrations); then
        fail_test "Migration re-application failed during rollback test (reverse step)"
        return 1
    fi
    log_success "Migrations re-applied in ${reapply_duration}s"

    # Capture final schema hash
    log_info "Capturing final schema hash..."
    local hash_reapplied
    if ! hash_reapplied=$(get_schema_hash); then
        fail_test "Failed to capture final schema hash for rollback test"
        return 1
    fi
    log_info "Final schema hash: $hash_reapplied"
    echo ""

    # Step 4: Compare schemas
    log_info "Step 4: Comparing baseline vs final schema..."

    if [ "$hash_baseline" = "$hash_reapplied" ]; then
        echo ""
        pass_test "ROLLBACK TEST PASSED - Public schema hash identical after up→down→up cycle"
        log_success "✓ A full down→up round-trip is schema-neutral for the public schema"
        log_success "✓ No unallowlisted residue found across the full (non-public-scoped) object inventory"
        log_warning "This does NOT prove each .down.sql individually reverses its own .up.sql (a later"
        log_warning ".down.sql could mask an earlier one's incomplete reversal), and does NOT prove"
        log_warning "data survives a rollback (every check here is schema-only, no row data compared)."
        return 0
    else
        echo ""
        fail_test "ROLLBACK TEST FAILED - Schema mismatch after up→down→up cycle"
        log_error "Baseline hash: $hash_baseline"
        log_error "Final hash:    $hash_reapplied"
        log_error "This indicates incomplete or incorrect rollback logic in .down.sql files"
        return 1
    fi
}

# =============================================================================
# Main Execution
# =============================================================================

main() {
    parse_args "$@"

    log_section "MIGRATION TESTING FRAMEWORK"
    log_info "Issue #326: Comprehensive Migration Testing (golang-migrate + Squawk)"
    log_info "Database: $DATABASE_URL"
    # Log the RESOLVED absolute path (what every migrate/psql -f call site below actually uses),
    # not the raw possibly-relative MIGRATIONS_DIR config value — see ABS_MIGRATIONS_DIR's
    # definition above for why: logging the raw value while migrating a different resolved path
    # was exactly the previous bug (Task S3-6).
    log_info "Migrations: $ABS_MIGRATIONS_DIR (from MIGRATIONS_DIR=$MIGRATIONS_DIR)"
    echo ""

    # FIRST, before the prerequisite checks and before any connection is opened (#1589). The order
    # matters twice over: a developer who pointed at the wrong database gets a refusal instead of a
    # connection, and the guard's own test suite can point at an unreachable host and still tell a
    # refusal (exit 3) apart from a failure to connect (exit 1).
    assert_host_is_local
    assert_target_is_disposable

    # Check prerequisites
    if [ ! -d "$ABS_MIGRATIONS_DIR" ]; then
        log_error "Migrations directory not found: $ABS_MIGRATIONS_DIR"
        exit 1
    fi

    if ! command -v migrate &> /dev/null; then
        log_error "golang-migrate CLI not found - please install: https://github.com/golang-migrate/migrate"
        exit 1
    fi

    if ! command -v psql &> /dev/null; then
        log_error "PostgreSQL client (psql) not found"
        exit 1
    fi

    # Test database connection
    if ! psql "$DATABASE_URL" -c "SELECT 1" > /dev/null 2>&1; then
        log_error "Cannot connect to database: $DATABASE_URL"
        exit 1
    fi

    # The connection is open; now make the database prove it is a disposable target. Deliberately
    # BEFORE the pg_dump preflight below — a wrong target should be refused at the first moment the
    # question can be answered, not after two more prerequisite checks.
    assert_target_cluster_is_disposable

    # pg_dump must be at least the server's major version — it REFUSES to dump a newer server.
    # Checked here, up front, because the failure is otherwise invisible: pg_dump writes its error to
    # stderr (discarded in get_schema_hash) and produces no stdout, so the idempotency and rollback
    # suites would hash an empty string TWICE and report "schema unchanged" / "schema identical"
    # having compared nothing. That is exactly what happened, undetected, both locally (client 14
    # vs server 15) and in CI (client 15 vs the postgres:17 container the job starts) until the
    # empty-dump guard in get_schema_hash caught it.
    if ! command -v "$PG_DUMP" &> /dev/null && [ ! -x "$PG_DUMP" ]; then
        log_error "pg_dump not found: $PG_DUMP"
        exit 1
    fi
    local client_major
    client_major=$("$PG_DUMP" --version | grep -oE '[0-9]+' | head -1)
    local server_major
    server_major=$(psql "$DATABASE_URL" -tAc 'SHOW server_version' 2>/dev/null | grep -oE '^[0-9]+')
    if [ -n "$client_major" ] && [ -n "$server_major" ] && [ "$client_major" -lt "$server_major" ]; then
        log_error "pg_dump ${client_major} is older than the server (${server_major}); pg_dump refuses this and the schema-identity suites would silently verify nothing."
        log_error "Fix either way:"
        log_error "  - install a client >= ${server_major} (e.g. brew install postgresql@${server_major}), or"
        log_error "  - set PG_DUMP to the server container's own binary:"
        log_error "      PG_DUMP=\"\$PWD/scripts/pg_dump-via-docker\" $0"
        exit 1
    fi
    log_info "pg_dump ${client_major:-?} vs server ${server_major:-?} — compatible"

    log_success "Prerequisites checked"

    # Structural precondition for the genuine-second-execution replay used by test_idempotency and
    # test_data_integrity (#1569 round 12 task 1): re-verify, on every run, that the assumption
    # backing REPLAY_RANGE_START_MIGRATION still holds. See replay_migrations_up_sql_range's header
    # comment for the full rationale. This is checked here, before any test runs, rather than
    # inside the tests themselves, because a violation here means the entire mechanism those two
    # tests rely on is unsound for this run — not a single test's finding.
    if ! assert_replay_range_has_no_destructive_ddl; then
        log_error "Aborting before running any test: the replay-range mechanism cannot be trusted this run."
        exit 1
    fi

    # Run all test suites. Each is invoked as `suite || true` DELIBERATELY (#1569 round 12 fix
    # round 1 — landing blocker). `set -euo pipefail` is active at the top of this script, and a
    # bare sequential call (`test_idempotency; test_performance; ...`) means the FIRST suite whose
    # own `return 1` (via fail_test) reaches `main()` aborts the WHOLE script right there — every
    # suite after it silently never runs, AND the TEST SUMMARY below never prints. Confirmed live:
    # once test_data_integrity's replay became a genuine check (this round) and it found a real
    # defect, test_schema_constraints and test_rollback never ran and there was no summary — this
    # round's own founding lesson ("a gate that does not run proves nothing") recurring inside
    # main() itself, not just inside the tests it calls. `|| true` neutralises set -e for exactly
    # an ORDINARY nonzero `return` from a test function (every `return 1` path in every test_*
    # function above is preceded by its own `fail_test` call — confirmed by inspection — so
    # TESTS_FAILED is already incremented correctly and nothing here needs to duplicate that
    # bookkeeping). A test function that calls `exit` directly (the pre-flight/connectivity guards,
    # and apply_migrations' own fail-closed exit on a real `migrate up` failure) still terminates
    # the whole script immediately regardless of `|| true` — that is intentional and unchanged:
    # those are genuinely fatal environment failures, not test assertions, and should still abort
    # everything rather than produce a summary that pretends the run was meaningful.
    test_idempotency || true
    test_performance || true
    test_data_integrity || true
    test_schema_constraints || true
    test_rollback || true

    # Summary
    log_section "TEST SUMMARY"
    echo ""
    echo -e "${GREEN}✅ Tests Passed: $TESTS_PASSED${NC}"
    if [ $TESTS_FAILED -gt 0 ]; then
        echo -e "${RED}❌ Tests Failed: $TESTS_FAILED${NC}"
    fi
    if [ $WARNINGS -gt 0 ]; then
        echo -e "${YELLOW}⚠️  Warnings: $WARNINGS${NC}"
    fi
    echo ""

    if [ $TESTS_FAILED -gt 0 ]; then
        log_error "MIGRATION TESTS FAILED"
        log_error "Please fix failing migrations before merging"
        echo ""
        echo "Common fixes:"
        echo "  - Add IF NOT EXISTS guards to CREATE TABLE/INDEX statements"
        echo "  - Use DO \$\$ blocks for ALTER TABLE ADD COLUMN"
        echo "  - See migrations 000013, 000015 for good idempotency patterns"
        echo ""
        exit 1
    else
        log_success "ALL MIGRATION TESTS PASSED"
        # NOT "safe to deploy" / "safe for production" unconditionally: this suite checks
        # idempotency, timing, row-count + JSONB-backfill preservation for ONE seeded fixture set,
        # expected-table/index/FK/constraint presence, and rollback schema-neutrality (see
        # test_rollback's own comment for what that test explicitly does not prove — per-pair
        # .down.sql correctness, or that a rollback preserves REAL production data).
        # Idempotency-via-genuine-re-execution is itself bounded: migrations
        # $REPLAY_RANGE_START_MIGRATION+ were actually re-run a second time against a live schema
        # (empty for test_idempotency, populated for test_data_integrity); migrations before that
        # were only ever re-run via golang-migrate's own no-op against an empty database, which
        # proves nothing about their idempotency against a populated one (see
        # replay_migrations_up_sql_range's header comment for why the range is bounded there).
        log_success "All automated migration checks passed for this run"
        echo ""
        exit 0
    fi
}

# Only run main when executed directly (`./test-migrations.sh ...` or `bash test-migrations.sh`),
# not when sourced — mirrors the same BASH_SOURCE guard in scripts/pg_dump-via-docker, and for the
# same reason: this lets scripts/__tests__/rollback-residue.test.sh source this file to call
# extract_object_identities/assert_clean_rollback_state/is_allowlisted_residue directly, with
# synthetic inventory strings, without touching a real database or running the full suite.
if [ "${BASH_SOURCE[0]}" = "${0}" ]; then
    main "$@"
fi
