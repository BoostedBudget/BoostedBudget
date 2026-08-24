package repository

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// Issue #1569 Task 19 — Guard G1: no new ON DELETE CASCADE into a history-bearing table.
//
// WHAT #1569 DECIDED. Financial history is retained unless the user explicitly asks for it to go.
// Eighteen tasks turned that into schema: four destructive config DELETEs became tombstones,
// transactions became tombstoned, accounts became soft-removed, and nine history tables were either
// created or detached from the delete paths that used to take them with them. Every one of those
// decisions is expressed, in the end, as the `ON DELETE` action on one foreign key. A single new
// `ON DELETE CASCADE` written into a future migration silently reverts whichever of them it touches,
// and nothing else in the suite would notice: the row simply is not there any more, and the tests
// that would have read it were never written.
//
// THIS IS NOT TestDeleteCascadeMapMatchesLiveSchema. That test (cache_completeness_cascade_
// integration_test.go) is about CACHE INVALIDATION COMPLETENESS — whether a DELETE reaches a table
// whose reads are cached. It filters `confdeltype IN ('c','n','d')`, so it deliberately lumps
// CASCADE together with SET NULL and SET DEFAULT, it is indexed by PARENT rather than by the edge,
// and it has no notion of a history-bearing table at all. It would stay green for every violation
// this guard exists to catch. Do not merge the two.
//
// THE RULE. For every foreign key whose CHILD is a history-bearing table and whose delete action is
// CASCADE ('c'), there must be an entry in `allowedHistoryCascades` giving the reason. Both
// directions fail:
//
//   - an edge with no entry is the DANGEROUS direction — history that a parent delete now destroys;
//   - an entry with no edge is ALLOWLIST ROT — it makes the allowlist read as though a decision were
//     still in force when the schema has moved on, which is how the next person justifies adding one.
//
// WHY THE CHILD AND NOT THE PARENT. "Cascade into a history table" is a property of the history
// table's own FK. A transitive path (delete P -> cascade to I -> cascade to H) still has to traverse
// H's own FK to I, so the direct-edge check sees it. Indexing by parent, as the cache map does, would
// not.
//
// THE ALLOWLIST IS NOT A LIST OF MISTAKES. Most entries below are load-bearing and must stay:
// GDPR erasure runs off the `users` FK graph (`docs/decisions/2026-08-01-gdpr-erasure-hard-delete.md`),
// and for three tables the CASCADE named here is their ONLY route to erasure. A guard that pushed
// someone into removing those would strand personal data forever, which is a worse defect than the
// one it prevents. Read the reason before you touch the edge.
//
// Mutation-checked: see TestHistoryCascadeGuard_* below for the constructed violations, and
// task-19-report.md for the live-schema mutation (a real CASCADE FK added to the test database)
// that turns TestNoUnallowlistedCascadeIntoHistoryTable red.

// fkCascadeEdge is one `ON DELETE CASCADE` foreign key: the CHILD table carries the constraint and
// is what gets deleted when a row in PARENT goes.
type fkCascadeEdge struct {
	child      string
	parent     string
	constraint string
}

func (e fkCascadeEdge) String() string {
	return fmt.Sprintf("%s -> %s (%s)", e.child, e.parent, e.constraint)
}

// historyBearingTables is the set #1569 established, with what each one is FOR. The value is used in
// the failure message, so that whoever trips this guard is told what they are about to destroy
// rather than just which constraint name was unexpected.
//
// Two kinds of table are in here:
//
//   - the history tables proper — append-only records of what was true on a day, which cannot be
//     reconstructed once deleted because the upstream provider no longer reports them;
//   - the TOMBSTONED and SOFT-REMOVED parents. These belong here for a different reason: they are
//     not deleted at all any more, so a CASCADE that hard-deletes one defeats the tombstone, and
//     takes its own history children with it in the same statement.
var historyBearingTables = map[string]string{
	// History proper.
	"account_status_history":     "the per-day record of whether an account was live; the liveness baseline reads it",
	"balance_snapshots":          "daily account balances; the only record of what a balance was on a past day",
	"holding_snapshots":          "daily investment positions; Plaid does not backfill them",
	"liability_statements":       "captured statement terms and APRs; the provider reports only the current one",
	"portfolio_snapshot_repairs": "the audit trail of snapshot repairs",
	"rule_execution_history":     "what an automation rule did and when",
	"boost_executions":           "what a boost moved and when",
	"rollover_history":           "budget rollovers between periods",
	"savings_goal_contributions": "the contributions that make up a goal's progress",
	// Tombstoned / soft-removed parents (#1569 tasks 5-8, 12).
	"transactions":      "tombstoned, not deleted, since #1569 task 7",
	"savings_goals":     "tombstoned since #1569 task 8",
	"boosts":            "tombstoned since #1569 task 8",
	"budgets":           "tombstoned since #1569 task 8",
	"budget_categories": "tombstoned since #1569 task 8",
	"custom_categories": "tombstoned; a re-categorisation history depends on the row surviving",
	"accounts":          "soft-removed (removed_at); a disconnected account keeps its history",
}

