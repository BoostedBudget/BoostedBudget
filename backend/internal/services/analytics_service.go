package services

import (
	"context"
	"fmt"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// AnalyticsService provides cash flow analytics functionality
// Issue #354: Cash Flow Analysis & Income/Expense Visualization
type AnalyticsService interface {
	// GetCashFlowSummary returns aggregated cash flow data for a date range
	// Includes income, expenses, net cash flow, and period comparison
	GetCashFlowSummary(ctx context.Context, userID string, filters domain.CashFlowFilters) (*domain.CashFlowSummaryResponse, error)

	// GetCashFlowHistory returns time series cash flow data grouped by period
	// Used for rendering cash flow bar charts
	GetCashFlowHistory(ctx context.Context, userID string, filters domain.CashFlowFilters) (*domain.CashFlowHistoryResponse, error)
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

// absFloat returns the absolute value of a float64
func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
