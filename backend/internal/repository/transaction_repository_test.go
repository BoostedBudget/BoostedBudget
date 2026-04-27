package repository

import (
	"context"
	"errors"
	
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/lib/pq"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTransactionRepository_Create_Success tests successful transaction creation
func TestTransactionRepository_Create_Success(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	externalID := "plaid_tx_123"
	transaction := &domain.Transaction{
		UserID:                "user-123",
		AccountID:             "account-123",
		LinkedItemID:          "item-123",
		Source:                domain.SourcePlaid,
		ExternalTransactionID: &externalID,
		Amount:                -50.25,
		Date:                  time.Now(),
		Name:                  "Starbucks",
	}

	// Expect INSERT query (20 args: added external_transaction_id_blind_idx)
	mock.ExpectExec("INSERT INTO transactions").
		WithArgs(
			pgxmock.AnyArg(), // id (UUID generated)
			transaction.UserID,
			transaction.AccountID,
			transaction.LinkedItemID,
			string(transaction.Source),
			pgxmock.AnyArg(), // external_transaction_id (encrypted)
			pgxmock.AnyArg(), // external_transaction_id_blind_idx
			pgxmock.AnyArg(), // external_metadata (encrypted)
			transaction.Amount,
			pgxmock.AnyArg(), // date
			pgxmock.AnyArg(), // authorized_date
			transaction.Name,
			pgxmock.AnyArg(), // merchant_name
			pgxmock.AnyArg(), // category
			pgxmock.AnyArg(), // personal_finance_category
			pgxmock.AnyArg(), // payment_channel
			transaction.Pending,
			pgxmock.AnyArg(), // user_notes
			pgxmock.AnyArg(), // created_at
			pgxmock.AnyArg(), // updated_at
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err = repo.Create(ctx, transaction)
	assert.NoError(t, err)
	assert.NotEmpty(t, transaction.ID, "ID should be generated")
	assert.False(t, transaction.CreatedAt.IsZero(), "CreatedAt should be set")
	assert.False(t, transaction.UpdatedAt.IsZero(), "UpdatedAt should be set")

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_CreateBatch_Success tests batch transaction creation
func TestTransactionRepository_CreateBatch_Success(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	externalID1 := "plaid_tx_1"
	externalID2 := "plaid_tx_2"
	transactions := []domain.Transaction{
		{
			UserID:                "user-123",
			AccountID:             "account-123",
			LinkedItemID:          "item-123",
			Source:                domain.SourcePlaid,
			ExternalTransactionID: &externalID1,
			Amount:                -50.00,
			Date:                  time.Now(),
			Name:                  "Transaction 1",
		},
		{
			UserID:                "user-123",
			AccountID:             "account-123",
			LinkedItemID:          "item-123",
			Source:                domain.SourcePlaid,
			ExternalTransactionID: &externalID2,
			Amount:                -25.00,
			Date:                  time.Now(),
			Name:                  "Transaction 2",
		},
	}

	// Expect batch INSERT with multiple value sets (21 fields per transaction * 2 transactions = 42 args)
	// Issue #171: Added user_account_id as the 5th field; encryption adds external_transaction_id_blind_idx
	args := make([]interface{}, 42)
	for i := range args {
		args[i] = pgxmock.AnyArg()
	}
	mock.ExpectExec("INSERT INTO transactions").
		WithArgs(args...).
		WillReturnResult(pgxmock.NewResult("INSERT", 2))

	err = repo.CreateBatch(ctx, transactions)
	assert.NoError(t, err)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_Update_Success tests transaction update
func TestTransactionRepository_Update_Success(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	externalID := "plaid_tx_123"
	transaction := &domain.Transaction{
		ID:                    "tx-uuid-123",
		UserID:                "user-123",
		AccountID:             "account-123",
		LinkedItemID:          "item-123",
		Source:                domain.SourcePlaid,
		ExternalTransactionID: &externalID,
		Amount:                -55.00, // Updated amount
		Date:                  time.Now(),
		Name:                  "Starbucks - Updated",
		Pending:               false,
	}

	// Expect UPDATE query
	mock.ExpectExec("UPDATE transactions SET").
		WithArgs(
			transaction.Amount,
			pgxmock.AnyArg(), // date
			pgxmock.AnyArg(), // authorized_date
			transaction.Name,
			pgxmock.AnyArg(), // merchant_name
			pgxmock.AnyArg(), // category
			pgxmock.AnyArg(), // personal_finance_category
			pgxmock.AnyArg(), // payment_channel
			transaction.Pending,
			pgxmock.AnyArg(), // external_metadata
			pgxmock.AnyArg(), // user_notes
			pgxmock.AnyArg(), // updated_at
			transaction.ID,
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err = repo.Update(ctx, transaction)
	assert.NoError(t, err)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_DeleteByExternalID_Success tests transaction deletion
func TestTransactionRepository_DeleteByExternalID_Success(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	externalID := "plaid_tx_123"
	userID := "user-123"

	// Expect DELETE query using blind index (not raw externalID)
	mock.ExpectExec("DELETE FROM transactions").
		WithArgs(string(domain.SourcePlaid), pgxmock.AnyArg(), userID).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err = repo.DeleteByExternalID(ctx, domain.SourcePlaid, externalID, userID)
	assert.NoError(t, err)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetByUserID_Success tests retrieving transactions by user
func TestTransactionRepository_GetByUserID_Success(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	limit := 10
	offset := 0

	// Use pointers for nullable columns (pgxmock v4 requirement)
	extID1 := "plaid_tx_1"
	extID2 := "plaid_tx_2"
	now := time.Now()

	// Mock result rows
	rows := pgxmock.NewRows([]string{
		"id", "user_id", "account_id", "linked_item_id",
		"source", "external_transaction_id", "external_metadata",
		"amount", "date", "authorized_date", "name", "merchant_name",
		"category", "personal_finance_category", "payment_channel",
		"pending", "user_notes", "created_at", "updated_at",
		"tags", "tags_source",
		"category_override", "category_source", "category_override_date", "original_category",
	}).
		AddRow(
			"tx-1", userID, "account-1", "item-1",
			"plaid", &extID1, nil,
			-50.00, now, nil, "Transaction 1", nil,
			nil, nil, nil,
			false, nil, now, now,
			nil, nil,
			nil, nil, nil, nil,
		).
		AddRow(
			"tx-2", userID, "account-2", "item-1",
			"plaid", &extID2, nil,
			-25.00, now, nil, "Transaction 2", nil,
			nil, nil, nil,
			false, nil, now, now,
			nil, nil,
			nil, nil, nil, nil,
		)

	// Expect SELECT query
	mock.ExpectQuery("SELECT (.+) FROM transactions WHERE user_id").
		WithArgs(userID, limit, offset).
		WillReturnRows(rows)

	transactions, err := repo.GetByUserID(ctx, userID, limit, offset)
	assert.NoError(t, err)
	assert.Len(t, transactions, 2)
	assert.Equal(t, "Transaction 1", transactions[0].Name)
	assert.Equal(t, "Transaction 2", transactions[1].Name)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetByAccountID_Success tests retrieving transactions by account
func TestTransactionRepository_GetByAccountID_Success(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	accountID := "account-123"

	// Use pointers for nullable columns (pgxmock v4 requirement)
	extID := "plaid_tx_1"
	now := time.Now()

	// Mock result rows
	rows := pgxmock.NewRows([]string{
		"id", "user_id", "account_id", "linked_item_id",
		"source", "external_transaction_id", "external_metadata",
		"amount", "date", "authorized_date", "name", "merchant_name",
		"category", "personal_finance_category", "payment_channel",
		"pending", "user_notes", "created_at", "updated_at",
		"tags", "tags_source",
		"category_override", "category_source", "category_override_date", "original_category",
	}).
		AddRow(
			"tx-1", "user-1", accountID, "item-1",
			"plaid", &extID, nil,
			-50.00, now, nil, "Transaction 1", nil,
			nil, nil, nil,
			false, nil, now, now,
			nil, nil,
			nil, nil, nil, nil,
		)

	// Expect SELECT query
	mock.ExpectQuery("SELECT (.+) FROM transactions WHERE account_id").
		WithArgs(accountID).
		WillReturnRows(rows)

	transactions, err := repo.GetByAccountID(ctx, accountID)
	assert.NoError(t, err)
	assert.Len(t, transactions, 1)
	assert.Equal(t, accountID, transactions[0].AccountID)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetBySource_Success tests retrieving transactions by source
func TestTransactionRepository_GetBySource_Success(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	source := domain.SourcePlaid

	// Use pointers for nullable columns (pgxmock v4 requirement)
	extID := "plaid_tx_1"
	now := time.Now()

	// Mock result rows
	rows := pgxmock.NewRows([]string{
		"id", "user_id", "account_id", "linked_item_id",
		"source", "external_transaction_id", "external_metadata",
		"amount", "date", "authorized_date", "name", "merchant_name",
		"category", "personal_finance_category", "payment_channel",
		"pending", "user_notes", "created_at", "updated_at",
		"tags", "tags_source",
		"category_override", "category_source", "category_override_date", "original_category",
	}).
		AddRow(
			"tx-1", userID, "account-1", "item-1",
			"plaid", &extID, nil,
			-50.00, now, nil, "Transaction 1", nil,
			nil, nil, nil,
			false, nil, now, now,
			nil, nil,
			nil, nil, nil, nil,
		)

	// Expect SELECT query (source is passed as string, not domain type)
	mock.ExpectQuery("SELECT (.+) FROM transactions WHERE source").
		WithArgs(string(source), userID).
		WillReturnRows(rows)

	transactions, err := repo.GetBySource(ctx, source, userID)
	assert.NoError(t, err)
	assert.Len(t, transactions, 1)
	assert.Equal(t, source, transactions[0].Source)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetByUserIDWithFilters_NoFilters tests retrieving transactions without filters
func TestTransactionRepository_GetByUserIDWithFilters_NoFilters(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	limit := 10
	offset := 0
	filters := domain.TransactionFilters{} // No filters

	// Use pointers for nullable columns (pgxmock v4 requirement)
	extID := "plaid_tx_1"
	now := time.Now()

	// Mock result rows
	rows := pgxmock.NewRows([]string{
		"id", "user_id", "account_id", "linked_item_id",
		"source", "external_transaction_id", "external_metadata",
		"amount", "date", "authorized_date", "name", "merchant_name",
		"category", "personal_finance_category", "payment_channel",
		"pending", "user_notes", "created_at", "updated_at",
		"tags", "tags_source",
		"category_override", "category_source", "category_override_date", "original_category",
	}).
		AddRow(
			"tx-1", userID, "account-1", "item-1",
			"plaid", &extID, nil,
			-50.00, now, nil, "Transaction 1", nil,
			nil, nil, nil,
			false, nil, now, now,
			nil, nil,
			nil, nil, nil, nil,
		)

	// Expect SELECT query with only user_id filter
	mock.ExpectQuery("SELECT (.+) FROM transactions WHERE user_id").
		WithArgs(userID, limit, offset).
		WillReturnRows(rows)

	transactions, err := repo.GetByUserIDWithFilters(ctx, userID, filters, limit, offset)
	assert.NoError(t, err)
	assert.Len(t, transactions, 1)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetByUserIDWithFilters_DateRange tests filtering by date range
func TestTransactionRepository_GetByUserIDWithFilters_DateRange(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	limit := 10
	offset := 0
	startDate := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)
	filters := domain.TransactionFilters{
		StartDate: &startDate,
		EndDate:   &endDate,
	}

	// Use pointers for nullable columns (pgxmock v4 requirement)
	extID := "plaid_tx_1"
	txDate := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)
	now := time.Now()

	// Mock result rows
	rows := pgxmock.NewRows([]string{
		"id", "user_id", "account_id", "linked_item_id",
		"source", "external_transaction_id", "external_metadata",
		"amount", "date", "authorized_date", "name", "merchant_name",
		"category", "personal_finance_category", "payment_channel",
		"pending", "user_notes", "created_at", "updated_at",
		"tags", "tags_source",
		"category_override", "category_source", "category_override_date", "original_category",
	}).
		AddRow(
			"tx-1", userID, "account-1", "item-1",
			"plaid", &extID, nil,
			-50.00, txDate, nil, "Transaction 1", nil,
			nil, nil, nil,
			false, nil, now, now,
			nil, nil,
			nil, nil, nil, nil,
		)

	// Expect SELECT query with date range filter
	mock.ExpectQuery("SELECT (.+) FROM transactions WHERE user_id(.+)AND date >= (.+)AND date <= ").
		WithArgs(userID, startDate, endDate, limit, offset).
		WillReturnRows(rows)

	transactions, err := repo.GetByUserIDWithFilters(ctx, userID, filters, limit, offset)
	assert.NoError(t, err)
	assert.Len(t, transactions, 1)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetByUserIDWithFilters_AccountID tests filtering by account
func TestTransactionRepository_GetByUserIDWithFilters_AccountID(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	accountID := "account-456"
	limit := 10
	offset := 0
	filters := domain.TransactionFilters{
		AccountID: &accountID,
	}

	// Use pointers for nullable columns (pgxmock v4 requirement)
	extID := "plaid_tx_1"
	now := time.Now()

	// Mock result rows
	rows := pgxmock.NewRows([]string{
		"id", "user_id", "account_id", "linked_item_id",
		"source", "external_transaction_id", "external_metadata",
		"amount", "date", "authorized_date", "name", "merchant_name",
		"category", "personal_finance_category", "payment_channel",
		"pending", "user_notes", "created_at", "updated_at",
		"tags", "tags_source",
		"category_override", "category_source", "category_override_date", "original_category",
	}).
		AddRow(
			"tx-1", userID, accountID, "item-1",
			"plaid", &extID, nil,
			-50.00, now, nil, "Transaction 1", nil,
			nil, nil, nil,
			false, nil, now, now,
			nil, nil,
			nil, nil, nil, nil,
		)

	// Expect SELECT query with account_id filter
	mock.ExpectQuery("SELECT (.+) FROM transactions WHERE user_id(.+)AND account_id").
		WithArgs(userID, accountID, limit, offset).
		WillReturnRows(rows)

	transactions, err := repo.GetByUserIDWithFilters(ctx, userID, filters, limit, offset)
	assert.NoError(t, err)
	assert.Len(t, transactions, 1)
	assert.Equal(t, accountID, transactions[0].AccountID)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetByUserIDWithFilters_Search tests searching by merchant/name
func TestTransactionRepository_GetByUserIDWithFilters_Search(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	searchQuery := "starbucks"
	limit := 10
	offset := 0
	filters := domain.TransactionFilters{
		Search: &searchQuery,
	}

	// Use pointers for nullable columns (pgxmock v4 requirement)
	merchantName := "Starbucks Coffee"
	extID := "plaid_tx_1"
	now := time.Now()

	// Mock result rows
	rows := pgxmock.NewRows([]string{
		"id", "user_id", "account_id", "linked_item_id",
		"source", "external_transaction_id", "external_metadata",
		"amount", "date", "authorized_date", "name", "merchant_name",
		"category", "personal_finance_category", "payment_channel",
		"pending", "user_notes", "created_at", "updated_at",
		"tags", "tags_source",
		"category_override", "category_source", "category_override_date", "original_category",
	}).
		AddRow(
			"tx-1", userID, "account-1", "item-1",
			"plaid", &extID, nil,
			-5.49, now, nil, "Starbucks", &merchantName,
			nil, nil, nil,
			false, nil, now, now,
			nil, nil,
			nil, nil, nil, nil,
		)

	// Expect SELECT query with ILIKE search (case-insensitive)
	// Note: Search pattern added twice for both name and merchant_name ILIKE conditions
	mock.ExpectQuery("SELECT (.+) FROM transactions WHERE user_id(.+)AND \\(name ILIKE (.+)OR merchant_name ILIKE").
		WithArgs(userID, "%"+searchQuery+"%", "%"+searchQuery+"%", limit, offset).
		WillReturnRows(rows)

	transactions, err := repo.GetByUserIDWithFilters(ctx, userID, filters, limit, offset)
	assert.NoError(t, err)
	assert.Len(t, transactions, 1)
	assert.Contains(t, transactions[0].Name, "Starbucks")

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetByUserIDWithFilters_Category tests filtering by category
func TestTransactionRepository_GetByUserIDWithFilters_Category(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	categoryFilter := "Food and Drink"
	limit := 10
	offset := 0
	filters := domain.TransactionFilters{
		Category: &categoryFilter,
	}

	// Use pointers for nullable columns (pgxmock v4 requirement)
	categoryJSON := `["Food and Drink", "Restaurants"]`
	extID := "plaid_tx_1"
	now := time.Now()

	// Mock result rows
	rows := pgxmock.NewRows([]string{
		"id", "user_id", "account_id", "linked_item_id",
		"source", "external_transaction_id", "external_metadata",
		"amount", "date", "authorized_date", "name", "merchant_name",
		"category", "personal_finance_category", "payment_channel",
		"pending", "user_notes", "created_at", "updated_at",
		"tags", "tags_source",
		"category_override", "category_source", "category_override_date", "original_category",
	}).
		AddRow(
			"tx-1", userID, "account-1", "item-1",
			"plaid", &extID, nil,
			-25.00, now, nil, "Restaurant", nil,
			&categoryJSON, nil, nil,
			false, nil, now, now,
			nil, nil,
			nil, nil, nil, nil,
		)

	// Expect SELECT query with category filter (JSON contains check)
	mock.ExpectQuery("SELECT (.+) FROM transactions WHERE user_id(.+)AND category::text ILIKE").
		WithArgs(userID, "%"+categoryFilter+"%", limit, offset).
		WillReturnRows(rows)

	transactions, err := repo.GetByUserIDWithFilters(ctx, userID, filters, limit, offset)
	assert.NoError(t, err)
	assert.Len(t, transactions, 1)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetByUserIDWithFilters_Pending tests filtering by pending status
func TestTransactionRepository_GetByUserIDWithFilters_Pending(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	pendingOnly := true
	limit := 10
	offset := 0
	filters := domain.TransactionFilters{
		Pending: &pendingOnly,
	}

	// Use pointers for nullable columns (pgxmock v4 requirement)
	extID := "plaid_tx_1"
	now := time.Now()

	// Mock result rows
	rows := pgxmock.NewRows([]string{
		"id", "user_id", "account_id", "linked_item_id",
		"source", "external_transaction_id", "external_metadata",
		"amount", "date", "authorized_date", "name", "merchant_name",
		"category", "personal_finance_category", "payment_channel",
		"pending", "user_notes", "created_at", "updated_at",
		"tags", "tags_source",
		"category_override", "category_source", "category_override_date", "original_category",
	}).
		AddRow(
			"tx-1", userID, "account-1", "item-1",
			"plaid", &extID, nil,
			-100.00, now, nil, "Pending Transaction", nil,
			nil, nil, nil,
			true, nil, now, now,
			nil, nil,
			nil, nil, nil, nil,
		)

	// Expect SELECT query with pending filter
	mock.ExpectQuery("SELECT (.+) FROM transactions WHERE user_id(.+)AND pending").
		WithArgs(userID, pendingOnly, limit, offset).
		WillReturnRows(rows)

	transactions, err := repo.GetByUserIDWithFilters(ctx, userID, filters, limit, offset)
	assert.NoError(t, err)
	assert.Len(t, transactions, 1)
	assert.True(t, transactions[0].Pending)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetByUserIDWithFilters_MultipleFilters tests combining multiple filters
func TestTransactionRepository_GetByUserIDWithFilters_MultipleFilters(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	accountID := "account-456"
	startDate := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	pendingOnly := false
	limit := 10
	offset := 0
	filters := domain.TransactionFilters{
		AccountID: &accountID,
		StartDate: &startDate,
		Pending:   &pendingOnly,
	}

	// Use pointers for nullable columns (pgxmock v4 requirement)
	extID := "plaid_tx_1"
	txDate := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)
	now := time.Now()

	// Mock result rows
	rows := pgxmock.NewRows([]string{
		"id", "user_id", "account_id", "linked_item_id",
		"source", "external_transaction_id", "external_metadata",
		"amount", "date", "authorized_date", "name", "merchant_name",
		"category", "personal_finance_category", "payment_channel",
		"pending", "user_notes", "created_at", "updated_at",
		"tags", "tags_source",
		"category_override", "category_source", "category_override_date", "original_category",
	}).
		AddRow(
			"tx-1", userID, accountID, "item-1",
			"plaid", &extID, nil,
			-50.00, txDate, nil, "Transaction 1", nil,
			nil, nil, nil,
			false, nil, now, now,
			nil, nil,
			nil, nil, nil, nil,
		)

	// Expect SELECT query with multiple filters (account_id AND date >= AND pending)
	mock.ExpectQuery("SELECT (.+) FROM transactions WHERE user_id(.+)AND account_id(.+)AND date >=(.+)AND pending").
		WithArgs(userID, accountID, startDate, pendingOnly, limit, offset).
		WillReturnRows(rows)

	transactions, err := repo.GetByUserIDWithFilters(ctx, userID, filters, limit, offset)
	assert.NoError(t, err)
	assert.Len(t, transactions, 1)
	assert.Equal(t, accountID, transactions[0].AccountID)
	assert.False(t, transactions[0].Pending)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_CountByUserID_NoFilters tests counting transactions without filters
func TestTransactionRepository_CountByUserID_NoFilters(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	filters := domain.TransactionFilters{}

	// Mock result rows
	rows := pgxmock.NewRows([]string{"count"}).AddRow(42)

	// Expect COUNT query
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM transactions WHERE user_id").
		WithArgs(userID).
		WillReturnRows(rows)

	count, err := repo.CountByUserID(ctx, userID, filters)
	assert.NoError(t, err)
	assert.Equal(t, 42, count)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_CountByUserID_WithFilters tests counting transactions with filters
func TestTransactionRepository_CountByUserID_WithFilters(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	accountID := "account-456"
	pendingOnly := true
	filters := domain.TransactionFilters{
		AccountID: &accountID,
		Pending:   &pendingOnly,
	}

	// Mock result rows
	rows := pgxmock.NewRows([]string{"count"}).AddRow(5)

	// Expect COUNT query with filters
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM transactions WHERE user_id(.+)AND account_id(.+)AND pending").
		WithArgs(userID, accountID, pendingOnly).
		WillReturnRows(rows)

	count, err := repo.CountByUserID(ctx, userID, filters)
	assert.NoError(t, err)
	assert.Equal(t, 5, count)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetByExternalID_Success tests retrieving a transaction by external ID
func TestTransactionRepository_GetByExternalID_Success(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	externalID := "plaid_tx_123"
	userID := "user-123"
	source := domain.SourcePlaid

	// Use pointers for nullable columns (pgxmock v4 requirement)
	merchantName := "Starbucks Coffee"
	categoryJSON := `["Food and Drink", "Coffee"]`
	now := time.Now()

	// Mock result row
	rows := pgxmock.NewRows([]string{
		"id", "user_id", "account_id", "linked_item_id",
		"source", "external_transaction_id", "external_metadata",
		"amount", "date", "authorized_date", "name", "merchant_name",
		"category", "personal_finance_category", "payment_channel",
		"pending", "user_notes", "created_at", "updated_at",
		"tags", "tags_source",
		"category_override", "category_source", "category_override_date", "original_category",
	}).
		AddRow(
			"tx-uuid-123", userID, "account-456", "item-789",
			"plaid", &externalID, nil,
			-25.50, now, nil, "Starbucks", &merchantName,
			&categoryJSON, nil, nil,
			false, nil, now, now,
			nil, nil,
			nil, nil, nil, nil,
		)

	// Expect SELECT query with WHERE source = $1 AND external_transaction_id_blind_idx = $2 AND user_id = $3
	mock.ExpectQuery("SELECT (.+) FROM transactions WHERE source (.+) AND external_transaction_id_blind_idx (.+) AND user_id").
		WithArgs(string(source), pgxmock.AnyArg(), userID).
		WillReturnRows(rows)

	transaction, err := repo.GetByExternalID(ctx, source, externalID, userID)
	assert.NoError(t, err)
	assert.NotNil(t, transaction)
	assert.Equal(t, "tx-uuid-123", transaction.ID)
	assert.Equal(t, externalID, *transaction.ExternalTransactionID)
	assert.Equal(t, userID, transaction.UserID)
	assert.Equal(t, source, transaction.Source)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetByExternalID_NotFound tests when transaction doesn't exist
func TestTransactionRepository_GetByExternalID_NotFound(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	externalID := "nonexistent_tx"
	userID := "user-123"
	source := domain.SourcePlaid

	// Expect SELECT query to return pgx.ErrNoRows (which gets converted to ErrTransactionNotFound)
	mock.ExpectQuery("SELECT (.+) FROM transactions WHERE source (.+) AND external_transaction_id_blind_idx (.+) AND user_id").
		WithArgs(string(source), pgxmock.AnyArg(), userID).
		WillReturnError(pgx.ErrNoRows)

	transaction, err := repo.GetByExternalID(ctx, source, externalID, userID)
	assert.Error(t, err)
	assert.Equal(t, ErrTransactionNotFound, err)
	assert.Nil(t, transaction)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetByUserIDWithFilters_ReturnsTags tests that tags are returned
// TDD RED: This test will FAIL until GetByUserIDWithFilters includes tags in SELECT
func TestTransactionRepository_GetByUserIDWithFilters_ReturnsTags(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	limit := 10
	offset := 0
	filters := domain.TransactionFilters{}

	// Use pointers for nullable columns (pgxmock v4 requirement)
	extID := "plaid_tx_1"
	now := time.Now()

	// Mock result rows including tags and tags_source
	rows := pgxmock.NewRows([]string{
		"id", "user_id", "account_id", "linked_item_id",
		"source", "external_transaction_id", "external_metadata",
		"amount", "date", "authorized_date", "name", "merchant_name",
		"category", "personal_finance_category", "payment_channel",
		"pending", "user_notes", "created_at", "updated_at",
		"tags", "tags_source",
		"category_override", "category_source", "category_override_date", "original_category",
	}).
		AddRow(
			"tx-1", userID, "account-1", "item-1",
			"plaid", &extID, nil,
			-50.00, now, nil, "Transaction 1", nil,
			nil, nil, nil,
			false, nil, now, now,
			[]byte(`["Food", "Shopping"]`), []byte(`[{"tag":"Food","source":"user"},{"tag":"Shopping","source":"user"}]`),
			nil, nil, nil, nil,
		)

	mock.ExpectQuery("SELECT (.+) FROM transactions WHERE user_id").
		WithArgs(userID, limit, offset).
		WillReturnRows(rows)

	transactions, err := repo.GetByUserIDWithFilters(ctx, userID, filters, limit, offset)
	assert.NoError(t, err)
	require.Len(t, transactions, 1)

	// Assert tags are returned
	assert.Equal(t, []string{"Food", "Shopping"}, transactions[0].Tags)
	assert.Len(t, transactions[0].TagsSource, 2)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetByUserIDWithFilters_ReturnsCategoryOverride tests category_override fields
// TDD RED: This test will FAIL until GetByUserIDWithFilters includes category_override in SELECT
func TestTransactionRepository_GetByUserIDWithFilters_ReturnsCategoryOverride(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	limit := 10
	offset := 0
	filters := domain.TransactionFilters{}

	// Use pointers for nullable columns (pgxmock v4 requirement)
	extID := "plaid_tx_1"
	now := time.Now()
	overrideDate := now
	categoryJSON := `["Food and Drink"]`
	categoryOverride := `["Custom Category"]`
	originalCategory := `["Food and Drink"]`

	// Mock result rows including category_override fields
	rows := pgxmock.NewRows([]string{
		"id", "user_id", "account_id", "linked_item_id",
		"source", "external_transaction_id", "external_metadata",
		"amount", "date", "authorized_date", "name", "merchant_name",
		"category", "personal_finance_category", "payment_channel",
		"pending", "user_notes", "created_at", "updated_at",
		"tags", "tags_source",
		"category_override", "category_source", "category_override_date", "original_category",
	}).
		AddRow(
			"tx-1", userID, "account-1", "item-1",
			"plaid", &extID, nil,
			-50.00, now, nil, "Transaction 1", nil,
			&categoryJSON, nil, nil,
			false, nil, now, now,
			nil, nil,
			&categoryOverride, "user", &overrideDate, &originalCategory,
		)

	mock.ExpectQuery("SELECT (.+) FROM transactions WHERE user_id").
		WithArgs(userID, limit, offset).
		WillReturnRows(rows)

	transactions, err := repo.GetByUserIDWithFilters(ctx, userID, filters, limit, offset)
	assert.NoError(t, err)
	require.Len(t, transactions, 1)

	// Assert category_override fields are returned
	assert.NotNil(t, transactions[0].CategoryOverride)
	assert.Equal(t, `["Custom Category"]`, *transactions[0].CategoryOverride)
	assert.Equal(t, domain.CategorySource("user"), transactions[0].CategorySourceField)
	assert.NotNil(t, transactions[0].CategoryOverrideDate)
	assert.NotNil(t, transactions[0].OriginalCategory)
	assert.Equal(t, `["Food and Drink"]`, *transactions[0].OriginalCategory)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetByID_ReturnsCategoryOverrideFields tests GetByID returns all category fields
// TDD RED: This test will FAIL until GetByID includes category_override fields in SELECT
func TestTransactionRepository_GetByID_ReturnsCategoryOverrideFields(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	txID := "tx-123"

	// Use pointers for nullable columns (pgxmock v4 requirement)
	extID := "plaid_tx_1"
	now := time.Now()
	overrideDate := now
	categoryJSON := `["Food and Drink"]`
	categoryOverride := `["Custom Category"]`
	originalCategory := `["Food and Drink"]`

	// Mock result row including all fields
	rows := pgxmock.NewRows([]string{
		"id", "user_id", "account_id", "linked_item_id", "source", "external_transaction_id",
		"external_metadata", "amount", "date", "authorized_date", "name", "merchant_name",
		"category", "personal_finance_category", "payment_channel", "pending", "user_notes",
		"created_at", "updated_at", "tags", "tags_source",
		"category_override", "category_source", "category_override_date", "original_category",
	}).
		AddRow(
			txID, "user-123", "account-1", "item-1", "plaid", &extID,
			nil, -50.00, now, nil, "Transaction 1", nil,
			&categoryJSON, nil, nil, false, nil,
			now, now, []byte(`["Food"]`), []byte(`[{"tag":"Food","source":"user"}]`),
			&categoryOverride, "user", &overrideDate, &originalCategory,
		)

	mock.ExpectQuery("SELECT (.+) FROM transactions WHERE id").
		WithArgs(txID).
		WillReturnRows(rows)

	transaction, err := repo.GetByID(ctx, txID)
	assert.NoError(t, err)
	require.NotNil(t, transaction)

	// Assert tags are returned
	assert.Equal(t, []string{"Food"}, transaction.Tags)

	// Assert category_override fields are returned
	assert.NotNil(t, transaction.CategoryOverride)
	assert.Equal(t, `["Custom Category"]`, *transaction.CategoryOverride)
	assert.Equal(t, domain.CategorySource("user"), transaction.CategorySourceField)
	assert.NotNil(t, transaction.CategoryOverrideDate)
	assert.NotNil(t, transaction.OriginalCategory)
	assert.Equal(t, `["Food and Drink"]`, *transaction.OriginalCategory)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_UpdateCategory_PreservesOriginalCategory tests that UpdateCategory
// preserves original_category using COALESCE when it's NULL
// TDD RED: This test will FAIL until UpdateCategory SQL includes COALESCE for original_category
func TestTransactionRepository_UpdateCategory_PreservesOriginalCategory(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	txID := "tx-123"
	userID := "user-123"
	categoryOverride := []string{"Custom", "Category"}

	// Expect the UPDATE query with COALESCE for original_category
	// This ensures the first manual edit preserves the original Plaid category
	mock.ExpectExec("UPDATE transactions SET category_override.*original_category = COALESCE\\(original_category, category\\)").
		WithArgs(
			pgxmock.AnyArg(), // category_override JSON
			domain.CategorySource("user"),
			pgxmock.AnyArg(), // category_override_date
			pgxmock.AnyArg(), // updated_at
			txID,
			userID,
		).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err = repo.UpdateCategory(ctx, txID, userID, categoryOverride, domain.CategorySource("user"))
	assert.NoError(t, err)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetDistinctPlaidCategories_Success tests retrieving distinct Plaid categories
// Returns primary category from personal_finance_category JSONB (Plaid v2 format)
func TestTransactionRepository_GetDistinctPlaidCategories_Success(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"

	// Mock result rows - distinct primary categories from personal_finance_category JSONB
	rows := pgxmock.NewRows([]string{"cat"}).
		AddRow("FOOD_AND_DRINK").
		AddRow("GENERAL_MERCHANDISE").
		AddRow("TRAVEL")

	// Expect SELECT DISTINCT query that extracts primary category from personal_finance_category
	mock.ExpectQuery("SELECT DISTINCT").
		WithArgs(userID).
		WillReturnRows(rows)

	categories, err := repo.GetDistinctPlaidCategories(ctx, userID)
	assert.NoError(t, err)
	assert.Len(t, categories, 3)
	assert.Contains(t, categories, "FOOD_AND_DRINK")
	assert.Contains(t, categories, "GENERAL_MERCHANDISE")
	assert.Contains(t, categories, "TRAVEL")

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetDistinctPlaidCategories_Empty tests when user has no categorized transactions
func TestTransactionRepository_GetDistinctPlaidCategories_Empty(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-no-transactions"

	// Mock empty result
	rows := pgxmock.NewRows([]string{"cat"})

	mock.ExpectQuery("SELECT DISTINCT").
		WithArgs(userID).
		WillReturnRows(rows)

	categories, err := repo.GetDistinctPlaidCategories(ctx, userID)
	assert.NoError(t, err)
	assert.Empty(t, categories)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// =============================================================================
// Issue #492: Income Source Matching Service Tests
// =============================================================================

// TestTransactionRepository_GetIncomeSumByCategory_Success tests successful sum calculation for matching category
func TestTransactionRepository_GetIncomeSumByCategory_Success(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	category := "Paycheck"
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	expectedSum := 5000.00

	// Category is formatted as JSON array: ["Paycheck"]
	rows := pgxmock.NewRows([]string{"sum"}).AddRow(expectedSum)

	mock.ExpectQuery(`SELECT COALESCE\(SUM\(amount\), 0\)`).
		WithArgs(userID, startDate, endDate, `["Paycheck"]`, category).
		WillReturnRows(rows)

	sum, err := repo.GetIncomeSumByCategory(ctx, userID, category, startDate, endDate)
	assert.NoError(t, err)
	assert.Equal(t, expectedSum, sum)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetIncomeSumByCategory_NoMatches tests returning 0 when no transactions match
func TestTransactionRepository_GetIncomeSumByCategory_NoMatches(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	category := "NonExistentCategory"
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)

	// COALESCE ensures 0 is returned for no matches
	rows := pgxmock.NewRows([]string{"sum"}).AddRow(0.0)

	mock.ExpectQuery(`SELECT COALESCE\(SUM\(amount\), 0\)`).
		WithArgs(userID, startDate, endDate, `["NonExistentCategory"]`, category).
		WillReturnRows(rows)

	sum, err := repo.GetIncomeSumByCategory(ctx, userID, category, startDate, endDate)
	assert.NoError(t, err)
	assert.Equal(t, 0.0, sum)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetIncomeSumByCategory_DBError tests database error handling
func TestTransactionRepository_GetIncomeSumByCategory_DBError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	category := "Paycheck"
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)

	mock.ExpectQuery(`SELECT COALESCE\(SUM\(amount\), 0\)`).
		WithArgs(userID, startDate, endDate, `["Paycheck"]`, category).
		WillReturnError(errors.New("connection done"))

	sum, err := repo.GetIncomeSumByCategory(ctx, userID, category, startDate, endDate)
	assert.Error(t, err)
	assert.Equal(t, 0.0, sum)
	assert.Contains(t, err.Error(), "failed to get income sum by category")

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetIncomeSumByCategory_PFCPrimary tests that the PFC primary enum
// is matched directly via personal_finance_category->>'primary' = $5.
func TestTransactionRepository_GetIncomeSumByCategory_PFCPrimary(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	category := "INCOME"
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	expectedSum := 4200.00

	rows := pgxmock.NewRows([]string{"sum"}).AddRow(expectedSum)

	// $4 is the JSONB array form, $5 is the raw enum string for personal_finance_category->>'primary'
	mock.ExpectQuery(`SELECT COALESCE\(SUM\(amount\), 0\)`).
		WithArgs(userID, startDate, endDate, `["INCOME"]`, category).
		WillReturnRows(rows)

	sum, err := repo.GetIncomeSumByCategory(ctx, userID, category, startDate, endDate)
	assert.NoError(t, err)
	assert.Equal(t, expectedSum, sum)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetIncomeSumByTag_Success tests successful sum calculation for matching tag
func TestTransactionRepository_GetIncomeSumByTag_Success(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	tag := "salary"
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	expectedSum := 3000.00

	rows := pgxmock.NewRows([]string{"sum"}).AddRow(expectedSum)

	mock.ExpectQuery(`SELECT COALESCE\(SUM\(amount\), 0\)`).
		WithArgs(userID, startDate, endDate, tag).
		WillReturnRows(rows)

	sum, err := repo.GetIncomeSumByTag(ctx, userID, tag, startDate, endDate)
	assert.NoError(t, err)
	assert.Equal(t, expectedSum, sum)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetIncomeSumByTag_NoMatches tests returning 0 when no transactions have the tag
func TestTransactionRepository_GetIncomeSumByTag_NoMatches(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	tag := "nonexistent-tag"
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)

	rows := pgxmock.NewRows([]string{"sum"}).AddRow(0.0)

	mock.ExpectQuery(`SELECT COALESCE\(SUM\(amount\), 0\)`).
		WithArgs(userID, startDate, endDate, tag).
		WillReturnRows(rows)

	sum, err := repo.GetIncomeSumByTag(ctx, userID, tag, startDate, endDate)
	assert.NoError(t, err)
	assert.Equal(t, 0.0, sum)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetIncomeSumByTag_DBError tests database error handling
func TestTransactionRepository_GetIncomeSumByTag_DBError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	tag := "salary"
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)

	mock.ExpectQuery(`SELECT COALESCE\(SUM\(amount\), 0\)`).
		WithArgs(userID, startDate, endDate, tag).
		WillReturnError(errors.New("connection done"))

	sum, err := repo.GetIncomeSumByTag(ctx, userID, tag, startDate, endDate)
	assert.Error(t, err)
	assert.Equal(t, 0.0, sum)
	assert.Contains(t, err.Error(), "failed to get income sum by tag")

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// =============================================================================
// Issue #493: GetSpentSumForBudgetCategory Tests
// =============================================================================

// TestTransactionRepository_GetSpentSumForBudgetCategory_CategoryOnly tests
// category_only match logic - matches transactions by category containment
func TestTransactionRepository_GetSpentSumForBudgetCategory_CategoryOnly(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	category := []string{"Food and Drink"}
	tags := []string{} // Not used for category_only
	matchLogic := domain.MatchLogicCategoryOnly
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	expectedSum := 250.50

	rows := pgxmock.NewRows([]string{"sum"}).AddRow(expectedSum)

	// Match the category_only SQL query pattern
	mock.ExpectQuery(`SELECT COALESCE\(ABS\(SUM\(amount\)\), 0\)`).
		WithArgs(userID, startDate, endDate, `["Food and Drink"]`).
		WillReturnRows(rows)

	sum, err := repo.GetSpentSumForBudgetCategory(ctx, userID, category, tags, matchLogic, startDate, endDate)
	assert.NoError(t, err)
	assert.Equal(t, expectedSum, sum)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetSpentSumForBudgetCategory_TagOnly tests
// tag_only match logic - matches transactions by tag existence
func TestTransactionRepository_GetSpentSumForBudgetCategory_TagOnly(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	category := []string{} // Not used for tag_only
	tags := []string{"groceries", "food"}
	matchLogic := domain.MatchLogicTagOnly
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	expectedSum := 175.25

	rows := pgxmock.NewRows([]string{"sum"}).AddRow(expectedSum)

	// Match the tag_only SQL query pattern with pq.Array
	mock.ExpectQuery(`SELECT COALESCE\(ABS\(SUM\(amount\)\), 0\)`).
		WithArgs(userID, startDate, endDate, pgxmock.AnyArg()). // pq.Array produces custom type
		WillReturnRows(rows)

	sum, err := repo.GetSpentSumForBudgetCategory(ctx, userID, category, tags, matchLogic, startDate, endDate)
	assert.NoError(t, err)
	assert.Equal(t, expectedSum, sum)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetSpentSumForBudgetCategory_And tests
// and match logic - requires BOTH category AND tag to match
func TestTransactionRepository_GetSpentSumForBudgetCategory_And(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	category := []string{"Food and Drink", "Restaurants"}
	tags := []string{"dinner", "date-night"}
	matchLogic := domain.MatchLogicAnd
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	expectedSum := 125.00

	rows := pgxmock.NewRows([]string{"sum"}).AddRow(expectedSum)

	// Match the and SQL query pattern with both category and tags
	mock.ExpectQuery(`SELECT COALESCE\(ABS\(SUM\(amount\)\), 0\)`).
		WithArgs(userID, startDate, endDate, `["Food and Drink","Restaurants"]`, pgxmock.AnyArg()). // category JSON + pq.Array
		WillReturnRows(rows)

	sum, err := repo.GetSpentSumForBudgetCategory(ctx, userID, category, tags, matchLogic, startDate, endDate)
	assert.NoError(t, err)
	assert.Equal(t, expectedSum, sum)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetSpentSumForBudgetCategory_Or tests
// or match logic - matches if EITHER category OR tag matches
func TestTransactionRepository_GetSpentSumForBudgetCategory_Or(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	category := []string{"Entertainment"}
	tags := []string{"streaming", "subscriptions"}
	matchLogic := domain.MatchLogicOr
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	expectedSum := 89.97

	rows := pgxmock.NewRows([]string{"sum"}).AddRow(expectedSum)

	// Match the or SQL query pattern with both category and tags
	mock.ExpectQuery(`SELECT COALESCE\(ABS\(SUM\(amount\)\), 0\)`).
		WithArgs(userID, startDate, endDate, `["Entertainment"]`, pgxmock.AnyArg()). // category JSON + pq.Array
		WillReturnRows(rows)

	sum, err := repo.GetSpentSumForBudgetCategory(ctx, userID, category, tags, matchLogic, startDate, endDate)
	assert.NoError(t, err)
	assert.Equal(t, expectedSum, sum)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetSpentSumForBudgetCategory_EmptyMatchLogic tests
// that empty match_logic defaults to category_only behavior
func TestTransactionRepository_GetSpentSumForBudgetCategory_EmptyMatchLogic(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	category := []string{"Shopping"}
	tags := []string{}
	matchLogic := domain.MatchLogic("") // Empty defaults to category_only
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	expectedSum := 500.00

	rows := pgxmock.NewRows([]string{"sum"}).AddRow(expectedSum)

	// Should use category_only query pattern
	mock.ExpectQuery(`SELECT COALESCE\(ABS\(SUM\(amount\)\), 0\)`).
		WithArgs(userID, startDate, endDate, `["Shopping"]`).
		WillReturnRows(rows)

	sum, err := repo.GetSpentSumForBudgetCategory(ctx, userID, category, tags, matchLogic, startDate, endDate)
	assert.NoError(t, err)
	assert.Equal(t, expectedSum, sum)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetSpentSumForBudgetCategory_NoMatches tests
// that COALESCE returns 0 when no transactions match
func TestTransactionRepository_GetSpentSumForBudgetCategory_NoMatches(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	category := []string{"NonExistentCategory"}
	tags := []string{}
	matchLogic := domain.MatchLogicCategoryOnly
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)

	// COALESCE ensures 0 is returned for no matches
	rows := pgxmock.NewRows([]string{"sum"}).AddRow(0.0)

	mock.ExpectQuery(`SELECT COALESCE\(ABS\(SUM\(amount\)\), 0\)`).
		WithArgs(userID, startDate, endDate, `["NonExistentCategory"]`).
		WillReturnRows(rows)

	sum, err := repo.GetSpentSumForBudgetCategory(ctx, userID, category, tags, matchLogic, startDate, endDate)
	assert.NoError(t, err)
	assert.Equal(t, 0.0, sum)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetSpentSumForBudgetCategory_DBError tests
// database error handling
func TestTransactionRepository_GetSpentSumForBudgetCategory_DBError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	category := []string{"Food and Drink"}
	tags := []string{}
	matchLogic := domain.MatchLogicCategoryOnly
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)

	mock.ExpectQuery(`SELECT COALESCE\(ABS\(SUM\(amount\)\), 0\)`).
		WithArgs(userID, startDate, endDate, `["Food and Drink"]`).
		WillReturnError(errors.New("connection done"))

	sum, err := repo.GetSpentSumForBudgetCategory(ctx, userID, category, tags, matchLogic, startDate, endDate)
	assert.Error(t, err)
	assert.Equal(t, 0.0, sum)
	assert.Contains(t, err.Error(), "failed to get spent sum for budget category")

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetSpentSumForBudgetCategory_HierarchicalCategory tests
// that hierarchical categories (e.g., ["Food and Drink", "Restaurants"]) work correctly
func TestTransactionRepository_GetSpentSumForBudgetCategory_HierarchicalCategory(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	// Budget category with full hierarchy
	category := []string{"Food and Drink", "Restaurants"}
	tags := []string{}
	matchLogic := domain.MatchLogicCategoryOnly
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	expectedSum := 350.00

	rows := pgxmock.NewRows([]string{"sum"}).AddRow(expectedSum)

	// Category should be marshaled as full JSON array
	mock.ExpectQuery(`SELECT COALESCE\(ABS\(SUM\(amount\)\), 0\)`).
		WithArgs(userID, startDate, endDate, `["Food and Drink","Restaurants"]`).
		WillReturnRows(rows)

	sum, err := repo.GetSpentSumForBudgetCategory(ctx, userID, category, tags, matchLogic, startDate, endDate)
	assert.NoError(t, err)
	assert.Equal(t, expectedSum, sum)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// =============================================================================
// Issue #497: GetMatchingTransactionStats Tests (Preview Feature)
// =============================================================================

// TestTransactionRepository_GetMatchingTransactionStats_SingleCategory tests
// preview with a single category using category_only match logic
func TestTransactionRepository_GetMatchingTransactionStats_SingleCategory(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	categories := [][]string{{"Food and Drink", "Restaurants"}}
	tags := []string{}
	matchLogic := domain.MatchLogicCategoryOnly
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	expectedCount := 5
	expectedSum := 250.00

	rows := pgxmock.NewRows([]string{"count", "sum"}).AddRow(expectedCount, expectedSum)

	mock.ExpectQuery(`SELECT COUNT\(\*\), COALESCE\(ABS\(SUM\(amount\)\), 0\)`).
		WithArgs(userID, startDate, endDate, `["Food and Drink","Restaurants"]`).
		WillReturnRows(rows)

	count, sum, err := repo.GetMatchingTransactionStats(ctx, userID, categories, tags, matchLogic, startDate, endDate)
	assert.NoError(t, err)
	assert.Equal(t, expectedCount, count)
	assert.Equal(t, expectedSum, sum)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetMatchingTransactionStats_MultipleCategories tests
// preview with multiple categories (OR across categories)
func TestTransactionRepository_GetMatchingTransactionStats_MultipleCategories(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	categories := [][]string{
		{"Food and Drink", "Restaurants"},
		{"Food and Drink", "Groceries"},
	}
	tags := []string{}
	matchLogic := domain.MatchLogicCategoryOnly
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	expectedCount := 12
	expectedSum := 650.00

	rows := pgxmock.NewRows([]string{"count", "sum"}).AddRow(expectedCount, expectedSum)

	// Expect query with OR for multiple categories
	mock.ExpectQuery(`SELECT COUNT\(\*\), COALESCE\(ABS\(SUM\(amount\)\), 0\)`).
		WithArgs(userID, startDate, endDate, pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(rows)

	count, sum, err := repo.GetMatchingTransactionStats(ctx, userID, categories, tags, matchLogic, startDate, endDate)
	assert.NoError(t, err)
	assert.Equal(t, expectedCount, count)
	assert.Equal(t, expectedSum, sum)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetMatchingTransactionStats_TagOnly tests
// preview with tag_only match logic
func TestTransactionRepository_GetMatchingTransactionStats_TagOnly(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	categories := [][]string{}
	tags := []string{"Essential", "groceries"}
	matchLogic := domain.MatchLogicTagOnly
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	expectedCount := 8
	expectedSum := 400.00

	rows := pgxmock.NewRows([]string{"count", "sum"}).AddRow(expectedCount, expectedSum)

	mock.ExpectQuery(`SELECT COUNT\(\*\), COALESCE\(ABS\(SUM\(amount\)\), 0\)`).
		WithArgs(userID, startDate, endDate, pq.Array(tags)).
		WillReturnRows(rows)

	count, sum, err := repo.GetMatchingTransactionStats(ctx, userID, categories, tags, matchLogic, startDate, endDate)
	assert.NoError(t, err)
	assert.Equal(t, expectedCount, count)
	assert.Equal(t, expectedSum, sum)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetMatchingTransactionStats_And tests
// preview with AND match logic (both category and tags must match)
func TestTransactionRepository_GetMatchingTransactionStats_And(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	categories := [][]string{{"Food and Drink", "Restaurants"}}
	tags := []string{"Essential"}
	matchLogic := domain.MatchLogicAnd
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	expectedCount := 3
	expectedSum := 150.00

	rows := pgxmock.NewRows([]string{"count", "sum"}).AddRow(expectedCount, expectedSum)

	mock.ExpectQuery(`SELECT COUNT\(\*\), COALESCE\(ABS\(SUM\(amount\)\), 0\)`).
		WithArgs(userID, startDate, endDate, `["Food and Drink","Restaurants"]`, pq.Array(tags)).
		WillReturnRows(rows)

	count, sum, err := repo.GetMatchingTransactionStats(ctx, userID, categories, tags, matchLogic, startDate, endDate)
	assert.NoError(t, err)
	assert.Equal(t, expectedCount, count)
	assert.Equal(t, expectedSum, sum)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetMatchingTransactionStats_Or tests
// preview with OR match logic (either category or tags match)
func TestTransactionRepository_GetMatchingTransactionStats_Or(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	categories := [][]string{{"Food and Drink", "Restaurants"}}
	tags := []string{"Essential"}
	matchLogic := domain.MatchLogicOr
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	expectedCount := 10
	expectedSum := 500.00

	rows := pgxmock.NewRows([]string{"count", "sum"}).AddRow(expectedCount, expectedSum)

	mock.ExpectQuery(`SELECT COUNT\(\*\), COALESCE\(ABS\(SUM\(amount\)\), 0\)`).
		WithArgs(userID, startDate, endDate, `["Food and Drink","Restaurants"]`, pq.Array(tags)).
		WillReturnRows(rows)

	count, sum, err := repo.GetMatchingTransactionStats(ctx, userID, categories, tags, matchLogic, startDate, endDate)
	assert.NoError(t, err)
	assert.Equal(t, expectedCount, count)
	assert.Equal(t, expectedSum, sum)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetMatchingTransactionStats_NoMatches tests
// that no matching transactions returns 0, 0
func TestTransactionRepository_GetMatchingTransactionStats_NoMatches(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	categories := [][]string{{"Nonexistent", "Category"}}
	tags := []string{}
	matchLogic := domain.MatchLogicCategoryOnly
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)

	rows := pgxmock.NewRows([]string{"count", "sum"}).AddRow(0, 0)

	mock.ExpectQuery(`SELECT COUNT\(\*\), COALESCE\(ABS\(SUM\(amount\)\), 0\)`).
		WithArgs(userID, startDate, endDate, `["Nonexistent","Category"]`).
		WillReturnRows(rows)

	count, sum, err := repo.GetMatchingTransactionStats(ctx, userID, categories, tags, matchLogic, startDate, endDate)
	assert.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.Equal(t, 0.0, sum)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetMatchingTransactionStats_DBError tests
// database error handling
func TestTransactionRepository_GetMatchingTransactionStats_DBError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	categories := [][]string{{"Food and Drink"}}
	tags := []string{}
	matchLogic := domain.MatchLogicCategoryOnly
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)

	mock.ExpectQuery(`SELECT COUNT\(\*\), COALESCE\(ABS\(SUM\(amount\)\), 0\)`).
		WithArgs(userID, startDate, endDate, `["Food and Drink"]`).
		WillReturnError(errors.New("connection done"))

	count, sum, err := repo.GetMatchingTransactionStats(ctx, userID, categories, tags, matchLogic, startDate, endDate)
	assert.Error(t, err)
	assert.Equal(t, 0, count)
	assert.Equal(t, 0.0, sum)
	assert.Contains(t, err.Error(), "failed to get matching transaction stats")

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// =============================================================================
// Issue #171: Re-Link Handling Tests
// =============================================================================

// TestTransactionRepository_CreateBatch_WithUserAccountID tests batch creation with user_account_id
// Issue #171: UserAccountID is added to transactions for stable queries across re-links
func TestTransactionRepository_CreateBatch_WithUserAccountID(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	externalID := "plaid_tx_relink_1"
	userAccountID := "user-account-uuid-123"
	transactions := []domain.Transaction{
		{
			UserID:                "user-123",
			AccountID:             "account-new-123",
			LinkedItemID:          "item-new-123",
			UserAccountID:         &userAccountID,
			Source:                domain.SourcePlaid,
			ExternalTransactionID: &externalID,
			Amount:                -50.00,
			Date:                  time.Now(),
			Name:                  "Starbucks",
		},
	}

	// Expect batch INSERT with user_account_id and external_transaction_id_blind_idx (21 fields now)
	// Build arguments for 21 fields per transaction
	args := make([]interface{}, 21)
	for i := range args {
		args[i] = pgxmock.AnyArg()
	}
	mock.ExpectExec("INSERT INTO transactions").
		WithArgs(args...).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err = repo.CreateBatch(ctx, transactions)
	assert.NoError(t, err)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_CreateBatch_DOUpdateBehavior verifies that ON CONFLICT DO UPDATE is used
// Issue #171: When a re-link occurs, duplicate transactions should UPDATE FKs, not be ignored
func TestTransactionRepository_CreateBatch_DOUpdateBehavior(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	externalID := "plaid_tx_duplicate_1"
	userAccountID := "user-account-uuid-123"

	// Simulate a re-link scenario: same transaction, new account_id
	transactions := []domain.Transaction{
		{
			UserID:                "user-123",
			AccountID:             "account-NEW-after-relink", // NEW account after re-link
			LinkedItemID:          "item-NEW-after-relink",    // NEW linked item after re-link
			UserAccountID:         &userAccountID,             // Stable user_account_id
			Source:                domain.SourcePlaid,
			ExternalTransactionID: &externalID, // Same external ID (Plaid transaction_id is stable)
			Amount:                -50.00,
			Date:                  time.Now(),
			Name:                  "Starbucks",
		},
	}

	// Build arguments for 21 fields per transaction (includes external_transaction_id_blind_idx)
	args := make([]interface{}, 21)
	for i := range args {
		args[i] = pgxmock.AnyArg()
	}

	// The query should use ON CONFLICT DO UPDATE (not DO NOTHING)
	// This test verifies the query structure includes DO UPDATE
	mock.ExpectExec("ON CONFLICT.*DO UPDATE").
		WithArgs(args...).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err = repo.CreateBatch(ctx, transactions)
	assert.NoError(t, err)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetByUserAccountID tests retrieval by stable user_account_id
// Issue #171: Allows querying transactions across re-links using stable reference
func TestTransactionRepository_GetByUserAccountID(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	userAccountID := "user-account-uuid-123"
	now := time.Now()

	externalID1 := "tx-1"
	externalID2 := "tx-2"

	// Build rows matching the SELECT query column order in GetByUserAccountID
	// Note: category_source must be raw string (not *string) for sql.NullString scanning
	rows := pgxmock.NewRows([]string{
		"id", "user_id", "account_id", "linked_item_id", "user_account_id",
		"source", "external_transaction_id", "external_metadata",
		"amount", "date", "authorized_date", "name", "merchant_name",
		"category", "personal_finance_category", "payment_channel", "pending", "user_notes",
		"created_at", "updated_at", "tags", "tags_source",
		"category_override", "category_source", "category_override_date", "original_category",
	}).
		AddRow(
			"tx-uuid-1", userID, "account-old", "item-old", &userAccountID,
			"plaid", &externalID1, nil,
			-50.00, now, nil, "Starbucks", ptrStr("Starbucks Corp"),
			nil, nil, nil, false, nil,
			now, now, []byte(`[]`), []byte(`[]`),
			nil, "plaid", nil, nil, // category_source as raw string for sql.NullString
		).
		AddRow(
			"tx-uuid-2", userID, "account-new", "item-new", &userAccountID,
			"plaid", &externalID2, nil,
			-25.00, now, nil, "Coffee", nil,
			nil, nil, nil, false, nil,
			now, now, []byte(`[]`), []byte(`[]`),
			nil, "plaid", nil, nil, // category_source as raw string for sql.NullString
		)

	mock.ExpectQuery("SELECT.*FROM transactions.*WHERE user_id.*AND user_account_id").
		WithArgs(userID, userAccountID).
		WillReturnRows(rows)

	// This method should exist for stable queries
	transactions, err := repo.GetByUserAccountID(ctx, userID, userAccountID)
	require.NoError(t, err)
	assert.Len(t, transactions, 2)
	// Both transactions have the same user_account_id despite different account_ids
	assert.Equal(t, "account-old", transactions[0].AccountID)
	assert.Equal(t, "account-new", transactions[1].AccountID)
	assert.Equal(t, userAccountID, *transactions[0].UserAccountID)
	assert.Equal(t, userAccountID, *transactions[1].UserAccountID)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// Helper function to create string pointer
func ptrStr(s string) *string {
	return &s
}

