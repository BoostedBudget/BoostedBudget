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

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetCategoryByIDAndUser_Integration(t *testing.T) {
	pool := connectTestDB(t)
	repo := NewBudgetRepository(pool)
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
			INSERT INTO users (id, auth0_user_id, email, subscription_tier, subscription_status, primary_region)
			VALUES ($1, $2, $3, 'premium', 'active', 'us-east-1')
		`, u.id, "test|"+u.id.String(), "int-cat-owner-"+u.id.String()+"@example.com")
		require.NoError(t, err, "seed user %s", u.id)
	}

	// ── seed: budget owned by ownerID ────────────────────────────────────────────
	ownerBudgetID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO budgets (id, user_id, name, period, start_date, amount, is_active)
		VALUES ($1, $2, 'Owner Budget', 'monthly', NOW()::date, 1000.00, true)
	`, ownerBudgetID, ownerID)
	require.NoError(t, err, "seed owner budget")

	// ── seed: category within ownerID's budget ───────────────────────────────────
	ownedCatID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO budget_categories (id, budget_id, category_kind, allocated_amount, spent_amount)
		VALUES ($1, $2, 'pfc', 200.00, 0.00)
	`, ownedCatID, ownerBudgetID)
	require.NoError(t, err, "seed owned category")

	// ── seed: budget owned by otherID ────────────────────────────────────────────
	otherBudgetID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO budgets (id, user_id, name, period, start_date, amount, is_active)
		VALUES ($1, $2, 'Other Budget', 'monthly', NOW()::date, 500.00, true)
	`, otherBudgetID, otherID)
	require.NoError(t, err, "seed other budget")

	// ── seed: category within otherID's budget ───────────────────────────────────
	foreignCatID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO budget_categories (id, budget_id, category_kind, allocated_amount, spent_amount)
		VALUES ($1, $2, 'pfc', 100.00, 0.00)
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
		assert.Contains(t, err.Error(), "category not found or not owned by user")
	})

	t.Run("non-existent category ID returns error", func(t *testing.T) {
		cat, err := txRepo.GetCategoryByIDAndUser(ctx, uuid.New().String(), ownerID.String())
		require.Error(t, err, "should reject a non-existent category")
		assert.Nil(t, cat)
		assert.Contains(t, err.Error(), "category not found or not owned by user")
	})
}
