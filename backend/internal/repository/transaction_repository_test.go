package repository

import (
	"context"
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
	}).
		AddRow(
			"tx-1", userID, "account-1", "item-1",
			"plaid", "plaid_tx_1", nil,
			-50.00, time.Now(), nil, "Transaction 1", nil,
			nil, nil, nil,
			false, nil, time.Now(), time.Now(),
		).
		AddRow(
			"tx-2", userID, "account-2", "item-1",
			"plaid", "plaid_tx_2", nil,
			-25.00, time.Now(), nil, "Transaction 2", nil,
			nil, nil, nil,
			false, nil, time.Now(), time.Now(),
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
	}).
		AddRow(
			"tx-1", "user-1", accountID, "item-1",
			"plaid", "plaid_tx_1", nil,
			-50.00, time.Now(), nil, "Transaction 1", nil,
			nil, nil, nil,
			false, nil, time.Now(), time.Now(),
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
	}).
		AddRow(
			"tx-1", userID, "account-1", "item-1",
			"plaid", "plaid_tx_1", nil,
			-50.00, time.Now(), nil, "Transaction 1", nil,
			nil, nil, nil,
			false, nil, time.Now(), time.Now(),
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
