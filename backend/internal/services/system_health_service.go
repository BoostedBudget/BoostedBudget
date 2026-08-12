package services

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// SystemHealthRepository defines methods for retrieving system health metrics
type SystemHealthRepository interface {
	GetRiverJobStats(ctx context.Context, queue string) (*domain.QueueMetrics, error)
	GetRiverJobsCompletedLastHour(ctx context.Context, queue string) (int64, error)
	GetActiveSyncCount(ctx context.Context) (int64, error)
	GetFailedSyncsLastHour(ctx context.Context) (int64, error)
	GetAverageSyncDuration(ctx context.Context) (int64, error)
	GetActiveSubscriptionCount(ctx context.Context) (int64, error)
	GetPaddleWebhookStats24h(ctx context.Context) (int64, int64, error)
	GetLastReconciliationRun(ctx context.Context) (*time.Time, error)
}

// DeadLetterJobRepository defines methods for dead letter job operations
type DeadLetterJobRepository interface {
	GetStats(ctx context.Context) (*repository.DeadLetterStats, error)
	GetOldestJobAge(ctx context.Context) (int64, error)
}

// DatabasePool defines minimal interface for database connection pool
type DatabasePool interface {
	Ping(ctx context.Context) error
	GetStats() PoolStats
}

// PoolStats represents connection pool statistics
type PoolStats struct {
	AcquiredConns     int32
	IdleConns         int32
	MaxConns          int32
	ConstructingConns int32
	TotalConns        int32
}

// SystemHealthService provides system health monitoring
type SystemHealthService interface {
	GetSystemHealth(ctx context.Context) (*domain.SystemHealthResponse, error)
}

type systemHealthService struct {
	healthRepo     SystemHealthRepository
	deadLetterRepo DeadLetterJobRepository
	pool           DatabasePool
	plaidEnv       string
	// jobHealthRepo backs the three DB-derived Plaid-cleanup/manual-tier-override backlog gauges
	// (Issue #1458) and, since Issue #1504, the deletion-residue count behind
	// SystemHealthResponse.DeletionResidue — see repository.JobHealthRepository's own doc comment for
	// why these queries are never duplicated here. Optional/nil-safe: an unwired jobHealthRepo (e.g. an
	// older test construction of this struct) degrades to zero-value JobBacklogMetrics and an
	// UNAVAILABLE (never zero) DeletionResidueMetrics rather than panicking.
	jobHealthRepo repository.JobHealthRepository
	// plaidOrphanRepo backs SystemHealthResponse.PlaidOrphanedItems (Issue #1497 item 5).
	// Optional/nil-safe: an unwired repo reports "unavailable", never a confident zero — see
	// domain.PlaidOrphanedItemMetrics.Available.
	plaidOrphanRepo PlaidOrphanedItemLister
	// invalidWebhookAttemptRepo backs SystemHealthResponse.RejectedWebhookAttempts24h (Issue
	// #1514 requirement #5). Optional/nil-safe: an unwired repo reports "unavailable", never a
	// confident zero — same convention as plaidOrphanRepo above.
	invalidWebhookAttemptRepo InvalidWebhookAttemptStatsReader
}

// PlaidOrphanedItemLister is the ONE verb the health derivation needs from
// repository.PlaidOrphanedItemRepository. Issue #1497 item 5.
//
// ⚠ NARROW ON PURPOSE, for the same reason services.orphanRetryRepo excludes PurgeExpiredTokens:
// /admin/health is a read surface, and giving it MarkResolved or PurgeExpiredTokens would make
// "an admin page destroyed a credential" a bug that is merely absent rather than one that cannot
// compile. List is also the read that deliberately FILTERS NOTHING — resolved and purged rows both
// appear — which is exactly what a derivation over all three AdminStates requires.
type PlaidOrphanedItemLister interface {
	List(ctx context.Context, limit, offset int) ([]domain.PlaidOrphanedItem, int, error)
}

// InvalidWebhookAttemptStatsReader narrows repository.InvalidWebhookAttemptRepository to its read
// method alone, for the same reason PlaidOrphanedItemLister above narrows to List: a read-only
// health surface must not gain the ability to insert or trim invalid-attempt rows just because it
// needs to read a count. Issue #1514 requirement #5.
type InvalidWebhookAttemptStatsReader interface {
	GetStats24h(ctx context.Context) (byProvider map[string]int64, mostRecentAt *time.Time, err error)
}

