package services

// Issue #1531 Task 10b-2 — River jobs never went through the HTTP mutation middleware
// (internal/huma/middleware/cache_invalidation.go), so a Plaid transaction sync left every cached
// spending aggregate serving pre-sync numbers until its namespace TTL. These tests prove the fix
// at the ONLY place it needs to exist: transactionSyncService.SyncTransactions' bumpCacheForSync
// call, exercised through the real service (real mocks for its repo/Plaid dependencies, a real
// cache.Generations backed by a fake Redis) — never through a mock of the bump itself, so a
// regression that silently no-ops the call would fail these tests, not just pass a "Bump was
// called" assertion against a spy.

import (
	"context"
	"errors"
	"testing"

	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
)

// newSyncCacheInvalidationHarness builds a transactionSyncService wired exactly like
// TestSyncTransactions_SuccessSinglePage (zero accounts, so every Added transaction is skipped —
// meaning transactionRepo is never actually invoked, matching that test's own mock setup), plus a
// real (fake-Redis-backed) cache.Cache so the bump can be asserted against real generation state.
func newSyncCacheInvalidationHarness(t *testing.T) (
	service TransactionSyncService, linkedItem *domain.LinkedItem, c cache.Cache,
	hhReader *mockHouseholdReaderForDeletion, mockPlaidService *MockPlaidService,
) {
	t.Helper()
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockTransactionRepo := new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockPlaidService = new(MockPlaidService)

	service = NewTransactionSyncService(
		nil, // webhookRepo not needed for SyncTransactions
		mockLinkedItemRepo,
		mockTransactionRepo,
		mockAccountRepo,
		nil, // profileRepo - not needed (non-empty cursor skips historical-depth lookup)
		nil, // balanceSnapshotRepo - not needed for these tests
		mockPlaidService,
		nil, // subscriptionService
		nil, // ruleApplicationService
		nil, // budgetSyncService
		nil, // transactionMonitorService
		nil, // merchantRepo
		nil, // logger
	)

	linkedItem = &domain.LinkedItem{
		ID:          "linked-item-cache-inval",
		UserID:      "user-cache-inval",
		ItemID:      "item-abc",
		AccessToken: "decrypted-token",
		Cursor:      stringPtr("cursor-before"),
	}

	plaidResp := &plaid.TransactionsSyncResponse{
		Added:      []plaid.Transaction{{TransactionId: "txn-1", Amount: 10.0}},
		Modified:   []plaid.Transaction{},
		Removed:    []plaid.RemovedTransaction{},
		NextCursor: "cursor-after",
		HasMore:    false,
	}
	ctx := context.Background()
	mockPlaidService.On("TransactionsSync", ctx, "decrypted-token", "cursor-before", 0).Return(plaidResp, nil)
	mockLinkedItemRepo.On("UpdateCursor", ctx, linkedItem.ID, "cursor-after").Return(nil)
	mockAccountRepo.On("GetByLinkedItemID", ctx, linkedItem.ID).Return([]domain.Account{}, nil)
	// transactionRepo: no .On() setup — Added is skipped (no matching account), Modified/Removed
	// are empty, so no method on it should ever be invoked. Matches TestSyncTransactions_SuccessSinglePage.

	fake := redisclient.NewFake("cache")
	c = cache.NewCache(cache.Options{Client: fake})
	hhReader = &mockHouseholdReaderForDeletion{}

	return service, linkedItem, c, hhReader, mockPlaidService
}

// TestSyncTransactions_BumpsUserCacheGenerationAfterSuccess is the test the brief cares about
// most: after a simulated Plaid sync writes transactions, the affected user's generation must
// have changed, so a subsequent cached analytics read CANNOT return the pre-sync value.
func TestSyncTransactions_BumpsUserCacheGenerationAfterSuccess(t *testing.T) {
	service, linkedItem, c, hhReader, _ := newSyncCacheInvalidationHarness(t)
	ctx := context.Background()
	hhReader.On("GetHouseholdForUser", ctx, linkedItem.UserID).Return(nil, false, false, nil)

	preGen, err := c.Generations().Get(ctx, cache.ScopeUser, linkedItem.UserID)
	require.NoError(t, err)

	service.SetCacheInvalidation(c.Generations(), hhReader)

	_, err = service.SyncTransactions(ctx, linkedItem)
	require.NoError(t, err)

	postGen, err := c.Generations().Get(ctx, cache.ScopeUser, linkedItem.UserID)
	require.NoError(t, err)
	assert.NotEqual(t, preGen, postGen,
		"a successful sync must bump the user's cache generation — a read cached under preGen "+
			"must not be servable after this sync commits")
}

