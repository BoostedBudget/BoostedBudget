package services

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// ─── Round 15, Tier A: the residue mechanism tracked the wrong fact ─────────────────────────────
//
// #1504 and round 12 both hung the residue record off ONE in-memory bool — `subscriptionCancelled`,
// set only by CancelSubscriptionImmediately succeeding and read only at the end of ExecuteDeletion.
// That bool is a PROXY for "an irreversible external effect landed", and it was a bad proxy in five
// independent ways. These tests pin the fix at the root: the fact recorded is now
// "THIS SPECIFIC IRREVERSIBLE CALL IS ABOUT TO BE MADE", one record per effect, written to the
// database immediately BEFORE the call rather than inferred from a variable at the end.
//
//	A1  ArchiveAndAnonymizeCustomer — the act the code itself calls "THE ONE ACTION NOTHING CAN
//	    UNDO" — set no flag at all, and neither residue effect constant named it. An
//	    archive-without-cancel could not be recorded by any path.
//	A2  A cancel that changes Paddle state but returns an error (timeout, lost response) never set
//	    the flag, so the generic Paddle-failure path recorded nothing.
//	A3  An ordinary error after a cancel that HAD landed — and process death or a panic — lost the
//	    flag with the process.
//	A4  A recognised-transient cascade error deliberately recorded nothing. See
//	    TestExecuteDeletion_CascadeFailsTransientlyAfterPaddleCancel_StillHasTheLandingRecord and
//	    the A4 note in deletion_service.go for the decision and how it engages round 14's argument.
//	A5  The residue write was issued on the caller's possibly-cancelled ctx, unlike every Release in
//	    the same function.
//
// ⚠ WHAT MAKES THIS SAFE TO WRITE EAGERLY: a landing record is not permanent. DeleteUserCascade
// deletes this user's residue rows inside the SAME transaction that deletes the user, so a deletion
// that COMPLETES leaves none — see the integration siblings in
// deletion_service_effect_residue_integration_test.go, which are the only place the net effect can
// actually be observed (a mock repository cannot show you a row that was written and then removed).

// TestExecuteDeletion_CascadeFailsAfterCustomerArchive_RecordsTheArchiveResidue is A1's RED test,
// and it is the deterministic retention-path case: an ex-subscriber (raw status "cancelled", so
// there is no subscription left to cancel) whose Paddle CUSTOMER is still archived and anonymised —
// unrecoverable by us or by Paddle — and whose deletion then fails.
//
// Before this change the archive set no flag, so recordPaddleCancelResidue early-returned and an
// Info log was the entire trace.
func TestExecuteDeletion_CascadeFailsAfterCustomerArchive_RecordsTheArchiveResidue(t *testing.T) {
	ctx := context.Background()
	h := newRevocationHarnessWithStatus(t, ctx, "cancelled")

	h.repo.ExpectedCalls = nil
	h.repo.On("GetUserByID", ctx, h.userID).Return(h.user, nil)
	h.repo.On("ClaimForDeletion", ctx, h.userID).Return(true, nil)
	h.repo.On("RecordDeletionResidue", mock.Anything, h.userID, mock.Anything).Return(nil).Maybe()
	h.repo.On("DeleteUserCascade", ctx, h.userID, mock.Anything).Return(false, wrapPgError("42501")).
		Run(func(mock.Arguments) { *h.callOrder = append(*h.callOrder, "DeleteUserCascade") })

	require.Error(t, h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindUserRequested))

	h.paddle.AssertNotCalled(t, "CancelSubscriptionImmediately", mock.Anything, mock.Anything)
	assert.Contains(t, *h.callOrder, "PaddleArchive",
		"the archive really did happen — this is the effect the record must follow")
	h.repo.AssertCalled(t, "RecordDeletionResidue", mock.Anything, h.userID,
		repository.DeletionResiduePaddleCustomerArchived)
}

