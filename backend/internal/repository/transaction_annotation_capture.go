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
// THE FIX IS LOCK-FIRST, and it is NOT the SELECT-then-UPDATE the plan rejected. Each of the four
// statements now runs as:
//
//	BEGIN;
//	SELECT id FROM transactions WHERE <the write's own predicate> ORDER BY id FOR UPDATE;
//	<the capturing UPDATE>;
//	COMMIT;
//
// The four are UpdateCategory, BulkUpdateCategory, SetReviewed and TombstoneByExternalID's
// settlement move; the last joined them in fix round 2, when the allowlist reason excusing it
// ("fill-only in every column") was re-derived from the SQL and found false.
//
// The difference from the rejected shape is the LOCK. A plain read-then-write holds nothing, so its
// window is unbounded. Here the target rows are locked before the capture, and under READ COMMITTED
// the UPDATE — a separate statement — takes a FRESH snapshot that already contains anything that
// committed while we waited for the lock. Nothing can commit against those rows between the two
// statements, so `prior` is exact rather than merely recent, and both halves of every row come from
// one snapshot.
//
// ORDER BY id is deadlock hygiene. WHAT IT ACTUALLY BUYS, scoped precisely rather than hedged: the
// measured plan puts LockRows ABOVE Sort, so the bulk lock really is taken in id order, and two
// concurrent CAPTURING statements over overlapping id sets therefore cannot invert against each
// other. That covers all four capturing statements, TombstoneByExternalID included, since its lock
// carries the same ORDER BY.
//
// ⚠ WHAT IT DOES NOT COVER is every OTHER statement that locks transactions rows. `Update` and
// `CreateBatch` take their row locks in whatever order their plan produces, so a capturing
// statement can still invert against one of those. Ordering those too would mean a lock discipline
// across the whole repository, which is a larger decision than this task; the residual is
// cross-statement, not capture-versus-capture.
//
// The cost is one extra round trip and an explicit transaction on the four capturing methods.
// r.db.Begin works whether r.db is a pool or an existing pgx.Tx (pgx opens a savepoint for the
// nested case), so a caller that already has a transaction is unaffected.
//
// MEASURED, on 200,000 transactions in the local Postgres 17 with fresh ANALYZE stats — not on an
// empty database, because an EXPLAIN on an empty database is worthless and this plan already
// shipped one headline number that was 5.4x backwards.
//
// ⚠ NO TIMINGS ARE QUOTED HERE, DELIBERATELY. An earlier version of this comment carried a
// millisecond column and a "2.1x" ratio from one run on one machine. Review re-ran the same probe
// and measured a 4.87 ms baseline against my 1.777 ms — same plans, same buffers, ratio 1.1x
// instead of 2.1x. The wall-clock numbers are not a property of this code, and a second bogus
// headline timing on this plan would be worse than none. What DOES reproduce, and is the part the
// question was about, is the plan shape and the buffer counts:
//
//	                              plan                                        buffers
//	bulk lock (50 ids)            LockRows -> Sort -> Bitmap Heap Scan          250
//	                              (Bitmap Index Scan on transactions_pkey)
//	bulk capturing UPDATE         Nested Loop: Bitmap Heap Scan on t           3343
//	                              x Index Scan on prior (id = t.id)
//	bulk baseline (pre-#1569,     Bitmap Heap Scan, no join, no capture        3168
//	  no self-join, no capture)
//	single lock                   LockRows -> Index Scan on pkey                  5
//	single capturing UPDATE       Nested Loop of two pkey Index Scans             66
//
// THE QUESTION THIS ANSWERS is whether `prior.id = t.id` degenerates into a seq scan on
// transactions for the `id = ANY(...)` bulk form. It does not: the inner side is an Index Scan on
// transactions_pkey with Index Cond `(id = t.id)` in every plan, and NO plan contains a Seq Scan.
// Independently reproduced by review, which measured the self-join delta at 175-260 buffers against
// my 175 (3168 -> 3343) on a 50-id batch. The rest of the delta is the history INSERT and its
// user_id FK trigger, which is the feature, not the join.
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
// TOMBSTONES. None of the capturing statements filters `deleted_at IS NULL` on its target (each carries its own
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

