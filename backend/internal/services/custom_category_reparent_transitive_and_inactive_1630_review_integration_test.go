//go:build integration

package services

// Issue #1630 review fix (HIGH + follow-up): two gaps found in reparentCategoryChecked by an
// independent adversarial review after the original 029e15d3 / B3b fixes landed.
//
// (1) Transitive overlay: the reparent's own PROPOSED-hierarchy overlay (reparentCategoryChecked,
// custom_category_service.go) set CustomParentID/CustomParentPFCPrimary from the request's new
// parent, but never CustomGrandparentPFCPrimary — so reparenting a category onto an UNBUDGETED
// intermediate custom parent that itself nests under a budgeted PFC root left the guard blind to
// the transitive pair, exactly like AddCategory's write-path gap.
//
// (2) Active-only scope: reparentCategoryChecked queried GetActiveByUserID — only ACTIVE budgets.
// An INACTIVE budget still holds its own budget_categories rows and can be reactivated later by a
// plain is_active update with no hierarchy validation of its own. Reparenting under a
// budgeted-but-INACTIVE PFC root, then reactivating, would silently commit the escaping pair the
// guard exists to prevent, because the guard's own query never saw that budget at reparent time.
//
// Run with:
//
//	DATABASE_URL=postgres://... CACHE_REDIS_TEST_URL=... REDIS_TEST_URL=... \
//	  go test ./internal/services/... -tags integration -p 1 \
//	  -run TestCustomCategoryReparent_1630_ReviewFollowup -v

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// TestCustomCategoryReparent_1630_ReviewFollowup_RefusesTransitiveEscapeOntoUnbudgetedParent
// reparents category G onto an UNBUDGETED custom parent C that itself nests under a budgeted,
// containing PFC root X. G's own match_logic is escaping ('or'). Before the overlay fix, the
// reparent's candidate never learned it was X's transitive grandchild (CustomGrandparentPFCPrimary
// stayed nil), so the guard saw no relationship between G and X at all, and the reparent
// succeeded.
func TestCustomCategoryReparent_1630_ReviewFollowup_RefusesTransitiveEscapeOntoUnbudgetedParent(t *testing.T) {
	pool := connectIntegrationDB(t)
	ctx := context.Background()

	userID := uuid.New().String()
	budgetID := uuid.New().String()
	pfcPrimary := "FOOD_AND_DRINK"

	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|1630rf1|"+userID, "1630-rf1-"+userID+"@example.com")
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
		Name:     "1630 Review Followup Transitive Reparent Budget",
		Period:   domain.BudgetPeriodMonthly,
		Amount:   1000.00,
		IsActive: true,
	}
	budget.StartDate = time.Now()
	require.NoError(t, budgetRepo.Create(ctx, budget), "seed budget")

	rootCat := &domain.BudgetCategory{
		BudgetID:        budgetID,
		CategoryKind:    "pfc",
		PFCPrimary:      &pfcPrimary,
		MatchLogic:      domain.MatchLogicCategoryOnly,
		AllocatedAmount: 300.00,
	}
	require.NoError(t, budgetRepo.CreateCategory(ctx, rootCat), "seed root pfc category X")

	// C: an UNBUDGETED custom category nested under X via parent_pfc_primary.
	catC := &domain.CustomCategory{UserID: userID, Name: "Intermediate C", ParentPFCPrimary: &pfcPrimary}
	require.NoError(t, customCategoryRepo.Create(ctx, catC), "seed custom parent C nested under X")

	// G: currently top-level, budgeted with an ESCAPING match_logic ('or'). Reparenting it under
	// C makes it X's transitive grandchild.
	catG := &domain.CustomCategory{UserID: userID, Name: "Grandchild G"}
	require.NoError(t, customCategoryRepo.Create(ctx, catG), "seed top-level custom category G")
	bcG := &domain.BudgetCategory{
		BudgetID:         budgetID,
		CategoryKind:     "custom",
		CustomCategoryID: &catG.ID,
		MatchLogic:       domain.MatchLogicOr,
		AllocatedAmount:  25.00,
	}
	require.NoError(t, budgetRepo.CreateCategory(ctx, bcG), "seed bcG at top level")

	svc := NewCustomCategoryService(customCategoryRepo)
	svc.SetBudgetCategoryRepository(budgetRepo)

	req := &domain.UpdateCustomCategoryRequest{ParentID: &catC.ID}
	_, err = svc.UpdateCategory(ctx, userID, catG.ID, req)

	require.Error(t, err, "reparenting G onto C (an unbudgeted transitive child of budgeted, containing root X) must be refused")
	assert.ErrorIs(t, err, domain.ErrEscapedNestedChild)

	persisted, err := customCategoryRepo.GetByID(ctx, catG.ID)
	require.NoError(t, err)
	assert.Nil(t, persisted.ParentID, "the refused reparent must not have persisted")
}

