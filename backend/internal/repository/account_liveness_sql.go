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
//	`holding_snapshots`, `liability_statements` (tasks 6/10). These DO carry an as_of_date and are
//	append-only, so a disconnected account's last confirmed position/statement is RETAINED either
//	way — but whether it is ANSWERABLE differs by table now, so treat the two entries below as
//	separate claims rather than one.
//
//	⚠ UPDATED #1569 R6B/C1, corrected again R6C — the "no caller" claim was FALSE for
//	holding_snapshots and was already half-false when it was written; both halves are corrected
//	below rather than deleted, because the shape of the mistake (declaring a universe unreachable
//	without checking for readers this round added or already had) is the thing worth a reader
//	seeing. R6C found that C1's fix had gone further than the facts warranted: it deleted the "no
//	way out" warning for BOTH tables when only holding_snapshots had actually gained a way out.
//	liability_statements has no PRODUCT reader, and the warning is restored for it below, scoped to
//	that table and to product surfaces — it is NOT unreachable, see below.
//
//	holding_snapshots now HAS a user-facing historical reader: HoldingHistoryService
//	(internal/services/holding_history_service.go), reached through
//	GET /api/v1/investments/holdings/history, via HoldingSnapshotRepository.
//	ListHistoryDatesForAccount and .GetHistoryForAccountOnDate — NOT through GetByAccountDateRange,
//	which still has zero non-test callers and remains exactly the unreached primitive the original
//	claim described.
//
//	It also already had a second, pre-existing consumer when this comment first shipped:
//	portfolio_snapshot_repair.go reads holding_snapshots directly in raw SQL (see its JOIN and
//	EXISTS clauses) to repair a day's recorded portfolio_snapshots aggregate. That repair path
//	predates this branch's holdings-history endpoint, so "written and never read" was already
//	inaccurate about the TABLE, even though it was accurate about the specific
//	GetByAccountDateRange query being described.
//
//	liability_statements, by contrast, has no PRODUCT reader: LiabilityStatementRepository appears
//	in exactly one non-test file — its own — so GetByAccountDateRange has zero non-test callers
//	here too, with no HoldingHistoryService-shaped path standing in for it. So for THIS table the
//	original "no way out" warning stands in the form that is actually true: a disconnected loan's
//	last confirmed statement is RETAINED and unreachable from every SCREEN in the app.
//
//	⚠ IT IS NOT "ANSWERABLE BY NOTHING", AND THIS PARAGRAPH SAID SO UNTIL #1569 T8.5 — eleven lines
//	below the paragraph that exists to apologise for the identical mistake about holding_snapshots.
//	The Article-20 GDPR export reads the WHOLE table for the user (data_export_repository.go,
//	`Table: "liability_statements"`, `OwnershipPredicate: "t.user_id = $1"`), and the
//	account-deletion notice names it to the user too (queue/jobs/retention_cleanup_check.go,
//	"including their statements and APR schedules"). Both are TABLE-DRIVEN: they name the table as
//	a string and never mention LiabilityStatementRepository, so the symbol grep that produced the
//	original claim could not see either of them, twice in a row.
//
//	⚠ SO: BEFORE WRITING "NOTHING READS X" ABOUT ANY TABLE, GREP THE TABLE NAME — not the
//	repository type — through data_export_repository.go and queue/jobs/retention_cleanup_check.go.
//	That is the check both earlier drafts skipped, and it is the only reason this mistake was able
//	to happen twice in one comment.
//
//	The RULING is unchanged and is not an oversight: no product surface is built for it, privacy
//	policy §8.2a does not name liabilities, so retaining the row with no in-app reader breaks no
//	promise, and building a liabilities reader alongside the holdings one was deliberately kept out
//	of scope for this branch.
//
//	The current-state ruling above does NOT depend on any of this. It stands on its own ground: an
//	unlabelled stale position would inflate net worth permanently, whether or not a history surface
//	exists for either table.
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
// position, as of 2025-06-04" becomes expressible and is strictly better than hiding it.
//
//	⚠ CORRECTED R6C — this paragraph used to say the service method, the endpoint and the UI for
//	that label do not exist. They do, as of this branch: HoldingHistoryService (:101 above), GET
//	/api/v1/investments/holdings/history (:102-103 above), and ClosedAccountHoldingsHistory
//	(apps/web/src/features/investments/components/HoldingsTable.tsx:414). What that caller
//	provides, though, is a separate HISTORICAL query path, not a label folded into the
//	CURRENT-STATE read this function guards — GetByUserID and the allocation totals still return
//	nothing for a terminal account, and still should, because "as of DATE" belongs in the
//	historical view, not in a query whose contract is "right now". So the ruling is unchanged, but
//	the reason has moved: not that the labelling capability is missing, but that folding it into
//	the current-state read would duplicate a capability that now correctly lives elsewhere.
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

