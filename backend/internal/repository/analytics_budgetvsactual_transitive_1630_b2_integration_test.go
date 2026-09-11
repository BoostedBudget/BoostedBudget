//go:build integration

package repository

// Issue #1630 Task B2 — GetBudgetVsActual's `is_sub_allocation` LATERAL is the SQL analogue of
// domain.BudgetCategory.IsSubAllocationWithin, and it has the identical one-hop limitation:
// before this commit it only recognized a direct parent (cc.parent_id / cc.parent_pfc_primary),
// never a transitive grandparent two hops away. Commit 1 widened buildCategoryClause so a
// PFC-root budget's spent_amount already reflects a custom grandchild's transactions; without the
// `gp` join added here, a budget on the root X plus a budget on the grandchild G (none on the
// intermediate C) double-counts G's spend in this aggregate exactly the way it did in
// Budget.TotalSpent() before the domain-level fix.
//
// Run with:
//
//	DATABASE_URL=postgres://... CACHE_REDIS_TEST_URL=... REDIS_TEST_URL=... \
//	  go test ./internal/repository/... -tags integration -p 1 \
//	  -run TestGetBudgetVsActual_TransitiveGrandchildNotDoubleCounted -v

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetBudgetVsActual_TransitiveGrandchildNotDoubleCounted(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	const pfcPrimary = "FOOD_AND_DRINK"
	userID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userID, "claim1630-b2-bva-"+userID+"@example.com")
	require.NoError(t, err, "seed user")

	childID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO custom_categories (id, user_id, name, parent_pfc_primary)
		VALUES ($1, $2, 'Eating Out', $3)
	`, childID, userID, pfcPrimary)
	require.NoError(t, err, "seed custom child C nested under pfc primary via parent_pfc_primary")

	grandchildID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO custom_categories (id, user_id, name, parent_id)
		VALUES ($1, $2, 'Coffee runs', $3)
	`, grandchildID, userID, childID)
	require.NoError(t, err, "seed custom grandchild G nested under C via parent_id")

	budgetID := uuid.New().String()
	now := time.Now()
	_, err = tx.Exec(ctx, `
		INSERT INTO budgets (id, user_id, name, period, start_date, amount, is_active, series_id)
		VALUES ($1, $2, 'Food Budget', 'monthly', $3, 1000.00, true, $1)
	`, budgetID, userID, now.Format("2006-01-02"))
	require.NoError(t, err, "seed budget")

	const grandchildSpend = 17.25
	_, err = tx.Exec(ctx, `
		INSERT INTO budget_categories (id, budget_id, category_kind, pfc_primary, allocated_amount, spent_amount, match_logic)
		VALUES ($1, $2, 'pfc', $3, 300.00, $4, 'category_only')
	`, uuid.New().String(), budgetID, pfcPrimary, grandchildSpend)
	require.NoError(t, err, "seed X's budget category row")

	_, err = tx.Exec(ctx, `
		INSERT INTO budget_categories (id, budget_id, category_kind, custom_category_id, allocated_amount, spent_amount, match_logic)
		VALUES ($1, $2, 'custom', $3, 50.00, $4, 'category_only')
	`, uuid.New().String(), budgetID, grandchildID, grandchildSpend)
	require.NoError(t, err, "seed G's budget category row (C has NO budget row at all)")

	repo := NewAnalyticsRepository(tx)
	rows, err := repo.GetBudgetVsActual(ctx, userID, "monthly")
	require.NoError(t, err)
	require.Len(t, rows, 1)

	assert.Equal(t, grandchildSpend, rows[0].SpentAmount,
		"#1630 Task B2: GetBudgetVsActual must count G's spend exactly once. Before the `gp` join "+
			"was added to its is_sub_allocation LATERAL, this read 2*grandchildSpend — X's own row "+
			"was never recognized as G's transitive grandparent, so both rows summed independently.")
}
