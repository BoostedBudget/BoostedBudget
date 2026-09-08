package services

// Issue #1569 Task 1b — stop dropping new transactions for retained accounts.
//
// Before this task, buildAccountMaps loaded accounts via GetByLinkedItemID, which filters
// WHERE status = 'active'. A user disconnects one card but keeps the rest of the bank; the
// Plaid item keeps syncing (CountActiveByLinkedItemID stays nonzero while siblings remain
// active); every transaction Plaid delivers for the disconnected card misses accountMap and
// is Warn'd-and-skipped by processAddedTransactions, forever. Task 1a made the cursor advance
// only after storage — this task stops the accounts from being excluded from storage in the
// first place, by having buildAccountMaps load ALL statuses via
// GetByLinkedItemIDIncludingRetained.
//
// These tests exercise buildAccountMaps directly (not the full sync pipeline) because that is
// exactly the function this task changed, and mapPlaidTransaction/processAddedTransactions
// consult accountMap by simple lookup — proving an account is IN the map is equivalent to
// proving its transactions are no longer skipped.

import (
	"context"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newBuildAccountMapsService(mockAccountRepo *MockAccountRepository) *transactionSyncService {
	return NewTransactionSyncService(
		nil, nil, nil,
		mockAccountRepo,
		nil, nil, nil, nil, nil, nil, nil, nil, nil,
	).(*transactionSyncService)
}

// TestBuildAccountMaps_RemovedAndActiveAccounts_BothMapped is THE regression test, stated as
// the bug: one linked item with one 'removed' account (the user disconnected that card) and
// one 'active' account (a sibling the user kept). Plaid delivers rows for both, so both must
// land in accountMap or the removed account's transactions are silently dropped forever.
//
// Mutation-check: reverting buildAccountMaps to call GetByLinkedItemID (active-only) must turn
// this RED, since the mock below only ever stubs GetByLinkedItemIDIncludingRetained.
func TestBuildAccountMaps_RemovedAndActiveAccounts_BothMapped(t *testing.T) {
	mockAccountRepo := new(MockAccountRepository)
	service := newBuildAccountMapsService(mockAccountRepo)

	ctx := context.Background()
	linkedItemID := "linked-item-retained-test"
	activeUserAccountID := "user-acct-active"
	removedUserAccountID := "user-acct-removed"

	activeAccount := domain.Account{
		ID:             "account-active-uuid",
		PlaidAccountID: "plaid-acc-active-sibling",
		Name:           "Checking (kept)",
		Status:         domain.AccountStatusActive,
		UserAccountID:  &activeUserAccountID,
	}
	removedAccount := domain.Account{
		ID:             "account-removed-uuid",
		PlaidAccountID: "plaid-acc-disconnected-card",
		Name:           "Credit Card (disconnected)",
		Status:         domain.AccountStatusRemoved,
		UserAccountID:  &removedUserAccountID,
	}

	mockAccountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItemID).
		Return([]domain.Account{activeAccount, removedAccount}, nil)

	accountMap, userAccountMap, err := service.buildAccountMaps(ctx, linkedItemID, "")
	require.NoError(t, err)

	require.Len(t, accountMap, 2, "both the active sibling and the removed account must be mapped")
	assert.Equal(t, "account-active-uuid", accountMap["plaid-acc-active-sibling"])
	assert.Equal(t, "account-removed-uuid", accountMap["plaid-acc-disconnected-card"],
		"a removed account must still be mapped so its transactions are stored, not skipped")

	require.Len(t, userAccountMap, 2)
	assert.Equal(t, removedUserAccountID, userAccountMap["plaid-acc-disconnected-card"])

	mockAccountRepo.AssertExpectations(t)
}

// TestBuildAccountMaps_ExpiredAccount_Mapped covers the 90-day fingerprint-TTL path the user
// never asked for: ExpireAccountsByFingerprint writes 'expired', not 'removed', but the same
// bug applies — Plaid can still deliver transactions for it.
func TestBuildAccountMaps_ExpiredAccount_Mapped(t *testing.T) {
	mockAccountRepo := new(MockAccountRepository)
	service := newBuildAccountMapsService(mockAccountRepo)

	ctx := context.Background()
	linkedItemID := "linked-item-expired-test"
	userAccountID := "user-acct-expired"

	expiredAccount := domain.Account{
		ID:             "account-expired-uuid",
		PlaidAccountID: "plaid-acc-expired-hold",
		Name:           "Savings (fingerprint expired)",
		Status:         domain.AccountStatusExpired,
		UserAccountID:  &userAccountID,
	}

	mockAccountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItemID).
		Return([]domain.Account{expiredAccount}, nil)

	accountMap, _, err := service.buildAccountMaps(ctx, linkedItemID, "")
	require.NoError(t, err)

	assert.Equal(t, "account-expired-uuid", accountMap["plaid-acc-expired-hold"],
		"an expired account must still be mapped so its transactions are stored, not skipped")

	mockAccountRepo.AssertExpectations(t)
}

