//go:build integration

package repository

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/require"
)

type snapshotGateDB struct {
	DB
	snapshotEstablished chan struct{}
	writerCommitted     chan struct{}
}

func (db *snapshotGateDB) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := db.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &snapshotGateTx{Tx: tx, snapshotEstablished: db.snapshotEstablished, writerCommitted: db.writerCommitted}, nil
}

type snapshotGateTx struct {
	pgx.Tx
	once                sync.Once
	snapshotEstablished chan struct{}
	writerCommitted     chan struct{}
}

func (tx *snapshotGateTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	row := tx.Tx.QueryRow(ctx, sql, args...)
	return snapshotGateRow{Row: row, afterScan: func() {
		tx.once.Do(func() {
			close(tx.snapshotEstablished)
			select {
			case <-tx.writerCommitted:
			case <-ctx.Done():
			}
		})
	}}
}

type snapshotGateRow struct {
	pgx.Row
	afterScan func()
}

func (row snapshotGateRow) Scan(dest ...any) error {
	err := row.Row.Scan(dest...)
	if err == nil {
		row.afterScan()
	}
	return err
}

func TestBudgetPlanRepositoryGetPlanVsActualUsesOneRepeatableReadSnapshot(t *testing.T) {
	pool := connectTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	userID, budgetID, itemID, accountID, transactionID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	_, err := pool.Exec(ctx, `INSERT INTO users (id,clerk_user_id,email,subscription_tier,subscription_status,primary_region) VALUES ($1,$2,$3,'premium','active','iad')`, userID, userID, userID+"@example.invalid")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID) })
	_, err = pool.Exec(ctx, `INSERT INTO linked_items (id,user_id,item_id,access_token_encrypted) VALUES ($1,$2,$3,'fixture')`, itemID, userID, itemID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO accounts (id,user_id,linked_item_id,plaid_account_id,name,type,currency_code) VALUES ($1,$2,$3,$4,'Fixture','depository','USD')`, accountID, userID, itemID, accountID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO budgets (id,user_id,name,period,start_date,end_date,amount,expected_income,series_id) VALUES ($1,$2,'Snapshot','monthly','2026-09-01','2026-09-30',500,500,$1)`, budgetID, userID)
	require.NoError(t, err)
	food := "FOOD_AND_DRINK"
	category, err := NewBudgetRepository(pool).AddCategoryChecked(ctx, budgetID, &domain.BudgetCategory{BudgetID: budgetID, CategoryKind: "pfc", PFCPrimary: &food, AllocatedAmount: 100})
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO transactions (id,user_id,account_id,linked_item_id,source,amount,name,date,pfc_primary,pending) VALUES ($1,$2,$3,$4,'manual',10,'Fixture','2026-09-10',$5,false)`, transactionID, userID, accountID, itemID, food)
	require.NoError(t, err)

	gate := &snapshotGateDB{DB: pool, snapshotEstablished: make(chan struct{}), writerCommitted: make(chan struct{})}
	type result struct {
		report *domain.BudgetPlanVsActualResponse
		err    error
	}
	resultCh := make(chan result, 1)
	go func() {
		report, readErr := NewBudgetPlanRepository(gate).GetPlanVsActual(ctx, userID, budgetID, time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC))
		resultCh <- result{report: report, err: readErr}
	}()

	select {
	case <-gate.snapshotEstablished:
	case <-ctx.Done():
		t.Fatal("reader did not establish its repeatable-read snapshot")
	}
	writer, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer writer.Rollback(ctx) //nolint:errcheck
	_, err = writer.Exec(ctx, `UPDATE budget_categories SET allocated_amount=200 WHERE id=$1`, category.ID)
	require.NoError(t, err)
	_, err = writer.Exec(ctx, `UPDATE transactions SET amount=20 WHERE id=$1`, transactionID)
	require.NoError(t, err)
	require.NoError(t, writer.Commit(ctx))
	close(gate.writerCommitted)

	var first result
	select {
	case first = <-resultCh:
	case <-ctx.Done():
		t.Fatal("first report did not finish after the writer committed")
	}
	require.NoError(t, first.err)
	require.NotNil(t, first.report)
	require.NotNil(t, first.report.Totals.Actual)
	require.Equal(t, 100.0, first.report.Totals.Allocated)
	require.Equal(t, 10.0, *first.report.Totals.Actual)

	second, err := NewBudgetPlanRepository(pool).GetPlanVsActual(ctx, userID, budgetID, time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.NotNil(t, second)
	require.NotNil(t, second.Totals.Actual)
	require.Equal(t, 200.0, second.Totals.Allocated)
	require.Equal(t, 20.0, *second.Totals.Actual)
}
