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
	// scope, when non-nil, aggregates across the household's shared accounts instead of
	// userID's own (Issue #1369 — Phase 5); nil = personal (today's behavior, unchanged).
	GetSavingsRateForPeriod(ctx context.Context, userID string, startDate time.Time, endDate time.Time, scope *domain.HouseholdScope) (*domain.SavingsRateRow, error)

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
	if filters.HouseholdScope != nil {
		return r.getCashFlowAggregationHousehold(ctx, userID, filters)
	}
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

// getCashFlowAggregationHousehold is the household-scope variant of GetCashFlowAggregation:
// aggregates across every account shared into the household, clipped per-account to
// share_started_at. Issue #1369 — Phase 5.
func (r *analyticsRepository) getCashFlowAggregationHousehold(ctx context.Context, requestingUserID string, filters domain.CashFlowFilters) (*domain.CashFlowAggregation, error) {
	observability.AccountsReplacedFiltered.WithLabelValues("analytics").Inc()

	args := householdScopeArgs(requestingUserID, filters.HouseholdScope)
	args = append(args, filters.StartDate, filters.EndDate)
	argIndex := 6

	query := fmt.Sprintf(`
		WITH %s
		SELECT
			COALESCE(SUM(CASE WHEN t.amount < 0 THEN ABS(t.amount) ELSE 0 END), 0) as income,
			COUNT(CASE WHEN t.amount < 0 THEN 1 END) as income_count,
			COALESCE(SUM(CASE WHEN t.amount > 0 THEN t.amount ELSE 0 END), 0) as expenses,
			COUNT(CASE WHEN t.amount > 0 THEN 1 END) as expense_count
		FROM transactions t
		JOIN accounts a ON t.account_id = a.id AND a.status = 'active'
		JOIN household_accounts ha ON ha.user_account_id = a.user_account_id
		WHERE t.date >= GREATEST($4::date, (ha.share_started_at AT TIME ZONE 'UTC')::date)
		  AND t.date <= $5
		  AND t.pending = false
	`, householdAccountsCTE())

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
		return nil, fmt.Errorf("failed to get household cash flow aggregation: %w", err)
	}

	return &agg, nil
}

// GetCashFlowGrouped returns cash flow data grouped by period (daily/weekly/monthly).
// Uses PostgreSQL DATE_TRUNC for period grouping.
// Returns data ordered by period_start ASC for chart display.
func (r *analyticsRepository) GetCashFlowGrouped(ctx context.Context, userID string, filters domain.CashFlowFilters) ([]domain.CashFlowGroupedData, error) {
	if filters.HouseholdScope != nil {
		return r.getCashFlowGroupedHousehold(ctx, userID, filters)
	}
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

// getCashFlowGroupedHousehold is the household-scope variant of GetCashFlowGrouped:
// aggregates across every account shared into the household, clipped per-account to
// share_started_at. Issue #1369 — Phase 5.
func (r *analyticsRepository) getCashFlowGroupedHousehold(ctx context.Context, requestingUserID string, filters domain.CashFlowFilters) ([]domain.CashFlowGroupedData, error) {
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

	args := householdScopeArgs(requestingUserID, filters.HouseholdScope)
	args = append(args, filters.StartDate, filters.EndDate)
	argIndex := 6

	query := fmt.Sprintf(`
		WITH %s
		SELECT
			DATE_TRUNC('%s', t.date)::date as period_start,
			COALESCE(SUM(CASE WHEN t.amount < 0 THEN ABS(t.amount) ELSE 0 END), 0) as income,
			COALESCE(SUM(CASE WHEN t.amount > 0 THEN t.amount ELSE 0 END), 0) as expenses,
			COUNT(*) as transaction_count
		FROM transactions t
		JOIN accounts a ON t.account_id = a.id AND a.status = 'active'
		JOIN household_accounts ha ON ha.user_account_id = a.user_account_id
		WHERE t.date >= GREATEST($4::date, (ha.share_started_at AT TIME ZONE 'UTC')::date)
		  AND t.date <= $5
		  AND t.pending = false
	`, householdAccountsCTE(), truncInterval)

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
		return nil, fmt.Errorf("failed to get household grouped cash flow: %w", err)
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
			return nil, fmt.Errorf("failed to scan household cash flow data: %w", err)
		}
		results = append(results, data)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating household cash flow rows: %w", err)
	}

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

