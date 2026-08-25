package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Issue #1569 Task 6 (#1579) — the WRITER for transaction_annotation_history.
//
// It is not a Go function, and that is the point. Three UPDATE statements destroy a user's
// annotation (UpdateCategory, BulkUpdateCategory, SetReviewed); each one wraps itself in the CTE
// built here, so the prior value is read under the SAME snapshot and the SAME row locks as the
// write that overwrites it. Codex's finding on this task was that the obvious shape — read the old
// category in the service, then update — records a WRONG prior value whenever another writer lands
// in between. Putting the INSERT in the same statement removes the window rather than narrowing
// it.
//
// HOW `prior` SEES THE OLD ROW. `UPDATE transactions t ... FROM transactions prior WHERE
// prior.id = t.id` joins the target against a second scan of the same table. A statement never sees
// its own effects, so `prior.*` resolves to the pre-update tuple while `RETURNING t.*` gives the
// post-update one — both from one statement, with no second round trip.
//
// ⚠ THE SELF-JOIN ALONE IS NOT ENOUGH, AND THE FIRST VERSION OF THIS FILE UNDERSTATED WHY. It
// claimed the residual was "a slightly stale prior value". Review measured it with two concurrent
// psql sessions and it is two distinct failures, both worse than staleness, both reproduced in
// services/transaction_annotation_history_concurrency_test.go:
//
//  1. A FABRICATED TRANSITION. prior.* is resolved against the statement's ORIGINAL snapshot;
//     RETURNING t.* is the POST-EvalPlanQual tuple. If another session commits between the two, one
//     history row is assembled from two different snapshots and describes a before->after step that
//     no write ever performed — measured: a single row claiming ENTERTAINMENT/reviewed-in-March ->
//     FOOD_AND_DRINK/unreviewed. A false record is worse than a missing one, because this table
//     exists to be believed.
//  2. THE RECORD IS DROPPED. The suppression below compares the STALE prior against the post-EPQ
//     new. A write that lands on exactly the stale value compares equal, the INSERT is suppressed,
//     the API returns success, and the other session's committed annotation is destroyed with no
//     trail — #1579's own defect, surviving #1579's fix. Measured with two SetReviewed calls.
//
// THE FIX IS LOCK-FIRST, and it is NOT the SELECT-then-UPDATE the plan rejected. Each of the three
// statements now runs as:
//
//	BEGIN;
//	SELECT id FROM transactions WHERE <the write's own predicate> ORDER BY id FOR UPDATE;
//	<the capturing UPDATE>;
//	COMMIT;
//
// The difference from the rejected shape is the LOCK. A plain read-then-write holds nothing, so its
// window is unbounded. Here the target rows are locked before the capture, and under READ COMMITTED
// the UPDATE — a separate statement — takes a FRESH snapshot that already contains anything that
// committed while we waited for the lock. Nothing can commit against those rows between the two
// statements, so `prior` is exact rather than merely recent, and both halves of every row come from
// one snapshot.
//
// ORDER BY id is deadlock hygiene, not decoration: two concurrent bulk writers over overlapping id
// sets would otherwise be free to take the same locks in opposite orders. It reduces the risk to
// the ordinary single-order case; it does not make deadlock unrepresentable, since Postgres does
// not contract to lock in sort order under every plan.
//
// The cost is one extra round trip and an explicit transaction on three methods. r.db.Begin works
// whether r.db is a pool or an existing pgx.Tx (pgx opens a savepoint for the nested case), so a
// caller that already has a transaction is unaffected.
//
// MEASURED, on 200,000 transactions in the local Postgres 17 with fresh ANALYZE stats — not on an
// empty database, because an EXPLAIN on an empty database is worthless and this plan already
// shipped one headline number that was 5.4x backwards. EXPLAIN (ANALYZE, BUFFERS):
//
//	                              plan                                     time     buffers
//	bulk lock (50 ids)            LockRows -> Sort -> Bitmap Heap Scan     0.363 ms   250
//	                              (Bitmap Index Scan on transactions_pkey)
//	bulk capturing UPDATE         Nested Loop: Bitmap Heap Scan on t       3.732 ms  3343
//	                              x Index Scan on prior (id = t.id)
//	bulk baseline (pre-#1569,     Bitmap Heap Scan, no join, no capture    1.777 ms  3168
//	  no self-join, no capture)
//	single lock                   LockRows -> Index Scan on pkey           0.031 ms     5
//	single capturing UPDATE       Nested Loop of two pkey Index Scans      0.291 ms    66
//	single baseline               Index Scan on pkey                       0.669 ms     -
//
// THE QUESTION THIS ANSWERS is whether `prior.id = t.id` degenerates into a seq scan on
// transactions for the `id = ANY(...)` bulk form. It does not: the inner side is an Index Scan on
// transactions_pkey with Index Cond `(id = t.id)` in every plan, and no plan contains a Seq Scan.
// The self-join adds ~200 buffer hits (3168 -> 3343) on a 50-id batch; the rest of the delta is the
// history INSERT and its user_id FK trigger, which is the feature, not the join.
//
// WHAT COUNTS AS A CHANGE. The INSERT is filtered by a row-wise IS DISTINCT FROM over EVERY tracked
// column, reviewed_at included. Consequences, both deliberate:
//
//   - Un-reviewing an already-unreviewed transaction records NOTHING. Nothing was overwritten, and
//     an append-only trail that grows on no-ops buries the overwrites it exists to show.
//   - Re-applying the SAME category IS recorded, because both category statements write
//     `reviewed = true, reviewed_at = <now>` unconditionally and that timestamp genuinely moved.
//     The moved timestamp is itself an unrecoverable fact — it is the half of #1579 the ticket
//     missed — so suppressing it would reintroduce the defect on the narrow path.
//
// TENANCY is inherited, never re-derived: the INSERT selects `user_id` out of the UPDATE's own
// RETURNING, so a row can only be recorded for a transaction the UPDATE's WHERE already matched.
// The bulk statements take a client-supplied id array, and this is why a foreign id in that array
// cannot produce a history row.
//
// TOMBSTONES. None of the three statements filters `deleted_at IS NULL` (each carries its own
// `tombstone-exempt:` note explaining that liveness is the service layer's gate), and this task does
// not change that — narrowing a write predicate is a behaviour change far outside "record the prior
// value". The capture therefore mirrors the write exactly: whatever the UPDATE touched is recorded.
// That is the safe direction, since a tombstoned transaction's prior annotation is the least
// recoverable of all.

