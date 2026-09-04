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

// ─────────────────────────────────────────────────────────────────────────────────────────────────
// ExecuteDeletion's IRREVERSIBLE EXTERNAL CALLS must each act on authorisation that still holds.
//
// The defect these tests pin: ExecuteDeletion read the user ONCE at the top, won the deletion claim,
// and then performed Paddle and Plaid cleanup from that stale snapshot. CancelDeletion can clear
// deletion_requested_at and deletion_scheduled_at during that window (profile_repository.go) without
// coordinating with the work claim, so a user who cancelled their deletion mid-flight still had their
// subscription cancelled, their Paddle customer archived and anonymised, and their Plaid items
// disconnected — and only THEN did DeleteUserCascade's in-transaction re-check correctly refuse to
// delete them. Net result: an account with no subscription and no bank connections, none of it
// reversible, for a user who explicitly asked to keep it.
//
// ⚠ THIS IS NOT THE SAME THING AS ErrSideEffectClaimBusy. That mechanism (#1413 H1) coordinates
// BETWEEN concurrent flows so two flows cannot both disconnect the same user. What was missing is
// different in kind: nothing re-checked that THIS flow's own authorisation still held.
// ─────────────────────────────────────────────────────────────────────────────────────────────────

// revocationHarness is the shared fixture for these tests: a user with a Paddle subscription, a
// Paddle customer and Plaid items, whose deletion authorisation the test can revoke at a chosen
// instant. callOrder records every external call so a test can assert both WHAT happened and in
// WHAT ORDER — ordering is load-bearing here (see the ordering test below).
type revocationHarness struct {
	svc        DeletionService
	repo       *MockProfileRepositoryForDeletion
	paddle     *MockPaddleDeletionCleaner
	plaid      *MockPlaidItemCleanupService
	clerk      *MockClerkService
	callOrder  *[]string
	subID      string
	custID     string
	userID     string
	user       *domain.User
	authorised *bool
}

func newRevocationHarness(t *testing.T, ctx context.Context) *revocationHarness {
	t.Helper()
	return newRevocationHarnessWithStatus(t, ctx, "active")
}

// newRevocationHarnessWithStatus builds the same fixture with a chosen RAW subscription status —
// the value cleanupPaddleResources' own PinnedRaw branch reads to decide whether there is a
// subscription to cancel at all. "active" (the default) reaches both Paddle calls; a status that is
// neither active nor past_due reaches only the customer archive, which is how a test can exercise an
// abort with NO subscription cancellation behind it.
func newRevocationHarnessWithStatus(t *testing.T, ctx context.Context, status string) *revocationHarness {
	t.Helper()

	repo := new(MockProfileRepositoryForDeletion)
	email := new(MockEmailService)
	clerk := new(MockClerkService)
	audit := new(MockAuditLogger)
	paddle := new(MockPaddleDeletionCleaner)
	plaid := new(MockPlaidItemCleanupService)

	svc := NewDeletionService(repo, email, clerk, audit, plaid, 30, "test-secret-key")
	svc.SetPaddleCleanupService(paddle)
	svc.SetWorkClaimsRepo(newAllowingClaimsRepo())

	user, subID, custID := buildUserWithPaddle(status)
	authorised := true
	callOrder := []string{}

	repo.AuthorisedFn = func(context.Context) (bool, error) { return authorised, nil }
	repo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	repo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	// Issue #1504: permissive by default so tests whose subject is NOT the residue record are not
	// forced to program it; the residue tests assert on it explicitly (deletion_service_residue_test.go).
	repo.On("RecordDeletionResidue", mock.Anything, user.ID, mock.Anything).Return(nil).Maybe()
	repo.On("DeleteUserCascade", ctx, user.ID, mock.Anything).Return(true, nil).Maybe().
		Run(func(mock.Arguments) { callOrder = append(callOrder, "DeleteUserCascade") })
	paddle.On("CancelSubscriptionImmediately", ctx, subID).Return(nil).Maybe().
		Run(func(mock.Arguments) { callOrder = append(callOrder, "PaddleCancel") })
	paddle.On("ArchiveAndAnonymizeCustomer", ctx, custID, expectedAnonymizedEmail).Return(nil).Maybe().
		Run(func(mock.Arguments) { callOrder = append(callOrder, "PaddleArchive") })
	plaid.On("RemoveAllUserItems", ctx, user.ID).Return(nil).Maybe().
		Run(func(mock.Arguments) { callOrder = append(callOrder, "PlaidRemoveAll") })
	clerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil).Maybe()
	audit.On("Log", ctx, mock.Anything, "user", user.ID).Return(nil).Maybe()

	return &revocationHarness{
		svc: svc, repo: repo, paddle: paddle, plaid: plaid, clerk: clerk,
		callOrder: &callOrder, subID: subID, custID: custID, userID: user.ID, user: user,
		authorised: &authorised,
	}
}

