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
	"time"

	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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
	return newSyncCacheInvalidationHarnessWithClient(t, redisclient.NewFake("cache"))
}

// flakySetClient wraps redisclient.Fake, failing the first failCount Set calls and succeeding on
// every subsequent one — used exclusively by the item 3 (#1569 round 11) tests below to model a
// transient Redis blip. setCalls records every SET's key, in order, so a test can assert on WHICH
// scope was written first (the reorder) as well as whether a retry recovered (the fail-open fix).
// Mirrors internal/huma/middleware/cache_invalidation_test.go's identical type, kept separate
// since Go test helpers are not exported across packages.
type flakySetClient struct {
	*redisclient.Fake
	failCount int
	setCalls  []string
}

func (c *flakySetClient) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	c.setCalls = append(c.setCalls, key)
	if c.failCount > 0 {
		c.failCount--
		return errors.New("redis: timeout")
	}
	return c.Fake.Set(ctx, key, value, ttl)
}

// newSyncCacheInvalidationHarnessWithClient is newSyncCacheInvalidationHarness with the
// underlying Redis client swappable — used by the #1569 round 11 item 3 tests below, which need a
// flakySetClient rather than the always-succeeding bare Fake.
func newSyncCacheInvalidationHarnessWithClient(t *testing.T, client redisclient.Client) (
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
	mockAccountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItem.ID).Return([]domain.Account{}, nil)
	// transactionRepo: no .On() setup — Added is skipped (no matching account), Modified/Removed
	// are empty, so no method on it should ever be invoked. Matches TestSyncTransactions_SuccessSinglePage.

	c = cache.NewCache(cache.Options{Client: client})
	hhReader = &mockHouseholdReaderForDeletion{}

	return service, linkedItem, c, hhReader, mockPlaidService
}

