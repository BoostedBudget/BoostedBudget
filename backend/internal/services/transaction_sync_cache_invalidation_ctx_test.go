package services

// Issue #1531 Blocker 2 — bumpCacheForSync (transaction_sync_service.go) is called from a defer at
// the very end of SyncTransactions, using the SAME ctx the whole sync ran under: a River job's
// context. A job timeout, or the worker pool shutting down, can cancel that ctx in the narrow
// window AFTER the sync's DB writes have already committed but BEFORE this deferred bump runs.
// Before the fix, gens.Bump(ctx, ...) inherited that cancellation silently (Bump swallows its own
// errors), leaving a committed sync with no cache invalidation — stale until the namespace TTL.
//
// This uses a ctx-respecting fake Redis client (ctxCheckingRedisClient below) rather than the
// bare redisclient.Fake used elsewhere in this package, because the bare Fake ignores its ctx
// argument entirely (verified by reading redisclient/fake.go) and so cannot distinguish "bumped
// with a live context" from "bumped with an already-cancelled one" — the exact distinction this
// regression test exists to make.

import (
	"context"
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

// ctxCheckingRedisClient wraps redisclient.Fake and actually returns ctx.Err() from Get/Set/SetNX
// when the passed context is already done — see the package doc comment above for why this,
// rather than the bare Fake, is required to make this test meaningful.
type ctxCheckingRedisClient struct {
	*redisclient.Fake
}

func (c *ctxCheckingRedisClient) Get(ctx context.Context, key string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return c.Fake.Get(ctx, key)
}

func (c *ctxCheckingRedisClient) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.Fake.Set(ctx, key, value, ttl)
}

func (c *ctxCheckingRedisClient) SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return c.Fake.SetNX(ctx, key, value, ttl)
}

// TestSyncTransactions_BumpsAfterSyncContextCancelled is the regression test for Blocker 2's fix
// to bumpCacheForSync. It cancels the SAME context object SyncTransactions is running under from
// inside one of the storage mocks (modelling a job timeout/shutdown firing after a write has
// already committed), then asserts the deferred cache bump still lands.
//
// ADVERSARIAL VALIDATION: reverted bumpCacheForSync's bumpCtx to plain ctx (the pre-fix shape) —
// this test FAILED (generation unchanged; Bump silently swallowed the "context canceled" error).
// Restored the fix — test PASSED.
func TestSyncTransactions_BumpsAfterSyncContextCancelled(t *testing.T) {
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

	linkedItem := &domain.LinkedItem{
		ID:          "linked-item-ctx-cancel",
		UserID:      "user-ctx-cancel",
		ItemID:      "item-ctx-cancel",
		AccessToken: "decrypted-token",
		Cursor:      stringPtr("cursor-before"),
	}

	plaidResp := &plaid.TransactionsSyncResponse{
		Added:      []plaid.Transaction{},
		Modified:   []plaid.Transaction{},
		Removed:    []plaid.RemovedTransaction{},
		NextCursor: "cursor-after",
		HasMore:    false,
	}

	ctx, cancel := context.WithCancel(context.Background())

	mockPlaidService.On("TransactionsSync", mock.Anything, "decrypted-token", "cursor-before", 0).Return(plaidResp, nil)
	// UpdateCursor is the sync's own committing write. Cancel the context right as it "commits" —
	// modelling a job timeout/shutdown landing right after, before the deferred bump below runs.
	mockLinkedItemRepo.On("UpdateCursor", mock.Anything, linkedItem.ID, "cursor-after").
		Run(func(mock.Arguments) { cancel() }).
		Return(nil)
	mockAccountRepo.On("GetByLinkedItemID", mock.Anything, linkedItem.ID).Return([]domain.Account{}, nil)

	client := &ctxCheckingRedisClient{Fake: redisclient.NewFake("cache")}
	gens := cache.NewGenerations(client)
	hhReader := &mockHouseholdReaderForDeletion{}
	hhReader.On("GetHouseholdForUser", mock.Anything, linkedItem.UserID).Return(nil, false, false, nil)
	service.SetCacheInvalidation(gens, hhReader)

	preGen, err := gens.Get(context.Background(), cache.ScopeUser, linkedItem.UserID)
	require.NoError(t, err)

	_, err = service.SyncTransactions(ctx, linkedItem)
	require.NoError(t, err)
	require.Error(t, ctx.Err(), "sanity: the sync's own context must actually be cancelled by now")

	postGen, err := gens.Get(context.Background(), cache.ScopeUser, linkedItem.UserID)
	require.NoError(t, err)
	assert.NotEqual(t, preGen, postGen,
		"the cache bump must survive the sync's own context being cancelled after the write committed")
}
