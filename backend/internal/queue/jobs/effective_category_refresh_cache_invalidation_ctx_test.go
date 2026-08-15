package jobs

// Issue #1531 Final fixes — regression test proving bumpCacheForUser (effective_category_refresh.go)
// survives its job context being cancelled AFTER the rename cascade has already committed. River
// cancels a job's context on shutdown, retry, or timeout; a cancellation landing in that narrow
// window must not take the post-commit bump down with it — this job rewrites
// transactions.effective_category_name, the very field spending-by-category/merchant and
// budget-vs-actual cached analytics GROUP BY.
//
// Uses a ctx-checking fake Redis client (ctxCheckingRedisClient below), NOT the bare
// redisclient.Fake used by the sibling *_cache_invalidation_test.go file: the bare Fake ignores
// its ctx argument entirely and so cannot distinguish "bumped with a live context" from "bumped
// with an already-cancelled one" — the exact distinction this test exists to make. Mirrors
// internal/services/transaction_sync_cache_invalidation_ctx_test.go's ctxCheckingRedisClient.
//
// ADVERSARIAL VALIDATION: reverted bumpCacheForUser's bumpCtx to plain ctx (the pre-fix shape) —
// this test FAILED (generation unchanged; Bump silently swallowed the "context canceled" error).
// Restored the fix — test PASSED.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
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

// TestEffectiveCategoryRefreshWorker_Work_BumpsAfterJobContextCancelled is the regression test for
// this fix. It cancels the SAME context object Work is running under from inside the repository
// mock (modelling River cancelling the job's context right as the rename cascade "commits"), then
// asserts the deferred cache bump still lands.
func TestEffectiveCategoryRefreshWorker_Work_BumpsAfterJobContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	userID := uuid.New().String()
	customCategoryID := uuid.New().String()

	mockRepo := new(MockTransactionRepositoryForCategoryRefresh)
	mockRepo.On("UpdateEffectiveCategoryNameByCustomCategoryID", ctx, userID, customCategoryID).
		Return(5, nil)

	worker := NewEffectiveCategoryRefreshWorker(EffectiveCategoryRefreshWorkerDeps{
		TransactionRepo: mockRepo,
	})

	client := &ctxCheckingRedisClient{Fake: redisclient.NewFake("cache")}
	c := cache.NewCache(cache.Options{Client: client})
	worker.SetCacheInvalidation(c.Generations(), nil)

	preGen, err := c.Generations().Get(context.Background(), cache.ScopeUser, userID)
	require.NoError(t, err)

	// Cancel the job's own context right after the repository call returns (modelling the
	// rename cascade having already committed) but before Work reaches the bump below.
	cancel()

	job := newTestEffectiveCategoryRefreshJob(EffectiveCategoryRefreshArgs{
		UserID:           userID,
		CustomCategoryID: customCategoryID,
	})
	err = worker.Work(ctx, job)
	require.NoError(t, err)
	require.Error(t, ctx.Err(), "sanity: the job's own context must actually be cancelled by now")

	postGen, err := c.Generations().Get(context.Background(), cache.ScopeUser, userID)
	require.NoError(t, err)
	assert.NotEqual(t, preGen, postGen,
		"the cache bump must survive the job's own context being cancelled after the rename "+
			"cascade committed")
}
