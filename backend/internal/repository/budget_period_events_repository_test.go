package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupBudgetPeriodEventsMockDB creates a mock database for period events repository testing.
func setupBudgetPeriodEventsMockDB(t *testing.T) (pgxmock.PgxPoolIface, BudgetPeriodEventsRepository) {
	t.Helper()
	mockDB, err := pgxmock.NewPool()
	require.NoError(t, err)
	repo := NewBudgetPeriodEventsRepository(mockDB)
	return mockDB, repo
}

// TestBudgetPeriodEventsRepository_HasMarker_True verifies that HasMarker returns
// true when a matching row exists.
func TestBudgetPeriodEventsRepository_HasMarker_True(t *testing.T) {
	mockDB, repo := setupBudgetPeriodEventsMockDB(t)
	defer mockDB.Close()

	ctx := context.Background()
	periodStart := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2024, time.January, 31, 0, 0, 0, 0, time.UTC)

	rows := pgxmock.NewRows([]string{"?column?"}).AddRow(1)
	mockDB.ExpectQuery(regexp.QuoteMeta(`SELECT 1`)).
		WithArgs("user-1", "budget-1", periodStart, periodEnd, "budget_period_end").
		WillReturnRows(rows)

	has, err := repo.HasMarker(ctx, "user-1", "budget-1", periodStart, periodEnd, "budget_period_end")

	require.NoError(t, err)
	assert.True(t, has)
	require.NoError(t, mockDB.ExpectationsWereMet())
}

// TestBudgetPeriodEventsRepository_HasMarker_False verifies that HasMarker returns
// false when no matching row exists (ErrNoRows).
func TestBudgetPeriodEventsRepository_HasMarker_False(t *testing.T) {
	mockDB, repo := setupBudgetPeriodEventsMockDB(t)
	defer mockDB.Close()

	ctx := context.Background()
	periodStart := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2024, time.January, 31, 0, 0, 0, 0, time.UTC)

	mockDB.ExpectQuery(regexp.QuoteMeta(`SELECT 1`)).
		WithArgs("user-1", "budget-1", periodStart, periodEnd, "budget_period_end").
		WillReturnError(pgx.ErrNoRows)

	has, err := repo.HasMarker(ctx, "user-1", "budget-1", periodStart, periodEnd, "budget_period_end")

	require.NoError(t, err)
	assert.False(t, has)
	require.NoError(t, mockDB.ExpectationsWereMet())
}

// TestBudgetPeriodEventsRepository_HasMarker_DBError verifies that database errors
// are propagated correctly.
func TestBudgetPeriodEventsRepository_HasMarker_DBError(t *testing.T) {
	mockDB, repo := setupBudgetPeriodEventsMockDB(t)
	defer mockDB.Close()

	ctx := context.Background()
	periodStart := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2024, time.January, 31, 0, 0, 0, 0, time.UTC)

	mockDB.ExpectQuery(regexp.QuoteMeta(`SELECT 1`)).
		WithArgs("user-1", "budget-1", periodStart, periodEnd, "budget_period_end").
		WillReturnError(errMock("connection refused"))

	has, err := repo.HasMarker(ctx, "user-1", "budget-1", periodStart, periodEnd, "budget_period_end")

	require.Error(t, err)
	assert.False(t, has)
}

// TestBudgetPeriodEventsRepository_InsertMarker_Success verifies that InsertMarker
// executes the idempotent INSERT ... ON CONFLICT DO NOTHING.
func TestBudgetPeriodEventsRepository_InsertMarker_Success(t *testing.T) {
	mockDB, repo := setupBudgetPeriodEventsMockDB(t)
	defer mockDB.Close()

	ctx := context.Background()
	periodStart := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2024, time.January, 31, 0, 0, 0, 0, time.UTC)

	mockDB.ExpectExec(regexp.QuoteMeta(`INSERT INTO budget_period_events`)).
		WithArgs(
			pgxmock.AnyArg(), // id (UUID)
			"user-1",
			"budget-1",
			periodStart,
			periodEnd,
			"budget_period_end",
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err := repo.InsertMarker(ctx, "user-1", "budget-1", periodStart, periodEnd, "budget_period_end")

	require.NoError(t, err)
	require.NoError(t, mockDB.ExpectationsWereMet())
}

// TestBudgetPeriodEventsRepository_InsertMarker_Idempotent verifies that inserting
// a duplicate marker (ON CONFLICT DO NOTHING) returns no error.
func TestBudgetPeriodEventsRepository_InsertMarker_Idempotent(t *testing.T) {
	mockDB, repo := setupBudgetPeriodEventsMockDB(t)
	defer mockDB.Close()

	ctx := context.Background()
	periodStart := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2024, time.January, 31, 0, 0, 0, 0, time.UTC)

	// ON CONFLICT DO NOTHING → 0 rows affected, but no error.
	mockDB.ExpectExec(regexp.QuoteMeta(`INSERT INTO budget_period_events`)).
		WithArgs(
			pgxmock.AnyArg(),
			"user-1",
			"budget-1",
			periodStart,
			periodEnd,
			"budget_period_end",
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 0))

	err := repo.InsertMarker(ctx, "user-1", "budget-1", periodStart, periodEnd, "budget_period_end")

	require.NoError(t, err)
	require.NoError(t, mockDB.ExpectationsWereMet())
}

// TestBudgetPeriodEventsRepository_InsertMarker_DBError verifies that DB errors
// are propagated.
func TestBudgetPeriodEventsRepository_InsertMarker_DBError(t *testing.T) {
	mockDB, repo := setupBudgetPeriodEventsMockDB(t)
	defer mockDB.Close()

	ctx := context.Background()
	periodStart := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2024, time.January, 31, 0, 0, 0, 0, time.UTC)

	mockDB.ExpectExec(regexp.QuoteMeta(`INSERT INTO budget_period_events`)).
		WithArgs(
			pgxmock.AnyArg(),
			"user-1",
			"budget-1",
			periodStart,
			periodEnd,
			"budget_period_end",
		).
		WillReturnError(errMock("disk full"))

	err := repo.InsertMarker(ctx, "user-1", "budget-1", periodStart, periodEnd, "budget_period_end")

	require.Error(t, err)
}

// errMock is a simple error for testing.
type errMock string

func (e errMock) Error() string { return string(e) }
