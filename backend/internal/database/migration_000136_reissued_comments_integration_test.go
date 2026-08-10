//go:build integration

// Integration tests for migration 000136 (re-issue the two corrected plaid_orphaned_items retention
// COMMENTs) — Issue #1497 follow-up.
//
// WHY THIS FILE IS WORTH ITS RUNTIME. 000136 does nothing but set two comments, so the only thing
// that can be wrong with it is the TEXT — and text is exactly what a reviewer's eye slides over. The
// migration exists because 000135's retention wording was corrected in place AFTER 000135 had been
// applied to QA/staging, leaving those catalogues on the old, overstated claim ("the token must not
// exist past the deadline"). The correction's whole point is that two halves of DIFFERENT STRENGTH
// must stay distinct: USE stops at the deadline exactly, EXISTENCE ends at the first purge sweep at
// or after it. Every previous attempt to restate that has re-compressed it into "gone within an
// hour", which is wrong twice over.
//
// So these tests do not paraphrase the expected text. They PARSE IT OUT OF 000135's own .up.sql at
// run time and require the live pg_description entry to equal it byte for byte. A paraphrase in
// 000136, a drift between the two files, or a future edit to one site only all fail here.
//
// Run with:
//
//	cd backend && go test ./internal/database/... \
//	  -run 'TestMigration000136' -tags integration -v -p 1
//
// Shares beginRolledBackTx/readMigrationFile with the 000131–000134 tests.

package database

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func applyMigration000136Up(t *testing.T, tx pgx.Tx) error {
	t.Helper()
	_, err := tx.Exec(context.Background(),
		readMigrationFile(t, "000136_reissue_plaid_orphaned_items_retention_comments.up.sql"))
	return err
}

func applyMigration000136Down(t *testing.T, tx pgx.Tx) error {
	t.Helper()
	_, err := tx.Exec(context.Background(),
		readMigrationFile(t, "000136_reissue_plaid_orphaned_items_retention_comments.down.sql"))
	return err
}

// sqlLiteralLine matches one line of a multi-line SQL string literal: leading whitespace, then a
// single-quoted chunk, optionally followed by the statement terminator.
var sqlLiteralLine = regexp.MustCompile(`^\s*'(.*)'\s*;?\s*$`)

// commentTextFromMigration extracts the STRING VALUE of a `COMMENT ON ... IS '...' '...';` statement
// from a migration file, concatenating the adjacent string literals exactly as Postgres does and
// un-escaping doubled quotes. This is how the expected text is obtained rather than being copied into
// this file: a hard-coded expectation here would be a third place the wording could drift.
func commentTextFromMigration(t *testing.T, migrationFile, statementPrefix string) string {
	t.Helper()

	body := readMigrationFile(t, migrationFile)
	idx := strings.Index(body, statementPrefix)
	require.GreaterOrEqual(t, idx, 0,
		"migration %s must contain a %q statement", migrationFile, statementPrefix)

	rest := body[idx+len(statementPrefix):]
	var parts []string
	for _, line := range strings.Split(rest, "\n") {
		m := sqlLiteralLine.FindStringSubmatch(line)
		require.NotNil(t, m,
			"expected a quoted literal line while parsing %s after %q, got: %q",
			migrationFile, statementPrefix, line)
		parts = append(parts, m[1])
		if strings.HasSuffix(strings.TrimSpace(line), ";") {
			break
		}
	}
	require.NotEmpty(t, parts)

	return strings.ReplaceAll(strings.Join(parts, ""), "''", "'")
}

func orphanedItemsTableComment(t *testing.T, tx pgx.Tx) string {
	t.Helper()
	var got string
	require.NoError(t, tx.QueryRow(context.Background(),
		`SELECT obj_description('plaid_orphaned_items'::regclass, 'pg_class')`).Scan(&got))
	return got
}

func orphanedItemsColumnComment(t *testing.T, tx pgx.Tx, column string) string {
	t.Helper()
	var got string
	require.NoError(t, tx.QueryRow(context.Background(), `
		SELECT col_description(a.attrelid, a.attnum)
		FROM pg_attribute a
		WHERE a.attrelid = 'plaid_orphaned_items'::regclass
		  AND a.attname = $1`, column).Scan(&got))
	return got
}

