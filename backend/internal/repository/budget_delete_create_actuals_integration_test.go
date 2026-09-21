//go:build integration

package repository

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/require"
)

// TestDeleteAndCreateCategoriesOnIncompleteActuals pins the two config-write paths
// the issue's reproduction table names but the earlier todos did not cover: DELETE
// and create. On an incomplete (ambiguous) ledger both must commit — the delete
// tombstones the row and writes its audit entry in the same transaction, and
// CreateWithCategories persists the budget and its categories — while the publish
// withholds and records the reason.
//
// The ambiguity is seeded with THREE tag_only lines matching the same tagged fact
// (not two) so that deleting one still leaves an overlapping pair: the delete's own
// publish must therefore still withhold, which is what makes the "reason persisted"
// assertion meaningful rather than vacuous.
func TestDeleteAndCreateCategoriesOnIncompleteActuals(t *testing.T) {
	food, travel, care := "FOOD_AND_DRINK", "TRAVEL", "PERSONAL_CARE"
	shared := []string{"shared"}
	f := newActualsTaxonomyFixture(t)

	first := f.addCategory("pfc", 5, func(c *domain.BudgetCategory) {
		c.PFCPrimary = &food
		c.Tags = shared
		c.MatchLogic = domain.MatchLogicTagOnly
	})
	second := f.addCategory("pfc", 6, func(c *domain.BudgetCategory) {
		c.PFCPrimary = &travel
		c.Tags = shared
		c.MatchLogic = domain.MatchLogicTagOnly
	})
	third := f.addCategory("pfc", 7, func(c *domain.BudgetCategory) {
		c.PFCPrimary = &care
		c.Tags = shared
		c.MatchLogic = domain.MatchLogicTagOnly
	})
	f.addTransaction(100, food, "USD", shared)

	// DELETE: the tombstone and the audit entry commit in the same transaction as the
	// withheld publish. A pre-todo-3 abort would have failed the whole delete.
	audit := NewAuditLogRepository(f.tx)
	require.NoError(t, f.repo.DeleteCategory(f.ctx, third.ID, f.budget.ID, audit),
		"a delete on an incomplete ledger must commit, not fail")

	var deletedAt *time.Time
	require.NoError(t, f.tx.QueryRow(f.ctx, `SELECT deleted_at FROM budget_categories WHERE id=$1`, third.ID).Scan(&deletedAt))
	require.NotNil(t, deletedAt, "the deleted category must be tombstoned")

	var auditCount int
	require.NoError(t, f.tx.QueryRow(f.ctx, `SELECT COUNT(*) FROM audit_logs WHERE action='budget_category.deleted' AND resource_id=$1`, third.ID).Scan(&auditCount))
	require.Equal(t, 1, auditCount, "the delete's audit entry must commit with the tombstone")

	require.Contains(t, reasonsOrEmpty(f.budgetRowReasons()), "overlapping_individual_lines",
		"the delete's withheld publish still records its reason")

	// The two surviving tag_only lines still overlap the same fact.
	require.Equal(t, 5.0, mustRowSpent(t, f, first.ID), "a withheld delete writes no spent_amount")
	require.Equal(t, 6.0, mustRowSpent(t, f, second.ID), "a withheld delete writes no spent_amount")

	// CREATE: CreateWithCategories commits the budget and its categories and persists the
	// reason the publish recorded. The new budget's current period includes the shared fact.
	created := &domain.Budget{
		ID:             uuid.NewString(),
		UserID:         f.user,
		Name:           "Created On Incomplete Actuals",
		Period:         domain.BudgetPeriodMonthly,
		StartDate:      time.Now().UTC().AddDate(0, 0, -1),
		Amount:         1000,
		ExpectedIncome: 1000,
		CurrencyCode:   "USD",
		IsActive:       true,
	}
	createdFood, createdTravel := "FOOD_AND_DRINK", "TRAVEL"
	categories := []*domain.BudgetCategory{
		{BudgetID: created.ID, CategoryKind: "pfc", PFCPrimary: &createdFood, Tags: shared, MatchLogic: domain.MatchLogicTagOnly, AllocatedAmount: 100},
		{BudgetID: created.ID, CategoryKind: "pfc", PFCPrimary: &createdTravel, Tags: shared, MatchLogic: domain.MatchLogicTagOnly, AllocatedAmount: 100},
	}
	require.NoError(t, f.repo.CreateWithCategories(f.ctx, created, categories),
		"CreateWithCategories on an incomplete ledger must commit")

	var createdReasons []string
	require.NoError(t, f.tx.QueryRow(f.ctx, `SELECT actuals_incomplete_reasons FROM budgets WHERE id=$1`, created.ID).Scan(&createdReasons))
	require.Contains(t, createdReasons, "overlapping_individual_lines",
		"CreateWithCategories must persist the reason the publish recorded")

	var createdCount int
	require.NoError(t, f.tx.QueryRow(f.ctx, `SELECT COUNT(*) FROM budget_categories WHERE budget_id=$1 AND deleted_at IS NULL`, created.ID).Scan(&createdCount))
	require.Equal(t, 2, createdCount, "CreateWithCategories must persist every category")
}

// mustRowSpent reads a category's spent_amount, failing the test on a read error.
func mustRowSpent(t *testing.T, f *actualsTaxonomyFixture, id string) float64 {
	t.Helper()
	spent, _ := f.row(id)
	return spent
}