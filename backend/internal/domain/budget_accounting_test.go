package domain

import (
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestCurrentBudgetActualsRangeExactContract(t *testing.T) {
	day := func(s string) time.Time {
		v, err := time.Parse("2006-01-02", s)
		require.NoError(t, err)
		return v
	}
	ptrDay := func(s string) *time.Time {
		v := day(s)
		return &v
	}
	now := time.Date(2026, 9, 17, 1, 30, 0, 0, time.FixedZone("UTC+02", 2*60*60))

	tests := []struct {
		name                             string
		budget                           Budget
		start, end, through, eligibility string
		eligible                         bool
	}{
		{
			name:   "weekly",
			budget: Budget{IsActive: true, Period: BudgetPeriodWeekly, StartDate: day("2020-01-01")},
			start:  "2026-09-13", end: "2026-09-19", through: "2026-09-16", eligible: true, eligibility: "eligible",
		},
		{
			name:   "monthly",
			budget: Budget{IsActive: true, Period: BudgetPeriodMonthly, StartDate: day("2020-01-01")},
			start:  "2026-09-01", end: "2026-09-30", through: "2026-09-16", eligible: true, eligibility: "eligible",
		},
		{
			name:   "quarterly",
			budget: Budget{IsActive: true, Period: BudgetPeriodQuarterly, StartDate: day("2020-01-01")},
			start:  "2026-07-01", end: "2026-09-30", through: "2026-09-16", eligible: true, eligibility: "eligible",
		},
		{
			name:   "yearly",
			budget: Budget{IsActive: true, Period: BudgetPeriodYearly, StartDate: day("2020-01-01")},
			start:  "2026-01-01", end: "2026-12-31", through: "2026-09-16", eligible: true, eligibility: "eligible",
		},
		{
			name:   "custom",
			budget: Budget{IsActive: true, Period: BudgetPeriodCustom, StartDate: day("2026-08-20"), EndDate: ptrDay("2026-10-05")},
			start:  "2026-08-20", end: "2026-10-05", through: "2026-09-16", eligible: true, eligibility: "eligible",
		},
		{
			name:   "recurring range is clipped to budget lifetime",
			budget: Budget{IsActive: true, Period: BudgetPeriodMonthly, StartDate: day("2026-09-05"), EndDate: ptrDay("2026-09-20")},
			start:  "2026-09-05", end: "2026-09-20", through: "2026-09-16", eligible: true, eligibility: "eligible",
		},
		{
			name:   "inactive planned view has unavailable actuals",
			budget: Budget{IsActive: false, Period: BudgetPeriodMonthly, StartDate: day("2026-09-01")},
			start:  "2026-09-01", end: "2026-09-30", through: "2026-09-16", eligible: false, eligibility: "inactive",
		},
		{
			name:   "future planned view has unavailable actuals",
			budget: Budget{IsActive: true, Period: BudgetPeriodMonthly, StartDate: day("2026-09-20")},
			start:  "2026-09-20", end: "2026-09-30", through: "2026-09-16", eligible: false, eligibility: "future",
		},
		{
			name:   "ended planned view has unavailable actuals",
			budget: Budget{IsActive: true, Period: BudgetPeriodMonthly, StartDate: day("2026-09-01"), EndDate: ptrDay("2026-09-10")},
			start:  "2026-09-01", end: "2026-09-10", through: "2026-09-10", eligible: false, eligibility: "ended",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CurrentBudgetActualsRange(&tt.budget, now)
			require.Equal(t, day(tt.start), got.Start)
			require.Equal(t, day(tt.end), got.End)
			require.Equal(t, day(tt.through), got.ActualThrough)
			require.Equal(t, tt.eligible, got.Eligible)
			require.Equal(t, tt.eligibility, got.Eligibility)
		})
	}
}

func TestBudgetExpenseLedgerPriorityAndPartialSavings(t *testing.T) {
	p := func(s string) *string { return &s }
	cats := []BudgetCategory{
		{ID: "individual", CategoryKind: "pfc", PFCPrimary: p("FOOD_AND_DRINK"), PFCDetailed: p("FOOD_AND_DRINK_RESTAURANTS")},
		{ID: "group", CategoryKind: "group", Members: []BudgetGroupMember{{CategoryKind: "pfc", PFCPrimary: p("FOOD_AND_DRINK")}, {CategoryKind: "uncategorized"}}},
		{ID: "other", CategoryKind: "remainder"},
	}
	facts := []BudgetExpenseFact{
		{ID: "dinner", Amount: 100, SavingsAmount: 30, PFCPrimary: "FOOD_AND_DRINK", PFCDetailed: "FOOD_AND_DRINK_RESTAURANTS", CurrencyCode: "USD"},
		{ID: "groceries", Amount: 50, PFCPrimary: "FOOD_AND_DRINK", CurrencyCode: "USD"},
		{ID: "unknown", Amount: 20, CurrencyCode: "USD"},
		{ID: "travel", Amount: 40, PFCPrimary: "TRAVEL", CurrencyCode: "USD"},
	}
	got := CalculateBudgetExpenseLedger(cats, facts)
	require.Equal(t, 70.0, *got.Lines["individual"].Amount)
	require.Equal(t, 70.0, *got.Lines["group"].Amount)
	require.Equal(t, 40.0, *got.Lines["other"].Amount)
	require.Equal(t, 0.0, *got.UnbudgetedAmount)
	require.Empty(t, got.Reason)
	withoutOther := CalculateBudgetExpenseLedger(cats[:2], facts)
	require.Equal(t, []string{"travel"}, withoutOther.UnbudgetedTransactionIDs)
	require.Equal(t, []string{"USD"}, withoutOther.UnbudgetedCurrencyCodes)
	facts[0].SavingsAmount = 101
	got = CalculateBudgetExpenseLedger(cats, facts)
	require.Nil(t, got.Lines["individual"].Amount)
	require.Equal(t, "savings_allocation_exceeds_transaction", got.Reason)
}

func TestBudgetExpenseLedgerNarrowParentStillDiagnosesOverlap(t *testing.T) {
	p := func(s string) *string { return &s }
	cats := []BudgetCategory{{ID: "parent", CategoryKind: "pfc", PFCPrimary: p("FOOD_AND_DRINK"), Tags: []string{"shared"}, MatchLogic: MatchLogicAnd}, {ID: "child", CategoryKind: "pfc", PFCPrimary: p("FOOD_AND_DRINK"), PFCDetailed: p("FOOD_AND_DRINK_RESTAURANTS")}}
	got := CalculateBudgetExpenseLedger(cats, []BudgetExpenseFact{{ID: "one", Amount: 10, PFCPrimary: "FOOD_AND_DRINK", PFCDetailed: "FOOD_AND_DRINK_RESTAURANTS", Tags: []string{"shared"}}})
	require.Contains(t, got.Reasons, "overlapping_individual_lines")
	cats[0].MatchLogic = MatchLogicCategoryOnly
	got = CalculateBudgetExpenseLedger(cats, []BudgetExpenseFact{{ID: "one", Amount: 10, PFCPrimary: "FOOD_AND_DRINK", PFCDetailed: "FOOD_AND_DRINK_RESTAURANTS"}})
	require.Empty(t, got.Reasons, "a true containing parent is a natural suballocation")
}
