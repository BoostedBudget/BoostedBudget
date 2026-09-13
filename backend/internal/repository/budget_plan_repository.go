package repository

import (
	"context"
	"encoding/json"
	"math"
	"strconv"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

type budgetPlanRepository struct{ db DB }

func NewBudgetPlanRepository(db DB) *budgetPlanRepository { return &budgetPlanRepository{db: db} }

// GetPlanVsActual does not synchronize or write any budget state. Every read,
// including membership and contribution provenance, sees one committed snapshot.
func (r *budgetPlanRepository) GetPlanVsActual(ctx context.Context, userID, budgetID string, asOf time.Time) (*domain.BudgetPlanVsActualResponse, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err = tx.Exec(ctx, "SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY"); err != nil {
		return nil, err
	}
	result, err := readBudgetPlanInSnapshot(ctx, tx, userID, budgetID, asOf)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

func readBudgetPlanInSnapshot(ctx context.Context, q DB, userID, budgetID string, asOf time.Time) (*domain.BudgetPlanVsActualResponse, error) {
	var owned bool
	if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM budgets WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL)`, budgetID, userID).Scan(&owned); err != nil {
		return nil, err
	}
	if !owned {
		return nil, domain.ErrBudgetNotFound
	}
	budget, err := NewBudgetRepository(q).GetByID(ctx, budgetID)
	if err != nil {
		return nil, err
	}
	customNames, err := budgetPlanCustomNames(ctx, q, userID, budget.Categories)
	if err != nil {
		return nil, err
	}
	rangeInfo := domain.CurrentBudgetActualsRange(budget, asOf)
	r := &domain.BudgetPlanVsActualResponse{BudgetID: budget.ID, CurrencyCode: budget.CurrencyCode, Period: budget.Period, AsOf: asOf.UTC(), Eligibility: rangeInfo.Eligibility, Completeness: "complete", Reasons: []string{}, Lines: []domain.BudgetPlanLine{}, Income: domain.BudgetPlanIncome{Expected: budget.ExpectedIncome, Reasons: []string{}}}
	addReason := func(reason string) {
		if reason == "" {
			return
		}
		for _, old := range r.Reasons {
			if old == reason {
				return
			}
		}
		r.Reasons = append(r.Reasons, reason)
	}
	for _, source := range budget.IncomeSources {
		if source.Type == domain.IncomeSourceTypeManual {
			amount, e := strconv.ParseFloat(source.Value, 64)
			if e != nil || math.IsNaN(amount) || math.IsInf(amount, 0) || amount < 0 {
				r.Income.Reasons = append(r.Income.Reasons, "invalid_manual_income")
				addReason("invalid_manual_income")
			} else {
				r.Income.ManualDeclared += amount
			}
		}
	}
	if rangeInfo.Eligible {
		start, end, through := rangeInfo.Start.Format("2006-01-02"), rangeInfo.End.Format("2006-01-02"), rangeInfo.ActualThrough.Format("2006-01-02")
		r.PeriodStart = &start
		r.PeriodEnd = &end
		r.ActualThrough = &through
	} else {
		r.Completeness = "unavailable"
		reason := map[string]string{"inactive": "budget_inactive", "future": "period_not_started", "ended": "period_ended"}[rangeInfo.Eligibility]
		addReason(reason)
	}
	r.Totals.Allocated = budget.TotalAllocated()
	r.Totals.Unallocated = math.Max(0, r.Income.Expected-r.Totals.Allocated)
	r.Totals.Overallocated = math.Max(0, r.Totals.Allocated-r.Income.Expected)
	ledger := domain.BudgetExpenseLedger{}
	savings := map[string]SavingsPeriodActivity{}
	if rangeInfo.Eligible {
		ledger, err = ReadBudgetExpenseLedger(ctx, q, userID, budget.Categories, rangeInfo.Start, rangeInfo.ActualThrough)
		if err != nil {
			return nil, err
		}
		for _, reason := range ledger.Reasons {
			addReason(reason)
		}
		for _, currency := range ledger.CurrencyCodes {
			if reason := budgetCurrencyReason(currency, budget.CurrencyCode); reason != "" {
				addReason(reason)
			}
		}
		activity, e := ReadSavingsPeriodActivity(ctx, q, userID, rangeInfo.Start, rangeInfo.ActualThrough)
		if e != nil {
			return nil, e
		}
		for _, row := range activity {
			savings[row.GoalID] = row
		}
		observed, currencies, e := readBudgetObservedIncome(ctx, q, userID, budget.IncomeSources, rangeInfo.Start, rangeInfo.ActualThrough)
		if e != nil {
			return nil, e
		}
		r.Income.Observed = &observed
		for _, currency := range currencies {
			if reason := budgetCurrencyReason(currency, budget.CurrencyCode); reason != "" {
				r.Income.Observed = nil
				r.Income.Reasons = append(r.Income.Reasons, reason)
				addReason(reason)
			}
		}
	}
	total := 0.0
	allKnown := rangeInfo.Eligible
	for _, cat := range budget.Categories {
		if nested, _ := domain.IsCategorySubAllocation(budget.Categories, cat.ID); nested {
			continue
		}
		line := domain.BudgetPlanLine{ID: cat.ID, BudgetCategoryID: &cat.ID, SavingsGoalID: cat.SavingsGoalID, Kind: "expense", Planned: cat.AllocatedAmount, Completeness: "complete", Reasons: []string{}}
		cat.PopulatePFCDisplay()
		line.Label = budgetPlanLabel(cat)
		if cat.CustomCategoryID != nil && customNames[*cat.CustomCategoryID] != "" {
			line.Label = customNames[*cat.CustomCategoryID]
		}
		if cat.CategoryKind == "savings" {
			line.Kind = "savings"
		}
		if rangeInfo.Eligible {
			if line.Kind == "savings" && cat.SavingsGoalID != nil {
				if row, ok := savings[*cat.SavingsGoalID]; ok {
					line.Actual = row.ActualAmount
					if row.Reason != "" {
						reason := map[string]string{"legacy_unknown": "legacy_savings_origin", "ambiguous_contribution_generation": "ambiguous_savings_generation"}[row.Reason]
						if reason == "" {
							reason = row.Reason
						}
						line.Reasons = append(line.Reasons, reason)
					}
					if reason := budgetCurrencyReason(row.CurrencyCode, budget.CurrencyCode); reason != "" {
						line.Actual = nil
						line.Reasons = append(line.Reasons, reason)
					}
				} else {
					line.Reasons = append(line.Reasons, "legacy_savings_origin")
				}
			} else if actual, ok := ledger.Lines[cat.ID]; ok {
				line.Actual = actual.Amount
				if actual.Reason != "" {
					line.Reasons = append(line.Reasons, actual.Reason)
				}
				for _, currency := range actual.CurrencyCodes {
					if reason := budgetCurrencyReason(currency, budget.CurrencyCode); reason != "" {
						line.Actual = nil
						line.Reasons = append(line.Reasons, reason)
					}
				}
			}
		}
		if line.Actual == nil {
			line.Completeness = "unavailable"
			allKnown = false
		} else {
			variance := *line.Actual - line.Planned
			line.Variance = &variance
			total += *line.Actual
		}
		for _, reason := range line.Reasons {
			addReason(reason)
		}
		r.Lines = append(r.Lines, line)
	}
	if rangeInfo.Eligible {
		unbudgetedReasons := []string{}
		for _, currency := range ledger.UnbudgetedCurrencyCodes {
			if reason := budgetCurrencyReason(currency, budget.CurrencyCode); reason != "" {
				ledger.UnbudgetedAmount = nil
				unbudgetedReasons = append(unbudgetedReasons, reason)
			}
		}
		r.Totals.Unbudgeted = ledger.UnbudgetedAmount
		if ledger.UnbudgetedAmount == nil || *ledger.UnbudgetedAmount > 0 {
			line := domain.BudgetPlanLine{ID: "unbudgeted", Kind: "unbudgeted", Label: "Unbudgeted spending", Actual: ledger.UnbudgetedAmount, Variance: ledger.UnbudgetedAmount, Completeness: "complete", Reasons: unbudgetedReasons}
			if line.Actual == nil {
				line.Completeness = "unavailable"
				allKnown = false
			} else {
				total += *line.Actual
			}
			r.Lines = append(r.Lines, line)
		}
		for _, reason := range r.Reasons {
			if reason != "invalid_manual_income" {
				allKnown = false
			}
		}
		if allKnown {
			r.Totals.Actual = &total
		} else {
			r.Completeness = "partial"
		}
	}
	return r, nil
}

func budgetPlanCustomNames(ctx context.Context, q Querier, userID string, categories []domain.BudgetCategory) (map[string]string, error) {
	ids := []string{}
	for _, category := range categories {
		if category.CustomCategoryID != nil {
			ids = append(ids, *category.CustomCategoryID)
		}
	}
	names := map[string]string{}
	if len(ids) == 0 {
		return names, nil
	}
	rows, err := q.Query(ctx, `SELECT id,name FROM custom_categories WHERE user_id=$1 AND id=ANY($2) AND deleted_at IS NULL`, userID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		names[id] = name
	}
	return names, rows.Err()
}

func budgetCurrencyReason(currency, expected string) string {
	if currency == "" || expected == "" {
		return "unknown_currency"
	}
	if currency != expected {
		return "mixed_currency"
	}
	return ""
}

func budgetPlanLabel(cat domain.BudgetCategory) string {
	if cat.CategoryKind == "remainder" {
		return "Other"
	}
	if cat.GroupName != nil {
		return *cat.GroupName
	}
	if cat.SavingsGoalName != nil {
		return *cat.SavingsGoalName
	}
	if cat.PFCDetailedDisplay != nil {
		return *cat.PFCDetailedDisplay
	}
	if cat.PFCPrimaryDisplay != nil {
		return *cat.PFCPrimaryDisplay
	}
	if cat.CategoryKind == "savings" {
		return "Savings goal"
	}
	if cat.CategoryKind == "custom" {
		return "Custom category"
	}
	return "Uncategorized"
}

const budgetObservedIncomeQuery = `SELECT COALESCE(a.currency_code,''),SUM(-t.amount)
	FROM transactions t JOIN accounts a ON a.id=t.account_id AND a.user_id=t.user_id
	LEFT JOIN custom_categories cc ON cc.id=t.custom_category_override_id AND cc.user_id=t.user_id
	LEFT JOIN custom_categories gp ON gp.id=cc.parent_id AND gp.user_id=t.user_id
	-- tombstone-exempt(custom_categories cc, custom_categories gp): HISTORY FILTER. Retained ancestry still classifies recorded income and transfers.
	WHERE t.user_id=$1 AND t.deleted_at IS NULL AND t.pending=false AND t.amount<0 AND t.date BETWEEN $2 AND $3
	AND COALESCE(t.effective_pfc_primary,cc.parent_pfc_primary,gp.parent_pfc_primary,'') NOT LIKE 'TRANSFER%'
	AND EXISTS (SELECT 1 FROM jsonb_array_elements($4::jsonb) AS configured(value) WHERE
	(configured.value->>'type'='pfc_primary' AND configured.value->>'value'=COALESCE(t.effective_pfc_primary,cc.parent_pfc_primary,gp.parent_pfc_primary)) OR
	(configured.value->>'type'='tag' AND t.tags ? (configured.value->>'value')))
	GROUP BY a.currency_code`

func readBudgetObservedIncome(ctx context.Context, q Querier, userID string, sources []domain.IncomeSource, start, end time.Time) (float64, []string, error) {
	if len(sources) == 0 {
		return 0, []string{}, nil
	}
	encoded, err := json.Marshal(sources)
	if err != nil {
		return 0, nil, err
	}
	rows, err := q.Query(ctx, budgetObservedIncomeQuery, userID, start, end, encoded)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	var total float64
	currencies := []string{}
	for rows.Next() {
		var currency string
		var amount float64
		if err := rows.Scan(&currency, &amount); err != nil {
			return 0, nil, err
		}
		total += amount
		currencies = append(currencies, currency)
	}
	return total, currencies, rows.Err()
}
