package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// InvestmentAnalyticsRepository defines single-round-trip aggregate queries
// backing the four Code-Mode analytics helpers. All methods are scoped by
// user_id and chain-expansion-consistent with the rest of the investment stack.
type InvestmentAnalyticsRepository interface {
	// AllocationBreakdown returns portfolio allocation grouped per AllocationOptions.
	// Default (zero value options): investment holdings only, grouped by security_type —
	// backward-compatible with the #138 behavior.
	// When options.IncludesCash()         → unions depository account balances as "cash".
	// When options.IncludesLiabilities()  → unions credit/loan balances as "liabilities".
	// When options.GroupBy == "asset_class" → collapses buckets into investments/cash/liabilities.
	AllocationBreakdown(ctx context.Context, userID string, opts domain.AllocationOptions) (*domain.AllocationBreakdown, error)

	// TopMovers returns the top N gainers and losers by day_change (today's value
	// minus prior close). Holdings with NULL institution_price or securities with NULL
	// close_price are excluded from the result — day_change is undefined for them.
	// Note: securities.institution_price is a dead column (always NULL from Plaid);
	// day_change uses holdings.institution_price vs securities.close_price.
	TopMovers(ctx context.Context, userID string, limit int) (*domain.TopMovers, error)

	// UnrealizedGainLoss returns unrealized gain/loss aggregated over all of the user's
	// active holdings, plus a per-account breakdown. Holdings with NULL cost_basis are
	// excluded from the per-account gain/loss percentage; their value is still counted.
	// Realized gain/loss is NOT supported (Plaid provides no per-lot cost basis).
	UnrealizedGainLoss(ctx context.Context, userID string) (*domain.GainLossSummary, error)

	// IncomeSummary returns investment income (dividends, interest, reinvested
	// dividends/interest) within the given date range, grouped by normalized income type.
	// The income set is wider than the display "dividend" bucket — it includes reinvested
	// income because economically that is still income.
	IncomeSummary(ctx context.Context, userID string, start, end time.Time) (*domain.IncomeSummary, error)
}

type investmentAnalyticsRepository struct {
	db DB
}

// NewInvestmentAnalyticsRepository creates a new InvestmentAnalyticsRepository.
func NewInvestmentAnalyticsRepository(db DB) InvestmentAnalyticsRepository {
	return &investmentAnalyticsRepository{db: db}
}

