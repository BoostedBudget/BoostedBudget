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
	// GetOldestExpiredCompGrantAge returns how long the OLDEST expired comp_* grant has been
	// sitting in RevenueCat's stored projection (revenuecat_customers.grants): zero when there are
	// none.
	//
	// This is the RevenueCat analogue of the retired legacy-column gauge (#1652 todo 4). The
	// legacy manual_tier_override column is no longer written by any production path — the admin
	// comp path records a revenuecat_operations intent instead — so a gauge fed by that column was
	// a dead alert. The live hazard is the RC projection: revenueCatSession.Save drops expired
	// grants before persisting, so a comp_* grant that has lapsed but is STILL in the stored
	// grants means the refresh path has stopped projecting. While it lingers, ResolveAccess refuses
	// the cached state (an expired grant requires refresh), so the inactive projection (retention
	// arming + plaid_cleanup_pending_at) is never persisted and the user is stuck out of the
	// retention pipeline. Healthy is ~0 and stays there; it climbs monotonically, in lockstep with
	// wall-clock time, the moment the refresh path stops.
	//
	// Only comp_* grants count: a paid_* or trial_* grant lapsing is a normal subscription event,
	// not this hazard.
	GetOldestExpiredCompGrantAge(ctx context.Context) (time.Duration, error)

	// GetOutstandingPlaidCleanupStats returns the number of users with outstanding Plaid-disconnect
	// cleanup, and how long the OLDEST such marker has been sitting uncleared — (0, 0) when there
	// are none.
	//
	// This is the direct measure of the Issue #1413 sibling hazard to
	// GetOldestExpiredCompGrantAge above:
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

	// GetDeletionResidueStats returns how many post-gate external-effect residue records exist, and
	// how long ago the NEWEST one was written — (0, 0) when there are none. Issue #1504.
	//
	// WHAT ONE RECORD MEANS, because nothing else about this method makes sense without it: a GDPR
	// deletion ISSUED an irreversible external call against the user's Paddle account — a
	// subscription cancel, or the customer archive-and-anonymise that nobody can undo — and the run
	// then ended with the user still here. The user keeps their account and every byte of their data,
	// and has had something done to them that they did not ask for. See
	// ProfileRepository.RecordDeletionResidue, which writes these rows, and
	// DeletionService.recordIssuedExternalEffect, which calls it from each irreversible call site
	// BEFORE the call goes out.
	//
	// ⚠ THE COUNT CAN GO DOWN, since round 15. DeleteUserCascade retracts a user's records inside the
	// transaction that deletes them, so a run that later completes closes its own work items. This
	// method is therefore a count of OPEN residue, not an all-time total; see the gauge's own comment
	// in observability/metrics.go for what that means for the alert rule.
	//
	// Reads audit_logs through deletionResidueActionPrefix — the SAME Go constant the writer builds
	// its action from, never a second hand-typed literal. That constant exists precisely so the
	// operator query cannot drift away from what is written, and a gauge that drifted here would
	// report zero forever while records accumulated: the exact "state changed, nobody can tell"
	// failure this record was created to end.
	//
	// ⚠ THE AGE IS THE NEWEST RECORD'S, NOT THE OLDEST'S — the deliberate inversion of
	// GetOutstandingPlaidCleanupStats above, and the reason is structural rather than stylistic.
	// That gauge measures a backlog whose rows get CLEARED, so its oldest entry is the chronic-failure
	// signal. Nothing clears a residue record: there is no resolution column, nothing prunes
	// audit_logs, and the harm is not reversible by any automated process. MIN(created_at) here would
	// therefore climb monotonically with wall-clock time forever after the first occurrence and tell
	// an operator nothing at all. MAX(created_at) answers the question that has an action attached:
	// "has this happened again recently?"
	//
	// ⚠ AND THE AGE IS ONLY MEANINGFUL ALONGSIDE THE COUNT. Zero is returned both when there are no
	// records and when one was written this instant; count is the disambiguator, and every consumer
	// must read it first. This is a real difference from the two backlog gauges above, where a zero
	// age is unambiguous good news.
	//
	// ⚠ DELIBERATELY NOT REMEDIATION, AND DELIBERATELY NOT A WINDOW. This method reports; it never
	// re-subscribes anybody (Issue #1504 is explicit that automatic re-subscription is a worse failure
	// than telling a human). And the count is all-time rather than windowed: a residue record nobody
	// acted on must not become invisible by ageing out. The consequence is honest and intended — once
	// this has ever happened, the count stays non-zero, because no record anywhere proves the affected
	// user was made whole. Claiming otherwise would be the claim-versus-proof conflation this branch
	// spent ten rounds removing.
	//
	// Bounded by idx_audit_logs_deletion_residue (migration 000138), whose predicate is the same
	// literal LIKE clause this query uses. audit_logs is append-only and unbounded, and
	// idx_audit_logs_action cannot serve a LIKE prefix under this database's collation — see that
	// migration for the measurement, and
	// TestJobHealthRepository_Integration_DeletionResidue_IsServedByThePartialIndex for the guard.
	GetDeletionResidueStats(ctx context.Context) (count int, newestAge time.Duration, err error)
}

