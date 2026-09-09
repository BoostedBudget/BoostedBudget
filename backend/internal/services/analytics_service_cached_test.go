package services

import (
	"context"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeCache is a minimal in-memory cache.Cache double: real enough to exercise
// cachedAnalyticsService's read-through behavior (hit/miss, per-key isolation) without a Redis
// dependency. It does not model generations/TTL — Task 10a's decorators do not touch either.
type fakeCache struct {
	store   map[string][]byte
	enabled bool
}

func newFakeCache() *fakeCache { return &fakeCache{store: map[string][]byte{}, enabled: true} }

func (c *fakeCache) dataKey(k cache.Key) string {
	return string(k.Namespace) + "|" + string(k.ScopeKind) + "|" + k.ScopeID + "|" + k.Method + "|" + k.ArgHash
}

func (c *fakeCache) GetOrLoad(ctx context.Context, k cache.Key, loader func(context.Context) ([]byte, error)) ([]byte, bool, error) {
	if !c.enabled {
		v, err := loader(ctx)
		return v, false, err
	}
	dk := c.dataKey(k)
	if v, ok := c.store[dk]; ok {
		return v, true, nil
	}
	v, err := loader(ctx)
	if err != nil {
		return nil, false, err
	}
	c.store[dk] = v
	return v, false, nil
}

func (c *fakeCache) Generations() *cache.Generations { return nil }
func (c *fakeCache) Enabled(cache.Namespace) bool    { return c.enabled }

var _ cache.Cache = (*fakeCache)(nil)

// countingAnalyticsService wraps a canned AnalyticsService and counts calls per method, so tests
// can assert a cache hit skipped the inner call entirely.
type countingAnalyticsService struct {
	AnalyticsService
	calls map[string]int
}

func newCountingAnalyticsService(inner AnalyticsService) *countingAnalyticsService {
	return &countingAnalyticsService{AnalyticsService: inner, calls: map[string]int{}}
}

func (s *countingAnalyticsService) GetSpendingByCategory(ctx context.Context, userID string, filters domain.SpendingFilters) (*domain.SpendingByCategoryResponse, error) {
	s.calls["GetSpendingByCategory"]++
	return s.AnalyticsService.GetSpendingByCategory(ctx, userID, filters)
}

func (s *countingAnalyticsService) GetSpendingByMerchant(ctx context.Context, userID string, filters domain.SpendingFilters, limit int) (*domain.SpendingByMerchantResponse, error) {
	s.calls["GetSpendingByMerchant"]++
	return s.AnalyticsService.GetSpendingByMerchant(ctx, userID, filters, limit)
}

func (s *countingAnalyticsService) GetCategoryTotalForPeriod(ctx context.Context, userID string, category string, startDate time.Time, endDate time.Time) (*domain.CategoryTotalResponse, error) {
	s.calls["GetCategoryTotalForPeriod"]++
	return s.AnalyticsService.GetCategoryTotalForPeriod(ctx, userID, category, startDate, endDate)
}

func (s *countingAnalyticsService) GetSavingsRateForPeriod(ctx context.Context, userID string, startDate time.Time, endDate time.Time, scope *domain.HouseholdScope) (*domain.SavingsRateResponse, error) {
	s.calls["GetSavingsRateForPeriod"]++
	return s.AnalyticsService.GetSavingsRateForPeriod(ctx, userID, startDate, endDate, scope)
}

func (s *countingAnalyticsService) GetRecurringSubscriptions(ctx context.Context, userID string) (*domain.RecurringSubscriptionsResponse, error) {
	s.calls["GetRecurringSubscriptions"]++
	return s.AnalyticsService.GetRecurringSubscriptions(ctx, userID)
}

func (s *countingAnalyticsService) GetNetWorthTrend(ctx context.Context, userID string, grouping string, scope *domain.HouseholdScope) (*domain.NetWorthTrendResponse, error) {
	s.calls["GetNetWorthTrend"]++
	return s.AnalyticsService.GetNetWorthTrend(ctx, userID, grouping, scope)
}

func (s *countingAnalyticsService) GetTopExpense(ctx context.Context, userID string, startDate time.Time, endDate time.Time) (*domain.TopExpenseResponse, error) {
	s.calls["GetTopExpense"]++
	return s.AnalyticsService.GetTopExpense(ctx, userID, startDate, endDate)
}

func TestAnalyticsCacheMethods_AreUnique(t *testing.T) {
	// Real constants, not Task 9's placeholders — see this file's const block.
	methods := []string{
		methodGetCashFlowSummary,
		methodGetCashFlowHistory,
		methodGetSpendingByCategory,
		methodGetSpendingByMerchant,
		methodGetSpendingTrends,
		methodGetRecurringSubscriptions,
		methodGetMonthOverMonthSpendingChange,
		methodGetNetWorthTrend,
		methodGetBudgetVsActual,
		methodGetSavingsRateForPeriod,
		methodGetAverageMonthlySpend,
		methodGetCategoryTotalForPeriod,
		methodGetIncomeSourceBreakdown,
		methodGetCategoryHierarchy,
		methodGetTopExpense,
		methodGetLargestTransactionsForPeriod,
		methodGetUnusualSpend,
		methodGetCategoryBreakdownForMerchant,
	}
	seen := make(map[string]bool, len(methods))
	for _, m := range methods {
		assert.False(t, seen[m], "duplicate method constant %q", m)
		seen[m] = true
	}
	assert.Len(t, seen, 18, "expected exactly 18 cached analytics methods (14 analytics + 4 analytics_rows)")
}

func testAnalyticsRepoFixture() *mockAnalyticsRepository {
	return &mockAnalyticsRepository{}
}

func TestCachedAnalyticsService_SecondIdenticalCallIsACacheHit(t *testing.T) {
	repo := testAnalyticsRepoFixture()
	inner := newCountingAnalyticsService(NewAnalyticsService(repo))
	c := newFakeCache()
	svc := NewCachedAnalyticsService(inner, c)

	filters := domain.SpendingFilters{
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC),
	}

	ctx := context.Background()
	_, err := svc.GetSpendingByCategory(ctx, "user-1", filters)
	require.NoError(t, err)
	_, err = svc.GetSpendingByCategory(ctx, "user-1", filters)
	require.NoError(t, err)

	assert.Equal(t, 1, inner.calls["GetSpendingByCategory"], "second identical call should have been served from cache")
}