// AllocationBreakdown returns portfolio value and holding count per AllocationOptions.
//
// Default (zero-value options): investment holdings only, grouped by security_type —
// identical to the pre-#1296 behavior (backward-compatible).
//
// When opts.IncludesCash()        → UNION in active depository accounts as "cash".
// When opts.IncludesLiabilities() → UNION in active credit/loan accounts as "liabilities"
//
//	(stored as absolute value; sign is implicit in the "liabilities" bucket label).
//
// When opts.GroupBy == "asset_class" → after bucketing, collapses security_type buckets
//
//	into three high-level classes: "investments" (all securities), "cash", "liabilities".
//
// Single DB round-trip: the UNION is inlined so the DB engine sees one query.
// Scoped strictly to user_id (IDOR-safe).
func (r *investmentAnalyticsRepository) AllocationBreakdown(ctx context.Context, userID string, opts domain.AllocationOptions) (*domain.AllocationBreakdown, error) {
	// group_by=account uses a per-account SQL shape — dispatch to a dedicated method.
	if opts.EffectiveGroupBy() == domain.AllocationGroupByAccount {
		return r.allocationBreakdownByAccount(ctx, userID, opts)
	}

	// Default: group by security_type (or asset_class, which post-processes security_type).
	// Base branch: investment holdings only (always present).
	query := `
		SELECT
			COALESCE(sec.type, 'unknown')          AS security_type,
			COUNT(*)                               AS holding_count,
			COALESCE(SUM(h.institution_value), 0)  AS total_value
		FROM holdings h
		JOIN accounts a ON a.id = h.account_id AND a.status = 'active'
		LEFT JOIN securities sec ON sec.id = h.security_id
		WHERE h.user_id = $1
		GROUP BY COALESCE(sec.type, 'unknown')`

	// Optional cash branch: aggregate depository account current_balance as "cash".
	if opts.IncludesCash() {
		query += `
		UNION ALL
		SELECT
			'cash'                                 AS security_type,
			COUNT(*)                               AS holding_count,
			COALESCE(SUM(a.current_balance), 0)    AS total_value
		FROM accounts a
		WHERE a.user_id = $1
		  AND a.status = 'active'
		  AND a.type = 'depository'`
	}

	// Optional liabilities branch: aggregate credit/loan account balances as "liabilities".
	// Stored as absolute values (negate the typically-negative balance) — the caller sees
	// a positive magnitude in the "liabilities" bucket.
	if opts.IncludesLiabilities() {
		query += `
		UNION ALL
		SELECT
			'liabilities'                          AS security_type,
			COUNT(*)                               AS holding_count,
			COALESCE(SUM(ABS(a.current_balance)), 0) AS total_value
		FROM accounts a
		WHERE a.user_id = $1
		  AND a.status = 'active'
		  AND a.type IN ('credit', 'loan')`
	}

	// Wrap in a CTE so we can ORDER BY total_value across the full union.
	query = `WITH combined AS (` + query + `)
		SELECT security_type, holding_count, total_value
		FROM combined
		ORDER BY total_value DESC`

	rows, err := r.db.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("AllocationBreakdown: %w", err)
	}
	defer rows.Close()

	var buckets []domain.AllocationItem
	var grandTotal float64
	for rows.Next() {
		var item domain.AllocationItem
		if err := rows.Scan(&item.SecurityType, &item.HoldingCount, &item.TotalValue); err != nil {
			return nil, fmt.Errorf("AllocationBreakdown scan: %w", err)
		}
		grandTotal += item.TotalValue
		buckets = append(buckets, item)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("AllocationBreakdown rows: %w", rows.Err())
	}

	// Compute percentages after the full total is known.
	if grandTotal > 0 {
		for i := range buckets {
			buckets[i].Percentage = buckets[i].TotalValue / grandTotal * 100
		}
	}

	// group_by=asset_class: collapse security_type buckets into 3 high-level classes.
	if opts.EffectiveGroupBy() == domain.AllocationGroupByAssetClass {
		buckets = collapseToAssetClasses(buckets)
	} else {
		// Populate AssetClass field for the default security_type grouping.
		for i := range buckets {
			buckets[i].AssetClass = securityTypeToAssetClass(buckets[i].SecurityType)
		}
	}

	// basis=net: remove liability buckets, subtract from total, recompute percentages.
	if opts.EffectiveBasis() == domain.AllocationBasisNet {
		buckets, grandTotal = applyNetBasis(buckets)
	}

	if buckets == nil {
		buckets = []domain.AllocationItem{}
	}
	return &domain.AllocationBreakdown{Buckets: buckets, TotalValue: grandTotal}, nil
}

