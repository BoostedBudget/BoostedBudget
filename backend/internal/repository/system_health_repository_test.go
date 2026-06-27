package repository

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupSystemHealthMockDB(t *testing.T) (pgxmock.PgxPoolIface, *SystemHealthRepository) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	repo := NewSystemHealthRepository(mock)
	return mock, repo
}

// GetRiverJobStats

func TestSystemHealthRepository_GetRiverJobStats_Success(t *testing.T) {
	mock, repo := setupSystemHealthMockDB(t)
	defer mock.Close()

	queue := "default"
	rows := pgxmock.NewRows([]string{"pending", "running", "failed"}).
		AddRow(int64(5), int64(2), int64(1))

	mock.ExpectQuery(regexp.QuoteMeta("FROM river_job")).
		WithArgs(queue).
		WillReturnRows(rows)

	metrics, err := repo.GetRiverJobStats(context.Background(), queue)

	require.NoError(t, err)
	assert.Equal(t, &domain.QueueMetrics{Pending: 5, Running: 2, Failed: 1}, metrics)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSystemHealthRepository_GetRiverJobStats_Error(t *testing.T) {
	mock, repo := setupSystemHealthMockDB(t)
	defer mock.Close()

	queue := "default"
	dbErr := errors.New("db error")

	mock.ExpectQuery(regexp.QuoteMeta("FROM river_job")).
		WithArgs(queue).
		WillReturnError(dbErr)

	metrics, err := repo.GetRiverJobStats(context.Background(), queue)

	assert.Error(t, err)
	assert.Nil(t, metrics)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// GetRiverJobsCompletedLastHour

func TestSystemHealthRepository_GetRiverJobsCompletedLastHour_Success(t *testing.T) {
	mock, repo := setupSystemHealthMockDB(t)
	defer mock.Close()

	queue := "default"
	rows := pgxmock.NewRows([]string{"count"}).AddRow(int64(42))

	mock.ExpectQuery(regexp.QuoteMeta("AND state = 'completed'")).
		WithArgs(queue).
		WillReturnRows(rows)

	count, err := repo.GetRiverJobsCompletedLastHour(context.Background(), queue)

	require.NoError(t, err)
	assert.Equal(t, int64(42), count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSystemHealthRepository_GetRiverJobsCompletedLastHour_Error(t *testing.T) {
	mock, repo := setupSystemHealthMockDB(t)
	defer mock.Close()

	queue := "default"
	dbErr := errors.New("db error")

	mock.ExpectQuery(regexp.QuoteMeta("AND state = 'completed'")).
		WithArgs(queue).
		WillReturnError(dbErr)

	count, err := repo.GetRiverJobsCompletedLastHour(context.Background(), queue)

	assert.Error(t, err)
	assert.Equal(t, int64(0), count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// GetActiveSyncCount

func TestSystemHealthRepository_GetActiveSyncCount_Success(t *testing.T) {
	mock, repo := setupSystemHealthMockDB(t)
	defer mock.Close()

	rows := pgxmock.NewRows([]string{"count"}).AddRow(int64(3))

	mock.ExpectQuery(regexp.QuoteMeta("WHERE kind = 'plaid_sync'")).
		WillReturnRows(rows)

	count, err := repo.GetActiveSyncCount(context.Background())

	require.NoError(t, err)
	assert.Equal(t, int64(3), count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSystemHealthRepository_GetActiveSyncCount_Error(t *testing.T) {
	mock, repo := setupSystemHealthMockDB(t)
	defer mock.Close()

	dbErr := errors.New("db error")

	mock.ExpectQuery(regexp.QuoteMeta("WHERE kind = 'plaid_sync'")).
		WillReturnError(dbErr)

	count, err := repo.GetActiveSyncCount(context.Background())

	assert.Error(t, err)
	assert.Equal(t, int64(0), count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// GetFailedSyncsLastHour

func TestSystemHealthRepository_GetFailedSyncsLastHour_Success(t *testing.T) {
	mock, repo := setupSystemHealthMockDB(t)
	defer mock.Close()

	rows := pgxmock.NewRows([]string{"count"}).AddRow(int64(7))

	mock.ExpectQuery(regexp.QuoteMeta("FROM linked_items")).
		WillReturnRows(rows)

	count, err := repo.GetFailedSyncsLastHour(context.Background())

	require.NoError(t, err)
	assert.Equal(t, int64(7), count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSystemHealthRepository_GetFailedSyncsLastHour_Error(t *testing.T) {
	mock, repo := setupSystemHealthMockDB(t)
	defer mock.Close()

	dbErr := errors.New("db error")

	mock.ExpectQuery(regexp.QuoteMeta("FROM linked_items")).
		WillReturnError(dbErr)

	count, err := repo.GetFailedSyncsLastHour(context.Background())

	assert.Error(t, err)
	assert.Equal(t, int64(0), count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// GetAverageSyncDuration

func TestSystemHealthRepository_GetAverageSyncDuration_Success(t *testing.T) {
	mock, repo := setupSystemHealthMockDB(t)
	defer mock.Close()

	rows := pgxmock.NewRows([]string{"avg_ms"}).AddRow(int64(1500))

	mock.ExpectQuery(regexp.QuoteMeta("COALESCE(AVG")).
		WillReturnRows(rows)

	avgMs, err := repo.GetAverageSyncDuration(context.Background())

	require.NoError(t, err)
	assert.Equal(t, int64(1500), avgMs)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSystemHealthRepository_GetAverageSyncDuration_Error(t *testing.T) {
	mock, repo := setupSystemHealthMockDB(t)
	defer mock.Close()

	dbErr := errors.New("db error")

	mock.ExpectQuery(regexp.QuoteMeta("COALESCE(AVG")).
		WillReturnError(dbErr)

	avgMs, err := repo.GetAverageSyncDuration(context.Background())

	assert.Error(t, err)
	assert.Equal(t, int64(0), avgMs)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// GetActiveSubscriptionCount

func TestSystemHealthRepository_GetActiveSubscriptionCount_Success(t *testing.T) {
	mock, repo := setupSystemHealthMockDB(t)
	defer mock.Close()

	rows := pgxmock.NewRows([]string{"count"}).AddRow(int64(99))

	mock.ExpectQuery(regexp.QuoteMeta("paddle_subscription_id IS NOT NULL")).
		WillReturnRows(rows)

	count, err := repo.GetActiveSubscriptionCount(context.Background())

	require.NoError(t, err)
	assert.Equal(t, int64(99), count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSystemHealthRepository_GetActiveSubscriptionCount_Error(t *testing.T) {
	mock, repo := setupSystemHealthMockDB(t)
	defer mock.Close()

	dbErr := errors.New("db error")

	mock.ExpectQuery(regexp.QuoteMeta("paddle_subscription_id IS NOT NULL")).
		WillReturnError(dbErr)

	count, err := repo.GetActiveSubscriptionCount(context.Background())

	assert.Error(t, err)
	assert.Equal(t, int64(0), count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// GetPaddleWebhookStats24h

func TestSystemHealthRepository_GetPaddleWebhookStats24h_Success(t *testing.T) {
	mock, repo := setupSystemHealthMockDB(t)
	defer mock.Close()

	rows := pgxmock.NewRows([]string{"total", "failed"}).AddRow(int64(200), int64(4))

	mock.ExpectQuery(regexp.QuoteMeta("FROM paddle_webhook_events")).
		WillReturnRows(rows)

	total, failed, err := repo.GetPaddleWebhookStats24h(context.Background())

	require.NoError(t, err)
	assert.Equal(t, int64(200), total)
	assert.Equal(t, int64(4), failed)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSystemHealthRepository_GetPaddleWebhookStats24h_Error(t *testing.T) {
	mock, repo := setupSystemHealthMockDB(t)
	defer mock.Close()

	dbErr := errors.New("db error")

	mock.ExpectQuery(regexp.QuoteMeta("FROM paddle_webhook_events")).
		WillReturnError(dbErr)

	total, failed, err := repo.GetPaddleWebhookStats24h(context.Background())

	assert.Error(t, err)
	assert.Equal(t, int64(0), total)
	assert.Equal(t, int64(0), failed)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// GetLastReconciliationRun

func TestSystemHealthRepository_GetLastReconciliationRun_Success(t *testing.T) {
	mock, repo := setupSystemHealthMockDB(t)
	defer mock.Close()

	expectedTime := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	rows := pgxmock.NewRows([]string{"max"}).AddRow(&expectedTime)

	mock.ExpectQuery(regexp.QuoteMeta("WHERE kind = 'paddle_reconciliation'")).
		WillReturnRows(rows)

	result, err := repo.GetLastReconciliationRun(context.Background())

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, expectedTime, *result)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSystemHealthRepository_GetLastReconciliationRun_ErrNoRows(t *testing.T) {
	mock, repo := setupSystemHealthMockDB(t)
	defer mock.Close()

	mock.ExpectQuery(regexp.QuoteMeta("WHERE kind = 'paddle_reconciliation'")).
		WillReturnError(pgx.ErrNoRows)

	result, err := repo.GetLastReconciliationRun(context.Background())

	assert.ErrorIs(t, err, pgx.ErrNoRows)
	assert.Nil(t, result)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSystemHealthRepository_GetLastReconciliationRun_Error(t *testing.T) {
	mock, repo := setupSystemHealthMockDB(t)
	defer mock.Close()

	dbErr := errors.New("db error")

	mock.ExpectQuery(regexp.QuoteMeta("WHERE kind = 'paddle_reconciliation'")).
		WillReturnError(dbErr)

	result, err := repo.GetLastReconciliationRun(context.Background())

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.NoError(t, mock.ExpectationsWereMet())
}
