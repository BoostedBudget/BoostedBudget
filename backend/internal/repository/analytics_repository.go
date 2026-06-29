package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
)

// AnalyticsRepository defines the interface for analytics data access.
// Provides aggregated financial data for cash flow analysis and other analytics features.
type AnalyticsRepository interface {
	// GetCashFlowAggregation returns aggregated income and expenses for a date range
	GetCashFlowAggregation(ctx context.Context, userID string, filters domain.CashFlowFilters) (*domain.CashFlowAggregation, error)

	// GetCashFlowGrouped returns cash flow data grouped by period (daily/weekly/monthly)
	GetCashFlowGrouped(ctx context.Context, userID string, filters domain.CashFlowFilters) ([]domain.CashFlowGroupedData, error)

	// GetSpendingByCategory returns expense totals grouped by effective category.
	// When filters.Category is set, returns subcategory breakdown for that primary category.
	GetSpendingByCategory(ctx context.Context, userID string, filters domain.SpendingFilters) ([]domain.SpendingByCategory, error)

	// GetSpendingByMerchant returns expense totals grouped by merchant, ordered by total desc.
	GetSpendingByMerchant(ctx context.Context, userID string, filters domain.SpendingFilters, limit int) ([]domain.SpendingByMerchant, error)

	// GetSpendingTrends returns monthly expense totals per category for trend analysis.
	GetSpendingTrends(ctx context.Context, userID string, filters domain.SpendingFilters) ([]domain.SpendingTrendRow, error)

	// GetCategoryTotalForPeriod returns the total expense amount for a specific category in a date range.
	// Returns zero Total and TransactionCount when no matching transactions exist.
	// AI-tool-callable: one DB round-trip, token-efficient summary (Issue #1087)
	GetCategoryTotalForPeriod(ctx context.Context, userID string, category string, startDate time.Time, endDate time.Time) (*domain.CategoryTotalRow, error)

	// GetTopExpense returns the single largest expense transaction in a date range.
	// Returns nil when no expense transactions exist in the period.
	// AI-tool-callable: one DB round-trip, returns structured summary (Issue #1087)
	GetTopExpense(ctx context.Context, userID string, startDate time.Time, endDate time.Time) (*domain.TopExpenseRow, error)

	// GetAverageMonthlySpend returns the total expense amount for computing average monthly spend.
	// Single DB round-trip; average computed in the service layer (Issue #1215).
	GetAverageMonthlySpend(ctx context.Context, userID string, filters domain.SpendingFilters) (*domain.AverageMonthlySpendRow, error)

	// GetRecurringSubscriptions returns heuristic recurring charge candidates over the lookback window.
	// Single DB round-trip using window functions; complex SQL requires real-DB integration tests (Issue #1215).
	GetRecurringSubscriptions(ctx context.Context, userID string, lookbackStart time.Time) ([]domain.RecurringSubscriptionRow, error)

	// GetSavingsRateForPeriod returns income and expense totals for computing savings rate.
	// Single DB round-trip; savings rate computed in the service layer (Issue #1215 PR2).
	GetSavingsRateForPeriod(ctx context.Context, userID string, startDate time.Time, endDate time.Time) (*domain.SavingsRateRow, error)

	// GetMonthOverMonthSpending returns monthly expense totals grouped by month.
	// Single DB round-trip using DATE_TRUNC GROUP BY. filters.AccountTypes and
	// filters.IncludeTransfers are honoured; StartDate/EndDate are computed from months. Issue #1215 PR2/gaps.
	GetMonthOverMonthSpending(ctx context.Context, userID string, startDate time.Time, category string, filters domain.SpendingFilters) ([]domain.MonthOverMonthBucketRow, error)

	// GetLargestTransactionsForPeriod returns the N largest expense transactions in a date range.
	// Single DB round-trip; limit enforced in SQL (Issue #1215 PR2).
	GetLargestTransactionsForPeriod(ctx context.Context, userID string, filters domain.SpendingFilters, limit int) ([]domain.LargestTransactionRow, error)

	// GetCategoryBreakdownForMerchant returns expense totals grouped by category for a specific merchant.
	// Filters by merchant_id (effective canonical) to include merged source transactions (#1348 G2b).
	// Single DB round-trip (Issue #1215 PR2).
	GetCategoryBreakdownForMerchant(ctx context.Context, userID string, merchantID string, filters domain.SpendingFilters) ([]domain.CategoryBreakdownForMerchantRow, error)

	// GetIncomeSourceBreakdown returns income (amount < 0) totals grouped by category.
	// Single DB round-trip (Issue #1215 PR2).
	GetIncomeSourceBreakdown(ctx context.Context, userID string, filters domain.SpendingFilters) ([]domain.IncomeSourceRow, error)

	// GetUnusualSpend returns transactions in [startDate, endDate] that exceed sigmaThreshold
	// standard deviations above the per-category mean computed over the 3-month baseline window
	// immediately before startDate. Integration test required (STDDEV_POP CTE). Issue #1215 PR3.
	GetUnusualSpend(ctx context.Context, userID string, baselineStart, startDate, endDate time.Time, sigmaThreshold float64) ([]domain.UnusualSpendRow, error)

	// GetBudgetVsActual returns active budgets filtered by period with their allocated and spent amounts.
	// period selects budgets by their configured budget period (not a date window). Issue #1215 PR3.
	GetBudgetVsActual(ctx context.Context, userID, period string) ([]domain.BudgetVsActualRow, error)
}

