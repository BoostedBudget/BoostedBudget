//go:build integration

package repository

// Integration tests for BudgetRepository.GetCategoryByIDAndUser.
// Tests the JOIN between budget_categories and budgets that pgxmock cannot validate,
// ensuring ownership scoping works correctly against a real PostgreSQL instance.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestGetCategoryByIDAndUser_Integration -tags integration -v
//
// Issue #1270: Task 3 — category ownership validation for automation rule conditions.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetCategoryByIDAndUser_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	// Wrap all seed data in a transaction that rolls back automatically.
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	// ── seed: two users ──────────────────────────────────────────────────────────
	ownerID := uuid.New()
	otherID := uuid.New()

	for _, u := range []struct{ id uuid.UUID }{
		{ownerID},
		{otherID},
	} {
		_, err = tx.Exec(ctx, `
			INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
			VALUES ($1, $2, $3, 'premium', 'active', 'iad')
		`, u.id, "test|"+u.id.String(), "int-cat-owner-"+u.id.String()+"@example.com")
		require.NoError(t, err, "seed user %s", u.id)
	}

	// ── seed: budget owned by ownerID ────────────────────────────────────────────
	ownerBudgetID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO budgets (id, user_id, name, period, start_date, amount, is_active, series_id)
		VALUES ($1, $2, 'Owner Budget', 'monthly', NOW()::date, 1000.00, true, $1)
	`, ownerBudgetID, ownerID)
	require.NoError(t, err, "seed owner budget")

	// ── seed: category within ownerID's budget ───────────────────────────────────
	ownedCatID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO budget_categories (id, budget_id, category_kind, pfc_primary, allocated_amount, spent_amount)
		VALUES ($1, $2, 'pfc', 'FOOD_AND_DRINK', 200.00, 0.00)
	`, ownedCatID, ownerBudgetID)
	require.NoError(t, err, "seed owned category")

	// ── seed: budget owned by otherID ────────────────────────────────────────────
	otherBudgetID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO budgets (id, user_id, name, period, start_date, amount, is_active, series_id)
		VALUES ($1, $2, 'Other Budget', 'monthly', NOW()::date, 500.00, true, $1)
	`, otherBudgetID, otherID)
	require.NoError(t, err, "seed other budget")

	// ── seed: category within otherID's budget ───────────────────────────────────
	foreignCatID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO budget_categories (id, budget_id, category_kind, pfc_primary, allocated_amount, spent_amount)
		VALUES ($1, $2, 'pfc', 'FOOD_AND_DRINK', 100.00, 0.00)
	`, foreignCatID, otherBudgetID)
	require.NoError(t, err, "seed foreign category")

	// Use a transaction-wrapped repo so queries see the seeded rows.
	txRepo := NewBudgetRepository(tx)

	t.Run("owned category returns category with no error", func(t *testing.T) {
		cat, err := txRepo.GetCategoryByIDAndUser(ctx, ownedCatID.String(), ownerID.String())
		require.NoError(t, err)
		require.NotNil(t, cat)
		assert.Equal(t, ownedCatID.String(), cat.ID)
		assert.Equal(t, ownerBudgetID.String(), cat.BudgetID)
	})

	t.Run("foreign category returns error", func(t *testing.T) {
		cat, err := txRepo.GetCategoryByIDAndUser(ctx, foreignCatID.String(), ownerID.String())
		require.Error(t, err, "should reject a category owned by a different user")
		assert.Nil(t, cat)
		assert.Contains(t, err.Error(), "budget category not found")
	})

	t.Run("non-existent category ID returns error", func(t *testing.T) {
		cat, err := txRepo.GetCategoryByIDAndUser(ctx, uuid.New().String(), ownerID.String())
		require.Error(t, err, "should reject a non-existent category")
		assert.Nil(t, cat)
		assert.Contains(t, err.Error(), "budget category not found")
	})
}

// TestBudgetRepository_CreateWithCategories_RollsBackOnCategoryFailure verifies that
// CreateWithCategories is atomic: when a mid-loop category INSERT fails (here via a
// duplicate PK on the second category), the budget row is also rolled back (R5-3).
//
// Run with: DATABASE_URL=postgres://... go test ./internal/repository/... -run TestBudgetRepository_CreateWithCategories_RollsBackOnCategoryFailure -tags integration -v
func TestBudgetRepository_CreateWithCategories_RollsBackOnCategoryFailure(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	// ── seed: one user ────────────────────────────────────────────────────────
	userID := uuid.New()
	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userID.String(), "cwc-rollback-"+userID.String()+"@example.com")
	require.NoError(t, err, "seed user")
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID) //nolint:errcheck
	})

	// ── prepare: budget ───────────────────────────────────────────────────────
	budgetID := uuid.New()
	budget := &domain.Budget{
		ID:             budgetID.String(),
		UserID:         userID.String(),
		Name:           "Atomic Test Budget",
		Period:         domain.BudgetPeriodMonthly,
		StartDate:      time.Now(),
		Amount:         1000.00,
		CurrencyCode:   "USD",
		IsActive:       true,
		ExpectedIncome: 1000.00,
		SeriesID:       budgetID.String(),
	}

	// ── prepare: two categories — cat2 has the SAME ID as cat1 (duplicate PK) ─
	catID := uuid.New()
	pfcPrimary := "FOOD_AND_DRINK"
	cat1 := &domain.BudgetCategory{
		ID:              catID.String(),
		BudgetID:        budgetID.String(),
		CategoryKind:    "pfc",
		PFCPrimary:      &pfcPrimary,
		AllocatedAmount: 200.00,
	}
	cat2 := &domain.BudgetCategory{
		ID:              catID.String(), // duplicate → triggers PK violation mid-loop
		BudgetID:        budgetID.String(),
		CategoryKind:    "pfc",
		PFCPrimary:      &pfcPrimary,
		AllocatedAmount: 300.00,
	}

	// ── call ──────────────────────────────────────────────────────────────────
	repo := NewBudgetRepository(pool)
	createErr := repo.CreateWithCategories(ctx, budget, []*domain.BudgetCategory{cat1, cat2})

	// ── assert: error was returned ────────────────────────────────────────────
	assert.Error(t, createErr, "CreateWithCategories must return error on mid-loop category failure")

	// ── assert: budget row was rolled back ────────────────────────────────────
	var budgetCount int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM budgets WHERE id = $1`, budgetID).Scan(&budgetCount)
	require.NoError(t, err)
	assert.Equal(t, 0, budgetCount, "budget row must be rolled back when category insert fails")

	// ── assert: no stray category rows ───────────────────────────────────────
	var categoryCount int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM budget_categories WHERE budget_id = $1`, budgetID).Scan(&categoryCount)
	require.NoError(t, err)
	assert.Equal(t, 0, categoryCount, "category rows must be rolled back when insert fails mid-loop")
}
