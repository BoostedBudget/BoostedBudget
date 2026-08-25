// Package annotationguard is NOT compiled: it lives under testdata, which the go tool ignores.
// It exists so TestAnnotationCaptureGuard_CatchesEveryConstructedViolation can construct the violation the
// real guard exists to catch and prove the guard fails on it, rather than asserting that the guard
// currently passes — which it would do just as happily if it had stopped looking at anything.
package annotationguard

import "context"

type db interface {
	Exec(ctx context.Context, sql string, args ...interface{}) (int64, error)
}

// UnguardedReviewWrite is the violation: a new write path, in a file that is not
// transaction_repository.go, overwriting a tracked annotation column with no capture.
func UnguardedReviewWrite(ctx context.Context, d db, userID string) error {
	query := `
		UPDATE transactions
		SET reviewed = false,
		    reviewed_at = NULL
		WHERE user_id = $1
	`
	_, err := d.Exec(ctx, query, userID)
	return err
}