// ─── #1591: THE OTHER HALF — a closed account's LAST KNOWN balance ───────────────────────────
//
// accountBalanceProjection above says a terminal account carries no CURRENT balance, and that
// stays. This says what it may carry instead: one figure from `balance_snapshots`, with the
// instant it was true, rendered as `last_known_balance` / `last_known_balance_at`.
//
// The two are complements, not a contradiction. `accounts.current_balance` is a column with no
// as-of instant, so a terminal account's copy of it is an orphan — a number that stopped meaning
// anything the day the account stopped syncing, and that nothing on screen can honestly label. A
// `balance_snapshots` row is the opposite: it carries `snapshot_at`, so it can be stated as what
// it is — "$1,234.56, as of 4 Jun 2025" — a sentence that stays true forever.
//
// ── DECISION 1: extend an existing read, do not add one. ────────────────────────────────────
// #1591 asks whether a "detail read" should fan out to snapshots. There is no detail read to
// extend: apps/web/src/features/accounts/AccountDetailPage.tsx has no single-account endpoint to
// call and instead fetches GET /api/v1/accounts with `status=all_statuses` and finds the row
// client-side (its own comment records why). The accounts LIST is the detail read, so the choice
// collapses — and the cost objection to a list read collapses with it, because this is a
// LEFT JOIN LATERAL and not a fan-out: one query, one round trip, one index probe per terminal
// row on idx_balance_snapshots_account_at (account_id, snapshot_at DESC), which is exactly the
// as-of index. An active account probes nothing: `a.status <> 'active'` sits INSIDE the lateral's
// WHERE, so the subquery yields no row and both columns come back NULL.
//
// ── DECISION 2: exactly ONE of the ten account reads carries it. ────────────────────────────
// GetAccountsWithInstitutionFiltered, and only when the caller opts in. Two separate rules put
// the other nine out of scope, and they are different rules:
//
//	(a) No renderer. GetByIDAndUser, GetByUserAccountIDAndUser, GetByLinkedItemID{,IncludingRetained},
//	    GetByUserID, GetByUserAccountAndStatus, GetByUserAccountID and GetAccountsWithInstitution
//	    feed sync, ownership checks, boost validation and savings progress. None of them reaches a
//	    surface that could print a date beside the number, and an unlabelled historical figure in a
//	    struct that also has CurrentBalance is precisely the confusion this field exists to avoid.
//	(b) Wrong universe, and this one is the load-bearing half. GetForHouseholdScopeLiveAsOf — and
//	    GetAccountsWithInstitutionFiltered's OWN as-of mode (accountStatusLiveAsOf, used by
//	    dashboard_service's baseline) — resolve the account universe at a PAST instant via
//	    accountLiveAsOfPredicate. "The latest snapshot ever recorded" is not an as-of answer: for a
//	    read anchored to 4 Jun it would hand back a figure from 20 Aug and label it with 20 Aug,
//	    inside a response whose every other number is dated 4 Jun. That is the blend every
//	    regression in this area has been. It is refused mechanically, not by convention:
//	    GetAccountsWithInstitutionFiltered returns ErrAccountFilterLastKnownAsOfConflict when both
//	    are set, so a caller cannot combine them by accident. An as-of last-known balance is a real
//	    question — it wants `snapshot_at <= $asOf` on this same lateral — and it is deliberately
//	    not answered here, because no caller asks it yet and an unasked half-answer is how the
//	    universes got blended before.
//
// ── DECISION 3: the no-snapshot rule. ───────────────────────────────────────────────────────
// A terminal account with zero snapshots yields NULL for BOTH columns, which `omitempty` drops
// from the JSON entirely, and the UI renders no caption at all — the Closed badge and its closure
// date (getAccountClosedDate) still show, unattached to any balance claim, exactly as #1569 T7
// left them. Three states, three renderings, all distinguishable:
//
//	closed, snapshot on record        → "Last known balance: $1,234.56 · as of 4 Jun 2025"
//	closed, snapshot recorded a zero  → "Last known balance: $0.00 · as of 4 Jun 2025"   ← a FACT
//	closed, no snapshot ever          → no last-known line; nothing is claimed
//
// The middle row is why NULL and zero must not be conflated, and it is not hypothetical: an
// account emptied before it was closed has a genuine 0.00 snapshot. `null < 1` and `null > 100`
// are BOTH false, so any comparison that reaches a NULL admits it to every range while reading as
// correct — the trap utils.ts's balanceMatchesRange documents. Nothing here compares these values
// at all, which is the point of Decision 2's split.
//
// ── WHY holding_snapshots IS NOT CONSULTED. ─────────────────────────────────────────────────
// #1591 asks about investment accounts. They are already covered: transactionSyncService's
// snapshot pass walks EVERY active account returned by Plaid /accounts/get, whatever its type, so
// an investment account accrues balance_snapshots rows like any other. holding_snapshots is a
// different grain — one row per security position, with no account-level total — so reading it
// here would mean summing positions into a figure that no single row asserts, at prices frozen on
// different days. balance_snapshots already holds the account-level number Plaid itself reported.
//
// ── GUARD COMPATIBILITY, stated so a reviewer can check rather than trust. ──────────────────
// TestNoAccountProjectionCarriesATerminalBalance flags any `current_balance` / `available_balance`
// in an account-shaped select list outside accountBalanceProjection. The lateral renames inside
// its OWN select list (`bs.current_balance AS amount`), whose list carries no `plaid_account_id`
// marker and is therefore judged separately; the outer list names only `lkb.amount` / `lkb.at`.
// TestNoRawAccountBalanceReadWithoutTheCurrentStateLeg is keyed on the alias that carries the
// balance column, and that alias here is `bs` over `balance_snapshots` — history, the seam the
// whole ruling turns on — not `a` over `accounts`.
// ── DECISION 4: BEFORE IT CLOSED, and the bound is read, not assumed (#1591 fix round). ─────
// "Last known balance … as of 4 Jun 2025" claims a figure the account held while it was still
// live. The first draft asked only for the NEWEST snapshot on a non-active account, and nothing
// serialises snapshot insertion against the status transition: transactionSyncService reads the
// account list, the user disconnects mid-run, and the sync's snapshot pass then writes a row dated
// AFTER the closure. balanceSnapshotRepository.Create has no idea; there is no status check on the
// write path and there should not be, because the row is a real observation and #1569 retains it.
// So the READ carries the constraint instead, and the bound comes from the same
// account_status_history the rest of this file's liveness rules come from: the OPEN interval
// (valid_to IS NULL) is the account's current, terminal one, and its valid_from is the instant it
// stopped being live.
//
// `<=`, not `<`. The interval is half-open — terminal AT valid_from — but a snapshot stamped in
// the same instant as the transition is a measurement of the account as it closed, not after it,
// and admitting it costs nothing a user could misread. A snapshot one tick later is refused.
//
// FAILS CLOSED, and that is deliberate. If an account somehow has no open interval — a status
// column written by something that bypassed the trigger, a row the 000152 backfill never reached —
// the scalar subquery is NULL, the comparison is NULL, the lateral yields no row, and BOTH columns
// come back NULL. That is Decision 3's existing no-snapshot rendering (no caption at all), not a
// new empty state and not an unbounded figure. `ash.status <> 'active'` on the subquery is the
// same reflex: if the open interval disagrees with accounts.status, no bound is derived and
// nothing is claimed.
//
// ── DECISION 5: EVERY closed window, not just the current one (#1569 round 6, G-D). ─────────
// Decision 4's bound is an upper one and nothing else, and an account can close more than once.
// After active → removed → active → removed it admits everything before the SECOND removal —
// including the post-first-closure race row Decision 4 exists specifically to exclude. If the
// second live period recorded no snapshot of its own, that row became "the last known balance
// before it closed": precisely the blend the decisions above were written to prevent. Reproduced
// against real Postgres before it was fixed, in
// TestAccountRepository_LastKnownBalance_AcrossARelink_Integration; the single-closure fixture
// could not see it because it closes each account exactly once and never restores one.
//
// ⚠ THE NARROW REPAIR IS WRONG, and the test separates the two. Bounding from below at the LAST
// ACTIVE interval's valid_from does kill the race row — and kills every measurement taken during
// an EARLIER live period with it, which on a re-linked-and-re-closed account is usually the only
// history there is. Answering "we recorded nothing" when a real, dated, retained figure exists is
// the wrong direction on the surface whose thesis is that history is retained. So the rule is
// stated as what it actually means: a snapshot is admissible unless it falls inside a window
// during which the account was not live. The `NOT EXISTS ... closed` leg below is that predicate,
// and its boundaries agree with Decision 4's — strictly AFTER a closure instant is excluded (a
// snapshot AT the closure is a measurement of the account as it closed, which the
// `closedAtTheInstant` case pins), and a snapshot at the re-open instant, `valid_to`, is
// admitted.
//
// ⚠ THE UPPER BOUND STAYS, and is not redundant belt-and-braces. The open interval's arm of that
// NOT EXISTS covers the same rows, but only WHEN THERE IS AN OPEN INTERVAL. A NOT
// EXISTS over an empty history admits everything; Decision 4's scalar subquery yields NULL and
// admits nothing. Fail-closed is the behaviour that must survive a row the 000152 backfill never
// reached, so the two are kept together deliberately.
const lastKnownBalanceLateral = `
	LEFT JOIN LATERAL (
	    SELECT bs.current_balance AS amount, bs.snapshot_at AS at
	    FROM balance_snapshots bs
	    WHERE bs.account_id = $ALIAS.id
	      AND $ALIAS.status <> 'active'
	      AND bs.snapshot_at <= (
	          SELECT ash.valid_from
	          FROM account_status_history ash
	          WHERE ash.account_id = $ALIAS.id
	            AND ash.valid_to IS NULL
	            AND ash.status <> 'active'
	      )
	      AND NOT EXISTS (
	          SELECT 1
	          FROM account_status_history closed
	          WHERE closed.account_id = $ALIAS.id
	            AND closed.status <> 'active'
	            AND bs.snapshot_at > closed.valid_from
	            AND (closed.valid_to IS NULL OR bs.snapshot_at < closed.valid_to)
	      )
	    ORDER BY bs.snapshot_at DESC
	    LIMIT 1
	) lkb ON TRUE`

