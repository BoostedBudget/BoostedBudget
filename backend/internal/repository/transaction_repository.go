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

// TransactionRepository defines the interface for transaction data access.
// Supports transactions from all providers (Plaid, manual, Teller, etc.)
type TransactionRepository interface {
	// Create creates a new transaction in the database
	Create(ctx context.Context, transaction *domain.Transaction) error

	// CreateBatch creates multiple transactions in a single database operation
	// Optimized for bulk inserts during transaction sync
	CreateBatch(ctx context.Context, transactions []domain.Transaction) error

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

	// UpdateCategory updates the category override for a transaction
	// Used when manually editing transaction category
	UpdateCategory(ctx context.Context, id string, userID string, categoryOverride []string, categorySource domain.CategorySource) error

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

	// GetSpentSumForBudgetCategory returns the sum of expenses (negative amount) transactions
	// matching the given category and/or tags based on match logic.
	// Supports four match logic types:
	//   - category_only: Match transactions where category contains the budget category
	//   - tag_only: Match transactions where tags contain ANY of the budget tags
	//   - and: Match transactions where BOTH category AND tags match
	//   - or: Match transactions where EITHER category OR tags match
	// Checks both category and category_override fields for category matching.
	GetSpentSumForBudgetCategory(ctx context.Context, userID string, category []string, tags []string, matchLogic domain.MatchLogic, startDate, endDate time.Time) (float64, error)

	// Issue #497: Category Preview Feature

	// GetMatchingTransactionStats returns the count and sum of expenses (negative amount) transactions
	// matching the given categories and/or tags based on match logic.
	// Supports multiple categories (OR across categories) and four match logic types:
	//   - category_only: Match transactions where category contains ANY of the provided categories
	//   - tag_only: Match transactions where tags contain ANY of the budget tags
	//   - and: Match transactions where BOTH category AND tags match
	//   - or: Match transactions where EITHER category OR tags match
	// Used for previewing how many transactions would match a budget category configuration.
	GetMatchingTransactionStats(ctx context.Context, userID string, categories [][]string, tags []string, matchLogic domain.MatchLogic, startDate, endDate time.Time) (count int, totalAmount float64, err error)

	// Issue #499: Transaction-Budget Linking UI

	// GetTransactionsForBudgetCategory returns paginated transactions matching the given category
	// and/or tags based on match logic, along with total count and amount.
	// Uses the same matching logic as GetSpentSumForBudgetCategory.
	// Returns transactions ordered by date DESC.
	GetTransactionsForBudgetCategory(ctx context.Context, userID string, category []string, tags []string, matchLogic domain.MatchLogic, startDate, endDate time.Time, limit, offset int) (transactions []domain.Transaction, totalCount int, totalAmount float64, err error)

	// Issue #171: Re-Link Handling

	// GetByUserAccountID retrieves all transactions for a specific user_account.
	// This provides stable queries across re-links since user_account_id survives
	// when a user re-links a bank (which assigns new item_id and account_id values).
	// Returns transactions from all associated accounts (current and replaced) ordered by date DESC.
	GetByUserAccountID(ctx context.Context, userID string, userAccountID string) ([]domain.Transaction, error)
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
		 name, merchant_name, category, personal_finance_category, payment_channel, pending,
		 user_notes, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
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
		transaction.Category,
		transaction.PersonalFinanceCategory,
		transaction.PaymentChannel,
		transaction.Pending,
		transaction.UserNotes,
		transaction.CreatedAt,
		transaction.UpdatedAt,
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
func (r *transactionRepository) CreateBatch(ctx context.Context, transactions []domain.Transaction) error {
	if len(transactions) == 0 {
		return nil
	}

	// Build multi-row INSERT query
	// Issue #171: Added user_account_id field; encryption adds external_transaction_id_blind_idx (21 fields)
	const numFields = 21
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
			return fmt.Errorf("failed to encrypt transaction fields: %w", err)
		}

		// Create placeholders for this row
		placeholders := make([]string, numFields)
		for j := 0; j < numFields; j++ {
			placeholders[j] = fmt.Sprintf("$%d", i*numFields+j+1)
		}
		valueStrings = append(valueStrings, fmt.Sprintf("(%s)", strings.Join(placeholders, ", ")))

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
			transactions[i].Category,
			transactions[i].PersonalFinanceCategory,
			transactions[i].PaymentChannel,
			transactions[i].Pending,
			transactions[i].UserNotes,
			transactions[i].CreatedAt,
			transactions[i].UpdatedAt,
		)
	}

	// Issue #171: Changed from DO NOTHING to DO UPDATE for re-link handling.
	// On conflict (duplicate transaction by blind index), we:
	// - UPDATE: FKs (account_id, linked_item_id, user_account_id) - changes on re-link
	// - UPDATE: Plaid fields (amount, date, name, merchant_name, pending, etc.) - may change
	// - UPDATE: external_transaction_id_blind_idx in case it was previously NULL
	// - PRESERVE: User enrichments (user_notes, category_override, tags, tags_source)
	query := fmt.Sprintf(`
		INSERT INTO transactions
		(id, user_id, account_id, linked_item_id, user_account_id,
		 source, external_transaction_id, external_transaction_id_blind_idx, external_metadata,
		 amount, date, authorized_date, name, merchant_name,
		 category, personal_finance_category, payment_channel, pending, user_notes,
		 created_at, updated_at)
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
			category = EXCLUDED.category,
			personal_finance_category = EXCLUDED.personal_finance_category,
			payment_channel = EXCLUDED.payment_channel,
			pending = EXCLUDED.pending,
			updated_at = NOW()
	`, strings.Join(valueStrings, ", "))
	// Note: user_notes, category_override, tags, tags_source are NOT in the SET clause
	// so they will be PRESERVED on conflict

	_, err := r.db.Exec(ctx, query, valueArgs...)
	if err != nil {
		return fmt.Errorf("failed to batch create transactions: %w", err)
	}

	return nil
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
		    merchant_name = $5, category = $6, personal_finance_category = $7,
		    payment_channel = $8, pending = $9, external_metadata = $10,
		    user_notes = $11, updated_at = $12
		WHERE id = $13
	`

	ct, err := r.db.Exec(ctx, query,
		transaction.Amount,
		transaction.Date,
		transaction.AuthorizedDate,
		transaction.Name,
		transaction.MerchantName,
		transaction.Category,
		transaction.PersonalFinanceCategory,
		transaction.PaymentChannel,
		transaction.Pending,
		encMeta,
		transaction.UserNotes,
		transaction.UpdatedAt,
		transaction.ID,
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
	query := `
		SELECT id, user_id, account_id, linked_item_id, source, external_transaction_id,
		       external_metadata, amount, date, authorized_date, name, merchant_name,
		       category, personal_finance_category, payment_channel, pending,
		       user_notes, created_at, updated_at
		FROM transactions
		WHERE user_id = $1
		ORDER BY date DESC, created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := r.db.Query(ctx, query, userID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query transactions: %w", err)
	}
	defer rows.Close()

	return r.scanTransactions(rows)
}

