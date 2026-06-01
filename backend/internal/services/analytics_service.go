package services

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/pfc"
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

	// GetCategoryTotalForPeriod returns the total spend for a specific category in a date range.
	// Single DB round-trip; token-efficient; reusable by MCP and React frontend (Issue #1087).
	GetCategoryTotalForPeriod(ctx context.Context, userID string, category string, startDate time.Time, endDate time.Time) (*domain.CategoryTotalResponse, error)

	// GetTopExpense returns the single largest expense transaction in a date range.
	// Returns nil response when no expense transactions exist in the period (Issue #1087).
	GetTopExpense(ctx context.Context, userID string, startDate time.Time, endDate time.Time) (*domain.TopExpenseResponse, error)

	// GetAverageMonthlySpend returns the average monthly spend over the date range in filters.
	// Single DB round-trip; token-efficient summary; AI-tool-callable (Issue #1215).
	GetAverageMonthlySpend(ctx context.Context, userID string, filters domain.SpendingFilters) (*domain.AverageMonthlySpendResponse, error)

	// GetRecurringSubscriptions detects recurring subscription charges using heuristic SQL.
	// No date params; uses a 12-month trailing window. AI-tool-callable (Issue #1215).
	GetRecurringSubscriptions(ctx context.Context, userID string) (*domain.RecurringSubscriptionsResponse, error)

	// GetSavingsRateForPeriod returns the savings rate (income - expenses) / income for a period.
	// Returns nil savings_rate pointer when income is zero. AI-tool-callable (Issue #1215 PR2).
	GetSavingsRateForPeriod(ctx context.Context, userID string, startDate time.Time, endDate time.Time) (*domain.SavingsRateResponse, error)

	// GetMonthOverMonthSpendingChange returns monthly spend buckets with period-over-period changes.
	// AI-tool-callable (Issue #1215 PR2).
	GetMonthOverMonthSpendingChange(ctx context.Context, userID string, category string, months int) (*domain.MonthOverMonthResponse, error)

	// GetLargestTransactionsForPeriod returns the N largest expense transactions in a date range.
	// AI-tool-callable (Issue #1215 PR2).
	GetLargestTransactionsForPeriod(ctx context.Context, userID string, filters domain.SpendingFilters, limit int) (*domain.LargestTransactionsResponse, error)

	// GetCategoryBreakdownForMerchant returns spending by category for a specific merchant.
	// AI-tool-callable (Issue #1215 PR2).
	GetCategoryBreakdownForMerchant(ctx context.Context, userID string, merchantName string, filters domain.SpendingFilters) (*domain.CategoryBreakdownForMerchantResponse, error)

	// GetIncomeSourceBreakdown returns income (inflow) totals grouped by category.
	// AI-tool-callable (Issue #1215 PR2).
	GetIncomeSourceBreakdown(ctx context.Context, userID string, filters domain.SpendingFilters) (*domain.IncomeSourceBreakdownResponse, error)
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

// isPFCPrimary reports whether s is a known PFCv2 primary enum.
func isPFCPrimary(s string) bool {
	for _, p := range pfc.AllPrimaries() {
		if p.Enum == s {
			return true
		}
	}
	return false
}

// pfcDisplayName returns the appropriate display name for a category enum.
// Primary enums use PrimaryDisplayName; detailed enums use DetailedDisplayName.
func pfcDisplayName(category string) string {
	if isPFCPrimary(category) {
		return pfc.PrimaryDisplayName(category)
	}
	// Detailed enum — look up the primary from the registry.
	for _, d := range pfc.AllDetailed() {
		if d.Enum == category {
			return pfc.DetailedDisplayName(d.PrimaryEnum, category)
		}
	}
	// Unknown detailed: fall back to primary display (title-case fallback inside).
	return pfc.PrimaryDisplayName(category)
}

