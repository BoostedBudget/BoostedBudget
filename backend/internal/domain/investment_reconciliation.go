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
// SUM(quantity) over the rows domain.NormalizeTradeType would label buy/sell/reinvest is A START
// on a net share delta — domain.TradeTypeToSQLPredicate already defines that partition and is
// reused verbatim rather than re-derived, for the same reason IncomeSummary and the transaction
// feed share it: two independent copies of "which rows count" is how #1631 happened in the first
// place. It is not the WHOLE delta: type='transfer' rows also modify a position — Plaid's own
// docs call type='transfer' "activity that modifies a position, but not through buy/sell
// activity", covering both a stock split (subtype='split', which NormalizeTradeType displays as
// "Other" for UX reasons — display and reconciliation are different questions) and an in-kind/
// ACATS transfer of shares between accounts. Both change holdings.quantity with no corresponding
// buy/sell/reinvest row, so the repository's PositionReconciliation query sums ALL type='transfer'
// rows into the ledger net regardless of subtype, in addition to the buy/sell/reinvest partition.
// Options assignment/exercise is a known further gap: Plaid does not expose a distinct subtype
// set for it in this codebase today, so it is NOT covered and would still misreport as a drift if
// it occurred against a held security. A second, UNVERIFIED gap sits in the transfer widening
// itself: the arithmetic above assumes Plaid signs a share-OUT transfer negatively, mirroring the
// buy/sell convention its docs state explicitly — but Plaid's docs do NOT state a sign convention
// for type='transfer' the way they do for buy/sell. If a brokerage instead reports a transfer-out
// with a POSITIVE quantity, this ledger double-counts it (once as the departing shares, and again
// because the sign is wrong), overstating the position by 2x the transferred amount. No test in
// this codebase can settle which convention Plaid actually uses for transfers; only real
// production data can, and none has been observed with an outbound transfer yet.
//
// ⚠ Drifts IS NOT YET FULLY ACTIONABLE — READ THIS BEFORE TREATING A NON-EMPTY Drifts LIST AS A
// CONFIRMED PROBLEM LIST. There are two distinct sources of expected, non-actionable drift, and
// only the first is currently addressed:
//
//  1. THE 2-YEAR LOOKBACK, ADDRESSED (partially) via SuppressedPreWindowCount. Plaid's investments
//     endpoints only return two years of investment_transactions. A position opened before that
//     window has real shares the ledger simply never received, so the ledger net for that
//     security will fall short of the holding's actual quantity through NO fault of the sync.
//     The chosen proxy for "the ledger can plausibly speak to this position's origin" is: DOES
//     THE LEDGER CONTAIN AT LEAST ONE type='buy' ROW FOR THIS (account, security) PAIR? A position
//     with zero buys in the ledger — only sells, only reinvestments/transfers, or no rows at all
//     — could not have been opened inside the visible window (you cannot sell, reinvest into, or
//     receive a transfer TO a position you never bought without Plaid also reporting that buy),
//     so a quantity mismatch there is exactly what the window's edge predicts. Those pairs are
//     reported separately, as SuppressedPreWindowCount, rather than folded into Drifts.
//
//  2. PRE-WINDOW BASE PLUS IN-WINDOW CONTRIBUTIONS — NOT ADDRESSED, AND LIKELY THE DOMINANT
//     POPULATION OF Drifts, NOT A RESIDUAL. The has-a-buy proxy above only excludes a position
//     with ZERO buys. A position opened years ago and topped up ever since by recurring buys —
//     dollar-cost averaging, an auto-invest schedule, a payroll-deferral retirement contribution —
//     has plenty of buys in the visible window, so it PASSES the proxy, and is then reported
//     forever with a Delta equal to its entire pre-window base (everything bought before the
//     window's start, never captured). This is precisely the modal long-held, regularly-
//     contributed-to position — the shape this issue's reinvesting-accounts framing exists to
//     serve — so it is not a rare edge case sitting alongside real drifts; on real portfolios it
//     is expected to be most of what Drifts contains. Treat a non-empty Drifts entry as "worth
//     investigating whether this is (a) a genuine ledger/position disagreement, (b) an
//     uncaptured corporate action/transfer this file doesn't yet enumerate, or (c) simply a
//     position older than two years with ongoing contributions" — NOT as a pre-filtered list of
//     confirmed problems. Closing this gap needs the position anchored to a WINDOW rather than
//     compared against the position's whole lifetime: reconcile holdings.quantity minus the
//     earliest available holding_snapshots row at or after the ledger's own starting point
//     against the ledger sum over that SAME interval, so the reconciliation never depends on
//     history older than what both sides can see. holding_snapshots (migration 000160) already
//     has the shape (account_id, security_id, as_of_date, quantity) this would need. Left for a
//     follow-up round rather than done here.
//
// WHY QUANTITY, NOT MONEY, AND WHY THE CURRENCY HELPERS DO NOT APPLY. domain.ResolveCurrencyCodes/
// CurrenciesConflict exist because a MONEY total summed across mixed currencies is meaningless
// without an FX rate this system does not have (see investment_currency.go). A share quantity has
// no currency at all — "42.93 shares" means the same thing regardless of what currency the
// brokerage prices them in — so there is no unit-mismatch hazard here to declare or refuse. This
// diagnostic sums quantity only and deliberately does not touch price, amount, or cost basis.
// ─────────────────────────────────────────────────────────────────────────────────────────────

