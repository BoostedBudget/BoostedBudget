package services

// Issue #1109: Chain-transparency test for buildAccountMaps.
// Verifies that after a re-link (old account replaced, new account active),
// the accountMap contains the new account keyed on its own Plaid account ID.
// These tests mock GetByLinkedItemIDIncludingRetained (Issue #1569 Task 1b) with
// only the current account in the returned set — the old/replaced/mid accounts
// in a chain typically carry DIFFERENT plaid_account_id values (a re-link mints a
// new one), so buildAccountMaps, which does not filter by status itself, would
// only ever map a same-keyed predecessor if one were included in the repo's
// result. This suite does not include one, so it is not itself proof that a
// retained account is excluded — see transaction_sync_retained_account_test.go for
// that (Task 1b's regression is the opposite: retained accounts MUST be mapped).

import (
	"context"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildAccountMaps_AfterRelink_OnlyContainsNewAccount verifies that when a
// re-link has occurred (old account replaced, new account active with the same
// linked_item_id), the accountMap built by buildAccountMaps contains the new
// active account, keyed by its Plaid account ID.
func TestBuildAccountMaps_AfterRelink_OnlyContainsNewAccount(t *testing.T) {
	mockAccountRepo := new(MockAccountRepository)

	service := NewTransactionSyncService(
		nil,
		nil,
		nil,
		mockAccountRepo,
		nil, // profileRepo
		nil, // balanceSnapshotRepo
		nil, // plaidService
		nil, // subscriptionService
		nil, // ruleApplicationService
		nil, // budgetSyncService
		nil, // transactionMonitorService
		nil, // merchantRepo
		nil, // logger
	).(*transactionSyncService)

	ctx := context.Background()
	linkedItemID := "linked-item-relink-test"
	stableUserAccountID := "user-acct-stable-uuid"

	// After re-link: the repo returns only the NEW active account for this test
	// fixture (the old account is a separate row this test does not include).
	newAccount := domain.Account{
		ID:             "new-account-uuid",
		PlaidAccountID: "plaid-acc-checking",
		Name:           "Checking",
		Status:         domain.AccountStatusActive,
		UserAccountID:  &stableUserAccountID,
	}
	mockAccountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItemID).
		Return([]domain.Account{newAccount}, nil)

	accountMap, userAccountMap, err := service.buildAccountMaps(ctx, linkedItemID)
	require.NoError(t, err)

	// accountMap should only contain the new account
	assert.Len(t, accountMap, 1, "accountMap should only contain the new active account")
	assert.Equal(t, "new-account-uuid", accountMap["plaid-acc-checking"])

	// userAccountMap should map to the stable user_account_id
	assert.Len(t, userAccountMap, 1)
	assert.Equal(t, stableUserAccountID, userAccountMap["plaid-acc-checking"])

	// The old replaced account must NOT be in the map
	_, oldPresent := accountMap["old-account-uuid"]
	assert.False(t, oldPresent, "replaced account must not be in accountMap")

	mockAccountRepo.AssertExpectations(t)
}

func TestBuildAccountMaps_Depth2Chain_OnlyContainsCurrentAccount(t *testing.T) {
	// Depth-2 chain: acct_old -> acct_mid -> acct_current
	// All three share the same user_account_id (stableUserAccountID).
	// This fixture's repo mock returns only the current account, so the account
	// map contains only its Plaid account ID.
	mockAccountRepo := new(MockAccountRepository)

	service := NewTransactionSyncService(
		nil, nil, nil,
		mockAccountRepo,
		nil, nil, nil, nil, nil, nil, nil, nil, nil,
	).(*transactionSyncService)

	ctx := context.Background()
	linkedItemID := "linked-item-depth2-test"
	stableUserAccountID := "user-acct-stable-depth2"
	plaidAccountID := "plaid-acc-shared" // same Plaid account ID across all re-links

	// This fixture's mock returns only the current account (older ones are
	// separate rows this test does not include).
	acctCurrent := domain.Account{
		ID:             "acct_current",
		PlaidAccountID: plaidAccountID,
		Name:           "Checking (current)",
		Status:         domain.AccountStatusActive,
		UserAccountID:  &stableUserAccountID,
	}
	mockAccountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItemID).
		Return([]domain.Account{acctCurrent}, nil)

	accountMap, userAccountMap, err := service.buildAccountMaps(ctx, linkedItemID)
	require.NoError(t, err)

	assert.Len(t, accountMap, 1, "accountMap should only contain the current active account")
	assert.Equal(t, "acct_current", accountMap[plaidAccountID])

	assert.Len(t, userAccountMap, 1)
	assert.Equal(t, stableUserAccountID, userAccountMap[plaidAccountID])

	// Ensure neither acct_old nor acct_mid are present
	for rawID, mappedID := range accountMap {
		assert.NotEqual(t, "acct_old", mappedID, "acct_old must not be in accountMap (key=%s)", rawID)
		assert.NotEqual(t, "acct_mid", mappedID, "acct_mid must not be in accountMap (key=%s)", rawID)
	}

	mockAccountRepo.AssertExpectations(t)
}
