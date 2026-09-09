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

// N1 (#1625, Codex adversarial round 5): the published Example must actually VALIDATE.
//
// Both PFC fields shipped `"example text"` — they use ShapeText, so exampleFor fell through to its
// default, and their EnumFn rejects anything outside the curated PFC registry. An agent copying the
// advertised example got a 422 from the endpoint that exists to stop agents guessing. The earlier
// test could not see this: it compared operator and shape STRINGS and never once exercised Example.
//
// This is deliberately written as a sweep over everything the schema publishes rather than a
// two-case check, so a field added later cannot reintroduce the same defect quietly.
func TestConditionSchema_EveryPublishedExampleValidates(t *testing.T) {
	for _, f := range ConditionSchema().Fields {
		for _, op := range f.Operators {
			cond := RuleCondition{
				Field:    RuleConditionField(f.Field),
				Operator: op.Operator,
				Value:    op.Example,
			}
			if err := cond.ValidateShape(); err != nil {
				t.Errorf("%s %s: the published example %#v does not validate: %v\n"+
					"a caller copying it verbatim gets a 422 from the endpoint meant to prevent that",
					f.Field, op.Operator, op.Example, err)
			}
		}
	}
}

// N3: a field with a closed set must publish it, or be an explicit, reasoned exemption.
//
// closedValuesFor is a hand-written switch keyed by field while FieldSpec.EnumFn is the actual
// declaration that a closed set exists. Nothing connected them, so a new field with an EnumFn
// published `nil` silently and an agent had to discover its legal values by 422.
//
// The PFC exemption is real and deliberate, not an oversight: those two sets are ~120 values that
// already reach clients through their own published schema, so inlining them would bloat every
// response with information the caller already has. Naming them here keeps that decision explicit
// — adding a field to this map is a choice someone has to make on purpose.
var closedSetExemptFields = map[RuleConditionField]string{
	FieldPFCPrimary:  "PFC primaries are published separately; ~120 values would bloat every response",
	FieldPFCDetailed: "PFC detaileds are published separately; ~120 values would bloat every response",
}

func TestConditionSchema_EveryClosedSetIsPublishedOrExempt(t *testing.T) {
	published := map[string]ConditionFieldSchema{}
	for _, f := range ConditionSchema().Fields {
		published[f.Field] = f
	}

	for field, spec := range FieldSpecs {
		if spec.EnumFn == nil {
			continue
		}
		if _, exempt := closedSetExemptFields[field]; exempt {
			continue
		}
		if len(published[string(field)].Values) == 0 {
			t.Errorf("field %q declares a closed set (EnumFn is set) but publishes no values, and "+
				"is not a documented exemption — an agent can only discover its legal values by 422",
				field)
		}
	}
}