// lastKnownBalanceLateralSQL renders lastKnownBalanceLateral for the accounts alias of one caller.
func lastKnownBalanceLateralSQL(alias string) string {
	return strings.ReplaceAll(lastKnownBalanceLateral, "$ALIAS", alias)
}

// lastKnownBalanceProjection is the pair of select-list columns lastKnownBalanceLateral feeds.
// Kept beside it so the two cannot be added independently — a projection with no lateral is a
// syntax error, and a lateral with no projection is dead weight nobody would notice.
const lastKnownBalanceProjection = "lkb.amount AS last_known_balance, lkb.at AS last_known_balance_at"

// ─── #1569 T9.1 item A: a SUPERSEDED predecessor must not be counted beside its successor ────
//
// THE DEFECT RETENTION CREATED. accountLiveOnDateClause above answers "was this PHYSICAL account
// live at any point during this day", and it is right to. But a re-link produces TWO physical
// `accounts` rows for ONE logical account, and on the changeover day BOTH are live: the
// predecessor until the switch, the successor after it. Summing "every account live that day"
// therefore counted one $5,000 portfolio as $9,000 — a doubling on the single day a user changes
// brokers, in a curve whose whole purpose is to be compared day to day. Measured, in Upsert and
// again in the repair's reconstruction, before it was fixed.
//
// It was not a bug before this branch. The old re-link path DELETED the predecessor's holdings, so
// "everything live that day" could only ever match one row's worth of money. Retaining them is
// what made the aggregate double, which is why the fix belongs here and NOT in the liveness
// predicate — narrowing liveness to "active right now" or "live at midnight" would re-break the
// two cases the long comment above exists to protect.
//
// ⚠⚠ WHY THIS IS NOT "ONE ROW PER user_account_id", WHICH IS WHAT I WROTE FIRST AND HAD TO UNDO.
//
// The balance side dedups exactly that way — `COALESCE(user_account_id, account_id)`, keep the
// latest (balance_snapshot_repository.go, and dashboard_service.go's
// TestGetBalanceSummary_PeriodComparison_DedupKeepsLatestInChain) — and reusing a rule that already
// exists beats inventing one. So the first version of this fix copied it. It rested on an
// invariant this file's own household comment states as fact: that "two 'active' accounts rows
// [sharing] one user_account_id ... the Plaid re-link handler guarantees never happens".
//
// THAT INVARIANT IS FALSE, and it was measured rather than argued. userAccountService.MatchOrCreate
// keys on (user_id, institution_id, MASK) and nothing else, and accountRepository's lookup treats a
// NULL mask as a value that matches every other NULL mask at that institution
// (FindByInstitutionAndMask's `mask_blind_idx IS NULL` branch). Probed against real Postgres: two
// MatchOrCreate calls for one institution with no mask — a brokerage and an IRA at the same broker,
// an entirely ordinary Plaid shape — return the SAME user_accounts row, as do two accounts that
// merely share a mask. Both accounts are then live, simultaneously, under one chain key. A
// one-row-per-chain rule silently drops one of them, which HALVES that user's portfolio: a bigger
// money error, in the opposite direction, than the doubling it was written to fix.
//
// ⚠ The balance side is dedup'ing these same accounts by the same key and therefore has the same
// hole. That is NOT fixed here (out of scope, and its shape differs — it dedups snapshot rows, not
// accounts), but nothing downstream should read the balance rule as validated by this file any
// more.
//
// THE RULE THAT IS ACTUALLY TRUE. A re-link predecessor is not merely "another row with the same
// chain key". It is a row whose LINKED ITEM was replaced by the item its sibling belongs to, and
// that is a recorded fact: account_service's re-link stamps linked_items.replaced_by_id on the old
// item, pointing at the new one. Two coexisting maskless accounts share ONE linked item and can
// never satisfy that test. So:
//
//	Exclude X from day D when a sibling Y is also live on D, Y belongs to the linked item that
//	replaced X's linked item, and Y has an observation.
//
//	1. same chain — plain SQL equality on user_account_id, so a legacy NULL matches NOTHING. Here
//	   that is the wanted behaviour, not a hazard to COALESCE away: two identity-less accounts have
//	   no recorded relationship and must both count.
//	2. item succession — `linked_items.replaced_by_id`. ⚠ NOT account_status_history intervals,
//	   which was this fix's SECOND wrong version: the status trigger stamps clock_timestamp(), and
//	   the successor's interval opens ~1 ms before the predecessor's closes, so "s began at or after
//	   l ended" simply does not hold. It fails in BOTH orderings, and the two orderings both exist —
//	   ExchangePublicToken marks the predecessor before inserting the successor, while item B's
//	   CreateBatch backstop supersedes it after. Measured, not reasoned: the intervals were printed.
//	   ⚠ NOT `accounts.replaced_by_id` ALONE, which reads like the obvious column. The production
//	   re-link passes nil for it (account_service.go's UpdateStatus call), so the re-link itself
//	   never writes it; the ONLY writer is CreateBatch's supersedeStrandedChainSiblings, which
//	   runs on every CreateBatch and pairs each predecessor of the chain with one of the batch's
//	   own new rows. So the column is present on a re-link that reached CreateBatch and absent on
//	   one that did not, and neither pointer is a substitute for the other — leg 2 reads BOTH.
//	   ⚠ It is no longer true, as this comment said for three rounds, that the account pointer is
//	   universally absent.
//	   ⚠ NOT current status, on either row. A past day must not be re-judged by what the accounts
//	   look like today — the same trap the liveness comment above exists for.
//	3. Y has an observation. Without this leg the window between a re-link and the successor's
//	   first sync writes ZERO for the day — a cliff in the curve on a day the user's money did not
//	   move — because the successor exists but holds nothing yet. Pinned by
//	   TestPortfolioSnapshot_Upsert_RelinkBeforeTheSuccessorHasSynced_KeepsThePredecessor.
//
// ⚠ WHAT THIS DOES NOT COVER, stated rather than left for a reader to discover. The two writes in
// account_service.go step 5b — the old item's `replaced_by_id` and the predecessor account's
// status — are INDEPENDENT fail-open blocks, so there are two single-write failures, not one:
//
//   - 7b, both fail: the predecessors stay 'active', and CreateBatch's backstop retires them and
//     writes the account pointer inside the transaction that inserts the successor.
//   - 7a, only the ITEM write fails: the predecessor is already 'replaced' with a NULL pointer,
//     which a `status = 'active'` backstop could not see at all. No item pointer, no account
//     pointer, no succession row — full doubling on a plain 1:1 chain, and the likelier of the two
//     failures. #1569 T10.1 item B: the backstop now also fills the pointer on a chain row left
//     'replaced' with none, which changes no status and therefore no current-state read.
//
// ⚠ What remains open is narrower: a re-link whose item write fails and which then never REACHES
// CreateBatch (a later Plaid call or MatchOrCreate refuses the link first). Nothing has recorded
// the succession and nothing later will, so both siblings are counted until the chain is re-linked
// again. It is smaller than the window before this task and it is real.
//
// ⚠ "Absence of holdings is not evidence of an empty portfolio" is the deliberate reading of leg 3,
// and it agrees with the repair's own ruling that a day with no rows is unreconstructable rather
// than zero. A CONFIRMED empty successor is representable — that is what holding_snapshots.present
// is for — and it reaches the repair through leg 3 being satisfied (rows exist) with nothing
// present to sum.

