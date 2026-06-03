package services

// Issue #1109: Chain-transparency test for buildAccountMaps.
// Verifies that after a re-link (old account replaced, new account active),
// the accountMap only contains the new account — GetByLinkedItemID already
// filters on status='active', so replaced accounts are excluded.

import (
	"context"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildAccountMaps_AfterRelink_OnlyContainsNewAccount verifies that when a
// re-link has occurred (old account replaced, new account active with the same
// linked_item_id), the accountMap built by buildAccountMaps contains ONLY the
// new active account. The replaced account must not appear because
// GetByLinkedItemID filters on status='active'.
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

	// After re-link: GetByLinkedItemID returns only the NEW active account.
	// The old account (status='replaced') is excluded by the SQL WHERE clause.
	newAccount := domain.Account{
		ID:             "new-account-uuid",
		PlaidAccountID: "plaid-acc-checking",
		Name:           "Checking",
		Status:         domain.AccountStatusActive,
		UserAccountID:  &stableUserAccountID,
	}
	mockAccountRepo.On("GetByLinkedItemID", ctx, linkedItemID).
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
	// GetByLinkedItemID returns only the current active account (the repo already
	// filters to active-only for sync purposes). The account map must only contain
	// the current active Plaid account ID.
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

	// Only the current active account is returned (older ones are replaced)
	acctCurrent := domain.Account{
		ID:             "acct_current",
		PlaidAccountID: plaidAccountID,
		Name:           "Checking (current)",
		Status:         domain.AccountStatusActive,
		UserAccountID:  &stableUserAccountID,
	}
	mockAccountRepo.On("GetByLinkedItemID", ctx, linkedItemID).
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
