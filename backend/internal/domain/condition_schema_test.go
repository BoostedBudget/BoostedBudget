package domain

import (
	"errors"
	"testing"
)

// TestNormalize_EveryLegacyFusedTypeExpandsToATriple is the test that makes "no data migration"
// a claim rather than a hope: every one of the fourteen persisted fused names must expand to
// exactly one (field, operator) pair, deterministically.
func TestNormalize_EveryLegacyFusedTypeExpandsToATriple(t *testing.T) {
	want := map[ConditionType][2]string{
		ConditionMerchantContains:    {"merchant", "contains"},
		ConditionMerchantEquals:      {"merchant", "eq"},
		ConditionMerchantStartsWith:  {"merchant", "starts_with"},
		ConditionMerchantEndsWith:    {"merchant", "ends_with"},
		ConditionAmountGreaterThan:   {"amount", "gt"},
		ConditionAmountLessThan:      {"amount", "lt"},
		ConditionAmountEquals:        {"amount", "eq"},
		ConditionAmountRange:         {"amount", "between"},
		ConditionAccountID:           {"account_id", "eq"},
		ConditionAccountType:         {"account_type", "eq"},
		ConditionPaymentChannel:      {"payment_channel", "eq"},
		ConditionDateRange:           {"date", "between"},
		ConditionExistingPFCPrimary:  {"pfc_primary", "eq"},
		ConditionExistingPFCDetailed: {"pfc_detailed", "eq"},
	}

	if len(want) != len(validConditionTypes) {
		t.Fatalf("every valid fused type needs an expansion: %d expansions vs %d types",
			len(want), len(validConditionTypes))
	}

	for fused, pair := range want {
		t.Run(string(fused), func(t *testing.T) {
			c := RuleCondition{Type: fused, Value: "x"}
			if err := c.Normalize(); err != nil {
				t.Fatalf("Normalize() = %v", err)
			}
			if string(c.Field) != pair[0] || c.Operator != pair[1] {
				t.Errorf("got (%s, %s), want (%s, %s)", c.Field, c.Operator, pair[0], pair[1])
			}
			// Round-trip: the derived Type must be the one we started from.
			if c.Type != fused {
				t.Errorf("Type round-trip: got %q, want %q", c.Type, fused)
			}
		})
	}
}

// TestNormalize_IsIdempotent — the repository normalizes on EVERY read, so a second pass over an
// already-canonical condition must be a no-op rather than, say, re-deriving Type as "expression".
func TestNormalize_IsIdempotent(t *testing.T) {
	cases := []RuleCondition{
		{Type: ConditionMerchantContains, Value: "amazon"},
		{Field: FieldName, Operator: "contains", Value: "zelle"},
		{Field: FieldSignedAmount, Operator: "gt", Value: 1000.0},
		{Type: ConditionAmountRange, Value: map[string]interface{}{"min": 1.0, "max": 2.0}},
	}
	for _, base := range cases {
		c := base
		if err := c.Normalize(); err != nil {
			t.Fatalf("first Normalize(): %v", err)
		}
		first := c
		if err := c.Normalize(); err != nil {
			t.Fatalf("second Normalize(): %v", err)
		}
		if c.Field != first.Field || c.Operator != first.Operator || c.Type != first.Type {
			t.Errorf("not idempotent: %+v then %+v", first, c)
		}
	}
}

// TestNormalize_NewFieldsHaveNoFusedAncestor pins the reserved sentinel. `type` is a REQUIRED
// response field, so a triple with no fused equivalent still has to emit something.
func TestNormalize_NewFieldsHaveNoFusedAncestor(t *testing.T) {
	for _, f := range []RuleConditionField{FieldName, FieldMerchantID, FieldSignedAmount, FieldDirection} {
		c := RuleCondition{Field: f, Operator: "eq", Value: "x"}
		if err := c.Normalize(); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if c.Type != ConditionTypeExpression {
			t.Errorf("%s: got Type %q, want %q", f, c.Type, ConditionTypeExpression)
		}
	}
}

func TestNormalize_ConflictingTypeAndFieldIsRejected(t *testing.T) {
	c := RuleCondition{Type: ConditionAmountEquals, Field: FieldMerchant, Operator: "contains", Value: "x"}
	err := c.Normalize()
	if !errors.Is(err, ErrConditionShapeConflict) {
		t.Fatalf("want ErrConditionShapeConflict, got %v", err)
	}
}

// An operator persisted before #1625 is residue: the field was never validated and no evaluator
// ever read it, so the fused type is what actually drove matching. Normalize overwrites it with
// the operator that type implies — which preserves behaviour exactly and lets the next write
// repair the row, rather than carrying meaningless data forward forever.
func TestNormalize_OverwritesUnvalidatedLegacyOperator(t *testing.T) {
	c := RuleCondition{Type: ConditionMerchantContains, Operator: "gibberish", Value: "amazon"}
	if err := c.Normalize(); err != nil {
		t.Fatalf("Normalize() = %v", err)
	}
	if c.Operator != "contains" {
		t.Errorf("Operator = %q — the fused type must win over an unvalidated legacy operator", c.Operator)
	}
	// And the rule still matches what it always matched.
	if c.Field != FieldMerchant {
		t.Errorf("Field = %q, want merchant", c.Field)
	}
}

func TestNormalize_ExpressionIsNotAcceptedAsInput(t *testing.T) {
	c := RuleCondition{Type: ConditionTypeExpression, Value: "x"}
	if err := c.Normalize(); !errors.Is(err, ErrConditionShapeConflict) {
		t.Fatalf("want ErrConditionShapeConflict for a client-sent %q, got %v", ConditionTypeExpression, err)
	}
}

