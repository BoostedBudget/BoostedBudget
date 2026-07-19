package repository

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupBoostExecutionMockDB(t *testing.T) (pgxmock.PgxPoolIface, BoostExecutionRepository) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	repo := NewBoostExecutionRepository(mock)
	return mock, repo
}

// boostExecutionColumns lists the columns returned by scanExecution.
var boostExecutionColumns = []string{
	"id", "boost_id", "status", "started_at", "completed_at",
	"execution_duration_ms", "trigger_context", "input_data", "output_data",
	"error_code", "error_message", "retry_count", "max_retries",
}

func addBoostExecutionRow(rows *pgxmock.Rows, execID, boostID string) *pgxmock.Rows {
	return rows.AddRow(
		execID,
		boostID,
		domain.ExecutionStatusPending,
		time.Now(),
		nil,       // completed_at
		nil,       // execution_duration_ms
		[]byte("{}"), // trigger_context JSONB
		nil,       // input_data
		nil,       // output_data
		nil,       // error_code
		nil,       // error_message
		0,         // retry_count
		3,         // max_retries
	)
}

// --- Constructor ---

func TestNewBoostExecutionRepository_ReturnsNonNil(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewBoostExecutionRepository(mock)
	assert.NotNil(t, repo)
}

// --- Create ---

func TestBoostExecutionRepository_Create_Success(t *testing.T) {
	mock, repo := setupBoostExecutionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	execution := &domain.BoostExecution{
		BoostID: uuid.New().String(),
	}

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO boost_executions`)).
		WithArgs(
			pgxmock.AnyArg(), // id (generated)
			execution.BoostID,
			domain.ExecutionStatusPending, // status (defaulted)
			pgxmock.AnyArg(), // started_at (defaulted)
			pgxmock.AnyArg(), // completed_at (nil)
			pgxmock.AnyArg(), // execution_duration_ms (nil)
			pgxmock.AnyArg(), // trigger_context (defaulted to {})
			pgxmock.AnyArg(), // input_data
			pgxmock.AnyArg(), // output_data
			pgxmock.AnyArg(), // error_code
			pgxmock.AnyArg(), // error_message
			pgxmock.AnyArg(), // retry_count
			3, // max_retries (defaulted to 3)
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err := repo.Create(ctx, execution)

	assert.NoError(t, err)
	assert.NotEmpty(t, execution.ID)
	assert.Equal(t, domain.ExecutionStatusPending, execution.Status)
	assert.Equal(t, 3, execution.MaxRetries)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestBoostExecutionRepository_Create_Error(t *testing.T) {
	mock, repo := setupBoostExecutionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	execution := &domain.BoostExecution{
		BoostID: uuid.New().String(),
	}

	dbErr := errors.New("db error")
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO boost_executions`)).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnError(dbErr)

	err := repo.Create(ctx, execution)

	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- Update ---

