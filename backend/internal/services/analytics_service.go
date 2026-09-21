package services

import (
	"context"
	"errors"
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

// ErrInvalidCategoryToken is returned when a `custom:<id>` category token's suffix is not a UUID.
//
// It exists because the suffix is CAST, not compared: GetCategoryTotalForPeriod resolves a custom
// category's name through `cc.id = $5::uuid` (#1626 Phase 2), so an unparseable suffix reaches
// Postgres and fails with invalid-input-syntax — which MapError classifies as a server error.
// Before that scalar subquery the token was only ever a text comparison and a malformed one simply
// matched nothing, so this is the guard for a regression the display fix introduced, not a
// pre-existing hole. Mapped to 400; see huma/errors/mapper.go.
var ErrInvalidCategoryToken = errors.New("category token suffix is not a valid UUID")

// AnalyticsService provides cash flow analytics functionality
// Issue #354: Cash Flow Analysis & Income/Expense Visualization
// Issue #953: Spending Reports
// Issue #1215: AI-tool-callable analytics endpoints
type AnalyticsService interface {
	// GetCashFlowSummary returns aggregated cash flow data for a date range
	// Includes income, expenses, net cash flow, and period comparison.
	// It answers for the REQUESTED range verbatim and echoes it — nothing is fabricated and
	// nothing is clamped, so the label always describes the window the numbers cover. Compare
	// GetCashFlowHistory, which clamps and discloses.
	GetCashFlowSummary(ctx context.Context, userID string, filters domain.CashFlowFilters) (*domain.CashFlowSummaryResponse, error)

	// GetCashFlowHistory returns time series cash flow data grouped by period
	// Used for rendering cash flow bar charts.
	// Because it GAP-FILLS, a requested end past today is clamped in both the query and the
	// response's end_date; the caller's own end is reported unchanged in requested_end_date, so
	// the two cash-flow endpoints agree about what was asked while each labels what it answered.
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

	// GetCategoryHierarchy returns the category tree (up to three levels) for one flow
	// direction, with roll-up totals and each node's own direct spend (#1626).
	GetCategoryHierarchy(ctx context.Context, userID string, filters domain.SpendingFilters, direction domain.CategoryDirection) (*domain.CategoryHierarchyResponse, error)

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

	// ⚠ NO CLAMP HERE, AND THAT IS A DECISION THIS METHOD MADE TWICE (final whole-branch review,
	// finding F2 — which reverses #1626 Phase 2's finding X7).
	//
	// X7 observed that for a future-ranged request this endpoint and GetCashFlowHistory echoed
	// different ranges for the same question, and clamped THIS ONE'S ECHO to today while leaving
	// its query on the requested range. That left the endpoint inconsistent WITH ITSELF: the label
	// said "through today", the number covered rows past today, and for an entirely-future range
	// responseEndDate floored the echo to StartDate so the response claimed [start, start] while
	// the aggregate covered [start, requestedEnd]. A label and a number describing different
	// windows is not a cosmetic improvement on two labels that differ.
	//
	// THE QUERY MUST NOT BE CLAMPED EITHER. History threads a clamp into its repository call
	// because it has gap-FILLING to stop: without it the query fabricates a zero-valued bucket for
	// every future period. This method fabricates nothing — it is one aggregate over rows that
	// exist — and transactions.date is a zoneless, effectively institution-local calendar date, so
	// a user ahead of UTC legitimately has rows dated later than the UTC day. Clamping the query
	// bound would DELETE those rows from their own total: real data loss, to fix a label.
	//
	// So: this endpoint queries the requested range and echoes the requested range. The two
	// endpoints still agree about the range they were ASKED for — GetCashFlowHistory reports it in
	// requested_end_date — and each labels the window it actually measured. Asserted by comparing
	// the echo with the filters the repository was handed, in
	// TestGetCashFlowSummary_EchoesTheWindowItActuallyQueried.

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
		StartDate:        filters.StartDate,
		StartDateDisplay: domain.FormatCalendarDayUTC(filters.StartDate),
		// The requested end, which is the end this aggregate measured — see the block above.
		EndDate:        filters.EndDate,
		EndDateDisplay: domain.FormatCalendarDayUTC(filters.EndDate),
		Income:         currentAgg.Income,
		Expenses:       currentAgg.Expenses,
		NetCashFlow:    currentAgg.NetCashFlow(),
		IncomeCount:    currentAgg.IncomeCount,
		ExpenseCount:   currentAgg.ExpenseCount,
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

	// Default to daily grouping if not specified. Deliberately BEFORE the clamp below: the
	// grouping describes the range the user ASKED for, so clamping first would silently
	// re-bucket a three-month request as weekly the moment most of it lies in the future.
	if filters.Grouping == "" {
		filters.Grouping = autoSelectGrouping(filters.StartDate, filters.EndDate)
	}

	// ONE effectiveEnd, computed here at the service boundary and threaded into the repository
	// call, so the same value bounds the query and the gap-filling (#1626 Phase 3, Defect 1).
	// Nothing downstream re-derives "now": a clamp buried in the fill loop would leave the
	// query, the fill and the response disagreeing about which day the answer stops on.
	//
	// requestedEnd is captured BEFORE the clamp overwrites it, because the response now DISCLOSES
	// the tail it dropped (final whole-branch review, finding F2). Without that, a caller
	// comparing this endpoint with GetCashFlowSummary — which queries and echoes the requested
	// range, having nothing to fabricate — sees two different end dates and no way to tell a
	// deliberate horizon from a disagreement.
	requestedEnd := filters.EndDate
	effectiveEnd := clampFillEnd(filters.EndDate, time.Now())
	filters.EndDate = effectiveEnd

	// Get grouped cash flow data
	groupedData, err := s.analyticsRepo.GetCashFlowGrouped(ctx, userID, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to get grouped cash flow: %w", err)
	}

	// Convert to response format
	periods := make([]domain.CashFlowPeriodData, len(groupedData))
	for i, data := range groupedData {
		periodEnd := clampPeriodEnd(data.PeriodStart, calculatePeriodEnd(data.PeriodStart, filters.Grouping), effectiveEnd)
		periods[i] = domain.CashFlowPeriodData{
			PeriodStart:        data.PeriodStart,
			PeriodStartDisplay: domain.FormatCalendarDayUTC(data.PeriodStart),
			PeriodEnd:          periodEnd,
			PeriodEndDisplay:   domain.FormatCalendarDayUTC(periodEnd),
			Income:             data.Income,
			Expenses:           data.Expenses,
			NetCashFlow:        data.Income - data.Expenses,
			TransactionCount:   data.TransactionCount,
		}
	}

	respEndDate := responseEndDate(filters.StartDate, effectiveEnd, periods)
	return &domain.CashFlowHistoryResponse{
		StartDate:               filters.StartDate,
		StartDateDisplay:        domain.FormatCalendarDayUTC(filters.StartDate),
		EndDate:                 respEndDate,
		EndDateDisplay:          domain.FormatCalendarDayUTC(respEndDate),
		RequestedEndDate:        requestedEnd,
		RequestedEndDateDisplay: domain.FormatCalendarDayUTC(requestedEnd),
		Grouping:                filters.Grouping,
		Periods:                 periods,
	}, nil
}

// clampFillEnd returns the last calendar day a response may FABRICATE data for: the requested
// end, or today (UTC) when the request reaches past it.
//
// UTC, not browser-local and not user-local: the API parses YYYY-MM-DD to UTC midnight and
// buckets on UTC calendar dates, and the frontend presets now resolve "today" the same way
// (shared/dates/presets.ts). A per-user IANA timezone exists (users.timezone, default "UTC")
// and moving every date boundary to user-local is a coherent follow-up; mixing the two is what
// produces an off-by-a-day disagreement, so both ends resolve UTC for now.
//
// This clamp is safe to thread into the query bound only because nothing downstream rebuilds the
// result from the clamped horizon alone — see fillMissingPeriods in
// repository/analytics_repository.go, which keeps every period its query returned even when the
// period lies beyond this end.
func clampFillEnd(requestedEnd, now time.Time) time.Time {
	u := now.UTC()
	today := time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
	if requestedEnd.After(today) {
		return today
	}
	return requestedEnd
}

// clampPeriodEnd stops a bucket advertising an end date the response has no data for: a weekly
// bucket beginning last Monday runs to Sunday, and on a Wednesday that Sunday has not happened.
// A bucket that begins beyond effectiveEnd keeps its own calculated end — it is real data past
// the horizon (see fillMissingPeriods), and clamping it would produce an end before its start.
func clampPeriodEnd(periodStart, periodEnd, effectiveEnd time.Time) time.Time {
	if periodStart.After(effectiveEnd) {
		return periodEnd
	}
	if periodEnd.After(effectiveEnd) {
		return effectiveEnd
	}
	return periodEnd
}

// responseEndDate reports the range the response actually answered for: the clamped end, widened
// to cover any real period lying beyond it rather than contradicting the periods it ships, and
// floored at the start date so start <= end survives a range that is entirely in the future.
func responseEndDate(startDate, effectiveEnd time.Time, periods []domain.CashFlowPeriodData) time.Time {
	end := effectiveEnd
	if end.Before(startDate) {
		end = startDate
	}
	for _, p := range periods {
		if p.PeriodEnd.After(end) {
			end = p.PeriodEnd
		}
	}
	return end
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
	// effective_category_refresh job catches up. Routed through mergeCategoryRows, like both
	// sibling reads, so the three cannot drift apart — this read's hand-rolled copy of the merge
	// had already dropped the display upgrade below (#1626, branch review I4).
	rows := make([]domain.SpendingByCategory, len(categories))
	for i, c := range categories {
		rows[i] = domain.SpendingByCategory{
			Category:          c.Category,
			PFCPrimaryDisplay: resolveCategoryDisplay(c.Category, c.PFCPrimaryDisplay),
			Total:             c.Total,
			TransactionCount:  c.TransactionCount,
		}
	}

	order := mergeCategoryRows(
		len(rows),
		func(i int) string { return rows[i].Category },
		func(dst, src int) {
			rows[dst].Total += rows[src].Total
			rows[dst].TransactionCount += rows[src].TransactionCount
			// First REAL name wins, whichever half carried it. The SQL orders total DESC, so the
			// survivor is simply the larger half — if that is the one whose effective_category_name
			// had not been refreshed yet, the placeholder would otherwise be the user's answer while
			// the real name sat in the very next row.
			if rows[dst].PFCPrimaryDisplay == unnamedCustomCategoryLabel && rows[src].PFCPrimaryDisplay != unnamedCustomCategoryLabel {
				rows[dst].PFCPrimaryDisplay = rows[src].PFCPrimaryDisplay
			}
		},
	)

	categories = make([]domain.SpendingByCategory, 0, len(order))
	var totalSpent float64
	for _, i := range order {
		categories = append(categories, rows[i])
		totalSpent += rows[i].Total
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
		// #1626: through resolveCategoryDisplay like every other category-shaped response. The old
		// `stored display, else pfcDisplayName` shape was wrong in BOTH directions here: the stored
		// display holds the RAW enum for a PFC row, and an empty one for a custom row fell through
		// to the registry, which title-cased the `custom:<uuid>` token into a UUID.
		var periodTotal float64
		cats := make([]domain.CategorySpending, 0)
		for _, tc := range topCategories {
			if spend, ok := catSpend[tc]; ok {
				display := resolveCategoryDisplay(tc, categoryDisplays[tc])
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

	// #1626 Phase 2 (X2): the repository casts a custom token's suffix to uuid, so a malformed one
	// must be refused HERE — reaching Postgres with it turns a bad request into a 500.
	if err := validateCategoryToken(category); err != nil {
		return nil, err
	}

	row, err := s.analyticsRepo.GetCategoryTotalForPeriod(ctx, userID, category, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("failed to get category total: %w", err)
	}

	return &domain.CategoryTotalResponse{
		Category:          category,
		PFCPrimaryDisplay: resolveCategoryDisplay(category, row.CategoryDisplay),
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
		// .UTC() before Format, because Format reads the value's Location: on a host behind UTC this
		// reported yesterday's date as "as of". Every other date this API emits is a UTC calendar
		// day (see clampFillEnd's docblock on why both ends resolve UTC for now), so this one has to
		// be as well. The lookbackStart above needs no .UTC(): it is compared as an instant
		// (`t.date >= $2`, no ::date cast) and is never formatted, so its Location is never read.
		AsOfDate: time.Now().UTC().Format("2006-01-02"),
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
			// change_pct is a PERCENTAGE (×100) to match the *_change_percent siblings (#1699 MCP-4).
			pct := ((row.Total - rows[i-1].Total) / rows[i-1].Total) * 100
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
			PFCPrimaryDisplay: resolveCategoryDisplay(row.Category, row.CategoryDisplay),
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

	// #1626 Phase 2, item 10. Merge by the STABLE KEY, exactly as the income and expense reads do.
	// Phase 1 added effective_category_name to this read's GROUP BY so a custom category could be
	// NAMED rather than showing a UUID, but did not add the merge — and that column is refreshed
	// asynchronously after a rename, so mid-refresh one category arrives as TWO rows. See
	// mergeCategoryRows for why merging alone is not enough.
	items := make([]domain.CategoryBreakdownItem, len(rows))
	for i, row := range rows {
		items[i] = domain.CategoryBreakdownItem{
			Category:          row.Category,
			PFCPrimaryDisplay: resolveCategoryDisplay(row.Category, row.CategoryDisplay),
			Total:             row.Total,
			TransactionCount:  row.Count,
		}
	}

	order := mergeCategoryRows(
		len(items),
		func(i int) string { return items[i].Category },
		func(dst, src int) {
			items[dst].Total += items[src].Total
			items[dst].TransactionCount += items[src].TransactionCount
			// First real name wins; a stale placeholder must not blank an established one.
			if items[dst].PFCPrimaryDisplay == unnamedCustomCategoryLabel && items[src].PFCPrimaryDisplay != unnamedCustomCategoryLabel {
				items[dst].PFCPrimaryDisplay = items[src].PFCPrimaryDisplay
			}
		},
	)

	categories := make([]domain.CategoryBreakdownItem, 0, len(order))
	for _, i := range order {
		item := items[i]
		// From the MERGED total. Computed before merging, each half of a split category reports
		// half its real share, and the halves together overstate the number of categories.
		item.Percentage = calculatePercentage(item.Total, totalSpend)
		categories = append(categories, item)
	}

	// Re-sort: a merged 120 must outrank an unmerged 100, and the rows arrive in the SQL's
	// pre-merge order, where each half was smaller than its rival.
	sortCategoryBreakdownItems(categories)

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

	// Merge by the STABLE KEY, mirroring the expense read. The SQL groups by key AND display, and
	// effective_category_name is refreshed asynchronously after a rename — so mid-window one custom
	// category yields TWO rows, an old-name one and a new-name one. Emitting both would split the
	// user's income source in half and halve each percentage.
	merged := make(map[string]*domain.IncomeSourceItem, len(rows))
	order := make([]string, 0, len(rows))
	for _, row := range rows {
		if existing, ok := merged[row.Category]; ok {
			existing.Total += row.Total
			existing.TransactionCount += row.Count
			// First non-empty name wins; a stale empty display must not blank an established name.
			if existing.PFCPrimaryDisplay == unnamedCustomCategoryLabel && row.CategoryDisplay != "" {
				existing.PFCPrimaryDisplay = resolveCategoryDisplay(row.Category, row.CategoryDisplay)
			}
			continue
		}
		merged[row.Category] = &domain.IncomeSourceItem{
			Category: row.Category,
			// #1626: NOT PFCPrimaryDisplayName(row.Category). For a custom-categorised income
			// source the key is `custom:<uuid>`, which that function title-cases into a UUID.
			PFCPrimaryDisplay: resolveCategoryDisplay(row.Category, row.CategoryDisplay),
			Total:             row.Total,
			TransactionCount:  row.Count,
		}
		order = append(order, row.Category)
	}

	sources := make([]domain.IncomeSourceItem, 0, len(order))
	for _, key := range order {
		item := *merged[key]
		item.Percentage = calculatePercentage(item.Total, totalIncome)
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
		PFCPrimaryDisplay: resolveCategoryDisplay(row.Category, row.CategoryDisplay),
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
			PFCPrimaryDisplay: resolveCategoryDisplay(row.Category, row.CategoryDisplay),
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

	// ⚠ .UTC() IS NOT DECORATION HERE, and it is the only one of this file's `time.Now()` sites
	// where it changes an answer. `time.Now().UTC()` is the same INSTANT with a different Location,
	// so it matters exactly where the Location is read — and this value's Location is read twice.
	// (1) GetAggregatedHistory bounds on `(bs.snapshot_at AT TIME ZONE 'UTC')::date <= $3::date`
	// (and its household twin on `<= $5::date`), so pgx encodes this as a `date` using the value's
	// OWN Location's calendar day; on a host behind UTC, snapshots written "today" in UTC fall
	// outside a range ending "today" and today's net worth is invisible until local midnight.
	// (2) both bounds are echoed as `Format("2006-01-02")` below, which is Location-dependent too.
	// Production runs UTC so this was latent, not live.
	endDate := time.Now().UTC()
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
				// change_pct is a PERCENTAGE (×100) to match the *_change_percent siblings (#1699 MCP-4).
				changePct := (changeAbs / math.Abs(prev.netWorth)) * 100
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
