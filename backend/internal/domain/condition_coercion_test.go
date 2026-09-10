package domain

import (
	"reflect"
	"testing"
)

// #1625 Task 2. CoercedValue is the READ-PATH repair for `invalid_condition_value_shape`.
//
// That reason is the ONE of the four that real user rules can be in: dedac0aa (#346) validated
// only cond.Type.IsValid() and never the value shape, so a wrong-typed value persisted through
// the ordinary public API for the entire life of the feature. Switching those rules off would
// switch off rules that people wrote and that mostly express an unambiguous intent, so where the
// intent IS unambiguous the value is repaired instead.
//
// ⚠ Every case here is a rule that ValidateShape REJECTS. The write path is unchanged — see
// TestCoercibleValuesAreStillRejectedOnTheWritePath.
func TestCoercedValue_RepairsTheUnambiguousShapes(t *testing.T) {
	tests := []struct {
		name string
		cond RuleCondition
		want interface{}
	}{
		{
			"a number sent for a text field becomes its string",
			RuleCondition{Field: FieldMerchant, Operator: "contains", Value: 42.0},
			[]string{"42"},
		},
		{
			"an integer for a text field keeps no spurious decimal point",
			RuleCondition{Field: FieldName, Operator: "eq", Value: 1000},
			[]string{"1000"},
		},
		{
			"a fractional number for a text field keeps its fraction",
			RuleCondition{Field: FieldName, Operator: "eq", Value: 12.5},
			[]string{"12.5"},
		},
		{
			"an array mixing numbers and strings coerces every member",
			RuleCondition{Field: FieldMerchant, Operator: "in", Value: []interface{}{7, "lyft"}},
			[]string{"7", "lyft"},
		},
		{
			"a numeric string for a number field becomes the number",
			RuleCondition{Field: FieldAmount, Operator: "gt", Value: "1000"},
			1000.0,
		},
		{
			"surrounding whitespace does not make a number unreadable",
			RuleCondition{Field: FieldSignedAmount, Operator: "lte", Value: "  -12.34 "},
			-12.34,
		},
		{
			"a numeric-range object with string bounds coerces both bounds",
			RuleCondition{Field: FieldAmount, Operator: "between",
				Value: map[string]interface{}{"min": "100", "max": "500"}},
			map[string]interface{}{"min": 100.0, "max": 500.0},
		},
		{
			"an RFC3339 timestamp for a date field becomes its calendar day",
			RuleCondition{Field: FieldDate, Operator: "eq", Value: "2026-01-31T00:00:00Z"},
			"2026-01-31",
		},
		{
			"an unpadded date becomes the padded form the evaluator parses",
			RuleCondition{Field: FieldDate, Operator: "gte", Value: "2026-1-5"},
			"2026-01-05",
		},
		{
			"a date range coerces each bound and keeps a single-sided range single-sided",
			RuleCondition{Field: FieldDate, Operator: "between",
				Value: map[string]interface{}{"start": "2026-1-1"}},
			map[string]interface{}{"start": "2026-01-01"},
		},
		{
			// The enum shape: direction is a text field with an EnumFn. A member of the closed
			// set passes through; a non-member is uncoercible (see the refusal table below),
			// because coercion has to re-check the enum or it would hand the evaluator a value
			// the write path refuses.
			"a valid closed-enum value passes through untouched",
			RuleCondition{Field: FieldDirection, Operator: "eq", Value: "outflow"},
			[]string{"outflow"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := tt.cond.CoercedValue()
			if !ok {
				t.Fatalf("value %#v was judged uncoercible; it has one unambiguous reading", tt.cond.Value)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("coerced to %#v, want %#v", got, tt.want)
			}
			// The repair must produce something the strict validator accepts, or it has moved
			// the problem rather than fixed it.
			repaired := tt.cond
			repaired.Value = got
			if err := repaired.ValidateShape(); err != nil {
				t.Errorf("coerced value still fails ValidateShape: %v", err)
			}
		})
	}
}

