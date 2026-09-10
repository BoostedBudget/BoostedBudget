package domain

import (
	"encoding/json"
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

// The wire shape of the no-history response (#1569 T7.8).
//
// Nothing serialized this response before, so `as_of_date` could document one contract
// (`null`), generate a second (`type: string`) and ship a third (the key absent) without a single
// test noticing. This marshals the REAL struct the handler returns and pins all three of the
// decisions the type makes about absence:
//
//	as_of_date      omitted when nil  — a *string CAN express null, and this one deliberately does not
//	available_dates always present    — `[]` is what tells the client there is no retained history
//	positions       always present    — never null, which a client cannot tell from a failed read
func TestHoldingSnapshotHistory_NoHistoryResponse_WireShape(t *testing.T) {
	// Exactly what holdingHistoryService returns on the bare no-history path.
	out := &HoldingSnapshotHistory{
		AccountID:      "acct-1",
		AvailableDates: []string{},
		Positions:      []HoldingSnapshotView{},
	}

	b, err := json.Marshal(out)
	require.NoError(t, err)

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(b, &raw))

	_, hasAsOf := raw["as_of_date"]
	assert.False(t, hasAsOf,
		"as_of_date is omitempty: it must be ABSENT, not null — the doc string says so and the generated schema types it as a plain string. Got: %s", b)
	// #1631 M8a added is_mixed_currency, deliberately WITHOUT omitempty: a client must be able to
	// read "these figures are not a currency-blind sum" as a positive statement, and an omitted
	// bool cannot be told from a field the server does not implement. currency_code stays
	// omitempty because nil there genuinely means "no answer", which is the same as absent.
	assert.JSONEq(t, `{"account_id":"acct-1","available_dates":[],"positions":[],"is_mixed_currency":false}`, string(b),
		"available_dates and positions must both be present and empty — `null` is indistinguishable from a failed read")

	// And the dated case still carries the day, so the assertion above is about absence rather
	// than about the field never being emitted at all.
	day := "2026-08-20"
	dated, err := json.Marshal(&HoldingSnapshotHistory{
		AccountID:      "acct-1",
		AsOfDate:       &day,
		AvailableDates: []string{day},
		Positions:      []HoldingSnapshotView{},
	})
	require.NoError(t, err)
	assert.Contains(t, string(dated), `"as_of_date":"2026-08-20"`)
	assert.NotContains(t, string(dated), `"total_value"`,
		"a day whose positions carry no value has an UNKNOWN total, and an omitted key is how that is said — never 0")
}
