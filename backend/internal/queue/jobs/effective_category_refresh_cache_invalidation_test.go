package jobs

// Issue #1531 Task 10b-2a-iii — end-to-end proof that EffectiveCategoryRefreshWorker.Work bumps
// the affected user's cache generation after a successful rename cascade. Mirrors
// plaid_sync_cache_invalidation_test.go's bar: assert cache.Generations.Get(...) genuinely
// differs pre/post, over a real cache.Generations backed by redisclient.NewFake.

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
)

// TestEffectiveCategoryRefreshWorker_Work_BumpsUserCacheGenerationAfterSuccess proves the
// headline fix: this job rewrites transactions.effective_category_name — the field several
// cached analytics queries GROUP BY — and after a successful call, the user's cache generation
// must have changed.
func TestEffectiveCategoryRefreshWorker_Work_BumpsUserCacheGenerationAfterSuccess(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	customCategoryID := uuid.New().String()

	mockRepo := new(MockTransactionRepositoryForCategoryRefresh)
	mockRepo.On("UpdateEffectiveCategoryNameByCustomCategoryID", ctx, userID, customCategoryID).
		Return(5, nil)

	worker := NewEffectiveCategoryRefreshWorker(EffectiveCategoryRefreshWorkerDeps{
		TransactionRepo: mockRepo,
	})

	fakeRedis := redisclient.NewFake("cache")
	c := cache.NewCache(cache.Options{Client: fakeRedis})
	worker.SetCacheInvalidation(c.Generations())

	preGen, err := c.Generations().Get(ctx, cache.ScopeUser, userID)
	require.NoError(t, err)

	job := newTestEffectiveCategoryRefreshJob(EffectiveCategoryRefreshArgs{
		UserID:           userID,
		CustomCategoryID: customCategoryID,
	})
	err = worker.Work(ctx, job)
	require.NoError(t, err)

	postGen, err := c.Generations().Get(ctx, cache.ScopeUser, userID)
	require.NoError(t, err)
	assert.NotEqual(t, preGen, postGen,
		"EffectiveCategoryRefreshWorker.Work must bump the affected user's cache generation "+
			"after a successful rename cascade — it rewrites the very field spending-by-category/"+
			"merchant and budget-vs-actual analytics GROUP BY")
}

// TestEffectiveCategoryRefreshWorker_Work_ZeroRowsUpdated_StillBumps documents a deliberate
// choice, not an oversight (raised in adversarial review): the bump is unconditional on
// rowsUpdated. A rename that matched zero transactions (e.g. the custom category had no
// transactions assigned at the moment of rename) still counts as a successful call — the bump
// is a false-positive-safe over-invalidation (wasted cache miss), never a false negative, and
// gating it on rowsUpdated>0 would add a second success/failure axis to reason about for no
// correctness gain.
func TestEffectiveCategoryRefreshWorker_Work_ZeroRowsUpdated_StillBumps(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	customCategoryID := uuid.New().String()

	mockRepo := new(MockTransactionRepositoryForCategoryRefresh)
	mockRepo.On("UpdateEffectiveCategoryNameByCustomCategoryID", ctx, userID, customCategoryID).
		Return(0, nil)

	worker := NewEffectiveCategoryRefreshWorker(EffectiveCategoryRefreshWorkerDeps{
		TransactionRepo: mockRepo,
	})

	fakeRedis := redisclient.NewFake("cache")
	c := cache.NewCache(cache.Options{Client: fakeRedis})
	worker.SetCacheInvalidation(c.Generations())

	preGen, err := c.Generations().Get(ctx, cache.ScopeUser, userID)
	require.NoError(t, err)

	job := newTestEffectiveCategoryRefreshJob(EffectiveCategoryRefreshArgs{
		UserID:           userID,
		CustomCategoryID: customCategoryID,
	})
	err = worker.Work(ctx, job)
	require.NoError(t, err)

	postGen, err := c.Generations().Get(ctx, cache.ScopeUser, userID)
	require.NoError(t, err)
	assert.NotEqual(t, preGen, postGen,
		"a successful call with rowsUpdated==0 still bumps — the call succeeded, and gating on "+
			"row count would only risk a false negative for no benefit")
}

// TestEffectiveCategoryRefreshWorker_Work_RepoError_DoesNotBump proves the negative: a failed
// update must not bump — nothing committed.
func TestEffectiveCategoryRefreshWorker_Work_RepoError_DoesNotBump(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	customCategoryID := uuid.New().String()
	dbErr := errors.New("database connection lost")

	mockRepo := new(MockTransactionRepositoryForCategoryRefresh)
	mockRepo.On("UpdateEffectiveCategoryNameByCustomCategoryID", ctx, userID, customCategoryID).
		Return(0, dbErr)

	worker := NewEffectiveCategoryRefreshWorker(EffectiveCategoryRefreshWorkerDeps{
		TransactionRepo: mockRepo,
	})

	fakeRedis := redisclient.NewFake("cache")
	c := cache.NewCache(cache.Options{Client: fakeRedis})
	worker.SetCacheInvalidation(c.Generations())

	preGen, err := c.Generations().Get(ctx, cache.ScopeUser, userID)
	require.NoError(t, err)

	job := newTestEffectiveCategoryRefreshJob(EffectiveCategoryRefreshArgs{
		UserID:           userID,
		CustomCategoryID: customCategoryID,
	})
	err = worker.Work(ctx, job)
	require.Error(t, err)

	postGen, err := c.Generations().Get(ctx, cache.ScopeUser, userID)
	require.NoError(t, err)
	assert.Equal(t, preGen, postGen, "a failed update must not bump — nothing committed")
}

// TestEffectiveCategoryRefreshWorker_Work_NilCacheGens_NoPanic proves SetCacheInvalidation is
// truly optional.
func TestEffectiveCategoryRefreshWorker_Work_NilCacheGens_NoPanic(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	customCategoryID := uuid.New().String()

	mockRepo := new(MockTransactionRepositoryForCategoryRefresh)
	mockRepo.On("UpdateEffectiveCategoryNameByCustomCategoryID", ctx, userID, customCategoryID).
		Return(0, nil)

	worker := NewEffectiveCategoryRefreshWorker(EffectiveCategoryRefreshWorkerDeps{
		TransactionRepo: mockRepo,
	})
	// SetCacheInvalidation deliberately not called.

	job := newTestEffectiveCategoryRefreshJob(EffectiveCategoryRefreshArgs{
		UserID:           userID,
		CustomCategoryID: customCategoryID,
	})
	require.NotPanics(t, func() {
		err := worker.Work(ctx, job)
		require.NoError(t, err)
	})
}
