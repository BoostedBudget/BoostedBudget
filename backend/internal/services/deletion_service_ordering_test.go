package services

import (
	"context"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// ─── Round 15, Tier D: two orderings that spend irreversible external effects on a run that was
// ─── already certain to be refused ───────────────────────────────────────────────────────────────
//
//	D2  The household-owner precondition was evaluated ONLY inside DeleteUserCascade's transaction —
//	    the last thing ExecuteDeletion does — although it is a pure local-state question needing no
//	    external call. And, uniquely among that function's stops, its own guard comment records that
//	    it CANNOT SELF-HEAL. The retention path never passes through RequestDeletion's #1367 check
//	    (retention_cleanup_check.go calls ExecuteDeletion in process), so a retention-expired
//	    household owner had their banks disconnected and their Paddle customer archived and
//	    anonymised, was refused, and had the whole sequence repeated the next day, indefinitely.
//
//	D1  ErrPlaidRemovalUnrecoverable proceeded past a terminal Plaid failure on the argument that
//	    DeleteUserCascade's C2 guard "still blocks unless the verdict really is recorded there". But
//	    C2 blocks while `plaid_removal_failed_at > now() - INTERVAL '7 days'`, and the verdict was
//	    written by that same pass — so C2 was GUARANTEED to refuse for a week, after the irreversible
//	    Paddle work had already gone out, every day, each run now also emitting its own residue
//	    record.
//
// ⚠ BOTH FIXES MOVE A CHECK EARLIER; NEITHER REMOVES ONE. DeleteUserCascade's in-transaction
// household guard and its C2 guard are untouched and remain the authority — see the sibling
// assertions below, which pin that these pre-checks cannot be satisfied by deleting the guards they
// anticipate. Neither fix makes an erasure EASIER: each can only stop a run, never permit one.

// ─── D2 ──────────────────────────────────────────────────────────────────────────────────────────

// The headline case. A household owner whose retention clock has expired must have NO external
// effect performed on them — not one Plaid disconnect, not the subscription cancel, and above all
// not ArchiveAndAnonymizeCustomer, which this file calls the one action nothing can undo.
//
// The assertion is bound to its specific cause: `callOrder` is empty, so the failure is "no external
// call was made", not merely "the deletion returned an error". A guard that refused for some other
// reason AFTER the Plaid leg would leave "PlaidRemoveAll" in it and fail here.
func TestExecuteDeletion_HouseholdOwnerBlocks_PerformsNoExternalEffects(t *testing.T) {
	ctx := context.Background()
	h := newRevocationHarness(t, ctx)
	h.repo.HouseholdOwnerBlocks = true

	err := h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindRetention)

	require.Error(t, err, "this stop cannot self-heal, so it must be a visible, alertable error — not a silent skip")
	assert.ErrorIs(t, err, domain.ErrHouseholdHasActiveMembers,
		"the error must name the household-ownership cause; that is the operator's next action")
	assert.Equal(t, 1, h.repo.HouseholdOwnerBlocksCalls,
		"the refusal must come from the precondition itself, not from some later guard")
	assert.Empty(t, *h.callOrder,
		"a household owner's retention deletion must spend NOTHING irreversible: no Plaid disconnect, "+
			"no subscription cancel, and no Paddle customer archive")
	h.plaid.AssertNotCalled(t, "RemoveAllUserItems", mock.Anything, mock.Anything)
	h.paddle.AssertNotCalled(t, "CancelSubscriptionImmediately", mock.Anything, mock.Anything)
	h.paddle.AssertNotCalled(t, "ArchiveAndAnonymizeCustomer", mock.Anything, mock.Anything, mock.Anything)
	h.repo.AssertNotCalled(t, "DeleteUserCascade", mock.Anything, mock.Anything, mock.Anything)
	h.clerk.AssertNotCalled(t, "DeleteUser", mock.Anything, mock.Anything)
	// No irreversible effect was issued, so there is nothing for an operator to reconcile.
	h.repo.AssertNotCalled(t, "RecordDeletionResidue", mock.Anything, mock.Anything, mock.Anything)
}

