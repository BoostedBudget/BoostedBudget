package rules

import (
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func evalCond(t *testing.T, tx *domain.Transaction, c domain.RuleCondition) bool {
	t.Helper()
	got, err := NewConditionRegistry().Evaluate(tx, c)
	require.NoError(t, err)
	return got
}

// The motivating case for #1625: a $1,000 Zelle transfer to one person. It has NO merchant_name,
// so no merchant condition can ever fire; the payee is only in tx.Name; and the amount must be
// distinguishable from an inbound Zelle of the same magnitude.
func TestName_MerchantID_Direction_SignedAmount_TheZelleCase(t *testing.T) {
	zelleOut := &domain.Transaction{
		ID:     "t1",
		Name:   "Zelle payment to Jack Liu 22371",
		Amount: 1000, // positive = outflow
		Date:   time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	}
	zelleIn := &domain.Transaction{
		ID:     "t2",
		Name:   "Zelle payment from Someone Else",
		Amount: -1000, // negative = inflow
		Date:   time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	}

	nameContainsJack := domain.RuleCondition{Field: domain.FieldName, Operator: "contains", Value: "jack liu"}
	assert.True(t, evalCond(t, zelleOut, nameContainsJack), "name must match the raw description")
	assert.False(t, evalCond(t, zelleIn, nameContainsJack))

	// merchant_name is nil on both, so the pre-#1625 vocabulary could not tell them apart at all.
	merchantContains := domain.RuleCondition{Type: domain.ConditionMerchantContains, Value: "jack liu"}
	assert.False(t, evalCond(t, zelleOut, merchantContains))

	outflow := domain.RuleCondition{Field: domain.FieldDirection, Operator: "eq", Value: "outflow"}
	assert.True(t, evalCond(t, zelleOut, outflow))
	assert.False(t, evalCond(t, zelleIn, outflow))

	signed := domain.RuleCondition{Field: domain.FieldSignedAmount, Operator: "eq", Value: 1000.0}
	assert.True(t, evalCond(t, zelleOut, signed), "signed_amount distinguishes an outflow from an inflow")
	assert.False(t, evalCond(t, zelleIn, signed))

	// ...whereas the frozen `amount` field cannot, which is exactly why it needed a sibling.
	abs := domain.RuleCondition{Type: domain.ConditionAmountEquals, Value: 1000.0}
	assert.True(t, evalCond(t, zelleOut, abs))
	assert.True(t, evalCond(t, zelleIn, abs))
}

func TestDirectionEvaluator_ZeroAmountMatchesNeither(t *testing.T) {
	tx := &domain.Transaction{ID: "t", Amount: 0}
	for _, dir := range []string{"inflow", "outflow"} {
		c := domain.RuleCondition{Field: domain.FieldDirection, Operator: "eq", Value: dir}
		assert.False(t, evalCond(t, tx, c), "a zero amount has no direction (%s)", dir)
	}
}

func TestMerchantIDEvaluator(t *testing.T) {
	id := "mch_abc123"
	tx := &domain.Transaction{ID: "t", MerchantID: &id}

	assert.True(t, evalCond(t, tx, domain.RuleCondition{Field: domain.FieldMerchantID, Operator: "eq", Value: id}))
	assert.True(t, evalCond(t, tx, domain.RuleCondition{
		Field: domain.FieldMerchantID, Operator: "in", Value: []interface{}{"other", id},
	}))
	// An opaque id is matched case-SENSITIVELY — unlike merchant NAME, which is text.
	assert.False(t, evalCond(t, tx, domain.RuleCondition{
		Field: domain.FieldMerchantID, Operator: "eq", Value: "MCH_ABC123",
	}))
	// A transaction with no merchant id matches nothing.
	assert.False(t, evalCond(t, &domain.Transaction{ID: "t"}, domain.RuleCondition{
		Field: domain.FieldMerchantID, Operator: "eq", Value: id,
	}))
}

func TestSignedAmountEvaluator_RespectsSign(t *testing.T) {
	inflow := &domain.Transaction{ID: "t", Amount: -500}

	// |−500| > 100 is true, but −500 > 100 is false. The two fields must disagree here, or
	// signed_amount is not actually signed.
	assert.True(t, evalCond(t, inflow, domain.RuleCondition{Type: domain.ConditionAmountGreaterThan, Value: 100.0}))
	assert.False(t, evalCond(t, inflow, domain.RuleCondition{
		Field: domain.FieldSignedAmount, Operator: "gt", Value: 100.0,
	}))
	assert.True(t, evalCond(t, inflow, domain.RuleCondition{
		Field: domain.FieldSignedAmount, Operator: "lt", Value: 0.0,
	}))
}

func TestNameEvaluator_Operators(t *testing.T) {
	tx := &domain.Transaction{ID: "t", Name: "CON EDISON BILL PAYMENT"}
	cases := []struct {
		op    string
		value interface{}
		want  bool
	}{
		{"contains", "edison", true},
		{"starts_with", "con ed", true},
		{"ends_with", "payment", true},
		{"eq", "con edison bill payment", true},
		{"eq", "con edison", false},
		{"in", []interface{}{"nope", "con edison bill payment"}, true},
		{"neq", "something else", true},
	}
	for _, tt := range cases {
		c := domain.RuleCondition{Field: domain.FieldName, Operator: tt.op, Value: tt.value}
		assert.Equal(t, tt.want, evalCond(t, tx, c), "name %s %v", tt.op, tt.value)
	}
}

func TestDateEvaluator_OpenEndedBounds(t *testing.T) {
	tx := &domain.Transaction{ID: "t", Date: time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)}

	assert.True(t, evalCond(t, tx, domain.RuleCondition{Field: domain.FieldDate, Operator: "gte", Value: "2026-01-01"}))
	assert.False(t, evalCond(t, tx, domain.RuleCondition{Field: domain.FieldDate, Operator: "gte", Value: "2026-07-01"}))
	assert.True(t, evalCond(t, tx, domain.RuleCondition{Field: domain.FieldDate, Operator: "lte", Value: "2026-12-31"}))
	// A single-bound range was already legal pre-#1625 and must stay so.
	assert.True(t, evalCond(t, tx, domain.RuleCondition{
		Type: domain.ConditionDateRange, Value: map[string]interface{}{"start": "2026-01-01"},
	}))
}