// NewSystemHealthService creates a new system health service
func NewSystemHealthService(
	healthRepo SystemHealthRepository,
	deadLetterRepo DeadLetterJobRepository,
	pool DatabasePool,
	plaidEnv string,
	jobHealthRepo repository.JobHealthRepository,
	plaidOrphanRepo PlaidOrphanedItemLister,
	invalidWebhookAttemptRepo InvalidWebhookAttemptStatsReader,
) SystemHealthService {
	return &systemHealthService{
		healthRepo:                healthRepo,
		deadLetterRepo:            deadLetterRepo,
		pool:                      pool,
		plaidEnv:                  plaidEnv,
		jobHealthRepo:             jobHealthRepo,
		plaidOrphanRepo:           plaidOrphanRepo,
		invalidWebhookAttemptRepo: invalidWebhookAttemptRepo,
	}
}

// GetSystemHealth retrieves comprehensive system health metrics
func (s *systemHealthService) GetSystemHealth(ctx context.Context) (*domain.SystemHealthResponse, error) {
	response := &domain.SystemHealthResponse{
		Timestamp: time.Now(),
		JobQueues: make(map[string]domain.QueueMetrics),
	}

	// Check all services
	response.Services = domain.ServiceHealthStatuses{
		Database: s.checkDatabase(ctx),
		Clerk:    s.checkClerk(ctx),
		Plaid:    s.checkPlaid(ctx),
	}

	// Get queue metrics for all queues
	queues := []string{"critical", "default", "notifications", "low"}
	for _, queue := range queues {
		metrics, err := s.getQueueMetrics(ctx, queue)
		if err != nil {
			// Log error but continue - partial data is better than none
			response.JobQueues[queue] = domain.QueueMetrics{}
			continue
		}
		response.JobQueues[queue] = *metrics
	}

	// Get sync status
	syncMetrics, err := s.getSyncMetrics(ctx)
	if err != nil {
		response.SyncStatus = domain.SyncStatusMetrics{}
	} else {
		response.SyncStatus = *syncMetrics
	}

	// Get dead letter metrics
	deadLetterMetrics, err := s.getDeadLetterMetrics(ctx)
	if err != nil {
		response.DeadLetterQueue = domain.DeadLetterMetrics{}
	} else {
		response.DeadLetterQueue = *deadLetterMetrics
	}

	// Get Paddle metrics
	paddleMetrics, err := s.getPaddleMetrics(ctx)
	if err != nil {
		response.PaddleStatus = domain.PaddleStatusMetrics{}
	} else {
		response.PaddleStatus = *paddleMetrics
	}

	// Get job backlog metrics (Issue #1458)
	jobBacklogMetrics, err := s.getJobBacklogMetrics(ctx)
	if err != nil {
		// Issue #1458 follow-up G16: this zero-valued struct leaves Available at its zero value
		// (false) — the query failed, so there is no verified backlog to report, not a healthy
		// zero. Scope discipline: this deliberately does NOT feed into calculateOverallStatus
		// below — whether a non-zero (or now-unavailable) backlog should degrade overall health
		// is a product decision #1458 never asked for; only making the failure VISIBLE is in
		// scope here.
		response.JobBacklog = domain.JobBacklogMetrics{}
	} else {
		response.JobBacklog = *jobBacklogMetrics
	}

	// Orphaned Plaid Items (Issue #1497 item 5). response.Timestamp is reused as the derivation
	// instant rather than a second time.Now(): two of AdminState's four checks are time-dependent, so
	// a second clock read would let the counts describe an instant the response does not claim.
	//
	// Same "partial data over none" convention as every gauge above — a failure leaves the zero
	// struct, whose Available is false, so an operator sees "Unavailable" rather than a green zero
	// asserting that no erased user's Plaid Item is still billing. Also deliberately NOT fed into
	// calculateOverallStatus: whether outstanding orphans should degrade overall health is a product
	// decision #1497 never asked for, and only making them VISIBLE is in scope.
	orphanMetrics, err := s.getPlaidOrphanedItemMetrics(ctx, response.Timestamp)
	if err != nil {
		slog.Error("could not derive the orphaned-Plaid-Item state for /admin/health; the counts are reported as unavailable rather than as zero",
			"error", err)
		response.PlaidOrphanedItems = domain.PlaidOrphanedItemMetrics{}
	} else {
		response.PlaidOrphanedItems = orphanMetrics
	}

	// Post-gate external-effect residue (Issue #1504) — GDPR deletions revoked after the user's Paddle
	// subscription had already been cancelled. Same "unknown is not zero" convention as every gauge
	// above, and its own section rather than a field on JobBacklog so one failing query cannot blank
	// out the other's numbers (see domain.DeletionResidueMetrics). Also deliberately NOT fed into
	// calculateOverallStatus, matching both sections above: #1504 asked for visibility, not for a new
	// definition of system health.
	residueMetrics, err := s.getDeletionResidueMetrics(ctx)
	if err != nil {
		slog.Error("could not read the deletion-residue count for /admin/health; it is reported as unavailable rather than as zero — a cancelled subscription nobody has been told about must never render as a green zero",
			"error", err)
		response.DeletionResidue = domain.DeletionResidueMetrics{}
	} else {
		response.DeletionResidue = residueMetrics
	}

	rejectedWebhookMetrics, err := s.getRejectedWebhookAttemptMetrics(ctx)
	if err != nil {
		slog.Error("could not read the rejected-webhook-attempt stats for /admin/health; reported as unavailable rather than as zero — a forged-webhook flood must never render as a confident '0 attacks'",
			"error", err)
		response.RejectedWebhookAttempts24h = domain.RejectedWebhookAttemptMetrics{}
	} else {
		response.RejectedWebhookAttempts24h = rejectedWebhookMetrics
	}

	// Calculate overall status
	response.Status = s.calculateOverallStatus(response)

	return response, nil
}

