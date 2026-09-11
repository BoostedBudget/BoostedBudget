package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/lib/pq"
	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

var (
	// ErrTransactionNotFound is returned when a transaction is not found in the database
	ErrTransactionNotFound = errors.New("transaction not found")
	// ErrDuplicateTransaction is returned when attempting to create a transaction that already exists
	ErrDuplicateTransaction = errors.New("transaction already exists for this source and external ID")
)

// transactionSelectColumns is the canonical ordered list of columns for SELECT queries
// that feed scanTransactions. Order must exactly match the Scan() destinations in
// scanTransactions. Every method calling scanTransactions MUST use transactionSelectExpr.
// Adding or removing a column requires updating both this slice and scanTransactions.Scan().
var transactionSelectColumns = []string{
	"id", "user_id", "account_id", "linked_item_id",
	"source", "external_transaction_id", "external_metadata",
	"amount", "date", "authorized_date", "name", "merchant_name", "merchant_id",
	"pfc_primary", "pfc_detailed", "pfc_confidence_level", "pfc_category_icon_url",
	"payment_channel", "pending", "user_notes", "created_at", "updated_at",
	"tags", "tags_source",
	"category_override_kind", "pfc_primary_override", "pfc_detailed_override",
	"custom_category_override_id", "category_source", "category_override_date",
	"effective_pfc_primary", "effective_pfc_detailed", "effective_category_name",
	"reviewed", "reviewed_at",
	// Tombstone provenance (#1569). Appended LAST on purpose: transactionSelectColumns and
	// scanTransactionDestSliceWithState are positionally coupled, and appending to both tails
	// keeps every existing offset unchanged.
	"deleted_at", "deleted_reason", "deleted_by",
}

var transactionSelectExpr = strings.Join(transactionSelectColumns, ", ")

// transactionSelectExprT is transactionSelectColumns with "t." prefix on each column.
// Used in queries that alias FROM transactions as t.
var transactionSelectExprT = func() string {
	cols := make([]string, len(transactionSelectColumns))
	for i, c := range transactionSelectColumns {
		cols[i] = "t." + c
	}
	return strings.Join(cols, ", ")
}()

// TransactionRepository defines the interface for transaction data access.
// Supports transactions from all providers (Plaid, manual, Teller, etc.)
type TransactionRepository interface {
	// CreateBatch creates multiple transactions in a single database operation.
	// Returns a map of transaction IDs that were genuinely inserted (as opposed to
	// conflict-preserved by ON CONFLICT DO UPDATE).  Callers use this to gate
	// analytics / match-count side-effects on fresh inserts only (Issue #1213).
	//
	// ⚠ IT ALSO WRITES THE PERSISTED id BACK INTO EACH ELEMENT (#1586). A conflict keeps the id the
	// row was first stored with, so a caller that minted its own UUID is otherwise left holding one
	// that names no row. Read ids out of the slice AFTER the call, never from before it. The
	// returned map is the other question — "is this row new?" — and does NOT widen to conflict rows.
	CreateBatch(ctx context.Context, transactions []domain.Transaction) (map[string]bool, error)

	// Update updates an existing transaction
	Update(ctx context.Context, transaction *domain.Transaction) error

	// TombstoneByExternalID marks a provider-removed transaction as deleted WITHOUT destroying
	// it, and reports which class of removal it turned out to be. Issue #1569.
	//
	// It replaces the DeleteByExternalID hard delete that used to serve Plaid's `removed` array.
	// That delete was the last unrecoverable destroyer of user-authored work in the sync path:
	// category overrides, tags, notes and `reviewed` state have no other copy anywhere, no user
	// action was involved in triggering it, and migration 000020's CASCADE took the
	// rule_execution_history trail with it.
	//
	// The reason is decided IN SQL from the row's own `pending` column, not by the caller, so
	// there is no read-then-write window in which a pending row posts between the two statements
	// and gets classified as the wrong kind of removal.
	//
	// Returns ErrTransactionNotFound when no LIVE row matched — either the transaction was never
	// stored or it is already tombstoned. Both are the idempotent case, and callers rely on being
	// able to tell them apart from a real database failure: #1570's storage-before-cursor
	// contract makes re-delivery of an already-processed removal the NORMAL outcome of a failed
	// sync, while a genuine failure must abort before the cursor advances.
	TombstoneByExternalID(
		ctx context.Context,
		source domain.TransactionSource,
		externalID string,
		userID string,
		deletedBy string,
	) (domain.TransactionDeletedReason, error)

	// PullForwardSettledPendingAnnotations is the storage-side MIRROR of the settlement move that
	// TombstoneByExternalID performs (#1586). Given transactions a sync has just stored, it finds
	// any that carry Plaid's pending_transaction_id pointing at a pending row ALREADY tombstoned as
	// a settlement, and copies that row's stranded user annotations forward onto the visible twin.
	//
	// It exists because the tombstone-side move can only see a twin that is already stored: when
	// Plaid delivers the removal in one sync and the posted twin in a later one, the user's
	// category, notes, tags and review state stay on a row that renders nowhere.
	//
	// Returns the number of twins written. Idempotent: a second pass over the same ids finds the
	// annotations already in place and both writes and records nothing.
	PullForwardSettledPendingAnnotations(ctx context.Context, userID string, transactionIDs []string) (int64, error)

	// GetByUserID retrieves transactions for a specific user with pagination
	GetByUserID(ctx context.Context, userID string, limit, offset int) ([]domain.Transaction, error)

	// GetByAccountID retrieves all transactions for a specific account
	GetByAccountID(ctx context.Context, accountID string) ([]domain.Transaction, error)

	// GetBySource retrieves all transactions from a specific source for a user
	GetBySource(ctx context.Context, source domain.TransactionSource, userID string) ([]domain.Transaction, error)

	// GetByUserIDWithFilters retrieves transactions for a specific user with optional filters and pagination
	// Filters: account_id, date range, category, search, pending status
	// All filters are optional (nil = not applied)
	// Returns transactions ordered by date DESC
	GetByUserIDWithFilters(ctx context.Context, userID string, filters domain.TransactionFilters, limit, offset int) ([]domain.Transaction, error)

	// CountByUserID returns the total count of transactions for a user with optional filters
	// Used for pagination metadata (total_count, has_more)
	CountByUserID(ctx context.Context, userID string, filters domain.TransactionFilters) (int, error)

	// GetByExternalID retrieves a transaction by its provider-specific external ID
	// Used during transaction sync to lookup existing transactions for updates
	// Returns ErrTransactionNotFound if transaction doesn't exist
	GetByExternalID(ctx context.Context, source domain.TransactionSource, externalID string, userID string) (*domain.Transaction, error)

	// GetByID retrieves a transaction by its internal ID
	// Returns ErrTransactionNotFound if transaction doesn't exist
	GetByID(ctx context.Context, id string) (*domain.Transaction, error)

	// UpdateTags updates the tags and tags_source for a transaction
	// Used when manually editing transaction tags
	UpdateTags(ctx context.Context, id string, userID string, tags []string, tagsSource []domain.TagSource) error

	// UpdateCategory updates the category override for a transaction using the PFC v2 discriminated union.
	// Pass overrideKind=nil to clear all overrides (kind=clear semantics).
	UpdateCategory(ctx context.Context, id string, userID string, overrideKind *string, pfcPrimaryOverride *string, pfcDetailedOverride *string, customCategoryOverrideID *string, categorySource domain.CategorySource) error

	// SetReviewed sets the review flag for the given transactions owned by userID
	// (Issue #1203). Returns the number of rows updated; IDs not owned by the user
	// are ignored. reviewed_at is set to now when reviewed=true, cleared when false.
	SetReviewed(ctx context.Context, userID string, transactionIDs []string, reviewed bool) (int64, error)

	// BulkUpdateCategory recategorizes many transactions at once using a single UPDATE (Issue #1210).
	// Best-effort: IDs not owned by userID are silently skipped. Always 200.
	// Sets reviewed=true + reviewed_at=now + category_source='manual' + effective_category_name
	// using the same CASE logic as UpdateCategory. Returns rows affected.
	BulkUpdateCategory(ctx context.Context, userID string, transactionIDs []string, overrideKind *string, pfcPrimaryOverride *string, pfcDetailedOverride *string, customCategoryOverrideID *string, categorySource domain.CategorySource) (int64, error)

	// GetReviewSummary returns pre-computed review-state counts and top unreviewed categories (Issue #1210).
	// Single CTE query; dates in filters are optional (all-time default). Transfers included.
	GetReviewSummary(ctx context.Context, userID string, filters domain.ReviewSummaryFilters) (*domain.ReviewSummaryResponse, error)

	// GetDistinctPlaidCategories returns distinct Plaid category names for a user's transactions
	// Prefers secondary category (index 1) when available for better granularity
	// Falls back to primary category (index 0) when secondary is not available
	// Used by the rule editor to populate the "existing_category" dropdown
	GetDistinctPlaidCategories(ctx context.Context, userID string) ([]string, error)

	// Issue #492: Income Source Matching Service

	// GetIncomeSumByCategory returns the sum of income (positive amount) transactions
	// matching the given category within the date range.
	// Checks both category and category_override fields using JSONB containment.
	GetIncomeSumByCategory(ctx context.Context, userID string, category string, startDate, endDate time.Time) (float64, error)

	// GetIncomeSumByTag returns the sum of income (positive amount) transactions
	// with the given tag within the date range.
	GetIncomeSumByTag(ctx context.Context, userID string, tag string, startDate, endDate time.Time) (float64, error)

	// Issue #493: Budget Category Matching Service

	// GetSpentSumForBudgetCategory returns the sum of expenses matching a budget category.
	// Uses the budget category's discriminated union (pfc/custom) to match against the
	// transaction's effective category. Supports all four match logic types.
	GetSpentSumForBudgetCategory(ctx context.Context, userID string, budgetCat *domain.BudgetCategory, startDate, endDate time.Time) (float64, error)

	// Issue #497: Category Preview Feature

	// GetMatchingTransactionStats returns the count and sum of expenses matching the given
	// category configuration (discriminated union fields) and/or tags.
	// Used for previewing how many transactions would match a budget category configuration.
	GetMatchingTransactionStats(ctx context.Context, userID string, categoryKind string, pfcPrimary *string, pfcDetailed *string, customCategoryID *string, tags []string, matchLogic domain.MatchLogic, startDate, endDate time.Time) (count int, totalAmount float64, err error)

	// Issue #499: Transaction-Budget Linking UI

	// GetTransactionsForBudgetCategory returns paginated transactions matching a budget category.
	// Uses the budget category's discriminated union to match against effective category.
	// Returns transactions ordered by date DESC.
	GetTransactionsForBudgetCategory(ctx context.Context, userID string, budgetCat *domain.BudgetCategory, startDate, endDate time.Time, limit, offset int) (transactions []domain.Transaction, totalCount int, totalAmount float64, err error)

	// Issue #171: Re-Link Handling

	// GetByUserAccountID retrieves all transactions for a specific user_account.
	// This provides stable queries across re-links since user_account_id survives
	// when a user re-links a bank (which assigns new item_id and account_id values).
	// Returns transactions from all associated accounts (current and replaced) ordered by date DESC.
	GetByUserAccountID(ctx context.Context, userID string, userAccountID string) ([]domain.Transaction, error)

	// UpdateEffectiveCategoryNameByCustomCategoryID bulk-updates effective_category_name
	// for all transactions in a user's account that are assigned to the given custom category.
	// Used when a custom category is renamed. Returns the number of rows updated.
	UpdateEffectiveCategoryNameByCustomCategoryID(ctx context.Context, userID string, customCategoryID string) (int64, error)

	// GetByUserIDKeyset retrieves transactions using keyset (cursor) pagination.
	// cursor=nil returns the first page. Fetches limit+1 rows to detect end-of-list.
	// Returns a slice of up to limit transactions and the cursor for the next page (nil = end of list).
	// The third return is whole-set totals for the filtered set, non-nil ONLY when cursor is nil
	// (the first page). nil means "not computed", never "zero" — see domain.FeedTotals (#1626).
	GetByUserIDKeyset(
		ctx context.Context,
		userID string,
		filters domain.TransactionFilters,
		cursor *domain.TransactionCursor,
		limit int,
	) ([]domain.Transaction, *domain.TransactionCursor, *domain.FeedTotals, error)

	// GetRecentInflowsByUserID returns posted (non-pending) inflow transactions
	// (amount < 0 under PFCv2) for a user with a date on or after since.
	// Used by TransferCorrelationService to match transfer suggestions.
	// Issue #972: Savings transfer automation.
	GetRecentInflowsByUserID(ctx context.Context, userID string, since time.Time) ([]*domain.Transaction, error)

	// GetInflowTransactionsForAccount returns posted inflow transactions
	// (amount < 0 under PFCv2) for a specific account within [start, end].
	// Chain-aware: expands to include transactions from all accounts in the re-link
	// chain via user_account_id (Issue #1109 pattern).
	// Returns lightweight projections with ABS(amount) as Magnitude — always positive.
	// Used by the savings progress service to detect and propose contributions
	// (Issue #1300 plan section 1D).
	GetInflowTransactionsForAccount(ctx context.Context, userID, accountID string, start, end time.Time) ([]*domain.SavingsInflowTransaction, error)

	// GetByUserIDKeysetUnified returns a merged feed of banking transactions and investment
	// transactions using a 3-part keyset (sort_date DESC, source ASC, id ASC).
	// Issue #138: unified activity feed with cursor-correct UNION ALL.
	// Requires filters.SortBy = "date" (enforced at service layer).
	// Returns V:2 cursor; nil cursor = end of list.
	// The third return is whole-set totals, non-nil ONLY on the first page — see GetByUserIDKeyset.
	GetByUserIDKeysetUnified(
		ctx context.Context,
		userID string,
		filters domain.TransactionFilters,
		cursor *domain.TransactionCursor,
		limit int,
	) ([]*domain.ActivityItem, *domain.TransactionCursor, *domain.FeedTotals, error)

	// ResolveEffectiveMerchantNames returns a map of merchant ID → effective display name
	// for the given set of merchant IDs and user, in one DB round-trip.
	// Resolution follows the per-user merge topology and override chain:
	//   COALESCE(source_override.display_name, target_override.display_name,
	//            canonical_merchant.normalized_name, canonical_merchant.name)
	// Merchant IDs absent from the merchants table are omitted from the result.
	// Used by enrichEffectiveMerchantNames (Transaction slices) and
	// enrichEffectiveMerchantNamesForItems (ActivityItem slices / unified feed).
	ResolveEffectiveMerchantNames(ctx context.Context, userID string, merchantIDs []string) (map[string]string, error)
}

// transactionRepository implements TransactionRepository using PostgreSQL
type transactionRepository struct {
	db  DB
	enc *appcrypto.FieldEncryptor
}

// NewTransactionRepository creates a new TransactionRepository instance
func NewTransactionRepository(db DB, enc *appcrypto.FieldEncryptor) TransactionRepository {
	return &transactionRepository{db: db, enc: enc}
}

// encryptTransactionPIIFields encrypts PII fields on a transaction in-place.
// Returns the computed blind index for external_transaction_id (nil when ExternalTransactionID is nil).
func (r *transactionRepository) encryptTransactionPIIFields(t *domain.Transaction) (extIDBlindIdx *string, err error) {
	// Compute blind index from plaintext before encrypting; keep nil when source is nil.
	if t.ExternalTransactionID != nil {
		blind := r.enc.BlindIndex(*t.ExternalTransactionID)
		extIDBlindIdx = &blind
		encrypted, encErr := r.enc.EncryptNullableString(t.ExternalTransactionID)
		if encErr != nil {
			return nil, fmt.Errorf("encrypt external_transaction_id: %w", encErr)
		}
		t.ExternalTransactionID = encrypted
	}

	encMeta, encErr := r.enc.EncryptNullableString(t.ExternalMetadata)
	if encErr != nil {
		return nil, fmt.Errorf("encrypt external_metadata: %w", encErr)
	}
	t.ExternalMetadata = encMeta

	return extIDBlindIdx, nil
}

// pendingTwinBlindIndex derives transactions.pending_transaction_id_blind_idx from
// domain.Transaction.PendingTransactionID (Plaid's pending_transaction_id). Issue #1578.
//
// It MUST use the same BlindIndex function external_transaction_id_blind_idx is derived with,
// because the settlement move compares the two columns directly: a posted row's pointer is matched
// against the pending row's external id. Deriving them differently would make every settlement
// silently un-matchable, with no error anywhere.
//
// nil in, nil out. Almost every transaction is not a settlement, and NULL is the honest value.
func (r *transactionRepository) pendingTwinBlindIndex(t *domain.Transaction) *string {
	if t.PendingTransactionID == nil || *t.PendingTransactionID == "" {
		return nil
	}
	blind := r.enc.BlindIndex(*t.PendingTransactionID)
	return &blind
}

// decryptTransactionPIIFields decrypts PII fields on a transaction in-place.
func (r *transactionRepository) decryptTransactionPIIFields(t *domain.Transaction) error {
	decExtID, err := r.enc.DecryptNullableString(t.ExternalTransactionID)
	if err != nil {
		return fmt.Errorf("decrypt external_transaction_id: %w", err)
	}
	t.ExternalTransactionID = decExtID

	decMeta, err := r.enc.DecryptNullableString(t.ExternalMetadata)
	if err != nil {
		return fmt.Errorf("decrypt external_metadata: %w", err)
	}
	t.ExternalMetadata = decMeta

	return nil
}

