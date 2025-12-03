package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTransactionRepository_Create_Success tests successful transaction creation
func TestTransactionRepository_Create_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewTransactionRepository(db)
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

	// Expect INSERT query
	mock.ExpectExec("INSERT INTO transactions").
		WithArgs(
			sqlmock.AnyArg(), // id (UUID generated)
			transaction.UserID,
			transaction.AccountID,
			transaction.LinkedItemID,
			string(transaction.Source),
			transaction.ExternalTransactionID,
			sqlmock.AnyArg(), // external_metadata
			transaction.Amount,
			sqlmock.AnyArg(), // date
			sqlmock.AnyArg(), // authorized_date
			transaction.Name,
			sqlmock.AnyArg(), // merchant_name
			sqlmock.AnyArg(), // category
			sqlmock.AnyArg(), // personal_finance_category
			sqlmock.AnyArg(), // payment_channel
			transaction.Pending,
			sqlmock.AnyArg(), // user_notes
			sqlmock.AnyArg(), // created_at
			sqlmock.AnyArg(), // updated_at
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

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
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewTransactionRepository(db)
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

	// Expect batch INSERT with multiple value sets
	mock.ExpectExec("INSERT INTO transactions").
		WillReturnResult(sqlmock.NewResult(1, 2))

	err = repo.CreateBatch(ctx, transactions)
	assert.NoError(t, err)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_Update_Success tests transaction update
func TestTransactionRepository_Update_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewTransactionRepository(db)
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
			sqlmock.AnyArg(), // date
			sqlmock.AnyArg(), // authorized_date
			transaction.Name,
			sqlmock.AnyArg(), // merchant_name
			sqlmock.AnyArg(), // category
			sqlmock.AnyArg(), // personal_finance_category
			sqlmock.AnyArg(), // payment_channel
			transaction.Pending,
			sqlmock.AnyArg(), // external_metadata
			sqlmock.AnyArg(), // user_notes
			sqlmock.AnyArg(), // updated_at
			transaction.ID,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err = repo.Update(ctx, transaction)
	assert.NoError(t, err)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_DeleteByExternalID_Success tests transaction deletion
func TestTransactionRepository_DeleteByExternalID_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewTransactionRepository(db)
	ctx := context.Background()

	externalID := "plaid_tx_123"
	userID := "user-123"

	// Expect DELETE query
	mock.ExpectExec("DELETE FROM transactions").
		WithArgs(domain.SourcePlaid, externalID, userID).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err = repo.DeleteByExternalID(ctx, domain.SourcePlaid, externalID, userID)
	assert.NoError(t, err)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetByUserID_Success tests retrieving transactions by user
func TestTransactionRepository_GetByUserID_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewTransactionRepository(db)
	ctx := context.Background()

	userID := "user-123"
	limit := 10
	offset := 0

	// Mock result rows
	rows := sqlmock.NewRows([]string{
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
			"plaid", "plaid_tx_1", nil,
			-50.00, time.Now(), nil, "Transaction 1", nil,
			nil, nil, nil,
			false, nil, time.Now(), time.Now(),
			nil, nil,
			nil, nil, nil, nil,
		).
		AddRow(
			"tx-2", userID, "account-2", "item-1",
			"plaid", "plaid_tx_2", nil,
			-25.00, time.Now(), nil, "Transaction 2", nil,
			nil, nil, nil,
			false, nil, time.Now(), time.Now(),
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
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewTransactionRepository(db)
	ctx := context.Background()

	accountID := "account-123"

	// Mock result rows
	rows := sqlmock.NewRows([]string{
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
			"plaid", "plaid_tx_1", nil,
			-50.00, time.Now(), nil, "Transaction 1", nil,
			nil, nil, nil,
			false, nil, time.Now(), time.Now(),
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
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewTransactionRepository(db)
	ctx := context.Background()

	userID := "user-123"
	source := domain.SourcePlaid

	// Mock result rows
	rows := sqlmock.NewRows([]string{
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
			"plaid", "plaid_tx_1", nil,
			-50.00, time.Now(), nil, "Transaction 1", nil,
			nil, nil, nil,
			false, nil, time.Now(), time.Now(),
			nil, nil,
			nil, nil, nil, nil,
		)

	// Expect SELECT query
	mock.ExpectQuery("SELECT (.+) FROM transactions WHERE source").
		WithArgs(source, userID).
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
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewTransactionRepository(db)
	ctx := context.Background()

	userID := "user-123"
	limit := 10
	offset := 0
	filters := domain.TransactionFilters{} // No filters

	// Mock result rows
	rows := sqlmock.NewRows([]string{
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
			"plaid", "plaid_tx_1", nil,
			-50.00, time.Now(), nil, "Transaction 1", nil,
			nil, nil, nil,
			false, nil, time.Now(), time.Now(),
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
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewTransactionRepository(db)
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

	// Mock result rows
	rows := sqlmock.NewRows([]string{
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
			"plaid", "plaid_tx_1", nil,
			-50.00, time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC), nil, "Transaction 1", nil,
			nil, nil, nil,
			false, nil, time.Now(), time.Now(),
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
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewTransactionRepository(db)
	ctx := context.Background()

	userID := "user-123"
	accountID := "account-456"
	limit := 10
	offset := 0
	filters := domain.TransactionFilters{
		AccountID: &accountID,
	}

	// Mock result rows
	rows := sqlmock.NewRows([]string{
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
			"plaid", "plaid_tx_1", nil,
			-50.00, time.Now(), nil, "Transaction 1", nil,
			nil, nil, nil,
			false, nil, time.Now(), time.Now(),
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
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewTransactionRepository(db)
	ctx := context.Background()

	userID := "user-123"
	searchQuery := "starbucks"
	limit := 10
	offset := 0
	filters := domain.TransactionFilters{
		Search: &searchQuery,
	}

	merchantName := "Starbucks Coffee"
	// Mock result rows
	rows := sqlmock.NewRows([]string{
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
			"plaid", "plaid_tx_1", nil,
			-5.49, time.Now(), nil, "Starbucks", &merchantName,
			nil, nil, nil,
			false, nil, time.Now(), time.Now(),
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
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewTransactionRepository(db)
	ctx := context.Background()

	userID := "user-123"
	categoryFilter := "Food and Drink"
	limit := 10
	offset := 0
	filters := domain.TransactionFilters{
		Category: &categoryFilter,
	}

	categoryJSON := `["Food and Drink", "Restaurants"]`
	// Mock result rows
	rows := sqlmock.NewRows([]string{
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
			"plaid", "plaid_tx_1", nil,
			-25.00, time.Now(), nil, "Restaurant", nil,
			&categoryJSON, nil, nil,
			false, nil, time.Now(), time.Now(),
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
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewTransactionRepository(db)
	ctx := context.Background()

	userID := "user-123"
	pendingOnly := true
	limit := 10
	offset := 0
	filters := domain.TransactionFilters{
		Pending: &pendingOnly,
	}

	// Mock result rows
	rows := sqlmock.NewRows([]string{
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
			"plaid", "plaid_tx_1", nil,
			-100.00, time.Now(), nil, "Pending Transaction", nil,
			nil, nil, nil,
			true, nil, time.Now(), time.Now(),
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
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewTransactionRepository(db)
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

	// Mock result rows
	rows := sqlmock.NewRows([]string{
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
			"plaid", "plaid_tx_1", nil,
			-50.00, time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC), nil, "Transaction 1", nil,
			nil, nil, nil,
			false, nil, time.Now(), time.Now(),
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
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewTransactionRepository(db)
	ctx := context.Background()

	userID := "user-123"
	filters := domain.TransactionFilters{}

	// Mock result rows
	rows := sqlmock.NewRows([]string{"count"}).AddRow(42)

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
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewTransactionRepository(db)
	ctx := context.Background()

	userID := "user-123"
	accountID := "account-456"
	pendingOnly := true
	filters := domain.TransactionFilters{
		AccountID: &accountID,
		Pending:   &pendingOnly,
	}

	// Mock result rows
	rows := sqlmock.NewRows([]string{"count"}).AddRow(5)

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
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewTransactionRepository(db)
	ctx := context.Background()

	externalID := "plaid_tx_123"
	userID := "user-123"
	source := domain.SourcePlaid

	merchantName := "Starbucks Coffee"
	categoryJSON := `["Food and Drink", "Coffee"]`
	// Mock result row
	rows := sqlmock.NewRows([]string{
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
			"plaid", externalID, nil,
			-25.50, time.Now(), nil, "Starbucks", &merchantName,
			&categoryJSON, nil, nil,
			false, nil, time.Now(), time.Now(),
			nil, nil,
			nil, nil, nil, nil,
		)

	// Expect SELECT query with WHERE source = $1 AND external_transaction_id = $2 AND user_id = $3
	mock.ExpectQuery("SELECT (.+) FROM transactions WHERE source (.+) AND external_transaction_id (.+) AND user_id").
		WithArgs(string(source), externalID, userID).
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
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewTransactionRepository(db)
	ctx := context.Background()

	externalID := "nonexistent_tx"
	userID := "user-123"
	source := domain.SourcePlaid

	// Expect SELECT query to return sql.ErrNoRows (which gets converted to ErrTransactionNotFound)
	mock.ExpectQuery("SELECT (.+) FROM transactions WHERE source (.+) AND external_transaction_id (.+) AND user_id").
		WithArgs(string(source), externalID, userID).
		WillReturnError(sql.ErrNoRows)

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
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewTransactionRepository(db)
	ctx := context.Background()

	userID := "user-123"
	limit := 10
	offset := 0
	filters := domain.TransactionFilters{}

	// Mock result rows including tags and tags_source
	rows := sqlmock.NewRows([]string{
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
			"plaid", "plaid_tx_1", nil,
			-50.00, time.Now(), nil, "Transaction 1", nil,
			nil, nil, nil,
			false, nil, time.Now(), time.Now(),
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
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewTransactionRepository(db)
	ctx := context.Background()

	userID := "user-123"
	limit := 10
	offset := 0
	filters := domain.TransactionFilters{}

	overrideDate := time.Now()

	// Mock result rows including category_override fields
	rows := sqlmock.NewRows([]string{
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
			"plaid", "plaid_tx_1", nil,
			-50.00, time.Now(), nil, "Transaction 1", nil,
			`["Food and Drink"]`, nil, nil,
			false, nil, time.Now(), time.Now(),
			nil, nil,
			`["Custom Category"]`, "user", overrideDate, `["Food and Drink"]`,
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
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewTransactionRepository(db)
	ctx := context.Background()

	txID := "tx-123"
	overrideDate := time.Now()

	// Mock result row including all fields
	rows := sqlmock.NewRows([]string{
		"id", "user_id", "account_id", "linked_item_id", "source", "external_transaction_id",
		"external_metadata", "amount", "date", "authorized_date", "name", "merchant_name",
		"category", "personal_finance_category", "payment_channel", "pending", "user_notes",
		"created_at", "updated_at", "tags", "tags_source",
		"category_override", "category_source", "category_override_date", "original_category",
	}).
		AddRow(
			txID, "user-123", "account-1", "item-1", "plaid", "plaid_tx_1",
			nil, -50.00, time.Now(), nil, "Transaction 1", nil,
			`["Food and Drink"]`, nil, nil, false, nil,
			time.Now(), time.Now(), []byte(`["Food"]`), []byte(`[{"tag":"Food","source":"user"}]`),
			`["Custom Category"]`, "user", overrideDate, `["Food and Drink"]`,
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
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewTransactionRepository(db)
	ctx := context.Background()

	txID := "tx-123"
	userID := "user-123"
	categoryOverride := []string{"Custom", "Category"}

	// Expect the UPDATE query with COALESCE for original_category
	// This ensures the first manual edit preserves the original Plaid category
	mock.ExpectExec("UPDATE transactions SET category_override.*original_category = COALESCE\\(original_category, category\\)").
		WithArgs(
			sqlmock.AnyArg(), // category_override JSON
			domain.CategorySource("user"),
			sqlmock.AnyArg(), // category_override_date
			sqlmock.AnyArg(), // updated_at
			txID,
			userID,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err = repo.UpdateCategory(ctx, txID, userID, categoryOverride, domain.CategorySource("user"))
	assert.NoError(t, err)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetDistinctPlaidCategories_Success tests retrieving distinct Plaid categories
// Returns primary category level (index 0) for consistency
func TestTransactionRepository_GetDistinctPlaidCategories_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewTransactionRepository(db)
	ctx := context.Background()

	userID := "user-123"

	// Mock result rows - distinct primary categories extracted from JSON arrays
	rows := sqlmock.NewRows([]string{"cat"}).
		AddRow("Food and Drink").
		AddRow("Shops").
		AddRow("Travel")

	// Expect SELECT DISTINCT query that extracts primary category (index 0)
	mock.ExpectQuery("SELECT DISTINCT").
		WithArgs(userID).
		WillReturnRows(rows)

	categories, err := repo.GetDistinctPlaidCategories(ctx, userID)
	assert.NoError(t, err)
	assert.Len(t, categories, 3)
	assert.Contains(t, categories, "Food and Drink")
	assert.Contains(t, categories, "Shops")
	assert.Contains(t, categories, "Travel")

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetDistinctPlaidCategories_Empty tests when user has no categorized transactions
func TestTransactionRepository_GetDistinctPlaidCategories_Empty(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewTransactionRepository(db)
	ctx := context.Background()

	userID := "user-no-transactions"

	// Mock empty result
	rows := sqlmock.NewRows([]string{"cat"})

	mock.ExpectQuery("SELECT DISTINCT").
		WithArgs(userID).
		WillReturnRows(rows)

	categories, err := repo.GetDistinctPlaidCategories(ctx, userID)
	assert.NoError(t, err)
	assert.Empty(t, categories)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

