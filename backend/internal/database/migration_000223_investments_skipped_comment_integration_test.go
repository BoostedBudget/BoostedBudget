//go:build integration

// Integration tests for migration 000223 (correct the COMMENT on
// linked_items.investments_skipped_at_link) — Issue #1707.
//
// WHY THIS FILE IS WORTH ITS RUNTIME. 000223 does nothing but set one column comment, so the only
// thing that can be wrong with it is the TEXT — and text is exactly what a reviewer's eye slides
// over. The migration exists because 000189 created the column deliberately unwired and said so in
// the comment, and the wiring landed later under #1613 (the writer) and #1707 (the reader), leaving
// the catalogue asserting a falsehood. Squawk lints clean on any COMMENT and the five-suite
// migration script compares schema shape/hashes, not comments, so neither gate can see this: the
// round-trip is asserted here or nowhere.
//
// BOTH DIRECTIONS ARE ASSERTED, and the negative half is load-bearing. A test that only greps the
// live comment for the corrected wording would still pass if the stale sentence survived ALONGSIDE
// it — which is precisely the self-contradicting comment 000223 exists to remove. So the up
// direction asserts the stale clause is GONE, and the down direction asserts 000189's text comes
// back byte-for-byte, so an up -> down -> up cycle reproduces the schema exactly.
//
// Run with:
//
//	cd backend && DATABASE_URL=postgres://... go test ./internal/database/... \
//	  -run TestMigration000223 -tags integration -v -p 1
//
// Shares beginRolledBackTx/readMigrationFile with the 000100 and 000131-000136 tests. Each test runs
// inside a transaction that is ALWAYS rolled back, so the committed catalogue is never modified.

package database

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	migration000223UpFile   = "000223_correct_investments_skipped_at_link_comment.up.sql"
	migration000223DownFile = "000223_correct_investments_skipped_at_link_comment.down.sql"
	migration000189UpFile   = "000189_add_repair_nonces_and_failure_assert_tracking.up.sql"

	// migration000223CommentStatement is the statement prefix 000189, 000223's up file and
	// 000223's down file all open the comment with. Anchoring the parser on it is LOAD-BEARING:
	// 000189's up file carries fourteen $COMMENT$ pairs (one per commented column), so a parser
	// that grabbed the FIRST $COMMENT$ in the file would extract a DIFFERENT column's text and
	// then compare two unrelated strings.
	migration000223CommentStatement = "COMMENT ON COLUMN linked_items.investments_skipped_at_link IS"

	// stalePhrase is the clause 000223's up migration exists to remove. It must be absent after
	// up — see the negative half below.
	stalePhrase = "deliberately NOT WIRED"

	// correctedPhrase is wording that only 000223's up migration introduces: the name of the
	// reader whose arrival made the old comment false.
	correctedPhrase = "JobHealthRepository.GetInvestmentsSkippedAtLinkCount"
)

func applyMigration000223Up(t *testing.T, tx pgx.Tx) error {
	t.Helper()
	_, err := tx.Exec(context.Background(), readMigrationFile(t, migration000223UpFile))
	return err
}

func applyMigration000223Down(t *testing.T, tx pgx.Tx) error {
	t.Helper()
	_, err := tx.Exec(context.Background(), readMigrationFile(t, migration000223DownFile))
	return err
}

// dollarQuotedCommentBody extracts the BODY of a
// `COMMENT ON COLUMN linked_items.investments_skipped_at_link IS $COMMENT$...$COMMENT$;`
// statement from a migration file.
//
// WHY A BESPOKE PARSER. migration_000136's commentTextFromMigration reads SINGLE-quoted literals
// only, and 000189/000223 dollar-quote the body with $COMMENT$, so that helper cannot read either
// file. The statement is located first for the reason spelled out on
// migration000223CommentStatement: 000189 contains many other $COMMENT$ bodies.
func dollarQuotedCommentBody(t *testing.T, migrationFile string) string {
	t.Helper()

	body := readMigrationFile(t, migrationFile)
	idx := strings.Index(body, migration000223CommentStatement)
	require.GreaterOrEqual(t, idx, 0,
		"migration %s must contain a %q statement", migrationFile, migration000223CommentStatement)

	const tag = "$COMMENT$"
	rest := body[idx+len(migration000223CommentStatement):]
	open := strings.Index(rest, tag)
	require.GreaterOrEqual(t, open, 0,
		"migration %s must dollar-quote the comment body with %s", migrationFile, tag)

	afterOpen := rest[open+len(tag):]
	closing := strings.Index(afterOpen, tag)
	require.GreaterOrEqual(t, closing, 0,
		"migration %s must close the %s body it opened", migrationFile, tag)

	return afterOpen[:closing]
}

