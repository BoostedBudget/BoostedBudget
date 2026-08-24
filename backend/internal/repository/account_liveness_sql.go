package repository

import "strings"

// ── The single account-liveness predicate (#1569 task 11, carried from task 10's R41) ─────────
//
// There used to be TWO copies of this rule: `accountLiveOnDateClause` here (used by the whole-user
// holdings capture) and an inline literal inside portfolio_snapshot_repository.go's Upsert. Task
// 10's review measured what that cost: changing `valid_to >` to `>=` on the history side alone left
// the ENTIRE ./internal/repository integration package green, because each side's fixture only
// probed the boundary the other side owned. The two are now one string, so a drift is no longer
// expressible — a boundary edit moves both callers at once and both boundary tests see it.
//
// "Live on the date" means the recorded 'active' interval OVERLAPS the calendar day — not "active
// at midnight" and not "active right now". An account linked earlier the same day has valid_from
// after midnight, and an account disconnected earlier today was genuinely live for part of it.
// Both must count for that day, and both callers must agree that they do, because task 11 repairs
// portfolio_snapshots aggregates FROM the rows the capture writes.
//
// Fix round 1, Critical 1 (task 10) is why the capture asks this at all: accounts are SOFT-removed
// and the only DELETE FROM holdings lives in ReplaceForAccount (which runs only for accounts a sync
// reaches), so a disconnected account's holdings live on indefinitely and the daily sweep used to
// re-capture them every day. Measured with one active account (1000) and one disconnected ten days
// ago whose holdings are retained (500): the history summed to 1500.00 over 2 rows while Upsert
// wrote 1000.00 over 1.
//
// Note the shape of the hole that hid in: GetDistinctUserIDsWithHoldings joins a.status = 'active',
// so a user whose accounts are ALL terminal is skipped entirely — but a user with even one active
// account had ALL their terminal accounts captured. A single-account fixture cannot see that.
const accountLiveOnDateClause = `
	EXISTS (
	    SELECT 1 FROM account_status_history ash
	    WHERE ash.account_id = $ACCOUNT
	      AND ash.status = 'active'
	      -- Explicitly UTC (#1569 F4): "$n::date + INTERVAL '1 day'" yields a timestamp WITHOUT
	      -- time zone, which Postgres resolves against the SESSION TimeZone when compared to a
	      -- timestamptz. Production is correct only because NewPool pins TimeZone=UTC (the #1461
	      -- lesson) — an implicit dependency this predicate must not carry, which also matters
	      -- because a migration or a psql session does NOT inherit that pin. The lower bound needs
	      -- the explicit ::timestamp step: a bare "date AT TIME ZONE 'UTC'" casts the date to
	      -- timestamptz FIRST (in the session zone) and then reads it out as UTC wall time — the
	      -- opposite of what is wanted — whereas "date + INTERVAL" is already a timestamp without
	      -- time zone and needs no such nudge.
	      AND ash.valid_from < ($DATE::date + INTERVAL '1 day') AT TIME ZONE 'UTC'
	      AND (ash.valid_to IS NULL OR ash.valid_to > ($DATE::date)::timestamp AT TIME ZONE 'UTC')
	)`

// accountLiveOnDateSQL renders accountLiveOnDateClause for one caller.
//
// accountExpr is the SQL expression naming the account being tested (`h.account_id`, `a.id`,
// `c.account_id`, …) and dateExpr is the expression naming the calendar day — a bind parameter
// (`$2`) for the callers that pass one, or a column (`ps.snapshot_date`) for the repair, which
// evaluates the predicate per candidate row. Both are compile-time constants at every call site;
// no user input reaches this substitution.
func accountLiveOnDateSQL(accountExpr, dateExpr string) string {
	return strings.NewReplacer("$ACCOUNT", accountExpr, "$DATE", dateExpr).Replace(accountLiveOnDateClause)
}

