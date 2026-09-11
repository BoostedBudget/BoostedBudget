package domain

// Issue #1630, Task 10: `amount` is silently overwritten by `expected_income` when the latter
// is set and > 0 (budget_service.go's reconciliation, both CreateBudget and UpdateBudget) —
// deliberate since issue #1048, but undocumented in the published contract, which is what the
// issue's reporter tripped on. Declining the issue's preferred "make amount read-only" fix
// (amount is still authoritative when expected_income is absent — read-only would break the
// non-envelope path); instead documenting the precedence on both fields so it is discoverable
// without reading source.
//
// end_date is DOC-ONLY per task 0: a monthly budget with no end_date is created successfully
// (TestClaim1630_P5_MonthlyWithoutEndDate_DoesNotReproduce), not rejected with 422 as claimed.
// The real rule — required only for period:"custom", and must be after start_date when set —
// is documented here instead of "fixed".
import (
	"reflect"
	"testing"
)

func TestBudgetRequestFieldsDocumentAmountPrecedenceAndEndDateRule(t *testing.T) {
	cases := []struct {
		what           string
		typ            reflect.Type
		field          string
		mustMentionAll []string
	}{
		{"CreateBudgetRequest.Amount", reflect.TypeOf(CreateBudgetRequest{}), "Amount", []string{"expected_income"}},
		{"CreateBudgetRequest.ExpectedIncome", reflect.TypeOf(CreateBudgetRequest{}), "ExpectedIncome", []string{"amount"}},
		{"CreateBudgetRequest.EndDate", reflect.TypeOf(CreateBudgetRequest{}), "EndDate", []string{"custom", "start_date"}},
		{"UpdateBudgetRequest.Amount", reflect.TypeOf(UpdateBudgetRequest{}), "Amount", []string{"expected_income"}},
		{"UpdateBudgetRequest.ExpectedIncome", reflect.TypeOf(UpdateBudgetRequest{}), "ExpectedIncome", []string{"amount"}},
		{"UpdateBudgetRequest.EndDate", reflect.TypeOf(UpdateBudgetRequest{}), "EndDate", []string{"custom", "start_date"}},
	}

	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			f, ok := c.typ.FieldByName(c.field)
			if !ok {
				t.Fatalf("%s: no such field", c.what)
			}
			doc := f.Tag.Get("doc")
			if doc == "" {
				t.Fatalf("%s needs a doc tag", c.what)
			}
			for _, want := range c.mustMentionAll {
				if !contains(doc, want) {
					t.Errorf("%s doc tag %q must mention %q", c.what, doc, want)
				}
			}
		})
	}
}
