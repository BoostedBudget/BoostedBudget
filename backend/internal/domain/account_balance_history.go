package domain

// Per-account retained balance history (Issue #1569).
//
// ⚠ TWO UNIVERSES, AND THIS IS THE HISTORICAL ONE. `accounts.current_balance` answers "what is in
// this account right now", and accountBalanceProjection (repository/account_liveness_sql.go) sends
// NULL for it on every terminal account precisely so a frozen figure cannot be read as a live one.
// The types below answer a different question — "what did this account hold on each day we
// recorded it" — and every value in them is inseparable from the UTC day it was true on. That is
// why Date is a plain non-pointer field on the point and not an optional decoration: a balance
// that cannot say WHEN it was true is exactly the orphan figure #1591 deleted, and rendering one
// beside a closed account reads as a current balance.
//
// Nothing here may be routed through balanceMatchesRange / compareBalanceValues on the frontend,
// for the same reason.

// AccountBalanceHistoryPoint is one UTC calendar day's LAST recorded balance for one account.
//
// "Last recorded" rather than "the balance": balance_snapshots deliberately has no per-day
// uniqueness key (see BalanceSnapshotRepository.Create), so a high-frequency sync day can hold
// several rows. The reader collapses them the same way GetAggregatedHistory's second stage does —
// freshest snapshot_at wins, id breaking a tie — so the series carries one point per day.
type AccountBalanceHistoryPoint struct {
	// Date is the UTC calendar day (YYYY-MM-DD) this balance was true on.
	Date string `json:"date" format:"date"`
	// CurrentBalance / AvailableBalance are pointers because the provider may report either as
	// absent, and a missing balance must stay missing rather than become a confident zero.
	CurrentBalance   *float64 `json:"current_balance,omitempty"`
	AvailableBalance *float64 `json:"available_balance,omitempty"`
	// CurrencyCode is carried per point rather than once for the series: a re-linked account can
	// in principle change it, and silently formatting an old figure in a new currency would be a
	// wrong number rather than a missing one.
	CurrencyCode string `json:"currency_code"`
}

// AccountBalanceHistory is one account's retained day-by-day balance series, oldest day first.
//
// Points is never nil — an account with nothing recorded returns an empty slice, so the caller
// distinguishes "we kept nothing" from "the read failed" without inspecting an error.
//
// ⚠ `nullable:"false"` IS LOAD-BEARING, NOT DECORATION (#1569 T7-FIX, finding H). huma marks every
// Go slice `type: [array, "null"]` by default, so the generated schema and orval's client declared
// `points` as `AccountBalanceHistoryPoint[] | null` while the service guaranteed the opposite. On
// this endpoint that ambiguity is the whole message: `null` reads as "the read failed" and `[]`
// reads as "nothing was destroyed, we simply kept nothing", and the screen it feeds exists to say
// the second one. The tag makes the schema follow the implementation instead of contradicting it.
// Pinned by TestAccountBalanceHistorySchema_PointsIsNeverNull (the schema) and
// TestAccountBalanceHistoryHandler_EmptyHistoryIs200 (the wire).
type AccountBalanceHistory struct {
	AccountID string `json:"account_id"`
	// FirstDate / LastDate bound the series without the caller re-deriving them, and are nil when
	// Points is empty. Pre-computed for the same reason the analytics layer pre-computes
	// everything: an LLM tool-caller should not spend tokens doing arithmetic over the rows.
	FirstDate *string                      `json:"first_date,omitempty" format:"date"`
	LastDate  *string                      `json:"last_date,omitempty"`
	Points    []AccountBalanceHistoryPoint `json:"points" nullable:"false"`
}
