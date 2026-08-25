package repository

import "fmt"

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
// ⚠ THE ONE RESIDUAL WINDOW, stated rather than hidden. Under READ COMMITTED, if a concurrent
// transaction commits an update to the same row between our snapshot and our row lock, Postgres
// re-evaluates the UPDATE against the new tuple (EvalPlanQual) but the `prior` scan keeps the value
// from our snapshot. The recorded prior would then be the value from before that concurrent write,
// not the one immediately before ours. Closing that completely needs a row trigger, which would
// also fire for every sync and rule write and is a much larger change than #1579 asks for. This is
// strictly better than SELECT-then-UPDATE, whose window is unbounded and spans a network round
// trip; it is not zero.
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

// annotationCaptureReturning is the RETURNING list every capturing UPDATE must expose. `prior.*` is
// the pre-update tuple from the self-join; the unqualified-target columns are the post-update ones.
const annotationCaptureReturning = `
			t.id      AS captured_transaction_id,
			t.user_id AS captured_user_id,
			prior.category_override_kind      AS prior_kind,
			prior.pfc_primary_override        AS prior_pfc_primary,
			prior.pfc_detailed_override       AS prior_pfc_detailed,
			prior.custom_category_override_id AS prior_custom_id,
			prior.effective_category_name     AS prior_effective_name,
			prior.category_source             AS prior_source,
			prior.reviewed                    AS prior_reviewed,
			prior.reviewed_at                 AS prior_reviewed_at,
			t.category_override_kind          AS new_kind,
			t.pfc_primary_override            AS new_pfc_primary,
			t.pfc_detailed_override           AS new_pfc_detailed,
			t.custom_category_override_id     AS new_custom_id,
			t.effective_category_name         AS new_effective_name,
			t.category_source                 AS new_source,
			t.reviewed                        AS new_reviewed,
			t.reviewed_at                     AS new_reviewed_at`

// annotationCaptureInsert returns the recording CTE plus the count projection that replaces the
// UPDATE's own RowsAffected. changeSource is a compile-time constant from this package, never user
// input — it is interpolated because a CTE body cannot take a parameter placeholder in a position
// the caller's own $n numbering does not control, and the two call-site values are pinned by
// TestAnnotationCaptureSourcesMatchTheCheckConstraint.
//
// The `recorded` CTE is never referenced by the final SELECT. That is correct and not a bug: a
// data-modifying statement inside WITH is executed exactly once, to completion, whether or not the
// primary query reads its output.
func annotationCaptureInsert(changeSource string) string {
	return fmt.Sprintf(`
		, recorded AS (
			INSERT INTO transaction_annotation_history (
				user_id, transaction_id, change_source,
				prior_category_override_kind, prior_pfc_primary_override, prior_pfc_detailed_override,
				prior_custom_category_override_id, prior_effective_category_name, prior_category_source,
				prior_reviewed, prior_reviewed_at,
				new_category_override_kind, new_pfc_primary_override, new_pfc_detailed_override,
				new_custom_category_override_id, new_effective_category_name, new_category_source,
				new_reviewed, new_reviewed_at
			)
			SELECT
				captured_user_id, captured_transaction_id, '%s',
				prior_kind, prior_pfc_primary, prior_pfc_detailed, prior_custom_id,
				prior_effective_name, prior_source, prior_reviewed, prior_reviewed_at,
				new_kind, new_pfc_primary, new_pfc_detailed, new_custom_id,
				new_effective_name, new_source, new_reviewed, new_reviewed_at
			FROM updated
			WHERE (prior_kind, prior_pfc_primary, prior_pfc_detailed, prior_custom_id,
			       prior_effective_name, prior_source, prior_reviewed, prior_reviewed_at)
			      IS DISTINCT FROM
			      (new_kind, new_pfc_primary, new_pfc_detailed, new_custom_id,
			       new_effective_name, new_source, new_reviewed, new_reviewed_at)
		)
		SELECT COUNT(*) FROM updated`, changeSource)
}
