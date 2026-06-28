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
}

var transactionSelectExpr = strings.Join(transactionSelectColumns, ", ")

// TransactionRepository defines the interface for transaction data access.
// Supports transactions from all providers (Plaid, manual, Teller, etc.)
type TransactionRepository interface {
	// Create creates a new transaction in the database
	Create(ctx context.Context, transaction *domain.Transaction) error

	// CreateBatch creates multiple transactions in a single database operation.
	// Returns a map of transaction IDs that were genuinely inserted (as opposed to
	// conflict-preserved by ON CONFLICT DO UPDATE).  Callers use this to gate
	// analytics / match-count side-effects on fresh inserts only (Issue #1213).
	CreateBatch(ctx context.Context, transactions []domain.Transaction) (map[string]bool, error)

	// Update updates an existing transaction
	Update(ctx context.Context, transaction *domain.Transaction) error

	// DeleteByExternalID deletes a transaction by its provider-specific ID
	// Used when providers report transactions as removed
	DeleteByExternalID(ctx context.Context, source domain.TransactionSource, externalID string, userID string) error

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
	GetByUserIDKeyset(
		ctx context.Context,
		userID string,
		filters domain.TransactionFilters,
		cursor *domain.TransactionCursor,
		limit int,
	) ([]domain.Transaction, *domain.TransactionCursor, error)

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
	GetByUserIDKeysetUnified(
		ctx context.Context,
		userID string,
		filters domain.TransactionFilters,
		cursor *domain.TransactionCursor,
		limit int,
	) ([]*domain.ActivityItem, *domain.TransactionCursor, error)

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

// Create creates a new transaction in the database
func (r *transactionRepository) Create(ctx context.Context, transaction *domain.Transaction) error {
	// Generate UUID if not already set
	if transaction.ID == "" {
		transaction.ID = uuid.New().String()
	}

	// Set timestamps
	now := time.Now()
	transaction.CreatedAt = now
	transaction.UpdatedAt = now

	// Encrypt PII fields and compute blind index before writing
	extIDBlindIdx, err := r.encryptTransactionPIIFields(transaction)
	if err != nil {
		return fmt.Errorf("failed to encrypt transaction fields: %w", err)
	}

	query := `
		INSERT INTO transactions
		(id, user_id, account_id, linked_item_id, source, external_transaction_id,
		 external_transaction_id_blind_idx, external_metadata, amount, date, authorized_date,
		 name, merchant_name, pfc_primary, pfc_detailed, pfc_confidence_level, pfc_category_icon_url,
		 payment_channel, pending, user_notes, created_at, updated_at, effective_category_name)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22,
		    CASE WHEN $23 = 'custom' THEN (SELECT name FROM custom_categories WHERE id = $24::uuid AND user_id = $2)
		         WHEN $23 = 'pfc'    THEN $25
		         ELSE $14 END)
	`

	_, err = r.db.Exec(ctx, query,
		transaction.ID,
		transaction.UserID,
		transaction.AccountID,
		transaction.LinkedItemID,
		string(transaction.Source),
		transaction.ExternalTransactionID,
		extIDBlindIdx,
		transaction.ExternalMetadata,
		transaction.Amount,
		transaction.Date,
		transaction.AuthorizedDate,
		transaction.Name,
		transaction.MerchantName,
		transaction.PFCPrimary,
		transaction.PFCDetailed,
		transaction.PFCConfidenceLevel,
		transaction.PFCCategoryIconURL,
		transaction.PaymentChannel,
		transaction.Pending,
		transaction.UserNotes,
		transaction.CreatedAt,
		transaction.UpdatedAt,
		transaction.CategoryOverrideKind,
		transaction.CustomCategoryOverrideID,
		transaction.PFCPrimaryOverride,
	)

	if err != nil {
		// Check for unique constraint violations
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
			if strings.Contains(err.Error(), "unique_source_transaction") {
				return ErrDuplicateTransaction
			}
		}
		return fmt.Errorf("failed to create transaction: %w", err)
	}

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
	const numFields = 32
	valueStrings := make([]string, 0, len(transactions))
	valueArgs := make([]interface{}, 0, len(transactions)*numFields)

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
		)
	}

	// Issue #171: Changed from DO NOTHING to DO UPDATE for re-link handling.
	// On conflict (duplicate transaction by blind index), we:
	// - UPDATE: FKs (account_id, linked_item_id, user_account_id) - changes on re-link
	// - UPDATE: Plaid fields (amount, date, name, merchant_name, pending, etc.) - may change
	// - UPDATE: external_transaction_id_blind_idx in case it was previously NULL
	// - PRESERVE: User enrichments (user_notes, category_override_kind, tags, tags_source)
	//
	// Issue #1213: RETURNING id, (xmax::text::bigint = 0) AS inserted
	// xmax = 0 for a freshly inserted row; non-zero for a conflict-updated row.
	// This lets the caller gate analytics/match-count side-effects on genuine inserts.
	// Note: user_notes, category_override_kind, tags, tags_source are NOT in the SET
	// clause so they will be PRESERVED on conflict.
	query := fmt.Sprintf(`
		INSERT INTO transactions
		(id, user_id, account_id, linked_item_id, user_account_id,
		 source, external_transaction_id, external_transaction_id_blind_idx, external_metadata,
		 amount, date, authorized_date, name, merchant_name, merchant_id,
		 pfc_primary, pfc_detailed, pfc_confidence_level, pfc_category_icon_url, payment_channel, pending, user_notes,
		 created_at, updated_at, effective_category_name,
		 category_override_kind, pfc_primary_override, pfc_detailed_override, custom_category_override_id,
		 category_source, reviewed, reviewed_at)
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
			updated_at = NOW()
		RETURNING id, (xmax::text::bigint = 0) AS inserted
	`, strings.Join(valueStrings, ", "))

	rows, err := r.db.Query(ctx, query, valueArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to batch create transactions: %w", err)
	}
	defer rows.Close()

	insertedIDs := make(map[string]bool)
	for rows.Next() {
		var id string
		var inserted bool
		if err := rows.Scan(&id, &inserted); err != nil {
			return nil, fmt.Errorf("failed to scan batch create result: %w", err)
		}
		if inserted {
			insertedIDs[id] = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate batch create results: %w", err)
	}

	return insertedIDs, nil
}

