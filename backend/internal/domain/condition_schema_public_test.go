package domain

import "testing"

// The condition MATRIX, not just the vocabulary.
//
// Two independent reviews landed on this as the single most important gap in #1625: the spec index
// gives an agent one flat field enum, one flat operator enum, and an untyped `value`. From that it
// cannot establish that name+contains is legal but name+gt is not, what shape `between` needs,
// which values `direction` takes, or whether an outgoing $1,000 is 1000 or -1000. A wrong sign
// VALIDATES SILENTLY and the rule simply never matches — the exact failure class #1625 opened with.
func TestConditionSchema_DescribesEveryFieldInFieldSpecs(t *testing.T) {
	schema := ConditionSchema()

	if len(schema.Fields) != len(FieldSpecs) {
		t.Fatalf("schema describes %d fields, FieldSpecs has %d — it must be DERIVED from the "+
			"matrix the validator actually enforces, not a hand-written copy",
			len(schema.Fields), len(FieldSpecs))
	}

	for _, f := range schema.Fields {
		spec, ok := FieldSpecs[RuleConditionField(f.Field)]
		if !ok {
			t.Errorf("schema describes %q, which FieldSpecs does not have", f.Field)
			continue
		}
		if len(f.Operators) != len(spec.Ops) {
			t.Errorf("%s: schema lists %d operators, FieldSpecs allows %d",
				f.Field, len(f.Operators), len(spec.Ops))
		}
		for _, op := range f.Operators {
			shape, allowed := spec.Ops[RuleConditionOperator(op.Operator)]
			if !allowed {
				t.Errorf("%s: schema offers operator %q the validator would reject", f.Field, op.Operator)
				continue
			}
			if op.ValueShape != string(shape) {
				t.Errorf("%s %s: schema says shape %q, validator requires %q",
					f.Field, op.Operator, op.ValueShape, shape)
			}
		}
	}
}

// The semantics an agent cannot guess and that validate silently when guessed wrong.
func TestConditionSchema_ExposesTheSemanticsThatFailSilently(t *testing.T) {
	byField := map[string]ConditionFieldSchema{}
	for _, f := range ConditionSchema().Fields {
		byField[f.Field] = f
	}

	direction, ok := byField["direction"]
	if !ok {
		t.Fatal("direction is missing from the schema")
	}
	if len(direction.Values) != 2 {
		t.Errorf("direction must publish its closed value set, got %v", direction.Values)
	}
	for _, want := range []string{DirectionInflow, DirectionOutflow} {
		found := false
		for _, v := range direction.Values {
			if v == want {
				found = true
			}
		}
		if !found {
			t.Errorf("direction does not publish %q — an agent would have to discover it by 422", want)
		}
	}

	signed, ok := byField["signed_amount"]
	if !ok {
		t.Fatal("signed_amount is missing from the schema")
	}
	if signed.Notes == "" {
		t.Error("signed_amount must state its sign convention: a wrong sign validates silently " +
			"and the rule just never matches, which is unguessable and undebuggable")
	}
}
