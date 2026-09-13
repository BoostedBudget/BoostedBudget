package domain

import "time"

// BudgetPlanVsActualResponse compares the current configuration with recorded
// activity. Null actuals are unprovable, not zero; AsOf is not bank freshness.
type BudgetPlanVsActualResponse struct {
	BudgetID      string           `json:"budget_id"`
	CurrencyCode  string           `json:"currency_code"`
	Period        BudgetPeriod     `json:"period"`
	PeriodStart   *string          `json:"period_start" format:"date"`
	PeriodEnd     *string          `json:"period_end" format:"date"`
	ActualThrough *string          `json:"actual_through" format:"date"`
	AsOf          time.Time        `json:"as_of"`
	Eligibility   string           `json:"eligibility" enum:"eligible,future,ended,inactive"`
	Completeness  string           `json:"completeness" enum:"complete,partial,unavailable"`
	Reasons       []string         `json:"reasons" nullable:"false"`
	Income        BudgetPlanIncome `json:"income"`
	Lines         []BudgetPlanLine `json:"lines" nullable:"false"`
	Totals        BudgetPlanTotals `json:"totals"`
}

type BudgetPlanIncome struct {
	Expected       float64  `json:"expected"`
	Observed       *float64 `json:"observed"`
	ManualDeclared float64  `json:"manual_declared"`
	Reasons        []string `json:"reasons" nullable:"false"`
}

type BudgetPlanLine struct {
	ID               string   `json:"id"`
	BudgetCategoryID *string  `json:"budget_category_id"`
	SavingsGoalID    *string  `json:"savings_goal_id"`
	Kind             string   `json:"kind" enum:"expense,savings,unbudgeted"`
	Label            string   `json:"label"`
	Planned          float64  `json:"planned"`
	Actual           *float64 `json:"actual"`
	Variance         *float64 `json:"variance"`
	Completeness     string   `json:"completeness" enum:"complete,partial,unavailable"`
	Reasons          []string `json:"reasons" nullable:"false"`
}

type BudgetPlanTotals struct {
	Allocated     float64  `json:"allocated"`
	Unallocated   float64  `json:"unallocated"`
	Overallocated float64  `json:"overallocated"`
	Actual        *float64 `json:"actual"`
	Unbudgeted    *float64 `json:"unbudgeted"`
}
