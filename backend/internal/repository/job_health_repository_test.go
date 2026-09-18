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

func TestJobHealthRepository_GetOldestExpiredCompGrantAge_Success(t *testing.T) {
	mock, repo := setupJobHealthMockDB(t)
	defer mock.Close()

	// 5 days, expressed in seconds — as EXTRACT(EPOCH FROM ...) returns.
	fiveDaysSeconds := float64(5 * 24 * 60 * 60)
	rows := pgxmock.NewRows([]string{"age_seconds"}).AddRow(fiveDaysSeconds)

	mock.ExpectQuery(regexp.QuoteMeta("FROM revenuecat_customers rc")).
		WillReturnRows(rows)

	age, err := repo.GetOldestExpiredCompGrantAge(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 5*24*time.Hour, age)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestJobHealthRepository_GetOldestExpiredCompGrantAge_NoRows_ReturnsZero(t *testing.T) {
	mock, repo := setupJobHealthMockDB(t)
	defer mock.Close()

	rows := pgxmock.NewRows([]string{"age_seconds"}).AddRow(float64(0))
	mock.ExpectQuery(regexp.QuoteMeta("FROM revenuecat_customers rc")).
		WillReturnRows(rows)

	age, err := repo.GetOldestExpiredCompGrantAge(context.Background())

	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), age)
}

