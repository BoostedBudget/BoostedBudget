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

	// PositionReconciliation is a DIAGNOSTIC, never a correction (#1631 M8b): it compares each
	// active investment holding's quantity against the net share delta domain.TradeTypeToSQLPredicate's
	// buy/sell/reinvest partition (plus type='transfer' rows) implies from investment_transactions,
	// and reports the mismatches worth an operator's attention. See
	// domain.PositionReconciliationReport's doc comment for the 2-year-lookback suppression rule and
	// for why Drifts is NOT yet fully actionable.
	PositionReconciliation(ctx context.Context, userID string) (*domain.PositionReconciliationReport, error)

	// PositionReconciliationSummary is the GLOBAL admin-health gauge sibling of
	// PositionReconciliation (#1631 M8b I1): the same classification, aggregated across every
	// user's active holdings as counts rather than one user's Drifts list.
	PositionReconciliationSummary(ctx context.Context) (*domain.PositionReconciliationSummary, error)
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
// distinctCurrencyCodesSQL renders an aggregate collecting the DISTINCT, normalised, non-blank
// currency codes contributing to one grouped row (#1631 M8a).
//
// An array rather than a single value, and it is resolved in Go rather than in SQL, because the
// SQL side cannot express the answer: a scalar would have to collapse "mixed" and "nobody stated
// one" into the same NULL, and those two cases require OPPOSITE handling — one labels the total,
// the other declares it a currency-blind sum. Returning the set keeps the distinction alive as
// far as domain.ResolveCurrencyCodes, which is the one place the rule is written down.
//
// NULLs and blanks are filtered out before aggregating: unknown is not a currency (see
// domain/investment_currency.go), so it must not be able to make a set look mixed.
func distinctCurrencyCodesSQL(column string) string {
	return "COALESCE(array_agg(DISTINCT UPPER(TRIM(" + column + "))) FILTER (WHERE " +
		column + " IS NOT NULL AND TRIM(" + column + ") <> ''), '{}')"
}

func (r *investmentAnalyticsRepository) AllocationBreakdown(ctx context.Context, userID string, opts domain.AllocationOptions) (*domain.AllocationBreakdown, error) {
	// group_by=account uses a per-account SQL shape — dispatch to a dedicated method.
	if opts.EffectiveGroupBy() == domain.AllocationGroupByAccount {
		return r.allocationBreakdownByAccount(ctx, userID, opts)
	}

	// Default: group by security_type (or asset_class, which post-processes security_type).
	// Base branch: investment holdings only (always present).
	//
	// #1616: a.type = 'investment' is required here, not optional decoration. Plaid types a
	// cash-management account 'depository' even though it also reports holdings rows for it;
	// without this predicate that account's h.institution_value was summed here AND its
	// a.current_balance was summed again below in the cash branch — the same dollars counted
	// twice into grandTotal. This is an arithmetic error, not a preference, so it is fixed for
	// every account type uniformly via accounts.type rather than via exclude_investment_sync
	// (see the standing decision on IncomeSummary below). Do not "fix" this by touching
	// SyncHoldings/GetByLinkedItemID — that path is already correct (active-only, by design);
	// the defect was always the missing predicate in this READ.
	query := `
		SELECT
			COALESCE(sec.type, 'unknown')          AS security_type,
			COUNT(*)                               AS holding_count,
			COALESCE(SUM(h.institution_value), 0)  AS total_value,
			` + distinctCurrencyCodesSQL("h.currency_code") + `  AS currency_codes
		FROM holdings h
		-- CURRENT-STATE universe (#1569 T12): see currentStateAccountLeg, account_liveness_sql.go
		JOIN accounts a ON a.id = h.account_id AND ` + currentStateAccountLeg("a") + `
		LEFT JOIN securities sec ON sec.id = h.security_id
		WHERE h.user_id = $1
		  AND a.type = 'investment'
		GROUP BY COALESCE(sec.type, 'unknown')`

	// Optional cash branch: aggregate depository account current_balance as "cash".
	// GREATEST(balance, 0) per-row floors overdrawn accounts to zero — a negative
	// checking balance must not reduce the cash asset bucket.
	if opts.IncludesCash() {
		query += `
		UNION ALL
		SELECT
			'cash'                                         AS security_type,
			COUNT(*)                                       AS holding_count,
			COALESCE(SUM(GREATEST(a.current_balance, 0)), 0) AS total_value,
			-- The cash branch sums ACCOUNT balances, so its currency is accounts.currency_code.
			-- ⚠ That column carries DEFAULT 'USD' (migration 000007), so an account whose currency
			-- Plaid never stated reports USD rather than NULL. Included anyway, deliberately:
			-- omitting it would let a EUR bank balance be added into a USD-labelled total and the
			-- result confidently named "USD", which is worse than an occasional over-eager mixed
			-- flag on an account whose currency we guessed. The flag warns; it refuses nothing.
			` + distinctCurrencyCodesSQL("a.currency_code") + `   AS currency_codes
		FROM accounts a
		WHERE a.user_id = $1
		-- CURRENT-STATE universe (#1569 T12): see currentStateAccountLeg, account_liveness_sql.go
		  AND ` + currentStateAccountLeg("a") + `
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
			COALESCE(SUM(ABS(a.current_balance)), 0) AS total_value,
			` + distinctCurrencyCodesSQL("a.currency_code") + ` AS currency_codes
		FROM accounts a
		WHERE a.user_id = $1
		-- CURRENT-STATE universe (#1569 T12): see currentStateAccountLeg, account_liveness_sql.go
		  AND ` + currentStateAccountLeg("a") + `
		  AND a.type IN ('credit', 'loan')`
	}

	// Wrap in a CTE so we can ORDER BY total_value across the full union.
	query = `WITH combined AS (` + query + `)
		SELECT security_type, holding_count, total_value, currency_codes
		FROM combined
		ORDER BY total_value DESC`

	rows, err := r.db.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("AllocationBreakdown: %w", err)
	}
	defer rows.Close()

	var buckets []domain.AllocationItem
	var grandTotal float64
	var currencyCodes []string
	for rows.Next() {
		var item domain.AllocationItem
		var rowCurrencies []string
		if err := rows.Scan(&item.SecurityType, &item.HoldingCount, &item.TotalValue, &rowCurrencies); err != nil {
			return nil, fmt.Errorf("AllocationBreakdown scan: %w", err)
		}
		currencyCodes = append(currencyCodes, rowCurrencies...)
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
	// #1631 M8a: the buckets and the grand total are summed across every branch above, so the
	// verdict is taken over the UNION of what they contributed — a USD holdings bucket beside a
	// EUR cash bucket is a mixed total even though neither bucket is mixed on its own.
	currencyCode, isMixed := domain.ResolveCurrencyCodes(currencyCodes)
	return &domain.AllocationBreakdown{
		Buckets:         buckets,
		TotalValue:      grandTotal,
		CurrencyCode:    currencyCode,
		IsMixedCurrency: isMixed,
	}, nil
}