// checkDatabase checks database health and connection pool metrics
func (s *systemHealthService) checkDatabase(ctx context.Context) domain.DatabaseHealth {
	start := time.Now()
	err := s.pool.Ping(ctx)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		return domain.DatabaseHealth{
			Status:      "down",
			Connections: domain.ConnectionPoolStats{},
			LatencyMs:   latency,
		}
	}

	stats := s.pool.GetStats()
	return domain.DatabaseHealth{
		Status: "up",
		Connections: domain.ConnectionPoolStats{
			Active:       stats.AcquiredConns,
			Idle:         stats.IdleConns,
			Max:          stats.MaxConns,
			Constructing: stats.ConstructingConns,
			Total:        stats.TotalConns,
		},
		LatencyMs: latency,
	}
}

// checkClerk checks Clerk service status (placeholder)
func (s *systemHealthService) checkClerk(ctx context.Context) domain.ServiceHealth {
	// Placeholder - Clerk is considered up if configured
	return domain.ServiceHealth{
		Status:    "up",
		LatencyMs: 0,
	}
}

// checkPlaid checks Plaid service status
func (s *systemHealthService) checkPlaid(ctx context.Context) domain.PlaidHealth {
	// Plaid is considered up if environment is configured
	return domain.PlaidHealth{
		Status:      "up",
		Environment: s.plaidEnv,
	}
}

// getQueueMetrics retrieves metrics for a specific queue
func (s *systemHealthService) getQueueMetrics(ctx context.Context, queue string) (*domain.QueueMetrics, error) {
	stats, err := s.healthRepo.GetRiverJobStats(ctx, queue)
	if err != nil {
		return nil, err
	}

	completedLastHour, err := s.healthRepo.GetRiverJobsCompletedLastHour(ctx, queue)
	if err != nil {
		return nil, err
	}

	stats.CompletedLastHour = completedLastHour
	return stats, nil
}