// TestExecuteDeletion_HouseholdOwnerBlocks_IncrementsTheBlockedCounter is the EE re-review's
// finding. The reorder above is correct about WHERE to refuse and was wrong about what the
// refusal reports: the only HouseholdOwnerDeletionBlockedTotal.Inc() lives inside
// DeleteUserCascade, which the early guard now never reaches — so the one purpose-built alert
// for a GDPR deletion that cannot self-heal went permanently flat, and the comment at the guard
// claiming the metric is "left to the cascade's guard" was false the moment the reorder landed.
//
// The counter still increments at most once per occasion: the cascade's own Inc() only runs on
// the race this precheck cannot see (an invite accepted between the two reads), which is a
// DIFFERENT occasion, not a double count of this one.
func TestExecuteDeletion_HouseholdOwnerBlocks_IncrementsTheBlockedCounter(t *testing.T) {
	ctx := context.Background()
	h := newRevocationHarness(t, ctx)
	h.repo.HouseholdOwnerBlocks = true

	before := testutil.ToFloat64(observability.HouseholdOwnerDeletionBlockedTotal)
	err := h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindRetention)
	require.Error(t, err)

	after := testutil.ToFloat64(observability.HouseholdOwnerDeletionBlockedTotal)
	assert.Equal(t, before+1, after,
		"the pre-cleanup refusal must fire the same alert the in-cascade guard fires; it is the only "+
			"dedicated signal that a GDPR erasure is blocked on a household nothing can resolve automatically")
}

// The MANDATORY sibling: the counter must not move on a deletion that is NOT household-blocked,
// or the assertion above would pass against an unconditional Inc().
func TestExecuteDeletion_NoHouseholdBlock_LeavesTheBlockedCounterAlone(t *testing.T) {
	ctx := context.Background()
	h := newRevocationHarness(t, ctx)

	before := testutil.ToFloat64(observability.HouseholdOwnerDeletionBlockedTotal)
	_ = h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindRetention)

	assert.Equal(t, before, testutil.ToFloat64(observability.HouseholdOwnerDeletionBlockedTotal))
}

// ⚠ SIBLING, MANDATORY — and it is the more dangerous direction to get wrong. A precondition that
// refused everyone would stop every account deletion in the product, which is a GDPR failure far
// worse than the one being fixed. A NON-owner (or an owner of a household with nobody else in it)
// must be processed exactly as before, all the way through the cascade.
func TestExecuteDeletion_HouseholdOwnerDoesNotBlock_ProcessedNormally(t *testing.T) {
	ctx := context.Background()
	h := newRevocationHarness(t, ctx)
	h.repo.HouseholdOwnerBlocks = false

	err := h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindRetention)

	require.NoError(t, err)
	assert.Equal(t, 1, h.repo.HouseholdOwnerBlocksCalls,
		"the precondition is asked on every run — its answer, not its absence, is what lets this one through")
	assert.Equal(t, []string{"PlaidRemoveAll", "PaddleCancel", "PaddleArchive", "DeleteUserCascade"}, *h.callOrder,
		"an unblocked user's deletion must still perform the full sequence in the established order")
	h.clerk.AssertCalled(t, "DeleteUser", ctx, h.user.ClerkUserID)
}

// Not knowing whether erasing this user would destroy a DIFFERENT user's data is not permission to
// start disconnecting banks. Fails closed, like every other undeterminable answer in this function.
func TestExecuteDeletion_HouseholdOwnerCheckFails_FailsClosedBeforeAnyExternalWork(t *testing.T) {
	ctx := context.Background()
	h := newRevocationHarness(t, ctx)
	probeErr := errors.New("connection refused")
	h.repo.HouseholdOwnerBlocksErr = probeErr

	err := h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindRetention)

	require.Error(t, err)
	assert.ErrorIs(t, err, probeErr)
	assert.Empty(t, *h.callOrder, "an undeterminable precondition must stop the run before the first external call")
	h.paddle.AssertNotCalled(t, "ArchiveAndAnonymizeCustomer", mock.Anything, mock.Anything, mock.Anything)
	h.repo.AssertNotCalled(t, "DeleteUserCascade", mock.Anything, mock.Anything, mock.Anything)
}

