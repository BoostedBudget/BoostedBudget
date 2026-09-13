//go:build integration

package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestSavingsFlowProvenanceRoundTrip(t *testing.T) {
	ctx := context.Background()
	pool := connectTestDB(t)
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck
	userID, goalID := uuid.NewString(), uuid.NewString()
	_, err = tx.Exec(ctx, `INSERT INTO users (id,clerk_user_id,email,subscription_tier,subscription_status,primary_region) VALUES ($1,$2,$3,'premium','active','iad')`, userID, "clerk_"+userID, userID+"@example.com")
	require.NoError(t, err)
	itemID, accountID := uuid.NewString(), uuid.NewString()
	_, err = tx.Exec(ctx, `INSERT INTO linked_items (id,user_id,item_id,access_token_encrypted) VALUES ($1,$2,$3,'fixture')`, itemID, userID, "item_"+itemID)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO accounts (id,user_id,linked_item_id,plaid_account_id,name,type) VALUES ($1,$2,$3,$4,'Fixture savings','depository')`, accountID, userID, itemID, "account_"+accountID)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO savings_goals (id,user_id,account_id,name,target_amount,progress_basis) VALUES ($1,$2,$3,'Flow provenance',1000,'contributions')`, goalID, userID, accountID)
	require.NoError(t, err)
	repo := NewSavingsContributionRepository(tx)
	for _, kind := range []string{"", "opening_balance"} {
		c := &domain.SavingsGoalContribution{SavingsGoalID: goalID, Amount: 50, Source: "manual", Status: "confirmed", OccurredOn: "2026-09-12", FlowKind: kind}
		require.NoError(t, repo.Insert(ctx, userID, c))
		var got string
		require.NoError(t, tx.QueryRow(ctx, `SELECT flow_kind FROM savings_goal_contributions WHERE id=$1 AND deleted_at IS NULL`, c.ID).Scan(&got))
		want := kind
		if want == "" {
			want = "contribution"
		}
		require.Equal(t, want, got)
	}
	// Goal progress still includes the opening balance, even though activity will not.
	progress, err := repo.SumConfirmedByGoal(ctx, userID, goalID)
	require.NoError(t, err)
	require.Equal(t, 100.0, progress)
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, -1)
	activity, err := ReadSavingsPeriodActivity(ctx, tx, userID, start, end)
	require.NoError(t, err)
	require.Len(t, activity, 1)
	require.NotNil(t, activity[0].ActualAmount)
	require.Equal(t, 50.0, *activity[0].ActualAmount)
	_, err = tx.Exec(ctx, `UPDATE savings_goal_contributions SET archived_at=clock_timestamp() WHERE savings_goal_id=$1 AND flow_kind='contribution'`, goalID)
	require.NoError(t, err)
	activity, err = ReadSavingsPeriodActivity(ctx, tx, userID, start, end)
	require.NoError(t, err)
	require.Equal(t, 50.0, *activity[0].ActualAmount, "basis changes must not erase actual activity")
	_, err = tx.Exec(ctx, `INSERT INTO savings_goal_contributions (savings_goal_id,amount,source,status,occurred_on) VALUES ($1,25,'manual','confirmed','2026-09-12')`, goalID)
	require.NoError(t, err)
	activity, err = ReadSavingsPeriodActivity(ctx, tx, userID, start, end)
	require.NoError(t, err)
	require.Nil(t, activity[0].ActualAmount, "unknown historical manual entries are not silently counted or zeroed")
	require.Equal(t, "legacy_unknown", activity[0].Reason)
	other, err := ReadSavingsPeriodActivity(ctx, tx, uuid.NewString(), start, end)
	require.NoError(t, err)
	require.Empty(t, other)

	// The same bank transaction can reappear after a basis archive in another
	// goal or month. Neither generation can safely be selected as the real one.
	oldGoal, newGoal, transactionID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, id := range []string{oldGoal, newGoal} {
		_, err = tx.Exec(ctx, `INSERT INTO savings_goals (id,user_id,account_id,name,target_amount,progress_basis) VALUES ($1,$2,$3,$4,1000,'contributions')`, id, userID, accountID, "Generation "+id)
		require.NoError(t, err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO transactions (id,user_id,account_id,linked_item_id,source,amount,name,date) VALUES ($1,$2,$3,$4,'manual',100,'Fixture','2026-09-12')`, transactionID, userID, accountID, itemID)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO savings_goal_contributions (savings_goal_id,transaction_id,amount,source,status,occurred_on,archived_at,flow_kind) VALUES ($1,$2,100,'manual','confirmed','2026-08-12',NOW(),'contribution')`, oldGoal, transactionID)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO savings_goal_contributions (savings_goal_id,transaction_id,amount,source,status,occurred_on,flow_kind) VALUES ($1,$2,100,'manual','confirmed','2026-09-12','contribution')`, newGoal, transactionID)
	require.NoError(t, err)
	for _, period := range []struct {
		start time.Time
		goal  string
	}{{start, newGoal}, {start.AddDate(0, -1, 0), oldGoal}} {
		activity, err = ReadSavingsPeriodActivity(ctx, tx, userID, period.start, period.start.AddDate(0, 1, -1))
		require.NoError(t, err)
		found := false
		for _, row := range activity {
			if row.GoalID == period.goal {
				found = true
				require.Nil(t, row.ActualAmount)
				require.Equal(t, "ambiguous_contribution_generation", row.Reason)
			}
		}
		require.True(t, found)
	}

	t.Run("retired goal remains evidence for later report", func(t *testing.T) {
		subtx, err := tx.Begin(ctx)
		require.NoError(t, err)
		defer subtx.Rollback(ctx) //nolint:errcheck
		// The expense query cannot expose this ambiguity in September because
		// the underlying bank transaction belongs to August.
		_, err = subtx.Exec(ctx, `UPDATE transactions SET date='2026-08-12' WHERE id=$1`, transactionID)
		require.NoError(t, err)
		require.NoError(t, NewSavingsGoalRepository(subtx).Delete(ctx, oldGoal, userID, nil))
		budgetID := uuid.NewString()
		_, err = subtx.Exec(ctx, `INSERT INTO budgets (id,user_id,name,period,start_date,end_date,amount,expected_income,currency_code,series_id)
			VALUES ($1,$2,'September generation','monthly','2026-09-01','2026-09-30',200,200,'USD',$1)`, budgetID, userID)
		require.NoError(t, err)
		_, err = subtx.Exec(ctx, `INSERT INTO budget_categories (budget_id,category_kind,savings_goal_id,allocated_amount)
			VALUES ($1,'savings',$2,100)`, budgetID, newGoal)
		require.NoError(t, err)
		report, err := readBudgetPlanInSnapshot(ctx, subtx, userID, budgetID, start.AddDate(0, 0, 11))
		require.NoError(t, err)
		require.Len(t, report.Lines, 1)
		require.Equal(t, &newGoal, report.Lines[0].SavingsGoalID)
		require.Nil(t, report.Lines[0].Actual, "retiring the archived goal must not make the newer generation provable")
		require.Contains(t, report.Lines[0].Reasons, "ambiguous_savings_generation")
		require.Nil(t, report.Totals.Actual)
		require.Equal(t, "partial", report.Completeness)
		activity, err := ReadSavingsPeriodActivity(ctx, subtx, userID, start, end)
		require.NoError(t, err)
		for _, row := range activity {
			require.NotEqual(t, oldGoal, row.GoalID, "retained evidence must not expose retired goals")
		}
	})

	// Exercise the migration itself, including retained rows, inside this
	// rollback-only fixture; never reverse the shared scratch schema.
	_, err = tx.Exec(ctx, `UPDATE savings_goal_contributions SET flow_kind='legacy_unknown',deleted_at=NOW(),deleted_reason='provider_removed_txn' WHERE savings_goal_id=$1`, oldGoal)
	require.NoError(t, err)
	backfill, err := os.ReadFile("../../migrations/000207_classify_known_savings_flows.up.sql")
	require.NoError(t, err)
	_, err = tx.Exec(ctx, string(backfill))
	require.NoError(t, err)
	var classified string
	require.NoError(t, tx.QueryRow(ctx, `SELECT flow_kind FROM savings_goal_contributions WHERE savings_goal_id=$1 AND deleted_at IS NOT NULL`, oldGoal).Scan(&classified))
	require.Equal(t, "contribution", classified)
	var unknownCount int
	require.NoError(t, tx.QueryRow(ctx, `SELECT COUNT(*) FROM savings_goal_contributions WHERE savings_goal_id=$1 AND transaction_id IS NULL AND flow_kind='legacy_unknown'`, goalID).Scan(&unknownCount))
	require.Equal(t, 1, unknownCount)
	down, err := os.ReadFile("../../migrations/000206_add_savings_flow_provenance.down.sql")
	require.NoError(t, err)
	_, err = tx.Exec(ctx, "SAVEPOINT provenance_down")
	require.NoError(t, err)
	_, err = tx.Exec(ctx, string(down))
	require.ErrorContains(t, err, "provenance")
	_, err = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT provenance_down")
	require.NoError(t, err)
}