// accountChainLiveCTE renders the per-day live set an aggregate may sum over: every account of the
// user live on the day, MINUS those a sibling superseded. It reuses accountLiveOnDateClause
// unchanged — the boundary rule stays in exactly one place.
//
// $DAYCOL / $FROM / $WHERE supply the caller's candidate (day, account) pairs, $DATE names the day
// for the liveness test, and $OBSERVED is the caller's has-an-observation test for the SUCCESSOR
// (alias `s`): current holdings or today's observation record for Upsert, that day's
// holding_snapshots or observation record for the repair.
//
// ⚠ CALLERS MUST OPEN THEIR WITH LIST WITH `WITH RECURSIVE`. The succession walk below is a
// recursive CTE, and Postgres takes RECURSIVE from the WITH keyword, not from the CTE.
//
// It is deliberately NOT a DISTINCT ON. Picking one winner per chain key is the rule that halves a
// maskless pair; every unsuperseded account survives here, however many share a key.
//
// ── THREE THINGS THIS DOES THAT THE FIRST CORRECT VERSION DID NOT ──────────────────────────────
//
//  1. IT COUNTS. The first version excluded a predecessor whenever SOME observed successor shared
//     its chain key under the replacing item. One old item can hold TWO maskless accounts that
//     MatchOrCreate gave the SAME user_account_id (the collision documented at length above), and
//     if the successor item reports only ONE of them, both old rows satisfied that test — so the
//     account nothing replaced was deleted from the total too. Probed against real Postgres:
//     $6,500 of retained holdings reported as $5,000. Succession is now RATIONED: within one
//     (day, predecessor item, chain key) group, at most as many predecessors are excluded as there
//     are surviving observed successors, ordered by account id so the choice is deterministic and
//     stable across runs. Which of two indistinguishable predecessors is kept is arbitrary; HOW
//     MANY are kept is not.
//
//  2. IT WALKS THE WHOLE CHAIN. The first version looked exactly ONE edge ahead. For A → B → C on
//     one day with B never synced, A saw its direct successor B unobserved and stayed in while B
//     was excluded by C — so A and C were both summed, which is the original doubling one link
//     further along ($9,000 for a $5,000 portfolio, measured).
//
//     ⚠ THE WALK IS NO LONGER DEPTH-CAPPED, and that is #1569 T10.1 item F. It used to stop at 16
//     edges, with `depth` carried inside the recursive tuple — which bounded the closure AND made
//     the UNION dedup weaker than the comment beside it claimed, since (pred, succ, 1) and
//     (pred, succ, 2) are distinct rows. Nothing anywhere proves a chain cannot exceed 16 links,
//     and a 17-edge same-day chain let its first and last member both count. Over a FINITE set of
//     accounts a UNION closure that does NOT carry depth converges on its own, so removing depth
//     removes the cap and restores the dedup at the same time. A cyclic pointer then terminates
//     rather than looping (measured:
//     TestPortfolioSnapshot_Upsert_CyclicSuccessionPointer_TerminatesAndCountsEverything), and a
//     20-edge chain counts once (TestPortfolioSnapshot_Upsert_ChainLongerThanTheOldRecursionCap_StillCountsOnce).
//     A cycle rations nobody — corrupt pointers must not delete a user's money.
//
//     ⚠ THAT LAST SENTENCE USED TO BE TRUE OF ONE TOPOLOGY AND STATED OF ALL OF THEM (#1569
//     T10-FIX item 4). For a closed A ↔ B it held for free: each member has a successor, so
//     neither is a survivor and nothing is rationed. Add one edge leaving the cycle and it broke —
//     A → B, B → A, B → C makes A → C derivable, C survives, and both cycle members are rationed
//     away. Measured: $11,000 reported as $2,000. The property is now ENFORCED rather than
//     emergent, by the `cyclic` CTE, which withholds every account on a cycle from `succession`.
//
//     ⚠ HONESTY ABOUT THAT TEST'S REACH. It was RED against the pre-fix single-layer walk, which
//     is what it was written for. Re-inserting a 16-cap into item_closure alone does NOT turn it
//     red again, because the account-level closure in (3) composes capped item paths into longer
//     ones — 20 edges are reachable as two hops of ten. That is a real robustness property of the
//     two layers, not evidence the cap was harmless; the cap is removed because nothing bounds a
//     chain's length and a comment claiming completeness beside a silent truncation is worse than
//     either.
//
//     ⚠ A transitive successor that is ITSELF superseded must not be counted as a survivor, or the
//     rationing in (1) inflates: A1/A2 → B → C would see two successors for one old item and
//     exclude both predecessors again. `surviving` drops any successor that has an observed
//     successor of its own.
//
//  3. IT ACCEPTS THE ACCOUNT-LEVEL POINTER TOO, AND COMPOSES THE TWO KINDS OF EDGE. The item
//     pointer is written fail-open (account_service.go step 5b logs and continues), and
//     CreateBatch's backstop — supersedeStrandedChainSiblings — repairs the ACCOUNT row, setting
//     accounts.replaced_by_id, not the item row. So on the failure path the item-level test finds
//     nothing and both rows are counted. Reading either recorded pointer closes that window, and
//     leg 1 (same chain key) still gates both.
//
//     ⚠ #1569 T10.1 item D: reading either one was NOT enough, because a chain need not use the
//     same pointer type throughout. A → B recorded only by the item pointer and B → C only by the
//     account pointer used to leave A and C both summed: each pointer type closed transitively
//     WITHIN itself, and nothing composed an item edge with an account edge. The walk is now built
//     in two layers — item_closure over linked_items.replaced_by_id, then succ_closure over
//     ACCOUNT-level edges that accept either an account pointer or an item_closure hop — so a
//     mixed chain is closed like any other.
//
//     ⚠ The two layers are BOTH needed and neither subsumes the other. item_closure alone cannot
//     see an account pointer. succ_closure alone cannot step through a link whose successor item
//     reported no account for the chain at all: there is no account row to hop through, and the
//     item layer supplies that edge directly.
//
//     ⚠ Both closures range over every account sharing the chain key, whatever its status and
//     whatever day it was live on. Succession is a recorded fact about two rows, not a per-day
//     fact; only the (pred, succ) pairs in `succession` are restricted to accounts live on the
//     day. Scoping the WALK to a day's live set is what stopped it stepping through an
//     intermediate that was not live.
//
//     ⚠ It is FALSE, as this comment said for three rounds, that "both pointers are written only
//     by a re-link". supersedeStrandedChainSiblings runs on EVERY CreateBatch and keys on the
//     chain alone, so a batch that is not a re-link at all can write the account pointer. That is
//     why it retires at most as many predecessors as the batch brought successors for the chain
//     (item E): the chain key is not by itself evidence that a live sibling was replaced.
//
// ⚠ KNOWN, AND STATED RATHER THAN LEFT TO BE DISCOVERED. The rationing groups by the PREDECESSOR'S
// linked item. Two DIFFERENT live items whose accounts share one chain key, both replaced by one
// successor, are two groups of one, so both predecessors are excluded against the same single
// successor. The re-link path replaces exactly one existing item per link, so producing that shape
// takes two separate re-links of the same chain in one day landing on one successor; it is not
// reachable from ExchangePublicToken as written. Grouping by chain key alone instead would be
// worse and was rejected: it collapses A → B → C into one group and keeps one of A/B.
// accountChainLiveAllCTE is the LIVENESS half: the caller's candidate (day, account) pairs,
// narrowed to accounts recorded as live on the day. It is the half the BALANCE reads do NOT use —
// a balance snapshot is itself the record that the account reported on that day, and re-asking
// account_status_history would delete a closed account's retained history from the chart, which
// is the opposite of what #1569 exists to do.
const accountChainLiveAllCTE = `
	live_all AS (
	    SELECT $DAYCOL AS snapshot_date, a.id AS account_id, a.user_id AS owner_id,
	           a.user_account_id, a.linked_item_id
	    FROM $FROM
	    WHERE $WHERE
	      AND $LIVENESS
	),`

