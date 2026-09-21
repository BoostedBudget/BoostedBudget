//go:build integration

package repository

import (
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/require"
)

// TestRetainedActualsAcrossPeriodBoundary pins that a period rollover neither
// fabricates nor wipes a line's prior-period value. The test seeds the budget's
// current period explicitly (rather than relying on wall-clock rollover) and seeds
// each line's spent_amount with the concrete value the PRIOR period left behind.
//
// A RETAIN line must keep that exact value and report its reason; a WITHHOLD run
// must leave every line at its prior-period value. The failure-path assertion is the
// anti-fabrication invariant: no affected row is ever rewritten to 0, and every value
// observed is one the test seeded.
func TestRetainedActualsAcrossPeriodBoundary(t *testing.T) {
	food, travel := "FOOD_AND_DRINK", "TRAVEL"

	// pinCurrentPeriod sets the budget's current period to the current calendar month
	// and returns the first day of the PRIOR month, so a test can seed a fact the new
	// period must not count. The seeded spent_amounts are the values the prior period
	// left on the rows.
	pinCurrentPeriod := func(t *testing.T, f *actualsTaxonomyFixture) time.Time {
		t.Helper()
		now := time.Now().UTC()
		periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		f.budget.StartDate = periodStart
		_, err := f.tx.Exec(f.ctx, `UPDATE budgets SET start_date=$2 WHERE id=$1`, f.budget.ID, periodStart)
		require.NoError(t, err)
		return periodStart.AddDate(0, -1, 0)
	}

	t.Run("a retain line keeps its prior-period value and reports the reason", func(t *testing.T) {
		f := newActualsTaxonomyFixture(t)
		pinCurrentPeriod(t, f)

		goal := f.addGoal("USD")
		savingsLine := f.addCategory("savings", 42, func(c *domain.BudgetCategory) { c.SavingsGoalID = &goal })
		f.addContribution(goal, nil, 20, "legacy_unknown")

		_, reasons, withheld, err := f.publish(nil)
		require.NoError(t, err)
		require.False(t, withheld)
		require.Equal(t, []string{"legacy_unknown"}, reasons, "the retained line's reason is reported")

		spent, _ := f.row(savingsLine.ID)
		require.Equal(t, 42.0, spent, "a retained line keeps the concrete prior-period value")
		require.NotZero(t, spent, "a retained line is never fabricated to 0")
	})

	t.Run("a withhold run leaves every prior-period value", func(t *testing.T) {
		f := newActualsTaxonomyFixture(t)
		pinCurrentPeriod(t, f)

		foodLine := f.addCategory("pfc", 42, func(c *domain.BudgetCategory) { c.PFCPrimary = &food })
		travelLine := f.addCategory("pfc", 17, func(c *domain.BudgetCategory) { c.PFCPrimary = &travel })
		// A current-period fact in a foreign currency withholds the whole run.
		f.addTransaction(100, food, "EUR", nil)

		_, reasons, withheld, err := f.publish(nil)
		require.NoError(t, err)
		require.True(t, withheld)
		require.Equal(t, []string{"currency_mismatch"}, reasons)

		for id, want := range map[string]float64{foodLine.ID: 42, travelLine.ID: 17} {
			spent, _ := f.row(id)
			require.Equal(t, want, spent, "a withheld run leaves the prior-period value")
			require.NotZero(t, spent, "a withheld run never fabricates 0")
		}
	})
}