// CreateBatch creates multiple transactions in a single database operation
// Uses a multi-row INSERT for performance.
//
// Issue #171: Changed from DO NOTHING to DO UPDATE for re-link handling.
// When a duplicate transaction is encountered (same source, external_transaction_id_blind_idx, user_id),
// we update the FKs (account_id, linked_item_id, user_account_id) and Plaid fields while
// PRESERVING user enrichments (user_notes, category_override, tags, tags_source).
//
// Issue #1213: Returns a map of transaction IDs that were genuinely inserted.
// RETURNING id, (xmax::text::bigint = 0) AS inserted lets us distinguish fresh
// inserts (xmax=0) from conflict-updates (xmax!=0) in a single round-trip.
//
// ⚠ WHY THIS METHOD HAS NO TENANCY ASSERTION, while its siblings do (#1537 round 9, F13). A review
// flagged it as "the same unguarded shape" as the four ReplaceForAccount methods that gained
// assertActorOwnsRows/assertRowsTargetAccount under #1566/#1568. It is not the same shape, and the
// difference is the whole reason those assertions exist:
//
//   - Those methods DELETE by a scalar accountID while INSERTing each element's own AccountID, so the
//     two can disagree and the mismatch silently moves data between accounts. That gap is what is
//     being asserted away.
//   - This method takes no scalar actor or account. Every row is self-describing, there is no scalar
//     for a row to disagree with, and the ON CONFLICT target is user-scoped by construction. A
//     mixed-tenant batch here would require the SERVICE to assemble one, which is a different defect
//     in a different layer — and adding an actor parameter would change the interface and every mock
//     to assert something no code path can currently violate.
//
// The tenancy risk on this path was closed at its actual source instead: the sync services now reject
// accounts whose user_id disagrees with the linked item's owner before any row is built
// (investment_sync_service.go, liability_sync_service.go). Compare
// investment_transaction_repository.go's UpsertBatch, which DID need a predicate, because its conflict
// target is a Plaid id that carries no ownership at all.
func (r *transactionRepository) CreateBatch(ctx context.Context, transactions []domain.Transaction) (map[string]bool, error) {
	if len(transactions) == 0 {
		return nil, nil
	}

	// Build multi-row INSERT query
	// Issue #171: Added user_account_id field; encryption adds external_transaction_id_blind_idx
	// Issue #1123: Added effective_category_name (24 fields)
	// Issue #1163: Added merchant_id (25 fields)
	// Issue #1203: persist category override + category_source + review state on
	// initial insert (was the documented #TODO-createbatch-overrides bug). 32 fields.
	// Issue #1578: added pending_transaction_id_blind_idx (33 fields). The placeholder maths a few
	// lines below is generated from numFields, so a new field is exactly three edits: this
	// constant, one column name in the INSERT list, one append to valueArgs in the SAME position.
	const numFields = 33
	valueStrings := make([]string, 0, len(transactions))
	valueArgs := make([]interface{}, 0, len(transactions)*numFields)

	// #1586 (gap G7): the blind index of each element, in input order. It is the ONLY key that
	// survives the round trip -- the id does not, because a conflict row keeps the id it was first
	// stored with -- so it is what the persisted ids returned below are matched back on.
	blindIdxByInput := make([]*string, len(transactions))

	for i, tx := range transactions {
		// Generate UUID if not set
		if tx.ID == "" {
			transactions[i].ID = uuid.New().String()
		}

		// Set timestamps
		now := time.Now()
		transactions[i].CreatedAt = now
		transactions[i].UpdatedAt = now

		// Encrypt PII fields and compute blind index before writing
		extIDBlindIdx, err := r.encryptTransactionPIIFields(&transactions[i])
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt transaction fields: %w", err)
		}
		blindIdxByInput[i] = extIDBlindIdx

		// Create placeholders for this row
		placeholders := make([]string, numFields)
		for j := 0; j < numFields; j++ {
			placeholders[j] = fmt.Sprintf("$%d", i*numFields+j+1)
		}
		valueStrings = append(valueStrings, fmt.Sprintf("(%s)", strings.Join(placeholders, ", ")))

		// Compute effective_category_name from in-memory override values (Issue #1203).
		// For a rule-applied custom override the name is filled in by the async
		// effective_category_refresh job if not already populated in memory.
		effectiveCategoryName := transactions[i].EffectiveCategoryName
		switch {
		case transactions[i].CategoryOverrideKind != nil && *transactions[i].CategoryOverrideKind == "pfc":
			if transactions[i].PFCPrimaryOverride != nil && *transactions[i].PFCPrimaryOverride != "" {
				effectiveCategoryName = transactions[i].PFCPrimaryOverride
			} else {
				effectiveCategoryName = transactions[i].PFCPrimary
			}
		case transactions[i].CategoryOverrideKind != nil && *transactions[i].CategoryOverrideKind == "custom":
			// leave as in-memory EffectiveCategoryName (may be nil; async refresh fills it)
		default:
			effectiveCategoryName = transactions[i].PFCPrimary
		}

		// category_source defaults to 'plaid' when unset to satisfy the CHECK constraint.
		categorySource := string(transactions[i].CategorySourceField)
		if categorySource == "" {
			categorySource = string(domain.CategorySourcePlaid)
		}

		// Add values (including user_account_id and external_transaction_id_blind_idx)
		valueArgs = append(valueArgs,
			transactions[i].ID,
			transactions[i].UserID,
			transactions[i].AccountID,
			transactions[i].LinkedItemID,
			transactions[i].UserAccountID, // Issue #171: Stable reference for re-links
			string(transactions[i].Source),
			transactions[i].ExternalTransactionID,
			extIDBlindIdx,
			transactions[i].ExternalMetadata,
			transactions[i].Amount,
			transactions[i].Date,
			transactions[i].AuthorizedDate,
			transactions[i].Name,
			transactions[i].MerchantName,
			transactions[i].MerchantID,
			transactions[i].PFCPrimary,
			transactions[i].PFCDetailed,
			transactions[i].PFCConfidenceLevel,
			transactions[i].PFCCategoryIconURL,
			transactions[i].PaymentChannel,
			transactions[i].Pending,
			transactions[i].UserNotes,
			transactions[i].CreatedAt,
			transactions[i].UpdatedAt,
			effectiveCategoryName,
			// Issue #1203: category override discriminated union + review state
			transactions[i].CategoryOverrideKind,
			transactions[i].PFCPrimaryOverride,
			transactions[i].PFCDetailedOverride,
			transactions[i].CustomCategoryOverrideID,
			categorySource,
			transactions[i].Reviewed,
			transactions[i].ReviewedAt,
			// Issue #1578. Position must match the INSERT column list's last entry.
			r.pendingTwinBlindIndex(&transactions[i]),
		)
	}

	// Issue #171: Changed from DO NOTHING to DO UPDATE for re-link handling.
	// On conflict (duplicate transaction by blind index), we:
	// - UPDATE: FKs (account_id, linked_item_id, user_account_id) - changes on re-link
	// - UPDATE: Plaid fields (amount, date, name, merchant_name, pending, etc.) - may change
	// - UPDATE: external_transaction_id_blind_idx in case it was previously NULL
	//
	// ⚠ WHAT IS ACTUALLY PRESERVED (#1578 correction). This comment used to name FOUR columns --
	// user_notes, category_override_kind, tags, tags_source -- and planning against that list
	// silently drops four more. Derived from the clause below rather than from memory, the
	// USER-AUTHORED columns this upsert preserves on conflict are:
	//
	//   in the INSERT list but absent from the SET clause, so EXCLUDED never reaches them:
	//     user_notes, category_override_kind, pfc_primary_override, pfc_detailed_override,
	//     custom_category_override_id, category_source, reviewed, reviewed_at
	//   never in the INSERT column list AT ALL, so a conflict cannot touch them and neither can a
	//   fresh insert -- see the tags gap noted below:
	//     tags, tags_source, category_override_date
	//   conditionally, via its own CASE: effective_category_name is preserved when the stored row
	//     carries an override, and refreshed from EXCLUDED.pfc_primary when it does not.
	//
	// (id, user_id, source, external_transaction_id and created_at are also preserved; they are
	// identity and provenance, not user enrichments.)
	//
	// ⚠ tags/tags_source ARE A PRE-EXISTING PERSISTENCE GAP, not a preservation choice. Being
	// absent from the INSERT column list means an in-memory Tags value -- which rule application
	// DOES produce, in-place, before this call (services/transaction_sync_service.go
	// processAddedTransactions) -- is silently dropped on a fresh insert. #1578 does not inherit
	// that gap into the settlement move: the move reads tags from the STORED pending row and
	// writes them to the STORED posted row, so it is unaffected by what this INSERT omits. Closing
	// the gap itself is a separate change to the rules-tagging path and is deliberately not made
	// here.
	//
	// Issue #1213: RETURNING id, (xmax::text::bigint = 0) AS inserted
	// xmax = 0 for a freshly inserted row; non-zero for a conflict-updated row.
	// This lets the caller gate analytics/match-count side-effects on genuine inserts.
	query := fmt.Sprintf(`
		INSERT INTO transactions
		(id, user_id, account_id, linked_item_id, user_account_id,
		 source, external_transaction_id, external_transaction_id_blind_idx, external_metadata,
		 amount, date, authorized_date, name, merchant_name, merchant_id,
		 pfc_primary, pfc_detailed, pfc_confidence_level, pfc_category_icon_url, payment_channel, pending, user_notes,
		 created_at, updated_at, effective_category_name,
		 category_override_kind, pfc_primary_override, pfc_detailed_override, custom_category_override_id,
		 category_source, reviewed, reviewed_at, pending_transaction_id_blind_idx)
		VALUES %s
		ON CONFLICT (source, external_transaction_id_blind_idx, user_id) DO UPDATE SET
			account_id = EXCLUDED.account_id,
			linked_item_id = EXCLUDED.linked_item_id,
			user_account_id = EXCLUDED.user_account_id,
			external_transaction_id_blind_idx = EXCLUDED.external_transaction_id_blind_idx,
			external_metadata = EXCLUDED.external_metadata,
			amount = EXCLUDED.amount,
			date = EXCLUDED.date,
			authorized_date = EXCLUDED.authorized_date,
			name = EXCLUDED.name,
			merchant_name = EXCLUDED.merchant_name,
			merchant_id = EXCLUDED.merchant_id,
			pfc_primary = EXCLUDED.pfc_primary,
			pfc_detailed = EXCLUDED.pfc_detailed,
			pfc_confidence_level = EXCLUDED.pfc_confidence_level,
			pfc_category_icon_url = EXCLUDED.pfc_category_icon_url,
			payment_channel = EXCLUDED.payment_channel,
			pending = EXCLUDED.pending,
			effective_category_name = CASE
			    WHEN transactions.category_override_kind IS NOT NULL THEN transactions.effective_category_name
			    ELSE EXCLUDED.pfc_primary
			END,
			-- Issue #1578, and this COALESCE is the whole point of the clause.
			--
			-- Leaving the column OUT of this SET list would have been the obvious "preserve it"
			-- choice, and it is wrong: a posted row already stored from an earlier delivery could
			-- then NEVER learn a pointer that arrives later, so the settlement move would have
			-- nothing to find and the user's work would stay stranded -- the exact bug, reached by
			-- a slower route.
			--
			-- Putting it in unguarded is the opposite hazard: Plaid re-delivers transactions, and
			-- an EXCLUDED value of NULL would erase a pointer we had already learned.
			--
			-- COALESCE resolves both: a non-NULL arrival wins, a NULL arrival is ignored. The
			-- column is therefore monotonic -- once known, never unknown again. Plaid does not
			-- repoint a settled transaction at a different pending id, so "a later non-NULL
			-- overwrites an earlier non-NULL" is a correction, not a loss.
			pending_transaction_id_blind_idx = COALESCE(
			    EXCLUDED.pending_transaction_id_blind_idx,
			    transactions.pending_transaction_id_blind_idx),
			updated_at = NOW()
		RETURNING id, (xmax::text::bigint = 0) AS inserted, external_transaction_id_blind_idx
	`, strings.Join(valueStrings, ", "))

	rows, err := r.db.Query(ctx, query, valueArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to batch create transactions: %w", err)
	}
	defer rows.Close()

	insertedIDs := make(map[string]bool)
	persistedByBlindIdx := make(map[string]string, len(transactions))
	for rows.Next() {
		var id string
		var inserted bool
		var blindIdx *string
		if err := rows.Scan(&id, &inserted, &blindIdx); err != nil {
			return nil, fmt.Errorf("failed to scan batch create result: %w", err)
		}
		if inserted {
			insertedIDs[id] = true
		}
		if blindIdx != nil {
			persistedByBlindIdx[*blindIdx] = id
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate batch create results: %w", err)
	}

	// #1586 (gap G7): TELL THE CALLER WHICH ROW IT ACTUALLY WROTE.
	//
	// TWO RETURNED SETS, TWO DIFFERENT MEANINGS, AND THEY MUST NOT BE CONFLATED:
	//
	//   insertedIDs        -- "this row is NEW". Deliberately excludes conflict rows, and stays
	//                         that way: EmitBatchSideEffects fires rule analytics and per-rule
	//                         match counts off it, and a re-delivered transaction must not re-fire
	//                         them (#1213). Nothing below widens it.
	//   transactions[i].ID -- "this is the row in the database". Now true for conflict rows too.
	//
	// WHY IT HAS TO BE WRITTEN BACK. Every id here is minted by the CALLER (or by the loop above)
	// before the statement runs, and ON CONFLICT DO UPDATE does not assign `id`: a conflict keeps
	// the id the row was FIRST stored with. The caller was left holding a UUID that names no row,
	// and there is no second query that could recover the real one without the blind index -- which
	// is derived in here and never leaves. So this is the only layer that can answer it.
	//
	// The concrete casualty was the settlement repair (#1586): pullForwardStrandedSettlements takes
	// the stored rows' ids, and on a RE-DELIVERY -- which is precisely the retry the repair's
	// fail-and-hold-the-cursor design is built on -- every one of them was a phantom, so the repair
	// matched zero rows and reported success forever. Pinned by
	// TestLateSettlement_RedeliveredTwinIsRepairableByItsPersistedID_Integration.
	//
	// Mutating the caller's slice is this method's existing contract, not a new one: it already
	// assigns missing ids, timestamps and the encrypted PII fields in place. Rule applications keyed
	// on the PRE-conflict id are unaffected, because they are gated on insertedIDs, which a conflict
	// row is never in.
	for i := range transactions {
		if blindIdxByInput[i] == nil {
			continue
		}
		if persisted, ok := persistedByBlindIdx[*blindIdxByInput[i]]; ok {
			transactions[i].ID = persisted
		}
	}

	return insertedIDs, nil
}

// Update updates an existing transaction.
//
// #1578: this is the OTHER write path for Plaid data, and it is easy to miss. Plaid's `modified`
// array does not go through CreateBatch -- services/transaction_sync_service.go
// processModifiedTransactions calls this method instead -- so a posted transaction whose
// pending_transaction_id only ever reaches us on a `modified` delivery would be permanently
// invisible to the settlement move if this method ignored the pointer. It does not; see the
// COALESCE on pending_transaction_id_blind_idx below, which has the same monotonic semantics as
// CreateBatch's ON CONFLICT clause and the same reason for them.
func (r *transactionRepository) Update(ctx context.Context, transaction *domain.Transaction) error {
	transaction.UpdatedAt = time.Now()

	// Encrypt external_metadata before writing
	encMeta, err := r.enc.EncryptNullableString(transaction.ExternalMetadata)
	if err != nil {
		return fmt.Errorf("failed to encrypt external_metadata: %w", err)
	}

	query := `
		UPDATE transactions
		SET amount = $1, date = $2, authorized_date = $3, name = $4,
		    merchant_name = $5, merchant_id = $6, pfc_primary = $7, pfc_detailed = $8, pfc_confidence_level = $9,
		    pfc_category_icon_url = $10, payment_channel = $11, pending = $12, external_metadata = $13,
		    user_notes = $14, updated_at = $15,
		    -- #1578: monotonic, exactly as in CreateBatch's ON CONFLICT clause. A non-NULL arrival
		    -- teaches a stored row its pending twin; a NULL arrival (every ordinary modification of
		    -- a transaction that is not a settlement) must not erase what we already know.
		    pending_transaction_id_blind_idx = COALESCE($18, pending_transaction_id_blind_idx),
		    -- tombstone-exempt(custom_categories, write:transactions): HISTORICAL name resolution (#1569 Task 9),
		    -- and the UPDATE target is unfiltered because liveness is the sync path's concern, not this
		    -- statement's: a tombstoned transaction Plaid still reports must keep a correct name
		    -- (#1569 T8 write-target audit). This recomputes the
		    -- denormalised name for a row that ALREADY carries this override. Filtering
		    -- tombstones here would set effective_category_name to NULL on every transaction
		    -- filed under a deleted category the next time Plaid refreshed it — the silent
		    -- history rewrite this task removes, arriving by a slower route.
		    effective_category_name = CASE
		        WHEN category_override_kind = 'custom' THEN
		            (SELECT name FROM custom_categories WHERE id = transactions.custom_category_override_id AND user_id = transactions.user_id)
		        WHEN category_override_kind = 'pfc' THEN COALESCE(NULLIF(pfc_primary_override, ''), $7)
		        ELSE $7
		    END
		WHERE id = $16 AND user_id = $17
	`

	ct, err := r.db.Exec(ctx, query,
		transaction.Amount,
		transaction.Date,
		transaction.AuthorizedDate,
		transaction.Name,
		transaction.MerchantName,
		transaction.MerchantID,
		transaction.PFCPrimary,
		transaction.PFCDetailed,
		transaction.PFCConfidenceLevel,
		transaction.PFCCategoryIconURL,
		transaction.PaymentChannel,
		transaction.Pending,
		encMeta,
		transaction.UserNotes,
		transaction.UpdatedAt,
		transaction.ID,
		transaction.UserID,
		r.pendingTwinBlindIndex(transaction),
	)

	if err != nil {
		return fmt.Errorf("failed to update transaction: %w", err)
	}

	if ct.RowsAffected() == 0 {
		return ErrTransactionNotFound
	}

	return nil
}

// settlementCategoryMoveCond and settlementTagsMoveCond are the two "may this move" predicates used
// by TombstoneByExternalID's settlement CTE. They are Go constants only because a SQL SET clause has
// nowhere to name a shared expression, and each is spliced into several CASEs: one copy of the text
// means the seven category columns cannot drift apart into an incoherent discriminated union, which
// is the failure mode of writing the condition out seven times.
//
// THE CATEGORY CONDITION IS NOT "the twin is blank". It is "the twin has no MANUAL category", and
// the difference decides whether the fix works at all. Rules are applied IN PLACE before the posted
// twin is ever inserted (services/transaction_sync_service.go, processAddedTransactions calls
// ApplyRulesToBatch before CreateBatch), so a posted twin ROUTINELY arrives already carrying
// category_source = 'rule' and an override kind. A blank-row-only gate would therefore refuse the
// move in the common case. `category_source` exists precisely to express the precedence that
// resolves this: manual outranks rule, and the pending row's manual category is the user's own work.
//
// A manual category already on the twin wins, because it is NEWER user work about the same charge.
//
// THE SEVEN CATEGORY COLUMNS MOVE AS ONE UNIT, and the database enforces it rather than merely
// preferring it: the `valid_category_override_discriminator` CHECK rejects a row whose
// category_override_kind is 'custom' without a custom_category_override_id. Splitting the tuple is
// therefore unrepresentable -- a mutation that moves the kind but not the id fails with SQLSTATE
// 23514 rather than writing an incoherent row. That is also why one shared condition constant is
// spliced into all seven CASEs instead of the condition being written out seven times.
const settlementCategoryMoveCond = `(s.category_source = 'manual' AND twin.category_source IS DISTINCT FROM 'manual')`

// THE REVIEWED CONDITION EXISTS BECAUSE `reviewed` IS NOT ALWAYS THE USER'S. A rule applying a
// category counts as an automated review: rule_application_service.go sets category_source = 'rule'
// and reviewed = true in the same block. Without a gate, `reviewed = twin.reviewed OR s.reviewed`
// would mark the twin reviewed off the back of a RULE's review even when the category gate above
// refused the move -- and would silently re-review a twin the user had deliberately un-reviewed.
//
// Rule-derived reviews are safe to drop: the same rules run on the posted twin before it is
// inserted, so a rule that reviewed the pending row reviews its twin too. What must survive is a
// review the USER performed, and that is what is left after excluding 'rule'.
//
// ⚠ The gate cannot be tightened further. SetReviewed(false) writes reviewed_at = NULL, so a
// deliberately un-reviewed row is indistinguishable in SQL from one never reviewed -- there is no
// column that records the un-review. Excluding 'rule' is the whole of what is knowable here.
const settlementReviewMoveCond = `(s.reviewed AND s.category_source IS DISTINCT FROM 'rule')`

// THE TAGS CONDITION IS "fill, do not merge". tags/tags_source are two JSONB arrays that must stay
// in lockstep -- every tag needs its provenance entry -- and a union would have to deduplicate
// across both, in SQL, with no test able to distinguish a correct merge from a plausible one. So
// tags move only onto a twin that has none, and a twin with its own tags keeps them. This is the
// refusal-to-overwrite rule the design note in processRemovedTransactions asked for; the pending
// row retains its own copy either way, because the move copies rather than cuts.
const settlementTagsMoveCond = `(COALESCE(jsonb_array_length(twin.tags), 0) = 0 AND COALESCE(jsonb_array_length(s.tags), 0) > 0)`

// TombstoneByExternalID marks a provider-removed transaction as deleted instead of destroying it,
// and -- when the removal is a SETTLEMENT -- moves the user's work onto the posted twin first.
//
// THE SETTLEMENT MOVE (#1578). When a pending charge posts, Plaid removes the pending transaction
// and delivers the posted one as a NEW row with a different transaction_id. Task 7 made that
// removal a tombstone rather than a DELETE, so nothing is destroyed -- but a settled pending renders
// NOWHERE (surfacing it would double-count the same spend against its twin), so the user's category
// override, notes, tags and `reviewed` state ended up stranded on an invisible row while the posted
// twin arrived blank. That happened every time anybody categorised a pending charge.
//
// The twin is identified by Plaid's OWN pointer -- transactions.pending_transaction_id_blind_idx,
// migration 000168, populated from Transaction.PendingTransactionId. It is NEVER identified by
// amount/date/merchant similarity: a heuristic can attach one transaction's category to a different
// transaction, which is an unrecoverable corruption of user data and strictly worse than the bug.
//
// WHY ONE STATEMENT. The move was asked for "inside its own transaction"; a CTE gives that and
// more, since both sub-statements are one statement, atomic at any isolation level, with no window
// in which a crash leaves the annotations on both rows. This note used to add "and there was no
// transaction to join: this method is one bare statement on r.db". T4 wrote that and T6 falsified
// it five commits later -- the statement now runs through runCapturingStatement, which calls
// db.Begin (transaction_annotation_capture.go:~249) so the FOR UPDATE lock and this statement share
// one transaction. The CTE is still right; "nothing here owns a transaction" is not.
//
// The CTE also gets the read-consistency right for free: `tombstoned` RETURNS the values it is
// about to overwrite for deleted_*, and leaves the annotation columns untouched, so `moved` reads
// exactly the pre-tombstone annotation state under the row lock the UPDATE already holds.
//
// WHAT MOVES. All twelve user-authored columns -- three times the four the CreateBatch comment used
// to name. user_notes; the seven-column category tuple (kind, pfc primary/detailed override, custom
// category id, category_source, override date, effective_category_name); tags and tags_source;
// reviewed and reviewed_at. See settlementCategoryMoveCond, settlementTagsMoveCond and
// settlementReviewMoveCond for the three refusal rules and why each is shaped the way it is.
//
// WHAT DOES NOT MOVE, and why each is deliberate:
//   - Nothing at all unless the removed row was PENDING (`AND s.pending`). A posted removal is a
//     bank reversal, a distinct event with no twin; donating its annotations to an unrelated row
//     carrying a stale pointer would be the corruption this design exists to avoid.
//   - Nothing to the removed row itself (`twin.id <> s.id`), and nothing across tenants
//     (`twin.user_id = s.user_id`). The pointer is a provider identifier; it carries no ownership.
//   - Nothing onto a row that is itself tombstoned (`twin.deleted_at IS NULL`).
//   - updated_at is not touched, for the same reason the tombstone does not touch it: it tracks
//     PROVIDER-data changes, and no provider data changed on the twin.
//   - The pending row keeps its own copy. The move COPIES; forensics on the retained row is the
//     whole point of #1569, and a cut would empty it.
//
// RESIDUAL, and it is not closed here. If Plaid delivers the removal in one sync and the posted
// twin only in a LATER one, the twin does not exist when this runs and the annotations stay on the
// tombstone. Closing it needs the mirror hook in the storage path (on inserting a posted row with a
// pointer, look for an already-tombstoned pending row and pull its annotations forward). Whether
// Plaid can actually split a settlement across syncs is NOT established -- it is a question about
// the provider's delivery semantics, not about this code -- and the common case is covered by
// ordering: processTransactions aggregates every page before storing, then runs added and modified
// (steps 6 and 7) before removed (step 8), so a settlement delivered together already has its twin
// stored when this executes.
//
// WHY THE REASON IS COMPUTED IN SQL. The caller has no reliable way to know whether the row was
// pending: reading it first and then updating leaves a window in which the same row posts (the
// pending -> posted transition is exactly what generates these removals in the first place), and
// the classification decides visibility for good. `CASE WHEN pending` inside the UPDATE reads the
// value the statement is about to overwrite, under the row lock the UPDATE already holds.
//
// WHY `deleted_at IS NULL` IS IN THE WHERE. Without it a re-delivered removal would restamp
// deleted_at and deleted_by on an already-tombstoned row, moving the recorded time away from when
// the removal actually happened, and would report success for work it did not do. With it, a
// repeat is zero rows, which surfaces as ErrTransactionNotFound — the same signal as "never stored"
// and, for the caller, the same correct response.
//
// updated_at is deliberately NOT touched: it tracks provider-data changes, and nothing about the
// transaction's own data changed here. deleted_at is the timestamp for this event.
func (r *transactionRepository) TombstoneByExternalID(
	ctx context.Context,
	source domain.TransactionSource,
	externalID string,
	userID string,
	deletedBy string,
) (domain.TransactionDeletedReason, error) {
	query := fmt.Sprintf(`
		WITH tombstoned AS (
			UPDATE transactions
			SET deleted_at     = NOW(),
			    deleted_by     = $4,
			    deleted_reason = CASE WHEN pending THEN '%[1]s' ELSE '%[2]s' END
			WHERE source = $1
			  AND external_transaction_id_blind_idx = $2
			  AND user_id = $3
			  AND deleted_at IS NULL
			RETURNING id, user_id, pending,
			          user_notes, category_override_kind, pfc_primary_override,
			          pfc_detailed_override, custom_category_override_id, category_source,
			          category_override_date, effective_category_name, tags, tags_source,
			          reviewed, reviewed_at, deleted_reason
		),
		moved AS (
			-- twin.deleted_at IS NULL is in this statement's own WHERE, below. It is NOT there
			-- because a guard asked -- it predates the rule. THAT IS NO LONGER THE STANDING
			-- POSITION: #1569 T8 taught the guard to audit an UPDATE's and a DELETE's TARGET as
			-- well as its read references, so removing this predicate now turns
			-- TestNoTransactionReadForgetsTheTombstoneFilter red rather than passing in silence.
			-- Do not remove it anyway -- donating a settled pending's annotations onto an
			-- already-tombstoned row would write user work onto a second row that renders nowhere.
			-- (Two earlier versions of this note are superseded. It once said the guard "never
			-- looks at an UPDATE's target", and once that deleting the exemption token below left
			-- the package green. Both were true when written; T8 falsified the first by adding the
			-- write rule and the second by teaching the guard to see an aliased reference in a
			-- comma join list, which is precisely what "transactions prior" is.)
			UPDATE transactions AS twin
			SET user_notes = COALESCE(twin.user_notes, s.user_notes),

			    category_override_kind      = CASE WHEN %[3]s THEN s.category_override_kind      ELSE twin.category_override_kind      END,
			    pfc_primary_override        = CASE WHEN %[3]s THEN s.pfc_primary_override        ELSE twin.pfc_primary_override        END,
			    pfc_detailed_override       = CASE WHEN %[3]s THEN s.pfc_detailed_override       ELSE twin.pfc_detailed_override       END,
			    custom_category_override_id = CASE WHEN %[3]s THEN s.custom_category_override_id ELSE twin.custom_category_override_id END,
			    category_source             = CASE WHEN %[3]s THEN s.category_source             ELSE twin.category_source             END,
			    category_override_date      = CASE WHEN %[3]s THEN s.category_override_date      ELSE twin.category_override_date      END,
			    effective_category_name     = CASE WHEN %[3]s THEN s.effective_category_name     ELSE twin.effective_category_name     END,

			    tags        = CASE WHEN %[4]s THEN s.tags        ELSE twin.tags        END,
			    tags_source = CASE WHEN %[4]s THEN s.tags_source ELSE twin.tags_source END,

			    reviewed    = twin.reviewed OR %[6]s,
			    reviewed_at = CASE
			        WHEN twin.reviewed THEN twin.reviewed_at
			        WHEN %[6]s         THEN s.reviewed_at
			        ELSE twin.reviewed_at
			    END
			-- tombstone-exempt(transactions prior): the prior alias is the PRE-IMAGE of the twin this statement is
			-- already updating (prior.id = twin.id), not an independent read. twin's own
			-- deleted_at IS NULL predicate is right below and governs which rows are touched;
			-- repeating it on prior would filter the capture differently from the write it
			-- captures (#1569 Task 6, review fix round 2 / D-4).
			FROM tombstoned s, transactions prior
			WHERE prior.id = twin.id
			  AND twin.pending_transaction_id_blind_idx = $2
			  AND twin.user_id = s.user_id
			  AND twin.id <> s.id
			  AND twin.deleted_at IS NULL
			  AND s.pending
			  AND (s.user_notes IS NOT NULL
			       OR s.category_source = '%[5]s'
			       OR %[6]s
			       OR COALESCE(jsonb_array_length(s.tags), 0) > 0)
			RETURNING `+annotationCaptureReturning("twin")+`
		)`+annotationCaptureInsert(domain.TransactionAnnotationChangeSettlement, "moved")+`
		SELECT deleted_reason FROM tombstoned
	`,
		domain.TxnDeletedProviderRemovedPending,
		domain.TxnDeletedProviderRemovedPosted,
		settlementCategoryMoveCond,
		settlementTagsMoveCond,
		domain.CategorySourceManual,
		settlementReviewMoveCond,
	)

	// Lock-first (#1569 Task 6, review fix round 2 / D-4). This statement now carries the annotation
	// capture, and a capture without the lock records a prior value from a snapshot older than the
	// write it describes -- see transaction_annotation_capture.go for the two measured failures.
	//
	// BOTH rows are locked in one ORDER BY id statement: the pending row this call tombstones and
	// the posted twin it may write onto. Locking only the twin would leave the two rows acquired in
	// plan order by this statement and in id order by a concurrent bulk capture, which is the
	// inversion ORDER BY id exists to remove.
	//
	// The lock predicate is the union of the two the statement itself uses. `user_id = $3` is what
	// lets the twin leg be expressed without the tombstoned CTE: the CTE filters user_id = $3, and
	// the moved CTE requires twin.user_id = s.user_id, so twin.user_id = $3 is equivalent.
	lockWhere := `(
		    (source = $1 AND external_transaction_id_blind_idx = $2 AND user_id = $3)
		 OR (pending_transaction_id_blind_idx = $2 AND user_id = $3)
		) AND deleted_at IS NULL`

	blindIdx := r.enc.BlindIndex(externalID)

	var reason string
	err := runCapturingStatement(ctx, r.db,
		lockWhere, []any{string(source), blindIdx, userID},
		query, []any{string(source), blindIdx, userID, deletedBy},
		func(row pgx.Row) error { return row.Scan(&reason) })
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrTransactionNotFound
	}
	if err != nil {
		return "", fmt.Errorf("failed to tombstone transaction: %w", err)
	}
	return domain.TransactionDeletedReason(reason), nil
}