// annotationCaptureReturning is the RETURNING list a capturing UPDATE must expose. `prior.*` is the
// pre-update tuple from the self-join; the target alias's own columns are the post-update ones.
//
// The alias is a parameter because the four capturing statements do not agree on one: the three
// user-facing ones update `transactions t`, while TombstoneByExternalID's settlement move updates
// `transactions AS twin` and cannot be renamed without rewriting #1578's whole CTE.
func annotationCaptureReturning(targetAlias string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n\t\t\t%[1]s.id      AS captured_transaction_id,\n\t\t\t%[1]s.user_id AS captured_user_id", targetAlias)
	for _, col := range annotationCapturedColumns {
		fmt.Fprintf(&b, ",\n\t\t\tprior.%s AS prior_%s", col, col)
	}
	for _, col := range annotationCapturedColumns {
		fmt.Fprintf(&b, ",\n\t\t\t%s.%s AS new_%s", targetAlias, col, col)
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

// annotationCaptureInsert returns the recording CTE: an INSERT that reads the capturing statement's
// RETURNING output out of the named CTE and writes one history row per row that actually changed.
//
// It returns ONLY the CTE. The caller appends its own final SELECT, because the four callers do not
// want the same one — the three user-facing statements project the affected-row count, while
// TombstoneByExternalID projects the deleted_reason its signature returns.
//
// changeSource and sourceCTE are compile-time constants from this package, never user input. They
// are interpolated because a CTE body cannot take a parameter placeholder in a position the
// caller's own $n numbering does not control, and the three change_source values are pinned against
// the live CHECK by TestAnnotationCaptureSourcesMatchTheCheckConstraint.
//
// ⚠ The result must contain no `%` verb: two of the callers splice it into a fmt.Sprintf format
// string. Pinned by TestAnnotationCaptureFragmentsCarryNoFormatVerb.
func annotationCaptureInsert(changeSource, sourceCTE string) string {
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
			FROM %[4]s
			WHERE (%[2]s) IS DISTINCT FROM (%[3]s)
		)`, changeSource, priorCols, newCols, sourceCTE)
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
		 -- tombstone-exempt(transactions): this predicate must be IDENTICAL to the UPDATE it precedes, and the
		 -- UPDATE does not filter deleted_at (each statement documents why). Adding the filter here
		 -- would lock a SMALLER set than the write touches, leaving the tombstoned rows unlocked and
		 -- reintroducing exactly the race this lock exists to close, for precisely the rows whose
		 -- prior annotation is least recoverable (#1569 Task 6, review fix R79).
		 WHERE `+where+` ORDER BY id FOR UPDATE`, args...); err != nil {
		return fmt.Errorf("failed to lock annotation targets: %w", err)
	}
	return nil
}

// runCapturingStatement wraps the row lock and a capturing statement in one transaction, then hands
// the statement's single result row to scan.
//
// The transaction is what makes the lock mean anything: row locks are held until commit, so the
// capturing statement's fresh snapshot cannot miss a write that landed while we were waiting for
// the lock. Every statement that carries the capture must come through here — a capture without the
// lock has the two failure modes documented at the top of this file, and
// TestAnnotationCaptureIsUsedByEveryAnnotationWritingStatement enforces the pairing.
func runCapturingStatement(
	ctx context.Context,
	db DB,
	lockWhere string,
	lockArgs []any,
	sql string,
	args []any,
	scan func(pgx.Row) error,
) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin annotation capture transaction: %w", err)
	}
	// Rollback is a no-op once Commit has succeeded. context.Background(), not ctx: a cancelled
	// request context must still release the row locks this transaction is holding.
	defer func() { _ = tx.Rollback(context.Background()) }()

	if err := annotationLockTargets(ctx, tx, lockWhere, lockArgs...); err != nil {
		return err
	}
	if err := scan(tx.QueryRow(ctx, sql, args...)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit annotation capture transaction: %w", err)
	}
	return nil
}

// runCapturingUpdate is runCapturingStatement for the three user-facing statements, whose result
// row is the number of rows the UPDATE affected.
//
// That count is read from the statement's own `updated` CTE, never from the statement tag: the tag
// now reports the recording INSERT, which is legitimately 0 when nothing changed.
func runCapturingUpdate(
	ctx context.Context,
	db DB,
	lockWhere string,
	lockArgs []any,
	updateSQL string,
	updateArgs []any,
) (int64, error) {
	var updatedRows int64
	err := runCapturingStatement(ctx, db, lockWhere, lockArgs, updateSQL, updateArgs,
		func(row pgx.Row) error { return row.Scan(&updatedRows) })
	if err != nil {
		return 0, err
	}
	return updatedRows, nil
}
