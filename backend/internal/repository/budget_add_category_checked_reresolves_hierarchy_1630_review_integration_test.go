//go:build integration

package repository

// Issue #1630 review fix (HIGH): AddCategoryChecked locks budgetID's row and re-reads its LIVE
// SIBLING categories under that lock, but — before this fix — never re-read the CANDIDATE's own
// custom-category hierarchy linkage. budget_service.go's AddCategory populates
// CustomParentID/CustomParentPFCPrimary/CustomGrandparentPFCPrimary via a plain
// customCategoryRepo.GetByID call BEFORE any lock is acquired. A concurrent reparent
// (custom_category_service.go's reparentCategoryChecked) can commit in the window between that
// read and AddCategoryChecked's lock acquisition, leaving the candidate validated against STALE
// (pre-reparent) hierarchy fields even though every SIBLING row it is compared against is fully
// fresh.
//
// This test reproduces the gap directly and deterministically (no goroutine race needed): build a
// candidate exactly as it would look immediately after being fetched pre-reparent (hierarchy
// fields nil/absent), then, BEFORE calling AddCategoryChecked, reparent the SAME custom category
// directly in the database — simulating a concurrent reparent that committed in the gap. If
// AddCategoryChecked trusts the candidate's (now-stale) fields, it sees no hierarchical
// relationship at all and wrongly allows an escaping pair. If it re-resolves under its own lock
// (resolveCandidateCustomHierarchyInTx), it sees the live parent and correctly refuses.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -tags integration -p 1 \
//	  -run TestAddCategoryChecked_1630_Review_ReResolvesCandidateHierarchyUnderLock -v

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

func TestAddCategoryChecked_1630_Review_ReResolvesCandidateHierarchyUnderLock(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	userID := uuid.New().String()
	budgetID := uuid.New().String()
	pfcPrimary := "FOOD_AND_DRINK"

	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|1630acr|"+userID, "1630-acr-"+userID+"@example.com")
	require.NoError(t, err, "seed user")

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM budget_categories WHERE budget_id = $1`, budgetID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM budgets WHERE id = $1`, budgetID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM custom_categories WHERE user_id = $1`, userID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM users WHERE id = $1`, userID)
	})

	repo := NewBudgetRepository(pool)

	budget := &domain.Budget{
		ID:       budgetID,
		UserID:   userID,
		Name:     "1630 Review AddCategoryChecked Staleness Budget",
		Period:   domain.BudgetPeriodMonthly,
		Amount:   1000.00,
		IsActive: true,
	}
	budget.StartDate = time.Now()
	require.NoError(t, repo.Create(ctx, budget), "seed budget")

	// X: the pfc root, budgeted with a CONTAINING match_logic.
	rootCat := &domain.BudgetCategory{
		BudgetID:        budgetID,
		CategoryKind:    "pfc",
		PFCPrimary:      &pfcPrimary,
		MatchLogic:      domain.MatchLogicCategoryOnly,
		AllocatedAmount: 300.00,
	}
	require.NoError(t, repo.CreateCategory(ctx, rootCat), "seed root pfc category X")

	// G: currently TOP-LEVEL (no parent at all).
	gID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO custom_categories (id, user_id, name)
		VALUES ($1, $2, 'Grandchild G')
	`, gID, userID)
	require.NoError(t, err, "seed top-level custom category G")

	// Build the candidate exactly as budget_service.go's AddCategory would have, fetching G's
	// hierarchy BEFORE any concurrent reparent — i.e. nil/absent, matching G's current top-level
	// state at the moment of that (hypothetical) pre-lock read.
	candidate := &domain.BudgetCategory{
		BudgetID:         budgetID,
		CategoryKind:     "custom",
		CustomCategoryID: &gID,
		MatchLogic:       domain.MatchLogicOr, // escaping once nested under a containing parent
		AllocatedAmount:  25.00,
		// Deliberately NOT set: CustomParentID / CustomParentPFCPrimary / CustomGrandparentPFCPrimary.
		// This is what the candidate looked like at the moment of the pre-lock read this test
		// simulates.
	}

	// Simulate a CONCURRENT reparent that commits in the gap between that pre-lock read and
	// AddCategoryChecked's own lock acquisition: G is nested directly under X via
	// parent_pfc_primary.
	_, err = pool.Exec(ctx, `UPDATE custom_categories SET parent_pfc_primary = $1 WHERE id = $2`, pfcPrimary, gID)
	require.NoError(t, err, "simulate a concurrent reparent of G under X, committing before AddCategoryChecked's lock")

	_, err = repo.AddCategoryChecked(ctx, budgetID, candidate)

	require.Error(t, err, "AddCategoryChecked must re-resolve candidate's hierarchy under its own lock and refuse the now-escaping pair, not trust the stale pre-lock candidate")
	assert.ErrorIs(t, err, domain.ErrEscapedNestedChild)

	cats, err := repo.GetCategoriesByBudgetID(ctx, budgetID)
	require.NoError(t, err)
	for i := range cats {
		if cats[i].CustomCategoryID != nil {
			assert.NotEqual(t, gID, *cats[i].CustomCategoryID, "the refused escaping candidate must not have persisted")
		}
	}
}