// PositionReconciliationQuantityTolerance bounds how far a ledger-implied position may differ
// from the Plaid snapshot before it counts as a drift, rather than float64 accumulation noise
// from summing many fractional-share rows. Three decimal places — thousandths of a share — is
// finer than any brokerage's minimum tradable increment for a fractional share in production data,
// so tightening it further would only make the report noisier without catching anything real.
//
// Exported (not a private constant) so repository.PositionReconciliationSummary's SQL-side
// FILTER clauses can format THIS value directly rather than hardcoding a second literal that
// could silently drift out of step with ClassifyPositionReconciliationMember below — see that
// method's own doc comment for why the two paths must never classify a row differently.
const PositionReconciliationQuantityTolerance = 0.001

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

// PositionReconciliationReport is GetPositionReconciliation's result: candidate drifts worth an
// operator's attention, plus a count of positions the 2-year lookback made unevaluable. See the
// file-level "NOT YET FULLY ACTIONABLE" comment above before treating Drifts as a pre-filtered
// problem list — a large share of it is expected to be ordinary long-held, regularly-contributed-
// to positions, not confirmed defects.
type PositionReconciliationReport struct {
	// Drifts are (account, security) pairs where the ledger contains at least one buy for the
	// pair (the "plausibly-in-window" proxy) AND the ledger-implied quantity disagrees with the
	// Plaid snapshot by more than PositionReconciliationQuantityTolerance. ⚠ See the file-level
	// comment: this proxy only catches source 1 (zero-buy pre-window positions), not source 2
	// (a pre-window base with in-window contributions) — Drifts is expected to contain real
	// noise from source 2 today, not just genuine problems.
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

// PositionReconciliationSummary is the /admin/health gauge (#1631 M8b I1): the SAME
// classification as PositionReconciliationReport, aggregated as counts across EVERY user's active
// holdings rather than materializing the per-user Drifts list. Global, not per-user, matching
// ForkedSecurityIdentityMetrics's convention — an operator gauge needs "how many", not one user's
// detail. See PositionReconciliationReport's doc comment for what DriftCount does and does not
// mean; the "not yet fully actionable" caveat applies here identically.
//
// Available follows the same "unknown is not zero" convention as every other optional gauge on
// domain.SystemHealthResponse: an unwired repository or a failed query reports Available=false,
// never a confident zero.
type PositionReconciliationSummary struct {
	DriftCount               int64 `json:"drift_count"`
	SuppressedPreWindowCount int64 `json:"suppressed_pre_window_count"`
	TotalPositionsChecked    int64 `json:"total_positions_checked"`
	Available                bool  `json:"available"`
}

// ClassifyPositionReconciliationMember reports whether a candidate (holding vs. ledger) pair
// should be added to a report as a Drift, counted as SuppressedPreWindowCount, or dropped as a
// clean reconciliation (delta within tolerance). hasLedgerBuy is the file-level "plausibly
// in-window" proxy: whether the ledger contains at least one type='buy' row for this
// (account, security) pair.
//
// Extracted as a pure function (no DB access) so the suppression rule — the part of this
// diagnostic most likely to need tuning — has a unit test that does not require Postgres.
//
// ⚠ THIS EXACT CLASSIFICATION IS ALSO EXPRESSED IN SQL, in
// repository.PositionReconciliationSummary's FILTER clauses (#1631 M8b N2) — the /admin/health
// gauge aggregates every user's holdings in one query rather than streaming every row into Go on
// a 30-second poll, so it cannot call this Go function per row. The SQL mirrors this function
// term for term (same PositionReconciliationQuantityTolerance constant, formatted directly into
// the query rather than re-typed as a literal; same hasLedgerBuy gate) and
// TestPositionReconciliationSummary_AgreesWithPerUserReportAcrossUsers_Integration proves the two
// paths agree on the same seeded data. If this function's logic ever changes, that SQL and that
// test must change with it.
func ClassifyPositionReconciliationMember(delta float64, hasLedgerBuy bool) (isDrift, isSuppressed bool) {
	if delta > PositionReconciliationQuantityTolerance || delta < -PositionReconciliationQuantityTolerance {
		if hasLedgerBuy {
			return true, false
		}
		return false, true
	}
	return false, false
}
