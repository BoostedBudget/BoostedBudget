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

// setupInvalidWebhookAttemptMockDB creates a mock database and repository for testing.
func setupInvalidWebhookAttemptMockDB(t *testing.T) (pgxmock.PgxPoolIface, InvalidWebhookAttemptRepository) {
	t.Helper()

	mock, err := pgxmock.NewPool()
	require.NoError(t, err)

	repo := NewInvalidWebhookAttemptRepository(mock)
	return mock, repo
}

// TestInvalidWebhookAttemptRecord_InsertsAndTrims verifies that Record runs the insert and the
// cap-enforcing trim inside a single transaction (begin -> insert -> trim -> commit).
func TestInvalidWebhookAttemptRecord_InsertsAndTrims(t *testing.T) {
	mock, repo := setupInvalidWebhookAttemptMockDB(t)
	defer mock.Close()

	ctx := context.Background()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("pg_advisory_xact_lock")).
		WithArgs(invalidWebhookAttemptLockKey).
		WillReturnResult(pgxmock.NewResult("SELECT", 0))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO invalid_webhook_attempts")).
		WithArgs("paddle", 42, "deadbeef").
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM invalid_webhook_attempts")).
		WithArgs(invalidWebhookAttemptCap).
		WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectCommit()

	err := repo.Record(ctx, "paddle", 42, "deadbeef")
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestInvalidWebhookAttemptRecord_InsertFails_RollsBack verifies a failed insert rolls back and
// surfaces an error rather than silently swallowing it.
func TestInvalidWebhookAttemptRecord_InsertFails_RollsBack(t *testing.T) {
	mock, repo := setupInvalidWebhookAttemptMockDB(t)
	defer mock.Close()

	ctx := context.Background()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("pg_advisory_xact_lock")).
		WithArgs(invalidWebhookAttemptLockKey).
		WillReturnResult(pgxmock.NewResult("SELECT", 0))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO invalid_webhook_attempts")).
		WithArgs("plaid", 10, "abc123").
		WillReturnError(errInvalidWebhookAttemptDB)
	mock.ExpectRollback()

	err := repo.Record(ctx, "plaid", 10, "abc123")
	require.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestInvalidWebhookAttemptRecord_TrimFails_RollsBack verifies a failed trim rolls back too.
func TestInvalidWebhookAttemptRecord_TrimFails_RollsBack(t *testing.T) {
	mock, repo := setupInvalidWebhookAttemptMockDB(t)
	defer mock.Close()

	ctx := context.Background()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("pg_advisory_xact_lock")).
		WithArgs(invalidWebhookAttemptLockKey).
		WillReturnResult(pgxmock.NewResult("SELECT", 0))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO invalid_webhook_attempts")).
		WithArgs("paddle", 5, "cafebabe").
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM invalid_webhook_attempts")).
		WithArgs(invalidWebhookAttemptCap).
		WillReturnError(errInvalidWebhookAttemptDB)
	mock.ExpectRollback()

	err := repo.Record(ctx, "paddle", 5, "cafebabe")
	require.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}
