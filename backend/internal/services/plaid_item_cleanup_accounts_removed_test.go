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

// Issue #1569 Task 20 (A2). The Plaid item cleanup flows removed the Item and left every account
// under it at status='active', forever.
//
// WHY THAT IS A LIVE-NUMBER DEFECT, not bookkeeping. Once plaid_removed_at is stamped the account
// stops syncing, so current_balance is frozen at whatever it last was. The frontend decides whether
// to caption a balance "as of <date>" purely from the account's status —
// apps/web/src/features/accounts/utils.ts getAccountClosedDate returns undefined for an
// isInactive()==false account — so a frozen figure was rendered as a current one, with no date and
// no qualification, on the accounts list and in every total built from it.
//
// It was reachable by a ROUTINE BACKGROUND JOB and needed no user action at all:
// repository.cleanupCandidateReasonPredicate's first leg selects any item that has been in
// 'error'/'login_required' for longer than errorDays, and three of its four legs say nothing
// whatsoever about the item's accounts (only the fourth requires zero active accounts). So a
// login_required item the user never repaired was swept, its billing and syncing stopped, and its
// accounts stayed "live".
//
// The residual was already known and written down at
// repository/linked_item_repository.go's PurgeLinkedItem guard: "accountRepository
// .MarkRemovedByLinkedItemID has exactly one production caller, accountService
// .DisconnectInstitution, and the sweep is not it".

// mockAccountRepoForCleanup records the MarkRemovedByLinkedItemID calls the service makes.
type mockAccountRepoForCleanup struct {
	mock.Mock
}

func (m *mockAccountRepoForCleanup) MarkRemovedByLinkedItemID(ctx context.Context, linkedItemID string, reason domain.RemovalReason) error {
	args := m.Called(ctx, linkedItemID, reason)
	return args.Error(0)
}

func newCleanupSvcWithAccounts(t *testing.T, repo linkedItemRepoForCleanup, plaidSvc plaidServiceForCleanup) (PlaidItemCleanupService, *mockAccountRepoForCleanup) {
	t.Helper()
	accts := new(mockAccountRepoForCleanup)
	svc := NewPlaidItemCleanupService(repo, plaidSvc)
	svc.SetWorkClaimsRepo(newAllowingPlaidCleanupClaimsRepo())
	svc.SetAccountRepo(accts)
	return svc, accts
}

// RemoveItem — the per-item worker path the cleanup sweep drives — must transition the item's
// accounts to 'removed'.
func TestPlaidCleanup_RemoveItem_MarksAccountsRemoved(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)
	svc, accts := newCleanupSvcWithAccounts(t, mockRepo, mockPlaid)

	item := &domain.LinkedItem{ID: "item-1", UserID: "user-1", ItemID: "plaid-item-1", AccessToken: "tok"}
	mockRepo.On("GetByID", ctx, "item-1").Return(item, nil)
	mockPlaid.On("RemoveItem", ctx, "tok").Return(nil)
	mockRepo.On("MarkItemsDisconnected", ctx, []string{"item-1"}).Return(nil)
	accts.On("MarkRemovedByLinkedItemID", ctx, "item-1", domain.RemovalReasonLinkRemoved).Return(nil)

	require.NoError(t, svc.RemoveItem(ctx, "item-1", alwaysEligible))

	accts.AssertCalled(t, "MarkRemovedByLinkedItemID", ctx, "item-1", domain.RemovalReasonLinkRemoved)
}

// The reason must be 'link_removed' specifically, and the two existing values are both WRONG here
// for reasons that are not cosmetic. This asserts on the recorded argument rather than merely that
// some call happened, so a fix that passed 'user_disconnect' would still fail.
func TestPlaidCleanup_RemoveItem_ReasonIsLinkRemovedNotUserDisconnectOrProviderAbsent(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)
	svc, accts := newCleanupSvcWithAccounts(t, mockRepo, mockPlaid)

	item := &domain.LinkedItem{ID: "item-2", UserID: "user-1", ItemID: "plaid-item-2", AccessToken: "tok"}
	mockRepo.On("GetByID", ctx, "item-2").Return(item, nil)
	mockPlaid.On("RemoveItem", ctx, "tok").Return(nil)
	mockRepo.On("MarkItemsDisconnected", ctx, []string{"item-2"}).Return(nil)

	var seen []domain.RemovalReason
	accts.On("MarkRemovedByLinkedItemID", ctx, "item-2", mock.Anything).
		Run(func(a mock.Arguments) { seen = append(seen, a.Get(2).(domain.RemovalReason)) }).
		Return(nil)

	require.NoError(t, svc.RemoveItem(ctx, "item-2", alwaysEligible))

	require.Len(t, seen, 1)
	assert.Equal(t, domain.RemovalReasonLinkRemoved, seen[0])
	// 'user_disconnect' would falsify the user's intent: nobody asked for this.
	assert.NotEqual(t, domain.RemovalReasonUserDisconnect, seen[0])
	// 'provider_absent' would be worse — it is the ONE value RestoreAccount acts on, so it would
	// make these accounts eligible for resurrection under a Plaid Item that no longer exists.
	assert.NotEqual(t, domain.RemovalReasonProviderAbsent, seen[0])
}

