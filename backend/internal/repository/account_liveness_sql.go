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