// PullForwardSettledPendingAnnotations is the MIRROR of TombstoneByExternalID's settlement move,
// for the case that move cannot reach (#1586).
//
// THE RESIDUAL IT CLOSES, established by measurement rather than by the ticket's framing. The move
// folded into TombstoneByExternalID runs at the TOMBSTONE point, so it only finds a twin that is
// already stored. Within one sync that is guaranteed by ordering (added and modified are steps 6
// and 7, removed is step 8). Across syncs it is not: a probe against a fresh database settled the
// pending row with no twin stored and then delivered the twin, and observed the twin arriving with
// `notes=nil kind=nil source=plaid reviewed=false tags=[]` while the tombstoned pending row kept
// `category_source=manual` and every annotation on it. Retained, and rendering nowhere.
//
// SO THIS RUNS FROM THE STORAGE SIDE, on the ids a sync just stored, and pulls forward from a
// pending row that is ALREADY tombstoned. `s.deleted_at IS NOT NULL` is what makes the two hooks
// disjoint rather than racing: while the pending row is live, the twin renders alongside it and the
// tombstone-side move is the one that will run.
//
// THE PAIRING IS PLAID'S OWN POINTER, NEVER A HEURISTIC. twin.pending_transaction_id_blind_idx is
// matched against s.external_transaction_id_blind_idx, both produced by
// crypto.FieldEncryptor.BlindIndex. Matching on amount/date/merchant similarity can attach one
// transaction's category to a DIFFERENT transaction, which is an unrecoverable corruption of the
// user's own data and strictly worse than the bug. See migration 000168.
//
// THE REFUSAL RULES ARE THE SAME THREE CONSTANTS the tombstone-side move uses --
// settlementCategoryMoveCond (manual on the twin wins), settlementTagsMoveCond (fill, do not merge)
// and settlementReviewMoveCond (a rule's review is not the user's). They are shared rather than
// restated so the two hooks cannot drift into disagreeing about the same event; the SET clause
// itself is deliberately WRITTEN OUT rather than factored into a shared constant, because
// TestAnnotationCaptureIsUsedByEveryAnnotationWritingStatement matches on the rendered text of each
// statement and a factored-out SET clause would make BOTH statements invisible to it -- a guard
// that silently stops guarding. TestSettlementMoveSetClausesAgree pins the duplication instead.
//
// IT CARRIES THE ANNOTATION CAPTURE, under change_source 'settlement'. The category leg is
// manual-wins, not fill-only: a twin already carrying a RULE-assigned override -- the common case,
// since rules run in place before CreateBatch -- has all seven category columns replaced. That is
// an overwrite of a stored annotation and #1579 requires it to be recorded. Lock-first, through
// runCapturingStatement, for the two reasons in transaction_annotation_capture.go.
//
// SCOPED BY ID, never a global scan. The caller passes the transactions it just stored, so this is
// one indexed statement per sync batch rather than a sweep over the table.
//
// Returns the number of twins actually written.
func (r *transactionRepository) PullForwardSettledPendingAnnotations(
	ctx context.Context,
	userID string,
	transactionIDs []string,
) (int64, error) {
	if len(transactionIDs) == 0 {
		return 0, nil
	}

	query := fmt.Sprintf(`
		WITH moved AS (
			UPDATE transactions AS twin
			SET user_notes = COALESCE(twin.user_notes, s.user_notes),

			    category_override_kind      = CASE WHEN %[3]s THEN s.category_override_kind      ELSE twin.category_override_kind      END,
			    pfc_primary_override        = CASE WHEN %[3]s THEN s.pfc_primary_override        ELSE twin.pfc_primary_override        END,
			    pfc_detailed_override       = CASE WHEN %[3]s THEN s.pfc_detailed_override       ELSE twin.pfc_detailed_override       END,
			    custom_category_override_id = CASE WHEN %[3]s THEN s.custom_category_override_id ELSE twin.custom_category_override_id END,
			    category_source             = CASE WHEN %[3]s THEN s.category_source             ELSE twin.category_source             END,
			    category_override_date      = CASE WHEN %[3]s THEN s.category_override_date      ELSE twin.category_override_date      END,
			    effective_category_name     = CASE WHEN %[3]s THEN s.effective_category_name     ELSE twin.effective_category_name     END,

			    tags        = CASE WHEN %[4]s THEN s.tags        ELSE twin.tags        END,
			    tags_source = CASE WHEN %[4]s THEN s.tags_source ELSE twin.tags_source END,

			    reviewed    = twin.reviewed OR %[6]s,
			    reviewed_at = CASE
			        WHEN twin.reviewed THEN twin.reviewed_at
			        WHEN %[6]s         THEN s.reviewed_at
			        ELSE twin.reviewed_at
			    END
			-- tombstone-exempt(transactions s): the DONOR is a row that MUST be tombstoned -- this
			-- statement exists only for annotations stranded on an already-tombstoned pending row,
			-- and s.deleted_at IS NOT NULL is asserted explicitly below. Filtering it to live
			-- rows would make the statement match nothing at all (#1586).
			-- tombstone-exempt(transactions prior): the prior alias is the PRE-IMAGE of the twin this
			-- statement is already updating (prior.id = twin.id), not an independent read. twin's
			-- own deleted_at IS NULL predicate is below and governs which rows are touched;
			-- repeating it on prior would filter the capture differently from the write it
			-- captures (same reasoning as TombstoneByExternalID's moved CTE).
			FROM transactions s, transactions prior
			WHERE prior.id = twin.id
			  AND twin.id = ANY($2::uuid[])
			  AND twin.user_id = $1
			  AND twin.deleted_at IS NULL
			  AND twin.pending_transaction_id_blind_idx IS NOT NULL
			  AND s.external_transaction_id_blind_idx = twin.pending_transaction_id_blind_idx
			  AND s.source = twin.source
			  AND s.user_id = twin.user_id
			  AND s.id <> twin.id
			  AND s.deleted_at IS NOT NULL
			  -- WHY deleted_reason AND NOT s.pending. The tombstone-side move gates on s.pending
			  -- because it reads the row's pre-image at the instant of removal, when pending IS the
			  -- classification. Here the removal already happened, possibly syncs ago, and pending
			  -- is a live column any later Update rewrites from the provider payload. deleted_reason
			  -- records what the removal WAS and nothing rewrites it, so it is the authority for
			  -- "this was a settlement, not a reversal". A reversal must never donate: its
			  -- annotations belong to a charge the bank withdrew, and a row carrying a stale pointer
			  -- is not the same charge (#1586).
			  AND s.deleted_reason = '%[1]s'
			  AND (s.user_notes IS NOT NULL
			       OR s.category_source = '%[5]s'
			       OR %[6]s
			       OR COALESCE(jsonb_array_length(s.tags), 0) > 0)
			RETURNING `+annotationCaptureReturning("twin")+`
		)`+annotationCaptureInsert(domain.TransactionAnnotationChangeSettlement, "moved")+`
		SELECT count(*) FROM moved
	`,
		// THE ARGUMENT ORDER IS THE SAME AS TombstoneByExternalID'S, DELIBERATELY. The two SET
		// clauses are compared verbatim by TestSettlementMoveSetClausesAgree, and a differing %[n]s
		// index would make two identical clauses read as divergent -- a guard failing on
		// bookkeeping instead of on behaviour is how a guard gets weakened into uselessness.
		// %[2]s (the posted-removal reason) is unused here: this statement only ever reads from a
		// PENDING removal. Explicit argument indexes suppress fmt's EXTRA reporting, so the unused
		// argument is inert rather than corrupting.
		domain.TxnDeletedProviderRemovedPending,
		domain.TxnDeletedProviderRemovedPosted,
		settlementCategoryMoveCond,
		settlementTagsMoveCond,
		domain.CategorySourceManual,
		settlementReviewMoveCond,
	)

	// Lock-first, exactly as the tombstone-side move does. BOTH sides are locked in one ORDER BY id
	// statement: the twins this may write onto, and the tombstoned pending rows they read from.
	// Locking only the twins would leave the donors acquired in plan order here and in id order by
	// a concurrent bulk capture, which is the inversion ORDER BY id exists to remove.
	//
	// The donor leg cannot carry `deleted_at IS NULL` -- the donor is tombstoned by definition -- so
	// the lock predicate is written without it and the write's own predicates do the filtering.
	lockWhere := `(
		    (id = ANY($2::uuid[]) AND user_id = $1)
		 OR (user_id = $1 AND external_transaction_id_blind_idx IN (
		        -- tombstone-exempt(transactions): resolving WHICH rows to lock, not reading a row's
		        -- data. It must see tombstoned twins and tombstoned donors alike, because the
		        -- statement's own predicates decide which of them it may touch (#1586).
		        SELECT pending_transaction_id_blind_idx FROM transactions
		        WHERE id = ANY($2::uuid[]) AND user_id = $1
		          AND pending_transaction_id_blind_idx IS NOT NULL))
		)`

	var moved int64
	err := runCapturingStatement(ctx, r.db,
		lockWhere, []any{userID, transactionIDs},
		query, []any{userID, transactionIDs},
		func(row pgx.Row) error { return row.Scan(&moved) })
	if err != nil {
		return 0, fmt.Errorf("failed to pull forward settled pending annotations: %w", err)
	}
	return moved, nil
}

// GetByUserID retrieves transactions for a specific user with pagination
func (r *transactionRepository) GetByUserID(ctx context.Context, userID string, limit, offset int) ([]domain.Transaction, error) {
	query := fmt.Sprintf(`
		SELECT %s
		FROM transactions
		WHERE user_id = $1
		  AND deleted_at IS NULL
		ORDER BY date DESC, created_at DESC
		LIMIT $2 OFFSET $3
	`, transactionSelectExpr)

	rows, err := r.db.Query(ctx, query, userID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query transactions: %w", err)
	}
	defer rows.Close()

	return r.scanTransactions(rows)
}

// GetByAccountID retrieves all transactions for a specific account.
// Chain-aware: expands to include transactions from all accounts in the re-link chain
// by matching on user_account_id (Issue #1109).
func (r *transactionRepository) GetByAccountID(ctx context.Context, accountID string) ([]domain.Transaction, error) {
	query := fmt.Sprintf(`
		SELECT %s
		FROM transactions
		WHERE deleted_at IS NULL
		  AND (
			user_account_id = (SELECT user_account_id FROM accounts WHERE id = $1 AND user_account_id IS NOT NULL)
			OR (account_id = $1 AND user_account_id IS NULL)
		)
		ORDER BY date DESC, created_at DESC
	`, transactionSelectExpr)

	rows, err := r.db.Query(ctx, query, accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to query transactions: %w", err)
	}
	defer rows.Close()

	return r.scanTransactions(rows)
}

// GetBySource retrieves all transactions from a specific source for a user
func (r *transactionRepository) GetBySource(ctx context.Context, source domain.TransactionSource, userID string) ([]domain.Transaction, error) {
	query := fmt.Sprintf(`
		SELECT %s
		FROM transactions
		WHERE source = $1 AND user_id = $2
		  AND deleted_at IS NULL
		ORDER BY date DESC, created_at DESC
	`, transactionSelectExpr)

	rows, err := r.db.Query(ctx, query, string(source), userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query transactions: %w", err)
	}
	defer rows.Close()

	return r.scanTransactions(rows)
}

// GetByUserIDWithFilters retrieves transactions for a user with optional filters and pagination
func (r *transactionRepository) GetByUserIDWithFilters(ctx context.Context, userID string, filters domain.TransactionFilters, limit, offset int) ([]domain.Transaction, error) {
	// Build dynamic WHERE clause based on filters
	whereClause, args := buildWhereClause(userID, filters)

	// Determine if merchant JOINs are needed (merchant sort or search active).
	sortKey := filters.SortBy
	if sortKey == "" {
		sortKey = "date"
	}
	sortDesc, ok := sortDescriptors[sortKey]
	if !ok {
		sortDesc = sortDescriptors["date"]
	}
	needsJoins := sortDesc.needsMerchantJoin || filters.Search != nil

	joinClause := ""
	if needsJoins {
		joinClause = merchantSortJoins
	}

	// Add limit and offset to args
	args = append(args, limit, offset)

	// Build complete query with dynamic WHERE clause
	query := fmt.Sprintf(`
		-- tombstone-exempt(transactions t): buildWhereClause emits transactionTombstoneCondition("t", ...) into
		-- the interpolated WHERE below, unconditionally and before any optional filter (#1569).
		SELECT %s
		FROM transactions t
		%s
		%s
		%s
		LIMIT $%d OFFSET $%d
	`, transactionSelectExprT, joinClause, whereClause, buildOrderBy(filters), len(args)-1, len(args))

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query transactions: %w", err)
	}
	defer rows.Close()

	txns, err := r.scanTransactions(rows)
	if err != nil {
		return nil, err
	}
	if err := r.enrichEffectiveMerchantNames(ctx, userID, txns); err != nil {
		return nil, err
	}
	return txns, nil
}

// CountByUserID returns the total count of transactions for a user with optional filters
func (r *transactionRepository) CountByUserID(ctx context.Context, userID string, filters domain.TransactionFilters) (int, error) {
	// Build dynamic WHERE clause (same logic as GetByUserIDWithFilters)
	whereClause, args := buildWhereClause(userID, filters)

	// Add merchant JOINs when search is active (effectiveMerchantNameExpr in WHERE requires the aliases).
	joinClause := ""
	if filters.Search != nil {
		joinClause = merchantSortJoins
	}

	// Build COUNT query
	query := fmt.Sprintf(`
		-- tombstone-exempt(transactions t): buildWhereClause emits transactionTombstoneCondition("t", ...) into
		-- the interpolated WHERE below, unconditionally and before any optional filter (#1569).
		SELECT COUNT(*)
		FROM transactions t
		%s
		%s
	`, joinClause, whereClause)

	var count int
	err := r.db.QueryRow(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count transactions: %w", err)
	}

	return count, nil
}

// GetByExternalID retrieves a transaction by its provider-specific external ID
// Used during transaction sync to lookup existing transactions for updates
func (r *transactionRepository) GetByExternalID(ctx context.Context, source domain.TransactionSource, externalID string, userID string) (*domain.Transaction, error) {
	query := fmt.Sprintf(`
		SELECT %s
		FROM transactions
		WHERE source = $1 AND external_transaction_id_blind_idx = $2 AND user_id = $3
		  AND deleted_at IS NULL
	`, transactionSelectExpr)

	var tx domain.Transaction
	dest, state := r.scanTransactionDestSliceWithState(&tx)

	err := r.db.QueryRow(ctx, query, string(source), r.enc.BlindIndex(externalID), userID).Scan(dest...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTransactionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get transaction by external ID: %w", err)
	}
	if err := r.postScanTransactionState(&tx, state); err != nil {
		return nil, err
	}

	return &tx, nil
}

// buildWhereClause constructs a dynamic WHERE clause based on filters
// Returns the WHERE clause string and the corresponding query arguments

type sortDescriptor struct {
	expr              string
	needsMerchantJoin bool
	nullable          bool
}

var sortDescriptors = map[string]sortDescriptor{
	"date":     {expr: "COALESCE(t.authorized_date, t.date)", needsMerchantJoin: false, nullable: false},
	"merchant": {expr: effectiveMerchantNameExpr, needsMerchantJoin: true, nullable: false},
	"amount":   {expr: "t.amount", needsMerchantJoin: false, nullable: false},
	"category": {expr: "COALESCE(t.effective_category_name, t.effective_pfc_primary, t.pfc_primary)", needsMerchantJoin: false, nullable: true},
}

// buildOrderBy constructs a safe ORDER BY clause from TransactionFilters.SortBy and SortDir.
// Defaults to date DESC with an id ASC tiebreaker for stable pagination.
func buildOrderBy(filters domain.TransactionFilters) string {
	dir := "DESC"
	if strings.EqualFold(filters.SortDir, "asc") {
		dir = "ASC"
	}
	desc, ok := sortDescriptors[filters.SortBy]
	if !ok {
		desc = sortDescriptors["date"]
	}
	return fmt.Sprintf("ORDER BY %s %s, t.id ASC", desc.expr, dir)
}

// transactionTombstoneCondition renders the #1569 tombstone predicate for a transactions reference
// aliased `alias` (pass "" for an unaliased `FROM transactions`).
//
// DEFAULT: `deleted_at IS NULL` — every tombstone hidden, whatever its reason.
//
// includeReversed widens it by exactly ONE reason, provider_removed_posted: a transaction the bank
// withdrew after it had posted. It never admits provider_removed_pending, and that asymmetry is the
// entire design. A pending row the provider removed has a posted twin already in the table;
// admitting it would double-count real money. A posted row the provider removed has no twin, and
// hiding it silently changes a month the user already reconciled.
//
// The reason value is interpolated, not parameterised, because buildWhereClause's callers number
// their placeholders positionally and an extra $N here would shift every subsequent one. It is a
// compile-time constant from domain, never user input — there is no injection surface. (The same
// pattern is already used for the status literals in retainedAccountVisibilityLeg.)
func transactionTombstoneCondition(alias string, includeReversed bool) string {
	prefix := ""
	if alias != "" {
		prefix = alias + "."
	}
	if includeReversed {
		return fmt.Sprintf("(%sdeleted_at IS NULL OR %sdeleted_reason = '%s')",
			prefix, prefix, domain.TxnDeletedProviderRemovedPosted)
	}
	return prefix + "deleted_at IS NULL"
}

