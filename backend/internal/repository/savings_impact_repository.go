// Package repository provides data access for savings impact snapshots.
// Issue #972: Savings Transfer Automation — daily aggregate stats.
package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// SavingsImpactRepository defines the data access methods for savings impact snapshots.
type SavingsImpactRepository interface {
	// WriteSnapshot inserts a daily snapshot. Uses ON CONFLICT ((computed_at::date)) DO UPDATE
	// so re-running the same day overwrites (idempotent per UTC date).
	WriteSnapshot(ctx context.Context, snapshot *domain.SavingsImpactSnapshot) error

	// GetLatestSnapshot returns the most recent snapshot row, or nil if none exists.
	GetLatestSnapshot(ctx context.Context) (*domain.SavingsImpactSnapshot, error)

	// GetUserImpact returns the sum of actual_amount for completed suggestions by a user
	// where include_in_aggregate_stats = true. Also returns count and recent items.
	GetUserImpact(ctx context.Context, userID string) (*domain.UserSavingsImpact, error)

	// GetAggregateSavingsStats returns cross-user aggregate totals for completed suggestions
	// where include_in_aggregate_stats = true. Used by the daily stats snapshot worker.
	GetAggregateSavingsStats(ctx context.Context) (totalAmount float64, userCount int, completedCount int, err error)
}

// savingsImpactRepository implements SavingsImpactRepository using PostgreSQL.
type savingsImpactRepository struct {
	db DB
}

// NewSavingsImpactRepository creates a new SavingsImpactRepository.
func NewSavingsImpactRepository(db DB) SavingsImpactRepository {
	return &savingsImpactRepository{db: db}
}

// WriteSnapshot inserts or updates a daily savings impact snapshot.
// The ON CONFLICT clause targets the unique expression index on
// (DATE(computed_at AT TIME ZONE 'UTC')) — this must exactly match the
// index expression in migration 000092 for PostgreSQL to use it.
func (r *savingsImpactRepository) WriteSnapshot(ctx context.Context, snapshot *domain.SavingsImpactSnapshot) error {
	query := `
		INSERT INTO savings_impact_snapshots (id, computed_at, total_amount, user_count, completed_count)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT ((DATE(computed_at AT TIME ZONE 'UTC'))) DO UPDATE SET
			total_amount    = EXCLUDED.total_amount,
			user_count      = EXCLUDED.user_count,
			completed_count = EXCLUDED.completed_count,
			computed_at     = EXCLUDED.computed_at
	`
	id := uuid.New().String()
	_, err := r.db.Exec(ctx, query,
		id,
		snapshot.ComputedAt,
		snapshot.TotalAmount,
		snapshot.UserCount,
		snapshot.CompletedCount,
	)
	return err
}

// GetLatestSnapshot returns the most recently computed snapshot, or nil if none exist.
func (r *savingsImpactRepository) GetLatestSnapshot(ctx context.Context) (*domain.SavingsImpactSnapshot, error) {
	query := `
		SELECT id, computed_at, total_amount, user_count, completed_count
		FROM savings_impact_snapshots
		ORDER BY computed_at DESC
		LIMIT 1
	`
	var s domain.SavingsImpactSnapshot
	err := r.db.QueryRow(ctx, query).Scan(
		&s.ID,
		&s.ComputedAt,
		&s.TotalAmount,
		&s.UserCount,
		&s.CompletedCount,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// GetAggregateSavingsStats returns cross-user aggregate totals for completed suggestions
// where include_in_aggregate_stats = true. This is the aggregate query used by the
// daily SavingsStatsWorker to build the platform-wide snapshot.
func (r *savingsImpactRepository) GetAggregateSavingsStats(ctx context.Context) (float64, int, int, error) {
	query := `
		SELECT
			COALESCE(SUM(ts.actual_amount), 0) AS total_amount,
			COUNT(DISTINCT ts.user_id)          AS user_count,
			COUNT(*)                            AS completed_count
		FROM transfer_suggestions ts
		JOIN privacy_settings ps ON ps.user_id = ts.user_id
		WHERE ts.status = 'completed'
		  AND ts.actual_amount IS NOT NULL
		  AND ps.include_in_aggregate_stats = true
	`
	var totalAmount float64
	var userCount, completedCount int
	err := r.db.QueryRow(ctx, query).Scan(&totalAmount, &userCount, &completedCount)
	if err != nil {
		return 0, 0, 0, err
	}
	return totalAmount, userCount, completedCount, nil
}

// GetUserImpact returns per-user savings totals and recent completed suggestions.
// Returns ALL completed suggestions for the requesting user regardless of their
// privacy/aggregate-stats setting — users always see their own data in full.
// The include_in_aggregate_stats privacy filter is applied only in GetCommunityImpact
// and similar aggregate functions, not here.
func (r *savingsImpactRepository) GetUserImpact(ctx context.Context, userID string) (*domain.UserSavingsImpact, error) {
	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	prevMonthStart := monthStart.AddDate(0, -1, 0)

	totalsQuery := `
		SELECT
			COALESCE(SUM(ts.actual_amount), 0)                                           AS total_amount,
			COUNT(*)                                                                      AS completed_count,
			COALESCE(SUM(ts.actual_amount) FILTER (WHERE ts.completed_at >= $2), 0)      AS month_amount,
			COALESCE(SUM(ts.actual_amount) FILTER (WHERE ts.completed_at >= $3 AND ts.completed_at < $2), 0) AS prev_month_amount
		FROM transfer_suggestions ts
		WHERE ts.user_id = $1
		  AND ts.status = 'completed'
		  AND ts.actual_amount IS NOT NULL
	`

	var impact domain.UserSavingsImpact
	err := r.db.QueryRow(ctx, totalsQuery, userID, monthStart, prevMonthStart).Scan(
		&impact.TotalAmount,
		&impact.CompletedCount,
		&impact.MonthAmount,
		&impact.PrevMonthAmount,
	)
	if err != nil && err != pgx.ErrNoRows {
		return nil, err
	}

	itemsQuery := `
		SELECT id, amount, actual_amount, to_account_id, to_user_account_id, completed_at, completion_source
		FROM transfer_suggestions
		WHERE user_id = $1
		  AND status = 'completed'
		  AND actual_amount IS NOT NULL
		ORDER BY completed_at DESC
		LIMIT 5
	`

	rows, err := r.db.Query(ctx, itemsQuery, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var item domain.CompletedSuggestionSummary
		if scanErr := rows.Scan(
			&item.ID,
			&item.Amount,
			&item.ActualAmount,
			&item.ToAccountID,
			&item.ToUserAccountID,
			&item.CompletedAt,
			&item.CompletionSource,
		); scanErr != nil {
			return nil, scanErr
		}
		impact.RecentItems = append(impact.RecentItems, &item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if impact.RecentItems == nil {
		impact.RecentItems = make([]*domain.CompletedSuggestionSummary, 0)
	}

	return &impact, nil
}