// TestSyncTransactions_BumpsUserCacheGenerationAfterSuccess is the test the brief cares about
// most: after a simulated Plaid sync writes transactions, the affected user's generation must
// have changed, so a subsequent cached analytics read CANNOT return the pre-sync value.
func TestSyncTransactions_BumpsUserCacheGenerationAfterSuccess(t *testing.T) {
	service, linkedItem, c, hhReader, _ := newSyncCacheInvalidationHarness(t)
	ctx := context.Background()
	hhReader.On("GetHouseholdForUser", mock.Anything, linkedItem.UserID).Return(nil, false, false, nil)

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
	hhReader.On("GetHouseholdForUser", mock.Anything, linkedItem.UserID).Return(household, false, true, nil)

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
	hhReader.On("GetHouseholdForUser", mock.Anything, linkedItem.UserID).Return(nil, false, false, nil)

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
	hhReader.On("GetHouseholdForUser", mock.Anything, linkedItem.UserID).Return(nil, false, false, errors.New("redis-adjacent lookup failure"))

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

// TestSyncTransactions_BumpsOnPartialCommitEvenWhenSyncLaterFails is the carried-forward fix
// from 2a-i's review: SyncTransactions used to bump ONLY on its final success path, so a sync
// that stored Added transactions and THEN failed while processing Modified transactions left
// the user's cache generation untouched — a permanently stale cached view (until the namespace
// TTL) despite genuinely new data having committed. This drives the real storage path (Added
// succeeds via CreateBatch, Modified fails via Update) and asserts the generation still changed.
func TestSyncTransactions_BumpsOnPartialCommitEvenWhenSyncLaterFails(t *testing.T) {
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockTransactionRepo := new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockPlaidService := new(MockPlaidService)

	service := NewTransactionSyncService(
		nil, // webhookRepo
		mockLinkedItemRepo,
		mockTransactionRepo,
		mockAccountRepo,
		nil, // profileRepo
		nil, // balanceSnapshotRepo
		mockPlaidService,
		nil, // subscriptionService
		nil, // ruleApplicationService
		nil, // budgetSyncService
		nil, // transactionMonitorService
		nil, // merchantRepo
		nil, // logger
	)

	ctx := context.Background()
	linkedItem := &domain.LinkedItem{
		ID:          "linked-item-partial",
		UserID:      "user-partial-commit",
		ItemID:      "item-partial",
		AccessToken: "decrypted-token",
		Cursor:      stringPtr("cursor-before"),
	}

	accounts := []domain.Account{
		{ID: "account-uuid-1", PlaidAccountID: "plaid-acc-1", UserID: linkedItem.UserID},
	}
	mockAccountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItem.ID).Return(accounts, nil)

	plaidResp := &plaid.TransactionsSyncResponse{
		Added: []plaid.Transaction{
			{TransactionId: "txn-added", AccountId: "plaid-acc-1", Amount: 10.0, Date: "2024-01-15", Name: "Added"},
		},
		Modified: []plaid.Transaction{
			{TransactionId: "txn-modified", AccountId: "plaid-acc-1", Amount: 20.0, Date: "2024-01-16", Name: "Modified"},
		},
		Removed:    []plaid.RemovedTransaction{},
		NextCursor: "cursor-after",
		HasMore:    false,
	}
	mockPlaidService.On("TransactionsSync", ctx, "decrypted-token", "cursor-before", 0).Return(plaidResp, nil)
	mockLinkedItemRepo.On("UpdateCursor", ctx, linkedItem.ID, "cursor-after").Return(nil)

	// Added transaction: storage SUCCEEDS — this is the commit that must not be lost.
	mockTransactionRepo.On("CreateBatch", ctx, mock.Anything).Return(map[string]bool{}, nil)

	// Modified transaction: storage FAILS after the Added commit above.
	existingTxn := &domain.Transaction{ID: "existing-uuid", ExternalTransactionID: stringPtr("txn-modified")}
	mockTransactionRepo.On("GetByExternalID", ctx, domain.SourcePlaid, "txn-modified", linkedItem.UserID).Return(existingTxn, nil)
	mockTransactionRepo.On("UpdateFromSync", ctx, mock.Anything).Return(errors.New("update failed"))

	fake := redisclient.NewFake("cache")
	c := cache.NewCache(cache.Options{Client: fake})
	hhReader := &mockHouseholdReaderForDeletion{}
	hhReader.On("GetHouseholdForUser", mock.Anything, linkedItem.UserID).Return(nil, false, false, nil)
	service.SetCacheInvalidation(c.Generations(), hhReader)

	preGen, err := c.Generations().Get(ctx, cache.ScopeUser, linkedItem.UserID)
	require.NoError(t, err)

	_, syncErr := service.SyncTransactions(ctx, linkedItem)
	require.Error(t, syncErr, "the sync as a whole must still surface the Modified-processing failure")

	postGen, err := c.Generations().Get(ctx, cache.ScopeUser, linkedItem.UserID)
	require.NoError(t, err)
	assert.NotEqual(t, preGen, postGen,
		"Added transactions committed before the Modified failure — the cache MUST be bumped "+
			"even though SyncTransactions ultimately returned an error")
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

// ============================================================================
// Issue #1537 T92: cacheBumpNeeded was armed AFTER each storage step SUCCEEDED,
// so a step that committed some rows and then failed returned before arming it.
// Every pre-existing test in this file has a PRIOR successful step (Added), which
// already armed the flag — so none of them can see this. The tests below keep
// result.Added EMPTY on purpose.
// ============================================================================

// newSingleStepSyncHarness wires a service with ONE real account and a Plaid response the
// caller supplies, so a single storage step (Added, Modified, or Removed) can be exercised in
// isolation with result.Added deliberately empty where required.
func newSingleStepSyncHarness(t *testing.T, itemSuffix string, resp *plaid.TransactionsSyncResponse) (
	service TransactionSyncService, txnRepo *MockTransactionRepository,
	linkedItem *domain.LinkedItem, c cache.Cache,
) {
	t.Helper()
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	txnRepo = new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockPlaidService := new(MockPlaidService)

	service = NewTransactionSyncService(
		nil, mockLinkedItemRepo, txnRepo, mockAccountRepo,
		nil, nil, mockPlaidService, nil, nil, nil, nil, nil, nil,
	)

	linkedItem = &domain.LinkedItem{
		ID:          "linked-item-" + itemSuffix,
		UserID:      "user-" + itemSuffix,
		ItemID:      "item-" + itemSuffix,
		AccessToken: "decrypted-token",
		Cursor:      stringPtr("cursor-before"),
	}

	ctx := context.Background()
	mockAccountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItem.ID).
		Return([]domain.Account{{ID: "account-uuid-1", PlaidAccountID: "plaid-acc-1", UserID: linkedItem.UserID}}, nil)
	mockPlaidService.On("TransactionsSync", ctx, "decrypted-token", "cursor-before", 0).Return(resp, nil)
	mockLinkedItemRepo.On("UpdateCursor", ctx, linkedItem.ID, "cursor-after").Return(nil)

	fake := redisclient.NewFake("cache")
	c = cache.NewCache(cache.Options{Client: fake})
	hhReader := &mockHouseholdReaderForDeletion{}
	hhReader.On("GetHouseholdForUser", mock.Anything, linkedItem.UserID).Return(nil, false, false, nil).Maybe()
	service.SetCacheInvalidation(c.Generations(), hhReader)

	return service, txnRepo, linkedItem, c
}