func buildWhereClause(userID string, filters domain.TransactionFilters) (string, []interface{}) {
	var conditions []string
	var args []interface{}
	argIndex := 1

	// ::uuid cast allows paired mno.user_id = $N::text joins without type-inference conflict
	conditions = append(conditions, fmt.Sprintf("t.user_id = $%d::uuid", argIndex))
	args = append(args, userID)
	argIndex++

	// #1569: unconditional tombstone gate. It is added HERE, immediately after tenancy and before
	// any optional filter, so that no future `if filters.X != nil` branch can be written in a way
	// that skips it. Every caller of buildWhereClause therefore inherits it:
	// GetByUserIDWithFilters, CountByUserID and GetByUserIDKeyset.
	conditions = append(conditions, transactionTombstoneCondition("t", filters.IncludeReversed))

	if filters.AccountID != nil {
		subq := fmt.Sprintf(
			"(t.user_account_id = (SELECT user_account_id FROM accounts WHERE id = $%d AND user_account_id IS NOT NULL)"+
				" OR (t.account_id = $%d AND t.user_account_id IS NULL))",
			argIndex, argIndex+1,
		)
		conditions = append(conditions, subq)
		args = append(args, *filters.AccountID, *filters.AccountID)
		argIndex += 2
	}

	if len(filters.AccountIDs) > 0 {
		placeholders := make([]string, len(filters.AccountIDs))
		for i, id := range filters.AccountIDs {
			placeholders[i] = fmt.Sprintf("$%d", argIndex)
			args = append(args, id)
			argIndex++
		}
		inList := strings.Join(placeholders, ", ")
		// Fallback placeholders for legacy accounts without user_account_id
		fallbackPlaceholders := make([]string, len(filters.AccountIDs))
		for i, id := range filters.AccountIDs {
			fallbackPlaceholders[i] = fmt.Sprintf("$%d", argIndex)
			args = append(args, id)
			argIndex++
		}
		fallbackInList := strings.Join(fallbackPlaceholders, ", ")
		subq := fmt.Sprintf(
			"(t.user_account_id IN (SELECT user_account_id FROM accounts WHERE id IN (%s) AND user_account_id IS NOT NULL)"+
				" OR (t.account_id IN (%s) AND t.user_account_id IS NULL))",
			inList, fallbackInList,
		)
		conditions = append(conditions, subq)
	}

	if filters.StartDate != nil {
		conditions = append(conditions, fmt.Sprintf("t.date >= $%d", argIndex))
		args = append(args, *filters.StartDate)
		argIndex++
	}

	if filters.EndDate != nil {
		conditions = append(conditions, fmt.Sprintf("t.date <= $%d", argIndex))
		args = append(args, *filters.EndDate)
		argIndex++
	}

	// #1626: the PFC and custom predicates are ONE OR-joined group. They used to be separate
	// top-level conditions, and because a row is either pfc-kind or custom-kind (never both), the
	// conjunction of the two matched nothing.
	if catClause, catArgs, nextIdx := categoryFilterGroup(filters, argIndex, args); catClause != "" {
		conditions = append(conditions, strings.TrimPrefix(catClause, " AND "))
		args = catArgs
		argIndex = nextIdx
	}

	if filters.Search != nil {
		searchCondition := fmt.Sprintf(
			"(t.name ILIKE $%d OR t.merchant_name ILIKE $%d OR "+effectiveMerchantNameExpr+" ILIKE $%d)",
			argIndex, argIndex, argIndex)
		conditions = append(conditions, searchCondition)
		searchPattern := "%" + *filters.Search + "%"
		args = append(args, searchPattern)
		argIndex++
	}

	if filters.Pending != nil {
		conditions = append(conditions, fmt.Sprintf("t.pending = $%d", argIndex))
		args = append(args, *filters.Pending)
		argIndex++
	}

	if filters.Reviewed != nil {
		conditions = append(conditions, fmt.Sprintf("t.reviewed = $%d", argIndex))
		args = append(args, *filters.Reviewed)
		argIndex++
	}

	if len(filters.CategorySources) > 0 {
		placeholders := make([]string, len(filters.CategorySources))
		for i, s := range filters.CategorySources {
			placeholders[i] = fmt.Sprintf("$%d", argIndex)
			args = append(args, s)
			argIndex++
		}
		conditions = append(conditions, "t.category_source IN ("+strings.Join(placeholders, ", ")+")")
	}

	if filters.MinAmount != nil {
		conditions = append(conditions, fmt.Sprintf("ABS(t.amount) >= $%d", argIndex))
		args = append(args, *filters.MinAmount)
		argIndex++
	}

	if filters.MaxAmount != nil {
		conditions = append(conditions, fmt.Sprintf("ABS(t.amount) <= $%d", argIndex))
		args = append(args, *filters.MaxAmount)
		argIndex++
	}

	if filters.AmountType != nil {
		switch *filters.AmountType {
		case "income":
			conditions = append(conditions, "t.amount < 0")
		case "expense":
			conditions = append(conditions, "t.amount > 0")
			// "all" means no filter - handled in handler by not setting AmountType
		}
	}

	if filters.UserAccountID != nil {
		conditions = append(conditions, fmt.Sprintf("t.user_account_id = $%d", argIndex))
		args = append(args, *filters.UserAccountID)
		argIndex++
	}

	if filters.MerchantID != nil {
		conditions = append(conditions, fmt.Sprintf("t.merchant_id = $%d", argIndex))
		args = append(args, *filters.MerchantID)
		argIndex++ //nolint:ineffassign // kept so a filter added below inherits the correct arg index
	}

	// Combine all conditions with AND
	whereClause := "WHERE " + strings.Join(conditions, " AND ")

	return whereClause, args
}

// ResolveEffectiveMerchantNames returns a map of merchant ID → effective display name
// for the given merchant IDs and user, in one DB round-trip.
//
// Resolution follows the per-user merge + override chain (G6b fix):
//
//	COALESCE(
//	  source_override.display_name,    -- user renamed the source merchant directly
//	  target_override.display_name,    -- user renamed the canonical (merged-into) merchant
//	  canonical_merchant.normalized_name,
//	  canonical_merchant.name
//	)
//
// When mno.merged_into_merchant_id is NULL, m2.id = m.id, so mno_t matches the same row as
// mno and the COALESCE resolves to mno.display_name (direct rename) or normalized/raw name —
// correct in both cases.
func (r *transactionRepository) ResolveEffectiveMerchantNames(ctx context.Context, userID string, merchantIDs []string) (map[string]string, error) {
	if len(merchantIDs) == 0 {
		return map[string]string{}, nil
	}

	const q = `
		SELECT m.id::text,
		       COALESCE(mno.display_name, mno_t.display_name, NULLIF(m2.normalized_name, ''), m2.name) AS effective_name
		FROM   merchants m
		LEFT   JOIN merchant_name_overrides mno
		            ON mno.merchant_id = m.id AND mno.user_id = $1
		LEFT   JOIN merchants m2
		            ON m2.id = COALESCE(mno.merged_into_merchant_id, m.id)
		LEFT   JOIN merchant_name_overrides mno_t
		            ON mno_t.merchant_id = m2.id AND mno_t.user_id = $1
		WHERE  m.id = ANY($2::uuid[])`

	rows, err := r.db.Query(ctx, q, userID, merchantIDs)
	if err != nil {
		return nil, fmt.Errorf("ResolveEffectiveMerchantNames: %w", err)
	}
	defer rows.Close()

	nameByID := make(map[string]string, len(merchantIDs))
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, fmt.Errorf("ResolveEffectiveMerchantNames scan: %w", err)
		}
		nameByID[id] = name
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return nameByID, nil
}

// enrichEffectiveMerchantNames resolves per-user merchant display names for a slice of
// transactions in a single extra DB round trip. Delegates to ResolveEffectiveMerchantNames
// so that the unified feed path (enrichEffectiveMerchantNamesForItems) uses the same SQL.
// Called after scanTransactions for user-facing list endpoints only.
func (r *transactionRepository) enrichEffectiveMerchantNames(ctx context.Context, userID string, txns []domain.Transaction) error {
	merchantIDSet := make(map[string]struct{}, len(txns))
	for _, tx := range txns {
		if tx.MerchantID != nil && *tx.MerchantID != "" {
			merchantIDSet[*tx.MerchantID] = struct{}{}
		}
	}
	if len(merchantIDSet) == 0 {
		return nil
	}

	merchantIDs := make([]string, 0, len(merchantIDSet))
	for id := range merchantIDSet {
		merchantIDs = append(merchantIDs, id)
	}

	nameByID, err := r.ResolveEffectiveMerchantNames(ctx, userID, merchantIDs)
	if err != nil {
		return fmt.Errorf("enrichEffectiveMerchantNames: %w", err)
	}

	for i := range txns {
		if txns[i].MerchantID != nil {
			if name, ok := nameByID[*txns[i].MerchantID]; ok {
				nameCopy := name
				txns[i].EffectiveMerchantName = &nameCopy
			}
		}
	}
	return nil
}

// scanTransactions scans SQL rows into Transaction structs.
//
// It delegates to scanTransactionDestSliceWithState/postScanTransactionState rather than repeating
// the destination list. Until #1569 it kept a SECOND, hand-maintained copy of that list, and the
// two silently disagreed the moment a column was appended to transactionSelectColumns: the keyset
// path scanned 38 values while this path scanned 35, and only the mock-backed test noticed.
// TestTransactionSelectColumns pins the column order against the shared helper, which is now
// genuinely the only place the order is written down.
func (r *transactionRepository) scanTransactions(rows pgx.Rows) ([]domain.Transaction, error) {
	var transactions []domain.Transaction

	for rows.Next() {
		var tx domain.Transaction
		dest, state := r.scanTransactionDestSliceWithState(&tx)

		if err := rows.Scan(dest...); err != nil {
			return nil, fmt.Errorf("failed to scan transaction: %w", err)
		}
		if err := r.postScanTransactionState(&tx, state); err != nil {
			return nil, err
		}

		transactions = append(transactions, tx)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating transaction rows: %w", err)
	}

	return transactions, nil
}

// transactionScanState holds the intermediate scan state for a single transaction row.
// It is used by scanTransactionDestSliceWithState + postScanTransactionState to decouple
// scan-destination construction from post-scan processing (needed when an extra column is
// appended to the scan destination, e.g. effective_merchant_sort_name in keyset queries).
type transactionScanState struct {
	source string
	// accountID is scanned NULL-TOLERANTLY because transactions.account_id is NULLABLE and nothing
	// in these reads hides such a row: migration 000035's ON DELETE SET NULL left an orphaned row
	// behind every pre-#1569-Task-2 hard delete, buildWhereClause carries no account predicate at
	// all, and the unified feed says outright that these are "still the user's own rows", admitting
	// them with `OR t.account_id IS NULL`.
	//
	// Scanned straight into domain.Transaction.AccountID (a plain string) pgx failed the ROW
	// outright — "cannot scan NULL into *string" — which is a 500 for the WHOLE list rather than one
	// missing row, on every read sharing this dest slice (GetByID, GetByUserIDWithFilters,
	// GetByUserIDKeyset). The unified feed's own scan already used a *string for this column, which
	// is why only the banking path failed. Found by
	// TestSankeyClickThrough_NodeAmountAndListTotalDescribeTheSameSet_Integration, whose fixture
	// seeds an orphan because the category hierarchy counts one toward a node's amount.
	//
	// ⚠ sql.NullString, NOT *string, and that is not a style choice: pgxmock rejects a **string
	// destination for a non-NULL string cell ("destination kind 'ptr' not supported for value kind
	// 'string'"), so a pointer here reddens every pgxmock test that supplies an account_id. metaStr
	// below gets away with *string only because those tests pass nil for it.
	accountID      sql.NullString
	metaStr        *string
	tagsJSON       []byte
	tagsSourceJSON []byte
	categorySource sql.NullString
	deletedReason  *string
}

// scanTransactionDestSliceWithState returns both the scan destination slice and the intermediate
// scan state for a single transaction row. Call postScanTransactionState after Scan.
func (r *transactionRepository) scanTransactionDestSliceWithState(tx *domain.Transaction) ([]any, *transactionScanState) {
	s := &transactionScanState{}
	dest := []any{
		&tx.ID,
		&tx.UserID,
		&s.accountID,
		&tx.LinkedItemID,
		&s.source,
		&tx.ExternalTransactionID,
		&s.metaStr,
		&tx.Amount,
		&tx.Date,
		&tx.AuthorizedDate,
		&tx.Name,
		&tx.MerchantName,
		&tx.MerchantID,
		&tx.PFCPrimary,
		&tx.PFCDetailed,
		&tx.PFCConfidenceLevel,
		&tx.PFCCategoryIconURL,
		&tx.PaymentChannel,
		&tx.Pending,
		&tx.UserNotes,
		&tx.CreatedAt,
		&tx.UpdatedAt,
		&s.tagsJSON,
		&s.tagsSourceJSON,
		&tx.CategoryOverrideKind,
		&tx.PFCPrimaryOverride,
		&tx.PFCDetailedOverride,
		&tx.CustomCategoryOverrideID,
		&s.categorySource,
		&tx.CategoryOverrideDate,
		&tx.EffectivePFCPrimary,
		&tx.EffectivePFCDetailed,
		&tx.EffectiveCategoryName,
		&tx.Reviewed,
		&tx.ReviewedAt,
		&tx.DeletedAt,
		&s.deletedReason,
		&tx.DeletedBy,
	}
	return dest, s
}

// postScanTransactionState applies post-scan conversions (source, metadata, decryption, JSON, category source).
func (r *transactionRepository) postScanTransactionState(tx *domain.Transaction, s *transactionScanState) error {
	tx.Source = domain.TransactionSource(s.source)
	tx.ExternalMetadata = s.metaStr

	// An orphaned row (account_id NULL — see transactionScanState.accountID) presents the empty
	// string, which is what ActivityItem.AccountID already does for the same row on the unified
	// path. It is deliberately NOT an error and NOT a dropped row: the row is retained history the
	// user still owns.
	tx.AccountID = s.accountID.String

	if err := r.decryptTransactionPIIFields(tx); err != nil {
		return fmt.Errorf("failed to decrypt transaction: %w", err)
	}

	if s.tagsJSON != nil {
		if err := json.Unmarshal(s.tagsJSON, &tx.Tags); err != nil {
			return fmt.Errorf("failed to unmarshal tags: %w", err)
		}
	}

	if s.tagsSourceJSON != nil {
		if err := json.Unmarshal(s.tagsSourceJSON, &tx.TagsSource); err != nil {
			return fmt.Errorf("failed to unmarshal tags_source: %w", err)
		}
	}

	if s.categorySource.Valid {
		tx.CategorySourceField = domain.CategorySource(s.categorySource.String)
	}

	// #1569: deleted_reason is scanned as a plain string and narrowed here rather than scanned
	// straight into *domain.TransactionDeletedReason, matching how `source` and `category_source`
	// are handled a few lines up.
	if s.deletedReason != nil {
		reason := domain.TransactionDeletedReason(*s.deletedReason)
		tx.DeletedReason = &reason
	} else {
		tx.DeletedReason = nil
	}

	return nil
}

// GetByID retrieves a transaction by its internal ID
func (r *transactionRepository) GetByID(ctx context.Context, id string) (*domain.Transaction, error) {
	query := fmt.Sprintf(`
		SELECT %s
		FROM transactions
		WHERE id = $1
		  AND deleted_at IS NULL
	`, transactionSelectExpr)

	var tx domain.Transaction
	dest, state := r.scanTransactionDestSliceWithState(&tx)

	err := r.db.QueryRow(ctx, query, id).Scan(dest...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTransactionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get transaction by ID: %w", err)
	}
	if err := r.postScanTransactionState(&tx, state); err != nil {
		return nil, err
	}

	return &tx, nil
}

// UpdateTags updates the tags and tags_source for a transaction
func (r *transactionRepository) UpdateTags(ctx context.Context, id string, userID string, tags []string, tagsSource []domain.TagSource) error {
	// Serialize tags to JSONB
	tagsJSON, err := json.Marshal(tags)
	if err != nil {
		return fmt.Errorf("failed to marshal tags: %w", err)
	}

	// Serialize tags_source to JSONB
	tagsSourceJSON, err := json.Marshal(tagsSource)
	if err != nil {
		return fmt.Errorf("failed to marshal tags_source: %w", err)
	}

	// #1569 gap G12: the annotation capture, on the statement that REPLACES the user's whole tag
	// array. `FROM transactions prior` is the self-join that makes `prior.*` the pre-update tuple
	// while RETURNING gives the post-update one, so what this statement overwrites is recorded BY
	// THE SAME STATEMENT — see transaction_annotation_capture.go for why a service-layer
	// read-then-write records a wrong prior value under concurrency.
	//
	// ⚠ THE COMMENT BELOW USED TO CLAIM THIS CAPTURE EXISTED. It said narrowing the predicate
	// "would make this statement disagree with the annotation capture that mirrors it", and there
	// was no capture to disagree with — the seventh false written justification on this branch. The
	// sentence is true now because the CTE is here, not because it was reworded.
	query := `
		WITH updated AS (
		-- tombstone-exempt(write:transactions t): liveness is the SERVICE layer's gate, exactly as for
		-- UpdateCategory below (#1569 T8 write-target audit). Narrowing the write predicate would
		-- change WHICH ROWS ARE AFFECTED, which is a behaviour change, and would make this statement
		-- disagree with the annotation capture that mirrors it.
		UPDATE transactions t
		SET tags = $1, tags_source = $2, updated_at = $3
		-- tombstone-exempt(transactions prior, write:transactions t): the prior alias is the PRE-IMAGE of the row this
		-- statement is already updating (prior.id = t.id), not an independent read. Filtering it
		-- would narrow WHICH ROWS THE UPDATE AFFECTS rather than filter a read, and would make the
		-- capture disagree with the write it is capturing — identical to UpdateCategory and
		-- SetReviewed below, and for the same reason (#1569 T8 write-target audit).
		FROM transactions prior
		WHERE prior.id = t.id
		  AND t.id = $4 AND t.user_id = $5
		RETURNING ` + annotationCaptureReturning("t") + `
		)` + annotationCaptureInsert(domain.TransactionAnnotationChangeTags, "updated") + `
		SELECT COUNT(*) FROM updated`

	// Lock-first (#1569 Task 6, review fix R79), with the write's own predicate — see
	// transaction_annotation_capture.go. The capture and the lock are ONE requirement: an unlocked
	// capture can record a transition no write performed, and can suppress the record entirely when
	// the write happens to restore the stale value.
	updatedRows, err := runCapturingUpdate(ctx, r.db,
		"id = $1 AND user_id = $2", []any{id, userID},
		query, []any{tagsJSON, tagsSourceJSON, time.Now(), id, userID})
	if err != nil {
		return fmt.Errorf("failed to update transaction tags: %w", err)
	}

	if updatedRows == 0 {
		return ErrTransactionNotFound
	}

	return nil
}

// UpdateCategory updates the category override for a transaction using the PFC v2 discriminated union.
func (r *transactionRepository) UpdateCategory(ctx context.Context, id string, userID string, overrideKind *string, pfcPrimaryOverride *string, pfcDetailedOverride *string, customCategoryOverrideID *string, categorySource domain.CategorySource) error {
	now := time.Now()

	// #1569 Task 6 (#1579): the annotation capture. `FROM transactions prior` is the self-join that
	// makes `prior.*` the pre-update tuple while RETURNING gives the post-update one, so what this
	// statement overwrites is recorded BY THE SAME STATEMENT — see transaction_annotation_capture.go
	// for why a service-layer read-then-write records a wrong prior value under concurrency.
	query := `
		WITH updated AS (
		UPDATE transactions t
		SET category_override_kind = $1,
		    pfc_primary_override = $2,
		    pfc_detailed_override = $3,
		    custom_category_override_id = $4,
		    category_source = $5,
		    category_override_date = $6,
		    reviewed = true,
		    reviewed_at = $6,
		    -- tombstone-exempt(custom_categories, write:transactions t): name resolution for the category being
		    -- assigned (#1569 Task 9); the UPDATE target is unfiltered because liveness is the service
		    -- layer's gate and narrowing it would change which rows the write affects (#1569 T8).
		    -- Liveness is enforced at the service layer, which validates the id through
		    -- CustomCategoryRepository.GetByID (tombstone-filtered) before calling this —
		    -- see transaction_service.UpdateTransactionCategory. Filtering here as well would
		    -- write a NULL name rather than refuse the write, which is the worse failure.
		    effective_category_name = CASE
		        WHEN $1 = 'custom' THEN
		            (SELECT name FROM custom_categories WHERE id = $4::uuid AND user_id = t.user_id)
		        WHEN $1 = 'pfc' THEN COALESCE(NULLIF($2, ''), t.pfc_primary)
		        ELSE t.pfc_primary
		    END,
		    updated_at = $7
		-- tombstone-exempt(transactions prior, write:transactions t): the prior alias is the PRE-IMAGE of the row this statement is already
		-- updating (prior.id = t.id), not an independent read. Adding prior.deleted_at IS NULL here
		-- would not filter a read -- it would narrow WHICH ROWS THE UPDATE AFFECTS, a behaviour
		-- change well outside #1579's "record the prior value", and it would make the capture
		-- disagree with the write it is capturing. The UPDATE target t is unfiltered for the same
		-- reason and by the same decision: liveness is the service layer's gate (#1569 T8
		-- write-target audit). The statement's own tombstone position is
		-- unchanged and is documented above (#1569 Task 6).
		FROM transactions prior
		WHERE prior.id = t.id
		  AND t.id = $8 AND t.user_id = $9
		RETURNING ` + annotationCaptureReturning("t") + `
		)` + annotationCaptureInsert(domain.TransactionAnnotationChangeCategory, "updated") + `
		SELECT COUNT(*) FROM updated`

	// Lock-first (#1569 Task 6, review fix R79): the row is locked with the write's own predicate
	// before the capturing UPDATE runs, so `prior` cannot be read from a snapshot older than the
	// write it describes. See transaction_annotation_capture.go for the two measured failures the
	// self-join alone left open.
	updatedRows, err := runCapturingUpdate(ctx, r.db,
		"id = $1 AND user_id = $2", []any{id, userID},
		query, []any{overrideKind, pfcPrimaryOverride, pfcDetailedOverride, customCategoryOverrideID, categorySource, now, now, id, userID})
	if err != nil {
		return fmt.Errorf("failed to update transaction category: %w", err)
	}

	if updatedRows == 0 {
		return ErrTransactionNotFound
	}

	return nil
}

