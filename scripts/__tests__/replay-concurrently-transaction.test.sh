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

# --- #1569 round 17 task 10 (B-1): the classifier's OWN dollar-quote semantics, plus three more
# lexical classes. The rule here was previously described as a port of blankSQLCommentsAndLiterals
# (backend/internal/repository/history_delete_sql_guard_test.go). That was wrong in ONE respect and
# the tests below are what pins the difference: that Go lexer deliberately lexes INSIDE a
# dollar-quoted region so the G4 guards can still see `DO $$ ... DELETE ... $$`. For THIS consumer a
# dollar-quoted region is opaque DATA — nothing inside it is executable SQL that psql's transaction
# flag could ever apply to. One shared idea, two consumers, opposite semantics; do NOT re-port.

cat > "$TMP/dollar_quoted_prose.up.sql" <<'EOF'
COMMENT ON TABLE probe IS $$this index was NOT built CONCURRENTLY$$;
CREATE TABLE IF NOT EXISTS probe (a INT);
EOF
if replay_file_runs_outside_a_transaction "$TMP/dollar_quoted_prose.up.sql"; then
    fail "CONCURRENTLY inside a \$\$dollar-quoted\$\$ literal must not strip -1"
else
    pass "CONCURRENTLY inside a \$\$dollar-quoted\$\$ literal keeps -1"
fi

cat > "$TMP/dollar_quoted_named_tag.up.sql" <<'EOF'
COMMENT ON TABLE probe IS $note$rebuilt, though not CONCURRENTLY$note$;
CREATE TABLE IF NOT EXISTS probe (a INT);
EOF
if replay_file_runs_outside_a_transaction "$TMP/dollar_quoted_named_tag.up.sql"; then
    fail "CONCURRENTLY inside a \$tag\$-delimited literal must not strip -1"
else
    pass "CONCURRENTLY inside a \$tag\$-delimited literal keeps -1"
fi

cat > "$TMP/dollar_body_procedural.up.sql" <<'EOF'
DO $$
BEGIN
    EXECUTE 'CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_probe ON probe (a)';
END $$;
CREATE TABLE IF NOT EXISTS probe (a INT);
EOF
if replay_file_runs_outside_a_transaction "$TMP/dollar_body_procedural.up.sql"; then
    fail "CONCURRENTLY inside a DO block body must not strip -1 (Postgres refuses it there anyway)"
else
    pass "CONCURRENTLY inside a DO block body keeps -1"
fi

cat > "$TMP/unterminated_dollar.up.sql" <<'EOF'
CREATE TABLE IF NOT EXISTS probe (a INT);
COMMENT ON TABLE probe IS $$ an unterminated region mentioning CONCURRENTLY
EOF
if replay_file_runs_outside_a_transaction "$TMP/unterminated_dollar.up.sql"; then
    fail "an UNTERMINATED dollar region must fail toward keeping -1, not toward dropping it"
else
    pass "an unterminated dollar region fails toward keeping -1 (the safe direction)"
fi

cat > "$TMP/nested_block_comment.up.sql" <<'EOF'
/* outer /* inner */ and the outer comment continues: not CONCURRENTLY */
CREATE TABLE IF NOT EXISTS probe (a INT);
INSERT INTO probe (a) VALUES (1);
EOF
if replay_file_runs_outside_a_transaction "$TMP/nested_block_comment.up.sql"; then
    fail "PostgreSQL nests /* */; text after an INNER close is still comment and must not strip -1"
else
    pass "a NESTED block comment is comment all the way to its outer close, and keeps -1"
fi

cat > "$TMP/quoted_identifier.up.sql" <<'EOF'
CREATE TABLE IF NOT EXISTS "not built CONCURRENTLY" (a INT);
INSERT INTO probe (a) VALUES (1);
EOF
if replay_file_runs_outside_a_transaction "$TMP/quoted_identifier.up.sql"; then
    fail "CONCURRENTLY inside a \"quoted identifier\" is a name, not the keyword, and must not strip -1"
else
    pass "CONCURRENTLY inside a quoted identifier keeps -1"
fi

cat > "$TMP/quoted_identifier_doubled.up.sql" <<'EOF'
CREATE TABLE IF NOT EXISTS "a "" quoted CONCURRENTLY name" (a INT);
INSERT INTO probe (a) VALUES (1);
EOF
if replay_file_runs_outside_a_transaction "$TMP/quoted_identifier_doubled.up.sql"; then
    fail "a \"\" escape must not end a quoted identifier early"
else
    pass "a \"\" escape inside a quoted identifier keeps the whole name blanked"
fi

cat > "$TMP/e_string_backslash.up.sql" <<'EOF'
INSERT INTO notes (body) VALUES (E'the migration\'s note about CONCURRENTLY');
INSERT INTO notes (body) VALUES ('second');
EOF
if replay_file_runs_outside_a_transaction "$TMP/e_string_backslash.up.sql"; then
    fail "a backslash escape inside an E'...' literal must not end it early"
else
    pass "a backslash escape inside an E'...' literal keeps the whole literal blanked"
fi

cat > "$TMP/non_ascii_identifier.up.sql" <<'EOF'
CREATE TABLE IF NOT EXISTS éCONCURRENTLYé (a INT);
INSERT INTO probe (a) VALUES (1);
EOF
if replay_file_runs_outside_a_transaction "$TMP/non_ascii_identifier.up.sql"; then
    fail "a non-ASCII letter is an identifier character in Postgres: éCONCURRENTLYé is a name, not the keyword"
