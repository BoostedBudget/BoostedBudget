package services

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// AnalyticsService provides cash flow analytics functionality
// Issue #354: Cash Flow Analysis & Income/Expense Visualization
// Issue #953: Spending Reports
type AnalyticsService interface {
	// GetCashFlowSummary returns aggregated cash flow data for a date range
	// Includes income, expenses, net cash flow, and period comparison
	GetCashFlowSummary(ctx context.Context, userID string, filters domain.CashFlowFilters) (*domain.CashFlowSummaryResponse, error)

	// GetCashFlowHistory returns time series cash flow data grouped by period
	// Used for rendering cash flow bar charts
	GetCashFlowHistory(ctx context.Context, userID string, filters domain.CashFlowFilters) (*domain.CashFlowHistoryResponse, error)

	// GetSpendingByCategory returns spending totals grouped by category with percentages
	GetSpendingByCategory(ctx context.Context, userID string, filters domain.SpendingFilters) (*domain.SpendingByCategoryResponse, error)

	// GetSpendingByMerchant returns top merchants by spending with percentages
	GetSpendingByMerchant(ctx context.Context, userID string, filters domain.SpendingFilters, limit int) (*domain.SpendingByMerchantResponse, error)

	// GetSpendingTrends returns monthly category spending trends for stacked charts
	GetSpendingTrends(ctx context.Context, userID string, filters domain.SpendingFilters) (*domain.SpendingTrendsResponse, error)
}

type analyticsService struct {
	analyticsRepo repository.AnalyticsRepository
}

// NewAnalyticsService creates a new AnalyticsService instance
func NewAnalyticsService(analyticsRepo repository.AnalyticsRepository) AnalyticsService {
	return &analyticsService{
		analyticsRepo: analyticsRepo,
	}
}

// GetCashFlowSummary returns aggregated cash flow data for a date range
func (s *analyticsService) GetCashFlowSummary(ctx context.Context, userID string, filters domain.CashFlowFilters) (*domain.CashFlowSummaryResponse, error) {
	// Validate filters
	if err := filters.Validate(); err != nil {
		return nil, fmt.Errorf("invalid filters: %w", err)
	}

	// Get aggregated cash flow for current period
	currentAgg, err := s.analyticsRepo.GetCashFlowAggregation(ctx, userID, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to get cash flow aggregation: %w", err)
	}

	// Calculate previous period for comparison
	periodDuration := filters.EndDate.Sub(filters.StartDate)
	previousStart := filters.StartDate.Add(-periodDuration - 24*time.Hour)
	previousEnd := filters.StartDate.Add(-24 * time.Hour)

	previousFilters := domain.CashFlowFilters{
		StartDate:        previousStart,
		EndDate:          previousEnd,
		AccountTypes:     filters.AccountTypes,
		IncludeTransfers: filters.IncludeTransfers,
	}

	previousAgg, err := s.analyticsRepo.GetCashFlowAggregation(ctx, userID, previousFilters)
	if err != nil {
		// Log error but don't fail - period comparison is optional
		previousAgg = nil
	}

	// Build response
	response := &domain.CashFlowSummaryResponse{
		StartDate:    filters.StartDate,
		EndDate:      filters.EndDate,
		Income:       currentAgg.Income,
		Expenses:     currentAgg.Expenses,
		NetCashFlow:  currentAgg.NetCashFlow(),
		IncomeCount:  currentAgg.IncomeCount,
		ExpenseCount: currentAgg.ExpenseCount,
	}

	// Add period comparison if we have previous period data
	if previousAgg != nil && (previousAgg.Income > 0 || previousAgg.Expenses > 0) {
		previousNet := previousAgg.NetCashFlow()

		incomeChangeAmount := currentAgg.Income - previousAgg.Income
		incomeChangePercent := 0.0
		if previousAgg.Income > 0 {
			incomeChangePercent = (incomeChangeAmount / previousAgg.Income) * 100
		}

		expenseChangeAmount := currentAgg.Expenses - previousAgg.Expenses
		expenseChangePercent := 0.0
		if previousAgg.Expenses > 0 {
			expenseChangePercent = (expenseChangeAmount / previousAgg.Expenses) * 100
		}

		netChangeAmount := response.NetCashFlow - previousNet
		netChangePercent := 0.0
		if previousNet != 0 {
			netChangePercent = (netChangeAmount / absFloat(previousNet)) * 100
		}

		response.PeriodComparison = &domain.CashFlowPeriodComparison{
			PreviousIncome:       previousAgg.Income,
			PreviousExpenses:     previousAgg.Expenses,
			PreviousNetCashFlow:  previousNet,
			IncomeChangeAmount:   incomeChangeAmount,
			IncomeChangePercent:  incomeChangePercent,
			ExpenseChangeAmount:  expenseChangeAmount,
			ExpenseChangePercent: expenseChangePercent,
			NetChangeAmount:      netChangeAmount,
			NetChangePercent:     netChangePercent,
		}
	}

	return response, nil
}

