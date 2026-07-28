package repository

// Unit tests for JobHealthRepository backed by pgxmock. These only prove the query is issued
// and scanned/wrapped correctly — pgxmock cannot validate the aggregate SQL itself (MIN/COALESCE/
// EXTRACT(EPOCH FROM ...)) or the predicate's type semantics. See
// job_health_repository_integration_test.go (real Postgres, -tags integration) for that proof.

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupJobHealthMockDB(t *testing.T) (pgxmock.PgxPoolIface, JobHealthRepository) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	repo := NewJobHealthRepository(mock)
	return mock, repo
}

func TestJobHealthRepository_GetOldestUnclearedExpiredManualTierOverrideAge_Success(t *testing.T) {
	mock, repo := setupJobHealthMockDB(t)
	defer mock.Close()

	// 5 days, expressed in seconds — as EXTRACT(EPOCH FROM ...) returns.
	fiveDaysSeconds := float64(5 * 24 * 60 * 60)
	rows := pgxmock.NewRows([]string{"age_seconds"}).AddRow(fiveDaysSeconds)

	mock.ExpectQuery(regexp.QuoteMeta("manual_tier_override IS NOT NULL")).
		WillReturnRows(rows)

	age, err := repo.GetOldestUnclearedExpiredManualTierOverrideAge(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 5*24*time.Hour, age)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestJobHealthRepository_GetOldestUnclearedExpiredManualTierOverrideAge_NoRows_ReturnsZero(t *testing.T) {
	mock, repo := setupJobHealthMockDB(t)
	defer mock.Close()

	rows := pgxmock.NewRows([]string{"age_seconds"}).AddRow(float64(0))
	mock.ExpectQuery(regexp.QuoteMeta("manual_tier_override IS NOT NULL")).
		WillReturnRows(rows)

	age, err := repo.GetOldestUnclearedExpiredManualTierOverrideAge(context.Background())

	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), age)
}

func TestJobHealthRepository_GetOldestUnclearedExpiredManualTierOverrideAge_QueryError(t *testing.T) {
	mock, repo := setupJobHealthMockDB(t)
	defer mock.Close()

	mock.ExpectQuery(regexp.QuoteMeta("manual_tier_override IS NOT NULL")).
		WillReturnError(errors.New("db unavailable"))

	age, err := repo.GetOldestUnclearedExpiredManualTierOverrideAge(context.Background())

	assert.Error(t, err)
	assert.Equal(t, time.Duration(0), age)
	assert.NoError(t, mock.ExpectationsWereMet())
}
