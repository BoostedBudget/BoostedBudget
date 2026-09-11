package operations

// THE ROUNDING BOUNDARY (#1636), implementing Decision 3 of
// docs/decisions/2026-09-11-money-representation-float64-and-the-rounding-boundary.md.
//
// Money is `float64` in Go by recorded choice (Decision 1) and is aggregated in Postgres' exact
// NUMERIC (Decision 2), which leaves exactly one artefact: a correct figure carrying a long
// binary tail — `1234.5600000000002` in shape — produced by the final Go-side subtraction or
// `× 100`. This file folds that tail away, HERE, at the last point the value is a Go number
// rather than JSON.
//
// ⚠ THREE RULES, and each of the three has a reason that is not "consistency":
//
//  1. CURRENCY-DENOMINATED fields fold to 2dp. Cents are the smallest unit any of these figures
//     can mean.
//  2. PERCENTAGE fields fold to 2dp. These are the sites that MANUFACTURE the tail: dividing and
//     then multiplying by 100 reliably produces a long run of digits from two clean inputs.
//  3. QUANTITY and PER-UNIT PRICE fields DO NOT FOLD. `holdings.quantity`,
//     `holdings.institution_price`, `investment_transactions.quantity`/`price` and
//     `holding_snapshots.quantity`/`institution_price` are `DECIMAL(18,8)` (migrations 000051 and
//     000160) because fractional shares and crypto need those digits. Folding one to cents
//     destroys real data rather than cosmetic noise. A blanket "round every float64 on the
//     response" satisfies rules 1 and 2 and is WRONG — it would silently turn 0.00123456 BTC
//     into 0.00. TestEveryMoneyFieldOnAnInvestmentResponseIsRoundedAtTheEdge asserts both
//     polarities, so that mistake fails rather than passing.
//
// ⚠ WHY HERE AND NOT IN THE PRODUCING LAYER. The repository's and domain's values are the ones
// that can feed further computation — snapshots, reconciliation, any future composition. Rounding
// there would make a stored or re-aggregated figure depend on how many rounding steps it had
// passed through. Three of the percentage sites are domain methods (Holding.GainLossPct,
// Holding.DayChangePct, HoldingSnapshotView.ComputeGainLoss) and two more are in
// investmentService.GetPortfolioSummary; none of them rounds, and NOTHING HERE ASSUMES ONE DID.
// Every field is folded on its own, at this edge.
//
// ⚠ WHAT THIS DOES NOT DO — ADR Decision 4. Rounding is applied to a figure that has ALREADY been
// cleared for emission. It never turns a withheld figure into a shown one and never converts a
// nil into a 0.00: money.ToCentsPtr returns nil unchanged, which is why every optional field below
// goes through it rather than through a dereference. A nil here means "we do not know" (no prior
// close, no cost basis surviving the `FILTER (WHERE h.cost_basis IS NOT NULL)`) or "we refuse to
// compute this" (a day change across two currencies, with no FX rate source in this system). A
// 0.00 in any of those places would be an assertion about the user's money that nothing in this
// system has grounds to make. Mixed-currency totals stay flagged, not tidied: rounding a
// currency-blind sum does not make it correct, it makes it tidily wrong.
//
// ⚠ IN-PLACE, ON A PER-REQUEST VALUE. Each function mutates the struct the service just built and
// is about to serialise; none of these values outlives the request or is written back. Optional
// fields are re-pointed at a fresh float (ToCentsPtr allocates) rather than written through, so
// nothing the producing layer still holds a pointer into is altered.

import (
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/money"
)

// roundHoldingViews folds the money and percentage figures on GET /investments/holdings.
//
// Quantity and InstitutionPrice are deliberately absent: DECIMAL(18,8), rule 3 above.
func roundHoldingViews(views []*domain.HoldingView) {
	for _, v := range views {
		if v == nil {
			continue
		}
		v.CostBasis = money.ToCentsPtr(v.CostBasis)
		v.InstitutionValue = money.ToCentsPtr(v.InstitutionValue)
		v.GainLoss = money.ToCentsPtr(v.GainLoss)
		v.GainLossPct = money.ToCentsPtr(v.GainLossPct)
		v.DayChange = money.ToCentsPtr(v.DayChange)
		v.DayChangePct = money.ToCentsPtr(v.DayChangePct)
	}
}