// analyticsRepository implements AnalyticsRepository using PostgreSQL
type analyticsRepository struct {
	db DB
}

// NewAnalyticsRepository creates a new AnalyticsRepository instance
func NewAnalyticsRepository(db DB) AnalyticsRepository {
	return &analyticsRepository{db: db}
}


// GetCashFlowAggregation returns aggregated income and expenses for a date range.
// Income: amount < 0 (inflow/credit)
// Expenses: amount > 0 (outflow/debit, PFCv2)
// Filters out pending transactions by default.
// Can optionally filter by account types and include/exclude transfers.
func (r *analyticsRepository) GetCashFlowAggregation(ctx context.Context, userID string, filters domain.CashFlowFilters) (*domain.CashFlowAggregation, error) {
	observability.AccountsReplacedFiltered.WithLabelValues("analytics").Inc()

	args := []interface{}{userID, filters.StartDate, filters.EndDate}
	argIndex := 4

	query := `
		SELECT
			COALESCE(SUM(CASE WHEN t.amount < 0 THEN ABS(t.amount) ELSE 0 END), 0) as income,
			COUNT(CASE WHEN t.amount < 0 THEN 1 END) as income_count,
			COALESCE(SUM(CASE WHEN t.amount > 0 THEN t.amount ELSE 0 END), 0) as expenses,
			COUNT(CASE WHEN t.amount > 0 THEN 1 END) as expense_count
		FROM transactions t
		LEFT JOIN accounts a ON t.account_id = a.id
		WHERE t.user_id = $1
		  AND t.date >= $2
		  AND t.date <= $3
		  AND t.pending = false
	`

	// Optional filter: account types
	if len(filters.AccountTypes) > 0 {
		placeholders := buildPlaceholders(len(filters.AccountTypes), argIndex)
		for _, at := range filters.AccountTypes {
			args = append(args, at)
		}
		argIndex += len(filters.AccountTypes)
		query += fmt.Sprintf(" AND a.type IN (%s)", strings.Join(placeholders, ", "))
	}

	if !filters.IncludeTransfers {
		query += ` AND ` + effectiveTransferExpr() + ` NOT LIKE 'TRANSFER%'`
	}

	var agg domain.CashFlowAggregation
	err := r.db.QueryRow(ctx, query, args...).Scan(
		&agg.Income,
		&agg.IncomeCount,
		&agg.Expenses,
		&agg.ExpenseCount,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get cash flow aggregation: %w", err)
	}

	return &agg, nil
}

// GetCashFlowGrouped returns cash flow data grouped by period (daily/weekly/monthly).
// Uses PostgreSQL DATE_TRUNC for period grouping.
// Returns data ordered by period_start ASC for chart display.
func (r *analyticsRepository) GetCashFlowGrouped(ctx context.Context, userID string, filters domain.CashFlowFilters) ([]domain.CashFlowGroupedData, error) {
	observability.AccountsReplacedFiltered.WithLabelValues("analytics").Inc()

	var truncInterval string
	switch filters.Grouping {
	case "daily":
		truncInterval = "day"
	case "weekly":
		truncInterval = "week"
	case "monthly":
		truncInterval = "month"
	default:
		truncInterval = "day"
	}

	args := []interface{}{userID, filters.StartDate, filters.EndDate}
	argIndex := 4

	query := fmt.Sprintf(`
		SELECT
			DATE_TRUNC('%s', t.date)::date as period_start,
			COALESCE(SUM(CASE WHEN t.amount < 0 THEN ABS(t.amount) ELSE 0 END), 0) as income,
			COALESCE(SUM(CASE WHEN t.amount > 0 THEN t.amount ELSE 0 END), 0) as expenses,
			COUNT(*) as transaction_count
		FROM transactions t
		LEFT JOIN accounts a ON t.account_id = a.id
		WHERE t.user_id = $1
		  AND t.date >= $2
		  AND t.date <= $3
		  AND t.pending = false
	`, truncInterval)

	if len(filters.AccountTypes) > 0 {
		placeholders := buildPlaceholders(len(filters.AccountTypes), argIndex)
		for _, at := range filters.AccountTypes {
			args = append(args, at)
		}
		argIndex += len(filters.AccountTypes)
		query += fmt.Sprintf(" AND a.type IN (%s)", strings.Join(placeholders, ", "))
	}

	if !filters.IncludeTransfers {
		query += ` AND ` + effectiveTransferExpr() + ` NOT LIKE 'TRANSFER%'`
	}

	query += fmt.Sprintf(`
		GROUP BY DATE_TRUNC('%s', t.date)
		ORDER BY period_start ASC
	`, truncInterval)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get grouped cash flow: %w", err)
	}
	defer rows.Close()

	var results []domain.CashFlowGroupedData
	for rows.Next() {
		var data domain.CashFlowGroupedData
		err := rows.Scan(
			&data.PeriodStart,
			&data.Income,
			&data.Expenses,
			&data.TransactionCount,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan cash flow data: %w", err)
		}
		results = append(results, data)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating cash flow rows: %w", err)
	}

	// Fill in missing periods with zeros for consistent chart display
	results = fillMissingPeriods(results, filters.StartDate, filters.EndDate, filters.Grouping)

	return results, nil
}

