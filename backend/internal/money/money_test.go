package money

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ToCents is the 2dp fold the money-representation ADR
// (docs/decisions/2026-09-11-money-representation-float64-and-the-rounding-boundary.md)
// prescribes at the API edge. These tests pin what it does AND what it deliberately does not.
func TestToCents_FoldsTheBinaryTail(t *testing.T) {
	// The shape the ADR describes reaching clients: a correct number with a long binary tail.
	require.NotEqual(t, 1234.56, 1234.5600000000002, "precondition: the dirty value is a distinct float64")
	assert.Equal(t, 1234.56, ToCents(1234.5600000000002))

	// Arrived at by arithmetic rather than by literal, which is how it actually happens.
	assert.Equal(t, 0.3, ToCents(0.1+0.2))
	assert.Equal(t, 25.0, ToCents(200.0/800.0*100))
}

func TestToCents_OrdinaryRounding(t *testing.T) {
	assert.Equal(t, 1.23, ToCents(1.234))
	assert.Equal(t, 1.24, ToCents(1.235))
	assert.Equal(t, 0.0, ToCents(0.0))
	assert.Equal(t, 333.33, ToCents(333.3333333))
	assert.Equal(t, -1.24, ToCents(-1.235), "away-from-zero, matching math.Round")
}

// ⚠ THE HONEST LIMITATION, pinned rather than implied. The ADR opens by rejecting overstatement,
// and a test asserting decimal exactness here would be exactly that. `1.005` is representable as
// slightly LESS than 1.005, so the fold goes DOWN. This repo already documents the same fact for
// the inbound canonicalisation in TestConfirmMatch_SubCent_IdempotencyRounding_GAPCG
// (internal/huma/operations/transfer_suggestion_test.go).
//
// The boundary makes output bounded and predictable in LENGTH. It does not make it decimal-exact.
func TestToCents_IsNotDecimalExact_TheADRsStatedLimitation(t *testing.T) {
	assert.Equal(t, 1.00, ToCents(1.005), "1.005 is stored below 1.005, so this rounds DOWN")

	// ⚠ The mechanism, spelled out — and it MUST go through a float64 variable to be visible.
	// Written as `math.Round(1.005*100)` the operands are UNTYPED CONSTANTS, which Go evaluates
	// at arbitrary precision at compile time: that expression is exactly 100.5 and yields 101,
	// the decimal-correct answer this function cannot give. (The ADR's prose asserts
	// "math.Round(1.005*100) is 100", which is true of the runtime float64 computation it means
	// and false of the Go expression as written. Corrected in the ADR by #1636.)
	x := 1.005
	assert.Equal(t, 100.49999999999999, x*100, "1.005 as a float64 is below 1.005")
	assert.Equal(t, 100.0, math.Round(x*100), "so the fold lands on 100, not 101")

	// ⚠ And it does NOT always go down either, which is the part that makes this unfixable by
	// picking a different tie-break rule. `1.015` is ALSO stored below its decimal value and also
	// rounds down (to 1.01), while `1.235` is stored ABOVE and rounds up. The direction at a .5
	// boundary is a property of each individual input's binary representation, not a rule.
	assert.Equal(t, 1.01, ToCents(1.015), "also stored below — also rounds down")
	assert.Equal(t, 1.24, ToCents(1.235), "stored above — rounds up")
}

func TestToCents_IsIdempotent(t *testing.T) {
	// Load-bearing for the guard in internal/huma/operations: it asserts a field is rounded by
	// checking ToCents(v) == v, which is only a valid test of "already rounded" if the fold is
	// idempotent.
	for _, v := range []float64{0, 1234.56, -1234.56, 0.01, 1.005, 1234.5600000000002} {
		assert.Equal(t, ToCents(v), ToCents(ToCents(v)), "input %v", v)
	}
}

// ============================================================================
// ToCentsPtr — nil-in/nil-out is LOAD-BEARING, not convenience.
// ============================================================================

// Decision 4 of the ADR: "Rounding never turns a withheld figure into a shown one, and never
// converts a nil to a 0.00". The difference between "we do not know" and "it is zero" is exactly
// what the FILTER (WHERE h.cost_basis IS NOT NULL) clauses and the currency refusals exist to
// preserve — a nil day change that came back as 0.00 would assert the portfolio did not move.
func TestToCentsPtr_NilStaysNil(t *testing.T) {
	assert.Nil(t, ToCentsPtr(nil), "a withheld figure must not become a shown 0.00")
}

func TestToCentsPtr_RoundsThroughThePointer(t *testing.T) {
	in := 1234.5600000000002
	got := ToCentsPtr(&in)
	require.NotNil(t, got)
	assert.Equal(t, 1234.56, *got)
}

// A genuine zero is NOT nil and must survive as a shown 0.00 — the other half of Decision 4.
func TestToCentsPtr_ZeroIsNotTreatedAsAbsent(t *testing.T) {
	in := 0.0
	got := ToCentsPtr(&in)
	require.NotNil(t, got, "a known zero is a figure, not an absence")
	assert.Equal(t, 0.0, *got)
}

// Rounding must not write through to the caller's float. The producing layer's value is the one
// that can feed further computation (ADR Decision 3, "why the handler and not the repository"),
// so the edge takes a copy rather than mutating it.
func TestToCentsPtr_DoesNotMutateTheInput(t *testing.T) {
	in := 1234.5600000000002
	got := ToCentsPtr(&in)
	require.NotNil(t, got)
	assert.Equal(t, 1234.5600000000002, in, "the producing layer's value is untouched")
	assert.NotSame(t, &in, got, "a fresh pointer, not the caller's")
}
