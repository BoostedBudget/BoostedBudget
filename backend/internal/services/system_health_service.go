package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/health"
	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
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
	// healthChecks holds the SHARED memoized Clerk/Redis Check instances (Issue #1531 Task 7d) —
	// the same instances cmd/server hands to the public /health handler, so an admin page load
	// reuses whatever the liveness probe already paid for instead of re-probing. Zero value
	// (both Checks nil) reports "not_configured" for both, matching every other optional
	// collaborator in this service.
	healthChecks SharedHealthChecks
	// cacheStatus backs SystemHealthResponse.CacheEnabled (Issue #1531 Task 11e). Optional/nil-safe:
	// an unwired cacheStatus reports "disabled" — see CacheStatusReader's doc comment for why that
	// is not ambiguous the way the other optional collaborators' Available flags are.
	cacheStatus CacheStatusReader
	// forkedIdentityRepo backs SystemHealthResponse.ForkedSecurityIdentities (#1631 M8b). Optional/
	// nil-safe: an unwired repo reports "unavailable", never a confident zero — same convention as
	// plaidOrphanRepo and invalidWebhookAttemptRepo above.
	forkedIdentityRepo ForkedSecurityIdentityLister
	// positionReconciliationRepo backs SystemHealthResponse.PositionReconciliation (#1631 M8b I1).
	// Optional/nil-safe: an unwired repo reports "unavailable", never a confident zero — same
	// convention as every other optional collaborator in this service.
	positionReconciliationRepo PositionReconciliationSummarizer
}

// ForkedSecurityIdentityLister is the ONE verb the health derivation needs from
// repository.SecurityRepository, narrowed the same way PlaidOrphanedItemLister and
// InvalidWebhookAttemptStatsReader narrow their repositories above: a read-only health surface
// gains no ability to touch `securities` just because it needs a count.
//
// ⚠ FindForkedIdentities, NOT CountForkedIdentityGroups (#1631 M8b I2 fix). The count-only method
// still exists on repository.SecurityRepository, but a bare "group_count: 2" told an operator a
// fork existed while giving them nothing to act on — no way to find out WHICH two securities.
// This is the same "list, not just count" call getPlaidOrphanedItemMetrics already makes for
// plaid_orphaned_items, and for the same reason: fork groups are expected to be a handful of rows
// at most (they only form from a corporate-action reissue of plaid_security_id), so a full read
// is cheap and the detail is the point.
type ForkedSecurityIdentityLister interface {
	FindForkedIdentities(ctx context.Context) ([]domain.ForkedSecurityIdentity, error)
}

// PositionReconciliationSummarizer is the ONE verb the health derivation needs from
// repository.InvestmentAnalyticsRepository, narrowed the same way every other repository
// dependency in this service is narrowed to a read-only surface.
type PositionReconciliationSummarizer interface {
	PositionReconciliationSummary(ctx context.Context) (*domain.PositionReconciliationSummary, error)
}

// CacheStatusReader is the ONE verb the health derivation needs from cache.Cache — matches the
// narrow-interface convention this file already uses for PlaidOrphanedItemLister and
// InvalidWebhookAttemptStatsReader. Issue #1531 Task 11e: added so the E2E cache-parity/fail-safe
// suite can assert against a live signal that caching is actually active before running, rather
// than trusting cfg.CacheEnabled alone — cache.Cache.Enabled already folds in the hard-disabled
// constant, the Redis control key, the env-disabled list, and the compiled default (see
// cache.Control's doc comment), and a disabled/unwired cache's Enabled always returns false (see
// cache.NewNoopCache), so there is no separate "unavailable" state to track here.
type CacheStatusReader interface {
	Enabled(ns cache.Namespace) bool
}