// fillMissingPeriods fills in missing periods with zero values for chart continuity
func fillMissingPeriods(data []domain.CashFlowGroupedData, startDate, endDate time.Time, grouping string) []domain.CashFlowGroupedData {
	if len(data) == 0 {
		return generateEmptyPeriods(startDate, endDate, grouping)
	}

	periodMap := make(map[string]domain.CashFlowGroupedData)
	for _, d := range data {
		key := d.PeriodStart.Format("2006-01-02")
		periodMap[key] = d
	}

	var result []domain.CashFlowGroupedData
	current := truncateDate(startDate, grouping)
	end := endDate

	for !current.After(end) {
		key := current.Format("2006-01-02")
		if existing, ok := periodMap[key]; ok {
			result = append(result, existing)
		} else {
			result = append(result, domain.CashFlowGroupedData{
				PeriodStart:      current,
				Income:           0,
				Expenses:         0,
				TransactionCount: 0,
			})
		}
		current = advancePeriod(current, grouping)
	}

	return result
}

// generateEmptyPeriods creates a list of zero-value periods
func generateEmptyPeriods(startDate, endDate time.Time, grouping string) []domain.CashFlowGroupedData {
	var result []domain.CashFlowGroupedData
	current := truncateDate(startDate, grouping)

	for !current.After(endDate) {
		result = append(result, domain.CashFlowGroupedData{
			PeriodStart:      current,
			Income:           0,
			Expenses:         0,
			TransactionCount: 0,
		})
		current = advancePeriod(current, grouping)
	}

	return result
}