// allocationBreakdownByAccount handles group_by=account: one bucket per active account,
// with SecurityType = account name and AssetClass = account type.
// Cash and liabilities branches add depository/credit accounts when requested via opts.Include.
// Scoped to user_id on every branch (IDOR-safe). Single round-trip via UNION CTE.
func (r *investmentAnalyticsRepository) allocationBreakdownByAccount(ctx context.Context, userID string, opts domain.AllocationOptions) (*domain.AllocationBreakdown, error) {
	// Investment holdings: one bucket per brokerage/investment account.
	query := `
		SELECT
			a.name                                AS security_type,
			'investment'                          AS asset_class,
			COUNT(*)                              AS holding_count,
			COALESCE(SUM(h.institution_value), 0) AS total_value
		FROM holdings h
		JOIN accounts a ON a.id = h.account_id AND a.status = 'active'
		WHERE h.user_id = $1
		GROUP BY a.id, a.name`

	// Optional cash: one row per depository account, labeled by account name.
	if opts.IncludesCash() {
		query += `
		UNION ALL
		SELECT
			a.name                                AS security_type,
			'depository'                          AS asset_class,
			1                                     AS holding_count,
			COALESCE(a.current_balance, 0)        AS total_value
		FROM accounts a
		WHERE a.user_id = $1
		  AND a.status = 'active'
		  AND a.type = 'depository'`
	}

	// Optional liabilities: one row per credit/loan account; balance stored as absolute value.
	if opts.IncludesLiabilities() {
		query += `
		UNION ALL
		SELECT
			a.name                                AS security_type,
			a.type                                AS asset_class,
			1                                     AS holding_count,
			COALESCE(ABS(a.current_balance), 0)   AS total_value
		FROM accounts a
		WHERE a.user_id = $1
		  AND a.status = 'active'
		  AND a.type IN ('credit', 'loan')`
	}

	query = `WITH combined AS (` + query + `)
		SELECT security_type, asset_class, holding_count, total_value
		FROM combined
		ORDER BY total_value DESC`

	rows, err := r.db.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("allocationBreakdownByAccount: %w", err)
	}
	defer rows.Close()

	var buckets []domain.AllocationItem
	var grandTotal float64
	for rows.Next() {
		var item domain.AllocationItem
		if err := rows.Scan(&item.SecurityType, &item.AssetClass, &item.HoldingCount, &item.TotalValue); err != nil {
			return nil, fmt.Errorf("allocationBreakdownByAccount scan: %w", err)
		}
		grandTotal += item.TotalValue
		buckets = append(buckets, item)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("allocationBreakdownByAccount rows: %w", rows.Err())
	}

	// Compute percentages.
	if grandTotal > 0 {
		for i := range buckets {
			buckets[i].Percentage = buckets[i].TotalValue / grandTotal * 100
		}
	}

	// basis=net: remove liability account buckets, recompute percentages on net total.
	if opts.EffectiveBasis() == domain.AllocationBasisNet {
		buckets, grandTotal = applyNetBasis(buckets)
	}

	if buckets == nil {
		buckets = []domain.AllocationItem{}
	}
	return &domain.AllocationBreakdown{Buckets: buckets, TotalValue: grandTotal}, nil
}

// applyNetBasis removes liability buckets from the slice and returns (asset-only buckets,
// net total). Option-A semantics (user-confirmed):
//
//   - TotalValue (headline) = asset_total − liabilities  (true net worth; e.g. 6800)
//   - Slice percentages use asset_total as the denominator so the donut ring sums to 100%
//     (e.g. stock=62.5%, cash=37.5%) rather than the net denominator (which would sum >100%)
//
// asset_total is computed by summing the non-liability buckets directly (not grandTotal − liab),
// so this function is resilient to future sign-convention changes in the SQL union query.
// Example: stock=5000, cash=3000, liab=1200 → assetTotal=8000, netTotal=6800, stock%=62.5.
//
// Liability detection uses AssetClass exclusively (never SecurityType) so that account names
// cannot collide with the "liabilities" sentinel used in security-type grouping mode:
//   - security_type/asset_class modes: AssetClass == "liabilities" (set by securityTypeToAssetClass)
//   - account mode:                    AssetClass == "credit" or "loan" (set by SQL a.type column)
func applyNetBasis(buckets []domain.AllocationItem) ([]domain.AllocationItem, float64) {
	var liabilitiesTotal float64
	var assetBuckets []domain.AllocationItem
	for _, b := range buckets {
		if b.AssetClass == "liabilities" || b.AssetClass == "credit" || b.AssetClass == "loan" {
			liabilitiesTotal += b.TotalValue
		} else {
			assetBuckets = append(assetBuckets, b)
		}
	}
	// Compute assetTotal by summing non-liability buckets directly — resilient to sign changes.
	var assetTotal float64
	for _, b := range assetBuckets {
		assetTotal += b.TotalValue
	}
	// netTotal is the true net-worth headline: what you own minus what you owe.
	netTotal := assetTotal - liabilitiesTotal
	// Percentages on assetTotal so the donut ring always completes 100%.
	if assetTotal > 0 {
		for i := range assetBuckets {
			assetBuckets[i].Percentage = assetBuckets[i].TotalValue / assetTotal * 100
		}
	}
	return assetBuckets, netTotal
}

// securityTypeToAssetClass maps a raw security_type string to a high-level asset class label.
// "liabilities" and "cash" (from UNION branches) are already the right class.
// All investment security types (stock, etf, bond, crypto, mutual fund, etc.) → "investments".
func securityTypeToAssetClass(secType string) string {
	switch secType {
	case "cash", "liabilities":
		return secType
	default:
		return "investments"
	}
}

