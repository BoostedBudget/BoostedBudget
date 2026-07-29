package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Application-level Prometheus metrics.
// HTTP metrics are handled by middleware/metrics.go.
// These cover business-logic and infrastructure events.

var (
	// AppErrorsTotal counts 5xx errors by type and status code
	AppErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "app_errors_total",
		Help: "Total number of application errors by type and HTTP status code.",
	}, []string{"error_type", "status_code"})

	// AppPanicsTotal is defined in middleware/recovery.go to avoid import cycles

	// PlaidSyncDuration tracks how long Plaid sync operations take
	PlaidSyncDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "plaid_sync_duration_seconds",
		Help:    "Duration of Plaid transaction sync operations in seconds.",
		Buckets: prometheus.DefBuckets,
	})

	// PlaidSyncTxns counts transactions processed during sync by action
	PlaidSyncTxns = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "plaid_sync_transactions_total",
		Help: "Total number of Plaid transactions processed by action (added/modified/removed).",
	}, []string{"action"})

	// PlaidAPIErrors counts Plaid API errors by type and error code
	PlaidAPIErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "plaid_api_errors_total",
		Help: "Total number of Plaid API errors by error type and code.",
	}, []string{"error_type", "error_code"})

	// AuthAttempts counts authentication attempts by result
	AuthAttempts = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "auth_attempts_total",
		Help: "Total number of authentication attempts by result (success/invalid/expired/error).",
	}, []string{"result"})

	// AuthRegistrations counts new user registrations
	AuthRegistrations = promauto.NewCounter(prometheus.CounterOpts{
		Name: "auth_registrations_total",
		Help: "Total number of new user registrations.",
	})

	// JobsCompleted counts River jobs completed by kind, queue, and status
	JobsCompleted = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "jobs_completed_total",
		Help: "Total number of River jobs completed by kind, queue, and status.",
	}, []string{"kind", "queue", "status"})

	// JobDuration tracks River job execution duration by kind
	JobDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "job_duration_seconds",
		Help:    "Duration of River job execution in seconds by kind.",
		Buckets: prometheus.DefBuckets,
	}, []string{"kind"})

	// DeadLetterQueueSize tracks the current size of the dead letter queue
	DeadLetterQueueSize = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "dead_letter_queue_size",
		Help: "Current number of jobs in the dead letter queue.",
	})

	// ManualTierOverrideOldestUnclearedExpiredSeconds tracks the age (in seconds) of the OLDEST
	// manual_tier_override that has expired (manual_tier_ends_at in the past) but has not yet been
	// cleared. Issue #1413 Task P4: the direct measure of the Critical finding C1 hazard —
	// noManualOverrideAtAllPredicate (profile_repository.go) excludes a user from retention notice/
	// hard-delete for as long as ANY override survives on their row, live or lapsed, and clearing a
	// LAPSED one is exclusively manual_tier_override_expiry_check's job. Healthy is ~0; it climbs
	// monotonically, in lockstep with wall-clock time, the moment that job stops clearing overrides
	// — independent of whether that job's own jobs_completed_total/JobLastSuccessTimestamp metrics
	// are even being scraped. Refreshed periodically from repository.JobHealthRepository (see
	// cmd/server/main.go's startJobHealthMetricsCollector — lives there, not here, because
	// internal/repository already imports internal/observability, so the reverse dependency this
	// collector needs would be an import cycle if placed in this package).
	ManualTierOverrideOldestUnclearedExpiredSeconds = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "manual_tier_override_oldest_uncleared_expired_seconds",
		Help: "Age in seconds of the oldest expired-but-uncleared manual_tier_override; 0 when none exist.",
	})

	// PlaidCleanupOutstandingUsers and PlaidCleanupOldestOutstandingSeconds are the Issue #1413
	// direct measure of the sibling hazard to the manual-tier-override one above:
	// RetentionCleanupCheckWorker.repairPlaidDisconnects (backend/internal/queue/jobs/retention_cleanup_check.go)
	// is the ONLY place a lapsed user's Plaid bank connections are ever disconnected, and its
	// per-user error isolation (`errorCount++; continue` — deliberate: one user's Plaid failure
	// must never block the GDPR pre-deletion notices/deletion sweep the same run performs for
	// everyone else) means Work() still takes its success path and RecordJobSuccess still fires
	// while that user is silently re-selected on every subsequent run, forever. Before these
	// gauges, repairedCount/skippedCount/errorCount were LOG FIELDS ONLY — no metric of any kind
	// told an operator how many users have outstanding cleanup or how old the oldest is.
	//
	// Both are refreshed together from repository.JobHealthRepository.GetOutstandingPlaidCleanupStats
	// (see cmd/server/main.go's startJobHealthMetricsCollector — lives there, not here, for the
	// same import-cycle reason ManualTierOverrideOldestUnclearedExpiredSeconds's collector does).
	//
	// Healthy is 0 for both. CRITICAL: "plaid_cleanup_pending_at IS NOT NULL" alone is NOT a valid
	// proxy for "cleanup outstanding" — migration 000119 stamped that marker on EVERY inactive/
	// cancelled user regardless of whether they had ever linked a Plaid item at all, and 000120
	// re-stamped those residue rows to its own NOW(). repairPlaidDisconnects's own candidate query
	// (GetUsersNeedingPlaidDisconnectRepair) never reaches those rows — its live-linked-item EXISTS
	// clause filters them out before the marker is ever cleared — so a query that skipped that same
	// join would report a large PERMANENT phantom backlog on day one and get ignored forever. See
	// GetOutstandingPlaidCleanupStats's doc comment for the query, which joins on the SAME liveness
	// definition (a NULL linked_items.status counts as live) the repair query itself uses.
	PlaidCleanupOutstandingUsers = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "plaid_cleanup_outstanding_users",
		Help: "Number of users with outstanding Plaid-disconnect cleanup (plaid_cleanup_pending_at set, with at least one live linked item); 0 when none exist.",
	})
	PlaidCleanupOldestOutstandingSeconds = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "plaid_cleanup_oldest_outstanding_seconds",
		Help: "Age in seconds of the oldest outstanding Plaid-disconnect cleanup (plaid_cleanup_pending_at with a live linked item); 0 when none exist.",
	})

	// PlaidDisconnectRepairUserFailuresTotal counts per-user failures inside
	// RetentionCleanupCheckWorker.repairPlaidDisconnects's per-user loop (retention_cleanup_check.go)
	// — Issue #1413. Incremented at BOTH per-user failure sites: the pre-disconnect isStillEligible
	// re-read erroring, and DisconnectAllUserItemsIfEligible itself failing for any reason other
	// than the benign services.ErrSideEffectClaimBusy contention case (another flow already
	// mid-disconnect for that user — expected, not a failure). Deliberately NOT labeled by user_id:
	// an unbounded, per-entity Prometheus label is a cardinality/ops hazard this codebase avoids
	// everywhere else (see JobsCompleted/PlaidAPIErrors above — every label here is a small, fixed
	// set of values).
	//
	// This counter alone cannot distinguish "one user failed once" from "the same user has failed
	// every run for weeks" — that distinction is made by reading it TOGETHER with
	// PlaidCleanupOldestOutstandingSeconds and the per-user structured log line this counter's
	// increment site already emits (`user_id`, `error`): a sustained nonzero rate on this counter
	// while the oldest-outstanding-age gauge keeps climbing (rather than dropping back down within
	// a run or two) means failures are NOT resolving — an operator can then grep the logs by
	// user_id to find exactly who is stuck. A transient failure shows up here as a brief blip while
	// the age gauge stays low, since that user's next run succeeds and clears their marker.
	PlaidDisconnectRepairUserFailuresTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "plaid_disconnect_repair_user_failures_total",
		Help: "Total number of per-user failures in RetentionCleanupCheckWorker's Plaid-disconnect repair pass.",
	})

	// InvestmentHoldingsSyncDuration tracks how long holdings sync operations take.
	// Issue #659: Plaid Investments
	InvestmentHoldingsSyncDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "investment_holdings_sync_duration_seconds",
		Help:    "Duration of Plaid investment holdings sync operations in seconds.",
		Buckets: prometheus.DefBuckets,
	})

	// InvestmentTransactionsSyncDuration tracks how long investment transaction sync operations take.
	// Issue #659: Plaid Investments
	InvestmentTransactionsSyncDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "investment_transactions_sync_duration_seconds",
		Help:    "Duration of Plaid investment transactions sync operations in seconds.",
		Buckets: prometheus.DefBuckets,
	})

	// InvestmentHoldingsSynced counts holdings replaced per sync by outcome.
	// Issue #659: Plaid Investments
	InvestmentHoldingsSynced = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "investment_holdings_synced_total",
		Help: "Total number of investment holdings synced by outcome (replaced/cleared).",
	}, []string{"outcome"})

	// InvestmentTransactionsSynced counts investment transactions processed by action.
	// Issue #659: Plaid Investments
	InvestmentTransactionsSynced = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "investment_transactions_synced_total",
		Help: "Total number of investment transactions processed by action (inserted/skipped).",
	}, []string{"action"})

	// AccountsReplacedFiltered counts query executions that exclude replaced accounts or
	// resolve a re-link chain (Issue #1109). The "family" label groups call sites by domain
	// so we can confirm every filter path is exercised after a staging deploy.
	//
	// Expected families:
	//   accounts_list      - GetAccountsWithInstitutionFiltered (active-only path)
	//   balance_summary    - GetBalanceSummary, GetAccountTypes
	//   dashboard_history  - GetLatestByUserID, GetAggregatedHistory
	//   analytics          - analytics_repository spending/cash-flow joins
	//   transaction_match  - MonitorForMatches chain resolution
	//   automation         - rule execution chain resolution, savings goal creation
	//   investments        - holdings/investment_transactions active filter
	//   liabilities        - liability repositories active filter
	AccountsReplacedFiltered = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "accounts_replaced_filtered_total",
		Help: "Number of query executions that excluded at least one replaced account or expanded a re-link chain",
	}, []string{"family"})
)
