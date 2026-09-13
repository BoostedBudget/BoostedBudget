package domain

import (
	"sort"
	"time"
)

// BudgetActualsRange is a UTC calendar interval. ActualThrough never includes a future day.
type BudgetActualsRange struct {
	Start, End, ActualThrough time.Time
	Eligible                  bool
	Eligibility               string
}

func CurrentBudgetActualsRange(b *Budget, now time.Time) BudgetActualsRange {
	day := func(t time.Time) time.Time {
		t = t.UTC()
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	}
	now = day(now)
	r := BudgetActualsRange{ActualThrough: now, Eligibility: "eligible"}
	switch b.Period {
	case BudgetPeriodWeekly:
		r.Start = now.AddDate(0, 0, -int(now.Weekday()))
		r.End = r.Start.AddDate(0, 0, 6)
	case BudgetPeriodQuarterly:
		r.Start = time.Date(now.Year(), time.Month((int(now.Month())-1)/3*3+1), 1, 0, 0, 0, 0, time.UTC)
		r.End = r.Start.AddDate(0, 3, -1)
	case BudgetPeriodYearly:
		r.Start = time.Date(now.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
		r.End = r.Start.AddDate(1, 0, -1)
	case BudgetPeriodCustom:
		r.Start = day(b.StartDate)
		r.End = now
	default:
		r.Start = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		r.End = r.Start.AddDate(0, 1, -1)
	}
	if day(b.StartDate).After(r.Start) {
		r.Start = day(b.StartDate)
	}
	if b.EndDate != nil && (b.Period == BudgetPeriodCustom || day(*b.EndDate).Before(r.End)) {
		r.End = day(*b.EndDate)
	}
	switch {
	case !b.IsActive:
		r.Eligibility = "inactive"
	case day(b.StartDate).After(now):
		r.Eligibility = "future"
	case b.EndDate != nil && day(*b.EndDate).Before(now):
		r.Eligibility = "ended"
	default:
		r.Eligible = true
	}
	if r.End.Before(r.ActualThrough) {
		r.ActualThrough = r.End
	}
	return r
}

// BudgetExpenseFact is an already-owned, posted, positive, non-transfer ledger row.
// Custom ancestry is historical: deleting a category must not restate old spending.
type BudgetExpenseFact struct {
	CustomCategoryName                                                                                                         string
	ID, PFCPrimary, PFCDetailed, CustomCategoryID, CustomParentID, CustomPFCPrimary, CustomGrandparentPFCPrimary, CurrencyCode string
	Tags                                                                                                                       []string
	Amount, SavingsAmount                                                                                                      float64
	Reason                                                                                                                     string
}
type BudgetExpenseLineActual struct {
	Amount         *float64
	TransactionIDs []string
	Reason         string
	CurrencyCodes  []string
}
type BudgetExpenseLedger struct {
	TransactionAmounts       map[string]*float64
	UnbudgetedTransactionIDs []string
	UnbudgetedCurrencyCodes  []string
	Lines                    map[string]BudgetExpenseLineActual
	UnbudgetedAmount         *float64
	Reason                   string
	Reasons                  []string
	CurrencyCodes            []string
}

func expenseSelectorMatches(m BudgetGroupMember, f BudgetExpenseFact) bool {
	equal := func(p *string, s string) bool { return p != nil && *p != "" && *p == s }
	switch m.CategoryKind {
	case "uncategorized":
		return f.PFCPrimary == "" && f.CustomCategoryID == ""
	case "custom":
		return equal(m.CustomCategoryID, f.CustomCategoryID) || equal(m.CustomCategoryID, f.CustomParentID)
	case "pfc":
		if m.PFCDetailed != nil && *m.PFCDetailed != "" {
			return equal(m.PFCPrimary, f.PFCPrimary) && equal(m.PFCDetailed, f.PFCDetailed)
		}
		return equal(m.PFCPrimary, f.PFCPrimary) || equal(m.PFCPrimary, f.CustomPFCPrimary) || equal(m.PFCPrimary, f.CustomGrandparentPFCPrimary)
	}
	return false
}
func expenseLineMatches(c BudgetCategory, f BudgetExpenseFact) bool {
	if c.CategoryKind == "group" {
		for _, m := range c.Members {
			if expenseSelectorMatches(m, f) {
				return true
			}
		}
		return false
	}
	category := expenseSelectorMatches(BudgetGroupMember{CategoryKind: c.CategoryKind, PFCPrimary: c.PFCPrimary, PFCDetailed: c.PFCDetailed, CustomCategoryID: c.CustomCategoryID}, f)
	tagged := false
	for _, tag := range c.Tags {
		for _, actual := range f.Tags {
			if tag == actual {
				tagged = true
			}
		}
	}
	switch c.MatchLogic {
	case MatchLogicTagOnly:
		return tagged
	case MatchLogicAnd:
		return category && tagged
	case MatchLogicOr:
		return category || tagged
	default:
		return category
	}
}

// CalculateBudgetExpenseLedger partitions by transaction identity, never by subtracting
// independently summed category totals. Natural suballocations remain visible but are
// not treated as competing top-level matches.
func CalculateBudgetExpenseLedger(categories []BudgetCategory, facts []BudgetExpenseFact) BudgetExpenseLedger {
	zero := 0.0
	r := BudgetExpenseLedger{Lines: map[string]BudgetExpenseLineActual{}, UnbudgetedAmount: &zero, TransactionAmounts: map[string]*float64{}}
	primary, custom := budgetedParentSetsFor(categories)
	for _, c := range categories {
		if c.CategoryKind != "savings" {
			v := 0.0
			r.Lines[c.ID] = BudgetExpenseLineActual{Amount: &v, TransactionIDs: []string{}}
		}
	}
	currencies := map[string]bool{}
	reasons := map[string]bool{}
	for _, f := range facts {
		currencies[f.CurrencyCode] = true
		reason := f.Reason
		if f.SavingsAmount > f.Amount {
			reason = "savings_allocation_exceeds_transaction"
		}
		amount := f.Amount - f.SavingsAmount
		if reason == "" {
			counted := amount
			r.TransactionAmounts[f.ID] = &counted
		} else {
			r.TransactionAmounts[f.ID] = nil
		}
		matched := []BudgetCategory{}
		top := 0
		for _, c := range categories {
			if c.CategoryKind != "group" && c.CategoryKind != "remainder" && c.CategoryKind != "savings" && expenseLineMatches(c, f) {
				matched = append(matched, c)
				if !c.IsSubAllocationWithin(primary, custom) {
					top++
				}
			}
		}
		if top > 1 {
			reasons["overlapping_individual_lines"] = true
		}
		if len(matched) == 0 {
			for _, c := range categories {
				if c.CategoryKind == "group" && expenseLineMatches(c, f) {
					matched = append(matched, c)
				}
			}
		}
		if len(matched) == 0 {
			for _, c := range categories {
				if c.CategoryKind == "remainder" {
					matched = append(matched, c)
				}
			}
		}
		if reason != "" {
			reasons[reason] = true
		}
		if len(matched) == 0 {
			r.UnbudgetedTransactionIDs = append(r.UnbudgetedTransactionIDs, f.ID)
			foundCurrency := false
			for _, code := range r.UnbudgetedCurrencyCodes {
				if code == f.CurrencyCode {
					foundCurrency = true
				}
			}
			if !foundCurrency {
				r.UnbudgetedCurrencyCodes = append(r.UnbudgetedCurrencyCodes, f.CurrencyCode)
			}
			if reason != "" {
				r.UnbudgetedAmount = nil
			} else if r.UnbudgetedAmount != nil {
				*r.UnbudgetedAmount += amount
			}
		}
		for _, c := range matched {
			line := r.Lines[c.ID]
			line.TransactionIDs = append(line.TransactionIDs, f.ID)
			found := false
			for _, currency := range line.CurrencyCodes {
				if currency == f.CurrencyCode {
					found = true
				}
			}
			if !found {
				line.CurrencyCodes = append(line.CurrencyCodes, f.CurrencyCode)
			}
			if reason != "" {
				line.Amount = nil
				line.Reason = reason
			} else if line.Amount != nil {
				*line.Amount += amount
			}
			r.Lines[c.ID] = line
		}
	}
	for c := range currencies {
		r.CurrencyCodes = append(r.CurrencyCodes, c)
	}
	sort.Strings(r.CurrencyCodes)
	for reason := range reasons {
		r.Reasons = append(r.Reasons, reason)
	}
	sort.Strings(r.Reasons)
	if len(r.Reasons) > 0 {
		r.Reason = r.Reasons[0]
	}
	return r
}
