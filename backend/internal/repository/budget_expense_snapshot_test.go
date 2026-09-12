package repository

import (
	"context"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestBudgetAccountingTransactionsAllowFreshReadsAfterConfigurationLock(t *testing.T) {
	m, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer m.Close()
	m.ExpectBegin()
	m.ExpectRollback()
	tx, err := NewBudgetRepository(m).BeginTx(context.Background())
	require.NoError(t, err)
	require.NoError(t, tx.Rollback(context.Background()))
	require.NoError(t, m.ExpectationsWereMet())
}

func TestBudgetDrilldownKeepsCountOnEmptyPageWithinSnapshot(t *testing.T) {
	m, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer m.Close()
	p := "FOOD_AND_DRINK"
	c := &domain.BudgetCategory{CategoryKind: "pfc", PFCPrimary: &p}
	start := time.Now()
	m.ExpectBegin()
	m.ExpectExec("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY").WillReturnResult(pgxmock.NewResult("SET", 0))
	m.ExpectQuery("WITH contribution_weights").WithArgs("user", start, start).WillReturnRows(budgetExpenseTestRows(c, 40, 2))
	m.ExpectCommit()
	rows, count, amount, err := NewTransactionRepository(m, nil).GetTransactionsForBudgetCategory(context.Background(), "user", c, start, start, 20, 30)
	require.NoError(t, err)
	require.Empty(t, rows)
	require.Equal(t, 2, count)
	require.Equal(t, 40.0, amount)
	require.NoError(t, m.ExpectationsWereMet())
}
