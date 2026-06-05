package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// BudgetPeriodEventsRepository manages exactly-once markers for budget period events.
// The underlying table (budget_period_events) has a UNIQUE constraint on
// (user_id, budget_id, period_start, period_end, event_type) which is the
// authoritative idempotency guard. HasMarker/InsertMarker are thin wrappers.
// Issue #972: Savings Transfer Automation — period-close scanner.
type BudgetPeriodEventsRepository interface {
	// HasMarker returns true if an event marker already exists for the given
	// (user, budget, period, event_type) combination.
	HasMarker(ctx context.Context, userID string, budgetID string, periodStart time.Time, periodEnd time.Time, eventType string) (bool, error)

	// InsertMarker inserts an event marker. Uses ON CONFLICT DO NOTHING so the
	// call is idempotent — the UNIQUE constraint is the real backstop.
	InsertMarker(ctx context.Context, userID string, budgetID string, periodStart time.Time, periodEnd time.Time, eventType string) error
}

// budgetPeriodEventsRepository implements BudgetPeriodEventsRepository using PostgreSQL.
type budgetPeriodEventsRepository struct {
	db DB
}

// NewBudgetPeriodEventsRepository creates a new BudgetPeriodEventsRepository.
func NewBudgetPeriodEventsRepository(db DB) BudgetPeriodEventsRepository {
	return &budgetPeriodEventsRepository{db: db}
}

// HasMarker checks whether a period event marker exists.
func (r *budgetPeriodEventsRepository) HasMarker(
	ctx context.Context,
	userID string,
	budgetID string,
	periodStart time.Time,
	periodEnd time.Time,
	eventType string,
) (bool, error) {
	query := `
		SELECT 1
		FROM budget_period_events
		WHERE user_id = $1
		  AND budget_id = $2
		  AND period_start = $3
		  AND period_end = $4
		  AND event_type = $5
		LIMIT 1
	`

	var dummy int
	err := r.db.QueryRow(ctx, query, userID, budgetID, periodStart, periodEnd, eventType).Scan(&dummy)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// InsertMarker inserts a period event marker. The ON CONFLICT DO NOTHING clause
// makes concurrent or re-run inserts safe — no error is returned if the marker
// already exists.
func (r *budgetPeriodEventsRepository) InsertMarker(
	ctx context.Context,
	userID string,
	budgetID string,
	periodStart time.Time,
	periodEnd time.Time,
	eventType string,
) error {
	query := `
		INSERT INTO budget_period_events (id, user_id, budget_id, period_start, period_end, event_type)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (user_id, budget_id, period_start, period_end, event_type) DO NOTHING
	`

	_, err := r.db.Exec(ctx, query,
		uuid.New().String(),
		userID,
		budgetID,
		periodStart,
		periodEnd,
		eventType,
	)
	return err
}
