package jobs

// Issue #1531 M7 — EffectiveCategoryRefreshWorker rewrites transactions.effective_category_name
// directly, and household-scoped spending-by-category and spending-trends both SELECT and GROUP
// BY that same column (analytics_repository.go's getSpendingByCategoryHousehold and
// getSpendingTrendsHousehold), yet the worker previously bumped ONLY cache.ScopeUser — an
// inconsistency with transaction_sync_service.go's bumpCacheForSync, which bumps both scopes for
// the equivalent kind of write. These tests mirror
// internal/services/transaction_sync_cache_invalidation_test.go's household-bump proof, applied
// to this worker.

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
)

// fakeEffectiveCategoryHouseholdLookupFound always reports the given household as found for
// every user — the simplest positive case for this test.
type fakeEffectiveCategoryHouseholdLookupFound struct {
	household *domain.Household
}

func (f fakeEffectiveCategoryHouseholdLookupFound) GetHouseholdForUser(context.Context, string) (*domain.Household, bool, bool, error) {
	return f.household, false, true, nil
}

// fakeEffectiveCategoryHouseholdLookupNone reports every user as having no household.
type fakeEffectiveCategoryHouseholdLookupNone struct{}

func (fakeEffectiveCategoryHouseholdLookupNone) GetHouseholdForUser(context.Context, string) (*domain.Household, bool, bool, error) {
	return nil, false, false, nil
}

// TestEffectiveCategoryRefreshWorker_Work_BumpsHouseholdCacheGenerationWhenResolvable proves the
// household side of the bump: when the renaming user belongs to a household, that household's
// cached aggregates (household spending-by-category/trends, which GROUP BY
// effective_category_name) must also be invalidated, not just the user's own scope.
func TestEffectiveCategoryRefreshWorker_Work_BumpsHouseholdCacheGenerationWhenResolvable(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	customCategoryID := uuid.New().String()
	household := &domain.Household{ID: "household-ecr-cache-inval"}

	mockRepo := new(MockTransactionRepositoryForCategoryRefresh)
	mockRepo.On("UpdateEffectiveCategoryNameByCustomCategoryID", ctx, userID, customCategoryID).
		Return(5, nil)

	worker := NewEffectiveCategoryRefreshWorker(EffectiveCategoryRefreshWorkerDeps{
		TransactionRepo: mockRepo,
	})

	fakeRedis := redisclient.NewFake("cache")
	c := cache.NewCache(cache.Options{Client: fakeRedis})
	worker.SetCacheInvalidation(c.Generations(), fakeEffectiveCategoryHouseholdLookupFound{household: household})

	preGen, err := c.Generations().Get(ctx, cache.ScopeHousehold, household.ID)
	require.NoError(t, err)

	job := newTestEffectiveCategoryRefreshJob(EffectiveCategoryRefreshArgs{
		UserID:           userID,
		CustomCategoryID: customCategoryID,
	})
	err = worker.Work(ctx, job)
	require.NoError(t, err)

	postGen, err := c.Generations().Get(ctx, cache.ScopeHousehold, household.ID)
	require.NoError(t, err)
	assert.NotEqual(t, preGen, postGen,
		"a successful rename cascade must also bump the renaming user's household scope — "+
			"household spending-by-category/trends GROUP BY the same effective_category_name "+
			"column this job rewrites (Issue #1531 M7)")
}

// TestEffectiveCategoryRefreshWorker_Work_NoHouseholdBumpWhenUserHasNone proves the negative: a
// user with no household must not spuriously mint/bump some unrelated household scope.
func TestEffectiveCategoryRefreshWorker_Work_NoHouseholdBumpWhenUserHasNone(t *testing.T) {
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
	worker.SetCacheInvalidation(c.Generations(), fakeEffectiveCategoryHouseholdLookupNone{})

	job := newTestEffectiveCategoryRefreshJob(EffectiveCategoryRefreshArgs{
		UserID:           userID,
		CustomCategoryID: customCategoryID,
	})
	err := worker.Work(ctx, job)
	require.NoError(t, err)

	preGen, err := c.Generations().Get(ctx, cache.ScopeHousehold, "some-unrelated-household")
	require.NoError(t, err)
	postGen, err := c.Generations().Get(ctx, cache.ScopeHousehold, "some-unrelated-household")
	require.NoError(t, err)
	assert.Equal(t, preGen, postGen, "a user with no household must not bump any household scope")
}