// householdAccountsCTE returns the canonical household-aggregation fan-out: for a given
// household ($2) and its active member user IDs ($3), the set of user_accounts that are
// currently shared into the household. Callers must supply exactly these three leading
// args: $1 = requesting userID (kept as $1, unused here, so buildSpendingFilter's
// custom-category subquery — `WHERE user_id = $1::uuid` — stays scoped to the requester
// even in household mode), $2 = household ID, $3 = active member user IDs (uuid[]).
//
// Household inclusion is governed SOLELY by the share (visibility='shared',
// revoked_at IS NULL) — is_hidden on user_accounts/accounts is the owner's own
// personal-view control and is deliberately ignored here (Issue #1369 decision #3;
// migration 000110's documented contract).
//
// Joint accounts (the same physical account linked and shared by two members) collapse to
// one row via DISTINCT ON physical_account_blind_idx (falling back to user_account_id when
// no fingerprint is recorded), keeping the EARLIEST-shared copy — deterministic, gives the
// longest legitimate shared history, and a later revocation of that copy naturally falls
// through to any surviving sharer (revoked rows are filtered by the WHERE clause before
// DISTINCT ON ever sees them). share_started_at is exposed so callers can clip
// time-ranged aggregations to when the account actually became household-visible
// (Issue #1369 decision #2).
func householdAccountsCTE() string {
	// The unused `$1::uuid AS _requesting_user_id` column exists solely to pin $1's type.
	// $1 is not otherwise referenced by every household query variant (only
	// buildSpendingFilter's custom-category subquery references it, and only when
	// CustomCategoryIDs is non-empty) — without a typed reference somewhere in the query
	// text, Postgres's extended query protocol cannot infer $1's type and errors with
	// "could not determine data type of parameter $1" (SQLSTATE 42P18).
	return `household_accounts AS (
		SELECT DISTINCT ON (COALESCE(has.physical_account_blind_idx, ua.id::text))
			ua.id         AS user_account_id,
			ua.user_id    AS owner_user_id,
			has.shared_at AS share_started_at,
			$1::uuid      AS _requesting_user_id
		FROM household_account_shares has
		JOIN user_accounts ua ON ua.id = has.user_account_id
		WHERE has.household_id = $2
		  AND has.visibility  = 'shared'
		  AND has.revoked_at IS NULL
		  AND ua.user_id = ANY($3)
		ORDER BY COALESCE(has.physical_account_blind_idx, ua.id::text), has.shared_at ASC, ua.id
	)`
}

// householdScopeArgs returns the three leading args every household-aware query must pass,
// in order: requesting userID (kept as $1), household ID ($2), active member IDs ($3).
func householdScopeArgs(requestingUserID string, scope *domain.HouseholdScope) []interface{} {
	return []interface{}{requestingUserID, scope.HouseholdID, scope.MemberUserIDs}
}