// getSyncMetrics retrieves Plaid sync status metrics
func (s *systemHealthService) getSyncMetrics(ctx context.Context) (*domain.SyncStatusMetrics, error) {
	activeSyncs, err := s.healthRepo.GetActiveSyncCount(ctx)
	if err != nil {
		return nil, err
	}
	failedLastHour, err := s.healthRepo.GetFailedSyncsLastHour(ctx)
	if err != nil {
		return nil, err
	}
	avgDuration, err := s.healthRepo.GetAverageSyncDuration(ctx)
	if err != nil {
		return nil, err
	}
	return &domain.SyncStatusMetrics{
		ActiveSyncs:       activeSyncs,
		FailedLastHour:    failedLastHour,
		AverageDurationMs: avgDuration,
	}, nil
}

// getDeadLetterMetrics retrieves dead letter queue metrics
func (s *systemHealthService) getDeadLetterMetrics(ctx context.Context) (*domain.DeadLetterMetrics, error) {
	stats, err := s.deadLetterRepo.GetStats(ctx)
	if err != nil {
		return nil, err
	}
	oldestJobAge, err := s.deadLetterRepo.GetOldestJobAge(ctx)
	if err != nil {
		return nil, err
	}
	return &domain.DeadLetterMetrics{
		Count:             stats.Total,
		OldestJobAgeHours: oldestJobAge,
	}, nil
}

// getPaddleMetrics retrieves Paddle subscription and webhook health metrics.
func (s *systemHealthService) getPaddleMetrics(ctx context.Context) (*domain.PaddleStatusMetrics, error) {
	activeCount, err := s.healthRepo.GetActiveSubscriptionCount(ctx)
	if err != nil {
		return nil, err
	}

	total, failed, err := s.healthRepo.GetPaddleWebhookStats24h(ctx)
	if err != nil {
		return nil, err
	}

	var successRate float64
	if total > 0 {
		successRate = float64(total-failed) / float64(total) * 100
	}

	lastRun, err := s.healthRepo.GetLastReconciliationRun(ctx)
	if err != nil {
		return nil, err
	}

	return &domain.PaddleStatusMetrics{
		ActiveSubscriptions:   activeCount,
		WebhookSuccessRate24h: successRate,
		FailedWebhooks24h:     failed,
		TotalWebhooks24h:      total,
		LastReconciliationAt:  lastRun,
	}, nil
}

// getJobBacklogMetrics retrieves the three DB-derived backlog gauges (Issue #1458) via
// repository.JobHealthRepository — deliberately the SAME interface (and underlying queries) the
// retention-cleanup repair job and the manual-tier-override expiry worker use, never duplicated
// SQL. Nil-safe: an unwired jobHealthRepo returns zero values rather than erroring, matching every
// other optional-dependency convention in this service (checkClerk/checkPlaid are placeholders;
// a missing jobHealthRepo simply means this gauge isn't wired yet, not a health-check failure).
func (s *systemHealthService) getJobBacklogMetrics(ctx context.Context) (*domain.JobBacklogMetrics, error) {
	if s.jobHealthRepo == nil {
		// Not wired yet, not a query failure — but still no real data to report, so this stays
		// Available: false (the zero value) rather than a falsely-confident "verified zero"
		// (Issue #1458 follow-up G16; see JobBacklogMetrics.Available's doc comment).
		return &domain.JobBacklogMetrics{}, nil
	}

	outstandingUsers, oldestOutstanding, err := s.jobHealthRepo.GetOutstandingPlaidCleanupStats(ctx)
	if err != nil {
		return nil, err
	}

	// Batch R4 Task 4 (Issue #1458 coverage gap): if this SECOND query fails, outstandingUsers/
	// oldestOutstanding from the FIRST query above are deliberately DISCARDED rather than
	// returned as a partial JobBacklogMetrics — the caller (getSystemHealth) treats any non-nil
	// error here as "no data, unavailable" and zero-values the whole struct (Available stays
	// false, its Go zero value). This is a deliberate choice, not an oversight: JobBacklogMetrics
	// has a single `Available` flag covering all three numeric fields together (see its own doc
	// comment on why — one bool, not three `*int64`s), so there is no wire-visible way to mark
	// "the Plaid-cleanup pair is verified but the override-age gauge is unknown" independently.
	// Splitting Available per-field would ripple into HealthDashboardPage.tsx's rendering and the
	// generated TypeScript client for a partial-data case with no operator-facing upside — an
	// admin looking at /admin/health during a query failure needs to know SOMETHING is currently
	// unverifiable, not which fraction of it. Discarding good data alongside the failed query
	// keeps that single signal simple and matches every other getX() helper in this service
	// (getPaddleMetrics, getDeadLetterMetrics): one query error zero-values the whole metrics
	// struct for that section, never a partially-populated one.
	oldestUnclearedExpired, err := s.jobHealthRepo.GetOldestUnclearedExpiredManualTierOverrideAge(ctx)
	if err != nil {
		return nil, err
	}

	return &domain.JobBacklogMetrics{
		PlaidCleanupOutstandingUsers:                    int64(outstandingUsers),
		PlaidCleanupOldestOutstandingSeconds:            int64(oldestOutstanding.Seconds()),
		ManualTierOverrideOldestUnclearedExpiredSeconds: int64(oldestUnclearedExpired.Seconds()),
		// Both queries above succeeded — this is a VERIFIED backlog snapshot (possibly a real
		// zero), not an unknown. Issue #1458 follow-up G16.
		Available: true,
	}, nil
}

