package repository

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

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

	// GetCategoryHierarchy returns one grouped row per leaf carrying its whole ancestor path,
	// for a single flow direction (#1626).
	GetCategoryHierarchy(ctx context.Context, userID string, filters domain.SpendingFilters, direction domain.CategoryDirection) ([]domain.CategoryHierarchyRow, error)

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
		  AND t.deleted_at IS NULL
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
		argIndex += len(filters.AccountTypes) //nolint:ineffassign // kept so a filter added below inherits the correct arg index
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
		JOIN accounts a ON t.account_id = a.id
		WHERE %s
		  AND t.date >= $4::date
		  AND t.deleted_at IS NULL
		  AND t.date <= $5
		  AND t.pending = false
	`, householdGrantsCTE(), householdSharedTransactionPredicate("t", "a"))

	if len(filters.AccountTypes) > 0 {
		placeholders := buildPlaceholders(len(filters.AccountTypes), argIndex)
		for _, at := range filters.AccountTypes {
			args = append(args, at)
		}
		argIndex += len(filters.AccountTypes) //nolint:ineffassign // kept so a filter added below inherits the correct arg index
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
		  AND t.deleted_at IS NULL
		  AND t.date >= $2
		  AND t.date <= $3
		  AND t.pending = false
	`, truncInterval)

	if len(filters.AccountTypes) > 0 {
		placeholders := buildPlaceholders(len(filters.AccountTypes), argIndex)
		for _, at := range filters.AccountTypes {
			args = append(args, at)
		}
		argIndex += len(filters.AccountTypes) //nolint:ineffassign // kept so a filter added below inherits the correct arg index
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
		JOIN accounts a ON t.account_id = a.id
		WHERE %s
		  AND t.date >= $4::date
		  AND t.deleted_at IS NULL
		  AND t.date <= $5
		  AND t.pending = false
	`, householdGrantsCTE(), truncInterval, householdSharedTransactionPredicate("t", "a"))

	if len(filters.AccountTypes) > 0 {
		placeholders := buildPlaceholders(len(filters.AccountTypes), argIndex)
		for _, at := range filters.AccountTypes {
			args = append(args, at)
		}
		argIndex += len(filters.AccountTypes) //nolint:ineffassign // kept so a filter added below inherits the correct arg index
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

// periodKeyLayout is the calendar-date key a cash-flow bucket is identified by. Buckets come
// out of DATE_TRUNC as dates with no time part, so the key is the whole of their identity.
const periodKeyLayout = "2006-01-02"

// fillMissingPeriods returns every period the query produced, plus zero-valued periods for the
// gaps between them, so a chart gets a continuous series.
//
// fillEnd is the last period this function may FABRICATE. The service clamps it (see
// clampFillEnd in services/analytics_service.go) so a request reaching into the future does not
// manufacture buckets for days that have not happened yet — the user-visible defect was a Cash
// Flow chart showing "Week of Sep 13 / Sep 20 / Sep 27" on Sep 9, flat at exactly 0
// (#1626 Phase 3, Defect 1). Two invariants make that clamp safe to apply:
//
//   - Nothing empty is generated past fillEnd, and nothing at all when the requested range lies
//     entirely beyond it. That second guard is not redundant: truncateDate moves a start date
//     BACKWARD to Monday/month-start, so a range wholly in the future (today Wednesday, request
//     starting Friday) still truncated to a Monday inside the horizon and emitted one fake
//     "current period" bucket.
//   - No period present in `data` is ever dropped. This function rebuilds its output from the
//     generated key sequence alone, so a real row whose key falls outside that sequence would be
//     DISCARDED rather than merely left unfilled. That is precisely why the obvious fix — clamp
//     the generated horizon to now — is a data-DELETING change and not a fix: transactions are
//     bucketed on t.date, a zoneless calendar date that is effectively institution-local, so a
//     user ahead of UTC has real, already-returned spending for a period the clamp would drop.
//     Pinned by TestFillMissingPeriods_KeepsEveryRealPeriodBeyondTheHorizon.
func fillMissingPeriods(data []domain.CashFlowGroupedData, startDate, fillEnd time.Time, grouping string) []domain.CashFlowGroupedData {
	if len(data) == 0 {
		return generateEmptyPeriods(startDate, fillEnd, grouping)
	}

	periodMap := make(map[string]domain.CashFlowGroupedData, len(data))
	for _, d := range data {
		periodMap[d.PeriodStart.Format(periodKeyLayout)] = d
	}

	result := make([]domain.CashFlowGroupedData, 0, len(data))
	emitted := make(map[string]bool, len(data))

	if !startDate.After(fillEnd) {
		for current := truncateDate(startDate, grouping); !current.After(fillEnd); current = advancePeriod(current, grouping) {
			key := current.Format(periodKeyLayout)
			emitted[key] = true
			if existing, ok := periodMap[key]; ok {
				result = append(result, existing)
				continue
			}
			result = append(result, domain.CashFlowGroupedData{
				PeriodStart:      current,
				Income:           0,
				Expenses:         0,
				TransactionCount: 0,
			})
		}
	}

	// Real periods the generated sequence did not cover — beyond fillEnd, or before the start
	// date the caller asked to fill from. Kept, never zeroed and never dropped.
	for _, d := range data {
		key := d.PeriodStart.Format(periodKeyLayout)
		if emitted[key] {
			continue
		}
		emitted[key] = true
		result = append(result, d)
	}

	sort.Slice(result, func(i, j int) bool { return result[i].PeriodStart.Before(result[j].PeriodStart) })

	return result
}

// generateEmptyPeriods creates a list of zero-value periods covering [startDate, fillEnd].
// Returns nothing when the range lies entirely beyond fillEnd — see fillMissingPeriods above
// for why truncateDate makes that guard necessary rather than merely defensive. This is the
// path an empty result set takes, so it needs the guard in its own right: the clamp would
// otherwise never be reached for a user with no transactions in the requested range.
func generateEmptyPeriods(startDate, fillEnd time.Time, grouping string) []domain.CashFlowGroupedData {
	if startDate.After(fillEnd) {
		return nil
	}

	var result []domain.CashFlowGroupedData
	current := truncateDate(startDate, grouping)

	for !current.After(fillEnd) {
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

// ─── #1585: the spending reads' tiebreaker ───────────────────────────────────────────────────────
//
// The six spending reads (GetSpendingByCategory / GetSpendingByMerchant / GetSpendingTrends, each
// with a household sibling) used to sort by `total` alone. Rows tied on total therefore came back
// in whatever order the executor emitted them: a HashAggregate emits hash-bucket order, a
// GroupAggregate emits GROUP BY key order, and Postgres's sort is not required to be stable. The
// SAME data could answer the SAME question two ways once the plan changed — which is exactly how
// this was found on #1569 T3 (identical result MULTISETS, different row ORDER inside equal totals).
//
// It matters beyond cosmetics because CLAUDE.md designs these methods to be tool-callable by an
// in-app AI assistant, which has no way to know a reordering is meaningless, and because the
// merchant reads apply a LIMIT — an unstable order makes the cut-off row arbitrary.
//
// The ruling is ALPHABETICAL BY NAME, applied identically to both members of every pair. NULL
// ordering is left at the ASC default (NULLS LAST) deliberately: a NULL name sorts last, which is
// itself deterministic.
//
// ⚠ #1626 falsified this paragraph's second half, which used to claim a NULL name was
// "unreachable today — `effective_category_name` is scanned into a non-pointer string (a NULL
// already fails the scan)". #1626 Phase 2 replaced it read-by-read because the tree was MIXED at
// that point; #1626 E2 Part 2 converted the remaining four, so every read of this column is now
// tolerant. The read-by-read form is KEPT anyway: it is what makes a new intolerant read visible as
// an omission from a list rather than as a silent exception to a blanket claim.
//
// TOLERANT (7 reads, all scanning through `sql.NullString` and then taking .String): GetTopExpense,
// GetLargestTransactionsForPeriod, GetCategoryBreakdownForMerchant, GetIncomeSourceBreakdown and
// its household twin, GetUnusualSpend — six reading `t.effective_category_name` — plus
// GetCategoryTotalForPeriod, whose display is a `cc.name` scalar subquery (NULL::text for every PFC
// token by construction). For these a NULL is reachable and survives; the ordering stays
// deterministic (NULLS LAST) and no longer decides what the user sees, because the service re-sorts
// on the RESOLVED display where a NULL custom name has already become the neutral placeholder.
//
// ⚠ THE OWED FOLLOW-UP IS PAID (#1626 E2 Part 2), so this paragraph is REPLACED, not trimmed. It
// used to record four reads — GetSpendingByCategory and getSpendingByCategoryHousehold scanning
// `t.effective_category_name` into domain.SpendingByCategory.PFCPrimaryDisplay, GetSpendingTrends
// and getSpendingTrendsHousehold scanning it into domain.SpendingTrendRow.CategoryDisplay — as
// plain `string` scans, i.e. a latent 500, deferred to keep an earlier PR's blast radius
// reviewable. All four now scan through `sql.NullString` like the seven above, so the tree is no
// longer MIXED: every read of this column tolerates the NULL the schema expects. Pinned by
// TestAnalyticsReads_NullEffectiveCategoryName_DoNotError, which is an INTEGRATION test because a
// pgxmock NULL does not reproduce the failure — pgxmock converts Go values itself and never reaches
// pgx's NULL handling, so the same assertions passed against the unfixed code. The merchant name is
// unaffected either way: it still COALESCEs to the literal 'Unknown'.
//
// ⚠ Alphabetical alone does NOT determinise the MERCHANT reads. Their name is an aggregate
// (`MAX(COALESCE(...))`) whose chain bottoms out in the literal 'Unknown', while the GROUP BY is on
// a merchant-KEY expression, not the name. Several distinct keys — a merchants row literally named
// "Unknown", a raw `merchant_name = 'Unknown'`, and the no-merchant 'unknown' bucket — render the
// same display name and tie again on it. So the merchant reads carry the grouped key as a THIRD
// sort leg. Pinned by TestAnalyticsRepository_TieOrder_SpendingByMerchant_IsPlanIndependent.
//
// merchantGroupKeyExpr is that key. It is shared by GROUP BY and ORDER BY in both merchant reads so
// the two can never drift apart — ordering by an expression the query does not actually group on
// would be a planner error, not a silent reordering.
const merchantGroupKeyExpr = `CASE
		    WHEN t.merchant_id IS NOT NULL
		      THEN 'merchant:' || COALESCE(mno.merged_into_merchant_id, t.merchant_id)::text
		    WHEN NULLIF(t.merchant_name, '') IS NOT NULL
		      THEN 'raw:' || t.merchant_name
		    ELSE 'unknown'
		  END`

// effectiveTransferExpr returns the SQL expression for the effective PFC primary used in
// transfer filtering (`<expr> NOT LIKE 'TRANSFER...'`).
//
// Issue #1609 Item 11 — WHY THERE IS A SECOND ARM. effective_pfc_primary is a GENERATED column
// (migration 000073) that is NULL for every custom-categorised row, because a custom category
// takes precedence over the PFC dimension. Reading it alone therefore made EVERY such row escape
// the transfer filter: NULL -> COALESCE -> the empty string, which NOT LIKE 'TRANSFER...' accepts.
// A category named "Paycheck" and one named "Internal Transfer To Savings" were therefore included
// identically, by accident of NULL-ness rather than by declaration. A real user was advised to "assign a custom
// category to reclassify these as income", which would have appeared to work for exactly that
// wrong reason. custom_categories.parent_pfc_primary already declares which PFC primary the
// category nests under (CHECK-constrained to the 18 valid primaries by migration 000065) and is
// already honoured by budget roll-up; this resolves through it so analytics agrees.
//
// CORRELATED SUBQUERY, NOT A JOIN, and deliberately: this expression is spliced into a WHERE
// clause that is already assembled (there is no FROM list left to extend at the splice point,
// and buildSpendingFilter's output is reused by ~15 further queries including CTE-based household
// variants). COALESCE evaluates left to right and short-circuits, so the lookup runs ONLY for the
// rows whose effective_pfc_primary is NULL — the custom-categorised minority — and each one is a
// primary-key point lookup on a small per-user table.
//
// A category with parent_pfc_primary IS NULL still resolves to the empty string and keeps today's
// behaviour; only an explicit TRANSFER_IN / TRANSFER_OUT changes an answer. Rows with no override at all are
// untouched, because custom_category_override_id is NULL for them (CHECK
// valid_category_override_discriminator, migration 000065, sets it if and only if the override
// dimension is the custom one — which is why this arm needs no discriminator test of its own).
//
// Pinned by TestCashFlow_CustomCategoryResolvesParentPFCPrimaryForTransferFilter_Integration.
func effectiveTransferExpr() string {
	return `COALESCE(
		    t.effective_pfc_primary,
		    (SELECT cc.parent_pfc_primary
		       FROM custom_categories cc
		      -- tombstone-exempt(custom_categories cc): HISTORY FILTER (#1609 Item 11). A transaction
		      -- filed under a since-deleted custom category is still real history, and the category
		      -- it was filed under still declares what it was. An 'AND cc.deleted_at IS NULL' here
		      -- would silently re-admit those rows to the non-transfer universe the day the user
		      -- tidied their category list, restating a closed month. Same rule, and same reason, as
		      -- the budget roll-up subqueries in transaction_repository.go.
		      WHERE cc.id = t.custom_category_override_id
		        AND cc.user_id = t.user_id),
		    '')`
}

// buildPlaceholders creates numbered placeholders ($1, $2, ...) for parameterized query building
func buildPlaceholders(count, startIndex int) []string {
	placeholders := make([]string, count)
	for i := 0; i < count; i++ {
		placeholders[i] = fmt.Sprintf("$%d", startIndex+i)
	}
	return placeholders
}

// householdGrantsCTE returns the canonical household-aggregation fan-out: every grant that ever
// made a member's account visible to household $2, as a half-open INTERVAL.
//
// Household inclusion is governed SOLELY by the share (visibility=shared, and the interval below)
// -- is_hidden on user_accounts/accounts is the owner's own personal-view control and is
// deliberately ignored here (Issue #1369 decision #3; migration 000110's documented contract).
//
// ── #1569 T23: this CTE emits INTERVALS; the caller decides inclusion with a SEMI-JOIN ───────────
//
// It used to be `DISTINCT ON (COALESCE(has.physical_account_blind_idx, ua.id::text))` filtered by
// `has.revoked_at IS NULL AND ua.user_id = ANY($3)`, and every caller JOINed to it. Two consequences,
// one of them a bug this task exists to fix:
//
//   - A departed member's ENTIRE past vanished from household spending, income and savings-rate --
//     not merely the part after they left. The balance/net-worth reads had already been fixed to
//     honour the shared period (householdAccountsCTEForBalancesPerDay, ...AsOf); these nine reads
//     had not, so the two answered about the same days and disagreed. They now agree.
//   - Retaining closed grants under a JOIN would have been WORSE than the bug. The dedup key is the
//     physical-account FINGERPRINT, not the user_account, so one user_account with two closed grants
//     (a re-fingerprinted account) emits two rows; a JOIN on ha.user_account_id then matches every
//     transaction twice and inflates SUM() and COUNT(). Silent money inflation across nine
//     endpoints is worse than under-reporting.
//
// So the JOIN is gone. Callers use householdSharedTransactionPredicate, an EXISTS: a transaction is
// included because SOME covering grant exists, and EXISTS yields at most one row no matter how many
// grants cover it. The double-count is not collapsed away, it is unrepresentable.
//
// ── What this does NOT relax: the membership guard ───────────────────────────────────────────────
//
// `ua.user_id = ANY($3) OR has.revoked_at IS NOT NULL` keeps the pre-#1569 active-membership
// requirement for OPEN grants and lifts it only for CLOSED ones -- identical to the balance CTEs,
// see householdAccountsCTEForBalancesAsOf for the full reasoning. A closed grant can only exist for
// someone who WAS accepted into this household (InsertShare runs in the accept fan-out and nowhere
// else), and the caller's interval predicate bounds WHEN it applies, so admitting it cannot show a
// stranger's data nor any day outside the window the household was legitimately shown.
//
// ── #1569 T3 (#1582): two performance properties this CTE carries ────────────────────────────────
//
//  1. `fingerprint_contested` — a window-computed flag saying whether MORE THAN ONE user_account
//     in this household holds this fingerprint. The predicate's inner joint-account tiebreak is a
//     correlated NOT EXISTS whose only matching condition is `g2.user_account_id <> g.user_account_id`
//     on the SAME fingerprint, so when exactly one user_account holds the fingerprint the subquery
//     is PROVABLY VACUOUS and skipping it is an identity, not an approximation. That is always the
//     case for mask-less accounts, whose fingerprint COALESCEs to their own unique `ua.id`.
//
//     ⚠ It MUST be computed over EVERY grant, closed ones included. Deciding contestedness from the
//     open grants alone makes a joint account whose winning copy has been revoked look uncontested,
//     skips the tiebreak, and counts that account TWICE. Pinned by
//     TestAnalyticsRepository_GetSpendingByCategory_Household_JointAccountWithClosedGrantCountedOnce,
//     which fails at $120-vs-$60 under exactly that mutation.
//
//  2. `share_started_at` / `share_ended_at` are emitted ALREADY converted to UTC wall time, so the
//     predicate's per-(transaction, grant) comparison does not re-convert them. The comparison rule
//     itself is unchanged — see householdSharedTransactionPredicate, whose two bounds are
//     mutation-pinned.
//
// ⚠ THE MEASUREMENTS, THE REJECTED ALTERNATIVES AND THE ANTI-JOIN REFUTATION ARE NOT HERE. They are
// docs/decisions/2026-08-26-household-grants-cte-performance.md, which carries the round-5 psql
// table and its per-measurement protocol (four of its six rows are hand-run and TestT3Bench cannot
// reproduce them), the AS NOT MATERIALIZED and inline-derived-table results, the round-6 proof that
// the `(fingerprint, day)` anti-join makes money DISAPPEAR, and the one direction still worth
// trying. This block was ~180 lines of measurement history in-line and shifted every citation below
// it three times on one branch; that churn is why it is an ADR now. Two facts a reader needs before
// touching this function: #1582's "within ~2x" is NOT met at HEAD (6.00x), and option (b) is refuted
// on OUTPUT, not on speed -- do not re-propose it in any transcription.
//
// Pinned by TestAnalyticsRepository_GetSpendingByCategory_Household_DepartedMemberSharedPeriodRetained.
func householdGrantsCTE() string {
	// The unused `$1::uuid AS _requesting_user_id` column exists solely to pin $1's type.
	// $1 is not otherwise referenced by every household query variant (only
	// buildSpendingFilter's custom-category subquery references it, and only when
	// CustomCategoryIDs is non-empty) -- without a typed reference somewhere in the query
	// text, Postgres's extended query protocol cannot infer $1's type and errors with
	// "could not determine data type of parameter $1" (SQLSTATE 42P18).
	return `household_grants AS (
		SELECT
			ua.id                                                 AS user_account_id,
			COALESCE(has.physical_account_blind_idx, ua.id::text) AS fingerprint,
			(has.shared_at AT TIME ZONE 'UTC')                    AS share_started_at,
			(has.revoked_at AT TIME ZONE 'UTC')                   AS share_ended_at,
			MIN(ua.id::text) OVER (PARTITION BY COALESCE(has.physical_account_blind_idx, ua.id::text))
			  <> MAX(ua.id::text) OVER (PARTITION BY COALESCE(has.physical_account_blind_idx, ua.id::text))
			                                                      AS fingerprint_contested,
			$1::uuid                                              AS _requesting_user_id
		FROM household_account_shares has
		JOIN user_accounts ua ON ua.id = has.user_account_id
		WHERE has.household_id = $2
		  AND has.visibility  = 'shared'
		  AND (ua.user_id = ANY($3) OR has.revoked_at IS NOT NULL)
	)`
}

// householdSharedTransactionPredicate returns the SQL boolean deciding whether one transaction row
// belongs in a household aggregate. txAlias is the transactions alias (its `.date` is read),
// accountAlias the accounts alias (its `.user_account_id` is read). It requires householdGrantsCTE
// to be in scope as `household_grants`.
//
// ── Why EXISTS and not a JOIN ────────────────────────────────────────────────────────────────────
//
// A JOIN emits one output row per matching grant, so an account covered by two grant intervals on
// the same day would have each of its transactions counted twice. EXISTS is a semi-join: it answers
// yes-or-no once per transaction, so overlapping, re-issued and re-fingerprinted grants cannot
// affect any SUM() or COUNT(). That is the property that made retaining closed grants safe here at
// all, and it is structural rather than a rule someone has to remember.
//
// ── Clipping is at DATE resolution here, and at INSTANT resolution on the balance side ───────────
//
// This is a considered difference, not an inconsistency. The balance reads clip `bs.snapshot_at`,
// a TIMESTAMPTZ, so they can and do compare instants. `transactions.date` is a DATE: a transaction
// carries no time of day, so there is no instant to compare and an instant-resolution predicate
// here would silently compare the day's midnight against the grant boundary -- exact-looking and
// wrong. What a DATE genuinely denotes is the whole day [date, date+1).
//
// ── The rule, applied identically at BOTH ends ───────────────────────────────────────────────────
//
// A transaction is inside a grant when the DAY it denotes OVERLAPS the half-open grant interval
// [share_started_at, share_ended_at):
//
//	day_end   > share_started_at   ->  (t.date + 1) > share_started_at
//	day_start < share_ended_at     ->  t.date       < share_ended_at
//
// Task 15's F4 found a closing bound argued from a leak-prevention principle that was never applied
// to the opening bound, which was quietly admitting pre-grant data. So both ends here are the SAME
// interval-overlap test, with the same tolerance in the same direction: a boundary day counts if
// ANY instant of it was shared. Being strict at one end and generous at the other is the defect,
// not the fix.
//
// The opening leg is exactly equivalent to the `t.date >= share_started_at::date`
// this replaced -- for a grant made at 18:00, the whole of that date was already included -- so the
// share-start behaviour is unchanged and stays pinned by
// TestAnalyticsRepository_GetSpendingByCategory_Household_ClipsToSharedAt. The closing leg is new,
// and it correctly excludes a revocation timestamp of exactly midnight (no instant of that date was
// shared) while including a revocation at 18:00.
//
// ── The joint-account winner is now resolved PER TRANSACTION ─────────────────────────────────────
//
// Two members who each link the SAME real-world bank account hold two different user_accounts with
// two separate copies of the same transactions, so exactly one copy may be counted (migration
// 000110's documented purpose for physical_account_blind_idx). The old CTE picked that winner ONCE,
// globally, with DISTINCT ON. That cannot survive retention: a copy shared only for part of the
// range would otherwise erase the other member's genuinely-earlier data from the whole range -- the
// same defect householdAccountsCTEForBalancesPerDay had to solve per day.
//
// So the winner is chosen among the grants COVERING THAT TRANSACTION, by the same order the
// DISTINCT ON used (earliest share_started_at, ties broken by user_account_id). The inner NOT EXISTS
// says "no OTHER user_account with my fingerprint, also covering this day, outranks me". Because a
// minimum is unique, exactly one user_account per (fingerprint, day) can pass -- so a joint account
// is still counted once, and a re-fingerprinted account never loses to itself
// (`g2.user_account_id <> g.user_account_id`).
//
// ── #1569 T3: the tiebreak is SKIPPED when the fingerprint is held by one user_account ───────────
//
// `NOT g.fingerprint_contested OR NOT EXISTS (...)` is not a fast path with a different answer: when
// only one user_account in household_grants holds g's fingerprint, the inner query's
// `g2.user_account_id <> g.user_account_id` can never be satisfied, so NOT EXISTS is already true.
// The short-circuit removes an evaluation, never a row. See householdGrantsCTE for why
// `fingerprint_contested` counts closed grants, and that function's cited ADR for the measurement.
//
// Pinned by TestAnalyticsRepository_GetSpendingByCategory_Household_JointAccountCountedOnce and
// TestAnalyticsRepository_GetSpendingByCategory_Household_RefingerprintedClosedGrantsCountedOnce.
func householdSharedTransactionPredicate(txAlias, accountAlias string) string {
	covers := func(grantAlias string) string {
		return fmt.Sprintf(
			`(%[1]s.date + 1) > %[2]s.share_started_at
			   AND (%[2]s.share_ended_at IS NULL OR %[1]s.date < %[2]s.share_ended_at)`,
			txAlias, grantAlias)
	}
	return fmt.Sprintf(`EXISTS (
			SELECT 1 FROM household_grants g
			WHERE g.user_account_id = %[1]s.user_account_id
			  AND %[2]s
			  AND (NOT g.fingerprint_contested OR NOT EXISTS (
			      SELECT 1 FROM household_grants g2
			      WHERE g2.fingerprint = g.fingerprint
			        AND g2.user_account_id <> g.user_account_id
			        AND %[3]s
			        AND (g2.share_started_at, g2.user_account_id) < (g.share_started_at, g.user_account_id)
			  ))
		)`, accountAlias, covers("g"), covers("g2"))
}

// householdScopeArgs returns the three leading args every household-aware query must pass,
// in order: requesting userID (kept as $1), household ID ($2), active member IDs ($3).
func householdScopeArgs(requestingUserID string, scope *domain.HouseholdScope) []interface{} {
	return []interface{}{requestingUserID, scope.HouseholdID, scope.MemberUserIDs}
}

// householdAccountsCTEForBalancesPerDay is the historical-aggregation sibling of the balance
// CTEs, used by GetAggregatedHistoryForHousehold. Unlike householdAccountsCTEForBalancesAsOf
// (a single point-in-time query), a date-range history query must choose a joint-account
// fingerprint winner PER CALENDAR DAY — a copy that only started syncing mid-range must not
// erase the other member's genuinely-earlier data from the whole series (Codex adversarial
// review P1-2, deferred fast-follow #1378). So this CTE does NOT collapse fingerprint
// collisions to one row; it exposes every shared user_account_id along with its physical
// fingerprint and its own interval bounds, leaving all per-day resolution to the caller. Same
// three leading args as householdGrantsCTE: $1 = requesting userID (pins its type; unused
// otherwise here), $2 = household ID, $3 = active member user IDs.
//
// ── #1569 T7: this CTE emits INTERVALS, and the caller clips per day ─────────────────────────
//
// It used to filter `revoked_at IS NULL AND ua.user_id = ANY($3)` — two independent reasons a
// departed member vanished from the household net-worth chart. Not clipped at the departure:
// erased from every day BEFORE it too, so the series a household saw for a shared period went to
// zero the moment someone left. That is the most visible instance of the defect #1569 exists to
// fix, so this read now answers from the same interval, on the same half-open rule, at the same
// instant resolution as the point-in-time one.
//
// The row therefore carries share_ended_at (the exclusive end, NULL when current) and
// share_row_id, and the membership guard is relaxed for CLOSED intervals only — identical
// reasoning to householdAccountsCTEForBalancesAsOf's, see there. What is NOT here is the
// interval predicate itself: a range read applies it per row of the aggregation rather than once
// for the query, so the clip lives in the caller's per-day stage. It is the SAME half-open
// interval evaluated at the SAME instant resolution the point-in-time CTE uses — a date-resolution
// clip would not mirror it, and got both bounds wrong in opposite directions before it was
// corrected; see GetAggregatedHistoryForHousehold's per_account_per_day WHERE clause.
//
// ⚠ The consequence a caller MUST handle: one user_account can now match SEVERAL rows of this
// CTE. Closed intervals are unconstrained by uq_household_account_shares_active and may overlap,
// and a re-fingerprinted account produces rows whose `fingerprint` differs — so a caller that
// keys its dedup on fingerprint alone drops ONE account into two buckets and sums its balance
// twice. GetAggregatedHistoryForHousehold collapses per (day, user_account_id) first for exactly
// this reason; any new consumer must do the same.
func householdAccountsCTEForBalancesPerDay() string {
	return `household_accounts AS (
		SELECT
			ua.id                                                 AS user_account_id,
			COALESCE(has.physical_account_blind_idx, ua.id::text) AS fingerprint,
			has.shared_at                                         AS share_started_at,
			has.revoked_at                                        AS share_ended_at,
			has.id                                                AS share_row_id,
			$1::uuid                                              AS _requesting_user_id
		FROM household_account_shares has
		JOIN user_accounts ua ON ua.id = has.user_account_id
		WHERE has.household_id = $2
		  AND has.visibility  = 'shared'
		  AND (ua.user_id = ANY($3) OR has.revoked_at IS NOT NULL)
	)`
}

// householdAccountsCTEForBalancesAsOf is the asOf-bounded, single-instant sibling of
// householdAccountsCTEForBalancesPerDay, used by point-in-time balance lookups
// (GetSnapshotsAsOfForHousehold, GetForHouseholdScopeActiveNow, GetForHouseholdScopeLiveAsOf) that are
// evaluated at a specific instant — both the "current" call (asOf=now) and a "previous period"
// call (asOf=now-30d) reuse this same query, so the joint-account fingerprint winner must be
// picked using "latest snapshot AT OR BEFORE asOf ($4)", not "latest snapshot ever". Without
// that bound, a previous-period query could pick the globally-freshest member's copy even when
// THAT copy has no snapshot before the cutoff, silently returning zero data for the joint
// account instead of falling back to the other member's genuinely earlier balance (Codex
// adversarial review, P1-2). Same three leading args as householdAccountsCTEForBalancesPerDay,
// PLUS $4 = the asOf cutoff (reused both for the winner-selection bound here and the caller's
// own snapshot_at <= $4 filter in the outer query).
//
// ── #1569 T7: the grant is a HALF-OPEN INTERVAL, and this read honours it ────────────────────
//
// A household_account_shares row is not a boolean "is this shared right now?". It is the
// household's own record that user_account X was visible to it over [shared_at, revoked_at) —
// inclusive at shared_at, EXCLUSIVE at revoked_at, the same shape account_status_history uses
// for [valid_from, valid_to). This CTE used to read `revoked_at IS NULL` — i.e. it answered
// "shared TODAY?" for every asOf, including asOf values months in the past. The product
// decision is the opposite: a member leaves and the remaining members keep what was shared FOR
// THE SHARED PERIOD ONLY. They must not gain visibility of anything after the member left, and
// must not LOSE visibility of the period when sharing was live. `shared_at <= $4 AND
// (revoked_at IS NULL OR revoked_at > $4)` is exactly that, in both directions.
//
// The boundary is load-bearing and easy to get wrong (an earlier task on this branch shipped a
// `valid_to >` vs `>=` bug that a whole integration package missed): asOf == revoked_at is
// OUTSIDE. Pinned by TestHouseholdShareAsOf_IntervalIsHalfOpen.
//
// ── What this does NOT relax: the membership guard ───────────────────────────────────────────
//
// `ua.user_id = ANY($3) OR has.revoked_at IS NOT NULL` deliberately keeps the pre-#1569
// active-membership requirement for OPEN grants and lifts it only for CLOSED ones. A closed
// grant can only exist for someone who WAS accepted into this household (InsertShare runs in
// the accept fan-out and nowhere else), and the interval predicate above already bounds WHEN it
// applies — so admitting it cannot show a stranger's data, nor any instant outside the window
// the household was legitimately shown. Meanwhile an OPEN grant still requires active
// membership, exactly as before — pinned by
// TestHouseholdShareAsOf_OpenGrantStillRequiresActiveMembership.
//
// Precisely, for a "current" call (asOf = the read's own instant) the only rows the relaxed
// clause can add are grants revoked AFTER that instant, i.e. revoked concurrently with the
// request. Those were open at the instant the caller asked about, so including them is the
// point-in-time answer, not a widening of who may be seen: the departed member's membership
// was live at asOf too. Anything revoked before asOf is filtered by the interval predicate
// before the membership clause is ever reached.
//
// ── Why the dedup is now two-stage ───────────────────────────────────────────────────────────
//
// uq_household_account_shares_active constrains OPEN rows only. Once closed rows are retained
// (they are — PurgeRemovedMembership no longer deletes them; see its own comment), a single
// asOf instant can match SEVERAL rows for one (household, user_account): re-shares overlap, and
// a re-fingerprinted account produces rows whose physical_account_blind_idx differs. Keying the
// joint-account dedup on the fingerprint alone would then drop that one account into two
// buckets and emit it TWICE. household_grants_asof collapses to one row per user_account FIRST,
// picking the interval actually in force (latest shared_at <= asOf, ties broken by has.id so
// the answer never varies between runs) — a nondeterministic as-of answer is worse than a
// missing one. household_accounts then does the joint-account fingerprint dedup on top, exactly
// as before. Pinned by TestHouseholdShareAsOf_OverlappingHistoricalRowsAreDeterministic.
func householdAccountsCTEForBalancesAsOf() string {
	return `household_grants_asof AS (
		SELECT DISTINCT ON (has.user_account_id)
			has.user_account_id,
			has.physical_account_blind_idx,
			has.shared_at
		FROM household_account_shares has
		JOIN user_accounts ua ON ua.id = has.user_account_id
		WHERE has.household_id = $2
		  AND has.visibility  = 'shared'
		  AND has.shared_at  <= $4
		  AND (has.revoked_at IS NULL OR has.revoked_at > $4)
		  AND (ua.user_id = ANY($3) OR has.revoked_at IS NOT NULL)
		ORDER BY has.user_account_id, has.shared_at DESC, has.id
	),
	household_accounts AS (
		SELECT DISTINCT ON (COALESCE(g.physical_account_blind_idx, ua.id::text))
			ua.id        AS user_account_id,
			ua.user_id   AS owner_user_id,
			g.shared_at  AS share_started_at,
			$1::uuid     AS _requesting_user_id
		FROM household_grants_asof g
		JOIN user_accounts ua ON ua.id = g.user_account_id
		LEFT JOIN LATERAL (
			SELECT MAX(bs.snapshot_at) AS latest_snapshot_at
			FROM balance_snapshots bs
			WHERE bs.user_account_id = ua.id AND bs.snapshot_at <= $4
		) latest ON true
		ORDER BY COALESCE(g.physical_account_blind_idx, ua.id::text), latest.latest_snapshot_at DESC NULLS LAST, ua.id
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
					"SELECT id FROM custom_categories WHERE user_id = $1::uuid AND (id::text IN (%s) OR parent_id::text IN (%s)))"+
					" /* tombstone-exempt(custom_categories): HISTORY FILTER (#1569 Task 9). Analytics answer questions"+
					" about the past. A report scoped to a category the user has since deleted must"+
					" still return the spend that happened in it; filtering here would quietly"+
					" restate a closed month. */",
				idList, idList,
			)
		} else {
			// #1369 gap-closure G4: this branch previously had no ownership check at all
			// (unlike the RollUpChildren=true branch above), inert in personal mode only
			// because the outer query's `t.user_id = $1` already constrained results to the
			// requester's own transactions. Household mode's cross-member fan-out makes it
			// live — namespace it the same way, without the parent_id OR-widening.
			clause += fmt.Sprintf(
				" AND t.category_override_kind = 'custom' AND t.custom_category_override_id IN ("+
					"SELECT id FROM custom_categories WHERE user_id = $1::uuid AND id::text IN (%s))"+
					" /* tombstone-exempt(custom_categories): HISTORY FILTER (#1569 Task 9) — same as the RollUpChildren"+
					" branch above, without the parent_id widening. */",
				idList,
			)
		}
	}

	return clause, args, argIndex
}

// GetSpendingByCategory returns expense totals grouped by effective primary category, ordered by
// total descending and then alphabetically by category display name (#1585).
// When filters.Category is non-empty, returns the detailed subcategory breakdown.
func (r *analyticsRepository) GetSpendingByCategory(ctx context.Context, userID string, filters domain.SpendingFilters) ([]domain.SpendingByCategory, error) {
	if filters.HouseholdScope != nil {
		return r.getSpendingByCategoryHousehold(ctx, userID, filters)
	}
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
		  AND t.deleted_at IS NULL
		  AND t.date >= $2
		  AND t.date <= $3
		  AND t.amount > 0
		  AND t.pending = false
		%s%s
		GROUP BY category, category_display
		ORDER BY total DESC, category_display ASC
	`, catExpr, displayExpr, categoryFilter, extraClause)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get spending by category: %w", err)
	}
	defer rows.Close()

	var results []domain.SpendingByCategory
	for rows.Next() {
		var row domain.SpendingByCategory
		// #1626 E2: sql.NullString, not string. effective_category_name is nullable BY DESIGN
		// (migration 000073 leaves it nullable and indexes it partially on IS NOT NULL) and is
		// refreshed asynchronously, so a custom-categorised row legitimately carries no name yet —
		// and pgx v5 fails the scan on NULL rather than yielding "", which was a 500 for the whole
		// report. The placeholder is resolveCategoryDisplay's decision in the service, not this
		// layer's. Pinned by TestAnalyticsReads_NullEffectiveCategoryName_DoNotError.
		var display sql.NullString
		if err := rows.Scan(&row.Category, &display, &row.Total, &row.TransactionCount); err != nil {
			return nil, fmt.Errorf("failed to scan spending by category: %w", err)
		}
		row.PFCPrimaryDisplay = display.String
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
		JOIN accounts a ON t.account_id = a.id
		WHERE %s
		  AND t.date >= $4::date
		  AND t.deleted_at IS NULL
		  AND t.date <= $5
		  AND t.amount > 0
		  AND t.pending = false
		%s%s
		GROUP BY category, category_display
		ORDER BY total DESC, category_display ASC
	`, householdGrantsCTE(), catExpr, displayExpr, householdSharedTransactionPredicate("t", "a"), categoryFilter, extraClause)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get household spending by category: %w", err)
	}
	defer rows.Close()

	var results []domain.SpendingByCategory
	for rows.Next() {
		var row domain.SpendingByCategory
		// Nullable display, same as the personal read above — see there.
		var display sql.NullString
		if err := rows.Scan(&row.Category, &display, &row.Total, &row.TransactionCount); err != nil {
			return nil, fmt.Errorf("failed to scan household spending by category: %w", err)
		}
		row.PFCPrimaryDisplay = display.String
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating household spending by category rows: %w", err)
	}

	return results, nil
}

// GetSpendingByMerchant returns expense totals grouped by effective merchant, ordered by total
// descending, ties broken alphabetically by merchant name and then by the grouped merchant key
// (#1585 — see merchantGroupKeyExpr for why the name alone is not enough here).
// Merchants renamed or merged by the user are grouped under their canonical (target) display name.
// Groups by a synthetic key: 'merchant:<id>', 'raw:<name>', or 'unknown' — so renamed/merged variants
// collapse into one row. merchant_id is NULL for unlinked/unknown groups (preserves "not clickable" frontend behavior).
func (r *analyticsRepository) GetSpendingByMerchant(ctx context.Context, userID string, filters domain.SpendingFilters, limit int) ([]domain.SpendingByMerchant, error) {
	if filters.HouseholdScope != nil {
		return r.getSpendingByMerchantHousehold(ctx, userID, filters, limit)
	}

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
		  AND t.deleted_at IS NULL
		  AND t.date >= $2
		  AND t.date <= $3
		  AND t.amount > 0
		  AND t.pending = false
		` + extraClause + `
		GROUP BY ` + merchantGroupKeyExpr + `
		ORDER BY total DESC, merchant_name ASC, ` + merchantGroupKeyExpr + ` ASC
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
	args := householdScopeArgs(requestingUserID, filters.HouseholdScope)
	args = append(args, filters.StartDate, filters.EndDate)
	extraClause, args, argIndex := buildSpendingFilter(filters, args)

	args = append(args, limit)
	limitPlaceholder := fmt.Sprintf("$%d", argIndex)

	// Plain concatenation, NOT fmt.Sprintf — extraClause can contain a literal '%' (e.g.
	// `LIKE 'TRANSFER%'`), which fmt.Sprintf would misinterpret as a format verb and
	// corrupt the query (matches the personal-path GetSpendingByMerchant above, which
	// concatenates for the same reason).
	query := "WITH " + householdGrantsCTE() + `
		SELECT
			MAX(COALESCE(mno_t.display_name, NULLIF(m2.normalized_name, ''), m2.name, NULLIF(t.merchant_name, ''), 'Unknown')) AS merchant_name,
			MAX(COALESCE(mno.merged_into_merchant_id, t.merchant_id)::text)                                                    AS merchant_id,
			COALESCE(SUM(ABS(t.amount)), 0)                                                                                    AS total,
			COUNT(*)                                                                                                            AS transaction_count
		FROM transactions t
		JOIN accounts a ON t.account_id = a.id
		` + merchantSortJoins + `
		WHERE t.deleted_at IS NULL
		  AND ` + householdSharedTransactionPredicate("t", "a") + `
		  AND t.date >= $4::date
		  AND t.date <= $5
		  AND t.amount > 0
		  AND t.pending = false
		` + extraClause + `
		GROUP BY ` + merchantGroupKeyExpr + `
		ORDER BY total DESC, merchant_name ASC, ` + merchantGroupKeyExpr + ` ASC
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
// Results are ordered by period_start ASC, total DESC, then alphabetically by category display
// name so that equal-total categories have a documented, plan-independent order (#1585).
func (r *analyticsRepository) GetSpendingTrends(ctx context.Context, userID string, filters domain.SpendingFilters) ([]domain.SpendingTrendRow, error) {
	if filters.HouseholdScope != nil {
		return r.getSpendingTrendsHousehold(ctx, userID, filters)
	}

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
		  AND t.deleted_at IS NULL
		  AND t.date >= $2
		  AND t.date <= $3
		  AND t.amount > 0
		  AND t.pending = false
		%s
		GROUP BY period_start, category, category_display
		ORDER BY period_start ASC, total DESC, category_display ASC
	`, catExpr, displayExprTrends, extraClause)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get spending trends: %w", err)
	}
	defer rows.Close()

	var results []domain.SpendingTrendRow
	for rows.Next() {
		var row domain.SpendingTrendRow
		// Nullable display, same as GetSpendingByCategory — see there.
		var display sql.NullString
		if err := rows.Scan(&row.PeriodStart, &row.Category, &display, &row.Total); err != nil {
			return nil, fmt.Errorf("failed to scan spending trend row: %w", err)
		}
		row.CategoryDisplay = display.String
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
		JOIN accounts a ON t.account_id = a.id
		WHERE %s
		  AND t.date >= $4::date
		  AND t.deleted_at IS NULL
		  AND t.date <= $5
		  AND t.amount > 0
		  AND t.pending = false
		%s
		GROUP BY period_start, category, category_display
		ORDER BY period_start ASC, total DESC, category_display ASC
	`, householdGrantsCTE(), catExpr, displayExprTrends, householdSharedTransactionPredicate("t", "a"), extraClause)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get household spending trends: %w", err)
	}
	defer rows.Close()

	var results []domain.SpendingTrendRow
	for rows.Next() {
		var row domain.SpendingTrendRow
		// Nullable display, same as GetSpendingByCategory — see there.
		var display sql.NullString
		if err := rows.Scan(&row.PeriodStart, &row.Category, &display, &row.Total); err != nil {
			return nil, fmt.Errorf("failed to scan household spending trend row: %w", err)
		}
		row.CategoryDisplay = display.String
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating household spending trends rows: %w", err)
	}

	return results, nil
}