// GetCashFlowHistory returns time series cash flow data grouped by period
func (s *analyticsService) GetCashFlowHistory(ctx context.Context, userID string, filters domain.CashFlowFilters) (*domain.CashFlowHistoryResponse, error) {
	// Validate filters
	if err := filters.Validate(); err != nil {
		return nil, fmt.Errorf("invalid filters: %w", err)
	}

	// Default to daily grouping if not specified
	if filters.Grouping == "" {
		filters.Grouping = autoSelectGrouping(filters.StartDate, filters.EndDate)
	}

	// Get grouped cash flow data
	groupedData, err := s.analyticsRepo.GetCashFlowGrouped(ctx, userID, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to get grouped cash flow: %w", err)
	}

	// Convert to response format
	periods := make([]domain.CashFlowPeriodData, len(groupedData))
	for i, data := range groupedData {
		periodEnd := calculatePeriodEnd(data.PeriodStart, filters.Grouping)

		periods[i] = domain.CashFlowPeriodData{
			PeriodStart:      data.PeriodStart,
			PeriodEnd:        periodEnd,
			Income:           data.Income,
			Expenses:         data.Expenses,
			NetCashFlow:      data.Income - data.Expenses,
			TransactionCount: data.TransactionCount,
		}
	}

	return &domain.CashFlowHistoryResponse{
		StartDate: filters.StartDate,
		EndDate:   filters.EndDate,
		Grouping:  filters.Grouping,
		Periods:   periods,
	}, nil
}

// autoSelectGrouping automatically determines the best grouping based on date range
// - <= 14 days: daily
// - <= 90 days: weekly
// - > 90 days: monthly
func autoSelectGrouping(startDate, endDate time.Time) string {
	days := int(endDate.Sub(startDate).Hours() / 24)

	if days <= 14 {
		return "daily"
	}
	if days <= 90 {
		return "weekly"
	}
	return "monthly"
}

// calculatePeriodEnd calculates the end of a period based on start and grouping
func calculatePeriodEnd(periodStart time.Time, grouping string) time.Time {
	switch grouping {
	case "weekly":
		return periodStart.AddDate(0, 0, 6)
	case "monthly":
		nextMonth := periodStart.AddDate(0, 1, 0)
		return nextMonth.AddDate(0, 0, -1)
	default: // daily
		return periodStart
	}
}

// GetSpendingByCategory returns spending totals grouped by category with percentages and display names
func (s *analyticsService) GetSpendingByCategory(ctx context.Context, userID string, filters domain.SpendingFilters) (*domain.SpendingByCategoryResponse, error) {
	categories, err := s.analyticsRepo.GetSpendingByCategory(ctx, userID, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to get spending by category: %w", err)
	}

	var totalSpent float64
	for _, c := range categories {
		totalSpent += c.Total
	}

	for i := range categories {
		categories[i].DisplayName = formatCategoryName(categories[i].Category)
		if totalSpent > 0 {
			categories[i].Percentage = (categories[i].Total / totalSpent) * 100
		}
	}

	return &domain.SpendingByCategoryResponse{
		StartDate:  filters.StartDate.Format("2006-01-02"),
		EndDate:    filters.EndDate.Format("2006-01-02"),
		TotalSpent: totalSpent,
		Categories: categories,
	}, nil
}

