package services

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// Issue #1494 — ExecuteDeletion's two Plaid-cleanup gaps, which are opposite failures of the same
// question: what happens when we cannot prove a Plaid Item was removed.
//
//	Part A — plaidCleanupSvc is nil (a Plaid-less deployment: main.go passes nil and only calls
//	SetPlaidCleanupService inside `if cfg.PlaidClientID != "" && cfg.PlaidSecret != ""`). The old code
//	skipped cleanup silently and called DeleteUserCascade anyway. Since #1495 the cascade's C2 guard
//	catches that and returns (false, nil), so the erasure is safe but NEVER COMPLETES, and the actual
//	cause — Plaid is not wired — is stated nowhere. It must fail with a named error when items are
//	outstanding, and still succeed when none are.
//
//	Part B — cleanup reports ErrPlaidRemovalUnrecoverable: every failure is one no retry can fix, and
//	each verdict is durably recorded. Aborting here is what made #1494 permanent. It must proceed —
//	and a GENUINE failure must still abort, which is the assertion that keeps this from being a
//	blanket "ignore Plaid errors" regression.

// newDeletionFixture builds the collaborator set every test below needs, wired for a deletion that
// would succeed. plaidCleanup is nil for the Part A tests.
func newDeletionFixture(t *testing.T, plaidCleanup PlaidItemCleanupService) (
	svc DeletionService,
	repo *MockProfileRepositoryForDeletion,
	clerk *MockClerkService,
	claims *MockWorkClaimsRepository,
	user *domain.User,
) {
	t.Helper()
	repo = new(MockProfileRepositoryForDeletion)
	email := new(MockEmailService)
	clerk = new(MockClerkService)
	audit := new(MockAuditLogger)

	svc = NewDeletionService(repo, email, clerk, audit, plaidCleanup, 30, "test-secret-key")

	user = &domain.User{
		ID:          "user-1494",
		ClerkUserID: "user_1494",
		Email:       "orphan@example.com",
	}

	claims = new(MockWorkClaimsRepository)
	claims.On("Claim", mock.Anything, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, accountDeletionClaimLease).
		Return(testDeletionClaimToken, true, nil)
	claims.On("Release", mock.Anything, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken, mock.AnythingOfType("string")).
		Return(true, nil).Maybe()
	claims.On("Complete", mock.Anything, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken).
		Return(true, nil).Maybe()
	svc.SetWorkClaimsRepo(claims)

	repo.On("GetUserByID", mock.Anything, user.ID).Return(user, nil)
	repo.On("ClaimForDeletion", mock.Anything, user.ID).Return(true, nil)
	// No `user.deleted` expectation: since Issue #1507 that entry is written inside
	// DeleteUserCascade's transaction (repository.LogInTx), never through this interface. Left
	// unstubbed deliberately — an unstubbed testify call panics, so a regression that re-adds the
	// post-commit write here fails loudly instead of silently double-recording.

	return svc, repo, clerk, claims, user
}

// TestExecuteDeletion_NilPlaidCleanup_WithOutstandingItems_RefusesWithNamedError is Part A's headline
// case. DeleteUserCascade and Clerk deletion are left UNSTUBBED on purpose: an unstubbed testify call
// panics, which proves the refusal happens before either, more strongly than AssertNotCalled.
func TestExecuteDeletion_NilPlaidCleanup_WithOutstandingItems_RefusesWithNamedError(t *testing.T) {
	ctx := context.Background()
	svc, repo, clerk, claims, user := newDeletionFixture(t, nil)
	repo.UncleanedLinkedItemCount = 2

	err := svc.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention)

	require.Error(t, err, "with no way to remove them, a user's outstanding Plaid items must stop the deletion loudly, not silently")
	assert.Contains(t, err.Error(), "Plaid cleanup service not configured",
		"the error must name the actual cause — it is otherwise three layers away from the operator")
	assert.Contains(t, err.Error(), "2 linked item(s)",
		"the count is the operator's next action: which Items still need removing at Plaid")
	assert.Equal(t, 1, repo.UncleanedLinkedItemCountCalls,
		"the decision must be made from the guard's own population, not assumed")
	repo.AssertNotCalled(t, "DeleteUserCascade", mock.Anything, mock.Anything, mock.Anything)
	clerk.AssertNotCalled(t, "DeleteUser", mock.Anything, mock.Anything)
	claims.AssertCalled(t, "Release", mock.Anything, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken, mock.AnythingOfType("string"))
	claims.AssertNotCalled(t, "Complete", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestExecuteDeletion_NilPlaidCleanup_NoOutstandingItems_Proceeds is the falsifying counterpart, and
// it is the more dangerous direction to get wrong: refusing here would break every deletion in a
// Plaid-less deployment — including users who never linked a bank at all. (A brief on this branch
// previously proposed a change that would have blocked EVERY account deletion; this is the test that
// would have caught it.)
func TestExecuteDeletion_NilPlaidCleanup_NoOutstandingItems_Proceeds(t *testing.T) {
	ctx := context.Background()
	svc, repo, clerk, claims, user := newDeletionFixture(t, nil)
	repo.UncleanedLinkedItemCount = 0

	repo.On("DeleteUserCascade", mock.Anything, user.ID, mock.Anything).Return(true, nil)
	clerk.On("DeleteUser", mock.Anything, user.ClerkUserID).Return(nil)

	err := svc.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention)

	require.NoError(t, err, "a Plaid-less deployment must still be able to erase a user with no outstanding Plaid work")
	repo.AssertCalled(t, "DeleteUserCascade", mock.Anything, user.ID, mock.Anything)
	claims.AssertCalled(t, "Complete", mock.Anything, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken)
}

