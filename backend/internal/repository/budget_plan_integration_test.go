//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestBudgetPlanSnapshotAccounting(t *testing.T) {
	ctx := context.Background()
	tx, err := connectTestDB(t).Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck
	user, budget, item, account, goal := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	_, err = tx.Exec(ctx, `INSERT INTO users (id,clerk_user_id,email,subscription_tier,subscription_status,primary_region) VALUES ($1,$2,$3,'premium','active','iad')`, user, user, user+"@example.com")
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO linked_items (id,user_id,item_id,access_token_encrypted) VALUES ($1,$2,$3,'fixture')`, item, user, item)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO accounts (id,user_id,linked_item_id,plaid_account_id,name,type,currency_code) VALUES ($1,$2,$3,$4,'Fixture','depository','USD')`, account, user, item, account)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO budgets (id,user_id,name,period,start_date,end_date,amount,expected_income,income_sources,series_id) VALUES ($1,$2,'Plan','monthly','2026-09-01','2026-09-30',1000,1000,'[{"type":"pfc_primary","value":"INCOME"},{"type":"manual","value":"200"}]',$1)`, budget, user)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO savings_goals (id,user_id,account_id,name,target_amount,progress_basis,currency_code) VALUES ($1,$2,$3,'Goal',1000,'contributions','USD')`, goal, user, account)
	require.NoError(t, err)
	name, food, shopping := "Daily life", "FOOD_AND_DRINK", "GENERAL_MERCHANDISE"
	repo := NewBudgetRepository(tx)
	group, err := repo.AddCategoryChecked(ctx, budget, &domain.BudgetCategory{BudgetID: budget, CategoryKind: "group", GroupName: &name, AllocatedAmount: 300, Members: []domain.BudgetGroupMember{{CategoryKind: "pfc", PFCPrimary: &food}, {CategoryKind: "pfc", PFCPrimary: &shopping}}})
	require.NoError(t, err)
	_, err = repo.AddCategoryChecked(ctx, budget, &domain.BudgetCategory{BudgetID: budget, CategoryKind: "remainder", AllocatedAmount: 100})
	require.NoError(t, err)
	_, err = repo.AddCategoryChecked(ctx, budget, &domain.BudgetCategory{BudgetID: budget, CategoryKind: "savings", SavingsGoalID: &goal, AllocatedAmount: 200})
	require.NoError(t, err)
	foodTxn := uuid.NewString()
	for _, fixture := range []struct {
		id, primary string
		amount      float64
		pending     bool
	}{
		{foodTxn, food, 100, false}, {uuid.NewString(), shopping, 40, false}, {uuid.NewString(), "", 10, false},
		{uuid.NewString(), food, 20, true}, {uuid.NewString(), "TRANSFER_OUT", 30, false}, {uuid.NewString(), "INCOME", -500, false},
	} {
		_, err = tx.Exec(ctx, `INSERT INTO transactions (id,user_id,account_id,linked_item_id,source,amount,name,date,pfc_primary,pending) VALUES ($1,$2,$3,$4,'manual',$5,'Fixture','2026-09-10',NULLIF($6,''),$7)`, fixture.id, user, account, item, fixture.amount, fixture.primary, fixture.pending)
		require.NoError(t, err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO savings_goal_contributions (savings_goal_id,transaction_id,amount,source,status,occurred_on,flow_kind) VALUES ($1,$2,50,'manual','confirmed','2026-09-10','contribution')`, goal, foodTxn)
	require.NoError(t, err)
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	report, err := readBudgetPlanInSnapshot(ctx, tx, user, budget, now)
	require.NoError(t, err)
	require.NotNil(t, report)
	require.Equal(t, "complete", report.Completeness)
	require.Equal(t, 600.0, report.Totals.Allocated)
	require.Equal(t, 400.0, report.Totals.Unallocated)
	require.Equal(t, 150.0, *report.Totals.Actual)
	require.Equal(t, 500.0, *report.Income.Observed)
	require.Equal(t, 200.0, report.Income.ManualDeclared)
	require.Equal(t, "2026-09-12", *report.ActualThrough)
	require.Len(t, report.Lines, 3)
	for _, line := range report.Lines {
		if line.ID == group.ID {
			require.Equal(t, 90.0, *line.Actual)
			require.Equal(t, -210.0, *line.Variance)
		}
	}
	// A read must not write cached amounts or trigger automation.
	var persisted float64
	require.NoError(t, tx.QueryRow(ctx, `SELECT spent_amount FROM budget_categories WHERE id=$1 AND deleted_at IS NULL`, group.ID).Scan(&persisted))
	require.Zero(t, persisted)
	t.Run("custom deletion republishes group and Other", func(t *testing.T) {
		subtx, err := tx.Begin(ctx)
		require.NoError(t, err)
		defer subtx.Rollback(ctx) //nolint:errcheck
		_, err = subtx.Exec(ctx, `UPDATE budgets SET start_date=CURRENT_DATE,end_date=CURRENT_DATE WHERE id=$1`, budget)
		require.NoError(t, err)
		_, err = subtx.Exec(ctx, `UPDATE transactions SET date=CURRENT_DATE WHERE user_id=$1 AND deleted_at IS NULL`, user)
		require.NoError(t, err)
		_, err = subtx.Exec(ctx, `UPDATE savings_goal_contributions SET occurred_on=CURRENT_DATE WHERE savings_goal_id=$1 AND deleted_at IS NULL`, goal)
		require.NoError(t, err)
		custom := uuid.NewString()
		_, err = subtx.Exec(ctx, `INSERT INTO custom_categories (id,user_id,name) VALUES ($1,$2,'Temporary member')`, custom, user)
		require.NoError(t, err)
		_, err = subtx.Exec(ctx, `UPDATE transactions SET category_override_kind='custom',custom_category_override_id=$2 WHERE id=$1 AND deleted_at IS NULL`, foodTxn, custom)
		require.NoError(t, err)
		br := NewBudgetRepository(subtx)
		updated, err := br.UpdateCategoryChecked(ctx, budget, group.ID, func(c *domain.BudgetCategory) error {
			c.Members = []domain.BudgetGroupMember{{CategoryKind: "custom", CustomCategoryID: &custom}}
			return nil
		})
		require.NoError(t, err)
		require.Equal(t, 50.0, updated.SpentAmount)
		require.NoError(t, NewCustomCategoryRepository(subtx).DeleteWithReassignment(ctx, user, custom, nil))
		categories, err := br.GetCategoriesByBudgetID(ctx, budget)
		require.NoError(t, err)
		for _, category := range categories {
			if category.ID == group.ID {
				require.Zero(t, category.SpentAmount)
			}
			if category.CategoryKind == "remainder" {
				require.Equal(t, 100.0, category.SpentAmount)
			}
		}
	})
	_, err = readBudgetPlanInSnapshot(ctx, tx, uuid.NewString(), budget, now)
	require.ErrorIs(t, err, domain.ErrBudgetNotFound)
	_, err = tx.Exec(ctx, `UPDATE budgets SET is_active=false WHERE id=$1`, budget)
	require.NoError(t, err)
	report, err = readBudgetPlanInSnapshot(ctx, tx, user, budget, now)
	require.NoError(t, err)
	require.Equal(t, "inactive", report.Eligibility)
	require.Nil(t, report.Totals.Actual)
	require.Equal(t, 600.0, report.Totals.Allocated)
	_, err = tx.Exec(ctx, `UPDATE budgets SET is_active=true WHERE id=$1`, budget)
	require.NoError(t, err)
	t.Run("pending savings is not posted activity", func(t *testing.T) {
		_, err := tx.Exec(ctx, `UPDATE transactions SET pending=true WHERE id=$1`, foodTxn)
		require.NoError(t, err)
		report, err := readBudgetPlanInSnapshot(ctx, tx, user, budget, now)
		require.NoError(t, err)
		for _, line := range report.Lines {
			if line.Kind == "savings" {
				require.NotNil(t, line.Actual)
				require.Zero(t, *line.Actual)
			}
		}
		_, err = tx.Exec(ctx, `UPDATE transactions SET pending=false WHERE id=$1`, foodTxn)
		require.NoError(t, err)
	})
	t.Run("income follows custom ancestry and deduplicates sources", func(t *testing.T) {
		parent, child := uuid.NewString(), uuid.NewString()
		_, err := tx.Exec(ctx, `INSERT INTO custom_categories (id,user_id,name,parent_pfc_primary) VALUES ($1,$2,'Income parent','INCOME')`, parent, user)
		require.NoError(t, err)
		_, err = tx.Exec(ctx, `INSERT INTO custom_categories (id,user_id,name,parent_id) VALUES ($1,$2,'Income child',$3)`, child, user, parent)
		require.NoError(t, err)
		for _, custom := range []string{parent, child} {
			_, err = tx.Exec(ctx, `UPDATE transactions SET category_override_kind='custom',custom_category_override_id=$2,tags='["salary"]' WHERE user_id=$1 AND amount<0 AND deleted_at IS NULL`, user, custom)
			require.NoError(t, err)
			for _, sources := range [][]domain.IncomeSource{{{Type: domain.IncomeSourceTypePFCPrimary, Value: "INCOME"}}, {{Type: domain.IncomeSourceTypePFCPrimary, Value: "INCOME"}, {Type: domain.IncomeSourceTypeTag, Value: "salary"}}} {
				observed, _, err := readBudgetObservedIncome(ctx, tx, user, sources, now.AddDate(0, 0, -11), now)
				require.NoError(t, err)
				require.Equal(t, 500.0, observed)
			}
		}
		observed, _, err := readBudgetObservedIncome(ctx, tx, user, nil, now.AddDate(0, 0, -11), now)
		require.NoError(t, err)
		require.Zero(t, observed)
	})
	for _, scenario := range []struct {
		name, currency string
		contribution   float64
		reason         string
	}{
		{"linked currency differs", "EUR", 50, "mixed_currency"},
		{"allocation exceeds transaction", "USD", 150, "savings_allocation_exceeds_transaction"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			_, err = tx.Exec(ctx, `UPDATE accounts SET currency_code=$2 WHERE id=$1`, account, scenario.currency)
			require.NoError(t, err)
			_, err = tx.Exec(ctx, `UPDATE savings_goal_contributions SET amount=$2 WHERE savings_goal_id=$1 AND deleted_at IS NULL`, goal, scenario.contribution)
			require.NoError(t, err)
			report, err = readBudgetPlanInSnapshot(ctx, tx, user, budget, now)
			require.NoError(t, err)
			require.Equal(t, "partial", report.Completeness)
			require.Nil(t, report.Totals.Actual)
			require.Contains(t, report.Reasons, scenario.reason)
			for _, line := range report.Lines {
				if line.Kind == "savings" {
					require.Nil(t, line.Actual, "unverifiable savings activity must not appear known")
				}
			}
		})
	}
	t.Run("custom line labels are resolved in the snapshot", func(t *testing.T) {
		custom := uuid.NewString()
		_, err := tx.Exec(ctx, `INSERT INTO custom_categories (id,user_id,name) VALUES ($1,$2,'Weekend workshops')`, custom, user)
		require.NoError(t, err)
		_, err = tx.Exec(ctx, `INSERT INTO budget_categories (budget_id,category_kind,custom_category_id,allocated_amount) VALUES ($1,'custom',$2,10)`, budget, custom)
		require.NoError(t, err)
		report, err := readBudgetPlanInSnapshot(ctx, tx, user, budget, now)
		require.NoError(t, err)
		var labels []string
		for _, line := range report.Lines {
			labels = append(labels, line.Label)
		}
		require.Contains(t, labels, "Weekend workshops")
	})
	t.Run("foreign currency unbudgeted amount is unavailable", func(t *testing.T) {
		_, err := tx.Exec(ctx, `UPDATE budget_categories SET deleted_at=NOW(),deleted_reason='user_deleted',deleted_by='user' WHERE budget_id=$1 AND category_kind='remainder' AND deleted_at IS NULL`, budget)
		require.NoError(t, err)
		_, err = tx.Exec(ctx, `UPDATE accounts SET currency_code='EUR' WHERE id=$1`, account)
		require.NoError(t, err)
		report, err := readBudgetPlanInSnapshot(ctx, tx, user, budget, now)
		require.NoError(t, err)
		require.Nil(t, report.Totals.Unbudgeted)
		_, err = NewTransactionRepository(tx, nil).GetSpentSumForBudgetCategory(ctx, user, &domain.BudgetCategory{ID: "unbudgeted", BudgetID: budget, CategoryKind: "unbudgeted"}, now.AddDate(0, 0, -11), now)
		require.ErrorContains(t, err, "mixed_currency", "saved USD budget cannot report a scalar EUR amount")
		var found bool
		for _, line := range report.Lines {
			if line.Kind == "unbudgeted" {
				found = true
				require.Nil(t, line.Actual)
				require.Contains(t, line.Reasons, "mixed_currency")
			}
		}
		require.True(t, found)
	})
}
