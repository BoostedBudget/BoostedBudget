//go:build integration

package repository

import (
	"testing"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/require"
)

// TestUpdateCategoryCommitsWithDegradedActuals pins todo 8's config-writer
// contract on a degraded ledger: the allocation edit commits, the mutated
// category keeps its prior spent_amount (the publish wrote no line), every
// sibling is untouched, and the reasons are persisted on the budget row by the
// same transaction. Both degradation classes are covered — WITHHOLD (an
// unattributed overlap) and RETAIN (a savings goal absent from current
// activity).
func TestUpdateCategoryCommitsWithDegradedActuals(t *testing.T) {
	food, travel := "FOOD_AND_DRINK", "TRAVEL"
	shared := []string{"shared"}

	t.Run("withhold keeps the mutated category's prior spent", func(t *testing.T) {
		f := newActualsTaxonomyFixture(t)
		a := f.addCategory("pfc", 5, func(c *domain.BudgetCategory) {
			c.PFCPrimary = &food
			c.Tags = shared
			c.MatchLogic = domain.MatchLogicTagOnly
		})
		b := f.addCategory("pfc", 6, func(c *domain.BudgetCategory) {
			c.PFCPrimary = &travel
			c.Tags = shared
			c.MatchLogic = domain.MatchLogicTagOnly
		})
		f.addTransaction(100, food, "USD", shared)

		updated, err := f.repo.UpdateCategoryChecked(f.ctx, f.budget.ID, a.ID, func(c *domain.BudgetCategory) error {
			c.AllocatedAmount = 999
			return nil
		})
		require.NoError(t, err, "a degraded ledger must not fail the config write")
		require.Equal(t, 999.0, updated.AllocatedAmount, "the new allocation is returned")
		require.Equal(t, 5.0, updated.SpentAmount, "a withheld category keeps its prior spent_amount")

		var alloc, spent float64
		require.NoError(t, f.tx.QueryRow(f.ctx, `SELECT allocated_amount, spent_amount FROM budget_categories WHERE id=$1`, a.ID).Scan(&alloc, &spent))
		require.Equal(t, 999.0, alloc, "the new allocation is persisted")
		require.Equal(t, 5.0, spent, "the withheld line is never rewritten")

		bSpent, _ := f.row(b.ID)
		require.Equal(t, 6.0, bSpent, "a sibling line is untouched by the withheld publish")
		require.Equal(t, []string{"overlapping_individual_lines"}, reasonsOrEmpty(f.budgetRowReasons()), "the reasons are persisted in the config write's transaction")
	})

	t.Run("retain keeps the mutated category's prior spent", func(t *testing.T) {
		f := newActualsTaxonomyFixture(t)
		goal := f.addGoal("USD")
		savings := f.addCategory("savings", 7, func(c *domain.BudgetCategory) { c.SavingsGoalID = &goal })
		f.addCategory("pfc", 3, func(c *domain.BudgetCategory) { c.PFCPrimary = &food })
		f.addContribution(goal, nil, 20, "legacy_unknown")
		f.addTransaction(100, food, "USD", nil)
		_, err := f.tx.Exec(f.ctx, `UPDATE savings_goals SET deleted_at=NOW(), deleted_reason='user_deleted', deleted_by='user' WHERE id=$1`, goal)
		require.NoError(t, err)

		updated, err := f.repo.UpdateCategoryChecked(f.ctx, f.budget.ID, savings.ID, func(c *domain.BudgetCategory) error {
			c.AllocatedAmount = 321
			return nil
		})
		require.NoError(t, err, "a retained ledger must not fail the config write")
		require.Equal(t, 321.0, updated.AllocatedAmount)
		require.Equal(t, 7.0, updated.SpentAmount, "a retained category keeps its prior spent_amount")

		var alloc, spent float64
		require.NoError(t, f.tx.QueryRow(f.ctx, `SELECT allocated_amount, spent_amount FROM budget_categories WHERE id=$1`, savings.ID).Scan(&alloc, &spent))
		require.Equal(t, 321.0, alloc)
		require.Equal(t, 7.0, spent)
		require.Equal(t, []string{"savings_activity_missing"}, reasonsOrEmpty(f.budgetRowReasons()), "the retain reason is persisted in the config write's transaction")
	})
}

// TestCustomCategoryDeleteWithReassignmentSurvivesDegradedActuals pins the
// custom-category delete-with-reassignment path on a WITHHOLD-degraded ledger:
// the tombstone and audit commit, the budget line is tombstoned, and the
// reasons are persisted by the same transaction.
func TestCustomCategoryDeleteWithReassignmentSurvivesDegradedActuals(t *testing.T) {
	food, travel := "FOOD_AND_DRINK", "TRAVEL"
	shared := []string{"shared"}
	f := newActualsTaxonomyFixture(t)
	f.addCategory("pfc", 5, func(c *domain.BudgetCategory) {
		c.PFCPrimary = &food
		c.Tags = shared
		c.MatchLogic = domain.MatchLogicTagOnly
	})
	f.addCategory("pfc", 6, func(c *domain.BudgetCategory) {
		c.PFCPrimary = &travel
		c.Tags = shared
		c.MatchLogic = domain.MatchLogicTagOnly
	})
	// A remainder makes the budget eligible for the delete path's sibling refresh.
	f.addCategory("remainder", 0, nil)
	f.addTransaction(100, food, "USD", shared)

	custom := uuid.NewString()
	_, err := f.tx.Exec(f.ctx, `INSERT INTO custom_categories (id,user_id,name) VALUES ($1,$2,'Custom')`, custom, f.user)
	require.NoError(t, err)
	line := f.addCategory("custom", 40, func(c *domain.BudgetCategory) { c.CustomCategoryID = &custom })

	require.NoError(t, NewCustomCategoryRepository(f.tx).DeleteWithReassignment(f.ctx, f.user, custom, nil),
		"a degraded ledger must not fail the delete-with-reassignment")

	var customDeleted, lineDeleted bool
	require.NoError(t, f.tx.QueryRow(f.ctx, `SELECT deleted_at IS NOT NULL FROM custom_categories WHERE id=$1`, custom).Scan(&customDeleted))
	require.True(t, customDeleted, "the custom category is tombstoned")
	require.NoError(t, f.tx.QueryRow(f.ctx, `SELECT deleted_at IS NOT NULL FROM budget_categories WHERE id=$1`, line.ID).Scan(&lineDeleted))
	require.True(t, lineDeleted, "the budget line is tombstoned, not hard-deleted")
	require.Equal(t, []string{"overlapping_individual_lines"}, reasonsOrEmpty(f.budgetRowReasons()), "the reasons are persisted in the delete's transaction")
}