package jobs

// Issue #1531 Task 10b-2 — end-to-end proof that WebhookProcessingWorker.Work, routing a
// TRANSACTIONS webhook through the REAL services.transactionSyncService (not a mock), actually
// bumps the synced user's cache generation. WebhookProcessingWorker.processTransactionsWebhook
// calls transactionSyncSvc.ProcessWebhook, which internally calls SyncTransactions — the single
// call site where the bump lives (see transaction_sync_cache_invalidation_test.go, internal/
// services). This test proves the webhook-routing layer doesn't prevent that from firing.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// fakeNoOpWebhookRepo satisfies repository.WebhookRepository (the LARGER interface
// services.transactionSyncService depends on — distinct from this package's own narrower
// jobs.WebhookRepository) for ProcessWebhook's own bookkeeping Update calls (marking
// processing-started / processing-completed), which this test doesn't assert on.
type fakeNoOpWebhookRepo struct {
	repository.WebhookRepository
}

func (fakeNoOpWebhookRepo) Update(context.Context, *domain.WebhookEvent) error { return nil }

// fakeWebhookLinkedItemRepo satisfies repository.LinkedItemRepository for the one lookup
// ProcessWebhook needs (GetByItemID, to resolve the webhook's item_id to a UserID) plus the
// UpdateCursor call SyncTransactions makes once it has the linked item.
type fakeWebhookLinkedItemRepo struct {
	repository.LinkedItemRepository
	item *domain.LinkedItem
}

func (f *fakeWebhookLinkedItemRepo) GetByItemID(_ context.Context, _ string) (*domain.LinkedItem, error) {
	return f.item, nil
}

func (f *fakeWebhookLinkedItemRepo) UpdateCursor(_ context.Context, _ string, cursor string) error {
	f.item.Cursor = &cursor
	return nil
}

// TestWebhookProcessingWorker_Work_BumpsUserCacheGenerationForTransactionsWebhook proves the
// TRANSACTIONS webhook route — the path an inbound Plaid webhook actually takes — ends in the
// same cache bump a direct PlaidSyncWorker-driven sync does.
func TestWebhookProcessingWorker_Work_BumpsUserCacheGenerationForTransactionsWebhook(t *testing.T) {
	const userID = "user-webhook-e2e"
	itemID := "plaid-item-e2e"
	linkedItem := &domain.LinkedItem{
		ID:          "linked-item-webhook-e2e",
		UserID:      userID,
		ItemID:      itemID,
		AccessToken: "decrypted-token",
		Cursor:      stringPtrForTest("cursor-before"),
	}

	realService := services.NewTransactionSyncService(
		fakeNoOpWebhookRepo{}, // webhookRepo — ProcessWebhook's own bookkeeping Update calls
		&fakeWebhookLinkedItemRepo{item: linkedItem},
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

	webhookID := "webhook-e2e-1"
	webhook := &domain.WebhookEvent{
		ID:             webhookID,
		WebhookType:    "TRANSACTIONS",
		ItemID:         &itemID,
		SignatureValid: true,
		Processed:      false,
	}
	mockWebhookRepo := new(MockWebhookRepository)
	ctx := context.Background()
	mockWebhookRepo.On("GetByID", ctx, webhookID).Return(webhook, nil)

	worker := NewWebhookProcessingWorker(WebhookProcessingWorkerDeps{
		WebhookRepo:        mockWebhookRepo,
		TransactionSyncSvc: realService, // real service satisfies jobs.TransactionSyncService (ProcessWebhook)
	})

	preGen, err := c.Generations().Get(ctx, cache.ScopeUser, userID)
	require.NoError(t, err)

	job := newTestWebhookProcessingJob(WebhookProcessingArgs{
		WebhookID:   webhookID,
		WebhookType: "TRANSACTIONS",
		ItemID:      itemID,
	})
	err = worker.Work(ctx, job)
	require.NoError(t, err)

	postGen, err := c.Generations().Get(ctx, cache.ScopeUser, userID)
	require.NoError(t, err)
	assert.NotEqual(t, preGen, postGen,
		"a TRANSACTIONS webhook processed via WebhookProcessingWorker must leave the synced "+
			"user's cache generation bumped, exactly like a direct PlaidSyncWorker-driven sync")
}