// A failure to mark the accounts must PROPAGATE, and must happen BEFORE the 'disconnected' status
// write. Both halves matter and they are the same argument the plaid_removed_at ordering rests on:
// once cleanupItems's skip branch can see 'disconnected', the item is never re-attempted, so any
// work not yet done at that point is never done.
func TestPlaidCleanup_RemoveItem_AccountMarkFailurePropagatesBeforeDisconnect(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)
	svc, accts := newCleanupSvcWithAccounts(t, mockRepo, mockPlaid)

	item := &domain.LinkedItem{ID: "item-3", UserID: "user-1", ItemID: "plaid-item-3", AccessToken: "tok"}
	mockRepo.On("GetByID", ctx, "item-3").Return(item, nil)
	mockPlaid.On("RemoveItem", ctx, "tok").Return(nil)
	boom := errors.New("accounts update failed")
	accts.On("MarkRemovedByLinkedItemID", ctx, "item-3", domain.RemovalReasonLinkRemoved).Return(boom)

	err := svc.RemoveItem(ctx, "item-3", alwaysEligible)

	require.Error(t, err)
	assert.ErrorIs(t, err, boom)
	// Not merely "not yet" — the item must stay live so the retry re-enters this method.
	mockRepo.AssertNotCalled(t, "MarkItemsDisconnected", mock.Anything, mock.Anything)
}

// Fail closed on an unconfigured account repository, exactly as the service already does for the
// work-claims repository. A silently-skipped account transition is the defect this test exists for;
// a service that quietly tolerates a nil repo would reintroduce it the first time a wiring change
// dropped the setter.
func TestPlaidCleanup_RemoveItem_NoAccountRepoConfigured_FailsLoudlyBeforePlaid(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)
	svc := NewPlaidItemCleanupService(mockRepo, mockPlaid)
	svc.SetWorkClaimsRepo(newAllowingPlaidCleanupClaimsRepo()) // SetAccountRepo deliberately NOT called

	err := svc.RemoveItem(ctx, "item-4", alwaysEligible)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "account repository not configured")
	mockPlaid.AssertNotCalled(t, "RemoveItem", mock.Anything, mock.Anything)
	mockRepo.AssertNotCalled(t, "GetByID", mock.Anything, mock.Anything)
}

// The batch path has the identical hole and the identical consequence. Fixing only RemoveItem would
// leave every subscription-cancellation disconnect (jobs.SubscriptionWebhookWorker ->
// DisconnectAllUserItemsIfEligible -> cleanupItems) showing frozen balances as live.
func TestPlaidCleanup_CleanupItems_MarksAccountsRemovedForEverySuccessfulItem(t *testing.T) {
	ctx := context.Background()
	userID := "user-batch"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)
	svc, accts := newCleanupSvcWithAccounts(t, mockRepo, mockPlaid)

	items := []domain.LinkedItem{
		{ID: "b-1", UserID: userID, AccessToken: "tok-1"},
		{ID: "b-2", UserID: userID, AccessToken: "tok-2"},
	}
	mockRepo.On("GetActiveItemsByUserID", ctx, userID).Return(items, nil)
	mockPlaid.On("RemoveItem", ctx, "tok-1").Return(nil)
	mockPlaid.On("RemoveItem", ctx, "tok-2").Return(nil)
	mockRepo.On("MarkItemsDisconnected", ctx, []string{"b-1", "b-2"}).Return(nil)
	accts.On("MarkRemovedByLinkedItemID", ctx, "b-1", domain.RemovalReasonLinkRemoved).Return(nil)
	accts.On("MarkRemovedByLinkedItemID", ctx, "b-2", domain.RemovalReasonLinkRemoved).Return(nil)

	require.NoError(t, svc.DisconnectAllUserItemsIfEligible(ctx, userID, alwaysEligible))

	accts.AssertCalled(t, "MarkRemovedByLinkedItemID", ctx, "b-1", domain.RemovalReasonLinkRemoved)
	accts.AssertCalled(t, "MarkRemovedByLinkedItemID", ctx, "b-2", domain.RemovalReasonLinkRemoved)
}

// An item that FAILED at Plaid must not have its accounts marked removed: the Item is still live,
// still billing, and still syncable once the failure clears. Without this the guard above could be
// satisfied by a blanket "mark everything" that discards the distinction.
func TestPlaidCleanup_CleanupItems_FailedItemAccountsAreNotMarkedRemoved(t *testing.T) {
	ctx := context.Background()
	userID := "user-partial"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)
	svc, accts := newCleanupSvcWithAccounts(t, mockRepo, mockPlaid)

	items := []domain.LinkedItem{
		{ID: "p-ok", UserID: userID, AccessToken: "tok-ok"},
		{ID: "p-bad", UserID: userID, AccessToken: "tok-bad"},
	}
	mockRepo.On("GetActiveItemsByUserID", ctx, userID).Return(items, nil)
	mockPlaid.On("RemoveItem", ctx, "tok-ok").Return(nil)
	mockPlaid.On("RemoveItem", ctx, "tok-bad").Return(errors.New("plaid is down"))
	mockRepo.On("MarkItemsDisconnected", ctx, []string{"p-ok"}).Return(nil)
	accts.On("MarkRemovedByLinkedItemID", ctx, "p-ok", domain.RemovalReasonLinkRemoved).Return(nil)

	err := svc.DisconnectAllUserItemsIfEligible(ctx, userID, alwaysEligible)
	require.Error(t, err, "the failed item must still be reported")

	accts.AssertCalled(t, "MarkRemovedByLinkedItemID", ctx, "p-ok", domain.RemovalReasonLinkRemoved)
	accts.AssertNotCalled(t, "MarkRemovedByLinkedItemID", ctx, "p-bad", domain.RemovalReasonLinkRemoved)
}