// TestSyncTransactions_ModifiedOnlyPartialCommitStillBumpsCache is the T92 bug proper.
// processModifiedTransactions commits ONE transactionRepo.UpdateFromSync per row and returns on the
// FIRST failure. On a MODIFIED-ONLY sync (Plaid DEFAULT_UPDATE's common shape) the rows updated
// before that failure are durable, but SyncTransactions returned before arming cacheBumpNeeded,
// so cached analytics kept serving pre-sync numbers for the whole namespace TTL.
func TestSyncTransactions_ModifiedOnlyPartialCommitStillBumpsCache(t *testing.T) {
	resp := &plaid.TransactionsSyncResponse{
		Added: []plaid.Transaction{}, // CRITICAL: no prior successful step to arm the flag
		Modified: []plaid.Transaction{
			{TransactionId: "txn-mod-1", AccountId: "plaid-acc-1", Amount: 10.0, Date: "2024-01-15", Name: "Mod one"},
			{TransactionId: "txn-mod-2", AccountId: "plaid-acc-1", Amount: 20.0, Date: "2024-01-16", Name: "Mod two"},
		},
		Removed:    []plaid.RemovedTransaction{},
		NextCursor: "cursor-after",
		HasMore:    false,
	}
	service, txnRepo, linkedItem, c := newSingleStepSyncHarness(t, "modified-only", resp)
	ctx := context.Background()

	txnRepo.On("GetByExternalID", ctx, domain.SourcePlaid, "txn-mod-1", linkedItem.UserID).
		Return(&domain.Transaction{ID: "existing-1", ExternalTransactionID: stringPtr("txn-mod-1")}, nil)
	txnRepo.On("GetByExternalID", ctx, domain.SourcePlaid, "txn-mod-2", linkedItem.UserID).
		Return(&domain.Transaction{ID: "existing-2", ExternalTransactionID: stringPtr("txn-mod-2")}, nil)
	// Row 1 COMMITS, row 2 fails: the partial-commit shape.
	txnRepo.On("UpdateFromSync", ctx, mock.Anything).Return(nil).Once()
	txnRepo.On("UpdateFromSync", ctx, mock.Anything).Return(errors.New("update failed on row 2")).Once()

	preGen, err := c.Generations().Get(ctx, cache.ScopeUser, linkedItem.UserID)
	require.NoError(t, err)

	_, syncErr := service.SyncTransactions(ctx, linkedItem)

	// Falsification guard for the ASSUMPTION the whole fix rests on: we must have left
	// SyncTransactions through the Modified error return, NOT the success path — otherwise a
	// passing generation assertion below would prove nothing about the deferred bump.
	require.Error(t, syncErr)
	require.Contains(t, syncErr.Error(), "failed to process modified transactions",
		"the test must exercise the Modified ERROR return path; if this changes, the deferred-bump "+
			"assertion below is no longer testing what it claims")

	postGen, err := c.Generations().Get(ctx, cache.ScopeUser, linkedItem.UserID)
	require.NoError(t, err)
	assert.NotEqual(t, preGen, postGen,
		"row 1's write COMMITTED before row 2 failed. transactions is a cached-read table, so "+
			"leaving the generation untouched serves stale financial data for the namespace TTL. "+
			"This also proves the deferred bump runs on an error return.")
}

