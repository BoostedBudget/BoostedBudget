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

func TestSystemHealthRepository_GetRiverJobStats_Success(t *testing.T) {
	mock, repo := setupSystemHealthMockDB(t)
	defer mock.Close()

	rows := pgxmock.NewRows([]string{"pending", "running", "failed"}).
		AddRow(int64(5), int64(2), int64(1))

	mock.ExpectQuery(regexp.QuoteMeta("FROM river_job")).
		WithArgs("default").
		WillReturnRows(rows)

	metrics, err := repo.GetRiverJobStats(context.Background(), "default")

	require.NoError(t, err)
	assert.Equal(t, &domain.QueueMetrics{Pending: 5, Running: 2, Failed: 1}, metrics)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSystemHealthRepository_GetRiverJobStats_Error(t *testing.T) {
	mock, repo := setupSystemHealthMockDB(t)
	defer mock.Close()

	mock.ExpectQuery(regexp.QuoteMeta("FROM river_job")).
		WithArgs("default").
		WillReturnError(errors.New("db error"))

	metrics, err := repo.GetRiverJobStats(context.Background(), "default")

	assert.Error(t, err)
	assert.Nil(t, metrics)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSystemHealthRepository_GetRiverJobsCompletedLastHour_Success(t *testing.T) {
	mock, repo := setupSystemHealthMockDB(t)
	defer mock.Close()

	rows := pgxmock.NewRows([]string{"count"}).AddRow(int64(42))

	mock.ExpectQuery(regexp.QuoteMeta("AND state = 'completed'")).
		WithArgs("default").
		WillReturnRows(rows)

	count, err := repo.GetRiverJobsCompletedLastHour(context.Background(), "default")

	require.NoError(t, err)
	assert.Equal(t, int64(42), count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSystemHealthRepository_GetRiverJobsCompletedLastHour_Error(t *testing.T) {
	mock, repo := setupSystemHealthMockDB(t)
	defer mock.Close()

	mock.ExpectQuery(regexp.QuoteMeta("AND state = 'completed'")).
		WithArgs("default").
		WillReturnError(errors.New("db error"))

	count, err := repo.GetRiverJobsCompletedLastHour(context.Background(), "default")

	assert.Error(t, err)
	assert.Equal(t, int64(0), count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSystemHealthRepository_GetActiveSyncCount_Success(t *testing.T) {
	mock, repo := setupSystemHealthMockDB(t)
	defer mock.Close()

	mock.ExpectQuery(regexp.QuoteMeta("WHERE kind = 'plaid_sync'")).
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(int64(3)))

	count, err := repo.GetActiveSyncCount(context.Background())

	require.NoError(t, err)
	assert.Equal(t, int64(3), count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSystemHealthRepository_GetActiveSyncCount_Error(t *testing.T) {
	mock, repo := setupSystemHealthMockDB(t)
	defer mock.Close()

	mock.ExpectQuery(regexp.QuoteMeta("WHERE kind = 'plaid_sync'")).
		WillReturnError(errors.New("db error"))

	count, err := repo.GetActiveSyncCount(context.Background())

	assert.Error(t, err)
	assert.Equal(t, int64(0), count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSystemHealthRepository_GetFailedSyncsLastHour_Success(t *testing.T) {
	mock, repo := setupSystemHealthMockDB(t)
	defer mock.Close()

	mock.ExpectQuery(regexp.QuoteMeta("FROM linked_items")).
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(int64(7)))

	count, err := repo.GetFailedSyncsLastHour(context.Background())

	require.NoError(t, err)
	assert.Equal(t, int64(7), count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSystemHealthRepository_GetFailedSyncsLastHour_Error(t *testing.T) {
	mock, repo := setupSystemHealthMockDB(t)
	defer mock.Close()

	mock.ExpectQuery(regexp.QuoteMeta("FROM linked_items")).
		WillReturnError(errors.New("db error"))

	count, err := repo.GetFailedSyncsLastHour(context.Background())

	assert.Error(t, err)
	assert.Equal(t, int64(0), count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSystemHealthRepository_GetAverageSyncDuration_Success(t *testing.T) {
	mock, repo := setupSystemHealthMockDB(t)
	defer mock.Close()

	mock.ExpectQuery(regexp.QuoteMeta("COALESCE(AVG")).
		WillReturnRows(pgxmock.NewRows([]string{"avg_ms"}).AddRow(int64(1500)))

	avgMs, err := repo.GetAverageSyncDuration(context.Background())

	require.NoError(t, err)
	assert.Equal(t, int64(1500), avgMs)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSystemHealthRepository_GetAverageSyncDuration_Error(t *testing.T) {
	mock, repo := setupSystemHealthMockDB(t)
	defer mock.Close()

	mock.ExpectQuery(regexp.QuoteMeta("COALESCE(AVG")).
		WillReturnError(errors.New("db error"))

	avgMs, err := repo.GetAverageSyncDuration(context.Background())

	assert.Error(t, err)
	assert.Equal(t, int64(0), avgMs)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSystemHealthRepository_GetActiveSubscriptionCount_Success(t *testing.T) {
	mock, repo := setupSystemHealthMockDB(t)
	defer mock.Close()

	mock.ExpectQuery(regexp.QuoteMeta("paddle_subscription_id IS NOT NULL")).
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(int64(99)))

	count, err := repo.GetActiveSubscriptionCount(context.Background())

	require.NoError(t, err)
	assert.Equal(t, int64(99), count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSystemHealthRepository_GetActiveSubscriptionCount_Error(t *testing.T) {
	mock, repo := setupSystemHealthMockDB(t)
	defer mock.Close()

	mock.ExpectQuery(regexp.QuoteMeta("paddle_subscription_id IS NOT NULL")).
		WillReturnError(errors.New("db error"))

	count, err := repo.GetActiveSubscriptionCount(context.Background())

	assert.Error(t, err)
	assert.Equal(t, int64(0), count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSystemHealthRepository_GetPaddleWebhookStats24h_Success(t *testing.T) {
	mock, repo := setupSystemHealthMockDB(t)
	defer mock.Close()

	mock.ExpectQuery(regexp.QuoteMeta("FROM paddle_webhook_events")).
		WillReturnRows(pgxmock.NewRows([]string{"total", "failed"}).AddRow(int64(200), int64(4)))

	total, failed, err := repo.GetPaddleWebhookStats24h(context.Background())

	require.NoError(t, err)
	assert.Equal(t, int64(200), total)
	assert.Equal(t, int64(4), failed)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSystemHealthRepository_GetPaddleWebhookStats24h_Error(t *testing.T) {
	mock, repo := setupSystemHealthMockDB(t)
	defer mock.Close()

	mock.ExpectQuery(regexp.QuoteMeta("FROM paddle_webhook_events")).
		WillReturnError(errors.New("db error"))

	total, failed, err := repo.GetPaddleWebhookStats24h(context.Background())

	assert.Error(t, err)
	assert.Equal(t, int64(0), total)
	assert.Equal(t, int64(0), failed)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSystemHealthRepository_GetLastReconciliationRun_Success(t *testing.T) {
	mock, repo := setupSystemHealthMockDB(t)
	defer mock.Close()

	expectedTime := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta("WHERE kind = 'paddle_reconciliation'")).
		WillReturnRows(pgxmock.NewRows([]string{"max"}).AddRow(&expectedTime))

	result, err := repo.GetLastReconciliationRun(context.Background())

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, expectedTime, *result)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSystemHealthRepository_GetLastReconciliationRun_NullResult(t *testing.T) {
	// MAX() always returns one row; NULL means no completed reconciliation jobs exist.
	mock, repo := setupSystemHealthMockDB(t)
	defer mock.Close()

	mock.ExpectQuery(regexp.QuoteMeta("WHERE kind = 'paddle_reconciliation'")).
		WillReturnRows(pgxmock.NewRows([]string{"max"}).AddRow((*time.Time)(nil)))

	result, err := repo.GetLastReconciliationRun(context.Background())

	require.NoError(t, err)
	assert.Nil(t, result)
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

	mock.ExpectQuery(regexp.QuoteMeta("WHERE kind = 'paddle_reconciliation'")).
		WillReturnError(errors.New("db error"))

	result, err := repo.GetLastReconciliationRun(context.Background())

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.NoError(t, mock.ExpectationsWereMet())
}