// ⚠ DEFENCE IN DEPTH, PINNED. The pre-check runs outside the cascade's transaction and its FOR
// UPDATE lock, so a household member accepted in the interval between them is caught ONLY by the
// in-cascade guard. This test states that contract at the service boundary: the cascade still gets
// to refuse, and that refusal is still honoured, even when the pre-check said "clear". The guard's
// own SQL behaviour is pinned in
// repository/profile_repository_delete_user_cascade_integration_test.go — this is the assertion that
// stops the guard from being deleted as "now redundant".
func TestExecuteDeletion_PreCheckClearButCascadeStillRefuses_UserSurvives(t *testing.T) {
	ctx := context.Background()
	h := newRevocationHarness(t, ctx)
	h.repo.HouseholdOwnerBlocks = false

	h.repo.ExpectedCalls = nil
	h.repo.On("GetUserByID", ctx, h.userID).Return(h.user, nil)
	h.repo.On("ClaimForDeletion", ctx, h.userID).Return(true, nil)
	h.repo.On("RecordDeletionResidue", mock.Anything, h.userID, mock.Anything).Return(nil).Maybe()
	// The membership changed after the pre-check: the in-transaction guard refuses.
	h.repo.On("DeleteUserCascade", ctx, h.userID, mock.Anything).Return(false, nil).
		Run(func(mock.Arguments) { *h.callOrder = append(*h.callOrder, "DeleteUserCascade") })

	require.NoError(t, h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindRetention),
		"a cascade that refuses is a normal, expected outcome")

	h.clerk.AssertNotCalled(t, "DeleteUser", mock.Anything, mock.Anything)
}

// ─── D1 ──────────────────────────────────────────────────────────────────────────────────────────

// D1's headline case. Every remaining Plaid removal failed terminally, and this pass is what wrote
// those verdicts — so C2's seven-day cooling-off window makes the cascade's refusal a certainty
// before the Paddle leg is even reached. The run must defer instead of spending the cancel and the
// archive on it.
//
// ⚠ The assertion is bound to THIS cause specifically. `callOrder` contains exactly the Plaid
// attempt: the Plaid work is not skipped (it must keep happening — the daily retries during the
// window are what convert the terminal verdict from an inference into an observation), only the
// IRREVERSIBLE Paddle work is withheld. A fix that aborted the whole Plaid leg would leave
// callOrder empty and fail here.
func TestExecuteDeletion_UnrecoverablePlaidRemovalInsideCoolingOffWindow_SpendsNoPaddleEffects(t *testing.T) {
	ctx := context.Background()
	h := newRevocationHarness(t, ctx)
	// Rows the C2 guard's own predicate still counts as outstanding: the terminal verdict is younger
	// than seven days, so the cascade cannot pass.
	h.repo.UncleanedLinkedItemCount = 1

	h.plaid.ExpectedCalls = nil
	h.plaid.On("RemoveAllUserItems", ctx, h.userID).Return(ErrPlaidRemovalUnrecoverable).
		Run(func(mock.Arguments) { *h.callOrder = append(*h.callOrder, "PlaidRemoveAll") })

	err := h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindUserRequested)

	require.NoError(t, err, "a bounded, self-resolving wait is a legitimate outcome, not a failure")
	assert.Equal(t, []string{"PlaidRemoveAll"}, *h.callOrder,
		"the Plaid attempt still happens — the retries are the point of the window — but the "+
			"subscription cancel and the customer archive must not be spent on a run the cascade guard "+
			"will certainly refuse")
	assert.Equal(t, 1, h.repo.UncleanedLinkedItemCountCalls,
		"the deferral must be decided from the guard's own population, not assumed from the sentinel alone")
	h.paddle.AssertNotCalled(t, "CancelSubscriptionImmediately", mock.Anything, mock.Anything)
	h.paddle.AssertNotCalled(t, "ArchiveAndAnonymizeCustomer", mock.Anything, mock.Anything, mock.Anything)
	h.repo.AssertNotCalled(t, "DeleteUserCascade", mock.Anything, mock.Anything, mock.Anything)
	h.clerk.AssertNotCalled(t, "DeleteUser", mock.Anything, mock.Anything)
	// Nothing irreversible was issued, so there is no residue row — which is the amplification round
	// 15 flagged: the old flow emitted one of these per day for the whole window, with no dedupe.
	h.repo.AssertNotCalled(t, "RecordDeletionResidue", mock.Anything, mock.Anything, mock.Anything)
}