// householdAccountsCTEForBalances is the balance/net-worth sibling of householdAccountsCTE.
// Same visibility/revocation/member filter, but joint-account fingerprint collisions are
// resolved by picking the copy with the FRESHEST synced balance snapshot, not the
// earliest-shared one — a stale, rarely-synced copy of a joint account must never win over
// a copy that's actively being kept up to date (Codex adversarial review, P1-8). Used only
// by account_repository.go / balance_snapshot_repository.go's household-scope methods; the
// spending/cash-flow queries in this file use householdAccountsCTE (earliest-shared)
// instead, since transaction history — not current balance freshness — is what matters
// there. Same three leading args as householdAccountsCTE: $1 = requesting userID (pins
// its type; unused otherwise here), $2 = household ID, $3 = active member user IDs.
func householdAccountsCTEForBalances() string {
	return `household_accounts AS (
		SELECT DISTINCT ON (COALESCE(has.physical_account_blind_idx, ua.id::text))
			ua.id         AS user_account_id,
			ua.user_id    AS owner_user_id,
			has.shared_at AS share_started_at,
			$1::uuid      AS _requesting_user_id
		FROM household_account_shares has
		JOIN user_accounts ua ON ua.id = has.user_account_id
		LEFT JOIN LATERAL (
			SELECT MAX(bs.snapshot_at) AS latest_snapshot_at
			FROM balance_snapshots bs
			WHERE bs.user_account_id = ua.id
		) latest ON true
		WHERE has.household_id = $2
		  AND has.visibility  = 'shared'
		  AND has.revoked_at IS NULL
		  AND ua.user_id = ANY($3)
		ORDER BY COALESCE(has.physical_account_blind_idx, ua.id::text), latest.latest_snapshot_at DESC NULLS LAST, ua.id
	)`
}

