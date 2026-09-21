package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockAnalyticsRepository is a mock implementation for testing
type mockAnalyticsRepository struct {
	getCashFlowAggregationFn          func(ctx context.Context, userID string, filters domain.CashFlowFilters) (*domain.CashFlowAggregation, error)
	getCashFlowGroupedFn              func(ctx context.Context, userID string, filters domain.CashFlowFilters) ([]domain.CashFlowGroupedData, error)
	getSpendingByCategoryFn           func(ctx context.Context, userID string, filters domain.SpendingFilters) ([]domain.SpendingByCategory, error)
	getSpendingByMerchantFn           func(ctx context.Context, userID string, filters domain.SpendingFilters, limit int) ([]domain.SpendingByMerchant, error)
	getSpendingTrendsFn               func(ctx context.Context, userID string, filters domain.SpendingFilters) ([]domain.SpendingTrendRow, error)
	getCategoryTotalForPeriodFn       func(ctx context.Context, userID string, category string, startDate time.Time, endDate time.Time) (*domain.CategoryTotalRow, error)
	getTopExpenseFn                   func(ctx context.Context, userID string, startDate time.Time, endDate time.Time) (*domain.TopExpenseRow, error)
	getAverageMonthlySpendFn          func(ctx context.Context, userID string, filters domain.SpendingFilters) (*domain.AverageMonthlySpendRow, error)
	getRecurringSubscriptionsFn       func(ctx context.Context, userID string, lookbackStart time.Time) ([]domain.RecurringSubscriptionRow, error)
	getSavingsRateForPeriodFn         func(ctx context.Context, userID string, startDate time.Time, endDate time.Time, scope *domain.HouseholdScope) (*domain.SavingsRateRow, error)
	getMonthOverMonthSpendingFn       func(ctx context.Context, userID string, startDate time.Time, category string, filters domain.SpendingFilters) ([]domain.MonthOverMonthBucketRow, error)
	getLargestTransactionsForPeriodFn func(ctx context.Context, userID string, filters domain.SpendingFilters, limit int) ([]domain.LargestTransactionRow, error)
	getCategoryBreakdownForMerchantFn func(ctx context.Context, userID string, merchantID string, filters domain.SpendingFilters) ([]domain.CategoryBreakdownForMerchantRow, error)
	getCategoryHierarchyFn            func(ctx context.Context, userID string, filters domain.SpendingFilters, direction domain.CategoryDirection) ([]domain.CategoryHierarchyRow, error)
	getIncomeSourceBreakdownFn        func(ctx context.Context, userID string, filters domain.SpendingFilters) ([]domain.IncomeSourceRow, error)
	getUnusualSpendFn                 func(ctx context.Context, userID string, baselineStart, startDate, endDate time.Time, sigmaThreshold float64) ([]domain.UnusualSpendRow, error)
	getBudgetVsActualFn               func(ctx context.Context, userID, period string) ([]domain.BudgetVsActualRow, error)
}

func (m *mockAnalyticsRepository) GetCashFlowAggregation(ctx context.Context, userID string, filters domain.CashFlowFilters) (*domain.CashFlowAggregation, error) {
	if m.getCashFlowAggregationFn != nil {
		return m.getCashFlowAggregationFn(ctx, userID, filters)
	}
	return nil, nil
}

func (m *mockAnalyticsRepository) GetCashFlowGrouped(ctx context.Context, userID string, filters domain.CashFlowFilters) ([]domain.CashFlowGroupedData, error) {
	if m.getCashFlowGroupedFn != nil {
		return m.getCashFlowGroupedFn(ctx, userID, filters)
	}
	return nil, nil
}

func (m *mockAnalyticsRepository) GetSpendingByCategory(ctx context.Context, userID string, filters domain.SpendingFilters) ([]domain.SpendingByCategory, error) {
	if m.getSpendingByCategoryFn != nil {
		return m.getSpendingByCategoryFn(ctx, userID, filters)
	}
	return nil, nil
}

func (m *mockAnalyticsRepository) GetSpendingByMerchant(ctx context.Context, userID string, filters domain.SpendingFilters, limit int) ([]domain.SpendingByMerchant, error) {
	if m.getSpendingByMerchantFn != nil {
		return m.getSpendingByMerchantFn(ctx, userID, filters, limit)
	}
	return nil, nil
}

func (m *mockAnalyticsRepository) GetSpendingTrends(ctx context.Context, userID string, filters domain.SpendingFilters) ([]domain.SpendingTrendRow, error) {
	if m.getSpendingTrendsFn != nil {
		return m.getSpendingTrendsFn(ctx, userID, filters)
	}
	return nil, nil
}

func (m *mockAnalyticsRepository) GetCategoryTotalForPeriod(ctx context.Context, userID string, category string, startDate time.Time, endDate time.Time) (*domain.CategoryTotalRow, error) {
	if m.getCategoryTotalForPeriodFn != nil {
		return m.getCategoryTotalForPeriodFn(ctx, userID, category, startDate, endDate)
	}
	return &domain.CategoryTotalRow{}, nil
}

func (m *mockAnalyticsRepository) GetTopExpense(ctx context.Context, userID string, startDate time.Time, endDate time.Time) (*domain.TopExpenseRow, error) {
	if m.getTopExpenseFn != nil {
		return m.getTopExpenseFn(ctx, userID, startDate, endDate)
	}
	return nil, nil
}

func (m *mockAnalyticsRepository) GetAverageMonthlySpend(ctx context.Context, userID string, filters domain.SpendingFilters) (*domain.AverageMonthlySpendRow, error) {
	if m.getAverageMonthlySpendFn != nil {
		return m.getAverageMonthlySpendFn(ctx, userID, filters)
	}
	return &domain.AverageMonthlySpendRow{}, nil
}

func (m *mockAnalyticsRepository) GetRecurringSubscriptions(ctx context.Context, userID string, lookbackStart time.Time) ([]domain.RecurringSubscriptionRow, error) {
	if m.getRecurringSubscriptionsFn != nil {
		return m.getRecurringSubscriptionsFn(ctx, userID, lookbackStart)
	}
	return nil, nil
}

func (m *mockAnalyticsRepository) GetSavingsRateForPeriod(ctx context.Context, userID string, startDate time.Time, endDate time.Time, scope *domain.HouseholdScope) (*domain.SavingsRateRow, error) {
	if m.getSavingsRateForPeriodFn != nil {
		return m.getSavingsRateForPeriodFn(ctx, userID, startDate, endDate, scope)
	}
	return &domain.SavingsRateRow{}, nil
}

func (m *mockAnalyticsRepository) GetMonthOverMonthSpending(ctx context.Context, userID string, startDate time.Time, category string, filters domain.SpendingFilters) ([]domain.MonthOverMonthBucketRow, error) {
	if m.getMonthOverMonthSpendingFn != nil {
		return m.getMonthOverMonthSpendingFn(ctx, userID, startDate, category, filters)
	}
	return nil, nil
}

func (m *mockAnalyticsRepository) GetLargestTransactionsForPeriod(ctx context.Context, userID string, filters domain.SpendingFilters, limit int) ([]domain.LargestTransactionRow, error) {
	if m.getLargestTransactionsForPeriodFn != nil {
		return m.getLargestTransactionsForPeriodFn(ctx, userID, filters, limit)
	}
	return []domain.LargestTransactionRow{}, nil
}

func (m *mockAnalyticsRepository) GetCategoryBreakdownForMerchant(ctx context.Context, userID string, merchantID string, filters domain.SpendingFilters) ([]domain.CategoryBreakdownForMerchantRow, error) {
	if m.getCategoryBreakdownForMerchantFn != nil {
		return m.getCategoryBreakdownForMerchantFn(ctx, userID, merchantID, filters)
	}
	return nil, nil
}

func (m *mockAnalyticsRepository) GetCategoryHierarchy(ctx context.Context, userID string, filters domain.SpendingFilters, direction domain.CategoryDirection) ([]domain.CategoryHierarchyRow, error) {
	if m.getCategoryHierarchyFn != nil {
		return m.getCategoryHierarchyFn(ctx, userID, filters, direction)
	}
	return nil, nil
}

func (m *mockAnalyticsRepository) GetIncomeSourceBreakdown(ctx context.Context, userID string, filters domain.SpendingFilters) ([]domain.IncomeSourceRow, error) {
	if m.getIncomeSourceBreakdownFn != nil {
		return m.getIncomeSourceBreakdownFn(ctx, userID, filters)
	}
	return nil, nil
}

func (m *mockAnalyticsRepository) GetUnusualSpend(ctx context.Context, userID string, baselineStart, startDate, endDate time.Time, sigmaThreshold float64) ([]domain.UnusualSpendRow, error) {
	if m.getUnusualSpendFn != nil {
		return m.getUnusualSpendFn(ctx, userID, baselineStart, startDate, endDate, sigmaThreshold)
	}
	return []domain.UnusualSpendRow{}, nil
}

func (m *mockAnalyticsRepository) GetBudgetVsActual(ctx context.Context, userID, period string) ([]domain.BudgetVsActualRow, error) {
	if m.getBudgetVsActualFn != nil {
		return m.getBudgetVsActualFn(ctx, userID, period)
	}
	return []domain.BudgetVsActualRow{}, nil
}

func TestAnalyticsService_GetCashFlowSummary(t *testing.T) {
	ctx := context.Background()
	loc := time.UTC

	t.Run("success with period comparison", func(t *testing.T) {
		callCount := 0
		mockRepo := &mockAnalyticsRepository{
			getCashFlowAggregationFn: func(ctx context.Context, userID string, filters domain.CashFlowFilters) (*domain.CashFlowAggregation, error) {
				callCount++
				if callCount == 1 {
					// Current period
					return &domain.CashFlowAggregation{
						Income:       5000.00,
						Expenses:     3000.00,
						IncomeCount:  10,
						ExpenseCount: 25,
					}, nil
				}
				// Previous period
				return &domain.CashFlowAggregation{
					Income:       4000.00,
					Expenses:     3500.00,
					IncomeCount:  8,
					ExpenseCount: 20,
				}, nil
			},
		}

		service := NewAnalyticsService(mockRepo)

		filters := domain.CashFlowFilters{
			StartDate: time.Date(2024, 3, 1, 0, 0, 0, 0, loc),
			EndDate:   time.Date(2024, 3, 31, 0, 0, 0, 0, loc),
		}

		result, err := service.GetCashFlowSummary(ctx, "user-123", filters)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.Income != 5000.00 {
			t.Errorf("expected income 5000, got %f", result.Income)
		}
		if result.Expenses != 3000.00 {
			t.Errorf("expected expenses 3000, got %f", result.Expenses)
		}
		if result.NetCashFlow != 2000.00 {
			t.Errorf("expected net cash flow 2000, got %f", result.NetCashFlow)
		}
		if result.IncomeCount != 10 {
			t.Errorf("expected income count 10, got %d", result.IncomeCount)
		}
		if result.ExpenseCount != 25 {
			t.Errorf("expected expense count 25, got %d", result.ExpenseCount)
		}
		// #1631 Batch F1: StartDate/EndDate are DATE-shaped, so StartDateDisplay/EndDateDisplay
		// (the fields that actually serialize as start_date/end_date) must be populated at this
		// construction site, not left as an empty string.
		if result.StartDateDisplay != "2024-03-01" {
			t.Errorf("expected start_date_display 2024-03-01, got %q", result.StartDateDisplay)
		}
		if result.EndDateDisplay != "2024-03-31" {
			t.Errorf("expected end_date_display 2024-03-31, got %q", result.EndDateDisplay)
		}

		// Check period comparison
		if result.PeriodComparison == nil {
			t.Fatal("expected period comparison to be present")
		}
		if result.PeriodComparison.PreviousIncome != 4000.00 {
			t.Errorf("expected previous income 4000, got %f", result.PeriodComparison.PreviousIncome)
		}
		if result.PeriodComparison.IncomeChangeAmount != 1000.00 {
			t.Errorf("expected income change 1000, got %f", result.PeriodComparison.IncomeChangeAmount)
		}
		// 1000/4000 * 100 = 25%
		if result.PeriodComparison.IncomeChangePercent != 25.0 {
			t.Errorf("expected income change percent 25, got %f", result.PeriodComparison.IncomeChangePercent)
		}
	})

	t.Run("success without previous period data", func(t *testing.T) {
		callCount := 0
		mockRepo := &mockAnalyticsRepository{
			getCashFlowAggregationFn: func(ctx context.Context, userID string, filters domain.CashFlowFilters) (*domain.CashFlowAggregation, error) {
				callCount++
				if callCount == 1 {
					return &domain.CashFlowAggregation{
						Income:   1000.00,
						Expenses: 500.00,
					}, nil
				}
				// Previous period returns error
				return nil, errors.New("no data")
			},
		}

		service := NewAnalyticsService(mockRepo)

		filters := domain.CashFlowFilters{
			StartDate: time.Date(2024, 3, 1, 0, 0, 0, 0, loc),
			EndDate:   time.Date(2024, 3, 31, 0, 0, 0, 0, loc),
		}

		result, err := service.GetCashFlowSummary(ctx, "user-123", filters)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.PeriodComparison != nil {
			t.Error("expected no period comparison when previous period fails")
		}
	})

	t.Run("invalid filters", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{}
		service := NewAnalyticsService(mockRepo)

		filters := domain.CashFlowFilters{
			// Missing dates
		}

		_, err := service.GetCashFlowSummary(ctx, "user-123", filters)
		if err == nil {
			t.Error("expected error for invalid filters")
		}
	})

	t.Run("repository error", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getCashFlowAggregationFn: func(ctx context.Context, userID string, filters domain.CashFlowFilters) (*domain.CashFlowAggregation, error) {
				return nil, errors.New("database error")
			},
		}

		service := NewAnalyticsService(mockRepo)

		filters := domain.CashFlowFilters{
			StartDate: time.Date(2024, 3, 1, 0, 0, 0, 0, loc),
			EndDate:   time.Date(2024, 3, 31, 0, 0, 0, 0, loc),
		}

		_, err := service.GetCashFlowSummary(ctx, "user-123", filters)
		if err == nil {
			t.Error("expected error from repository")
		}
	})
}

// TestAnalyticsService_GetCashFlowSummary_HouseholdScopePropagatesToPreviousPeriod
// guards against Codex adversarial review finding P0-1: previousFilters (built for the
// period-comparison call) must inherit HouseholdScope from the current-period filters —
// otherwise the previous-period comparison would silently fall back to personal scope
// even when the current period is household-scoped. Issue #1369 — Phase 5.
func TestAnalyticsService_GetCashFlowSummary_HouseholdScopePropagatesToPreviousPeriod(t *testing.T) {
	ctx := context.Background()
	loc := time.UTC
	scope := &domain.HouseholdScope{HouseholdID: "hh-1", MemberUserIDs: []string{"owner-1", "member-2"}}

	var seenScopes []*domain.HouseholdScope
	mockRepo := &mockAnalyticsRepository{
		getCashFlowAggregationFn: func(ctx context.Context, userID string, filters domain.CashFlowFilters) (*domain.CashFlowAggregation, error) {
			seenScopes = append(seenScopes, filters.HouseholdScope)
			return &domain.CashFlowAggregation{Income: 100, Expenses: 50}, nil
		},
	}

	service := NewAnalyticsService(mockRepo)
	filters := domain.CashFlowFilters{
		StartDate:      time.Date(2024, 3, 1, 0, 0, 0, 0, loc),
		EndDate:        time.Date(2024, 3, 31, 0, 0, 0, 0, loc),
		HouseholdScope: scope,
	}

	_, err := service.GetCashFlowSummary(ctx, "owner-1", filters)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(seenScopes) != 2 {
		t.Fatalf("expected 2 repo calls (current + previous period), got %d", len(seenScopes))
	}
	if seenScopes[0] != scope {
		t.Errorf("current-period call must use the requested HouseholdScope")
	}
	if seenScopes[1] != scope {
		t.Errorf("previous-period call must inherit the SAME HouseholdScope, not fall back to personal scope")
	}
}