// TestExecuteDeletion_NilPlaidCleanup_CountFails_FailsClosed: not knowing whether Plaid work is owed
// is not permission to proceed to an irreversible erasure.
func TestExecuteDeletion_NilPlaidCleanup_CountFails_FailsClosed(t *testing.T) {
	ctx := context.Background()
	svc, repo, clerk, claims, user := newDeletionFixture(t, nil)
	countErr := errors.New("connection refused")
	repo.UncleanedLinkedItemCountErr = countErr

	err := svc.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention)

	require.Error(t, err)
	assert.ErrorIs(t, err, countErr)
	repo.AssertNotCalled(t, "DeleteUserCascade", mock.Anything, mock.Anything, mock.Anything)
	clerk.AssertNotCalled(t, "DeleteUser", mock.Anything, mock.Anything)
	claims.AssertCalled(t, "Release", mock.Anything, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken, mock.AnythingOfType("string"))
}

// TestExecuteDeletion_UnrecoverablePlaidRemoval_ProceedsToCascade is Part B: the specific escape #1494
// asks for. The cascade is reached, and it is the cascade's own C2 guard (re-reading each row) plus its
// plaid_orphaned_items copy — both proved in the repository integration tests — that make proceeding
// safe. This test's job is only that the deletion is no longer abandoned forever.
func TestExecuteDeletion_UnrecoverablePlaidRemoval_ProceedsToCascade(t *testing.T) {
	ctx := context.Background()
	plaidCleanup := new(MockPlaidItemCleanupService)
	svc, repo, clerk, claims, user := newDeletionFixture(t, plaidCleanup)

	unrecoverable := errors.New("wrapped: " + ErrPlaidRemovalUnrecoverable.Error())
	plaidCleanup.On("RemoveAllUserItems", mock.Anything, user.ID).
		Return(errors.Join(ErrPlaidRemovalUnrecoverable, unrecoverable))
	repo.On("DeleteUserCascade", mock.Anything, user.ID, mock.Anything).Return(true, nil)
	clerk.On("DeleteUser", mock.Anything, user.ClerkUserID).Return(nil)

	err := svc.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention)

	require.NoError(t, err, "an item that can NEVER be removed must not block this user's erasure forever — that is #1494")
	repo.AssertCalled(t, "DeleteUserCascade", mock.Anything, user.ID, mock.Anything)
	claims.AssertCalled(t, "Complete", mock.Anything, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken)
	assert.Equal(t, 0, repo.UncleanedLinkedItemCountCalls,
		"the count is the nil-service branch's diagnosis only; with a wired service the pass result is the authority")
}

// TestExecuteDeletion_GenuinePlaidFailure_StillAborts is the guard that keeps the test above from
// being satisfied by an implementation that ignores Plaid errors generally. It duplicates
// TestExecuteDeletion_PlaidCleanupFails_AbortsDeletion's rule deliberately: that test predates the
// sentinel, and this one states the rule as a CONTRAST to it, so a future widening of the sentinel's
// reach fails here with an explanation attached.
func TestExecuteDeletion_GenuinePlaidFailure_StillAborts(t *testing.T) {
	ctx := context.Background()
	plaidCleanup := new(MockPlaidItemCleanupService)
	svc, repo, clerk, claims, user := newDeletionFixture(t, plaidCleanup)

	outage := errors.New("plaid API unavailable")
	plaidCleanup.On("RemoveAllUserItems", mock.Anything, user.ID).Return(outage)

	err := svc.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention)

	require.Error(t, err, "a retryable Plaid failure must still abort — the Item may yet be removable")
	assert.ErrorIs(t, err, outage)
	assert.NotErrorIs(t, err, ErrPlaidRemovalUnrecoverable)
	repo.AssertNotCalled(t, "DeleteUserCascade", mock.Anything, mock.Anything, mock.Anything)
	clerk.AssertNotCalled(t, "DeleteUser", mock.Anything, mock.Anything)
	claims.AssertCalled(t, "Release", mock.Anything, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken, mock.AnythingOfType("string"))
}