// GetCategoryTotalForPeriod returns the total expense amount for a specific effective category.
// Matches both PFC primary categories (e.g. "FOOD_AND_DRINK") and custom categories ("custom:<uuid>").
// Expenses are amount > 0 (PFCv2 convention). Returns a single row of ZEROES when no matching
// transactions exist — the aggregate is ungrouped, so there is no no-rows case (see the scan below).
// customCategoryIDFromToken splits a `custom:<uuid>` grouping key. Kept beside its only caller
// rather than shared: effectiveCategoryExpr's token format is an implementation detail of these
// reads, and a widely-imported helper would invite parsing the token in places that should be
// handling display through resolveCategoryDisplay instead (#1626).
//
// ⚠ IT PARSES THE SUFFIX AS A UUID, AND THAT IS WHAT MAKES THE REPOSITORY SAFE BY CONSTRUCTION
// (#1626 gap G9). categoryTotalQuery(true) emits `cc.id = $5::uuid`, so the suffix is CAST by
// Postgres rather than compared as text, and an unparseable one fails the whole statement — which
// MapError classifies as a 500. services.validateCategoryToken already rejects that shape with a
// 400, but it guards the SERVICE, not the cast: any other repository caller reaching
// GetCategoryTotalForPeriod directly would get the 500 back, with nothing in the query's shape to
// warn them. Returning false here means the uuid subquery is never emitted and no uuid is ever
// bound, so a malformed token degrades to the plain text comparison it had before that subquery
// existed — it matches no transaction and answers zero.
//
// ⚠ IT SAYS NOTHING ABOUT WHETHER THE CATEGORY EXISTS, and must not: a valid uuid naming a
// tombstoned or absent custom category is a legitimate request that answers with a zero total and
// resolveCategoryDisplay's "Custom category" label. Only the SHAPE is checked.
//
// ⚠ uuid.Parse, not a regexp, and the SAME call the service layer makes: it is the parser the
// ::uuid cast will apply, so this gate can neither admit a spelling Postgres would reject nor
// reject one it would accept.
func customCategoryIDFromToken(category string) (string, bool) {
	const tok = "custom:"
	if !strings.HasPrefix(category, tok) {
		return "", false
	}
	id := strings.TrimPrefix(category, tok)
	if id == "" {
		return "", false
	}
	if _, err := uuid.Parse(id); err != nil {
		return "", false
	}
	return id, true
}

