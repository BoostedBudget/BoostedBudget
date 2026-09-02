package domain

import "time"

// Issue #1569 Task 6 (#1579) — what a category or review-state write overwrote.
//
// THE DEFECT. Recategorising a transaction, or flipping its review flag, replaced the user's
// previous annotation with no record of what it had been. The provider's own categorisation was
// never at risk (pfc_primary / pfc_detailed are untouched by every capturing statement, so clearing an
// override restores it) — what vanished was a PREVIOUS USER OVERRIDE, and, the fact #1579 missed,
// the pre-existing `reviewed_at`, which BOTH category statements clobber unconditionally with
// `reviewed = true, reviewed_at = now`.
//
// ⚠ #1579 AND THE PLAN BOTH SAY FOUR REPOSITORY METHODS. Re-derived: four ENDPOINTS, three
// methods. `SetTransactionReviewed` is a handler (huma/operations/transactions.go), and it calls
// transactionService.SetReviewed with a one-element slice — the single and bulk review endpoints
// share ONE statement, so there is nothing separate to fix for the single one.

// Change sources, pinned by the live `chk_transaction_annotation_history_source` CHECK:
// change_source IN ('category','review','settlement','erasure','tags').
//
// ⚠ THIS LINE SAID `('category','review')` UNTIL IT WAS CORRECTED, and it was wrong by two values:
// migration 000170 widened the CHECK to admit 'settlement' and 000172 to admit 'erasure', both of
// which are declared right below it. A stale statement of what the database allows is worse than no
// statement, because the next reader takes it as the authority on which sources are legal and
// concludes that writing one of the other two would be rejected. It is not re-derived by hand any
// more: TestAnnotationChangeSourcesMatchTheLiveCheck reads the constraint out of pg_constraint and
// fails when this set and the database's disagree in either direction.
const (
	// TransactionAnnotationChangeCategory marks a row written by UpdateCategory or
	// BulkUpdateCategory. Those statements rewrite `reviewed` and `reviewed_at` as a SIDE EFFECT of
	// recategorising, which is why a reader needs to be able to tell the two sources apart: the
	// user asked for a category, and lost a review timestamp.
	TransactionAnnotationChangeCategory = "category"

	// TransactionAnnotationChangeReview marks a row written by SetReviewed (single or bulk).
	TransactionAnnotationChangeReview = "review"

	// TransactionAnnotationChangeSettlement marks a row written by TombstoneByExternalID's
	// settlement move (#1578): a pending charge posted, and the sync carried the user's manual
	// category from the pending row onto the posted twin, REPLACING whatever the twin had.
	//
	// It is its own source because the user asked for neither. Filing it under 'category' would
	// claim they recategorised the twin. A reader needs this row specifically to explain why the
	// twin no longer agrees with the rule_execution_history entry for the rule that categorised it
	// moments earlier in the same sync.
	TransactionAnnotationChangeSettlement = "settlement"

	// TransactionAnnotationChangeErasure marks a row written by DeleteUserCascade's dangling
	// custom-category-override clear (#1590).
	//
	// It is the ONE source whose subject is not the row's own owner. When user A exercises GDPR
	// erasure, A's custom_categories rows are hard-deleted; a transaction belonging to a DIFFERENT,
	// surviving user B that pointed at one of them must have its override cleared first, or the
	// ON DELETE RESTRICT foreign key would refuse the erasure entirely. B loses their annotation as
	// a consequence of somebody else's request, and this row is what tells them so.
	//
	// It is its own source for the same reason 'settlement' is: the user did not ask. Filing it
	// under 'category' would claim B recategorised their own transaction on the day a stranger was
	// erased, which is both false and unexplainable.
	//
	// ⚠ Rows are written ONLY for surviving users. The erased user's own transactions are cleared by
	// the same statement and are hard-deleted moments later in the same database transaction; a
	// record that vanishes with the thing it records is not a record.
	TransactionAnnotationChangeErasure = "erasure"

	// TransactionAnnotationChangeTags marks a row written by UpdateTags (#1569 gap G12).
	//
	// It is a WHOLE-ARRAY REPLACE: every tag dropped from the array is gone the moment the
	// statement returns, and nothing else in the system holds the previous set. The statement
	// carried no capture at all until G12, and the guard could not see it — annotationTrackedColumns
	// is derived from annotationCapturedColumns, which had no tags column, so `SET tags =` matched
	// nothing and UpdateTags sat in neither the captured set nor the exemption list.
	//
	// It is its own source because change_source answers "what did the user ask for". They asked to
	// retag. Every category column on one of these rows is unchanged by construction, so filing it
	// under 'category' would describe a recategorisation that did not happen.
	TransactionAnnotationChangeTags = "tags"
)