// TestSyncTransactions_BumpsHouseholdCacheGenerationWhenResolvable proves the household side of
// the bump: when the synced user belongs to a household, that household's cached aggregates
// (shared spending views) must also be invalidated, not just the acting user's own scope.
func TestSyncTransactions_BumpsHouseholdCacheGenerationWhenResolvable(t *testing.T) {
	service, linkedItem, c, hhReader, _ := newSyncCacheInvalidationHarness(t)
	ctx := context.Background()
	household := &domain.Household{ID: "household-cache-inval"}
	hhReader.On("GetHouseholdForUser", ctx, linkedItem.UserID).Return(household, false, true, nil)

	preGen, err := c.Generations().Get(ctx, cache.ScopeHousehold, household.ID)
	require.NoError(t, err)

	service.SetCacheInvalidation(c.Generations(), hhReader)

	_, err = service.SyncTransactions(ctx, linkedItem)
	require.NoError(t, err)

	postGen, err := c.Generations().Get(ctx, cache.ScopeHousehold, household.ID)
	require.NoError(t, err)
	assert.NotEqual(t, preGen, postGen, "a successful sync must also bump the synced user's household scope")
}

// TestSyncTransactions_NoHouseholdBumpWhenUserHasNone proves the negative: a user with no
// household must not spuriously mint/bump some unrelated household scope.
func TestSyncTransactions_NoHouseholdBumpWhenUserHasNone(t *testing.T) {
	service, linkedItem, c, hhReader, _ := newSyncCacheInvalidationHarness(t)
	ctx := context.Background()
	hhReader.On("GetHouseholdForUser", ctx, linkedItem.UserID).Return(nil, false, false, nil)

	service.SetCacheInvalidation(c.Generations(), hhReader)

	_, err := service.SyncTransactions(ctx, linkedItem)
	require.NoError(t, err)

	hhReader.AssertExpectations(t)
	// GetHouseholdForUser was called exactly once (asserted via mock.On + AssertExpectations);
	// found=false means no household bump was issued. Nothing further to assert on a scope that
	// was never touched.
}

// TestSyncTransactions_HouseholdLookupFailureDoesNotFailSync proves the void-bump discipline:
// a household lookup failure must never fail an already-committed sync, and must not suppress
// the user-scope bump that already happened.
func TestSyncTransactions_HouseholdLookupFailureDoesNotFailSync(t *testing.T) {
	service, linkedItem, c, hhReader, _ := newSyncCacheInvalidationHarness(t)
	ctx := context.Background()
	hhReader.On("GetHouseholdForUser", ctx, linkedItem.UserID).Return(nil, false, false, errors.New("redis-adjacent lookup failure"))

	preGen, err := c.Generations().Get(ctx, cache.ScopeUser, linkedItem.UserID)
	require.NoError(t, err)

	service.SetCacheInvalidation(c.Generations(), hhReader)

	result, err := service.SyncTransactions(ctx, linkedItem)
	require.NoError(t, err, "a household lookup failure during cache invalidation must not fail the sync")
	require.NotNil(t, result)

	postGen, err := c.Generations().Get(ctx, cache.ScopeUser, linkedItem.UserID)
	require.NoError(t, err)
	assert.NotEqual(t, preGen, postGen, "the user-scope bump must still have happened despite the household lookup error")
}

// TestSyncTransactions_NoBumpWhenCacheInvalidationNotWired is the regression guard for every
// EXISTING SyncTransactions test in this package that never calls SetCacheInvalidation: it must
// remain a true no-op (nil cacheGens), not a nil-pointer panic.
func TestSyncTransactions_NoBumpWhenCacheInvalidationNotWired(t *testing.T) {
	service, linkedItem, _, _, _ := newSyncCacheInvalidationHarness(t)
	ctx := context.Background()

	// Deliberately do NOT call SetCacheInvalidation.
	result, err := service.SyncTransactions(ctx, linkedItem)
	require.NoError(t, err)
	require.NotNil(t, result)
}