// TestMigration000136_ReissuesExactly000135sCorrectedText is the test the migration exists for.
//
// It overwrites both comments with the OLD overstated wording first — taken from 000136's own
// .down.sql, which is where that wording is preserved — so that the "before" state is the state a
// QA/staging database is genuinely in, not merely whatever the last migration left behind. Then it
// applies 000136 up and requires both catalogue entries to equal 000135's text exactly.
//
// Without this, a version of 000136 that summarised the retention bound instead of re-issuing it
// would pass every other gate in the repo: Squawk lints clean on any COMMENT, and the migration test
// suite only checks that migrations apply.
func TestMigration000136_ReissuesExactly000135sCorrectedText(t *testing.T) {
	tx := beginRolledBackTx(t)

	// Put the catalogue into the pre-correction state.
	require.NoError(t, applyMigration000136Down(t, tx),
		"000136 down must apply — it restores 000135's original wording")

	overstatedColumn := orphanedItemsColumnComment(t, tx, "token_expires_at")
	require.Contains(t, overstatedColumn, "must not exist",
		"sanity: the down migration is supposed to reinstate the overstated claim, "+
			"otherwise this test is not testing a real transition")

	require.NoError(t, applyMigration000136Up(t, tx))

	wantTable := commentTextFromMigration(t,
		"000135_add_plaid_orphaned_items_token_retention.up.sql",
		"COMMENT ON TABLE plaid_orphaned_items IS\n")
	wantColumn := commentTextFromMigration(t,
		"000135_add_plaid_orphaned_items_token_retention.up.sql",
		"COMMENT ON COLUMN plaid_orphaned_items.token_expires_at IS\n")

	assert.Equal(t, wantTable, orphanedItemsTableComment(t, tx),
		"the live table comment must be 000135's text VERBATIM — not a summary, and not drifted from it")
	assert.Equal(t, wantColumn, orphanedItemsColumnComment(t, tx, "token_expires_at"),
		"the live token_expires_at comment must be 000135's text VERBATIM")
}

// TestMigration000136_KeepsTheTwoHalvesOfTheBoundDistinct pins the SEMANTICS the wording carries,
// independently of whether the two files agree with each other. If some future edit changed BOTH
// 000135 and 000136 to the same wrong thing, the verbatim test above would still pass; this one would
// not.
//
// The two halves are not the same strength and the whole GDPR argument rests on that:
//   - USE stops at the deadline EXACTLY (enforced on every path that could present the token to Plaid).
//   - EXISTENCE ends at the FIRST SWEEP at or after the deadline — a bound PER SWEEP, not a ceiling.
func TestMigration000136_KeepsTheTwoHalvesOfTheBoundDistinct(t *testing.T) {
	tx := beginRolledBackTx(t)

	require.NoError(t, applyMigration000136Up(t, tx))

	got := orphanedItemsColumnComment(t, tx, "token_expires_at")

	assert.Contains(t, got, "MUST NOT BE USED FROM THIS INSTANT",
		"half 1: the exact-boundary claim about USE must survive")
	assert.Contains(t, got, "CEASES TO EXIST AT THE FIRST PURGE SWEEP AT OR AFTER THIS INSTANT",
		"half 2: existence must be tied to a sweep, not to the deadline")
	assert.Contains(t, got, "bound PER SWEEP, not an absolute ceiling",
		"half 2 must stay explicitly non-absolute — this is the phrase every paraphrase drops")
	assert.NotContains(t, got, "must not exist",
		"the overstated claim 000136 exists to remove must not be reintroduced")

	table := orphanedItemsTableComment(t, tx)
	assert.Contains(t, table, "at the first run at or after the deadline",
		"the table comment must not collapse the purge into the deadline itself")
	assert.Contains(t, table, "the token stops being USABLE at the",
		"the table comment must point at the use/existence split rather than restating one half")
}

// TestMigration000136_ReapplyUpIsNoOp: COMMENT is inherently repeatable — setting the same comment
// twice leaves the same comment — so a re-run must be byte-identical rather than merely error-free.
// Pinned because golang-migrate will not re-run 000136, but scripts/test-migrations.sh's idempotency
// suite does, and a hand-run of `psql -f` on a QA box is exactly the situation this migration was
// written for.
func TestMigration000136_ReapplyUpIsNoOp(t *testing.T) {
	tx := beginRolledBackTx(t)

	require.NoError(t, applyMigration000136Up(t, tx))
	firstTable := orphanedItemsTableComment(t, tx)
	firstColumn := orphanedItemsColumnComment(t, tx, "token_expires_at")

	require.NoError(t, applyMigration000136Up(t, tx), "000136 must be re-appliable")

	assert.Equal(t, firstTable, orphanedItemsTableComment(t, tx))
	assert.Equal(t, firstColumn, orphanedItemsColumnComment(t, tx, "token_expires_at"))
}

