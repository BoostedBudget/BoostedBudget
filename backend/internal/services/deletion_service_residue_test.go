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

// ─── Issue #1504: the post-gate external-effect residue, made visible ───────────────────────────
//
// ExecuteDeletion now re-verifies authorisation immediately before each irreversible external call,
// and runs Plaid first / Paddle last so the least-reversible act is last and always immediately
// gated. ONE residue is irreducible: if the deletion is revoked while the Paddle subscription-cancel
// call is IN FLIGHT, the cancel still lands. The gate passed a moment earlier; a network call cannot
// be recalled.
//
// The user then keeps their account and all their local data — and their subscription is cancelled.
// Until this change the only trace was an Info log line: nobody was told, nothing was queryable.
//
// WHAT IS DELIBERATELY NOT DONE: no automatic re-subscribe. Re-creating a subscription (and a charge)
// on a user's behalf from a partially-failed deletion is a worse failure than telling a human. The
// goal is VISIBILITY.
//
// WHY IT IS RECORDED AGAINST THE USER. Unlike plaid_orphaned_items — the precedent for a record that
// outlives its subject, which carries no user_id at all — this record belongs to a SURVIVING user:
// the defining fact of the case is that the deletion did NOT complete (the user cancelled it, or the
// cascade refused), so the users row is still there and the operator's whole task is to find that
// person and restore what was taken from them. Keying it to the user is therefore correct rather
// than a leak, and it means the erasure redaction (`UPDATE audit_logs SET changes = NULL WHERE
// resource_id = $1 OR user_id = $1`) applies to it if that user IS eventually erased — at which
// point the residue is moot anyway, because the subscription cancellation the record is about has
// become the intended outcome.

// TestExecuteDeletion_RevokedWhilePaddleCancelInFlight_RecordsTheResidue is the RED test. It is the
// same narrow window as
// TestExecuteDeletion_AuthorisationRevokedBetweenPaddleCalls_DoesNotAnonymiseTheCustomer — that test
// pins what must NOT happen next; this one pins that what ALREADY happened is written down.
func TestExecuteDeletion_RevokedWhilePaddleCancelInFlight_RecordsTheResidue(t *testing.T) {
	ctx := context.Background()
	h := newRevocationHarness(t, ctx)

	h.paddle.ExpectedCalls = nil
	h.paddle.On("CancelSubscriptionImmediately", ctx, h.subID).Return(nil).
		Run(func(mock.Arguments) {
			*h.callOrder = append(*h.callOrder, "PaddleCancel")
			*h.authorised = false // the user cancels their deletion while Paddle is answering
		})
	h.paddle.On("ArchiveAndAnonymizeCustomer", ctx, h.custID, expectedAnonymizedEmail).Return(nil).Maybe()

	err := h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindUserRequested)
	require.NoError(t, err, "a cancelled deletion is a legitimate outcome, not a failure")

	h.repo.AssertCalled(t, "RecordDeletionResidue", mock.Anything, h.userID,
		repository.DeletionResiduePaddleSubscriptionCancelled)
}

// SIBLING ASSERTION, mandatory: an ordinary successful deletion must leave NO such record. Without
// it, the test above passes on a version that records unconditionally — which would tell an operator
// nothing at all, since every deletion would raise the same flag.
//
// ⚠ ROUND 15 MOVED WHERE THIS CAN HONESTLY BE ASSERTED, AND THAT IS THE POINT OF THE CHANGE. Each
// irreversible external call now writes its record as it is ISSUED, so a successful deletion does
// call RecordDeletionResidue — and then DeleteUserCascade deletes those rows inside the same
// transaction that deletes the user, so nothing is left behind. A mock repository cannot show you a
// row that was written and then removed, so the "leaves no record" half now lives where it can
// actually be observed: TestExecuteDeletion_Integration_OrdinarySuccess_LeavesNoResidueRow, on a
// real Postgres. What is still assertable here — and still worth pinning — is that a run which
// COMPLETES never raises the ..._cascade_failed kind, which is the one that says "and this is stuck".
func TestExecuteDeletion_OrdinarySuccess_RecordsNoResidue(t *testing.T) {
	ctx := context.Background()
	h := newRevocationHarness(t, ctx)

	require.NoError(t, h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindUserRequested))

	assert.Contains(t, *h.callOrder, "DeleteUserCascade", "this run really did complete")
	h.repo.AssertNotCalled(t, "RecordDeletionResidue", mock.Anything, mock.Anything,
		repository.DeletionResiduePaddleSubscriptionCancelledCascadeFailed)
}

