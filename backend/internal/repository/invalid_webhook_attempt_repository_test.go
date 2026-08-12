package repository

import (
	"context"
	"errors"
	"regexp"
	"testing"

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

	// Call 3 (the interval): insert, then lock+trim in their own transaction.
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO invalid_webhook_attempts")).
		WithArgs("paddle", 1, "d3").WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("pg_advisory_xact_lock")).
		WithArgs("paddle").
		WillReturnResult(pgxmock.NewResult("SELECT", 0))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM invalid_webhook_attempts")).
		WithArgs(invalidWebhookAttemptCap, "paddle").
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

	repo := NewInvalidWebhookAttemptRepository(mock).(*invalidWebhookAttemptRepository)
	require.Equal(t, invalidWebhookAttemptTrimInterval, repo.trimInterval)
	require.Equal(t, int64(invalidWebhookAttemptTrimInterval), repo.effectiveTrimInterval())
}
