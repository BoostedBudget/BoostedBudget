package domain

import "time"

// PaddleStatusMetrics represents Paddle payment system health metrics
type PaddleStatusMetrics struct {
	ActiveSubscriptions         int64      `json:"active_subscriptions"`
	WebhookSuccessRate24h       float64    `json:"webhook_success_rate_24h"`
	FailedWebhooks24h           int64      `json:"failed_webhooks_24h"`
	TotalWebhooks24h            int64      `json:"total_webhooks24h"`
	LastReconciliationAt        *time.Time `json:"last_reconciliation_at"`
	ReconciliationDiscrepancies int64      `json:"reconciliation_discrepancies"`
}

// SystemHealthResponse represents the overall system health status
type SystemHealthResponse struct {
	Status          string                  `json:"status"` // "healthy", "degraded", "unhealthy"
	Timestamp       time.Time               `json:"timestamp"`
	Services        ServiceHealthStatuses   `json:"services"`
	JobQueues       map[string]QueueMetrics `json:"job_queues"`
	SyncStatus      SyncStatusMetrics       `json:"sync_status"`
	DeadLetterQueue DeadLetterMetrics       `json:"dead_letter_queue"`
	PaddleStatus    PaddleStatusMetrics     `json:"paddle_status"`
	// JobBacklog surfaces the three DB-derived Plaid-cleanup/manual-tier-override backlog gauges
	// (Issue #1458) — point-in-time snapshots structurally identical to DeadLetterMetrics above,
	// backed by repository.JobHealthRepository (the same queries the retention-cleanup repair
	// job and the override-expiry worker themselves use — never duplicated SQL). The other four
	// metrics PR #1443 added (three lifetime monotonic counters plus the in-process
	// JobLastSuccessTimestamp heartbeat) deliberately stay Prometheus-only — see
	// JobBacklogMetrics's own doc comment for why.
	JobBacklog JobBacklogMetrics `json:"job_backlog"`
}

// JobBacklogMetrics represents point-in-time DB snapshots of two operational backlogs PR #1443
// introduced (Issue #1458): the Plaid-disconnect cleanup queue and the manual-tier-override
// expiry-clearing queue. Both measure "how much work is stuck", the same class of signal as
// DeadLetterMetrics — an ageing, non-zero backlog means a repair/expiry job is stalling.
//
// Deliberately excludes the OTHER four metrics PR #1443 added
// (PlaidDisconnectRepairUserFailuresTotal, PlaidCleanupBackstopFailuresTotal,
// PlaidCleanupServiceNotConfiguredTotal, JobLastSuccessTimestamp): the first three are lifetime
// monotonic counters meaningful only as a Prometheus rate() — a point-in-time total can't express
// "sustained non-zero rate" vs. "one blip", and PlaidCleanupServiceNotConfiguredTotal is expected
// to be persistently non-zero in QA/E2E, so a bare number would send an operator chasing a
// non-problem. JobLastSuccessTimestamp is an in-process promauto gauge with no DB row to
// re-derive it from — in a multi-instance deploy the replica serving /admin/health may not be the
// one that ran the job, so the value would be stale or zero. All four stay exclusively
// Prometheus/Alertmanager's job.
type JobBacklogMetrics struct {
	// PlaidCleanupOutstandingUsers is the count of users with outstanding Plaid-disconnect
	// cleanup (repository.JobHealthRepository.GetOutstandingPlaidCleanupStats).
	PlaidCleanupOutstandingUsers int64 `json:"plaid_cleanup_outstanding_users"`
	// PlaidCleanupOldestOutstandingSeconds is how long the OLDEST such marker has been sitting
	// uncleared, in seconds (0 when there are none).
	PlaidCleanupOldestOutstandingSeconds int64 `json:"plaid_cleanup_oldest_outstanding_seconds"`
	// ManualTierOverrideOldestUnclearedExpiredSeconds is how long the OLDEST currently
	// expired-but-uncleared manual_tier_override has been sitting uncleared, in seconds (0 when
	// there are none) — repository.JobHealthRepository.GetOldestUnclearedExpiredManualTierOverrideAge.
	ManualTierOverrideOldestUnclearedExpiredSeconds int64 `json:"manual_tier_override_oldest_uncleared_expired_seconds"`
}

// ServiceHealthStatuses contains health status for all services
type ServiceHealthStatuses struct {
	Database DatabaseHealth `json:"database"`
	Clerk    ServiceHealth  `json:"clerk"`
	Plaid    PlaidHealth    `json:"plaid"`
}

// DatabaseHealth represents database connection pool health
type DatabaseHealth struct {
	Status      string              `json:"status"` // "up", "down"
	Connections ConnectionPoolStats `json:"connections"`
	LatencyMs   int64               `json:"latency_ms"`
}

// ConnectionPoolStats represents pgxpool connection statistics
type ConnectionPoolStats struct {
	Active       int32 `json:"active"`       // AcquiredConns
	Idle         int32 `json:"idle"`         // IdleConns
	Max          int32 `json:"max"`          // MaxConns
	Constructing int32 `json:"constructing"` // ConstructingConns
	Total        int32 `json:"total"`        // TotalConns
}

// ServiceHealth represents a generic service health status
type ServiceHealth struct {
	Status    string `json:"status"` // "up", "down"
	LatencyMs int64  `json:"latency_ms"`
}

// PlaidHealth represents Plaid service health
type PlaidHealth struct {
	Status      string `json:"status"`      // "up", "down"
	Environment string `json:"environment"` // "sandbox", "development", "production"
}

// QueueMetrics represents River job queue metrics
type QueueMetrics struct {
	Pending           int64 `json:"pending"`
	Running           int64 `json:"running"`
	Failed            int64 `json:"failed"`
	CompletedLastHour int64 `json:"completed_last_hour"`
}

// SyncStatusMetrics represents Plaid sync status metrics
type SyncStatusMetrics struct {
	ActiveSyncs       int64 `json:"active_syncs"`
	FailedLastHour    int64 `json:"failed_last_hour"`
	AverageDurationMs int64 `json:"average_duration_ms"`
}

// DeadLetterMetrics represents dead letter queue metrics
type DeadLetterMetrics struct {
	Count             int64 `json:"count"`
	OldestJobAgeHours int64 `json:"oldest_job_age_hours"`
}