// ⚠ SIBLING, MANDATORY — this is what keeps the fix from being a re-run of #1494's permanent abort.
// Once the verdict has aged past the seven-day window, C2's own predicate stops counting those rows,
// the count falls to zero, and the erasure completes exactly as #1494 requires. The deferral above
// is bounded by the same window the guard imposes and by nothing else.
func TestExecuteDeletion_UnrecoverablePlaidRemovalPastCoolingOffWindow_CompletesTheErasure(t *testing.T) {
	ctx := context.Background()
	h := newRevocationHarness(t, ctx)
	// The verdict is older than seven days, so uncleanedLinkedItemPredicate no longer matches it.
	h.repo.UncleanedLinkedItemCount = 0

	h.plaid.ExpectedCalls = nil
	h.plaid.On("RemoveAllUserItems", ctx, h.userID).Return(ErrPlaidRemovalUnrecoverable).
		Run(func(mock.Arguments) { *h.callOrder = append(*h.callOrder, "PlaidRemoveAll") })

	err := h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindUserRequested)

	require.NoError(t, err, "an Item that can NEVER be removed must not block this user's erasure forever — that is #1494")
	assert.Equal(t, []string{"PlaidRemoveAll", "PaddleCancel", "PaddleArchive", "DeleteUserCascade"}, *h.callOrder,
		"past the window the flow is unchanged: the erasure completes")
	h.clerk.AssertCalled(t, "DeleteUser", ctx, h.user.ClerkUserID)
}

// Not knowing whether the cascade guard will refuse is not permission to archive a Paddle customer.
func TestExecuteDeletion_UnrecoverablePlaidRemovalAndCountFails_FailsClosedBeforePaddle(t *testing.T) {
	ctx := context.Background()
	h := newRevocationHarness(t, ctx)
	countErr := errors.New("connection refused")
	h.repo.UncleanedLinkedItemCountErr = countErr

	h.plaid.ExpectedCalls = nil
	h.plaid.On("RemoveAllUserItems", ctx, h.userID).Return(ErrPlaidRemovalUnrecoverable).
		Run(func(mock.Arguments) { *h.callOrder = append(*h.callOrder, "PlaidRemoveAll") })

	err := h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindUserRequested)

	require.Error(t, err)
	assert.ErrorIs(t, err, countErr)
	assert.Equal(t, []string{"PlaidRemoveAll"}, *h.callOrder)
	h.paddle.AssertNotCalled(t, "ArchiveAndAnonymizeCustomer", mock.Anything, mock.Anything, mock.Anything)
	h.repo.AssertNotCalled(t, "DeleteUserCascade", mock.Anything, mock.Anything, mock.Anything)
}

// ⚠ SIBLING, MANDATORY — the boundary of D1's deferral. A GENUINE (retryable) Plaid failure must
// still ABORT with an error, not defer quietly: the Item may yet be removable and the operator's
// signal is different. This is the assertion that stops "defer on any Plaid error" from satisfying
// the tests above.
func TestExecuteDeletion_GenuinePlaidFailureWithOutstandingItems_StillAbortsWithError(t *testing.T) {
	ctx := context.Background()
	h := newRevocationHarness(t, ctx)
	h.repo.UncleanedLinkedItemCount = 1

	outage := errors.New("plaid API unavailable")
	h.plaid.ExpectedCalls = nil
	h.plaid.On("RemoveAllUserItems", ctx, h.userID).Return(outage).
		Run(func(mock.Arguments) { *h.callOrder = append(*h.callOrder, "PlaidRemoveAll") })

	err := h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindUserRequested)

	require.Error(t, err, "a retryable Plaid failure must still abort — the Item may yet be removable")
	assert.ErrorIs(t, err, outage)
	assert.NotErrorIs(t, err, ErrPlaidRemovalUnrecoverable)
	assert.Equal(t, 0, h.repo.UncleanedLinkedItemCountCalls,
		"the cooling-off deferral belongs to the terminal-verdict branch only; a retryable failure "+
			"must not be routed through it")
	h.paddle.AssertNotCalled(t, "ArchiveAndAnonymizeCustomer", mock.Anything, mock.Anything, mock.Anything)
}
