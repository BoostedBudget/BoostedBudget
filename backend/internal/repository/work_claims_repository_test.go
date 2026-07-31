package repository

// Unit tests for WorkClaimsRepository against a pgxmock pool.
//
// IMPORTANT: pgxmock only matches query text/args and cannot validate the
// ON CONFLICT ... WHERE semantics that make Claim's exclusivity guarantee real,
// nor the AND lease_token = $n fencing semantics on Complete/Release. These
// tests exist to (a) pin down the query shape and error handling, and (b)
// satisfy the backend coverage floor for code paths not exercised by the
// integration-tagged tests. The actual exclusivity and fencing-token proofs
// live in work_claims_repository_integration_test.go against a live Postgres
// instance.

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
		WithArgs(ResourceTypeUser, "user-1", ActionPlaidDisconnectPastDue, 900.0, pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	token, claimed, err := repo.Claim(ctx, ResourceTypeUser, "user-1", ActionPlaidDisconnectPastDue, 15*time.Minute)

	require.NoError(t, err)
	assert.True(t, claimed)
	assert.NotEmpty(t, token, "a won claim must return a non-empty fencing token")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWorkClaimsRepository_Claim_LosesToExistingLease(t *testing.T) {
	mock, repo := setupWorkClaimsMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO work_claims`)).
		WithArgs(ResourceTypeUser, "user-1", ActionAccountDeletion, 60.0, pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 0))

	token, claimed, err := repo.Claim(ctx, ResourceTypeUser, "user-1", ActionAccountDeletion, time.Minute)

	require.NoError(t, err)
	assert.False(t, claimed)
	assert.Empty(t, token, "a lost claim must return an empty token")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWorkClaimsRepository_Claim_Error(t *testing.T) {
	mock, repo := setupWorkClaimsMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO work_claims`)).
		WithArgs(ResourceTypeUser, "user-1", ActionAccountDeletion, 60.0, pgxmock.AnyArg()).
		WillReturnError(errors.New("db error"))

	token, claimed, err := repo.Claim(ctx, ResourceTypeUser, "user-1", ActionAccountDeletion, time.Minute)

	assert.Error(t, err)
	assert.False(t, claimed)
	assert.Empty(t, token)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- ClaimOutcome (#1413 round-3 finding H3) ---
//
// Claim's bare bool conflates two outcomes that need different caller responses: another
// worker mid-lease (retryable) vs. already completed (terminal). ClaimOutcome is the additive
// widening — a NEW method on a NEW interface (WorkClaimsOutcomeRepository) rather than a
// changed Claim signature, so every existing WorkClaimsRepository implementation/mock needs no
// changes. These tests pin down its query shape; the outcome DISTINCTION itself (which depends
// on real ON CONFLICT ... WHERE semantics pgxmock cannot model) is proven against a live
// Postgres instance in work_claims_repository_integration_test.go.

func TestWorkClaimsRepository_ClaimOutcome_Wins(t *testing.T) {
	mock, repo := setupWorkClaimsMockDB(t)
	defer mock.Close()

	outcomeRepo, ok := repo.(WorkClaimsOutcomeRepository)
	require.True(t, ok, "NewWorkClaimsRepository's concrete type must also satisfy WorkClaimsOutcomeRepository")

	ctx := context.Background()
	rows := pgxmock.NewRows([]string{"state", "token"}).AddRow("won", "fresh-token-1")
	mock.ExpectQuery(regexp.QuoteMeta(`WITH attempt AS`)).
		WithArgs(ResourceTypeUser, "user-1", ActionPlaidDisconnectPastDue, 900.0, pgxmock.AnyArg()).
		WillReturnRows(rows)

	token, outcome, err := outcomeRepo.ClaimOutcome(ctx, ResourceTypeUser, "user-1", ActionPlaidDisconnectPastDue, 15*time.Minute)

	require.NoError(t, err)
	assert.Equal(t, ClaimWon, outcome)
	assert.Equal(t, "fresh-token-1", token)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWorkClaimsRepository_ClaimOutcome_HeldByOther(t *testing.T) {
	mock, repo := setupWorkClaimsMockDB(t)
	defer mock.Close()
	outcomeRepo := repo.(WorkClaimsOutcomeRepository)

	ctx := context.Background()
	rows := pgxmock.NewRows([]string{"state", "token"}).AddRow("held_by_other", nil)
	mock.ExpectQuery(regexp.QuoteMeta(`WITH attempt AS`)).
		WithArgs(ResourceTypeUser, "user-1", ActionSubscriptionCancelledCleanup, 900.0, pgxmock.AnyArg()).
		WillReturnRows(rows)

	token, outcome, err := outcomeRepo.ClaimOutcome(ctx, ResourceTypeUser, "user-1", ActionSubscriptionCancelledCleanup, 15*time.Minute)

	require.NoError(t, err)
	assert.Equal(t, ClaimHeldByOther, outcome)
	assert.Empty(t, token, "a HeldByOther outcome must return an empty token")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWorkClaimsRepository_ClaimOutcome_AlreadyCompleted(t *testing.T) {
	mock, repo := setupWorkClaimsMockDB(t)
	defer mock.Close()
	outcomeRepo := repo.(WorkClaimsOutcomeRepository)

	ctx := context.Background()
	rows := pgxmock.NewRows([]string{"state", "token"}).AddRow("already_completed", nil)
	mock.ExpectQuery(regexp.QuoteMeta(`WITH attempt AS`)).
		WithArgs(ResourceTypeUser, "user-1", ActionSubscriptionCancelledCleanup, 900.0, pgxmock.AnyArg()).
		WillReturnRows(rows)

	token, outcome, err := outcomeRepo.ClaimOutcome(ctx, ResourceTypeUser, "user-1", ActionSubscriptionCancelledCleanup, 15*time.Minute)

	require.NoError(t, err)
	assert.Equal(t, ClaimAlreadyCompleted, outcome)
	assert.Empty(t, token, "an AlreadyCompleted outcome must return an empty token")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWorkClaimsRepository_ClaimOutcome_Error(t *testing.T) {
	mock, repo := setupWorkClaimsMockDB(t)
	defer mock.Close()
	outcomeRepo := repo.(WorkClaimsOutcomeRepository)

	ctx := context.Background()
	mock.ExpectQuery(regexp.QuoteMeta(`WITH attempt AS`)).
		WithArgs(ResourceTypeUser, "user-1", ActionAccountDeletion, 60.0, pgxmock.AnyArg()).
		WillReturnError(errors.New("db error"))

	token, outcome, err := outcomeRepo.ClaimOutcome(ctx, ResourceTypeUser, "user-1", ActionAccountDeletion, time.Minute)

	assert.Error(t, err)
	assert.Empty(t, token)
	assert.Equal(t, ClaimHeldByOther, outcome, "on error, outcome must be the safe non-terminal zero value, never ClaimWon")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWorkClaimsRepository_ClaimOutcome_RejectsNonPositiveLease(t *testing.T) {
	for _, lease := range []time.Duration{0, -time.Second, -time.Hour} {
		mock, repo := setupWorkClaimsMockDB(t)
		outcomeRepo := repo.(WorkClaimsOutcomeRepository)

		ctx := context.Background()
		token, outcome, err := outcomeRepo.ClaimOutcome(ctx, ResourceTypeUser, "user-1", ActionAccountDeletion, lease)

		assert.Error(t, err, "lease=%s must be rejected", lease)
		assert.Empty(t, token)
		assert.Equal(t, ClaimHeldByOther, outcome)
		assert.NoError(t, mock.ExpectationsWereMet())
		mock.Close()
	}
}

// TestClaimOutcome_ZeroValueIsNotWon guards the deliberate iota ordering: the zero value of
// ClaimOutcome must never be ClaimWon, so a forgotten/zeroed variable is never mistaken for
// success by a caller that skips checking err.
func TestClaimOutcome_ZeroValueIsNotWon(t *testing.T) {
	var zero ClaimOutcome
	assert.NotEqual(t, ClaimWon, zero)
	assert.Equal(t, ClaimHeldByOther, zero)
}

// TestWorkClaimsRepository_Claim_RejectsNonPositiveLease covers a bug an
// adversarial review surfaced: a zero or negative lease.Seconds() would make
// make_interval produce a zero/negative interval, so the freshly "claimed" row
// would already be expired — letting a caller re-claim (and re-run destructive
// work) on every subsequent call. Claim must reject this before touching the DB.
func TestWorkClaimsRepository_Claim_RejectsNonPositiveLease(t *testing.T) {
	for _, lease := range []time.Duration{0, -time.Second, -time.Hour} {
		mock, repo := setupWorkClaimsMockDB(t)

		ctx := context.Background()
		token, claimed, err := repo.Claim(ctx, ResourceTypeUser, "user-1", ActionAccountDeletion, lease)

		assert.Error(t, err, "lease=%s must be rejected", lease)
		assert.False(t, claimed)
		assert.Empty(t, token)
		// No SQL should have been attempted at all.
		assert.NoError(t, mock.ExpectationsWereMet())
		mock.Close()
	}
}

// --- Complete ---

func TestWorkClaimsRepository_Complete_Success(t *testing.T) {
	mock, repo := setupWorkClaimsMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE work_claims`)).
		WithArgs(ResourceTypeUser, "user-1", ActionRetentionNotice30d, "token-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	ok, err := repo.Complete(ctx, ResourceTypeUser, "user-1", ActionRetentionNotice30d, "token-1")

	require.NoError(t, err)
	assert.True(t, ok)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWorkClaimsRepository_Complete_Error(t *testing.T) {
	mock, repo := setupWorkClaimsMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE work_claims`)).
		WithArgs(ResourceTypeUser, "user-1", ActionRetentionNotice30d, "token-1").
		WillReturnError(errors.New("db error"))

	ok, err := repo.Complete(ctx, ResourceTypeUser, "user-1", ActionRetentionNotice30d, "token-1")

	assert.Error(t, err)
	assert.False(t, ok)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestWorkClaimsRepository_Complete_NoMatch covers the no-match signal: Complete
// on a tuple+token with no matching row (e.g. a mismatched action from a typo,
// OR a stale/fenced-out token) must return ok=false with no error — Complete no
// longer distinguishes "never claimed" from "token is stale," because the
// caller-side response is identical either way: do not treat the work as
// recorded.
func TestWorkClaimsRepository_Complete_NoMatch(t *testing.T) {
	mock, repo := setupWorkClaimsMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE work_claims`)).
		WithArgs(ResourceTypeUser, "user-1", ActionRetentionNotice30d, "stale-token").
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	ok, err := repo.Complete(ctx, ResourceTypeUser, "user-1", ActionRetentionNotice30d, "stale-token")

	require.NoError(t, err)
	assert.False(t, ok)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- Release ---

func TestWorkClaimsRepository_Release_Success(t *testing.T) {
	mock, repo := setupWorkClaimsMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE work_claims`)).
		WithArgs(ResourceTypeUser, "user-1", ActionPastDueGraceCancel, "token-1", "plaid call failed").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	ok, err := repo.Release(ctx, ResourceTypeUser, "user-1", ActionPastDueGraceCancel, "token-1", "plaid call failed")

	require.NoError(t, err)
	assert.True(t, ok)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWorkClaimsRepository_Release_Error(t *testing.T) {
	mock, repo := setupWorkClaimsMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE work_claims`)).
		WithArgs(ResourceTypeUser, "user-1", ActionPastDueGraceCancel, "token-1", "plaid call failed").
		WillReturnError(errors.New("db error"))

	ok, err := repo.Release(ctx, ResourceTypeUser, "user-1", ActionPastDueGraceCancel, "token-1", "plaid call failed")

	assert.Error(t, err)
	assert.False(t, ok)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestWorkClaimsRepository_Release_NoOpWhenNoMatchingRow documents the deliberate
// asymmetry with a caller's expectations: Release returns ok=false (with no
// error) when zero rows match, whether because the claim never existed, because
// it was already Completed by another worker (Release's WHERE clause excludes
// completed_at IS NOT NULL rows), or because token is stale (another worker has
// since reclaimed this tuple, invalidating the caller's token).
func TestWorkClaimsRepository_Release_NoOpWhenNoMatchingRow(t *testing.T) {
	mock, repo := setupWorkClaimsMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE work_claims`)).
		WithArgs(ResourceTypeUser, "user-1", ActionPastDueGraceCancel, "stale-token", "plaid call failed").
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	ok, err := repo.Release(ctx, ResourceTypeUser, "user-1", ActionPastDueGraceCancel, "stale-token", "plaid call failed")

	require.NoError(t, err)
	assert.False(t, ok)
	assert.NoError(t, mock.ExpectationsWereMet())
}