// The same A1 harm on the OTHER path a user survives: the cascade's own guard declining, which is
// not an error at all. Stated separately because a fix that only touched the cascade-error branch
// would pass the test above and fail this one.
func TestExecuteDeletion_CascadeRefusesAfterCustomerArchive_RecordsTheArchiveResidue(t *testing.T) {
	ctx := context.Background()
	h := newRevocationHarnessWithStatus(t, ctx, "cancelled")

	h.repo.ExpectedCalls = nil
	h.repo.On("GetUserByID", ctx, h.userID).Return(h.user, nil)
	h.repo.On("ClaimForDeletion", ctx, h.userID).Return(true, nil)
	h.repo.On("RecordDeletionResidue", mock.Anything, h.userID, mock.Anything).Return(nil).Maybe()
	h.repo.On("DeleteUserCascade", ctx, h.userID, mock.Anything).Return(false, nil)

	require.NoError(t, h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindUserRequested),
		"a cascade that refuses is a normal, expected outcome")

	h.repo.AssertCalled(t, "RecordDeletionResidue", mock.Anything, h.userID,
		repository.DeletionResiduePaddleCustomerArchived)
}

// A2's RED test — AMBIGUOUS ACKNOWLEDGEMENT. Paddle received the cancellation and acted on it; the
// response never made it back. `subscriptionCancelled` stayed false, the run aborted down the
// generic Paddle-failure path, and nothing was recorded. Since the record is now written BEFORE the
// call, the ambiguity is resolved in the only honest direction: the operator is told the call was
// issued and may have landed.
func TestExecuteDeletion_SubscriptionCancelReturnsAnError_RecordsTheResidue(t *testing.T) {
	ctx := context.Background()
	h := newRevocationHarness(t, ctx)

	h.paddle.ExpectedCalls = nil
	h.paddle.On("CancelSubscriptionImmediately", ctx, h.subID).
		Return(errors.New("504 gateway timeout — Paddle may or may not have acted")).
		Run(func(mock.Arguments) { *h.callOrder = append(*h.callOrder, "PaddleCancel") })

	require.Error(t, h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindUserRequested))

	h.paddle.AssertNotCalled(t, "ArchiveAndAnonymizeCustomer", mock.Anything, mock.Anything, mock.Anything)
	h.repo.AssertCalled(t, "RecordDeletionResidue", mock.Anything, h.userID,
		repository.DeletionResiduePaddleSubscriptionCancelled)
}

// ⚠ SIBLING, MANDATORY — the boundary of A2's fix. ErrSideEffectClaimBusy means the Paddle call was
// NEVER ISSUED: claimSideEffectAndRun refuses before invoking the closure. That is not ambiguous, it
// is a known nothing-happened, and recording it would put a work item in front of an operator for a
// subscription that is demonstrably untouched. This is the assertion that stops "record on every
// cancel error" from satisfying the test above.
func TestExecuteDeletion_SubscriptionCancelClaimBusy_RecordsNoResidue(t *testing.T) {
	ctx := context.Background()
	h := newRevocationHarness(t, ctx)

	claims := new(MockWorkClaimsRepository)
	claims.On("Claim", mock.Anything, repository.ResourceTypeUser, mock.Anything, repository.ActionAccountDeletion, accountDeletionClaimLease).
		Return(testDeletionClaimToken, true, nil).Maybe()
	claims.On("Complete", mock.Anything, repository.ResourceTypeUser, mock.Anything, repository.ActionAccountDeletion, testDeletionClaimToken).
		Return(true, nil).Maybe()
	claims.On("Release", mock.Anything, repository.ResourceTypeUser, mock.Anything, repository.ActionAccountDeletion, testDeletionClaimToken, mock.AnythingOfType("string")).
		Return(true, nil).Maybe()
	// The side-effect claim is HELD BY ANOTHER FLOW: no token, not claimed.
	claims.On("Claim", mock.Anything, repository.ResourceTypeUser, mock.Anything, repository.ActionPaddleCancelSubscription, paddleCancelSideEffectLease).
		Return("", false, nil).Maybe()
	h.svc.SetWorkClaimsRepo(claims)

	require.Error(t, h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindUserRequested))

	h.paddle.AssertNotCalled(t, "CancelSubscriptionImmediately", mock.Anything, mock.Anything)
	h.repo.AssertNotCalled(t, "RecordDeletionResidue", mock.Anything, mock.Anything, mock.Anything)
}

