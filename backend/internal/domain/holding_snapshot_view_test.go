package domain

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHoldingSnapshotView_ComputeGainLoss(t *testing.T) {
	tests := []struct {
		name        string
		value       *float64
		costBasis   *float64
		wantGain    *float64
		wantGainPct *float64
	}{
		{"gain", float64Ptr(5000), float64Ptr(4000), float64Ptr(1000), float64Ptr(25)},
		{"loss", float64Ptr(3000), float64Ptr(4000), float64Ptr(-1000), float64Ptr(-25)},
		{"no value at all is not a gain of zero", nil, float64Ptr(4000), nil, nil},
		{"no cost basis at all is not a gain of zero", float64Ptr(5000), nil, nil, nil},
		// A zero cost basis is a real thing (a granted position). The absolute gain is knowable;
		// the percentage is a division by zero and must stay nil rather than become +Inf, which
		// would render as "Infinity%" beside the user's money.
		{"zero cost basis yields an amount but no percentage", float64Ptr(5000), float64Ptr(0), float64Ptr(5000), nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := HoldingSnapshotView{InstitutionValue: tc.value, CostBasis: tc.costBasis}
			v.ComputeGainLoss()

			if tc.wantGain == nil {
				assert.Nil(t, v.GainLoss)
			} else {
				require.NotNil(t, v.GainLoss)
				assert.InDelta(t, *tc.wantGain, *v.GainLoss, 0.0001)
			}
			if tc.wantGainPct == nil {
				assert.Nil(t, v.GainLossPct)
			} else {
				require.NotNil(t, v.GainLossPct)
				assert.InDelta(t, *tc.wantGainPct, *v.GainLossPct, 0.0001)
			}
		})
	}
}

// The type-level half of the two-universes separation: HoldingSnapshotView must carry no
// day-change field. Day change is measured against securities.close_price — TODAY's prior close —
// and there must be nowhere on a historical row for that comparison to land.
//
// Asserted by reflection rather than by comment, because a comment does not fail.
func TestHoldingSnapshotView_HasNoDayChangeField(t *testing.T) {
	v := HoldingSnapshotView{}
	for _, forbidden := range []string{"DayChange", "DayChangePct", "ClosePrice"} {
		assert.False(t, hasField(v, forbidden),
			"HoldingSnapshotView must not carry %s: it would pair a historical position with today's close price", forbidden)
	}
}

func hasField(v any, name string) bool {
	_, ok := reflect.TypeOf(v).FieldByName(name)
	return ok
}