// TestMigration000136_TouchesNothingButTheTwoComments guards the scope claim in the migration's own
// header. A comment-only migration that quietly altered a column would be the worst kind of surprise
// in a file nobody expects to be structural, and the five OTHER column comments 000135 sets are
// deliberately not re-issued — re-issuing unchanged text would obscure which two claims moved.
//
// ⚠ TWO THINGS THIS TEST GETS RIGHT ONLY BECAUSE THE FIRST VERSION GOT THEM WRONG (Codex, #1503
// review):
//
//  1. It applies the DOWN migration before snapshotting. The test DB arrives fully migrated, i.e.
//     with 000136 ALREADY APPLIED, so a snapshot taken before re-applying up would already contain
//     any stray comment or structural change the migration makes — and comparing that state to
//     itself would show no difference. Running down first makes the "before" state genuinely
//     pre-000136. (down only rewrites the same two comments, so it cannot itself mask a
//     third-object change.)
//  2. It snapshots CONSTRAINTS, INDEXES, the table comment and the actual DEFAULT EXPRESSION —
//     not merely "does a default exist". The first version checked four column attributes and
//     claimed to guard the whole scope; several of the things 000136's own header forbids were
//     invisible to it.
func TestMigration000136_TouchesNothingButTheTwoComments(t *testing.T) {
	tx := beginRolledBackTx(t)
	ctx := context.Background()

	type colShape struct {
		dataType   string
		isNullable string
		colDefault string
		comment    string
	}
	snapshotColumns := func() map[string]colShape {
		rows, err := tx.Query(ctx, `
			SELECT c.column_name, c.data_type, c.is_nullable,
			       COALESCE(c.column_default, ''),
			       COALESCE(col_description(a.attrelid, a.attnum), '')
			FROM information_schema.columns c
			JOIN pg_attribute a
			  ON a.attrelid = 'plaid_orphaned_items'::regclass
			 AND a.attname = c.column_name
			WHERE c.table_schema = current_schema()
			  AND c.table_name = 'plaid_orphaned_items'`)
		require.NoError(t, err)
		defer rows.Close()

		out := map[string]colShape{}
		for rows.Next() {
			var name string
			var s colShape
			require.NoError(t, rows.Scan(&name, &s.dataType, &s.isNullable, &s.colDefault, &s.comment))
			out[name] = s
		}
		require.NoError(t, rows.Err())
		return out
	}

	// Every constraint on the table, by name, with its full definition — so a widened CHECK or a
	// dropped NOT NULL is visible, not just an added/removed one.
	snapshotConstraints := func() map[string]string {
		rows, err := tx.Query(ctx, `
			SELECT conname, pg_get_constraintdef(oid)
			FROM pg_constraint
			WHERE conrelid = 'plaid_orphaned_items'::regclass`)
		require.NoError(t, err)
		defer rows.Close()

		out := map[string]string{}
		for rows.Next() {
			var name, def string
			require.NoError(t, rows.Scan(&name, &def))
			out[name] = def
		}
		require.NoError(t, rows.Err())
		return out
	}

	// 000135's lock-cost argument rests on adding NO index. An index appearing here would falsify
	// that argument silently.
	snapshotIndexes := func() map[string]string {
		rows, err := tx.Query(ctx, `
			SELECT indexname, indexdef
			FROM pg_indexes
			WHERE schemaname = current_schema() AND tablename = 'plaid_orphaned_items'`)
		require.NoError(t, err)
		defer rows.Close()

		out := map[string]string{}
		for rows.Next() {
			var name, def string
			require.NoError(t, rows.Scan(&name, &def))
			out[name] = def
		}
		require.NoError(t, rows.Err())
		return out
	}

	rowCount := func() int64 {
		var n int64
		require.NoError(t, tx.QueryRow(ctx, `SELECT count(*) FROM plaid_orphaned_items`).Scan(&n))
		return n
	}

	// Get to a genuinely pre-000136 state first — see note (1) above.
	require.NoError(t, applyMigration000136Down(t, tx))

	beforeCols := snapshotColumns()
	beforeCons := snapshotConstraints()
	beforeIdx := snapshotIndexes()
	beforeRows := rowCount()
	require.NotEmpty(t, beforeCols, "plaid_orphaned_items must exist once 000135 is applied")

	require.NoError(t, applyMigration000136Up(t, tx))

	afterCols := snapshotColumns()
	require.Equal(t, len(beforeCols), len(afterCols), "000136 must not add or drop a column")
	for name, b := range beforeCols {
		a, ok := afterCols[name]
		require.True(t, ok, "column %s must survive", name)
		assert.Equal(t, b.dataType, a.dataType, "column %s data type must not move", name)
		assert.Equal(t, b.isNullable, a.isNullable, "column %s nullability must not move", name)
		assert.Equal(t, b.colDefault, a.colDefault, "column %s DEFAULT EXPRESSION must not move", name)
		if name != "token_expires_at" {
			assert.Equal(t, b.comment, a.comment,
				"000136 must re-issue ONLY token_expires_at's comment, but %s's changed", name)
		}
	}

	assert.Equal(t, beforeCons, snapshotConstraints(),
		"000136 must not add, drop or redefine a constraint — including the "+
			"plaid_orphaned_items_purged_token_is_null CHECK that 000135 spends its one constraint on")
	assert.Equal(t, beforeIdx, snapshotIndexes(),
		"000136 must add no index — 000135's lock-cost argument rests on there being none")
	assert.Equal(t, beforeRows, rowCount(), "000136 must not touch a single row")
}
