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
