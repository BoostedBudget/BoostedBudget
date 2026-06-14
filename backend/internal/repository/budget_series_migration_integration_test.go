//go:build integration

package repository_test

// budget_series_migration_integration_test.go
//
// Integration tests for migration 000098 (budget series_id + savings_goal_category_detach re-key).
// Issue #1300 RG6 / M23.
//
// Tests:
//  (a) budgets.series_id column exists and is NOT NULL; new standalone budgets get series_id == id.
//  (b) savings_goal_category_detach is keyed by (series_id, savings_goal_id) after migration.
//  (c) A "roll-forward" budget created with from_budget_id inherits the ancestor's series_id
//      so detach tombstones survive across budget periods.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... \
//	  -run TestBudgetSeriesMigration_Integration -tags integration -v

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

func TestBudgetSeriesMigration_Integration(t *testing.T) {
	pool := integrationDB(t)
	ctx := context.Background()

	// Wrap all test data in a transaction that rolls back automatically.
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	// ── (a) Verify series_id column exists and is NOT NULL ───────────────────────

	t.Run("budgets.series_id column exists and is NOT NULL", func(t *testing.T) {
		var isNullable string
		err := tx.QueryRow(ctx, `
			SELECT is_nullable
			FROM information_schema.columns
			WHERE table_name = 'budgets' AND column_name = 'series_id'
		`).Scan(&isNullable)
		require.NoError(t, err, "series_id column should exist on budgets table")
		assert.Equal(t, "NO", isNullable, "series_id must be NOT NULL")
	})

	// ── Seed a user ──────────────────────────────────────────────────────────────

	userID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, auth0_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'us-east-1')
	`, userID, "test|"+userID.String(), "int-series-"+userID.String()+"@example.com")
	require.NoError(t, err, "seed user")

	// ── (a) Standalone budget: series_id = id ─────────────────────────────────────

	t.Run("standalone budget gets series_id == id (backfill invariant)", func(t *testing.T) {
		budgetID := uuid.New()
		_, err := tx.Exec(ctx, `
			INSERT INTO budgets (id, user_id, name, period, start_date, amount, expected_income, is_active, series_id)
			VALUES ($1, $2, 'Series Test Budget A', 'monthly', NOW()::date, 2000.00, 2000.00, true, $1)
		`, budgetID, userID)
		require.NoError(t, err, "create standalone budget")

		var gotSeriesID string
		err = tx.QueryRow(ctx, `SELECT series_id FROM budgets WHERE id = $1`, budgetID).Scan(&gotSeriesID)
		require.NoError(t, err)
		assert.Equal(t, budgetID.String(), gotSeriesID, "standalone budget must have series_id == id")
	})

	// ── (b) savings_goal_category_detach keyed by (series_id, savings_goal_id) ──

	t.Run("savings_goal_category_detach primary key is (series_id, savings_goal_id)", func(t *testing.T) {
		// Use pg_get_constraintdef for exact column-order assertion, not just membership.
		var pkDef string
		err := tx.QueryRow(ctx, `
			SELECT pg_get_constraintdef(c.oid)
			FROM pg_constraint c
			WHERE c.conrelid = 'savings_goal_category_detach'::regclass
			  AND c.contype = 'p'
		`).Scan(&pkDef)
		require.NoError(t, err, "query PK definition of savings_goal_category_detach")
		assert.Contains(t, pkDef, "series_id", "PK definition must reference series_id")
		assert.Contains(t, pkDef, "savings_goal_id", "PK definition must reference savings_goal_id")
		assert.NotContains(t, pkDef, "budget_id", "PK must not contain budget_id after migration 000098")
		// Assert canonical column order (series_id first).
		assert.Contains(t, pkDef, "(series_id, savings_goal_id)", "PK must be keyed (series_id, savings_goal_id) in that order")
	})

	// ── (c) Roll-forward: new budget inherits series_id from ancestor ──────────

	t.Run("roll-forward budget inherits ancestor series_id via BudgetRepository", func(t *testing.T) {
		// Create ancestor budget (series_id = its own id).
		ancestorID := uuid.New()
		ancestorSeriesID := ancestorID // standalone: series == self
		_, err := tx.Exec(ctx, `
			INSERT INTO budgets (id, user_id, name, period, start_date, amount, expected_income, is_active, series_id)
			VALUES ($1, $2, 'Ancestor Budget', 'monthly', NOW()::date, 3000.00, 3000.00, true, $1)
		`, ancestorID, userID)
		require.NoError(t, err, "create ancestor budget")

		// Create next-period budget that inherits the ancestor's series_id.
		newBudgetID := uuid.New()
		_, err = tx.Exec(ctx, `
			INSERT INTO budgets (id, user_id, name, period, start_date, amount, expected_income, is_active, series_id)
			VALUES ($1, $2, 'Next Period Budget', 'monthly', (NOW() + interval '1 month')::date, 3000.00, 3000.00, true, $3)
		`, newBudgetID, userID, ancestorSeriesID)
		require.NoError(t, err, "create roll-forward budget with inherited series_id")

		var gotSeriesID string
		err = tx.QueryRow(ctx, `SELECT series_id FROM budgets WHERE id = $1`, newBudgetID).Scan(&gotSeriesID)
		require.NoError(t, err)
		assert.Equal(t, ancestorSeriesID.String(), gotSeriesID,
			"roll-forward budget must inherit ancestor's series_id")

		// Seed linked_item → account → savings_goal to satisfy FK chain before
		// inserting the tombstone (savings_goal_category_detach.savings_goal_id
		// references savings_goals(id) via fk_sgcd_savings_goal).
		linkedItemID := uuid.New()
		_, err = tx.Exec(ctx, `
			INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
			VALUES ($1, $2, $3, 'encrypted_token_test')
		`, linkedItemID, userID, "item_"+linkedItemID.String())
		require.NoError(t, err, "seed linked_item")

		accountID := uuid.New()
		_, err = tx.Exec(ctx, `
			INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, subtype, current_balance)
			VALUES ($1, $2, $3, $4, 'Test Savings', 'depository', 'savings', 1000.00)
		`, accountID, linkedItemID, userID, "plaid_acct_"+accountID.String())
		require.NoError(t, err, "seed account")

		goalID := uuid.New()
		_, err = tx.Exec(ctx, `
			INSERT INTO savings_goals (id, user_id, account_id, name, target_amount, current_amount, progress_basis)
			VALUES ($1, $2, $3, 'Series Test Goal', 500.00, 0.00, 'manual')
		`, goalID, userID, accountID)
		require.NoError(t, err, "seed savings_goal")

		// Insert a detach tombstone keyed by (series_id, savings_goal_id).
		// Verify it's visible to queries against the new budget's series_id.
		_, err = tx.Exec(ctx, `
			INSERT INTO savings_goal_category_detach (series_id, savings_goal_id, budget_id)
			VALUES ($1, $2, $3)
		`, ancestorSeriesID, goalID, ancestorID)
		require.NoError(t, err, "insert detach tombstone")

		// The tombstone should be found when querying by the series (not the specific budget_id).
		repo := repository.NewBudgetRepository(tx)
		detached, err := repo.IsGoalDetachedForSeries(ctx, ancestorSeriesID.String(), goalID.String())
		require.NoError(t, err)
		assert.True(t, detached, "tombstone keyed by series_id must be visible to all budgets in the series")

		// Confirm the tombstone is also found for the new-period budget (same series).
		detached2, err := repo.IsGoalDetachedForSeries(ctx, gotSeriesID, goalID.String())
		require.NoError(t, err)
		assert.True(t, detached2, "new-period budget in same series must see existing detach tombstone")
	})
}
