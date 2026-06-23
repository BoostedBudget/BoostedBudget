//go:build integration

package repository

// Integration test for BudgetPeriodEventsRepository.
// Verifies that the UNIQUE constraint on (user_id, budget_id, period_start, period_end, event_type)
// enforces exactly-once event firing — the core idempotency guarantee of the
// period-close scanner (#972).
//
// pgxmock cannot verify this because it never sends SQL to a real planner:
// it matches query strings and returns canned rows, so ON CONFLICT DO NOTHING
// behavior and unique-constraint violations are invisible to mock-based tests.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestInsertMarker -tags integration -v

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInsertMarker_UniqueExactlyOnce_Integration proves the budget_period_events
// UNIQUE index enforces exactly-once firing. The first InsertMarker must return
// inserted=true; a second identical call must return inserted=false (ON CONFLICT
// DO NOTHING) and leave exactly one row in the table.
func TestInsertMarker_UniqueExactlyOnce_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	// Seed user and budget — budget_id has a FK constraint to budgets(id).
	userID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userID.String(), "int-marker-"+userID.String()+"@example.com")
	require.NoError(t, err, "seed user")

	budgetID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO budgets (id, user_id, name, period, start_date, amount, series_id)
		VALUES ($1, $2, 'Integration Test Budget', 'monthly', '2024-01-01', 1000.00, $1)
	`, budgetID, userID)
	require.NoError(t, err, "seed budget")

	repo := NewBudgetPeriodEventsRepository(tx)

	periodStart := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)
	const eventType = "budget_period_end"

	// First insert: must succeed and return inserted=true.
	inserted, err := repo.InsertMarker(ctx,
		userID.String(), budgetID.String(), periodStart, periodEnd, eventType)
	require.NoError(t, err)
	assert.True(t, inserted)

	// Second insert with identical natural key: ON CONFLICT DO NOTHING → inserted=false.
	inserted, err = repo.InsertMarker(ctx,
		userID.String(), budgetID.String(), periodStart, periodEnd, eventType)
	require.NoError(t, err)
	assert.False(t, inserted)

	// Verify exactly one row was created; the UNIQUE index is the backstop.
	var rowCount int
	err = tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM budget_period_events
		WHERE user_id   = $1
		  AND budget_id = $2
		  AND period_start = $3
		  AND period_end   = $4
		  AND event_type   = $5
	`, userID.String(), budgetID.String(), periodStart, periodEnd, eventType).Scan(&rowCount)
	require.NoError(t, err)
	assert.Equal(t, 1, rowCount)
}