// getPlaidOrphanedItemMetrics derives the aggregate state of plaid_orphaned_items for /admin/health.
// Issue #1497 item 5.
//
// ⚠ WHY THIS IS A FULL READ AND NOT A `COUNT(*) FILTER (WHERE ...)`, precisely stated: of
// domain.PlaidOrphanedItem.AdminState's four checks, exactly ONE resists SQL — check 2 (TokenIsLive),
// which depends on whether THIS PROCESS can decrypt the stored ciphertext, a fact the database cannot
// see because the key lives in the application. Checks 1, 3 and 4 are all SQL-expressible (resolved_at,
// the plaintext last_retry_error_code, and an interval comparison). One irreducible check is enough:
// the derivation has to happen in Go, so it happens ONCE, in the domain, shared with the periodic
// monitoring pass that publishes the same numbers to Prometheus. Two derivations of one state is how
// a dashboard and an alert come to disagree.
//
// ⚠ AND WHY A FULL READ IS ACCEPTABLE, WITH THE HONEST CAVEAT. Migrations 000134 and 000135 both argue
// this table holds single digits of rows, and that argument is about the arrival rate — it takes a
// permanently-unremovable Plaid Item AND a completed erasure (which the C2 guard makes wait out a
// 7-day terminal-failure window) to write one. It is NOT a bound: nothing prunes this table, so the
// population only ever grows. domain.PlaidOrphanedItemHealthScanCap is that gap closed mechanically —
// past the cap this refuses to report anything at all rather than publishing a truncated scan as a
// total. See that constant.
//
// Nil-safe, matching getJobBacklogMetrics: an unwired repo is "unknown", never "zero".
func (s *systemHealthService) getPlaidOrphanedItemMetrics(ctx context.Context, asOf time.Time) (domain.PlaidOrphanedItemMetrics, error) {
	if s.plaidOrphanRepo == nil {
		// Not wired, not failed — but equally not a verified reading, so Available stays at its zero
		// value. An /admin/health that reported "0 Items need manual intervention" because nobody
		// wired the repository would be this branch's signature defect on its most compliance-sensitive
		// surface.
		return domain.PlaidOrphanedItemMetrics{}, nil
	}

	items, total, err := s.plaidOrphanRepo.List(ctx, domain.PlaidOrphanedItemHealthScanCap, 0)
	if err != nil {
		return domain.PlaidOrphanedItemMetrics{}, fmt.Errorf("list plaid_orphaned_items: %w", err)
	}
	// The cap check lives inside SummarisePlaidOrphanedItemsBounded so this reader and the periodic
	// Prometheus pass share one bound as well as one derivation — the tripwire migration 000134 asked
	// for in prose ("revisit if it ever grows, which would itself be the alarming signal").
	m, err := domain.SummarisePlaidOrphanedItemsBounded(items, total, asOf)
	if err != nil {
		return domain.PlaidOrphanedItemMetrics{}, err
	}
	// The read covered the whole table — this is a VERIFIED state snapshot, possibly a real zero.
	// Claimed here and nowhere else: the domain summariser deliberately leaves this false so that
	// every path has to say so explicitly. See domain.PlaidOrphanedItemMetrics.Available.
	m.Available = true
	return m, nil
}