// A3's RED test for the in-process half: an ordinary error AFTER a cancel that is known to have
// succeeded. The archive fails; cleanupPaddleResources returns (true, err); the caller's generic
// Paddle-failure branch recorded nothing, because round 12 reasoned that an errored run still
// retries into success. BOTH effects must now be on record — the cancel landed, and the archive was
// issued and may have landed too.
func TestExecuteDeletion_ArchiveFailsAfterCancelLanded_RecordsBothEffects(t *testing.T) {
	ctx := context.Background()
	h := newRevocationHarness(t, ctx)

	h.paddle.ExpectedCalls = nil
	h.paddle.On("CancelSubscriptionImmediately", ctx, h.subID).Return(nil).
		Run(func(mock.Arguments) { *h.callOrder = append(*h.callOrder, "PaddleCancel") })
	h.paddle.On("ArchiveAndAnonymizeCustomer", ctx, h.custID, expectedAnonymizedEmail).
		Return(errors.New("paddle 500")).
		Run(func(mock.Arguments) { *h.callOrder = append(*h.callOrder, "PaddleArchive") })

	require.Error(t, h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindUserRequested))

	h.repo.AssertCalled(t, "RecordDeletionResidue", mock.Anything, h.userID,
		repository.DeletionResiduePaddleSubscriptionCancelled)
	h.repo.AssertCalled(t, "RecordDeletionResidue", mock.Anything, h.userID,
		repository.DeletionResiduePaddleCustomerArchived)
}

// A5's RED test. Every Release in ExecuteDeletion runs on context.WithoutCancel because the same
// shutdown or deadline that caused the failure has usually cancelled ctx too — and releasing with a
// cancelled context silently fails to release. The residue write had exactly that bug and none of
// that protection: it is written on the failure path, from the same ctx, and a cancelled ctx made it
// a no-op that logged an error nobody was going to correlate.
//
// The assertion is on the CONTEXT THE REPOSITORY IS HANDED, not on a call count: a write issued on a
// dead context still "happens" from the caller's point of view and still records nothing.
func TestExecuteDeletion_ResidueWriteSurvivesACancelledCallerContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := newRevocationHarness(t, ctx)

	var residueCtxErr error
	seen := false
	h.repo.ExpectedCalls = nil
	h.repo.On("GetUserByID", ctx, h.userID).Return(h.user, nil)
	h.repo.On("ClaimForDeletion", ctx, h.userID).Return(true, nil)
	h.repo.On("RecordDeletionResidue", mock.Anything, h.userID, mock.Anything).Return(nil).
		Run(func(args mock.Arguments) {
			if !seen {
				seen = true
				residueCtxErr = args.Get(0).(context.Context).Err()
			}
		})
	h.repo.On("DeleteUserCascade", ctx, h.userID, mock.Anything).Return(false, nil).Maybe()

	// The job's context dies while the Plaid leg is in flight — the realistic shape: a deploy or a
	// shutdown cancels the worker, and the very next thing the code does is handle a failure.
	h.plaid.ExpectedCalls = nil
	h.plaid.On("RemoveAllUserItems", ctx, h.userID).Return(nil).
		Run(func(mock.Arguments) {
			*h.callOrder = append(*h.callOrder, "PlaidRemoveAll")
			cancel()
		})

	_ = h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindUserRequested)

	require.True(t, seen, "the residue write must still be attempted after the caller's context died")
	require.NoError(t, residueCtxErr,
		"the residue write must be issued on context.WithoutCancel + a timeout, exactly like every Release in the same function")
}