func TestAnalyticsService_GetCashFlowHistory(t *testing.T) {
	ctx := context.Background()
	loc := time.UTC

	t.Run("success with daily grouping", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getCashFlowGroupedFn: func(ctx context.Context, userID string, filters domain.CashFlowFilters) ([]domain.CashFlowGroupedData, error) {
				return []domain.CashFlowGroupedData{
					{PeriodStart: time.Date(2024, 3, 1, 0, 0, 0, 0, loc), Income: 100, Expenses: 50, TransactionCount: 5},
					{PeriodStart: time.Date(2024, 3, 2, 0, 0, 0, 0, loc), Income: 200, Expenses: 75, TransactionCount: 3},
					{PeriodStart: time.Date(2024, 3, 3, 0, 0, 0, 0, loc), Income: 0, Expenses: 100, TransactionCount: 2},
				}, nil
			},
		}

		service := NewAnalyticsService(mockRepo)

		filters := domain.CashFlowFilters{
			StartDate: time.Date(2024, 3, 1, 0, 0, 0, 0, loc),
			EndDate:   time.Date(2024, 3, 3, 0, 0, 0, 0, loc),
			Grouping:  "daily",
		}

		result, err := service.GetCashFlowHistory(ctx, "user-123", filters)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result.Periods) != 3 {
			t.Errorf("expected 3 periods, got %d", len(result.Periods))
		}
		if result.Grouping != "daily" {
			t.Errorf("expected grouping 'daily', got %s", result.Grouping)
		}

		// Check first period
		if result.Periods[0].Income != 100 {
			t.Errorf("expected income 100, got %f", result.Periods[0].Income)
		}
		if result.Periods[0].NetCashFlow != 50 { // 100 - 50
			t.Errorf("expected net 50, got %f", result.Periods[0].NetCashFlow)
		}
		// #1631 Batch F1: PeriodStart/PeriodEnd are DATE-shaped, so the Display siblings that
		// actually serialize as period_start/period_end must be populated in the conversion loop.
		if result.Periods[0].PeriodStartDisplay != "2024-03-01" {
			t.Errorf("expected period_start_display 2024-03-01, got %q", result.Periods[0].PeriodStartDisplay)
		}
		if result.Periods[0].PeriodEndDisplay == "" {
			t.Error("expected period_end_display to be populated, got empty string")
		}

		// Check third period (negative net)
		if result.Periods[2].NetCashFlow != -100 { // 0 - 100
			t.Errorf("expected net -100, got %f", result.Periods[2].NetCashFlow)
		}

		// #1631 Batch F1: the response's own StartDate/EndDate must reach the wire through
		// StartDateDisplay/EndDateDisplay too, not just the per-period fields above.
		if result.StartDateDisplay != "2024-03-01" {
			t.Errorf("expected start_date_display 2024-03-01, got %q", result.StartDateDisplay)
		}
		if result.EndDateDisplay != "2024-03-03" {
			t.Errorf("expected end_date_display 2024-03-03, got %q", result.EndDateDisplay)
		}
	})

	t.Run("auto-select grouping for long range", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getCashFlowGroupedFn: func(ctx context.Context, userID string, filters domain.CashFlowFilters) ([]domain.CashFlowGroupedData, error) {
				// Verify auto-selected grouping
				if filters.Grouping != "monthly" {
					t.Errorf("expected auto-selected grouping 'monthly', got %s", filters.Grouping)
				}
				return []domain.CashFlowGroupedData{}, nil
			},
		}

		service := NewAnalyticsService(mockRepo)

		filters := domain.CashFlowFilters{
			StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, loc),
			EndDate:   time.Date(2024, 12, 31, 0, 0, 0, 0, loc),
			// Grouping not specified - should auto-select
		}

		_, err := service.GetCashFlowHistory(ctx, "user-123", filters)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestAutoSelectGrouping(t *testing.T) {
	loc := time.UTC

	tests := []struct {
		name      string
		startDate time.Time
		endDate   time.Time
		expected  string
	}{
		{
			name:      "7 days - daily",
			startDate: time.Date(2024, 3, 1, 0, 0, 0, 0, loc),
			endDate:   time.Date(2024, 3, 7, 0, 0, 0, 0, loc),
			expected:  "daily",
		},
		{
			name:      "14 days - daily",
			startDate: time.Date(2024, 3, 1, 0, 0, 0, 0, loc),
			endDate:   time.Date(2024, 3, 14, 0, 0, 0, 0, loc),
			expected:  "daily",
		},
		{
			name:      "30 days - weekly",
			startDate: time.Date(2024, 3, 1, 0, 0, 0, 0, loc),
			endDate:   time.Date(2024, 3, 31, 0, 0, 0, 0, loc),
			expected:  "weekly",
		},
		{
			name:      "60 days - weekly",
			startDate: time.Date(2024, 1, 1, 0, 0, 0, 0, loc),
			endDate:   time.Date(2024, 3, 1, 0, 0, 0, 0, loc),
			expected:  "weekly",
		},
		{
			name:      "6 months - monthly",
			startDate: time.Date(2024, 1, 1, 0, 0, 0, 0, loc),
			endDate:   time.Date(2024, 7, 1, 0, 0, 0, 0, loc),
			expected:  "monthly",
		},
		{
			name:      "1 year - monthly",
			startDate: time.Date(2024, 1, 1, 0, 0, 0, 0, loc),
			endDate:   time.Date(2024, 12, 31, 0, 0, 0, 0, loc),
			expected:  "monthly",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := autoSelectGrouping(tt.startDate, tt.endDate)
			if result != tt.expected {
				t.Errorf("autoSelectGrouping() = %s, expected %s", result, tt.expected)
			}
		})
	}
}

func TestCalculatePeriodEnd(t *testing.T) {
	loc := time.UTC

	tests := []struct {
		name        string
		periodStart time.Time
		grouping    string
		expected    time.Time
	}{
		{
			name:        "daily",
			periodStart: time.Date(2024, 3, 15, 0, 0, 0, 0, loc),
			grouping:    "daily",
			expected:    time.Date(2024, 3, 15, 0, 0, 0, 0, loc),
		},
		{
			name:        "weekly",
			periodStart: time.Date(2024, 3, 11, 0, 0, 0, 0, loc), // Monday
			grouping:    "weekly",
			expected:    time.Date(2024, 3, 17, 0, 0, 0, 0, loc), // Sunday
		},
		{
			name:        "monthly - 31 days",
			periodStart: time.Date(2024, 3, 1, 0, 0, 0, 0, loc),
			grouping:    "monthly",
			expected:    time.Date(2024, 3, 31, 0, 0, 0, 0, loc),
		},
		{
			name:        "monthly - 28 days (Feb non-leap)",
			periodStart: time.Date(2023, 2, 1, 0, 0, 0, 0, loc),
			grouping:    "monthly",
			expected:    time.Date(2023, 2, 28, 0, 0, 0, 0, loc),
		},
		{
			name:        "monthly - 29 days (Feb leap)",
			periodStart: time.Date(2024, 2, 1, 0, 0, 0, 0, loc),
			grouping:    "monthly",
			expected:    time.Date(2024, 2, 29, 0, 0, 0, 0, loc),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculatePeriodEnd(tt.periodStart, tt.grouping)
			if !result.Equal(tt.expected) {
				t.Errorf("calculatePeriodEnd() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestAbsFloat(t *testing.T) {
	tests := []struct {
		input    float64
		expected float64
	}{
		{5.0, 5.0},
		{-5.0, 5.0},
		{0.0, 0.0},
		{-0.1, 0.1},
	}

	for _, tt := range tests {
		result := absFloat(tt.input)
		if result != tt.expected {
			t.Errorf("absFloat(%f) = %f, expected %f", tt.input, result, tt.expected)
		}
	}
}

// TestFormatCategoryName_UsesCuratedRegistry verifies analytics uses the curated PFC registry
// (via PFCPrimaryDisplayName) and outputs "Food And Drink" not "Food And Drink".
func TestFormatCategoryName_UsesCuratedRegistry(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"FOOD_AND_DRINK", "Food And Drink"}, // curated registry uses "And" not "&"
		{"TRANSPORTATION", "Transportation"},
		{"GENERAL_MERCHANDISE", "General Merchandise"},
		{"UNCATEGORIZED", "Uncategorized"},
		{"PERSONAL_CARE", "Personal Care"},
		{"HOME_IMPROVEMENT", "Home Improvement"},
		{"ENTERTAINMENT", "Entertainment"},
	}

	for _, tt := range tests {
		result := PFCPrimaryDisplayName(tt.input)
		if result != tt.expected {
			t.Errorf("PFCPrimaryDisplayName(%q) = %q, expected %q", tt.input, result, tt.expected)
		}
	}
}

func TestAnalyticsService_GetSpendingByCategory(t *testing.T) {
	ctx := context.Background()
	loc := time.UTC

	t.Run("returns categories with percentages and display names", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getSpendingByCategoryFn: func(ctx context.Context, userID string, filters domain.SpendingFilters) ([]domain.SpendingByCategory, error) {
				// PFCPrimaryDisplay is populated with the raw PFC enum, exactly like
				// effective_category_name does in production (effectiveCategoryDisplayExpr()) —
				// left empty here would make this fixture unable to tell the fixed
				// resolveCategoryDisplay path from the old stored-display-else-pfcDisplayName
				// bug, since an empty stored value falls through to the same lookup either way.
				return []domain.SpendingByCategory{
					{Category: "FOOD_AND_DRINK", PFCPrimaryDisplay: "FOOD_AND_DRINK", Total: 300.0, TransactionCount: 20},
					{Category: "TRANSPORTATION", PFCPrimaryDisplay: "TRANSPORTATION", Total: 100.0, TransactionCount: 8},
					{Category: "ENTERTAINMENT", PFCPrimaryDisplay: "ENTERTAINMENT", Total: 100.0, TransactionCount: 5},
				}, nil
			},
		}

		service := NewAnalyticsService(mockRepo)

		filters := domain.SpendingFilters{
			StartDate: time.Date(2024, 3, 1, 0, 0, 0, 0, loc),
			EndDate:   time.Date(2024, 3, 31, 0, 0, 0, 0, loc),
		}

		result, err := service.GetSpendingByCategory(ctx, "user-123", filters)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.TotalSpent != 500.0 {
			t.Errorf("expected total 500, got %f", result.TotalSpent)
		}
		if len(result.Categories) != 3 {
			t.Fatalf("expected 3 categories, got %d", len(result.Categories))
		}

		// First category: FOOD_AND_DRINK
		cat := result.Categories[0]
		if cat.Category != "FOOD_AND_DRINK" {
			t.Errorf("expected FOOD_AND_DRINK, got %s", cat.Category)
		}
		if cat.PFCPrimaryDisplay != "Food And Drink" {
			t.Errorf("expected 'Food And Drink', got %s", cat.PFCPrimaryDisplay)
		}
		// 300/500 * 100 = 60%
		if cat.Percentage != 60.0 {
			t.Errorf("expected 60%%, got %f", cat.Percentage)
		}
	})

	t.Run("repository error is returned", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getSpendingByCategoryFn: func(ctx context.Context, userID string, filters domain.SpendingFilters) ([]domain.SpendingByCategory, error) {
				return nil, errors.New("db error")
			},
		}

		service := NewAnalyticsService(mockRepo)

		filters := domain.SpendingFilters{
			StartDate: time.Date(2024, 3, 1, 0, 0, 0, 0, loc),
			EndDate:   time.Date(2024, 3, 31, 0, 0, 0, 0, loc),
		}

		_, err := service.GetSpendingByCategory(ctx, "user-123", filters)
		if err == nil {
			t.Error("expected error from repository")
		}
	})

	t.Run("returns empty categories when no data", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getSpendingByCategoryFn: func(ctx context.Context, userID string, filters domain.SpendingFilters) ([]domain.SpendingByCategory, error) {
				return []domain.SpendingByCategory{}, nil
			},
		}

		service := NewAnalyticsService(mockRepo)

		filters := domain.SpendingFilters{
			StartDate: time.Date(2024, 3, 1, 0, 0, 0, 0, loc),
			EndDate:   time.Date(2024, 3, 31, 0, 0, 0, 0, loc),
		}

		result, err := service.GetSpendingByCategory(ctx, "user-123", filters)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.TotalSpent != 0 {
			t.Errorf("expected total 0, got %f", result.TotalSpent)
		}
		if len(result.Categories) != 0 {
			t.Errorf("expected 0 categories, got %d", len(result.Categories))
		}
	})

	t.Run("primary enum gets PrimaryDisplayName", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getSpendingByCategoryFn: func(ctx context.Context, userID string, filters domain.SpendingFilters) ([]domain.SpendingByCategory, error) {
				// PFCPrimaryDisplay populated with the raw enum, as production always does —
				// left empty this cannot distinguish the fixed resolveCategoryDisplay path from
				// the old stored-display-else-pfcDisplayName bug (see fixture note above).
				return []domain.SpendingByCategory{
					{Category: "FOOD_AND_DRINK", PFCPrimaryDisplay: "FOOD_AND_DRINK", Total: 200.0, TransactionCount: 10},
				}, nil
			},
		}

		service := NewAnalyticsService(mockRepo)

		filters := domain.SpendingFilters{
			StartDate: time.Date(2024, 3, 1, 0, 0, 0, 0, loc),
			EndDate:   time.Date(2024, 3, 31, 0, 0, 0, 0, loc),
			// No Category filter — top-level, primary enums expected
		}

		result, err := service.GetSpendingByCategory(ctx, "user-123", filters)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Categories) != 1 {
			t.Fatalf("expected 1 category, got %d", len(result.Categories))
		}
		cat := result.Categories[0]
		// Primary enum should use PrimaryDisplayName: "Food And Drink"
		if cat.PFCPrimaryDisplay != "Food And Drink" {
			t.Errorf("primary enum: expected PFCPrimaryDisplay 'Food And Drink', got %q", cat.PFCPrimaryDisplay)
		}
	})

	t.Run("detailed enum drill-down gets DetailedDisplayName not PrimaryDisplayName", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getSpendingByCategoryFn: func(ctx context.Context, userID string, filters domain.SpendingFilters) ([]domain.SpendingByCategory, error) {
				// Drill-down: repository returns detailed-level enums. PFCPrimaryDisplay is set
				// to the PRIMARY enum ("FOOD_AND_DRINK"), matching what
				// effective_category_name actually stores for a PFC row in production (#1626)
				// — the key is the detailed enum but the stored display is always the primary.
				// Leaving this field empty would make the test unable to tell the fixed
				// resolveCategoryDisplay path from the old stored-display-else-pfcDisplayName
				// bug: an empty stored value falls through to pfcDisplayName either way, so
				// both implementations would produce "Fast Food" and the test would pass
				// against the broken code too.
				return []domain.SpendingByCategory{
					{Category: "FOOD_AND_DRINK_FAST_FOOD", PFCPrimaryDisplay: "FOOD_AND_DRINK", Total: 120.0, TransactionCount: 8},
					{Category: "FOOD_AND_DRINK_COFFEE", PFCPrimaryDisplay: "FOOD_AND_DRINK", Total: 80.0, TransactionCount: 5},
				}, nil
			},
		}

		service := NewAnalyticsService(mockRepo)

		filters := domain.SpendingFilters{
			StartDate: time.Date(2024, 3, 1, 0, 0, 0, 0, loc),
			EndDate:   time.Date(2024, 3, 31, 0, 0, 0, 0, loc),
			Category:  "FOOD_AND_DRINK", // drill-down filter
		}

		result, err := service.GetSpendingByCategory(ctx, "user-123", filters)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Categories) != 2 {
			t.Fatalf("expected 2 categories, got %d", len(result.Categories))
		}

		// Find FOOD_AND_DRINK_FAST_FOOD
		var fastFood *domain.SpendingByCategory
		for i := range result.Categories {
			if result.Categories[i].Category == "FOOD_AND_DRINK_FAST_FOOD" {
				fastFood = &result.Categories[i]
			}
		}
		if fastFood == nil {
			t.Fatal("FOOD_AND_DRINK_FAST_FOOD not found in results")
		}
		// Detailed enum should use DetailedDisplayName: "Fast Food", NOT the primary "Food And Drink"
		if fastFood.PFCPrimaryDisplay != "Fast Food" {
			t.Errorf("detailed enum: expected PFCPrimaryDisplay 'Fast Food', got %q", fastFood.PFCPrimaryDisplay)
		}

		// Find FOOD_AND_DRINK_COFFEE
		var coffee *domain.SpendingByCategory
		for i := range result.Categories {
			if result.Categories[i].Category == "FOOD_AND_DRINK_COFFEE" {
				coffee = &result.Categories[i]
			}
		}
		if coffee == nil {
			t.Fatal("FOOD_AND_DRINK_COFFEE not found in results")
		}
		// Detailed enum should use DetailedDisplayName: "Coffee", NOT the primary "Food And Drink"
		if coffee.PFCPrimaryDisplay != "Coffee" {
			t.Errorf("detailed enum: expected PFCPrimaryDisplay 'Coffee', got %q", coffee.PFCPrimaryDisplay)
		}
	})
}

