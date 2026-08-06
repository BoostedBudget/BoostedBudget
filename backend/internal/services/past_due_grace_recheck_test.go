package services

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// ─── Issue #1413 round-15 (Codex pass 5, Medium 1): the grace-cancel flow's per-item recheck ────
//
// processGraceExpiry re-verifies reReadStillPastDue immediately before the Plaid disconnect and
// again immediately before the Paddle cancel — but the disconnect itself is not one call, it is ONE
// PLAID CALL PER LINKED ITEM. Passing DisconnectAllUserItems (whose recheck parameter is nil) meant
// a single authorisation read covered the whole loop, so a user who paid their invoice after the
// first item was removed still had every remaining bank connection removed.
//
// This is the matched sibling of the fix already made for RemoveAllUserItems (deletion flow) — same
// cleanupItems recheck parameter, one caller over. It reuses reReadStillPastDue rather than
// inventing a second notion of "is this user still past due", so all three re-verifications in this
// flow cannot disagree.

// fakePerItemPlaidCleanup simulates cleanupItems's per-item loop over a fixed item list, which is
// the only way a unit test can observe the difference the recheck parameter makes: the defect is
// invisible to a mock that treats "disconnect this user" as one atomic call.
//
// Its contract deliberately mirrors the real cleanupItems (plaid_item_cleanup_service.go): the
// recheck runs immediately before EVERY remaining item including the first, the first (false, nil)
// stops the loop without attempting any further item, and a loop that stopped early reports
// ErrCleanupAborted rather than nil.
type fakePerItemPlaidCleanup struct {
	itemIDs []string
	// removed records, in order, the items actually sent to Plaid /item/remove.
	removed []string
	// ifEligibleCalls / plainCalls record WHICH entry point production used, so a test can pin that
	// the recheck-carrying variant is the one being called at all.
	ifEligibleCalls int
	plainCalls      int
}

func (f *fakePerItemPlaidCleanup) DisconnectAllUserItems(ctx context.Context, userID string) error {
	f.plainCalls++
	return f.loop(ctx, nil)
}

func (f *fakePerItemPlaidCleanup) DisconnectAllUserItemsIfEligible(ctx context.Context, userID string, recheck func(ctx context.Context) (bool, error)) error {
	f.ifEligibleCalls++
	return f.loop(ctx, recheck)
}

func (f *fakePerItemPlaidCleanup) loop(ctx context.Context, recheck func(ctx context.Context) (bool, error)) error {
	attempted := 0
	for _, id := range f.itemIDs {
		if recheck != nil {
			eligible, err := recheck(ctx)
			if err != nil {
				return fmt.Errorf("eligibility recheck failed before item %s: %w", id, err)
			}
			if !eligible {
				break
			}
		}
		f.removed = append(f.removed, id)
		attempted++
	}
	if attempted < len(f.itemIDs) {
		return fmt.Errorf("%w for the fake's user (%d of %d item(s) not attempted)",
			ErrCleanupAborted, len(f.itemIDs)-attempted, len(f.itemIDs))
	}
	return nil
}

// programGraceExpiryTail allows every write processGraceExpiry performs AFTER the Plaid disconnect.
// Permissive (.Maybe()) on purpose: the abort test's whole point is that these must NOT run, and a
// missing expectation would surface as a testify panic instead of the assertion that actually
// carries the meaning.
func programGraceExpiryTail(repo *mockPastDueProfileRepo, paddle *mockPastDuePaddleClient, clerk *mockPastDueClerkClient, notif *mockPastDueNotificationRepo, user domain.User) {
	paddle.On("CancelSubscription", mock.Anything, *user.PaddleSubscriptionID).Return(nil).Maybe()
	repo.On("CancelIfPastDueOccurrenceWithRetention", mock.Anything, user.ID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), mock.AnythingOfType("int")).Return(true, nil).Maybe()
	repo.On("ClearPastDueStartedAtIfMatches", mock.Anything, user.ID, mock.AnythingOfType("time.Time")).Return(true, nil).Maybe()
	repo.On("ClearPlaidCleanupPendingIfMatches", mock.Anything, user.ID, mock.AnythingOfType("time.Time")).Return(true, nil).Maybe()
	clerk.On("UpdateUserMetadata", mock.Anything, user.ClerkUserID, mock.Anything).Return(nil).Maybe()
	notif.On("Create", mock.Anything, mock.Anything).Return(nil).Maybe()
}