// GetSpendingByMerchant returns top merchants by spending with percentages
func (s *analyticsService) GetSpendingByMerchant(ctx context.Context, userID string, filters domain.SpendingFilters, limit int) (*domain.SpendingByMerchantResponse, error) {
	merchants, err := s.analyticsRepo.GetSpendingByMerchant(ctx, userID, filters, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get spending by merchant: %w", err)
	}

	var totalSpent float64
	for _, m := range merchants {
		totalSpent += m.Total
	}

	for i := range merchants {
		if totalSpent > 0 {
			merchants[i].Percentage = (merchants[i].Total / totalSpent) * 100
		}
	}

	return &domain.SpendingByMerchantResponse{
		StartDate: filters.StartDate.Format("2006-01-02"),
		EndDate:   filters.EndDate.Format("2006-01-02"),
		TopN:      limit,
		Merchants: merchants,
	}, nil
}

// GetSpendingTrends returns monthly category spending with top 5 categories and "Other" bucketing
func (s *analyticsService) GetSpendingTrends(ctx context.Context, userID string, filters domain.SpendingFilters) (*domain.SpendingTrendsResponse, error) {
	rows, err := s.analyticsRepo.GetSpendingTrends(ctx, userID, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to get spending trends: %w", err)
	}

	// Calculate total per category across all periods
	categoryTotals := make(map[string]float64)
	for _, row := range rows {
		categoryTotals[row.Category] += row.Total
	}

	// Sort categories by total descending, take top 5
	type catTotal struct {
		category string
		total    float64
	}
	sorted := make([]catTotal, 0, len(categoryTotals))
	for cat, total := range categoryTotals {
		sorted = append(sorted, catTotal{cat, total})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].total > sorted[j].total
	})

	const maxTopCategories = 5
	topSet := make(map[string]bool)
	topCategories := make([]string, 0, maxTopCategories)
	for i, ct := range sorted {
		if i >= maxTopCategories {
			break
		}
		topSet[ct.category] = true
		topCategories = append(topCategories, ct.category)
	}

	// Group rows by period
	type periodKey = time.Time
	periodOrder := make([]periodKey, 0)
	periodRows := make(map[periodKey][]domain.SpendingTrendRow)
	for _, row := range rows {
		if _, exists := periodRows[row.PeriodStart]; !exists {
			periodOrder = append(periodOrder, row.PeriodStart)
		}
		periodRows[row.PeriodStart] = append(periodRows[row.PeriodStart], row)
	}

	periods := make([]domain.SpendingTrendPeriod, 0, len(periodOrder))
	for _, periodStart := range periodOrder {
		// Aggregate into top categories + Other
		catSpend := make(map[string]float64)
		for _, row := range periodRows[periodStart] {
			if topSet[row.Category] {
				catSpend[row.Category] += row.Total
			} else {
				catSpend["OTHER"] += row.Total
			}
		}

		// Build category list (top categories first, then Other if present)
		var periodTotal float64
		cats := make([]domain.CategorySpending, 0)
		for _, tc := range topCategories {
			if spend, ok := catSpend[tc]; ok {
				cats = append(cats, domain.CategorySpending{
					Category:    tc,
					DisplayName: formatCategoryName(tc),
					Total:       spend,
				})
				periodTotal += spend
			}
		}
		if otherSpend, ok := catSpend["OTHER"]; ok {
			cats = append(cats, domain.CategorySpending{
				Category:    "OTHER",
				DisplayName: "Other",
				Total:       otherSpend,
			})
			periodTotal += otherSpend
		}

		// Calculate period end (last day of month)
		periodEnd := periodStart.AddDate(0, 1, 0).AddDate(0, 0, -1)

		periods = append(periods, domain.SpendingTrendPeriod{
			PeriodStart: periodStart.Format("2006-01-02"),
			PeriodEnd:   periodEnd.Format("2006-01-02"),
			Categories:  cats,
			Total:       periodTotal,
		})
	}

	return &domain.SpendingTrendsResponse{
		StartDate:     filters.StartDate.Format("2006-01-02"),
		EndDate:       filters.EndDate.Format("2006-01-02"),
		Periods:       periods,
		TopCategories: topCategories,
	}, nil
}

// formatCategoryName converts a SCREAMING_SNAKE_CASE category to Title Case
// e.g. FOOD_AND_DRINK → "Food & Drink"
func formatCategoryName(s string) string {
	words := strings.Split(s, "_")
	for i, w := range words {
		if len(w) == 0 {
			continue
		}
		words[i] = strings.ToUpper(w[:1]) + strings.ToLower(w[1:])
	}
	result := strings.Join(words, " ")
	result = strings.ReplaceAll(result, " And ", " & ")
	return result
}

// absFloat returns the absolute value of a float64
func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