func TestAnalyticsService_GetSpendingByMerchant(t *testing.T) {
	ctx := context.Background()
	loc := time.UTC

	t.Run("returns merchants with percentages", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getSpendingByMerchantFn: func(ctx context.Context, userID string, filters domain.SpendingFilters, limit int) ([]domain.SpendingByMerchant, error) {
				if limit != 10 {
					t.Errorf("expected limit 10, got %d", limit)
				}
				return []domain.SpendingByMerchant{
					{MerchantName: "Starbucks", Total: 150.0, TransactionCount: 15},
					{MerchantName: "Amazon", Total: 350.0, TransactionCount: 5},
				}, nil
			},
		}

		service := NewAnalyticsService(mockRepo)

		filters := domain.SpendingFilters{
			StartDate: time.Date(2024, 3, 1, 0, 0, 0, 0, loc),
			EndDate:   time.Date(2024, 3, 31, 0, 0, 0, 0, loc),
		}

		result, err := service.GetSpendingByMerchant(ctx, "user-123", filters, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result.Merchants) != 2 {
			t.Fatalf("expected 2 merchants, got %d", len(result.Merchants))
		}
		if result.TopN != 10 {
			t.Errorf("expected TopN 10, got %d", result.TopN)
		}

		// Amazon has total 350, Starbucks 150, total = 500
		// Amazon: 350/500*100 = 70%
		amazonIdx := -1
		for i, m := range result.Merchants {
			if m.MerchantName == "Amazon" {
				amazonIdx = i
			}
		}
		if amazonIdx == -1 {
			t.Fatal("Amazon not found in merchants")
		}
		if result.Merchants[amazonIdx].Percentage != 70.0 {
			t.Errorf("expected Amazon 70%%, got %f", result.Merchants[amazonIdx].Percentage)
		}
	})

	t.Run("repository error is returned", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getSpendingByMerchantFn: func(ctx context.Context, userID string, filters domain.SpendingFilters, limit int) ([]domain.SpendingByMerchant, error) {
				return nil, errors.New("db error")
			},
		}

		service := NewAnalyticsService(mockRepo)

		filters := domain.SpendingFilters{
			StartDate: time.Date(2024, 3, 1, 0, 0, 0, 0, loc),
			EndDate:   time.Date(2024, 3, 31, 0, 0, 0, 0, loc),
		}

		_, err := service.GetSpendingByMerchant(ctx, "user-123", filters, 10)
		if err == nil {
			t.Error("expected error from repository")
		}
	})
}

func TestAnalyticsService_GetSpendingTrends(t *testing.T) {
	ctx := context.Background()
	loc := time.UTC

	t.Run("groups rows into periods and identifies top categories", func(t *testing.T) {
		jan := time.Date(2024, 1, 1, 0, 0, 0, 0, loc)
		feb := time.Date(2024, 2, 1, 0, 0, 0, 0, loc)

		mockRepo := &mockAnalyticsRepository{
			getSpendingTrendsFn: func(ctx context.Context, userID string, filters domain.SpendingFilters) ([]domain.SpendingTrendRow, error) {
				return []domain.SpendingTrendRow{
					{PeriodStart: jan, Category: "FOOD_AND_DRINK", Total: 200.0},
					{PeriodStart: jan, Category: "TRANSPORTATION", Total: 80.0},
					{PeriodStart: feb, Category: "FOOD_AND_DRINK", Total: 230.0},
					{PeriodStart: feb, Category: "TRANSPORTATION", Total: 90.0},
				}, nil
			},
		}

		service := NewAnalyticsService(mockRepo)

		filters := domain.SpendingFilters{
			StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, loc),
			EndDate:   time.Date(2024, 2, 29, 0, 0, 0, 0, loc),
		}

		result, err := service.GetSpendingTrends(ctx, "user-123", filters)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result.Periods) != 2 {
			t.Fatalf("expected 2 periods, got %d", len(result.Periods))
		}

		// Check top categories identified (by total spend across all periods)
		// FOOD_AND_DRINK: 200+230=430, TRANSPORTATION: 80+90=170
		if len(result.TopCategories) == 0 {
			t.Error("expected top categories to be identified")
		}
		if result.TopCategories[0] != "FOOD_AND_DRINK" {
			t.Errorf("expected FOOD_AND_DRINK first, got %s", result.TopCategories[0])
		}

		// Check first period
		period := result.Periods[0]
		if period.PeriodStart != "2024-01-01" {
			t.Errorf("expected period start 2024-01-01, got %s", period.PeriodStart)
		}
		if period.Total != 280.0 { // 200+80
			t.Errorf("expected period total 280, got %f", period.Total)
		}
		if len(period.Categories) != 2 {
			t.Errorf("expected 2 categories in period, got %d", len(period.Categories))
		}
	})

	t.Run("buckets categories beyond top 5 as Other", func(t *testing.T) {
		jan := time.Date(2024, 1, 1, 0, 0, 0, 0, loc)

		mockRepo := &mockAnalyticsRepository{
			getSpendingTrendsFn: func(ctx context.Context, userID string, filters domain.SpendingFilters) ([]domain.SpendingTrendRow, error) {
				return []domain.SpendingTrendRow{
					{PeriodStart: jan, Category: "CAT_A", Total: 100.0},
					{PeriodStart: jan, Category: "CAT_B", Total: 90.0},
					{PeriodStart: jan, Category: "CAT_C", Total: 80.0},
					{PeriodStart: jan, Category: "CAT_D", Total: 70.0},
					{PeriodStart: jan, Category: "CAT_E", Total: 60.0},
					{PeriodStart: jan, Category: "CAT_F", Total: 50.0}, // Should become "Other"
					{PeriodStart: jan, Category: "CAT_G", Total: 40.0}, // Should become "Other"
				}, nil
			},
		}

		service := NewAnalyticsService(mockRepo)

		filters := domain.SpendingFilters{
			StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, loc),
			EndDate:   time.Date(2024, 1, 31, 0, 0, 0, 0, loc),
		}

		result, err := service.GetSpendingTrends(ctx, "user-123", filters)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// TopCategories should have exactly 5
		if len(result.TopCategories) != 5 {
			t.Errorf("expected 5 top categories, got %d", len(result.TopCategories))
		}

		// Period should have 6 entries: 5 top + Other
		period := result.Periods[0]
		if len(period.Categories) != 6 {
			t.Errorf("expected 6 categories (5 top + Other), got %d", len(period.Categories))
		}

		// Find "Other"
		var otherTotal float64
		for _, c := range period.Categories {
			if c.Category == "OTHER" {
				otherTotal = c.Total
			}
		}
		if otherTotal != 90.0 { // 50+40
			t.Errorf("expected Other total 90, got %f", otherTotal)
		}
	})

	t.Run("repository error is returned", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getSpendingTrendsFn: func(ctx context.Context, userID string, filters domain.SpendingFilters) ([]domain.SpendingTrendRow, error) {
				return nil, errors.New("db error")
			},
		}

		service := NewAnalyticsService(mockRepo)

		filters := domain.SpendingFilters{
			StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, loc),
			EndDate:   time.Date(2024, 1, 31, 0, 0, 0, 0, loc),
		}

		_, err := service.GetSpendingTrends(ctx, "user-123", filters)
		if err == nil {
			t.Error("expected error from repository")
		}
	})
}

func TestAnalyticsService_GetAverageMonthlySpend(t *testing.T) {
	ctx := context.Background()
	loc := time.UTC

	t.Run("happy path: total=300 over 3 months gives average=100", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getAverageMonthlySpendFn: func(ctx context.Context, userID string, filters domain.SpendingFilters) (*domain.AverageMonthlySpendRow, error) {
				return &domain.AverageMonthlySpendRow{Total: 300.0}, nil
			},
		}
		service := NewAnalyticsService(mockRepo)

		// ~91 days spans 3 months ceiling(91/30.4375)=3
		filters := domain.SpendingFilters{
			StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, loc),
			EndDate:   time.Date(2024, 3, 31, 0, 0, 0, 0, loc),
		}

		result, err := service.GetAverageMonthlySpend(ctx, "user-123", filters)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.TotalSpend != 300.0 {
			t.Errorf("expected total 300, got %f", result.TotalSpend)
		}
		if result.MonthCount != 3 {
			t.Errorf("expected 3 months, got %d", result.MonthCount)
		}
		if result.AverageMonthlySpend != 100.0 {
			t.Errorf("expected average 100, got %f", result.AverageMonthlySpend)
		}
		if result.StartDate != "2024-01-01" {
			t.Errorf("expected start_date 2024-01-01, got %s", result.StartDate)
		}
		if result.EndDate != "2024-03-31" {
			t.Errorf("expected end_date 2024-03-31, got %s", result.EndDate)
		}
	})

	t.Run("zero total returns average=0", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getAverageMonthlySpendFn: func(ctx context.Context, userID string, filters domain.SpendingFilters) (*domain.AverageMonthlySpendRow, error) {
				return &domain.AverageMonthlySpendRow{Total: 0.0}, nil
			},
		}
		service := NewAnalyticsService(mockRepo)

		filters := domain.SpendingFilters{
			StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, loc),
			EndDate:   time.Date(2024, 1, 31, 0, 0, 0, 0, loc),
		}

		result, err := service.GetAverageMonthlySpend(ctx, "user-123", filters)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.AverageMonthlySpend != 0.0 {
			t.Errorf("expected average 0, got %f", result.AverageMonthlySpend)
		}
		if result.TotalSpend != 0.0 {
			t.Errorf("expected total 0, got %f", result.TotalSpend)
		}
	})

	t.Run("single day range gives month_count=1", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getAverageMonthlySpendFn: func(ctx context.Context, userID string, filters domain.SpendingFilters) (*domain.AverageMonthlySpendRow, error) {
				return &domain.AverageMonthlySpendRow{Total: 50.0}, nil
			},
		}
		service := NewAnalyticsService(mockRepo)

		filters := domain.SpendingFilters{
			StartDate: time.Date(2024, 3, 15, 0, 0, 0, 0, loc),
			EndDate:   time.Date(2024, 3, 15, 0, 0, 0, 0, loc),
		}

		result, err := service.GetAverageMonthlySpend(ctx, "user-123", filters)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.MonthCount != 1 {
			t.Errorf("expected month_count=1 for same-day range, got %d", result.MonthCount)
		}
		if result.AverageMonthlySpend != 50.0 {
			t.Errorf("expected average 50, got %f", result.AverageMonthlySpend)
		}
	})

	t.Run("end_date before start_date returns error", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{}
		service := NewAnalyticsService(mockRepo)

		filters := domain.SpendingFilters{
			StartDate: time.Date(2024, 3, 31, 0, 0, 0, 0, loc),
			EndDate:   time.Date(2024, 3, 1, 0, 0, 0, 0, loc),
		}

		_, err := service.GetAverageMonthlySpend(ctx, "user-123", filters)
		if err == nil {
			t.Error("expected error when end_date is before start_date")
		}
	})

	t.Run("repository error is returned", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getAverageMonthlySpendFn: func(ctx context.Context, userID string, filters domain.SpendingFilters) (*domain.AverageMonthlySpendRow, error) {
				return nil, errors.New("db error")
			},
		}
		service := NewAnalyticsService(mockRepo)

		filters := domain.SpendingFilters{
			StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, loc),
			EndDate:   time.Date(2024, 3, 31, 0, 0, 0, 0, loc),
		}

		_, err := service.GetAverageMonthlySpend(ctx, "user-123", filters)
		if err == nil {
			t.Error("expected error from repository")
		}
	})
}

func TestAnalyticsService_GetSpendingByCategory_CustomCategory(t *testing.T) {
	ctx := context.Background()
	loc := time.UTC

	t.Run("custom category row uses server-resolved display name not pfcDisplayName", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getSpendingByCategoryFn: func(ctx context.Context, userID string, filters domain.SpendingFilters) ([]domain.SpendingByCategory, error) {
				// Repository resolves the UUID to the user's custom-category name
				return []domain.SpendingByCategory{
					{
						Category:          "custom:7a93a4f0-dead-beef-1234-abcdef012345",
						PFCPrimaryDisplay: "My Coffee Budget",
						Total:             150.0,
						TransactionCount:  5,
					},
				}, nil
			},
		}
		service := NewAnalyticsService(mockRepo)
		filters := domain.SpendingFilters{
			StartDate: time.Date(2024, 3, 1, 0, 0, 0, 0, loc),
			EndDate:   time.Date(2024, 3, 31, 0, 0, 0, 0, loc),
		}
		result, err := service.GetSpendingByCategory(ctx, "user-123", filters)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Categories) != 1 {
			t.Fatalf("expected 1 category, got %d", len(result.Categories))
		}
		cat := result.Categories[0]
		if cat.PFCPrimaryDisplay != "My Coffee Budget" {
			t.Errorf("expected PFCPrimaryDisplay 'My Coffee Budget', got %q (uuid must not be passed through pfcDisplayName)", cat.PFCPrimaryDisplay)
		}
		if cat.Category != "custom:7a93a4f0-dead-beef-1234-abcdef012345" {
			t.Errorf("expected raw category token preserved, got %q", cat.Category)
		}
	})

	t.Run("two custom categories with same display name remain separate rows", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getSpendingByCategoryFn: func(ctx context.Context, userID string, filters domain.SpendingFilters) ([]domain.SpendingByCategory, error) {
				return []domain.SpendingByCategory{
					{Category: "custom:uuid-aaa-111", PFCPrimaryDisplay: "Groceries", Total: 100.0, TransactionCount: 3},
					{Category: "custom:uuid-bbb-222", PFCPrimaryDisplay: "Groceries", Total: 80.0, TransactionCount: 2},
				}, nil
			},
		}
		service := NewAnalyticsService(mockRepo)
		filters := domain.SpendingFilters{
			StartDate: time.Date(2024, 3, 1, 0, 0, 0, 0, loc),
			EndDate:   time.Date(2024, 3, 31, 0, 0, 0, 0, loc),
		}
		result, err := service.GetSpendingByCategory(ctx, "user-123", filters)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Categories) != 2 {
			t.Fatalf("expected 2 separate rows (not merged despite same display name), got %d", len(result.Categories))
		}
	})
}

