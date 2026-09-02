package domain

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The type-level half of the two-universes separation (Issue #1569).
//
// Every balance on a historical point must be inseparable from the day it was true on. A JSON tag
// carrying `omitempty` on Date would let a point serialise WITHOUT its day whenever the day were
// empty, and an undated balance beside a closed account is indistinguishable from a current one —
// which is the exact defect #1591 deleted a caption for.
//
// Asserted by reflection and by an actual marshal rather than by comment, because a comment does
// not fail.
func TestAccountBalanceHistoryPoint_DateIsNeverOmitted(t *testing.T) {
	f, ok := reflect.TypeOf(AccountBalanceHistoryPoint{}).FieldByName("Date")
	require.True(t, ok, "AccountBalanceHistoryPoint must carry a Date")
	assert.Equal(t, "date", f.Tag.Get("json"),
		"Date must serialise unconditionally: an omitempty/omitzero here emits an undated balance, "+
			"which reads as a current one")

	b, err := json.Marshal(AccountBalanceHistoryPoint{})
	require.NoError(t, err)
	assert.Contains(t, string(b), `"date"`, "the zero point must still carry its date key")
}

// A historical point must have nowhere for a CURRENT-STATE figure to land. `accounts` carries the
// live pair and accountBalanceProjection nulls it for a terminal account; if this type grew an
// IsCurrent / AsOfNow-shaped field, the frontend could label a retained figure as today's.
func TestAccountBalanceHistoryPoint_CarriesNoCurrentStateField(t *testing.T) {
	v := AccountBalanceHistoryPoint{}
	for _, forbidden := range []string{"IsCurrent", "IsLatest", "Change", "ChangePct"} {
		assert.False(t, hasField(v, forbidden),
			"AccountBalanceHistoryPoint must not carry %s: a retained day is not a comparison against today", forbidden)
	}
}

// Points must marshal as [] rather than null when nothing was retained: "we kept nothing" and
// "the read failed" have to be distinguishable by a caller that only looks at the body.
func TestAccountBalanceHistory_EmptyPointsMarshalAsArray(t *testing.T) {
	b, err := json.Marshal(AccountBalanceHistory{AccountID: "a", Points: []AccountBalanceHistoryPoint{}})
	require.NoError(t, err)
	assert.Contains(t, string(b), `"points":[]`)
	assert.NotContains(t, string(b), `"first_date"`, "an empty series bounds nothing")
}
