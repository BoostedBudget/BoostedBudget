// Package pgtime normalizes Go timestamp fixtures to pgx/PostgreSQL storage precision.
package pgtime

import "time"

// Stored returns the timestamp pgx encodes for a finite Go fixture: UTC at microsecond
// precision, truncated (not rounded), with no monotonic clock metadata. PostgreSQL cannot
// preserve the remaining nanoseconds. Normalize the expected fixture only; compare an actual
// database value's UTC() result exactly, so even a one-microsecond mutation remains a failure.
func Stored(value time.Time) time.Time {
	return value.UTC().Truncate(time.Microsecond)
}
