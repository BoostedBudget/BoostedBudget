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