// SECOND SIBLING: an abort BEFORE the subscription cancel lands must also record nothing. This is
// the assertion that makes the record mean "an external effect landed and the user kept their
// account", rather than the far weaker "this deletion aborted somewhere". Revocation during the
// Plaid leg is the case: Plaid runs first, so nothing at Paddle has happened yet.
func TestExecuteDeletion_RevokedBeforePaddleCancel_RecordsNoResidue(t *testing.T) {
	ctx := context.Background()
	h := newRevocationHarness(t, ctx)

	h.plaid.ExpectedCalls = nil
	h.plaid.On("RemoveAllUserItems", ctx, h.userID).Return(nil).
		Run(func(mock.Arguments) {
			*h.callOrder = append(*h.callOrder, "PlaidRemoveAll")
			*h.authorised = false
		})

	require.NoError(t, h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindUserRequested))

	assert.Equal(t, []string{"PlaidRemoveAll"}, *h.callOrder, "the cancel never happened")
	h.repo.AssertNotCalled(t, "RecordDeletionResidue", mock.Anything, mock.Anything, mock.Anything)
}

// The OTHER way a user survives a deletion whose Paddle cancel already landed: the cascade's own
// in-transaction re-verification (DeleteUserCascade's C1/C2 guard) refuses. The harm is identical —
// account kept, subscription gone — so the record must be identical too. Without this case the fix
// would cover one of the two paths to the same outcome and silently miss the other.
func TestExecuteDeletion_CascadeRefusesAfterPaddleCancel_RecordsTheResidue(t *testing.T) {
	ctx := context.Background()
	h := newRevocationHarness(t, ctx)

	h.repo.ExpectedCalls = nil
	h.repo.On("GetUserByID", ctx, h.userID).Return(h.user, nil)
	h.repo.On("ClaimForDeletion", ctx, h.userID).Return(true, nil)
	h.repo.On("RecordDeletionResidue", mock.Anything, h.userID, mock.Anything).Return(nil).Maybe()
	h.repo.On("DeleteUserCascade", ctx, h.userID, mock.Anything).Return(false, nil).
		Run(func(mock.Arguments) { *h.callOrder = append(*h.callOrder, "DeleteUserCascade") })

	require.NoError(t, h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindUserRequested),
		"a cascade that refuses is a normal, expected outcome")

	assert.Equal(t, []string{"PlaidRemoveAll", "PaddleCancel", "PaddleArchive", "DeleteUserCascade"},
		*h.callOrder)
	h.repo.AssertCalled(t, "RecordDeletionResidue", mock.Anything, h.userID,
		repository.DeletionResiduePaddleSubscriptionCancelled)
}

// A user with no active Paddle subscription has nothing cancelled, so a revocation at the archive
// gate leaves no residue to report. This pins that the record follows the EFFECT, not the abort.
func TestExecuteDeletion_NoSubscriptionCancelled_RecordsNoResidueOnRevocation(t *testing.T) {
	ctx := context.Background()
	h := newRevocationHarnessWithStatus(t, ctx, "cancelled")

	h.paddle.ExpectedCalls = nil
	h.paddle.On("ArchiveAndAnonymizeCustomer", ctx, h.custID, expectedAnonymizedEmail).Return(nil).Maybe().
		Run(func(mock.Arguments) { *h.callOrder = append(*h.callOrder, "PaddleArchive") })
	h.plaid.ExpectedCalls = nil
	h.plaid.On("RemoveAllUserItems", ctx, h.userID).Return(nil).
		Run(func(mock.Arguments) {
			*h.callOrder = append(*h.callOrder, "PlaidRemoveAll")
			*h.authorised = false
		})

	require.NoError(t, h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindUserRequested))

	h.paddle.AssertNotCalled(t, "CancelSubscriptionImmediately", mock.Anything, mock.Anything)
	h.repo.AssertNotCalled(t, "RecordDeletionResidue", mock.Anything, mock.Anything, mock.Anything)
}

// A failed residue write must not turn a legitimate outcome into a job failure — the deletion is
// already over and retrying it would not un-cancel anything — but it must not be silent either. The
// flow still returns nil; the failure is logged at Error (see the call site), which is the only
// escalation available once the record itself cannot be written.
func TestExecuteDeletion_ResidueWriteFails_StillReturnsTheLegitimateOutcome(t *testing.T) {
	ctx := context.Background()
	h := newRevocationHarness(t, ctx)

	h.repo.ExpectedCalls = nil
	h.repo.On("GetUserByID", ctx, h.userID).Return(h.user, nil)
	h.repo.On("ClaimForDeletion", ctx, h.userID).Return(true, nil)
	h.repo.On("RecordDeletionResidue", mock.Anything, h.userID, mock.Anything).
		Return(errors.New("audit insert failed"))

	h.paddle.ExpectedCalls = nil
	h.paddle.On("CancelSubscriptionImmediately", ctx, h.subID).Return(nil).
		Run(func(mock.Arguments) {
			*h.callOrder = append(*h.callOrder, "PaddleCancel")
			*h.authorised = false
		})

	err := h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindUserRequested)
	require.NoError(t, err,
		"the deletion outcome is unchanged by whether the visibility record could be written")
	h.repo.AssertCalled(t, "RecordDeletionResidue", mock.Anything, h.userID,
		repository.DeletionResiduePaddleSubscriptionCancelled)
}