// annotationCapturedColumns is the ONE place the captured column set is written down. The RETURNING
// list, the INSERT column list, the SELECT list and BOTH sides of the change comparison are all
// generated from it, in this order.
//
// ⚠ Review asked for this and was right: the comparison tuple used to be spelled out TWICE inside
// one fmt.Sprintf. A column added to the table and to only one of the two tuples would have changed
// what counts as a no-op — silently, with every test green, because no assertion anywhere pinned
// the two spellings against each other. Generating them removes the failure mode instead of
// guarding it.
//
// Order matters only for readability; it is the table's column order.
var annotationCapturedColumns = []string{
	"category_override_kind",
	"pfc_primary_override",
	"pfc_detailed_override",
	"custom_category_override_id",
	"effective_category_name",
	"category_source",
	"reviewed",
	"reviewed_at",
}

// annotationCaptureReturning is the RETURNING list every capturing UPDATE must expose. `prior.*` is
// the pre-update tuple from the self-join; the target's own columns are the post-update ones.
var annotationCaptureReturning = buildAnnotationCaptureReturning()

func buildAnnotationCaptureReturning() string {
	var b strings.Builder
	b.WriteString("\n\t\t\tt.id      AS captured_transaction_id,\n\t\t\tt.user_id AS captured_user_id")
	for _, col := range annotationCapturedColumns {
		fmt.Fprintf(&b, ",\n\t\t\tprior.%s AS prior_%s", col, col)
	}
	for _, col := range annotationCapturedColumns {
		fmt.Fprintf(&b, ",\n\t\t\tt.%s AS new_%s", col, col)
	}
	return b.String()
}