// categoryTotalQuery builds GetCategoryTotalForPeriod's statement.
//
// ⚠ WHY THE DISPLAY IS A SCALAR SUBQUERY AND NOT MAX(). This read used
// MAX(t.effective_category_name), defended by "the WHERE pins one category, so MAX picks that
// category's name rather than choosing between rivals". That is true of the CATEGORY and false of
// the NAME: effective_category_name is denormalised per transaction and refreshed ASYNCHRONOUSLY
// after a rename, so mid-refresh one category's rows legitimately carry BOTH names — and MAX is
// lexicographic. It returns neither the stale nor the fresh name but whichever sorts higher, which
// is an arbitrary answer wearing the costume of a deterministic one.
//
// A scalar subquery keeps this a one-row QueryRow aggregate. Selecting cc.name directly would be
// invalid without grouping, and grouping risks multiple rows in ghost states.
//
// ⚠ The subquery is emitted ONLY for a custom token, and the uuid arrives as $5 rather than as a
// substring of $4. An unconditional `substring($4 from 8)::uuid` is a cast Postgres may evaluate
// before the guarding LIKE, turning every PFC-token call into invalid-input-syntax.
func categoryTotalQuery(isCustomToken bool) string {
	display := "NULL::text"
	if isCustomToken {
		// The exemption comment lives INSIDE the literal because the guard scans SQL strings, not
		// the Go comments around them — a note up here excuses nothing.
		display = `(
				-- tombstone-exempt(custom_categories cc): HISTORY FILTER. A report for a category the
				-- user has since deleted must still NAME it: the spend is retained history (#1569),
				-- and a tombstoned category reverting to a raw UUID in the report is the #1626 bug
				-- coming back through the door this very method was fixed to close.
				SELECT cc.name FROM custom_categories cc
				WHERE cc.id = $5::uuid AND cc.user_id = $1::uuid
			)`
	}

	return fmt.Sprintf(`
		SELECT
			COALESCE(SUM(t.amount), 0)                    as total,
			COUNT(*)                                      as transaction_count,
			%s                                            as category_display
		FROM transactions t
		WHERE t.user_id = $1
		  AND t.deleted_at IS NULL
		  AND t.date >= $2
		  AND t.date <= $3
		  AND t.amount > 0
		  AND t.pending = false
		  AND (%s) = $4
	`, display, effectiveCategoryExpr(false))
}

