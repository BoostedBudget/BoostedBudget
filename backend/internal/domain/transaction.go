package domain

import (
	"errors"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/pfc"
)

// TransactionSource represents the source of a transaction data
// Supports multiple providers and manual transactions
type TransactionSource string

const (
	// SourcePlaid represents transactions synced from Plaid API (current implementation)
	SourcePlaid TransactionSource = "plaid"

	// SourceManual represents manually-entered transactions by users (future)
	SourceManual TransactionSource = "manual"

	// SourceTeller represents transactions synced from Teller API (future provider)
	SourceTeller TransactionSource = "teller"

	// SourceFinicity represents transactions synced from Finicity API (future provider)
	SourceFinicity TransactionSource = "finicity"

	// SourceYodlee represents transactions synced from Yodlee API (future provider)
	SourceYodlee TransactionSource = "yodlee"
)

// CategorySource represents where the current category came from
type CategorySource string

const (
	// CategorySourcePlaid is the original category from Plaid API
	CategorySourcePlaid CategorySource = "plaid"

	// CategorySourceRule is a category applied by an automation rule
	CategorySourceRule CategorySource = "rule"

	// CategorySourceManual is a category manually set by the user
	CategorySourceManual CategorySource = "manual"
)

// TransactionDeletedReason explains why a transaction row carries a tombstone
// (transactions.deleted_at). Issue #1569.
//
// The provider removing a row is NOT the user deleting it, and before #1569 the two were the same
// operation: Plaid's `removed` array ran `DELETE FROM transactions`, destroying category overrides,
// tags, notes and `reviewed` state that only the user could have produced, plus the
// rule_execution_history trail behind them. The row is now retained and marked instead.
//
// The reason decides visibility, and the split is derivable from transactions.pending alone. That
// remains true and is what this enum rests on. ⚠ The sentence that used to follow it — "because
// this codebase does not store Plaid's pending_transaction_id" — is NO LONGER TRUE as of #1578:
// the posted twin now carries transactions.pending_transaction_id_blind_idx, and the settlement
// annotation move matches on it. The pointer is a LINK between the two rows; it is not, and must
// not become, the input to this classification — a settled pending is recognised by its own
// `pending` column under the tombstoning UPDATE's row lock, with no second read to race.
type TransactionDeletedReason string

const (
	// TxnDeletedProviderRemovedPending marks a row the provider removed while it was still
	// pending. That is the ordinary pending -> posted lifecycle: a posted twin has already
	// replaced it. Retained for forensics, and shown NOWHERE — surfacing it would double-count
	// the same spend against its twin.
	TxnDeletedProviderRemovedPending TransactionDeletedReason = "provider_removed_pending"

	// TxnDeletedProviderRemovedPosted marks a row the provider removed after it had posted: a
	// bank reversal or correction. Retained AND surfacable as reversed
	// (TransactionFilters.IncludeReversed), because a closed month whose totals change without
	// the user being told is the failure #1569 exists to end.
	TxnDeletedProviderRemovedPosted TransactionDeletedReason = "provider_removed_posted"
)

// TxnDeletedByPlaidSync is the actor recorded in transactions.deleted_by when the Plaid sync path
// tombstones a row. It names the ACTOR, not a user — no user is involved in a provider removal,
// which is the whole reason the row is kept.
const TxnDeletedByPlaidSync = "plaid_sync"

// TagSource tracks the origin of a tag on a transaction
type TagSource struct {
	Tag    string  `json:"tag"`
	Source string  `json:"source"` // "manual" or "rule"
	RuleID *string `json:"rule_id,omitempty"`
}