// collapseToAssetClasses aggregates buckets by their asset class, summing values and counts.
// The three canonical classes are: "investments", "cash", "liabilities".
// Re-computes percentages on the collapsed total.
func collapseToAssetClasses(buckets []domain.AllocationItem) []domain.AllocationItem {
	totals := make(map[string]*domain.AllocationItem)
	var grandTotal float64
	for _, b := range buckets {
		cls := securityTypeToAssetClass(b.SecurityType)
		if totals[cls] == nil {
			totals[cls] = &domain.AllocationItem{AssetClass: cls, SecurityType: cls}
		}
		totals[cls].TotalValue += b.TotalValue
		totals[cls].HoldingCount += b.HoldingCount
		grandTotal += b.TotalValue
	}
	// Order: investments → cash → liabilities for a consistent presentation.
	order := []string{"investments", "cash", "liabilities"}
	var result []domain.AllocationItem
	for _, cls := range order {
		if item, ok := totals[cls]; ok {
			if grandTotal > 0 {
				item.Percentage = item.TotalValue / grandTotal * 100
			}
			result = append(result, *item)
		}
	}
	return result
}

// TopMovers returns the top N gainers and losers ranked by day_change.
// day_change = quantity × (holdings.institution_price − securities.close_price).
// Holdings with NULL institution_price or securities with NULL close_price are excluded.
// Uses a single CTE query that computes both gainers and losers in one round-trip.
//
// The UNION ALL is wrapped in a `combined` CTE to allow ORDER BY with a CASE expression.
// PostgreSQL forbids expressions in set-operation ORDER BY, so the UNION is lifted into
// a CTE and the outer SELECT applies the final ORDER BY.
func (r *investmentAnalyticsRepository) TopMovers(ctx context.Context, userID string, limit int) (*domain.TopMovers, error) {
	query := `
		WITH movers AS (
			SELECT
				h.id                                                                          AS holding_id,
				h.account_id,
				sec.ticker_symbol,
				sec.name                                                                      AS security_name,
				h.quantity * (h.institution_price - sec.close_price)                         AS day_change,
				CASE
					WHEN h.institution_value IS NOT NULL AND h.institution_value != 0
					THEN h.quantity * (h.institution_price - sec.close_price) / h.institution_value * 100
					ELSE NULL
				END                                                                           AS day_change_pct,
				CASE
					WHEN h.institution_value IS NOT NULL AND h.cost_basis IS NOT NULL
					THEN h.institution_value - h.cost_basis
					ELSE NULL
				END                                                                           AS total_gain_loss
			FROM holdings h
			JOIN accounts a ON a.id = h.account_id AND a.status = 'active'
			JOIN securities sec ON sec.id = h.security_id
			WHERE h.user_id = $1
			  AND h.institution_price IS NOT NULL
			  AND sec.close_price IS NOT NULL
		),
		gainers AS (
			SELECT holding_id, account_id, ticker_symbol, security_name,
			       day_change, day_change_pct, total_gain_loss, 'gainer' AS mover_type
			FROM movers
			WHERE day_change > 0
			ORDER BY day_change DESC
			LIMIT $2
		),
		losers AS (
			SELECT holding_id, account_id, ticker_symbol, security_name,
			       day_change, day_change_pct, total_gain_loss, 'loser' AS mover_type
			FROM movers
			WHERE day_change < 0
			ORDER BY day_change ASC
			LIMIT $2
		),
		combined AS (
			SELECT holding_id::text, account_id::text, ticker_symbol, security_name,
			       day_change, day_change_pct, total_gain_loss, mover_type
			FROM gainers
			UNION ALL
			SELECT holding_id::text, account_id::text, ticker_symbol, security_name,
			       day_change, day_change_pct, total_gain_loss, mover_type
			FROM losers
		)
		SELECT holding_id, account_id, ticker_symbol, security_name,
		       day_change, day_change_pct, total_gain_loss, mover_type
		FROM combined
		ORDER BY mover_type ASC, CASE WHEN mover_type = 'gainer' THEN day_change ELSE -day_change END DESC
	`

	rows, err := r.db.Query(ctx, query, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("TopMovers: %w", err)
	}
	defer rows.Close()

	result := &domain.TopMovers{
		Gainers: []domain.TopMoverItem{},
		Losers:  []domain.TopMoverItem{},
	}
	for rows.Next() {
		var item domain.TopMoverItem
		var moverType string
		if err := rows.Scan(
			&item.HoldingID, &item.AccountID,
			&item.TickerSymbol, &item.SecurityName,
			&item.DayChange, &item.DayChangePct, &item.TotalGainLoss,
			&moverType,
		); err != nil {
			return nil, fmt.Errorf("TopMovers scan: %w", err)
		}
		if moverType == "gainer" {
			result.Gainers = append(result.Gainers, item)
		} else {
			result.Losers = append(result.Losers, item)
		}
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("TopMovers rows: %w", rows.Err())
	}
	return result, nil
}