// TestBuildAccountMaps_ReplacedAccount_Mapped covers 'replaced' (superseded by a re-link).
// Task 1109's chain tests prove the CURRENT account in a chain gets mapped; this proves an
// OLDER, replaced account sharing this linked_item_id is not excluded either, if Plaid still
// names its plaid_account_id (e.g. a straggler webhook for the pre-relink account).
func TestBuildAccountMaps_ReplacedAccount_Mapped(t *testing.T) {
	mockAccountRepo := new(MockAccountRepository)
	service := newBuildAccountMapsService(mockAccountRepo)

	ctx := context.Background()
	linkedItemID := "linked-item-replaced-test"
	userAccountID := "user-acct-replaced-chain"

	replacedAccount := domain.Account{
		ID:             "account-replaced-uuid",
		PlaidAccountID: "plaid-acc-pre-relink",
		Name:           "Checking (pre-relink)",
		Status:         domain.AccountStatusReplaced,
		UserAccountID:  &userAccountID,
	}

	mockAccountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItemID).
		Return([]domain.Account{replacedAccount}, nil)

	accountMap, _, err := service.buildAccountMaps(ctx, linkedItemID, "")
	require.NoError(t, err)

	assert.Equal(t, "account-replaced-uuid", accountMap["plaid-acc-pre-relink"],
		"a replaced account must still be mapped so its transactions are stored, not skipped")

	mockAccountRepo.AssertExpectations(t)
}

// TestBuildAccountMaps_CrossTenantAccount_SkippedAndLogged is the #1615 regression test.
// GetByLinkedItemIDIncludingRetained is keyed on linked_item_id only, with no user_id
// predicate — see the guard's comment in buildAccountMaps. If accounts.user_id ever
// diverged from the linked item's owner (the ownerUserID parameter), a transaction would
// be written against another tenant's account. This proves the account is excluded from
// both maps rather than mapped.
//
// The investment side (investment_sync_service.go ~:282-298, ~:523) carries the identical
// guard but, as of this writing, no test of its own pins the skip behavior directly --
// verified: no test file under internal/services matches "does not belong to the linked
// item". This test is that coverage for the banking side only.
func TestBuildAccountMaps_CrossTenantAccount_SkippedAndLogged(t *testing.T) {
	mockAccountRepo := new(MockAccountRepository)
	service := newBuildAccountMapsService(mockAccountRepo)

	ctx := context.Background()
	linkedItemID := "linked-item-cross-tenant-test"
	ownerUserID := "user-owner"
	otherUserAccountID := "user-acct-other-tenant"

	ownedAccount := domain.Account{
		ID:             "account-owned-uuid",
		PlaidAccountID: "plaid-acc-owned",
		Name:           "Checking (owned)",
		Status:         domain.AccountStatusActive,
		UserID:         ownerUserID,
	}
	crossTenantAccount := domain.Account{
		ID:             "account-cross-tenant-uuid",
		PlaidAccountID: "plaid-acc-cross-tenant",
		Name:           "Checking (wrong tenant)",
		Status:         domain.AccountStatusActive,
		UserID:         "user-someone-else",
		UserAccountID:  &otherUserAccountID,
	}

	mockAccountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItemID).
		Return([]domain.Account{ownedAccount, crossTenantAccount}, nil)

	accountMap, userAccountMap, err := service.buildAccountMaps(ctx, linkedItemID, ownerUserID)
	require.NoError(t, err)

	require.Len(t, accountMap, 1, "only the account owned by the linked item's owner may be mapped")
	assert.Equal(t, "account-owned-uuid", accountMap["plaid-acc-owned"])

	_, crossTenantPresent := accountMap["plaid-acc-cross-tenant"]
	assert.False(t, crossTenantPresent, "a cross-tenant account must not be mapped")
	_, crossTenantUserAccountPresent := userAccountMap["plaid-acc-cross-tenant"]
	assert.False(t, crossTenantUserAccountPresent, "a cross-tenant account must not appear in userAccountMap either")

	mockAccountRepo.AssertExpectations(t)
}
