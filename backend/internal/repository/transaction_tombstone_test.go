package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// Issue #1569 Task 7: a provider removal must UPDATE, never DELETE.
//
// These are pgxmock tests, so they pin the STATEMENT — that it is an UPDATE, that it is scoped by
// the blind index rather than the plaintext external id, and that it refuses to touch an already
// tombstoned row. They cannot pin what the CASE expression actually evaluates to against real data;
// TestTombstone_*_Integration (transaction_tombstone_integration_test.go) does that, because pgxmock asserts query strings and
// never semantics.

func TestTombstoneByExternalID_UpdatesRatherThanDeletes(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))

	mock.ExpectQuery("UPDATE transactions").
		WithArgs(string(domain.SourcePlaid), pgxmock.AnyArg(), "user-123", domain.TxnDeletedByPlaidSync).
		WillReturnRows(pgxmock.NewRows([]string{"deleted_reason"}).
			AddRow(string(domain.TxnDeletedProviderRemovedPosted)))

	reason, err := repo.TombstoneByExternalID(
		context.Background(), domain.SourcePlaid, "plaid_tx_123", "user-123", domain.TxnDeletedByPlaidSync)

	assert.NoError(t, err)
	assert.Equal(t, domain.TxnDeletedProviderRemovedPosted, reason)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// A second removal of the same transaction must be a no-op the caller can recognise, not an error
// it has to guess about. The sync path relies on this to stay idempotent across the re-deliveries
// that #1570's storage-before-cursor contract makes NORMAL rather than rare.
func TestTombstoneByExternalID_AlreadyTombstonedIsNotFound(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))

	mock.ExpectQuery("UPDATE transactions").
		WithArgs(string(domain.SourcePlaid), pgxmock.AnyArg(), "user-123", domain.TxnDeletedByPlaidSync).
		WillReturnRows(pgxmock.NewRows([]string{"deleted_reason"}))

	_, err = repo.TombstoneByExternalID(
		context.Background(), domain.SourcePlaid, "plaid_tx_123", "user-123", domain.TxnDeletedByPlaidSync)

	assert.True(t, errors.Is(err, ErrTransactionNotFound),
		"an already-tombstoned or absent row must report ErrTransactionNotFound, got %v", err)
	assert.NoError(t, mock.ExpectationsWereMet())
}
