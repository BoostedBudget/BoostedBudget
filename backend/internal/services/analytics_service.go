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

// unusualSpendSigmaThreshold is the number of standard deviations above category mean
// required for a transaction to be flagged as unusual.
const unusualSpendSigmaThreshold = 2.0

// AnalyticsService provides cash flow analytics functionality
// Issue #354: Cash Flow Analysis & Income/Expense Visualization
// Issue #953: Spending Reports
// Issue #1215: AI-tool-callable analytics endpoints
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
	// scope, when non-nil, aggregates across the household's shared accounts (Issue #1369 —
	// Phase 5); nil = personal (today's behavior, unchanged).
	GetSavingsRateForPeriod(ctx context.Context, userID string, startDate time.Time, endDate time.Time, scope *domain.HouseholdScope) (*domain.SavingsRateResponse, error)

	// GetMonthOverMonthSpendingChange returns monthly spend buckets with period-over-period delta.
	// category is optional; accountTypes and includeTransfers filter the data. scope, when
	// non-nil, aggregates across the household's shared accounts (Issue #1369 — Phase 5).
	GetMonthOverMonthSpendingChange(ctx context.Context, userID string, category string, months int, accountTypes []string, includeTransfers bool, scope *domain.HouseholdScope) (*domain.MonthOverMonthResponse, error)

	// GetLargestTransactionsForPeriod returns the N largest expense transactions in a date range.
	// AI-tool-callable (Issue #1215 PR2).
	GetLargestTransactionsForPeriod(ctx context.Context, userID string, filters domain.SpendingFilters, limit int) (*domain.LargestTransactionsResponse, error)

	// GetCategoryBreakdownForMerchant returns spending by category for a specific merchant.
	// Accepts merchant_id (UUID) to filter by effective canonical merchant (#1348 G2b).
	// AI-tool-callable (Issue #1215 PR2).
	GetCategoryBreakdownForMerchant(ctx context.Context, userID string, merchantID string, filters domain.SpendingFilters) (*domain.CategoryBreakdownForMerchantResponse, error)

	// GetIncomeSourceBreakdown returns income (inflow) totals grouped by category.
	// AI-tool-callable (Issue #1215 PR2).
	GetIncomeSourceBreakdown(ctx context.Context, userID string, filters domain.SpendingFilters) (*domain.IncomeSourceBreakdownResponse, error)

	// GetUnusualSpend returns transactions in [startDate, endDate] that exceed 2 sigma above
	// per-category baseline mean (computed 3 months prior). AI-tool-callable (Issue #1215 PR3).
	GetUnusualSpend(ctx context.Context, userID string, startDate, endDate time.Time) (*domain.UnusualSpendResponse, error)

	// GetNetWorthTrend returns net worth bucketed by grouping (daily/weekly/monthly) over
	// the trailing 12 months. Requires SetBalanceSnapshotRepo. AI-tool-callable (Issue #1215 PR3).
	// scope, when non-nil, aggregates across the household's shared accounts (Issue #1369
	// — Phase 5); nil = personal (today's behavior, unchanged).
	GetNetWorthTrend(ctx context.Context, userID string, grouping string, scope *domain.HouseholdScope) (*domain.NetWorthTrendResponse, error)

	// GetBudgetVsActual returns active budgets for the given budget period with allocated vs spent amounts.
	// period selects budgets by their configured budget period (weekly/monthly/quarterly/yearly/custom).
	// AI-tool-callable (Issue #1215 PR3).
	GetBudgetVsActual(ctx context.Context, userID string, period string) (*domain.BudgetVsActualResponse, error)
}

type analyticsService struct {
	analyticsRepo       repository.AnalyticsRepository
	balanceSnapshotRepo repository.BalanceSnapshotRepository // optional; required for GetNetWorthTrend
}

// NewAnalyticsService creates a new analyticsService instance.
// Returns the concrete pointer so callers can wire optional deps (e.g. SetBalanceSnapshotRepo)
// without changing the 50+ test call-sites that receive the interface.
func NewAnalyticsService(analyticsRepo repository.AnalyticsRepository) *analyticsService {
	return &analyticsService{
		analyticsRepo: analyticsRepo,
	}
}