type jobHealthRepository struct {
	db DB
}

// NewJobHealthRepository creates a new JobHealthRepository instance.
func NewJobHealthRepository(db DB) JobHealthRepository {
	return &jobHealthRepository{db: db}
}

// oldestExpiredCompGrantAgeQuery is GetOldestExpiredCompGrantAge's SQL, hoisted to a package-level
// constant so a plain unit test can assert its shape without a database.
//
// The key prefix is matched with LIKE 'comp\_%' (the backslash escapes the underscore, which is a
// single-character wildcard in LIKE) so only comp_* grants count.
const oldestExpiredCompGrantAgeQuery = `
	SELECT COALESCE(EXTRACT(EPOCH FROM (NOW() - MIN((g->>'expires_at')::timestamptz))), 0)
	FROM revenuecat_customers rc
	CROSS JOIN LATERAL jsonb_array_elements(rc.grants) AS g
	WHERE g->>'key' LIKE 'comp\_%'
	  AND g->>'expires_at' IS NOT NULL
	  AND (g->>'expires_at')::timestamptz < NOW()
`

func (r *jobHealthRepository) GetOldestExpiredCompGrantAge(ctx context.Context) (time.Duration, error) {
	var ageSeconds float64
	if err := r.db.QueryRow(ctx, oldestExpiredCompGrantAgeQuery).Scan(&ageSeconds); err != nil {
		return 0, fmt.Errorf("failed to query oldest expired comp grant age: %w", err)
	}
	if ageSeconds < 0 {
		// Defensive only: the `expires_at < NOW()` clause means every grant this aggregate
		// considers is already in the past, so NOW() - MIN(...) should never go negative — but
		// never let a clock-skew edge case report a negative "age".
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
		// Defensive only, mirroring GetOldestExpiredCompGrantAge's clamp above —
		// the `plaid_cleanup_pending_at IS NOT NULL` clause means every row this aggregate
		// considers already has a real timestamp in the past, so NOW() - MIN(...) should never go
		// negative, but never let a clock-skew edge case report a negative "age".
		ageSeconds = 0
	}
	return int(count), time.Duration(ageSeconds * float64(time.Second)), nil
}

// deletionResidueStatsQuery is GetDeletionResidueStats' SQL, hoisted to a package-level constant for
// two reasons beyond readability.
//
// First, the LIKE pattern is INLINED from deletionResidueActionPrefix rather than bound as $1, and that
// is load-bearing, not a shortcut: Postgres decides whether a partial index applies by proving the
// query's predicate implies the index's, and a bind parameter has no value at plan time, so
// parameterising this silently reverts the query to a sequential scan of an append-only table. There is
// no injection surface — the prefix is a compile-time Go constant with no caller input in it.
//
// Second, hoisting it lets a plain unit test read the SQL and assert both of those properties without a
// database, so the drift guard cannot be skipped along with the integration suite. See
// TestJobHealthRepository_DeletionResidueStatsQuery_UsesTheWriterSharedPrefix.
const deletionResidueStatsQuery = `
	SELECT COUNT(*), COALESCE(EXTRACT(EPOCH FROM (NOW() - MAX(created_at))), 0)
	FROM audit_logs
	WHERE action LIKE '` + deletionResidueActionPrefix + `%'
`

// GetDeletionResidueStats implements the interface doc comment above — see it for what a residue record
// means, why the age is the NEWEST record's rather than the oldest's, and why the count is all-time
// rather than windowed. One round trip for both numbers, matching the two gauges above.
func (r *jobHealthRepository) GetDeletionResidueStats(ctx context.Context) (int, time.Duration, error) {
	var count int64
	var ageSeconds float64
	if err := r.db.QueryRow(ctx, deletionResidueStatsQuery).Scan(&count, &ageSeconds); err != nil {
		return 0, 0, fmt.Errorf("failed to query deletion-residue stats: %w", err)
	}
	if ageSeconds < 0 {
		// Defensive only, mirroring the two clamps above: every row this aggregate considers was
		// INSERTed with a DEFAULT NOW() already in the past, so NOW() - MAX(...) should never go
		// negative — but a clock-skew edge case must not be allowed to report a negative "age", which a
		// consumer would render as a nonsense duration on an operator dashboard.
		ageSeconds = 0
	}
	return int(count), time.Duration(ageSeconds * float64(time.Second)), nil
}
