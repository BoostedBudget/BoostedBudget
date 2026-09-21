//go:build integration

package repository

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type countingBudgetAccountingQuerier struct {
	Querier
	queries int
}

func (q *countingBudgetAccountingQuerier) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	q.queries++
	return q.Querier.Query(ctx, sql, args...)
}

func TestBudgetExpenseLedgerPostedNonTransferAndPartialSavings(t *testing.T) {
	ctx := context.Background()
	pool := connectTestDB(t)
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	user, item, account, goal := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	_, err = tx.Exec(ctx, `INSERT INTO users (id,clerk_user_id,email,subscription_tier,subscription_status,primary_region) VALUES ($1,$2,$3,'premium','active','iad')`, user, "clerk_"+user, user+"@example.com")
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO linked_items (id,user_id,item_id,access_token_encrypted) VALUES ($1,$2,$3,'fixture')`, item, user, "item_"+item)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO accounts (id,user_id,linked_item_id,plaid_account_id,name,type,currency_code) VALUES ($1,$2,$3,$4,'Fixture','depository','USD')`, account, user, item, "account_"+account)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO savings_goals (id,user_id,account_id,name,target_amount,progress_basis) VALUES ($1,$2,$3,'Fixture',1000,'contributions')`, goal, user, account)
	require.NoError(t, err)
	food := "FOOD_AND_DRINK"
	primary := "TRANSFER_OUT"
	ids := []string{}
	for _, v := range []struct {
		pending bool
		primary string
	}{{false, food}, {true, food}, {false, primary}} {
		id := uuid.NewString()
		ids = append(ids, id)
		_, err = tx.Exec(ctx, `INSERT INTO transactions (id,user_id,account_id,linked_item_id,source,amount,name,date,pending,category_override_kind,pfc_primary_override) VALUES ($1,$2,$3,$4,'manual',100,'Fixture','2026-09-12',$5,'pfc',$6)`, id, user, account, item, v.pending, v.primary)
		require.NoError(t, err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO savings_goal_contributions (savings_goal_id,transaction_id,amount,source,status,occurred_on,flow_kind) VALUES ($1,$2,30,'manual','confirmed','2026-09-12','contribution')`, goal, ids[0])
	require.NoError(t, err)
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	ledger, err := ReadBudgetExpenseLedger(ctx, tx, user, []domain.BudgetCategory{{ID: "food", CategoryKind: "pfc", PFCPrimary: &food}, {ID: "other", CategoryKind: "remainder"}}, start, start.AddDate(0, 1, -1))
	require.NoError(t, err)
	require.Equal(t, 70.0, *ledger.Lines["food"].Amount)
	require.Equal(t, []string{ids[0]}, ledger.Lines["food"].TransactionIDs)
	require.Equal(t, 0.0, *ledger.Lines["other"].Amount)
	counted := &countingBudgetAccountingQuerier{Querier: tx}
	combined, savings, err := ReadBudgetAccountingInputs(ctx, counted, user, []domain.BudgetCategory{{ID: "food", CategoryKind: "pfc", PFCPrimary: &food}}, start, start.AddDate(0, 1, -1))
	require.NoError(t, err)
	require.Equal(t, 1, counted.queries, "expense weights and savings activity must share one SQL statement snapshot")
	require.Equal(t, 70.0, *combined.Lines["food"].Amount)
	require.Len(t, savings, 1)
	require.Equal(t, 30.0, *savings[0].ActualAmount)
	t.Run("tombstoned goal retains expense weight but not current activity", func(t *testing.T) {
		subtx, err := tx.Begin(ctx)
		require.NoError(t, err)
		defer subtx.Rollback(ctx) //nolint:errcheck

		_, err = subtx.Exec(ctx, `UPDATE savings_goals SET deleted_at=NOW(),deleted_reason='user_deleted',deleted_by='user' WHERE id=$1`, goal)
		require.NoError(t, err)

		retained, currentSavings, err := ReadBudgetAccountingInputs(ctx, subtx, user, []domain.BudgetCategory{{ID: "food", CategoryKind: "pfc", PFCPrimary: &food}}, start, start.AddDate(0, 1, -1))
		require.NoError(t, err)
		require.Equal(t, 70.0, *retained.Lines["food"].Amount)
		require.Empty(t, currentSavings, "a deleted goal must remain absent from current savings activity")

		require.NoError(t, NewSavingsGoalRepository(subtx).PurgeGoal(ctx, goal, user, nil))
		purged, _, err := ReadBudgetAccountingInputs(ctx, subtx, user, []domain.BudgetCategory{{ID: "food", CategoryKind: "pfc", PFCPrimary: &food}}, start, start.AddDate(0, 1, -1))
		require.NoError(t, err)
		require.Equal(t, 100.0, *purged.Lines["food"].Amount, "explicit purge removes the retained expense weight")
	})
	_, err = tx.Exec(ctx, `UPDATE savings_goals SET currency_code='EUR' WHERE id=$1`, goal)
	require.NoError(t, err)
	foreign, err := ReadBudgetExpenseLedger(ctx, tx, user, []domain.BudgetCategory{{ID: "food", CategoryKind: "pfc", PFCPrimary: &food}}, start, start.AddDate(0, 1, -1))
	require.NoError(t, err)
	require.Nil(t, foreign.Lines["food"].Amount, "an unlinked foreign goal cannot reduce expense units")
	require.Contains(t, foreign.Reasons, "mixed_currency")
	_, err = tx.Exec(ctx, `UPDATE savings_goals SET currency_code='USD' WHERE id=$1`, goal)
	require.NoError(t, err)
	spent, err := NewTransactionRepository(tx, nil).GetSpentSumForBudgetCategory(ctx, user, &domain.BudgetCategory{ID: "food", CategoryKind: "pfc", PFCPrimary: &food}, start, start.AddDate(0, 1, -1))
	require.NoError(t, err)
	require.Equal(t, 70.0, spent, "the existing sum consumer must use the shared posted, contribution-weighted matcher")
	count, total, err := NewTransactionRepository(tx, nil).GetMatchingTransactionStats(ctx, user, "pfc", &food, nil, nil, nil, domain.MatchLogicCategoryOnly, start, start.AddDate(0, 1, -1))
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.Equal(t, 70.0, total)
	items, count, total, err := NewTransactionRepository(tx, nil).GetTransactionsForBudgetCategory(ctx, user, &domain.BudgetCategory{ID: "food", CategoryKind: "pfc", PFCPrimary: &food}, start, start.AddDate(0, 1, -1), 10, 0)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.NotNil(t, items[0].BudgetExpenseAmount)
	require.Equal(t, 70.0, *items[0].BudgetExpenseAmount, "drilldown exposes the net amount counted while preserving gross transaction amount")
	require.Equal(t, 1, count)
	require.Equal(t, 70.0, total)
	_, direct, directAmount, _, _, err := NewTransactionRepository(tx, nil).GetCategoryMatchBreakdown(ctx, user, "pfc", &food, nil, nil, nil, domain.MatchLogicCategoryOnly, start, start.AddDate(0, 1, -1))
	require.NoError(t, err)
	require.Equal(t, 1, direct)
	require.Equal(t, 70.0, directAmount)
	preview, err := NewTransactionRepository(tx, nil).(*transactionRepository).PreviewBudgetCategory(ctx, user, &domain.CategoryPreviewRequest{CategoryKind: "group", GroupName: ptrString("Daily"), Members: []domain.BudgetGroupMember{{CategoryKind: "pfc", PFCPrimary: &food}}, DraftCategories: []domain.BudgetCategory{{ID: "preview", CategoryKind: "pfc", PFCPrimary: &food}}, MatchLogic: domain.MatchLogicCategoryOnly, StartDate: domain.FlexibleDate(start), EndDate: domain.FlexibleDate(start.AddDate(0, 1, -1))})
	require.NoError(t, err)
	require.Zero(t, preview.TotalAmount, "draft sibling individual takes priority")
	preview, err = NewTransactionRepository(tx, nil).(*transactionRepository).PreviewBudgetCategory(ctx, user, &domain.CategoryPreviewRequest{CategoryKind: "group", GroupName: ptrString("Daily"), Members: []domain.BudgetGroupMember{{CategoryKind: "pfc", PFCPrimary: &food}}, DraftCategories: []domain.BudgetCategory{{CategoryKind: "pfc", PFCPrimary: &food}}, MatchLogic: domain.MatchLogicCategoryOnly, StartDate: domain.FlexibleDate(start), EndDate: domain.FlexibleDate(start.AddDate(0, 1, -1))})
	require.NoError(t, err)
	require.Zero(t, preview.TotalAmount, "unsaved siblings without IDs are not an edit target")
	budget := &domain.Budget{ID: uuid.NewString(), UserID: user, Name: "Atomic", Period: domain.BudgetPeriodMonthly, StartDate: start, Amount: 1000, ExpectedIncome: 1000, CurrencyCode: "USD", IsActive: true}
	br := NewBudgetRepository(tx)
	require.NoError(t, br.Create(ctx, budget))
	for _, cat := range []*domain.BudgetCategory{{BudgetID: budget.ID, CategoryKind: "pfc", PFCPrimary: &food, AllocatedAmount: 500, SpentAmount: 7}, {BudgetID: budget.ID, CategoryKind: "savings", SavingsGoalID: &goal, AllocatedAmount: 100}} {
		require.NoError(t, br.CreateCategory(ctx, cat))
	}
	_, err = tx.Exec(ctx, `INSERT INTO savings_goal_contributions (savings_goal_id,amount,source,status,occurred_on) VALUES ($1,20,'manual','confirmed','2026-09-12')`, goal)
	require.NoError(t, err)
	_, err = br.(*budgetRepository).RecalculateBudgetActuals(ctx, user, budget.ID, true)
	require.NoError(t, err, "an ambiguous savings reason retains that line instead of aborting the whole sibling refresh")
	period := domain.CurrentBudgetActualsRange(budget, time.Now())
	_, reasons, withheld, err := br.(*budgetRepository).recalculateBudgetActualsInTx(ctx, tx, user, budget.ID, period.Start, period.ActualThrough, 0)
	require.NoError(t, err)
	require.False(t, withheld, "a per-category savings reason retains that line rather than withholding the run")
	require.Equal(t, []string{"legacy_unknown"}, reasons, "the retained savings line reports the reason it could not be interpreted")
	var persistedReasons []string
	require.NoError(t, tx.QueryRow(ctx, `SELECT actuals_incomplete_reasons FROM budgets WHERE id=$1`, budget.ID).Scan(&persistedReasons))
	require.Contains(t, persistedReasons, "legacy_unknown", "the removed hard-fail assertion is relocated to the persisted diagnostic")
	var retained float64
	require.NoError(t, tx.QueryRow(ctx, `SELECT spent_amount FROM budget_categories WHERE budget_id=$1 AND category_kind='pfc' AND deleted_at IS NULL`, budget.ID).Scan(&retained))
	require.Equal(t, 70.0, retained, "the sibling pfc line publishes while the savings line is retained")
	_, err = tx.Exec(ctx, `UPDATE savings_goal_contributions SET flow_kind='contribution' WHERE savings_goal_id=$1 AND flow_kind='legacy_unknown'`, goal)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `UPDATE accounts SET currency_code='EUR' WHERE id=$1`, account)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `UPDATE budget_categories SET deleted_at=NOW(),deleted_reason='user_deleted',deleted_by='user' WHERE budget_id=$1 AND category_kind='savings'`, budget.ID)
	require.NoError(t, err)
	beforeWithheld := sumBudgetCategorySpent(ctx, t, tx, budget.ID)
	_, err = br.(*budgetRepository).RecalculateBudgetActuals(ctx, user, budget.ID, true)
	require.NoError(t, err, "a budget-currency mismatch withholds the publish instead of failing the caller")
	_, reasons, withheld, err = br.(*budgetRepository).recalculateBudgetActualsInTx(ctx, tx, user, budget.ID, period.Start, period.ActualThrough, 0)
	require.NoError(t, err)
	require.True(t, withheld, "a foreign-currency ledger withholds the whole publish")
	require.Equal(t, []string{"currency_mismatch"}, reasons)
	require.NoError(t, tx.QueryRow(ctx, `SELECT actuals_incomplete_reasons FROM budgets WHERE id=$1`, budget.ID).Scan(&persistedReasons))
	require.Contains(t, persistedReasons, "currency_mismatch", "the removed hard-fail assertion is relocated to the persisted diagnostic")
	require.Equal(t, beforeWithheld, sumBudgetCategorySpent(ctx, t, tx, budget.ID), "a withheld run writes no spent_amount")
}
