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

type gatedBudgetAccountingQuerier struct{ Querier }

func (q gatedBudgetAccountingQuerier) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	return q.Querier.Query(ctx, `WITH accounting_gate AS MATERIALIZED (SELECT pg_advisory_xact_lock(16371627)) SELECT result.* FROM accounting_gate CROSS JOIN LATERAL (`+sql+`) result`, args...)
}

func TestBudgetAccountingStatementSnapshotSurvivesConcurrentContributionCommit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool := connectTestDB(t)
	user, item, account, goal, transaction := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	setup, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer setup.Rollback(ctx)
	_, err = setup.Exec(ctx, `INSERT INTO users (id,clerk_user_id,email,subscription_tier,subscription_status,primary_region) VALUES ($1,$2,$3,'premium','active','iad')`, user, user, user+"@example.com")
	require.NoError(t, err)
	_, err = setup.Exec(ctx, `INSERT INTO linked_items (id,user_id,item_id,access_token_encrypted) VALUES ($1,$2,$3,'fixture')`, item, user, item)
	require.NoError(t, err)
	_, err = setup.Exec(ctx, `INSERT INTO accounts (id,user_id,linked_item_id,plaid_account_id,name,type,currency_code) VALUES ($1,$2,$3,$4,'Fixture','depository','USD')`, account, user, item, account)
	require.NoError(t, err)
	_, err = setup.Exec(ctx, `INSERT INTO savings_goals (id,user_id,account_id,name,target_amount,progress_basis,currency_code) VALUES ($1,$2,$3,'Fixture',1000,'contributions','USD')`, goal, user, account)
	require.NoError(t, err)
	_, err = setup.Exec(ctx, `INSERT INTO transactions (id,user_id,account_id,linked_item_id,source,amount,name,date) VALUES ($1,$2,$3,$4,'manual',100,'Fixture',CURRENT_DATE)`, transaction, user, account, item)
	require.NoError(t, err)
	incomeTransaction := uuid.NewString()
	_, err = setup.Exec(ctx, `INSERT INTO transactions (id,user_id,account_id,linked_item_id,source,amount,name,date,category_override_kind,pfc_primary_override) VALUES ($1,$2,$3,$4,'manual',-80,'Income',CURRENT_DATE,'pfc','INCOME')`, incomeTransaction, user, account, item)
	require.NoError(t, err)
	require.NoError(t, setup.Commit(ctx))
	t.Cleanup(func() {
		_, cleanupErr := pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, user)
		require.NoError(t, cleanupErr)
	})
	writer, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer writer.Rollback(ctx)
	_, err = writer.Exec(ctx, `SELECT pg_advisory_xact_lock(16371627)`)
	require.NoError(t, err)
	reader, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer reader.Release()
	type result struct {
		ledger  domain.BudgetExpenseLedger
		savings []SavingsPeriodActivity
		income  []budgetIncomeFact
		err     error
	}
	finished := make(chan result, 1)
	now := time.Now().UTC()
	categories := []domain.BudgetCategory{{ID: "other", CategoryKind: "remainder"}}
	sources := []domain.IncomeSource{{Type: domain.IncomeSourceTypePFCPrimary, Value: "INCOME"}}
	go func() {
		l, s, i, e := readBudgetAccountingInputs(ctx, gatedBudgetAccountingQuerier{reader}, user, categories, now.AddDate(0, 0, -1), now, sources)
		finished <- result{l, s, i, e}
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := pool.QueryRow(ctx, `SELECT COALESCE(wait_event='advisory',false) FROM pg_stat_activity WHERE pid=$1`, reader.Conn().PgConn().PID()).Scan(&waiting)
		return err == nil && waiting
	}, 3*time.Second, 10*time.Millisecond, "financial statement must have started its snapshot before the writer commits")
	_, err = writer.Exec(ctx, `INSERT INTO savings_goal_contributions (savings_goal_id,transaction_id,amount,source,status,occurred_on,flow_kind) VALUES ($1,$2,30,'manual','confirmed',CURRENT_DATE,'contribution')`, goal, transaction)
	require.NoError(t, err)
	_, err = writer.Exec(ctx, `UPDATE transactions SET amount=80 WHERE id=$1 AND user_id=$2`, incomeTransaction, user)
	require.NoError(t, err)
	require.NoError(t, writer.Commit(ctx))
	got := <-finished
	require.NoError(t, got.err)
	require.Equal(t, 100.0, *got.ledger.Lines["other"].Amount)
	require.Len(t, got.savings, 1)
	require.Equal(t, 0.0, *got.savings[0].ActualAmount, "the blocked statement sees neither side of the later contribution")
	require.Len(t, got.income, 1)
	require.Equal(t, 80.0, got.income[0].Amount, "income shares the old financial snapshot")
	current, savings, income, err := readBudgetAccountingInputs(ctx, reader, user, categories, now.AddDate(0, 0, -1), now, sources)
	require.NoError(t, err)
	require.Equal(t, 150.0, *current.Lines["other"].Amount)
	require.Equal(t, 30.0, *savings[0].ActualAmount, "the next statement sees both sides together")
	require.Empty(t, income, "the income-to-expense correction appears wholly in the next snapshot")
}
