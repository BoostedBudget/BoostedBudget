package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func strptr(s string) *string { return &s }

// Issue #1631 M8a: DayChange subtracts securities.close_price from holdings.institution_price,
// and Plaid documents those two figures in DIFFERENT currencies for a foreign-currency security —
// close_price is "priced in USD" (model_security.go) while institution_price is "the last price
// given by the institution" (model_holding.go). The subtraction was therefore a unit error, and
// the result was rendered as money.
//
// The fix is to refuse the figure, not to convert it: there is NO FX rate source anywhere in this
// system, so a conversion would have to invent a rate. An absent day change is a gap in the UI; a
// converted one at a made-up rate is a wrong number the user cannot tell from a right one.
func TestHolding_DayChange_RefusesToSubtractTwoCurrencies(t *testing.T) {
	price := 105.0
	closePx := 100.0

	newHolding := func(holdingCcy, securityCcy *string) *Holding {
		return &Holding{
			Quantity:         10,
			InstitutionPrice: &price,
			CurrencyCode:     holdingCcy,
			Security: &Security{
				ClosePrice:   &closePx,
				CurrencyCode: securityCcy,
			},
		}
	}

	t.Run("known and differing currencies produce no day change", func(t *testing.T) {
		h := newHolding(strptr("GBP"), strptr("USD"))
		assert.Nil(t, h.DayChange(), "GBP position minus a USD close is not a quantity of money")
		assert.Nil(t, h.DayChangePct(), "the percentage is built on the same subtraction")
		assert.True(t, h.HasCurrencyMismatch())
	})

	t.Run("matching currencies still compute", func(t *testing.T) {
		h := newHolding(strptr("GBP"), strptr("GBP"))
		require.NotNil(t, h.DayChange())
		assert.InDelta(t, 50.0, *h.DayChange(), 1e-9)
		assert.False(t, h.HasCurrencyMismatch())
	})

	t.Run("case and padding differences are not a mismatch", func(t *testing.T) {
		// Provider vocabulary is not guaranteed to be normalised, and treating "usd" as a third
		// currency would suppress the figure for a holding that has no problem at all.
		h := newHolding(strptr("usd"), strptr(" USD "))
		require.NotNil(t, h.DayChange())
		assert.False(t, h.HasCurrencyMismatch())
	})

	t.Run("an unknown currency on either side is not a mismatch", func(t *testing.T) {
		// Every row ingested before this change has NULL on both sides. Treating unknown as a
		// distinct currency would blank the day change for the ENTIRE existing user base on
		// deploy day, with no underlying data change — a regression dressed as a safety check.
		for _, tc := range []struct {
			name             string
			holding, securit *string
		}{
			{"both unknown", nil, nil},
			{"holding unknown", nil, strptr("USD")},
			{"security unknown", strptr("USD"), nil},
			{"blank string is unknown", strptr(""), strptr("USD")},
		} {
			t.Run(tc.name, func(t *testing.T) {
				h := newHolding(tc.holding, tc.securit)
				require.NotNil(t, h.DayChange(), "unknown must not suppress the figure")
				assert.False(t, h.HasCurrencyMismatch())
			})
		}
	})
}

// The HoldingView is what actually reaches the client, so the refusal has to survive the mapping
// AND be distinguishable from "we have no close price": a client that cannot tell the two apart
// will report a currency defect as missing provider data.
func TestNewHoldingView_ReportsACurrencyMismatchRatherThanJustOmittingTheFigure(t *testing.T) {
	price := 105.0
	closePx := 100.0
	h := &Holding{
		Quantity:         10,
		InstitutionPrice: &price,
		CurrencyCode:     strptr("GBP"),
		Security: &Security{
			Name:         "Foreign Thing plc",
			ClosePrice:   &closePx,
			CurrencyCode: strptr("USD"),
		},
	}

	v := NewHoldingView(h)
	assert.Nil(t, v.DayChange)
	assert.Nil(t, v.DayChangePct)
	require.NotNil(t, v.CurrencyCode)
	assert.Equal(t, "GBP", *v.CurrencyCode, "the view reports the currency the POSITION is in")
	require.NotNil(t, v.DayChangeUnavailableReason)
	assert.Equal(t, DayChangeUnavailableCurrencyMismatch, *v.DayChangeUnavailableReason)

	// The staleness verdict is deliberately absent: it qualifies a figure, and there is no figure.
	assert.Nil(t, v.DayChangeIsStale)
}

// PortfolioSummary sums institution_value across holdings. With no FX source that sum is only
// meaningful when the holdings agree on a currency, so the summary has to SAY which case it is in.
func TestPortfolioSummaryCurrency_DetectsTheMixedCase(t *testing.T) {
	tests := []struct {
		name      string
		codes     []*string
		wantCcy   *string
		wantMixed bool
	}{
		{"no holdings", nil, nil, false},
		{"all unknown", []*string{nil, nil}, nil, false},
		{"one currency", []*string{strptr("USD"), strptr("USD")}, strptr("USD"), false},
		{
			// Unknown does not vote. It is the pre-#1631 state of every row in the table, so
			// letting it create a conflict would flag every existing portfolio as mixed.
			name:  "unknown alongside a known currency is not mixed",
			codes: []*string{nil, strptr("EUR"), strptr("")}, wantCcy: strptr("EUR"), wantMixed: false,
		},
		{
			name:  "two known currencies are mixed and name neither",
			codes: []*string{strptr("USD"), strptr("EUR")}, wantCcy: nil, wantMixed: true,
		},
		{
			name:  "normalisation happens before the comparison",
			codes: []*string{strptr("usd"), strptr(" USD")}, wantCcy: strptr("USD"), wantMixed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			holdings := make([]Holding, 0, len(tt.codes))
			for _, c := range tt.codes {
				holdings = append(holdings, Holding{CurrencyCode: c})
			}
			ccy, mixed := ResolveHoldingsCurrency(holdings)
			assert.Equal(t, tt.wantMixed, mixed)
			if tt.wantCcy == nil {
				assert.Nil(t, ccy)
			} else {
				require.NotNil(t, ccy)
				assert.Equal(t, *tt.wantCcy, *ccy)
			}
		})
	}
}