// allocationBreakdownByAccount handles group_by=account: one bucket per active account,
// with SecurityType = account name and AssetClass = account type.
// Cash and liabilities branches add depository/credit accounts when requested via opts.Include.
// Scoped to user_id on every branch (IDOR-safe). Single round-trip via UNION CTE.
func (r *investmentAnalyticsRepository) allocationBreakdownByAccount(ctx context.Context, userID string, opts domain.AllocationOptions) (*domain.AllocationBreakdown, error) {
	// Investment holdings: one bucket per brokerage/investment account.
	//
	// #1616: a.type = 'investment' is required — same defect and same fix as the security_type
	// shape above. Without it, a depository account that also carries holdings rows produced TWO
	// buckets bearing the SAME account name (one here with asset_class hardcoded 'investment',
	// one from the cash branch below with asset_class='depository'), each holding half of the
	// same double-counted dollars.
	query := `
		SELECT
			a.name                                AS security_type,
			'investment'                          AS asset_class,
			COUNT(*)                              AS holding_count,
			COALESCE(SUM(h.institution_value), 0) AS total_value,
			` + distinctCurrencyCodesSQL("h.currency_code") + ` AS currency_codes
		FROM holdings h
		-- CURRENT-STATE universe (#1569 T12): see currentStateAccountLeg, account_liveness_sql.go
		JOIN accounts a ON a.id = h.account_id AND ` + currentStateAccountLeg("a") + `
		WHERE h.user_id = $1
		  AND a.type = 'investment'
		GROUP BY a.id, a.name`

	// Optional cash: one row per depository account, labeled by account name.
	// GREATEST(balance, 0) floors overdrawn accounts to zero — same invariant as the
	// security-type/asset-class cash branch above.
	if opts.IncludesCash() {
		query += `
		UNION ALL
		SELECT
			a.name                                AS security_type,
			'depository'                          AS asset_class,
			1                                     AS holding_count,
			GREATEST(COALESCE(a.current_balance, 0), 0) AS total_value,
			-- One row per account here, so this is that account's own currency rather than an
			-- aggregate. Same DEFAULT 'USD' caveat as the grouped shape above.
			ARRAY[UPPER(TRIM(COALESCE(a.currency_code, '')))] AS currency_codes
		FROM accounts a
		WHERE a.user_id = $1
		-- CURRENT-STATE universe (#1569 T12): see currentStateAccountLeg, account_liveness_sql.go
		  AND ` + currentStateAccountLeg("a") + `
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
			COALESCE(ABS(a.current_balance), 0)   AS total_value,
			ARRAY[UPPER(TRIM(COALESCE(a.currency_code, '')))] AS currency_codes
		FROM accounts a
		WHERE a.user_id = $1
		-- CURRENT-STATE universe (#1569 T12): see currentStateAccountLeg, account_liveness_sql.go
		  AND ` + currentStateAccountLeg("a") + `
		  AND a.type IN ('credit', 'loan')`
	}

	query = `WITH combined AS (` + query + `)
		SELECT security_type, asset_class, holding_count, total_value, currency_codes
		FROM combined
		ORDER BY total_value DESC`

	rows, err := r.db.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("allocationBreakdownByAccount: %w", err)
	}
	defer rows.Close()

	var buckets []domain.AllocationItem
	var grandTotal float64
	var currencyCodes []string
	for rows.Next() {
		var item domain.AllocationItem
		var rowCurrencies []string
		if err := rows.Scan(&item.SecurityType, &item.AssetClass, &item.HoldingCount, &item.TotalValue,
			&rowCurrencies); err != nil {
			return nil, fmt.Errorf("allocationBreakdownByAccount scan: %w", err)
		}
		currencyCodes = append(currencyCodes, rowCurrencies...)
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
	currencyCode, isMixed := domain.ResolveCurrencyCodes(currencyCodes)
	return &domain.AllocationBreakdown{
		Buckets:         buckets,
		TotalValue:      grandTotal,
		CurrencyCode:    currencyCode,
		IsMixedCurrency: isMixed,
	}, nil
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
// Everything else → "investments". ⚠ #1631: the values reaching this function are PLAID's
// security-type enum — cash, cryptocurrency, derivative, equity, etf, fixed income, loan,
// mutual fund, other — plus the synthetic "unknown" (COALESCEd for a NULL securities.type) and
// "liabilities". This line previously advertised "stock", "bond" and "crypto", none of which
// Plaid ever sends; the default arm is what makes that misdescription harmless rather than a bug,
// but a reader adding a case for "stock" would have added dead code.
//
// ⚠ Note "cash" is BOTH a Plaid security type and this file's own synthetic label for the
// depository UNION branch, so the first case arm catches two different things that happen to
// deserve the same class.
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
// #1616 T2c: a.type = 'investment' is required here, same as AllocationBreakdown /
// allocationBreakdownByAccount above. A Plaid cash-management account is typed 'depository' even
// though it also reports holdings rows for it; without this predicate such an account's holdings
// still ranked in the gainers/losers list — a checking account appearing in a portfolio view. This
// gap was found by an adversarial review of the AllocationBreakdown fix and explicitly authorised
// for extension by the user; see the standing-decision comment on IncomeSummary below, which this
// updates.
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
				h.currency_code                                                               AS currency_code,
				-- #1631: measured against the PRIOR close value (quantity x close_price), not the
				-- current institution_value. The two differ by the day change itself, so the old
				-- form understated every gain and overstated every loss by exactly that amount —
				-- a true +5% day read 4.76%, and a doubling read 50%. Mirrors
				-- domain.Holding.DayChangePct, which is pinned by
				-- TestHolding_DayChangePct_IsMeasuredAgainstThePriorClose.
				CASE
					WHEN h.quantity * sec.close_price != 0
					THEN h.quantity * (h.institution_price - sec.close_price)
					     / (h.quantity * sec.close_price) * 100
					ELSE NULL
				END                                                                           AS day_change_pct,
				CASE
					WHEN h.institution_value IS NOT NULL AND h.cost_basis IS NOT NULL
					THEN h.institution_value - h.cost_basis
					ELSE NULL
				END                                                                           AS total_gain_loss
			FROM holdings h
			-- CURRENT-STATE universe (#1569 T12): see currentStateAccountLeg, account_liveness_sql.go
			JOIN accounts a ON a.id = h.account_id AND ` + currentStateAccountLeg("a") + `
			JOIN securities sec ON sec.id = h.security_id
			WHERE h.user_id = $1
			  AND a.type = 'investment' -- #1616 T2c: holdings-based surfaces are governed by accounts.type
			  AND h.institution_price IS NOT NULL
			  AND sec.close_price IS NOT NULL
			  -- #1631 M8a: this day change is computed in SQL rather than through
			  -- domain.Holding.DayChange, so that method's currency refusal has to be repeated
			  -- here or the two surfaces disagree about the same holding — the holdings table
			  -- showing no day change while this widget ranks the position by one.
			  --
			  -- The holding is EXCLUDED rather than returned with a NULL day change, matching how
			  -- this query already treats a missing close price: a top-MOVERS list ranks by the
			  -- movement, so a row whose movement has no defined value has nothing to be ranked
			  -- on. (See TopMoverItem's doc comment, which spells out that exclusion-not-nil is
			  -- this surface's rule.)
			  --
			  -- Both codes must be KNOWN and DIFFERENT to exclude. An unstated currency on either
			  -- side is not a conflict — that is every row written before migration 000197, and
			  -- treating it as one would empty this widget for the entire existing user base.
			  AND NOT (
			      h.currency_code IS NOT NULL AND TRIM(h.currency_code) <> ''
			      AND sec.currency_code IS NOT NULL AND TRIM(sec.currency_code) <> ''
			      AND UPPER(TRIM(h.currency_code)) <> UPPER(TRIM(sec.currency_code))
			  )
		),
		-- #1631 CX1(c). Ranking is by NOMINAL day change, and a nominal comparison across
		-- currencies is not a comparison at all: a JPY position outranks a USD one on the
		-- exchange rate alone. There is no FX rate source in this system to convert with, so the
		-- list is ranked WITHIN ONE CURRENCY rather than across all of them.
		--
		-- The ranking currency is the one carried by the most eligible movers, with the code as a
		-- deterministic tiebreak so the widget does not reshuffle between identical requests.
		ranking_currency AS (
			SELECT UPPER(TRIM(currency_code)) AS code
			FROM movers
			WHERE currency_code IS NOT NULL AND TRIM(currency_code) <> ''
			GROUP BY 1
			ORDER BY COUNT(*) DESC, 1 ASC
			LIMIT 1
		),
		-- A holding that states NO currency rides along, on the same unknown-is-not-a-conflict
		-- rule the rest of this surface uses: it conflicts with nothing, and excluding it would
		-- empty the widget for every row written before migration 000197. When no mover states a
		-- currency at all, ranking_currency is empty and every row is eligible.
		eligible AS (
			SELECT * FROM movers m
			WHERE m.currency_code IS NULL OR TRIM(m.currency_code) = ''
			   OR UPPER(TRIM(m.currency_code)) = (SELECT code FROM ranking_currency)
		),
		excluded AS (
			SELECT COUNT(*) AS n FROM movers m
			WHERE m.currency_code IS NOT NULL AND TRIM(m.currency_code) <> ''
			  AND UPPER(TRIM(m.currency_code)) IS DISTINCT FROM (SELECT code FROM ranking_currency)
		),
		gainers AS (
			SELECT holding_id, account_id, ticker_symbol, security_name,
			       day_change, day_change_pct, total_gain_loss, currency_code, 'gainer' AS mover_type
			FROM eligible
			WHERE day_change > 0
			ORDER BY day_change DESC
			LIMIT $2
		),
		losers AS (
			SELECT holding_id, account_id, ticker_symbol, security_name,
			       day_change, day_change_pct, total_gain_loss, currency_code, 'loser' AS mover_type
			FROM eligible
			WHERE day_change < 0
			ORDER BY day_change ASC
			LIMIT $2
		),
		combined AS (
			SELECT holding_id::text, account_id::text, ticker_symbol, security_name,
			       day_change, day_change_pct, total_gain_loss, currency_code, mover_type
			FROM gainers
			UNION ALL
			SELECT holding_id::text, account_id::text, ticker_symbol, security_name,
			       day_change, day_change_pct, total_gain_loss, currency_code, mover_type
			FROM losers
		)
		SELECT c.holding_id, c.account_id, c.ticker_symbol, c.security_name,
		       c.day_change, c.day_change_pct, c.total_gain_loss, c.currency_code, c.mover_type,
		       (SELECT code FROM ranking_currency) AS ranking_currency_code,
		       (SELECT n FROM excluded)            AS excluded_by_currency_count
		FROM combined c
		ORDER BY c.mover_type ASC, CASE WHEN c.mover_type = 'gainer' THEN c.day_change ELSE -c.day_change END DESC
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
		// The two ranking-scope columns are constant across every row. They are carried on the
		// rows rather than fetched separately to keep this a single round trip; the consequence
		// is that a result with NO movers reports no ranking currency and a zero exclusion count,
		// which is the truthful answer when nothing was ranked.
		var rankingCurrency *string
		var excludedByCurrency int
		if err := rows.Scan(
			&item.HoldingID, &item.AccountID,
			&item.TickerSymbol, &item.SecurityName,
			&item.DayChange, &item.DayChangePct, &item.TotalGainLoss,
			&item.CurrencyCode,
			&moverType,
			&rankingCurrency, &excludedByCurrency,
		); err != nil {
			return nil, fmt.Errorf("TopMovers scan: %w", err)
		}
		result.RankingCurrencyCode = rankingCurrency
		result.ExcludedByCurrencyCount = excludedByCurrency
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
// #1616 T2c: a.type = 'investment' is required in the join below, same as AllocationBreakdown /
// allocationBreakdownByAccount and TopMovers. A Plaid cash-management account is typed
// 'depository' even though it also reports holdings rows for it; without this predicate such an
// account's holdings still contributed to portfolio gain/loss — a checking account appearing in a
// portfolio view. This gap was found by an adversarial review of the AllocationBreakdown fix and
// explicitly authorised for extension by the user; see the standing-decision comment on
// IncomeSummary below, which this updates.
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
			COALESCE(SUM(h.cost_basis), 0) AS cost_basis_all,
			-- #1631 M8a. Per-account, then resolved across accounts in Go: a portfolio of a USD
			-- account and a EUR account is mixed even though neither account is mixed alone.
			` + distinctCurrencyCodesSQL("h.currency_code") + ` AS currency_codes
		FROM holdings h
		-- CURRENT-STATE universe (#1569 T12): see currentStateAccountLeg, account_liveness_sql.go
		JOIN accounts a ON a.id = h.account_id AND ` + currentStateAccountLeg("a") + `
		WHERE h.user_id = $1
		  AND a.type = 'investment' -- #1616 T2c: holdings-based surfaces are governed by accounts.type
		GROUP BY h.account_id
	`

	rows, err := r.db.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("UnrealizedGainLoss: %w", err)
	}
	defer rows.Close()

	var byAccount []domain.AccountGainLoss
	var totalValue, totalCostBasis, totalKnownPairCost, totalGL float64
	var currencyCodes []string
	for rows.Next() {
		var ag domain.AccountGainLoss
		var costBasisKnownPair float64
		var rowCurrencies []string
		if err := rows.Scan(
			&ag.AccountID,
			&ag.Value,
			&ag.UnrealizedGainLoss,
			&ag.UnrealizedGainLossPct,
			&costBasisKnownPair,
			&ag.CostBasis, // cost_basis_all: all-non-nil, exposed in API response
			&rowCurrencies,
		); err != nil {
			return nil, fmt.Errorf("UnrealizedGainLoss scan: %w", err)
		}
		totalValue += ag.Value
		totalCostBasis += ag.CostBasis           // all-non-nil → exposed TotalCostBasis
		totalKnownPairCost += costBasisKnownPair // both-non-nil → pct denominator only
		// Accumulate per-account GL directly (already FILTER-scoped in SQL) rather than
		// recomputing totalValue - totalCostBasis, which would mix all-holdings value
		// with known-basis-only cost and produce phantom gains for NULL-basis holdings.
		totalGL += ag.UnrealizedGainLoss
		currencyCodes = append(currencyCodes, rowCurrencies...)
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
	currencyCode, isMixed := domain.ResolveCurrencyCodes(currencyCodes)
	return &domain.GainLossSummary{
		TotalUnrealizedGainLoss:    totalGL,
		TotalUnrealizedGainLossPct: totalGLPct,
		TotalCostBasis:             totalCostBasis, // all-non-nil basis
		TotalValue:                 totalValue,
		RealizedGainLossSupported:  false,
		ByAccount:                  byAccount,
		CurrencyCode:               currencyCode,
		IsMixedCurrency:            isMixed,
	}, nil
}

// IncomeSummary aggregates investment income within [start, end] by normalized income type.
// The income set (domain.IncomeSubtypes) is wider than the display "dividend" bucket —
// it includes reinvested dividends/interest. Plaid stores income (cash credits) as negative
// amounts; the query negates them so returned TotalAmount values are positive magnitudes.
//
// UNIVERSE: EVENT, not current-state (#1569 T12). Every other query in this file asks "what does
// this portfolio look like NOW" and is therefore scoped to live accounts (see
// currentStateAccountLeg in account_liveness_sql.go for why). This one does not: it asks what
// income was RECEIVED over a closed date range. A dividend paid in March is a fact about March,
// and disconnecting the brokerage in June cannot un-pay it — so this query carries
// retainedAccountVisibilityLeg, exactly as investmentTransactionRepository.GetByUserID does over
// the same rows. Before T12 it did not, and the two disagreed: the investments feed listed the
// dividend while the income total silently dropped it. TotalIncome went 120.00 -> 0.00 on a
// disconnection with no transaction deleted. Pinned by
// TestRetentionUniverses_DisconnectedBrokerage_Integration.
//
// #1614: this was also the ONE live read of investment_transactions missing
// investmentSyncExclusionCondition("it") — the per-chain exclude_investment_sync opt-out
// (transaction_repository.go:2985) applied everywhere else that surface is read
// (transaction_repository.go:3554, investment_transaction_repository.go's GetByUserIDKeyset). A double-reported
// cash-management account's dividends/interest kept counting here after the user flipped the
// flag, disagreeing with the activity feed the same way T12 describes above. Fixed by adding the
// condition to this query's WHERE clause.
//
// STANDING DECISION (governs both #1614 and #1616, do not re-litigate): exclude_investment_sync
// governs investment_transactions reads ONLY. Holdings-based surfaces are never governed by this
// flag — they are governed by accounts.type instead.
//
// ⚠ BE PRECISE ABOUT WHAT IS ACTUALLY GATED TODAY — an earlier draft of this comment (T2) named
// AllocationBreakdown, UnrealizedGainLoss and TopMovers together as all type-gated, which was
// FALSE for the latter two at the time (they had no type predicate at all) and would have led a
// reader to believe coverage that did not exist. A T2c adversarial review caught that and, after
// asking the user, was given explicit authorisation to extend the gate for real rather than just
// fix the comment. All four holdings surfaces are now type-gated:
//   - AllocationBreakdown / allocationBreakdownByAccount: a.type = 'investment' (#1616 T2).
//   - UnrealizedGainLoss (~:483) and TopMovers (~:380): a.type = 'investment' (#1616 T2c).
//
// Do not re-add this predicate's absence as a "separate product question" — that framing was
// itself the defect this comment now corrects, twice. Reason exclude_investment_sync still does
// not apply here: the feed duplication that flag exists for is a per-account user preference,
// whereas the type predicate is an arithmetic/classification fact about what belongs in a
// portfolio view — wrong for every user regardless of preference, not opt-in/opt-out.
// Consequence, intended and not an oversight: an 'investment'-typed account whose owner HAS set
// exclude_investment_sync still contributes its holdings to allocation and portfolio summary; only
// accounts.type controls holdings-surface membership.
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
	// #1631: the subtype allowlist is no longer the whole universe.
	//
	// This brokerage reports a dividend as `cash/deposit` against the ETF and never emits a
	// dividend subtype, so 211 production rows worth $6,249.43 were excluded OUTRIGHT — not
	// mislabelled, absent — and reported income was a third of the truth.
	//
	// ⚠ Only the DEPOSIT (credit) leg is admitted. Its paired `cash/withdrawal` reinvestment is the
	// same economic dollar spent on shares: a −$100 deposit with a +$100 withdrawal is $100 of
	// income, not $0 (both counted under the negation below) and not $200 (both counted with
	// corrected signs). The purchase leg was excluded before this change and must stay excluded.
	// The rule, stated once: a subtype-literal reinvestment CREDIT is income; a structural
	// reinvestment DEBIT is a purchase.
	//
	// The structural condition is domain.SecurityIsStructuralSQL, shared verbatim with
	// TradeTypeToSQLPredicate so the income total and the feed's labels cannot drift apart — the
	// exact failure #1631 was filed for.
	normSub := domain.NormalizedSubtypeSQL("it.subtype")
	// #1631 I1: BOTH legs are gated on it.type = 'cash' — NormalizeTradeType and
	// TradeTypeToSQLPredicate only ever apply the subtype-literal and structural income rules
	// inside their `case "cash"` / `type = 'cash'` branches (investment_trade_type.go), and this
	// query is the third consumer of the same classification and had silently dropped that gate
	// from both legs. Without it, an ACATS/in-kind movement posted as type='transfer',
	// subtype='deposit' against a real security would be labelled Transfer everywhere else but
	// counted here as a dividend — and with the -SUM(amount) negation below, a large transfer IN
	// reads as that much dividend income. Production's transfer rows all carry subtype='transfer'
	// today, so this does not yet fire, but it is exactly the "two surfaces disagree" failure the
	// shared helpers exist to prevent.
	//
	// #1631 CX3: the structural condition is domain.StructuralDividendSQL, not a hand-rolled
	// subtype-plus-security test. It carries the amount sign and share count the rule now requires,
	// so a bank sweep deposit stops being counted as dividend income here at the same moment it
	// stops being LABELLED one in the feed. Rendering the condition inline is how income and labels
	// came to need reconciling in the first place.
	structuralIncome := fmt.Sprintf("(it.type = 'cash' AND %s)",
		domain.StructuralDividendSQL(normSub, "it.amount", "it.quantity", "sec"))

	query := fmt.Sprintf(`
		SELECT
			CASE
				WHEN %[6]s IN ('dividend', 'qualified dividend', 'non-qualified dividend')
				     THEN 'dividend'
				WHEN %[6]s IN ('interest', 'interest receivable')
				     THEN 'interest'
				WHEN %[6]s IN ('long-term capital gain', 'short-term capital gain', 'unqualified gain')
				     THEN 'capital_gain'
				WHEN %[6]s = 'dividend reinvestment'
				     THEN 'dividend_reinvestment'
				WHEN %[6]s = 'interest reinvestment'
				     THEN 'interest_reinvestment'
				-- #1631 CX4: the capital-gain reinvestments are income too, and were absent from
				-- IncomeSubtypes entirely. They get their own label rather than folding into
				-- 'capital_gain' so the distribution/reinvestment split stays symmetric with the
				-- dividend and interest pairs above.
				WHEN %[6]s IN ('long-term capital gain reinvestment', 'short-term capital gain reinvestment')
				     THEN 'capital_gain_reinvestment'
				-- A structural credit against a real security IS a dividend; the ETF distribution
				-- this institution never labels.
				WHEN %[5]s THEN 'dividend'
				ELSE 'other_income'
			END                          AS income_type,
			COUNT(*)                     AS count,
			COALESCE(-SUM(it.amount), 0) AS total_amount,
			-- #1631 M8a: income is summed over investment_transactions.amount, so the currency
			-- comes from the TRANSACTION rather than from the holding. Per income_type here, then
			-- resolved across types in Go — a USD dividend and a EUR dividend make the TOTAL
			-- mixed even if each bucket is internally consistent.
			%[7]s AS currency_codes
		FROM investment_transactions it
		JOIN accounts a ON a.id = it.account_id
		LEFT JOIN securities sec ON sec.id = it.security_id
		WHERE it.user_id = $1
		  AND (a.status = 'active'%[1]s)
		  AND it.date >= $2::date
		  AND it.date <= $3::date
		  AND ((it.type = 'cash' AND %[6]s IN (%[2]s)) OR %[5]s)
		%[3]s
		GROUP BY income_type
		ORDER BY total_amount DESC
	`, retainedAccountVisibilityLegs("it", "it.date"), inClause,
		investmentSyncExclusionCondition("it"), "", structuralIncome, normSub,
		distinctCurrencyCodesSQL("it.currency_code"))

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("IncomeSummary: %w", err)
	}
	defer rows.Close()

	var byType []domain.IncomeByType
	var totalIncome float64
	var currencyCodes []string
	for rows.Next() {
		var bt domain.IncomeByType
		var rowCurrencies []string
		if err := rows.Scan(&bt.IncomeType, &bt.Count, &bt.TotalAmount, &rowCurrencies); err != nil {
			return nil, fmt.Errorf("IncomeSummary scan: %w", err)
		}
		currencyCodes = append(currencyCodes, rowCurrencies...)
		totalIncome += bt.TotalAmount
		byType = append(byType, bt)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("IncomeSummary rows: %w", rows.Err())
	}

	if byType == nil {
		byType = []domain.IncomeByType{}
	}
	currencyCode, isMixed := domain.ResolveCurrencyCodes(currencyCodes)
	return &domain.IncomeSummary{
		TotalIncome:      totalIncome,
		ByType:           byType,
		StartDate:        start,
		StartDateDisplay: start.Format(time.DateOnly),
		EndDate:          end,
		EndDateDisplay:   end.Format(time.DateOnly),
		CurrencyCode:     currencyCode,
		IsMixedCurrency:  isMixed,
	}, nil
}

// positionReconciliationLedgerFilterSQL renders the WHERE fragment for "this
// investment_transactions row moves the position's share count" — the ledger side of the
// reconciliation. It is buy/sell/reinvest (domain.TradeTypeToSQLPredicate's partition, reused
// rather than re-derived — see the file-level comment on domain/investment_reconciliation.go) OR
// ANY type='transfer' row regardless of subtype.
//
// ⚠ The `OR it.type = 'transfer'` half is NOT optional and is not the same as
// domain.TradeTypeTransfer. That bucket deliberately EXCLUDES corporate-action subtypes (a stock
// split, subtype='split') because NormalizeTradeType displays a split as "Other", not "Transfer" —
// a display-layer call domain/investment_trade_type.go documents. But display and reconciliation
// answer different questions: Plaid's own docs describe type='transfer' as "activity that
// modifies a position, but not through buy/sell activity", covering BOTH a split and an in-kind/
// ACATS share transfer between accounts, and both change holdings.quantity with no accompanying
// buy/sell/reinvest row. A reconciliation that used TradeTypeToSQLPredicate's Transfer bucket
// (subtype-filtered) would still show a permanent drift after every split. Summing plain
// `type = 'transfer'` here, unfiltered by subtype, is what closes that gap.
func positionReconciliationLedgerFilterSQL(tradePredicate string) string {
	return "(" + tradePredicate + " OR it.type = 'transfer')"
}

// positionReconciliationLedgerCTE renders the "WITH ledger AS (...)" body shared by
// PositionReconciliation and PositionReconciliationSummary: the per (account, security)
// net ledger quantity and whether an opening buy was observed. ONE definition, so the two
// read paths can never quietly diverge about which investment_transactions rows count toward
// a position's ledger-implied delta — see positionReconciliationLedgerFilterSQL for what the
// filter itself covers (buy/sell/reinvest plus any type='transfer' row).
//
// userFilterSQL is an extra WHERE fragment (e.g. "AND it.user_id = $1", or "" for no filter);
// callers own placeholder numbering.
func positionReconciliationLedgerCTE(tradePredicate, userFilterSQL string) string {
	ledgerFilter := positionReconciliationLedgerFilterSQL(tradePredicate)
	return fmt.Sprintf(`
		ledger AS (
			SELECT
				it.account_id,
				it.security_id,
				SUM(it.quantity) FILTER (WHERE %[1]s) AS ledger_quantity,
				BOOL_OR(it.type = 'buy') AS has_buy
			FROM investment_transactions it
			LEFT JOIN securities sec ON sec.id = it.security_id
			WHERE it.security_id IS NOT NULL
			  AND it.quantity IS NOT NULL
			  %[2]s
			GROUP BY it.account_id, it.security_id
		)
	`, ledgerFilter, userFilterSQL)
}

// buildPositionReconciliationDetailQuery renders the per-user, per-row query behind
// PositionReconciliation — one row per (account, security) pair, carrying the security detail
// (ticker/name/plaid_security_id) a Drifts entry needs to be actionable. userIdx is the bind
// position for both the ledger and holdings user_id filters.
func buildPositionReconciliationDetailQuery(tradePredicate string, userIdx int) string {
	userFilter := fmt.Sprintf("AND it.user_id = $%d", userIdx)
	holdingsUserFilter := fmt.Sprintf("AND h.user_id = $%d", userIdx)

	return fmt.Sprintf(`
		WITH %[1]s
		SELECT
			h.account_id::text,
			h.security_id::text,
			sec.plaid_security_id,
			sec.ticker_symbol,
			sec.name,
			h.quantity,
			COALESCE(ledger.ledger_quantity, 0),
			COALESCE(ledger.has_buy, false)
		FROM holdings h
		-- CURRENT-STATE universe (#1569 T12) and the a.type='investment' gate (#1616 T2c), matching
		-- every other holdings-based analytics surface in this file.
		JOIN accounts a ON a.id = h.account_id AND `+currentStateAccountLeg("a")+` AND a.type = 'investment'
		JOIN securities sec ON sec.id = h.security_id
		LEFT JOIN ledger ON ledger.account_id = h.account_id AND ledger.security_id = h.security_id
		WHERE h.quantity <> 0
		%[2]s
	`, positionReconciliationLedgerCTE(tradePredicate, userFilter), holdingsUserFilter)
}

// buildPositionReconciliationSummaryQuery renders the GLOBAL, aggregate-only query behind
// PositionReconciliationSummary (#1631 M8b N2). Unlike the detail query above, this pushes
// domain.ClassifyPositionReconciliationMember's classification into SQL via COUNT(*) FILTER,
// so /admin/health's 30-second poll gets back ONE row instead of streaming every (account,
// security) pair in the system into Go just to throw all but three integers away.
//
// ⚠ THIS SHARES domain.PositionReconciliationQuantityTolerance WITH
// domain.ClassifyPositionReconciliationMember, RATHER THAN RE-TYPING THE NUMBER: %[2]g below is
// that exact Go constant formatted directly into the query text, not a second hand-typed literal
// that could drift out of sync with it. The two comparisons this renders (delta beyond
// tolerance, split on has_buy) are the same two branches that function's `if` and inner `if`
// express, and TestPositionReconciliationSummary_AgreesWithPerUserReportAcrossUsers_Integration
// proves empirically that the SQL and Go paths agree on the same seeded data across many rows.
//
// ⚠ THAT AGREEMENT HAS ONE KNOWN, NARROW, DOCUMENTED EXCEPTION: a delta of EXACTLY the tolerance.
// holdings.quantity and investment_transactions.quantity are DECIMAL(18,8) (migration 000051),
// so this SQL path compares EXACT NUMERIC arithmetic against %[2]g; the Go path instead scans
// those same columns into float64 before subtracting, and float64 cannot represent every such
// value exactly. A holding/ledger pair whose true decimal difference is precisely the tolerance
// can therefore round to slightly ABOVE it in float64 while remaining exactly AT it in NUMERIC —
// the SQL path then says "not a drift" (correctly, by strict `>`) while the Go path says "a
// drift" (also by strict `>`, just against a different, rounded number). This is a genuine
// production divergence within roughly 1e-15 of the tolerance value, not a bug in either
// comparison and not something an agreement test can paper over — see
// domain.PositionReconciliationQuantityTolerance's doc comment for the same note at the source,
// and domain.TestClassifyPositionReconciliationMember_ExactlyAtToleranceIsNotADrift (which needs
// no database) for the one thing that IS cleanly pinned: the Go function's own strictness in
// isolation, against the constant directly, with no float64 rounding involved.
//
// No securities JOIN on holdings here (unlike the detail query): the aggregate needs no
// ticker/name/plaid_security_id, so that join — and the four columns it would add to every row
// of a full-table scan — is dropped along with everything else the detail query selects that a
// count does not need.
func buildPositionReconciliationSummaryQuery(tradePredicate string) string {
	tol := domain.PositionReconciliationQuantityTolerance
	return fmt.Sprintf(`
		WITH %[1]s
		SELECT
			COUNT(*) FILTER (
				WHERE ABS(h.quantity - COALESCE(ledger.ledger_quantity, 0)) > %[2]g
				  AND COALESCE(ledger.has_buy, false)
			) AS drift_count,
			COUNT(*) FILTER (
				WHERE ABS(h.quantity - COALESCE(ledger.ledger_quantity, 0)) > %[2]g
				  AND NOT COALESCE(ledger.has_buy, false)
			) AS suppressed_count,
			COUNT(*) AS total_checked
		FROM holdings h
		-- CURRENT-STATE universe (#1569 T12) and the a.type='investment' gate (#1616 T2c), matching
		-- every other holdings-based analytics surface in this file.
		JOIN accounts a ON a.id = h.account_id AND `+currentStateAccountLeg("a")+` AND a.type = 'investment'
		LEFT JOIN ledger ON ledger.account_id = h.account_id AND ledger.security_id = h.security_id
		WHERE h.quantity <> 0
	`, positionReconciliationLedgerCTE(tradePredicate, ""), tol)
}

// PositionReconciliation compares each active holding's quantity against the net share delta
// implied by the trade ledger, and reports the drifts — see domain.PositionReconciliationReport's
// doc comment for the arithmetic, the 2-year-lookback suppression rule, why Drifts is not yet
// fully actionable, and why this never writes anything.
//
// ONE ROUND TRIP: the ledger aggregate (per account+security net quantity, and whether it
// contains an opening buy) is computed in a CTE and LEFT JOINed onto the live holdings, rather
// than fetched with a second query — a holding with NO matching ledger CTE row (no transactions
// at all for that security/account) still needs to be evaluated, which COALESCE below handles as
// a ledger quantity of 0 with no buy observed — exactly the "cannot evaluate, presumed
// pre-window" case.
func (r *investmentAnalyticsRepository) PositionReconciliation(ctx context.Context, userID string) (*domain.PositionReconciliationReport, error) {
	predicate, predArgs := domain.TradeTypeToSQLPredicate(
		[]string{domain.TradeTypeBuy, domain.TradeTypeSell, domain.TradeTypeReinvest}, "it", "sec")

	// userID is bound AFTER the predicate's own placeholders so TradeTypeToSQLPredicate's internal
	// numbering (which always starts at $1) does not collide with it.
	userIdx := len(predArgs) + 1
	args := append(append([]any{}, predArgs...), userID)

	query := buildPositionReconciliationDetailQuery(predicate, userIdx)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("PositionReconciliation: %w", err)
	}
	defer rows.Close()

	report := &domain.PositionReconciliationReport{
		Drifts: []domain.PositionReconciliationMember{},
	}
	for rows.Next() {
		var m domain.PositionReconciliationMember
		var hasBuy bool
		if err := rows.Scan(
			&m.AccountID, &m.SecurityID, &m.PlaidSecurityID, &m.TickerSymbol, &m.SecurityName,
			&m.HoldingQuantity, &m.LedgerQuantity, &hasBuy,
		); err != nil {
			return nil, fmt.Errorf("PositionReconciliation scan: %w", err)
		}
		m.Delta = m.HoldingQuantity - m.LedgerQuantity
		report.TotalPositionsChecked++

		isDrift, isSuppressed := domain.ClassifyPositionReconciliationMember(m.Delta, hasBuy)
		switch {
		case isDrift:
			report.Drifts = append(report.Drifts, m)
		case isSuppressed:
			report.SuppressedPreWindowCount++
		}
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("PositionReconciliation rows: %w", rows.Err())
	}

	return report, nil
}

// PositionReconciliationSummary is the GLOBAL admin gauge behind
// SystemHealthResponse's position-reconciliation section (#1631 M8b I1). ONE aggregate query —
// see buildPositionReconciliationSummaryQuery for why this pushes classification into SQL
// rather than reusing PositionReconciliation's per-row scan: /admin/health polls this every 30
// seconds (HealthDashboardPage.tsx's default refetchInterval) for as long as any admin has the
// tab open, and streaming every (account, security) pair in the system into Go on that cadence
// just to discard all but three integers is the exact hazard migration 000199's own comment
// warns against for the sibling forked-identity gauge (#1631 M8b N2).
func (r *investmentAnalyticsRepository) PositionReconciliationSummary(ctx context.Context) (*domain.PositionReconciliationSummary, error) {
	predicate, predArgs := domain.TradeTypeToSQLPredicate(
		[]string{domain.TradeTypeBuy, domain.TradeTypeSell, domain.TradeTypeReinvest}, "it", "sec")

	query := buildPositionReconciliationSummaryQuery(predicate)

	summary := &domain.PositionReconciliationSummary{}
	err := r.db.QueryRow(ctx, query, predArgs...).Scan(
		&summary.DriftCount, &summary.SuppressedPreWindowCount, &summary.TotalPositionsChecked,
	)
	if err != nil {
		return nil, fmt.Errorf("PositionReconciliationSummary: %w", err)
	}

	// The query completed with no error — this is a VERIFIED aggregate, possibly a real zero.
	summary.Available = true
	return summary, nil
}