// TestPastDueService_GraceExpiry_RecoveryMidLoop_StopsRemainingRemovals is the RED test for Codex
// pass 5's Medium 1. A past-due user with three linked items pays their invoice after the FIRST
// item has been removed; the remaining two must be left connected, and the Paddle cancellation must
// not happen at all.
//
// Items already removed stay removed — that is inherent to a loop of irreversible external calls,
// and it is why this finding is a recoverable Medium: the user relinks those accounts.
func TestPastDueService_GraceExpiry_RecoveryMidLoop_StopsRemainingRemovals(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &fakePerItemPlaidCleanup{itemIDs: []string{"item-1", "item-2", "item-3"}}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := NewPastDueService(repo, plaid, paddle, clerk, notif, testGraceDays, testPlaidDisconnectDays)
	svc.SetWorkClaimsRepo(newAllowingPastDueClaimsRepo())

	userID := "user-recovers-mid-loop"
	now := time.Now()
	pastDueStarted := now.Add(-time.Duration(testGraceDays+1) * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted)

	recovered := user
	recovered.SubscriptionStatus = string(domain.SubscriptionStatusActive)

	// Three past-due reads — the cheap early check, the post-claim re-verify, and the per-item
	// recheck before item-1 — then the invoice is paid, so every later read sees 'active'.
	repo.On("GetUserByID", mock.Anything, userID).Return(&user, nil).Times(3)
	repo.On("GetUserByID", mock.Anything, userID).Return(&recovered, nil)
	programGraceExpiryTail(repo, paddle, clerk, notif, user)

	err := svc.ProcessPastDueUser(context.Background(), user, now)

	require.NoError(t, err, "a mid-loop recovery is a legitimate partial completion, not a failure")
	assert.Equal(t, []string{"item-1"}, plaid.removed,
		"only the item removed before the recovery may be gone; the remaining two must stay connected")
	assert.Equal(t, 1, plaid.ifEligibleCalls,
		"the grace-cancel flow must use the recheck-carrying entry point")
	assert.Zero(t, plaid.plainCalls,
		"the nil-recheck entry point must not be used by a flow whose authorisation can be revoked mid-loop")
	paddle.AssertNotCalled(t, "CancelSubscription", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "CancelIfPastDueOccurrenceWithRetention",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestPastDueService_GraceExpiry_NoRecovery_RemovesEveryItem is the MANDATORY sibling assertion.
// Without it, the test above passes just as well if the disconnect broke entirely — a "does not
// act" assertion cannot tell a correct abort apart from a feature that no longer acts at all.
func TestPastDueService_GraceExpiry_NoRecovery_RemovesEveryItem(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &fakePerItemPlaidCleanup{itemIDs: []string{"item-1", "item-2", "item-3"}}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := NewPastDueService(repo, plaid, paddle, clerk, notif, testGraceDays, testPlaidDisconnectDays)
	svc.SetWorkClaimsRepo(newAllowingPastDueClaimsRepo())

	userID := "user-stays-past-due"
	now := time.Now()
	pastDueStarted := now.Add(-time.Duration(testGraceDays+1) * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted)

	repo.On("GetUserByID", mock.Anything, userID).Return(&user, nil)
	programGraceExpiryTail(repo, paddle, clerk, notif, user)

	err := svc.ProcessPastDueUser(context.Background(), user, now)

	require.NoError(t, err)
	assert.Equal(t, []string{"item-1", "item-2", "item-3"}, plaid.removed,
		"a user who never recovers must have every item removed — the recheck must not become a blanket refusal")
	paddle.AssertCalled(t, "CancelSubscription", mock.Anything, *user.PaddleSubscriptionID)
	repo.AssertCalled(t, "CancelIfPastDueOccurrenceWithRetention",
		mock.Anything, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), mock.AnythingOfType("int"))
}
