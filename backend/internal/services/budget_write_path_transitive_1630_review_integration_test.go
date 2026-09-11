//go:build integration

package services

// Issue #1630 review fix (HIGH): the transitive guard (domain.EscapedNestedChild /
// IsSubAllocationWithin, widened in 029e15d3 to recognize a GRANDCHILD of a PFC root via
// BudgetCategory.CustomGrandparentPFCPrimary) was populated on READ (budget_repository.go's "gp"
// join) but NOT on the WRITE-path candidate: budget_service.go's AddCategory populated only
// CustomParentID/CustomParentPFCPrimary via customCategoryRepo.GetByID, never
// CustomGrandparentPFCPrimary. An escaping grandchild nested under an UNBUDGETED intermediate
// parent therefore sailed past EscapedNestedChild and persisted — the exact double-count 029e15d3
// closed on the read path, reopened here on the write path.
//
// This test constructs that shape directly against a real Postgres: pfc root X (budgeted,
// 'category_only' — a containing shape), custom parent C (parent_pfc_primary = X, UNBUDGETED —
// no row in budget_categories at all), custom grandchild G (parent_id = C), and adds G to the
// budget with an escaping match_logic ('or'). Before the fix, AddCategory's in-memory candidate
// had CustomGrandparentPFCPrimary == nil (never populated), so EscapedNestedChild — which can only
// recognize the transitive pair via that field — saw no hierarchical relationship between G and X
// at all, and the add succeeded. After the fix it must be refused.
//
// Run with:
//
//	DATABASE_URL=postgres://... CACHE_REDIS_TEST_URL=... REDIS_TEST_URL=... \
//	  go test ./internal/services/... -tags integration -p 1 \
//	  -run TestBudgetWritePathTransitive_1630_Review -v

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

func TestBudgetWritePathTransitive_1630_Review_AddCategory_RefusesEscapingGrandchildUnderUnbudgetedParent(t *testing.T) {
	pool := connectIntegrationDB(t)
	ctx := context.Background()

	userID := uuid.New().String()
	budgetID := uuid.New().String()
	pfcPrimary := "FOOD_AND_DRINK"

	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|1630review|"+userID, "1630-review-"+userID+"@example.com")
	require.NoError(t, err, "seed user")

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM budget_categories WHERE budget_id = $1`, budgetID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM budgets WHERE id = $1`, budgetID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM custom_categories WHERE user_id = $1`, userID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM users WHERE id = $1`, userID)
	})

	budgetRepo := repository.NewBudgetRepository(pool)
	customCategoryRepo := repository.NewCustomCategoryRepository(pool)

	budget := &domain.Budget{
		ID:       budgetID,
		UserID:   userID,
		Name:     "1630 Review Transitive Write Path Budget",
		Period:   domain.BudgetPeriodMonthly,
		Amount:   1000.00,
		IsActive: true,
	}
	budget.StartDate = time.Now()
	require.NoError(t, budgetRepo.Create(ctx, budget), "seed budget")

	// X: the pfc root, budgeted with a CONTAINING match_logic — the shape that must roll up
	// (and therefore must be checked against) a transitive grandchild.
	rootCat := &domain.BudgetCategory{
		BudgetID:        budgetID,
		CategoryKind:    "pfc",
		PFCPrimary:      &pfcPrimary,
		MatchLogic:      domain.MatchLogicCategoryOnly,
		AllocatedAmount: 300.00,
	}
	require.NoError(t, budgetRepo.CreateCategory(ctx, rootCat), "seed root pfc category X")

	// C: an UNBUDGETED custom category nested under X via parent_pfc_primary. Deliberately no
	// budget_categories row for C at all — that's the "unbudgeted intermediate parent" this test
	// targets.
	catC := &domain.CustomCategory{UserID: userID, Name: "Intermediate C", ParentPFCPrimary: &pfcPrimary}
	require.NoError(t, customCategoryRepo.Create(ctx, catC), "seed custom parent C nested under X")

	// G: a custom grandchild nested under C via parent_id — two hops from X.
	catG := &domain.CustomCategory{UserID: userID, Name: "Grandchild G", ParentID: &catC.ID}
	require.NoError(t, customCategoryRepo.Create(ctx, catG), "seed custom grandchild G nested under C")

	budgetService := NewBudgetService(budgetRepo, customCategoryRepo, nil, slog.Default())

	// Add G to the budget with an ESCAPING match_logic ('or' is neither contained-nor-containing
	// in a way that stays inside X's clean containment envelope). Before the fix this succeeds
	// silently because AddCategory's candidate never learned it was X's transitive grandchild.
	_, err = budgetService.AddCategory(ctx, userID, budgetID, &domain.CreateBudgetCategoryRequest{
		CategoryKind:     "custom",
		CustomCategoryID: &catG.ID,
		MatchLogic:       domain.MatchLogicOr,
		AllocatedAmount:  25.00,
	})

	require.Error(t, err, "adding an escaping grandchild under an unbudgeted intermediate parent must be refused")
	assert.ErrorIs(t, err, domain.ErrEscapedNestedChild)

	cats, err := budgetRepo.GetCategoriesByBudgetID(ctx, budgetID)
	require.NoError(t, err)
	for i := range cats {
		require.NotEqual(t, catG.ID, derefOrEmpty(cats[i].CustomCategoryID),
			"the refused escaping grandchild must not have persisted")
	}
}

func derefOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
