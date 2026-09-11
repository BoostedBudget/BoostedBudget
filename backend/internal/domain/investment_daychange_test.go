package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Issue #1631 M7: DayChangePct divided by the CURRENT value instead of the prior close, so every
// day-change percentage in the app was wrong — understating gains and overstating losses by
// exactly the day change.
//
// A day change is measured against where the position STARTED the day. Dividing by where it ended
// answers a different question, and the error grows with the size of the move.
func TestHolding_DayChangePct_IsMeasuredAgainstThePriorClose(t *testing.T) {
	tests := []struct {
		name     string
		quantity float64
		close    float64 // prior session close, per share
		current  float64 // institution_price, per share
		wantPct  float64
	}{
		{
			// The canonical case. Prior close 100 → now 105 is a +5% day.
			// Dividing by the current value gives 5/105 = 4.76%, which is the old behaviour.
			name: "a five percent gain reports five percent", quantity: 10,
			close: 100, current: 105, wantPct: 5,
		},
		{
			// Losses were overstated by the same mechanism: -5/95 = -5.26%.
			name: "a five percent loss reports five percent", quantity: 10,
			close: 100, current: 95, wantPct: -5,
		},
		{
			name: "a flat day reports zero", quantity: 10,
			close: 100, current: 100, wantPct: 0,
		},
		{
			// A large move is where the old denominator diverged most: 100/200 = 50% vs 100%.
			name: "a doubling reports one hundred percent", quantity: 3,
			close: 100, current: 200, wantPct: 100,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			currentValue := tt.quantity * tt.current
			h := Holding{
				Quantity:         tt.quantity,
				InstitutionPrice: &tt.current,
				InstitutionValue: &currentValue,
				Security:         &Security{ClosePrice: &tt.close},
			}
			pct := h.DayChangePct()
			require.NotNil(t, pct)
			assert.InDelta(t, tt.wantPct, *pct, 0.0001,
				"day change %% must be (current - prior) / PRIOR, not / current")
		})
	}
}

// TestHolding_DayChangePct_NilWhenPriorCloseCannotAnchorIt keeps the nil contract honest now that
// the denominator is the prior close rather than the current value.
func TestHolding_DayChangePct_NilWhenPriorCloseCannotAnchorIt(t *testing.T) {
	qty, price, value, zero := 10.0, 105.0, 1050.0, 0.0

	t.Run("no close price", func(t *testing.T) {
		h := Holding{Quantity: qty, InstitutionPrice: &price, InstitutionValue: &value, Security: &Security{}}
		assert.Nil(t, h.DayChangePct())
	})
	t.Run("no institution price", func(t *testing.T) {
		close := 100.0
		h := Holding{Quantity: qty, InstitutionValue: &value, Security: &Security{ClosePrice: &close}}
		assert.Nil(t, h.DayChangePct())
	})
	t.Run("a zero prior close cannot be a denominator", func(t *testing.T) {
		h := Holding{Quantity: qty, InstitutionPrice: &price, InstitutionValue: &value, Security: &Security{ClosePrice: &zero}}
		assert.Nil(t, h.DayChangePct(), "dividing by a zero prior value must yield nil, not Inf")
	})
	t.Run("a zero quantity cannot be a denominator", func(t *testing.T) {
		close := 100.0
		h := Holding{Quantity: 0, InstitutionPrice: &price, InstitutionValue: &value, Security: &Security{ClosePrice: &close}}
		assert.Nil(t, h.DayChangePct(), "a closed position has no day-change percentage")
	})
}