func TestCachedAnalyticsService_DifferentFiltersAreDifferentCacheEntries(t *testing.T) {
	repo := testAnalyticsRepoFixture()
	inner := newCountingAnalyticsService(NewAnalyticsService(repo))
	c := newFakeCache()
	svc := NewCachedAnalyticsService(inner, c)

	ctx := context.Background()
	f1 := domain.SpendingFilters{StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), EndDate: time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)}
	f2 := domain.SpendingFilters{StartDate: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC), EndDate: time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC)}

	_, err := svc.GetSpendingByCategory(ctx, "user-1", f1)
	require.NoError(t, err)
	_, err = svc.GetSpendingByCategory(ctx, "user-1", f2)
	require.NoError(t, err)

	assert.Equal(t, 2, inner.calls["GetSpendingByCategory"], "different filters must not share a cache entry")
}

func TestCachedAnalyticsService_DifferentUsersAreDifferentCacheEntries(t *testing.T) {
	repo := testAnalyticsRepoFixture()
	inner := newCountingAnalyticsService(NewAnalyticsService(repo))
	c := newFakeCache()
	svc := NewCachedAnalyticsService(inner, c)

	ctx := context.Background()
	filters := domain.SpendingFilters{StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), EndDate: time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)}

	_, err := svc.GetSpendingByCategory(ctx, "user-1", filters)
	require.NoError(t, err)
	_, err = svc.GetSpendingByCategory(ctx, "user-2", filters)
	require.NoError(t, err)

	assert.Equal(t, 2, inner.calls["GetSpendingByCategory"], "different users must not share a cache entry")
}

func TestCachedAnalyticsService_LimitIsPartOfTheKey(t *testing.T) {
	repo := testAnalyticsRepoFixture()
	inner := newCountingAnalyticsService(NewAnalyticsService(repo))
	c := newFakeCache()
	svc := NewCachedAnalyticsService(inner, c)

	ctx := context.Background()
	filters := domain.SpendingFilters{StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), EndDate: time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)}

	_, err := svc.GetSpendingByMerchant(ctx, "user-1", filters, 5)
	require.NoError(t, err)
	_, err = svc.GetSpendingByMerchant(ctx, "user-1", filters, 10)
	require.NoError(t, err)

	assert.Equal(t, 2, inner.calls["GetSpendingByMerchant"], "different limit must not share a cache entry")
}

