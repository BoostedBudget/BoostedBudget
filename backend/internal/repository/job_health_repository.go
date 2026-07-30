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

	// GetOutstandingPlaidCleanupStats returns the number of users with outstanding Plaid-disconnect
	// cleanup, and how long the OLDEST such marker has been sitting uncleared — (0, 0) when there
	// are none.
	//
	// This is the direct measure of the Issue #1413 sibling hazard to
	// GetOldestUnclearedExpiredManualTierOverrideAge above:
	// RetentionCleanupCheckWorker.repairPlaidDisconnects (queue/jobs/retention_cleanup_check.go) is
	// the ONLY place a lapsed user's Plaid bank connections are ever disconnected, and its per-user
	// error isolation (deliberate — one user's Plaid failure must never block the GDPR notice/
	// deletion steps the same run performs for everyone else) means a stuck user is silently
	// re-selected on every subsequent run while the job's own success heartbeat keeps advancing.
	//
	// CRITICAL — "plaid_cleanup_pending_at IS NOT NULL" alone is NOT a valid proxy for "cleanup
	// outstanding": migration 000119 stamped that marker on EVERY inactive/cancelled user
	// regardless of whether they had ever linked a Plaid item at all, and 000120 re-stamped those
	// residue rows to its own NOW(). repairPlaidDisconnects's own candidate query
	// (GetUsersNeedingPlaidDisconnectRepair, profile_repository.go) never reaches those rows — its
	// live-linked-item EXISTS clause filters them out before the marker is ever cleared. So this
	// query joins on the EXACT SAME liveness definition that candidate query uses (a NULL
	// linked_items.status counts as live — see that query's own doc comment for why a bare
	// `status NOT IN (...)` would silently exclude such a row via SQL's three-valued logic), or it
	// would report a large PERMANENT phantom backlog on day one and be ignored forever.
	//
	// Deliberately does NOT apply plaidDisconnectRepairGracePeriod's cutoff: this gauge measures
	// the AGE of the backlog itself (so an operator can alert on "the oldest has been outstanding
	// far longer than one grace window", the actual chronic-failure signal), not which users are
	// due for repair RIGHT NOW — that eligibility decision stays exclusively
	// GetUsersNeedingPlaidDisconnectRepair's.
	//
	// Deliberately does NOT apply noActiveManualOverridePredicate, unlike
	// GetUsersNeedingPlaidDisconnectRepair (Task X / Codex review round on PR #1443, M3): a user
	// under a currently-live manual tier override genuinely has outstanding Plaid cleanup — the
	// comp only exempts them from being ACTED ON this run, via the repair query's own predicate, not
	// from the cleanup existing at all. A comp can live for months; a gauge whose whole purpose is
	// surfacing a stuck backlog must not go blind to a comped user's backlog for that entire window.
	// This is a deliberate, permanent divergence between this gauge and the repair query's row set —
	// see GetUsersNeedingPlaidDisconnectRepair's own doc comment for why ITS exclusion is correct.
	// The two are NOT required to agree on which rows count; they answer different questions ("how
	// much is outstanding" vs. "whom do I act on this run"), and this predicate difference is
	// exactly that divergence, not a bug. See
	// TestJobHealthRepository_Integration_MarkedUserWithLiveItemAndLiveComp_StillCounted
	// (job_health_repository_plaid_cleanup_integration_test.go).
	GetOutstandingPlaidCleanupStats(ctx context.Context) (count int, oldestAge time.Duration, err error)
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

// GetOutstandingPlaidCleanupStats implements the interface doc comment's query: count of users
// with plaid_cleanup_pending_at set AND at least one live linked item, plus the age of the OLDEST
// such marker. Reuses plaidDisconnectRepairEligibleStatusPredicate and liveLinkedItemExistsClause
// (profile_repository.go) — the SAME Go constants GetUsersNeedingPlaidDisconnectRepair's own
// candidate query uses, not a second hand-copied SQL literal, so this query and that one can never
// silently drift apart on eligible status or item liveness. A second, independently-worded copy of
// liveLinkedItemExistsClause is exactly the failure class that constant's own doc comment warns
// against (Issue #1413 round-14 finding M1) — see
// TestJobHealthRepository_Integration_MarkedUserWithNullStatusLiveItem_Counted
// (job_health_repository_plaid_cleanup_integration_test.go) for the regression test pinning this
// query to the shared constant's NULL-status-is-live rule specifically.
//
// This query deliberately does NOT also reuse noActiveManualOverridePredicate — see the interface
// doc comment above for why this gauge and that repair pass are intentionally allowed to disagree
// on that one axis: a comped user still has outstanding cleanup; the comp only defers WHEN it gets
// acted on, not WHETHER it counts here. One round trip for both numbers.
func (r *jobHealthRepository) GetOutstandingPlaidCleanupStats(ctx context.Context) (int, time.Duration, error) {
	query := `
		SELECT COUNT(*), COALESCE(EXTRACT(EPOCH FROM (NOW() - MIN(plaid_cleanup_pending_at))), 0)
		FROM users
		WHERE ` + plaidDisconnectRepairEligibleStatusPredicate + `
		  AND is_deleted = FALSE
		  AND deletion_requested_at IS NULL
		  AND plaid_cleanup_pending_at IS NOT NULL
		  AND ` + liveLinkedItemExistsClause + `
	`

	var count int64
	var ageSeconds float64
	if err := r.db.QueryRow(ctx, query).Scan(&count, &ageSeconds); err != nil {
		return 0, 0, fmt.Errorf("failed to query outstanding Plaid cleanup stats: %w", err)
	}
	if ageSeconds < 0 {
		// Defensive only, mirroring GetOldestUnclearedExpiredManualTierOverrideAge's clamp above —
		// the `plaid_cleanup_pending_at IS NOT NULL` clause means every row this aggregate
		// considers already has a real timestamp in the past, so NOW() - MIN(...) should never go
		// negative, but never let a clock-skew edge case report a negative "age".
		ageSeconds = 0
	}
	return int(count), time.Duration(ageSeconds * float64(time.Second)), nil
}