// The core case. Authorisation is revoked WHILE the Plaid cleanup is in flight — the widest, most
// realistic window, since that leg makes one network round-trip per linked item. Everything after the
// revocation must not happen.
//
// ⚠ The revocation lands BETWEEN two external calls, not before either. A test that revoked before
// the flow started would be caught by ClaimForDeletion and would prove nothing about the use-time
// rechecks this test exists to pin.
func TestExecuteDeletion_AuthorisationRevokedDuringPlaidCleanup_MakesNoPaddleCalls(t *testing.T) {
	ctx := context.Background()
	h := newRevocationHarness(t, ctx)

	// The user hits "cancel deletion" while we are disconnecting their banks.
	h.plaid.ExpectedCalls = nil
	h.plaid.On("RemoveAllUserItems", ctx, h.userID).Return(nil).
		Run(func(mock.Arguments) {
			*h.callOrder = append(*h.callOrder, "PlaidRemoveAll")
			*h.authorised = false
		})

	err := h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindUserRequested)
	require.NoError(t, err, "a cancelled deletion is a legitimate outcome, not a failure")

	assert.Equal(t, []string{"PlaidRemoveAll"}, *h.callOrder,
		"the subscription must NOT be cancelled, the Paddle customer must NOT be archived, and the "+
			"cascade must not be reached, once the user has cancelled their deletion")
	h.paddle.AssertNotCalled(t, "CancelSubscriptionImmediately", mock.Anything, mock.Anything)
	h.paddle.AssertNotCalled(t, "ArchiveAndAnonymizeCustomer", mock.Anything, mock.Anything, mock.Anything)
	h.repo.AssertNotCalled(t, "DeleteUserCascade", mock.Anything, mock.Anything, mock.Anything)
	h.clerk.AssertNotCalled(t, "DeleteUser", mock.Anything, mock.Anything)
}

// SIBLING ASSERTION for the test above, and mandatory: without it that test passes just as well if
// the Paddle and Plaid cleanup broke entirely. Authorisation holds throughout, so every irreversible
// call still fires.
//
// It also pins the ORDER, which the recheck work deliberately changed: Plaid FIRST, then Paddle. A
// Plaid disconnect is recoverable (the user relinks); ArchiveAndAnonymizeCustomer destroys PII at
// Paddle and is not. Putting the least-reversible call last means the widest exposure window carries
// the cheapest possible loss, and means an abort in the Plaid leg costs the user nothing permanent.
func TestExecuteDeletion_AuthorisationHolds_StillPerformsEveryCleanupLeastReversibleLast(t *testing.T) {
	ctx := context.Background()
	h := newRevocationHarness(t, ctx)

	err := h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindUserRequested)
	require.NoError(t, err)

	assert.Equal(t,
		[]string{"PlaidRemoveAll", "PaddleCancel", "PaddleArchive", "DeleteUserCascade"},
		*h.callOrder,
		"recoverable external work first, irreversible Paddle anonymisation last, cascade after both")
	h.clerk.AssertCalled(t, "DeleteUser", ctx, "user_123")
}

