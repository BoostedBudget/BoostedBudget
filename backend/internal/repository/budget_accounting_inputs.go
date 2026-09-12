package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// ReadBudgetAccountingInputs reads both sides of a contribution-backed transfer
// in ONE SQL statement snapshot, including at READ COMMITTED. Configuration writers
// acquire the hierarchy lock before this statement, so their config is fresh
// without letting a contribution commit between expense and savings reads.
func ReadBudgetAccountingInputs(ctx context.Context, q Querier, userID string, categories []domain.BudgetCategory, start, end time.Time) (domain.BudgetExpenseLedger, []SavingsPeriodActivity, error) {
	ledger, savings, _, err := readBudgetAccountingInputs(ctx, q, userID, categories, start, end, nil)
	return ledger, savings, err
}

type budgetIncomeFact struct {
	CurrencyCode string
	Amount       float64
}

func readBudgetAccountingInputs(ctx context.Context, q Querier, userID string, categories []domain.BudgetCategory, start, end time.Time, sources []domain.IncomeSource) (domain.BudgetExpenseLedger, []SavingsPeriodActivity, []budgetIncomeFact, error) {
	query := `SELECT 'expense',row_to_json(e) FROM (` + budgetExpenseFactsQuery + `) e("ID","Amount","PFCPrimary","PFCDetailed","CustomCategoryID","CustomParentID","CustomPFCPrimary","CustomGrandparentPFCPrimary","CurrencyCode","Tags","SavingsAmount","Reason","CustomCategoryName")
 UNION ALL SELECT 'savings',row_to_json(s) FROM (` + savingsPeriodActivityQuery + `) s("GoalID","CurrencyCode","Amount","Unknown","Ambiguous","OverCapacity","UnknownCurrency","MixedCurrency")`
	args := []any{userID, start, end}
	if len(sources) > 0 {
		encoded, err := json.Marshal(sources)
		if err != nil {
			return domain.BudgetExpenseLedger{}, nil, nil, err
		}
		query += ` UNION ALL SELECT 'income',row_to_json(i) FROM (` + budgetObservedIncomeQuery + `) i("CurrencyCode","Amount")`
		args = append(args, encoded)
	}
	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return domain.BudgetExpenseLedger{}, nil, nil, err
	}
	defer rows.Close()
	facts := []domain.BudgetExpenseFact{}
	activity := []SavingsPeriodActivity{}
	income := []budgetIncomeFact{}
	for rows.Next() {
		var kind string
		var raw []byte
		if err := rows.Scan(&kind, &raw); err != nil {
			return domain.BudgetExpenseLedger{}, nil, nil, err
		}
		if kind == "expense" {
			var f domain.BudgetExpenseFact
			if err := json.Unmarshal(raw, &f); err != nil {
				return domain.BudgetExpenseLedger{}, nil, nil, err
			}
			facts = append(facts, f)
		} else if kind == "income" {
			var f budgetIncomeFact
			if err := json.Unmarshal(raw, &f); err != nil {
				return domain.BudgetExpenseLedger{}, nil, nil, err
			}
			income = append(income, f)
		} else {
			var f savingsActivityFact
			if err := json.Unmarshal(raw, &f); err != nil {
				return domain.BudgetExpenseLedger{}, nil, nil, err
			}
			activity = append(activity, f.activity())
		}
	}
	if err := rows.Err(); err != nil {
		return domain.BudgetExpenseLedger{}, nil, nil, err
	}
	return domain.CalculateBudgetExpenseLedger(categories, facts), activity, income, nil
}