// UnrealizedGainLoss returns the user's total unrealized gain/loss and a
// per-account breakdown.
//
// Two cost-basis figures are computed:
//   - cost_basis_known_pair (FILTER WHERE both non-null): used only for gain/loss gain figure
//     and per-account/total percentage denominator — keeps the gain phantom-free.
//   - cost_basis_all (COALESCE SUM over all non-nil): exposed as the API TotalCostBasis
//     and per-account CostBasis, matching GetPortfolioSummary + snapshot Upsert semantics
//     (all-non-null anchor). A holding with cost_basis set but institution_value NULL
//     still has a known basis that belongs in the total. — R9-1 fix.
//
// Single round-trip aggregate.
func (r *investmentAnalyticsRepository) UnrealizedGainLoss(ctx context.Context, userID string) (*domain.GainLossSummary, error) {
	query := `
		SELECT
			h.account_id::text,
			COALESCE(SUM(h.institution_value), 0) AS value,
			-- Gain/loss only over holdings with non-NULL cost_basis AND institution_value
			-- (see portfolio_snapshot_repository.go Upsert comment for rationale).
			COALESCE(SUM(h.institution_value) FILTER (WHERE h.cost_basis IS NOT NULL AND h.institution_value IS NOT NULL), 0)
			    - COALESCE(SUM(h.cost_basis) FILTER (WHERE h.cost_basis IS NOT NULL AND h.institution_value IS NOT NULL), 0) AS unrealized_gl,
			CASE
				WHEN COALESCE(SUM(h.cost_basis) FILTER (WHERE h.cost_basis IS NOT NULL AND h.institution_value IS NOT NULL), 0) > 0
				THEN (
				    COALESCE(SUM(h.institution_value) FILTER (WHERE h.cost_basis IS NOT NULL AND h.institution_value IS NOT NULL), 0)
				    - SUM(h.cost_basis) FILTER (WHERE h.cost_basis IS NOT NULL AND h.institution_value IS NOT NULL)
				) / SUM(h.cost_basis) FILTER (WHERE h.cost_basis IS NOT NULL AND h.institution_value IS NOT NULL) * 100
				ELSE NULL
			END AS unrealized_gl_pct,
			-- known_pair: used only as pct denominator (not exposed directly).
			COALESCE(SUM(h.cost_basis) FILTER (WHERE h.cost_basis IS NOT NULL AND h.institution_value IS NOT NULL), 0) AS cost_basis_known_pair,
			-- cost_basis_all: all non-nil cost_basis, exposed as the API CostBasis field.
			-- Matches GetPortfolioSummary totalCostBasis and snapshot Upsert total_cost_basis.
			COALESCE(SUM(h.cost_basis), 0) AS cost_basis_all
		FROM holdings h
		JOIN accounts a ON a.id = h.account_id AND a.status = 'active'
		WHERE h.user_id = $1
		GROUP BY h.account_id
	`

	rows, err := r.db.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("UnrealizedGainLoss: %w", err)
	}
	defer rows.Close()

	var byAccount []domain.AccountGainLoss
	var totalValue, totalCostBasis, totalKnownPairCost, totalGL float64
	for rows.Next() {
		var ag domain.AccountGainLoss
		var costBasisKnownPair float64
		if err := rows.Scan(
			&ag.AccountID,
			&ag.Value,
			&ag.UnrealizedGainLoss,
			&ag.UnrealizedGainLossPct,
			&costBasisKnownPair,
			&ag.CostBasis, // cost_basis_all: all-non-nil, exposed in API response
		); err != nil {
			return nil, fmt.Errorf("UnrealizedGainLoss scan: %w", err)
		}
		totalValue += ag.Value
		totalCostBasis += ag.CostBasis         // all-non-nil → exposed TotalCostBasis
		totalKnownPairCost += costBasisKnownPair // both-non-nil → pct denominator only
		// Accumulate per-account GL directly (already FILTER-scoped in SQL) rather than
		// recomputing totalValue - totalCostBasis, which would mix all-holdings value
		// with known-basis-only cost and produce phantom gains for NULL-basis holdings.
		totalGL += ag.UnrealizedGainLoss
		byAccount = append(byAccount, ag)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("UnrealizedGainLoss rows: %w", rows.Err())
	}

	// Pct denominator = known-pair cost (not totalCostBasis) so a holding with
	// cost_basis set but NULL institution_value doesn't inflate the denominator
	// and collapse the apparent return percentage.
	var totalGLPct *float64
	if totalKnownPairCost > 0 {
		v := totalGL / totalKnownPairCost * 100
		totalGLPct = &v
	}
	if byAccount == nil {
		byAccount = []domain.AccountGainLoss{}
	}
	return &domain.GainLossSummary{
		TotalUnrealizedGainLoss:    totalGL,
		TotalUnrealizedGainLossPct: totalGLPct,
		TotalCostBasis:             totalCostBasis, // all-non-nil basis
		TotalValue:                 totalValue,
		RealizedGainLossSupported:  false,
		ByAccount:                  byAccount,
	}, nil
}

