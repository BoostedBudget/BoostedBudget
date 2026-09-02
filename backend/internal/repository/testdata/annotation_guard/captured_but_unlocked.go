package annotationguard

import "context"

// CapturedButUnlockedWrite is review fix round 2's finding D-3, constructed. It carries the full
// capture CTE and issues it straight through Exec — no lock. The first version of the guard decided
// `captured` with a single strings.Contains and passed this, so a fourth write path could reopen the
// fabricated-transition and dropped-record failures with the suite green.
func CapturedButUnlockedWrite(ctx context.Context, d db, userID string) error {
	query := `
		WITH updated AS (
		UPDATE transactions t
		SET reviewed = false,
		    reviewed_at = NULL
		WHERE t.user_id = $1
		RETURNING t.id
		)` + annotationCaptureInsert("review", "updated")
	_, err := d.Exec(ctx, query, userID)
	return err
}