// annotationCaptureColumnList renders the captured columns with a prefix and a separator, which is
// how one slice becomes four different lists.
func annotationCaptureColumnList(prefix, sep string) string {
	out := make([]string, 0, len(annotationCapturedColumns))
	for _, col := range annotationCapturedColumns {
		out = append(out, prefix+col)
	}
	return strings.Join(out, sep)
}

// annotationCaptureInsert returns the recording CTE plus the count projection that replaces the
// UPDATE's own RowsAffected. changeSource is a compile-time constant from this package, never user
// input — it is interpolated because a CTE body cannot take a parameter placeholder in a position
// the caller's own $n numbering does not control, and the two call-site values are pinned against
// the live CHECK by TestAnnotationCaptureSourcesMatchTheCheckConstraint.
//
// The `recorded` CTE is never referenced by the final SELECT. That is correct and not a bug: a
// data-modifying statement inside WITH is executed exactly once, to completion, whether or not the
// primary query reads its output.
func annotationCaptureInsert(changeSource string) string {
	priorCols := annotationCaptureColumnList("prior_", ", ")
	newCols := annotationCaptureColumnList("new_", ", ")

	return fmt.Sprintf(`
		, recorded AS (
			INSERT INTO transaction_annotation_history (
				user_id, transaction_id, change_source,
				%[2]s,
				%[3]s
			)
			SELECT
				captured_user_id, captured_transaction_id, '%[1]s',
				%[2]s,
				%[3]s
			FROM updated
			WHERE (%[2]s) IS DISTINCT FROM (%[3]s)
		)
		SELECT COUNT(*) FROM updated`, changeSource, priorCols, newCols)
}

// annotationLockTargets takes the row locks the capture depends on, inside tx, using the SAME
// predicate as the write that follows. Returning no rows is not an error: an id the caller does not
// own, or one that does not exist, simply locks nothing and the UPDATE then affects nothing — the
// pre-existing "silently skipped" behaviour is unchanged.
//
// ORDER BY id: see the header. It is here so two concurrent bulk writers over overlapping id sets
// take their locks in one direction.
func annotationLockTargets(ctx context.Context, tx pgx.Tx, where string, args ...any) error {
	if _, err := tx.Exec(ctx,
		`SELECT id FROM transactions
		 -- tombstone-exempt: this predicate must be IDENTICAL to the UPDATE it precedes, and the
		 -- UPDATE does not filter deleted_at (each statement documents why). Adding the filter here
		 -- would lock a SMALLER set than the write touches, leaving the tombstoned rows unlocked and
		 -- reintroducing exactly the race this lock exists to close, for precisely the rows whose
		 -- prior annotation is least recoverable (#1569 Task 6, review fix R79).
		 WHERE `+where+` ORDER BY id FOR UPDATE`, args...); err != nil {
		return fmt.Errorf("failed to lock annotation targets: %w", err)
	}
	return nil
}

// runCapturingUpdate wraps the lock and the capturing UPDATE in one transaction and returns the
// number of rows the UPDATE affected, read from its `updated` CTE rather than from the statement
// tag — the tag now counts the recording INSERT, which is legitimately 0 when nothing changed.
//
// The transaction is what makes the lock mean anything: row locks are held until commit, so the
// UPDATE's fresh snapshot cannot miss a write that landed while we were waiting.
func runCapturingUpdate(
	ctx context.Context,
	db DB,
	lockWhere string,
	lockArgs []any,
	updateSQL string,
	updateArgs []any,
) (int64, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to begin annotation capture transaction: %w", err)
	}
	// Rollback is a no-op once Commit has succeeded. context.Background(), not ctx: a cancelled
	// request context must still release the row locks this transaction is holding.
	defer func() { _ = tx.Rollback(context.Background()) }()

	if err := annotationLockTargets(ctx, tx, lockWhere, lockArgs...); err != nil {
		return 0, err
	}

	var updatedRows int64
	if err := tx.QueryRow(ctx, updateSQL, updateArgs...).Scan(&updatedRows); err != nil {
		return 0, err
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("failed to commit annotation capture transaction: %w", err)
	}
	return updatedRows, nil
}
