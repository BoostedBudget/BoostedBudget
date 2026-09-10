package domain

import "errors"

// The READ side of holding_snapshots (Issue #1569).
//
// holding_snapshots was created by this branch and, until this file, was written and never read —
// the same failure shape as the deleted-goal ledger, one table over. Privacy policy §8.2a tells
// the user that a removed account's "holdings history is retained ... so you can still view it
// under 'Closed accounts'". These types are what makes that sentence true.
//
// ⚠ TWO UNIVERSES, SEPARATED BY THE TYPE SYSTEM. HoldingSnapshotView is deliberately NOT
// HoldingView, and the difference is load-bearing rather than cosmetic: it carries no DayChange /
// DayChangePct. Those are computed as quantity x (institution_price - securities.close_price),
// and close_price is TODAY's prior close. Pairing a position that was true on 2025-04-02 with
// today's close would state a historical movement as a current one — the exact divergence #1580
// and #1591 were both about. There is no field here for that mistake to be written into.
//
// Following #1591's precedent, the refusal is MECHANICAL (a distinct type with no such field,
// which flows through to a distinct generated TypeScript model) rather than a comment asking the
// next reader not to. Nothing that answers "what does this account hold NOW" may read these
// types, and nothing here may enter balanceMatchesRange / compareBalanceValues.

// HoldingSnapshotView is one retained investment position as it stood on one past UTC day,
// joined with the security metadata needed to name it. Every figure on it is as-of AsOfDate.
type HoldingSnapshotView struct {
	SecurityID string `json:"security_id"`
	// AsOfDate is a UTC calendar day, YYYY-MM-DD. Deliberately a plain date string rather than a
	// timestamp: a date-only value rendered through a timestamp shifts back a day for every user
	// west of UTC, and this figure's whole meaning is which day it was true on.
	AsOfDate string `json:"as_of_date" doc:"UTC calendar day (YYYY-MM-DD) these figures were true"`

	Quantity         float64  `json:"quantity"`
	CostBasis        *float64 `json:"cost_basis,omitempty"`
	InstitutionValue *float64 `json:"institution_value,omitempty"`
	InstitutionPrice *float64 `json:"institution_price,omitempty"`
	// CurrencyCode is the currency the figures above were reported in, as at AsOfDate (#1631
	// M8a), or nil when the provider never told us. As-of like everything else on this type: a
	// past position is stated in the currency it was reported in at the time, not in whatever
	// currency the live holding reports now.
	CurrencyCode *string `json:"currency_code,omitempty"`

	// Security metadata (LEFT JOIN securities — a security row can be absent).
	TickerSymbol *string `json:"ticker_symbol,omitempty"`
	SecurityName *string `json:"security_name,omitempty"`
	SecurityType *string `json:"security_type,omitempty"`

	// Computed from THIS row's own historical value and cost basis. Both inputs were true on the
	// same day, so unlike day-change this one carries no cross-universe comparison.
	GainLoss    *float64 `json:"gain_loss,omitempty"`
	GainLossPct *float64 `json:"gain_loss_pct,omitempty"`
}

// HoldingSnapshotHistory is one account's retained holdings history: the day being shown, the
// positions on it, and every other day that can be asked for.
type HoldingSnapshotHistory struct {
	AccountID string `json:"account_id"`
	// AsOfDate is the day Positions describes. Echoed back so a client that asked for "the latest"
	// learns which day that was.
	//
	// ⚠ OMITTED — NOT null — when the account has no retained history at all (#1569 T7.8). The
	// doc string used to promise `null` while `omitempty` made the key vanish, and the generated
	// schema said plain `type: string`, so the promise was one no response could keep. The
	// mismatch is resolved towards what SHIPS rather than towards the promise, deliberately:
	// dropping omitempty would make huma mark the field required-and-nullable, a breaking change
	// to every generated client model, to convey a fact AvailableDates already carries
	// unambiguously (it is never nil, so `available_dates: []` IS "no retained history"). A key
	// that is present-and-null and a key that is absent are the same `undefined` to the one
	// consumer that reads it.
	AsOfDate *string `json:"as_of_date,omitempty" doc:"UTC calendar day (YYYY-MM-DD) the positions describe; omitted when no history is retained for this account"`
	// AvailableDates lists every UTC day with at least one retained position, newest first.
	AvailableDates []string              `json:"available_dates" doc:"Every UTC day (YYYY-MM-DD) with retained positions, newest first"`
	Positions      []HoldingSnapshotView `json:"positions"`
	// TotalValue is the sum of the day's institution_value, pre-computed so no consumer has to do
	// the arithmetic. Nil when no position on the day carries a value.
	TotalValue *float64 `json:"total_value,omitempty"`

	// CurrencyCode is the single currency TotalValue is expressed in, or nil when one cannot be
	// stated; IsMixedCurrency says which of the two reasons applies — true means the day's
	// positions were reported in two or more DIFFERENT currencies, so TotalValue is a
	// currency-blind sum (there is no FX rate source in this system), false means no position
	// stated a currency at all. #1631 M8a.
	CurrencyCode    *string `json:"currency_code,omitempty"`
	IsMixedCurrency bool    `json:"is_mixed_currency"`
}

// ComputeGainLoss fills GainLoss / GainLossPct from the row's own historical figures, using the
// same rules as Holding.GainLoss / Holding.GainLossPct: nil when either input is missing, and a
// nil percentage when cost basis is zero (rather than a division by zero).
func (v *HoldingSnapshotView) ComputeGainLoss() {
	if v.InstitutionValue == nil || v.CostBasis == nil {
		return
	}
	gl := *v.InstitutionValue - *v.CostBasis
	v.GainLoss = &gl
	if *v.CostBasis == 0 {
		return
	}
	pct := gl / *v.CostBasis * 100
	v.GainLossPct = &pct
}

// ErrHoldingHistoryDateNotRetained is returned when a caller asks for a UTC day on which this
// account has no retained positions.
//
// A MECHANICAL REFUSAL, following #1591's ErrAccountFilterLastKnownAsOfConflict rather than a
// comment. The alternative — returning an empty position list for an unretained day — would make
// "we kept nothing for that day" indistinguishable from "the account held nothing that day", and
// the second is a claim about the user's money that this surface has no basis to make. The whole
// point of the endpoint is to stop the product asserting things about retained history that are
// not so.
var ErrHoldingHistoryDateNotRetained = errors.New("no retained holdings history for that date")