func (r *analyticsRepository) GetCategoryTotalForPeriod(ctx context.Context, userID string, category string, startDate time.Time, endDate time.Time) (*domain.CategoryTotalRow, error) {
	// #1626 Phase 2: a custom token resolves its name from custom_categories; a PFC token has no
	// row there and selects NULL, which resolveCategoryDisplay turns into the registry label.
	customID, isCustomToken := customCategoryIDFromToken(category)
	query := categoryTotalQuery(isCustomToken)

	args := []interface{}{userID, startDate, endDate, category}
	if isCustomToken {
		args = append(args, customID)
	}

	var row domain.CategoryTotalRow
	// Nullable: NULL for every PFC token by construction, and for a custom token whose category
	// row is gone.
	//
	// ⚠ An empty period returns ONE row of zeroes, not no rows: categoryTotalQuery is an UNGROUPED
	// aggregate (COALESCE(SUM(...),0), COUNT(*)), so Postgres always emits exactly one row and
	// QueryRow can never yield ErrNoRows here. An earlier revision of this comment claimed the
	// opposite; nothing downstream relies on the no-rows case because there is none.
	var display sql.NullString
	err := r.db.QueryRow(ctx, query, args...).Scan(
		&row.Total,
		&row.TransactionCount,
		&display,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get category total for period: %w", err)
	}
	row.CategoryDisplay = display.String
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
			t.effective_category_name     as category_display,
			t.date
		FROM transactions t
		WHERE t.user_id = $1
		  AND t.deleted_at IS NULL
		  AND t.date >= $2
		  AND t.date <= $3
		  AND t.amount > 0
		  AND t.pending = false
		ORDER BY t.amount DESC
		LIMIT 1
	`, catExpr)

	var row domain.TopExpenseRow
	var topDisplay sql.NullString
	err := r.db.QueryRow(ctx, query, userID, startDate, endDate).Scan(
		&row.TransactionID,
		&row.MerchantName,
		&row.Amount,
		&row.Category,
		&topDisplay,
		&row.Date,
	)
	if err != nil {
		if isNotFoundError(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get top expense: %w", err)
	}
	row.CategoryDisplay = topDisplay.String
	return &row, nil
}

// GetAverageMonthlySpend returns the total expense amount for computing average monthly spend.
// Single DB round-trip; average computed in the service layer (Issue #1215).
func (r *analyticsRepository) GetAverageMonthlySpend(ctx context.Context, userID string, filters domain.SpendingFilters) (*domain.AverageMonthlySpendRow, error) {
	if filters.HouseholdScope != nil {
		return r.getAverageMonthlySpendHousehold(ctx, userID, filters)
	}

	args := []interface{}{userID, filters.StartDate, filters.EndDate}
	filterClause, args, _ := buildSpendingFilter(filters, args)

	query := fmt.Sprintf(`
		SELECT COALESCE(SUM(t.amount), 0) as total
		FROM transactions t
		LEFT JOIN accounts a ON t.account_id = a.id
		WHERE t.user_id = $1
		  AND t.deleted_at IS NULL
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
	args := householdScopeArgs(requestingUserID, filters.HouseholdScope)
	args = append(args, filters.StartDate, filters.EndDate)
	filterClause, args, _ := buildSpendingFilter(filters, args)

	query := fmt.Sprintf(`
		WITH %s
		SELECT COALESCE(SUM(t.amount), 0) as total
		FROM transactions t
		JOIN accounts a ON t.account_id = a.id
		WHERE %s
		  AND t.date >= $4::date
		  AND t.deleted_at IS NULL
		  AND t.date <= $5
		  AND t.amount > 0
		  AND t.pending = false
		  %s
	`, householdGrantsCTE(), householdSharedTransactionPredicate("t", "a"), filterClause)

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
			  AND t.deleted_at IS NULL
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

	const query = `
		SELECT
			COALESCE(ABS(SUM(CASE WHEN t.amount < 0 THEN t.amount ELSE 0 END)), 0) AS income,
			COALESCE(SUM(CASE WHEN t.amount > 0 THEN t.amount ELSE 0 END), 0) AS expenses
		FROM transactions t
		WHERE t.user_id = $1
		  AND t.deleted_at IS NULL
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
	query := fmt.Sprintf(`
		WITH %s
		SELECT
			COALESCE(ABS(SUM(CASE WHEN t.amount < 0 THEN t.amount ELSE 0 END)), 0) AS income,
			COALESCE(SUM(CASE WHEN t.amount > 0 THEN t.amount ELSE 0 END), 0) AS expenses
		FROM transactions t
		JOIN accounts a ON t.account_id = a.id
		WHERE %s
		  AND t.date >= $4::date
		  AND t.deleted_at IS NULL
		  AND t.date <= $5
		  AND t.pending = false
	`, householdGrantsCTE(), householdSharedTransactionPredicate("t", "a"))

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
		  AND t.deleted_at IS NULL
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
		JOIN accounts a ON t.account_id = a.id
		WHERE %s
		  AND t.date >= $4::date
		  AND t.deleted_at IS NULL
		  AND t.amount > 0
		  AND t.pending = false
		  %s
		  %s
		GROUP BY DATE_TRUNC('month', t.date)
		ORDER BY month ASC
	`, householdGrantsCTE(), householdSharedTransactionPredicate("t", "a"), categoryFilter, filterClause)

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
	catExpr := effectiveCategoryExpr(false)
	args := []interface{}{userID, filters.StartDate, filters.EndDate}
	filterClause, args, argIndex := buildSpendingFilter(filters, args)

	args = append(args, limit)
	limitPlaceholder := fmt.Sprintf("$%d", argIndex)

	query := fmt.Sprintf(`
		SELECT t.id, COALESCE(t.merchant_name, ''), t.amount, (%s), t.effective_category_name, t.date, t.account_id::text
		FROM transactions t
		LEFT JOIN accounts a ON t.account_id = a.id
		WHERE t.user_id = $1
		  AND t.deleted_at IS NULL
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
		var display sql.NullString
		if err := rows.Scan(&row.TransactionID, &row.MerchantName, &row.Amount, &row.Category, &display, &row.Date, &row.AccountID); err != nil {
			return nil, fmt.Errorf("failed to scan largest transaction row: %w", err)
		}
		row.CategoryDisplay = display.String
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
	catExpr := effectiveCategoryExpr(false)
	args := []interface{}{userID, filters.StartDate, filters.EndDate, merchantID}
	filterClause, args, _ := buildSpendingFilter(filters, args)

	query := fmt.Sprintf(`
		SELECT (%s), t.effective_category_name, SUM(t.amount), COUNT(*)
		FROM transactions t
		LEFT JOIN accounts a ON t.account_id = a.id
		%s
		WHERE t.user_id = $1::uuid
		  AND t.deleted_at IS NULL
		  AND t.date >= $2
		  AND t.date <= $3
		  AND t.amount > 0
		  AND t.pending = false
		  AND %s = $4::uuid
		  %s
		GROUP BY (%s), t.effective_category_name
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
		var display sql.NullString
		if err := rows.Scan(&row.Category, &display, &row.Total, &row.Count); err != nil {
			return nil, fmt.Errorf("failed to scan category breakdown row: %w", err)
		}
		row.CategoryDisplay = display.String
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

	catExpr := effectiveCategoryExpr(false)
	displayExpr := effectiveCategoryDisplayExpr()
	args := []interface{}{userID, filters.StartDate, filters.EndDate}
	filterClause, args, _ := buildSpendingFilter(filters, args)

	query := fmt.Sprintf(`
		SELECT (%s) AS category, %s AS category_display, ABS(SUM(t.amount)) AS total, COUNT(*) AS count
		FROM transactions t
		LEFT JOIN accounts a ON t.account_id = a.id
		WHERE t.user_id = $1
		  AND t.deleted_at IS NULL
		  AND t.date >= $2
		  AND t.date <= $3
		  AND t.amount < 0
		  AND t.pending = false
		  %s
		GROUP BY (%s), category_display
		ORDER BY total DESC, category_display ASC
	`, catExpr, displayExpr, filterClause, catExpr)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get income source breakdown: %w", err)
	}
	defer rows.Close()

	results := make([]domain.IncomeSourceRow, 0)
	for rows.Next() {
		var row domain.IncomeSourceRow
		// effective_category_name is nullable and app-maintained asynchronously, so it is scanned
		// through a nullable type rather than straight into a string (#1626).
		var display sql.NullString
		if err := rows.Scan(&row.Category, &display, &row.Total, &row.Count); err != nil {
			return nil, fmt.Errorf("failed to scan income source row: %w", err)
		}
		row.CategoryDisplay = display.String
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
	catExpr := effectiveCategoryExpr(false)
	args := householdScopeArgs(requestingUserID, filters.HouseholdScope)
	args = append(args, filters.StartDate, filters.EndDate)
	filterClause, args, _ := buildSpendingFilter(filters, args)

	query := fmt.Sprintf(`
		WITH %s
		SELECT (%s) AS category, %s AS category_display, ABS(SUM(t.amount)) AS total, COUNT(*) AS count
		FROM transactions t
		JOIN accounts a ON t.account_id = a.id
		WHERE %s
		  AND t.date >= $4::date
		  AND t.deleted_at IS NULL
		  AND t.date <= $5
		  AND t.amount < 0
		  AND t.pending = false
		  %s
		GROUP BY (%s), category_display
		ORDER BY total DESC, category_display ASC
	`, householdGrantsCTE(), catExpr, effectiveCategoryDisplayExpr(), householdSharedTransactionPredicate("t", "a"), filterClause, catExpr)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get household income source breakdown: %w", err)
	}
	defer rows.Close()

	var results []domain.IncomeSourceRow
	for rows.Next() {
		var row domain.IncomeSourceRow
		// See the personal read: nullable, asynchronously maintained column (#1626).
		var display sql.NullString
		if err := rows.Scan(&row.Category, &display, &row.Total, &row.Count); err != nil {
			return nil, fmt.Errorf("failed to scan household income source row: %w", err)
		}
		row.CategoryDisplay = display.String
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
			  AND t.deleted_at IS NULL
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
			t.effective_category_name,
			t.date,
			b.mean_amount,
			(t.amount - b.mean_amount) / b.stddev_amount AS sigma_above
		FROM transactions t
		JOIN baseline b ON (%s) = b.category
		WHERE t.user_id = $1
		  AND t.deleted_at IS NULL
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
		var unusualDisplay sql.NullString
		if err := rows.Scan(
			&row.TransactionID,
			&row.MerchantName,
			&row.Amount,
			&row.Category,
			&unusualDisplay,
			&row.Date,
			&row.BaselineAvg,
			&row.SigmaAbove,
		); err != nil {
			return nil, fmt.Errorf("failed to scan unusual spend row: %w", err)
		}
		row.CategoryDisplay = unusualDisplay.String
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating unusual spend rows: %w", err)
	}

	return results, nil
}