func TestBoostExecutionRepository_Update_Success(t *testing.T) {
	mock, repo := setupBoostExecutionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	execution := &domain.BoostExecution{
		ID:     uuid.New().String(),
		Status: domain.ExecutionStatusCompleted,
	}

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE boost_executions`)).
		WithArgs(
			execution.Status,
			pgxmock.AnyArg(), // completed_at
			pgxmock.AnyArg(), // execution_duration_ms
			pgxmock.AnyArg(), // output_data
			pgxmock.AnyArg(), // error_code
			pgxmock.AnyArg(), // error_message
			execution.RetryCount,
			execution.ID,
		).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.Update(ctx, execution)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestBoostExecutionRepository_Update_Error(t *testing.T) {
	mock, repo := setupBoostExecutionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	execution := &domain.BoostExecution{
		ID:     uuid.New().String(),
		Status: domain.ExecutionStatusFailed,
	}

	dbErr := errors.New("update error")
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE boost_executions`)).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnError(dbErr)

	err := repo.Update(ctx, execution)

	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestBoostExecutionRepository_Update_NotFound(t *testing.T) {
	mock, repo := setupBoostExecutionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	execution := &domain.BoostExecution{
		ID:     uuid.New().String(),
		Status: domain.ExecutionStatusCompleted,
	}

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE boost_executions`)).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := repo.Update(ctx, execution)

	assert.ErrorIs(t, err, pgx.ErrNoRows)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- GetByID ---

func TestBoostExecutionRepository_GetByID_Success(t *testing.T) {
	mock, repo := setupBoostExecutionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	execID := uuid.New().String()
	boostID := uuid.New().String()

	rows := pgxmock.NewRows(boostExecutionColumns)
	addBoostExecutionRow(rows, execID, boostID)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, boost_id, status`)).
		WithArgs(execID).
		WillReturnRows(rows)

	result, err := repo.GetByID(ctx, execID)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, execID, result.ID)
	assert.Equal(t, boostID, result.BoostID)
	assert.Equal(t, domain.ExecutionStatusPending, result.Status)
	assert.Equal(t, 3, result.MaxRetries)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestBoostExecutionRepository_GetByID_NotFound(t *testing.T) {
	mock, repo := setupBoostExecutionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	execID := uuid.New().String()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, boost_id, status`)).
		WithArgs(execID).
		WillReturnError(pgx.ErrNoRows)

	result, err := repo.GetByID(ctx, execID)

	assert.Nil(t, result)
	assert.ErrorIs(t, err, pgx.ErrNoRows)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- GetByBoostID ---

func TestBoostExecutionRepository_GetByBoostID_Success(t *testing.T) {
	mock, repo := setupBoostExecutionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	boostID := uuid.New().String()
	execID1 := uuid.New().String()
	execID2 := uuid.New().String()

	rows := pgxmock.NewRows(boostExecutionColumns)
	addBoostExecutionRow(rows, execID1, boostID)
	addBoostExecutionRow(rows, execID2, boostID)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, boost_id, status`)).
		WithArgs(boostID, 10, 0).
		WillReturnRows(rows)

	results, err := repo.GetByBoostID(ctx, boostID, 10, 0)

	require.NoError(t, err)
	assert.Len(t, results, 2)
	assert.Equal(t, execID1, results[0].ID)
	assert.Equal(t, execID2, results[1].ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestBoostExecutionRepository_GetByBoostID_Empty(t *testing.T) {
	mock, repo := setupBoostExecutionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	boostID := uuid.New().String()

	rows := pgxmock.NewRows(boostExecutionColumns)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, boost_id, status`)).
		WithArgs(boostID, 10, 0).
		WillReturnRows(rows)

	results, err := repo.GetByBoostID(ctx, boostID, 10, 0)

	require.NoError(t, err)
	assert.Empty(t, results)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- GetByBoostIDWithFilter ---

func TestBoostExecutionRepository_GetByBoostIDWithFilter_NoStatus(t *testing.T) {
	mock, repo := setupBoostExecutionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	boostID := uuid.New().String()
	execID := uuid.New().String()

	// Count query (no status filter)
	countRows := pgxmock.NewRows([]string{"count"}).AddRow(1)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM boost_executions WHERE boost_id = $1`)).
		WithArgs(boostID).
		WillReturnRows(countRows)

	// Data query
	rows := pgxmock.NewRows(boostExecutionColumns)
	addBoostExecutionRow(rows, execID, boostID)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, boost_id, status`)).
		WithArgs(boostID, 10, 0).
		WillReturnRows(rows)

	results, total, err := repo.GetByBoostIDWithFilter(ctx, boostID, nil, 10, 0)

	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Len(t, results, 1)
	assert.Equal(t, execID, results[0].ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestBoostExecutionRepository_GetByBoostIDWithFilter_WithStatus(t *testing.T) {
	mock, repo := setupBoostExecutionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	boostID := uuid.New().String()
	execID := uuid.New().String()
	status := domain.ExecutionStatusCompleted

	// Count query with status filter
	countRows := pgxmock.NewRows([]string{"count"}).AddRow(1)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM boost_executions WHERE boost_id = $1`)).
		WithArgs(boostID, status).
		WillReturnRows(countRows)

	// Data query with status filter
	rows := pgxmock.NewRows(boostExecutionColumns)
	rows.AddRow(
		execID, boostID, domain.ExecutionStatusCompleted,
		time.Now(), nil, nil, []byte("{}"), nil, nil, nil, nil, 0, 3,
	)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, boost_id, status`)).
		WithArgs(boostID, status, 10, 0).
		WillReturnRows(rows)

	results, total, err := repo.GetByBoostIDWithFilter(ctx, boostID, &status, 10, 0)

	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Len(t, results, 1)
	assert.Equal(t, execID, results[0].ID)
	assert.Equal(t, domain.ExecutionStatusCompleted, results[0].Status)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- GetLastExecutionForBoost ---

func TestBoostExecutionRepository_GetLastExecutionForBoost_Found(t *testing.T) {
	mock, repo := setupBoostExecutionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	boostID := uuid.New().String()
	execID := uuid.New().String()

	rows := pgxmock.NewRows(boostExecutionColumns)
	addBoostExecutionRow(rows, execID, boostID)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, boost_id, status`)).
		WithArgs(boostID).
		WillReturnRows(rows)

	result, err := repo.GetLastExecutionForBoost(ctx, boostID)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, execID, result.ID)
	assert.Equal(t, boostID, result.BoostID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestBoostExecutionRepository_GetLastExecutionForBoost_NotFound(t *testing.T) {
	mock, repo := setupBoostExecutionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	boostID := uuid.New().String()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, boost_id, status`)).
		WithArgs(boostID).
		WillReturnError(pgx.ErrNoRows)

	result, err := repo.GetLastExecutionForBoost(ctx, boostID)

	// Repository returns nil, pgx.ErrNoRows (passes through scanExecution)
	assert.Nil(t, result)
	assert.ErrorIs(t, err, pgx.ErrNoRows)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- GetPendingExecutions ---

func TestBoostExecutionRepository_GetPendingExecutions_Success(t *testing.T) {
	mock, repo := setupBoostExecutionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	execID := uuid.New().String()
	boostID := uuid.New().String()

	rows := pgxmock.NewRows(boostExecutionColumns)
	addBoostExecutionRow(rows, execID, boostID)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, boost_id, status`)).
		WithArgs(10).
		WillReturnRows(rows)

	results, err := repo.GetPendingExecutions(ctx, 10)

	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, execID, results[0].ID)
	assert.Equal(t, domain.ExecutionStatusPending, results[0].Status)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestBoostExecutionRepository_GetPendingExecutions_Empty(t *testing.T) {
	mock, repo := setupBoostExecutionMockDB(t)
	defer mock.Close()

	ctx := context.Background()

	rows := pgxmock.NewRows(boostExecutionColumns)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, boost_id, status`)).
		WithArgs(10).
		WillReturnRows(rows)

	results, err := repo.GetPendingExecutions(ctx, 10)

	require.NoError(t, err)
	assert.Empty(t, results)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- GetFailedExecutions ---

func TestBoostExecutionRepository_GetFailedExecutions_Success(t *testing.T) {
	mock, repo := setupBoostExecutionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	boostID := uuid.New().String()
	execID := uuid.New().String()

	rows := pgxmock.NewRows(boostExecutionColumns)
	rows.AddRow(
		execID, boostID, domain.ExecutionStatusFailed,
		time.Now(), nil, nil, []byte("{}"), nil, nil,
		strPtr("ERR_001"), strPtr("failure message"), 1, 3,
	)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, boost_id, status`)).
		WithArgs(boostID, 5).
		WillReturnRows(rows)

	results, err := repo.GetFailedExecutions(ctx, boostID, 5)

	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, execID, results[0].ID)
	assert.Equal(t, domain.ExecutionStatusFailed, results[0].Status)
	assert.Equal(t, 1, results[0].RetryCount)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestBoostExecutionRepository_GetFailedExecutions_QueryError(t *testing.T) {
	mock, repo := setupBoostExecutionMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	boostID := uuid.New().String()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, boost_id, status`)).
		WithArgs(boostID, 5).
		WillReturnError(errors.New("db error"))

	results, err := repo.GetFailedExecutions(ctx, boostID, 5)

	assert.Nil(t, results)
	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}