// SetBalanceSnapshotRepo wires the optional BalanceSnapshotRepository.
// Required for GetNetWorthTrend; safe to omit for all other methods.
func (s *analyticsService) SetBalanceSnapshotRepo(repo repository.BalanceSnapshotRepository) {
	s.balanceSnapshotRepo = repo
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
		// HouseholdScope must carry over — otherwise the previous-period comparison
		// silently falls back to personal scope even when the current period is
		// household-scoped (Codex adversarial review, P0-1). Issue #1369 — Phase 5.
		HouseholdScope: filters.HouseholdScope,
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

// pfcDisplayName returns the appropriate display name for a category enum.
// Primary enums use PrimaryDisplayName; detailed enums use DetailedDisplayName.
func pfcDisplayName(category string) string {
	// Check if it's a primary enum
	for _, p := range pfc.AllPrimaries() {
		if p.Enum == category {
			return pfc.PrimaryDisplayName(category)
		}
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

	// Merge by raw category token (UUID for custom, enum for PFC) to keep distinct rows — never by
	// display, which is not unique and can change mid-rename while the async
	// effective_category_refresh job catches up.
	merged := make(map[string]*domain.SpendingByCategory)
	var order []string
	for _, c := range categories {
		displayName := resolveCategoryDisplay(c.Category, c.PFCPrimaryDisplay)
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

	// Re-sort on the RESOLVED label (#1626). The SQL ordered by category_display, which is the raw
	// PFC enum, so "FOOD_AND_DRINK" sorted where "Food And Drink" does not.
	sortSpendingByCategory(categories)

	for i := range categories {
		categories[i].Percentage = calculatePercentage(categories[i].Total, totalSpent)
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
		merchants[i].Percentage = calculatePercentage(merchants[i].Total, totalSpent)
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

// absFloat returns the absolute value of a float64 (used in period comparison calculations)
func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// calculatePercentage safely computes (value / total) * 100, returning 0 if total is 0
func calculatePercentage(value, total float64) float64 {
	if total > 0 {
		return (value / total) * 100
	}
	return 0
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
func (s *analyticsService) GetSavingsRateForPeriod(ctx context.Context, userID string, startDate time.Time, endDate time.Time, scope *domain.HouseholdScope) (*domain.SavingsRateResponse, error) {
	if endDate.Before(startDate) {
		return nil, fmt.Errorf("end_date cannot be before start_date")
	}

	row, err := s.analyticsRepo.GetSavingsRateForPeriod(ctx, userID, startDate, endDate, scope)
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
// months must be between 2 and 24. accountTypes and includeTransfers are forwarded to the repo layer.
func (s *analyticsService) GetMonthOverMonthSpendingChange(ctx context.Context, userID string, category string, months int, accountTypes []string, includeTransfers bool, scope *domain.HouseholdScope) (*domain.MonthOverMonthResponse, error) {
	if months < 2 || months > 24 {
		return nil, fmt.Errorf("months must be between 2 and 24")
	}

	startDate := time.Now().AddDate(0, -months, 0)
	filters := domain.SpendingFilters{
		AccountTypes:     accountTypes,
		IncludeTransfers: includeTransfers,
		HouseholdScope:   scope,
	}
	rows, err := s.analyticsRepo.GetMonthOverMonthSpending(ctx, userID, startDate, category, filters)
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
// merchantID is the canonical merchant UUID; merged source transactions are included via the
// effective merchant id expression in the repository layer (#1348 G2b).
func (s *analyticsService) GetCategoryBreakdownForMerchant(ctx context.Context, userID string, merchantID string, filters domain.SpendingFilters) (*domain.CategoryBreakdownForMerchantResponse, error) {
	rows, err := s.analyticsRepo.GetCategoryBreakdownForMerchant(ctx, userID, merchantID, filters)
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
			Percentage:        calculatePercentage(row.Total, totalSpend),
		}
		categories = append(categories, item)
	}

	return &domain.CategoryBreakdownForMerchantResponse{
		MerchantName: merchantID,
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
			Category: row.Category,
			// #1626: NOT PFCPrimaryDisplayName(row.Category). For a custom-categorised income
			// source the key is `custom:<uuid>`, which that function title-cases into a UUID.
			PFCPrimaryDisplay: resolveCategoryDisplay(row.Category, row.CategoryDisplay),
			Total:             row.Total,
			TransactionCount:  row.Count,
			Percentage:        calculatePercentage(row.Total, totalIncome),
		}
		sources = append(sources, item)
	}

	sortIncomeSources(sources)

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

// GetUnusualSpend returns transactions in [startDate, endDate] that exceed unusualSpendSigmaThreshold
// standard deviations above the per-category mean computed over the 3-month baseline window before startDate.
// AI-tool-callable (Issue #1215 PR3).
func (s *analyticsService) GetUnusualSpend(ctx context.Context, userID string, startDate, endDate time.Time) (*domain.UnusualSpendResponse, error) {
	if endDate.Before(startDate) {
		return nil, fmt.Errorf("end_date cannot be before start_date")
	}

	baselineStart := startDate.AddDate(0, -3, 0)
	rows, err := s.analyticsRepo.GetUnusualSpend(ctx, userID, baselineStart, startDate, endDate, unusualSpendSigmaThreshold)
	if err != nil {
		return nil, fmt.Errorf("failed to get unusual spend: %w", err)
	}

	items := make([]domain.UnusualSpendItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, domain.UnusualSpendItem{
			TransactionID:     row.TransactionID,
			MerchantName:      row.MerchantName,
			Amount:            row.Amount,
			Category:          row.Category,
			PFCPrimaryDisplay: PFCPrimaryDisplayName(row.Category),
			Date:              row.Date.Format("2006-01-02"),
			BaselineAvg:       row.BaselineAvg,
			SigmaAbove:        row.SigmaAbove,
		})
	}

	return &domain.UnusualSpendResponse{
		Items:          items,
		StartDate:      startDate.Format("2006-01-02"),
		EndDate:        endDate.Format("2006-01-02"),
		SigmaThreshold: unusualSpendSigmaThreshold,
	}, nil
}

// GetNetWorthTrend returns net worth bucketed by grouping (daily/weekly/monthly) over the trailing 12 months.
// Reuses BalanceSnapshotRepository.GetAggregatedHistory. Requires SetBalanceSnapshotRepo to be called first.
// AI-tool-callable (Issue #1215 PR3).
func (s *analyticsService) GetNetWorthTrend(ctx context.Context, userID string, grouping string, scope *domain.HouseholdScope) (*domain.NetWorthTrendResponse, error) {
	if s.balanceSnapshotRepo == nil {
		return nil, fmt.Errorf("balance snapshot repository not configured")
	}

	validGroupings := map[string]bool{"daily": true, "weekly": true, "monthly": true}
	if !validGroupings[grouping] {
		return nil, fmt.Errorf("grouping must be one of: daily, weekly, monthly")
	}

	endDate := time.Now()
	startDate := endDate.AddDate(-1, 0, 0)

	// Household mode fans out across every member's shared accounts instead of the
	// personal, single-user GetAggregatedHistory (Issue #1369 — Phase 5).
	var points []domain.BalanceHistoryPoint
	var err error
	if scope != nil {
		points, err = s.balanceSnapshotRepo.GetAggregatedHistoryForHousehold(ctx, userID, scope, startDate, endDate, nil)
	} else {
		points, err = s.balanceSnapshotRepo.GetAggregatedHistory(ctx, userID, startDate, endDate, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get balance history: %w", err)
	}

	// Aggregate per-day net worth: sum all account-type balances for each date.
	dailyTotals := aggregateDailyNetWorth(points)

	// Bucket daily totals into the requested grouping, taking the last available value.
	buckets := bucketNetWorth(dailyTotals, grouping, startDate, endDate)

	return &domain.NetWorthTrendResponse{
		Buckets:   buckets,
		Grouping:  grouping,
		StartDate: startDate.Format("2006-01-02"),
		EndDate:   endDate.Format("2006-01-02"),
	}, nil
}

// aggregateDailyNetWorth converts a slice of BalanceHistoryPoint into a map of date→totalNetWorth.
// Uses += to accumulate TypeBalance across all account types per date, defending against
// any future change in GetAggregatedHistory that returns multiple rows per date.
func aggregateDailyNetWorth(points []domain.BalanceHistoryPoint) map[time.Time]float64 {
	daily := make(map[time.Time]float64, len(points))
	for _, p := range points {
		day := p.Date.Truncate(24 * time.Hour)
		daily[day] += p.TotalBalance
	}
	return daily
}

// bucketNetWorth groups daily net-worth values into the requested grouping.
// For each bucket, the LAST available daily value within the bucket is used.
// Returns a slice of NetWorthBucket with change_abs and change_pct computed between buckets.
func bucketNetWorth(dailyTotals map[time.Time]float64, grouping string, startDate, endDate time.Time) []domain.NetWorthBucket {
	if len(dailyTotals) == 0 {
		return make([]domain.NetWorthBucket, 0)
	}

	// Collect and sort all dates with data.
	type dateval struct {
		d time.Time
		v float64
	}
	days := make([]dateval, 0, len(dailyTotals))
	for d, v := range dailyTotals {
		days = append(days, dateval{d, v})
	}
	sort.Slice(days, func(i, j int) bool { return days[i].d.Before(days[j].d) })

	// For each data point assign it to a bucket key (first day of the bucket period).
	bucketOf := func(t time.Time) time.Time {
		switch grouping {
		case "weekly":
			weekday := int(t.Weekday())
			if weekday == 0 {
				weekday = 7
			}
			return time.Date(t.Year(), t.Month(), t.Day()-(weekday-1), 0, 0, 0, 0, t.Location())
		case "daily":
			return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
		default: // monthly
			return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
		}
	}

	// Group: for each bucket keep the last daily value (latest date wins).
	type bucketEntry struct {
		key      time.Time
		lastDate time.Time
		netWorth float64
	}
	bucketMap := make(map[time.Time]*bucketEntry)
	var bucketOrder []time.Time

	for _, dv := range days {
		key := bucketOf(dv.d)
		if existing, ok := bucketMap[key]; ok {
			if dv.d.After(existing.lastDate) {
				existing.lastDate = dv.d
				existing.netWorth = dv.v
			}
		} else {
			bucketMap[key] = &bucketEntry{key: key, lastDate: dv.d, netWorth: dv.v}
			bucketOrder = append(bucketOrder, key)
		}
	}

	sort.Slice(bucketOrder, func(i, j int) bool { return bucketOrder[i].Before(bucketOrder[j]) })

	buckets := make([]domain.NetWorthBucket, 0, len(bucketOrder))
	for i, key := range bucketOrder {
		entry := bucketMap[key]
		b := domain.NetWorthBucket{
			Date:     key.Format("2006-01-02"),
			NetWorth: entry.netWorth,
		}
		if i > 0 {
			prev := bucketMap[bucketOrder[i-1]]
			changeAbs := entry.netWorth - prev.netWorth
			b.ChangeAbs = &changeAbs
			if prev.netWorth != 0 {
				changePct := changeAbs / math.Abs(prev.netWorth)
				b.ChangePct = &changePct
			}
		}
		buckets = append(buckets, b)
	}

	return buckets
}

// GetBudgetVsActual returns active budgets for the given period with computed utilisation metrics.
// period selects by the budget's configured period column (weekly/monthly/quarterly/yearly/custom).
// AI-tool-callable (Issue #1215 PR3).
func (s *analyticsService) GetBudgetVsActual(ctx context.Context, userID string, period string) (*domain.BudgetVsActualResponse, error) {
	if !domain.BudgetPeriod(period).IsValid() {
		return nil, fmt.Errorf("period must be one of: weekly, monthly, quarterly, yearly, custom")
	}

	rows, err := s.analyticsRepo.GetBudgetVsActual(ctx, userID, period)
	if err != nil {
		return nil, fmt.Errorf("failed to get budget vs actual: %w", err)
	}

	budgets := make([]domain.BudgetVsActualItem, 0, len(rows))
	var totalAllocated, totalSpent float64

	for _, row := range rows {
		remaining := row.AllocatedAmount - row.SpentAmount
		overBudget := row.SpentAmount > row.AllocatedAmount
		budgets = append(budgets, domain.BudgetVsActualItem{
			BudgetID:        row.BudgetID,
			BudgetName:      row.BudgetName,
			Period:          row.Period,
			AllocatedAmount: row.AllocatedAmount,
			SpentAmount:     row.SpentAmount,
			Remaining:       remaining,
			OverBudget:      overBudget,
			PctUsed:         calculatePercentage(row.SpentAmount, row.AllocatedAmount),
		})
		totalAllocated += row.AllocatedAmount
		totalSpent += row.SpentAmount
	}

	return &domain.BudgetVsActualResponse{
		Budgets:        budgets,
		TotalAllocated: totalAllocated,
		TotalSpent:     totalSpent,
		TotalRemaining: totalAllocated - totalSpent,
		Period:         period,
	}, nil
}
