#!/usr/bin/env bash
# Regression test for replay_file_runs_outside_a_transaction / replay_migrations_up_sql_range's
# transaction flag in scripts/test-migrations.sh.
#
# Run: bash scripts/__tests__/replay-concurrently-transaction.test.sh
# Exit 0 on all assertions passing, non-zero on any failure.
#
# WHAT THIS EXISTS TO CATCH, and it is a defect that actually shipped (#1569 round 15 task 9, found
# by review, then reproduced). The replay loop re-executes every in-range .up.sql with `psql -1`,
# which is right for a multi-statement file — it reproduces the implicit transaction golang-migrate's
# simple query gets — and WRONG for a single-statement `CREATE INDEX CONCURRENTLY` file, which
# golang-migrate runs with no transaction at all and which Postgres refuses inside one:
#
#     ERROR:  CREATE INDEX CONCURRENTLY cannot run inside a transaction block
#
# No CONCURRENTLY migration had ever been inside the replay range (it starts at 000149; the four
# older ones are 000077/000082/000138/000144), so each layer was correct and the COMPOSITION had
# never been executed. This suite executes it.
#
# Two halves:
#   1. the pure classifier, against synthetic fixtures AND the real migration files — no database;
#   2. the composition itself, against real PostgreSQL — psql -1 must FAIL on a CONCURRENTLY file
#      and psql without -1 must SUCCEED. Skipped loudly if DATABASE_URL is unset (CI's Migration
#      Tests job always has one).
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
SCRIPT="$SCRIPT_DIR/test-migrations.sh"

PASS=0
FAIL=0
pass() { echo "  ✅ PASS: $1"; PASS=$((PASS + 1)); }
fail() { echo "  ❌ FAIL: $1"; FAIL=$((FAIL + 1)); }

echo "=== replay-concurrently-transaction test suite ==="
echo ""

# test-migrations.sh requires DATABASE_URL at source time. The classifier does not use it; a
# placeholder keeps the pure half runnable with no database at all.
export DATABASE_URL="${DATABASE_URL:-postgres://placeholder@127.0.0.1:1/placeholder}"
# shellcheck source=/dev/null
source "$SCRIPT"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "--- 1. the classifier, on synthetic fixtures ---"

cat > "$TMP/concurrent.up.sql" <<'EOF'
-- a real concurrent build
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_probe ON probe (a) WHERE b IS NULL;
EOF
if replay_file_runs_outside_a_transaction "$TMP/concurrent.up.sql"; then
    pass "a CREATE INDEX CONCURRENTLY file is replayed WITHOUT -1"
else
    fail "a CREATE INDEX CONCURRENTLY file must be replayed without -1, or psql refuses it"
fi

cat > "$TMP/comment_only.up.sql" <<'EOF'
-- Plain CREATE INDEX (not CONCURRENTLY) because golang-migrate wraps .up.sql in a transaction
--     CREATE INDEX CONCURRENTLY would be illegal here
CREATE INDEX IF NOT EXISTS idx_probe ON probe (a);
EOF
if replay_file_runs_outside_a_transaction "$TMP/comment_only.up.sql"; then
    fail "a file that only MENTIONS concurrently in comments must still be replayed with -1"
else
    pass "a file that only mentions CONCURRENTLY in a comment keeps -1 (comments are stripped first)"
fi

cat > "$TMP/plain.up.sql" <<'EOF'
ALTER TABLE probe ADD COLUMN IF NOT EXISTS c INT;
EOF
if replay_file_runs_outside_a_transaction "$TMP/plain.up.sql"; then
    fail "an ordinary migration must keep -1"
else
    pass "an ordinary migration keeps -1"
fi

cat > "$TMP/lowercase.up.sql" <<'EOF'
drop index concurrently if exists idx_probe;
EOF
if replay_file_runs_outside_a_transaction "$TMP/lowercase.up.sql"; then
    pass "the match is case-insensitive (drop index concurrently)"
else
    fail "the match must be case-insensitive: Postgres accepts lowercase DDL"
fi

echo ""
echo "--- 2. the classifier, on the REAL migration files (the composition that was missed) ---"

assert_real_file() {
    local file="$1" want="$2" desc="$3"
    local path="$REPO_ROOT/backend/migrations/$file"
    if [ ! -f "$path" ]; then
        fail "$file is missing — this suite's fixtures have drifted from the tree"
        return
    fi
    if replay_file_runs_outside_a_transaction "$path"; then
        [ "$want" = "no-txn" ] && pass "$desc" || fail "$desc (classified as needing NO transaction)"
    else
        [ "$want" = "txn" ] && pass "$desc" || fail "$desc (classified as needing -1)"
    fi
}

# Every CONCURRENTLY file in the tree, so a new one added outside the replay range is still covered
# the day the range moves forward.
for f in "$REPO_ROOT"/backend/migrations/*.up.sql; do
    if /usr/bin/grep -viE '^[[:space:]]*--' "$f" | /usr/bin/grep -qiE '\bCONCURRENTLY\b'; then
        assert_real_file "$(basename "$f")" "no-txn" "$(basename "$f") (real CONCURRENTLY statement) is replayed without -1"
    fi
done
assert_real_file "000143_drop_invalid_deletion_residue_index.up.sql" "txn" \
    "000143 (discusses CONCURRENTLY in prose, DO block in SQL) keeps -1"
assert_real_file "000074_add_transactions_keyset_indexes.up.sql" "txn" \
    "000074 (comment explains why it is NOT concurrent) keeps -1"

echo ""
echo "--- 3. the composition, against real PostgreSQL ---"

CONCURRENT_MIGRATION="$REPO_ROOT/backend/migrations/000182_add_accounts_user_account_backfill_index.up.sql"
if ! command -v psql > /dev/null 2>&1; then
    echo "  ⚠️  psql not installed — skipping the live half (CI's Migration Tests job always has it)."
elif [ "$DATABASE_URL" = "postgres://placeholder@127.0.0.1:1/placeholder" ]; then
    echo "  ⚠️  DATABASE_URL not set — skipping the live half (CI's Migration Tests job always sets it)."
elif ! psql "$DATABASE_URL" -c 'SELECT 1' > /dev/null 2>&1; then
    echo "  ⚠️  DATABASE_URL is set but not reachable — skipping the live half."
elif [ ! -f "$CONCURRENT_MIGRATION" ]; then
    fail "000182 is missing — the live half has nothing to execute"
else
    # The RED this suite is named for: with -1, Postgres refuses the file outright.
    if psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -1 -f "$CONCURRENT_MIGRATION" > /dev/null 2>&1; then
        fail "psql -1 accepted a CONCURRENTLY migration — the premise of this suite no longer holds; re-derive it"
    else
        pass "psql -1 rejects the CONCURRENTLY migration (this is what the replay loop used to do)"
    fi
    if psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f "$CONCURRENT_MIGRATION" > /dev/null 2>&1; then
        pass "psql WITHOUT -1 replays the CONCURRENTLY migration cleanly (what the loop does now)"
    else
        fail "psql without -1 could not replay the CONCURRENTLY migration"
    fi
fi

echo ""
echo "=== $PASS passed, $FAIL failed ==="
[ "$FAIL" -eq 0 ]