// roundPortfolioSummary folds GET /investments/portfolio/summary.
//
// ⚠ TotalGainLossPct and DayChangePct are produced by `× 100` sites in
// services/investment_service.go (GetPortfolioSummary), which the ADR's own percentage inventory
// MISSED — it enumerated the repository and domain layers and not the service. They are folded
// here for exactly the reason the domain sites are: the boundary rounds the field and assumes
// nothing about its producer.
func roundPortfolioSummary(s *domain.PortfolioSummary) {
	if s == nil {
		return
	}
	s.TotalValue = money.ToCents(s.TotalValue)
	s.TotalCostBasis = money.ToCents(s.TotalCostBasis)
	s.TotalGainLoss = money.ToCents(s.TotalGainLoss)
	s.TotalGainLossPct = money.ToCentsPtr(s.TotalGainLossPct)
	s.DayChange = money.ToCentsPtr(s.DayChange)
	s.DayChangePct = money.ToCentsPtr(s.DayChangePct)
}

// roundPortfolioHistoryPoints folds GET /investments/portfolio/history.
//
// Date is a YYYY-MM-DD string and CurrencyCode / IsMixedCurrency are declarations about the
// series, not figures — nothing else on this surface is money.
func roundPortfolioHistoryPoints(points []domain.PortfolioHistoryPoint) {
	for i := range points {
		points[i].TotalValue = money.ToCents(points[i].TotalValue)
	}
}

// roundInvestmentTransactions folds GET /investments/investment-transactions.
//
// ⚠ Quantity and Price are deliberately absent. This is the trap struct: `Price` is the per-unit
// price at the time of the transaction over a DECIMAL(18,8) column, sitting directly beside
// `Amount`, which IS money. Fees is money.
func roundInvestmentTransactions(txns []domain.InvestmentTransaction) {
	for i := range txns {
		txns[i].Amount = money.ToCents(txns[i].Amount)
		txns[i].Fees = money.ToCentsPtr(txns[i].Fees)
	}
}

// roundAllocationBreakdown folds GET /investments/portfolio/allocation.
//
// Percentage is one of the four `buckets[i].TotalValue / grandTotal * 100` sites in
// investment_analytics_repository.go — the densest source of the artefact on this surface.
func roundAllocationBreakdown(b *domain.AllocationBreakdown) {
	if b == nil {
		return
	}
	b.TotalValue = money.ToCents(b.TotalValue)
	for i := range b.Buckets {
		b.Buckets[i].TotalValue = money.ToCents(b.Buckets[i].TotalValue)
		b.Buckets[i].Percentage = money.ToCents(b.Buckets[i].Percentage)
	}
}

// roundTopMovers folds GET /investments/portfolio/top-movers.
//
// ExcludedByCurrencyCount and UnknownCurrencyCount are COUNTS of holdings, not money, and are
// `int` — they cannot reach this function. RankingCurrencyCode is a declaration.
func roundTopMovers(tm *domain.TopMovers) {
	if tm == nil {
		return
	}
	roundTopMoverItems(tm.Gainers)
	roundTopMoverItems(tm.Losers)
}

func roundTopMoverItems(items []domain.TopMoverItem) {
	for i := range items {
		items[i].DayChange = money.ToCentsPtr(items[i].DayChange)
		items[i].DayChangePct = money.ToCentsPtr(items[i].DayChangePct)
		items[i].TotalGainLoss = money.ToCentsPtr(items[i].TotalGainLoss)
	}
}

// roundGainLossSummary folds GET /investments/portfolio/gain-loss — the endpoint the ADR names as
// the illustrative case, where `total_unrealized_gain_loss_pct` comes from
// `totalGL / totalKnownPairCost * 100` in investment_analytics_repository.go.
//
// RealizedGainLossSupported is a bool and always false; it is a contract statement, not a figure.
func roundGainLossSummary(g *domain.GainLossSummary) {
	if g == nil {
		return
	}
	g.TotalUnrealizedGainLoss = money.ToCents(g.TotalUnrealizedGainLoss)
	g.TotalUnrealizedGainLossPct = money.ToCentsPtr(g.TotalUnrealizedGainLossPct)
	g.TotalCostBasis = money.ToCents(g.TotalCostBasis)
	g.TotalValue = money.ToCents(g.TotalValue)
	for i := range g.ByAccount {
		g.ByAccount[i].UnrealizedGainLoss = money.ToCents(g.ByAccount[i].UnrealizedGainLoss)
		g.ByAccount[i].UnrealizedGainLossPct = money.ToCentsPtr(g.ByAccount[i].UnrealizedGainLossPct)
		g.ByAccount[i].CostBasis = money.ToCents(g.ByAccount[i].CostBasis)
		g.ByAccount[i].Value = money.ToCents(g.ByAccount[i].Value)
	}
}

