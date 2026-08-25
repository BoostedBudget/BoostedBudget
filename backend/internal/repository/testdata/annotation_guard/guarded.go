package annotationguard

import "context"

func annotationCaptureInsert(source string) string { return source }

// GuardedReviewWrite carries the capture, so the guard must NOT report it. Without this file the
// meta-test could not tell "the guard flags the violation" from "the guard flags everything".
func GuardedReviewWrite(ctx context.Context, d db, userID string) error {
	query := `
		WITH updated AS (
		UPDATE transactions t
		SET reviewed = false,
		    reviewed_at = NULL
		WHERE t.user_id = $1
		RETURNING t.id
		)` + annotationCaptureInsert("review")
	_, err := d.Exec(ctx, query, userID)
	return err
}
