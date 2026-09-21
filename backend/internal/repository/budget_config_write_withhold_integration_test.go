//go:build integration

package repository

import (
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/require"
)

// TestConfigWriteWithholdsOnOverlapAndReports is the repository-level equivalent
// of the issue's PUT: on an ambiguous ledger the config edit must succeed (no
// error), persist its new allocation, report the reason, and leave every other
// category's spent_amount and updated_at untouched — never fabricating a value
// and never double-counting the aggregate.
//
// Every line is seeded via raw INSERT with an explicit updated_at one hour
// before the transaction, and the ambiguity is created by the seed itself (three
// tag_only lines match the same tagged facts) rather than by AddCategoryChecked.
// That matters: AddCategoryChecked publishes, and under the very regression this
// test targets (a withheld publish that writes anyway) those setup publishes
// would stamp NOW() on every row, making the pre-state equal to the post-state
// and the "was this row written?" assertion vacuous. With a raw-INSERT seed the
// mutation's publish is the only publish, so a write is detectable.
func TestConfigWriteWithholdsOnOverlapAndReports(t *testing.T) {
	f := newActualsTaxonomyFixture(t)
	food, travel, care := "FOOD_AND_DRINK", "TRAVEL", "PERSONAL_CARE"
	shared := []string{"shared"}
	line := f.addCategory("pfc", 5, func(c *domain.BudgetCategory) {
		c.PFCPrimary = &care
		c.Tags = shared
		c.MatchLogic = domain.MatchLogicTagOnly
	})
	other := f.addCategory("remainder", 100, nil)
	first := f.addCategory("pfc", 6, func(c *domain.BudgetCategory) {
		c.PFCPrimary = &food
		c.Tags = shared
		c.MatchLogic = domain.MatchLogicTagOnly
	})
	second := f.addCategory("pfc", 7, func(c *domain.BudgetCategory) {
		c.PFCPrimary = &travel
		c.Tags = shared
		c.MatchLogic = domain.MatchLogicTagOnly
	})
	f.addTransaction(100, food, "USD", shared)
	f.addTransaction(40, travel, "USD", shared)

	type rowState struct {
		spent     float64
		updatedAt time.Time
	}
	pre := map[string]rowState{}
	for _, id := range []string{line.ID, other.ID, first.ID, second.ID} {
		spent, updatedAt := f.row(id)
		pre[id] = rowState{spent: spent, updatedAt: updatedAt}
	}
	beforeSum := f.sumSpent()

	mutated, err := f.repo.UpdateCategoryChecked(f.ctx, f.budget.ID, line.ID, func(c *domain.BudgetCategory) error {
		c.AllocatedAmount = 777
		return nil
	})
	require.NoError(t, err, "a config edit on an ambiguous ledger must not fail")
	require.Equal(t, 777.0, mutated.AllocatedAmount, "the new allocation is returned")

	fresh, err := f.repo.GetByID(f.ctx, f.budget.ID)
	require.NoError(t, err)
	var persistedAllocation float64
	for _, c := range fresh.Categories {
		if c.ID == line.ID {
			persistedAllocation = c.AllocatedAmount
		}
	}
	require.Equal(t, 777.0, persistedAllocation, "the new allocation is persisted")
	require.Contains(t, reasonsOrEmpty(fresh.ActualsIncompleteReasons), "overlapping_individual_lines",
		"the user-facing read returns the persisted reason")

	for _, id := range []string{other.ID, first.ID, second.ID} {
		spent, updatedAt := f.row(id)
		require.Equal(t, pre[id].spent, spent, "a withheld publish must not write spent_amount for %s", id)
		requireSameInstant(t, pre[id].updatedAt, updatedAt, "a withheld publish must not touch updated_at for "+id)
	}
	require.Equal(t, beforeSum, f.sumSpent(), "a withheld publish must not change the top-level sum")
}
