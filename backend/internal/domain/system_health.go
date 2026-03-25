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
	Status          string                  `json:"status"`           // "healthy", "degraded", "unhealthy"
	Timestamp       time.Time               `json:"timestamp"`
	Services        ServiceHealthStatuses   `json:"services"`
	JobQueues       map[string]QueueMetrics `json:"job_queues"`
	SyncStatus      SyncStatusMetrics       `json:"sync_status"`
	DeadLetterQueue DeadLetterMetrics       `json:"dead_letter_queue"`
	PaddleStatus    PaddleStatusMetrics     `json:"paddle_status"`
}

// ServiceHealthStatuses contains health status for all services
type ServiceHealthStatuses struct {
	Database DatabaseHealth `json:"database"`
	Clerk    ServiceHealth  `json:"clerk"`
	Plaid    PlaidHealth    `json:"plaid"`
}

// DatabaseHealth represents database connection pool health
type DatabaseHealth struct {
	Status      string              `json:"status"`       // "up", "down"
	Connections ConnectionPoolStats `json:"connections"`
	LatencyMs   int64               `json:"latency_ms"`
}

// ConnectionPoolStats represents pgxpool connection statistics
type ConnectionPoolStats struct {
	Active       int32 `json:"active"`        // AcquiredConns
	Idle         int32 `json:"idle"`          // IdleConns
	Max          int32 `json:"max"`           // MaxConns
	Constructing int32 `json:"constructing"`  // ConstructingConns
	Total        int32 `json:"total"`         // TotalConns
}

// ServiceHealth represents a generic service health status
type ServiceHealth struct {
	Status    string `json:"status"`      // "up", "down"
	LatencyMs int64  `json:"latency_ms"`
}

// PlaidHealth represents Plaid service health
type PlaidHealth struct {
	Status      string `json:"status"`       // "up", "down"
	Environment string `json:"environment"`  // "sandbox", "development", "production"
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