// TestSyncTransactions_RemovedOnlyPartialCommitStillBumpsCache is the Removed-only twin.
//
// HISTORY, because the reason it passes has changed. It PASSED at HEAD before the arm-before-write
// fix, and the finding recorded here was that the T92 brief's claim — that the Removed arm can be
// skipped by a mid-step failure — was FALSE, because processRemovedTransactions swallowed every
// per-row error and returned nil unconditionally.
//
// #1569 task 7 made that swallow a real error return (residual path (4)), so the arm IS now
// reachable-past, and this test is no longer a characterization of an unreachable branch: it is a
// live guard. Row 1's tombstone commits, row 2 fails, SyncTransactions returns an error — and the
// generation must still move, because a committed write that no cache bump follows serves stale
// financial data for the namespace TTL.
func TestSyncTransactions_RemovedOnlyPartialCommitStillBumpsCache(t *testing.T) {
	resp := &plaid.TransactionsSyncResponse{
		Added:    []plaid.Transaction{}, // CRITICAL: no prior successful step
		Modified: []plaid.Transaction{},
		Removed: []plaid.RemovedTransaction{
			{TransactionId: stringPtr("txn-rem-1")},
			{TransactionId: stringPtr("txn-rem-2")},
		},
		NextCursor: "cursor-after",
		HasMore:    false,
	}
	service, txnRepo, linkedItem, c := newSingleStepSyncHarness(t, "removed-only", resp)
	ctx := context.Background()

	txnRepo.On("TombstoneByExternalID", ctx, domain.SourcePlaid, "txn-rem-1", linkedItem.UserID, domain.TxnDeletedByPlaidSync).Return(domain.TxnDeletedProviderRemovedPosted, nil).Once()
	txnRepo.On("TombstoneByExternalID", ctx, domain.SourcePlaid, "txn-rem-2", linkedItem.UserID, domain.TxnDeletedByPlaidSync).
		Return(domain.TransactionDeletedReason(""), errors.New("delete failed on row 2")).Once()

	preGen, err := c.Generations().Get(ctx, cache.ScopeUser, linkedItem.UserID)
	require.NoError(t, err)

	_, syncErr := service.SyncTransactions(ctx, linkedItem)
	require.Error(t, syncErr,
		"since #1569 a genuine per-row tombstone failure propagates; if this stops being an error "+
			"the residual-(4) fix has been undone")

	postGen, err := c.Generations().Get(ctx, cache.ScopeUser, linkedItem.UserID)
	require.NoError(t, err)
	assert.NotEqual(t, preGen, postGen,
		"row 1's tombstone COMMITTED; the generation must move regardless of row 2's outcome")
}

