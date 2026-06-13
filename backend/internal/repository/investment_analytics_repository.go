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
	// AllocationBreakdown returns portfolio allocation grouped by security type.
	// NULL security.type rows are bucketed as "unknown".
	// Joins accounts (status='active') to exclude replaced accounts.
	AllocationBreakdown(ctx context.Context, userID string) (*domain.AllocationBreakdown, error)

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

// AllocationBreakdown returns portfolio value and holding count grouped by
// security type (stock/etf/mutual fund/etc.), with NULL type bucketed as "unknown".
// Single round-trip aggregate.
func (r *investmentAnalyticsRepository) AllocationBreakdown(ctx context.Context, userID string) (*domain.AllocationBreakdown, error) {
	query := `
		SELECT
			COALESCE(sec.type, 'unknown')          AS security_type,
			COUNT(*)                               AS holding_count,
			COALESCE(SUM(h.institution_value), 0)  AS total_value
		FROM holdings h
		JOIN accounts a ON a.id = h.account_id AND a.status = 'active'
		LEFT JOIN securities sec ON sec.id = h.security_id
		WHERE h.user_id = $1
		GROUP BY COALESCE(sec.type, 'unknown')
		ORDER BY total_value DESC
	`

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
	if buckets == nil {
		buckets = []domain.AllocationItem{}
	}
	return &domain.AllocationBreakdown{Buckets: buckets, TotalValue: grandTotal}, nil
}

// TopMovers returns the top N gainers and losers ranked by day_change.
// day_change = quantity × (holdings.institution_price − securities.close_price).
// Holdings with NULL institution_price or securities with NULL close_price are excluded.
// Uses a single CTE query that computes both gainers and losers in one round-trip.
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
		)
		SELECT holding_id::text, account_id::text, ticker_symbol, security_name,
		       day_change, day_change_pct, total_gain_loss, mover_type
		FROM gainers
		UNION ALL
		SELECT holding_id::text, account_id::text, ticker_symbol, security_name,
		       day_change, day_change_pct, total_gain_loss, mover_type
		FROM losers
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
// per-account breakdown. Holdings with NULL cost_basis are included in the
// value total but excluded from gain/loss calculations to avoid phantom gains.
// Single round-trip aggregate.
func (r *investmentAnalyticsRepository) UnrealizedGainLoss(ctx context.Context, userID string) (*domain.GainLossSummary, error) {
	query := `
		SELECT
			h.account_id::text,
			COALESCE(SUM(h.institution_value), 0) AS value,
			-- Gain/loss only over holdings with non-NULL cost_basis and institution_value
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
			COALESCE(SUM(h.cost_basis) FILTER (WHERE h.cost_basis IS NOT NULL AND h.institution_value IS NOT NULL), 0) AS cost_basis_known
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
	var totalValue, totalCostBasis, totalGL float64
	for rows.Next() {
		var ag domain.AccountGainLoss
		if err := rows.Scan(
			&ag.AccountID,
			&ag.Value,
			&ag.UnrealizedGainLoss,
			&ag.UnrealizedGainLossPct,
			&ag.CostBasis,
		); err != nil {
			return nil, fmt.Errorf("UnrealizedGainLoss scan: %w", err)
		}
		totalValue += ag.Value
		totalCostBasis += ag.CostBasis
		// Accumulate per-account GL directly (already FILTER-scoped in SQL) rather than
		// recomputing totalValue - totalCostBasis, which would mix all-holdings value
		// with known-basis-only cost and produce phantom gains for NULL-basis holdings.
		totalGL += ag.UnrealizedGainLoss
		byAccount = append(byAccount, ag)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("UnrealizedGainLoss rows: %w", rows.Err())
	}

	var totalGLPct *float64
	if totalCostBasis > 0 {
		v := totalGL / totalCostBasis * 100
		totalGLPct = &v
	}
	if byAccount == nil {
		byAccount = []domain.AccountGainLoss{}
	}
	return &domain.GainLossSummary{
		TotalUnrealizedGainLoss:    totalGL,
		TotalUnrealizedGainLossPct: totalGLPct,
		TotalCostBasis:             totalCostBasis,
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