func TestCachedAnalyticsService_ScopeParamIsPartOfTheKey(t *testing.T) {
	repo := testAnalyticsRepoFixture()
	inner := newCountingAnalyticsService(NewAnalyticsService(repo))
	c := newFakeCache()
	svc := NewCachedAnalyticsService(inner, c)

	ctx := context.Background()
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)

	_, err := svc.GetSavingsRateForPeriod(ctx, "user-1", start, end, nil)
	require.NoError(t, err)
	_, err = svc.GetSavingsRateForPeriod(ctx, "user-1", start, end, &domain.HouseholdScope{HouseholdID: "hh-1", MemberUserIDs: []string{"user-1"}})
	require.NoError(t, err)

	assert.Equal(t, 2, inner.calls["GetSavingsRateForPeriod"], "personal vs household scope must not share a cache entry")
}

func TestCachedAnalyticsService_NoArgMethodStillCaches(t *testing.T) {
	repo := testAnalyticsRepoFixture()
	inner := newCountingAnalyticsService(NewAnalyticsService(repo))
	c := newFakeCache()
	svc := NewCachedAnalyticsService(inner, c)

	ctx := context.Background()
	_, err := svc.GetRecurringSubscriptions(ctx, "user-1")
	require.NoError(t, err)
	_, err = svc.GetRecurringSubscriptions(ctx, "user-1")
	require.NoError(t, err)

	assert.Equal(t, 1, inner.calls["GetRecurringSubscriptions"])
}

func TestCachedAnalyticsService_NamespaceDisabledBypassesCache(t *testing.T) {
	repo := testAnalyticsRepoFixture()
	inner := newCountingAnalyticsService(NewAnalyticsService(repo))
	c := newFakeCache()
	c.enabled = false
	svc := NewCachedAnalyticsService(inner, c)

	ctx := context.Background()
	filters := domain.SpendingFilters{StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), EndDate: time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)}

	_, err := svc.GetSpendingByCategory(ctx, "user-1", filters)
	require.NoError(t, err)
	_, err = svc.GetSpendingByCategory(ctx, "user-1", filters)
	require.NoError(t, err)

	assert.Equal(t, 2, inner.calls["GetSpendingByCategory"], "a disabled/noop cache must call inner on every request")
}

// TestCachedAnalyticsService_NetWorthTrendDelegatesToWiredInnerService is the regression test for
// the "wrap AFTER SetBalanceSnapshotRepo" ordering hazard: this decorator never touches
// balanceSnapshotRepo itself, so if the inner *analyticsService is wrapped before its snapshot
// repo is set, EVERY subsequent call — cached or not — permanently sees a nil repo. Wrapping the
// correctly-configured inner service must produce a successful (cacheable) result.
func TestCachedAnalyticsService_NetWorthTrendDelegatesToWiredInnerService(t *testing.T) {
	repo := testAnalyticsRepoFixture()
	inner := NewAnalyticsService(repo)
	inner.SetBalanceSnapshotRepo(&mockBalanceSnapshotRepository{})
	counting := newCountingAnalyticsService(inner)
	c := newFakeCache()
	svc := NewCachedAnalyticsService(counting, c)

	ctx := context.Background()
	_, err := svc.GetNetWorthTrend(ctx, "user-1", "monthly", nil)
	require.NoError(t, err)
	_, err = svc.GetNetWorthTrend(ctx, "user-1", "monthly", nil)
	require.NoError(t, err)

	assert.Equal(t, 1, counting.calls["GetNetWorthTrend"])
}

func TestCachedAnalyticsService_TopExpenseUsesAnalyticsRowsNamespace(t *testing.T) {
	repo := testAnalyticsRepoFixture()
	inner := newCountingAnalyticsService(NewAnalyticsService(repo))
	c := newFakeCache()
	svc := NewCachedAnalyticsService(inner, c)

	ctx := context.Background()
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)

	_, err := svc.GetTopExpense(ctx, "user-1", start, end)
	require.NoError(t, err)

	found := false
	for dk := range c.store {
		if len(dk) > 0 {
			found = found || (dk[:len(string(cache.NamespaceAnalyticsRows))] == string(cache.NamespaceAnalyticsRows))
		}
	}
	assert.True(t, found, "expected GetTopExpense's cache entry to be keyed under the analytics_rows namespace")
}