// IncomeSummary aggregates investment income within [start, end] by normalized income type.
// The income set (domain.IncomeSubtypes) is wider than the display "dividend" bucket —
// it includes reinvested dividends/interest. Plaid stores income (cash credits) as negative
// amounts; the query negates them so returned TotalAmount values are positive magnitudes.
func (r *investmentAnalyticsRepository) IncomeSummary(ctx context.Context, userID string, start, end time.Time) (*domain.IncomeSummary, error) {
	// Build IN clause for the income subtype set.
	subtypes := domain.IncomeSubtypes
	placeholders := make([]string, len(subtypes))
	args := []any{userID, start, end}
	for i, s := range subtypes {
		placeholders[i] = fmt.Sprintf("$%d", i+4)
		args = append(args, s)
	}
	inClause := strings.Join(placeholders, ", ")

	// Plaid investment-transaction amount sign convention:
	//   positive  = cash DEBITED  (buys, fees)
	//   negative  = cash CREDITED (sells, dividends, interest)
	// Income subtypes (dividends, interest, reinvestments) are cash credits, so Plaid
	// stores them as NEGATIVE amounts. Negate before SUM so callers receive a positive
	// income magnitude. COALESCE guards the edge case where all amounts in a group are NULL.
	query := fmt.Sprintf(`
		SELECT
			CASE
				WHEN it.subtype IN ('dividend', 'qualified dividend', 'non-qualified dividend')
				     THEN 'dividend'
				WHEN it.subtype IN ('interest', 'interest receivable')
				     THEN 'interest'
				WHEN it.subtype = 'dividend reinvestment'
				     THEN 'dividend_reinvestment'
				WHEN it.subtype = 'interest reinvestment'
				     THEN 'interest_reinvestment'
				ELSE 'other_income'
			END                          AS income_type,
			COUNT(*)                     AS count,
			COALESCE(-SUM(it.amount), 0) AS total_amount
		FROM investment_transactions it
		JOIN accounts a ON a.id = it.account_id AND a.status = 'active'
		WHERE it.user_id = $1
		  AND it.date >= $2::date
		  AND it.date <= $3::date
		  AND it.subtype IN (%s)
		GROUP BY income_type
		ORDER BY total_amount DESC
	`, inClause)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("IncomeSummary: %w", err)
	}
	defer rows.Close()

	var byType []domain.IncomeByType
	var totalIncome float64
	for rows.Next() {
		var bt domain.IncomeByType
		if err := rows.Scan(&bt.IncomeType, &bt.Count, &bt.TotalAmount); err != nil {
			return nil, fmt.Errorf("IncomeSummary scan: %w", err)
		}
		totalIncome += bt.TotalAmount
		byType = append(byType, bt)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("IncomeSummary rows: %w", rows.Err())
	}

	if byType == nil {
		byType = []domain.IncomeByType{}
	}
	return &domain.IncomeSummary{
		TotalIncome: totalIncome,
		ByType:      byType,
		StartDate:   start,
		EndDate:     end,
	}, nil
}