// GetSpendingByCategory returns spending totals grouped by category with percentages and display names
func (s *analyticsService) GetSpendingByCategory(ctx context.Context, userID string, filters domain.SpendingFilters) (*domain.SpendingByCategoryResponse, error) {
	categories, err := s.analyticsRepo.GetSpendingByCategory(ctx, userID, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to get spending by category: %w", err)
	}

	// Merge by raw category token (UUID for custom, enum for PFC) to keep distinct rows.
	// Use server-resolved display from repo when available; fall back to pfcDisplayName for PFC.
	merged := make(map[string]*domain.SpendingByCategory)
	var order []string
	for _, c := range categories {
		displayName := c.PFCPrimaryDisplay
		if displayName == "" {
			displayName = pfcDisplayName(c.Category)
		}
		if existing, ok := merged[c.Category]; ok {
			existing.Total += c.Total
			existing.TransactionCount += c.TransactionCount
		} else {
			merged[c.Category] = &domain.SpendingByCategory{
				Category:          c.Category,
				PFCPrimaryDisplay: displayName,
				Total:             c.Total,
				TransactionCount:  c.TransactionCount,
			}
			order = append(order, c.Category)
		}
	}

	categories = make([]domain.SpendingByCategory, 0, len(order))
	var totalSpent float64
	for _, key := range order {
		categories = append(categories, *merged[key])
		totalSpent += merged[key].Total
	}

	for i := range categories {
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

	// Calculate total per category across all periods and collect display names.
	categoryTotals := make(map[string]float64)
	categoryDisplays := make(map[string]string)
	for _, row := range rows {
		categoryTotals[row.Category] += row.Total
		if row.CategoryDisplay != "" {
			categoryDisplays[row.Category] = row.CategoryDisplay
		}
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

	// Group rows by period, preserving order
	periodOrder := make([]time.Time, 0)
	periodRows := make(map[time.Time][]domain.SpendingTrendRow)
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

		// Build category list (top categories first, then Other if present).
		// Use server-resolved display when available; fall back to pfcDisplayName for PFC rows.
		var periodTotal float64
		cats := make([]domain.CategorySpending, 0)
		for _, tc := range topCategories {
			if spend, ok := catSpend[tc]; ok {
				display := categoryDisplays[tc]
				if display == "" {
					display = pfcDisplayName(tc)
				}
				cats = append(cats, domain.CategorySpending{
					Category:          tc,
					PFCPrimaryDisplay: display,
					Total:             spend,
				})
				periodTotal += spend
			}
		}
		if otherSpend, ok := catSpend["OTHER"]; ok {
			cats = append(cats, domain.CategorySpending{
				Category:          "OTHER",
				PFCPrimaryDisplay: "Other",
				Total:             otherSpend,
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

// absFloat returns the absolute value of a float64
func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// GetCategoryTotalForPeriod returns the total spend for a specific category in a date range.
func (s *analyticsService) GetCategoryTotalForPeriod(ctx context.Context, userID string, category string, startDate time.Time, endDate time.Time) (*domain.CategoryTotalResponse, error) {
	if endDate.Before(startDate) {
		return nil, fmt.Errorf("end_date cannot be before start_date")
	}

	row, err := s.analyticsRepo.GetCategoryTotalForPeriod(ctx, userID, category, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("failed to get category total: %w", err)
	}

	return &domain.CategoryTotalResponse{
		Category:          category,
		PFCPrimaryDisplay: PFCPrimaryDisplayName(category),
		Total:             row.Total,
		TransactionCount:  row.TransactionCount,
		StartDate:         startDate.Format("2006-01-02"),
		EndDate:           endDate.Format("2006-01-02"),
	}, nil
}

// GetAverageMonthlySpend returns the average monthly spend over the date range in filters.
func (s *analyticsService) GetAverageMonthlySpend(ctx context.Context, userID string, filters domain.SpendingFilters) (*domain.AverageMonthlySpendResponse, error) {
	if filters.EndDate.Before(filters.StartDate) {
		return nil, fmt.Errorf("end_date cannot be before start_date")
	}

	row, err := s.analyticsRepo.GetAverageMonthlySpend(ctx, userID, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to get average monthly spend: %w", err)
	}

	// Compute month count: ceiling of days span / 30.4375 (average days/month)
	days := filters.EndDate.Sub(filters.StartDate).Hours() / 24
	monthCount := int(math.Ceil(days / 30.4375))
	if monthCount < 1 {
		monthCount = 1
	}

	avg := row.Total / float64(monthCount)

	return &domain.AverageMonthlySpendResponse{
		AverageMonthlySpend: avg,
		TotalSpend:          row.Total,
		MonthCount:          monthCount,
		StartDate:           filters.StartDate.Format("2006-01-02"),
		EndDate:             filters.EndDate.Format("2006-01-02"),
	}, nil
}

// GetRecurringSubscriptions detects recurring subscription charges using heuristic SQL.
// Uses a trailing 12-month window. AI-tool-callable (Issue #1215).
func (s *analyticsService) GetRecurringSubscriptions(ctx context.Context, userID string) (*domain.RecurringSubscriptionsResponse, error) {
	lookbackStart := time.Now().AddDate(-1, 0, 0)

	rows, err := s.analyticsRepo.GetRecurringSubscriptions(ctx, userID, lookbackStart)
	if err != nil {
		return nil, fmt.Errorf("failed to get recurring subscriptions: %w", err)
	}

	items := make([]domain.RecurringSubscriptionItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, domain.RecurringSubscriptionItem{
			MerchantName:     row.MerchantName,
			Amount:           row.Amount,
			OccurrenceCount:  row.OccurrenceCount,
			AvgGapDays:       row.AvgGapDays,
			LastChargeDate:   row.LastChargeDate.Format("2006-01-02"),
			NextExpectedDate: row.NextExpectedDate.Format("2006-01-02"),
		})
	}

	return &domain.RecurringSubscriptionsResponse{
		Subscriptions: items,
		AsOfDate:      time.Now().Format("2006-01-02"),
	}, nil
}

// GetSavingsRateForPeriod returns the savings rate for a date range.
// SavingsRate = (income - expenses) / income; nil when income is zero.
func (s *analyticsService) GetSavingsRateForPeriod(ctx context.Context, userID string, startDate time.Time, endDate time.Time) (*domain.SavingsRateResponse, error) {
	if endDate.Before(startDate) {
		return nil, fmt.Errorf("end_date cannot be before start_date")
	}

	row, err := s.analyticsRepo.GetSavingsRateForPeriod(ctx, userID, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("failed to get savings rate: %w", err)
	}

	var rate *float64
	if row.Income > 0 {
		r := (row.Income - row.Expenses) / row.Income
		rate = &r
	}

	return &domain.SavingsRateResponse{
		SavingsRate: rate,
		Income:      row.Income,
		Expenses:    row.Expenses,
		NetSavings:  row.Income - row.Expenses,
		StartDate:   startDate.Format("2006-01-02"),
		EndDate:     endDate.Format("2006-01-02"),
	}, nil
}

// GetMonthOverMonthSpendingChange returns monthly spend buckets with period-over-period delta.
// months must be between 2 and 24.
func (s *analyticsService) GetMonthOverMonthSpendingChange(ctx context.Context, userID string, category string, months int) (*domain.MonthOverMonthResponse, error) {
	if months < 2 || months > 24 {
		return nil, fmt.Errorf("months must be between 2 and 24")
	}

	startDate := time.Now().AddDate(0, -months, 0)
	rows, err := s.analyticsRepo.GetMonthOverMonthSpending(ctx, userID, startDate, category)
	if err != nil {
		return nil, fmt.Errorf("failed to get month-over-month spending: %w", err)
	}

	buckets := make([]domain.MonthOverMonthBucket, 0, len(rows))
	for i, row := range rows {
		b := domain.MonthOverMonthBucket{
			Month: row.Month.Format("2006-01-02"),
			Total: row.Total,
		}
		if i > 0 && rows[i-1].Total > 0 {
			pct := (row.Total - rows[i-1].Total) / rows[i-1].Total
			abs := row.Total - rows[i-1].Total
			b.ChangePct = &pct
			b.ChangeAbs = &abs
		} else if i > 0 {
			// previous bucket was zero — can't compute pct, but set abs
			abs := row.Total - rows[i-1].Total
			b.ChangeAbs = &abs
		}
		buckets = append(buckets, b)
	}

	return &domain.MonthOverMonthResponse{
		Buckets:    buckets,
		Category:   category,
		MonthCount: months,
	}, nil
}

// GetLargestTransactionsForPeriod returns the N largest expense transactions in a date range.
func (s *analyticsService) GetLargestTransactionsForPeriod(ctx context.Context, userID string, filters domain.SpendingFilters, limit int) (*domain.LargestTransactionsResponse, error) {
	rows, err := s.analyticsRepo.GetLargestTransactionsForPeriod(ctx, userID, filters, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get largest transactions: %w", err)
	}

	items := make([]domain.LargestTransactionItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, domain.LargestTransactionItem{
			TransactionID:     row.TransactionID,
			MerchantName:      row.MerchantName,
			Amount:            row.Amount,
			Category:          row.Category,
			PFCPrimaryDisplay: PFCPrimaryDisplayName(row.Category),
			Date:              row.Date.Format("2006-01-02"),
			AccountID:         row.AccountID,
		})
	}

	return &domain.LargestTransactionsResponse{
		Transactions: items,
		Limit:        limit,
		StartDate:    filters.StartDate.Format("2006-01-02"),
		EndDate:      filters.EndDate.Format("2006-01-02"),
	}, nil
}

// GetCategoryBreakdownForMerchant returns spending by category for a specific merchant.
func (s *analyticsService) GetCategoryBreakdownForMerchant(ctx context.Context, userID string, merchantName string, filters domain.SpendingFilters) (*domain.CategoryBreakdownForMerchantResponse, error) {
	rows, err := s.analyticsRepo.GetCategoryBreakdownForMerchant(ctx, userID, merchantName, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to get category breakdown for merchant: %w", err)
	}

	var totalSpend float64
	for _, row := range rows {
		totalSpend += row.Total
	}

	categories := make([]domain.CategoryBreakdownItem, 0, len(rows))
	for _, row := range rows {
		item := domain.CategoryBreakdownItem{
			Category:          row.Category,
			PFCPrimaryDisplay: PFCPrimaryDisplayName(row.Category),
			Total:             row.Total,
			TransactionCount:  row.Count,
		}
		if totalSpend > 0 {
			item.Percentage = (row.Total / totalSpend) * 100
		}
		categories = append(categories, item)
	}

	return &domain.CategoryBreakdownForMerchantResponse{
		MerchantName: merchantName,
		Categories:   categories,
		TotalSpend:   totalSpend,
		StartDate:    filters.StartDate.Format("2006-01-02"),
		EndDate:      filters.EndDate.Format("2006-01-02"),
	}, nil
}

// GetIncomeSourceBreakdown returns income totals grouped by category with percentages.
func (s *analyticsService) GetIncomeSourceBreakdown(ctx context.Context, userID string, filters domain.SpendingFilters) (*domain.IncomeSourceBreakdownResponse, error) {
	rows, err := s.analyticsRepo.GetIncomeSourceBreakdown(ctx, userID, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to get income source breakdown: %w", err)
	}

	var totalIncome float64
	for _, row := range rows {
		totalIncome += row.Total
	}

	sources := make([]domain.IncomeSourceItem, 0, len(rows))
	for _, row := range rows {
		item := domain.IncomeSourceItem{
			Category:          row.Category,
			PFCPrimaryDisplay: PFCPrimaryDisplayName(row.Category),
			Total:             row.Total,
			TransactionCount:  row.Count,
		}
		if totalIncome > 0 {
			item.Percentage = (row.Total / totalIncome) * 100
		}
		sources = append(sources, item)
	}

	return &domain.IncomeSourceBreakdownResponse{
		Sources:     sources,
		TotalIncome: totalIncome,
		StartDate:   filters.StartDate.Format("2006-01-02"),
		EndDate:     filters.EndDate.Format("2006-01-02"),
	}, nil
}

// GetTopExpense returns the single largest expense transaction in a date range.
func (s *analyticsService) GetTopExpense(ctx context.Context, userID string, startDate time.Time, endDate time.Time) (*domain.TopExpenseResponse, error) {
	row, err := s.analyticsRepo.GetTopExpense(ctx, userID, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("failed to get top expense: %w", err)
	}
	if row == nil {
		return nil, nil
	}

	return &domain.TopExpenseResponse{
		TransactionID:     row.TransactionID,
		MerchantName:      row.MerchantName,
		Amount:            row.Amount,
		Category:          row.Category,
		PFCPrimaryDisplay: PFCPrimaryDisplayName(row.Category),
		Date:              row.Date.Format("2006-01-02"),
		StartDate:         startDate.Format("2006-01-02"),
		EndDate:           endDate.Format("2006-01-02"),
	}, nil
}