// Update updates an existing transaction
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
	)

	if err != nil {
		return fmt.Errorf("failed to update transaction: %w", err)
	}

	if ct.RowsAffected() == 0 {
		return ErrTransactionNotFound
	}

	return nil
}

// DeleteByExternalID deletes a transaction by its provider-specific ID
func (r *transactionRepository) DeleteByExternalID(ctx context.Context, source domain.TransactionSource, externalID string, userID string) error {
	query := `
		DELETE FROM transactions
		WHERE source = $1 AND external_transaction_id_blind_idx = $2 AND user_id = $3
	`

	ct, err := r.db.Exec(ctx, query, string(source), r.enc.BlindIndex(externalID), userID)
	if err != nil {
		return fmt.Errorf("failed to delete transaction: %w", err)
	}

	if ct.RowsAffected() == 0 {
		return ErrTransactionNotFound
	}

	return nil
}

// GetByUserID retrieves transactions for a specific user with pagination
func (r *transactionRepository) GetByUserID(ctx context.Context, userID string, limit, offset int) ([]domain.Transaction, error) {
	query := fmt.Sprintf(`
		SELECT %s
		FROM transactions
		WHERE user_id = $1
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
		WHERE (
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

	// Add limit and offset to args
	args = append(args, limit, offset)

	// Build complete query with dynamic WHERE clause
	query := fmt.Sprintf(`
		SELECT %s
		FROM transactions
		%s
		%s
		LIMIT $%d OFFSET $%d
	`, transactionSelectExpr, whereClause, buildOrderBy(filters), len(args)-1, len(args))

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

	// Build COUNT query
	query := fmt.Sprintf(`
		SELECT COUNT(*)
		FROM transactions
		%s
	`, whereClause)

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
	`, transactionSelectExpr)

	var tx domain.Transaction
	var sourceStr string
	var metaStr *string
	var tagsJSON []byte
	var tagsSourceJSON []byte
	var categorySource sql.NullString

	err := r.db.QueryRow(ctx, query, string(source), r.enc.BlindIndex(externalID), userID).Scan(
		&tx.ID,
		&tx.UserID,
		&tx.AccountID,
		&tx.LinkedItemID,
		&sourceStr,
		&tx.ExternalTransactionID,
		&metaStr,
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
		&tagsJSON,
		&tagsSourceJSON,
		&tx.CategoryOverrideKind,
		&tx.PFCPrimaryOverride,
		&tx.PFCDetailedOverride,
		&tx.CustomCategoryOverrideID,
		&categorySource,
		&tx.CategoryOverrideDate,
		&tx.EffectivePFCPrimary,
		&tx.EffectivePFCDetailed,
		&tx.EffectiveCategoryName,
		&tx.Reviewed,
		&tx.ReviewedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTransactionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get transaction by external ID: %w", err)
	}

	tx.Source = domain.TransactionSource(sourceStr)
	tx.ExternalMetadata = metaStr

	// Decrypt PII fields
	if err := r.decryptTransactionPIIFields(&tx); err != nil {
		return nil, fmt.Errorf("failed to decrypt transaction: %w", err)
	}

	// Parse tags from JSONB
	if tagsJSON != nil {
		if err := json.Unmarshal(tagsJSON, &tx.Tags); err != nil {
			return nil, fmt.Errorf("failed to unmarshal tags: %w", err)
		}
	}

	// Parse tags_source from JSONB
	if tagsSourceJSON != nil {
		if err := json.Unmarshal(tagsSourceJSON, &tx.TagsSource); err != nil {
			return nil, fmt.Errorf("failed to unmarshal tags_source: %w", err)
		}
	}

	// Set category source
	if categorySource.Valid {
		tx.CategorySourceField = domain.CategorySource(categorySource.String)
	}

	return &tx, nil
}

// buildWhereClause constructs a dynamic WHERE clause based on filters
// Returns the WHERE clause string and the corresponding query arguments
// sortColumnMap maps sort_by parameter values to their SQL column expressions.
var sortColumnMap = map[string]string{
	"date":     "COALESCE(authorized_date, date)",
	"merchant": "COALESCE(merchant_name, name)",
	"amount":   "amount",
	"category": "COALESCE(effective_category_name, effective_pfc_primary, pfc_primary)",
}

// buildOrderBy constructs a safe ORDER BY clause from TransactionFilters.SortBy and SortDir.
// Defaults to date DESC with an id ASC tiebreaker for stable pagination.
func buildOrderBy(filters domain.TransactionFilters) string {
	dir := "DESC"
	if strings.EqualFold(filters.SortDir, "asc") {
		dir = "ASC"
	}
	col, ok := sortColumnMap[filters.SortBy]
	if !ok {
		col = sortColumnMap["date"] // safe default
	}
	return fmt.Sprintf("ORDER BY %s %s, id ASC", col, dir)
}

