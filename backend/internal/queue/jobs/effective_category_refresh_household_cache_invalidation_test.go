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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
)

// householdGenerationWriteRecorder wraps redisclient.Client and records every key passed to Set,
// so a test can assert that NO household generation key (rendered as "{h:...}:gen") was ever
// written — a positive assertion on writes, not a comparison of two back-to-back Gets of an
// unrelated scope (which are trivially equal with no possible intervening write and would pass
// even a bug that bumped "" or the wrong household ID).
type householdGenerationWriteRecorder struct {
	redisclient.Client
	mu          sync.Mutex
	writtenKeys []string
}

func (r *householdGenerationWriteRecorder) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	r.mu.Lock()
	r.writtenKeys = append(r.writtenKeys, key)
	r.mu.Unlock()
	return r.Client.Set(ctx, key, value, ttl)
}

func (r *householdGenerationWriteRecorder) householdGenerationKeysWritten() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, k := range r.writtenKeys {
		if strings.Contains(k, "{h:") && strings.HasSuffix(k, ":gen") {
			out = append(out, k)
		}
	}
	return out
}

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
// user with no household must not spuriously mint/bump ANY household scope.
//
// Issue #1531 GA-6: the previous version of this test read cache.ScopeHousehold,
// "some-unrelated-household"'s generation TWICE, both AFTER Work had already returned, with no
// possible intervening write between the two Gets — two consecutive Gets of an unrelated,
// untouched scope are trivially equal regardless of what Work actually did. A bug that bumped ""
// or the WRONG household ID (any ID other than "some-unrelated-household") would still pass this
// assertion vacuously. This version instead asserts on writes: a recording client proves no
// household generation key (any household, not just "some-unrelated-household") was ever
// Set-called during Work.
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
	recorder := &householdGenerationWriteRecorder{Client: fakeRedis}
	c := cache.NewCache(cache.Options{Client: recorder})
	worker.SetCacheInvalidation(c.Generations(), fakeEffectiveCategoryHouseholdLookupNone{})

	job := newTestEffectiveCategoryRefreshJob(EffectiveCategoryRefreshArgs{
		UserID:           userID,
		CustomCategoryID: customCategoryID,
	})
	err := worker.Work(ctx, job)
	require.NoError(t, err)

	assert.Empty(t, recorder.householdGenerationKeysWritten(),
		"a user with no household must not write ANY household generation key — a bug that bumped "+
			"\"\" or the wrong household ID must fail this, unlike two back-to-back Gets of an "+
			"unrelated scope (Issue #1531 GA-6)")
}