// GetByAccountID retrieves all transactions for a specific account
func (r *transactionRepository) GetByAccountID(ctx context.Context, accountID string) ([]domain.Transaction, error) {
	query := `
		SELECT id, user_id, account_id, linked_item_id, source, external_transaction_id,
		       external_metadata, amount, date, authorized_date, name, merchant_name,
		       category, personal_finance_category, payment_channel, pending,
		       user_notes, created_at, updated_at
		FROM transactions
		WHERE account_id = $1
		ORDER BY date DESC, created_at DESC
	`

	rows, err := r.db.Query(ctx, query, accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to query transactions: %w", err)
	}
	defer rows.Close()

	return r.scanTransactions(rows)
}

// GetBySource retrieves all transactions from a specific source for a user
func (r *transactionRepository) GetBySource(ctx context.Context, source domain.TransactionSource, userID string) ([]domain.Transaction, error) {
	query := `
		SELECT id, user_id, account_id, linked_item_id, source, external_transaction_id,
		       external_metadata, amount, date, authorized_date, name, merchant_name,
		       category, personal_finance_category, payment_channel, pending,
		       user_notes, created_at, updated_at
		FROM transactions
		WHERE source = $1 AND user_id = $2
		ORDER BY date DESC, created_at DESC
	`

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
		SELECT id, user_id, account_id, linked_item_id, source, external_transaction_id,
		       external_metadata, amount, date, authorized_date, name, merchant_name,
		       category, personal_finance_category, payment_channel, pending,
		       user_notes, created_at, updated_at,
		       tags, tags_source,
		       category_override, category_source, category_override_date, original_category
		FROM transactions
		%s
		ORDER BY date DESC, created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, len(args)-1, len(args))

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query transactions: %w", err)
	}
	defer rows.Close()

	return r.scanTransactions(rows)
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
	query := `
		SELECT id, user_id, account_id, linked_item_id, source,
		       external_transaction_id, external_metadata, amount, date,
		       authorized_date, name, merchant_name, category,
		       personal_finance_category, payment_channel, pending,
		       user_notes, created_at, updated_at,
		       tags, tags_source,
		       category_override, category_source, category_override_date, original_category
		FROM transactions
		WHERE source = $1 AND external_transaction_id_blind_idx = $2 AND user_id = $3
	`

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
		&tx.Category,
		&tx.PersonalFinanceCategory,
		&tx.PaymentChannel,
		&tx.Pending,
		&tx.UserNotes,
		&tx.CreatedAt,
		&tx.UpdatedAt,
		&tagsJSON,
		&tagsSourceJSON,
		&tx.CategoryOverride,
		&categorySource,
		&tx.CategoryOverrideDate,
		&tx.OriginalCategory,
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
func buildWhereClause(userID string, filters domain.TransactionFilters) (string, []interface{}) {
	var conditions []string
	var args []interface{}
	argIndex := 1

	// Always filter by user_id
	conditions = append(conditions, fmt.Sprintf("user_id = $%d", argIndex))
	args = append(args, userID)
	argIndex++

	// Optional filter: account_id (single, deprecated)
	if filters.AccountID != nil {
		conditions = append(conditions, fmt.Sprintf("account_id = $%d", argIndex))
		args = append(args, *filters.AccountID)
		argIndex++
	}

	// Optional filter: account_ids (multi-value, Issue #529)
	// Takes precedence over single account_id if both provided
	if len(filters.AccountIDs) > 0 {
		placeholders := make([]string, len(filters.AccountIDs))
		for i, id := range filters.AccountIDs {
			placeholders[i] = fmt.Sprintf("$%d", argIndex)
			args = append(args, id)
			argIndex++
		}
		conditions = append(conditions, fmt.Sprintf("account_id IN (%s)", strings.Join(placeholders, ", ")))
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

	// Optional filter: category (JSONB text search, deprecated)
	if filters.Category != nil {
		conditions = append(conditions, fmt.Sprintf("category::text ILIKE $%d", argIndex))
		args = append(args, "%"+*filters.Category+"%")
		argIndex++
	}

	// Optional filter: categories (multi-value, Issue #529)
	// Searches in both category_override and original category fields
	if len(filters.Categories) > 0 {
		var categoryConditions []string
		for _, cat := range filters.Categories {
			// Check category_override first (user-set category)
			categoryConditions = append(categoryConditions,
				fmt.Sprintf("(category_override IS NOT NULL AND category_override::text ILIKE $%d)", argIndex))
			args = append(args, "%"+cat+"%")
			argIndex++
			// Also check original category
			categoryConditions = append(categoryConditions,
				fmt.Sprintf("category::text ILIKE $%d", argIndex))
			args = append(args, "%"+cat+"%")
			argIndex++
		}
		conditions = append(conditions, "("+strings.Join(categoryConditions, " OR ")+")")
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
	// income = positive amounts, expense = negative amounts
	if filters.AmountType != nil {
		switch *filters.AmountType {
		case "income":
			conditions = append(conditions, "amount > 0")
		case "expense":
			conditions = append(conditions, "amount < 0")
		// "all" means no filter - handled in handler by not setting AmountType
		}
	}

	// Combine all conditions with AND
	whereClause := "WHERE " + strings.Join(conditions, " AND ")

	return whereClause, args
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
			&tx.Category,
			&tx.PersonalFinanceCategory,
			&tx.PaymentChannel,
			&tx.Pending,
			&tx.UserNotes,
			&tx.CreatedAt,
			&tx.UpdatedAt,
			&tagsJSON,
			&tagsSourceJSON,
			&tx.CategoryOverride,
			&categorySource,
			&tx.CategoryOverrideDate,
			&tx.OriginalCategory,
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
	query := `
		SELECT id, user_id, account_id, linked_item_id, source, external_transaction_id,
			   external_metadata, amount, date, authorized_date, name, merchant_name,
			   category, personal_finance_category, payment_channel, pending, user_notes,
			   created_at, updated_at, tags, tags_source,
			   category_override, category_source, category_override_date, original_category
		FROM transactions
		WHERE id = $1
	`

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
		&tx.Category,
		&tx.PersonalFinanceCategory,
		&tx.PaymentChannel,
		&tx.Pending,
		&tx.UserNotes,
		&tx.CreatedAt,
		&tx.UpdatedAt,
		&tagsJSON,
		&tagsSourceJSON,
		&tx.CategoryOverride,
		&categorySource,
		&tx.CategoryOverrideDate,
		&tx.OriginalCategory,
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

// UpdateCategory updates the category override for a transaction
func (r *transactionRepository) UpdateCategory(ctx context.Context, id string, userID string, categoryOverride []string, categorySource domain.CategorySource) error {
	// Serialize category override to JSON
	categoryJSON, err := json.Marshal(categoryOverride)
	if err != nil {
		return fmt.Errorf("failed to marshal category override: %w", err)
	}

	now := time.Now()

	query := `
		UPDATE transactions
		SET category_override = $1, category_source = $2, category_override_date = $3,
		    original_category = COALESCE(original_category, category), updated_at = $4
		WHERE id = $5 AND user_id = $6
	`

	ct, err := r.db.Exec(ctx, query, categoryJSON, categorySource, now, now, id, userID)
	if err != nil {
		return fmt.Errorf("failed to update transaction category: %w", err)
	}

	if ct.RowsAffected() == 0 {
		return ErrTransactionNotFound
	}

	return nil
}

// GetDistinctPlaidCategories returns distinct Plaid category names for a user's transactions
// Uses personal_finance_category.primary (Plaid v2 format) for consistency across the application
func (r *transactionRepository) GetDistinctPlaidCategories(ctx context.Context, userID string) ([]string, error) {
	// Query extracts the primary category from personal_finance_category JSONB
	// - Uses personal_finance_category->>'primary' (e.g., "FOOD_AND_DRINK")
	// - Filters out NULL categories and empty strings
	query := `
		SELECT DISTINCT personal_finance_category->>'primary' as cat
		FROM transactions
		WHERE user_id = $1
		  AND personal_finance_category IS NOT NULL
		  AND personal_finance_category->>'primary' IS NOT NULL
		  AND personal_finance_category->>'primary' != ''
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

// GetIncomeSumByCategory returns the sum of income (positive amount) transactions
// matching the given category within the date range.
// Supports both legacy category arrays and PFCv2 primary enums for backward compatibility.
func (r *transactionRepository) GetIncomeSumByCategory(ctx context.Context, userID string, category string, startDate, endDate time.Time) (float64, error) {
	categoryJSONBytes, _ := json.Marshal([]string{category})
	categoryJSON := string(categoryJSONBytes)

	query := `
		SELECT COALESCE(SUM(amount), 0)
		FROM transactions
		WHERE user_id = $1
		  AND amount > 0
		  AND date BETWEEN $2 AND $3
		  AND (
		      category @> $4::jsonb
		      OR category_override @> $4::jsonb
		      OR personal_finance_category->>'primary' = $5
		  )
	`

	var sum float64
	err := r.db.QueryRow(ctx, query, userID, startDate, endDate, categoryJSON, category).Scan(&sum)
	if err != nil {
		return 0, fmt.Errorf("failed to get income sum by category: %w", err)
	}

	return sum, nil
}

// GetIncomeSumByTag returns the sum of income (positive amount) transactions
// with the given tag within the date range.
// Uses JSONB ? operator to check if tags array contains the tag.
func (r *transactionRepository) GetIncomeSumByTag(ctx context.Context, userID string, tag string, startDate, endDate time.Time) (float64, error) {
	query := `
		SELECT COALESCE(SUM(amount), 0)
		FROM transactions
		WHERE user_id = $1
		  AND amount > 0
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

// GetSpentSumForBudgetCategory returns the sum of expenses (negative amount) transactions
// matching the given category and/or tags based on match logic.
// Supports four match logic types:
//   - category_only: Match transactions where category contains the budget category
//   - tag_only: Match transactions where tags contain ANY of the budget tags
//   - and: Match transactions where BOTH category AND tags match
//   - or: Match transactions where EITHER category OR tags match
func (r *transactionRepository) GetSpentSumForBudgetCategory(ctx context.Context, userID string, category []string, tags []string, matchLogic domain.MatchLogic, startDate, endDate time.Time) (float64, error) {
	// Default to category_only if empty
	if matchLogic == "" {
		matchLogic = domain.MatchLogicCategoryOnly
	}

	// Build query based on match logic
	var query string
	var args []interface{}

	// Marshal category to JSON for JSONB containment operator
	categoryJSON, err := json.Marshal(category)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal category: %w", err)
	}

	switch matchLogic {
	case domain.MatchLogicCategoryOnly:
		query = `
			SELECT COALESCE(ABS(SUM(amount)), 0)
			FROM transactions
			WHERE user_id = $1 AND amount < 0
			  AND date BETWEEN $2 AND $3
			  AND (category @> $4::jsonb OR category_override @> $4::jsonb)
		`
		args = []interface{}{userID, startDate, endDate, string(categoryJSON)}

	case domain.MatchLogicTagOnly:
		query = `
			SELECT COALESCE(ABS(SUM(amount)), 0)
			FROM transactions
			WHERE user_id = $1 AND amount < 0
			  AND date BETWEEN $2 AND $3
			  AND tags ?| $4
		`
		args = []interface{}{userID, startDate, endDate, pq.Array(tags)}

	case domain.MatchLogicAnd:
		query = `
			SELECT COALESCE(ABS(SUM(amount)), 0)
			FROM transactions
			WHERE user_id = $1 AND amount < 0
			  AND date BETWEEN $2 AND $3
			  AND (category @> $4::jsonb OR category_override @> $4::jsonb)
			  AND tags ?| $5
		`
		args = []interface{}{userID, startDate, endDate, string(categoryJSON), pq.Array(tags)}

	case domain.MatchLogicOr:
		query = `
			SELECT COALESCE(ABS(SUM(amount)), 0)
			FROM transactions
			WHERE user_id = $1 AND amount < 0
			  AND date BETWEEN $2 AND $3
			  AND (
				(category @> $4::jsonb OR category_override @> $4::jsonb)
				OR tags ?| $5
			  )
		`
		args = []interface{}{userID, startDate, endDate, string(categoryJSON), pq.Array(tags)}

	default:
		return 0, fmt.Errorf("unsupported match logic: %s", matchLogic)
	}

	var sum float64
	err = r.db.QueryRow(ctx, query, args...).Scan(&sum)
	if err != nil {
		return 0, fmt.Errorf("failed to get spent sum for budget category: %w", err)
	}

	return sum, nil
}

// GetMatchingTransactionStats returns the count and sum of expenses (negative amount) transactions
// matching the given categories and/or tags based on match logic.
// Issue #497: Transaction preview feature
func (r *transactionRepository) GetMatchingTransactionStats(ctx context.Context, userID string, categories [][]string, tags []string, matchLogic domain.MatchLogic, startDate, endDate time.Time) (int, float64, error) {
	// Default to category_only if empty
	if matchLogic == "" {
		matchLogic = domain.MatchLogicCategoryOnly
	}

	// Build query based on match logic
	var query string
	var args []interface{}

	switch matchLogic {
	case domain.MatchLogicCategoryOnly:
		// Build OR clause for multiple categories
		categoryConditions, categoryArgs := r.buildCategoryConditions(categories)
		query = `
			SELECT COUNT(*), COALESCE(ABS(SUM(amount)), 0)
			FROM transactions
			WHERE user_id = $1 AND amount < 0
			  AND date BETWEEN $2 AND $3
			  AND (` + categoryConditions + `)
		`
		args = append([]interface{}{userID, startDate, endDate}, categoryArgs...)

	case domain.MatchLogicTagOnly:
		query = `
			SELECT COUNT(*), COALESCE(ABS(SUM(amount)), 0)
			FROM transactions
			WHERE user_id = $1 AND amount < 0
			  AND date BETWEEN $2 AND $3
			  AND tags ?| $4
		`
		args = []interface{}{userID, startDate, endDate, pq.Array(tags)}

	case domain.MatchLogicAnd:
		// Build OR clause for multiple categories
		categoryConditions, categoryArgs := r.buildCategoryConditions(categories)
		nextArgNum := 4 + len(categoryArgs)
		query = fmt.Sprintf(`
			SELECT COUNT(*), COALESCE(ABS(SUM(amount)), 0)
			FROM transactions
			WHERE user_id = $1 AND amount < 0
			  AND date BETWEEN $2 AND $3
			  AND (%s)
			  AND tags ?| $%d
		`, categoryConditions, nextArgNum)
		args = append([]interface{}{userID, startDate, endDate}, categoryArgs...)
		args = append(args, pq.Array(tags))

	case domain.MatchLogicOr:
		// Build OR clause for multiple categories
		categoryConditions, categoryArgs := r.buildCategoryConditions(categories)
		nextArgNum := 4 + len(categoryArgs)
		query = fmt.Sprintf(`
			SELECT COUNT(*), COALESCE(ABS(SUM(amount)), 0)
			FROM transactions
			WHERE user_id = $1 AND amount < 0
			  AND date BETWEEN $2 AND $3
			  AND (
				(%s)
				OR tags ?| $%d
			  )
		`, categoryConditions, nextArgNum)
		args = append([]interface{}{userID, startDate, endDate}, categoryArgs...)
		args = append(args, pq.Array(tags))

	default:
		return 0, 0, fmt.Errorf("unsupported match logic: %s", matchLogic)
	}

	var count int
	var sum float64
	err := r.db.QueryRow(ctx, query, args...).Scan(&count, &sum)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get matching transaction stats: %w", err)
	}

	return count, sum, nil
}

// buildCategoryConditions builds SQL OR conditions for multiple categories
// Returns the condition string and the arguments for the query
func (r *transactionRepository) buildCategoryConditions(categories [][]string) (string, []interface{}) {
	if len(categories) == 0 {
		return "1=0", nil // No categories means no matches
	}

	var conditions []string
	var args []interface{}
	argNum := 4 // Start after $1, $2, $3 (userID, startDate, endDate)

	for _, category := range categories {
		categoryJSON, err := json.Marshal(category)
		if err != nil {
			continue // Skip invalid categories
		}
		conditions = append(conditions, fmt.Sprintf("(category @> $%d::jsonb OR category_override @> $%d::jsonb)", argNum, argNum))
		args = append(args, string(categoryJSON))
		argNum++
	}

	if len(conditions) == 0 {
		return "1=0", nil
	}

	return strings.Join(conditions, " OR "), args
}

// =============================================================================
// Issue #499: Transaction-Budget Linking UI
// =============================================================================

// GetTransactionsForBudgetCategory returns paginated transactions matching the given category
// and/or tags based on match logic, along with total count and amount.
// Uses the same matching logic as GetSpentSumForBudgetCategory.
// Returns transactions ordered by date DESC.
func (r *transactionRepository) GetTransactionsForBudgetCategory(ctx context.Context, userID string, category []string, tags []string, matchLogic domain.MatchLogic, startDate, endDate time.Time, limit, offset int) ([]domain.Transaction, int, float64, error) {
	// Default to category_only if empty
	if matchLogic == "" {
		matchLogic = domain.MatchLogicCategoryOnly
	}

	// Marshal category to JSON for JSONB containment operator
	categoryJSON, err := json.Marshal(category)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("failed to marshal category: %w", err)
	}

	// Build WHERE clause based on match logic
	var whereClause string
	var baseArgs []interface{}

	switch matchLogic {
	case domain.MatchLogicCategoryOnly:
		whereClause = `user_id = $1 AND amount < 0
			  AND date BETWEEN $2 AND $3
			  AND (category @> $4::jsonb OR category_override @> $4::jsonb)`
		baseArgs = []interface{}{userID, startDate, endDate, string(categoryJSON)}

	case domain.MatchLogicTagOnly:
		whereClause = `user_id = $1 AND amount < 0
			  AND date BETWEEN $2 AND $3
			  AND tags ?| $4`
		baseArgs = []interface{}{userID, startDate, endDate, pq.Array(tags)}

	case domain.MatchLogicAnd:
		whereClause = `user_id = $1 AND amount < 0
			  AND date BETWEEN $2 AND $3
			  AND (category @> $4::jsonb OR category_override @> $4::jsonb)
			  AND tags ?| $5`
		baseArgs = []interface{}{userID, startDate, endDate, string(categoryJSON), pq.Array(tags)}

	case domain.MatchLogicOr:
		whereClause = `user_id = $1 AND amount < 0
			  AND date BETWEEN $2 AND $3
			  AND (
				(category @> $4::jsonb OR category_override @> $4::jsonb)
				OR tags ?| $5
			  )`
		baseArgs = []interface{}{userID, startDate, endDate, string(categoryJSON), pq.Array(tags)}

	default:
		return nil, 0, 0, fmt.Errorf("unsupported match logic: %s", matchLogic)
	}

	// First, get total count and sum
	countQuery := fmt.Sprintf(`
		SELECT COUNT(*), COALESCE(ABS(SUM(amount)), 0)
		FROM transactions
		WHERE %s
	`, whereClause)

	var totalCount int
	var totalAmount float64
	err = r.db.QueryRow(ctx, countQuery, baseArgs...).Scan(&totalCount, &totalAmount)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("failed to get transaction count: %w", err)
	}

	// Then, get the paginated transactions
	nextArgNum := len(baseArgs) + 1
	selectQuery := fmt.Sprintf(`
		SELECT id, user_id, account_id, linked_item_id, source, external_transaction_id,
			   external_metadata, amount, date, authorized_date, name, merchant_name,
			   category, personal_finance_category, payment_channel, pending, user_notes,
			   created_at, updated_at, tags, tags_source,
			   category_override, category_source, category_override_date, original_category
		FROM transactions
		WHERE %s
		ORDER BY date DESC, created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, nextArgNum, nextArgNum+1)

	selectArgs := append(baseArgs, limit, offset)
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
// Issue #171: Provides stable queries across re-links since user_account_id survives
// when a user re-links a bank (which assigns new item_id and account_id values).
func (r *transactionRepository) GetByUserAccountID(ctx context.Context, userID string, userAccountID string) ([]domain.Transaction, error) {
	query := `
		SELECT id, user_id, account_id, linked_item_id, user_account_id,
			   source, external_transaction_id, external_metadata,
			   amount, date, authorized_date, name, merchant_name,
			   category, personal_finance_category, payment_channel, pending, user_notes,
			   created_at, updated_at, tags, tags_source,
			   category_override, category_source, category_override_date, original_category
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
			&tx.Category,
			&tx.PersonalFinanceCategory,
			&tx.PaymentChannel,
			&tx.Pending,
			&tx.UserNotes,
			&tx.CreatedAt,
			&tx.UpdatedAt,
			&tagsJSON,
			&tagsSourceJSON,
			&tx.CategoryOverride,
			&categorySource,
			&tx.CategoryOverrideDate,
			&tx.OriginalCategory,
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
