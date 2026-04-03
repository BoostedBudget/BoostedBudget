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
)