// TestUnknownFieldStillFailsQuiet is a regression guard for the sync path: EvaluateRules aborts
// a transaction's ENTIRE rule set on an evaluator error, so an unknown field must return
// (false, nil) rather than an error, or one junk condition disables a user's other rules.
func TestUnknownFieldStillFailsQuiet(t *testing.T) {
	got, err := NewConditionRegistry().Evaluate(
		&domain.Transaction{ID: "t", Amount: 10},
		domain.RuleCondition{Field: "not_a_field", Operator: "eq", Value: "x"},
	)
	require.NoError(t, err, "an unknown field must NOT error — it would abort every other rule")
	assert.False(t, got)
}

// TestRegistry_CoversExactlyTheFieldOperatorMatrix fails when FieldSpecs and the evaluator drift
// apart in EITHER direction: a declared pair the evaluator cannot handle, or an operator the
// evaluator honours that the validator would reject at write time.
func TestRegistry_CoversExactlyTheFieldOperatorMatrix(t *testing.T) {
	tx := &domain.Transaction{
		ID: "t", Name: "x", Amount: 1,
		Date: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	for field, spec := range domain.FieldSpecs {
		for op, shape := range spec.Ops {
			c := domain.RuleCondition{Field: field, Operator: string(op), Value: sampleValue(shape, field)}
			_, err := NewConditionRegistry().Evaluate(tx, c)
			assert.NoError(t, err,
				"FieldSpecs declares %s %s but the evaluator cannot evaluate it", field, op)
		}
	}
}

func sampleValue(shape domain.ValueShape, field domain.RuleConditionField) interface{} {
	switch shape {
	case domain.ShapeNumber:
		return 1.0
	case domain.ShapeNumberRange:
		return map[string]interface{}{"min": 0.0, "max": 2.0}
	case domain.ShapeDate:
		return "2026-01-01"
	case domain.ShapeDateRange:
		return map[string]interface{}{"start": "2026-01-01", "end": "2026-12-31"}
	default:
		if field == domain.FieldDirection {
			return "outflow"
		}
		return "x"
	}
}

// Codex review: `account_id neq X` used to return true for a transaction with NO account
// identity at all, because "not found among the values" is trivially true when there is nothing
// to compare. That is both surprising and inconsistent with every other matcher — matchText and
// matchExactCaseSensitive reject an empty subject outright.
func TestAccountIDEvaluator_NoIdentityMatchesNothingIncludingNeq(t *testing.T) {
	noIdentity := &domain.Transaction{ID: "t", AccountID: "", UserAccountID: nil}

	for _, op := range []string{"eq", "neq", "in"} {
		c := domain.RuleCondition{Field: domain.FieldAccountID, Operator: op, Value: "acc-1"}
		assert.False(t, evalCond(t, noIdentity, c),
			"a transaction with no account identity must not match account_id %s", op)
	}

	// A transaction that DOES have an identity still negates normally.
	withIdentity := &domain.Transaction{ID: "t", AccountID: "acc-2"}
	assert.True(t, evalCond(t, withIdentity, domain.RuleCondition{
		Field: domain.FieldAccountID, Operator: "neq", Value: "acc-1",
	}))
}