// linkedItemsInvestmentsSkippedComment reads the LIVE column comment out of the catalogue inside
// the current transaction, so the assertion is about the state this run actually produced rather
// than anything cached or read at compile time.
func linkedItemsInvestmentsSkippedComment(t *testing.T, tx pgx.Tx) string {
	t.Helper()
	var got string
	require.NoError(t, tx.QueryRow(context.Background(), `
		SELECT col_description(a.attrelid, a.attnum)
		FROM pg_attribute a
		WHERE a.attrelid = 'linked_items'::regclass
		  AND a.attname = 'investments_skipped_at_link'`).Scan(&got))
	return got
}

// TestMigration000223_UpRemovesTheStaleClauseAndInstallsTheCorrectedOne is the test the migration
// exists for.
//
// It overwrites the comment with the stale wording first — taken from 000223's own .down.sql, which
// is where 000189's text is preserved — so the "before" state is the state a database that has not
// yet run 000223 is genuinely in, not merely whatever the last migration left behind. Then it
// applies 000223 up and requires BOTH halves: the corrected wording is present, and the stale clause
// is gone.
func TestMigration000223_UpRemovesTheStaleClauseAndInstallsTheCorrectedOne(t *testing.T) {
	tx := beginRolledBackTx(t)

	// Put the catalogue into the pre-correction state. The database arrives with 000223 already
	// applied (that is the state `migrate up` leaves), so a snapshot taken before re-applying up
	// would already contain the corrected text and the comparison would show no transition.
	require.NoError(t, applyMigration000223Down(t, tx),
		"000223 down must apply — it restores 000189's wording")

	stale := linkedItemsInvestmentsSkippedComment(t, tx)
	require.Contains(t, stale, stalePhrase,
		"sanity: the down migration is supposed to reinstate the stale clause, "+
			"otherwise this test is not testing a real transition")
	require.NotContains(t, stale, correctedPhrase,
		"sanity: the pre-correction text must not already name the reader")

	require.NoError(t, applyMigration000223Up(t, tx))

	got := linkedItemsInvestmentsSkippedComment(t, tx)
	assert.Contains(t, got, correctedPhrase,
		"the corrected comment must name the reader that now exists")

	// THE NEGATIVE HALF, and the reason this test is not a one-line grep: a comment that kept the
	// stale sentence and appended the corrected one would pass a Contains-only assertion while
	// remaining exactly as misleading as before.
	assert.NotContains(t, got, stalePhrase,
		"the stale clause must be GONE, not merely joined by the corrected wording")
}

// TestMigration000223_DownRestores000189sTextVerbatim pins the rollback's faithfulness. The down
// migration is not a correction — it deliberately restores text known to be stale — and its whole
// contract is that an up -> down -> up cycle reproduces the schema byte-for-byte. The expected text
// is PARSED OUT OF 000189's own up file rather than copied into this file, so a paraphrase in the
// down file, a drift between the two files, or a future edit to one site only all fail here.
func TestMigration000223_DownRestores000189sTextVerbatim(t *testing.T) {
	tx := beginRolledBackTx(t)

	require.NoError(t, applyMigration000223Up(t, tx))
	require.NoError(t, applyMigration000223Down(t, tx))

	want := dollarQuotedCommentBody(t, migration000189UpFile)
	got := linkedItemsInvestmentsSkippedComment(t, tx)

	assert.Equal(t, want, got,
		"the down migration must restore 000189's text VERBATIM — a paraphrase would make the "+
			"up -> down -> up cycle fail to reproduce the schema byte-for-byte")
}

// TestMigration000223_ReapplyUpIsNoOp: COMMENT is inherently repeatable — setting the same comment
// twice leaves the same comment — so a re-run must be byte-identical rather than merely error-free.
// Pinned because a container restart re-runs migrations and a hand-run of `psql -f` on a QA box is
// exactly the situation this migration was written for.
func TestMigration000223_ReapplyUpIsNoOp(t *testing.T) {
	tx := beginRolledBackTx(t)

	require.NoError(t, applyMigration000223Up(t, tx))
	first := linkedItemsInvestmentsSkippedComment(t, tx)

	require.NoError(t, applyMigration000223Up(t, tx), "000223 must be re-appliable")

	assert.Equal(t, first, linkedItemsInvestmentsSkippedComment(t, tx))
}
