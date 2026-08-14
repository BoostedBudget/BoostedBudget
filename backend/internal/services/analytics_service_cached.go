package services

import (
	"context"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// Issue #1531 Task 10a: cache method constants. Each is a `const` string literal (never derived
// via reflection) so a method rename is a compile error rather than a silent key-space change —
// see cache.Key's doc comment. TestAnalyticsCacheMethods_AreUnique asserts these stay distinct;
// unlike Task 9's TestMethodConstants_AreUnique (placeholder literals, no real consumer existed
// yet), this test runs over the actual constants this file uses.
const (
	methodGetCashFlowSummary              = "GetCashFlowSummary"
	methodGetCashFlowHistory              = "GetCashFlowHistory"
	methodGetSpendingByCategory           = "GetSpendingByCategory"
	methodGetSpendingByMerchant           = "GetSpendingByMerchant"
	methodGetSpendingTrends               = "GetSpendingTrends"
	methodGetRecurringSubscriptions       = "GetRecurringSubscriptions"
	methodGetMonthOverMonthSpendingChange = "GetMonthOverMonthSpendingChange"
	methodGetNetWorthTrend                = "GetNetWorthTrend"
	methodGetBudgetVsActual               = "GetBudgetVsActual"
	methodGetSavingsRateForPeriod         = "GetSavingsRateForPeriod"
	methodGetAverageMonthlySpend          = "GetAverageMonthlySpend"
	methodGetCategoryTotalForPeriod       = "GetCategoryTotalForPeriod"
	methodGetIncomeSourceBreakdown        = "GetIncomeSourceBreakdown"

	methodGetTopExpense                   = "GetTopExpense"
	methodGetLargestTransactionsForPeriod = "GetLargestTransactionsForPeriod"
	methodGetUnusualSpend                 = "GetUnusualSpend"
	methodGetCategoryBreakdownForMerchant = "GetCategoryBreakdownForMerchant"
)

// cachedAnalyticsService decorates an AnalyticsService with read-through caching. It wraps the
// SERVICE, not the repository: GetNetWorthTrend fans out via balanceSnapshotRepo INSIDE the
// service (analytics_service.go's GetNetWorthTrend), and 39+ non-test call sites construct
// repositories directly over a pgx.Tx, bypassing anything wired here — a repository-level
// decorator would silently miss both. See task-10a-brief.md for the full rationale.
//
// ⚠ Until Task 10b lands invalidation (generation bumps), every method here can serve stale data
// for up to its namespace's TTL after a write. That is a deliberate, temporary property of this
// task's scope — not an oversight.
type cachedAnalyticsService struct {
	inner AnalyticsService
	c     cache.Cache
}

// NewCachedAnalyticsService returns an AnalyticsService that serves read methods from c when
// possible, falling through to inner on a miss/bypass. inner MUST already have
// SetBalanceSnapshotRepo called (if applicable) before it is wrapped — this decorator does not
// forward the setter, and a service wrapped before that call would permanently miss net-worth data.
func NewCachedAnalyticsService(inner AnalyticsService, c cache.Cache) AnalyticsService {
	return &cachedAnalyticsService{inner: inner, c: c}
}

func (s *cachedAnalyticsService) GetCashFlowSummary(ctx context.Context, userID string, filters domain.CashFlowFilters) (*domain.CashFlowSummaryResponse, error) {
	k := cache.NewKey(cache.NamespaceAnalytics, cache.ScopeFor(userID, filters.HouseholdScope), methodGetCashFlowSummary).
		Enc(func(e *cache.ArgEncoder) { cache.EncodeCashFlowFilters(e, filters) })
	return cache.GetOrLoad(ctx, s.c, k, func(ctx context.Context) (*domain.CashFlowSummaryResponse, error) {
		return s.inner.GetCashFlowSummary(ctx, userID, filters)
	})
}

func (s *cachedAnalyticsService) GetCashFlowHistory(ctx context.Context, userID string, filters domain.CashFlowFilters) (*domain.CashFlowHistoryResponse, error) {
	k := cache.NewKey(cache.NamespaceAnalytics, cache.ScopeFor(userID, filters.HouseholdScope), methodGetCashFlowHistory).
		Enc(func(e *cache.ArgEncoder) { cache.EncodeCashFlowFilters(e, filters) })
	return cache.GetOrLoad(ctx, s.c, k, func(ctx context.Context) (*domain.CashFlowHistoryResponse, error) {
		return s.inner.GetCashFlowHistory(ctx, userID, filters)
	})
}

func (s *cachedAnalyticsService) GetSpendingByCategory(ctx context.Context, userID string, filters domain.SpendingFilters) (*domain.SpendingByCategoryResponse, error) {
	k := cache.NewKey(cache.NamespaceAnalytics, cache.ScopeFor(userID, filters.HouseholdScope), methodGetSpendingByCategory).
		Enc(func(e *cache.ArgEncoder) { cache.EncodeSpendingFilters(e, filters) })
	return cache.GetOrLoad(ctx, s.c, k, func(ctx context.Context) (*domain.SpendingByCategoryResponse, error) {
		return s.inner.GetSpendingByCategory(ctx, userID, filters)
	})
}

func (s *cachedAnalyticsService) GetSpendingByMerchant(ctx context.Context, userID string, filters domain.SpendingFilters, limit int) (*domain.SpendingByMerchantResponse, error) {
	k := cache.NewKey(cache.NamespaceAnalytics, cache.ScopeFor(userID, filters.HouseholdScope), methodGetSpendingByMerchant).
		Enc(func(e *cache.ArgEncoder) {
			cache.EncodeSpendingFilters(e, filters)
			e.Int(limit)
		})
	return cache.GetOrLoad(ctx, s.c, k, func(ctx context.Context) (*domain.SpendingByMerchantResponse, error) {
		return s.inner.GetSpendingByMerchant(ctx, userID, filters, limit)
	})
}

func (s *cachedAnalyticsService) GetSpendingTrends(ctx context.Context, userID string, filters domain.SpendingFilters) (*domain.SpendingTrendsResponse, error) {
	k := cache.NewKey(cache.NamespaceAnalytics, cache.ScopeFor(userID, filters.HouseholdScope), methodGetSpendingTrends).
		Enc(func(e *cache.ArgEncoder) { cache.EncodeSpendingFilters(e, filters) })
	return cache.GetOrLoad(ctx, s.c, k, func(ctx context.Context) (*domain.SpendingTrendsResponse, error) {
		return s.inner.GetSpendingTrends(ctx, userID, filters)
	})
}

func (s *cachedAnalyticsService) GetCategoryTotalForPeriod(ctx context.Context, userID string, category string, startDate time.Time, endDate time.Time) (*domain.CategoryTotalResponse, error) {
	// No HouseholdScope on this method's signature — always personal scope.
	k := cache.NewKey(cache.NamespaceAnalytics, cache.ScopeFor(userID, nil), methodGetCategoryTotalForPeriod).
		Enc(func(e *cache.ArgEncoder) {
			e.String(category)
			e.Time(startDate)
			e.Time(endDate)
		})
	return cache.GetOrLoad(ctx, s.c, k, func(ctx context.Context) (*domain.CategoryTotalResponse, error) {
		return s.inner.GetCategoryTotalForPeriod(ctx, userID, category, startDate, endDate)
	})
}

func (s *cachedAnalyticsService) GetTopExpense(ctx context.Context, userID string, startDate time.Time, endDate time.Time) (*domain.TopExpenseResponse, error) {
	// analytics_rows: carries merchant names / transaction descriptions — 5-min GDPR cap.
	k := cache.NewKey(cache.NamespaceAnalyticsRows, cache.ScopeFor(userID, nil), methodGetTopExpense).
		Enc(func(e *cache.ArgEncoder) {
			e.Time(startDate)
			e.Time(endDate)
		})
	return cache.GetOrLoad(ctx, s.c, k, func(ctx context.Context) (*domain.TopExpenseResponse, error) {
		return s.inner.GetTopExpense(ctx, userID, startDate, endDate)
	})
}

func (s *cachedAnalyticsService) GetAverageMonthlySpend(ctx context.Context, userID string, filters domain.SpendingFilters) (*domain.AverageMonthlySpendResponse, error) {
	k := cache.NewKey(cache.NamespaceAnalytics, cache.ScopeFor(userID, filters.HouseholdScope), methodGetAverageMonthlySpend).
		Enc(func(e *cache.ArgEncoder) { cache.EncodeSpendingFilters(e, filters) })
	return cache.GetOrLoad(ctx, s.c, k, func(ctx context.Context) (*domain.AverageMonthlySpendResponse, error) {
		return s.inner.GetAverageMonthlySpend(ctx, userID, filters)
	})
}

func (s *cachedAnalyticsService) GetRecurringSubscriptions(ctx context.Context, userID string) (*domain.RecurringSubscriptionsResponse, error) {
	// No params beyond userID: the underlying query uses a trailing-12-months-from-now window
	// computed inside the inner service, so no explicit args to encode.
	k := cache.NewKey(cache.NamespaceAnalytics, cache.ScopeFor(userID, nil), methodGetRecurringSubscriptions).
		Enc(func(e *cache.ArgEncoder) {})
	return cache.GetOrLoad(ctx, s.c, k, func(ctx context.Context) (*domain.RecurringSubscriptionsResponse, error) {
		return s.inner.GetRecurringSubscriptions(ctx, userID)
	})
}

func (s *cachedAnalyticsService) GetSavingsRateForPeriod(ctx context.Context, userID string, startDate time.Time, endDate time.Time, scope *domain.HouseholdScope) (*domain.SavingsRateResponse, error) {
	k := cache.NewKey(cache.NamespaceAnalytics, cache.ScopeFor(userID, scope), methodGetSavingsRateForPeriod).
		Enc(func(e *cache.ArgEncoder) {
			e.Time(startDate)
			e.Time(endDate)
			cache.EncodeHouseholdScope(e, scope)
		})
	return cache.GetOrLoad(ctx, s.c, k, func(ctx context.Context) (*domain.SavingsRateResponse, error) {
		return s.inner.GetSavingsRateForPeriod(ctx, userID, startDate, endDate, scope)
	})
}

func (s *cachedAnalyticsService) GetMonthOverMonthSpendingChange(ctx context.Context, userID string, category string, months int, accountTypes []string, includeTransfers bool, scope *domain.HouseholdScope) (*domain.MonthOverMonthResponse, error) {
	k := cache.NewKey(cache.NamespaceAnalytics, cache.ScopeFor(userID, scope), methodGetMonthOverMonthSpendingChange).
		Enc(func(e *cache.ArgEncoder) {
			e.String(category)
			e.Int(months)
			e.StringSetSorted(accountTypes)
			e.Bool(includeTransfers)
			cache.EncodeHouseholdScope(e, scope)
		})
	return cache.GetOrLoad(ctx, s.c, k, func(ctx context.Context) (*domain.MonthOverMonthResponse, error) {
		return s.inner.GetMonthOverMonthSpendingChange(ctx, userID, category, months, accountTypes, includeTransfers, scope)
	})
}

func (s *cachedAnalyticsService) GetLargestTransactionsForPeriod(ctx context.Context, userID string, filters domain.SpendingFilters, limit int) (*domain.LargestTransactionsResponse, error) {
	k := cache.NewKey(cache.NamespaceAnalyticsRows, cache.ScopeFor(userID, filters.HouseholdScope), methodGetLargestTransactionsForPeriod).
		Enc(func(e *cache.ArgEncoder) {
			cache.EncodeSpendingFilters(e, filters)
			e.Int(limit)
		})
	return cache.GetOrLoad(ctx, s.c, k, func(ctx context.Context) (*domain.LargestTransactionsResponse, error) {
		return s.inner.GetLargestTransactionsForPeriod(ctx, userID, filters, limit)
	})
}

func (s *cachedAnalyticsService) GetCategoryBreakdownForMerchant(ctx context.Context, userID string, merchantID string, filters domain.SpendingFilters) (*domain.CategoryBreakdownForMerchantResponse, error) {
	k := cache.NewKey(cache.NamespaceAnalyticsRows, cache.ScopeFor(userID, filters.HouseholdScope), methodGetCategoryBreakdownForMerchant).
		Enc(func(e *cache.ArgEncoder) {
			e.String(merchantID)
			cache.EncodeSpendingFilters(e, filters)
		})
	return cache.GetOrLoad(ctx, s.c, k, func(ctx context.Context) (*domain.CategoryBreakdownForMerchantResponse, error) {
		return s.inner.GetCategoryBreakdownForMerchant(ctx, userID, merchantID, filters)
	})
}

func (s *cachedAnalyticsService) GetIncomeSourceBreakdown(ctx context.Context, userID string, filters domain.SpendingFilters) (*domain.IncomeSourceBreakdownResponse, error) {
	k := cache.NewKey(cache.NamespaceAnalytics, cache.ScopeFor(userID, filters.HouseholdScope), methodGetIncomeSourceBreakdown).
		Enc(func(e *cache.ArgEncoder) { cache.EncodeSpendingFilters(e, filters) })
	return cache.GetOrLoad(ctx, s.c, k, func(ctx context.Context) (*domain.IncomeSourceBreakdownResponse, error) {
		return s.inner.GetIncomeSourceBreakdown(ctx, userID, filters)
	})
}

func (s *cachedAnalyticsService) GetUnusualSpend(ctx context.Context, userID string, startDate, endDate time.Time) (*domain.UnusualSpendResponse, error) {
	// No HouseholdScope on this method's signature — always personal scope.
	k := cache.NewKey(cache.NamespaceAnalyticsRows, cache.ScopeFor(userID, nil), methodGetUnusualSpend).
		Enc(func(e *cache.ArgEncoder) {
			e.Time(startDate)
			e.Time(endDate)
		})
	return cache.GetOrLoad(ctx, s.c, k, func(ctx context.Context) (*domain.UnusualSpendResponse, error) {
		return s.inner.GetUnusualSpend(ctx, userID, startDate, endDate)
	})
}

func (s *cachedAnalyticsService) GetNetWorthTrend(ctx context.Context, userID string, grouping string, scope *domain.HouseholdScope) (*domain.NetWorthTrendResponse, error) {
	k := cache.NewKey(cache.NamespaceAnalytics, cache.ScopeFor(userID, scope), methodGetNetWorthTrend).
		Enc(func(e *cache.ArgEncoder) {
			e.String(grouping)
			cache.EncodeHouseholdScope(e, scope)
		})
	return cache.GetOrLoad(ctx, s.c, k, func(ctx context.Context) (*domain.NetWorthTrendResponse, error) {
		return s.inner.GetNetWorthTrend(ctx, userID, grouping, scope)
	})
}

func (s *cachedAnalyticsService) GetBudgetVsActual(ctx context.Context, userID string, period string) (*domain.BudgetVsActualResponse, error) {
	// No HouseholdScope on this method's signature — always personal scope.
	k := cache.NewKey(cache.NamespaceAnalytics, cache.ScopeFor(userID, nil), methodGetBudgetVsActual).
		Enc(func(e *cache.ArgEncoder) { e.String(period) })
	return cache.GetOrLoad(ctx, s.c, k, func(ctx context.Context) (*domain.BudgetVsActualResponse, error) {
		return s.inner.GetBudgetVsActual(ctx, userID, period)
	})
}

var _ AnalyticsService = (*cachedAnalyticsService)(nil)
