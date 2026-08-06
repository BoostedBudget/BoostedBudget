package services

import (
	"context"
	"errors"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// ─── Issue #1486 round-15 (Codex pass 5, Medium 2): RemoveItem's use-time eligibility recheck ────
//
// ItemCleanupWorker.Work re-verifies eligibility with ClaimItemForCleanup, but that claim is a
// conditional UPDATE and grants NO exclusivity beyond its own statement (linked_item_repository.go
// says so itself). The worker then calls this separate service method, which performs its own read
// before presenting the token to Plaid — so a connection repair or an active-account write can
// commit in between, and an item that just became healthy is still disconnected.
//
// The fix is a recheck at the POINT OF USE, in the read RemoveItem already performs, not a lease:
// a lease adds persistent state that can be orphaned, which is the "state marked, work never
// happened, mark survives forever" failure this branch has already fixed five times. What remains
// after this fix is the same irreducible check-to-call window every other flow here accepts.

// declineAfter returns a recheck that reports eligible for the first `eligibleCalls` calls and
// ineligible afterwards, plus a pointer to its call count — so a test can pin BOTH the verdict the
// production path acted on and the number of times it actually asked.
func declineAfter(eligibleCalls int) (func(context.Context) (bool, error), *int) {
	calls := 0
	return func(context.Context) (bool, error) {
		calls++
		return calls <= eligibleCalls, nil
	}, &calls
}

// Test_RemoveItem_UseTimeRecheckDeclines_DoesNotCallPlaid is the RED test: an item that became
// ineligible between the worker's claim and this method's own read must not be sent to Plaid, and
// the caller must be able to tell that outcome apart from a failure.
func Test_RemoveItem_UseTimeRecheckDeclines_DoesNotCallPlaid(t *testing.T) {
	ctx := context.Background()
	itemID := "item-repaired-after-claim"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)
	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	mockRepo.On("GetByID", ctx, itemID).Return(&domain.LinkedItem{
		ID:          itemID,
		ItemID:      "plaid-item-1",
		AccessToken: "access-token-1",
		Status:      domain.LinkedItemStatusError,
	}, nil)

	// Permissive stubs, not expectations: the assertions below are AssertNotCalled, and an
	// unstubbed call would abort the whole test binary with a testify panic instead of failing on
	// the assertion that carries the meaning.
	mockPlaid.On("RemoveItem", mock.Anything, mock.Anything).Return(nil).Maybe()
	mockRepo.On("MarkItemsDisconnected", mock.Anything, mock.Anything).Return(nil).Maybe()

	recheck, calls := declineAfter(0)

	err := service.RemoveItem(ctx, itemID, recheck)

	require.Error(t, err, "a declined use-time recheck must be reported, not silently swallowed")
	assert.ErrorIs(t, err, ErrItemNoLongerEligible,
		"the caller distinguishes this from a Plaid failure by sentinel, and it must not be reported as one")
	assert.Equal(t, 1, *calls, "the recheck must be consulted exactly once, at the point of use")
	mockPlaid.AssertNotCalled(t, "RemoveItem", mock.Anything, mock.Anything)
	assert.Empty(t, mockRepo.PlaidRemovedIDs,
		"nothing may claim the Plaid side is done for an item that was never sent to Plaid")
	assert.Empty(t, mockRepo.CallLog, "no durable removal state may be written")
	mockRepo.AssertNotCalled(t, "MarkItemsDisconnected", mock.Anything, mock.Anything)
}

// Test_RemoveItem_StillEligible_RemovesAndRecords is the MANDATORY sibling assertion: an item that
// remains a legitimate cleanup target IS removed and IS recorded. Without it, the test above passes
// just as well if RemoveItem stopped removing anything at all.
func Test_RemoveItem_StillEligible_RemovesAndRecords(t *testing.T) {
	ctx := context.Background()
	itemID := "item-still-a-candidate"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)
	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	mockRepo.On("GetByID", ctx, itemID).Return(&domain.LinkedItem{
		ID:          itemID,
		ItemID:      "plaid-item-2",
		AccessToken: "access-token-2",
		Status:      domain.LinkedItemStatusError,
	}, nil)
	mockPlaid.On("RemoveItem", ctx, "access-token-2").Return(nil)
	mockRepo.On("MarkItemsDisconnected", ctx, []string{itemID}).Return(nil)

	recheck, calls := declineAfter(1)

	err := service.RemoveItem(ctx, itemID, recheck)

	require.NoError(t, err)
	assert.Equal(t, 1, *calls)
	mockPlaid.AssertCalled(t, "RemoveItem", ctx, "access-token-2")
	assert.Equal(t, []string{itemID}, mockRepo.PlaidRemovedIDs,
		"a genuine removal must still be recorded — the recheck must not become a blanket refusal")
	assert.Equal(t, []string{"MarkPlaidRemoved", "MarkItemsDisconnected"}, mockRepo.CallLog)
}

// A nil recheck is REFUSED rather than treated as "no recheck wanted", mirroring
// RemoveAllUserItems's own fail-closed guard: making the parameter required is worth nothing if a
// caller can pass nil and get the old behaviour back.
func Test_RemoveItem_NilRecheckIsRefusedBeforeTouchingPlaid(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)
	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	mockRepo.On("GetByID", mock.Anything, mock.Anything).Return(&domain.LinkedItem{
		ID: "item-nil-recheck", ItemID: "plaid-item-nil", AccessToken: "tok", Status: domain.LinkedItemStatusError,
	}, nil).Maybe()
	mockPlaid.On("RemoveItem", mock.Anything, mock.Anything).Return(nil).Maybe()
	mockRepo.On("MarkItemsDisconnected", mock.Anything, mock.Anything).Return(nil).Maybe()

	err := service.RemoveItem(ctx, "item-nil-recheck", nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "eligibility recheck is required")
	assert.NotErrorIs(t, err, ErrItemNoLongerEligible,
		"a missing recheck is a defect in the caller, not a legitimate decline")
	mockPlaid.AssertNotCalled(t, "RemoveItem", mock.Anything, mock.Anything)
	mockRepo.AssertNotCalled(t, "GetByID", mock.Anything, mock.Anything)
}

// A recheck that ERRORS is fail-closed: "we could not tell" is not "still eligible", and it must not
// be reported as the benign decline sentinel either — the caller has to retry.
func Test_RemoveItem_RecheckErrorFailsClosed(t *testing.T) {
	ctx := context.Background()
	itemID := "item-recheck-errors"
	recheckErr := errors.New("database unavailable")

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)
	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	mockRepo.On("GetByID", ctx, itemID).Return(&domain.LinkedItem{
		ID:          itemID,
		ItemID:      "plaid-item-3",
		AccessToken: "access-token-3",
		Status:      domain.LinkedItemStatusError,
	}, nil)

	mockPlaid.On("RemoveItem", mock.Anything, mock.Anything).Return(nil).Maybe()
	mockRepo.On("MarkItemsDisconnected", mock.Anything, mock.Anything).Return(nil).Maybe()

	err := service.RemoveItem(ctx, itemID, func(context.Context) (bool, error) {
		return false, recheckErr
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, recheckErr)
	assert.NotErrorIs(t, err, ErrItemNoLongerEligible,
		"an undetermined eligibility must retry, so it must not carry the benign-decline sentinel")
	mockPlaid.AssertNotCalled(t, "RemoveItem", mock.Anything, mock.Anything)
}