func TestAnalyticsService_GetSpendingTrends_CustomCategory(t *testing.T) {
	ctx := context.Background()
	loc := time.UTC

	t.Run("custom category trend row uses CategoryDisplay field from repository", func(t *testing.T) {
		jan := time.Date(2024, 1, 1, 0, 0, 0, 0, loc)
		mockRepo := &mockAnalyticsRepository{
			getSpendingTrendsFn: func(ctx context.Context, userID string, filters domain.SpendingFilters) ([]domain.SpendingTrendRow, error) {
				return []domain.SpendingTrendRow{
					{
						PeriodStart:     jan,
						Category:        "custom:uuid-aaa-111",
						CategoryDisplay: "My Coffee Budget",
						Total:           150.0,
					},
				}, nil
			},
		}
		service := NewAnalyticsService(mockRepo)
		filters := domain.SpendingFilters{
			StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, loc),
			EndDate:   time.Date(2024, 1, 31, 0, 0, 0, 0, loc),
		}
		result, err := service.GetSpendingTrends(ctx, "user-123", filters)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Periods) != 1 {
			t.Fatalf("expected 1 period, got %d", len(result.Periods))
		}
		cats := result.Periods[0].Categories
		if len(cats) != 1 {
			t.Fatalf("expected 1 category in period, got %d", len(cats))
		}
		if cats[0].PFCPrimaryDisplay != "My Coffee Budget" {
			t.Errorf("expected PFCPrimaryDisplay 'My Coffee Budget', got %q (uuid must not leak through pfcDisplayName)", cats[0].PFCPrimaryDisplay)
		}
	})
}

func TestAnalyticsService_GetSavingsRateForPeriod(t *testing.T) {
	ctx := context.Background()
	loc := time.UTC

	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, loc)
	endDate := time.Date(2024, 3, 31, 0, 0, 0, 0, loc)

	t.Run("happy path: income > expenses yields positive rate", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getSavingsRateForPeriodFn: func(ctx context.Context, userID string, s time.Time, e time.Time, scope *domain.HouseholdScope) (*domain.SavingsRateRow, error) {
				return &domain.SavingsRateRow{Income: 5000.0, Expenses: 3000.0}, nil
			},
		}
		service := NewAnalyticsService(mockRepo)

		result, err := service.GetSavingsRateForPeriod(ctx, "user-123", startDate, endDate, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if result.SavingsRate == nil {
			t.Fatal("expected non-nil savings_rate pointer")
		}
		expected := (5000.0 - 3000.0) / 5000.0 // 0.4
		if *result.SavingsRate != expected {
			t.Errorf("expected savings_rate %f, got %f", expected, *result.SavingsRate)
		}
		if result.Income != 5000.0 {
			t.Errorf("expected income 5000, got %f", result.Income)
		}
		if result.Expenses != 3000.0 {
			t.Errorf("expected expenses 3000, got %f", result.Expenses)
		}
		if result.NetSavings != 2000.0 {
			t.Errorf("expected net_savings 2000, got %f", result.NetSavings)
		}
		if result.StartDate != "2024-01-01" {
			t.Errorf("expected start_date 2024-01-01, got %s", result.StartDate)
		}
		if result.EndDate != "2024-03-31" {
			t.Errorf("expected end_date 2024-03-31, got %s", result.EndDate)
		}
	})

	t.Run("income=0 yields nil savings_rate pointer", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getSavingsRateForPeriodFn: func(ctx context.Context, userID string, s time.Time, e time.Time, scope *domain.HouseholdScope) (*domain.SavingsRateRow, error) {
				return &domain.SavingsRateRow{Income: 0.0, Expenses: 500.0}, nil
			},
		}
		service := NewAnalyticsService(mockRepo)

		result, err := service.GetSavingsRateForPeriod(ctx, "user-123", startDate, endDate, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.SavingsRate != nil {
			t.Errorf("expected nil savings_rate when income=0, got %f", *result.SavingsRate)
		}
	})

	t.Run("income < expenses yields negative savings rate", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getSavingsRateForPeriodFn: func(ctx context.Context, userID string, s time.Time, e time.Time, scope *domain.HouseholdScope) (*domain.SavingsRateRow, error) {
				return &domain.SavingsRateRow{Income: 2000.0, Expenses: 3000.0}, nil
			},
		}
		service := NewAnalyticsService(mockRepo)

		result, err := service.GetSavingsRateForPeriod(ctx, "user-123", startDate, endDate, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.SavingsRate == nil {
			t.Fatal("expected non-nil savings_rate")
		}
		if *result.SavingsRate >= 0 {
			t.Errorf("expected negative savings_rate, got %f", *result.SavingsRate)
		}
		if result.NetSavings != -1000.0 {
			t.Errorf("expected net_savings -1000, got %f", result.NetSavings)
		}
	})

	t.Run("end_date before start_date returns error", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{}
		service := NewAnalyticsService(mockRepo)

		_, err := service.GetSavingsRateForPeriod(ctx, "user-123", endDate, startDate, nil)
		if err == nil {
			t.Error("expected error when end_date before start_date")
		}
	})
}

func TestAnalyticsService_GetMonthOverMonthSpendingChange(t *testing.T) {
	ctx := context.Background()
	loc := time.UTC

	jan := time.Date(2024, 1, 1, 0, 0, 0, 0, loc)
	feb := time.Date(2024, 2, 1, 0, 0, 0, 0, loc)
	mar := time.Date(2024, 3, 1, 0, 0, 0, 0, loc)

	t.Run("months<2 returns error", func(t *testing.T) {
		service := NewAnalyticsService(&mockAnalyticsRepository{})
		_, err := service.GetMonthOverMonthSpendingChange(ctx, "user-123", "", 1, nil, false, nil)
		if err == nil {
			t.Error("expected error when months < 2")
		}
	})

	t.Run("months>24 returns error", func(t *testing.T) {
		service := NewAnalyticsService(&mockAnalyticsRepository{})
		_, err := service.GetMonthOverMonthSpendingChange(ctx, "user-123", "", 25, nil, false, nil)
		if err == nil {
			t.Error("expected error when months > 24")
		}
	})

	t.Run("happy path: first bucket has nil deltas, subsequent buckets have pct/abs", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getMonthOverMonthSpendingFn: func(ctx context.Context, userID string, startDate time.Time, category string, filters domain.SpendingFilters) ([]domain.MonthOverMonthBucketRow, error) {
				return []domain.MonthOverMonthBucketRow{
					{Month: jan, Total: 200.0},
					{Month: feb, Total: 300.0},
					{Month: mar, Total: 250.0},
				}, nil
			},
		}
		service := NewAnalyticsService(mockRepo)

		result, err := service.GetMonthOverMonthSpendingChange(ctx, "user-123", "", 3, nil, false, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Buckets) != 3 {
			t.Fatalf("expected 3 buckets, got %d", len(result.Buckets))
		}
		// First bucket: nil deltas
		if result.Buckets[0].ChangePct != nil {
			t.Error("expected nil ChangePct for first bucket")
		}
		if result.Buckets[0].ChangeAbs != nil {
			t.Error("expected nil ChangeAbs for first bucket")
		}
		// Second bucket: (300-200)/200 = 0.5 = +50%. change_pct is a PERCENTAGE (#1699 MCP-4),
		// matching its *_change_percent siblings, so 50.0 — not the 0.5 fraction.
		if result.Buckets[1].ChangePct == nil {
			t.Fatal("expected non-nil ChangePct for second bucket")
		}
		if *result.Buckets[1].ChangePct != 50.0 {
			t.Errorf("expected ChangePct 50.0, got %f", *result.Buckets[1].ChangePct)
		}
		if result.Buckets[1].ChangeAbs == nil {
			t.Fatal("expected non-nil ChangeAbs for second bucket")
		}
		if *result.Buckets[1].ChangeAbs != 100.0 {
			t.Errorf("expected ChangeAbs 100, got %f", *result.Buckets[1].ChangeAbs)
		}
		// Third bucket: 250/300 - 1 = -16.67% change
		if result.Buckets[2].ChangePct == nil {
			t.Fatal("expected non-nil ChangePct for third bucket")
		}
		if result.MonthCount != 3 {
			t.Errorf("expected month_count 3, got %d", result.MonthCount)
		}
	})

	t.Run("zero previous total leaves change_pct nil but sets change_abs", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getMonthOverMonthSpendingFn: func(ctx context.Context, userID string, startDate time.Time, category string, filters domain.SpendingFilters) ([]domain.MonthOverMonthBucketRow, error) {
				return []domain.MonthOverMonthBucketRow{
					{Month: jan, Total: 0},
					{Month: feb, Total: 150},
				}, nil
			},
		}
		service := NewAnalyticsService(mockRepo)

		result, err := service.GetMonthOverMonthSpendingChange(ctx, "user-123", "", 3, nil, false, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Buckets[1].ChangePct != nil {
			t.Errorf("expected nil ChangePct when previous total is zero, got %f", *result.Buckets[1].ChangePct)
		}
		if result.Buckets[1].ChangeAbs == nil || *result.Buckets[1].ChangeAbs != 150.0 {
			t.Errorf("expected ChangeAbs 150 when previous total is zero, got %v", result.Buckets[1].ChangeAbs)
		}
	})

	t.Run("empty repo result returns non-nil response with empty buckets", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getMonthOverMonthSpendingFn: func(ctx context.Context, userID string, startDate time.Time, category string, filters domain.SpendingFilters) ([]domain.MonthOverMonthBucketRow, error) {
				return nil, nil
			},
		}
		service := NewAnalyticsService(mockRepo)

		result, err := service.GetMonthOverMonthSpendingChange(ctx, "user-123", "", 6, nil, false, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if result.Buckets == nil {
			t.Error("expected non-nil (empty) buckets slice")
		}
	})

	t.Run("threads accountTypes to repo", func(t *testing.T) {
		var capturedFilters domain.SpendingFilters
		mockRepo := &mockAnalyticsRepository{
			getMonthOverMonthSpendingFn: func(ctx context.Context, userID string, startDate time.Time, category string, filters domain.SpendingFilters) ([]domain.MonthOverMonthBucketRow, error) {
				capturedFilters = filters
				return nil, nil
			},
		}
		service := NewAnalyticsService(mockRepo)
		_, err := service.GetMonthOverMonthSpendingChange(ctx, "user-123", "", 6, []string{"depository"}, false, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(capturedFilters.AccountTypes) != 1 || capturedFilters.AccountTypes[0] != "depository" {
			t.Errorf("expected account type depository to be threaded to repo, got %v", capturedFilters.AccountTypes)
		}
	})
}

func TestAnalyticsService_GetLargestTransactionsForPeriod(t *testing.T) {
	ctx := context.Background()
	loc := time.UTC

	filters := domain.SpendingFilters{
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, loc),
		EndDate:   time.Date(2024, 3, 31, 0, 0, 0, 0, loc),
	}
	txDate := time.Date(2024, 2, 15, 0, 0, 0, 0, loc)

	t.Run("happy path: display names resolved via PFCPrimaryDisplayName", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getLargestTransactionsForPeriodFn: func(ctx context.Context, userID string, f domain.SpendingFilters, limit int) ([]domain.LargestTransactionRow, error) {
				return []domain.LargestTransactionRow{
					{
						TransactionID: "tx-001",
						MerchantName:  "Amazon",
						Amount:        500.0,
						Category:      "SHOPPING",
						Date:          txDate,
						AccountID:     "acc-001",
					},
				}, nil
			},
		}
		service := NewAnalyticsService(mockRepo)

		result, err := service.GetLargestTransactionsForPeriod(ctx, "user-123", filters, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Transactions) != 1 {
			t.Fatalf("expected 1 transaction, got %d", len(result.Transactions))
		}
		tx := result.Transactions[0]
		if tx.TransactionID != "tx-001" {
			t.Errorf("expected tx-001, got %s", tx.TransactionID)
		}
		if tx.PFCPrimaryDisplay == "" {
			t.Error("expected non-empty PFCPrimaryDisplay")
		}
		if tx.Date != "2024-02-15" {
			t.Errorf("expected date 2024-02-15, got %s", tx.Date)
		}
		if result.Limit != 10 {
			t.Errorf("expected limit 10, got %d", result.Limit)
		}
	})

	t.Run("empty result returns non-nil response with empty slice", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getLargestTransactionsForPeriodFn: func(ctx context.Context, userID string, f domain.SpendingFilters, limit int) ([]domain.LargestTransactionRow, error) {
				return []domain.LargestTransactionRow{}, nil
			},
		}
		service := NewAnalyticsService(mockRepo)

		result, err := service.GetLargestTransactionsForPeriod(ctx, "user-123", filters, 5)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Transactions == nil {
			t.Error("expected non-nil transactions slice")
		}
	})

	t.Run("repository error is propagated", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getLargestTransactionsForPeriodFn: func(ctx context.Context, userID string, f domain.SpendingFilters, limit int) ([]domain.LargestTransactionRow, error) {
				return nil, errors.New("db error")
			},
		}
		service := NewAnalyticsService(mockRepo)

		_, err := service.GetLargestTransactionsForPeriod(ctx, "user-123", filters, 10)
		if err == nil {
			t.Error("expected error from repository")
		}
	})

	t.Run("limit is passed through to repository", func(t *testing.T) {
		var capturedLimit int
		mockRepo := &mockAnalyticsRepository{
			getLargestTransactionsForPeriodFn: func(ctx context.Context, userID string, f domain.SpendingFilters, limit int) ([]domain.LargestTransactionRow, error) {
				capturedLimit = limit
				return []domain.LargestTransactionRow{}, nil
			},
		}
		service := NewAnalyticsService(mockRepo)

		_, err := service.GetLargestTransactionsForPeriod(ctx, "user-123", filters, 25)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if capturedLimit != 25 {
			t.Errorf("expected limit 25 passed to repo, got %d", capturedLimit)
		}
	})
}