// householdAccountsCTEForBalancesAsOf is the asOf-bounded sibling of
// householdAccountsCTEForBalances, used by point-in-time balance lookups
// (GetSnapshotsAsOfForHousehold) that are evaluated at a specific instant — both the
// "current" call (asOf=now) and a "previous period" call (asOf=now-30d) reuse this same
// query, so the joint-account fingerprint winner must be picked using "latest snapshot AT
// OR BEFORE asOf ($4)", not "latest snapshot ever". Without this bound, a previous-period
// query could pick the globally-freshest member's copy even when THAT copy has no snapshot
// before the cutoff, silently returning zero data for the joint account instead of falling
// back to the other member's genuinely earlier balance (Codex adversarial review, P1-2).
// Same three leading args as householdAccountsCTEForBalances, PLUS $4 = the asOf cutoff
// (reused both for the winner-selection bound here and the caller's own snapshot_at <= $4
// filter in the outer query).
func householdAccountsCTEForBalancesAsOf() string {
	return `household_accounts AS (
		SELECT DISTINCT ON (COALESCE(has.physical_account_blind_idx, ua.id::text))
			ua.id         AS user_account_id,
			ua.user_id    AS owner_user_id,
			has.shared_at AS share_started_at,
			$1::uuid      AS _requesting_user_id
		FROM household_account_shares has
		JOIN user_accounts ua ON ua.id = has.user_account_id
		LEFT JOIN LATERAL (
			SELECT MAX(bs.snapshot_at) AS latest_snapshot_at
			FROM balance_snapshots bs
			WHERE bs.user_account_id = ua.id AND bs.snapshot_at <= $4
		) latest ON true
		WHERE has.household_id = $2
		  AND has.visibility  = 'shared'
		  AND has.revoked_at IS NULL
		  AND ua.user_id = ANY($3)
		ORDER BY COALESCE(has.physical_account_blind_idx, ua.id::text), latest.latest_snapshot_at DESC NULLS LAST, ua.id
	)`
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
	if filters.HouseholdScope != nil {
		return r.getSpendingByCategoryHousehold(ctx, userID, filters)
	}
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

// getSpendingByCategoryHousehold is the household-scope variant of GetSpendingByCategory:
// aggregates across every account shared into the household (visibility='shared', not
// revoked), clipped per-account to when it became shared (share_started_at). requestingUserID
// stays $1 so buildSpendingFilter's custom-category subquery (`WHERE user_id = $1::uuid`)
// resolves only the REQUESTER's own custom categories, never a household member's — the
// custom-category namespace is never widened by household scope. Issue #1369 — Phase 5.
func (r *analyticsRepository) getSpendingByCategoryHousehold(ctx context.Context, requestingUserID string, filters domain.SpendingFilters) ([]domain.SpendingByCategory, error) {
	observability.AccountsReplacedFiltered.WithLabelValues("analytics").Inc()
	isDrillDown := filters.Category != ""
	catExpr := effectiveCategoryExpr(isDrillDown)
	displayExpr := effectiveCategoryDisplayExpr()

	args := householdScopeArgs(requestingUserID, filters.HouseholdScope)
	args = append(args, filters.StartDate, filters.EndDate)

	var categoryFilter string
	if isDrillDown {
		args = append(args, filters.Category)
		primaryCatExpr := effectiveCategoryExpr(false)
		categoryFilter = fmt.Sprintf(" AND %s = $%d", primaryCatExpr, len(args))
	}

	extraClause, args, _ := buildSpendingFilter(filters, args)

	query := fmt.Sprintf(`
		WITH %s
		SELECT
			%s as category,
			%s as category_display,
			COALESCE(SUM(ABS(t.amount)), 0) as total,
			COUNT(*) as transaction_count
		FROM transactions t
		JOIN accounts a ON t.account_id = a.id AND a.status = 'active'
		JOIN household_accounts ha ON ha.user_account_id = a.user_account_id
		WHERE t.date >= GREATEST($4::date, (ha.share_started_at AT TIME ZONE 'UTC')::date)
		  AND t.date <= $5
		  AND t.amount > 0
		  AND t.pending = false
		%s%s
		GROUP BY category, category_display
		ORDER BY total DESC
	`, householdAccountsCTE(), catExpr, displayExpr, categoryFilter, extraClause)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get household spending by category: %w", err)
	}
	defer rows.Close()

	var results []domain.SpendingByCategory
	for rows.Next() {
		var row domain.SpendingByCategory
		if err := rows.Scan(&row.Category, &row.PFCPrimaryDisplay, &row.Total, &row.TransactionCount); err != nil {
			return nil, fmt.Errorf("failed to scan household spending by category: %w", err)
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating household spending by category rows: %w", err)
	}

	return results, nil
}

// GetSpendingByMerchant returns expense totals grouped by effective merchant, ordered by total descending.
// Merchants renamed or merged by the user are grouped under their canonical (target) display name.
// Groups by a synthetic key: 'merchant:<id>', 'raw:<name>', or 'unknown' — so renamed/merged variants
// collapse into one row. merchant_id is NULL for unlinked/unknown groups (preserves "not clickable" frontend behavior).
func (r *analyticsRepository) GetSpendingByMerchant(ctx context.Context, userID string, filters domain.SpendingFilters, limit int) ([]domain.SpendingByMerchant, error) {
	if filters.HouseholdScope != nil {
		return r.getSpendingByMerchantHousehold(ctx, userID, filters, limit)
	}
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

// getSpendingByMerchantHousehold is the household-scope variant of GetSpendingByMerchant.
// Issue #1369 — Phase 5.
func (r *analyticsRepository) getSpendingByMerchantHousehold(ctx context.Context, requestingUserID string, filters domain.SpendingFilters, limit int) ([]domain.SpendingByMerchant, error) {
	observability.AccountsReplacedFiltered.WithLabelValues("analytics").Inc()

	args := householdScopeArgs(requestingUserID, filters.HouseholdScope)
	args = append(args, filters.StartDate, filters.EndDate)
	extraClause, args, argIndex := buildSpendingFilter(filters, args)

	args = append(args, limit)
	limitPlaceholder := fmt.Sprintf("$%d", argIndex)

	// Plain concatenation, NOT fmt.Sprintf — extraClause can contain a literal '%' (e.g.
	// `LIKE 'TRANSFER%'`), which fmt.Sprintf would misinterpret as a format verb and
	// corrupt the query (matches the personal-path GetSpendingByMerchant above, which
	// concatenates for the same reason).
	query := "WITH " + householdAccountsCTE() + `
		SELECT
			MAX(COALESCE(mno_t.display_name, NULLIF(m2.normalized_name, ''), m2.name, NULLIF(t.merchant_name, ''), 'Unknown')) AS merchant_name,
			MAX(COALESCE(mno.merged_into_merchant_id, t.merchant_id)::text)                                                    AS merchant_id,
			COALESCE(SUM(ABS(t.amount)), 0)                                                                                    AS total,
			COUNT(*)                                                                                                            AS transaction_count
		FROM transactions t
		JOIN accounts a ON t.account_id = a.id AND a.status = 'active'
		JOIN household_accounts ha ON ha.user_account_id = a.user_account_id
		` + merchantSortJoins + `
		WHERE t.date >= GREATEST($4::date, (ha.share_started_at AT TIME ZONE 'UTC')::date)
		  AND t.date <= $5
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
		return nil, fmt.Errorf("failed to get household spending by merchant: %w", err)
	}
	defer rows.Close()

	var results []domain.SpendingByMerchant
	for rows.Next() {
		var row domain.SpendingByMerchant
		if err := rows.Scan(&row.MerchantName, &row.MerchantID, &row.Total, &row.TransactionCount); err != nil {
			return nil, fmt.Errorf("failed to scan household spending by merchant: %w", err)
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating household spending by merchant rows: %w", err)
	}

	return results, nil
}

// GetSpendingTrends returns monthly expense totals per category for trend chart data.
// Results are ordered by period_start ASC, total DESC.
func (r *analyticsRepository) GetSpendingTrends(ctx context.Context, userID string, filters domain.SpendingFilters) ([]domain.SpendingTrendRow, error) {
	if filters.HouseholdScope != nil {
		return r.getSpendingTrendsHousehold(ctx, userID, filters)
	}
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

// getSpendingTrendsHousehold is the household-scope variant of GetSpendingTrends.
// Issue #1369 — Phase 5.
func (r *analyticsRepository) getSpendingTrendsHousehold(ctx context.Context, requestingUserID string, filters domain.SpendingFilters) ([]domain.SpendingTrendRow, error) {
	observability.AccountsReplacedFiltered.WithLabelValues("analytics").Inc()

	catExpr := effectiveCategoryExpr(false)
	args := householdScopeArgs(requestingUserID, filters.HouseholdScope)
	args = append(args, filters.StartDate, filters.EndDate)
	extraClause, args, _ := buildSpendingFilter(filters, args)

	displayExprTrends := effectiveCategoryDisplayExpr()
	query := fmt.Sprintf(`
		WITH %s
		SELECT
			DATE_TRUNC('month', t.date)::date as period_start,
			%s as category,
			%s as category_display,
			COALESCE(SUM(ABS(t.amount)), 0) as total
		FROM transactions t
		JOIN accounts a ON t.account_id = a.id AND a.status = 'active'
		JOIN household_accounts ha ON ha.user_account_id = a.user_account_id
		WHERE t.date >= GREATEST($4::date, (ha.share_started_at AT TIME ZONE 'UTC')::date)
		  AND t.date <= $5
		  AND t.amount > 0
		  AND t.pending = false
		%s
		GROUP BY period_start, category, category_display
		ORDER BY period_start ASC, total DESC
	`, householdAccountsCTE(), catExpr, displayExprTrends, extraClause)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get household spending trends: %w", err)
	}
	defer rows.Close()

	var results []domain.SpendingTrendRow
	for rows.Next() {
		var row domain.SpendingTrendRow
		if err := rows.Scan(&row.PeriodStart, &row.Category, &row.CategoryDisplay, &row.Total); err != nil {
			return nil, fmt.Errorf("failed to scan household spending trend row: %w", err)
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating household spending trends rows: %w", err)
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
	if filters.HouseholdScope != nil {
		return r.getAverageMonthlySpendHousehold(ctx, userID, filters)
	}
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

// getAverageMonthlySpendHousehold is the household-scope variant of GetAverageMonthlySpend.
// Issue #1369 — Phase 5.
func (r *analyticsRepository) getAverageMonthlySpendHousehold(ctx context.Context, requestingUserID string, filters domain.SpendingFilters) (*domain.AverageMonthlySpendRow, error) {
	observability.AccountsReplacedFiltered.WithLabelValues("analytics").Inc()

	args := householdScopeArgs(requestingUserID, filters.HouseholdScope)
	args = append(args, filters.StartDate, filters.EndDate)
	filterClause, args, _ := buildSpendingFilter(filters, args)

	query := fmt.Sprintf(`
		WITH %s
		SELECT COALESCE(SUM(t.amount), 0) as total
		FROM transactions t
		JOIN accounts a ON t.account_id = a.id AND a.status = 'active'
		JOIN household_accounts ha ON ha.user_account_id = a.user_account_id
		WHERE t.date >= GREATEST($4::date, (ha.share_started_at AT TIME ZONE 'UTC')::date)
		  AND t.date <= $5
		  AND t.amount > 0
		  AND t.pending = false
		  %s
	`, householdAccountsCTE(), filterClause)

	var row domain.AverageMonthlySpendRow
	err := r.db.QueryRow(ctx, query, args...).Scan(&row.Total)
	if err != nil {
		return nil, fmt.Errorf("failed to get household average monthly spend: %w", err)
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
func (r *analyticsRepository) GetSavingsRateForPeriod(ctx context.Context, userID string, startDate time.Time, endDate time.Time, scope *domain.HouseholdScope) (*domain.SavingsRateRow, error) {
	if scope != nil {
		return r.getSavingsRateForPeriodHousehold(ctx, userID, startDate, endDate, scope)
	}
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

// getSavingsRateForPeriodHousehold is the household-scope variant of
// GetSavingsRateForPeriod. Issue #1369 — Phase 5.
func (r *analyticsRepository) getSavingsRateForPeriodHousehold(ctx context.Context, requestingUserID string, startDate, endDate time.Time, scope *domain.HouseholdScope) (*domain.SavingsRateRow, error) {
	observability.AccountsReplacedFiltered.WithLabelValues("analytics").Inc()

	query := fmt.Sprintf(`
		WITH %s
		SELECT
			COALESCE(ABS(SUM(CASE WHEN t.amount < 0 THEN t.amount ELSE 0 END)), 0) AS income,
			COALESCE(SUM(CASE WHEN t.amount > 0 THEN t.amount ELSE 0 END), 0) AS expenses
		FROM transactions t
		JOIN accounts a ON t.account_id = a.id AND a.status = 'active'
		JOIN household_accounts ha ON ha.user_account_id = a.user_account_id
		WHERE t.date >= GREATEST($4::date, (ha.share_started_at AT TIME ZONE 'UTC')::date)
		  AND t.date <= $5
		  AND t.pending = false
	`, householdAccountsCTE())

	args := householdScopeArgs(requestingUserID, scope)
	args = append(args, startDate, endDate)

	var row domain.SavingsRateRow
	err := r.db.QueryRow(ctx, query, args...).Scan(&row.Income, &row.Expenses)
	if err != nil {
		return nil, fmt.Errorf("failed to get household savings rate for period: %w", err)
	}
	return &row, nil
}

// GetMonthOverMonthSpending returns monthly expense totals grouped by month.
// Single DB round-trip using DATE_TRUNC GROUP BY. filters.AccountTypes and
// filters.IncludeTransfers are honoured; StartDate/EndDate are computed from months. Issue #1215 PR2/gaps.
func (r *analyticsRepository) GetMonthOverMonthSpending(ctx context.Context, userID string, startDate time.Time, category string, filters domain.SpendingFilters) ([]domain.MonthOverMonthBucketRow, error) {
	if filters.HouseholdScope != nil {
		return r.getMonthOverMonthSpendingHousehold(ctx, userID, startDate, category, filters)
	}
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

// getMonthOverMonthSpendingHousehold is the household-scope variant of
// GetMonthOverMonthSpending. Issue #1369 — Phase 5.
func (r *analyticsRepository) getMonthOverMonthSpendingHousehold(ctx context.Context, requestingUserID string, startDate time.Time, category string, filters domain.SpendingFilters) ([]domain.MonthOverMonthBucketRow, error) {
	observability.AccountsReplacedFiltered.WithLabelValues("analytics").Inc()

	catExpr := effectiveCategoryExpr(false)
	args := householdScopeArgs(requestingUserID, filters.HouseholdScope)
	args = append(args, startDate)

	var categoryFilter string
	if category != "" {
		args = append(args, category)
		categoryFilter = fmt.Sprintf(" AND (%s) = $%d", catExpr, len(args))
	}

	// Pass only AccountTypes/IncludeTransfers — StartDate/EndDate/HouseholdScope are not
	// used by buildSpendingFilter directly (scope is already consumed via the CTE args).
	accountFilter := domain.SpendingFilters{
		AccountTypes:     filters.AccountTypes,
		IncludeTransfers: filters.IncludeTransfers,
	}
	filterClause, args, _ := buildSpendingFilter(accountFilter, args)

	query := fmt.Sprintf(`
		WITH %s
		SELECT DATE_TRUNC('month', t.date)::date AS month, COALESCE(SUM(t.amount), 0) AS total
		FROM transactions t
		JOIN accounts a ON t.account_id = a.id AND a.status = 'active'
		JOIN household_accounts ha ON ha.user_account_id = a.user_account_id
		WHERE t.date >= GREATEST($4::date, (ha.share_started_at AT TIME ZONE 'UTC')::date)
		  AND t.amount > 0
		  AND t.pending = false
		  %s
		  %s
		GROUP BY DATE_TRUNC('month', t.date)
		ORDER BY month ASC
	`, householdAccountsCTE(), categoryFilter, filterClause)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get household month-over-month spending: %w", err)
	}
	defer rows.Close()

	var results []domain.MonthOverMonthBucketRow
	for rows.Next() {
		var row domain.MonthOverMonthBucketRow
		if err := rows.Scan(&row.Month, &row.Total); err != nil {
			return nil, fmt.Errorf("failed to scan household month-over-month row: %w", err)
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating household month-over-month rows: %w", err)
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
	if filters.HouseholdScope != nil {
		return r.getIncomeSourceBreakdownHousehold(ctx, userID, filters)
	}
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

// getIncomeSourceBreakdownHousehold is the household-scope variant of
// GetIncomeSourceBreakdown. Issue #1369 — Phase 5.
func (r *analyticsRepository) getIncomeSourceBreakdownHousehold(ctx context.Context, requestingUserID string, filters domain.SpendingFilters) ([]domain.IncomeSourceRow, error) {
	observability.AccountsReplacedFiltered.WithLabelValues("analytics").Inc()

	catExpr := effectiveCategoryExpr(false)
	args := householdScopeArgs(requestingUserID, filters.HouseholdScope)
	args = append(args, filters.StartDate, filters.EndDate)
	filterClause, args, _ := buildSpendingFilter(filters, args)

	query := fmt.Sprintf(`
		WITH %s
		SELECT (%s) AS category, ABS(SUM(t.amount)) AS total, COUNT(*) AS count
		FROM transactions t
		JOIN accounts a ON t.account_id = a.id AND a.status = 'active'
		JOIN household_accounts ha ON ha.user_account_id = a.user_account_id
		WHERE t.date >= GREATEST($4::date, (ha.share_started_at AT TIME ZONE 'UTC')::date)
		  AND t.date <= $5
		  AND t.amount < 0
		  AND t.pending = false
		  %s
		GROUP BY (%s)
		ORDER BY ABS(SUM(t.amount)) DESC
	`, householdAccountsCTE(), catExpr, filterClause, catExpr)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get household income source breakdown: %w", err)
	}
	defer rows.Close()

	var results []domain.IncomeSourceRow
	for rows.Next() {
		var row domain.IncomeSourceRow
		if err := rows.Scan(&row.Category, &row.Total, &row.Count); err != nil {
			return nil, fmt.Errorf("failed to scan household income source row: %w", err)
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating household income source rows: %w", err)
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