// TestSyncTransactions_AddedOnlyFailureStillBumpsCache is the Added-only twin. CreateBatch is a
// single multi-row INSERT, so it cannot half-commit *server-side* — but a client-visible error
// (connection reset, context deadline) after the server committed is indistinguishable from a
// real rollback, so the error alone is not evidence that nothing landed. Fail safe: bump.
func TestSyncTransactions_AddedOnlyFailureStillBumpsCache(t *testing.T) {
	resp := &plaid.TransactionsSyncResponse{
		Added: []plaid.Transaction{
			{TransactionId: "txn-add-1", AccountId: "plaid-acc-1", Amount: 10.0, Date: "2024-01-15", Name: "Add one"},
		},
		Modified:   []plaid.Transaction{},
		Removed:    []plaid.RemovedTransaction{},
		NextCursor: "cursor-after",
		HasMore:    false,
	}
	service, txnRepo, linkedItem, c := newSingleStepSyncHarness(t, "added-only", resp)
	ctx := context.Background()

	txnRepo.On("CreateBatch", ctx, mock.Anything).
		Return(map[string]bool(nil), errors.New("connection reset after commit"))

	preGen, err := c.Generations().Get(ctx, cache.ScopeUser, linkedItem.UserID)
	require.NoError(t, err)

	_, syncErr := service.SyncTransactions(ctx, linkedItem)
	require.Error(t, syncErr)
	require.Contains(t, syncErr.Error(), "failed to process added transactions",
		"must exercise the Added ERROR return path")

	postGen, err := c.Generations().Get(ctx, cache.ScopeUser, linkedItem.UserID)
	require.NoError(t, err)
	assert.NotEqual(t, preGen, postGen,
		"a CreateBatch error cannot prove the rows did not land; the fail-safe is to bump")
}

// TestSyncTransactions_HouseholdBumpedBeforeUser (#1569 round 11 item 3) is the sync-worker twin
// of the HTTP middleware's identical reorder test: bumpCacheForSync must write the HOUSEHOLD
// generation key before the user's, so a shared-budget or lookup failure lands on the lower
// blast-radius (self-scoped) bump rather than the one visible to a different household member.
func TestSyncTransactions_HouseholdBumpedBeforeUser(t *testing.T) {
	client := &flakySetClient{Fake: redisclient.NewFake("cache")}
	service, linkedItem, c, hhReader, _ := newSyncCacheInvalidationHarnessWithClient(t, client)
	ctx := context.Background()
	household := &domain.Household{ID: "household-order"}
	hhReader.On("GetHouseholdForUser", mock.Anything, linkedItem.UserID).Return(household, false, true, nil)

	service.SetCacheInvalidation(c.Generations(), hhReader)

	_, err := service.SyncTransactions(ctx, linkedItem)
	require.NoError(t, err)

	userKey := cache.GenerationKey(cache.ScopeTag(cache.ScopeUser, linkedItem.UserID))
	hhKey := cache.GenerationKey(cache.ScopeTag(cache.ScopeHousehold, household.ID))
	require.Len(t, client.setCalls, 2, "exactly one SET per scope")
	assert.Equal(t, hhKey, client.setCalls[0], "the household generation must be written FIRST")
	assert.Equal(t, userKey, client.setCalls[1], "the user generation must be written SECOND")
}

// TestSyncTransactions_HouseholdBumpRecoversFromTransientSetFailure (#1569 round 11 item 3) is
// the fail-open half: bumpCacheForSync's household bump must survive a single transient Redis SET
// failure via BumpWithRetry, where a bare Bump would have logged and silently given up.
func TestSyncTransactions_HouseholdBumpRecoversFromTransientSetFailure(t *testing.T) {
	client := &flakySetClient{Fake: redisclient.NewFake("cache"), failCount: 1}
	service, linkedItem, c, hhReader, _ := newSyncCacheInvalidationHarnessWithClient(t, client)
	ctx := context.Background()
	household := &domain.Household{ID: "household-retry"}
	hhReader.On("GetHouseholdForUser", mock.Anything, linkedItem.UserID).Return(household, false, true, nil)

	preGen, err := c.Generations().Get(ctx, cache.ScopeHousehold, household.ID)
	require.NoError(t, err)

	service.SetCacheInvalidation(c.Generations(), hhReader)

	_, err = service.SyncTransactions(ctx, linkedItem)
	require.NoError(t, err)

	postGen, err := c.Generations().Get(ctx, cache.ScopeHousehold, household.ID)
	require.NoError(t, err)
	assert.NotEqual(t, preGen, postGen,
		"the household bump must recover from a single transient Redis SET failure via retry")
}