func TestAnalyticsService_GetCategoryBreakdownForMerchant(t *testing.T) {
	ctx := context.Background()
	loc := time.UTC
	merchantID := "550e8400-e29b-41d4-a716-446655440000"

	filters := domain.SpendingFilters{
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, loc),
		EndDate:   time.Date(2024, 3, 31, 0, 0, 0, 0, loc),
	}

	t.Run("happy path: percentages computed correctly", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getCategoryBreakdownForMerchantFn: func(ctx context.Context, userID string, mID string, f domain.SpendingFilters) ([]domain.CategoryBreakdownForMerchantRow, error) {
				return []domain.CategoryBreakdownForMerchantRow{
					{Category: "SHOPPING", Total: 400.0, Count: 5},
					{Category: "GENERAL_MERCHANDISE", Total: 100.0, Count: 2},
				}, nil
			},
		}
		service := NewAnalyticsService(mockRepo)

		result, err := service.GetCategoryBreakdownForMerchant(ctx, "user-123", merchantID, filters)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Categories) != 2 {
			t.Fatalf("expected 2 categories, got %d", len(result.Categories))
		}
		if result.TotalSpend != 500.0 {
			t.Errorf("expected total_spend 500, got %f", result.TotalSpend)
		}
		// SHOPPING: 400/500 = 80%
		if result.Categories[0].Percentage != 80.0 {
			t.Errorf("expected SHOPPING percentage 80, got %f", result.Categories[0].Percentage)
		}
		if result.Categories[0].PFCPrimaryDisplay == "" {
			t.Error("expected non-empty PFCPrimaryDisplay for SHOPPING")
		}
		if result.MerchantName != merchantID {
			t.Errorf("expected merchant_name %s, got %s", merchantID, result.MerchantName)
		}
	})

	// ── #1626 Phase 2, item 10: PHASE 1 CAUSED THIS ──────────────────────────────────────────
	//
	// Phase 1 added t.effective_category_name to this read's GROUP BY so a custom category could
	// be NAMED instead of showing a UUID. It did not add the merge the income and expense paths
	// already had. That column is denormalised per transaction and refreshed ASYNCHRONOUSLY after
	// a rename, so mid-refresh ONE custom category yields TWO rows — one per name — and the fix
	// for a cosmetic bug became a correctness bug: the category appears twice, its spend split,
	// and both percentages halved.
	//
	// ⚠ Merging alone is not sufficient, which is why this fixture has a COMPETING category. Two
	// merged 60s outrank an unmerged 100, so percentages must come from the MERGED total and the
	// list must be RE-SORTED after merging. A two-row single-key fixture cannot expose either
	// failure — it would pass against a merge that forgot both.
	t.Run("a mid-rename custom category is ONE row, correctly ranked", func(t *testing.T) {
		const catKey = "custom:11111111-1111-1111-1111-111111111111"
		mockRepo := &mockAnalyticsRepository{
			getCategoryBreakdownForMerchantFn: func(ctx context.Context, userID string, mID string, f domain.SpendingFilters) ([]domain.CategoryBreakdownForMerchantRow, error) {
				// The SQL orders by total DESC, so the competing category leads on arrival: each
				// half of the split category is smaller than it.
				return []domain.CategoryBreakdownForMerchantRow{
					{Category: "SHOPPING", CategoryDisplay: "SHOPPING", Total: 100.0, Count: 1},
					{Category: catKey, CategoryDisplay: "Coffee runs", Total: 60.0, Count: 3},
					{Category: catKey, CategoryDisplay: "Coffee", Total: 60.0, Count: 2},
				}, nil
			},
		}
		service := NewAnalyticsService(mockRepo)

		result, err := service.GetCategoryBreakdownForMerchant(ctx, "user-123", merchantID, filters)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result.Categories) != 2 {
			t.Fatalf("expected the two names to merge into ONE category (2 total), got %d: %+v",
				len(result.Categories), result.Categories)
		}

		// Ranking: 120 merged beats 100, so the custom category must lead. This is the assertion a
		// merge without a re-sort fails — it would leave SHOPPING first, in pre-merge order.
		if result.Categories[0].Category != catKey {
			t.Errorf("expected the merged category (120) to outrank SHOPPING (100), got %q first",
				result.Categories[0].Category)
		}
		if result.Categories[0].Total != 120.0 {
			t.Errorf("expected merged total 120, got %f", result.Categories[0].Total)
		}
		if result.Categories[0].TransactionCount != 5 {
			t.Errorf("expected merged count 5, got %d", result.Categories[0].TransactionCount)
		}

		// Percentages from the MERGED total: 120/220. Computed pre-merge they would be 27.27 each,
		// which sums past 100 across the two halves and understates the real share.
		wantPct := 120.0 / 220.0 * 100
		if diff := result.Categories[0].Percentage - wantPct; diff > 0.001 || diff < -0.001 {
			t.Errorf("expected merged percentage %f, got %f", wantPct, result.Categories[0].Percentage)
		}

		// The name resolves to one of the two the rows carried — never to a UUID, which is the
		// bug #1626 exists to fix and which a merge could reintroduce by blanking the display.
		if d := result.Categories[0].PFCPrimaryDisplay; d != "Coffee runs" && d != "Coffee" {
			t.Errorf("expected a real name, got %q", d)
		}
	})

	t.Run("empty result returns non-nil response with empty categories", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getCategoryBreakdownForMerchantFn: func(ctx context.Context, userID string, mID string, f domain.SpendingFilters) ([]domain.CategoryBreakdownForMerchantRow, error) {
				return nil, nil
			},
		}
		service := NewAnalyticsService(mockRepo)

		result, err := service.GetCategoryBreakdownForMerchant(ctx, "user-123", merchantID, filters)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Categories == nil {
			t.Error("expected non-nil categories slice")
		}
	})

	t.Run("repository error is propagated", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getCategoryBreakdownForMerchantFn: func(ctx context.Context, userID string, mID string, f domain.SpendingFilters) ([]domain.CategoryBreakdownForMerchantRow, error) {
				return nil, errors.New("db error")
			},
		}
		service := NewAnalyticsService(mockRepo)

		_, err := service.GetCategoryBreakdownForMerchant(ctx, "user-123", merchantID, filters)
		if err == nil {
			t.Error("expected error from repository")
		}
	})
}

func TestAnalyticsService_GetIncomeSourceBreakdown(t *testing.T) {
	ctx := context.Background()
	loc := time.UTC

	filters := domain.SpendingFilters{
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, loc),
		EndDate:   time.Date(2024, 3, 31, 0, 0, 0, 0, loc),
	}

	t.Run("happy path: percentages from ABS totals", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getIncomeSourceBreakdownFn: func(ctx context.Context, userID string, f domain.SpendingFilters) ([]domain.IncomeSourceRow, error) {
				return []domain.IncomeSourceRow{
					{Category: "INCOME", Total: 5000.0, Count: 3},
					{Category: "INCOME_DIVIDENDS", Total: 1000.0, Count: 1},
				}, nil
			},
		}
		service := NewAnalyticsService(mockRepo)

		result, err := service.GetIncomeSourceBreakdown(ctx, "user-123", filters)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Sources) != 2 {
			t.Fatalf("expected 2 sources, got %d", len(result.Sources))
		}
		if result.TotalIncome != 6000.0 {
			t.Errorf("expected total_income 6000, got %f", result.TotalIncome)
		}
		// INCOME: 5000/6000 = 83.33%
		if result.Sources[0].Percentage <= 0 {
			t.Errorf("expected positive percentage for INCOME, got %f", result.Sources[0].Percentage)
		}
		if result.Sources[0].PFCPrimaryDisplay == "" {
			t.Error("expected non-empty PFCPrimaryDisplay for INCOME")
		}
		if result.StartDate != "2024-01-01" {
			t.Errorf("expected start_date 2024-01-01, got %s", result.StartDate)
		}
	})

	t.Run("empty result returns non-nil response with empty sources", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getIncomeSourceBreakdownFn: func(ctx context.Context, userID string, f domain.SpendingFilters) ([]domain.IncomeSourceRow, error) {
				return nil, nil
			},
		}
		service := NewAnalyticsService(mockRepo)

		result, err := service.GetIncomeSourceBreakdown(ctx, "user-123", filters)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Sources == nil {
			t.Error("expected non-nil sources slice")
		}
	})

	t.Run("repository error is propagated", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getIncomeSourceBreakdownFn: func(ctx context.Context, userID string, f domain.SpendingFilters) ([]domain.IncomeSourceRow, error) {
				return nil, errors.New("db error")
			},
		}
		service := NewAnalyticsService(mockRepo)

		_, err := service.GetIncomeSourceBreakdown(ctx, "user-123", filters)
		if err == nil {
			t.Error("expected error from repository")
		}
	})
}

func TestAnalyticsService_GetRecurringSubscriptions(t *testing.T) {
	ctx := context.Background()

	lastCharge := time.Date(2024, 12, 1, 0, 0, 0, 0, time.UTC)
	nextExpected := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	t.Run("happy path: two subscriptions mapped to response items", func(t *testing.T) {
		var capturedLookback time.Time
		mockRepo := &mockAnalyticsRepository{
			getRecurringSubscriptionsFn: func(ctx context.Context, userID string, lookbackStart time.Time) ([]domain.RecurringSubscriptionRow, error) {
				capturedLookback = lookbackStart
				return []domain.RecurringSubscriptionRow{
					{
						MerchantName:     "Netflix",
						Amount:           15.99,
						OccurrenceCount:  12,
						AvgGapDays:       30.5,
						StddevGapDays:    1.2,
						LastChargeDate:   lastCharge,
						NextExpectedDate: nextExpected,
					},
					{
						MerchantName:     "Spotify",
						Amount:           9.99,
						OccurrenceCount:  12,
						AvgGapDays:       29.8,
						StddevGapDays:    0.9,
						LastChargeDate:   lastCharge,
						NextExpectedDate: nextExpected,
					},
				}, nil
			},
		}
		service := NewAnalyticsService(mockRepo)

		result, err := service.GetRecurringSubscriptions(ctx, "user-123")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if len(result.Subscriptions) != 2 {
			t.Fatalf("expected 2 subscriptions, got %d", len(result.Subscriptions))
		}
		if result.Subscriptions[0].MerchantName != "Netflix" {
			t.Errorf("expected first merchant Netflix, got %q", result.Subscriptions[0].MerchantName)
		}
		if result.Subscriptions[0].Amount != 15.99 {
			t.Errorf("expected amount 15.99, got %f", result.Subscriptions[0].Amount)
		}
		if result.Subscriptions[0].OccurrenceCount != 12 {
			t.Errorf("expected occurrence_count 12, got %d", result.Subscriptions[0].OccurrenceCount)
		}
		if result.Subscriptions[0].LastChargeDate != "2024-12-01" {
			t.Errorf("expected last_charge_date 2024-12-01, got %q", result.Subscriptions[0].LastChargeDate)
		}
		if result.Subscriptions[0].NextExpectedDate != "2025-01-01" {
			t.Errorf("expected next_expected_date 2025-01-01, got %q", result.Subscriptions[0].NextExpectedDate)
		}
		if result.AsOfDate == "" {
			t.Error("expected non-empty as_of_date")
		}
		if result.Subscriptions[1].MerchantName != "Spotify" {
			t.Errorf("expected second merchant Spotify, got %q", result.Subscriptions[1].MerchantName)
		}
		// Verify the service passes a ~12-month lookback to the repository.
		expectedLookback := time.Now().AddDate(-1, 0, 0)
		if capturedLookback.IsZero() {
			t.Error("expected lookbackStart to be passed to repository")
		} else if diff := expectedLookback.Sub(capturedLookback); diff < -24*time.Hour || diff > 24*time.Hour {
			t.Errorf("expected lookbackStart to be ~12 months ago, got %v (diff: %v)", capturedLookback, diff)
		}
	})

	t.Run("empty: no subscriptions returns empty slice not nil", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getRecurringSubscriptionsFn: func(ctx context.Context, userID string, lookbackStart time.Time) ([]domain.RecurringSubscriptionRow, error) {
				return nil, nil
			},
		}
		service := NewAnalyticsService(mockRepo)

		result, err := service.GetRecurringSubscriptions(ctx, "user-123")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if result.Subscriptions == nil {
			t.Error("expected empty (not nil) subscriptions slice")
		}
		if len(result.Subscriptions) != 0 {
			t.Errorf("expected 0 subscriptions, got %d", len(result.Subscriptions))
		}
	})

	t.Run("repository error is propagated", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getRecurringSubscriptionsFn: func(ctx context.Context, userID string, lookbackStart time.Time) ([]domain.RecurringSubscriptionRow, error) {
				return nil, errors.New("db error")
			},
		}
		service := NewAnalyticsService(mockRepo)

		_, err := service.GetRecurringSubscriptions(ctx, "user-123")
		if err == nil {
			t.Error("expected error from repository")
		}
	})
}

func TestAnalyticsService_GetUnusualSpend(t *testing.T) {
	ctx := context.Background()
	loc := time.UTC

	start := time.Date(2024, 4, 1, 0, 0, 0, 0, loc)
	end := time.Date(2024, 4, 30, 0, 0, 0, 0, loc)
	txDate := time.Date(2024, 4, 15, 0, 0, 0, 0, loc)

	t.Run("happy path: outlier returned with correct fields", func(t *testing.T) {
		var capturedBaseline time.Time
		var capturedSigma float64
		mockRepo := &mockAnalyticsRepository{
			getUnusualSpendFn: func(ctx context.Context, userID string, baselineStart, startDate, endDate time.Time, sigmaThreshold float64) ([]domain.UnusualSpendRow, error) {
				capturedBaseline = baselineStart
				capturedSigma = sigmaThreshold
				return []domain.UnusualSpendRow{
					{
						TransactionID: "tx-outlier",
						MerchantName:  "Fancy Restaurant",
						Amount:        350.0,
						Category:      "FOOD_AND_DRINK",
						Date:          txDate,
						BaselineAvg:   50.0,
						SigmaAbove:    3.5,
					},
				}, nil
			},
		}
		service := NewAnalyticsService(mockRepo)

		result, err := service.GetUnusualSpend(ctx, "user-123", start, end)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if len(result.Items) != 1 {
			t.Fatalf("expected 1 item, got %d", len(result.Items))
		}
		item := result.Items[0]
		if item.TransactionID != "tx-outlier" {
			t.Errorf("expected transaction_id tx-outlier, got %s", item.TransactionID)
		}
		if item.MerchantName != "Fancy Restaurant" {
			t.Errorf("expected merchant_name Fancy Restaurant, got %s", item.MerchantName)
		}
		if item.Amount != 350.0 {
			t.Errorf("expected amount 350, got %f", item.Amount)
		}
		if item.Category != "FOOD_AND_DRINK" {
			t.Errorf("expected category FOOD_AND_DRINK, got %s", item.Category)
		}
		if item.PFCPrimaryDisplay == "" {
			t.Error("expected non-empty pfc_primary_display")
		}
		if item.Date != "2024-04-15" {
			t.Errorf("expected date 2024-04-15, got %s", item.Date)
		}
		if item.BaselineAvg != 50.0 {
			t.Errorf("expected baseline_avg 50, got %f", item.BaselineAvg)
		}
		if item.SigmaAbove != 3.5 {
			t.Errorf("expected sigma_above 3.5, got %f", item.SigmaAbove)
		}
		if result.SigmaThreshold != 2.0 {
			t.Errorf("expected sigma_threshold 2.0, got %f", result.SigmaThreshold)
		}
		// Service should compute baseline 3 months before start
		expectedBaseline := start.AddDate(0, -3, 0)
		if !capturedBaseline.Equal(expectedBaseline) {
			t.Errorf("expected baseline %v, got %v", expectedBaseline, capturedBaseline)
		}
		if capturedSigma != 2.0 {
			t.Errorf("expected sigma 2.0 passed to repo, got %f", capturedSigma)
		}
	})

	t.Run("end_date before start_date returns error", func(t *testing.T) {
		service := NewAnalyticsService(&mockAnalyticsRepository{})
		_, err := service.GetUnusualSpend(ctx, "user-123", end, start)
		if err == nil {
			t.Error("expected error when end_date before start_date")
		}
	})

	t.Run("empty result returns empty items slice not nil", func(t *testing.T) {
		service := NewAnalyticsService(&mockAnalyticsRepository{})
		result, err := service.GetUnusualSpend(ctx, "user-123", start, end)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Items == nil {
			t.Error("expected non-nil items slice")
		}
	})

	t.Run("repository error is propagated", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getUnusualSpendFn: func(ctx context.Context, userID string, baselineStart, startDate, endDate time.Time, sigmaThreshold float64) ([]domain.UnusualSpendRow, error) {
				return nil, errors.New("db error")
			},
		}
		service := NewAnalyticsService(mockRepo)
		_, err := service.GetUnusualSpend(ctx, "user-123", start, end)
		if err == nil {
			t.Error("expected error from repository")
		}
	})
}

