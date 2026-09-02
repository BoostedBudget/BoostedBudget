package jobs

// Issue #1531 Task 10b-2 — end-to-end proof that PlaidSyncWorker.Work, running the REAL
// services.transactionSyncService (not a mock), actually bumps the synced user's cache
// generation. transaction_sync_cache_invalidation_test.go (internal/services) is the unit-level
// proof of the bump itself; this test proves the job layer does not swallow, short-circuit, or
// otherwise prevent that bump from firing when driven through PlaidSyncWorker.Work exactly as
// River would call it.
//
// The fakes below use the "embed the interface, override only what's exercised" technique: each
// fake embeds the real (possibly large) repository/service interface as a nil field and only
// overrides the handful of methods this single-page, zero-existing-accounts sync path actually
// calls — mirroring TestSyncTransactions_SuccessSinglePage's own mock setup (accountRepo returns
// zero accounts, so every Added transaction is skipped and transactionRepo is never invoked).
// Any accidental call to an unoverridden method panics on the nil embedded interface, which is
// the desired failure mode: it means this test's premise (which methods get called) is wrong.

import (
	"context"
	"testing"

	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// fakeZeroAccountRepo returns no accounts for any linked item, so every synced transaction is
// skipped for account-mapping purposes (never reaches transactionRepo).
type fakeZeroAccountRepo struct {
	repository.AccountRepository
}

func (fakeZeroAccountRepo) GetByLinkedItemID(context.Context, string) ([]domain.Account, error) {
	return nil, nil
}

// GetByLinkedItemIDIncludingRetained is what buildAccountMaps actually calls (Issue #1569
// Task 1b); it must also be overridden here or the embedded nil AccountRepository panics.
func (fakeZeroAccountRepo) GetByLinkedItemIDIncludingRetained(context.Context, string) ([]domain.Account, error) {
	return nil, nil
}

// fakeSinglePagePlaidService returns one page (has_more=false) of a single Added transaction.
type fakeSinglePagePlaidService struct {
	services.PlaidService
}

func (fakeSinglePagePlaidService) TransactionsSync(_ context.Context, _ string, _ string, _ int) (*plaid.TransactionsSyncResponse, error) {
	return &plaid.TransactionsSyncResponse{
		Added:      []plaid.Transaction{{TransactionId: "txn-1", Amount: 10.0}},
		Modified:   []plaid.Transaction{},
		Removed:    []plaid.RemovedTransaction{},
		NextCursor: "cursor-after",
		HasMore:    false,
	}, nil
}

// fakeCursorLinkedItemRepo satisfies both repository.LinkedItemRepository (used by
// transactionSyncService.UpdateCursor) and, via GetByIDAndUser, PlaidSyncWorker's own
// non-locking lookup path (w.db is nil in this test, so acquireLockAndGetItem falls back to
// linkedItemRepo.GetByIDAndUser).
type fakeCursorLinkedItemRepo struct {
	repository.LinkedItemRepository
	item *domain.LinkedItem
}

func (f *fakeCursorLinkedItemRepo) GetByIDAndUser(_ context.Context, _, _ string) (*domain.LinkedItem, error) {
	return f.item, nil
}

func (f *fakeCursorLinkedItemRepo) UpdateCursor(_ context.Context, _ string, cursor string) error {
	f.item.Cursor = &cursor
	return nil
}

// fakeHouseholdLookupNone reports every user as having no household — the simplest case for
// this end-to-end test; the household-bump branch itself is covered exhaustively by
// transaction_sync_cache_invalidation_test.go.
type fakeHouseholdLookupNone struct{}

func (fakeHouseholdLookupNone) GetHouseholdForUser(context.Context, string) (*domain.Household, bool, bool, error) {
	return nil, false, false, nil
}

// TestPlaidSyncWorker_Work_BumpsUserCacheGenerationAfterSuccessfulSync is the test the brief
// cares about most, run through the actual job entry point: after PlaidSyncWorker.Work
// completes successfully, the synced user's cache generation must have changed — a subsequent
// cached analytics read cannot return the pre-sync value.
func TestPlaidSyncWorker_Work_BumpsUserCacheGenerationAfterSuccessfulSync(t *testing.T) {
	const userID = "user-plaid-sync-e2e"
	linkedItem := &domain.LinkedItem{
		ID:          "linked-item-e2e",
		UserID:      userID,
		ItemID:      "item-abc",
		AccessToken: "decrypted-token",
		Cursor:      stringPtrForTest("cursor-before"),
	}

	realService := services.NewTransactionSyncService(
		nil, // webhookRepo — not used by SyncTransactions directly
		&fakeCursorLinkedItemRepo{item: linkedItem},
		nil, // transactionRepo — never invoked: every Added transaction is skipped (no accounts)
		fakeZeroAccountRepo{},
		nil, // profileRepo — non-empty cursor skips the historical-depth lookup
		nil, // balanceSnapshotRepo — nil short-circuits createBalanceSnapshots
		fakeSinglePagePlaidService{},
		nil, // subscriptionService
		nil, // ruleApplicationService
		nil, // budgetSyncService
		nil, // transactionMonitorService
		nil, // merchantRepo
		nil, // logger
	)

	fakeRedis := redisclient.NewFake("cache")
	c := cache.NewCache(cache.Options{Client: fakeRedis})
	realService.SetCacheInvalidation(c.Generations(), fakeHouseholdLookupNone{})

	worker := NewPlaidSyncWorker(PlaidSyncWorkerDeps{
		SyncService:    realService,
		LinkedItemRepo: &fakeCursorLinkedItemRepo{item: linkedItem},
		JobQueue:       nil,
		DB:             nil, // non-locking path: uses LinkedItemRepo.GetByIDAndUser
	})

	ctx := context.Background()
	preGen, err := c.Generations().Get(ctx, cache.ScopeUser, userID)
	require.NoError(t, err)

	job := newTestRiverJob(PlaidSyncArgs{UserID: userID, ItemID: linkedItem.ID})
	err = worker.Work(ctx, job)
	require.NoError(t, err)

	postGen, err := c.Generations().Get(ctx, cache.ScopeUser, userID)
	require.NoError(t, err)
	assert.NotEqual(t, preGen, postGen,
		"PlaidSyncWorker.Work must leave the synced user's cache generation bumped — this is "+
			"Issue #1531 Task 10b-2's headline fix: a bank sync run as a River job must not leave "+
			"cached analytics stale until TTL")
}

func stringPtrForTest(s string) *string { return &s }