// deliberatelyNotHistoryBearing records the tables someone will reasonably MISTAKE for history and
// then "fix", undoing a decision that was made on purpose. They are absent from
// historyBearingTables, so G1 says nothing about their foreign keys; this map is why.
//
// The only member today is the one that cost a task to establish. `budget_period_events` looks like
// history — it is append-only and it is named after a past event — but it is an exactly-once
// SCANNER MARKER: it records that the period-close job has already run for a budget period, so that
// it does not run twice. It has no value to the user, it must not be retained, and it has no FK to
// `users`, so the CASCADE from `budgets` is its ONLY route to GDPR erasure. Turning that edge into
// RESTRICT — the change this guard would otherwise invite — would strand it forever. #1569 task 14.
var deliberatelyNotHistoryBearing = map[string]string{
	"budget_period_events": "exactly-once scanner marker, not history; its CASCADE from budgets is " +
		"its only route to GDPR erasure and must stay CASCADE (#1569 task 14)",
}

// allowedHistoryCascades is the pinned set of CASCADE edges into those tables, each with the reason
// it is allowed to exist. Adding an entry here is a deliberate decision to let some delete destroy
// history; say why, in the same commit as the migration.
var allowedHistoryCascades = map[fkCascadeEdge]string{
	// ---- GDPR erasure off `users`. -------------------------------------------------------------
	// One real `DELETE FROM users` is the whole erasure mechanism (ADR 2026-08-01). Every one of
	// these is required: removing any of them strands personal data after a completed erasure.
	{child: "account_status_history", parent: "users", constraint: "fk_ash_user_id"}:                              "GDPR erasure path off users",
	{child: "balance_snapshots", parent: "users", constraint: "fk_balance_snapshots_user_id"}:                     "GDPR erasure path off users",
	{child: "boost_executions", parent: "users", constraint: "fk_boost_executions_user_id"}:                       "GDPR erasure path off users",
	{child: "boosts", parent: "users", constraint: "fk_boosts_user_id"}:                                           "GDPR erasure path off users",
	{child: "budgets", parent: "users", constraint: "fk_budgets_user_id"}:                                         "GDPR erasure path off users",
	{child: "custom_categories", parent: "users", constraint: "fk_custom_categories_user_id"}:                     "GDPR erasure path off users",
	{child: "holding_snapshots", parent: "users", constraint: "fk_holding_snapshots_user_id"}:                     "GDPR erasure path off users",
	{child: "liability_statements", parent: "users", constraint: "fk_liability_statements_user_id"}:               "GDPR erasure path off users",
	{child: "portfolio_snapshot_repairs", parent: "users", constraint: "portfolio_snapshot_repairs_user_id_fkey"}: "GDPR erasure path off users",
	{child: "rollover_history", parent: "users", constraint: "fk_rollover_history_user_id"}:                       "GDPR erasure path off users",
	{child: "rule_execution_history", parent: "users", constraint: "fk_rule_execution_history_user_id"}:           "GDPR erasure path off users",
	{child: "savings_goals", parent: "users", constraint: "fk_savings_goals_user_id"}:                             "GDPR erasure path off users",
	{child: "transactions", parent: "users", constraint: "fk_transactions_user_id"}:                               "GDPR erasure path off users",
	{child: "accounts", parent: "users", constraint: "fk_accounts_user_id"}:                                       "GDPR erasure path off users",

	// ---- The three tables whose ONLY erasure route is this edge. -------------------------------
	// None of these three has a foreign key to `users`, so `DELETE FROM users` reaches them only
	// through the parent named here. Task 14 found all three; removing any of them would leave rows
	// behind after a completed GDPR erasure. All three parents are themselves TOMBSTONED, so in
	// ordinary use the cascade never fires — a user deleting a budget or a goal sets deleted_at and
	// the children survive. The cascade is reachable only from the hard delete of the users row.
	{child: "budget_categories", parent: "budgets", constraint: "fk_budget_categories_budget_id"}:               "ONLY erasure path: budget_categories has no user_id FK; budgets is tombstoned so this fires only under GDPR erasure",
	{child: "savings_goal_contributions", parent: "savings_goals", constraint: "fk_contributions_savings_goal"}: "ONLY erasure path: savings_goal_contributions has no user_id column; savings_goals is tombstoned so this fires only under GDPR erasure",

	// ---- Explicit destruction: the account purge. ----------------------------------------------
	// `PurgeAccount` (account_repository.go) is the user asking, in words, for one account's data to
	// be destroyed; it refuses to run on a live account. These three edges are how the derived
	// history goes with it. See TestAccountRepository_PurgeAccount_Integration_RemovesEverythingDerived.
	{child: "account_status_history", parent: "accounts", constraint: "fk_ash_account_id"}:                "explicit account purge destroys derived history for that account",
	{child: "holding_snapshots", parent: "accounts", constraint: "fk_holding_snapshots_account_id"}:       "explicit account purge destroys derived history for that account",
	{child: "liability_statements", parent: "accounts", constraint: "fk_liability_statements_account_id"}: "explicit account purge destroys derived history for that account",

	// ---- Tombstoned-parent cascades that no ordinary delete can reach. -------------------------
	{child: "accounts", parent: "linked_items", constraint: "fk_accounts_linked_item_id"}:                         "linked_items rows are themselves purged only by an explicit disconnect-and-purge; a disconnect soft-removes and keeps the row",
	{child: "budget_categories", parent: "custom_categories", constraint: "fk_budget_categories_custom_category"}: "custom_categories is tombstoned, so this cascade fires only under GDPR erasure",
	{child: "budget_categories", parent: "savings_goals", constraint: "fk_budget_categories_savings_goal"}:        "savings_goals is tombstoned, so this cascade fires only under GDPR erasure",
}

