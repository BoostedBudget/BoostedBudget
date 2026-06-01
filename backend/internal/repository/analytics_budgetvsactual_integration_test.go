//go:build integration

package repository

// Integration test for GetBudgetVsActual.
// Tests the budgets + budget_categories JOIN that pgxmock cannot validate.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestGetBudgetVsActual_Integration -tags integration -v

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetBudgetVsActual_Integration(t *testing.T) {
	pool := connectTestDB(t)
	repo := NewAnalyticsRepository(pool)
	ctx := context.Background()

	// Wrap all seed data in a transaction so it is automatically rolled back.
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New()

	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, auth0_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'us-east-1')
	`, userID, "test|"+userID.String(), "int-bva-"+userID.String()+"@example.com")
	require.NoError(t, err, "seed user")

	now := time.Now().UTC()

	// Seed 1 active monthly budget with 2 categories: one over budget, one under.
	monthlyBudgetID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO budgets (id, user_id, name, period, start_date, amount, is_active)
		VALUES ($1, $2, 'Monthly Budget', 'monthly', $3, 1000.00, true)
	`, monthlyBudgetID, userID, now.Format("2006-01-02"))
	require.NoError(t, err, "seed monthly budget")

	// Category 1: Groceries — over budget (spent 300 > allocated 200)
	cat1ID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO budget_categories (id, budget_id, category, allocated_amount, spent_amount)
		VALUES ($1, $2, '["FOOD_AND_DRINK"]', 200.00, 300.00)
	`, cat1ID, monthlyBudgetID)
	require.NoError(t, err, "seed budget category 1")

	// Category 2: Transport — under budget (spent 50 < allocated 150)
	cat2ID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO budget_categories (id, budget_id, category, allocated_amount, spent_amount)
		VALUES ($1, $2, '["TRANSPORTATION"]', 150.00, 50.00)
	`, cat2ID, monthlyBudgetID)
	require.NoError(t, err, "seed budget category 2")

	// Seed 1 active yearly budget — should NOT be returned when querying monthly.
	yearlyBudgetID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO budgets (id, user_id, name, period, start_date, amount, is_active)
		VALUES ($1, $2, 'Yearly Budget', 'yearly', $3, 5000.00, true)
	`, yearlyBudgetID, userID, now.Format("2006-01-02"))
	require.NoError(t, err, "seed yearly budget")

	cat3ID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO budget_categories (id, budget_id, category, allocated_amount, spent_amount)
		VALUES ($1, $2, '["SHOPPING"]', 500.00, 250.00)
	`, cat3ID, yearlyBudgetID)
	require.NoError(t, err, "seed yearly budget category")

	// Use transaction-wrapped repo.
	txRepo := NewAnalyticsRepository(tx)

	t.Run("monthly period returns monthly budget with correct totals", func(t *testing.T) {
		_ = repo // silence unused warning — txRepo is used below
		results, err := txRepo.GetBudgetVsActual(ctx, userID.String(), "monthly")
		require.NoError(t, err)
		require.Len(t, results, 1, "only the monthly budget should be returned")

		r := results[0]
		assert.Equal(t, monthlyBudgetID.String(), r.BudgetID)
		assert.Equal(t, "Monthly Budget", r.BudgetName)
		assert.Equal(t, "monthly", r.Period)
		// SUM of allocated: 200+150=350
		assert.InDelta(t, 350.0, r.AllocatedAmount, 0.01)
		// SUM of spent: 300+50=350
		assert.InDelta(t, 350.0, r.SpentAmount, 0.01)
	})

	t.Run("yearly period returns yearly budget not monthly budget", func(t *testing.T) {
		results, err := txRepo.GetBudgetVsActual(ctx, userID.String(), "yearly")
		require.NoError(t, err)
		require.Len(t, results, 1, "only the yearly budget should be returned")
		assert.Equal(t, yearlyBudgetID.String(), results[0].BudgetID)
	})

	t.Run("quarterly period returns empty slice when none exist", func(t *testing.T) {
		results, err := txRepo.GetBudgetVsActual(ctx, userID.String(), "quarterly")
		require.NoError(t, err)
		assert.Empty(t, results, "no quarterly budgets seeded")
	})
}
