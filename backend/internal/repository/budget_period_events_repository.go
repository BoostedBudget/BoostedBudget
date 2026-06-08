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

	// InsertMarker inserts an event marker. Returns (true, nil) if the row was
	// actually inserted, (false, nil) if the marker already existed (ON CONFLICT).
	// The UNIQUE constraint is the real idempotency backstop; the bool lets the
	// caller distinguish a fresh insert from a concurrent/repeat call without a
	// separate HasMarker round-trip.
	InsertMarker(ctx context.Context, userID string, budgetID string, periodStart time.Time, periodEnd time.Time, eventType string) (bool, error)

	// DeleteMarker removes a previously-inserted marker so the next daily run can
	// retry. Used when Enqueue fails after a successful InsertMarker — without this,
	// the orphaned marker would permanently suppress the event for that period.
	DeleteMarker(ctx context.Context, userID string, budgetID string, periodStart time.Time, periodEnd time.Time, eventType string) error
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

// InsertMarker inserts a period event marker. Returns (true, nil) if the row was
// actually inserted, (false, nil) if the marker already existed (ON CONFLICT DO
// NOTHING). The caller should only proceed with enqueueing when inserted=true to
// avoid duplicate rule evaluations under concurrent worker execution.
func (r *budgetPeriodEventsRepository) InsertMarker(
	ctx context.Context,
	userID string,
	budgetID string,
	periodStart time.Time,
	periodEnd time.Time,
	eventType string,
) (bool, error) {
	query := `
		INSERT INTO budget_period_events (id, user_id, budget_id, period_start, period_end, event_type)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (user_id, budget_id, period_start, period_end, event_type) DO NOTHING
	`

	ct, err := r.db.Exec(ctx, query,
		uuid.New().String(),
		userID,
		budgetID,
		periodStart,
		periodEnd,
		eventType,
	)
	if err != nil {
		return false, err
	}
	// RowsAffected == 0 means the ON CONFLICT clause fired — marker already existed.
	return ct.RowsAffected() > 0, nil
}

// DeleteMarker removes a period event marker by the natural key. Used when Enqueue
// fails after a successful InsertMarker — callers delete the orphaned marker so the
// next daily run can retry (rather than silently suppressing the event forever).
// Deleting a marker that doesn't exist is a no-op (idempotent).
func (r *budgetPeriodEventsRepository) DeleteMarker(
	ctx context.Context,
	userID string,
	budgetID string,
	periodStart time.Time,
	periodEnd time.Time,
	eventType string,
) error {
	query := `
		DELETE FROM budget_period_events
		WHERE user_id = $1
		  AND budget_id = $2
		  AND period_start = $3
		  AND period_end = $4
		  AND event_type = $5
	`
	_, err := r.db.Exec(ctx, query, userID, budgetID, periodStart, periodEnd, eventType)
	return err
}