// historyCascadeVerdict is the whole decision procedure, kept pure so that the meta-tests below can
// construct the violation each direction exists to catch and prove the guard fails on it. The live
// integration test supplies `edges` from pg_constraint; nothing else differs.
func historyCascadeVerdict(
	edges []fkCascadeEdge,
	history map[string]string,
	allow map[fkCascadeEdge]string,
) (unallowlisted []string, stale []string) {
	seen := map[fkCascadeEdge]bool{}
	for _, e := range edges {
		role, isHistory := history[e.child]
		if !isHistory {
			continue
		}
		seen[e] = true
		if _, ok := allow[e]; !ok {
			unallowlisted = append(unallowlisted,
				fmt.Sprintf("%s\n      %s is %s", e, e.child, role))
		}
	}
	for e := range allow {
		if !seen[e] {
			stale = append(stale, e.String())
		}
	}
	sort.Strings(unallowlisted)
	sort.Strings(stale)
	return unallowlisted, stale
}

// ---- Meta-tests: the constructed violations. ---------------------------------------------------

// TestHistoryCascadeGuard_FlagsACascadeIntoAHistoryTable is the case G1 exists for: someone writes a
// new migration turning `rule_execution_history.rule_id` into ON DELETE CASCADE, so deleting an
// automation rule erases the record of everything it ever did. Live today it is 'n' (SET NULL).
func TestHistoryCascadeGuard_FlagsACascadeIntoAHistoryTable(t *testing.T) {
	newCascade := fkCascadeEdge{
		child: "rule_execution_history", parent: "categorization_rules",
		constraint: "fk_rule_execution_history_rule_id",
	}
	bad, stale := historyCascadeVerdict([]fkCascadeEdge{newCascade}, historyBearingTables, allowedHistoryCascades)
	if len(bad) != 1 || !strings.Contains(bad[0], "fk_rule_execution_history_rule_id") {
		t.Fatalf("a new CASCADE into rule_execution_history must be reported, got %v", bad)
	}
	if !strings.Contains(bad[0], "what an automation rule did") {
		t.Fatalf("the report must say what the table is for, got %q", bad[0])
	}
	if len(stale) == 0 {
		t.Fatalf("the pinned allowlist entries are absent from this edge set, so all of them are stale here")
	}
}