// mockBalanceSnapshotRepository implements the minimal interface needed for testing GetNetWorthTrend.
type mockBalanceSnapshotRepository struct {
	getAggregatedHistoryFn             func(ctx context.Context, userID string, startDate, endDate time.Time, accountTypes []string) ([]domain.BalanceHistoryPoint, error)
	getAggregatedHistoryForHouseholdFn func(ctx context.Context, requestingUserID string, scope *domain.HouseholdScope, startDate, endDate time.Time, accountTypes []string) ([]domain.BalanceHistoryPoint, error)
}

func (m *mockBalanceSnapshotRepository) Create(ctx context.Context, snapshot *domain.BalanceSnapshot) error {
	return nil
}
func (m *mockBalanceSnapshotRepository) GetAggregatedHistory(ctx context.Context, userID string, startDate, endDate time.Time, accountTypes []string) ([]domain.BalanceHistoryPoint, error) {
	if m.getAggregatedHistoryFn != nil {
		return m.getAggregatedHistoryFn(ctx, userID, startDate, endDate, accountTypes)
	}
	return nil, nil
}
func (m *mockBalanceSnapshotRepository) GetSnapshotsAsOfForUser(ctx context.Context, userID string, asOf time.Time) ([]domain.BalanceSnapshot, error) {
	return nil, nil
}
func (m *mockBalanceSnapshotRepository) GetLatestByUserID(ctx context.Context, userID string) ([]domain.BalanceSnapshot, error) {
	return nil, nil
}
func (m *mockBalanceSnapshotRepository) GetLatestByAccountID(ctx context.Context, accountID string) (*domain.BalanceSnapshot, error) {
	return nil, nil
}

// The per-account retained balance history (#1569) is a HISTORICAL read and no analytics or
// dashboard path may reach it — those all answer current-state or aggregate questions. Panics
// rather than returning nil so a future edit that routes one through here fails loudly.
func (m *mockBalanceSnapshotRepository) GetAccountBalanceHistory(context.Context, string, string, *time.Time, *time.Time) ([]domain.AccountBalanceHistoryPoint, error) {
	panic("analytics/dashboard must not read per-account retained balance history")
}

// The two #1626 Phase 3 gap-filler methods are WRITE/PLANNING operations belonging to the periodic
// balance-capture job, and nothing in analytics or the dashboard may reach them — analytics reads;
// it does not decide what to snapshot. Panics for the same reason the method above does.
func (m *mockBalanceSnapshotRepository) CreateDailyCapture(context.Context, *domain.BalanceSnapshot) (bool, error) {
	panic("analytics/dashboard must not write balance snapshots")
}
func (m *mockBalanceSnapshotRepository) AccountsMissingDailySnapshot(context.Context, string, time.Time) ([]string, error) {
	panic("analytics/dashboard must not ask the daily-capture cohort question")
}
func (m *mockBalanceSnapshotRepository) GetSnapshotsAsOfForHousehold(ctx context.Context, requestingUserID string, scope *domain.HouseholdScope, asOf time.Time) ([]domain.BalanceSnapshot, error) {
	return nil, nil
}
func (m *mockBalanceSnapshotRepository) GetAggregatedHistoryForHousehold(ctx context.Context, requestingUserID string, scope *domain.HouseholdScope, startDate, endDate time.Time, accountTypes []string) ([]domain.BalanceHistoryPoint, error) {
	if m.getAggregatedHistoryForHouseholdFn != nil {
		return m.getAggregatedHistoryForHouseholdFn(ctx, requestingUserID, scope, startDate, endDate, accountTypes)
	}
	return nil, nil
}

func TestAnalyticsService_GetNetWorthTrend(t *testing.T) {
	ctx := context.Background()
	loc := time.UTC

	t.Run("nil balanceSnapshotRepo returns error", func(t *testing.T) {
		service := NewAnalyticsService(&mockAnalyticsRepository{})
		// No SetBalanceSnapshotRepo call — repo is nil
		_, err := service.GetNetWorthTrend(ctx, "user-123", "monthly", nil)
		if err == nil {
			t.Error("expected error when balanceSnapshotRepo is nil")
		}
	})

	t.Run("invalid grouping returns error", func(t *testing.T) {
		service := NewAnalyticsService(&mockAnalyticsRepository{})
		service.SetBalanceSnapshotRepo(&mockBalanceSnapshotRepository{})
		_, err := service.GetNetWorthTrend(ctx, "user-123", "quarterly", nil)
		if err == nil {
			t.Error("expected error for invalid grouping")
		}
	})

	t.Run("3 daily points grouped monthly → 1 bucket", func(t *testing.T) {
		day1 := time.Date(2024, 3, 10, 0, 0, 0, 0, loc)
		day2 := time.Date(2024, 3, 20, 0, 0, 0, 0, loc)
		day3 := time.Date(2024, 3, 25, 0, 0, 0, 0, loc)

		mockBalRepo := &mockBalanceSnapshotRepository{
			getAggregatedHistoryFn: func(ctx context.Context, userID string, startDate, endDate time.Time, accountTypes []string) ([]domain.BalanceHistoryPoint, error) {
				return []domain.BalanceHistoryPoint{
					{Date: day1, TotalBalance: 10000.0},
					{Date: day2, TotalBalance: 10500.0},
					{Date: day3, TotalBalance: 11000.0},
				}, nil
			},
		}

		service := NewAnalyticsService(&mockAnalyticsRepository{})
		service.SetBalanceSnapshotRepo(mockBalRepo)

		result, err := service.GetNetWorthTrend(ctx, "user-123", "monthly", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		// 3 days all in March → 1 monthly bucket
		if len(result.Buckets) != 1 {
			t.Fatalf("expected 1 bucket for monthly grouping of 3 March points, got %d", len(result.Buckets))
		}
		bucket := result.Buckets[0]
		// Last daily value in March = 11000
		if bucket.NetWorth != 11000.0 {
			t.Errorf("expected net_worth 11000 (last daily value), got %f", bucket.NetWorth)
		}
		// First bucket: no change
		if bucket.ChangeAbs != nil {
			t.Error("expected nil change_abs for first bucket")
		}
		if bucket.ChangePct != nil {
			t.Error("expected nil change_pct for first bucket")
		}
		if result.Grouping != "monthly" {
			t.Errorf("expected grouping monthly, got %s", result.Grouping)
		}
	})

	t.Run("two monthly buckets have change_abs and change_pct", func(t *testing.T) {
		day1 := time.Date(2024, 2, 28, 0, 0, 0, 0, loc)
		day2 := time.Date(2024, 3, 31, 0, 0, 0, 0, loc)

		mockBalRepo := &mockBalanceSnapshotRepository{
			getAggregatedHistoryFn: func(ctx context.Context, userID string, startDate, endDate time.Time, accountTypes []string) ([]domain.BalanceHistoryPoint, error) {
				return []domain.BalanceHistoryPoint{
					{Date: day1, TotalBalance: 10000.0},
					{Date: day2, TotalBalance: 12000.0},
				}, nil
			},
		}

		service := NewAnalyticsService(&mockAnalyticsRepository{})
		service.SetBalanceSnapshotRepo(mockBalRepo)

		result, err := service.GetNetWorthTrend(ctx, "user-123", "monthly", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Buckets) != 2 {
			t.Fatalf("expected 2 buckets, got %d", len(result.Buckets))
		}
		b1 := result.Buckets[0]
		b2 := result.Buckets[1]
		if b1.ChangeAbs != nil || b1.ChangePct != nil {
			t.Error("expected nil changes for first bucket")
		}
		if b2.ChangeAbs == nil {
			t.Fatal("expected non-nil change_abs for second bucket")
		}
		if *b2.ChangeAbs != 2000.0 {
			t.Errorf("expected change_abs 2000, got %f", *b2.ChangeAbs)
		}
		if b2.ChangePct == nil {
			t.Fatal("expected non-nil change_pct for second bucket")
		}
		// 2000/10000 = 0.2 = +20%. change_pct is a PERCENTAGE (#1699 MCP-4), so 20.0.
		if *b2.ChangePct != 20.0 {
			t.Errorf("expected change_pct 20.0, got %f", *b2.ChangePct)
		}
	})

	t.Run("zero previous net worth leaves change_pct nil but sets change_abs", func(t *testing.T) {
		day1 := time.Date(2024, 2, 28, 0, 0, 0, 0, loc)
		day2 := time.Date(2024, 3, 31, 0, 0, 0, 0, loc)

		mockBalRepo := &mockBalanceSnapshotRepository{
			getAggregatedHistoryFn: func(ctx context.Context, userID string, startDate, endDate time.Time, accountTypes []string) ([]domain.BalanceHistoryPoint, error) {
				return []domain.BalanceHistoryPoint{
					{Date: day1, TotalBalance: 0.0},
					{Date: day2, TotalBalance: 5000.0},
				}, nil
			},
		}

		service := NewAnalyticsService(&mockAnalyticsRepository{})
		service.SetBalanceSnapshotRepo(mockBalRepo)

		result, err := service.GetNetWorthTrend(ctx, "user-123", "monthly", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		b2 := result.Buckets[1]
		if b2.ChangePct != nil {
			t.Errorf("expected nil change_pct when previous net worth is zero, got %f", *b2.ChangePct)
		}
		if b2.ChangeAbs == nil || *b2.ChangeAbs != 5000.0 {
			t.Errorf("expected change_abs 5000 when previous net worth is zero, got %v", b2.ChangeAbs)
		}
	})

	t.Run("repository error is propagated", func(t *testing.T) {
		mockBalRepo := &mockBalanceSnapshotRepository{
			getAggregatedHistoryFn: func(ctx context.Context, userID string, startDate, endDate time.Time, accountTypes []string) ([]domain.BalanceHistoryPoint, error) {
				return nil, errors.New("db error")
			},
		}
		service := NewAnalyticsService(&mockAnalyticsRepository{})
		service.SetBalanceSnapshotRepo(mockBalRepo)

		_, err := service.GetNetWorthTrend(ctx, "user-123", "monthly", nil)
		if err == nil {
			t.Error("expected error from repository")
		}
	})

	// #1369 — Phase 5: household scope must use GetAggregatedHistoryForHousehold, not the
	// personal GetAggregatedHistory (which would silently drop other members' data).
	t.Run("household scope uses GetAggregatedHistoryForHousehold", func(t *testing.T) {
		day1 := time.Date(2024, 3, 10, 0, 0, 0, 0, loc)
		scope := &domain.HouseholdScope{HouseholdID: "hh-1", MemberUserIDs: []string{"user-123", "member-2"}}
		var capturedScope *domain.HouseholdScope

		mockBalRepo := &mockBalanceSnapshotRepository{
			getAggregatedHistoryFn: func(ctx context.Context, userID string, startDate, endDate time.Time, accountTypes []string) ([]domain.BalanceHistoryPoint, error) {
				t.Fatal("personal GetAggregatedHistory must not be called when scope is household")
				return nil, nil
			},
			getAggregatedHistoryForHouseholdFn: func(ctx context.Context, requestingUserID string, s *domain.HouseholdScope, startDate, endDate time.Time, accountTypes []string) ([]domain.BalanceHistoryPoint, error) {
				capturedScope = s
				return []domain.BalanceHistoryPoint{{Date: day1, TotalBalance: 15000.0}}, nil
			},
		}
		service := NewAnalyticsService(&mockAnalyticsRepository{})
		service.SetBalanceSnapshotRepo(mockBalRepo)

		result, err := service.GetNetWorthTrend(ctx, "user-123", "monthly", scope)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if capturedScope != scope {
			t.Error("expected household scope to be passed to GetAggregatedHistoryForHousehold")
		}
	})
}

func TestAnalyticsService_GetBudgetVsActual(t *testing.T) {
	ctx := context.Background()

	t.Run("happy path: computed fields correct", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getBudgetVsActualFn: func(ctx context.Context, userID, period string) ([]domain.BudgetVsActualRow, error) {
				return []domain.BudgetVsActualRow{
					{BudgetID: "bgt-1", BudgetName: "Groceries", Period: "monthly", AllocatedAmount: 500.0, SpentAmount: 600.0},
					{BudgetID: "bgt-2", BudgetName: "Transport", Period: "monthly", AllocatedAmount: 200.0, SpentAmount: 150.0},
				}, nil
			},
		}
		service := NewAnalyticsService(mockRepo)

		result, err := service.GetBudgetVsActual(ctx, "user-123", "monthly")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Budgets) != 2 {
			t.Fatalf("expected 2 budgets, got %d", len(result.Budgets))
		}
		// Groceries: over budget
		g := result.Budgets[0]
		if g.BudgetName != "Groceries" {
			t.Errorf("expected Groceries first, got %s", g.BudgetName)
		}
		if !g.OverBudget {
			t.Error("expected Groceries to be over budget")
		}
		if g.Remaining != -100.0 {
			t.Errorf("expected Remaining -100, got %f", g.Remaining)
		}
		if g.PctUsed != 120.0 {
			t.Errorf("expected PctUsed 120, got %f", g.PctUsed)
		}
		// Transport: under budget
		tr := result.Budgets[1]
		if tr.OverBudget {
			t.Error("expected Transport to be under budget")
		}
		if tr.Remaining != 50.0 {
			t.Errorf("expected Remaining 50, got %f", tr.Remaining)
		}
		if tr.PctUsed != 75.0 {
			t.Errorf("expected PctUsed 75, got %f", tr.PctUsed)
		}
		// Totals
		if result.TotalAllocated != 700.0 {
			t.Errorf("expected total_allocated 700, got %f", result.TotalAllocated)
		}
		if result.TotalSpent != 750.0 {
			t.Errorf("expected total_spent 750, got %f", result.TotalSpent)
		}
		if result.TotalRemaining != -50.0 {
			t.Errorf("expected total_remaining -50, got %f", result.TotalRemaining)
		}
		if result.Period != "monthly" {
			t.Errorf("expected period monthly, got %s", result.Period)
		}
	})

	t.Run("allocated_amount=0 yields pct_used=0 not division error", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getBudgetVsActualFn: func(ctx context.Context, userID, period string) ([]domain.BudgetVsActualRow, error) {
				return []domain.BudgetVsActualRow{
					{BudgetID: "bgt-1", BudgetName: "Empty", Period: "monthly", AllocatedAmount: 0, SpentAmount: 10.0},
				}, nil
			},
		}
		service := NewAnalyticsService(mockRepo)
		result, err := service.GetBudgetVsActual(ctx, "user-123", "monthly")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Budgets[0].PctUsed != 0.0 {
			t.Errorf("expected pct_used 0 when allocated=0, got %f", result.Budgets[0].PctUsed)
		}
	})

	t.Run("empty result returns non-nil response with empty budgets", func(t *testing.T) {
		service := NewAnalyticsService(&mockAnalyticsRepository{})
		result, err := service.GetBudgetVsActual(ctx, "user-123", "yearly")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Budgets == nil {
			t.Error("expected non-nil budgets slice")
		}
	})

	t.Run("invalid period returns error", func(t *testing.T) {
		service := NewAnalyticsService(&mockAnalyticsRepository{})
		_, err := service.GetBudgetVsActual(ctx, "user-123", "biannual")
		if err == nil {
			t.Error("expected error for invalid period")
		}
	})

	t.Run("repository error is propagated", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getBudgetVsActualFn: func(ctx context.Context, userID, period string) ([]domain.BudgetVsActualRow, error) {
				return nil, errors.New("db error")
			},
		}
		service := NewAnalyticsService(mockRepo)
		_, err := service.GetBudgetVsActual(ctx, "user-123", "monthly")
		if err == nil {
			t.Error("expected error from repository")
		}
	})
}

