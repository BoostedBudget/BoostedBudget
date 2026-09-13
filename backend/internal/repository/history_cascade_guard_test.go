package repository

import (
	"fmt"
	"regexp"
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
//
// THE COLUMN LISTS ARE PART OF THE KEY (#1569 T8.7 item B). Without them the pinned identity of an
// edge is only {child, parent, conname}, and a migration that DROPs and re-ADDs the same conname
// between the same pair of tables pointing at DIFFERENT columns keeps the key intact — G1 stays
// green while the edge now cascades along a path nobody allowlisted. With them, such a re-add
// produces an edge the allowlist does not contain (reported as unallowlisted) and leaves the pinned
// entry unmatched (reported as stale), so it fails in both directions at once.
//
// The FK's delete ACTION needs no pinning here: the live query filters on `confdeltype = 'c'`, so
// an edge that stops being CASCADE simply drops out of the set and is reported stale.
//
// ⚠ Derived from `conkey`/`confkey` as column NAMES, never from `pg_get_constraintdef` text. The
// deparse output is `ruleutils.c` formatting and shifts with the server version and the column
// types — see TestAccountTypeUniverseMatchesTheHoldingsGate, which had to stop asserting it.
// Comma-joined in CONSTRAINT ORDER, not sorted: for a composite FK the pairing of child column to
// parent column is positional, so reordering is a real change.
type fkCascadeEdge struct {
	child      string
	parent     string
	constraint string
	childCols  string
	parentCols string
}

func (e fkCascadeEdge) String() string {
	return fmt.Sprintf("%s(%s) -> %s(%s) (%s)", e.child, e.childCols, e.parent, e.parentCols, e.constraint)
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
	"budget_group_members": "retained selector membership history for shared allocations (#1637)",
	// History proper.
	"account_status_history":     "the per-day record of whether an account was live; the liveness baseline reads it",
	"balance_snapshots":          "daily account balances; the only record of what a balance was on a past day",
	"holding_snapshots":          "daily investment positions; Plaid does not backfill them",
	"liability_statements":       "captured statement terms and APRs; the provider reports only the current one",
	"portfolio_snapshots":        "the per-day portfolio total (value, cost basis, gain/loss); the only record of what a portfolio was worth on a past day",
	"portfolio_snapshot_repairs": "the audit trail of snapshot repairs",
	"rule_execution_history":     "what an automation rule did and when",
	"transaction_annotation_history": "the before/after record of every user category or " +
		"review-state overwrite on a transaction (#1569 task 6 / #1579); the prior override and " +
		"the pre-existing reviewed_at survive nowhere else, because the UPDATE that wrote each " +
		"row is what removed them",
	"investment_transactions": "the append-only record of every buy, sell, dividend and fee Plaid " +
		"ever reported, upserted by plaid_investment_transaction_id and read by the investment " +
		"analytics, the unified transaction feed and the account-liveness baseline; it has no " +
		"deleted_at, no Go delete path at all, and both its user_id and its account_id FK are " +
		"ON DELETE CASCADE, so a cascade destroys money history the provider will not re-report " +
		"(#1569 T8 — derived from the live schema, not inherited from a list)",
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

	// The rest are here because TestEveryHistoryShapedTableIsClassified demands a verdict on every
	// table whose NAME reads like history. Each of these is a system record, not the user's
	// financial history: nothing here is something the user would notice losing, and none of it is
	// what #1569 promised to retain.
	"holding_observations": "CAPTURE MARKER, not history (#1569 T9-FIX): one row per (account, " +
		"UTC day) saying a holdings capture completed. It records that we LOOKED; what we FOUND " +
		"is holding_snapshots, which is history and is classified as such. Nothing here is a " +
		"value, and a user would not notice losing it — the reason it exists at all is that an " +
		"account holding nothing writes no snapshot row, so 'confirmed empty' and 'never synced' " +
		"were indistinguishable and the portfolio re-link rule reported a stale predecessor.",
	"holdings": "LIVE STATE, not history: what the account holds right now. ReplaceForAccount " +
		"deliberately deletes and re-inserts it on every sync, which is how it stays true. The " +
		"history is holding_snapshots, captured in that SAME transaction but AFTER the inserts, " +
		"derived from the rows just written — it records the new state, it does not preserve the " +
		"old one. What preserves the past is earlier days' captures (ADR 2026-08-24, Decision 10). " +
		"Same split for liabilities.",
	"audit_logs": "system/compliance record of who did what, not the user's financial history. Its " +
		"PII payload is REDACTED rather than retained on erasure (ADR 2026-08-01)",
	"mcp_audit_log":         "MCP tool-call audit trail; a system record, hard-deleted on erasure",
	"webhook_events":        "inbound provider webhook receipts, kept for replay/idempotency only",
	"paddle_webhook_events": "inbound Paddle webhook receipts, kept for replay/idempotency only",
	"savings_impact_snapshots": "a GLOBAL aggregate, not scoped to any user; there is no user history " +
		"in it to retain or erase",
}

// allowedHistoryCascades is the pinned set of CASCADE edges into those tables, each with the reason
// it is allowed to exist. Adding an entry here is a deliberate decision to let some delete destroy
// history; say why, in the same commit as the migration.
var allowedHistoryCascades = map[fkCascadeEdge]string{
	{child: "budget_group_members", parent: "budget_categories", constraint: "fk_budget_group_members_category", childCols: "budget_category_id", parentCols: "id"}: "erasure reachability through retained budget categories; normal category removal tombstones the parent",
	{child: "budget_group_members", parent: "custom_categories", constraint: "fk_budget_group_members_custom", childCols: "custom_category_id", parentCols: "id"}:   "custom categories are tombstoned on ordinary deletion; this edge only removes membership during GDPR erasure",
	// ---- GDPR erasure off `users`. -------------------------------------------------------------
	// One real `DELETE FROM users` is the whole erasure mechanism (ADR 2026-08-01). Every one of
	// these is required: removing any of them strands personal data after a completed erasure.
	{child: "account_status_history", parent: "users", constraint: "fk_ash_user_id", childCols: "user_id", parentCols: "id"}:                                    "GDPR erasure path off users",
	{child: "balance_snapshots", parent: "users", constraint: "fk_balance_snapshots_user_id", childCols: "user_id", parentCols: "id"}:                           "GDPR erasure path off users",
	{child: "boost_executions", parent: "users", constraint: "fk_boost_executions_user_id", childCols: "user_id", parentCols: "id"}:                             "GDPR erasure path off users",
	{child: "boosts", parent: "users", constraint: "fk_boosts_user_id", childCols: "user_id", parentCols: "id"}:                                                 "GDPR erasure path off users",
	{child: "budgets", parent: "users", constraint: "fk_budgets_user_id", childCols: "user_id", parentCols: "id"}:                                               "GDPR erasure path off users",
	{child: "custom_categories", parent: "users", constraint: "fk_custom_categories_user_id", childCols: "user_id", parentCols: "id"}:                           "GDPR erasure path off users",
	{child: "holding_snapshots", parent: "users", constraint: "fk_holding_snapshots_user_id", childCols: "user_id", parentCols: "id"}:                           "GDPR erasure path off users",
	{child: "liability_statements", parent: "users", constraint: "fk_liability_statements_user_id", childCols: "user_id", parentCols: "id"}:                     "GDPR erasure path off users",
	{child: "portfolio_snapshot_repairs", parent: "users", constraint: "portfolio_snapshot_repairs_user_id_fkey", childCols: "user_id", parentCols: "id"}:       "GDPR erasure path off users",
	{child: "transaction_annotation_history", parent: "users", constraint: "fk_transaction_annotation_history_user_id", childCols: "user_id", parentCols: "id"}: "GDPR erasure path off users; the row carries no FK to transactions, so this is its ONLY route to erasure",
	{child: "portfolio_snapshots", parent: "users", constraint: "fk_portfolio_snapshots_user_id", childCols: "user_id", parentCols: "id"}:                       "GDPR erasure path off users",
	{child: "rollover_history", parent: "users", constraint: "fk_rollover_history_user_id", childCols: "user_id", parentCols: "id"}:                             "GDPR erasure path off users",
	{child: "rule_execution_history", parent: "users", constraint: "fk_rule_execution_history_user_id", childCols: "user_id", parentCols: "id"}:                 "GDPR erasure path off users",
	{child: "savings_goals", parent: "users", constraint: "fk_savings_goals_user_id", childCols: "user_id", parentCols: "id"}:                                   "GDPR erasure path off users",
	{child: "transactions", parent: "users", constraint: "fk_transactions_user_id", childCols: "user_id", parentCols: "id"}:                                     "GDPR erasure path off users",
	{child: "accounts", parent: "users", constraint: "fk_accounts_user_id", childCols: "user_id", parentCols: "id"}:                                             "GDPR erasure path off users",
	{child: "investment_transactions", parent: "users", constraint: "fk_inv_txn_user_id", childCols: "user_id", parentCols: "id"}:                               "GDPR erasure path off users",

	// ---- investment_transactions off accounts. -------------------------------------------------
	// Re-derived for #1569 T8 rather than assumed, and corrected again in round 12 task 5:
	// `accounts` is soft-removed, so this edge only fires when an accounts ROW is genuinely
	// hard-deleted, and there are TWO explicit, user-invoked purge routes that do that, not one.
	// (1) account_repository.go:1295 (`DELETE FROM accounts WHERE id = $1 AND user_id = $2 AND
	// status <> $3`), inside PurgeAccount — its own endpoint, its own audit row, a live-data
	// precondition now restated in the DELETE's own WHERE as well as the preceding Go check
	// (#1569 round 14). (2) linked_item_repository.go:911 (`DELETE FROM linked_items WHERE id = $1
	// AND user_id = $2 AND plaid_removed_at IS NOT NULL AND NOT EXISTS (...)`), inside
	// PurgeLinkedItem — `fk_accounts_linked_item_id` (migrations/000007) is itself ON
	// DELETE CASCADE, so purging the linked item hard-deletes its accounts rows too, one level up
	// from this edge; PurgeLinkedItem has the same shape of own endpoint, own audit row and
	// live-data precondition as PurgeAccount. Both are explicit purge paths, so this was a
	// documentation defect, not a data-loss path: the CASCADE was already correct either way, and
	// no third caller does a bare `DELETE FROM accounts` (checked: nowhere else in non-test Go).
	// Migration 000151's header anticipated this path and left the edge CASCADE deliberately: a
	// purged account's investment transactions are precisely what the user asked to destroy, and
	// leaving them behind orphaned would be the residue the purge exists to remove. A third route
	// to this edge is the users cascade above, which reaches accounts first.
	{child: "investment_transactions", parent: "accounts", constraint: "fk_inv_txn_account_id", childCols: "account_id", parentCols: "id"}: "two explicit user-invoked purge routes hard-delete accounts rows — PurgeAccount (account_repository.go, direct) and PurgeLinkedItem (linked_item_repository.go, via fk_accounts_linked_item_id CASCADE) — the row is what the user asked to destroy either way",

	// ---- The three tables whose ONLY erasure route is this edge. -------------------------------
	// None of these three has a foreign key to `users`, so `DELETE FROM users` reaches them only
	// through the parent named here. Task 14 found all three; removing any of them would leave rows
	// behind after a completed GDPR erasure.
	//
	// ⚠ "ORDINARY DELETE" AND "GDPR ERASURE ONLY" ARE NOT THE SAME CLAIM, and two reasons in this
	// map used to conflate them. All three parents are tombstoned by their ORDINARY delete, so no
	// ordinary delete fires any of these cascades — a user deleting a budget or a goal sets
	// deleted_at and the children survive. That is the property worth relying on. It does NOT
	// follow that only `DELETE FROM users` fires them: `savings_goals` is additionally hard-deleted
	// by three explicit bucket-1 purge paths, each of which requires an EXISTING tombstone
	// (PurgeGoal, savings_goal_repository.go; PurgeAccount, account_repository.go; PurgeLinkedItem,
	// linked_item_repository.go — all three allowlisted in allowedHistoryDeletes). `budgets` and
	// `custom_categories` have no such purge today; verified with
	// `/usr/bin/grep -rn "DELETE FROM budgets\|DELETE FROM custom_categories" internal --include="*.go"`
	// over non-test Go, which returns only prose in comments explaining why they no longer do it.
	//
	// Nothing in this file checks that a reason string is TRUE — they are prose for the next
	// reader. Re-derive before relying on one.
	{child: "budget_categories", parent: "budgets", constraint: "fk_budget_categories_budget_id", childCols: "budget_id", parentCols: "id"}:                     "ONLY erasure path: budget_categories has no user_id FK; budgets is tombstoned by its ordinary delete and has no purge path, so this fires only under GDPR erasure",
	{child: "savings_goal_contributions", parent: "savings_goals", constraint: "fk_contributions_savings_goal", childCols: "savings_goal_id", parentCols: "id"}: "ONLY erasure path: savings_goal_contributions has no user_id column. savings_goals is tombstoned by its ordinary delete, so no ordinary delete fires this — but GDPR erasure AND the three explicit purges that hard-delete a tombstoned goal (PurgeGoal, PurgeAccount, PurgeLinkedItem) all do",

	// ---- Explicit destruction: the account purge. ----------------------------------------------
	// `PurgeAccount` (account_repository.go) is the user asking, in words, for one account's data to
	// be destroyed; it refuses to run on a live account. These three edges are how the derived
	// history goes with it. See TestAccountRepository_PurgeAccount_Integration_RemovesEverythingDerived.
	{child: "account_status_history", parent: "accounts", constraint: "fk_ash_account_id", childCols: "account_id", parentCols: "id"}:                "explicit account purge destroys derived history for that account",
	{child: "holding_snapshots", parent: "accounts", constraint: "fk_holding_snapshots_account_id", childCols: "account_id", parentCols: "id"}:       "explicit account purge destroys derived history for that account",
	{child: "liability_statements", parent: "accounts", constraint: "fk_liability_statements_account_id", childCols: "account_id", parentCols: "id"}: "explicit account purge destroys derived history for that account",

	// ---- Tombstoned-parent cascades that no ordinary delete can reach. -------------------------
	//
	// ⚠ CORRECTED (#1569 round 14). The `accounts ← linked_items` reason said linked_items rows
	// "are themselves purged ONLY by an explicit disconnect-and-purge". That word was false and
	// dangerously so: `fk_linked_items_user_id` (migrations/000006) is itself ON DELETE CASCADE from
	// `users`, so GDPR erasure's one real `DELETE FROM users` (profile_repository.go,
	// DeleteUserCascade) hard-deletes linked_items too and fires this edge — reaching accounts and
	// everything derived from them. An engineer trusting "only ... an explicit purge" could restrict
	// this FK and STRAND erasure data, the one thing this branch may never do. What is true, and is
	// the property worth relying on, is that no ORDINARY delete fires it: a disconnect soft-removes
	// the linked item and keeps the row (`status`/`deleted_at`), so only PurgeLinkedItem's explicit
	// hard delete and the users cascade reach it. Re-derived by reading migration 000006's
	// `ALTER TABLE linked_items ADD CONSTRAINT fk_linked_items_user_id ... ON DELETE CASCADE`;
	// no later migration alters or drops that constraint (`/usr/bin/grep -rn fk_linked_items_user_id
	// migrations/` returns only 000006).
	{child: "accounts", parent: "linked_items", constraint: "fk_accounts_linked_item_id", childCols: "linked_item_id", parentCols: "id"}:                             "no ORDINARY delete fires this: a disconnect soft-removes the linked item and keeps the row. Two hard-delete routes do — PurgeLinkedItem (the explicit disconnect-and-purge) and GDPR erasure, since fk_linked_items_user_id CASCADEs from users. NOT 'purge only': restricting this FK would strand erasure data",
	{child: "budget_categories", parent: "custom_categories", constraint: "fk_budget_categories_custom_category", childCols: "custom_category_id", parentCols: "id"}: "custom_categories is tombstoned, so this cascade fires only under GDPR erasure",
	{child: "budget_categories", parent: "savings_goals", constraint: "fk_budget_categories_savings_goal", childCols: "savings_goal_id", parentCols: "id"}:           "savings_goals is tombstoned by its ordinary delete, so no ordinary goal delete fires this; GDPR erasure and the three explicit purges that hard-delete a tombstoned goal (PurgeGoal, PurgeAccount, PurgeLinkedItem) all do",
}

// historyShapedTableName matches the table names that READ as a record of something that happened.
// It is the completeness net: G1 and G2 both work off a hand-maintained list, and a hand-maintained
// list has no way to notice a table nobody added to it. `portfolio_snapshots` was exactly that —
// per-day total_value/total_cost_basis/total_gain_loss, cascading from users, and invisible to both
// guards, while its own REPAIRS table was protected.
//
// Name-shaped rather than column-shaped on purpose. A column heuristic ("has user_id and a date")
// sweeps in join tables and settings rows, and the argument about whether each one counts is
// exactly the argument that stops people running the guard. A name ending in _history, _snapshots,
// _events, _executions, _statements, _contributions, _repairs, _log or _audit is a claim the author
// made about what the table is; this test only asks them to finish the sentence.
//
// ⚠ HOW MUCH IT ACTUALLY COVERS, measured rather than assumed: **9 of the 20 tables already in
// historyBearingTables do not match this regexp** — accounts, boosts, budget_categories, budgets,
// custom_categories, investment_transactions, savings_goals, transactions, budget_group_members.
// That is 45%, and it is
// the LARGEST class the guards protect: the tombstoned parents. The net is structurally incapable
// of finding another one of those, because their names describe a thing rather than a record of a
// thing.
//
// ⚠ AND THE EIGHTH IS NOT A TOMBSTONED PARENT — it is a third failure mode. `investment_transactions`
// (#1569 T8) is a genuine append-only provider record whose name simply ends in a suffix nobody put
// on the list. It was in NEITHER classification map, matched NEITHER the net, and carried an
// ON DELETE CASCADE from a soft-removed parent — invisible to G1, G2 and the completeness check
// simultaneously, which is the exact hole all three exist to close. Widening the regexp to
// `_transactions` was considered and rejected: it would demand a verdict on `transactions` (already
// classified) and `paddle_transactions` (billing, not financial history) without finding anything
// the hand list does not already have, and a net tuned after the fact to catch the one case that
// escaped it is not evidence about the next one.
//
// It caught `portfolio_snapshots` because that table happened to be shaped like history. Read this
// as "it will catch the next _snapshots table", not as "the universe is now complete". Adding a
// table to historyBearingTables by hand remains the primary mechanism and is not made redundant by
// this. TestHistoryShapedTableClassification_TheNetMissesMostOfWhatItProtects pins the 8 so this
// paragraph cannot quietly become false.
//
// ⚠ AND IT IS ONE LINE TO UNDO, demonstrated, not hypothesised. Moving `portfolio_snapshots` out of
// historyBearingTables into deliberatelyNotHistoryBearing with a plausible-sounding false reason
// ("derived aggregate, recomputable from holdings at any time") and deleting its allowlist edge
// leaves the ENTIRE guard suite green — G1, G2, all three classification meta-tests, everything.
// Nothing checks that a reason is TRUE, only that it is non-empty. These guards make forgetting
// impossible; they do not make a wrong decision impossible, and a reviewer reading a migration is
// still the only thing standing between a reclassification and a silent reversal of #1569.
var historyShapedTableName = regexp.MustCompile(
	`(_history|_histories|_snapshot|_snapshots|_event|_events|_executions|_statements|` +
		`_contributions|_repairs|_log|_logs|_audit)$`)

// unclassifiedHistoryShapedTables returns the history-shaped table names that appear in NEITHER
// map. Kept pure so the meta-test can construct the case.
func unclassifiedHistoryShapedTables(tables []string, history, notHistory map[string]string) []string {
	var out []string
	for _, tbl := range tables {
		if !historyShapedTableName.MatchString(tbl) {
			continue
		}
		if _, ok := history[tbl]; ok {
			continue
		}
		if _, ok := notHistory[tbl]; ok {
			continue
		}
		out = append(out, tbl)
	}
	sort.Strings(out)
	return out
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
		childCols:  "rule_id", parentCols: "id",
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

// TestHistoryCascadeGuard_FlagsAColumnListChangeUnderTheSameConstraintName is the case item B of
// #1569 T8.7 exists for, and it is the one shape that used to slip through entirely.
//
// A migration DROPs `fk_holding_snapshots_account_id` and re-ADDs a constraint with the SAME NAME,
// between the SAME two tables, still ON DELETE CASCADE — but pointing at a different column. Before
// the column lists were part of the key, the edge Postgres reported was byte-identical to the pinned
// one and G1 said nothing, even though the cascade now travels a path nobody reviewed.
//
// Both directions must fire: the new edge is unallowlisted, and the pinned entry is stale.
func TestHistoryCascadeGuard_FlagsAColumnListChangeUnderTheSameConstraintName(t *testing.T) {
	pinned := fkCascadeEdge{
		child: "holding_snapshots", parent: "accounts",
		constraint: "fk_holding_snapshots_account_id",
		childCols:  "account_id", parentCols: "id",
	}
	if _, ok := allowedHistoryCascades[pinned]; !ok {
		t.Fatalf("%s must be in the allowlist for this test to mean anything", pinned)
	}
	repointed := pinned
	repointed.childCols = "source_account_id"

	var edges []fkCascadeEdge
	for e := range allowedHistoryCascades {
		if e == pinned {
			edges = append(edges, repointed)
			continue
		}
		edges = append(edges, e)
	}

	bad, stale := historyCascadeVerdict(edges, historyBearingTables, allowedHistoryCascades)
	if len(bad) != 1 || !strings.Contains(bad[0], "source_account_id") {
		t.Fatalf("the re-pointed constraint must be reported as unallowlisted, got %v", bad)
	}
	if len(stale) != 1 || !strings.Contains(stale[0], "(account_id)") {
		t.Fatalf("the pinned entry it no longer matches must be reported stale, got %v", stale)
	}
}

// TestHistoryCascadeGuard_FlagsAStaleAllowlistEntry pins the other direction. An allowlist that
// outlives its edge is how the next reader concludes that destroying this history was already
// agreed.
func TestHistoryCascadeGuard_FlagsAStaleAllowlistEntry(t *testing.T) {
	gone := fkCascadeEdge{child: "balance_snapshots", parent: "users", constraint: "fk_balance_snapshots_user_id", childCols: "user_id", parentCols: "id"}
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
		[]fkCascadeEdge{{child: "notifications", parent: "users", constraint: "fk_notifications_user_id", childCols: "user_id", parentCols: "id"}},
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
		if strings.TrimSpace(e.childCols) == "" || strings.TrimSpace(e.parentCols) == "" {
			t.Errorf("allowlist entry %s has an empty column list. An entry written without one can "+
				"never match a live edge (the live query always supplies both), so it is permanently "+
				"stale — and it pins nothing about WHICH columns the cascade travels along", e)
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
		[]fkCascadeEdge{{child: "budget_period_events", parent: "budgets", constraint: "fk_budget_period_events_budget", childCols: "budget_id", parentCols: "id"}},
		historyBearingTables, allowedHistoryCascades)
	if len(bad) != 0 {
		t.Fatalf("budget_period_events must stay CASCADE and must not be flagged; got %v", bad)
	}
}

// TestHistoryShapedTableClassification_FlagsAnUnclassifiedTable is the case F3 exists for: a new
// migration adds a per-day table and nobody remembers the two maps, so G1 and G2 both go on being
// green about a table they cannot see.
func TestHistoryShapedTableClassification_FlagsAnUnclassifiedTable(t *testing.T) {
	got := unclassifiedHistoryShapedTables(
		[]string{"net_worth_snapshots", "balance_snapshots", "budget_period_events", "users", "accounts"},
		historyBearingTables, deliberatelyNotHistoryBearing)
	if len(got) != 1 || got[0] != "net_worth_snapshots" {
		t.Fatalf("only the unclassified history-shaped table should be reported, got %v", got)
	}
}

// TestHistoryShapedTableClassification_EitherMapSatisfiesIt pins that the requirement is a VERDICT,
// not a particular verdict. Forcing every shaped table into historyBearingTables would be the wrong
// fix, and is how budget_period_events would get reclassified.
func TestHistoryShapedTableClassification_EitherMapSatisfiesIt(t *testing.T) {
	if got := unclassifiedHistoryShapedTables([]string{"rollover_history"},
		historyBearingTables, deliberatelyNotHistoryBearing); len(got) != 0 {
		t.Fatalf("a table in historyBearingTables is classified; got %v", got)
	}
	if got := unclassifiedHistoryShapedTables([]string{"budget_period_events"},
		historyBearingTables, deliberatelyNotHistoryBearing); len(got) != 0 {
		t.Fatalf("a table in deliberatelyNotHistoryBearing is classified; got %v", got)
	}
	if got := unclassifiedHistoryShapedTables([]string{"user_preferences", "merchants"},
		historyBearingTables, deliberatelyNotHistoryBearing); len(got) != 0 {
		t.Fatalf("a table whose name is not history-shaped is out of scope; got %v", got)
	}
}

// TestHistoryShapedTableClassification_MapsAgreeOnNothing keeps a table from being in both maps,
// which would let a reader take whichever answer suited them.
func TestHistoryShapedTableClassification_MapsAgreeOnNothing(t *testing.T) {
	for tbl := range deliberatelyNotHistoryBearing {
		if _, ok := historyBearingTables[tbl]; ok {
			t.Errorf("%s is in BOTH historyBearingTables and deliberatelyNotHistoryBearing", tbl)
		}
		if strings.TrimSpace(deliberatelyNotHistoryBearing[tbl]) == "" {
			t.Errorf("%s is excluded with no reason; the reason is the point", tbl)
		}
	}
}

// TestHistoryShapedTableClassification_TheNetMissesMostOfWhatItProtects pins the measured coverage of
// historyShapedTableName against the list it is meant to backstop.
//
// It is not an aspiration to fix — the tombstoned parents SHOULD NOT be renamed to satisfy a regexp.
// It exists so that the honest disclosure above stays honest: if someone widens the pattern, or adds
// a shaped table, the number here changes and the comment gets updated with it.
func TestHistoryShapedTableClassification_TheNetMissesMostOfWhatItProtects(t *testing.T) {
	want := map[string]bool{
		"budget_group_members": true, // retained membership configuration also has no history-shaped suffix
		"accounts": true, "boosts": true, "budget_categories": true, "budgets": true,
		"custom_categories": true, "investment_transactions": true, "savings_goals": true,
		"transactions": true,
	}
	got := map[string]bool{}
	for tbl := range historyBearingTables {
		if !historyShapedTableName.MatchString(tbl) {
			got[tbl] = true
		}
	}
	for tbl := range want {
		if !got[tbl] {
			t.Errorf("%s now MATCHES historyShapedTableName; the net got wider, which is good — update "+
				"the count in the doc comment above", tbl)
		}
	}
	for tbl := range got {
		if !want[tbl] {
			t.Errorf("%s is history-bearing and does NOT match historyShapedTableName, and it is not in "+
				"the recorded list. The completeness net cannot find tables like it; add it here and "+
				"update the count in the doc comment above", tbl)
		}
	}
}
