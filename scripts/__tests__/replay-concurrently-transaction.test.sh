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


# --- the lexical cases (#1569 round 16 task 9, D-1). The classifier used to strip only FULL-LINE
# comments, so CONCURRENTLY appearing anywhere else that is not executable SQL — an inline comment,
# a block comment, a string literal — falsely classified the file and stripped `-1` from an
# unrelated MULTI-statement migration's replay, silently removing transaction semantics. The rule
# is ported from blankSQLCommentsAndLiterals (backend/internal/repository/history_delete_sql_guard_test.go).

cat > "$TMP/inline_comment.up.sql" <<'EOF'
CREATE TABLE IF NOT EXISTS probe (a INT); -- deliberately NOT built CONCURRENTLY
INSERT INTO probe (a) VALUES (1);
EOF
if replay_file_runs_outside_a_transaction "$TMP/inline_comment.up.sql"; then
    fail "CONCURRENTLY in a TRAILING (inline) comment must not strip -1"
else
    pass "CONCURRENTLY in a trailing inline comment keeps -1"
fi

cat > "$TMP/block_comment.up.sql" <<'EOF'
/* this index could have been built CONCURRENTLY, but the file has two statements */
CREATE TABLE IF NOT EXISTS probe (a INT);
INSERT INTO probe (a) VALUES (1);
EOF
if replay_file_runs_outside_a_transaction "$TMP/block_comment.up.sql"; then
    fail "CONCURRENTLY inside a /* block comment */ must not strip -1"
else
    pass "CONCURRENTLY inside a block comment keeps -1"
fi

cat > "$TMP/block_comment_midline.up.sql" <<'EOF'
CREATE TABLE IF NOT EXISTS probe (a INT); /* not
   CONCURRENTLY, on purpose */
INSERT INTO probe (a) VALUES (1);
EOF
if replay_file_runs_outside_a_transaction "$TMP/block_comment_midline.up.sql"; then
    fail "CONCURRENTLY inside a MULTI-LINE block comment must not strip -1"
else
    pass "CONCURRENTLY inside a multi-line block comment keeps -1"
fi

cat > "$TMP/string_literal.up.sql" <<'EOF'
INSERT INTO notes (body) VALUES ('rebuilt without CONCURRENTLY');
INSERT INTO notes (body) VALUES ('second statement');
EOF
if replay_file_runs_outside_a_transaction "$TMP/string_literal.up.sql"; then
    fail "CONCURRENTLY inside a single-quoted string literal must not strip -1"
else
    pass "CONCURRENTLY inside a string literal keeps -1"
fi

cat > "$TMP/dollar_body.up.sql" <<'EOF'
DO $$
BEGIN
    RAISE NOTICE 'we did not use CONCURRENTLY here';
END $$;
CREATE TABLE IF NOT EXISTS probe (a INT);
EOF
if replay_file_runs_outside_a_transaction "$TMP/dollar_body.up.sql"; then
    fail "CONCURRENTLY inside a string nested in a DO \$\$ block must not strip -1"
else
    pass "CONCURRENTLY inside a string nested in a DO block keeps -1"
fi

# The other polarity: a comment/string must never HIDE a real CONCURRENTLY statement either. A
# scanner that fails closed the wrong way (blanking too much) would re-introduce the psql -1 error
# this whole mechanism exists to avoid.
cat > "$TMP/comment_then_real.up.sql" <<'EOF'
-- prose mentioning CONCURRENTLY, plus an apostrophe: the migration's own note
/* and a block comment */
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_probe ON probe (a);
EOF
if replay_file_runs_outside_a_transaction "$TMP/comment_then_real.up.sql"; then
    pass "a real CONCURRENTLY statement is still found after comments and an apostrophe"
else
    fail "a real CONCURRENTLY statement must still be found after comments containing an apostrophe"
fi

cat > "$TMP/dollar_shaped_substring.up.sql" <<'EOF'
INSERT INTO notes (body) VALUES ('placeholder $1$ and $word$ as data');
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_probe ON probe (a);
EOF
if replay_file_runs_outside_a_transaction "$TMP/dollar_shaped_substring.up.sql"; then
    pass "a \$..\$-shaped substring inside an ordinary string does not flip quote parity"
