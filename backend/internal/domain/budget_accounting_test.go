package domain

import (
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestCurrentBudgetActualsRangeClipsUTCAndRejectsNoCurrentPeriod(t *testing.T) {
	day := func(s string) time.Time { v, e := time.Parse("2006-01-02", s); require.NoError(t, e); return v }
	end := day("2026-09-20")
	b := &Budget{IsActive: true, Period: BudgetPeriodMonthly, StartDate: day("2026-09-05"), EndDate: &end}
	got := CurrentBudgetActualsRange(b, time.Date(2026, 9, 13, 1, 0, 0, 0, time.FixedZone("east", 7200)))
	require.True(t, got.Eligible)
	require.Equal(t, day("2026-09-05"), got.Start)
	require.Equal(t, day("2026-09-20"), got.End)
	require.Equal(t, day("2026-09-12"), got.ActualThrough)
	for _, now := range []string{"2026-09-01", "2026-09-21"} {
		require.False(t, CurrentBudgetActualsRange(b, day(now)).Eligible)
	}
	b.IsActive = false
	require.False(t, CurrentBudgetActualsRange(b, day("2026-09-12")).Eligible)
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