// SetReviewed sets the review flag for the given transactions owned by userID.
func (r *transactionRepository) SetReviewed(ctx context.Context, userID string, transactionIDs []string, reviewed bool) (int64, error) {
	if len(transactionIDs) == 0 {
		return 0, nil
	}

	var reviewedAt *time.Time
	if reviewed {
		now := time.Now()
		reviewedAt = &now
	}

	// #1569 Task 6 (#1579): the annotation capture, on the statement BOTH review endpoints use.
	// ⚠ SetTransactionReviewed is a HANDLER, not a second repository method — it calls
	// transactionService.SetReviewed with a one-element slice — so #1579's "four methods" is three,
	// and this one statement covers two of the four endpoints.
	query := `
		WITH updated AS (
		UPDATE transactions t
		SET reviewed = $1, reviewed_at = $2, updated_at = NOW()
		-- tombstone-exempt(transactions prior, write:transactions t): the prior alias is the PRE-IMAGE of the row this statement is already
		-- updating (prior.id = t.id), not an independent read. Adding prior.deleted_at IS NULL here
		-- would not filter a read -- it would narrow WHICH ROWS THE UPDATE AFFECTS, a behaviour
		-- change well outside #1579's "record the prior value", and it would make the capture
		-- disagree with the write it is capturing. The UPDATE target t is unfiltered for the same
		-- reason and by the same decision: liveness is the service layer's gate (#1569 T8
		-- write-target audit). The statement's own tombstone position is
		-- unchanged and is documented above (#1569 Task 6).
		FROM transactions prior
		WHERE prior.id = t.id
		  AND t.user_id = $3 AND t.id = ANY($4::uuid[])
		RETURNING ` + annotationCaptureReturning("t") + `
		)` + annotationCaptureInsert(domain.TransactionAnnotationChangeReview, "updated") + `
		SELECT COUNT(*) FROM updated`

	// Lock-first (#1569 Task 6, review fix R79) — see transaction_annotation_capture.go. This path
	// is where review measured the worse of the two races: two concurrent SetReviewed calls could
	// destroy a committed reviewed_at and record NOTHING, because the stale prior made the write
	// look like a no-op.
	updatedRows, err := runCapturingUpdate(ctx, r.db,
		"user_id = $1 AND id = ANY($2::uuid[])", []any{userID, transactionIDs},
		query, []any{reviewed, reviewedAt, userID, transactionIDs})
	if err != nil {
		return 0, fmt.Errorf("failed to set reviewed: %w", err)
	}
	return updatedRows, nil
}

// BulkUpdateCategory recategorizes many transactions in one UPDATE (Issue #1210).
//
// THE PRIOR VALUE IS NOW RECORDED (#1569 Task 6 / #1579). This header used to say it was not, and
// used to argue the fix should be a ticket rather than a side effect of a retention sweep. That
// ticket is #1579, and this is it. What the old text got right is worth keeping, because it is
// still what bounds the feature:
//
//   - The PROVIDER's categorisation was never at risk. pfc_primary and pfc_detailed are not
//     written by these statements — the override lives in category_override_kind /
//     pfc_*_override / custom_category_override_id, and effective_category_name is derived from
//     them — so clearing an override has always restored the original.
//   - What WAS lost is a PREVIOUS USER OVERRIDE replaced by a newer one, plus (the half #1579
//     missed) the pre-existing reviewed_at, which both category statements clobber
//     unconditionally with `reviewed = true, reviewed_at = now`.
//   - PostHog's `transaction_recategorized` event is not a record of the user's data: it carries
//     the new kind and a count, never the old value. It is analytics, and it stays that way.
//
// The old text also framed this as a BULK defect. It was not — the single-row UpdateCategory above
// lost exactly the same thing, and both statements carry the capture now.
//
// Single statement (atomic by definition) — no explicit transaction needed, and the capture is
// INSIDE it rather than beside it.
// IDs not owned by userID are silently skipped; returns rows affected.
func (r *transactionRepository) BulkUpdateCategory(ctx context.Context, userID string, transactionIDs []string, overrideKind *string, pfcPrimaryOverride *string, pfcDetailedOverride *string, customCategoryOverrideID *string, categorySource domain.CategorySource) (int64, error) {
	if len(transactionIDs) == 0 {
		return 0, nil
	}
	now := time.Now()

	// #1569 Task 6 (#1579): the annotation capture. `FROM transactions prior` is the self-join that
	// makes `prior.*` the pre-update tuple while RETURNING gives the post-update one, so what this
	// statement overwrites is recorded BY THE SAME STATEMENT — see transaction_annotation_capture.go
	// for why a service-layer read-then-write records a wrong prior value under concurrency.
	query := `
		WITH updated AS (
		UPDATE transactions t
		SET category_override_kind      = $1,
		    pfc_primary_override        = $2,
		    pfc_detailed_override       = $3,
		    custom_category_override_id = $4,
		    category_source             = $5,
		    category_override_date      = $6,
		    reviewed                    = true,
		    reviewed_at                 = $6,
		    -- tombstone-exempt(custom_categories, write:transactions t): same as UpdateCategory above (#1569 Task 9)
		    -- — and the UPDATE target is unfiltered for the same reason (#1569 T8). Liveness is the
		    -- service layer's gate (transaction_service.BulkUpdateTransactionCategory validates
		    -- through the tombstone-filtered GetByID); this subquery only resolves the name.
		    effective_category_name     = CASE
		        WHEN $1 = 'custom' THEN
		            (SELECT name FROM custom_categories WHERE id = $4::uuid AND user_id = t.user_id)
		        WHEN $1 = 'pfc' THEN COALESCE(NULLIF($2, ''), t.pfc_primary)
		        ELSE t.pfc_primary
		    END,
		    updated_at = $7
		-- tombstone-exempt(transactions prior, write:transactions t): the prior alias is the PRE-IMAGE of the row this statement is already
		-- updating (prior.id = t.id), not an independent read. Adding prior.deleted_at IS NULL here
		-- would not filter a read -- it would narrow WHICH ROWS THE UPDATE AFFECTS, a behaviour
		-- change well outside #1579's "record the prior value", and it would make the capture
		-- disagree with the write it is capturing. The UPDATE target t is unfiltered for the same
		-- reason and by the same decision: liveness is the service layer's gate (#1569 T8
		-- write-target audit). The statement's own tombstone position is
		-- unchanged and is documented above (#1569 Task 6).
		FROM transactions prior
		WHERE prior.id = t.id
		  AND t.user_id = $8
		  AND t.id = ANY($9::uuid[])
		RETURNING ` + annotationCaptureReturning("t") + `
		)` + annotationCaptureInsert(domain.TransactionAnnotationChangeCategory, "updated") + `
		SELECT COUNT(*) FROM updated`

	// Lock-first (#1569 Task 6, review fix R79) — see transaction_annotation_capture.go. The lock
	// SELECT carries ORDER BY id so two concurrent bulk writers over overlapping id sets take their
	// locks in one direction.
	updatedRows, err := runCapturingUpdate(ctx, r.db,
		"user_id = $1 AND id = ANY($2::uuid[])", []any{userID, transactionIDs},
		query, []any{overrideKind, pfcPrimaryOverride, pfcDetailedOverride, customCategoryOverrideID, categorySource, now, now, userID, transactionIDs})
	if err != nil {
		return 0, fmt.Errorf("failed to bulk update transaction category: %w", err)
	}
	return updatedRows, nil
}

// GetReviewSummary returns review-state counts and top-5 unreviewed categories in one CTE query (Issue #1210).
// Designed for AI-tool consumption: single DB round-trip, structured output, all-time default.
func (r *transactionRepository) GetReviewSummary(ctx context.Context, userID string, filters domain.ReviewSummaryFilters) (*domain.ReviewSummaryResponse, error) {
	// Build optional date conditions (appended to both the summary CTE and the top-categories CTE).
	args := []any{userID}
	argIndex := 2
	dateCondition := ""
	if filters.StartDate != nil {
		dateCondition += fmt.Sprintf(" AND date >= $%d", argIndex)
		args = append(args, *filters.StartDate)
		argIndex++
	}
	if filters.EndDate != nil {
		dateCondition += fmt.Sprintf(" AND date <= $%d", argIndex)
		args = append(args, *filters.EndDate)
		argIndex++ //nolint:ineffassign // kept so a filter added below inherits the correct arg index
	}

	query := fmt.Sprintf(`
		WITH base AS (
			SELECT
				reviewed,
				category_source,
				reviewed_at,
				created_at,
				CASE
					WHEN custom_category_override_id IS NOT NULL THEN custom_category_override_id::text
					ELSE COALESCE(pfc_primary, 'UNCATEGORIZED')
				END AS cat_key,
				COALESCE(effective_category_name, pfc_primary, 'Uncategorized') AS cat_display
			FROM transactions
			WHERE user_id = $1%s
			  AND deleted_at IS NULL
		),
		summary AS (
			SELECT
				COUNT(*) FILTER (WHERE NOT reviewed)                                       AS unreviewed_count,
				COUNT(*) FILTER (WHERE reviewed AND category_source = 'rule')              AS reviewed_by_rule_count,
				COUNT(*) FILTER (WHERE reviewed AND category_source = 'manual')            AS reviewed_by_manual_count,
				COUNT(*) FILTER (WHERE reviewed AND category_source = 'plaid')             AS reviewed_by_plaid_count,
				AVG(EXTRACT(EPOCH FROM reviewed_at - created_at)) FILTER (WHERE reviewed) AS avg_ingestion_to_review_sec
			FROM base
		),
		top_cats AS (
			SELECT
				cat_key,
				cat_display,
				COUNT(*) AS cnt
			FROM base
			WHERE NOT reviewed
			GROUP BY cat_key, cat_display
			ORDER BY cnt DESC
			LIMIT 5
		)
		SELECT
			s.unreviewed_count,
			s.reviewed_by_rule_count,
			s.reviewed_by_manual_count,
			s.reviewed_by_plaid_count,
			s.avg_ingestion_to_review_sec,
			COALESCE(
				json_agg(json_build_object('category_key', t.cat_key, 'display_name', t.cat_display, 'count', t.cnt) ORDER BY t.cnt DESC)
				FILTER (WHERE t.cat_key IS NOT NULL),
				'[]'::json
			) AS top_unreviewed_categories
		FROM summary s
		LEFT JOIN top_cats t ON true
		GROUP BY
			s.unreviewed_count, s.reviewed_by_rule_count,
			s.reviewed_by_manual_count, s.reviewed_by_plaid_count,
			s.avg_ingestion_to_review_sec
	`, dateCondition)

	row := r.db.QueryRow(ctx, query, args...)

	var (
		unreviewedCount       int64
		reviewedByRuleCount   int64
		reviewedByManualCount int64
		reviewedByPlaidCount  int64
		avgIngestionSec       *float64
		topCategoriesJSON     []byte
	)

	err := row.Scan(
		&unreviewedCount,
		&reviewedByRuleCount,
		&reviewedByManualCount,
		&reviewedByPlaidCount,
		&avgIngestionSec,
		&topCategoriesJSON,
	)
	if err != nil {
		// No rows means zero transactions — return a zeroed summary.
		if errors.Is(err, pgx.ErrNoRows) {
			return &domain.ReviewSummaryResponse{
				TopUnreviewedCategories: []domain.TopUnreviewedCategory{},
			}, nil
		}
		return nil, fmt.Errorf("failed to get review summary: %w", err)
	}

	// Unmarshal the json_agg result for top categories.
	var topCategories []domain.TopUnreviewedCategory
	if len(topCategoriesJSON) > 0 && string(topCategoriesJSON) != "null" && string(topCategoriesJSON) != "[]" {
		if err := json.Unmarshal(topCategoriesJSON, &topCategories); err != nil {
			return nil, fmt.Errorf("failed to unmarshal top unreviewed categories: %w", err)
		}
	}
	if topCategories == nil {
		topCategories = []domain.TopUnreviewedCategory{}
	}

	return &domain.ReviewSummaryResponse{
		UnreviewedCount:         unreviewedCount,
		ReviewedByRuleCount:     reviewedByRuleCount,
		ReviewedByManualCount:   reviewedByManualCount,
		ReviewedByPlaidCount:    reviewedByPlaidCount,
		AvgIngestionToReviewSec: avgIngestionSec,
		TopUnreviewedCategories: topCategories,
	}, nil
}

// GetDistinctPlaidCategories returns distinct Plaid category names for a user's transactions
// Uses personal_finance_category.primary (Plaid v2 format) for consistency across the application
func (r *transactionRepository) GetDistinctPlaidCategories(ctx context.Context, userID string) ([]string, error) {
	query := `
		SELECT DISTINCT pfc_primary as cat
		FROM transactions
		WHERE user_id = $1
		  AND deleted_at IS NULL
		  AND pfc_primary IS NOT NULL
		  AND pfc_primary != ''
		ORDER BY cat
	`

	rows, err := r.db.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query distinct plaid categories: %w", err)
	}
	defer rows.Close()

	var categories []string
	for rows.Next() {
		var cat sql.NullString
		if err := rows.Scan(&cat); err != nil {
			return nil, fmt.Errorf("failed to scan category: %w", err)
		}
		if cat.Valid && cat.String != "" {
			categories = append(categories, cat.String)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating category rows: %w", err)
	}

	return categories, nil
}

// =============================================================================
// Issue #492: Income Source Matching Service
// =============================================================================

// GetIncomeSumByCategory returns the sum of income (inflow/negative amount under PFCv2) transactions
// matching the given category within the date range, using the effective category.
func (r *transactionRepository) GetIncomeSumByCategory(ctx context.Context, userID string, category string, startDate, endDate time.Time) (float64, error) {
	query := `
		SELECT COALESCE(ABS(SUM(amount)), 0)
		FROM transactions
		WHERE user_id = $1
		  AND deleted_at IS NULL
		  AND amount < 0
		  AND date BETWEEN $2 AND $3
		  AND effective_pfc_primary = $4
	`

	var sum float64
	if err := r.db.QueryRow(ctx, query, userID, startDate, endDate, category).Scan(&sum); err != nil {
		return 0, fmt.Errorf("failed to get income sum by category: %w", err)
	}
	return sum, nil
}

// GetIncomeSumByTag returns the sum of income (inflow/negative amount under PFCv2) transactions
// with the given tag within the date range.
// Uses JSONB ? operator to check if tags array contains the tag.
func (r *transactionRepository) GetIncomeSumByTag(ctx context.Context, userID string, tag string, startDate, endDate time.Time) (float64, error) {
	query := `
		SELECT COALESCE(ABS(SUM(amount)), 0)
		FROM transactions
		WHERE user_id = $1
		  AND deleted_at IS NULL
		  AND amount < 0
		  AND date BETWEEN $2 AND $3
		  AND tags ? $4
	`

	var sum float64
	err := r.db.QueryRow(ctx, query, userID, startDate, endDate, tag).Scan(&sum)
	if err != nil {
		return 0, fmt.Errorf("failed to get income sum by tag: %w", err)
	}

	return sum, nil
}

// =============================================================================
// Issue #493: Budget Category Matching Service
// =============================================================================

// buildCategoryClause returns the SQL fragment and args for matching
// a transaction's effective category against a budget category discriminated union.
// Uses the pre-computed effective_pfc_primary, effective_pfc_detailed, and
// custom_category_override_id columns (no CASE expressions needed).
// argStart is the starting positional parameter number (e.g. 4 when $1-$3 are already used).
// Returns (clause string, args []interface{}).
func buildCategoryClause(categoryKind string, pfcPrimary *string, pfcDetailed *string, customCategoryID *string, argStart int) (string, []interface{}) {
	// Issue #1203: roll-up is intrinsic. Targeting a parent category sums the
	// parent plus its children, applied consistently here so every caller
	// (spent sum, preview stats, transaction listing) agrees. The subqueries are
	// no-ops for leaf categories, so existing single-category budgets are unaffected.
	// user_id is always $1 in the budget-matching queries that call this helper.
	switch categoryKind {
	case "pfc":
		if pfcPrimary == nil {
			return "1=0", nil
		}
		if pfcDetailed != nil {
			// A PFC detailed is a leaf — exact match, never rolls up.
			return fmt.Sprintf(`(effective_pfc_primary = $%d AND effective_pfc_detailed = $%d)`, argStart, argStart+1),
				[]interface{}{*pfcPrimary, *pfcDetailed}
		}
		// A PFC primary rolls up its detaileds (via effective_pfc_primary) AND any
		// custom categories the user nested under it (parent_pfc_primary).
		clause := fmt.Sprintf(`(effective_pfc_primary = $%d OR (category_override_kind = 'custom' AND custom_category_override_id IN (SELECT id FROM custom_categories WHERE user_id = $1 AND parent_pfc_primary = $%d))) /* tombstone-exempt(custom_categories): HISTORY FILTER (#1569 Task 9) — a PFC-primary budget rolls up the custom categories nested under it, and the transactions filed under a deleted one are still spend that happened in that primary. Excluding them would silently shrink a closed period's total. */`, argStart, argStart)
		return clause, []interface{}{*pfcPrimary}
	case "custom":
		if customCategoryID == nil {
			return "1=0", nil
		}
		// A custom category rolls up its custom children (parent_id). A leaf
		// custom category's subquery resolves to just itself.
		clause := fmt.Sprintf(`(category_override_kind = 'custom' AND custom_category_override_id IN (SELECT id FROM custom_categories WHERE user_id = $1 AND (id::text = $%d OR parent_id::text = $%d))) /* tombstone-exempt(custom_categories): HISTORY FILTER (#1569 Task 9) — same reason as the PFC branch above; a budget line's spend must keep counting the transactions already filed under a since-deleted child category. */`, argStart, argStart)
		return clause, []interface{}{*customCategoryID}
	default:
		return "1=0", nil
	}
}

// GetSpentSumForBudgetCategory returns the sum of expenses (outflow/positive amount under PFCv2) transactions
// matching the budget category's effective category and/or tags based on match logic.
func (r *transactionRepository) GetSpentSumForBudgetCategory(ctx context.Context, userID string, budgetCat *domain.BudgetCategory, startDate, endDate time.Time) (float64, error) {
	if budgetCat == nil {
		return 0, nil
	}
	matchLogic := budgetCat.MatchLogic
	if matchLogic == "" {
		matchLogic = domain.MatchLogicCategoryOnly
	}
	tags := budgetCat.Tags

	var query string
	var args []interface{}
	baseArgs := []interface{}{userID, startDate, endDate}

	switch matchLogic {
	case domain.MatchLogicCategoryOnly:
		catClause, catArgs := buildCategoryClause(budgetCat.CategoryKind, budgetCat.PFCPrimary, budgetCat.PFCDetailed, budgetCat.CustomCategoryID, 4)
		query = fmt.Sprintf(`
			SELECT COALESCE(ABS(SUM(amount)), 0)
			FROM transactions
			WHERE user_id = $1 AND amount > 0
			  AND deleted_at IS NULL
			  AND date BETWEEN $2 AND $3
			  AND (%s)
		`, catClause)
		args = append(baseArgs, catArgs...)

	case domain.MatchLogicTagOnly:
		query = `
			SELECT COALESCE(ABS(SUM(amount)), 0)
			FROM transactions
			WHERE user_id = $1 AND amount > 0
			  AND deleted_at IS NULL
			  AND date BETWEEN $2 AND $3
			  AND tags ?| $4
		`
		args = append(baseArgs, pq.Array(tags))

	case domain.MatchLogicAnd:
		catClause, catArgs := buildCategoryClause(budgetCat.CategoryKind, budgetCat.PFCPrimary, budgetCat.PFCDetailed, budgetCat.CustomCategoryID, 4)
		nextArg := 4 + len(catArgs)
		query = fmt.Sprintf(`
			SELECT COALESCE(ABS(SUM(amount)), 0)
			FROM transactions
			WHERE user_id = $1 AND amount > 0
			  AND deleted_at IS NULL
			  AND date BETWEEN $2 AND $3
			  AND (%s)
			  AND tags ?| $%d
		`, catClause, nextArg)
		args = append(baseArgs, catArgs...)
		args = append(args, pq.Array(tags))

	case domain.MatchLogicOr:
		catClause, catArgs := buildCategoryClause(budgetCat.CategoryKind, budgetCat.PFCPrimary, budgetCat.PFCDetailed, budgetCat.CustomCategoryID, 4)
		nextArg := 4 + len(catArgs)
		query = fmt.Sprintf(`
			SELECT COALESCE(ABS(SUM(amount)), 0)
			FROM transactions
			WHERE user_id = $1 AND amount > 0
			  AND deleted_at IS NULL
			  AND date BETWEEN $2 AND $3
			  AND (
				(%s)
				OR tags ?| $%d
			  )
		`, catClause, nextArg)
		args = append(baseArgs, catArgs...)
		args = append(args, pq.Array(tags))

	default:
		return 0, fmt.Errorf("unsupported match logic: %s", matchLogic)
	}

	var sum float64
	if err := r.db.QueryRow(ctx, query, args...).Scan(&sum); err != nil {
		return 0, fmt.Errorf("failed to get spent sum for budget category: %w", err)
	}
	return sum, nil
}

// GetMatchingTransactionStats returns the count and sum of expenses matching the given
// category configuration and/or tags based on match logic. Issue #497: preview feature.
func (r *transactionRepository) GetMatchingTransactionStats(ctx context.Context, userID string, categoryKind string, pfcPrimary *string, pfcDetailed *string, customCategoryID *string, tags []string, matchLogic domain.MatchLogic, startDate, endDate time.Time) (int, float64, error) {
	if matchLogic == "" {
		matchLogic = domain.MatchLogicCategoryOnly
	}

	baseArgs := []interface{}{userID, startDate, endDate}

	var query string
	var args []interface{}

	switch matchLogic {
	case domain.MatchLogicCategoryOnly:
		catClause, catArgs := buildCategoryClause(categoryKind, pfcPrimary, pfcDetailed, customCategoryID, 4)
		query = fmt.Sprintf(`
			SELECT COUNT(*), COALESCE(ABS(SUM(amount)), 0)
			FROM transactions
			WHERE user_id = $1 AND amount > 0
			  AND deleted_at IS NULL
			  AND date BETWEEN $2 AND $3
			  AND (%s)
		`, catClause)
		args = append(baseArgs, catArgs...)

	case domain.MatchLogicTagOnly:
		query = `
			SELECT COUNT(*), COALESCE(ABS(SUM(amount)), 0)
			FROM transactions
			WHERE user_id = $1 AND amount > 0
			  AND deleted_at IS NULL
			  AND date BETWEEN $2 AND $3
			  AND tags ?| $4
		`
		args = append(baseArgs, pq.Array(tags))

	case domain.MatchLogicAnd:
		catClause, catArgs := buildCategoryClause(categoryKind, pfcPrimary, pfcDetailed, customCategoryID, 4)
		nextArg := 4 + len(catArgs)
		query = fmt.Sprintf(`
			SELECT COUNT(*), COALESCE(ABS(SUM(amount)), 0)
			FROM transactions
			WHERE user_id = $1 AND amount > 0
			  AND deleted_at IS NULL
			  AND date BETWEEN $2 AND $3
			  AND (%s)
			  AND tags ?| $%d
		`, catClause, nextArg)
		args = append(baseArgs, catArgs...)
		args = append(args, pq.Array(tags))

	case domain.MatchLogicOr:
		catClause, catArgs := buildCategoryClause(categoryKind, pfcPrimary, pfcDetailed, customCategoryID, 4)
		nextArg := 4 + len(catArgs)
		query = fmt.Sprintf(`
			SELECT COUNT(*), COALESCE(ABS(SUM(amount)), 0)
			FROM transactions
			WHERE user_id = $1 AND amount > 0
			  AND deleted_at IS NULL
			  AND date BETWEEN $2 AND $3
			  AND (
				(%s)
				OR tags ?| $%d
			  )
		`, catClause, nextArg)
		args = append(baseArgs, catArgs...)
		args = append(args, pq.Array(tags))

	default:
		return 0, 0, fmt.Errorf("unsupported match logic: %s", matchLogic)
	}

	var count int
	var sum float64
	if err := r.db.QueryRow(ctx, query, args...).Scan(&count, &sum); err != nil {
		return 0, 0, fmt.Errorf("failed to get matching transaction stats: %w", err)
	}
	return count, sum, nil
}