// GetBudgetVsActual returns active budgets for a user filtered by their budget period,
// joined with aggregated allocated and spent amounts from budget_categories.
//
// A budget_categories row that is a SUB-ALLOCATION of another live row in the same budget
// is excluded from both sums: the parent's own allocated/spent amount already rolls the
// child up (Issue #1203), so summing every row double-counts it. The exclusion rule mirrors
// domain.BudgetCategory.IsSubAllocationWithin exactly, including its Issue #1630 correction:
// hierarchy alone is NOT sufficient. A child is excluded only when BOTH
//   - the PARENT's own match_logic is 'category_only' or 'or' (its matched set still contains
//     the plain category match — an 'and'/'tag_only' parent has no envelope a plain-category
//     child could fall inside), AND
//   - the CHILD's own match_logic is 'category_only' or 'and' (it cannot escape the parent's
//     category clause via tags — a 'tag_only'/'or' child can match transactions outside the
//     parent's category clause entirely, so summing it separately is not a NEW double-count).
//
// ⚠ That does NOT mean an escaping child's spend is absent from the parent's own sum. The
// parent's roll-up (buildCategoryClause, transaction_repository.go) is hierarchical and
// match_logic-BLIND — it has no sibling/match_logic context — so it counts every transaction
// filed under a nested custom category or PFC detailed regardless of that child's own
// match_logic. A legal shape (a budgeted 'category_only'/'or' parent with a nested child whose
// own match_logic is 'or' or 'tag_only') is therefore double-counted here: once via the child's
// own top-level row (this method does not exclude it, per the rule above) and again inside the
// parent's SpentAmount (which the child's SQL-side transactions still feed). This was a
// HIGH-severity finding on Issue #1630's own fix. It is addressed by refusing to PERSIST that
// shape at write time (domain.EscapedNestedChild, wired into budget_service.go's CreateBudget
// and AddCategory) rather than by changing this query, which has no sibling context to subtract
// from — any pre-existing row of this shape (there should be none) keeps the double-count.
//
// '' is treated as 'category_only' (the domain default and the column default), which SQL
// will not coerce on its own. A savings-kind category is never a parent or a child of this
// rule — Issue #1203's original switch has no "savings" case, and this reproduces that
// omission deliberately: a savings-kind row always counts in full.
//
// pfc_detailed = '' is treated identically to NULL/absent (Issue #1630): a plain
// `IS NOT NULL` / `IS NULL` test disagreed with the frontend's `!cat.pfc_detailed`, which
// already reads '' as falsy — the same row would be excluded from a total here but included
// there. `bc.pfc_detailed IS NOT NULL AND bc.pfc_detailed <> ''` / `(bc2.pfc_detailed IS NULL
// OR bc2.pfc_detailed = '')` below keep this query in agreement with
// domain.hasPFCDetailed and the frontend predicates.
// period selects by budget.period column (weekly/monthly/quarterly/yearly/custom).
// Returns an empty slice when no matching active budgets exist. Issue #1215 PR3; Issue #1630.
func (r *analyticsRepository) GetBudgetVsActual(ctx context.Context, userID, period string) ([]domain.BudgetVsActualRow, error) {
	const query = `
		SELECT
			b.id,
			b.name,
			b.period,
			COALESCE(SUM(CASE WHEN sub.is_sub_allocation THEN 0 ELSE bc.allocated_amount END), 0) AS allocated_amount,
			COALESCE(SUM(CASE WHEN sub.is_sub_allocation THEN 0 ELSE bc.spent_amount END), 0)     AS spent_amount
		FROM budgets b
		LEFT JOIN budget_categories bc ON bc.budget_id = b.id AND bc.deleted_at IS NULL
		-- tombstone-exempt(custom_categories cc): the cc join resolves the PARENT LINKAGE of a
		-- live budget line (bc.deleted_at IS NULL already filtered above), mirroring
		-- budget_repository.go's GetCategoriesByBudgetID. DeleteWithReassignment tombstones a
		-- category's budget lines in the same transaction as the category, so a live bc never
		-- names a tombstoned cc; adding AND cc.deleted_at IS NULL would buy nothing and would
		-- silently blank sub-allocation detection if that invariant ever slipped. #1569 T9.
		LEFT JOIN custom_categories cc ON cc.id = bc.custom_category_id
		LEFT JOIN LATERAL (
			SELECT EXISTS (
				SELECT 1
				FROM budget_categories bc2
				WHERE bc2.budget_id = bc.budget_id
				  AND bc2.deleted_at IS NULL
				  AND bc2.id <> bc.id
				  AND COALESCE(NULLIF(bc2.match_logic, ''), 'category_only') IN ('category_only', 'or')
				  AND COALESCE(NULLIF(bc.match_logic, ''), 'category_only') IN ('category_only', 'and')
				  AND (
					(bc.category_kind = 'pfc' AND bc.pfc_detailed IS NOT NULL AND bc.pfc_detailed <> '' AND bc.pfc_primary IS NOT NULL
					 AND bc2.category_kind = 'pfc' AND (bc2.pfc_detailed IS NULL OR bc2.pfc_detailed = '') AND bc2.pfc_primary = bc.pfc_primary)
					OR (bc.category_kind = 'custom' AND cc.parent_id IS NOT NULL
					 AND bc2.category_kind = 'custom' AND bc2.custom_category_id = cc.parent_id)
					OR (bc.category_kind = 'custom' AND cc.parent_pfc_primary IS NOT NULL
					 AND bc2.category_kind = 'pfc' AND (bc2.pfc_detailed IS NULL OR bc2.pfc_detailed = '') AND bc2.pfc_primary = cc.parent_pfc_primary)
				  )
			) AS is_sub_allocation
		) sub ON true
		WHERE b.user_id = $1
		  AND b.is_active = true
		  AND b.period = $2
		  AND b.deleted_at IS NULL
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
