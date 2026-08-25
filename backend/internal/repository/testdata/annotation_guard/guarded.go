package annotationguard

import "context"

func annotationCaptureInsert(source, cte string) string { return source + cte }

func runCapturingUpdate(ctx context.Context, d db, sql string, args ...interface{}) error {
	_, err := d.Exec(ctx, sql, args...)
	return err
}

// GuardedReviewWrite is the compliant control: it carries the capture AND goes through the
// lock-first helper. Without it the meta-test could not tell "the guard flags the violations" from
// "the guard flags everything".
func GuardedReviewWrite(ctx context.Context, d db, userID string) error {
	query := `
		WITH updated AS (
		UPDATE transactions t
		SET reviewed = false,
		    reviewed_at = NULL
		WHERE t.user_id = $1
		RETURNING t.id
		)` + annotationCaptureInsert("review", "updated")
	return runCapturingUpdate(ctx, d, query, userID)
}
