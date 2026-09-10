package domain

// Trade-vs-position reconciliation (#1631 M8b).
//
// ─────────────────────────────────────────────────────────────────────────────────────────────
// WHAT THIS IS, AND WHAT IT IS NOT. `holdings` is a pure Plaid snapshot — DELETE+INSERT replaced
// per account on every sync (holding_repository.go ReplaceForAccount) — and NOTHING in this
// backend aggregates investment_transactions.quantity to check it against that snapshot. This
// file adds that check as a DIAGNOSTIC: it reports a drift between "what the trade ledger implies
// the position should be" and "what Plaid's snapshot says the position actually is". It NEVER
// writes anything. The position of record stays holdings, unconditionally — nothing here may be
// used to correct a holding, and nothing here is wired to do so.
//
// THE ARITHMETIC. A buy is a positive quantity, a sell is a NEGATIVE quantity (Plaid's own
// convention), and a reinvestment (type='cash', a real Plaid subtype like "dividend
// reinvestment", or this brokerage's structural cash/withdrawal-against-a-security pattern — see
// domain.NormalizeTradeType) carries a POSITIVE quantity for the shares it purchased. So
// SUM(quantity) over exactly the rows domain.NormalizeTradeType would label buy/sell/reinvest is
// a valid net share delta — domain.TradeTypeToSQLPredicate already defines that partition and is
// reused verbatim rather than re-derived, for the same reason IncomeSummary and the transaction
// feed share it: two independent copies of "which rows count" is how #1631 happened in the first
// place.
//
// THE 2-YEAR LOOKBACK DECISION, WRITTEN DOWN. Plaid's investments endpoints only return two years
// of investment_transactions. A position opened before that window has real shares the ledger
// simply never received, so Σ(buy+sell+reinvest quantity) for that security will fall short of
// the holding's actual quantity through NO fault of the sync — the ledger is complete FOR THE
// WINDOW, not complete for the position's whole life. A diagnostic that flagged every such
// position would flag most long-held holdings in the product, which is noise, not signal, and
// would train an operator to ignore the report entirely.
//
// The chosen proxy for "the ledger can plausibly speak to this position's origin" is: DOES THE
// LEDGER CONTAIN AT LEAST ONE type='buy' ROW FOR THIS (account, security) PAIR? A position with
// zero buys in the ledger — only sells, only reinvestments, or no rows at all — could not have
// been opened inside the visible window (you cannot sell or reinvest into a position you never
// bought), so its whole opening trade predates the lookback and a quantity mismatch there is
// exactly what the window's edge predicts, not evidence of anything wrong. Those pairs are
// reported separately, as SuppressedPreWindowCount, rather than folded into Drifts — the report
// stays honest about how many positions it could not evaluate instead of silently going quiet
// about them.
//
// This is a proxy, not a proof: a position can have a qualifying buy AND still predate the window
// (an old position topped up with a recent purchase), which would still surface as a Drift. That
// residual false-positive rate is accepted deliberately — the alternative (also requiring the
// ledger's earliest row to precede the account's overall earliest row by some margin) trades one
// unproven heuristic for a second, no more defensible one, on a feature that is diagnostic-only
// and has no write path to protect against overreacting to.
//
// WHY QUANTITY, NOT MONEY, AND WHY THE CURRENCY HELPERS DO NOT APPLY. domain.ResolveCurrencyCodes/
// CurrenciesConflict exist because a MONEY total summed across mixed currencies is meaningless
// without an FX rate this system does not have (see investment_currency.go). A share quantity has
// no currency at all — "42.93 shares" means the same thing regardless of what currency the
// brokerage prices them in — so there is no unit-mismatch hazard here to declare or refuse. This
// diagnostic sums quantity only and deliberately does not touch price, amount, or cost basis.
// ─────────────────────────────────────────────────────────────────────────────────────────────

// positionReconciliationQuantityTolerance bounds how far a ledger-implied position may differ
// from the Plaid snapshot before it counts as a drift, rather than float64 accumulation noise
// from summing many fractional-share rows. Three decimal places — thousandths of a share — is
// finer than any brokerage's minimum tradable increment for a fractional share in production data,
// so tightening it further would only make the report noisier without catching anything real.
const positionReconciliationQuantityTolerance = 0.001

// PositionReconciliationMember is one (account, security) position the diagnostic evaluated.
type PositionReconciliationMember struct {
	AccountID       string  `json:"account_id"`
	SecurityID      string  `json:"security_id"`
	PlaidSecurityID string  `json:"plaid_security_id"`
	TickerSymbol    *string `json:"ticker_symbol,omitempty"`
	SecurityName    string  `json:"security_name"`
	HoldingQuantity float64 `json:"holding_quantity"`
	LedgerQuantity  float64 `json:"ledger_quantity"`
	Delta           float64 `json:"delta"`
}

// PositionReconciliationReport is GetPositionReconciliation's result: the drifts worth an
// operator's attention, plus a count of positions the 2-year lookback made unevaluable (see the
// file-level comment for exactly what that means and why it is a separate bucket rather than
// silence).
type PositionReconciliationReport struct {
	// Drifts are (account, security) pairs where the ledger contains at least one buy for the
	// pair (the "plausibly-in-window" proxy) AND the ledger-implied quantity disagrees with the
	// Plaid snapshot by more than positionReconciliationQuantityTolerance.
	Drifts []PositionReconciliationMember `json:"drifts"`
	// SuppressedPreWindowCount is the number of positions with a non-zero delta that were NOT
	// added to Drifts because the ledger held no buy for the pair — the signature of a position
	// opened before Plaid's 2-year lookback. Reported as a count, not a list: these are expected,
	// not actionable, and the report exists to make that volume visible rather than to itemize it.
	SuppressedPreWindowCount int `json:"suppressed_pre_window_count"`
	// TotalPositionsChecked is every (account, security) pair with a non-zero holding quantity
	// that this report evaluated, drifting or not — the denominator for the two counts above.
	TotalPositionsChecked int `json:"total_positions_checked"`
}

// ClassifyPositionReconciliationMember reports whether a candidate (holding vs. ledger) pair
// should be added to a report as a Drift, counted as SuppressedPreWindowCount, or dropped as a
// clean reconciliation (delta within tolerance). hasLedgerBuy is the file-level "plausibly
// in-window" proxy: whether the ledger contains at least one type='buy' row for this
// (account, security) pair.
//
// Extracted as a pure function (no DB access) so the suppression rule — the part of this
// diagnostic most likely to need tuning — has a unit test that does not require Postgres.
func ClassifyPositionReconciliationMember(delta float64, hasLedgerBuy bool) (isDrift, isSuppressed bool) {
	if delta > positionReconciliationQuantityTolerance || delta < -positionReconciliationQuantityTolerance {
		if hasLedgerBuy {
			return true, false
		}
		return false, true
	}
	return false, false
}