else
    fail "a \$..\$-shaped substring inside an ordinary string must not hide the real CONCURRENTLY that follows"
fi

cat > "$TMP/doubled_quote.up.sql" <<'EOF'
INSERT INTO notes (body) VALUES ('the migration''s note about CONCURRENTLY');
INSERT INTO notes (body) VALUES ('second');
EOF
if replay_file_runs_outside_a_transaction "$TMP/doubled_quote.up.sql"; then
    fail "a doubled-quote escape must not end the literal early"
else
    pass "a '' escape inside a literal keeps the whole literal blanked"
fi

cat > "$TMP/comment_marker_in_literal.up.sql" <<'EOF'
INSERT INTO notes (body) VALUES ('a -- marker inside data');
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_probe ON probe (a);
EOF
if replay_file_runs_outside_a_transaction "$TMP/comment_marker_in_literal.up.sql"; then
    pass "a -- inside a string literal is data, not a comment start"
else
    fail "a -- inside a string literal must not blank the rest of the file"
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

# THE WHOLE CORPUS, PARTITIONED EXPLICITLY (#1569 round 16 task 9). This used to derive the
# expected set by re-running the classifier's own (then-buggy) full-line-comment grep, which meant
# the assertion could never disagree with the implementation. The expected set is now written down,
# and every .up.sql that MENTIONS the keyword at all — in code or in prose — must appear in exactly
# one of the two lists below. A new migration that mentions CONCURRENTLY anywhere fails this suite
# until someone classifies it by hand, which is the point.
REAL_CONCURRENTLY_UP_FILES=(
    000077_add_idx_transactions_effective_pfc_detailed.up.sql
    000079_add_custom_category_parent_indexes.up.sql
    000080_add_custom_category_parent_pfc_index.up.sql
    000082_add_transaction_reviewed_index.up.sql
    000138_add_audit_logs_deletion_residue_index.up.sql
    000144_rebuild_deletion_residue_index.up.sql
    000182_add_accounts_user_account_backfill_index.up.sql
)
# Files whose ONLY occurrences of the keyword are prose (comments) or SQL string data. Every one of
# these is a file the old full-line-only strip happened to get right; they stay listed because the
# new scanner must keep getting them right.
PROSE_ONLY_CONCURRENTLY_UP_FILES=(
    000074_add_transactions_keyset_indexes.up.sql
    000078_add_custom_category_parent_id.up.sql
    000081_add_transaction_reviewed.up.sql
    000116_add_work_claims_side_effect_actions.up.sql
    000143_drop_invalid_deletion_residue_index.up.sql
    000163_portfolio_snapshot_repair_ledger.up.sql
    000168_transactions_pending_twin_pointer.up.sql
    000181_drop_invalid_accounts_backfill_index.up.sql
)

for file in "${REAL_CONCURRENTLY_UP_FILES[@]}"; do
    assert_real_file "$file" "no-txn" "$file (real CONCURRENTLY statement) is replayed without -1"
done
for file in "${PROSE_ONLY_CONCURRENTLY_UP_FILES[@]}"; do
    assert_real_file "$file" "txn" "$file (mentions CONCURRENTLY only in prose/data) keeps -1"
done

# Exhaustiveness: no .up.sql mentioning the keyword may be absent from both lists.
UNCLASSIFIED=""
for f in "$REPO_ROOT"/backend/migrations/*.up.sql; do
    /usr/bin/grep -qie 'concurrently' "$f" || continue
    base="$(basename "$f")"
    found=""
    for known in "${REAL_CONCURRENTLY_UP_FILES[@]}" "${PROSE_ONLY_CONCURRENTLY_UP_FILES[@]}"; do
        [ "$known" = "$base" ] && found="yes" && break
    done
    [ -z "$found" ] && UNCLASSIFIED="$UNCLASSIFIED $base"
done
if [ -n "$UNCLASSIFIED" ]; then
    fail "these .up.sql files mention CONCURRENTLY but are in neither list — classify them by hand:$UNCLASSIFIED"
else
    pass "every .up.sql mentioning CONCURRENTLY is explicitly classified"
fi

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