func buildWhereClause(userID string, filters domain.TransactionFilters) (string, []interface{}) {
	var conditions []string
	var args []interface{}
	argIndex := 1

	// Always filter by user_id
	conditions = append(conditions, fmt.Sprintf("user_id = $%d", argIndex))
	args = append(args, userID)
	argIndex++

	// Optional filter: account_id (single).
	// Issue #1109: Chain-expand via user_account_id to include pre-relink transactions
	if filters.AccountID != nil {
		subq := fmt.Sprintf(
			"(user_account_id = (SELECT user_account_id FROM accounts WHERE id = $%d AND user_account_id IS NOT NULL)"+
				" OR (account_id = $%d AND user_account_id IS NULL))",
			argIndex, argIndex+1,
		)
		conditions = append(conditions, subq)
		args = append(args, *filters.AccountID, *filters.AccountID)
		argIndex += 2
	}

	// Optional filter: account_ids (multi-value, Issue #529).
	// Issue #1109: Chain-expand via user_account_id subquery to include pre-relink transactions
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
			"(user_account_id IN (SELECT user_account_id FROM accounts WHERE id IN (%s) AND user_account_id IS NOT NULL)"+
				" OR (account_id IN (%s) AND user_account_id IS NULL))",
			inList, fallbackInList,
		)
		conditions = append(conditions, subq)
	}

	// Optional filter: start_date
	if filters.StartDate != nil {
		conditions = append(conditions, fmt.Sprintf("date >= $%d", argIndex))
		args = append(args, *filters.StartDate)
		argIndex++
	}

	// Optional filter: end_date
	if filters.EndDate != nil {
		conditions = append(conditions, fmt.Sprintf("date <= $%d", argIndex))
		args = append(args, *filters.EndDate)
		argIndex++
	}

	// pfc_primaries and pfc_detaileds use OR logic between them
	// (a transaction matches if its primary is in primaries OR its detailed is in detaileds)
	if len(filters.PFCPrimaries) > 0 || len(filters.PFCDetaileds) > 0 {
		var orParts []string
		if len(filters.PFCPrimaries) > 0 {
			placeholders := make([]string, len(filters.PFCPrimaries))
			for i, p := range filters.PFCPrimaries {
				placeholders[i] = fmt.Sprintf("$%d", argIndex)
				args = append(args, p)
				argIndex++
			}
			orParts = append(orParts, "effective_pfc_primary IN ("+strings.Join(placeholders, ", ")+")")
		}
		if len(filters.PFCDetaileds) > 0 {
			placeholders := make([]string, len(filters.PFCDetaileds))
			for i, d := range filters.PFCDetaileds {
				placeholders[i] = fmt.Sprintf("$%d", argIndex)
				args = append(args, d)
				argIndex++
			}
			orParts = append(orParts, "effective_pfc_detailed IN ("+strings.Join(placeholders, ", ")+")")
		}
		conditions = append(conditions, "("+strings.Join(orParts, " OR ")+")")
	}

	// Optional filter: custom_category_ids (multi-value) — matches the effective custom
	// category. Issue #1203: rolls up children — selecting a parent custom category also
	// matches transactions assigned to its children (parent_id). Leaf categories match
	// only themselves, so this is a no-op for non-parent selections.
	if len(filters.CustomCategoryIDs) > 0 {
		placeholders := make([]string, len(filters.CustomCategoryIDs))
		for i, id := range filters.CustomCategoryIDs {
			placeholders[i] = fmt.Sprintf("$%d", argIndex)
			args = append(args, id)
			argIndex++
		}
		idList := strings.Join(placeholders, ", ")
		conditions = append(conditions, fmt.Sprintf(
			"(category_override_kind = 'custom' AND custom_category_override_id IN ("+
				"SELECT id FROM custom_categories WHERE user_id = $1 AND (id::text IN (%s) OR parent_id::text IN (%s))))",
			idList, idList,
		))
	}

	// Optional filter: search (name OR merchant_name)
	if filters.Search != nil {
		searchCondition := fmt.Sprintf("(name ILIKE $%d OR merchant_name ILIKE $%d)", argIndex, argIndex+1)
		conditions = append(conditions, searchCondition)
		searchPattern := "%" + *filters.Search + "%"
		args = append(args, searchPattern, searchPattern) // Add pattern twice for both ILIKE conditions
		argIndex += 2
	}

	// Optional filter: pending status
	if filters.Pending != nil {
		conditions = append(conditions, fmt.Sprintf("pending = $%d", argIndex))
		args = append(args, *filters.Pending)
		argIndex++
	}

	// Optional filter: review state (Issue #1203)
	if filters.Reviewed != nil {
		conditions = append(conditions, fmt.Sprintf("reviewed = $%d", argIndex))
		args = append(args, *filters.Reviewed)
		argIndex++
	}

	// Optional filter: category_source (Issue #1210) — OR logic within the enum set
	if len(filters.CategorySources) > 0 {
		placeholders := make([]string, len(filters.CategorySources))
		for i, s := range filters.CategorySources {
			placeholders[i] = fmt.Sprintf("$%d", argIndex)
			args = append(args, s)
			argIndex++
		}
		conditions = append(conditions, "category_source IN ("+strings.Join(placeholders, ", ")+")")
	}

	// Optional filter: min_amount (Issue #529)
	// Filters by absolute amount (users see positive amounts in UI)
	if filters.MinAmount != nil {
		conditions = append(conditions, fmt.Sprintf("ABS(amount) >= $%d", argIndex))
		args = append(args, *filters.MinAmount)
		argIndex++
	}

	// Optional filter: max_amount (Issue #529)
	if filters.MaxAmount != nil {
		conditions = append(conditions, fmt.Sprintf("ABS(amount) <= $%d", argIndex))
		args = append(args, *filters.MaxAmount)
		argIndex++
	}

	// Optional filter: amount_type (Issue #529)
	// PFCv2 sign convention: income = inflow (negative amount), expense = outflow (positive amount)
	if filters.AmountType != nil {
		switch *filters.AmountType {
		case "income":
			conditions = append(conditions, "amount < 0")
		case "expense":
			conditions = append(conditions, "amount > 0")
			// "all" means no filter - handled in handler by not setting AmountType
		}
	}

	// Optional filter: user_account_id (Issue #1109)
	// Filters by stable logical account identity across re-links
	if filters.UserAccountID != nil {
		conditions = append(conditions, fmt.Sprintf("user_account_id = $%d", argIndex))
		args = append(args, *filters.UserAccountID)
		argIndex++
	}

	// Optional filter: merchant_id (Issue #1163)
	// Filters by exact merchant UUID match
	if filters.MerchantID != nil {
		conditions = append(conditions, fmt.Sprintf("merchant_id = $%d", argIndex))
		args = append(args, *filters.MerchantID)
		argIndex++
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

// scanTransactions scans SQL rows into Transaction structs
func (r *transactionRepository) scanTransactions(rows pgx.Rows) ([]domain.Transaction, error) {
	var transactions []domain.Transaction

	for rows.Next() {
		var tx domain.Transaction
		var source string
		var metaStr *string
		var tagsJSON []byte
		var tagsSourceJSON []byte
		var categorySource sql.NullString

		err := rows.Scan(
			&tx.ID,
			&tx.UserID,
			&tx.AccountID,
			&tx.LinkedItemID,
			&source,
			&tx.ExternalTransactionID,
			&metaStr,
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
			&tagsJSON,
			&tagsSourceJSON,
			&tx.CategoryOverrideKind,
			&tx.PFCPrimaryOverride,
			&tx.PFCDetailedOverride,
			&tx.CustomCategoryOverrideID,
			&categorySource,
			&tx.CategoryOverrideDate,
			&tx.EffectivePFCPrimary,
			&tx.EffectivePFCDetailed,
			&tx.EffectiveCategoryName,
			&tx.Reviewed,
			&tx.ReviewedAt,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan transaction: %w", err)
		}

		tx.Source = domain.TransactionSource(source)
		tx.ExternalMetadata = metaStr

		// Decrypt PII fields
		if err := r.decryptTransactionPIIFields(&tx); err != nil {
			return nil, fmt.Errorf("failed to decrypt transaction: %w", err)
		}

		// Parse tags from JSONB
		if tagsJSON != nil {
			if err := json.Unmarshal(tagsJSON, &tx.Tags); err != nil {
				return nil, fmt.Errorf("failed to unmarshal tags: %w", err)
			}
		}

		// Parse tags_source from JSONB
		if tagsSourceJSON != nil {
			if err := json.Unmarshal(tagsSourceJSON, &tx.TagsSource); err != nil {
				return nil, fmt.Errorf("failed to unmarshal tags_source: %w", err)
			}
		}

		// Set category source
		if categorySource.Valid {
			tx.CategorySourceField = domain.CategorySource(categorySource.String)
		}

		transactions = append(transactions, tx)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating transaction rows: %w", err)
	}

	return transactions, nil
}

// GetByID retrieves a transaction by its internal ID
func (r *transactionRepository) GetByID(ctx context.Context, id string) (*domain.Transaction, error) {
	query := fmt.Sprintf(`
		SELECT %s
		FROM transactions
		WHERE id = $1
	`, transactionSelectExpr)

	var tx domain.Transaction
	var source string
	var metaStr *string
	var tagsJSON []byte
	var tagsSourceJSON []byte
	var categorySource sql.NullString

	err := r.db.QueryRow(ctx, query, id).Scan(
		&tx.ID,
		&tx.UserID,
		&tx.AccountID,
		&tx.LinkedItemID,
		&source,
		&tx.ExternalTransactionID,
		&metaStr,
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
		&tagsJSON,
		&tagsSourceJSON,
		&tx.CategoryOverrideKind,
		&tx.PFCPrimaryOverride,
		&tx.PFCDetailedOverride,
		&tx.CustomCategoryOverrideID,
		&categorySource,
		&tx.CategoryOverrideDate,
		&tx.EffectivePFCPrimary,
		&tx.EffectivePFCDetailed,
		&tx.EffectiveCategoryName,
		&tx.Reviewed,
		&tx.ReviewedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTransactionNotFound
		}
		return nil, fmt.Errorf("failed to get transaction by ID: %w", err)
	}

	tx.Source = domain.TransactionSource(source)
	tx.ExternalMetadata = metaStr

	// Decrypt PII fields
	if err := r.decryptTransactionPIIFields(&tx); err != nil {
		return nil, fmt.Errorf("failed to decrypt transaction: %w", err)
	}

	// Parse tags from JSONB
	if tagsJSON != nil {
		if err := json.Unmarshal(tagsJSON, &tx.Tags); err != nil {
			return nil, fmt.Errorf("failed to unmarshal tags: %w", err)
		}
	}

	// Parse tags_source from JSONB
	if tagsSourceJSON != nil {
		if err := json.Unmarshal(tagsSourceJSON, &tx.TagsSource); err != nil {
			return nil, fmt.Errorf("failed to unmarshal tags_source: %w", err)
		}
	}

	// Set category source
	if categorySource.Valid {
		tx.CategorySourceField = domain.CategorySource(categorySource.String)
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

	query := `
		UPDATE transactions
		SET tags = $1, tags_source = $2, updated_at = $3
		WHERE id = $4 AND user_id = $5
	`

	ct, err := r.db.Exec(ctx, query, tagsJSON, tagsSourceJSON, time.Now(), id, userID)
	if err != nil {
		return fmt.Errorf("failed to update transaction tags: %w", err)
	}

	if ct.RowsAffected() == 0 {
		return ErrTransactionNotFound
	}

	return nil
}

// UpdateCategory updates the category override for a transaction using the PFC v2 discriminated union.
func (r *transactionRepository) UpdateCategory(ctx context.Context, id string, userID string, overrideKind *string, pfcPrimaryOverride *string, pfcDetailedOverride *string, customCategoryOverrideID *string, categorySource domain.CategorySource) error {
	now := time.Now()

	query := `
		UPDATE transactions
		SET category_override_kind = $1,
		    pfc_primary_override = $2,
		    pfc_detailed_override = $3,
		    custom_category_override_id = $4,
		    category_source = $5,
		    category_override_date = $6,
		    reviewed = true,
		    reviewed_at = $6,
		    effective_category_name = CASE
		        WHEN $1 = 'custom' THEN
		            (SELECT name FROM custom_categories WHERE id = $4::uuid AND user_id = transactions.user_id)
		        WHEN $1 = 'pfc' THEN COALESCE(NULLIF($2, ''), pfc_primary)
		        ELSE pfc_primary
		    END,
		    updated_at = $7
		WHERE id = $8 AND user_id = $9
	`

	ct, err := r.db.Exec(ctx, query, overrideKind, pfcPrimaryOverride, pfcDetailedOverride, customCategoryOverrideID, categorySource, now, now, id, userID)
	if err != nil {
		return fmt.Errorf("failed to update transaction category: %w", err)
	}

	if ct.RowsAffected() == 0 {
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

	query := `
		UPDATE transactions
		SET reviewed = $1, reviewed_at = $2, updated_at = NOW()
		WHERE user_id = $3 AND id = ANY($4::uuid[])
	`

	ct, err := r.db.Exec(ctx, query, reviewed, reviewedAt, userID, transactionIDs)
	if err != nil {
		return 0, fmt.Errorf("failed to set reviewed: %w", err)
	}
	return ct.RowsAffected(), nil
}

// BulkUpdateCategory recategorizes many transactions in one UPDATE (Issue #1210).
// Single statement (atomic by definition) — no explicit transaction needed.
// IDs not owned by userID are silently skipped; returns rows affected.
func (r *transactionRepository) BulkUpdateCategory(ctx context.Context, userID string, transactionIDs []string, overrideKind *string, pfcPrimaryOverride *string, pfcDetailedOverride *string, customCategoryOverrideID *string, categorySource domain.CategorySource) (int64, error) {
	if len(transactionIDs) == 0 {
		return 0, nil
	}
	now := time.Now()

	query := `
		UPDATE transactions
		SET category_override_kind      = $1,
		    pfc_primary_override        = $2,
		    pfc_detailed_override       = $3,
		    custom_category_override_id = $4,
		    category_source             = $5,
		    category_override_date      = $6,
		    reviewed                    = true,
		    reviewed_at                 = $6,
		    effective_category_name     = CASE
		        WHEN $1 = 'custom' THEN
		            (SELECT name FROM custom_categories WHERE id = $4::uuid AND user_id = transactions.user_id)
		        WHEN $1 = 'pfc' THEN COALESCE(NULLIF($2, ''), pfc_primary)
		        ELSE pfc_primary
		    END,
		    updated_at = $7
		WHERE user_id = $8
		  AND id = ANY($9::uuid[])
	`

	ct, err := r.db.Exec(ctx, query, overrideKind, pfcPrimaryOverride, pfcDetailedOverride, customCategoryOverrideID, categorySource, now, now, userID, transactionIDs)
	if err != nil {
		return 0, fmt.Errorf("failed to bulk update transaction category: %w", err)
	}
	return ct.RowsAffected(), nil
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
		argIndex++
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
		unreviewedCount        int64
		reviewedByRuleCount    int64
		reviewedByManualCount  int64
		reviewedByPlaidCount   int64
		avgIngestionSec        *float64
		topCategoriesJSON      []byte
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
		clause := fmt.Sprintf(`(effective_pfc_primary = $%d OR (category_override_kind = 'custom' AND custom_category_override_id IN (SELECT id FROM custom_categories WHERE user_id = $1 AND parent_pfc_primary = $%d)))`, argStart, argStart)
		return clause, []interface{}{*pfcPrimary}
	case "custom":
		if customCategoryID == nil {
			return "1=0", nil
		}
		// A custom category rolls up its custom children (parent_id). A leaf
		// custom category's subquery resolves to just itself.
		clause := fmt.Sprintf(`(category_override_kind = 'custom' AND custom_category_override_id IN (SELECT id FROM custom_categories WHERE user_id = $1 AND (id::text = $%d OR parent_id::text = $%d)))`, argStart, argStart)
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
			  AND date BETWEEN $2 AND $3
			  AND (%s)
		`, catClause)
		args = append(baseArgs, catArgs...)

	case domain.MatchLogicTagOnly:
		query = `
			SELECT COALESCE(ABS(SUM(amount)), 0)
			FROM transactions
			WHERE user_id = $1 AND amount > 0
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
			  AND date BETWEEN $2 AND $3
			  AND (%s)
		`, catClause)
		args = append(baseArgs, catArgs...)

	case domain.MatchLogicTagOnly:
		query = `
			SELECT COUNT(*), COALESCE(ABS(SUM(amount)), 0)
			FROM transactions
			WHERE user_id = $1 AND amount > 0
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
	countQuery := fmt.Sprintf(`SELECT COUNT(*), COALESCE(SUM(amount), 0) FROM transactions WHERE %s`, whereClause)
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
// NOTE: This SELECT includes user_account_id at position 5 (34 columns total).
// Columns 1-4 and 6-34 must stay in sync with transactionSelectColumns.
func (r *transactionRepository) GetByUserAccountID(ctx context.Context, userID string, userAccountID string) ([]domain.Transaction, error) {
	query := `
		SELECT id, user_id, account_id, linked_item_id, user_account_id,
			   source, external_transaction_id, external_metadata,
			   amount, date, authorized_date, name, merchant_name, merchant_id,
			   pfc_primary, pfc_detailed, pfc_confidence_level, pfc_category_icon_url, payment_channel, pending, user_notes,
			   created_at, updated_at, tags, tags_source,
			   category_override_kind, pfc_primary_override, pfc_detailed_override,
			   custom_category_override_id, category_source, category_override_date,
			   effective_pfc_primary, effective_pfc_detailed, effective_category_name,
			   reviewed, reviewed_at
		FROM transactions
		WHERE user_id = $1 AND user_account_id = $2
		ORDER BY date DESC, created_at DESC
	`

	rows, err := r.db.Query(ctx, query, userID, userAccountID)
	if err != nil {
		return nil, fmt.Errorf("failed to query transactions by user_account_id: %w", err)
	}
	defer rows.Close()

	var transactions []domain.Transaction

	for rows.Next() {
		var tx domain.Transaction
		var source string
		var metaStr *string
		var tagsJSON []byte
		var tagsSourceJSON []byte
		var categorySource sql.NullString

		err := rows.Scan(
			&tx.ID,
			&tx.UserID,
			&tx.AccountID,
			&tx.LinkedItemID,
			&tx.UserAccountID,
			&source,
			&tx.ExternalTransactionID,
			&metaStr,
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
			&tagsJSON,
			&tagsSourceJSON,
			&tx.CategoryOverrideKind,
			&tx.PFCPrimaryOverride,
			&tx.PFCDetailedOverride,
			&tx.CustomCategoryOverrideID,
			&categorySource,
			&tx.CategoryOverrideDate,
			&tx.EffectivePFCPrimary,
			&tx.EffectivePFCDetailed,
			&tx.EffectiveCategoryName,
			&tx.Reviewed,
			&tx.ReviewedAt,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan transaction: %w", err)
		}

		tx.Source = domain.TransactionSource(source)
		tx.ExternalMetadata = metaStr

		// Decrypt PII fields
		if err := r.decryptTransactionPIIFields(&tx); err != nil {
			return nil, fmt.Errorf("failed to decrypt transaction: %w", err)
		}

		// Parse tags from JSONB
		if tagsJSON != nil {
			if err := json.Unmarshal(tagsJSON, &tx.Tags); err != nil {
				return nil, fmt.Errorf("failed to unmarshal tags: %w", err)
			}
		}

		// Parse tags_source from JSONB
		if tagsSourceJSON != nil {
			if err := json.Unmarshal(tagsSourceJSON, &tx.TagsSource); err != nil {
				return nil, fmt.Errorf("failed to unmarshal tags_source: %w", err)
			}
		}

		// Set category source
		if categorySource.Valid {
			tx.CategorySourceField = domain.CategorySource(categorySource.String)
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
) ([]domain.Transaction, *domain.TransactionCursor, error) {
	whereClause, args := buildWhereClause(userID, filters)

	sortKey := filters.SortBy
	if sortKey == "" {
		sortKey = "date"
	}
	sortDir := strings.ToLower(filters.SortDir)
	if sortDir == "" {
		sortDir = "desc"
	}

	col, ok := sortColumnMap[sortKey]
	if !ok {
		col = sortColumnMap["date"]
		sortKey = "date"
	}

	if cursor != nil {
		argBase := len(args) + 1
		if cursor.SV == nil {
			args = append(args, cursor.ID.String())
			if sortDir == "desc" {
				// DESC NULLS FIRST: cursor in NULL region; include remaining NULLs + all non-NULLs
				whereClause += fmt.Sprintf(" AND ((%s IS NULL AND id > $%d) OR %s IS NOT NULL)", col, argBase, col)
			} else {
				// ASC NULLS LAST: cursor in NULL region at end; only remaining NULLs
				whereClause += fmt.Sprintf(" AND (%s IS NULL AND id > $%d)", col, argBase)
			}
		} else {
			cmpOp := "<"
			if sortDir == "asc" {
				cmpOp = ">"
			}
			args = append(args, *cursor.SV, cursor.ID.String())

			if sortKey == "date" {
				whereClause += fmt.Sprintf(
					" AND ((%s::date %s $%d::date) OR (%s::date = $%d::date AND id > $%d))",
					col, cmpOp, argBase, col, argBase, argBase+1,
				)
			} else if sortKey == "amount" {
				whereClause += fmt.Sprintf(
					" AND ((%s %s $%d::numeric) OR (%s = $%d::numeric AND id > $%d))",
					col, cmpOp, argBase, col, argBase, argBase+1,
				)
			} else if sortKey == "category" && sortDir == "asc" {
				// category can be NULL; ASC NULLS LAST — include NULL rows that come after non-NULLs
				whereClause += fmt.Sprintf(
					" AND ((%s %s $%d) OR (%s = $%d AND id > $%d) OR %s IS NULL)",
					col, cmpOp, argBase, col, argBase, argBase+1, col,
				)
			} else {
				whereClause += fmt.Sprintf(
					" AND ((%s %s $%d) OR (%s = $%d AND id > $%d))",
					col, cmpOp, argBase, col, argBase, argBase+1,
				)
			}
		}
	}

	dir := "DESC"
	if sortDir == "asc" {
		dir = "ASC"
	}
	orderBy := fmt.Sprintf("ORDER BY %s %s, id ASC", col, dir)

	args = append(args, limit+1)
	limitPlaceholder := len(args)

	query := fmt.Sprintf(`
		SELECT %s
		FROM transactions
		%s
		%s
		LIMIT $%d
	`, transactionSelectExpr, whereClause, orderBy, limitPlaceholder)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query transactions (keyset): %w", err)
	}
	defer rows.Close()

	txs, err := r.scanTransactions(rows)
	if err != nil {
		return nil, nil, err
	}
	if err := r.enrichEffectiveMerchantNames(ctx, userID, txs); err != nil {
		return nil, nil, err
	}

	if len(txs) == limit+1 {
		last := txs[limit-1]
		nextCursor, buildErr := buildKeysetCursor(last, sortKey, sortDir)
		if buildErr != nil {
			return nil, nil, fmt.Errorf("failed to build next cursor: %w", buildErr)
		}
		return txs[:limit], nextCursor, nil
	}

	return txs, nil, nil
}

// buildKeysetCursor constructs a TransactionCursor from a transaction row.
func buildKeysetCursor(tx domain.Transaction, sortKey, sortDir string) (*domain.TransactionCursor, error) {
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
		sv := tx.MerchantName
		if sv == nil || *sv == "" {
			cursor.SV = &tx.Name
		} else {
			cursor.SV = sv
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
func (r *transactionRepository) GetByUserIDKeysetUnified(
	ctx context.Context,
	userID string,
	filters domain.TransactionFilters,
	cursor *domain.TransactionCursor,
	limit int,
) ([]*domain.ActivityItem, *domain.TransactionCursor, error) {
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
	// Banking: name OR merchant_name; Investment: name OR ticker_symbol OR security_name.
	bankingSearchFilter := ""
	investSearchFilter := ""
	if filters.Search != nil {
		pattern := "%" + *filters.Search + "%"
		bankingSearchFilter = fmt.Sprintf(
			" AND (t.name ILIKE $%d OR t.merchant_name ILIKE $%d)",
			argIdx, argIdx)
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

	// PFC / custom-category filters (banking only).
	if len(filters.PFCPrimaries) > 0 || len(filters.PFCDetaileds) > 0 {
		var orParts []string
		var primPHs, detPHs []string
		for _, p := range filters.PFCPrimaries {
			primPHs = append(primPHs, fmt.Sprintf("$%d", argIdx))
			args = append(args, p)
			argIdx++
		}
		for _, d := range filters.PFCDetaileds {
			detPHs = append(detPHs, fmt.Sprintf("$%d", argIdx))
			args = append(args, d)
			argIdx++
		}
		if len(primPHs) > 0 {
			orParts = append(orParts, "t.effective_pfc_primary IN ("+strings.Join(primPHs, ", ")+")")
		}
		if len(detPHs) > 0 {
			orParts = append(orParts, "t.effective_pfc_detailed IN ("+strings.Join(detPHs, ", ")+")")
		}
		if len(orParts) > 0 {
			bankingExtraFilter += " AND (" + strings.Join(orParts, " OR ") + ")"
			investBankingOnlyFilter = " AND FALSE"
		}
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

	// CustomCategoryIDs — rolls up children (parent_id), banking rows only → zero out investment.
	if len(filters.CustomCategoryIDs) > 0 {
		phs := make([]string, len(filters.CustomCategoryIDs))
		for i, id := range filters.CustomCategoryIDs {
			phs[i] = fmt.Sprintf("$%d", argIdx)
			args = append(args, id)
			argIdx++
		}
		idList := strings.Join(phs, ", ")
		bankingExtraFilter += fmt.Sprintf(
			" AND (t.category_override_kind = 'custom' AND t.custom_category_override_id IN ("+
				"SELECT id FROM custom_categories WHERE user_id = $1 AND (id::text IN (%s) OR parent_id::text IN (%s))))",
			idList, idList,
		)
		investBankingOnlyFilter = " AND FALSE"
	}

	// ── Investment-only filters ───────────────────────────────────────────────

	// bankingActivityFilter zeroes the banking branch when an activity_types filter is active:
	// banking rows carry no activity_type and must not bleed into an investment-activity-scoped result.
	// When TradeTypeToSQLPredicate returns an empty clause (all types unrecognized), both branches
	// return zero rows — consistent empty result rather than asymmetric (all invest, no banking).
	var bankingActivityFilter, investActivityFilter string
	if len(filters.ActivityTypes) > 0 {
		clause, typeArgs := domain.TradeTypeToSQLPredicate(filters.ActivityTypes, "it")
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
			return nil, nil, fmt.Errorf("cursor: missing sort date — restart pagination")
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

	query := fmt.Sprintf(`
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
			NULL::text,
			NULL::text,
			NULL::text,
			NULL::text,
			NULL::text,
			NULL::float8,
			NULL::float8,
			NULL::float8
		FROM transactions t
		WHERE t.user_id = $1
		  -- Chain-expansion active-account filter: include pre-relink rows whose
		  -- account_id is replaced, as long as an active account shares the same
		  -- user_account_id (the stable chain key set at insert time — Issue #1109).
		  -- Using EXISTS subqueries (no JOIN) avoids row duplication when multiple
		  -- accounts share a user_account_id (Issue #1304).
		  -- Note: chain_a.user_id is intentionally omitted — user-ownership is
		  -- transitive via the user_accounts FK (migration 000033), and the outer
		  -- query is already gated on t.user_id = $1.  Parity with the investment
		  -- branch is intentional; diverging here would reintroduce asymmetry.
		  AND (
		      EXISTS (SELECT 1 FROM accounts WHERE id = t.account_id AND status = 'active') -- NULL account_id always false here; falls through to chain leg below
		      OR (
		          t.user_account_id IS NOT NULL
		          AND EXISTS (
		              SELECT 1 FROM accounts chain_a
		              WHERE chain_a.user_account_id = t.user_account_id
		                AND chain_a.status = 'active'
		          )
		      )
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
			it.security_id::text,
			sec.ticker_symbol,
			sec.name,
			it.type,
			it.subtype,
			it.quantity,
			it.price,
			it.fees
		FROM investment_transactions it
		LEFT JOIN securities sec ON sec.id = it.security_id
		WHERE it.user_id = $1
		  -- Chain-expansion active-account filter: include pre-relink rows whose
		  -- account_id is replaced, as long as an active account shares the same
		  -- user_account_id (the stable chain key set at insert time — Issue #1109).
		  -- Using EXISTS subqueries (no JOIN) avoids row duplication when multiple
		  -- accounts share a user_account_id (Issue #1304).
		  -- Note: chain_a.user_id is intentionally omitted — user-ownership is
		  -- transitive via the user_accounts FK (migration 000033), and the outer
		  -- query is already gated on it.user_id = $1.  Parity with the banking
		  -- branch is intentional; diverging here would reintroduce asymmetry.
		  AND (
		      EXISTS (SELECT 1 FROM accounts WHERE id = it.account_id AND status = 'active')
		      OR (
		          it.user_account_id IS NOT NULL
		          AND EXISTS (
		              SELECT 1 FROM accounts chain_a
		              WHERE chain_a.user_account_id = it.user_account_id
		                AND chain_a.status = 'active'
		          )
		      )
		  )
		%s%s%s%s%s%s

		ORDER BY sort_date DESC, source_disc ASC, id ASC
		LIMIT $%d
	`,
		bankingAcctFilter, bankingDateFilter, bankingSearchFilter, bankingExtraFilter, bankingCursorFilter, bankingActivityFilter,
		investAcctFilter, investDateFilter, investSearchFilter, investActivityFilter, investCursorFilter, investBankingOnlyFilter,
		limitPH,
	)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("GetByUserIDKeysetUnified: %w", err)
	}
	defer rows.Close()

	var items []*domain.ActivityItem
	for rows.Next() {
		var (
			id, userIDCol, sourceDisc, name string
			accountID                        *string
			sortDate, date                             time.Time
			amount                                     float64
			userAccountID, linkedItemID                *string
			authorizedDate                             *time.Time
			merchantName, merchantID, dataSource       *string
			externalTxnID                              *string
			pfcPrimary, pfcDetailed                    *string
			pfcConfidenceLevel, pfcCategoryIconURL     *string
			pfcPrimaryOverride, pfcDetailedOverride    *string
			customCategoryOverrideID                   *string
			paymentChannel                             *string
			pending                                    *bool
			userNotes, categoryOverrideKind            *string
			effectivePFCPrimary, effectivePFCDetailed  *string
			effectiveCategoryName                      *string
			reviewed                                   *bool
			reviewedAt                                 *time.Time
			tagsJSON                                   []byte
			securityID, tickerSymbol, securityName     *string
			invType, invSubtype                        *string
			invQuantity, invPrice, invFees             *float64
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
			&securityID, &tickerSymbol, &securityName,
			&invType, &invSubtype,
			&invQuantity, &invPrice, &invFees,
		); err != nil {
			return nil, nil, fmt.Errorf("GetByUserIDKeysetUnified scan: %w", err)
		}

		item := &domain.ActivityItem{
			Source:                sourceDisc,
			SortDate:              sortDate,
			ID:                    id,
			UserID:                userIDCol,
			AccountID:             nilOrEmpty(accountID),
			Date:                  date,
			Amount:                amount,
			Name:                  name,
			UserAccountID:         userAccountID,
			LinkedItemID:          linkedItemID,
			AuthorizedDate:        authorizedDate,
			MerchantName:          merchantName,
			MerchantID:            merchantID,
			DataSource:            dataSource,
			PFCPrimary:               pfcPrimary,
			PFCDetailed:              pfcDetailed,
			PFCConfidenceLevel:       pfcConfidenceLevel,
			PFCCategoryIconURL:       pfcCategoryIconURL,
			PFCPrimaryOverride:       pfcPrimaryOverride,
			PFCDetailedOverride:      pfcDetailedOverride,
			CustomCategoryOverrideID: customCategoryOverrideID,
			PaymentChannel:           paymentChannel,
			Pending:               pending,
			UserNotes:             userNotes,
			CategoryOverrideKind:  categoryOverrideKind,
			EffectivePFCPrimary:   effectivePFCPrimary,
			EffectivePFCDetailed:  effectivePFCDetailed,
			EffectiveCategoryName: effectiveCategoryName,
			Reviewed:              reviewed,
			ReviewedAt:            reviewedAt,
			SecurityID:            securityID,
			TickerSymbol:          tickerSymbol,
			SecurityName:          securityName,
			InvestmentSubtype:     invSubtype,
			InvestmentQuantity:    invQuantity,
			InvestmentPrice:       invPrice,
			InvestmentFees:        invFees,
		}

		// Normalize ActivityType from raw Plaid type/subtype for investment rows.
		if invType != nil {
			at := domain.NormalizeTradeType(*invType, invSubtype)
			item.ActivityType = &at
		}

		// Decrypt external_transaction_id (encrypted in DB for banking rows).
		if externalTxnID != nil {
			decrypted, decErr := r.enc.DecryptNullableString(externalTxnID)
			if decErr != nil {
				return nil, nil, fmt.Errorf("GetByUserIDKeysetUnified decrypt: %w", decErr)
			}
			item.ExternalTransactionID = decrypted
		}

		// Unmarshal tags JSONB (banking rows only; investment rows get NULL::jsonb → nil slice).
		if len(tagsJSON) > 0 {
			var tags []string
			if err := json.Unmarshal(tagsJSON, &tags); err != nil {
				return nil, nil, fmt.Errorf("GetByUserIDKeysetUnified unmarshal tags: %w", err)
			}
			item.Tags = tags
		}

		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, nil, fmt.Errorf("GetByUserIDKeysetUnified rows: %w", rows.Err())
	}

	// Build next-page cursor from the limit+1 probe row.
	if len(items) == limit+1 {
		last := items[limit-1]
		lastID, err := uuid.Parse(last.ID)
		if err != nil {
			return nil, nil, fmt.Errorf("GetByUserIDKeysetUnified: invalid id %q: %w", last.ID, err)
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
		return items[:limit], nextCursor, nil
	}

	return items, nil, nil
}
