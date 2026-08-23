package domain

import "time"

// #1569 Task 11 (S6/E4): the vocabulary of the portfolio_snapshots repair.
//
// The repair rewrites recorded financial history, so every day it looks at gets a verdict and
// every verdict is recorded — including the days it declines to touch. A day left alone silently
// is indistinguishable from a day never examined, and the whole point of the refusals is that they
// are deliberate.
const (
	// PortfolioRepairOutcomeRepaired — the day had full holding_snapshots coverage and the stored
	// aggregate disagreed with what that coverage reconstructs. The row was rewritten.
	PortfolioRepairOutcomeRepaired = "repaired"
	// PortfolioRepairOutcomeVerified — full coverage, and the stored aggregate already matched.
	// Nothing was written, not even updated_at.
	PortfolioRepairOutcomeVerified = "verified"
	// PortfolioRepairOutcomeRefusedNoCoverage — state (a): NO holding_snapshots rows for that day.
	// The day is left exactly as it is and no zero is written. A day can lack rows because it
	// predates the history tables or because a partial sync skipped the account and the daily
	// sweep never ran; "no rows" is indistinguishable from "held nothing", and guessing which is
	// the same corruption this repair exists to remove. Binding (task 10, ruling R36).
	PortfolioRepairOutcomeRefusedNoCoverage = "refused_no_coverage"
	// PortfolioRepairOutcomeRefusedPartialCoverage — state (b): some of the day's live,
	// holdings-bearing accounts have rows and some do not. Reconstructing from an incomplete row
	// set produces a number that is wrong in a PLAUSIBLE way, which is worse than an obvious gap,
	// so the day is refused rather than half-rebuilt.
	PortfolioRepairOutcomeRefusedPartialCoverage = "refused_partial_coverage"
)

// PortfolioSnapshotTotals is the four-column aggregate portfolio_snapshots stores.
//
// TotalGainLoss and TotalGainLossPct are NOT derivable from the other two: Upsert computes them
// over only the holdings where cost_basis AND institution_value are both non-NULL, and the pct
// denominator is that FILTERED basis rather than TotalCostBasis. A reconstruction that divides by
// TotalCostBasis produces a plausible wrong number.
type PortfolioSnapshotTotals struct {
	TotalValue       float64  `json:"total_value"`
	TotalCostBasis   float64  `json:"total_cost_basis"`
	TotalGainLoss    float64  `json:"total_gain_loss"`
	TotalGainLossPct *float64 `json:"total_gain_loss_pct,omitempty"`
}

// PortfolioSnapshotRepair is one examined (user, day) — one row of portfolio_snapshot_repairs.
type PortfolioSnapshotRepair struct {
	UserID       string    `json:"user_id"`
	SnapshotDate time.Time `json:"snapshot_date"`
	Outcome      string    `json:"outcome"`
	Reason       string    `json:"reason"`

	// ExpectedAccountCount is the accounts recorded live on the day that are known to bear
	// holdings; ObservedAccountCount is how many of them the history actually has rows for on that
	// day, counted WITHOUT filtering on present — a row's existence proves the capture observed the
	// account, while present says what it held.
	ExpectedAccountCount int `json:"expected_account_count"`
	ObservedAccountCount int `json:"observed_account_count"`

	Previous PortfolioSnapshotTotals  `json:"previous"`
	Repaired *PortfolioSnapshotTotals `json:"repaired,omitempty"`

	ExaminedAt time.Time `json:"examined_at"`
}

// PortfolioRepairSummary counts one repair run's verdicts.
type PortfolioRepairSummary struct {
	Examined               int `json:"examined"`
	Repaired               int `json:"repaired"`
	Verified               int `json:"verified"`
	RefusedNoCoverage      int `json:"refused_no_coverage"`
	RefusedPartialCoverage int `json:"refused_partial_coverage"`
}

// Add folds one verdict into the summary.
func (s *PortfolioRepairSummary) Add(outcome string, n int) {
	s.Examined += n
	switch outcome {
	case PortfolioRepairOutcomeRepaired:
		s.Repaired += n
	case PortfolioRepairOutcomeVerified:
		s.Verified += n
	case PortfolioRepairOutcomeRefusedNoCoverage:
		s.RefusedNoCoverage += n
	case PortfolioRepairOutcomeRefusedPartialCoverage:
		s.RefusedPartialCoverage += n
	}
}

// Merge folds another run's counts in, for a caller repairing many users in one pass.
func (s *PortfolioRepairSummary) Merge(o PortfolioRepairSummary) {
	s.Examined += o.Examined
	s.Repaired += o.Repaired
	s.Verified += o.Verified
	s.RefusedNoCoverage += o.RefusedNoCoverage
	s.RefusedPartialCoverage += o.RefusedPartialCoverage
}