// SharedHealthChecks bundles the memoized health.Check instances (internal/health) that
// SystemHealthService reuses rather than re-probing, plus an optional way to read the limiter
// Redis client's local pool stats for the /admin/health Redis detail. See internal/health's
// package doc comment for why sharing one Check instance across consumers matters most during an
// outage — an admin page load must not fire a second Redis PING (or Clerk call) that the public
// liveness probe already paid for a moment earlier.
type SharedHealthChecks struct {
	// Clerk is the same Check the public /health handler's registry.Clerk holds (though that
	// handler never calls it — see operations.HealthHandler's doc comment). Nil means Clerk is
	// not configured for this deployment.
	Clerk health.Check
	// Redis is the same Check the public /health handler's registry.Redis holds (also never
	// called there). Nil means no Redis client is wired for this deployment.
	Redis health.Check
	// RedisPoolStats optionally reports the limiter Redis client's local connection-pool state.
	// Nil when no Redis client is wired — checkRedis skips populating RedisHealth.PoolStats
	// entirely rather than reporting a zero-valued snapshot that would look like an idle-but-live
	// pool.
	RedisPoolStats func() redisclient.PoolStats
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
	healthChecks SharedHealthChecks,
	cacheStatus CacheStatusReader,
	forkedIdentityRepo ForkedSecurityIdentityLister,
	positionReconciliationRepo PositionReconciliationSummarizer,
) SystemHealthService {
	return &systemHealthService{
		healthRepo:                 healthRepo,
		deadLetterRepo:             deadLetterRepo,
		pool:                       pool,
		plaidEnv:                   plaidEnv,
		jobHealthRepo:              jobHealthRepo,
		plaidOrphanRepo:            plaidOrphanRepo,
		invalidWebhookAttemptRepo:  invalidWebhookAttemptRepo,
		healthChecks:               healthChecks,
		cacheStatus:                cacheStatus,
		forkedIdentityRepo:         forkedIdentityRepo,
		positionReconciliationRepo: positionReconciliationRepo,
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
		// Redis IS consulted by calculateOverallStatus below (Issue #1531 criterion 12: Redis
		// down must surface as "degraded") — but only as far as "degraded", never "unhealthy".
		// See domain.ServiceHealthStatuses.Redis's doc comment and calculateOverallStatus's own
		// comment for the boundary that must never move.
		Redis: s.checkRedis(ctx),
	}

	// Get queue metrics for all queues
	queues := []string{"critical", "default", "notifications", "low", "revenuecat"}
	for _, queue := range queues {
		metrics, err := s.getQueueMetrics(ctx, queue)
		if err != nil {
			// Log error but continue - partial data is better than none
			response.JobQueues[queue] = domain.QueueMetrics{Unavailable: true}
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

	// Forked security identities (#1631 M8b) — a Plaid corporate action can reissue
	// plaid_security_id, silently forking one real instrument's identity into multiple `securities`
	// rows with separate transaction/holding histories. Same "unknown is not zero" convention as
	// every gauge above, and deliberately NOT fed into calculateOverallStatus: whether a non-zero
	// count should degrade overall health is a product decision this task never asked for, only
	// making it VISIBLE is in scope.
	forkedIdentityMetrics, err := s.getForkedSecurityIdentityMetrics(ctx)
	if err != nil {
		slog.Error("could not list forked security identities for /admin/health; reported as unavailable rather than as zero",
			"error", err)
		response.ForkedSecurityIdentities = domain.ForkedSecurityIdentityMetrics{}
	} else {
		response.ForkedSecurityIdentities = forkedIdentityMetrics
	}

	// Position reconciliation (#1631 M8b I1) — the trade-vs-position diagnostic's own admin gauge.
	// ⚠ Read domain.PositionReconciliationSummary's doc comment (and
	// domain.PositionReconciliationReport's "NOT YET FULLY ACTIONABLE" section) before treating a
	// non-zero DriftCount as a confirmed problem count — it is expected to contain real noise from
	// ordinary long-held, regularly-contributed-to positions today. Same "unknown is not zero"
	// convention as every gauge above, and deliberately NOT fed into calculateOverallStatus for the
	// same reason: whether this should degrade overall health is a product decision this task never
	// asked for, and DriftCount is not yet trustworthy enough to gate anything on regardless.
	positionReconciliationMetrics, err := s.getPositionReconciliationSummary(ctx)
	if err != nil {
		slog.Error("could not compute the position-reconciliation summary for /admin/health; reported as unavailable rather than as zero",
			"error", err)
		response.PositionReconciliation = domain.PositionReconciliationSummary{}
	} else {
		response.PositionReconciliation = positionReconciliationMetrics
	}

	// Cache status (Issue #1531 Task 11e) — deliberately NOT fed into calculateOverallStatus below,
	// matching every gauge above added after the original health check set: whether a disabled
	// cache should degrade overall health is a product decision this task never asked for, only
	// making it VISIBLE (so the E2E guard can gate on it) is in scope.
	response.CacheEnabled = s.checkCacheEnabled()

	// Calculate overall status. Redis and Clerk detail above is deliberately gathered before
	// this call so calculateOverallStatus can read Services.Redis/Services.Clerk directly — see
	// that function's doc comment for the "degraded, never unhealthy" rule Issue #1531 criterion
	// 12 requires for both.
	response.Status = s.calculateOverallStatus(response)

	return response, nil
}

// checkCacheEnabled reports whether the analytics-namespace cache is currently active, or false
// when no CacheStatusReader is wired. See CacheStatusReader's doc comment for why nil and
// "wired but disabled" are the same reportable state here.
func (s *systemHealthService) checkCacheEnabled() bool {
	if s.cacheStatus == nil {
		return false
	}
	return s.cacheStatus.Enabled(cache.NamespaceAnalytics)
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

// checkClerk reports Clerk service status via the SHARED memoized Clerk health.Check (Issue
// #1531 Task 7d) — the same Check instance the public /health handler's registry.Clerk holds,
// so an admin page load never fires a second api.clerk.com/v1/jwks call beyond whatever the
// liveness probe already paid for within the check's TTL.
func (s *systemHealthService) checkClerk(ctx context.Context) domain.ServiceHealth {
	if s.healthChecks.Clerk == nil {
		return domain.ServiceHealth{Status: "not_configured"}
	}

	result := s.healthChecks.Clerk(ctx)
	// In production the Check is never nil even when CLERK_SECRET_KEY is unset —
	// createClerkCheckWithURL's PROBE returns an error wrapping health.ErrNotConfigured instead
	// (see cmd/server/health_checks.go). So the nil-Check branch above is production-unreachable;
	// this errors.Is check is what actually distinguishes "not configured" from "down" on a real
	// deployment. Without it, an unconfigured Clerk reads as "down" forever, which
	// calculateOverallStatus below then escalates to a permanent top-line "degraded" (Issue #1531
	// L2's failure mode, applied here to Clerk instead of Redis).
	if errors.Is(result.Err, health.ErrNotConfigured) {
		return domain.ServiceHealth{Status: "not_configured", LatencyMs: result.Latency.Milliseconds()}
	}
	status := "up"
	if !result.Healthy() {
		status = "down"
	}
	return domain.ServiceHealth{
		Status:    status,
		LatencyMs: result.Latency.Milliseconds(),
	}
}

// checkRedis derives the admin-only domain.RedisHealth detail from the SHARED memoized Redis
// health.Check (Issue #1531 Task 7d) — the same Check instance the public /health handler's
// registry.Redis holds (but never calls). Redis is deliberately never critical here: this method
// only DESCRIBES Redis's state. Its caller (GetSystemHealth) feeds the result into
// calculateOverallStatus, which may only ever move the top-line status to "degraded" for Redis
// down — see that function's doc comment for the "degraded, never unhealthy" rule Issue #1531
// criterion 12 requires ("not_configured", produced below, is excluded from that escalation
// entirely: an intentionally memory-only deployment is not degraded).
func (s *systemHealthService) checkRedis(ctx context.Context) domain.RedisHealth {
	if s.healthChecks.Redis == nil {
		return domain.RedisHealth{Status: "not_configured", Reason: "not_configured"}
	}

	result := s.healthChecks.Redis(ctx)
	h := domain.RedisHealth{
		LatencyMs:     result.Latency.Milliseconds(),
		ObservedAgoMs: time.Since(result.ObservedAt).Milliseconds(),
	}
	if errors.Is(result.Err, health.ErrNotConfigured) {
		// In production the Check is never nil even when no Redis client is wired —
		// createRedisCheck's PROBE returns an error wrapping health.ErrNotConfigured instead of
		// the Check being absent (see cmd/server/health_checks.go). So the nil-Check branch above
		// is production-unreachable; this is what actually makes "not_configured" reachable on a
		// real deployment, rather than every unwired Redis permanently reading as "down" and
		// forcing calculateOverallStatus to "degraded" (Issue #1531 L2).
		h.Status = "not_configured"
		h.Reason = "not_configured"
		return h
	}
	if result.Healthy() {
		h.Status = "up"
	} else {
		h.Status = "down"
		h.Reason = classifyRedisError(result.Err)
	}

	if s.healthChecks.RedisPoolStats != nil {
		stats := s.healthChecks.RedisPoolStats()
		h.PoolStats = &domain.RedisPoolStats{
			Hits:       stats.Hits,
			Misses:     stats.Misses,
			Timeouts:   stats.Timeouts,
			TotalConns: stats.TotalConns,
			IdleConns:  stats.IdleConns,
			StaleConns: stats.StaleConns,
			PoolSize:   stats.PoolSize,
		}
	}

	return h
}

// classifyRedisError maps a Redis health-check error into a SANITIZED CLASSIFICATION —
// "timeout", "connection_refused", "auth_failed", or "unknown" — and NEVER returns the raw error
// text. See domain.RedisHealth.Reason's doc comment for why: admin screens get screenshotted
// into support tickets and pasted into Slack, and the full error (host:port, Upstash endpoint
// names, credentials-adjacent detail) is already in slog/Sentry, findable by request ID.
func classifyRedisError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, redisclient.ErrBreakerOpen) {
		return "timeout"
	}

	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "i/o timeout") || strings.Contains(msg, "deadline exceeded"):
		return "timeout"
	case strings.Contains(msg, "connection refused") || strings.Contains(msg, "no route to host") ||
		strings.Contains(msg, "network is unreachable") || strings.Contains(msg, "no such host"):
		return "connection_refused"
	case strings.Contains(msg, "noauth") || strings.Contains(msg, "wrongpass") ||
		strings.Contains(msg, "invalid password") || strings.Contains(msg, "authentication"):
		return "auth_failed"
	default:
		return "unknown"
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

// getForkedSecurityIdentityMetrics derives the /admin/health forked-identity section (#1631 M8b).
//
// ⚠ A FULL READ (FindForkedIdentities), NOT JUST THE COUNT (#1631 M8b I2 fix). The earlier version
// of this gauge called CountForkedIdentityGroups, which told an operator a fork existed but never
// which securities forked — a "group_count: 2" with nothing to look up in Plaid's dashboard or
// this codebase's own tables. Fork groups are expected to be a handful of rows at most (they only
// form from a corporate-action reissue of plaid_security_id, not from ordinary sync volume), so a
// full read is cheap here in the same way it is for getPlaidOrphanedItemMetrics's bounded read —
// unlike that gauge, though, nothing here depends on in-process state (no decryption), so there is
// no separate cap to enforce.
//
// ⚠ CANDIDATES, not confirmed forks (#1631 CX7). institution_security_id is institution-scoped and
// this grouping has no institution dimension, so a group is a REVIEW ITEM. The member listing this
// gauge exists to serve is what lets an operator settle one. See domain.ForkedSecurityIdentity's
// CANDIDATES note before wording this as a fork count anywhere.
//
// Nil-safe, matching every other optional gauge in this service: an unwired repo is "unknown",
// never "zero".
func (s *systemHealthService) getForkedSecurityIdentityMetrics(ctx context.Context) (domain.ForkedSecurityIdentityMetrics, error) {
	if s.forkedIdentityRepo == nil {
		return domain.ForkedSecurityIdentityMetrics{}, nil
	}

	groups, err := s.forkedIdentityRepo.FindForkedIdentities(ctx)
	if err != nil {
		return domain.ForkedSecurityIdentityMetrics{}, fmt.Errorf("find forked security identities: %w", err)
	}
	return domain.ForkedSecurityIdentityMetrics{
		GroupCount: int64(len(groups)),
		Groups:     groups,
		// The query succeeded — this is a VERIFIED reading, possibly a real (and welcome) zero.
		Available: true,
	}, nil
}

// getPositionReconciliationSummary derives the /admin/health position-reconciliation gauge (#1631
// M8b I1). A single SQL aggregate over ALL users' active holdings, matching
// getForkedSecurityIdentityMetrics's "not user-scoped" shape for an admin gauge — see
// repository.InvestmentAnalyticsRepository.PositionReconciliationSummary and
// domain.PositionReconciliationSummary for what DriftCount does and does not prove.
//
// Nil-safe, matching every other optional gauge in this service: an unwired repo is "unknown",
// never "zero".
func (s *systemHealthService) getPositionReconciliationSummary(ctx context.Context) (domain.PositionReconciliationSummary, error) {
	if s.positionReconciliationRepo == nil {
		return domain.PositionReconciliationSummary{}, nil
	}

	summary, err := s.positionReconciliationRepo.PositionReconciliationSummary(ctx)
	if err != nil {
		return domain.PositionReconciliationSummary{}, fmt.Errorf("compute position reconciliation summary: %w", err)
	}
	return *summary, nil
}

// calculateOverallStatus determines the overall system health status
//
// ⚠ Issue #1531 criterion 12: "Redis remains non-critical — Redis down yields degraded + HTTP
// 200, never 503." Prior to this fix, Redis (and Clerk) were never consulted here at all, so a
// Redis outage left the admin dashboard's top-line badge reading "healthy" while
// Services.Redis.Status said "down" lower in the payload — exactly the failure the issue calls
// out: "/admin/health reporting green regardless of state is worse than no dashboard, because it
// gets trusted during an incident." TestSystemHealth_RedisDownReportsDegradedOverallStatus pins
// the fix (renamed from TestSystemHealth_RedisDownDoesNotChangeOverallStatus, which asserted the
// defect itself and is why four prior review passes never caught this).
//
// Two hard boundaries this function must never cross:
//  1. Redis/Clerk may only ever push the result to "degraded", NEVER "unhealthy"/503. Only
//     Database may return "unhealthy" (checked first, below). If Redis could 503 this probe, a
//     Redis blip would cycle every machine via Fly's http_checks — a self-inflicted outage this
//     branch's Task 6 already closed off on the PUBLIC /health endpoint. Promoting Redis to
//     critical here would reopen the exact same hazard one level up.
//  2. This function is never called from the public /health handler — that endpoint
//     structurally never runs the Redis/Clerk checks at all (see operations.HealthHandler's doc
//     comment and TestGetHealth_DoesNotRunNonCriticalChecks), so "degraded" can never leak onto
//     the unauthenticated liveness probe. This change is admin-endpoint (GetSystemHealth) only.
func (s *systemHealthService) calculateOverallStatus(response *domain.SystemHealthResponse) string {
	// Unhealthy: Database down. This is the ONLY path that may return "unhealthy" — see the
	// boundary above.
	if response.Services.Database.Status == "down" {
		return "unhealthy"
	}

	// Degraded: Redis or Clerk down (Issue #1531 criterion 12). Never escalated to "unhealthy" —
	// see this function's doc comment.
	if response.Services.Redis.Status == "down" || response.Services.Clerk.Status == "down" {
		return "degraded"
	}

	// Degraded: High pending jobs or dead letter queue issues
	for _, metrics := range response.JobQueues {
		if metrics.Unavailable || metrics.Pending > 100 {
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
