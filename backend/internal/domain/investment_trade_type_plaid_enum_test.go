package domain

import (
	"testing"

	"github.com/plaid/plaid-go/v23/plaid"
)

// TestSubtypeSetsAreAllRealPlaidEnumMembers pins #1631's remaining ask from the M6 brief: every
// literal in every one of investment_trade_type.go's curated subtype sets must be a real member of
// Plaid's InvestmentTransactionSubtype enum. Before this test existed, "capital gain reinvestment"
// sat in reinvestSubtypes as a phantom — a string that reads plausibly but Plaid never sends
// (the real values are "long-term capital gain reinvestment" and "short-term capital gain
// reinvestment") — with nothing to catch it.
//
// This file lives in package domain (not internal/services, where the plaid import already
// exists elsewhere) so it can reach the unexported *Subtypes slices directly, without exporting
// internals purely for a test. Being a _test.go file, it does NOT make production
// internal/domain import plaid — only the test binary does; `go build ./...` (as opposed to
// `go test`) never compiles this file in.
func TestSubtypeSetsAreAllRealPlaidEnumMembers(t *testing.T) {
	sets := map[string][]string{
		"dividendSubtypes":        dividendSubtypes,
		"reinvestSubtypes":        reinvestSubtypes,
		"feeTaxSubtypes":          feeTaxSubtypes,
		"interestSubtypes":        interestSubtypes,
		"capitalGainSubtypes":     capitalGainSubtypes,
		"transferSubtypes":        transferSubtypes,
		"corporateActionSubtypes": corporateActionSubtypes,
	}

	for setName, literals := range sets {
		for _, literal := range literals {
			if !plaid.InvestmentTransactionSubtype(literal).IsValid() {
				t.Errorf("%s contains %q, which is not a member of Plaid's InvestmentTransactionSubtype enum (plaid-go/v23)", setName, literal)
			}
		}
	}
}