// The narrowest and most valuable window: revocation lands BETWEEN the two Paddle calls, i.e. during
// the subscription cancellation's own network round-trip. The customer must NOT then be archived and
// anonymised.
//
// ⚠ THIS IS THE ONE IRREVERSIBLE ACT WITH NO RECOVERY AT ALL. A cancelled subscription can be
// re-created; an anonymised Paddle customer cannot be restored by us or by Paddle. That is why
// cleanupPaddleResources re-asks before EACH of its two calls rather than once for the pair — a
// single check for the pair would satisfy every other test in this file and still lose the customer
// record here. Established by mutation: deleting the archive-side gate kills only this test.
//
// It also documents the residue this batch does NOT recover: the subscription IS already cancelled
// when we stop. See the report's "what remains unrecoverable" section.
func TestExecuteDeletion_AuthorisationRevokedBetweenPaddleCalls_DoesNotAnonymiseTheCustomer(t *testing.T) {
	ctx := context.Background()
	h := newRevocationHarness(t, ctx)

	h.paddle.ExpectedCalls = nil
	h.paddle.On("CancelSubscriptionImmediately", ctx, h.subID).Return(nil).
		Run(func(mock.Arguments) {
			*h.callOrder = append(*h.callOrder, "PaddleCancel")
			*h.authorised = false // the user cancels their deletion while Paddle is answering
		})
	h.paddle.On("ArchiveAndAnonymizeCustomer", ctx, h.custID, expectedAnonymizedEmail).Return(nil).Maybe().
		Run(func(mock.Arguments) { *h.callOrder = append(*h.callOrder, "PaddleArchive") })

	err := h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindUserRequested)
	require.NoError(t, err, "a cancelled deletion is a legitimate outcome, not a Paddle failure")

	assert.Equal(t, []string{"PlaidRemoveAll", "PaddleCancel"}, *h.callOrder,
		"the customer must NOT be anonymised, and the cascade must not be reached")
	h.paddle.AssertNotCalled(t, "ArchiveAndAnonymizeCustomer", mock.Anything, mock.Anything, mock.Anything)
	h.repo.AssertNotCalled(t, "DeleteUserCascade", mock.Anything, mock.Anything, mock.Anything)
}

// The ErrCleanupAborted disposition. When the per-item recheck stops the Plaid loop partway,
// cleanupItems returns ErrCleanupAborted — the SAME sentinel it returns when a new linked item
// appeared mid-pass, which is retryable. ExecuteDeletion must tell the two apart: a revocation is a
// quiet, final stop (nil, no Paddle work, no cascade), whereas the other cause must keep behaving like
// a failure so the next run retries.
func TestExecuteDeletion_PlaidCleanupAbortedByRevocation_StopsQuietlyWithoutTouchingPaddle(t *testing.T) {
	ctx := context.Background()
	h := newRevocationHarness(t, ctx)

	h.plaid.ExpectedCalls = nil
	h.plaid.On("RemoveAllUserItems", ctx, h.userID).Return(ErrCleanupAborted).
		Run(func(mock.Arguments) {
			*h.callOrder = append(*h.callOrder, "PlaidRemoveAll")
			*h.authorised = false
		})

	err := h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindUserRequested)
	require.NoError(t, err,
		"a revocation-triggered abort is not a Plaid failure and must not be reported as one")
	assert.Equal(t, []string{"PlaidRemoveAll"}, *h.callOrder)

	// SIBLING ASSERTION: the same sentinel from the OTHER cause (a new item appeared, authorisation
	// intact) must still abort loudly, or the branch above would swallow every retryable abort.
	h2 := newRevocationHarness(t, ctx)
	h2.plaid.ExpectedCalls = nil
	h2.plaid.On("RemoveAllUserItems", ctx, h2.userID).Return(ErrCleanupAborted).
		Run(func(mock.Arguments) { *h2.callOrder = append(*h2.callOrder, "PlaidRemoveAll") })

	err = h2.svc.ExecuteDeletion(ctx, h2.userID, repository.DeletionKindUserRequested)
	require.Error(t, err, "authorisation still holds, so this abort is the retryable kind")
	assert.ErrorIs(t, err, ErrCleanupAborted)
	assert.Equal(t, []string{"PlaidRemoveAll"}, *h2.callOrder, "and it still stops before Paddle")
}

// Revoked before ANY external call: nothing external happens at all. This is the cheapest abort and
// the one with no residue whatsoever.
func TestExecuteDeletion_AuthorisationRevokedBeforeAnyExternalCall_TouchesNothing(t *testing.T) {
	ctx := context.Background()
	h := newRevocationHarness(t, ctx)
	*h.authorised = false

	err := h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindUserRequested)
	require.NoError(t, err)

	assert.Empty(t, *h.callOrder, "no Plaid call, no Paddle call, no cascade")
}

