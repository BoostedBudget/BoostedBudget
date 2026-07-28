package repository

import (
	"context"
	"fmt"
	"time"
)

// JobHealthRepository backs periodic-job health metrics — signals that let an operator detect a
// periodic job has silently stopped running, distinct from a single logged/counted failure (see
// internal/observability.RecordJobSuccess, the complementary generic last-success heartbeat every
// periodic worker's Work() reports to on its success path).
type JobHealthRepository interface {
	// GetOldestUnclearedExpiredManualTierOverrideAge returns how long the OLDEST currently
	// expired-but-uncleared manual_tier_override has been sitting uncleared: zero when there are
	// none.
	//
	// This is the direct measure of the Issue #1413 Critical finding C1 hazard:
	// noManualOverrideAtAllPredicate (profile_repository.go) excludes a user from the retention-
	// notice/hard-delete queries for as long as ANY manual_tier_override survives on their row —
	// live or lapsed — and clearing a LAPSED one is exclusively manual_tier_override_expiry_check's
	// job. Healthy is ~0 and stays there; it climbs monotonically, in lockstep with wall-clock
	// time, the moment that job stops actually clearing overrides — whether it's crashing,
	// discarded into the dead-letter queue, or simply not scheduled anymore.
	//
	// Uses the exact same expiredManualTierOverridePredicate (profile_repository.go) that
	// GetUsersWithExpiredManualTierOverride's own candidate query uses, so this gauge and that job
	// can never disagree about which rows count.
	GetOldestUnclearedExpiredManualTierOverrideAge(ctx context.Context) (time.Duration, error)
}

type jobHealthRepository struct {
	db DB
}

// NewJobHealthRepository creates a new JobHealthRepository instance.
func NewJobHealthRepository(db DB) JobHealthRepository {
	return &jobHealthRepository{db: db}
}

func (r *jobHealthRepository) GetOldestUnclearedExpiredManualTierOverrideAge(ctx context.Context) (time.Duration, error) {
	query := `
		SELECT COALESCE(EXTRACT(EPOCH FROM (NOW() - MIN(manual_tier_ends_at))), 0)
		FROM users
		WHERE ` + expiredManualTierOverridePredicate

	var ageSeconds float64
	if err := r.db.QueryRow(ctx, query).Scan(&ageSeconds); err != nil {
		return 0, fmt.Errorf("failed to query oldest uncleared expired manual tier override age: %w", err)
	}
	if ageSeconds < 0 {
		// Defensive only: expiredManualTierOverridePredicate's manual_tier_ends_at < NOW() clause
		// means every row this aggregate considers is already in the past, so NOW() - MIN(...)
		// should never go negative — but never let a clock-skew edge case report a negative "age".
		ageSeconds = 0
	}
	return time.Duration(ageSeconds * float64(time.Second)), nil
}