// =============================================================================
// Issue #499: Transaction-Budget Linking UI
// =============================================================================

// GetTransactionsForBudgetCategory returns paginated transactions matching the given category
// and/or tags based on match logic, along with total count and amount.
// Uses the budget category's discriminated union to match against effective category.
// Returns transactions ordered by date DESC.
func (r *transactionRepository) GetTransactionsForBudgetCategory(ctx context.Context, userID string, budgetCat *domain.BudgetCategory, startDate, endDate time.Time, limit, offset int) ([]domain.Transaction, int, float64, error) {
	if budgetCat == nil {
		return []domain.Transaction{}, 0, 0, nil
	}
	matchLogic := budgetCat.MatchLogic
	if matchLogic == "" {
		matchLogic = domain.MatchLogicCategoryOnly
	}
	tags := budgetCat.Tags

	baseArgs := []interface{}{userID, startDate, endDate}

	var whereClause string
	var allBaseArgs []interface{}

	switch matchLogic {
	case domain.MatchLogicCategoryOnly:
		catClause, catArgs := buildCategoryClause(budgetCat.CategoryKind, budgetCat.PFCPrimary, budgetCat.PFCDetailed, budgetCat.CustomCategoryID, 4)
		whereClause = fmt.Sprintf(`user_id = $1 AND amount > 0 AND date BETWEEN $2 AND $3 AND (%s)`, catClause)
		allBaseArgs = append(baseArgs, catArgs...)

	case domain.MatchLogicTagOnly:
		whereClause = `user_id = $1 AND amount > 0 AND date BETWEEN $2 AND $3 AND tags ?| $4`
		allBaseArgs = append(baseArgs, pq.Array(tags))

	case domain.MatchLogicAnd:
		catClause, catArgs := buildCategoryClause(budgetCat.CategoryKind, budgetCat.PFCPrimary, budgetCat.PFCDetailed, budgetCat.CustomCategoryID, 4)
		nextArg := 4 + len(catArgs)
		whereClause = fmt.Sprintf(`user_id = $1 AND amount > 0 AND date BETWEEN $2 AND $3 AND (%s) AND tags ?| $%d`, catClause, nextArg)
		allBaseArgs = append(baseArgs, catArgs...)
		allBaseArgs = append(allBaseArgs, pq.Array(tags))

	case domain.MatchLogicOr:
		catClause, catArgs := buildCategoryClause(budgetCat.CategoryKind, budgetCat.PFCPrimary, budgetCat.PFCDetailed, budgetCat.CustomCategoryID, 4)
		nextArg := 4 + len(catArgs)
		whereClause = fmt.Sprintf(`user_id = $1 AND amount > 0 AND date BETWEEN $2 AND $3 AND ((%s) OR tags ?| $%d)`, catClause, nextArg)
		allBaseArgs = append(baseArgs, catArgs...)
		allBaseArgs = append(allBaseArgs, pq.Array(tags))

	default:
		return nil, 0, 0, fmt.Errorf("unsupported match logic: %s", matchLogic)
	}

	// Get total count and sum
	countQuery := fmt.Sprintf(`SELECT COUNT(*), COALESCE(SUM(amount), 0) FROM transactions WHERE deleted_at IS NULL AND %s`, whereClause)
	var totalCount int
	var totalAmount float64
	if err := r.db.QueryRow(ctx, countQuery, allBaseArgs...).Scan(&totalCount, &totalAmount); err != nil {
		return nil, 0, 0, fmt.Errorf("failed to get transaction count: %w", err)
	}

	// Get paginated transactions
	pagArgOffset := len(allBaseArgs) + 1
	selectQuery := fmt.Sprintf(`
		SELECT %s
		FROM transactions
		WHERE %s
		  AND deleted_at IS NULL
		ORDER BY date DESC, created_at DESC
		LIMIT $%d OFFSET $%d
	`, transactionSelectExpr, whereClause, pagArgOffset, pagArgOffset+1)

	selectArgs := append(allBaseArgs, limit, offset)
	rows, err := r.db.Query(ctx, selectQuery, selectArgs...)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("failed to query transactions: %w", err)
	}
	defer rows.Close()

	transactions, err := r.scanTransactions(rows)
	if err != nil {
		return nil, 0, 0, err
	}
	return transactions, totalCount, totalAmount, nil
}

// GetByUserAccountID retrieves all transactions for a specific user_account.
// Issue #171: Provides stable queries across re-links since user_account_id survives re-links.
//
// NOTE: This SELECT includes user_account_id at position 5. Everything either side of it is
// transactionSelectColumns itself, so the two cannot drift apart (#1569).
func (r *transactionRepository) GetByUserAccountID(ctx context.Context, userID string, userAccountID string) ([]domain.Transaction, error) {
	query := fmt.Sprintf(`
		SELECT id, user_id, account_id, linked_item_id, user_account_id, %s
		FROM transactions
		WHERE user_id = $1 AND user_account_id = $2
		  AND deleted_at IS NULL
		ORDER BY date DESC, created_at DESC
	`, strings.Join(transactionSelectColumns[4:], ", "))

	rows, err := r.db.Query(ctx, query, userID, userAccountID)
	if err != nil {
		return nil, fmt.Errorf("failed to query transactions by user_account_id: %w", err)
	}
	defer rows.Close()

	var transactions []domain.Transaction

	for rows.Next() {
		var tx domain.Transaction
		shared, state := r.scanTransactionDestSliceWithState(&tx)

		// user_account_id is projected at position 5, between linked_item_id and source, so the
		// destination slice is the shared one with &tx.UserAccountID spliced in at the same index.
		// Splicing beats a hand-written copy of the list: #1569 appended three columns to
		// transactionSelectColumns and the two hand-maintained copies that existed then
		// (scanTransactions, GetByExternalID) both silently went out of sync.
		dest := make([]any, 0, len(shared)+1)
		dest = append(dest, shared[:4]...)
		dest = append(dest, &tx.UserAccountID)
		dest = append(dest, shared[4:]...)

		if err := rows.Scan(dest...); err != nil {
			return nil, fmt.Errorf("failed to scan transaction: %w", err)
		}
		if err := r.postScanTransactionState(&tx, state); err != nil {
			return nil, err
		}

		transactions = append(transactions, tx)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating transaction rows: %w", err)
	}

	return transactions, nil
}

// UpdateEffectiveCategoryNameByCustomCategoryID bulk-updates effective_category_name
// for all transactions in a user's account that are assigned to the given custom category.
// Used when a custom category is renamed. Returns the number of rows updated.
func (r *transactionRepository) UpdateEffectiveCategoryNameByCustomCategoryID(ctx context.Context, userID string, customCategoryID string) (int64, error) {
	// Read the current name from custom_categories at execution time so that stale
	// job payloads from rapid successive renames always converge to the latest name.
	query := `
		UPDATE transactions
		SET effective_category_name = (
			-- tombstone-exempt(custom_categories, write:transactions): HISTORICAL name resolution (#1569 Task 9),
			-- with the UPDATE target unfiltered so a tombstoned transaction keeps a correct name rather
			-- than a NULL one (#1569 T8 write-target audit). A rename job that
			-- lands after the category was deleted must still write the name, not NULL. The
			-- rename itself is gated upstream by the tombstone-filtered GetByID.
			SELECT name FROM custom_categories
			WHERE id = $2::uuid AND user_id = $1
		),
		updated_at = NOW()
		WHERE user_id = $1
		  AND category_override_kind = 'custom'
		  AND custom_category_override_id = $2::uuid
	`
	ct, err := r.db.Exec(ctx, query, userID, customCategoryID)
	if err != nil {
		return 0, fmt.Errorf("failed to update effective category name: %w", err)
	}
	return ct.RowsAffected(), nil
}

// GetByUserIDKeyset retrieves transactions using keyset (cursor) pagination.
// cursor=nil returns the first page. Fetches limit+1 rows to detect end-of-list.
// Returns a slice of up to limit transactions and the cursor for the next page (nil = end of list).
func (r *transactionRepository) GetByUserIDKeyset(
	ctx context.Context,
	userID string,
	filters domain.TransactionFilters,
	cursor *domain.TransactionCursor,
	limit int,
) ([]domain.Transaction, *domain.TransactionCursor, *domain.FeedTotals, error) {
	whereClause, args := buildWhereClause(userID, filters)

	sortKey := filters.SortBy
	if sortKey == "" {
		sortKey = "date"
	}
	sortDir := strings.ToLower(filters.SortDir)
	if sortDir == "" {
		sortDir = "desc"
	}

	sortDesc, ok := sortDescriptors[sortKey]
	if !ok {
		sortDesc = sortDescriptors["date"]
		sortKey = "date"
	}
	col := sortDesc.expr
	needsJoins := sortDesc.needsMerchantJoin || filters.Search != nil

	if cursor != nil {
		argBase := len(args) + 1
		if cursor.SV == nil {
			args = append(args, cursor.ID.String())
			if sortDir == "desc" {
				// DESC NULLS FIRST: cursor in NULL region; include remaining NULLs + all non-NULLs
				whereClause += fmt.Sprintf(" AND ((%s IS NULL AND t.id > $%d) OR %s IS NOT NULL)", col, argBase, col)
			} else {
				// ASC NULLS LAST: cursor in NULL region at end; only remaining NULLs
				whereClause += fmt.Sprintf(" AND (%s IS NULL AND t.id > $%d)", col, argBase)
			}
		} else {
			cmpOp := "<"
			if sortDir == "asc" {
				cmpOp = ">"
			}
			args = append(args, *cursor.SV, cursor.ID.String())

			if sortKey == "date" {
				whereClause += fmt.Sprintf(
					" AND ((%s::date %s $%d::date) OR (%s::date = $%d::date AND t.id > $%d))",
					col, cmpOp, argBase, col, argBase, argBase+1,
				)
			} else if sortKey == "amount" {
				whereClause += fmt.Sprintf(
					" AND ((%s %s $%d::numeric) OR (%s = $%d::numeric AND t.id > $%d))",
					col, cmpOp, argBase, col, argBase, argBase+1,
				)
			} else if sortKey == "category" && sortDir == "asc" {
				// category can be NULL; ASC NULLS LAST — include NULL rows that come after non-NULLs
				whereClause += fmt.Sprintf(
					" AND ((%s %s $%d) OR (%s = $%d AND t.id > $%d) OR %s IS NULL)",
					col, cmpOp, argBase, col, argBase, argBase+1, col,
				)
			} else if sortKey == "merchant" {
				// effectiveMerchantNameExpr is never NULL; no NULL branch needed.
				whereClause += fmt.Sprintf(
					" AND ((%s %s $%d) OR (%s = $%d AND t.id > $%d))",
					col, cmpOp, argBase, col, argBase, argBase+1,
				)
			} else {
				whereClause += fmt.Sprintf(
					" AND ((%s %s $%d) OR (%s = $%d AND t.id > $%d))",
					col, cmpOp, argBase, col, argBase, argBase+1,
				)
			}
		}
	}

	dir := "DESC"
	if sortDir == "asc" {
		dir = "ASC"
	}
	orderBy := fmt.Sprintf("ORDER BY %s %s, t.id ASC", col, dir)

	joinClause := ""
	if needsJoins {
		joinClause = merchantSortJoins
	}

	// ── Whole-set totals, FIRST PAGE ONLY (#1626 Phase 2) ────────────────────
	//
	// WHY. The response carried only a page, so "what does this filtered set come to" was not a
	// question the product could answer — which is what made the reports Sankey's click-through
	// claim unverifiable rather than merely untested.
	//
	// ⚠ STATED PRECISELY, because the loose form ("this node's amount equals the filtered list's
	// total") is FALSE for income and shipped in five comments before this branch was reviewed. A
	// hierarchy node's amount is COALESCE(SUM(ABS(t.amount)),0) — an unsigned chart quantity — while
	// this total is SUM(t.amount) under PFCv2 signs, where inflows are NEGATIVE. So the property that
	// holds over the same filtered set is |node amount| == |total_amount|; for an expense node the
	// two numbers also happen to be equal, and for an income node they differ by a factor of −1.
	// Both SQL expressions are right for their own consumer: a chart needs magnitudes, a ledger
	// total needs signs. Do not "fix" either one to make the sentence simpler.
	//
	// ⚠ ONLY when no cursor was supplied. Re-aggregating the whole filtered set on every page of
	// the busiest list in the app would be real cost for a number that does not change as the user
	// pages. TestBankingKeysetCursoredPage_SkipsTheAggregateEntirely pins the skip.
	//
	// ⚠ IT REUSES THE PAGE QUERY’S OWN FRAGMENTS AND ARGS — that is the whole correctness
	// argument. Rebuilding the predicate would let the total drift from the rows it describes, and
	// a wrong total is worse than none because the client cannot tell that it is wrong. Two SQL
	// strings agreeing is not two result sets agreeing, so the agreement is pinned behaviourally
	// by TestFeedTotals_AgreeWithTheRowsTheyDescribe_Integration, not by reading these queries.
	// Exact here because the cursor clause is the only later mutation of whereClause and the LIMIT
	// arg the only later append to args, and neither has happened yet on this path.
	var totals *domain.FeedTotals
	if cursor == nil {
		aggQuery := fmt.Sprintf(`
		-- tombstone-exempt(transactions t): shares buildWhereClause's interpolated WHERE with the
		-- page query below, which emits transactionTombstoneCondition("t", ...) unconditionally (#1569).
		SELECT COUNT(*), COALESCE(SUM(t.amount), 0)
		FROM transactions t
		%s
		%s
	`, joinClause, whereClause)

		var count int
		var amount float64
		if err := r.db.QueryRow(ctx, aggQuery, args...).Scan(&count, &amount); err != nil {
			return nil, nil, nil, fmt.Errorf("failed to aggregate transactions (keyset totals): %w", err)
		}
		totals = &domain.FeedTotals{Count: count, Amount: amount}
	}

	// For merchant sort, select an extra column to capture the EFF value for cursor building.
	selectExpr := transactionSelectExprT
	effMerchantExtra := ""
	if sortKey == "merchant" {
		effMerchantExtra = ", " + effectiveMerchantNameExpr + " AS effective_merchant_sort_name"
	}

	args = append(args, limit+1)
	limitPlaceholder := len(args)

	query := fmt.Sprintf(`
		-- tombstone-exempt(transactions t): buildWhereClause emits transactionTombstoneCondition("t", ...) into
		-- the interpolated WHERE below, unconditionally and before any optional filter (#1569).
		SELECT %s%s
		FROM transactions t
		%s
		%s
		%s
		LIMIT $%d
	`, selectExpr, effMerchantExtra, joinClause, whereClause, orderBy, limitPlaceholder)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to query transactions (keyset): %w", err)
	}
	defer rows.Close()

	// Scan rows; for merchant sort we also capture the EFF sort value.
	var txs []domain.Transaction
	var effMerchantNames []*string
	if sortKey == "merchant" {
		for rows.Next() {
			var tx domain.Transaction
			var effMerchantName *string
			dest, state := r.scanTransactionDestSliceWithState(&tx)
			dest = append(dest, &effMerchantName)
			if err := rows.Scan(dest...); err != nil {
				return nil, nil, nil, fmt.Errorf("failed to scan transaction: %w", err)
			}
			if err := r.postScanTransactionState(&tx, state); err != nil {
				return nil, nil, nil, err
			}
			txs = append(txs, tx)
			effMerchantNames = append(effMerchantNames, effMerchantName)
		}
		if err := rows.Err(); err != nil {
			return nil, nil, nil, fmt.Errorf("error iterating transactions (keyset): %w", err)
		}
	} else {
		txs, err = r.scanTransactions(rows)
		if err != nil {
			return nil, nil, nil, err
		}
	}

	if err := r.enrichEffectiveMerchantNames(ctx, userID, txs); err != nil {
		return nil, nil, nil, err
	}

	if len(txs) == limit+1 {
		last := txs[limit-1]
		var effMN *string
		if sortKey == "merchant" && len(effMerchantNames) > limit-1 {
			effMN = effMerchantNames[limit-1]
		}
		nextCursor, buildErr := buildKeysetCursor(last, sortKey, sortDir, effMN)
		if buildErr != nil {
			return nil, nil, nil, fmt.Errorf("failed to build next cursor: %w", buildErr)
		}
		return txs[:limit], nextCursor, totals, nil
	}

	return txs, nil, totals, nil
}

// buildKeysetCursor constructs a TransactionCursor from a transaction row.
// effMerchantName is the scanned effective merchant sort name (only used for sortKey=="merchant").
func buildKeysetCursor(tx domain.Transaction, sortKey, sortDir string, effMerchantName *string) (*domain.TransactionCursor, error) {
	txID, err := uuid.Parse(tx.ID)
	if err != nil {
		return nil, fmt.Errorf("invalid transaction id %q: %w", tx.ID, err)
	}

	cursor := &domain.TransactionCursor{
		V:  1,
		K:  sortKey,
		D:  sortDir,
		ID: txID,
	}

	switch sortKey {
	case "date":
		effDate := tx.Date
		if tx.AuthorizedDate != nil {
			effDate = *tx.AuthorizedDate
		}
		sv := effDate.Format(time.DateOnly)
		cursor.SV = &sv
		utcDate := effDate.UTC()
		cursor.SVT = &utcDate
	case "merchant":
		// Use the scanned EFF value when available; fall back to tx.Name.
		if effMerchantName != nil {
			cursor.SV = effMerchantName
		} else {
			sv := tx.Name
			cursor.SV = &sv
		}
	case "amount":
		sv := fmt.Sprintf("%g", tx.Amount)
		cursor.SV = &sv
	case "category":
		if tx.EffectiveCategoryName != nil {
			cursor.SV = tx.EffectiveCategoryName
		} else if tx.EffectivePFCPrimary != nil {
			cursor.SV = tx.EffectivePFCPrimary
		} else {
			cursor.SV = tx.PFCPrimary
		}
	default:
		sv := effDateStr(tx)
		cursor.SV = &sv
	}

	return cursor, nil
}

// effDateStr returns COALESCE(authorized_date, date) formatted as a date string.
func effDateStr(tx domain.Transaction) string {
	if tx.AuthorizedDate != nil {
		return tx.AuthorizedDate.Format(time.DateOnly)
	}
	return tx.Date.Format(time.DateOnly)
}

// nilOrEmpty returns s if non-nil, else empty string.
// Used for optional string pointers in scanning operations.
func nilOrEmpty(s *string) string {
	if s != nil {
		return *s
	}
	return ""
}

// GetInflowTransactionsForAccount returns posted inflow transactions (amount < 0 under
// PFCv2) for a specific account within [start, end].  Chain-aware via user_account_id
// (Issue #1109). Returns lightweight projections; Magnitude = ABS(amount) (always
// positive — #1303 sign-inversion guard, Issue #1300 plan section 1D).
func (r *transactionRepository) GetInflowTransactionsForAccount(
	ctx context.Context,
	userID, accountID string,
	start, end time.Time,
) ([]*domain.SavingsInflowTransaction, error) {
	query := `
		SELECT t.id::text, ABS(t.amount) AS magnitude, t.date::text
		FROM transactions t
		WHERE t.user_id = $1
		  AND t.deleted_at IS NULL
		  AND (
		      t.user_account_id = (SELECT user_account_id FROM accounts WHERE id = $2 AND user_account_id IS NOT NULL)
		      OR (t.account_id = $2 AND t.user_account_id IS NULL)
		  )
		  AND t.amount < 0
		  AND t.pending = false
		  AND t.date >= $3::date
		  AND t.date <= $4::date
		ORDER BY t.date ASC
	`

	rows, err := r.db.Query(ctx, query, userID, accountID, start, end)
	if err != nil {
		return nil, fmt.Errorf("failed to query inflow transactions for account: %w", err)
	}
	defer rows.Close()

	var inflows []*domain.SavingsInflowTransaction
	for rows.Next() {
		inflow := &domain.SavingsInflowTransaction{}
		if err := rows.Scan(&inflow.ID, &inflow.Magnitude, &inflow.Date); err != nil {
			return nil, fmt.Errorf("failed to scan inflow transaction: %w", err)
		}
		inflows = append(inflows, inflow)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating inflow transactions: %w", err)
	}

	return inflows, nil
}

// GetRecentInflowsByUserID returns posted (non-pending) inflow transactions
// (amount < 0 under PFCv2) for a user with a date on or after since.
// Only the fields required for correlation matching are populated.
// Issue #972: Savings transfer automation.
func (r *transactionRepository) GetRecentInflowsByUserID(ctx context.Context, userID string, since time.Time) ([]*domain.Transaction, error) {
	query := `
		SELECT id, user_id, account_id, user_account_id, amount, date, pending
		FROM transactions
		WHERE user_id = $1
		  AND deleted_at IS NULL
		  AND amount < 0
		  AND pending = false
		  AND date >= $2
		ORDER BY date ASC
	`

	rows, err := r.db.Query(ctx, query, userID, since)
	if err != nil {
		return nil, fmt.Errorf("failed to query recent inflow transactions: %w", err)
	}
	defer rows.Close()

	var transactions []*domain.Transaction
	for rows.Next() {
		var tx domain.Transaction
		if err := rows.Scan(
			&tx.ID,
			&tx.UserID,
			&tx.AccountID,
			&tx.UserAccountID,
			&tx.Amount,
			&tx.Date,
			&tx.Pending,
		); err != nil {
			return nil, fmt.Errorf("failed to scan inflow transaction: %w", err)
		}
		transactions = append(transactions, &tx)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating inflow transactions: %w", err)
	}

	return transactions, nil
}

// =============================================================================
// Issue #138: Unified activity feed (banking + investment UNION ALL)
// =============================================================================

