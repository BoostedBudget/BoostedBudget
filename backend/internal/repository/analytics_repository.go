package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
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
	// Build dynamic query based on filters
	args := []interface{}{userID, filters.StartDate, filters.EndDate}
	argIndex := 4

	// Base query - joins with accounts to filter by account type
	query := `
		SELECT
			COALESCE(SUM(CASE WHEN t.amount < 0 THEN ABS(t.amount) ELSE 0 END), 0) as income,
			COUNT(CASE WHEN t.amount < 0 THEN 1 END) as income_count,
			COALESCE(SUM(CASE WHEN t.amount > 0 THEN t.amount ELSE 0 END), 0) as expenses,
			COUNT(CASE WHEN t.amount > 0 THEN 1 END) as expense_count
		FROM transactions t
		JOIN accounts a ON t.account_id = a.id
		WHERE t.user_id = $1
		  AND t.date >= $2
		  AND t.date <= $3
		  AND t.pending = false
	`

	// Optional filter: account types
	if len(filters.AccountTypes) > 0 {
		placeholders := make([]string, len(filters.AccountTypes))
		for i, at := range filters.AccountTypes {
			placeholders[i] = fmt.Sprintf("$%d", argIndex)
			args = append(args, at)
			argIndex++
		}
		query += fmt.Sprintf(" AND a.type IN (%s)", strings.Join(placeholders, ", "))
	}

	// Exclude transfers using effective PFC primary (respects category overrides).
	// A user who overrides TRANSFER_IN → FOOD_AND_DRINK expresses intent that it is spending.
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
	// Determine the DATE_TRUNC interval based on grouping
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

	// Build dynamic query based on filters
	args := []interface{}{userID, filters.StartDate, filters.EndDate}
	argIndex := 4

	// Base query with period grouping
	query := fmt.Sprintf(`
		SELECT
			DATE_TRUNC('%s', t.date)::date as period_start,
			COALESCE(SUM(CASE WHEN t.amount < 0 THEN ABS(t.amount) ELSE 0 END), 0) as income,
			COALESCE(SUM(CASE WHEN t.amount > 0 THEN t.amount ELSE 0 END), 0) as expenses,
			COUNT(*) as transaction_count
		FROM transactions t
		JOIN accounts a ON t.account_id = a.id
		WHERE t.user_id = $1
		  AND t.date >= $2
		  AND t.date <= $3
		  AND t.pending = false
	`, truncInterval)

	// Optional filter: account types
	if len(filters.AccountTypes) > 0 {
		placeholders := make([]string, len(filters.AccountTypes))
		for i, at := range filters.AccountTypes {
			placeholders[i] = fmt.Sprintf("$%d", argIndex)
			args = append(args, at)
			argIndex++
		}
		query += fmt.Sprintf(" AND a.type IN (%s)", strings.Join(placeholders, ", "))
	}

	if !filters.IncludeTransfers {
		query += ` AND ` + effectiveTransferExpr() + ` NOT LIKE 'TRANSFER%'`
	}

	// Group and order
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

	// Create a map of existing periods
	periodMap := make(map[string]domain.CashFlowGroupedData)
	for _, d := range data {
		key := d.PeriodStart.Format("2006-01-02")
		periodMap[key] = d
	}

	// Generate all expected periods
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

// truncateDate truncates a date to the start of its period
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

// advancePeriod advances the date by one period
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
// of the effective category. For custom-category overrides it resolves cc.name from the joined
// custom_categories table (requires LEFT JOIN custom_categories cc). For all other rows it
// returns '' so the service applies its PFCv2 registry lookup instead.
func effectiveCategoryDisplayExpr() string {
	return `CASE
		WHEN t.category_override_kind = 'custom' THEN COALESCE(cc.name, 'Uncategorized')
		ELSE ''
	END`
}

// effectiveTransferExpr returns the SQL expression for the effective PFC primary used
// in transfer filtering. Custom overrides always return '' (never match TRANSFER%).
// PFC overrides: use pfc_primary_override; otherwise use raw pfc_primary.
func effectiveTransferExpr() string {
	return `CASE
		WHEN t.category_override_kind = 'pfc'    THEN COALESCE(t.pfc_primary_override, '')
		WHEN t.category_override_kind = 'custom' THEN ''
		ELSE                                          COALESCE(t.pfc_primary, '')
	END`
}

// spendingBaseQuery returns the common WHERE clause args and starting argIndex for spending queries.
func buildSpendingFilter(filters domain.SpendingFilters, baseArgs []interface{}) (string, []interface{}, int) {
	args := baseArgs
	argIndex := len(args) + 1
	clause := ""

	if len(filters.AccountTypes) > 0 {
		placeholders := make([]string, len(filters.AccountTypes))
		for i, at := range filters.AccountTypes {
			placeholders[i] = fmt.Sprintf("$%d", argIndex)
			args = append(args, at)
			argIndex++
		}
		clause += fmt.Sprintf(" AND a.type IN (%s)", strings.Join(placeholders, ", "))
	}

	if !filters.IncludeTransfers {
		clause += ` AND ` + effectiveTransferExpr() + ` NOT LIKE 'TRANSFER%'`
	}

	return clause, args, argIndex
}

// GetSpendingByCategory returns expense totals grouped by effective primary category.
// When filters.Category is non-empty, returns the detailed subcategory breakdown.
func (r *analyticsRepository) GetSpendingByCategory(ctx context.Context, userID string, filters domain.SpendingFilters) ([]domain.SpendingByCategory, error) {
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
		JOIN accounts a ON t.account_id = a.id
		LEFT JOIN custom_categories cc
		       ON cc.id = t.custom_category_override_id
		      AND t.category_override_kind = 'custom'
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

// GetSpendingByMerchant returns expense totals grouped by merchant name, ordered by total descending.
// Null merchant names are coalesced to "Unknown".
func (r *analyticsRepository) GetSpendingByMerchant(ctx context.Context, userID string, filters domain.SpendingFilters, limit int) ([]domain.SpendingByMerchant, error) {
	args := []interface{}{userID, filters.StartDate, filters.EndDate}
	extraClause, args, argIndex := buildSpendingFilter(filters, args)

	args = append(args, limit)
	limitPlaceholder := fmt.Sprintf("$%d", argIndex)

	query := fmt.Sprintf(`
		SELECT
			COALESCE(t.merchant_name, 'Unknown') as merchant_name,
			COALESCE(SUM(ABS(t.amount)), 0) as total,
			COUNT(*) as transaction_count
		FROM transactions t
		JOIN accounts a ON t.account_id = a.id
		WHERE t.user_id = $1
		  AND t.date >= $2
		  AND t.date <= $3
		  AND t.amount > 0
		  AND t.pending = false
		%s
		GROUP BY merchant_name
		ORDER BY total DESC
		LIMIT %s
	`, extraClause, limitPlaceholder)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get spending by merchant: %w", err)
	}
	defer rows.Close()

	var results []domain.SpendingByMerchant
	for rows.Next() {
		var row domain.SpendingByMerchant
		if err := rows.Scan(&row.MerchantName, &row.Total, &row.TransactionCount); err != nil {
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
		JOIN accounts a ON t.account_id = a.id
		LEFT JOIN custom_categories cc
		       ON cc.id = t.custom_category_override_id
		      AND t.category_override_kind = 'custom'
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
