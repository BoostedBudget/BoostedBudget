//go:build integration

package repository

import (
	"context"
	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestBudgetConfigurationPublishesAllSiblingActualsAtomically(t *testing.T) {
	ctx := context.Background()
	tx, err := connectTestDB(t).Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	user, item, account := uuid.NewString(), uuid.NewString(), uuid.NewString()
	_, err = tx.Exec(ctx, `INSERT INTO users (id,clerk_user_id,email,subscription_tier,subscription_status,primary_region) VALUES ($1,$2,$3,'premium','active','iad')`, user, user, user+"@example.com")
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO linked_items (id,user_id,item_id,access_token_encrypted) VALUES ($1,$2,$3,'fixture')`, item, user, item)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO accounts (id,user_id,linked_item_id,plaid_account_id,name,type,currency_code) VALUES ($1,$2,$3,$4,'Fixture','depository','USD')`, account, user, item, account)
	require.NoError(t, err)
	food, travel, name := "FOOD_AND_DRINK", "TRAVEL", "Daily"
	for _, v := range []struct {
		primary string
		amount  float64
	}{{food, 100}, {travel, 40}} {
		_, err = tx.Exec(ctx, `INSERT INTO transactions (id,user_id,account_id,linked_item_id,source,amount,name,date,category_override_kind,pfc_primary_override) VALUES ($1,$2,$3,$4,'manual',$5,'Fixture',CURRENT_DATE,'pfc',$6)`, uuid.NewString(), user, account, item, v.amount, v.primary)
		require.NoError(t, err)
	}
	b := &domain.Budget{ID: uuid.NewString(), UserID: user, Name: "Current", Period: domain.BudgetPeriodMonthly, StartDate: time.Now().UTC().AddDate(0, 0, -1), Amount: 1000, ExpectedIncome: 1000, CurrencyCode: "USD", IsActive: true}
	group := &domain.BudgetCategory{BudgetID: b.ID, CategoryKind: "group", GroupName: &name, AllocatedAmount: 300, Members: []domain.BudgetGroupMember{{CategoryKind: "pfc", PFCPrimary: &food}}}
	other := &domain.BudgetCategory{BudgetID: b.ID, CategoryKind: "remainder", AllocatedAmount: 100}
	repo := NewBudgetRepository(tx)
	require.NoError(t, repo.CreateWithCategories(ctx, b, []*domain.BudgetCategory{group, other}))
	require.Equal(t, 100.0, group.SpentAmount)
	require.Equal(t, 40.0, other.SpentAmount)
	_, err = tx.Exec(ctx, `UPDATE budgets SET income_amount=200 WHERE id=$1`, b.ID)
	require.NoError(t, err)
	fresh, err := repo.(*budgetRepository).RecalculateBudgetActuals(ctx, user, b.ID, false)
	require.NoError(t, err)
	require.Equal(t, 200.0, fresh.IncomeAmount, "hierarchy refresh preserves locked income, not stale caller income")
	_, err = tx.Exec(ctx, `UPDATE budgets SET income_sources='[{"type":"pfc_primary","value":"INCOME"},{"type":"manual","value":"25"}]'::jsonb WHERE id=$1`, b.ID)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO transactions (id,user_id,account_id,linked_item_id,source,amount,name,date,category_override_kind,pfc_primary_override) VALUES ($1,$2,$3,$4,'manual',-80,'Income',CURRENT_DATE,'pfc','INCOME')`, uuid.NewString(), user, account, item)
	require.NoError(t, err)
	fresh, err = repo.(*budgetRepository).RecalculateBudgetActuals(ctx, user, b.ID, true)
	require.NoError(t, err)
	require.Equal(t, 105.0, fresh.IncomeAmount, "sync uses fresh sources and observed income in the financial snapshot")
	check := func(wantGroup, wantOther float64) {
		cats, err := repo.GetCategoriesByBudgetID(ctx, b.ID)
		require.NoError(t, err)
		for _, c := range cats {
			if c.ID == group.ID {
				require.Equal(t, wantGroup, c.SpentAmount)
			}
			if c.ID == other.ID {
				require.Equal(t, wantOther, c.SpentAmount)
			}
		}
	}
	single, err := repo.AddCategoryChecked(ctx, b.ID, &domain.BudgetCategory{BudgetID: b.ID, CategoryKind: "pfc", PFCPrimary: &food, AllocatedAmount: 100})
	require.NoError(t, err)
	require.Equal(t, 100.0, single.SpentAmount)
	check(0, 40)
	staleGroup := *group
	group, err = repo.UpdateCategoryChecked(ctx, b.ID, group.ID, func(c *domain.BudgetCategory) error {
		c.Members = []domain.BudgetGroupMember{{CategoryKind: "pfc", PFCPrimary: &travel}}
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 40.0, group.SpentAmount)
	check(40, 0)
	period := domain.CurrentBudgetActualsRange(b, time.Now())
	amount, err := NewTransactionRepository(tx, nil).GetSpentSumForBudgetCategory(ctx, user, &staleGroup, period.Start, period.ActualThrough)
	require.NoError(t, err)
	require.Equal(t, 40.0, amount, "saved target must be resolved in accounting snapshot, never replaced by stale caller metadata")
	require.NoError(t, repo.DeleteCategory(ctx, single.ID, b.ID, nil))
	check(40, 100)
	_, err = tx.Exec(ctx, `UPDATE transactions SET tags='["shared"]'::jsonb WHERE user_id=$1`, user)
	require.NoError(t, err)
	_, err = repo.AddCategoryChecked(ctx, b.ID, &domain.BudgetCategory{BudgetID: b.ID, CategoryKind: "pfc", PFCPrimary: &food, Tags: []string{"shared"}, MatchLogic: domain.MatchLogicTagOnly, AllocatedAmount: 100})
	require.NoError(t, err)
	transport := "TRANSPORTATION"
	before := sumBudgetCategorySpent(ctx, t, tx, b.ID)
	_, err = repo.AddCategoryChecked(ctx, b.ID, &domain.BudgetCategory{BudgetID: b.ID, CategoryKind: "pfc", PFCPrimary: &transport, Tags: []string{"shared"}, MatchLogic: domain.MatchLogicTagOnly, AllocatedAmount: 100})
	require.NoError(t, err, "an ambiguous ledger withholds the publish instead of failing the config write")
	require.Equal(t, before, sumBudgetCategorySpent(ctx, t, tx, b.ID), "ambiguous global totals must not publish individually known duplicated lines")
	// The removed hard-fail assertion (require.ErrorContains(err, "overlapping_individual_lines"))
	// is relocated, not deleted: the reason is asserted against the persisted diagnostic.
	var persistedReasons []string
	require.NoError(t, tx.QueryRow(ctx, `SELECT actuals_incomplete_reasons FROM budgets WHERE id=$1`, b.ID).Scan(&persistedReasons))
	require.Contains(t, persistedReasons, "overlapping_individual_lines", "the withheld reason is persisted on the budget row")
}

// sumBudgetCategorySpent is the aggregate form of the guarantee that an
// ambiguous ledger must not double-count: a withheld publish leaves the
// top-level sum exactly where it was.
func sumBudgetCategorySpent(ctx context.Context, t *testing.T, q Querier, budgetID string) float64 {
	t.Helper()
	var sum float64
	require.NoError(t, q.QueryRow(ctx, `SELECT COALESCE(SUM(spent_amount),0) FROM budget_categories WHERE budget_id=$1 AND deleted_at IS NULL`, budgetID).Scan(&sum))
	return sum
}
