package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// SystemHealthRepository handles system health related database operations
type SystemHealthRepository struct {
	db *pgxpool.Pool
}

// NewSystemHealthRepository creates a new SystemHealthRepository
func NewSystemHealthRepository(db *pgxpool.Pool) *SystemHealthRepository {
	return &SystemHealthRepository{db: db}
}

// GetRiverJobStats retrieves job statistics for a specific queue
func (r *SystemHealthRepository) GetRiverJobStats(ctx context.Context, queue string) (*domain.QueueMetrics, error) {
	query := `
		SELECT
			COUNT(*) FILTER (WHERE state = 'available') as pending,
			COUNT(*) FILTER (WHERE state = 'running') as running,
			COUNT(*) FILTER (WHERE state IN ('retryable', 'discarded')) as failed
		FROM river_job
		WHERE queue = $1
	`

	var metrics domain.QueueMetrics
	err := r.db.QueryRow(ctx, query, queue).Scan(
		&metrics.Pending,
		&metrics.Running,
		&metrics.Failed,
	)
	if err != nil {
		return nil, err
	}

	return &metrics, nil
}

// GetRiverJobsCompletedLastHour retrieves the count of completed jobs in the last hour for a specific queue
func (r *SystemHealthRepository) GetRiverJobsCompletedLastHour(ctx context.Context, queue string) (int64, error) {
	query := `
		SELECT COUNT(*)
		FROM river_job
		WHERE queue = $1
		AND state = 'completed'
		AND finalized_at > NOW() - INTERVAL '1 hour'
	`

	var count int64
	err := r.db.QueryRow(ctx, query, queue).Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

// GetActiveSyncCount retrieves the count of currently running plaid_sync jobs
func (r *SystemHealthRepository) GetActiveSyncCount(ctx context.Context) (int64, error) {
	query := `
		SELECT COUNT(*)
		FROM river_job
		WHERE kind = 'plaid_sync'
		AND state = 'running'
	`

	var count int64
	err := r.db.QueryRow(ctx, query).Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

// GetFailedSyncsLastHour retrieves the count of failed syncs in the last hour
func (r *SystemHealthRepository) GetFailedSyncsLastHour(ctx context.Context) (int64, error) {
	query := `
		SELECT COUNT(*)
		FROM linked_items
		WHERE status IN ('error', 'login_required')
		AND last_error_at > NOW() - INTERVAL '1 hour'
	`

	var count int64
	err := r.db.QueryRow(ctx, query).Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

// GetAverageSyncDuration retrieves the average sync duration in milliseconds for the last hour
func (r *SystemHealthRepository) GetAverageSyncDuration(ctx context.Context) (int64, error) {
	query := `
		SELECT COALESCE(AVG(EXTRACT(epoch FROM (finalized_at - scheduled_at)) * 1000), 0)::BIGINT
		FROM river_job
		WHERE kind = 'plaid_sync'
		AND state = 'completed'
		AND finalized_at > NOW() - INTERVAL '1 hour'
	`

	var avgMs int64
	err := r.db.QueryRow(ctx, query).Scan(&avgMs)
	if err != nil {
		return 0, err
	}

	return avgMs, nil
}

// GetActiveSubscriptionCount returns count of users with active Paddle subscriptions
func (r *SystemHealthRepository) GetActiveSubscriptionCount(ctx context.Context) (int64, error) {
	query := `
        SELECT COUNT(*)
        FROM users
        WHERE paddle_subscription_id IS NOT NULL
          AND subscription_status NOT IN ('inactive', 'cancelled')
          AND is_deleted = FALSE
    `
	var count int64
	err := r.db.QueryRow(ctx, query).Scan(&count)
	return count, err
}

// GetPaddleWebhookStats24h returns total and failed webhook counts in the last 24 hours
func (r *SystemHealthRepository) GetPaddleWebhookStats24h(ctx context.Context) (total int64, failed int64, err error) {
	query := `
        SELECT
            COUNT(*) as total,
            COUNT(*) FILTER (WHERE processing_error IS NOT NULL) as failed
        FROM paddle_webhook_events
        WHERE created_at > NOW() - INTERVAL '24 hours'
    `
	err = r.db.QueryRow(ctx, query).Scan(&total, &failed)
	return total, failed, err
}

// GetLastReconciliationRun returns the time of the last completed reconciliation job
func (r *SystemHealthRepository) GetLastReconciliationRun(ctx context.Context) (*time.Time, error) {
	query := `
        SELECT MAX(finalized_at)
        FROM river_job
        WHERE kind = 'paddle_reconciliation'
          AND state = 'completed'
    `
	var lastRun *time.Time
	err := r.db.QueryRow(ctx, query).Scan(&lastRun)
	if err != nil {
		return nil, err
	}
	return lastRun, nil
}
