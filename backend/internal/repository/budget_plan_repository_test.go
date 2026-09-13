package repository

import (
	"context"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/require"
)

func TestBudgetPlanReadUsesReadOnlyRepeatableSnapshotBeforeOwnershipRead(t *testing.T) {
	m, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer m.Close()
	m.ExpectBegin()
	m.ExpectExec("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY").WillReturnResult(pgxmock.NewResult("SET", 0))
	m.ExpectQuery("SELECT EXISTS").WithArgs("budget", "stranger").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))
	m.ExpectRollback()
	_, err = NewBudgetPlanRepository(m).GetPlanVsActual(context.Background(), "stranger", "budget", time.Now())
	require.ErrorIs(t, err, domain.ErrBudgetNotFound)
	require.NoError(t, m.ExpectationsWereMet())
}