func TestJobHealthRepository_GetOldestExpiredCompGrantAge_QueryError(t *testing.T) {
	mock, repo := setupJobHealthMockDB(t)
	defer mock.Close()

	mock.ExpectQuery(regexp.QuoteMeta("FROM revenuecat_customers rc")).
		WillReturnError(errors.New("db unavailable"))

	age, err := repo.GetOldestExpiredCompGrantAge(context.Background())

	assert.Error(t, err)
	assert.Equal(t, time.Duration(0), age)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestJobHealthRepository_OldestExpiredCompGrantAgeQuery_ScopesToCompGrants is the drift guard, and
// it runs without a database so it cannot be skipped. The gauge must count ONLY comp_* grants (a
// paid_* or trial_* grant lapsing is a normal subscription event, not this hazard), and it must
// read RC's stored projection rather than the retired legacy column.
func TestJobHealthRepository_OldestExpiredCompGrantAgeQuery_ScopesToCompGrants(t *testing.T) {
	assert.Contains(t, oldestExpiredCompGrantAgeQuery, "revenuecat_customers",
		"the gauge must read RevenueCat's stored projection, not the retired legacy column")
	assert.Contains(t, oldestExpiredCompGrantAgeQuery, `LIKE 'comp\_%'`,
		"only comp_* grants count; the underscore must be escaped so it is not a LIKE wildcard")
	assert.NotContains(t, oldestExpiredCompGrantAgeQuery, "manual_tier_override",
		"the retired legacy column must not reappear in this query")
}

// --- GetOutstandingPlaidCleanupStats (Issue #1413 — Plaid-disconnect-repair backlog gauge) ---
//
// pgxmock only proves the query is issued and the two columns are scanned/wrapped correctly — it
// cannot validate the EXISTS-subquery liveness join or the aggregate SQL (COUNT, MIN, COALESCE,
// EXTRACT(EPOCH FROM ...)). See job_health_repository_plaid_cleanup_integration_test.go (real
// Postgres, -tags integration) for that proof — in particular the migration-000119/000120
// residue trap (a marker set with zero live linked items must NOT be counted).

func TestJobHealthRepository_GetOutstandingPlaidCleanupStats_Success(t *testing.T) {
	mock, repo := setupJobHealthMockDB(t)
	defer mock.Close()

	threeDaysSeconds := float64(3 * 24 * 60 * 60)
	rows := pgxmock.NewRows([]string{"count", "age_seconds"}).AddRow(int64(2), threeDaysSeconds)

	mock.ExpectQuery(regexp.QuoteMeta("plaid_cleanup_pending_at IS NOT NULL")).
		WillReturnRows(rows)

	count, age, err := repo.GetOutstandingPlaidCleanupStats(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 2, count)
	assert.Equal(t, 3*24*time.Hour, age)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestJobHealthRepository_GetOutstandingPlaidCleanupStats_NoRows_ReturnsZero(t *testing.T) {
	mock, repo := setupJobHealthMockDB(t)
	defer mock.Close()

	rows := pgxmock.NewRows([]string{"count", "age_seconds"}).AddRow(int64(0), float64(0))
	mock.ExpectQuery(regexp.QuoteMeta("plaid_cleanup_pending_at IS NOT NULL")).
		WillReturnRows(rows)

	count, age, err := repo.GetOutstandingPlaidCleanupStats(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.Equal(t, time.Duration(0), age)
}

func TestJobHealthRepository_GetOutstandingPlaidCleanupStats_QueryError(t *testing.T) {
	mock, repo := setupJobHealthMockDB(t)
	defer mock.Close()

	mock.ExpectQuery(regexp.QuoteMeta("plaid_cleanup_pending_at IS NOT NULL")).
		WillReturnError(errors.New("db unavailable"))

	count, age, err := repo.GetOutstandingPlaidCleanupStats(context.Background())

	assert.Error(t, err)
	assert.Equal(t, 0, count)
	assert.Equal(t, time.Duration(0), age)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- GetDeletionResidueStats (Issue #1504 — the post-gate external-effect residue gauge) ---
//
// pgxmock proves the query is issued and both columns are scanned/wrapped correctly. It cannot prove
// the predicate selects the right rows, nor that the partial index serves it — see
// job_health_repository_deletion_residue_integration_test.go (real Postgres, -tags integration).

func TestJobHealthRepository_GetDeletionResidueStats_Success(t *testing.T) {
	mock, repo := setupJobHealthMockDB(t)
	defer mock.Close()

	twoHoursSeconds := float64(2 * 60 * 60)
	rows := pgxmock.NewRows([]string{"count", "age_seconds"}).AddRow(int64(4), twoHoursSeconds)

	mock.ExpectQuery(regexp.QuoteMeta("user.deletion_residue.%")).WillReturnRows(rows)

	count, newest, err := repo.GetDeletionResidueStats(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 4, count)
	assert.Equal(t, 2*time.Hour, newest)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestJobHealthRepository_GetDeletionResidueStats_NoRows_ReturnsZero(t *testing.T) {
	mock, repo := setupJobHealthMockDB(t)
	defer mock.Close()

	rows := pgxmock.NewRows([]string{"count", "age_seconds"}).AddRow(int64(0), float64(0))
	mock.ExpectQuery(regexp.QuoteMeta("user.deletion_residue.%")).WillReturnRows(rows)

	count, newest, err := repo.GetDeletionResidueStats(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.Equal(t, time.Duration(0), newest)
}

func TestJobHealthRepository_GetDeletionResidueStats_QueryError(t *testing.T) {
	mock, repo := setupJobHealthMockDB(t)
	defer mock.Close()

	mock.ExpectQuery(regexp.QuoteMeta("user.deletion_residue.%")).
		WillReturnError(errors.New("db unavailable"))

	count, newest, err := repo.GetDeletionResidueStats(context.Background())

	assert.Error(t, err)
	assert.Equal(t, 0, count)
	assert.Equal(t, time.Duration(0), newest)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestJobHealthRepository_GetDeadLetterQueueSizeCountsOnlyUnresolvedRows(t *testing.T) {
	mockDB, repo := setupJobHealthMockDB(t)
	mockDB.ExpectQuery("SELECT COUNT.*FROM dead_letter_jobs.*NOT.*retried_at IS NOT NULL").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(int64(7)))

	count, err := repo.GetDeadLetterQueueSize(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 7, count)
	assert.NoError(t, mockDB.ExpectationsWereMet())
}

// TestJobHealthRepository_DeletionResidueStatsQuery_UsesTheWriterSharedPrefix is the drift guard, and
// it runs without a database so it cannot be skipped. The read predicate and the write's action are
// the same Go constant; a second hand-typed literal here is exactly how a gauge comes to report zero
// forever while records are being written.
func TestJobHealthRepository_DeletionResidueStatsQuery_UsesTheWriterSharedPrefix(t *testing.T) {
	assert.Contains(t, deletionResidueStatsQuery, "'"+deletionResidueActionPrefix+"%'",
		"the LIKE pattern must be built from deletionResidueActionPrefix — the same constant RecordDeletionResidue writes")
	assert.Contains(t, deletionResidueStatsQuery, "MAX(created_at)",
		"the age reported is the NEWEST record's, not the oldest: nothing prunes audit_logs, so MIN would climb forever and tell an operator nothing about whether this just happened")
}

// --- RevenueCat backlog gauges (#1652 todo 12) ---
//
// pgxmock proves the query is issued and the columns are scanned/wrapped correctly. The SQL
// semantics (the pending predicate, the comp-grant liveness rule, the MIN/COALESCE aggregate) are
// pinned by the drift guards below, which run without a database.

func TestJobHealthRepository_GetRevenueCatWebhookInboxStats_Success(t *testing.T) {
	mock, repo := setupJobHealthMockDB(t)
	defer mock.Close()

	rows := pgxmock.NewRows([]string{"count", "age_seconds"}).AddRow(int64(3), float64(120))
	mock.ExpectQuery(regexp.QuoteMeta("FROM revenuecat_webhooks")).WillReturnRows(rows)

	count, age, err := repo.GetRevenueCatWebhookInboxStats(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 3, count)
	assert.Equal(t, 2*time.Minute, age)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestJobHealthRepository_GetRevenueCatWebhookInboxStats_QueryError(t *testing.T) {
	mock, repo := setupJobHealthMockDB(t)
	defer mock.Close()

	mock.ExpectQuery(regexp.QuoteMeta("FROM revenuecat_webhooks")).
		WillReturnError(errors.New("db unavailable"))

	count, age, err := repo.GetRevenueCatWebhookInboxStats(context.Background())

	assert.Error(t, err)
	assert.Equal(t, 0, count)
	assert.Equal(t, time.Duration(0), age)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestJobHealthRepository_GetRevenueCatOperationsStats_Success(t *testing.T) {
	mock, repo := setupJobHealthMockDB(t)
	defer mock.Close()

	rows := pgxmock.NewRows([]string{"pending", "errored", "age_seconds"}).AddRow(int64(5), int64(1), float64(600))
	mock.ExpectQuery(regexp.QuoteMeta("FROM revenuecat_operations")).WillReturnRows(rows)

	pending, errored, age, err := repo.GetRevenueCatOperationsStats(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 5, pending)
	assert.Equal(t, 1, errored)
	assert.Equal(t, 10*time.Minute, age)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestJobHealthRepository_GetRevenueCatOperationsStats_QueryError(t *testing.T) {
	mock, repo := setupJobHealthMockDB(t)
	defer mock.Close()

	mock.ExpectQuery(regexp.QuoteMeta("FROM revenuecat_operations")).
		WillReturnError(errors.New("db unavailable"))

	pending, errored, age, err := repo.GetRevenueCatOperationsStats(context.Background())

	assert.Error(t, err)
	assert.Equal(t, 0, pending)
	assert.Equal(t, 0, errored)
	assert.Equal(t, time.Duration(0), age)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestJobHealthRepository_GetRevenueCatCompGrantsOutstanding_Success(t *testing.T) {
	mock, repo := setupJobHealthMockDB(t)
	defer mock.Close()

	mock.ExpectQuery(regexp.QuoteMeta("LIKE 'comp\\_%'")).
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(int64(4)))

	count, err := repo.GetRevenueCatCompGrantsOutstanding(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 4, count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestJobHealthRepository_GetRevenueCatRefreshOverdueUsers_Success(t *testing.T) {
	mock, repo := setupJobHealthMockDB(t)
	defer mock.Close()

	mock.ExpectQuery(regexp.QuoteMeta("next_refresh_at < NOW()")).
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(int64(9)))

	count, err := repo.GetRevenueCatRefreshOverdueUsers(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 9, count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestJobHealthRepository_GetRevenueCatRefreshOverdueUsers_QueryError(t *testing.T) {
	mock, repo := setupJobHealthMockDB(t)
	defer mock.Close()

	mock.ExpectQuery(regexp.QuoteMeta("next_refresh_at < NOW()")).
		WillReturnError(errors.New("db unavailable"))

	count, err := repo.GetRevenueCatRefreshOverdueUsers(context.Background())

	assert.Error(t, err)
	assert.Equal(t, 0, count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestJobHealthRepository_RevenueCatBacklogQueries_PinTheirPredicates are the drift guards. The
// operations one is the sharpest: `errored` must count only rows that are STILL pending with a
// last_error, because there is no 'failed' stage — a query that looked for one would report zero
// forever while failures accumulated.
func TestJobHealthRepository_RevenueCatBacklogQueries_PinTheirPredicates(t *testing.T) {
	assert.Contains(t, revenueCatWebhookInboxStatsQuery, "processed_at IS NULL",
		"the inbox backlog is unprocessed receipts")
	assert.Contains(t, revenueCatWebhookInboxStatsQuery, "MIN(received_at)",
		"the age is the OLDEST unprocessed receipt's")

	assert.Contains(t, revenueCatOperationsStatsQuery, "stage = 'pending'")
	assert.Contains(t, revenueCatOperationsStatsQuery, "last_error <> ''",
		"errored is stage='pending' AND last_error <> '' — there is no 'failed' stage")
	assert.Contains(t, revenueCatOperationsStatsQuery, "MIN(created_at)",
		"the age is the OLDEST pending intent's")

	assert.Contains(t, revenueCatCompGrantsOutstandingQuery, `LIKE 'comp\_%'`,
		"only comp_* grants count; the underscore must be escaped so it is not a LIKE wildcard")
	assert.Contains(t, revenueCatCompGrantsOutstandingQuery, "> NOW()",
		"a comp grant is outstanding only while its expiry is still in the future (or absent)")

	assert.Contains(t, revenueCatRefreshOverdueUsersQuery, "next_refresh_at < NOW()",
		"the refresh backlog is customers whose next_refresh_at has elapsed")
}