// TestAnalyticsService_GetSpendingByCategory_MidRefreshNameSurvives is #1626 in mild form, found by
// the branch review.
//
// The SQL groups by (category, category_display), and effective_category_name is denormalised per
// transaction and refreshed ASYNCHRONOUSLY after a rename — so mid-refresh one custom category
// arrives as TWO rows, one carrying the name and one carrying NULL (which the repository scans as
// ""). This read's hand-rolled merge added the totals and never revisited the survivor's display.
// The SQL orders total DESC, so when the UN-named half is the larger one it wins the survivor slot
// and the response says "Custom category" while the real name sat in the very next row — the UUID
// defect one step removed. Both sibling reads (income sources, merchant breakdown) already guard
// this, and mergeCategoryRows exists "so the two directions cannot drift apart". This was the drift.
//
// ⚠ The un-named half is deliberately the LARGER one; with the order reversed the test passes
// against the unfixed code, because first-seen would already be the named row.
func TestAnalyticsService_GetSpendingByCategory_MidRefreshNameSurvives(t *testing.T) {
	ctx := context.Background()
	const catKey = "custom:22222222-2222-2222-2222-222222222222"

	mockRepo := &mockAnalyticsRepository{
		getSpendingByCategoryFn: func(ctx context.Context, userID string, filters domain.SpendingFilters) ([]domain.SpendingByCategory, error) {
			return []domain.SpendingByCategory{
				// A competing category, so a merge that forgets to re-sort or to recompute
				// percentages from the merged total is also caught.
				{Category: "SHOPPING", PFCPrimaryDisplay: "SHOPPING", Total: 100.0, TransactionCount: 1},
				{Category: catKey, PFCPrimaryDisplay: "", Total: 70.0, TransactionCount: 4},
				{Category: catKey, PFCPrimaryDisplay: "Therapy sessions", Total: 50.0, TransactionCount: 2},
			}, nil
		},
	}
	service := NewAnalyticsService(mockRepo)
	result, err := service.GetSpendingByCategory(ctx, "user-123", domain.SpendingFilters{
		StartDate: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	require.Len(t, result.Categories, 2, "the two names must merge into ONE category")

	assert.Equal(t, catKey, result.Categories[0].Category,
		"the merged 120 must outrank SHOPPING's 100 — a merge without a re-sort leaves SHOPPING first")
	assert.Equal(t, "Therapy sessions", result.Categories[0].PFCPrimaryDisplay,
		"the survivor arrived UN-named and the real name was in the next row. Keeping the placeholder "+
			"shows the user \"Custom category\" for a category that has a name — the #1626 defect one "+
			"step removed")
	assert.InDelta(t, 120.0, result.Categories[0].Total, 0.0001)
	assert.Equal(t, 6, result.Categories[0].TransactionCount)
	assert.InDelta(t, 120.0/220.0*100, result.Categories[0].Percentage, 0.001,
		"percentages must come from the MERGED total")
}

// ─── #1626 Phase 3, Defect 1: the cash-flow response must not reach into the future ─────────
//
// With "this month" selected on 2026-09-09 the Cash Flow chart showed weekly buckets labelled
// "Week of Sep 13 / Sep 20 / Sep 27" — all entirely in the future, with Net Cash Flow flat at
// exactly 0 — because the requested end date (the 30th) was taken verbatim and the repository
// zero-filled every period up to it. The clamp lives HERE, at the service boundary, as a single
// effectiveEnd threaded into the repository call, rather than buried in the fill loop.

func todayUTCForTest() time.Time {
	n := time.Now().UTC()
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
}

func TestGetCashFlowHistory_ClampsAFutureEndDateToToday(t *testing.T) {
	today := todayUTCForTest()
	var seen domain.CashFlowFilters
	repo := &mockAnalyticsRepository{
		getCashFlowGroupedFn: func(_ context.Context, _ string, filters domain.CashFlowFilters) ([]domain.CashFlowGroupedData, error) {
			seen = filters
			return []domain.CashFlowGroupedData{
				{PeriodStart: today, Income: 10, Expenses: 4, TransactionCount: 2},
			}, nil
		},
	}
	svc := NewAnalyticsService(repo)

	resp, err := svc.GetCashFlowHistory(context.Background(), "u1", domain.CashFlowFilters{
		StartDate: today.AddDate(0, 0, -2),
		EndDate:   today.AddDate(0, 0, 21),
		Grouping:  "daily",
	})
	require.NoError(t, err)

	// The clamp is threaded into the repository call: the same value bounds the query and the
	// gap-filling, so neither can fabricate a bucket the other would not.
	assert.True(t, seen.EndDate.Equal(today), "repository received end date %v, expected today %v", seen.EndDate, today)
	assert.True(t, seen.StartDate.Equal(today.AddDate(0, 0, -2)), "the start date must not move")

	// The response reports the range it actually answered for.
	assert.True(t, resp.EndDate.Equal(today), "response end_date %v should be clamped to %v", resp.EndDate, today)
	for _, p := range resp.Periods {
		assert.False(t, p.PeriodStart.After(today), "period starts in the future: %v", p.PeriodStart)
		assert.False(t, p.PeriodEnd.After(today), "period_end reaches into the future: %v", p.PeriodEnd)
	}
}

// The grouping describes the range the USER asked for, so it is chosen before the clamp:
// clamping first would silently re-bucket a three-month request as weekly or daily.
func TestGetCashFlowHistory_ClampDoesNotChangeTheAutoSelectedGrouping(t *testing.T) {
	today := todayUTCForTest()
	repo := &mockAnalyticsRepository{
		getCashFlowGroupedFn: func(_ context.Context, _ string, _ domain.CashFlowFilters) ([]domain.CashFlowGroupedData, error) {
			return nil, nil
		},
	}
	svc := NewAnalyticsService(repo)

	resp, err := svc.GetCashFlowHistory(context.Background(), "u1", domain.CashFlowFilters{
		StartDate: today.AddDate(0, 0, -2),
		EndDate:   today.AddDate(0, 0, 21), // 23 days requested → weekly
	})
	require.NoError(t, err)
	assert.Equal(t, "weekly", resp.Grouping)
}

// A weekly bucket that STARTS in the past but whose calendar week has not finished must not
// advertise an end date in the future.
func TestGetCashFlowHistory_ClampsTheLastPeriodEnd(t *testing.T) {
	today := todayUTCForTest()
	weekday := int(today.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	monday := today.AddDate(0, 0, -(weekday - 1))

	repo := &mockAnalyticsRepository{
		getCashFlowGroupedFn: func(_ context.Context, _ string, _ domain.CashFlowFilters) ([]domain.CashFlowGroupedData, error) {
			return []domain.CashFlowGroupedData{
				{PeriodStart: monday, Income: 500, Expenses: 120, TransactionCount: 9},
			}, nil
		},
	}
	svc := NewAnalyticsService(repo)

	resp, err := svc.GetCashFlowHistory(context.Background(), "u1", domain.CashFlowFilters{
		StartDate: monday,
		EndDate:   today.AddDate(0, 0, 30),
		Grouping:  "weekly",
	})
	require.NoError(t, err)
	require.Len(t, resp.Periods, 1)
	assert.True(t, resp.Periods[0].PeriodEnd.Equal(today),
		"period_end %v should be clamped to today %v", resp.Periods[0].PeriodEnd, today)
	assert.Equal(t, 500.0-120.0, resp.Periods[0].NetCashFlow)
}

// The clamp must not delete data. Transactions are bucketed on t.date — a zoneless calendar
// date that is effectively institution-local — so a user ahead of UTC legitimately has rows
// dated later than the UTC calendar day. Whatever the repository returns is reported.
func TestGetCashFlowHistory_KeepsARealPeriodDatedBeyondTheClamp(t *testing.T) {
	today := todayUTCForTest()
	tomorrow := today.AddDate(0, 0, 1)
	repo := &mockAnalyticsRepository{
		getCashFlowGroupedFn: func(_ context.Context, _ string, _ domain.CashFlowFilters) ([]domain.CashFlowGroupedData, error) {
			return []domain.CashFlowGroupedData{
				{PeriodStart: today, Income: 0, Expenses: 20, TransactionCount: 1},
				{PeriodStart: tomorrow, Income: 0, Expenses: 33, TransactionCount: 1},
			}, nil
		},
	}
	svc := NewAnalyticsService(repo)

	resp, err := svc.GetCashFlowHistory(context.Background(), "u1", domain.CashFlowFilters{
		StartDate: today.AddDate(0, 0, -1),
		EndDate:   today.AddDate(0, 0, 21),
		Grouping:  "daily",
	})
	require.NoError(t, err)

	require.Len(t, resp.Periods, 2, "a period the repository returned was dropped: %+v", resp.Periods)
	beyond := resp.Periods[1]
	assert.True(t, beyond.PeriodStart.Equal(tomorrow))
	assert.Equal(t, 33.0, beyond.Expenses)
	// Its own period_end is never clamped BEFORE its start — that would be a nonsense range.
	assert.False(t, beyond.PeriodEnd.Before(beyond.PeriodStart))
	// And the response's end_date widens to cover it rather than contradicting the data.
	assert.False(t, resp.EndDate.Before(beyond.PeriodStart),
		"end_date %v excludes a period the response contains (%v)", resp.EndDate, beyond.PeriodStart)
}

// A range lying entirely in the future answers for nothing at all — no fabricated "current
// period", which is what truncateDate's backward move used to produce.
func TestGetCashFlowHistory_EntirelyFutureRangeReturnsNoPeriods(t *testing.T) {
	today := todayUTCForTest()
	repo := &mockAnalyticsRepository{
		getCashFlowGroupedFn: func(_ context.Context, _ string, filters domain.CashFlowFilters) ([]domain.CashFlowGroupedData, error) {
			// Mirror the repository's own behaviour for the clamped bounds it is handed.
			return fillMissingPeriodsForTest(filters), nil
		},
	}
	svc := NewAnalyticsService(repo)

	resp, err := svc.GetCashFlowHistory(context.Background(), "u1", domain.CashFlowFilters{
		StartDate: today.AddDate(0, 0, 3),
		EndDate:   today.AddDate(0, 0, 10),
		Grouping:  "weekly",
	})
	require.NoError(t, err)
	assert.Empty(t, resp.Periods)
}

// fillMissingPeriodsForTest is the repository's guard in miniature: nothing is generated when
// the (already clamped) end lies before the start.
func fillMissingPeriodsForTest(filters domain.CashFlowFilters) []domain.CashFlowGroupedData {
	if filters.StartDate.After(filters.EndDate) {
		return nil
	}
	return []domain.CashFlowGroupedData{{PeriodStart: filters.StartDate}}
}

// ─── #1626 final whole-branch review, F2: each endpoint labels the window it MEASURED ─────────
//
// ⚠ THIS FAMILY REPLACES THE X7 ONE, AND REVERSES ITS RULING. X7 observed that for a
// future-ranged request the two cash-flow endpoints echoed different ranges for the same
// question, and fixed it by clamping the SUMMARY'S ECHO to today while leaving its QUERY on the
// requested range. That made the endpoint inconsistent WITH ITSELF: the label said "through
// today", the number covered rows past today, and for an entirely-future range responseEndDate
// floored the echo to StartDate so the response claimed [start, start] while the aggregate
// covered [start, requestedEnd]. Before X7 each endpoint was at least internally coherent and
// they disagreed only on the LABEL — which the code called cosmetic; the fix is what made it
// non-cosmetic.
//
// THE RULING NOW: a range and the numbers described by it must be the same range, per endpoint.
//
//   - The SUMMARY fabricates nothing — one aggregate over rows that exist — so it queries the
//     requested range and echoes the requested range. It must NOT clamp the query:
//     transactions.date is a zoneless, effectively institution-local calendar date, so a user
//     ahead of UTC legitimately has rows dated later than the UTC day (the same fact
//     TestGetCashFlowHistory_KeepsARealPeriodDatedBeyondTheClamp rests on), and clamping would
//     delete those rows from their own total. That is real data loss to fix a label.
//   - The HISTORY endpoint gap-FILLS, so an unclamped future end fabricates a zero-valued bucket
//     per unhappened period. It keeps its clamp on both the query and the echo, and DISCLOSES the
//     difference in requested_end_date rather than leaving a caller to guess why the two
//     endpoints' windows differ.
//
// So the two endpoints still agree about the range they were ASKED for, and each is honest about
// the range it ANSWERED for. What is gone is the third state: a label describing a window nobody
// measured.

// TestGetCashFlowSummary_EchoesTheWindowItActuallyQueried asserts the property by COMPARING the
// echo with the filters the repository was handed, not by restating the expected date — so the
// label and the measurement cannot drift apart without this failing, whichever of them moves.
func TestGetCashFlowSummary_EchoesTheWindowItActuallyQueried(t *testing.T) {
	today := todayUTCForTest()
	start := today.AddDate(0, 0, -2)
	requestedEnd := today.AddDate(0, 0, 21)

	var seen domain.CashFlowFilters
	repo := &mockAnalyticsRepository{
		getCashFlowAggregationFn: func(_ context.Context, _ string, filters domain.CashFlowFilters) (*domain.CashFlowAggregation, error) {
			if filters.StartDate.Equal(start) {
				seen = filters
			}
			return &domain.CashFlowAggregation{Income: 900, Expenses: 250, IncomeCount: 3, ExpenseCount: 7}, nil
		},
	}
	svc := NewAnalyticsService(repo)

	resp, err := svc.GetCashFlowSummary(context.Background(), "u1", domain.CashFlowFilters{
		StartDate: start,
		EndDate:   requestedEnd,
	})
	require.NoError(t, err)

	assert.True(t, seen.EndDate.Equal(requestedEnd),
		"the QUERY bound must stay on the requested end (%v, got %v): transactions.date is "+
			"institution-local, so clamping it would drop rows a user ahead of UTC really has",
		requestedEnd, seen.EndDate)
	assert.True(t, resp.EndDate.Equal(seen.EndDate),
		"summary end_date %v must be the window it MEASURED (%v). Clamping the echo alone made "+
			"this endpoint label a range it did not query — its number covered rows past today "+
			"while its label said 'through today'",
		resp.EndDate, seen.EndDate)
	assert.True(t, resp.StartDate.Equal(seen.StartDate), "and the start likewise")
}

// TestGetCashFlowSummary_LeavesAPastRangeAlone is the narrow half: nothing may rewrite the echoed
// end for a request that does not reach past today, which is nearly all of them — so a bug here
// cannot be dismissed as "only affects future ranges".
func TestGetCashFlowSummary_LeavesAPastRangeAlone(t *testing.T) {
	today := todayUTCForTest()
	start := today.AddDate(0, 0, -40)
	end := today.AddDate(0, 0, -9)

	repo := &mockAnalyticsRepository{
		getCashFlowAggregationFn: func(_ context.Context, _ string, _ domain.CashFlowFilters) (*domain.CashFlowAggregation, error) {
			return &domain.CashFlowAggregation{Income: 100, Expenses: 40}, nil
		},
	}
	resp, err := NewAnalyticsService(repo).GetCashFlowSummary(context.Background(), "u1", domain.CashFlowFilters{
		StartDate: start,
		EndDate:   end,
	})
	require.NoError(t, err)
	assert.True(t, resp.EndDate.Equal(end), "a wholly-past end date must be echoed verbatim")
}

// TestGetCashFlowSummary_EntirelyFutureRange_TheTwoEndpointsAgreeAboutWhatWasASKED pins the
// awkward boundary rather than only the easy middle: when the whole requested range lies ahead,
// history's clamp alone would produce an end BEFORE its start, and responseEndDate floors it to
// the start. That is the one case where the two endpoints' ANSWERED windows genuinely differ, so
// it is the case where the disclosure has to work.
//
// Asserted by running both and comparing, never by restating an expected value, so a change to
// the rule cannot leave this test agreeing with only one of them.
func TestGetCashFlowSummary_EntirelyFutureRange_TheTwoEndpointsAgreeAboutWhatWasASKED(t *testing.T) {
	today := todayUTCForTest()
	filters := domain.CashFlowFilters{
		StartDate: today.AddDate(0, 0, 3),
		EndDate:   today.AddDate(0, 0, 10),
		Grouping:  "weekly",
	}

	var summarySeen domain.CashFlowFilters
	repo := &mockAnalyticsRepository{
		getCashFlowAggregationFn: func(_ context.Context, _ string, f domain.CashFlowFilters) (*domain.CashFlowAggregation, error) {
			if f.StartDate.Equal(filters.StartDate) {
				summarySeen = f
			}
			return &domain.CashFlowAggregation{}, nil
		},
		getCashFlowGroupedFn: func(_ context.Context, _ string, f domain.CashFlowFilters) ([]domain.CashFlowGroupedData, error) {
			return fillMissingPeriodsForTest(f), nil
		},
	}
	svc := NewAnalyticsService(repo)

	summary, err := svc.GetCashFlowSummary(context.Background(), "u1", filters)
	require.NoError(t, err)
	history, err := svc.GetCashFlowHistory(context.Background(), "u1", filters)
	require.NoError(t, err)

	assert.True(t, summary.StartDate.Equal(history.StartDate),
		"summary start %v vs history start %v", summary.StartDate, history.StartDate)

	// They agree about the REQUEST.
	assert.True(t, history.RequestedEndDate.Equal(filters.EndDate),
		"history must echo the caller's own end date in requested_end_date: %v vs %v",
		history.RequestedEndDate, filters.EndDate)
	assert.True(t, summary.EndDate.Equal(history.RequestedEndDate),
		"the two endpoints must agree about the range they were ASKED for: summary end_date %v "+
			"vs history requested_end_date %v", summary.EndDate, history.RequestedEndDate)

	// And each is honest about what it ANSWERED. History answered nothing here — its whole range
	// is unhappened — so its end_date floors to the start and it ships no periods.
	assert.True(t, summary.EndDate.Equal(summarySeen.EndDate),
		"summary still labels the window it measured, even when that window is entirely ahead: "+
			"echoed %v, queried %v", summary.EndDate, summarySeen.EndDate)
	assert.True(t, history.EndDate.Equal(filters.StartDate),
		"history answered for no period at all, so its end_date floors to the start: got %v",
		history.EndDate)
	assert.Empty(t, history.Periods)
	assert.False(t, history.EndDate.After(history.RequestedEndDate),
		"the answered window can never exceed the requested one")
	assert.False(t, summary.EndDate.Before(summary.StartDate),
		"and no echoed range may be inverted")
}

// TestGetCashFlowHistory_DisclosesTheRequestedEndItClampedAway is the disclosure in the ordinary
// case: a range that straddles today. Without the field a caller comparing this endpoint with
// cash-flow-summary sees two different end dates and no way to tell that one of them is a
// deliberate horizon rather than a disagreement.
func TestGetCashFlowHistory_DisclosesTheRequestedEndItClampedAway(t *testing.T) {
	today := todayUTCForTest()
	requestedEnd := today.AddDate(0, 0, 21)
	repo := &mockAnalyticsRepository{
		getCashFlowGroupedFn: func(_ context.Context, _ string, _ domain.CashFlowFilters) ([]domain.CashFlowGroupedData, error) {
			return []domain.CashFlowGroupedData{
				{PeriodStart: today, Income: 10, Expenses: 4, TransactionCount: 2},
			}, nil
		},
	}

	resp, err := NewAnalyticsService(repo).GetCashFlowHistory(context.Background(), "u1", domain.CashFlowFilters{
		StartDate: today.AddDate(0, 0, -2),
		EndDate:   requestedEnd,
		Grouping:  "daily",
	})
	require.NoError(t, err)

	assert.True(t, resp.RequestedEndDate.Equal(requestedEnd),
		"requested_end_date must be the caller's own end date, unchanged: %v vs %v",
		resp.RequestedEndDate, requestedEnd)
	assert.True(t, resp.EndDate.Equal(today),
		"end_date is the horizon actually answered for: %v vs %v", resp.EndDate, today)
	assert.True(t, resp.EndDate.Before(resp.RequestedEndDate),
		"and the gap between them is what this response deliberately does not cover")
}

// TestGetCashFlowHistory_RequestedEndEqualsEndDateForAPastRange is the narrow half: the two fields
// coincide for every request that does not reach past today, which is nearly all of them. A
// disclosure field that silently differed on ordinary ranges would be worse than none.
func TestGetCashFlowHistory_RequestedEndEqualsEndDateForAPastRange(t *testing.T) {
	today := todayUTCForTest()
	end := today.AddDate(0, 0, -9)
	repo := &mockAnalyticsRepository{
		getCashFlowGroupedFn: func(_ context.Context, _ string, _ domain.CashFlowFilters) ([]domain.CashFlowGroupedData, error) {
			return []domain.CashFlowGroupedData{
				{PeriodStart: today.AddDate(0, 0, -40), Income: 100, Expenses: 40, TransactionCount: 1},
			}, nil
		},
	}

	resp, err := NewAnalyticsService(repo).GetCashFlowHistory(context.Background(), "u1", domain.CashFlowFilters{
		StartDate: today.AddDate(0, 0, -40),
		EndDate:   end,
		Grouping:  "monthly",
	})
	require.NoError(t, err)
	assert.True(t, resp.RequestedEndDate.Equal(end))
	assert.True(t, resp.EndDate.Equal(end),
		"nothing was clamped away, so the two windows are the same window")
}

// TestAnalyticsService_TrailingWindowDatesAreUTCNotProcessLocal is the final-gate regression for
// finding F6.
//
// THE DEFECT. `time.Now().UTC()` is the SAME INSTANT as `time.Now()` with a different Location, so
// dropping the .UTC() is invisible until something READS that Location — and two things do.
//   - GetNetWorthTrend's bounds are compared as CALENDAR DATES: GetAggregatedHistory ends on
//     `(bs.snapshot_at AT TIME ZONE 'UTC')::date <= $3::date` (its household twin on `<= $5::date`),
//     so pgx encodes the parameter as a `date` built from the value's own Location. On a host behind
//     UTC, a snapshot written "today" in UTC falls outside a range ending "today" and the day is
//     invisible until local midnight.
//   - Both bounds, and GetRecurringSubscriptions's AsOfDate, are echoed through
//     `Format("2006-01-02")`, which is Location-dependent too.
//
// ⚠ WHY THE ZONE IS CHOSEN FROM THE CURRENT UTC HOUR RATHER THAN FIXED, and this is the whole
// reason the test is written this way. A fixed offset only puts the local date on a DIFFERENT day
// than UTC's for part of the day: UTC-11 does so while the UTC hour is under 11, UTC+14 while it is
// 10 or over. So a fixed zone would make this test pass with or without the fix for most of the
// day — a fixture sitting exactly where mutant and correct code agree, which is the trap this
// branch has now hit repeatedly. Choosing per UTC hour covers all 24, and the require() below
// FAILS THE TEST rather than skipping if the two dates ever coincide, so the assertion can never
// be vacuous.
//
// time.Local is process-global; nothing in this package runs t.Parallel(), and Cleanup restores it.
func TestAnalyticsService_TrailingWindowDatesAreUTCNotProcessLocal(t *testing.T) {
	original := time.Local
	t.Cleanup(func() { time.Local = original })

	offset := 14 * 3600
	if time.Now().UTC().Hour() < 11 {
		offset = -11 * 3600
	}
	time.Local = time.FixedZone("test-offset", offset)

	utcToday := time.Now().UTC().Format("2006-01-02")
	require.NotEqual(t, utcToday, time.Now().Format("2006-01-02"),
		"fixture guard: the process-local calendar day MUST differ from UTC's, or neither assertion "+
			"below can tell the fix from the defect")

	t.Run("GetNetWorthTrend", func(t *testing.T) {
		var gotStart, gotEnd time.Time
		snapRepo := &mockBalanceSnapshotRepository{
			getAggregatedHistoryFn: func(_ context.Context, _ string, startDate, endDate time.Time, _ []string) ([]domain.BalanceHistoryPoint, error) {
				gotStart, gotEnd = startDate, endDate
				return nil, nil
			},
		}
		service := NewAnalyticsService(&mockAnalyticsRepository{})
		service.SetBalanceSnapshotRepo(snapRepo)

		result, err := service.GetNetWorthTrend(context.Background(), "user-123", "monthly", nil)
		require.NoError(t, err)

		// The parameter as PostgreSQL will see it: pgx renders a `date` from the value's own
		// Location, so this Format is the pgx-visible property, not a proxy for it.
		assert.Equal(t, utcToday, gotEnd.Format("2006-01-02"),
			"the end bound handed to GetAggregatedHistory must be the UTC calendar day; it is "+
				"compared against ::date, so a process-local day hides today's snapshots")
		assert.Equal(t, utcToday, gotStart.AddDate(1, 0, 0).Format("2006-01-02"),
			"the trailing-12-month start must be derived from the same UTC day")
		assert.Equal(t, utcToday, result.EndDate, "and the response must echo the UTC day it queried")
	})

	t.Run("GetRecurringSubscriptions AsOfDate", func(t *testing.T) {
		service := NewAnalyticsService(&mockAnalyticsRepository{
			getRecurringSubscriptionsFn: func(context.Context, string, time.Time) ([]domain.RecurringSubscriptionRow, error) {
				return nil, nil
			},
		})

		result, err := service.GetRecurringSubscriptions(context.Background(), "user-123")
		require.NoError(t, err)
		assert.Equal(t, utcToday, result.AsOfDate,
			"every other date this API emits is a UTC calendar day, so 'as of' must be one too")
	})
}

// #1631 gap D1. Every date display field on these responses documents its value as the UTC
// calendar day, and these values are COMPUTED (the request's own range, echoed; a clamp; a bucket
// end) rather than scanned from a DATE column — so nothing upstream guarantees the location in
// which they name a day. Today's HTTP layer happens to parse YYYY-MM-DD with time.Parse, which
// yields UTC, but that is a convention held at the operations layer and invisible here: the
// service is a public Go API and is the site that must make its own documented promise true.
//
// Midnight in a POSITIVE offset is the previous day in UTC. That is the direction the gap actually
// bites in, and the exact shift domain.formatDateOnly refuses to perform on a caller's behalf.
func TestAnalyticsService_DateDisplaysAreTheUTCCalendarDay(t *testing.T) {
	ctx := context.Background()
	plus14 := time.FixedZone("+14", 14*3600)

	// 2024-03-01 and 2024-03-31 at midnight in +14:00 are 2024-02-29 and 2024-03-30 in UTC.
	filters := domain.CashFlowFilters{
		StartDate: time.Date(2024, 3, 1, 0, 0, 0, 0, plus14),
		EndDate:   time.Date(2024, 3, 31, 0, 0, 0, 0, plus14),
		Grouping:  "daily",
	}

	t.Run("cash flow summary echoes the requested range as UTC days", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getCashFlowAggregationFn: func(context.Context, string, domain.CashFlowFilters) (*domain.CashFlowAggregation, error) {
				return &domain.CashFlowAggregation{Income: 1, Expenses: 1}, nil
			},
		}
		result, err := NewAnalyticsService(mockRepo).GetCashFlowSummary(ctx, "user-123", filters)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.StartDateDisplay != "2024-02-29" {
			t.Errorf("start_date = %q, want the UTC calendar day 2024-02-29", result.StartDateDisplay)
		}
		if result.EndDateDisplay != "2024-03-30" {
			t.Errorf("end_date = %q, want the UTC calendar day 2024-03-30", result.EndDateDisplay)
		}
	})

	t.Run("cash flow history echoes requested_end_date as a UTC day", func(t *testing.T) {
		// A range entirely in the past, so the clamp does not move the end and the echo is the
		// only thing under test.
		past := domain.CashFlowFilters{
			StartDate: time.Date(2020, 3, 1, 0, 0, 0, 0, plus14),
			EndDate:   time.Date(2020, 3, 31, 0, 0, 0, 0, plus14),
			Grouping:  "daily",
		}
		mockRepo := &mockAnalyticsRepository{
			getCashFlowGroupedFn: func(context.Context, string, domain.CashFlowFilters) ([]domain.CashFlowGroupedData, error) {
				return []domain.CashFlowGroupedData{{
					// A pgx-scanned DATE bucket: already midnight UTC.
					PeriodStart: time.Date(2020, 3, 2, 0, 0, 0, 0, time.UTC),
					Income:      10,
				}}, nil
			},
		}
		result, err := NewAnalyticsService(mockRepo).GetCashFlowHistory(ctx, "user-123", past)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.StartDateDisplay != "2020-02-29" {
			t.Errorf("start_date = %q, want the UTC calendar day 2020-02-29", result.StartDateDisplay)
		}
		if result.RequestedEndDateDisplay != "2020-03-30" {
			t.Errorf("requested_end_date = %q, want the UTC calendar day 2020-03-30", result.RequestedEndDateDisplay)
		}
		if result.EndDateDisplay != "2020-03-30" {
			t.Errorf("end_date = %q, want the UTC calendar day 2020-03-30", result.EndDateDisplay)
		}
		if len(result.Periods) != 1 {
			t.Fatalf("expected 1 period, got %d", len(result.Periods))
		}
		if result.Periods[0].PeriodStartDisplay != "2020-03-02" {
			t.Errorf("period_start = %q, want 2020-03-02", result.Periods[0].PeriodStartDisplay)
		}
	})
}
