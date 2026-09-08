package rules

import (
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These guards pin the behaviour of the PRE-#1625 fused condition types BEFORE the
// {field, operator, value} rework touches them. They are deliberately written against the
// public registry, not against internals, so they keep meaning after the refactor.
//
// The point is not that this behaviour is good — `amount` discarding the sign is exactly the
// gap #1625 exists to close. The point is that thousands of persisted rules depend on it, so
// the rework must ADD a signed field rather than change what `amount` means.

func txWithAmount(amount float64) *domain.Transaction {
	return &domain.Transaction{ID: "t1", Name: "ACME", Amount: amount}
}

// TestAmountField_StillUsesAbsoluteValue_LegacyGuard: a persisted `amount_greater_than: 50`
// matches an INFLOW of -75 today, because every amount evaluator calls math.Abs. If the rework
// ever makes `amount` signed, this fails — and it must, because that would silently change what
// every existing amount rule matches.
func TestAmountField_StillUsesAbsoluteValue_LegacyGuard(t *testing.T) {
	r := NewConditionRegistry()

	matched, err := r.Evaluate(txWithAmount(-75), domain.RuleCondition{
		Type:  domain.ConditionAmountGreaterThan,
		Value: 50.0,
	})

	require.NoError(t, err)
	assert.True(t, matched,
		"amount_greater_than must keep matching on |amount| — an inflow of -75 is > 50 in magnitude. "+
			"Direction belongs in the new signed_amount field, never in a redefinition of `amount`.")
}

// TestLegacyArrayAndRangeValuesStillValidate pins the value shapes already in the database.
// Migration 000072 wrote ARRAY account_id values; merchant conditions accept arrays via
// matching.ExtractStringValues; amount_range and date_range are OBJECTS with optional bounds.
// A FieldSpecs matrix that assumes scalar-only values would quarantine every one of them.
func TestLegacyArrayAndRangeValuesStillValidate(t *testing.T) {
	r := NewConditionRegistry()

	tests := []struct {
		name string
		tx   *domain.Transaction
		cond domain.RuleCondition
		want bool
	}{
		{
			name: "merchant_contains with an ARRAY value matches any member",
			tx:   &domain.Transaction{ID: "t", MerchantName: strptr("Lyft Ride")},
			cond: domain.RuleCondition{Type: domain.ConditionMerchantContains, Value: []interface{}{"uber", "lyft"}},
			want: true,
		},
		{
			name: "account_id with an ARRAY value (migration 000072 shape)",
			tx:   &domain.Transaction{ID: "t", AccountID: "acc-2"},
			cond: domain.RuleCondition{Type: domain.ConditionAccountID, Value: []interface{}{"acc-1", "acc-2"}},
			want: true,
		},
		{
			name: "amount_range object with min and max",
			tx:   txWithAmount(120),
			cond: domain.RuleCondition{Type: domain.ConditionAmountRange, Value: map[string]interface{}{"min": 100.0, "max": 200.0}},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.Evaluate(tt.tx, tt.cond)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func strptr(s string) *string { return &s }
