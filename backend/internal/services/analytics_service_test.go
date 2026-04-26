package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// mockAnalyticsRepository is a mock implementation for testing
type mockAnalyticsRepository struct {
	getCashFlowAggregationFn func(ctx context.Context, userID string, filters domain.CashFlowFilters) (*domain.CashFlowAggregation, error)
	getCashFlowGroupedFn     func(ctx context.Context, userID string, filters domain.CashFlowFilters) ([]domain.CashFlowGroupedData, error)
	getSpendingByCategoryFn  func(ctx context.Context, userID string, filters domain.SpendingFilters) ([]domain.SpendingByCategory, error)
	getSpendingByMerchantFn  func(ctx context.Context, userID string, filters domain.SpendingFilters, limit int) ([]domain.SpendingByMerchant, error)
	getSpendingTrendsFn      func(ctx context.Context, userID string, filters domain.SpendingFilters) ([]domain.SpendingTrendRow, error)
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

		// Check third period (negative net)
		if result.Periods[2].NetCashFlow != -100 { // 0 - 100
			t.Errorf("expected net -100, got %f", result.Periods[2].NetCashFlow)
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
				return []domain.SpendingByCategory{
					{Category: "FOOD_AND_DRINK", Total: 300.0, TransactionCount: 20},
					{Category: "TRANSPORTATION", Total: 100.0, TransactionCount: 8},
					{Category: "ENTERTAINMENT", Total: 100.0, TransactionCount: 5},
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
		if cat.DisplayName != "Food And Drink" {
			t.Errorf("expected 'Food & Drink', got %s", cat.DisplayName)
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
