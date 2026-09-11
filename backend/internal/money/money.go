// Package money holds the one 2dp rounding fold this codebase applies to money and percentage
// figures on their way out of the API.
//
// It exists because of a decision, not a convenience:
// docs/decisions/2026-09-11-money-representation-float64-and-the-rounding-boundary.md.
// That ADR records that `float64` IS the money type in Go (Decision 1), that aggregation happens
// in Postgres' exact NUMERIC (Decision 2), and that the *representation* artefact this leaves —
// a correct number carrying a long binary tail, `1234.5600000000002` in shape — is folded at the
// API edge and nowhere else (Decision 3). #1636 is the implementation of that boundary.
//
// ⚠ WHY THIS IS ITS OWN PACKAGE rather than an unexported helper. Two separate handler packages'
// worth of surfaces need the identical fold — the nine investment/holdings-history operations and
// the savings progress summary, whose previous service-layer `roundToCents` this replaces — and
// the ADR's argument against a per-layer copy is that "a convention with a named exception drifts
// into two conventions". One exported fold, imported by the edge, is the whole point.
//
// ⚠ WHAT DOES NOT BELONG HERE. Quantity and per-unit price are NOT money for this purpose.
// `DECIMAL(18,8)` columns (holdings.quantity, holdings.institution_price,
// investment_transactions.quantity/price, holding_snapshots.quantity/institution_price,
// securities.close_price) exist because fractional shares and crypto need those digits; folding
// one to cents destroys real data rather than cosmetic noise. If a quantity ever needs
// presentational rounding it is 8dp, and per the ADR that is a separate decision.
package money

import "math"

// ToCents folds a figure to 2 decimal places.
//
// ⚠ It is NOT decimal-exact and does not claim to be. `math.Round(x*100)/100` is itself binary
// arithmetic: `1.005` is representable only as slightly less than 1.005, so this returns `1.00`
// rather than `1.01`, and `1.015` likewise returns `1.01`, while `1.235` — stored slightly above
// — returns `1.24`. The direction at a .5 boundary is a property of the individual input's
// representation, not a tie-break rule that could be changed. What this function guarantees is
// that output is BOUNDED AND PREDICTABLE IN LENGTH, which is the artefact the ADR set out to
// remove. Pinned by TestToCents_IsNotDecimalExact_TheADRsStatedLimitation, and by the
// pre-existing TestConfirmMatch_SubCent_IdempotencyRounding_GAPCG which documents the same fact
// for the inbound canonicalisation in transfer_suggestion.go.
func ToCents(v float64) float64 {
	return math.Round(v*100) / 100
}

// ToCentsPtr folds an optional figure to 2 decimal places, returning nil unchanged.
//
// ⚠ NIL-IN/NIL-OUT IS LOAD-BEARING, not ergonomics. ADR Decision 4: "Rounding never turns a
// withheld figure into a shown one, and never converts a nil to a 0.00." Across this surface a
// nil means "we do not know" or "we refuse to compute this" — a holding with no prior close, a
// day change refused because two operands are in different currencies, a percentage whose
// denominator is zero, an aggregate whose `FILTER (WHERE h.cost_basis IS NOT NULL)` excluded
// every row. A 0.00 in any of those places is an assertion about the user's money that nothing
// in this system has grounds to make. Rounding decides HOW MANY DIGITS an emittable figure
// carries; it never decides WHETHER a figure may be emitted.
//
// It returns a fresh pointer rather than writing through the caller's, so the producing layer's
// value — the one the ADR keeps unrounded because it can feed further computation — is untouched.
func ToCentsPtr(v *float64) *float64 {
	if v == nil {
		return nil
	}
	r := ToCents(*v)
	return &r
}