// The other half. A value with no single honest reading is NOT guessed at: the rule is
// quarantined and stopped. Guessing here would be the same class of defect as the silent
// match-everything this ticket exists to remove.
func TestCoercedValue_RefusesToGuess(t *testing.T) {
	tests := []struct {
		name string
		cond RuleCondition
	}{
		{"an object where a text scalar is required",
			RuleCondition{Field: FieldMerchant, Operator: "contains",
				Value: map[string]interface{}{"contains": "x"}}},
		{"an empty array for a text field names nothing to match",
			RuleCondition{Field: FieldMerchant, Operator: "in", Value: []interface{}{}}},
		{"a null value",
			RuleCondition{Field: FieldMerchant, Operator: "contains", Value: nil}},
		{"a boolean for a text field — no merchant is named true",
			RuleCondition{Field: FieldMerchant, Operator: "eq", Value: true}},
		{"a non-numeric string for a number field",
			RuleCondition{Field: FieldAmount, Operator: "gt", Value: "not-a-number"}},
		{"a currency-formatted string is ambiguous, not obvious",
			RuleCondition{Field: FieldAmount, Operator: "gt", Value: "$1,000"}},
		{"an array where a number is required",
			RuleCondition{Field: FieldAmount, Operator: "gt", Value: []interface{}{1, 2}}},
		{"a string where a numeric range object is required",
			RuleCondition{Field: FieldAmount, Operator: "between", Value: "not-an-object"}},
		{"a two-element array for a range — which end is which is a guess",
			RuleCondition{Field: FieldAmount, Operator: "between", Value: []interface{}{100, 500}}},
		{"a numeric range missing a bound",
			RuleCondition{Field: FieldAmount, Operator: "between",
				Value: map[string]interface{}{"min": 100.0}}},
		{"an inverted numeric range — swapping the bounds invents intent",
			RuleCondition{Field: FieldAmount, Operator: "between",
				Value: map[string]interface{}{"min": 500.0, "max": 100.0}}},
		{"a numeric range with an unreadable bound",
			RuleCondition{Field: FieldAmount, Operator: "between",
				Value: map[string]interface{}{"min": "cheap", "max": 500.0}}},
		{"a number for a date field",
			RuleCondition{Field: FieldDate, Operator: "eq", Value: 20260131}},
		{"an ambiguous slash date — 01/02 is January 2nd or February 1st",
			RuleCondition{Field: FieldDate, Operator: "eq", Value: "01/02/2026"}},
		{"a date range with an unreadable bound",
			RuleCondition{Field: FieldDate, Operator: "between",
				Value: map[string]interface{}{"start": "yesterday"}}},
		{"an inverted date range",
			RuleCondition{Field: FieldDate, Operator: "between",
				Value: map[string]interface{}{"start": "2026-02-01", "end": "2026-01-01"}}},
		{"an empty date range object",
			RuleCondition{Field: FieldDate, Operator: "between",
				Value: map[string]interface{}{}}},
		{"a value outside a closed enum is a typo, not a shape",
			RuleCondition{Field: FieldDirection, Operator: "eq", Value: "inflows"}},
		{"a number for a closed enum coerces to a string that is still not a member",
			RuleCondition{Field: FieldDirection, Operator: "eq", Value: 1}},
		{"an unknown PFC enum",
			RuleCondition{Field: FieldPFCPrimary, Operator: "eq", Value: "FOOD"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, ok := tt.cond.CoercedValue(); ok {
				t.Errorf("guessed %#v from %#v — an uncoercible value must stop the rule, not be invented",
					got, tt.cond.Value)
			}
		})
	}
}

// A value that ALREADY validates must come back unchanged in meaning. Coercion runs on every
// evaluation, so a "repair" that quietly rewrote healthy rules would be a far larger blast
// radius than the one it fixes.
func TestCoercedValue_LeavesValidValuesAlone(t *testing.T) {
	valid := []RuleCondition{
		{Field: FieldMerchant, Operator: "contains", Value: "starbucks"},
		{Field: FieldMerchant, Operator: "in", Value: []interface{}{"uber", "lyft"}},
		{Field: FieldAccountID, Operator: "in", Value: []interface{}{"acc-1", "acc-2"}},
		{Field: FieldAmount, Operator: "gt", Value: 50.0},
		{Field: FieldAmount, Operator: "between", Value: map[string]interface{}{"min": 1.0, "max": 2.0}},
		{Field: FieldDate, Operator: "between", Value: map[string]interface{}{"start": "2026-01-01"}},
		{Field: FieldDate, Operator: "eq", Value: "2026-01-31"},
		{Field: FieldDirection, Operator: "eq", Value: "outflow"},
	}

	for _, c := range valid {
		if err := c.ValidateShape(); err != nil {
			t.Fatalf("fixture %+v is not actually valid: %v", c, err)
		}
		got, ok := c.CoercedValue()
		if !ok {
			t.Errorf("a value the strict validator ACCEPTS was judged uncoercible: %+v", c)
			continue
		}
		repaired := c
		repaired.Value = got
		if err := repaired.ValidateShape(); err != nil {
			t.Errorf("coercion broke an already-valid value %+v: %v", c, err)
		}
	}
}

// CoercedValue must not touch the row it reads. definition_snapshot rows are immutable audit
// records and the live rule is shared across every transaction in a batch.
func TestCoercedValue_DoesNotMutateTheCondition(t *testing.T) {
	c := RuleCondition{Field: FieldAmount, Operator: "gt", Value: "1000"}
	if _, ok := c.CoercedValue(); !ok {
		t.Fatal(`"1000" must coerce`)
	}
	if c.Value != "1000" {
		t.Errorf("CoercedValue rewrote the stored value to %#v — coercion is read-path only", c.Value)
	}
}

// ⚠ THE ASYMMETRY, stated as a test. Coercion is an accommodation for rows that ALREADY exist.
// If the write path loosened too, the corpus of malformed rows would keep growing and the repair
// would become permanent instead of transitional.
func TestCoercibleValuesAreStillRejectedOnTheWritePath(t *testing.T) {
	coercible := []RuleCondition{
		{Field: FieldMerchant, Operator: "contains", Value: 42.0},
		{Field: FieldAmount, Operator: "gt", Value: "1000"},
		{Field: FieldAmount, Operator: "between", Value: map[string]interface{}{"min": "100", "max": "500"}},
		{Field: FieldDate, Operator: "eq", Value: "2026-01-31T00:00:00Z"},
	}

	for _, c := range coercible {
		if _, ok := c.CoercedValue(); !ok {
			t.Fatalf("fixture %+v is meant to be coercible", c)
		}
		if err := c.ValidateShape(); err == nil {
			t.Errorf("ValidateShape ACCEPTED %+v — the write path has loosened along with the "+
				"read path, so new malformed rows will keep being created", c)
		}

		rule := &CategorizationRule{
			Name: "r", ConditionLogic: "AND", Conditions: []RuleCondition{c},
			TargetKind: strPtrForTest("pfc"), TargetPFCPrimary: strPtrForTest("FOOD_AND_DRINK"),
		}
		if err := rule.Validate(); err == nil {
			t.Errorf("Validate ACCEPTED a rule whose condition value only survives read-path "+
				"coercion: %+v", c)
		}
	}
}

func strPtrForTest(s string) *string { return &s }
