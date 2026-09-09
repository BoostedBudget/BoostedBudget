package services

import (
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/stretchr/testify/assert"
)

// quarantineReason is the shadow-mode classifier. These tests pin WHAT it flags — the counts it
// produces are what a future enforcement pass will act on, so a wrong classification here would
// either disable a working rule or leave a broken one invisible.
func TestQuarantineReason_FlagsTheStructurallyBrokenRules(t *testing.T) {
	valid := func() *domain.CategorizationRule {
		return &domain.CategorizationRule{
			ID:             "r1",
			ConditionLogic: "AND",
			Conditions: []domain.RuleCondition{
				{Type: domain.ConditionMerchantContains, Value: "amazon"},
			},
		}
	}

	tests := []struct {
		name       string
		mutate     func(*domain.CategorizationRule)
		wantReason observability.QuarantineReason
	}{
		{"a valid rule is not flagged", func(*domain.CategorizationRule) {}, ""},
		{
			// engine.go returns true for zero conditions, so such a rule matches EVERY
			// transaction. Create-time validation rejects it, so it can only exist on a
			// persisted row — which is exactly where it would recategorise a whole ledger.
			"no conditions matches everything",
			func(r *domain.CategorizationRule) { r.Conditions = nil },
			observability.QuarantineNoConditions,
		},
		{
			"invalid condition_logic silently defaults to AND",
			func(r *domain.CategorizationRule) { r.ConditionLogic = "XOR" },
			observability.QuarantineInvalidLogic,
		},
		{
			"unknown condition type cannot be normalized",
			func(r *domain.CategorizationRule) {
				r.Conditions = []domain.RuleCondition{{Type: "amount", Value: 1000}}
			},
			observability.QuarantineUnnormalizable,
		},
		{
			"a value shape the evaluator can never use",
			func(r *domain.CategorizationRule) {
				r.Conditions = []domain.RuleCondition{
					{Type: domain.ConditionAmountRange, Value: "not-an-object"},
				}
			},
			observability.QuarantineInvalidValue,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := valid()
			tt.mutate(r)
			assert.Equal(t, tt.wantReason, quarantineReason(r))
		})
	}
}

// TestQuarantineReason_AcceptsLegacyShapesAlreadyInTheDatabase is the anti-false-positive half.
// Arrays and single-bound date ranges are persisted TODAY. Flagging them would mean enforcement
// later disables rules that work.
func TestQuarantineReason_AcceptsLegacyShapesAlreadyInTheDatabase(t *testing.T) {
	legacy := []domain.RuleCondition{
		{Type: domain.ConditionMerchantContains, Value: []interface{}{"uber", "lyft"}},
		{Type: domain.ConditionAccountID, Value: []interface{}{"acc-1", "acc-2"}},
		{Type: domain.ConditionDateRange, Value: map[string]interface{}{"start": "2026-01-01"}},
		{Type: domain.ConditionAmountRange, Value: map[string]interface{}{"min": 1.0, "max": 2.0}},
		// An operator persisted back when the field was never validated must not condemn a rule.
		{Type: domain.ConditionMerchantContains, Operator: "gibberish", Value: "amazon"},
	}

	for _, c := range legacy {
		r := &domain.CategorizationRule{ID: "r", ConditionLogic: "OR", Conditions: []domain.RuleCondition{c}}
		assert.Empty(t, quarantineReason(r), "legacy shape %+v must not be flagged", c)
	}
}

// TestQuarantineReason_DoesNotMutateTheRule — shadow mode inspects; it must not normalize the
// live rule in place, or it would silently change what the very next evaluation matches.
func TestQuarantineReason_DoesNotMutateTheRule(t *testing.T) {
	r := &domain.CategorizationRule{
		ID:             "r",
		ConditionLogic: "AND",
		Conditions:     []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "amazon"}},
	}

	_ = quarantineReason(r)

	assert.Empty(t, string(r.Conditions[0].Field), "quarantineReason must not normalize the live rule")
	assert.Empty(t, r.Conditions[0].Operator)
}
