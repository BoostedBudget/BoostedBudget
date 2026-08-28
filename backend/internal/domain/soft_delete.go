package domain

// Issue #1569, Task 8 — the tombstone vocabulary shared by the four config parents that migration
// 000158 gave a soft delete: savings_goals, boosts, budgets and budget_categories.
//
// Each of those four DELETEs used to cascade a HISTORY table away with it — hand-logged savings
// contributions, boost execution records, rollover history, budget period events. Tombstoning the
// parent keeps the foreign keys intact, so the history keeps a row to point at, and every read
// default-excludes tombstoned parents instead.
//
// These are separate from the transactions vocabulary (TxnDeletedProviderRemovedPending /
// …Posted) on purpose: those two reasons classify a PROVIDER action and decide visibility between
// themselves. These classify a USER action, and both of them hide the row unconditionally.
const (
	// SoftDeleteReasonUser marks a row the user asked to delete.
	SoftDeleteReasonUser = "user_deleted"

	// SoftDeleteReasonParent marks a row tombstoned because its parent was — a budget's
	// categories, or the savings categories auto-created for a goal. Both used to disappear by
	// ON DELETE CASCADE and must keep disappearing.
	SoftDeleteReasonParent = "parent_deleted"
)

// SoftDeletedByUser is the ACTOR recorded in deleted_by on these four tables. It is a role label,
// not a user id: audit_logs already carries the subject in resource_id, and deleted_by answers
// "what kind of thing did this", the way transactions.deleted_by records "plaid_sync". A
// user-initiated delete and a provider-initiated one must stay distinguishable at a glance.
const SoftDeletedByUser = "user"

// SavingsContributionDeletedReasonProviderRemovedTxn marks a savings_goal_contributions row
// tombstoned because the transaction it was derived from came back in Plaid's `removed` array
// (migration 000180, #1569 round 12 task 2). This is a THIRD vocabulary, distinct from both of the
// above: SoftDeleteReasonUser/Parent classify a USER action on the four config parents, and
// TxnDeletedProviderRemovedPending/Posted classify a provider action on transactions themselves
// with two values because pending-vs-posted changes what is shown. This table has exactly one
// destructive path today — DeleteManagedForTransaction, reached only from
// transactionSyncService.processRemovedTransactions — so one value names it precisely. It is NOT
// "user_deleted": the provider withdrawing a transaction is not the user asking for a contribution
// to be destroyed ("the provider is not the user", #1569's founding corollary). DeleteContribution
// (the user-facing endpoint) remains a real hard delete and does not write this column.
const SavingsContributionDeletedReasonProviderRemovedTxn = "provider_removed_txn"