else
    pass "non-ASCII identifier characters count as word characters at the keyword boundary"
fi

# The other polarity for every new class: none of them may HIDE a real CONCURRENTLY that follows.
cat > "$TMP/dollar_then_real.up.sql" <<'EOF'
COMMENT ON TABLE probe IS $$prose with an apostrophe: the migration's note$$;
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_probe ON probe (a);
EOF
if replay_file_runs_outside_a_transaction "$TMP/dollar_then_real.up.sql"; then
    pass "a real CONCURRENTLY statement is still found after a dollar-quoted literal"
else
    fail "a dollar-quoted literal must not swallow the real CONCURRENTLY that follows it"
fi

cat > "$TMP/quoted_ident_then_real.up.sql" <<'EOF'
CREATE TABLE IF NOT EXISTS "probe's table" (a INT);
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_probe ON probe (a);
EOF
if replay_file_runs_outside_a_transaction "$TMP/quoted_ident_then_real.up.sql"; then
    pass "a real CONCURRENTLY statement is still found after a quoted identifier containing an apostrophe"
else
    fail "a quoted identifier must not swallow the real CONCURRENTLY that follows it"
fi

cat > "$TMP/e_string_then_real.up.sql" <<'EOF'
INSERT INTO notes (body) VALUES (E'ends with a literal backslash \\');
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_probe ON probe (a);
EOF
if replay_file_runs_outside_a_transaction "$TMP/e_string_then_real.up.sql"; then
    pass "a real CONCURRENTLY statement is still found after an E'...' literal ending in \\\\"
else
    fail "an E'...' literal ending in an escaped backslash must not swallow the CONCURRENTLY that follows"
fi

cat > "$TMP/nested_comment_then_real.up.sql" <<'EOF'
/* outer /* inner */ still outer */
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_probe ON probe (a);
EOF
if replay_file_runs_outside_a_transaction "$TMP/nested_comment_then_real.up.sql"; then
    pass "a real CONCURRENTLY statement is still found after a nested block comment"
else
    fail "a nested block comment must not swallow the real CONCURRENTLY that follows it"
fi

# standard_conforming_strings is ON by default in PostgreSQL 17, so a backslash in an ORDINARY
# '...' literal escapes nothing — only E'...' gives it that power. Treating every literal as
# backslash-escaped would run this one past its closing quote and hide the statement below.
cat > "$TMP/standard_backslash_literal.up.sql" <<'EOF'
INSERT INTO notes (body) VALUES ('ends with a backslash \');
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_probe ON probe (a);
EOF
if replay_file_runs_outside_a_transaction "$TMP/standard_backslash_literal.up.sql"; then
    pass "a backslash in a STANDARD literal does not escape the closing quote"
else
    fail "a backslash in a standard-conforming literal must not extend the literal past its close"
fi

# ...and the E must be a token of its own. A trailing e on an identifier immediately followed by
# a quote is an ordinary literal, not an escape-string. (Lexically synthetic on purpose: the point
# is the boundary rule, and a fixture that is also idiomatic SQL cannot exhibit it.)
cat > "$TMP/trailing_e_is_not_estring.up.sql" <<'EOF'
INSERT INTO notes (body) VALUES (probe'ends with a backslash \');
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_probe ON probe (a);
EOF
if replay_file_runs_outside_a_transaction "$TMP/trailing_e_is_not_estring.up.sql"; then
    pass "an e ending an identifier does not turn the following literal into an E-string"
else
    fail "an e that is part of an identifier must not be read as the E-string prefix"
fi

# ⚠ THE KEYWORD AT COLUMN 1. `CREATE INDEX` on one line and `CONCURRENTLY IF NOT EXISTS ...` on the
# next is ordinary formatting, and it puts the keyword at offset 1 of its line — where the
# left-hand boundary check has no preceding byte to look at. awk implementations DISAGREE about
# substr(s, 0, 1): gawk returns "", the BWK awk that ships on macOS returns the FIRST character,
# which reads as a word byte and suppresses the match. Found by an edge sweep of this classifier's
# own rewrite (#1569 round 17 task 10) — it was live, and it failed toward a spurious `-1`, i.e.
# toward a loud psql error rather than a silent one.
cat > "$TMP/keyword_at_column_one.up.sql" <<'EOF'
CREATE INDEX
CONCURRENTLY IF NOT EXISTS idx_probe ON probe (a);
EOF
if replay_file_runs_outside_a_transaction "$TMP/keyword_at_column_one.up.sql"; then
    pass "a CONCURRENTLY that starts its line (column 1) is still found"
else
    fail "a CONCURRENTLY at column 1 must be found: there is no preceding byte, not a word byte"
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
    # #1626 Phase 3 Task G: the partial UNIQUE index scoped to the daily_capture source. Sole
    # statement in its file, CONCURRENTLY, exactly like 000182 above.
    000194_add_balance_snapshots_daily_capture_unique_index.up.sql
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
    # #1626 Phase 3 Task G: 000194's drop-first companion. It mentions CONCURRENTLY only while
    # EXPLAINING why 000194 cannot share a file with it; its own statements are a COMMENT ON COLUMN
    # and a DO block, both of which need -1's single-transaction semantics.
    000193_balance_snapshot_daily_capture_provenance.up.sql
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