// TestCustomCategoryReparent_1630_ReviewFollowup_ChecksInactiveBudgetsToo reparents a category
// onto a parent whose OWN budgeted row lives in an INACTIVE budget. Before the fix,
// reparentCategoryChecked's GetActiveByUserID query never saw this budget at all, so the reparent
// succeeded unconditionally — leaving an escaping pair that a later plain is_active=true flip
// would silently reactivate with no hierarchy check of its own.
func TestCustomCategoryReparent_1630_ReviewFollowup_ChecksInactiveBudgetsToo(t *testing.T) {
	pool := connectIntegrationDB(t)
	ctx := context.Background()

	userID := uuid.New().String()
	budgetID := uuid.New().String()

	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|1630rf2|"+userID, "1630-rf2-"+userID+"@example.com")
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

	// The budget is INACTIVE from the start.
	budget := &domain.Budget{
		ID:       budgetID,
		UserID:   userID,
		Name:     "1630 Review Followup Inactive Budget",
		Period:   domain.BudgetPeriodMonthly,
		Amount:   1000.00,
		IsActive: false,
	}
	budget.StartDate = time.Now()
	require.NoError(t, budgetRepo.Create(ctx, budget), "seed inactive budget")

	// catP: the reparent's target parent, budgeted in the INACTIVE budget with a non-containing
	// match_logic ('and') — nesting a category_only child under it escapes containment.
	catP := &domain.CustomCategory{UserID: userID, Name: "Parent P"}
	require.NoError(t, customCategoryRepo.Create(ctx, catP), "seed catP")
	bcP := &domain.BudgetCategory{
		BudgetID:         budgetID,
		CategoryKind:     "custom",
		CustomCategoryID: &catP.ID,
		MatchLogic:       domain.MatchLogicAnd,
		Tags:             []string{"splurge"},
		AllocatedAmount:  200.00,
	}
	require.NoError(t, budgetRepo.CreateCategory(ctx, bcP), "seed bcP in the inactive budget")

	// catA: currently top-level, will be reparented under catP.
	catA := &domain.CustomCategory{UserID: userID, Name: "Child A"}
	require.NoError(t, customCategoryRepo.Create(ctx, catA), "seed catA")
	bcA := &domain.BudgetCategory{
		BudgetID:         budgetID,
		CategoryKind:     "custom",
		CustomCategoryID: &catA.ID,
		MatchLogic:       domain.MatchLogicCategoryOnly,
		AllocatedAmount:  50.00,
	}
	require.NoError(t, budgetRepo.CreateCategory(ctx, bcA), "seed bcA in the inactive budget")

	svc := NewCustomCategoryService(customCategoryRepo)
	svc.SetBudgetCategoryRepository(budgetRepo)

	req := &domain.UpdateCustomCategoryRequest{ParentID: &catP.ID}
	_, err = svc.UpdateCategory(ctx, userID, catA.ID, req)

	require.Error(t, err, "reparenting under a budgeted-but-INACTIVE non-containing parent must still be refused")
	assert.ErrorIs(t, err, domain.ErrEscapedNestedChild)

	persisted, err := customCategoryRepo.GetByID(ctx, catA.ID)
	require.NoError(t, err)
	assert.Nil(t, persisted.ParentID, "the refused reparent must not have persisted, even against an inactive budget")
}