// TestFieldSpecs_CoverEveryConditionField guards the matrix against a field being added to the
// enum without a spec — which would make it unvalidatable and silently no-match at evaluation.
func TestFieldSpecs_CoverEveryConditionField(t *testing.T) {
	// Derived from allConditionFieldsOrdered rather than hand-copied. #1625 Task 11 found THREE
	// hand-maintained copies of this list (here, allConditionFieldsOrdered, and FieldSpecs), so
	// adding a field failed two guards for the same reason twice. One list to update now.
	all := allConditionFieldsOrdered()
	if len(all) != len(FieldSpecs) {
		t.Fatalf("FieldSpecs has %d entries, the enumerated list has %d — one of them was not updated",
			len(FieldSpecs), len(all))
	}
	for _, f := range all {
		spec, ok := FieldSpecs[f]
		if !ok {
			t.Errorf("field %q has no FieldSpec", f)
			continue
		}
		if len(spec.Ops) == 0 {
			t.Errorf("field %q supports no operators", f)
		}
	}
}

// TestValidateShape_AcceptsEveryShapeAlreadyInTheDatabase is the anti-quarantine guard. Arrays
// and partial date ranges are ALREADY persisted (migration 000072 wrote array account_id values;
// the pre-#1625 date_range evaluator accepts a single bound). A matrix that rejected them would
// disable working rules the moment enforcement is turned on.
func TestValidateShape_AcceptsEveryShapeAlreadyInTheDatabase(t *testing.T) {
	ok := []RuleCondition{
		{Type: ConditionMerchantContains, Value: "amazon"},
		{Type: ConditionMerchantContains, Value: []interface{}{"uber", "lyft"}},
		{Type: ConditionAccountID, Value: []interface{}{"acc-1", "acc-2"}},
		{Type: ConditionAmountRange, Value: map[string]interface{}{"min": 10.0, "max": 20.0}},
		{Type: ConditionDateRange, Value: map[string]interface{}{"start": "2026-01-01"}},
		{Type: ConditionDateRange, Value: map[string]interface{}{"end": "2026-12-31"}},
		{Type: ConditionDateRange, Value: map[string]interface{}{"start": "2026-01-01", "end": "2026-12-31"}},
		{Type: ConditionAmountGreaterThan, Value: 50.0},
		{Type: ConditionExistingPFCPrimary, Value: "FOOD_AND_DRINK"},
	}
	for _, c := range ok {
		cond := c
		if err := cond.Normalize(); err != nil {
			t.Fatalf("Normalize(%+v) = %v", c, err)
		}
		if err := cond.ValidateShape(); err != nil {
			t.Errorf("ValidateShape(%+v) rejected a shape already in the database: %v", c, err)
		}
	}
}

func TestValidateShape_RejectsGenuinelyBadValues(t *testing.T) {
	bad := []RuleCondition{
		{Type: ConditionAmountGreaterThan, Value: "not-a-number"},
		{Type: ConditionAmountRange, Value: "not-an-object"},
		{Type: ConditionAmountRange, Value: map[string]interface{}{"min": 10.0}}, // numeric range needs both
		{Type: ConditionAmountRange, Value: map[string]interface{}{"min": 20.0, "max": 10.0}},
		{Type: ConditionDateRange, Value: map[string]interface{}{}},
		{Type: ConditionDateRange, Value: map[string]interface{}{"start": "01/02/2026"}},
		{Type: ConditionMerchantContains, Value: 42},
		{Type: ConditionMerchantContains, Value: []interface{}{}},
		{Field: FieldDirection, Operator: "eq", Value: "sideways"},
	}
	for _, c := range bad {
		cond := c
		if err := cond.Normalize(); err != nil {
			continue // rejected even earlier, which is fine
		}
		if err := cond.ValidateShape(); err == nil {
			t.Errorf("ValidateShape(%+v) accepted an invalid value", c)
		} else if !errors.Is(err, ErrInvalidCondition) {
			t.Errorf("ValidateShape(%+v) error must wrap ErrInvalidCondition, got %v", c, err)
		}
	}
}

// Codex review: a persisted mixed array such as ["uber", 42] still MATCHES at evaluation time —
// matching.ExtractStringValues simply ignores the non-string member. Rejecting it at write time
// left such a rule matchable but permanently un-updatable: the user could not save an unrelated
// edit. That is the quarantine-a-working-rule failure this validator exists to avoid.
func TestValidateShape_ToleratesLegacyMixedArrays(t *testing.T) {
	c := RuleCondition{Type: ConditionMerchantContains, Value: []interface{}{"uber", 42}}
	if err := c.Normalize(); err != nil {
		t.Fatalf("Normalize() = %v", err)
	}
	if err := c.ValidateShape(); err != nil {
		t.Errorf("a mixed array that still matches at runtime must remain saveable: %v", err)
	}
}

// ...but an array with no usable string member can never match, so it is still rejected.
func TestValidateShape_RejectsAnArrayWithNoUsableString(t *testing.T) {
	c := RuleCondition{Type: ConditionMerchantContains, Value: []interface{}{42, true}}
	if err := c.Normalize(); err != nil {
		t.Fatalf("Normalize() = %v", err)
	}
	if err := c.ValidateShape(); err == nil {
		t.Error("an array with no string member can never match and must be rejected")
	}
}