// roundIncomeSummary folds GET /investments/portfolio/income.
//
// Count is a transaction count. StartDate/EndDate are `json:"-"` and their *Display siblings are
// YYYY-MM-DD strings, not money.
func roundIncomeSummary(s *domain.IncomeSummary) {
	if s == nil {
		return
	}
	s.TotalIncome = money.ToCents(s.TotalIncome)
	for i := range s.ByType {
		s.ByType[i].TotalAmount = money.ToCents(s.ByType[i].TotalAmount)
	}
}

// roundHoldingSnapshotHistory folds GET /investments/holdings/history.
//
// ⚠ HISTORICAL figures, and the fold is applied for the same presentational reason regardless —
// a retained as-of value carries the same binary tail as a live one. Quantity and
// InstitutionPrice stay exact: holding_snapshots gives them DECIMAL(18,8) (migration 000160)
// precisely so a past fractional position is retained as it stood.
func roundHoldingSnapshotHistory(h *domain.HoldingSnapshotHistory) {
	if h == nil {
		return
	}
	h.TotalValue = money.ToCentsPtr(h.TotalValue)
	for i := range h.Positions {
		h.Positions[i].CostBasis = money.ToCentsPtr(h.Positions[i].CostBasis)
		h.Positions[i].InstitutionValue = money.ToCentsPtr(h.Positions[i].InstitutionValue)
		h.Positions[i].GainLoss = money.ToCentsPtr(h.Positions[i].GainLoss)
		h.Positions[i].GainLossPct = money.ToCentsPtr(h.Positions[i].GainLossPct)
	}
}

// roundSavingsProgressSummary folds GET /savings-goals/progress-summary.
//
// ⚠ THIS IS THE RECONCILIATION THE ADR NAMED, and it is here rather than in the service on
// purpose. savings_progress_service.go used to define its own `roundToCents` — the identical 2dp
// fold — and apply it while BUILDING this summary, one layer below where Decision 3 puts the
// boundary. The ADR stated outright that "round at the handler and nowhere else" and a service
// that rounds cannot both be the convention indefinitely, and assigned the resolution to #1636.
// Resolved upward: the service now returns unrounded figures and this is the only place the fold
// happens, because a service value can feed further computation and a convention with a named
// exception drifts into two conventions.
//
// ⚠ ONE FIGURE IN THAT SERVICE STILL ROUNDS, and it is NOT an exception to the above. See the
// note on savings_progress_service.go's RecomputeGoal: the value it folds is PERSISTED to
// current_amount and then compared against the target to decide completion. That is an inbound
// canonicalisation of a stored figure — the same class as ConfirmMatch's in transfer_suggestion.go,
// which the ADR itself classifies as "an input canonicalisation, not an output boundary" — and
// moving it here would change which rows complete. It is a different mechanism, not a surviving
// copy of this one.
//
// ⚠ AccountPercentage is deliberately absent. Despite the name it is a FRACTION, a 0–1 multiplier
// applied to a linked account's balance, not a 0–100 display percentage. Folding it to 2dp would
// turn a 33.5%-of-balance goal into 34% and move the user's target. LinkedBalance IS money and IS
// folded here — the old service-layer helper never touched it, which is the kind of gap a single
// boundary closes.
func roundSavingsProgressSummary(s *domain.SavingsProgressSummary) {
	if s == nil {
		return
	}
	s.TotalSaved = money.ToCents(s.TotalSaved)
	s.TotalTarget = money.ToCents(s.TotalTarget)
	s.OverallPercentComplete = money.ToCents(s.OverallPercentComplete)
	for i := range s.Goals {
		s.Goals[i].CurrentAmount = money.ToCents(s.Goals[i].CurrentAmount)
		s.Goals[i].TargetAmount = money.ToCents(s.Goals[i].TargetAmount)
		s.Goals[i].PercentComplete = money.ToCents(s.Goals[i].PercentComplete)
		s.Goals[i].ConfirmedContributionsTotal = money.ToCents(s.Goals[i].ConfirmedContributionsTotal)
		s.Goals[i].LinkedBalance = money.ToCentsPtr(s.Goals[i].LinkedBalance)
	}
}