// getDeletionResidueMetrics reads the post-gate external-effect residue count for /admin/health.
// Issue #1504.
//
// ⚠ WHY THIS ONE IS A SQL COUNT WHILE getPlaidOrphanedItemMetrics above is a bounded full read — the
// distinction matters, because the wrong one of these two shapes is a defect in either direction. That
// derivation needs Go because one of AdminState's four checks (TokenIsLive) depends on whether THIS
// PROCESS can decrypt a stored ciphertext, a fact no database can see. Nothing of the kind applies
// here: a residue record has no state to derive at all. Its existence IS the fact, its action names
// the effect, and its resource_id names the user. So a single indexed aggregate answers it exactly,
// and loading rows to count them would be strictly worse — audit_logs is far larger than
// plaid_orphaned_items, is appended to by every write path in the product, and has no pruning path, so
// there is no cap that would make a full read safe here the way domain.PlaidOrphanedItemHealthScanCap
// makes one safe there.
//
// Nil-safe, matching getJobBacklogMetrics and getPlaidOrphanedItemMetrics: an unwired repo is
// "unknown", never "zero".
func (s *systemHealthService) getDeletionResidueMetrics(ctx context.Context) (domain.DeletionResidueMetrics, error) {
	if s.jobHealthRepo == nil {
		// Not wired, not failed — but equally not a verified reading, so Available stays at its zero
		// value. See domain.DeletionResidueMetrics.Available.
		return domain.DeletionResidueMetrics{}, nil
	}

	count, newestAge, err := s.jobHealthRepo.GetDeletionResidueStats(ctx)
	if err != nil {
		return domain.DeletionResidueMetrics{}, fmt.Errorf("read deletion-residue stats: %w", err)
	}
	return domain.DeletionResidueMetrics{
		Records:             int64(count),
		NewestRecordSeconds: int64(newestAge.Seconds()),
		// The query succeeded — this is a VERIFIED count, possibly a real (and very welcome) zero.
		Available: true,
	}, nil
}

// getRejectedWebhookAttemptMetrics derives domain.RejectedWebhookAttemptMetrics from
// InvalidWebhookAttemptStatsReader.GetStats24h — Issue #1514 requirement #5. Total24h is summed
// in Go from the per-provider map GetStats24h returns rather than asking the repository for a
// second, separately-aggregated total: one query, one result set, both numbers derived from it.
func (s *systemHealthService) getRejectedWebhookAttemptMetrics(ctx context.Context) (domain.RejectedWebhookAttemptMetrics, error) {
	if s.invalidWebhookAttemptRepo == nil {
		return domain.RejectedWebhookAttemptMetrics{}, nil
	}

	byProvider, mostRecentAt, err := s.invalidWebhookAttemptRepo.GetStats24h(ctx)
	if err != nil {
		return domain.RejectedWebhookAttemptMetrics{}, fmt.Errorf("read rejected-webhook-attempt stats: %w", err)
	}

	var total int64
	for _, count := range byProvider {
		total += count
	}

	return domain.RejectedWebhookAttemptMetrics{
		Total24h:     total,
		ByProvider:   byProvider,
		MostRecentAt: mostRecentAt,
		Available:    true,
	}, nil
}

// calculateOverallStatus determines the overall system health status
func (s *systemHealthService) calculateOverallStatus(response *domain.SystemHealthResponse) string {
	// Unhealthy: Database down
	if response.Services.Database.Status == "down" {
		return "unhealthy"
	}

	// Degraded: High pending jobs or dead letter queue issues
	for _, metrics := range response.JobQueues {
		if metrics.Pending > 100 {
			return "degraded"
		}
	}

	if response.DeadLetterQueue.Count > 10 {
		return "degraded"
	}

	// Degraded: Low Paddle webhook success rate (with sufficient volume)
	if response.PaddleStatus.TotalWebhooks24h > 10 && response.PaddleStatus.WebhookSuccessRate24h < 80 {
		return "degraded"
	}

	// Healthy: Everything nominal
	return "healthy"
}
