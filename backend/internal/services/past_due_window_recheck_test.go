package services

import (
	"context"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// ─── Issue #1500: the LAST un-rechecked caller inside PastDueService ────────────────────────────
//
// processGraceExpiry got its per-item recheck in round-15; processPlaidDisconnectWindow — the
// pre-emptive disconnect that runs a week EARLIER in the same dunning cycle — kept passing the
// nil-recheck entry point, with a comment saying so. Same shape, same consequence: the window path
// makes one Plaid /item/remove call per linked item after a single reReadStillPastDue, so a user who
// paid their invoice after the first item was removed still lost every remaining bank connection.
//
// The predicate here is reReadStillPastDue — the SAME one this flow already uses for its two
// pre-loop verifications, deliberately reused rather than re-invented, so none of this flow's
// re-verifications can disagree about what "still past due" means. (The subscription-cancellation
// webhook's sibling fix uses a DIFFERENT predicate — comp/entitlement, not past-due — because that
// is what IT already evaluates before its own loop. See subscription_webhook_cancel_recheck_test.go.)

// recoverPastDueAfter returns an onRemoved hook that mutates the user row the mocked repository
// hands back — flipping it out of past_due — the moment `afterItemID` has been removed. Mutating the
// pointed-to struct (rather than programming a read count) is what keeps these tests robust: they
// assert on WHEN the world changed relative to the loop, not on how many times the flow happens to
// re-read.
func recoverPastDueAfter(row *domain.User, afterItemID string) func(string) {
	return func(itemID string) {
		if itemID == afterItemID {
			row.SubscriptionStatus = string(domain.SubscriptionStatusActive)
		}
	}
}

// TestPastDueService_PlaidDisconnectWindow_RecoveryMidLoop_StopsRemainingRemovals is the RED test
// for #1500's past-due call site. A past-due user in the Plaid-disconnect window has three linked
// items and pays their invoice after the FIRST is removed; the remaining two must be left connected,
// and the flow's own downstream effects (the "we disconnected your banks" notification) must not run
// for a user who is no longer past due.
//
// Items already removed stay removed — inherent to a loop of irreversible external calls, and why
// this is a recoverable Medium rather than a blocker: the user relinks those accounts.
func TestPastDueService_PlaidDisconnectWindow_RecoveryMidLoop_StopsRemainingRemovals(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &fakePerItemPlaidCleanup{itemIDs: []string{"item-1", "item-2", "item-3"}}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := NewPastDueService(repo, plaid, paddle, clerk, notif, testGraceDays, testPlaidDisconnectDays)
	svc.SetWorkClaimsRepo(newAllowingPastDueClaimsRepo())

	userID := "user-recovers-mid-window-loop"
	now := time.Now()
	// 15 days: inside the Plaid-disconnect window (14) and before grace expiry (21).
	pastDueStarted := now.Add(-15 * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted)

	row := userWithPastDue(userID, pastDueStarted)
	repo.On("GetUserByID", mock.Anything, userID).Return(&row, nil)
	plaid.onRemoved = recoverPastDueAfter(&row, "item-1")
	notif.On("Create", mock.Anything, mock.Anything).Return(nil).Maybe()

	err := svc.ProcessPastDueUser(context.Background(), user, now)

	require.NoError(t, err, "a mid-loop recovery is a legitimate partial completion, not a job failure")
	assert.Equal(t, []string{"item-1"}, plaid.removed,
		"only the item removed before the recovery may be gone; the remaining two must stay connected")
	assert.Equal(t, 1, plaid.ifEligibleCalls,
		"the Plaid-disconnect window must use the recheck-carrying entry point")
	notif.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
}

// TestPastDueService_PlaidDisconnectWindow_NoRecovery_RemovesEveryItem is the MANDATORY sibling
// assertion. Without it the test above passes just as well if the disconnect broke outright — a
// "does not act" assertion cannot tell a correct abort apart from a feature that no longer acts at
// all. This one pins that a user who stays past due still loses EVERY item and still gets the
// notification.
func TestPastDueService_PlaidDisconnectWindow_NoRecovery_RemovesEveryItem(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &fakePerItemPlaidCleanup{itemIDs: []string{"item-1", "item-2", "item-3"}}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := NewPastDueService(repo, plaid, paddle, clerk, notif, testGraceDays, testPlaidDisconnectDays)
	svc.SetWorkClaimsRepo(newAllowingPastDueClaimsRepo())

	userID := "user-stays-past-due-in-window"
	now := time.Now()
	pastDueStarted := now.Add(-15 * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted)

	row := userWithPastDue(userID, pastDueStarted)
	repo.On("GetUserByID", mock.Anything, userID).Return(&row, nil)
	notif.On("Create", mock.Anything, mock.Anything).Return(nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)

	require.NoError(t, err)
	assert.Equal(t, []string{"item-1", "item-2", "item-3"}, plaid.removed,
		"a user who never recovers must have every item removed — the recheck must not become a blanket refusal")
	assert.Equal(t, 1, plaid.ifEligibleCalls)
	notif.AssertCalled(t, "Create", mock.Anything, mock.Anything)
}