// ─── #1569 T12: THE THREE RETENTION UNIVERSES ────────────────────────────────────────────────
//
// Tasks 1-11 replaced deletion with retention. That answer is deliberately NOT "show every
// retained row in every query", and the line between them had been drawn implicitly, one query at
// a time, by whoever wrote each one. Task 12's brief called that out correctly: the defect was
// never that a disconnected brokerage's holdings are hidden — it is that nobody had CHOSEN it.
// So it is chosen here, once, and every site that participates points back at this comment.
//
// EVENT universe — a dated thing that HAPPENED.
//
//	`transactions`, `investment_transactions`. The row asserts "on 2025-03-14, 120.00 moved".
//	That is true forever; disconnecting the account in June cannot un-pay a March dividend.
//	Reads here are status-agnostic (banking analytics) or carry retainedAccountVisibilityLeg
//	(transaction_repository.go), which re-admits a row whose whole account chain has gone
//	terminal. Hiding these IS #1569's bug — the user paid for their own history.
//
// CURRENT-STATE universe — what is held / owed RIGHT NOW.
//
//	`holdings`, `credit_liabilities`, `mortgage_liabilities`, `student_loan_liabilities`. These
//	are REPLACE-semantics tables (holdingRepository.ReplaceForAccount deletes and re-inserts on
//	every sync), and the row carries NO as-of date. It is therefore only ever meaningful as an
//	assertion about the present. Once the account is terminal, nobody is confirming it any more:
//	the position may have been liquidated at the old broker, the loan may have been paid off, and
//	the price is frozen at whatever it was on disconnection day. Surfacing such a row through
//	GetByUserID or an allocation total would state a stale figure as a live one — and it would do
//	so permanently and invisibly, inflating net worth by an amount that never changes again.
//	That is a different lie from the one #1569 was filed about, not a smaller one.
//	Reads here therefore keep currentStateAccountLeg. This is the ruling, not the status quo
//	surviving by default.
//
// HISTORICAL-STATE universe — what was held / owed THEN.
//
//	`holding_snapshots`, `liability_statements` (tasks 6/10). These DO carry an as_of_date, are
//	append-only, and their readers (GetByAccountDateRange) are deliberately independent of the
//	account's current status, so a disconnected brokerage's last confirmed position is RETAINED
//	and answerable by a query that can say when it was true.
//
//	⚠ AS OF TASK 12 THAT QUERY HAS NO CALLER. GetByAccountDateRange has zero non-test callers on
//	both tables, and neither repository is referenced anywhere in internal/services or
//	internal/huma — only main.go's wiring and the worker that WRITES the rows. This universe is
//	currently written and never read. Do not describe it as something a user can reach; it is a
//	table with rows in it and no way out.
//
//	The current-state ruling above does NOT depend on that. It stands on its own ground: an
//	unlabelled stale position would inflate net worth permanently, whether or not a history
//	surface exists. What the missing surface costs is the CONSOLATION — "hidden here, visible
//	there" is not yet true, only "hidden here, retained there".
//
// The one query T12 moved across the line is investmentAnalyticsRepository.IncomeSummary: it
// aggregates investment_transactions over a closed date range — an event question — but was
// scoped like a current-state one, so a disconnection silently zeroed a historical income total
// while the investments feed over the SAME rows still listed the dividend. See its doc comment.
//
// TestRetentionUniverses_DisconnectedBrokerage_Integration asserts all three universes in one
// test, before and after a disconnection, so the boundary cannot drift without a failure.
//
// WHAT WOULD CHANGE THIS RULING. Not an opinion — a capability. If a current-state read ever
// gains a way to LABEL a row with the date it was last confirmed, then "show the last confirmed
// position, as of 2025-06-04" becomes expressible and is strictly better than hiding it. The
// DATA for that already exists in holding_snapshots; the service method, the endpoint and the UI
// do not. Until a caller can render that label, widening the read only produces an unlabelled
// stale number, which is the thing this ruling refuses.
func currentStateAccountLeg(alias string) string {
	return alias + ".status = 'active'"
}

// accountBalanceProjection renders the `current_balance` / `available_balance` pair of an
// account-shaped SELECT so that a TERMINAL-status account (removed / replaced / expired) carries
// NO balance at all. Issue #1569, task 21 (item D).
//
// THE RULING, from the user, direct: "If account is closed, treat as empty balance, we don't want
// orphaned balances." A closed account's `accounts.current_balance` is a figure frozen at the
// moment it stopped syncing. It is not what the account holds — nobody has confirmed that since —
// and it is not history either, because the column carries no as-of instant to make it one. It is
// an orphan: a number that no longer means anything, attached to a row that still renders.
//
// ONE CHOKE POINT, NOT ELEVEN EDITS. Every account-shaped projection in this package renders these
// two columns through here. That is the whole design: eleven hand-edited scan sites drift, and the
// drift is invisible (a stale balance looks exactly like a fresh one), whereas a single renderer is
// mutation-checkable and its absence is mechanically detectable —
// TestNoAccountProjectionCarriesATerminalBalance judges each SELECT list on its own and cannot be
// satisfied by a neighbouring query.
//
// NULL, NOT ZERO, and the difference is not cosmetic. The fields are `*float64` with
// `json:"...,omitempty"`, so NULL vanishes from the JSON entirely: a client has nothing to render
// and a consumer that sums has nothing to add. Zero is a NUMBER — it renders as "$0.00", which is a
// positive claim that the account is empty, and it is summed as a real datapoint. NULL is also
// already this field's established "no balance known" value (Plaid omits available_balance for
// several account types), so every existing nil-check already does the right thing: savings
// progress freezes rather than recomputing, and LinkedBalance is simply absent.
//
// ⚠ CURRENT UNIVERSE ONLY. This touches `accounts` projections and nothing else. Historical
// balances come from balance_snapshots and holding_snapshots, which carry their own as_of instant
// and are deliberately independent of the account's current status — a closed account's history
// stays exactly as queryable as it was. An AS-OF account read (accountLiveAsOfPredicate) is
// included in the rule rather than exempt from it: it returns the account because it was LIVE then,
// but `accounts.current_balance` is still today's frozen column and has no as-of meaning, so the
// balance for that instant must come from the snapshot either way.
//
// The INSERT path is untouched: CreateBatch writes what Plaid reported for an account it is
// creating, which is by construction active.
func accountBalanceProjection(alias string) string {
	q := ""
	if alias != "" {
		q = alias + "."
	}
	return "CASE WHEN " + q + "status = 'active' THEN " + q + "current_balance END AS current_balance, " +
		"CASE WHEN " + q + "status = 'active' THEN " + q + "available_balance END AS available_balance"
}