// The per-item seam. RemoveAllUserItems used to pass nil as cleanupItems' eligibility callback
// (plaid_item_cleanup_service.go:467) even though the parameter exists for exactly this, so a
// revocation landing between item 1 and item 200 of a large account went unnoticed for the remaining
// 199 items. The callback ExecuteDeletion hands over must be non-nil AND must report live
// authorisation rather than a value captured when the flow started.
func TestExecuteDeletion_HandsPlaidCleanupALiveEligibilityRecheck(t *testing.T) {
	ctx := context.Background()
	h := newRevocationHarness(t, ctx)

	require.NoError(t, h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindUserRequested))

	require.False(t, h.plaid.RemoveAllRecheckWasNil,
		"a nil eligibility callback is the whole defect: the per-item recheck cannot fire without one")
	recheck := h.plaid.LastRemoveAllRecheck
	require.NotNil(t, recheck)

	ok, err := recheck(ctx)
	require.NoError(t, err)
	assert.True(t, ok, "it reports the authorisation that actually holds right now")

	*h.authorised = false
	ok, err = recheck(ctx)
	require.NoError(t, err)
	assert.False(t, ok,
		"and it must re-read: a callback closing over a value captured at flow start would still say true")
}

// Fail closed. Not being able to establish whether the deletion is still authorised is not permission
// to proceed to an irreversible external call.
func TestExecuteDeletion_AuthorisationRecheckFails_AbortsWithoutTouchingAnything(t *testing.T) {
	ctx := context.Background()
	h := newRevocationHarness(t, ctx)
	h.repo.AuthorisedFn = func(context.Context) (bool, error) { return false, errors.New("db down") }

	err := h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindUserRequested)
	require.Error(t, err, "an undetermined authorisation must surface, not be treated as revoked-and-fine")
	assert.Contains(t, err.Error(), "db down")
	assert.Empty(t, *h.callOrder)
}

// Fail closed AT THE PADDLE GATE specifically, not merely at the first check of the run.
//
// TestExecuteDeletion_AuthorisationRecheckFails above makes the very first recheck fail, which aborts
// before anything external happens — so it does NOT exercise cleanupPaddleResources' own gate error
// path at all. Mutation confirmed that: making the gate treat an error as "proceed" survived that
// test. Here the recheck succeeds for the pre-Plaid check and then starts erroring, so the failure
// lands on the gate itself, with the Plaid leg already done.
func TestExecuteDeletion_AuthorisationRecheckFailsAtThePaddleGate_AbortsBeforeCancelling(t *testing.T) {
	ctx := context.Background()
	h := newRevocationHarness(t, ctx)

	calls := 0
	h.repo.AuthorisedFn = func(context.Context) (bool, error) {
		calls++
		if calls == 1 {
			return true, nil // the pre-Plaid check passes, so the run gets as far as Paddle
		}
		return false, errors.New("db down")
	}

	err := h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindUserRequested)
	require.Error(t, err, "an undetermined authorisation must not be read as permission to cancel a subscription")
	assert.Contains(t, err.Error(), "db down")

	assert.Equal(t, []string{"PlaidRemoveAll"}, *h.callOrder,
		"the Plaid leg had already run; nothing at Paddle may be touched on an undetermined answer")
	h.paddle.AssertNotCalled(t, "CancelSubscriptionImmediately", mock.Anything, mock.Anything)
	h.repo.AssertNotCalled(t, "DeleteUserCascade", mock.Anything, mock.Anything, mock.Anything)
}

// The recheck is asked the question ExecuteDeletion was authorised under — the same kind, and the same
// deletion_requested_at occurrence the top-of-function read observed. A recheck that evaluated the
// union of both authorisation kinds would let the retention rule (which allows any uncomped user with
// no outstanding request) re-authorise a run whose user-requested authorisation has just been
// cancelled, which is exactly the substitution #1440 T0 closed at claim time.
func TestExecuteDeletion_RechecksTheAuthorisationItWasIssuedUnder(t *testing.T) {
	ctx := context.Background()
	h := newRevocationHarness(t, ctx)

	require.NoError(t, h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindRetention))

	require.NotEmpty(t, h.repo.AuthorisedKinds)
	for _, kind := range h.repo.AuthorisedKinds {
		assert.Equal(t, repository.DeletionKindRetention, kind,
			"the recheck must be asked about the kind this run was claimed under, not a union")
	}
	require.NotEmpty(t, h.repo.AuthorisedOccurrences)
	for _, occ := range h.repo.AuthorisedOccurrences {
		assert.Nil(t, occ, "buildUserWithPaddle has no deletion request, so the pinned occurrence is NULL")
	}
}