// GetByUserIDKeysetUnified returns a merged, cursor-paginated feed of banking and
// investment transactions using a UNION ALL with a 3-part keyset
// (sort_date DESC, source ASC, id ASC).
//
// Banking sort_date  = COALESCE(authorized_date, date)::date
// Investment sort_date = date::date
//
// The V:2 cursor carries Src ("banking"|"investment") + Inc=true so the service
// layer can detect flag-toggle mid-scroll and return 400 (cursor mismatch).
// Note: external_transaction_id from banking rows is encrypted; we decrypt it
// inline after scanning.
// retainedAccountVisibilityLegs renders BOTH historical account-visibility legs for a dated row
// table aliased `alias` with a date column `dateCol`, as ONE string that a caller cannot take half
// of.
//
// WHY THEY ARE ONE FUNCTION (#1569, task 21 / C4). The two legs below are COMPLEMENTS, and that is
// not a stylistic observation — it is the whole reason this exists:
//
//	activeChainSiblingLeg              fires only when an ACTIVE account shares the row's chain key
//	retainedAccountNoLiveSuccessorLeg  fires only when NO active account shares it (its NOT EXISTS)
//
// So a read that carries exactly one of them covers exactly half of the retained universe and
// silently drops the other half. IncomeSummary and investmentTransactionRepository.GetByUserID both
// carried only the second: a brokerage that was RE-LINKED (old account 'replaced', new one 'active'
// on the same user_account_id) had every pre-relink dividend excluded by both legs at once — the
// chain leg was absent and the retained leg's NOT EXISTS was closed by the live successor. Measured
// on a real chain: IncomeSummary 0.00 and the investments feed empty, while
// GetByUserIDKeysetUnified over the same rows listed the dividend.
//
// Rendering them together makes that state unrepresentable. The individual legs are documented
// below; neither should be called on its own, and TestRetainedVisibilityLegsTravelTogether holds
// that to account.
func retainedAccountVisibilityLegs(alias, dateCol string) string {
	return activeChainSiblingLeg(alias) +
		retainedAccountNoLiveSuccessorLeg(alias, dateCol) +
		accountWithNoActiveIntervalLeg(alias)
}

// accountWithNoActiveIntervalLeg admits a transaction whose account has NO 'active' row in
// account_status_history at all (#1626 Phase 2).
//
// WHY IT EXISTS. The two legs above cover "the chain has a live sibling" and "this account was
// active over an interval covering the row's date". An account with no 'active' row satisfies
// neither, and migration 000152's zero-length-interval guard creates exactly that shape: it skips
// the 'active' prefix when replaced_at/removed_at/updated_at is NULL or <= created_at, leaving only
// a terminal row. The comment on the leg above already recorded the consequence — "its transactions
// stay hidden ... a backfill-data property, not a decision taken here".
//
// It stopped being merely a data property once the reports Sankey began offering click-through
// whose list is supposed to account for the node's amount: the analytics reads apply no visibility
// predicate at all, so such a row counts toward the chart and could never appear in the list.
// Hiding a user's own retained history because an interval was never written is the #1569 defect in
// miniature, so absence of interval data is treated as "no reason to hide", not as evidence.
//
// ⚠ NARROW BY CONSTRUCTION, and this is the whole safety argument — stated precisely, because an
// earlier version of this comment stated it wrongly and this branch's own fixture is the
// counterexample. What the leg cannot do is override a RECORDED active interval: it fires only when
// there is no 'active' row WHATSOEVER, so the moment one exists the historical leg governs the
// account with its existing date bounds, whatever the removal cause.
//
// It does NOT key on removal cause, and it must not. Removal CAUSE and whether an interval was ever
// WRITTEN are orthogonal: migration 000152's removed branch skips the 'active' prefix whenever
// removed_at IS NULL or <= created_at while hardcoding the terminal reason 'user_disconnect'
// (000152_backfill_account_status_history.up.sql, the 'removed accounts' INSERT pair), and
// record_account_status_change() writes the same single-terminal-row shape for any account whose
// FIRST recorded status is already terminal. So a 'user_disconnect' account with no 'active' row is
// reachable, and this leg admits its transactions. That is the #1569 answer, not an accident: a
// disconnected chain KEEPS its history (#1569 task 2), and nothing here changes a balance or
// un-removes an account — it only stops hiding retained rows because an interval was never written.
// Adding `AND na.removed_reason IS DISTINCT FROM 'user_disconnect'` would contradict #1569, not
// tighten this. TestUnifiedFeed_AccountWithNoActiveInterval_StillShowsItsTransactions_Integration
// seeds exactly that account and pins both halves.
//
// ⚠ IT WIDENS FIVE READS, NOT ONE, and three of them are not the transactions feed. It is rendered
// inside retainedAccountVisibilityLegs (see above), whose call sites are: this file's unified feed
// (banking and investment arms), investmentTransactionRepository.GetByUserIDKeyset — the
// investments page's own list — investmentTransactionRepository.GetByUserID, which has no
// production caller today and is documented as a latent trap, and
// investmentAnalyticsRepository.IncomeSummary. That last one is not a list: it is a
// `COALESCE(-SUM(it.amount),0) ... GROUP BY income_type` aggregate whose result is dividend and
// interest income REPORTED TO THE USER, so this leg changes a money number as well as a row set.
//
// That is in scope on purpose. IncomeSummary already carries these legs precisely so the income
// total and the investments feed cannot disagree about the same rows (#1569 T12 — before it did,
// TotalIncome went 120.00 -> 0.00 on a disconnection with nothing deleted). Widening the feed but
// not the total would recreate exactly that divergence, one artifact narrower. The money change is
// pinned in its own right by
// TestIncomeSummary_AccountWithNoActiveInterval_StillCountsItsIncome_Integration, which also holds
// the narrow half: an account whose recorded 'active' interval closed before the row's date stays
// excluded.
//
// ⚠ NOT EXISTS, not a LEFT JOIN: one account has many history rows, so joining would multiply
// every feed row by that count. A semi-join asks the only question that matters and cannot fan out.
// It is also why the widening cannot double-count inside the aggregate: the semi-join is over
// accounts.id, the primary key.
func accountWithNoActiveIntervalLeg(alias string) string {
	return fmt.Sprintf(`
		      OR EXISTS (
		          SELECT 1
		          FROM accounts na
		          WHERE na.id = %s.account_id
		            AND NOT EXISTS (
		                SELECT 1 FROM account_status_history h2
		                WHERE h2.account_id = na.id AND h2.status = 'active'
		            )
		      )`, alias)
}

// investmentSyncExclusionCondition renders the predicate that hides an investment_transactions row
// belonging to a chain whose owner set user_accounts.exclude_investment_sync (#1609 Item 5,
// migration 000187).
//
// WHY THIS EXISTS. Plaid reports a brokerage cash-management account through BOTH
// /transactions/sync and /investments/transactions/get, so one real movement lands once in
// `transactions` and once in `investment_transactions` and the unified feed shows it twice. The
// flag is the user's per-chain opt-out, and this predicate is why rows stored BEFORE it was set
// stop appearing in the feed: deleting or tombstoning them would be destruction as a side effect of
// a preference (docs/decisions/2026-08-24-financial-history-retained-by-default.md), so they are
// retained and filtered here. TestExcludeInvestmentSync_IsReversible pins that those rows are
// untouched and reappear when the flag is cleared.
//
// ⚠ CORRECTED (#1631 M4). This comment used to list "dropping them at ingestion" alongside
// deleting and tombstoning as equally forbidden. That is no longer the tree's position: the sync
// now DOES decline to persist new investment_transactions for an excluded chain, which is why this
// read filter matters only for rows written before the flag was set. The ADR was amended to
// authorise it — never ingesting a row is not destroying a retained one, and the row is a duplicate
// of one /transactions/sync stores anyway — under three narrow scope tests set out in that
// document's § "AMENDMENT (#1631, 2026-09-10): a fourth category — never ingesting a row". Deleting and
// tombstoning remain forbidden, and this predicate is NOT redundant: it is the only thing that
// keeps the already-stored duplicates out of the feed.
//
// ⚠ NOT EXISTS, NOT A JOIN. One user_account has MANY accounts rows — that is what a re-link chain
// IS (000033) — so joining user_accounts through accounts would multiply every feed row by the
// number of chain members. A semi-join asks the only question that matters ("is this row's chain
// flagged?") and cannot fan out. TestExcludeInvestmentSync_MultipleAccountsOnOneChainDoNotFanOut
// is the guard.
//
// ⚠ NULL-TOLERANT, AND THAT IS THE LOAD-BEARING PART. investment_transactions.user_account_id has
// been nullable since 000071 and a great many legacy rows carry NULL. An inner
// `JOIN user_accounts ... WHERE NOT exclude_investment_sync` would drop EVERY one of them from
// both read surfaces — invisible data loss, and a far worse defect than the double-count it was
// written to fix. Here a NULL-attribution row is attributed the way every other chain-expanding
// read in this package already attributes one: through accounts.user_account_id for its
// account_id (see investmentTransactionRepository.GetByAccountID and the unified feed's own
// account filter). If that yields no chain, or yields an unflagged one, the row stays VISIBLE.
// TestExcludeInvestmentSync_LegacyNullUserAccountIDStaysVisible and
// TestExcludeInvestmentSync_LegacyNullRowOnTheFlaggedAccountIsStillExcluded pin both directions —
// the second matters because without the accounts fallback the reported defect would survive this
// fix for exactly the accounts whose rows predate the 000071 backfill.
//
// No user_id predicate is needed inside: ownership is transitive through the user_accounts FK
// (000033) and every caller's outer query is already gated on user_id, exactly as
// activeChainSiblingLeg documents. TestExcludeInvestmentSync_IsScopedToItsOwnChain holds that to
// account.
func investmentSyncExclusionCondition(alias string) string {
	return fmt.Sprintf(`
		  AND NOT EXISTS (
		      SELECT 1
		      FROM user_accounts excl_ua
		      WHERE excl_ua.exclude_investment_sync
		        AND (
		            excl_ua.id = %s.user_account_id
		            OR (
		                %s.user_account_id IS NULL
		                AND EXISTS (
		                    SELECT 1 FROM accounts excl_a
		                    WHERE excl_a.id = %s.account_id
		                      AND excl_a.user_account_id = excl_ua.id
		                )
		            )
		        )
		  )`, alias, alias, alias)
}

// activeChainSiblingLeg renders the RE-LINK half: a row whose own account is no longer active is
// still visible while an ACTIVE account shares its user_account_id — the stable chain key set at
// insert time (Issue #1109). EXISTS rather than a JOIN so that several accounts sharing one chain
// key cannot duplicate the row (Issue #1304).
//
// chain_a.user_id is intentionally omitted: user-ownership is transitive via the user_accounts FK
// (migration 000033), and every caller's outer query is already gated on user_id.
func activeChainSiblingLeg(alias string) string {
	return fmt.Sprintf(`
		      OR (
		          %s.user_account_id IS NOT NULL
		          AND EXISTS (
		              SELECT 1 FROM accounts chain_a
		              WHERE chain_a.user_account_id = %s.user_account_id
		                AND chain_a.status = 'active'
		          )
		      )`, alias, alias)
}

// retainedAccountNoLiveSuccessorLeg renders the HISTORICAL half of the unified feed's account
// visibility predicate for a transaction table aliased `t` with a date column `dateCol`.
//
// WHY IT EXISTS (#1569). Task 2 stopped hard-deleting disconnected accounts; they are soft-removed
// and retained. The pre-existing predicate showed a transaction only if its account was 'active',
// or an 'active' account shared its user_account_id — so a fully disconnected chain now keeps its
// transactions on disk and loses them from the UI. Retained-but-invisible fails #1569's
// requirement exactly as completely as deleting them would have.
//
// WHY THE ACTIVE LEG STAYS. This is ADDED BESIDE the active-chain branch, never in place of it.
// #1561 removed status predicates wholesale in the household analytics/history joins and had to be
// reverted; balance_snapshot_repository.go narrates both failure modes at length.
//
// WHY IT IS SCOPED TO "NO LIVE SUCCESSOR" rather than to a status list. The leg fires only when
// the account's chain has no 'active' member left — which is precisely the case the active-chain
// leg above cannot cover, and precisely what a disconnection leaves behind.
//
// An earlier revision scoped it to `disc_a.status IN ('removed','expired')` instead, on the theory
// that a 'replaced' account is always covered by its surviving active successor. That theory holds
// only while a successor survives, which is by definition NOT true in this issue's scenario: link
// bank A, re-link (A -> 'replaced', B -> 'active' on the same user_account_id), then disconnect
// entirely (B -> 'removed'). B's rows come back through this leg; A's pre-relink rows do not — the
// active leg has nothing active to find and a status list excludes 'replaced'. Every user who ever
// re-linked would lose the older half of their history: the same failure #1569 was filed for,
// displaced one chain hop back.
//
// The NOT EXISTS keys on disc_a.user_account_id, so it also preserves what #1109/#1304 decided
// about a live re-link chain: a pre-relink row whose own user_account_id was never backfilled stays
// hidden while an active sibling exists, because that sibling satisfies the NOT EXISTS and closes
// the leg. TestTransactions_UnifiedFeed_BankingSurvivesRelink_Integration pins exactly that, and it
// still passes. NULL user_account_id on disc_a itself never matches a sibling (NULL = NULL is
// UNKNOWN), so a chainless disconnected account has no live successor and its rows are shown —
// which is the answer we want for an account that never had a chain key at all.
//
// The status of disc_a is not consulted at all any more; liveness comes from the recorded
// intervals and successorship from the chain key.
//
// TWO KNOWN EDGES, stated rather than discovered later:
//
//   - MULTI-INTERVAL ACCOUNTS. An account can have several 'active' intervals with gaps between
//     them (deactivate, reactivate). EXISTS is satisfied by ANY of them, so a transaction dated
//     inside a GAP is admitted by a LATER interval's valid_to. That is deliberate: this leg decides
//     VISIBILITY of a retained row, not point-in-time liveness, and hiding a user's own transaction
//     because their bank was briefly disconnected around that date would be the #1569 bug again in
//     miniature. Contrast AccountFilterOptions.AsOf, which is a genuine point-in-time question and
//     therefore bounds both ends of a single interval.
//   - ACCOUNTS WITH NO 'active' INTERVAL AT ALL. Migration 000152's zero-length-interval guard
//     skips the 'active' prefix when replaced_at/removed_at/updated_at is NULL or <= created_at,
//     so such an account has only a terminal row. It satisfies neither this leg nor any as-of
//     universe, and its transactions stay hidden. That is a backfill-data property, not a decision
//     taken here; it is called out so the next reader does not conclude the predicate is wrong.
//
// WHY THERE IS NO LOWER BOUND ON THE INTERVAL. The obvious form of this predicate also asserts
// `h.valid_from <= dateCol` — "the account was live on the transaction's own date". That is wrong
// here, and wrong in the direction that destroys the most data. valid_from for an account's first
// 'active' interval is accounts.created_at (migration 000152's backfill; the INSERT trigger's
// clock_timestamp() for new rows), while a Plaid initial sync pulls up to 24 MONTHS of
// transactions dated long before that row existed. A lower bound would hide essentially all of a
// disconnected account's backfilled history — the exact history this issue exists to keep — and it
// would do so silently, only after the disconnection, when the active leg stopped covering for it.
// Pinned by TestUnifiedFeed_DisconnectedChain_KeepsTransactionsDatedBeforeAccountCreation.
//
// So only the UPPER bound is applied: the account must not already have STOPPED being live before
// the transaction's date. That still excludes a row dated after the account's last active
// interval closed, which is the only case this leg has any business hiding.
//
// The comparison is day-granular and explicitly UTC ((valid_to AT TIME ZONE 'UTC')::date), matching
// how every other date/timestamptz comparison in this package is written, so the result does not
// depend on the session TimeZone. A transaction dated exactly on the day the account was removed
// stays visible; that is deliberate, since the removal happened partway through that day.
//
// TENANCY: this leg adds NO user predicate of its own — it keys on t.account_id, and ownership is
// carried entirely by the outer query's `user_id = $1::uuid`, exactly as the chain leg above does.
// TestUnifiedFeed_DisconnectedChain_DoesNotLeakAcrossUsers is what holds that claim to account.
func retainedAccountNoLiveSuccessorLeg(alias, dateCol string) string {
	return fmt.Sprintf(`
		      OR EXISTS (
		          SELECT 1
		          FROM accounts disc_a
		          JOIN account_status_history h
		            ON h.account_id = disc_a.id AND h.status = 'active'
		          WHERE disc_a.id = %s.account_id
		            AND NOT EXISTS (
		                SELECT 1 FROM accounts s
		                WHERE s.user_account_id = disc_a.user_account_id
		                  AND s.status = 'active'
		            )
		            AND (h.valid_to IS NULL OR (h.valid_to AT TIME ZONE 'UTC')::date >= %s)
		      )`, alias, dateCol)
}

