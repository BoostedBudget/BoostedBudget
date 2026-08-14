package repository

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/require"
)

var errInvalidWebhookAttemptDB = errors.New("db error")

// setupInvalidWebhookAttemptMockDB creates a mock database and a repository with a small,
// deterministic trim interval — tests are in-package, so they construct the struct directly
// (bypassing NewInvalidWebhookAttemptRepository's production default) to make trim scheduling
// exact and fast to exercise.
func setupInvalidWebhookAttemptMockDB(t *testing.T, trimInterval int) (pgxmock.PgxPoolIface, *invalidWebhookAttemptRepository) {
	t.Helper()

	mock, err := pgxmock.NewPool()
	require.NoError(t, err)

	repo := &invalidWebhookAttemptRepository{db: mock, trimInterval: trimInterval}
	return mock, repo
}

// TestInvalidWebhookAttemptRecord_CheapPath_NoLockNoTrim is Issue #1514 Codex review round 2,
// Important 2: the steady-state cost of a rejected webhook must be a single unlocked INSERT — no
// BEGIN, no advisory lock, no DELETE. Configuring pgxmock with ONLY the insert expectation proves
// this: pgxmock errors on any unexpected call, so if Record tried to Begin/lock/trim here, this
// test would fail.
func TestInvalidWebhookAttemptRecord_CheapPath_NoLockNoTrim(t *testing.T) {
	mock, repo := setupInvalidWebhookAttemptMockDB(t, 10) // interval=10, this is call #1
	defer mock.Close()

	ctx := context.Background()

	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO invalid_webhook_attempts")).
		WithArgs("paddle", 42, "deadbeef").
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err := repo.Record(ctx, "paddle", 42, "deadbeef")
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestInvalidWebhookAttemptRecord_MultipleCheapCalls_StayCheap proves the cheap path holds across
// repeated calls right up to (but not including) the interval boundary — not just on the very
// first call.
func TestInvalidWebhookAttemptRecord_MultipleCheapCalls_StayCheap(t *testing.T) {
	mock, repo := setupInvalidWebhookAttemptMockDB(t, 5)
	defer mock.Close()

	ctx := context.Background()

	// Calls 1-4 of an interval-5 schedule: plain inserts only, no lock/trim.
	for i := 0; i < 4; i++ {
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO invalid_webhook_attempts")).
			WithArgs("plaid", 1, "digest").
			WillReturnResult(pgxmock.NewResult("INSERT", 1))
	}

	for i := 0; i < 4; i++ {
		require.NoError(t, repo.Record(ctx, "plaid", 1, "digest"))
	}
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestInvalidWebhookAttemptRecord_IntervalReached_LocksAndTrims verifies that the Nth call (per
// the configured interval) is the one that pays for the advisory lock + bounded DELETE, in its
// own transaction, separate from the (already-committed) plain inserts before it.
func TestInvalidWebhookAttemptRecord_IntervalReached_LocksAndTrims(t *testing.T) {
	mock, repo := setupInvalidWebhookAttemptMockDB(t, 3)
	defer mock.Close()

	ctx := context.Background()

	// Calls 1-2: cheap.
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO invalid_webhook_attempts")).
		WithArgs("paddle", 1, "d1").WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO invalid_webhook_attempts")).
		WithArgs("paddle", 1, "d2").WillReturnResult(pgxmock.NewResult("INSERT", 1))

	// Call 3 (the interval): insert, then lock+trim (count-based, then age-based) in their own
	// transaction.
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO invalid_webhook_attempts")).
		WithArgs("paddle", 1, "d3").WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("pg_advisory_xact_lock")).
		WithArgs("paddle").
		WillReturnResult(pgxmock.NewResult("SELECT", 0))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM invalid_webhook_attempts")).
		WithArgs(invalidWebhookAttemptCap, "paddle").
		WillReturnResult(pgxmock.NewResult("DELETE", 0))
	// Issue #1514 G3: trim() now also runs an age-based DELETE (independent of the count-based
	// one above) in the same transaction. The cutoff timestamp is computed from time.Now(), so
	// only the provider arg is asserted exactly.
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM invalid_webhook_attempts")).
		WithArgs("paddle", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectCommit()

	require.NoError(t, repo.Record(ctx, "paddle", 1, "d1"))
	require.NoError(t, repo.Record(ctx, "paddle", 1, "d2"))
	require.NoError(t, repo.Record(ctx, "paddle", 1, "d3"))
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestInvalidWebhookAttemptRecord_InsertFails_NoTransactionToRollBack verifies that an insert
// failure surfaces as an error. The insert is a plain, unlocked Exec (no surrounding
// transaction), so there is nothing to roll back — only ExpectationsWereMet needs to hold.
func TestInvalidWebhookAttemptRecord_InsertFails_NoTransactionToRollBack(t *testing.T) {
	mock, repo := setupInvalidWebhookAttemptMockDB(t, 10)
	defer mock.Close()

	ctx := context.Background()

	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO invalid_webhook_attempts")).
		WithArgs("plaid", 10, "abc123").
		WillReturnError(errInvalidWebhookAttemptDB)

	err := repo.Record(ctx, "plaid", 10, "abc123")
	require.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestInvalidWebhookAttemptRecord_TrimFails_RollsBack verifies a failed trim rolls back its OWN
// transaction (the insert immediately before it already committed independently).
func TestInvalidWebhookAttemptRecord_TrimFails_RollsBack(t *testing.T) {
	mock, repo := setupInvalidWebhookAttemptMockDB(t, 1) // interval=1: every call trims
	defer mock.Close()

	ctx := context.Background()

	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO invalid_webhook_attempts")).
		WithArgs("paddle", 5, "cafebabe").
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("pg_advisory_xact_lock")).
		WithArgs("paddle").
		WillReturnResult(pgxmock.NewResult("SELECT", 0))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM invalid_webhook_attempts")).
		WithArgs(invalidWebhookAttemptCap, "paddle").
		WillReturnError(errInvalidWebhookAttemptDB)
	mock.ExpectRollback()

	err := repo.Record(ctx, "paddle", 5, "cafebabe")
	require.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestNewInvalidWebhookAttemptRepository_DefaultsTrimInterval verifies the production
// constructor wires up the real default interval (not zero, which would divide/mod incorrectly
// and would also mean effectiveTrimInterval's fallback is silently masking a bug here).
func TestNewInvalidWebhookAttemptRepository_DefaultsTrimInterval(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewInvalidWebhookAttemptRepository(mock, 0).(*invalidWebhookAttemptRepository)
	require.Equal(t, invalidWebhookAttemptTrimInterval, repo.trimInterval)
	require.Equal(t, int64(invalidWebhookAttemptTrimInterval), repo.effectiveTrimInterval())
	// Issue #1514 G3: 0 (no explicit override) falls back to the production retention default,
	// and the production constructor always enables the once-per-(instance,provider) startup
	// trim that closes the restart hole.
	require.Equal(t, invalidWebhookAttemptRetentionDefaultDays*24*time.Hour, repo.effectiveRetentionWindow())
	require.True(t, repo.startupTrimEnabled)
}

// TestNewInvalidWebhookAttemptRepository_RetentionOverride verifies a non-zero retentionDays
// argument is honored rather than always falling back to the default (Issue #1514 G3).
func TestNewInvalidWebhookAttemptRepository_RetentionOverride(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewInvalidWebhookAttemptRepository(mock, 7).(*invalidWebhookAttemptRepository)
	require.Equal(t, 7*24*time.Hour, repo.effectiveRetentionWindow())
}

// TestInvalidWebhookAttemptRecord_StartupForcesTrimOnFirstCallOnly verifies the exact scope of
// the Issue #1514 G3 fix at the unit level: startupTrimEnabled forces a trim on the FIRST Record
// call for a provider (even though the interval is far from reached), but NOT on the second call
// for that same provider — proving this is a once-per-(instance,provider) cost, not a permanent
// escalation of the steady-state path.
func TestInvalidWebhookAttemptRecord_StartupForcesTrimOnFirstCallOnly(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewInvalidWebhookAttemptRepository(mock, 30).(*invalidWebhookAttemptRepository)
	repo.trimInterval = 50 // production-shaped: far from reached by either of the two calls below

	ctx := context.Background()

	// Call 1: insert, then a FORCED trim (lock + both DELETEs) even though count=1 is nowhere
	// near the interval boundary.
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO invalid_webhook_attempts")).
		WithArgs("plaid", 1, "first").
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("pg_advisory_xact_lock")).
		WithArgs("plaid").
		WillReturnResult(pgxmock.NewResult("SELECT", 0))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM invalid_webhook_attempts")).
		WithArgs(invalidWebhookAttemptCap, "plaid").
		WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM invalid_webhook_attempts")).
		WithArgs("plaid", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectCommit()

	// Call 2: plain insert only — no lock, no trim. If startupTrimEnabled leaked into forcing a
	// trim on every call instead of just the first, pgxmock's unexpected-call check would fail
	// this test.
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO invalid_webhook_attempts")).
		WithArgs("plaid", 1, "second").
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	require.NoError(t, repo.Record(ctx, "plaid", 1, "first"))
	require.NoError(t, repo.Record(ctx, "plaid", 1, "second"))
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestInvalidWebhookAttemptGetStats24h_MultipleProviders_ReturnsPerProviderCountsAndOverallMostRecent
// is Issue #1514 requirement #5: the repository read method that restores operator visibility.
// Proves the per-provider counts come back distinctly (not summed) and MostRecentAt reflects the
// single latest row ACROSS providers, not either provider's own latest in isolation.
func TestInvalidWebhookAttemptGetStats24h_MultipleProviders_ReturnsPerProviderCountsAndOverallMostRecent(t *testing.T) {
	mock, repo := setupInvalidWebhookAttemptMockDB(t, 10)
	defer mock.Close()

	ctx := context.Background()

	paddleLatest := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	plaidLatest := time.Date(2026, 8, 12, 9, 0, 0, 0, time.UTC) // earlier than paddle's

	rows := pgxmock.NewRows([]string{"provider", "count", "max_created_at"}).
		AddRow("paddle", int64(7), paddleLatest).
		AddRow("plaid", int64(3), plaidLatest)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT provider, COUNT(*), MAX(created_at)")).
		WillReturnRows(rows)

	byProvider, mostRecentAt, err := repo.GetStats24h(ctx)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())

	require.Equal(t, map[string]int64{"paddle": 7, "plaid": 3}, byProvider,
		"per-provider counts must stay distinct, not be summed into a single number")
	require.NotNil(t, mostRecentAt)
	require.True(t, mostRecentAt.Equal(paddleLatest),
		"MostRecentAt must be the latest timestamp ACROSS providers (paddle's), not plaid's own latest or the first row scanned")
}

// TestInvalidWebhookAttemptGetStats24h_NoAttempts_ReturnsEmptyMapAndNilMostRecent proves the zero
// state is unambiguous: no rows in the trailing window means an empty map and a nil timestamp, not
// a struct that could be confused with "the query failed".
func TestInvalidWebhookAttemptGetStats24h_NoAttempts_ReturnsEmptyMapAndNilMostRecent(t *testing.T) {
	mock, repo := setupInvalidWebhookAttemptMockDB(t, 10)
	defer mock.Close()

	ctx := context.Background()

	rows := pgxmock.NewRows([]string{"provider", "count", "max_created_at"})
	mock.ExpectQuery(regexp.QuoteMeta("SELECT provider, COUNT(*), MAX(created_at)")).
		WillReturnRows(rows)

	byProvider, mostRecentAt, err := repo.GetStats24h(ctx)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
	require.Empty(t, byProvider)
	require.Nil(t, mostRecentAt)
}

// TestInvalidWebhookAttemptGetStats24h_QueryFails_ReturnsError proves a query failure surfaces as
// an error rather than a silently-empty result — the caller (services.systemHealthService) relies
// on this to distinguish "zero attacks" from "we don't know" (domain.RejectedWebhookAttemptMetrics.Available).
func TestInvalidWebhookAttemptGetStats24h_QueryFails_ReturnsError(t *testing.T) {
	mock, repo := setupInvalidWebhookAttemptMockDB(t, 10)
	defer mock.Close()

	ctx := context.Background()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT provider, COUNT(*), MAX(created_at)")).
		WillReturnError(errInvalidWebhookAttemptDB)

	byProvider, mostRecentAt, err := repo.GetStats24h(ctx)
	require.Error(t, err)
	require.Nil(t, byProvider)
	require.Nil(t, mostRecentAt)
	require.NoError(t, mock.ExpectationsWereMet())
}
