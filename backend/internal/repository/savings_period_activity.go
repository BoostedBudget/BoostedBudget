package repository

import (
	"context"
	"fmt"
	"time"
)

// SavingsPeriodActivity describes recorded activity, not the current goal balance.
// Nil actual means some history cannot safely be interpreted as new contributions.
type SavingsPeriodActivity struct {
	GoalID       string
	CurrencyCode string
	ActualAmount *float64
	Reason       string
}

type savingsActivityFact struct {
	GoalID, CurrencyCode                                             string
	Amount                                                           float64
	Unknown, Ambiguous, OverCapacity, UnknownCurrency, MixedCurrency bool
}

func (f savingsActivityFact) activity() SavingsPeriodActivity {
	row := SavingsPeriodActivity{GoalID: f.GoalID, CurrencyCode: f.CurrencyCode}
	switch {
	case f.Unknown:
		row.Reason = "legacy_unknown"
	case f.Ambiguous:
		row.Reason = "ambiguous_contribution_generation"
	case f.OverCapacity:
		row.Reason = "savings_allocation_exceeds_transaction"
	case f.UnknownCurrency:
		row.Reason = "unknown_currency"
	case f.MixedCurrency:
		row.Reason = "mixed_currency"
	default:
		row.ActualAmount = &f.Amount
	}
	return row
}

// ReadSavingsPeriodActivity can run on the report's transaction, preserving its
// snapshot. Progress-basis archives retain activity; carried balances do not add it.
const savingsPeriodActivityQuery = `
		WITH contributions AS (
			SELECT c.*,g.currency_code AS goal_currency_code FROM savings_goal_contributions c
			JOIN savings_goals g ON g.id=c.savings_goal_id AND g.user_id=$1 AND g.deleted_at IS NULL
			WHERE c.deleted_at IS NULL AND c.status='confirmed'
		), ambiguous_transactions AS (
			SELECT transaction_id FROM contributions
			WHERE transaction_id IS NOT NULL
			GROUP BY transaction_id
			HAVING BOOL_OR(archived_at IS NOT NULL) AND COUNT(*) > 1
		), transaction_weights AS (
			SELECT transaction_id,SUM(amount) FILTER (WHERE flow_kind='contribution') AS amount
			FROM contributions WHERE transaction_id IS NOT NULL GROUP BY transaction_id
		), activity AS (
			SELECT c.savings_goal_id,
				COALESCE(SUM(c.amount) FILTER (WHERE c.flow_kind='contribution'),0) AS actual,
				BOOL_OR(c.flow_kind='legacy_unknown') AS unknown,
				BOOL_OR(a.transaction_id IS NOT NULL) AS ambiguous,
				BOOL_OR(w.amount>ABS(t.amount)) AS over_capacity,
				BOOL_OR(c.transaction_id IS NOT NULL AND (account.currency_code IS NULL OR account.currency_code='')) AS unknown_currency,
				BOOL_OR(c.transaction_id IS NOT NULL AND account.currency_code<>c.goal_currency_code) AS mixed_currency
			FROM contributions c LEFT JOIN ambiguous_transactions a ON a.transaction_id=c.transaction_id
			LEFT JOIN transaction_weights w ON w.transaction_id=c.transaction_id
			LEFT JOIN transactions t ON t.id=c.transaction_id AND t.user_id=$1 AND t.deleted_at IS NULL
			LEFT JOIN accounts account ON account.id=t.account_id AND account.user_id=$1
			WHERE c.occurred_on >= $2::date AND c.occurred_on <= $3::date
			AND (c.transaction_id IS NULL OR t.pending IS DISTINCT FROM true)
			GROUP BY c.savings_goal_id
		)
		SELECT g.id,g.currency_code,COALESCE(a.actual,0),COALESCE(a.unknown,false),COALESCE(a.ambiguous,false),
			COALESCE(a.over_capacity,false),COALESCE(a.unknown_currency,false),COALESCE(a.mixed_currency,false)
		FROM savings_goals g LEFT JOIN activity a ON a.savings_goal_id=g.id
		WHERE g.user_id=$1 AND g.deleted_at IS NULL ORDER BY g.id`

func ReadSavingsPeriodActivity(ctx context.Context, q Querier, userID string, start, end time.Time) ([]SavingsPeriodActivity, error) {
	rows, err := q.Query(ctx, savingsPeriodActivityQuery, userID, start, end)
	if err != nil {
		return nil, fmt.Errorf("read savings period activity: %w", err)
	}
	defer rows.Close()
	result := make([]SavingsPeriodActivity, 0)
	for rows.Next() {
		var f savingsActivityFact
		if err := rows.Scan(&f.GoalID, &f.CurrencyCode, &f.Amount, &f.Unknown, &f.Ambiguous, &f.OverCapacity, &f.UnknownCurrency, &f.MixedCurrency); err != nil {
			return nil, fmt.Errorf("scan savings period activity: %w", err)
		}
		result = append(result, f.activity())
	}
	return result, rows.Err()
}