// Transaction represents a financial transaction from any provider (Plaid, manual, or future providers).
// This is a provider-agnostic model that standardizes transaction data across all sources.
//
// Design: The struct uses generic field names (source, external_transaction_id) instead of
// provider-specific names (plaid_transaction_id) to support multiple providers and manual
// transactions without schema changes.
//
// Issue #171: Added UserAccountID for stable queries across re-links.
// The UserAccountID links to a user_accounts record that survives when a user
// re-links a bank (which assigns new item_id and account_id values).
type Transaction struct {
	// Core identifiers
	ID           string `json:"id" db:"id"`
	UserID       string `json:"user_id" db:"user_id"`
	AccountID    string `json:"account_id" db:"account_id"`
	LinkedItemID string `json:"linked_item_id" db:"linked_item_id"`

	// Stable reference for re-link handling (Issue #171)
	// Links to user_accounts table which survives Plaid Item re-links
	UserAccountID *string `json:"user_account_id,omitempty" db:"user_account_id"`

	// Provider abstraction
	// Source identifies where this transaction came from (plaid, manual, teller, etc.)
	Source TransactionSource `json:"source" db:"source"`

	// ExternalTransactionID is the provider's unique transaction identifier.
	// For Plaid: transaction_id from Plaid API
	// For Teller: transaction_id from Teller API
	// For manual: NULL (users don't have external IDs)
	ExternalTransactionID *string `json:"external_transaction_id,omitempty" db:"external_transaction_id"`

	// ExternalMetadata stores provider-specific data as JSON
	// For Plaid: logo_url, website, location data, etc.
	// For Teller: provider-specific enrichment data
	// For manual: NULL or user-added structured data
	ExternalMetadata *string `json:"external_metadata,omitempty" db:"external_metadata"`

	// Core transaction fields (standardized across all providers)
	Amount float64 `json:"amount" db:"amount"` // Positive=outflow, Negative=inflow (Plaid PFCv2)

	// Date is the posting day, over a DATE column (transactions.date, migration 000010). Kept as
	// time.Time — NOT serialized — because it is also a SQL bind param on insert/update, a scan
	// target, and the source of the V:2 keyset cursor sort value, all of which need a real
	// time.Time rather than a formatted string. DateDisplay below is what reaches clients.
	Date time.Time `json:"-" db:"date"`
	// AuthorizedDate is the authorization day, also a DATE column, and also the preferred half of
	// the feed sort key (COALESCE(authorized_date, date)). Same split, same reasons.
	AuthorizedDate *time.Time `json:"-" db:"authorized_date"`

	// DateDisplay / AuthorizedDateDisplay are the UTC calendar days that actually serialize as
	// "date" and "authorized_date". Populated at the DB edge by
	// transactionRepository.postScanTransactionState, the single finalizer every full-transaction
	// scan runs through — the same transient-field pattern as InvestmentTransaction.DateDisplay.
	//
	// Added by #1631: Date carried json:"date" directly, so a DATE column serialized as a full
	// timestamp (2026-01-02T00:00:00Z), which renders a day early for any client west of UTC that
	// parses it in local time. That is the defect class HoldingSnapshotView.AsOfDate documents,
	// and the investment leg of this very feed had already been fixed for it.
	//
	// AuthorizedDateDisplay stays a *string so a NULL authorized_date is OMITTED rather than
	// present-and-empty: the two most prominent UI sites render
	// formatDate(authorized_date || date), and an empty string is truthy enough to win that `||`
	// and print "Invalid Date".
	DateDisplay           string  `json:"date" db:"-" format:"date" doc:"UTC calendar day (YYYY-MM-DD) the transaction posted"`
	AuthorizedDateDisplay *string `json:"authorized_date,omitempty" db:"-" format:"date" doc:"UTC calendar day (YYYY-MM-DD) the transaction was authorized; omitted when the provider gave none"`

	Name                  string  `json:"name" db:"name"`
	MerchantName          *string `json:"merchant_name,omitempty" db:"merchant_name"`
	MerchantID            *string `json:"merchant_id,omitempty" db:"merchant_id"`
	EffectiveMerchantName *string `json:"effective_merchant_name,omitempty" db:"-"`

	// PFCv2 source columns (from Plaid)
	PFCPrimary         *string `json:"pfc_primary,omitempty" db:"pfc_primary"`
	PFCDetailed        *string `json:"pfc_detailed,omitempty" db:"pfc_detailed"`
	PFCConfidenceLevel *string `json:"pfc_confidence_level,omitempty" db:"pfc_confidence_level"`
	PFCCategoryIconURL *string `json:"pfc_category_icon_url,omitempty" db:"pfc_category_icon_url"`

	// PFC display names (not stored in DB)
	PFCPrimaryDisplay          *string `json:"pfc_primary_display,omitempty" db:"-"`
	PFCDetailedDisplay         *string `json:"pfc_detailed_display,omitempty" db:"-"`
	PFCPrimaryOverrideDisplay  *string `json:"pfc_primary_override_display,omitempty" db:"-"`
	PFCDetailedOverrideDisplay *string `json:"pfc_detailed_override_display,omitempty" db:"-"`

	// Custom category display name (not stored in DB)
	CustomCategoryOverrideDisplay *string `json:"custom_category_override_display,omitempty" db:"-"`

	// Payment details
	PaymentChannel *string `json:"payment_channel,omitempty" db:"payment_channel"`
	Pending        bool    `json:"pending" db:"pending"`

	// PendingTransactionID is the provider's id of the PENDING transaction this one replaced.
	// Plaid sets it on the POSTED transaction when a pending charge settles
	// (plaid-go v23.0.0, plaid/model_transaction.go:47). Issue #1578.
	//
	// It is the ONLY safe way to link a settled pending to its posted twin. Matching on
	// (account, amount, date window, merchant) instead can attach one transaction's category to a
	// different transaction, which is worse than the bug it would fix.
	//
	// NOT SERIALISED and NOT a `db` column, deliberately. The plaintext is never stored and never
	// read back: the repository derives transactions.pending_transaction_id_blind_idx from it on
	// write and only ever MATCHES against that. So this field is write-only plumbing between the
	// Plaid mapper and the repository, and putting it in the JSON contract would publish an
	// external identifier the client has no use for.
	PendingTransactionID *string `json:"-" db:"-"`

	// User customization
	UserNotes *string `json:"user_notes,omitempty" db:"user_notes"`

	// Category override — discriminated union (category_override_kind determines which branch is active)
	CategoryOverrideKind     *string        `json:"category_override_kind,omitempty" db:"category_override_kind"`
	PFCPrimaryOverride       *string        `json:"pfc_primary_override,omitempty" db:"pfc_primary_override"`
	PFCDetailedOverride      *string        `json:"pfc_detailed_override,omitempty" db:"pfc_detailed_override"`
	CustomCategoryOverrideID *string        `json:"custom_category_override_id,omitempty" db:"custom_category_override_id"`
	CategorySourceField      CategorySource `json:"category_source" db:"category_source"`
	CategoryOverrideDate     *time.Time     `json:"category_override_date,omitempty" db:"category_override_date"`

	// Review state (Issue #1203): new Plaid transactions arrive unreviewed and
	// become reviewed when a rule applies a category, the user categorizes them,
	// or the user explicitly toggles review.
	Reviewed   bool       `json:"reviewed" db:"reviewed"`
	ReviewedAt *time.Time `json:"reviewed_at,omitempty" db:"reviewed_at"`

	// Tombstone (Issue #1569). DeletedAt is NULL on every live row, and every read in the
	// codebase excludes non-NULL rows by default — see repository's
	// TestNoTransactionReadForgetsTheTombstoneFilter, which enforces that mechanically.
	//
	// A caller that opts into reversals (TransactionFilters.IncludeReversed) receives rows with
	// DeletedReason == TxnDeletedProviderRemovedPosted; Reversed() is the predicate to render on.
	DeletedAt     *time.Time                `json:"deleted_at,omitempty" db:"deleted_at"`
	DeletedReason *TransactionDeletedReason `json:"deleted_reason,omitempty" db:"deleted_reason" enum:"provider_removed_pending,provider_removed_posted"`

	// DeletedBy is `json:"-"` ON PURPOSE. It is an internal ACTOR label ("plaid_sync"), not a user
	// or anything a client can act on, and the reason a client actually needs is already in
	// DeletedReason. Serialising it would put an operational implementation detail into
	// api/openapi.yaml, where it becomes a contract we have to keep. It stays on the struct because
	// the scan path populates it and the integration tests assert provenance was recorded.
	DeletedBy *string `json:"-" db:"deleted_by"`

	// Effective category columns (denormalized, generated or app-maintained)
	EffectivePFCPrimary   *string `json:"effective_pfc_primary,omitempty" db:"effective_pfc_primary"`
	EffectivePFCDetailed  *string `json:"effective_pfc_detailed,omitempty" db:"effective_pfc_detailed"`
	EffectiveCategoryName *string `json:"effective_category_name,omitempty" db:"effective_category_name"`

	// Tags
	Tags       []string    `json:"tags" db:"tags"`
	TagsSource []TagSource `json:"tags_source" db:"tags_source"`

	// Timestamps
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`

	// Associated data (populated on demand, not stored in DB)
	Account *Account `json:"account,omitempty" db:"-"`
}

// EffectiveCategoryKind is the discriminator for which category dimension is active.
type EffectiveCategoryKind string

const (
	EffectiveCategoryKindPFC    EffectiveCategoryKind = "pfc"
	EffectiveCategoryKindCustom EffectiveCategoryKind = "custom"
)

// EffectiveCategory is the resolved category for a transaction — either PFC or custom, never both.
type EffectiveCategory struct {
	Kind               EffectiveCategoryKind `json:"kind"`
	PFCPrimary         *string               `json:"pfc_primary,omitempty"`
	PFCDetailed        *string               `json:"pfc_detailed,omitempty"`
	PFCPrimaryDisplay  *string               `json:"pfc_primary_display,omitempty"`
	PFCDetailedDisplay *string               `json:"pfc_detailed_display,omitempty"`
	CustomCategoryID   *string               `json:"custom_category_id,omitempty"`
}

// GetEffectiveCategory resolves the active category for this transaction.
// Priority: override (custom > pfc) > source PFC > nil (uncategorized).
// Display fields on the returned EffectiveCategory are populated from the pfc registry.
func (t *Transaction) GetEffectiveCategory() *EffectiveCategory {
	if t.CategoryOverrideKind != nil {
		switch *t.CategoryOverrideKind {
		case "custom":
			return &EffectiveCategory{Kind: EffectiveCategoryKindCustom, CustomCategoryID: t.CustomCategoryOverrideID}
		case "pfc":
			ec := &EffectiveCategory{Kind: EffectiveCategoryKindPFC, PFCPrimary: t.PFCPrimaryOverride, PFCDetailed: t.PFCDetailedOverride}
			if t.PFCPrimaryOverride != nil && *t.PFCPrimaryOverride != "" {
				d := pfc.PrimaryDisplayName(*t.PFCPrimaryOverride)
				ec.PFCPrimaryDisplay = &d
			}
			if t.PFCPrimaryOverride != nil && t.PFCDetailedOverride != nil && *t.PFCDetailedOverride != "" {
				d := pfc.DetailedDisplayName(*t.PFCPrimaryOverride, *t.PFCDetailedOverride)
				ec.PFCDetailedDisplay = &d
			}
			return ec
		}
	}
	if t.PFCPrimary != nil {
		ec := &EffectiveCategory{Kind: EffectiveCategoryKindPFC, PFCPrimary: t.PFCPrimary, PFCDetailed: t.PFCDetailed}
		if *t.PFCPrimary != "" {
			d := pfc.PrimaryDisplayName(*t.PFCPrimary)
			ec.PFCPrimaryDisplay = &d
		}
		if t.PFCDetailed != nil && *t.PFCDetailed != "" {
			d := pfc.DetailedDisplayName(*t.PFCPrimary, *t.PFCDetailed)
			ec.PFCDetailedDisplay = &d
		}
		return ec
	}
	return nil
}

// Validate checks that the Transaction has all required fields and valid values.
// Returns an error if validation fails.
//
// Validation rules:
//   - UserID is required
//   - Source is required and must be a valid TransactionSource
//   - ExternalTransactionID is required for all non-manual transactions
//   - Name is required
func (t *Transaction) Validate() error {
	if t.UserID == "" {
		return errors.New("user_id is required")
	}

	if t.Source == "" {
		return errors.New("source is required")
	}

	// Validate source is a known provider
	validSources := map[TransactionSource]bool{
		SourcePlaid:    true,
		SourceManual:   true,
		SourceTeller:   true,
		SourceFinicity: true,
		SourceYodlee:   true,
	}
	if !validSources[t.Source] {
		return errors.New("invalid transaction source")
	}

	// External ID is required for all non-manual transactions
	// Manual transactions are user-created and don't have provider IDs
	if t.Source != SourceManual {
		if t.ExternalTransactionID == nil || *t.ExternalTransactionID == "" {
			return errors.New("external_transaction_id required for non-manual transactions")
		}
	}

	if t.Name == "" {
		return errors.New("name is required")
	}

	return nil
}

// IsOutflow returns true when the transaction is an outflow (positive amount = spending/debit under Plaid PFCv2).
func (t Transaction) IsOutflow() bool { return t.Amount > 0 }

// IsInflow returns true when the transaction is an inflow (negative amount = refund/income under Plaid PFCv2).
func (t Transaction) IsInflow() bool { return t.Amount < 0 }

// ExpenseAmount returns the transaction amount when it is an outflow, and 0 otherwise.
// Always non-negative.
func (t Transaction) ExpenseAmount() float64 {
	if t.Amount > 0 {
		return t.Amount
	}
	return 0
}

// IncomeAmount returns the absolute transaction amount when it is an inflow, and 0 otherwise.
// Always non-negative.
func (t Transaction) IncomeAmount() float64 {
	if t.Amount < 0 {
		return -t.Amount
	}
	return 0
}

// TopUnreviewedCategory holds aggregated counts of unreviewed transactions grouped by
// a stable category key (custom_category_override_id or PFC primary enum).
// Issue #1210.
type TopUnreviewedCategory struct {
	// CategoryKey is the stable grouping key: custom-category UUID for custom, PFC primary enum otherwise.
	CategoryKey string `json:"category_key"`
	// DisplayName is a human-readable label derived from the stable key.
	DisplayName string `json:"display_name"`
	// Count is the number of unreviewed transactions in this category bucket.
	Count int64 `json:"count"`
}

// ReviewSummaryResponse is the structured response for GET /api/v1/transactions/review-summary.
// Designed for AI-tool consumption: single-responsibility, pre-computed, token-efficient.
// Issue #1210.
type ReviewSummaryResponse struct {
	// UnreviewedCount is the total number of transactions not yet reviewed.
	UnreviewedCount int64 `json:"unreviewed_count"`
	// ReviewedByRuleCount is the number of transactions reviewed via automation rule.
	ReviewedByRuleCount int64 `json:"reviewed_by_rule_count"`
	// ReviewedByManualCount is the number of transactions reviewed via manual user action.
	ReviewedByManualCount int64 `json:"reviewed_by_manual_count"`
	// ReviewedByPlaidCount is the number of transactions reviewed with original Plaid category (approve-as-is).
	ReviewedByPlaidCount int64 `json:"reviewed_by_plaid_count"`
	// AvgIngestionToReviewSec is the average time in seconds from transaction creation (sync time)
	// to when it was reviewed. Named "ingestion-to-review" because created_at reflects sync time,
	// not the original transaction date; historical imports skew this metric.
	// Nil when no reviewed transactions exist.
	AvgIngestionToReviewSec *float64 `json:"avg_ingestion_to_review_seconds,omitempty"`
	// TopUnreviewedCategories is the top-5 unreviewed category buckets, ordered by count descending.
	// Empty slice (not null) when no unreviewed transactions exist.
	TopUnreviewedCategories []TopUnreviewedCategory `json:"top_unreviewed_categories"`
}

// ReviewSummaryFilters provides optional date-range scoping for the review summary query.
// Both fields are optional — omitting them returns all-time data (transfers included).
// Issue #1210.
type ReviewSummaryFilters struct {
	// StartDate optionally scopes the summary to transactions on or after this date.
	StartDate *time.Time
	// EndDate optionally scopes the summary to transactions on or before this date.
	EndDate *time.Time
}

// TransactionFilters represents optional filters for querying transactions.
// All fields are optional pointers - nil means no filter applied for that field.
// Used by repository layer to build dynamic WHERE clauses.
type TransactionFilters struct {
	// AccountID filters transactions by specific account (deprecated: use AccountIDs)
	AccountID *string

	// AccountIDs filters transactions by multiple accounts (OR logic within)
	// Issue #529: Multi-account filter support
	AccountIDs []string

	// StartDate filters transactions on or after this date (inclusive)
	StartDate *time.Time

	// EndDate filters transactions on or before this date (inclusive)
	EndDate *time.Time

	// Uncategorized selects transactions with NO category at all — the rows the analytics layer
	// reports under its synthesised 'UNCATEGORIZED' sentinel (#1626 Phase 2). It exists because
	// that sentinel is not a value any column holds, so `pfc_primaries=UNCATEGORIZED` matches
	// nothing and the Sankey's Uncategorized node had no way to be clickable.
	//
	// A plain bool, like IncludeReversed below: this is a positive-only opt-in, so false and absent
	// both mean "no predicate". Absence is expressed at the handler boundary, not in the type.
	// It joins the OR group with the three fields below — see categoryFilterGroup.
	Uncategorized bool

	// PFCPrimaries filters by effective PFC primary enum(s).
	// Matches the effective category: override if set, else source pfc_primary.
	//
	// #1626: PFCPrimaries, PFCDetaileds and CustomCategoryIDs are ONE OR-joined group — a
	// transaction matches if any of them matches. They used to be separate AND-ed conditions,
	// which returned zero rows whenever a PFC and a custom filter were combined: a row is either
	// pfc-kind or custom-kind (effective_pfc_primary is NULL on custom rows), so the conjunction
	// of the two selected disjoint sets. See categoryFilterGroup.
	PFCPrimaries []string

	// PFCDetaileds filters by effective PFC detailed sub-category enum(s) (OR logic within).
	// Combined with PFCPrimaries using OR: a transaction matches if its primary is in PFCPrimaries
	// OR its detailed is in PFCDetaileds.
	PFCDetaileds []string

	// CustomCategoryIDs filters by effective custom category override ID(s) (OR logic within).
	CustomCategoryIDs []string

	// IncludeReversed admits retained provider REVERSALS into the result set: rows tombstoned
	// with TxnDeletedProviderRemovedPosted, i.e. transactions the bank withdrew after they had
	// already posted. Issue #1569.
	//
	// Default false, and that default is load-bearing rather than conservative: a tombstoned row
	// that leaks into a total the user has already reconciled is the same silent change this
	// issue exists to stop, arriving from the other direction. Settled pendings
	// (TxnDeletedProviderRemovedPending) are NEVER admitted by this flag — nothing admits them.
	//
	// Callers that set it must render Transaction.Reversed() distinctly; an unlabelled reversal
	// in a list is worse than a hidden one.
	IncludeReversed bool

	// Search filters transactions by merchant name or transaction name
	// Uses ILIKE for case-insensitive partial matching
	Search *string

	// Pending filters transactions by pending status
	// True = only pending, False = only settled, nil = both
	Pending *bool

	// Reviewed filters by review state (Issue #1203).
	// True = only reviewed, False = only unreviewed (inbox), nil = both
	Reviewed *bool

	// CategorySources filters by category origin (OR logic within): plaid, rule, manual.
	// Nil/empty means no filter. Issue #1210.
	CategorySources []string

	// MinAmount filters transactions with absolute amount >= this value
	// Issue #529: Amount range filter support
	MinAmount *float64

	// MaxAmount filters transactions with absolute amount <= this value
	// Issue #529: Amount range filter support
	MaxAmount *float64

	// AmountType filters transactions by direction: "income" (amount < 0, inflow), "expense" (amount > 0, outflow), or nil for both
	// Issue #529: Transaction type filter support
	AmountType *string

	// UserAccountID filters transactions by logical account identity (user_accounts.id).
	// Stable across re-links: returns transactions from both old and new chain members
	// for this logical account. Use instead of AccountID when the caller has a
	// user_account_id (e.g. the frontend after re-link transparency work in #1109).
	UserAccountID *string

	// MerchantID filters transactions by a specific merchant UUID.
	// Nil means no filter; non-nil matches exactly the given merchant_id.
	MerchantID *string

	// SortBy specifies the column to sort by. Allowed values: date, merchant, amount, category.
	// Defaults to "date" when empty. Validated by Huma enum tag at the operation layer.
	SortBy string

	// SortDir specifies sort direction. Allowed values: asc, desc. Defaults to "desc" when empty.
	SortDir string

	// IncludeInvestmentActivity merges investment_transactions into the feed when true.
	// Requires SortBy="date" (or empty, which defaults to date). Returns
	// ErrInvestmentMergeRequiresDateSort when combined with a non-date SortBy.
	// Default false — service layer sets true based on query param.
	IncludeInvestmentActivity bool

	// ActivityTypes filters investment activity by normalized trade type when
	// IncludeInvestmentActivity=true. Empty = no filter. Values: buy/sell/dividend/
	// reinvest/fee/cash/other (see domain.TradeType* constants).
	ActivityTypes []string
}

// Reversed reports whether this row is a retained, surfacable provider reversal — a transaction
// that had already POSTED when the provider withdrew it. It is the only tombstone class any read
// is allowed to return, and only when the caller asked for it
// (TransactionFilters.IncludeReversed).
//
// A settled pending (TxnDeletedProviderRemovedPending) deliberately returns false here as well as
// being excluded at the query: two independent gates, because showing one double-counts real money
// against its posted twin.
func (t *Transaction) Reversed() bool {
	return t.DeletedAt != nil &&
		t.DeletedReason != nil &&
		*t.DeletedReason == TxnDeletedProviderRemovedPosted
}

// FeedTotals carries whole-result-set aggregates for a transaction feed page (#1626 Phase 2).
//
// WHY IT EXISTS. The cursor-paginated feed returned only a page of rows, so nothing in the product
// could answer "how much does this filtered set come to". That made the reports Sankey's
// click-through unverifiable: a node showed an amount, the list showed rows, and no one could tell
// whether they agreed. They now can.
//
// ⚠ WHAT "AGREED" MEANS, exactly. Amount is SIGNED, following the rows (PFCv2: expenses positive,
// inflows negative). A category-hierarchy node's amount is COALESCE(SUM(ABS(t.amount)),0), an
// unsigned chart quantity. The property that holds over the same filtered set is therefore
// |node amount| == |Amount| — equal outright for an expense node, off by a factor of −1 for an
// income one. Five comments on this feature asserted plain equality before review caught it; both
// SQL expressions are correct for their own consumer (a chart wants magnitudes, a ledger total
// wants signs), so the sentence is what needed fixing, not the queries.
//
// ⚠ Populated ONLY for the FIRST page (no cursor supplied). Computing it on every page would put a
// full aggregate over the filtered set on the busiest list in the app, for a number that does not
// change as the user pages. A nil pointer means "not computed", never "zero".
type FeedTotals struct {
	Count  int     `json:"count"`
	Amount float64 `json:"amount"`
}
