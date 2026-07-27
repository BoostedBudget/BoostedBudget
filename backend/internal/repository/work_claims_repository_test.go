package repository

// Unit tests for WorkClaimsRepository against a pgxmock pool.
//
// IMPORTANT: pgxmock only matches query text/args and cannot validate the
// ON CONFLICT ... WHERE semantics that make Claim's exclusivity guarantee real.
// These tests exist to (a) pin down the query shape and error handling, and
// (b) satisfy the backend coverage floor for code paths not exercised by the
// integration-tagged tests. The actual exclusivity proof lives in
// work_claims_repository_integration_test.go against a live Postgres instance.

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

func setupWorkClaimsMockDB(t *testing.T) (pgxmock.PgxPoolIface, WorkClaimsRepository) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	repo := NewWorkClaimsRepository(mock)
	return mock, repo
}

// --- Constructor ---

func TestNewWorkClaimsRepository_ReturnsNonNil(t *testing.T) {
	mock, repo := setupWorkClaimsMockDB(t)
	defer mock.Close()
	assert.NotNil(t, repo)
}

// --- Action / resource-type constants ---

func TestActionConstants_AreDistinctNonEmptyStrings(t *testing.T) {
	actions := []string{
		ActionPlaidDisconnectPastDue,
		ActionPastDueGraceCancel,
		ActionAccountDeletion,
		ActionRetentionNotice30d,
		ActionRetentionNotice7d,
		ActionSubscriptionCancelledCleanup,
	}
	seen := make(map[string]bool, len(actions))
	for _, a := range actions {
		assert.NotEmpty(t, a)
		assert.False(t, seen[a], "duplicate action constant: %s", a)
		seen[a] = true
	}
	assert.Equal(t, "user", ResourceTypeUser)
}

// --- Claim ---

func TestWorkClaimsRepository_Claim_Wins(t *testing.T) {
	mock, repo := setupWorkClaimsMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO work_claims`)).
		WithArgs(ResourceTypeUser, "user-1", ActionPlaidDisconnectPastDue, 900.0).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	claimed, err := repo.Claim(ctx, ResourceTypeUser, "user-1", ActionPlaidDisconnectPastDue, 15*time.Minute)

	require.NoError(t, err)
	assert.True(t, claimed)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWorkClaimsRepository_Claim_LosesToExistingLease(t *testing.T) {
	mock, repo := setupWorkClaimsMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO work_claims`)).
		WithArgs(ResourceTypeUser, "user-1", ActionAccountDeletion, 60.0).
		WillReturnResult(pgxmock.NewResult("INSERT", 0))

	claimed, err := repo.Claim(ctx, ResourceTypeUser, "user-1", ActionAccountDeletion, time.Minute)

	require.NoError(t, err)
	assert.False(t, claimed)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWorkClaimsRepository_Claim_Error(t *testing.T) {
	mock, repo := setupWorkClaimsMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO work_claims`)).
		WithArgs(ResourceTypeUser, "user-1", ActionAccountDeletion, 60.0).
		WillReturnError(errors.New("db error"))

	claimed, err := repo.Claim(ctx, ResourceTypeUser, "user-1", ActionAccountDeletion, time.Minute)

	assert.Error(t, err)
	assert.False(t, claimed)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- Complete ---

func TestWorkClaimsRepository_Complete_Success(t *testing.T) {
	mock, repo := setupWorkClaimsMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE work_claims`)).
		WithArgs(ResourceTypeUser, "user-1", ActionRetentionNotice30d).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.Complete(ctx, ResourceTypeUser, "user-1", ActionRetentionNotice30d)

	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWorkClaimsRepository_Complete_Error(t *testing.T) {
	mock, repo := setupWorkClaimsMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE work_claims`)).
		WithArgs(ResourceTypeUser, "user-1", ActionRetentionNotice30d).
		WillReturnError(errors.New("db error"))

	err := repo.Complete(ctx, ResourceTypeUser, "user-1", ActionRetentionNotice30d)

	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- Release ---

func TestWorkClaimsRepository_Release_Success(t *testing.T) {
	mock, repo := setupWorkClaimsMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE work_claims`)).
		WithArgs(ResourceTypeUser, "user-1", ActionPastDueGraceCancel, "plaid call failed").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.Release(ctx, ResourceTypeUser, "user-1", ActionPastDueGraceCancel, "plaid call failed")

	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWorkClaimsRepository_Release_Error(t *testing.T) {
	mock, repo := setupWorkClaimsMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE work_claims`)).
		WithArgs(ResourceTypeUser, "user-1", ActionPastDueGraceCancel, "plaid call failed").
		WillReturnError(errors.New("db error"))

	err := repo.Release(ctx, ResourceTypeUser, "user-1", ActionPastDueGraceCancel, "plaid call failed")

	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}