func (r *transactionRepository) GetByUserIDKeysetUnified(
	ctx context.Context,
	userID string,
	filters domain.TransactionFilters,
	cursor *domain.TransactionCursor,
	limit int,
) ([]*domain.ActivityItem, *domain.TransactionCursor, *domain.FeedTotals, error) {
	args := []any{userID}
	argIdx := 2

	// ── Shared filters (both branches use the same $N) ────────────────────────

	// Account filter (chain-expanded via user_account_id).
	// AccountIDs (multi-value) is not supported in the unified path; use AccountID.
	bankingAcctFilter := ""
	investAcctFilter := ""
	if filters.AccountID != nil {
		bankingAcctFilter = fmt.Sprintf(`
			AND (
				(t.user_account_id = (SELECT user_account_id FROM accounts WHERE id = $%d AND user_account_id IS NOT NULL))
				OR (t.account_id = $%d AND t.user_account_id IS NULL)
			)`, argIdx, argIdx+1)
		investAcctFilter = fmt.Sprintf(`
			AND (
				(it.user_account_id = (SELECT user_account_id FROM accounts WHERE id = $%d AND user_account_id IS NOT NULL))
				OR (it.account_id = $%d AND it.user_account_id IS NULL)
			)`, argIdx, argIdx+1)
		args = append(args, *filters.AccountID, *filters.AccountID)
		argIdx += 2
	}

	// Date range — applies to both branches (same $N).
	bankingDateFilter := ""
	investDateFilter := ""
	if filters.StartDate != nil {
		bankingDateFilter += fmt.Sprintf(" AND t.date >= $%d", argIdx)
		investDateFilter += fmt.Sprintf(" AND it.date >= $%d", argIdx)
		args = append(args, *filters.StartDate)
		argIdx++
	}
	if filters.EndDate != nil {
		bankingDateFilter += fmt.Sprintf(" AND t.date <= $%d", argIdx)
		investDateFilter += fmt.Sprintf(" AND it.date <= $%d", argIdx)
		args = append(args, *filters.EndDate)
		argIdx++
	}

	// Search — same pattern $N for both branches.
	// Banking: name OR merchant_name OR effective merchant name (via merchant JOINs);
	// Investment: name OR ticker_symbol OR security_name.
	bankingSearchFilter := ""
	investSearchFilter := ""
	bankingMerchantJoins := "" // conditionally populated when search active
	if filters.Search != nil {
		pattern := "%" + *filters.Search + "%"
		bankingMerchantJoins = merchantSortJoins
		bankingSearchFilter = fmt.Sprintf(
			" AND (t.name ILIKE $%d OR t.merchant_name ILIKE $%d OR "+effectiveMerchantNameExpr+" ILIKE $%d)",
			argIdx, argIdx, argIdx)
		investSearchFilter = fmt.Sprintf(
			" AND (it.name ILIKE $%d OR sec.ticker_symbol ILIKE $%d OR sec.name ILIKE $%d)",
			argIdx, argIdx, argIdx)
		args = append(args, pattern)
		argIdx++
	}

	// ── Banking-only filters ──────────────────────────────────────────────────
	// These filters apply to the banking branch only.  When any of them is set we
	// also append AND FALSE to the investment branch so that investment rows are
	// excluded rather than silently over-returned.

	bankingExtraFilter := ""
	investBankingOnlyFilter := "" // zeroes out investment rows when a banking-only filter is active
	hasBankingOnlyFilter := false

	if filters.Pending != nil {
		bankingExtraFilter += fmt.Sprintf(" AND t.pending = $%d", argIdx)
		args = append(args, *filters.Pending)
		argIdx++
		hasBankingOnlyFilter = true
	}
	if filters.Reviewed != nil {
		bankingExtraFilter += fmt.Sprintf(" AND t.reviewed = $%d", argIdx)
		args = append(args, *filters.Reviewed)
		argIdx++
		hasBankingOnlyFilter = true
	}
	if filters.AmountType != nil {
		switch *filters.AmountType {
		case "income":
			bankingExtraFilter += " AND t.amount < 0"
		case "expense":
			bankingExtraFilter += " AND t.amount > 0"
		}
		hasBankingOnlyFilter = true
	}
	if filters.MinAmount != nil {
		bankingExtraFilter += fmt.Sprintf(" AND ABS(t.amount) >= $%d", argIdx)
		args = append(args, *filters.MinAmount)
		argIdx++
		hasBankingOnlyFilter = true
	}
	if filters.MaxAmount != nil {
		bankingExtraFilter += fmt.Sprintf(" AND ABS(t.amount) <= $%d", argIdx)
		args = append(args, *filters.MaxAmount)
		argIdx++
		hasBankingOnlyFilter = true
	}
	if hasBankingOnlyFilter {
		investBankingOnlyFilter = " AND FALSE"
	}

	// PFC / custom-category filters (banking only) — ONE OR-joined group (#1626). Investment rows
	// carry no category at all, so any category filter zeroes that branch.
	if catClause, catArgs, nextIdx := categoryFilterGroup(filters, argIdx, args); catClause != "" {
		bankingExtraFilter += catClause
		args = catArgs
		argIdx = nextIdx
		investBankingOnlyFilter = " AND FALSE"
	}

	// AccountIDs (multi-value) — chain-expanded via user_account_id on both branches.
	if len(filters.AccountIDs) > 0 {
		bPHs := make([]string, len(filters.AccountIDs))
		iPHs := make([]string, len(filters.AccountIDs))
		bFbPHs := make([]string, len(filters.AccountIDs))
		iFbPHs := make([]string, len(filters.AccountIDs))
		for i, id := range filters.AccountIDs {
			bPHs[i] = fmt.Sprintf("$%d", argIdx)
			bFbPHs[i] = fmt.Sprintf("$%d", argIdx+1)
			iPHs[i] = fmt.Sprintf("$%d", argIdx+2)
			iFbPHs[i] = fmt.Sprintf("$%d", argIdx+3)
			args = append(args, id, id, id, id)
			argIdx += 4
		}
		bankingExtraFilter += fmt.Sprintf(
			" AND (t.user_account_id IN (SELECT user_account_id FROM accounts WHERE id IN (%s) AND user_account_id IS NOT NULL)"+
				" OR (t.account_id IN (%s) AND t.user_account_id IS NULL))",
			strings.Join(bPHs, ", "), strings.Join(bFbPHs, ", "),
		)
		investAcctFilter += fmt.Sprintf(
			" AND (it.user_account_id IN (SELECT user_account_id FROM accounts WHERE id IN (%s) AND user_account_id IS NOT NULL)"+
				" OR (it.account_id IN (%s) AND it.user_account_id IS NULL))",
			strings.Join(iPHs, ", "), strings.Join(iFbPHs, ", "),
		)
	}

	// UserAccountID — stable logical account identity across re-links (both branches).
	if filters.UserAccountID != nil {
		bankingExtraFilter += fmt.Sprintf(" AND t.user_account_id = $%d", argIdx)
		investAcctFilter += fmt.Sprintf(" AND it.user_account_id = $%d", argIdx)
		args = append(args, *filters.UserAccountID)
		argIdx++
	}

	// MerchantID — banking rows only; investment rows have no merchant → zero them out.
	if filters.MerchantID != nil {
		bankingExtraFilter += fmt.Sprintf(" AND t.merchant_id = $%d", argIdx)
		args = append(args, *filters.MerchantID)
		argIdx++
		investBankingOnlyFilter = " AND FALSE"
	}

	// CategorySources — banking rows only; investment rows have no category_source → zero them out.
	if len(filters.CategorySources) > 0 {
		phs := make([]string, len(filters.CategorySources))
		for i, s := range filters.CategorySources {
			phs[i] = fmt.Sprintf("$%d", argIdx)
			args = append(args, s)
			argIdx++
		}
		bankingExtraFilter += " AND t.category_source IN (" + strings.Join(phs, ", ") + ")"
		investBankingOnlyFilter = " AND FALSE"
	}

	// ── Investment-only filters ───────────────────────────────────────────────

	// bankingActivityFilter zeroes the banking branch when an activity_types filter is active:
	// banking rows carry no activity_type and must not bleed into an investment-activity-scoped result.
	// When TradeTypeToSQLPredicate returns an empty clause (all types unrecognized), both branches
	// return zero rows — consistent empty result rather than asymmetric (all invest, no banking).
	var bankingActivityFilter, investActivityFilter string
	if len(filters.ActivityTypes) > 0 {
		clause, typeArgs := domain.TradeTypeToSQLPredicate(filters.ActivityTypes, "it", "sec")
		if clause != "" {
			renumbered := renumberPlaceholders(clause, argIdx)
			investActivityFilter = " AND (" + renumbered + ")"
			args = append(args, typeArgs...)
			argIdx += len(typeArgs)
		} else {
			// Unrecognized activity types → zero both branches (consistent empty result).
			investActivityFilter = " AND FALSE"
		}
		bankingActivityFilter = " AND FALSE"
	}

	// ── Keyset predicate: 3-part (sort_date DESC, source ASC, id ASC) ─────────
	// Banking source = 'banking', Investment source = 'investment'.
	// 'banking' < 'investment' lexicographically, so for same sort_date:
	//   banking rows come first (source ASC), then investment rows.
	//
	// Predicate for next page after cursor (D, S, I):
	//   sort_date < D
	//   OR (sort_date = D AND source > S)
	//   OR (sort_date = D AND source = S AND id > I)
	//
	// Simplified per branch (source is a constant):
	//
	// Banking (source = 'banking'):
	//   cursor.Src = "banking":  sort_date < D OR (sort_date = D AND id > I)
	//   cursor.Src = "investment": sort_date < D
	//
	// Investment (source = 'investment'):
	//   cursor.Src = "banking":  sort_date <= D   (because 'investment' > 'banking')
	//   cursor.Src = "investment": sort_date < D OR (sort_date = D AND id > I)

	bankingCursorFilter := ""
	investCursorFilter := ""
	if cursor != nil {
		// cursor.SVT is the sort_date of the last-seen item.
		// SVT is always populated for K="date" cursors by DecodeTransactionCursor;
		// guard here defensively to avoid a nil-pointer on tampered/malformed cursors.
		if cursor.SVT == nil {
			return nil, nil, nil, fmt.Errorf("cursor: missing sort date — restart pagination")
		}
		// We add: cursorDate, cursorDate, cursorID to args (3 slots).
		// Both branches can reference argIdx..(argIdx+2) as needed.
		args = append(args, *cursor.SVT, *cursor.SVT, cursor.ID.String())
		dPH := argIdx
		idPH := argIdx + 2

		switch cursor.Src {
		case domain.FeedSourceInvestment:
			// Banking: sort_date < D
			bankingCursorFilter = fmt.Sprintf(
				" AND COALESCE(t.authorized_date, t.date)::date < $%d::date", dPH)
			// Investment: sort_date < D OR (sort_date = D AND id > I)
			investCursorFilter = fmt.Sprintf(
				" AND (it.date::date < $%d::date OR (it.date::date = $%d::date AND it.id::text > $%d))",
				dPH, dPH+1, idPH)
		default: // "banking" or "" (first-page with Src unset falls through to no predicate)
			if cursor.Src != "" {
				// Banking: sort_date < D OR (sort_date = D AND id > I)
				bankingCursorFilter = fmt.Sprintf(
					" AND (COALESCE(t.authorized_date, t.date)::date < $%d::date"+
						" OR (COALESCE(t.authorized_date, t.date)::date = $%d::date AND t.id::text > $%d))",
					dPH, dPH+1, idPH)
				// Investment: sort_date <= D (all investment rows on same date come after banking)
				investCursorFilter = fmt.Sprintf(
					" AND it.date::date <= $%d::date", dPH)
			}
		}
		argIdx += 3
	}

	// LIMIT placeholder.
	args = append(args, limit+1)
	limitPH := argIdx

	// ── Build UNION ALL query ─────────────────────────────────────────────────
	//
	// ⚠ unionBody CARRIES THE PREDICATES AND NOTHING ELSE — no ORDER BY, no LIMIT (#1626 Phase 2,
	// review finding X3). Both statements this method issues are derived from it: the page appends
	// the presentation ordering and the limit, the whole-set aggregate wraps it as-is. That keeps
	// the drift-safety that mattered (there is exactly ONE copy of the predicate, so the total
	// cannot describe a different row set than the rows) while stopping the page's ordering from
	// leaking into a COUNT and a SUM, which are order-independent. It used to leak: the aggregate
	// wrapped the ORDERED page query, so an unfiltered first page sorted every banking AND
	// investment row merely to count them, and then the page query sorted again.
	unionBody := fmt.Sprintf(`
		SELECT
			t.id::text,
			t.user_id::text,
			t.account_id::text,
			COALESCE(t.authorized_date, t.date)::date   AS sort_date,
			'banking'::text                              AS source_disc,
			t.date,
			t.amount,
			t.name,
			t.user_account_id::text,
			t.linked_item_id::text,
			t.authorized_date,
			t.merchant_name,
			t.merchant_id::text,
			t.source                                     AS data_source,
			t.external_transaction_id,
			t.pfc_primary,
			t.pfc_detailed,
			t.pfc_confidence_level,
			t.pfc_category_icon_url,
			t.pfc_primary_override,
			t.pfc_detailed_override,
			t.custom_category_override_id::text,
			t.payment_channel,
			t.pending,
			t.user_notes,
			t.category_override_kind,
			t.effective_pfc_primary,
			t.effective_pfc_detailed,
			t.effective_category_name,
			t.reviewed,
			t.reviewed_at,
			t.tags,
			t.deleted_at,
			t.deleted_reason,
			NULL::text,
			NULL::text,
			NULL::text,
			NULL::text,
			NULL::text,
			NULL::float8,
			NULL::float8,
			NULL::float8,
			NULL::bool,          -- inv_security_attached (#1631)
			NULL::bool           -- inv_security_is_cash_equivalent (#1631)
		FROM transactions t
		%s
		WHERE t.user_id = $1::uuid
		  -- tombstone-exempt(transactions t): transactionTombstoneCondition("t", filters.IncludeReversed) is
		  -- interpolated at the %%s immediately below. It is placed HERE, ahead of the account
		  -- visibility disjunction, because retainedAccountVisibilityLeg deliberately WIDENS which
		  -- accounts' rows are visible (a fully disconnected chain keeps its history, #1569 task 2)
		  -- — so without this gate the one query that most needs to hide tombstones is the one most
		  -- likely to surface them.
		  AND %s
		  -- The chain-expansion and retained legs are rendered together by
		  -- retainedAccountVisibilityLegs — they are complements and a read that carries only one
		  -- covers only half the retained universe (#1569 task 21 / C4).
		  AND (
		      EXISTS (SELECT 1 FROM accounts WHERE id = t.account_id AND status = 'active') -- NULL account_id always false here; falls through to chain leg below
%s
		      -- Truly orphaned rows: migration 000035's ON DELETE SET NULL left transactions with
		      -- no account_id behind every pre-Task-2 hard delete. They match neither leg above
		      -- (NULL never satisfies "id = t.account_id", and there is no accounts row left to
		      -- have history), so without this disjunct they are retained and permanently
		      -- invisible. They are still the user's own rows — the outer user_id predicate is
		      -- what makes that safe. investment_transactions.account_id is NOT NULL, so its
		      -- branch below has no equivalent and needs none.
		      OR t.account_id IS NULL
		  )
		%s%s%s%s%s%s

		UNION ALL

		SELECT
			it.id::text,
			it.user_id::text,
			it.account_id::text,
			it.date::date                                AS sort_date,
			'investment'::text                           AS source_disc,
			it.date,
			it.amount,
			it.name,
			it.user_account_id::text,
			NULL::text,
			NULL::date,
			NULL::text,
			NULL::text,
			NULL::text,
			NULL::text,
			NULL::text,
			NULL::text,
			NULL::text,
			NULL::text,
			NULL::text,   -- pfc_primary_override
			NULL::text,   -- pfc_detailed_override
			NULL::text,   -- custom_category_override_id
			NULL::text,
			NULL::bool,
			NULL::text,
			NULL::text,
			NULL::text,
			NULL::text,
			NULL::text,
			NULL::bool,
			NULL::timestamptz,
			NULL::jsonb,
			NULL::timestamptz,   -- deleted_at: investment_transactions has no tombstone (#1569 task 10)
			NULL::text,          -- deleted_reason
			it.security_id::text,
			sec.ticker_symbol,
			sec.name,
			it.type,
			it.subtype,
			it.quantity,
			it.price,
			it.fees,
			-- #1631: the two facts NormalizeTradeType needs about the joined security. Two
			-- columns, not one nullable bool: "no security" and "a security whose
			-- is_cash_equivalent is NULL" are different facts and must not arrive as one nil.
			-- The flag is passed through RAW, not COALESCEd: an absent flag must not be read as
			-- "not cash-equivalent", which would reclassify a row on a fact we do not have.
			(sec.id IS NOT NULL)                        AS inv_security_attached,
			sec.is_cash_equivalent                      AS inv_security_is_cash_equivalent
		FROM investment_transactions it
		LEFT JOIN securities sec ON sec.id = it.security_id
		WHERE it.user_id = $1::uuid
		  -- Both legs together, exactly as the banking branch above — see
		  -- retainedAccountVisibilityLegs. Parity is intentional; diverging here would
		  -- reintroduce asymmetry.
		  AND (
		      EXISTS (SELECT 1 FROM accounts WHERE id = it.account_id AND status = 'active')
%s
		  )
		  -- The per-chain investment opt-out (#1609 Item 5). Deliberately NOT mirrored into the
		  -- banking branch above: the flag exists to suppress the DUPLICATE that Plaid reports
		  -- through /investments/transactions/get, and the banking row is the real movement the
		  -- user still needs to see.
%s
		%s%s%s%s%s%s
	`,
		bankingMerchantJoins,
		transactionTombstoneCondition("t", filters.IncludeReversed),
		retainedAccountVisibilityLegs("t", "t.date"),
		bankingAcctFilter, bankingDateFilter, bankingSearchFilter, bankingExtraFilter, bankingCursorFilter, bankingActivityFilter,
		retainedAccountVisibilityLegs("it", "it.date"),
		investmentSyncExclusionCondition("it"),
		investAcctFilter, investDateFilter, investSearchFilter, investActivityFilter, investCursorFilter, investBankingOnlyFilter,
	)

	// The presentation ordering and the LIMIT are APPENDED rather than interpolated, so the
	// whole-set aggregate below can wrap unionBody without either of them (#1626 Phase 2). They are
	// the page's only two differences from the set, and both belong to the page alone: a list has an
	// order the user sees, a COUNT and a SUM do not.
	orderClause := "\n\t\tORDER BY sort_date DESC, source_disc ASC, id ASC"
	limitClause := fmt.Sprintf("\n\t\tLIMIT $%d", limitPH)
	query := unionBody + orderClause

	// ── Whole-set totals, FIRST PAGE ONLY (#1626 Phase 2) ────────────────────
	//
	// WHY. The response carried only a page, so "what does this filtered set come to" was not a
	// question the product could answer — which is what made the reports Sankey's click-through
	// claim unverifiable rather than merely untested.
	//
	// ⚠ STATED PRECISELY, because the loose form ("this node's amount equals the filtered list's
	// total") is FALSE for income and shipped in five comments before this branch was reviewed. A
	// hierarchy node's amount is COALESCE(SUM(ABS(t.amount)),0) — an unsigned chart quantity — while
	// this total is SUM(t.amount) under PFCv2 signs, where inflows are NEGATIVE. So the property that
	// holds over the same filtered set is |node amount| == |total_amount|; for an expense node the
	// two numbers also happen to be equal, and for an income node they differ by a factor of −1.
	// Both SQL expressions are right for their own consumer: a chart needs magnitudes, a ledger
	// total needs signs. Do not "fix" either one to make the sentence simpler.
	//
	// ⚠ ONLY when no cursor was supplied. Re-aggregating the whole filtered set on every page of
	// the busiest list in the app would be real cost for a number that does not change as the user
	// pages. TestBankingKeysetCursoredPage_SkipsTheAggregateEntirely pins the skip.
	//
	// ⚠ IT WRAPS THE SHARED unionBody rather than restating its predicates, and that is deliberate.
	// The first attempt here hand-copied the two UNION arms' WHERE clauses; it was syntactically
	// invalid on the first real-Postgres run, and had it merely been subtly wrong instead it would
	// have shipped a total that quietly disagreed with the rows it described — worse than no total,
	// because the client cannot tell that it is wrong. Deriving both statements from one string
	// makes drift impossible by construction: there is only one copy of the predicate.
	//
	// ⚠ unionBody, NOT the page query, and that distinction is #1626 Phase 2's review finding X3.
	// This used to wrap `query`, which by then already carried the presentation ORDER BY — so an
	// unfiltered first page sorted every banking AND investment row merely to COUNT and SUM them,
	// and the page query then sorted again. COUNT and SUM are order-independent; the ordering is
	// the page's alone. The drift-safety is unchanged, because the ordering was never part of the
	// predicate. TestUnifiedFeed_EmittedQuery_EXPLAIN asserts BOTH halves — no ORDER BY in the
	// aggregate's text and no Sort in its plan, while the page's plan still has one.
	//
	// `amount` is the second UNION arm's own column too, and a UNION takes its output names from
	// the first arm, so the sum is over both banking and investment rows with their own signs.
	//
	// ⚠ And the LIMIT's ARGUMENT has to go with it. An earlier version of this comment claimed
	// Postgres does not mind an unused trailing parameter; it does — the extended protocol rejects
	// the bind outright ("expected 1 arguments, got 2"). args[:limitPH-1] drops exactly that one,
	// which is sound only because the limit is appended last and $limitPH appears nowhere else.
	var totals *domain.FeedTotals
	if cursor == nil {
		aggQuery := fmt.Sprintf(
			`SELECT COUNT(*), COALESCE(SUM(amount), 0) FROM (%s) agg`, unionBody)

		var count int
		var amount float64
		if err := r.db.QueryRow(ctx, aggQuery, args[:limitPH-1]...).Scan(&count, &amount); err != nil {
			return nil, nil, nil, fmt.Errorf("GetByUserIDKeysetUnified totals: %w", err)
		}
		totals = &domain.FeedTotals{Count: count, Amount: amount}
	}

	rows, err := r.db.Query(ctx, query+limitClause, args...)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("GetByUserIDKeysetUnified: %w", err)
	}
	defer rows.Close()

	var items []*domain.ActivityItem
	for rows.Next() {
		var (
			id, userIDCol, sourceDisc, name           string
			accountID                                 *string
			sortDate, date                            time.Time
			amount                                    float64
			userAccountID, linkedItemID               *string
			authorizedDate                            *time.Time
			merchantName, merchantID, dataSource      *string
			externalTxnID                             *string
			pfcPrimary, pfcDetailed                   *string
			pfcConfidenceLevel, pfcCategoryIconURL    *string
			pfcPrimaryOverride, pfcDetailedOverride   *string
			customCategoryOverrideID                  *string
			paymentChannel                            *string
			pending                                   *bool
			userNotes, categoryOverrideKind           *string
			effectivePFCPrimary, effectivePFCDetailed *string
			effectiveCategoryName                     *string
			reviewed                                  *bool
			deletedAt                                 *time.Time
			deletedReason                             *string
			reviewedAt                                *time.Time
			tagsJSON                                  []byte
			securityID, tickerSymbol, securityName    *string
			invType, invSubtype                       *string
			invQuantity, invPrice, invFees            *float64
			invSecAttached, invSecIsCashEquivalent    *bool
		)

		if err := rows.Scan(
			&id, &userIDCol, &accountID,
			&sortDate, &sourceDisc,
			&date, &amount, &name,
			&userAccountID, &linkedItemID, &authorizedDate,
			&merchantName, &merchantID, &dataSource,
			&externalTxnID,
			&pfcPrimary, &pfcDetailed, &pfcConfidenceLevel, &pfcCategoryIconURL,
			&pfcPrimaryOverride, &pfcDetailedOverride, &customCategoryOverrideID,
			&paymentChannel, &pending, &userNotes,
			&categoryOverrideKind,
			&effectivePFCPrimary, &effectivePFCDetailed, &effectiveCategoryName,
			&reviewed, &reviewedAt,
			&tagsJSON,
			&deletedAt, &deletedReason,
			&securityID, &tickerSymbol, &securityName,
			&invType, &invSubtype,
			&invQuantity, &invPrice, &invFees,
			&invSecAttached, &invSecIsCashEquivalent,
		); err != nil {
			return nil, nil, nil, fmt.Errorf("GetByUserIDKeysetUnified scan: %w", err)
		}

		item := &domain.ActivityItem{
			Source:                   sourceDisc,
			SortDate:                 sortDate,
			ID:                       id,
			UserID:                   userIDCol,
			AccountID:                nilOrEmpty(accountID),
			Date:                     date,
			Amount:                   amount,
			Name:                     name,
			UserAccountID:            userAccountID,
			LinkedItemID:             linkedItemID,
			AuthorizedDate:           authorizedDate,
			MerchantName:             merchantName,
			MerchantID:               merchantID,
			DataSource:               dataSource,
			PFCPrimary:               pfcPrimary,
			PFCDetailed:              pfcDetailed,
			PFCConfidenceLevel:       pfcConfidenceLevel,
			PFCCategoryIconURL:       pfcCategoryIconURL,
			PFCPrimaryOverride:       pfcPrimaryOverride,
			PFCDetailedOverride:      pfcDetailedOverride,
			CustomCategoryOverrideID: customCategoryOverrideID,
			PaymentChannel:           paymentChannel,
			Pending:                  pending,
			UserNotes:                userNotes,
			CategoryOverrideKind:     categoryOverrideKind,
			EffectivePFCPrimary:      effectivePFCPrimary,
			EffectivePFCDetailed:     effectivePFCDetailed,
			EffectiveCategoryName:    effectiveCategoryName,
			Reviewed:                 reviewed,
			ReviewedAt:               reviewedAt,
			SecurityID:               securityID,
			TickerSymbol:             tickerSymbol,
			SecurityName:             securityName,
			InvestmentSubtype:        invSubtype,
			InvestmentQuantity:       invQuantity,
			InvestmentPrice:          invPrice,
			InvestmentFees:           invFees,
		}

		// #1569: a row with deleted_at set reached this feed only because the caller opted in via
		// TransactionFilters.IncludeReversed, and transactionTombstoneCondition admits exactly one
		// reason. Reversed is set on every banking row (false for live ones) so a client can
		// branch on presence rather than absence; investment rows leave it nil, matching how
		// Pending and Reviewed are already handled.
		if sourceDisc == domain.FeedSourceBanking {
			isReversed := deletedAt != nil &&
				deletedReason != nil &&
				domain.TransactionDeletedReason(*deletedReason) == domain.TxnDeletedProviderRemovedPosted
			item.Reversed = &isReversed
			if isReversed {
				item.ReversedAt = deletedAt
			}
		}

		// Normalize ActivityType from the raw Plaid type/subtype plus the two joined-security facts
		// (#1631). The cash-equivalence flag stays a *bool so a NULL reaches the rule as "unknown"
		// rather than as "false" — the SQL filter's `is_cash_equivalent IS FALSE` treats it the same
		// way, and that agreement is what keeps display and filter a partition.
		if invType != nil {
			at := domain.NormalizeTradeType(
				*invType, invSubtype,
				invSecAttached != nil && *invSecAttached,
				invSecIsCashEquivalent,
			)
			item.ActivityType = &at
		}

		// Decrypt external_transaction_id (encrypted in DB for banking rows).
		if externalTxnID != nil {
			decrypted, decErr := r.enc.DecryptNullableString(externalTxnID)
			if decErr != nil {
				return nil, nil, nil, fmt.Errorf("GetByUserIDKeysetUnified decrypt: %w", decErr)
			}
			item.ExternalTransactionID = decrypted
		}

		// Unmarshal tags JSONB (banking rows only; investment rows get NULL::jsonb → nil slice).
		if len(tagsJSON) > 0 {
			var tags []string
			if err := json.Unmarshal(tagsJSON, &tags); err != nil {
				return nil, nil, nil, fmt.Errorf("GetByUserIDKeysetUnified unmarshal tags: %w", err)
			}
			item.Tags = tags
		}

		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, nil, nil, fmt.Errorf("GetByUserIDKeysetUnified rows: %w", rows.Err())
	}

	// Build next-page cursor from the limit+1 probe row.
	if len(items) == limit+1 {
		last := items[limit-1]
		lastID, err := uuid.Parse(last.ID)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("GetByUserIDKeysetUnified: invalid id %q: %w", last.ID, err)
		}
		svt := last.SortDate
		sv := last.SortDate.Format(time.DateOnly)
		nextCursor := &domain.TransactionCursor{
			V:   2,
			K:   "date",
			D:   "desc",
			SV:  &sv,
			SVT: &svt,
			ID:  lastID,
			Src: last.Source,
			Inc: true,
		}
		return items[:limit], nextCursor, totals, nil
	}

	return items, nil, totals, nil
}
