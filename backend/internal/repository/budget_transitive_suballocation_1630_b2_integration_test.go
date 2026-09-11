//go:build integration

package repository

// Issue #1630 Task B2 — Commit 2: teaching the sub-allocation guards about transitive ancestry.
//
// Commit 1 widened buildCategoryClause's "pfc" branch to roll a PFC root's total up through a
// custom grandchild two hops away (pfc root X <- custom child C via parent_pfc_primary <- custom
// grandchild G via parent_id). Taken alone, that widening creates a NEW double-count: if X and G
// both have budget rows but C does not, X's own SpentAmount now reflects G's transactions (via
// the widened roll-up) WHILE G's own top-level row ALSO counts them, because
// IsSubAllocationWithin previously inspected only bc's DIRECT parent linkage
// (CustomParentID/CustomParentPFCPrimary) and had no way to recognize the transitive X<->G pair.
//
// This test proves the real repository — not an in-memory struct — actually populates the new
// CustomGrandparentPFCPrimary field from a live 2-hop join (GetCategoriesByBudgetID's `gp` join),
// and that Budget.TotalSpent() then agrees: G's spend is counted exactly once.
//
// Run with:
//
//	DATABASE_URL=postgres://... CACHE_REDIS_TEST_URL=... REDIS_TEST_URL=... \
//	  go test ./internal/repository/... -tags integration -p 1 \
//	  -run TestBudget_TotalSpent_TransitiveGrandchildNotDoubleCounted -v

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

func TestBudget_TotalSpent_TransitiveGrandchildNotDoubleCounted(t *testing.T) {
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
	`, userID, "test|"+userID, "claim1630-b2-c2-"+userID+"@example.com")
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

	// Budget rows on X (the pfc root) and G (the grandchild) — deliberately NONE on C, the
	// intermediate parent. spent_amount values below are what a real budget-sync recalculation
	// would produce post-Commit-1: X's widened roll-up already reaches G's transactions, so both
	// rows independently report the SAME $42.75 spend.
	const grandchildSpend = 42.75
	rootCatID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO budget_categories (id, budget_id, category_kind, pfc_primary, allocated_amount, spent_amount, match_logic)
		VALUES ($1, $2, 'pfc', $3, 300.00, $4, 'category_only')
	`, rootCatID, budgetID, pfcPrimary, grandchildSpend)
	require.NoError(t, err, "seed X's budget category row")

	grandchildCatID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO budget_categories (id, budget_id, category_kind, custom_category_id, allocated_amount, spent_amount, match_logic)
		VALUES ($1, $2, 'custom', $3, 50.00, $4, 'category_only')
	`, grandchildCatID, budgetID, grandchildID, grandchildSpend)
	require.NoError(t, err, "seed G's budget category row (C has NO budget row at all)")

	repo := NewBudgetRepository(tx)
	categories, err := repo.GetCategoriesByBudgetID(ctx, budgetID)
	require.NoError(t, err)
	require.Len(t, categories, 2)

	var grandchildCat *domain.BudgetCategory
	for i := range categories {
		if categories[i].CategoryKind == "custom" {
			grandchildCat = &categories[i]
		}
	}
	require.NotNil(t, grandchildCat, "the custom (grandchild) row must be present")
	require.NotNil(t, grandchildCat.CustomGrandparentPFCPrimary,
		"#1630 Task B2: GetCategoriesByBudgetID's new `gp` join must populate "+
			"CustomGrandparentPFCPrimary from the real 2-hop chain (G -> C -> X) — if this is nil, "+
			"the join is broken and IsSubAllocationWithin cannot recognize the transitive pair")
	assert.Equal(t, pfcPrimary, *grandchildCat.CustomGrandparentPFCPrimary)

	budget := &domain.Budget{ID: budgetID, UserID: userID, Categories: categories}
	assert.Equal(t, grandchildSpend, budget.TotalSpent(),
		"#1630 Task B2: G's spend must be counted EXACTLY ONCE. Before this commit's "+
			"IsSubAllocationWithin widening, this read 2*grandchildSpend — X's SpentAmount already "+
			"reflected G's transactions (Commit 1's widened roll-up) AND G's own top-level row "+
			"counted them again, because neither CustomParentID nor CustomParentPFCPrimary named X "+
			"directly (G's direct parent is the unbudgeted C).")
}