// TestHistoryCascadeGuard_FlagsAStaleAllowlistEntry pins the other direction. An allowlist that
// outlives its edge is how the next reader concludes that destroying this history was already
// agreed.
func TestHistoryCascadeGuard_FlagsAStaleAllowlistEntry(t *testing.T) {
	gone := fkCascadeEdge{child: "balance_snapshots", parent: "users", constraint: "fk_balance_snapshots_user_id"}
	var kept []fkCascadeEdge
	for e := range allowedHistoryCascades {
		if e != gone {
			kept = append(kept, e)
		}
	}
	bad, stale := historyCascadeVerdict(kept, historyBearingTables, allowedHistoryCascades)
	if len(bad) != 0 {
		t.Fatalf("every remaining edge is allowlisted, so nothing should be unallowlisted: %v", bad)
	}
	if len(stale) != 1 || !strings.Contains(stale[0], "fk_balance_snapshots_user_id") {
		t.Fatalf("the dropped edge's allowlist entry must be reported stale, got %v", stale)
	}
}

// TestHistoryCascadeGuard_IgnoresCascadesIntoNonHistoryTables keeps the guard from becoming a
// blanket ban on CASCADE. Most of the schema's cascades are fine and are none of this guard's
// business; a guard that fires on all of them gets an allowlist entry added reflexively.
func TestHistoryCascadeGuard_IgnoresCascadesIntoNonHistoryTables(t *testing.T) {
	bad, _ := historyCascadeVerdict(
		[]fkCascadeEdge{{child: "notifications", parent: "users", constraint: "fk_notifications_user_id"}},
		historyBearingTables, allowedHistoryCascades)
	if len(bad) != 0 {
		t.Fatalf("notifications is not history-bearing; got %v", bad)
	}
}

// TestHistoryCascadeGuard_PinnedAllowlistIsSelfConsistent proves the allowlist is exactly satisfied
// by itself — so that a failure of the live test below is always about the SCHEMA, never about the
// pinned data being internally contradictory. Without this, the integration test skipping (no
// DATABASE_URL) would leave the allowlist entirely unexercised.
func TestHistoryCascadeGuard_PinnedAllowlistIsSelfConsistent(t *testing.T) {
	var edges []fkCascadeEdge
	for e := range allowedHistoryCascades {
		edges = append(edges, e)
		if _, ok := historyBearingTables[e.child]; !ok {
			t.Errorf("allowlist entry %s names a child that is not in historyBearingTables — it can "+
				"never match, so it is permanently stale", e)
		}
		if strings.TrimSpace(allowedHistoryCascades[e]) == "" {
			t.Errorf("allowlist entry %s has no reason; the reason is the point", e)
		}
	}
	bad, stale := historyCascadeVerdict(edges, historyBearingTables, allowedHistoryCascades)
	if len(bad) != 0 || len(stale) != 0 {
		t.Fatalf("pinned allowlist is not self-consistent: unallowlisted=%v stale=%v", bad, stale)
	}
}

// TestHistoryCascadeGuard_BudgetPeriodEventsStaysOutOfScope pins the decision that would otherwise be
// re-litigated by whoever reads the table's name. If someone adds it to historyBearingTables, the
// pinned allowlist has no entry for its CASCADE from budgets, G1 goes red, and the obvious repair is
// a migration to RESTRICT — which would leave the row unerasable. This test fails FIRST, at the
// point where the classification is being changed, and says why.
func TestHistoryCascadeGuard_BudgetPeriodEventsStaysOutOfScope(t *testing.T) {
	for tbl, why := range deliberatelyNotHistoryBearing {
		if _, ok := historyBearingTables[tbl]; ok {
			t.Fatalf("%s was added to historyBearingTables, but it is deliberately excluded: %s", tbl, why)
		}
	}
	bad, _ := historyCascadeVerdict(
		[]fkCascadeEdge{{child: "budget_period_events", parent: "budgets", constraint: "fk_budget_period_events_budget"}},
		historyBearingTables, allowedHistoryCascades)
	if len(bad) != 0 {
		t.Fatalf("budget_period_events must stay CASCADE and must not be flagged; got %v", bad)
	}
}