// accountChainSupersessionCTE is the SUCCESSION half, split out in #1569 T10.2 so the balance
// reads can apply the SAME rule over a live_all of their own. It consumes a `live_all` relation
// with columns (snapshot_date, account_id, owner_id, user_account_id, linked_item_id) and emits
// `superseded` (snapshot_date, account_id) plus `live` (live_all minus superseded).
//
// ⚠ THE SPLIT IS THE POINT. There is ONE succession rule in the backend and this is it; a second
// copy is how the balance side and the portfolio side came to disagree about the same two rows in
// the first place. A caller supplies only what genuinely differs — what its candidate rows are,
// and what "the successor was observed" means for its own data.
const accountChainSupersessionCTE = `
	-- Every account sharing a chain key with something live on some day here, WHATEVER its status
	-- and whatever day it was live on. The walk below runs over this set rather than over the
	-- day's live set: succession is a recorded fact about two rows, not a per-day fact, and
	-- scoping the walk to one day is what stopped it stepping through an intermediate that was
	-- not live that day. Only the finished (pred, succ) pairs are day-restricted.
	chain_accounts AS (
	    SELECT a.id, a.user_account_id, a.linked_item_id, a.replaced_by_id
	    FROM accounts a
	    WHERE a.user_id IN (SELECT owner_id FROM live_all)
	      AND a.user_account_id IN (
	          SELECT user_account_id FROM live_all WHERE user_account_id IS NOT NULL)
	),
	-- Layer 1: transitive closure of linked_items.replaced_by_id. "<> id" refuses a self-edge.
	-- NO depth column: over a finite set of items a UNION closure converges on its own, and
	-- carrying depth both capped the walk (item F) and made this UNION dedup weaker than it looks,
	-- since (pred, succ, 1) and (pred, succ, 2) are distinct rows.
	item_closure AS (
	    SELECT li.id AS pred_item_id, li.replaced_by_id AS succ_item_id
	    FROM linked_items li
	    WHERE li.replaced_by_id IS NOT NULL
	      AND li.replaced_by_id <> li.id
	      AND li.id IN (SELECT linked_item_id FROM chain_accounts)
	  UNION
	    SELECT x.pred_item_id, li.replaced_by_id
	    FROM item_closure x
	    JOIN linked_items li ON li.id = x.succ_item_id
	    WHERE li.replaced_by_id IS NOT NULL
	      AND li.replaced_by_id <> li.id
	      AND li.replaced_by_id <> x.pred_item_id
	),
	-- Layer 2, one ACCOUNT-level edge. Leg 1 — same logical account, plain equality so a legacy
	-- NULL matches nothing — gates both succession routes. Either recorded pointer supplies an
	-- edge, which is what lets a chain switch pointer type half way along (item D).
	succ_edge AS (
	    SELECT p.id AS pred_id, c.id AS succ_id
	    FROM chain_accounts p
	    JOIN chain_accounts c
	      ON c.user_account_id = p.user_account_id
	     AND c.id <> p.id
	    WHERE c.id = p.replaced_by_id
	       OR EXISTS (SELECT 1 FROM item_closure x
	                  WHERE x.pred_item_id = p.linked_item_id
	                    AND x.succ_item_id = c.linked_item_id)
	),
	-- Layer 2's transitive closure, again with no depth: this is the one that composes an item
	-- edge with an account edge.
	succ_closure AS (
	    SELECT pred_id, succ_id FROM succ_edge
	  UNION
	    SELECT x.pred_id, e.succ_id
	    FROM succ_closure x
	    JOIN succ_edge e ON e.pred_id = x.succ_id
	    WHERE e.succ_id <> x.pred_id
	),
	-- Accounts that lie on a SUCCESSION CYCLE — X reaches Y and Y reaches X. A cycle is corrupt
	-- data: it asserts that two accounts each replaced the other, and no reading of it is true.
	-- Members are withheld from the succession relation entirely, so a cycle rations nobody.
	--
	-- ⚠ #1569 T10-FIX item 4. The cycle-safety property was previously an EMERGENT one — a closed
	-- A ↔ B produces no surviving pair, because each member has a successor of its own — and it
	-- held only for that topology. Add one real edge leaving the cycle (A → B, B → A, B → C) and
	-- the closure also derives A → C; C supersedes nobody, so (A,C) and (B,C) are both surviving
	-- pairs and BOTH cycle members are rationed away. Measured against Postgres, not reasoned: a
	-- $11,000 portfolio reported as $2,000, with $9,000 deleted by a pointer nobody can act on.
	-- Pinned by TestPortfolioSnapshot_Upsert_CycleWithAnOutgoingTail_StillRationsNobody.
	--
	-- ⚠ THE COST, STATED. B → C may be a perfectly real re-link, and withholding B keeps its value
	-- beside C's — an over-count. That is the deliberate direction: this whole family exists
	-- because succession rules DELETE money when they are wrong, and a corrupt pointer is exactly
	-- when the rule cannot be trusted to be right. An over-count is visible and repairable; a
	-- silently missing account is neither.
	cyclic AS (
	    SELECT DISTINCT x.pred_id AS account_id
	    FROM succ_closure x
	    JOIN succ_closure y
	      ON y.pred_id = x.succ_id
	     AND y.succ_id = x.pred_id
	),
	-- Every (predecessor, observed successor) pair on a day, both members live on that day.
	succession AS (
	    SELECT DISTINCT l.snapshot_date,
	           l.linked_item_id AS pred_item_id,
	           l.user_account_id,
	           l.account_id AS pred_id,
	           s.account_id AS succ_id
	    FROM live_all l
	    JOIN live_all s
	      ON s.snapshot_date = l.snapshot_date
	     AND s.account_id <> l.account_id
	     AND s.user_account_id = l.user_account_id
	    WHERE EXISTS (SELECT 1 FROM succ_closure x
	                  WHERE x.pred_id = l.account_id
	                    AND x.succ_id = s.account_id)
	      AND NOT EXISTS (SELECT 1 FROM cyclic z WHERE z.account_id = l.account_id)
	      AND $OBSERVED
	),
	-- A successor that is itself superseded by an observed successor is not a survivor and must
	-- not be counted as one — see (1)/(2) above.
	surviving AS (
	    SELECT p.*
	    FROM succession p
	    WHERE NOT EXISTS (
	        SELECT 1 FROM succession q
	        WHERE q.snapshot_date = p.snapshot_date
	          AND q.pred_id = p.succ_id
	    )
	),
	surviving_counts AS (
	    SELECT snapshot_date, pred_item_id, user_account_id,
	           count(DISTINCT succ_id) AS n_surviving
	    FROM surviving
	    GROUP BY snapshot_date, pred_item_id, user_account_id
	),
	superseded AS (
	    SELECT r.snapshot_date, r.pred_id AS account_id
	    FROM (
	        SELECT DISTINCT snapshot_date, pred_item_id, user_account_id, pred_id,
	               dense_rank() OVER (
	                   PARTITION BY snapshot_date, pred_item_id, user_account_id
	                   ORDER BY pred_id
	               ) AS rk
	        FROM surviving
	    ) r
	    JOIN surviving_counts c
	      ON c.snapshot_date = r.snapshot_date
	     AND c.pred_item_id = r.pred_item_id
	     AND c.user_account_id = r.user_account_id
	    WHERE r.rk <= c.n_surviving
	),
	live AS (
	    SELECT l.snapshot_date, l.account_id
	    FROM live_all l
	    WHERE NOT EXISTS (
	        SELECT 1 FROM superseded x
	        WHERE x.snapshot_date = l.snapshot_date
	          AND x.account_id = l.account_id
	    )
	)`

// accountChainLiveCTE is the two halves joined, for the callers that want both.
const accountChainLiveCTE = accountChainLiveAllCTE + accountChainSupersessionCTE

// accountChainLiveCTESQL renders accountChainLiveCTE for one caller.
func accountChainLiveCTESQL(dayCol, fromClause, whereClause, dateExpr, observedExpr string) string {
	return strings.NewReplacer(
		"$DAYCOL", dayCol,
		"$FROM", fromClause,
		"$WHERE", whereClause,
		"$LIVENESS", accountLiveOnDateSQL("a.id", dateExpr),
		"$OBSERVED", observedExpr,
	).Replace(accountChainLiveCTE)
}

// accountChainSupersessionCTESQL renders the succession half alone, for a caller that supplies its
// own `live_all` CTE ahead of it (and, like every caller, opens its WITH list with RECURSIVE).
//
// observedExpr is the caller's "the successor was observed" test on alias `s`. A caller whose
// live_all is BUILT FROM its own observations — the balance reads, whose candidate rows are
// snapshots — passes TRUE, because membership of live_all already is the observation.
func accountChainSupersessionCTESQL(observedExpr string) string {
	return strings.NewReplacer("$OBSERVED", observedExpr).Replace(accountChainSupersessionCTE)
}