// TransactionAnnotation is the complete user-set annotation on a transaction at one moment: the
// category-override tuple plus the review state. Used for both halves of a recorded change.
type TransactionAnnotation struct {
	// CategoryOverrideKind is 'pfc', 'custom', or nil for "no override" (the `clear` action).
	CategoryOverrideKind *string `json:"category_override_kind,omitempty"`

	PFCPrimaryOverride       *string `json:"pfc_primary_override,omitempty"`
	PFCDetailedOverride      *string `json:"pfc_detailed_override,omitempty"`
	CustomCategoryOverrideID *string `json:"custom_category_override_id,omitempty"`

	// EffectiveCategoryName is the denormalised display name the override resolved to. Recorded
	// because a custom category can itself be tombstoned later, at which point the id alone can no
	// longer answer "what did this say".
	EffectiveCategoryName *string `json:"effective_category_name,omitempty"`

	// CategorySource is 'plaid', 'rule' or 'manual' — who last set the category.
	CategorySource *string `json:"category_source,omitempty"`

	Reviewed bool `json:"reviewed"`

	// ReviewedAt is the fact #1579 missed. A transaction reviewed in March and recategorised in
	// August reports August, and March survives nowhere else.
	ReviewedAt *time.Time `json:"reviewed_at,omitempty"`

	// Tags and TagsSource are the user's tag array and its per-tag provenance (#1569 gap G12).
	//
	// They are here because the WRITER records them and a writer with no reader ships nothing
	// observable — the lesson task 5 paid for on rule_execution_history. A 'tags' row whose every
	// visible field were identical on both sides would read as a change that did not happen.
	//
	// nil means NOT RECORDED, which is what every row written before migration 000173 carries. An
	// empty array means the user genuinely had no tags, and the two must not be conflated: `[]`
	// would claim a fact about a row that predates the column.
	Tags       []string    `json:"tags,omitempty"`
	TagsSource []TagSource `json:"tags_source,omitempty"`
}

// TransactionAnnotationChange is one row of transaction_annotation_history: a before/after pair for
// a single transaction, written inside the same statement as the UPDATE that destroyed the prior
// value.
type TransactionAnnotationChange struct {
	ID string `json:"id"`

	// UserID is denormalised from the UPDATE's own tenancy predicate at write time, never from
	// client input. It is the row's only path to GDPR erasure and the key every read filters on.
	UserID string `json:"user_id"`

	// TransactionID carries NO foreign key, deliberately: `transactions` is tombstoned rather than
	// deleted, and on the paths that do hard-delete one (erasure, the three purges) the user_id
	// CASCADE already reaches this table. A purge of one transaction must not take the record of
	// what was overwritten on it.
	TransactionID string `json:"transaction_id"`

	// ChangeSource is one of the five TransactionAnnotationChange* constants above —
	// 'category', 'review', 'settlement', 'erasure' or 'tags'. (This comment named only the first
	// two, which stopped being true at migration 000170; 000173 added the fifth.)
	ChangeSource string `json:"change_source"`

	Prior TransactionAnnotation `json:"prior"`
	New   TransactionAnnotation `json:"new"`

	ChangedAt time.Time `json:"changed_at"`
}

// TransactionAnnotationHistoryResponse is the paginated per-user annotation history payload.
// Mirrors RuleExecutionsResponse.
type TransactionAnnotationHistoryResponse struct {
	Changes []*TransactionAnnotationChange `json:"changes"`
	Total   int                            `json:"total"`
	Limit   int                            `json:"limit"`
	Offset  int                            `json:"offset"`
}