// truncateDate truncates a date to the start of its period (used for both cash flow and net worth bucketing)
func truncateDate(t time.Time, grouping string) time.Time {
	switch grouping {
	case "weekly":
		// Truncate to start of week (Monday)
		weekday := int(t.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		return time.Date(t.Year(), t.Month(), t.Day()-(weekday-1), 0, 0, 0, 0, t.Location())
	case "monthly":
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
	default: // daily
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	}
}

// advancePeriod advances the date by one period (used for both cash flow and net worth bucketing)
func advancePeriod(t time.Time, grouping string) time.Time {
	switch grouping {
	case "weekly":
		return t.AddDate(0, 0, 7)
	case "monthly":
		return t.AddDate(0, 1, 0)
	default: // daily
		return t.AddDate(0, 0, 1)
	}
}

// effectiveCategoryExpr returns the SQL expression for the effective PFC category.
// Priority: pfc override → raw pfc → 'UNCATEGORIZED'. Custom overrides are expressed
// as 'custom:<uuid>' so they group separately from PFC categories.
func effectiveCategoryExpr(subcategory bool) string {
	if subcategory {
		return `CASE
			WHEN t.category_override_kind = 'custom' THEN 'custom:' || t.custom_category_override_id::text
			WHEN t.category_override_kind = 'pfc'    THEN COALESCE(t.pfc_detailed_override, t.pfc_primary_override, 'UNCATEGORIZED')
			ELSE                                          COALESCE(t.pfc_detailed, t.pfc_primary, 'UNCATEGORIZED')
		END`
	}
	return `CASE
		WHEN t.category_override_kind = 'custom' THEN 'custom:' || t.custom_category_override_id::text
		WHEN t.category_override_kind = 'pfc'    THEN COALESCE(t.pfc_primary_override, 'UNCATEGORIZED')
		ELSE                                          COALESCE(t.pfc_primary, 'UNCATEGORIZED')
	END`
}

// effectiveCategoryDisplayExpr returns the SQL expression for the human-readable display name
// of the effective category. Reads the denormalized effective_category_name column directly,
// eliminating the need for a LEFT JOIN on custom_categories.
func effectiveCategoryDisplayExpr() string {
	return `t.effective_category_name`
}

// effectiveTransferExpr returns the SQL expression for the effective PFC primary used
// in transfer filtering. Reads the denormalized effective_pfc_primary column directly.
// effective_pfc_primary is NULL for custom-kind rows (never match TRANSFER%), so
// COALESCE to ” ensures the NOT LIKE filter still works correctly.
func effectiveTransferExpr() string {
	return `COALESCE(t.effective_pfc_primary, '')`
}

// buildPlaceholders creates numbered placeholders ($1, $2, ...) for parameterized query building
func buildPlaceholders(count, startIndex int) []string {
	placeholders := make([]string, count)
	for i := 0; i < count; i++ {
		placeholders[i] = fmt.Sprintf("$%d", startIndex+i)
	}
	return placeholders
}

// buildSpendingFilter returns the common WHERE clause args and starting argIndex for spending queries.
func buildSpendingFilter(filters domain.SpendingFilters, baseArgs []interface{}) (string, []interface{}, int) {
	args := baseArgs
	argIndex := len(args) + 1
	clause := ""

	if len(filters.AccountTypes) > 0 {
		placeholders := buildPlaceholders(len(filters.AccountTypes), argIndex)
		for _, at := range filters.AccountTypes {
			args = append(args, at)
		}
		argIndex += len(filters.AccountTypes)
		clause += fmt.Sprintf(" AND a.type IN (%s)", strings.Join(placeholders, ", "))
	}

	if !filters.IncludeTransfers {
		clause += ` AND ` + effectiveTransferExpr() + ` NOT LIKE 'TRANSFER%'`
	}

	// Issue #1203: scope to specific custom categories. With RollUpChildren a parent
	// custom id also matches its children (parent_id). user_id cast to $1::uuid in subquery.
	if len(filters.CustomCategoryIDs) > 0 {
		placeholders := buildPlaceholders(len(filters.CustomCategoryIDs), argIndex)
		for _, id := range filters.CustomCategoryIDs {
			args = append(args, id)
		}
		argIndex += len(filters.CustomCategoryIDs)
		idList := strings.Join(placeholders, ", ")
		if filters.RollUpChildren {
			clause += fmt.Sprintf(
				" AND t.category_override_kind = 'custom' AND t.custom_category_override_id IN ("+
					"SELECT id FROM custom_categories WHERE user_id = $1::uuid AND (id::text IN (%s) OR parent_id::text IN (%s)))",
				idList, idList,
			)
		} else {
			clause += fmt.Sprintf(
				" AND t.category_override_kind = 'custom' AND t.custom_category_override_id::text IN (%s)",
				idList,
			)
		}
	}

	return clause, args, argIndex
}

// GetSpendingByCategory returns expense totals grouped by effective primary category.
// When filters.Category is non-empty, returns the detailed subcategory breakdown.
func (r *analyticsRepository) GetSpendingByCategory(ctx context.Context, userID string, filters domain.SpendingFilters) ([]domain.SpendingByCategory, error) {
	observability.AccountsReplacedFiltered.WithLabelValues("analytics").Inc()
	isDrillDown := filters.Category != ""
	catExpr := effectiveCategoryExpr(isDrillDown)

	args := []interface{}{userID, filters.StartDate, filters.EndDate}

	var categoryFilter string
	if isDrillDown {
		args = append(args, filters.Category)
		primaryCatExpr := effectiveCategoryExpr(false)
		categoryFilter = fmt.Sprintf(" AND %s = $4", primaryCatExpr)
	}

	extraClause, args, _ := buildSpendingFilter(filters, args)
	displayExpr := effectiveCategoryDisplayExpr()

	query := fmt.Sprintf(`
		SELECT
			%s as category,
			%s as category_display,
			COALESCE(SUM(ABS(t.amount)), 0) as total,
			COUNT(*) as transaction_count
		FROM transactions t
		LEFT JOIN accounts a ON t.account_id = a.id
		WHERE t.user_id = $1
		  AND t.date >= $2
		  AND t.date <= $3
		  AND t.amount > 0
		  AND t.pending = false
		%s%s
		GROUP BY category, category_display
		ORDER BY total DESC
	`, catExpr, displayExpr, categoryFilter, extraClause)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get spending by category: %w", err)
	}
	defer rows.Close()

	var results []domain.SpendingByCategory
	for rows.Next() {
		var row domain.SpendingByCategory
		if err := rows.Scan(&row.Category, &row.PFCPrimaryDisplay, &row.Total, &row.TransactionCount); err != nil {
			return nil, fmt.Errorf("failed to scan spending by category: %w", err)
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating spending by category rows: %w", err)
	}

	return results, nil
}

// GetSpendingByMerchant returns expense totals grouped by effective merchant, ordered by total descending.
// Merchants renamed or merged by the user are grouped under their canonical (target) display name.
// Groups by a synthetic key: 'merchant:<id>', 'raw:<name>', or 'unknown' — so renamed/merged variants
// collapse into one row. merchant_id is NULL for unlinked/unknown groups (preserves "not clickable" frontend behavior).
func (r *analyticsRepository) GetSpendingByMerchant(ctx context.Context, userID string, filters domain.SpendingFilters, limit int) ([]domain.SpendingByMerchant, error) {
	observability.AccountsReplacedFiltered.WithLabelValues("analytics").Inc()

	args := []interface{}{userID, filters.StartDate, filters.EndDate}
	extraClause, args, argIndex := buildSpendingFilter(filters, args)

	args = append(args, limit)
	limitPlaceholder := fmt.Sprintf("$%d", argIndex)

	query := `
		SELECT
			MAX(COALESCE(mno_t.display_name, NULLIF(m2.normalized_name, ''), m2.name, NULLIF(t.merchant_name, ''), 'Unknown')) AS merchant_name,
			MAX(COALESCE(mno.merged_into_merchant_id, t.merchant_id)::text)                                                    AS merchant_id,
			COALESCE(SUM(ABS(t.amount)), 0)                                                                                    AS total,
			COUNT(*)                                                                                                            AS transaction_count
		FROM transactions t
		LEFT JOIN accounts a ON t.account_id = a.id
		` + merchantSortJoins + `
		WHERE t.user_id = $1::uuid
		  AND t.date >= $2
		  AND t.date <= $3
		  AND t.amount > 0
		  AND t.pending = false
		` + extraClause + `
		GROUP BY
		  CASE
		    WHEN t.merchant_id IS NOT NULL
		      THEN 'merchant:' || COALESCE(mno.merged_into_merchant_id, t.merchant_id)::text
		    WHEN NULLIF(t.merchant_name, '') IS NOT NULL
		      THEN 'raw:' || t.merchant_name
		    ELSE 'unknown'
		  END
		ORDER BY total DESC
		LIMIT ` + limitPlaceholder

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get spending by merchant: %w", err)
	}
	defer rows.Close()

	var results []domain.SpendingByMerchant
	for rows.Next() {
		var row domain.SpendingByMerchant
		if err := rows.Scan(&row.MerchantName, &row.MerchantID, &row.Total, &row.TransactionCount); err != nil {
			return nil, fmt.Errorf("failed to scan spending by merchant: %w", err)
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating spending by merchant rows: %w", err)
	}

	return results, nil
}

// GetSpendingTrends returns monthly expense totals per category for trend chart data.
// Results are ordered by period_start ASC, total DESC.
func (r *analyticsRepository) GetSpendingTrends(ctx context.Context, userID string, filters domain.SpendingFilters) ([]domain.SpendingTrendRow, error) {
	observability.AccountsReplacedFiltered.WithLabelValues("analytics").Inc()

	catExpr := effectiveCategoryExpr(false)
	args := []interface{}{userID, filters.StartDate, filters.EndDate}
	extraClause, args, _ := buildSpendingFilter(filters, args)

	displayExprTrends := effectiveCategoryDisplayExpr()
	query := fmt.Sprintf(`
		SELECT
			DATE_TRUNC('month', t.date)::date as period_start,
			%s as category,
			%s as category_display,
			COALESCE(SUM(ABS(t.amount)), 0) as total
		FROM transactions t
		LEFT JOIN accounts a ON t.account_id = a.id
		WHERE t.user_id = $1
		  AND t.date >= $2
		  AND t.date <= $3
		  AND t.amount > 0
		  AND t.pending = false
		%s
		GROUP BY period_start, category, category_display
		ORDER BY period_start ASC, total DESC
	`, catExpr, displayExprTrends, extraClause)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get spending trends: %w", err)
	}
	defer rows.Close()

	var results []domain.SpendingTrendRow
	for rows.Next() {
		var row domain.SpendingTrendRow
		if err := rows.Scan(&row.PeriodStart, &row.Category, &row.CategoryDisplay, &row.Total); err != nil {
			return nil, fmt.Errorf("failed to scan spending trend row: %w", err)
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating spending trends rows: %w", err)
	}

	return results, nil
}

// GetCategoryTotalForPeriod returns the total expense amount for a specific effective category.
// Matches both PFC primary categories (e.g. "FOOD_AND_DRINK") and custom categories ("custom:<uuid>").
// Expenses are amount > 0 (PFCv2 convention). Returns zero row when no matching transactions exist.
func (r *analyticsRepository) GetCategoryTotalForPeriod(ctx context.Context, userID string, category string, startDate time.Time, endDate time.Time) (*domain.CategoryTotalRow, error) {
	catExpr := effectiveCategoryExpr(false)

	query := fmt.Sprintf(`
		SELECT
			COALESCE(SUM(t.amount), 0) as total,
			COUNT(*)                   as transaction_count
		FROM transactions t
		WHERE t.user_id = $1
		  AND t.date >= $2
		  AND t.date <= $3
		  AND t.amount > 0
		  AND t.pending = false
		  AND (%s) = $4
	`, catExpr)

	var row domain.CategoryTotalRow
	err := r.db.QueryRow(ctx, query, userID, startDate, endDate, category).Scan(
		&row.Total,
		&row.TransactionCount,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get category total for period: %w", err)
	}
	return &row, nil
}

// GetTopExpense returns the single largest expense transaction (by amount) in a date range.
// Returns nil when no expense transactions exist in the period.
func (r *analyticsRepository) GetTopExpense(ctx context.Context, userID string, startDate time.Time, endDate time.Time) (*domain.TopExpenseRow, error) {
	catExpr := effectiveCategoryExpr(false)

	query := fmt.Sprintf(`
		SELECT
			t.id,
			COALESCE(t.merchant_name, '') as merchant_name,
			t.amount,
			(%s)                          as category,
			t.date
		FROM transactions t
		WHERE t.user_id = $1
		  AND t.date >= $2
		  AND t.date <= $3
		  AND t.amount > 0
		  AND t.pending = false
		ORDER BY t.amount DESC
		LIMIT 1
	`, catExpr)

	var row domain.TopExpenseRow
	err := r.db.QueryRow(ctx, query, userID, startDate, endDate).Scan(
		&row.TransactionID,
		&row.MerchantName,
		&row.Amount,
		&row.Category,
		&row.Date,
	)
	if err != nil {
		if isNotFoundError(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get top expense: %w", err)
	}
	return &row, nil
}

// GetAverageMonthlySpend returns the total expense amount for computing average monthly spend.
// Single DB round-trip; average computed in the service layer (Issue #1215).
func (r *analyticsRepository) GetAverageMonthlySpend(ctx context.Context, userID string, filters domain.SpendingFilters) (*domain.AverageMonthlySpendRow, error) {
	observability.AccountsReplacedFiltered.WithLabelValues("analytics").Inc()

	args := []interface{}{userID, filters.StartDate, filters.EndDate}
	filterClause, args, _ := buildSpendingFilter(filters, args)

	query := fmt.Sprintf(`
		SELECT COALESCE(SUM(t.amount), 0) as total
		FROM transactions t
		LEFT JOIN accounts a ON t.account_id = a.id
		WHERE t.user_id = $1
		  AND t.date >= $2
		  AND t.date <= $3
		  AND t.amount > 0
		  AND t.pending = false
		  %s
	`, filterClause)

	var row domain.AverageMonthlySpendRow
	err := r.db.QueryRow(ctx, query, args...).Scan(&row.Total)
	if err != nil {
		return nil, fmt.Errorf("failed to get average monthly spend: %w", err)
	}
	return &row, nil
}

// GetRecurringSubscriptions returns heuristic recurring charge candidates over the lookback window.
// Uses window functions (LAG) to compute per-merchant inter-charge gaps; filters to monthly cadence
// (avg gap 25-40 days, stddev < 10, ≥3 occurrences). Single DB round-trip. Issue #1215.
func (r *analyticsRepository) GetRecurringSubscriptions(ctx context.Context, userID string, lookbackStart time.Time) ([]domain.RecurringSubscriptionRow, error) {
	observability.AccountsReplacedFiltered.WithLabelValues("analytics").Inc()

	const query = `
		WITH merchant_gaps AS (
			SELECT
				COALESCE(t.merchant_name, '') AS merchant_name,
				ROUND(t.amount::numeric, 2)   AS rounded_amount,
				t.date,
				LAG(t.date) OVER (
					PARTITION BY COALESCE(t.merchant_name, ''), ROUND(t.amount::numeric, 2)
					ORDER BY t.date
				) AS prev_date
			FROM transactions t
			WHERE t.user_id = $1
			  AND t.amount > 0
			  AND t.pending = false
			  AND t.date >= $2
			  AND COALESCE(t.effective_pfc_primary, '') NOT LIKE 'TRANSFER%'
		),
		merchant_stats AS (
			SELECT
				merchant_name,
				rounded_amount                                                          AS amount,
				COUNT(*)                                                                AS occurrence_count,
				MAX(date)                                                               AS last_charge_date,
				AVG((date - prev_date)::integer) FILTER (WHERE prev_date IS NOT NULL)  AS avg_gap_days,
				STDDEV_POP((date - prev_date)::integer) FILTER (WHERE prev_date IS NOT NULL) AS stddev_gap_days
			FROM merchant_gaps
			GROUP BY merchant_name, rounded_amount
			HAVING COUNT(*) >= 3
		)
		SELECT
			merchant_name,
			amount,
			occurrence_count,
			avg_gap_days,
			COALESCE(stddev_gap_days, 0)                                               AS stddev_gap_days,
			last_charge_date,
			last_charge_date + ROUND(avg_gap_days)::integer AS next_expected_date
		FROM merchant_stats
		WHERE avg_gap_days BETWEEN 25 AND 40
		  AND COALESCE(stddev_gap_days, 0) < 10
		ORDER BY amount DESC
	`

	rows, err := r.db.Query(ctx, query, userID, lookbackStart)
	if err != nil {
		return nil, fmt.Errorf("failed to get recurring subscriptions: %w", err)
	}
	defer rows.Close()

	var results []domain.RecurringSubscriptionRow
	for rows.Next() {
		var row domain.RecurringSubscriptionRow
		if err := rows.Scan(
			&row.MerchantName,
			&row.Amount,
			&row.OccurrenceCount,
			&row.AvgGapDays,
			&row.StddevGapDays,
			&row.LastChargeDate,
			&row.NextExpectedDate,
		); err != nil {
			return nil, fmt.Errorf("failed to scan recurring subscription row: %w", err)
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating recurring subscription rows: %w", err)
	}

	return results, nil
}

// GetSavingsRateForPeriod returns income and expense totals for computing savings rate.
// Income: ABS(amount < 0), Expenses: amount > 0. Excludes pending transactions.
// No transfer filter: TRANSFER_IN transactions represent real income (e.g. payroll at some banks).
// Single DB round-trip; rate computed in service layer (Issue #1215 PR2).
func (r *analyticsRepository) GetSavingsRateForPeriod(ctx context.Context, userID string, startDate time.Time, endDate time.Time) (*domain.SavingsRateRow, error) {
	observability.AccountsReplacedFiltered.WithLabelValues("analytics").Inc()

	const query = `
		SELECT
			COALESCE(ABS(SUM(CASE WHEN t.amount < 0 THEN t.amount ELSE 0 END)), 0) AS income,
			COALESCE(SUM(CASE WHEN t.amount > 0 THEN t.amount ELSE 0 END), 0) AS expenses
		FROM transactions t
		WHERE t.user_id = $1
		  AND t.date >= $2
		  AND t.date <= $3
		  AND t.pending = false
	`

	var row domain.SavingsRateRow
	err := r.db.QueryRow(ctx, query, userID, startDate, endDate).Scan(&row.Income, &row.Expenses)
	if err != nil {
		return nil, fmt.Errorf("failed to get savings rate for period: %w", err)
	}
	return &row, nil
}

// GetMonthOverMonthSpending returns monthly expense totals grouped by month.
// Single DB round-trip using DATE_TRUNC GROUP BY. filters.AccountTypes and
// filters.IncludeTransfers are honoured; StartDate/EndDate are computed from months. Issue #1215 PR2/gaps.
func (r *analyticsRepository) GetMonthOverMonthSpending(ctx context.Context, userID string, startDate time.Time, category string, filters domain.SpendingFilters) ([]domain.MonthOverMonthBucketRow, error) {
	observability.AccountsReplacedFiltered.WithLabelValues("analytics").Inc()

	catExpr := effectiveCategoryExpr(false)
	args := []interface{}{userID, startDate}

	var categoryFilter string
	if category != "" {
		args = append(args, category)
		categoryFilter = fmt.Sprintf(" AND (%s) = $%d", catExpr, len(args))
	}

	// Pass only AccountTypes/IncludeTransfers — StartDate/EndDate are not used by buildSpendingFilter
	accountFilter := domain.SpendingFilters{
		AccountTypes:     filters.AccountTypes,
		IncludeTransfers: filters.IncludeTransfers,
	}
	filterClause, args, _ := buildSpendingFilter(accountFilter, args)

	query := fmt.Sprintf(`
		SELECT DATE_TRUNC('month', t.date)::date AS month, COALESCE(SUM(t.amount), 0) AS total
		FROM transactions t
		LEFT JOIN accounts a ON t.account_id = a.id
		WHERE t.user_id = $1
		  AND t.date >= $2
		  AND t.amount > 0
		  AND t.pending = false
		  %s
		  %s
		GROUP BY DATE_TRUNC('month', t.date)
		ORDER BY month ASC
	`, categoryFilter, filterClause)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get month-over-month spending: %w", err)
	}
	defer rows.Close()

	results := make([]domain.MonthOverMonthBucketRow, 0)
	for rows.Next() {
		var row domain.MonthOverMonthBucketRow
		if err := rows.Scan(&row.Month, &row.Total); err != nil {
			return nil, fmt.Errorf("failed to scan month-over-month row: %w", err)
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating month-over-month rows: %w", err)
	}

	return results, nil
}

// GetLargestTransactionsForPeriod returns the N largest expense transactions in a date range.
// Single DB round-trip; limit enforced in SQL (Issue #1215 PR2).
func (r *analyticsRepository) GetLargestTransactionsForPeriod(ctx context.Context, userID string, filters domain.SpendingFilters, limit int) ([]domain.LargestTransactionRow, error) {
	observability.AccountsReplacedFiltered.WithLabelValues("analytics").Inc()

	catExpr := effectiveCategoryExpr(false)
	args := []interface{}{userID, filters.StartDate, filters.EndDate}
	filterClause, args, argIndex := buildSpendingFilter(filters, args)

	args = append(args, limit)
	limitPlaceholder := fmt.Sprintf("$%d", argIndex)

	query := fmt.Sprintf(`
		SELECT t.id, COALESCE(t.merchant_name, ''), t.amount, (%s), t.date, t.account_id::text
		FROM transactions t
		LEFT JOIN accounts a ON t.account_id = a.id
		WHERE t.user_id = $1
		  AND t.date >= $2
		  AND t.date <= $3
		  AND t.amount > 0
		  AND t.pending = false
		  %s
		ORDER BY t.amount DESC
		LIMIT %s
	`, catExpr, filterClause, limitPlaceholder)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get largest transactions: %w", err)
	}
	defer rows.Close()

	results := make([]domain.LargestTransactionRow, 0)
	for rows.Next() {
		var row domain.LargestTransactionRow
		if err := rows.Scan(&row.TransactionID, &row.MerchantName, &row.Amount, &row.Category, &row.Date, &row.AccountID); err != nil {
			return nil, fmt.Errorf("failed to scan largest transaction row: %w", err)
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating largest transaction rows: %w", err)
	}

	return results, nil
}

// GetCategoryBreakdownForMerchant returns expense totals grouped by category for a specific merchant.
// Filters by merchant_id (effective canonical via merge chain) so merged source transactions are included (#1348 G2b).
// Single DB round-trip (Issue #1215 PR2).
func (r *analyticsRepository) GetCategoryBreakdownForMerchant(ctx context.Context, userID string, merchantID string, filters domain.SpendingFilters) ([]domain.CategoryBreakdownForMerchantRow, error) {
	observability.AccountsReplacedFiltered.WithLabelValues("analytics").Inc()

	catExpr := effectiveCategoryExpr(false)
	args := []interface{}{userID, filters.StartDate, filters.EndDate, merchantID}
	filterClause, args, _ := buildSpendingFilter(filters, args)

	query := fmt.Sprintf(`
		SELECT (%s), SUM(t.amount), COUNT(*)
		FROM transactions t
		LEFT JOIN accounts a ON t.account_id = a.id
		%s
		WHERE t.user_id = $1::uuid
		  AND t.date >= $2
		  AND t.date <= $3
		  AND t.amount > 0
		  AND t.pending = false
		  AND %s = $4::uuid
		  %s
		GROUP BY (%s)
		ORDER BY SUM(t.amount) DESC
	`, catExpr, merchantSortJoins, effectiveMerchantIdExpr, filterClause, catExpr)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get category breakdown for merchant: %w", err)
	}
	defer rows.Close()

	results := make([]domain.CategoryBreakdownForMerchantRow, 0)
	for rows.Next() {
		var row domain.CategoryBreakdownForMerchantRow
		if err := rows.Scan(&row.Category, &row.Total, &row.Count); err != nil {
			return nil, fmt.Errorf("failed to scan category breakdown row: %w", err)
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating category breakdown rows: %w", err)
	}

	return results, nil
}

// GetIncomeSourceBreakdown returns income (amount < 0) totals grouped by category.
// Single DB round-trip (Issue #1215 PR2).
func (r *analyticsRepository) GetIncomeSourceBreakdown(ctx context.Context, userID string, filters domain.SpendingFilters) ([]domain.IncomeSourceRow, error) {
	observability.AccountsReplacedFiltered.WithLabelValues("analytics").Inc()

	catExpr := effectiveCategoryExpr(false)
	args := []interface{}{userID, filters.StartDate, filters.EndDate}
	filterClause, args, _ := buildSpendingFilter(filters, args)

	query := fmt.Sprintf(`
		SELECT (%s) AS category, ABS(SUM(t.amount)) AS total, COUNT(*) AS count
		FROM transactions t
		LEFT JOIN accounts a ON t.account_id = a.id
		WHERE t.user_id = $1
		  AND t.date >= $2
		  AND t.date <= $3
		  AND t.amount < 0
		  AND t.pending = false
		  %s
		GROUP BY (%s)
		ORDER BY ABS(SUM(t.amount)) DESC
	`, catExpr, filterClause, catExpr)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get income source breakdown: %w", err)
	}
	defer rows.Close()

	results := make([]domain.IncomeSourceRow, 0)
	for rows.Next() {
		var row domain.IncomeSourceRow
		if err := rows.Scan(&row.Category, &row.Total, &row.Count); err != nil {
			return nil, fmt.Errorf("failed to scan income source row: %w", err)
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating income source rows: %w", err)
	}

	return results, nil
}

// GetUnusualSpend returns transactions in [startDate, endDate] where the transaction amount
// exceeds sigmaThreshold standard deviations above the per-category mean over the baseline window.
// The baseline window is [baselineStart, startDate). Categories with fewer than 3 transactions
// or zero variance are excluded. Single DB round-trip via CTE with STDDEV_POP. Issue #1215 PR3.
func (r *analyticsRepository) GetUnusualSpend(ctx context.Context, userID string, baselineStart, startDate, endDate time.Time, sigmaThreshold float64) ([]domain.UnusualSpendRow, error) {
	observability.AccountsReplacedFiltered.WithLabelValues("analytics").Inc()

	catExpr := effectiveCategoryExpr(false)

	query := fmt.Sprintf(`
		WITH baseline AS (
			SELECT
				(%s)                                                        AS category,
				AVG(t.amount)                                               AS mean_amount,
				NULLIF(STDDEV_POP(t.amount), 0)                            AS stddev_amount,
				COUNT(*)                                                    AS sample_count
			FROM transactions t
			WHERE t.user_id = $1
			  AND t.date >= $2
			  AND t.date < $3
			  AND t.amount > 0
			  AND t.pending = false
			  AND COALESCE(t.effective_pfc_primary, '') NOT LIKE 'TRANSFER%%'
			GROUP BY (%s)
			HAVING COUNT(*) >= 3
		)
		SELECT
			t.id,
			COALESCE(t.merchant_name, ''),
			t.amount,
			(%s),
			t.date,
			b.mean_amount,
			(t.amount - b.mean_amount) / b.stddev_amount AS sigma_above
		FROM transactions t
		JOIN baseline b ON (%s) = b.category
		WHERE t.user_id = $1
		  AND t.date >= $3
		  AND t.date <= $4
		  AND t.amount > 0
		  AND t.pending = false
		  AND COALESCE(t.effective_pfc_primary, '') NOT LIKE 'TRANSFER%%'
		  AND b.stddev_amount IS NOT NULL
		  AND (t.amount - b.mean_amount) / b.stddev_amount > $5
		ORDER BY sigma_above DESC
	`, catExpr, catExpr, catExpr, catExpr)

	rows, err := r.db.Query(ctx, query, userID, baselineStart, startDate, endDate, sigmaThreshold)
	if err != nil {
		return nil, fmt.Errorf("failed to get unusual spend: %w", err)
	}
	defer rows.Close()

	results := make([]domain.UnusualSpendRow, 0)
	for rows.Next() {
		var row domain.UnusualSpendRow
		if err := rows.Scan(
			&row.TransactionID,
			&row.MerchantName,
			&row.Amount,
			&row.Category,
			&row.Date,
			&row.BaselineAvg,
			&row.SigmaAbove,
		); err != nil {
			return nil, fmt.Errorf("failed to scan unusual spend row: %w", err)
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating unusual spend rows: %w", err)
	}

	return results, nil
}

// GetBudgetVsActual returns active budgets for a user filtered by their budget period,
// joined with aggregated allocated and spent amounts from budget_categories.
// period selects by budget.period column (weekly/monthly/quarterly/yearly/custom).
// Returns an empty slice when no matching active budgets exist. Issue #1215 PR3.
func (r *analyticsRepository) GetBudgetVsActual(ctx context.Context, userID, period string) ([]domain.BudgetVsActualRow, error) {
	const query = `
		SELECT
			b.id,
			b.name,
			b.period,
			COALESCE(SUM(bc.allocated_amount), 0) AS allocated_amount,
			COALESCE(SUM(bc.spent_amount), 0)     AS spent_amount
		FROM budgets b
		LEFT JOIN budget_categories bc ON bc.budget_id = b.id
		WHERE b.user_id = $1
		  AND b.is_active = true
		  AND b.period = $2
		GROUP BY b.id, b.name, b.period
		ORDER BY b.name ASC
	`

	rows, err := r.db.Query(ctx, query, userID, period)
	if err != nil {
		return nil, fmt.Errorf("failed to get budget vs actual: %w", err)
	}
	defer rows.Close()

	results := make([]domain.BudgetVsActualRow, 0)
	for rows.Next() {
		var row domain.BudgetVsActualRow
		if err := rows.Scan(
			&row.BudgetID,
			&row.BudgetName,
			&row.Period,
			&row.AllocatedAmount,
			&row.SpentAmount,
		); err != nil {
			return nil, fmt.Errorf("failed to scan budget vs actual row: %w", err)
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating budget vs actual rows: %w", err)
	}

	return results, nil
}

// isNotFoundError returns true for pgx "no rows" sentinel errors
func isNotFoundError(err error) bool {
	return err != nil && err.Error() == "no rows in result set"
}
