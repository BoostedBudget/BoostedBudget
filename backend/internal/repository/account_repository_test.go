package repository

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupAccountMockDB creates a mock database and sqlmock instance for account repository testing
func setupAccountMockDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock, AccountRepository) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)

	repo := NewAccountRepository(db)
	return db, mock, repo
}

// TestAccountRepository_Create tests successful creation of a single account
func TestAccountRepository_Create(t *testing.T) {
	db, mock, repo := setupAccountMockDB(t)
	defer db.Close()

	ctx := context.Background()
	now := time.Now()
	balance := 1500.50
	available := 1450.25

	account := &domain.Account{
		ID:               uuid.New().String(),
		LinkedItemID:     uuid.New().String(),
		UserID:           uuid.New().String(),
		PlaidAccountID:   "plaid-acc-xyz123",
		Name:             "Chase Checking",
		OfficialName:     strPtr("Chase Total Checking®"),
		Type:             "depository",
		Subtype:          strPtr("checking"),
		Mask:             strPtr("1234"),
		CurrentBalance:   &balance,
		AvailableBalance: &available,
		CurrencyCode:     "USD",
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO accounts
		(id, linked_item_id, user_id, plaid_account_id, name, official_name,
		 type, subtype, mask, current_balance, available_balance, currency_code,
		 created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`)).WithArgs(
		account.ID,
		account.LinkedItemID,
		account.UserID,
		account.PlaidAccountID,
		account.Name,
		account.OfficialName,
		account.Type,
		account.Subtype,
		account.Mask,
		account.CurrentBalance,
		account.AvailableBalance,
		account.CurrencyCode,
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
	).WillReturnResult(sqlmock.NewResult(1, 1))

	// Execute
	err := repo.Create(ctx, account)

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_CreateBatch tests batch creation of multiple accounts
func TestAccountRepository_CreateBatch(t *testing.T) {
	db, mock, repo := setupAccountMockDB(t)
	defer db.Close()

	ctx := context.Background()
	linkedItemID := uuid.New().String()
	userID := uuid.New().String()

	accounts := []domain.Account{
		{
			ID:             uuid.New().String(),
			LinkedItemID:   linkedItemID,
			UserID:         userID,
			PlaidAccountID: "acc-1",
			Name:           "Checking",
			Type:           "depository",
			CurrencyCode:   "USD",
		},
		{
			ID:             uuid.New().String(),
			LinkedItemID:   linkedItemID,
			UserID:         userID,
			PlaidAccountID: "acc-2",
			Name:           "Savings",
			Type:           "depository",
			CurrencyCode:   "USD",
		},
	}

	// Expect transaction begin
	mock.ExpectBegin()

	// Expect two insert statements
	for _, acc := range accounts {
		mock.ExpectExec(regexp.QuoteMeta(`
			INSERT INTO accounts
			(id, linked_item_id, user_id, plaid_account_id, name, official_name,
			 type, subtype, mask, current_balance, available_balance, currency_code,
			 created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		`)).WithArgs(
			acc.ID,
			acc.LinkedItemID,
			acc.UserID,
			acc.PlaidAccountID,
			acc.Name,
			acc.OfficialName,
			acc.Type,
			acc.Subtype,
			acc.Mask,
			acc.CurrentBalance,
			acc.AvailableBalance,
			acc.CurrencyCode,
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
		).WillReturnResult(sqlmock.NewResult(1, 1))
	}

	// Expect transaction commit
	mock.ExpectCommit()

	// Execute
	err := repo.CreateBatch(ctx, accounts)

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_GetByLinkedItemID tests retrieval of accounts by linked item
func TestAccountRepository_GetByLinkedItemID(t *testing.T) {
	db, mock, repo := setupAccountMockDB(t)
	defer db.Close()

	ctx := context.Background()
	linkedItemID := uuid.New().String()
	now := time.Now()

	rows := sqlmock.NewRows([]string{
		"id", "linked_item_id", "user_id", "plaid_account_id",
		"name", "official_name", "type", "subtype", "mask",
		"current_balance", "available_balance", "currency_code",
		"created_at", "updated_at",
	}).
		AddRow(
			uuid.New().String(), linkedItemID, uuid.New().String(), "acc-1",
			"Checking", "Chase Total Checking", "depository", "checking", "1234",
			1500.50, 1450.25, "USD",
			now, now,
		).
		AddRow(
			uuid.New().String(), linkedItemID, uuid.New().String(), "acc-2",
			"Savings", nil, "depository", "savings", "5678",
			5000.00, 5000.00, "USD",
			now, now,
		)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, linked_item_id, user_id, plaid_account_id,
		       name, official_name, type, subtype, mask,
		       current_balance, available_balance, currency_code,
		       created_at, updated_at
		FROM accounts
		WHERE linked_item_id = $1
		ORDER BY created_at ASC
	`)).WithArgs(linkedItemID).WillReturnRows(rows)

	// Execute
	accounts, err := repo.GetByLinkedItemID(ctx, linkedItemID)

	// Assert
	assert.NoError(t, err)
	assert.Len(t, accounts, 2)
	assert.Equal(t, "Checking", accounts[0].Name)
	assert.Equal(t, "Savings", accounts[1].Name)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_GetByUserID tests retrieval of all accounts for a user
func TestAccountRepository_GetByUserID(t *testing.T) {
	db, mock, repo := setupAccountMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	now := time.Now()

	rows := sqlmock.NewRows([]string{
		"id", "linked_item_id", "user_id", "plaid_account_id",
		"name", "official_name", "type", "subtype", "mask",
		"current_balance", "available_balance", "currency_code",
		"created_at", "updated_at",
	}).AddRow(
		uuid.New().String(), uuid.New().String(), userID, "acc-1",
		"Checking", nil, "depository", "checking", "1234",
		1000.00, 950.00, "USD",
		now, now,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, linked_item_id, user_id, plaid_account_id,
		       name, official_name, type, subtype, mask,
		       current_balance, available_balance, currency_code,
		       created_at, updated_at
		FROM accounts
		WHERE user_id = $1
		ORDER BY created_at ASC
	`)).WithArgs(userID).WillReturnRows(rows)

	// Execute
	accounts, err := repo.GetByUserID(ctx, userID)

	// Assert
	assert.NoError(t, err)
	assert.Len(t, accounts, 1)
	assert.Equal(t, userID, accounts[0].UserID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_CountByUserID tests counting accounts for subscription limit checks
func TestAccountRepository_CountByUserID(t *testing.T) {
	db, mock, repo := setupAccountMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	expectedCount := 5

	rows := sqlmock.NewRows([]string{"count"}).AddRow(expectedCount)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT COUNT(*)
		FROM accounts
		WHERE user_id = $1
	`)).WithArgs(userID).WillReturnRows(rows)

	// Execute
	count, err := repo.CountByUserID(ctx, userID)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, expectedCount, count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_UniqueConstraint tests that duplicate accounts are rejected
func TestAccountRepository_UniqueConstraint(t *testing.T) {
	db, mock, repo := setupAccountMockDB(t)
	defer db.Close()

	ctx := context.Background()
	account := &domain.Account{
		ID:             uuid.New().String(),
		LinkedItemID:   uuid.New().String(),
		UserID:         uuid.New().String(),
		PlaidAccountID: "duplicate-account-id",
		Name:           "Account",
		Type:           "depository",
		CurrencyCode:   "USD",
	}

	// Simulate unique constraint violation
	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO accounts
	`)).WillReturnError(sql.ErrConnDone) // Simulating constraint error

	// Execute
	err := repo.Create(ctx, account)

	// Assert
	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_GetAccountsWithInstitution tests retrieval of accounts with institution names
func TestAccountRepository_GetAccountsWithInstitution(t *testing.T) {
	db, mock, repo := setupAccountMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	linkedItemID1 := uuid.New().String()
	linkedItemID2 := uuid.New().String()

	// Mock the LEFT JOIN query
	rows := sqlmock.NewRows([]string{
		"id", "linked_item_id", "user_id", "plaid_account_id",
		"name", "official_name", "type", "subtype", "mask",
		"current_balance", "available_balance", "currency_code",
		"created_at", "updated_at", "institution_name",
	}).
		AddRow(
			uuid.New().String(), linkedItemID1, userID, "plaid-acc-1",
			"Chase Checking", "Chase Total Checking®", "depository", "checking", "1234",
			1500.50, 1450.25, "USD",
			time.Now(), time.Now(), "Chase",
		).
		AddRow(
			uuid.New().String(), linkedItemID1, userID, "plaid-acc-2",
			"Chase Savings", "Chase Savings℠", "depository", "savings", "5678",
			5000.00, 5000.00, "USD",
			time.Now(), time.Now(), "Chase",
		).
		AddRow(
			uuid.New().String(), linkedItemID2, userID, "plaid-acc-3",
			"Wells Fargo Checking", nil, "depository", "checking", "9012",
			750.00, 700.00, "USD",
			time.Now(), time.Now(), "Wells Fargo",
		)

	expectedQuery := `
		SELECT a.id, a.linked_item_id, a.user_id, a.plaid_account_id,
		       a.name, a.official_name, a.type, a.subtype, a.mask,
		       a.current_balance, a.available_balance, a.currency_code,
		       a.created_at, a.updated_at, l.institution_name
		FROM accounts a
		LEFT JOIN linked_items l ON a.linked_item_id = l.id
		WHERE a.user_id = $1
		ORDER BY a.created_at ASC
	`

	mock.ExpectQuery(regexp.QuoteMeta(expectedQuery)).
		WithArgs(userID).
		WillReturnRows(rows)

	// Execute
	accounts, err := repo.GetAccountsWithInstitution(ctx, userID)

	// Assert
	require.NoError(t, err)
	assert.Len(t, accounts, 3)

	// Verify first account (Chase Checking)
	assert.Equal(t, "Chase Checking", accounts[0].Name)
	assert.Equal(t, "Chase", accounts[0].InstitutionName)
	assert.NotNil(t, accounts[0].OfficialName)
	assert.Equal(t, "Chase Total Checking®", *accounts[0].OfficialName)

	// Verify second account (Chase Savings)
	assert.Equal(t, "Chase Savings", accounts[1].Name)
	assert.Equal(t, "Chase", accounts[1].InstitutionName)

	// Verify third account (Wells Fargo)
	assert.Equal(t, "Wells Fargo Checking", accounts[2].Name)
	assert.Equal(t, "Wells Fargo", accounts[2].InstitutionName)
	assert.Nil(t, accounts[2].OfficialName)

	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_GetAccountsWithInstitution_Empty tests when user has no accounts
func TestAccountRepository_GetAccountsWithInstitution_Empty(t *testing.T) {
	db, mock, repo := setupAccountMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()

	// Mock empty result
	rows := sqlmock.NewRows([]string{
		"id", "linked_item_id", "user_id", "plaid_account_id",
		"name", "official_name", "type", "subtype", "mask",
		"current_balance", "available_balance", "currency_code",
		"created_at", "updated_at", "institution_name",
	})

	expectedQuery := `
		SELECT a.id, a.linked_item_id, a.user_id, a.plaid_account_id,
		       a.name, a.official_name, a.type, a.subtype, a.mask,
		       a.current_balance, a.available_balance, a.currency_code,
		       a.created_at, a.updated_at, l.institution_name
		FROM accounts a
		LEFT JOIN linked_items l ON a.linked_item_id = l.id
		WHERE a.user_id = $1
		ORDER BY a.created_at ASC
	`

	mock.ExpectQuery(regexp.QuoteMeta(expectedQuery)).
		WithArgs(userID).
		WillReturnRows(rows)

	// Execute
	accounts, err := repo.GetAccountsWithInstitution(ctx, userID)

	// Assert
	require.NoError(t, err)
	assert.Empty(t, accounts)
	assert.NotNil(t, accounts) // Should return empty slice, not nil

	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_Delete_Success tests successful account deletion with ownership validation
func TestAccountRepository_Delete_Success(t *testing.T) {
	// Given: Mock DB and account repository
	db, mock, repo := setupAccountMockDB(t)
	defer db.Close()

	ctx := context.Background()
	accountID := uuid.New().String()
	userID := uuid.New().String()

	// Expect: DELETE query with ownership validation (WHERE id = $1 AND user_id = $2)
	expectedQuery := `DELETE FROM accounts WHERE id = $1 AND user_id = $2`
	mock.ExpectExec(regexp.QuoteMeta(expectedQuery)).
		WithArgs(accountID, userID).
		WillReturnResult(sqlmock.NewResult(0, 1)) // 1 row affected

	// When: Deleting account
	err := repo.Delete(ctx, accountID, userID)

	// Then: No error, deletion successful
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_Delete_NotFound tests deletion when account doesn't exist
func TestAccountRepository_Delete_NotFound(t *testing.T) {
	// Given: Mock DB and account repository
	db, mock, repo := setupAccountMockDB(t)
	defer db.Close()

	ctx := context.Background()
	accountID := uuid.New().String()
	userID := uuid.New().String()

	// Expect: DELETE query returns 0 rows affected (account not found)
	expectedQuery := `DELETE FROM accounts WHERE id = $1 AND user_id = $2`
	mock.ExpectExec(regexp.QuoteMeta(expectedQuery)).
		WithArgs(accountID, userID).
		WillReturnResult(sqlmock.NewResult(0, 0)) // 0 rows affected

	// When: Attempting to delete non-existent account
	err := repo.Delete(ctx, accountID, userID)

	// Then: Returns ErrAccountNotFound
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrAccountNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_Delete_NotOwner tests deletion when user doesn't own the account
// This validates ownership via the WHERE clause (id AND user_id must match)
func TestAccountRepository_Delete_NotOwner(t *testing.T) {
	// Given: Mock DB and account repository
	db, mock, repo := setupAccountMockDB(t)
	defer db.Close()

	ctx := context.Background()
	accountID := uuid.New().String()
	wrongUserID := uuid.New().String() // Different user

	// Expect: DELETE query with wrong user_id returns 0 rows affected
	expectedQuery := `DELETE FROM accounts WHERE id = $1 AND user_id = $2`
	mock.ExpectExec(regexp.QuoteMeta(expectedQuery)).
		WithArgs(accountID, wrongUserID).
		WillReturnResult(sqlmock.NewResult(0, 0)) // 0 rows affected (ownership check fails)

	// When: Attempting to delete account owned by different user
	err := repo.Delete(ctx, accountID, wrongUserID)

	// Then: Returns ErrAccountNotFound (account not found for this user)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrAccountNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_Delete_DatabaseError tests handling of database errors during deletion
func TestAccountRepository_Delete_DatabaseError(t *testing.T) {
	// Given: Mock DB and account repository
	db, mock, repo := setupAccountMockDB(t)
	defer db.Close()

	ctx := context.Background()
	accountID := uuid.New().String()
	userID := uuid.New().String()

	// Expect: DELETE query returns database error
	expectedQuery := `DELETE FROM accounts WHERE id = $1 AND user_id = $2`
	mock.ExpectExec(regexp.QuoteMeta(expectedQuery)).
		WithArgs(accountID, userID).
		WillReturnError(sql.ErrConnDone) // Database connection error

	// When: Attempting to delete account with DB error
	err := repo.Delete(ctx, accountID, userID)

	// Then: Returns database error
	assert.Error(t, err)
	assert.ErrorIs(t, err, sql.ErrConnDone)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// Helper function to create string pointers
func strPtr(s string) *string {
	return &s
}

// Helper function to create int pointers
func intPtr(i int) *int {
	return &i
}

// TestAccountRepository_GetByIDAndUser_Success tests successful retrieval of an account by ID and user
func TestAccountRepository_GetByIDAndUser_Success(t *testing.T) {
	db, mock, repo := setupAccountMockDB(t)
	defer db.Close()

	ctx := context.Background()
	accountID := uuid.New().String()
	userID := uuid.New().String()
	now := time.Now()

	rows := sqlmock.NewRows([]string{
		"id", "linked_item_id", "user_id", "plaid_account_id",
		"name", "official_name", "type", "subtype", "mask",
		"current_balance", "available_balance", "currency_code",
		"nickname", "custom_color", "custom_icon", "custom_emoji",
		"display_order", "is_hidden",
		"created_at", "updated_at",
	}).AddRow(
		accountID, uuid.New().String(), userID, "plaid-acc-1",
		"Checking", "Chase Total Checking", "depository", "checking", "1234",
		1500.50, 1450.25, "USD",
		"My Checking", "#3B82F6", "wallet", "💰",
		1, false,
		now, now,
	)

	expectedQuery := `
		SELECT id, linked_item_id, user_id, plaid_account_id,
		       name, official_name, type, subtype, mask,
		       current_balance, available_balance, currency_code,
		       nickname, custom_color, custom_icon, custom_emoji,
		       display_order, is_hidden,
		       created_at, updated_at
		FROM accounts
		WHERE id = $1 AND user_id = $2
	`

	mock.ExpectQuery(regexp.QuoteMeta(expectedQuery)).
		WithArgs(accountID, userID).
		WillReturnRows(rows)

	// Execute
	account, err := repo.GetByIDAndUser(ctx, accountID, userID)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, accountID, account.ID)
	assert.Equal(t, userID, account.UserID)
	assert.Equal(t, "My Checking", *account.Nickname)
	assert.Equal(t, "#3B82F6", *account.CustomColor)
	assert.Equal(t, "wallet", *account.CustomIcon)
	assert.Equal(t, "💰", *account.CustomEmoji)
	assert.Equal(t, 1, *account.DisplayOrder)
	assert.False(t, account.IsHidden)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_GetByIDAndUser_NotFound tests when account doesn't exist
func TestAccountRepository_GetByIDAndUser_NotFound(t *testing.T) {
	db, mock, repo := setupAccountMockDB(t)
	defer db.Close()

	ctx := context.Background()
	accountID := uuid.New().String()
	userID := uuid.New().String()

	expectedQuery := `
		SELECT id, linked_item_id, user_id, plaid_account_id,
		       name, official_name, type, subtype, mask,
		       current_balance, available_balance, currency_code,
		       nickname, custom_color, custom_icon, custom_emoji,
		       display_order, is_hidden,
		       created_at, updated_at
		FROM accounts
		WHERE id = $1 AND user_id = $2
	`

	mock.ExpectQuery(regexp.QuoteMeta(expectedQuery)).
		WithArgs(accountID, userID).
		WillReturnError(sql.ErrNoRows)

	// Execute
	account, err := repo.GetByIDAndUser(ctx, accountID, userID)

	// Assert
	assert.Nil(t, account)
	assert.ErrorIs(t, err, ErrAccountNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_Update_Success tests successful account update
func TestAccountRepository_Update_Success(t *testing.T) {
	db, mock, repo := setupAccountMockDB(t)
	defer db.Close()

	ctx := context.Background()
	accountID := uuid.New().String()
	userID := uuid.New().String()

	account := &domain.Account{
		ID:           accountID,
		UserID:       userID,
		Nickname:     strPtr("My Savings"),
		CustomColor:  strPtr("#10B981"),
		CustomIcon:   strPtr("piggy-bank"),
		CustomEmoji:  strPtr("🐷"),
		DisplayOrder: intPtr(2),
		IsHidden:     false,
	}

	expectedQuery := `
		UPDATE accounts
		SET nickname = $1, custom_color = $2, custom_icon = $3,
		    custom_emoji = $4, display_order = $5, is_hidden = $6, updated_at = $7
		WHERE id = $8 AND user_id = $9
	`

	mock.ExpectExec(regexp.QuoteMeta(expectedQuery)).
		WithArgs(
			account.Nickname, account.CustomColor, account.CustomIcon,
			account.CustomEmoji, account.DisplayOrder, account.IsHidden, sqlmock.AnyArg(),
			accountID, userID,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	// Execute
	err := repo.Update(ctx, account, userID)

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_Update_NotFound tests update when account doesn't exist
func TestAccountRepository_Update_NotFound(t *testing.T) {
	db, mock, repo := setupAccountMockDB(t)
	defer db.Close()

	ctx := context.Background()
	accountID := uuid.New().String()
	userID := uuid.New().String()

	account := &domain.Account{
		ID:       accountID,
		UserID:   userID,
		Nickname: strPtr("Test"),
	}

	expectedQuery := `
		UPDATE accounts
		SET nickname = $1, custom_color = $2, custom_icon = $3,
		    custom_emoji = $4, display_order = $5, is_hidden = $6, updated_at = $7
		WHERE id = $8 AND user_id = $9
	`

	mock.ExpectExec(regexp.QuoteMeta(expectedQuery)).
		WithArgs(
			account.Nickname, account.CustomColor, account.CustomIcon,
			account.CustomEmoji, account.DisplayOrder, account.IsHidden, sqlmock.AnyArg(),
			accountID, userID,
		).
		WillReturnResult(sqlmock.NewResult(0, 0)) // 0 rows affected

	// Execute
	err := repo.Update(ctx, account, userID)

	// Assert
	assert.ErrorIs(t, err, ErrAccountNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_UpdateDisplayOrders_Success tests bulk display order update
func TestAccountRepository_UpdateDisplayOrders_Success(t *testing.T) {
	db, mock, repo := setupAccountMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	acc1ID := uuid.New().String()
	acc2ID := uuid.New().String()

	orders := []domain.AccountOrder{
		{AccountID: acc1ID, DisplayOrder: 0},
		{AccountID: acc2ID, DisplayOrder: 1},
	}

	expectedQuery := `
		UPDATE accounts
		SET display_order = $1, updated_at = $2
		WHERE id = $3 AND user_id = $4
	`

	mock.ExpectBegin()
	for _, order := range orders {
		mock.ExpectExec(regexp.QuoteMeta(expectedQuery)).
			WithArgs(order.DisplayOrder, sqlmock.AnyArg(), order.AccountID, userID).
			WillReturnResult(sqlmock.NewResult(0, 1))
	}
	mock.ExpectCommit()

	// Execute
	err := repo.UpdateDisplayOrders(ctx, userID, orders)

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_UpdateDisplayOrders_Empty tests with empty orders list
func TestAccountRepository_UpdateDisplayOrders_Empty(t *testing.T) {
	db, mock, repo := setupAccountMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()

	// Execute with empty list
	err := repo.UpdateDisplayOrders(ctx, userID, []domain.AccountOrder{})

	// Assert - should return nil without any DB calls
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_UpdateDisplayOrders_AccountNotFound tests when one account doesn't exist
func TestAccountRepository_UpdateDisplayOrders_AccountNotFound(t *testing.T) {
	db, mock, repo := setupAccountMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	acc1ID := uuid.New().String()
	acc2ID := uuid.New().String() // This one won't be found

	orders := []domain.AccountOrder{
		{AccountID: acc1ID, DisplayOrder: 0},
		{AccountID: acc2ID, DisplayOrder: 1},
	}

	expectedQuery := `
		UPDATE accounts
		SET display_order = $1, updated_at = $2
		WHERE id = $3 AND user_id = $4
	`

	mock.ExpectBegin()
	// First update succeeds
	mock.ExpectExec(regexp.QuoteMeta(expectedQuery)).
		WithArgs(orders[0].DisplayOrder, sqlmock.AnyArg(), orders[0].AccountID, userID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	// Second update returns 0 rows (not found)
	mock.ExpectExec(regexp.QuoteMeta(expectedQuery)).
		WithArgs(orders[1].DisplayOrder, sqlmock.AnyArg(), orders[1].AccountID, userID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	// Execute
	err := repo.UpdateDisplayOrders(ctx, userID, orders)

	// Assert
	assert.ErrorIs(t, err, ErrAccountNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_GetAccountsWithInstitutionFiltered_ExcludeHidden tests filtering hidden accounts
func TestAccountRepository_GetAccountsWithInstitutionFiltered_ExcludeHidden(t *testing.T) {
	db, mock, repo := setupAccountMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	now := time.Now()

	rows := sqlmock.NewRows([]string{
		"id", "linked_item_id", "user_id", "plaid_account_id",
		"name", "official_name", "type", "subtype", "mask",
		"current_balance", "available_balance", "currency_code",
		"nickname", "custom_color", "custom_icon", "custom_emoji",
		"display_order", "is_hidden",
		"created_at", "updated_at", "institution_name",
	}).
		AddRow(
			uuid.New().String(), uuid.New().String(), userID, "acc-1",
			"Checking", nil, "depository", "checking", "1234",
			1500.50, 1450.25, "USD",
			"My Checking", "#3B82F6", "wallet", nil,
			0, false,
			now, now, "Chase",
		).
		AddRow(
			uuid.New().String(), uuid.New().String(), userID, "acc-2",
			"Savings", nil, "depository", "savings", "5678",
			5000.00, 5000.00, "USD",
			nil, nil, nil, nil,
			1, false,
			now, now, "Chase",
		)

	// Note: This query excludes hidden accounts (AND a.is_hidden = FALSE)
	expectedQuery := `
		SELECT a.id, a.linked_item_id, a.user_id, a.plaid_account_id,
		       a.name, a.official_name, a.type, a.subtype, a.mask,
		       a.current_balance, a.available_balance, a.currency_code,
		       a.nickname, a.custom_color, a.custom_icon, a.custom_emoji,
		       a.display_order, a.is_hidden,
		       a.created_at, a.updated_at, l.institution_name
		FROM accounts a
		LEFT JOIN linked_items l ON a.linked_item_id = l.id
		WHERE a.user_id = $1 AND a.is_hidden = FALSE ORDER BY COALESCE(a.display_order, 999999) ASC, a.created_at ASC
	`

	mock.ExpectQuery(regexp.QuoteMeta(expectedQuery)).
		WithArgs(userID).
		WillReturnRows(rows)

	// Execute with includeHidden=false
	accounts, err := repo.GetAccountsWithInstitutionFiltered(ctx, userID, false)

	// Assert
	require.NoError(t, err)
	assert.Len(t, accounts, 2)
	assert.Equal(t, "My Checking", *accounts[0].Nickname)
	assert.Equal(t, 0, *accounts[0].DisplayOrder)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_GetAccountsWithInstitutionFiltered_IncludeHidden tests including hidden accounts
func TestAccountRepository_GetAccountsWithInstitutionFiltered_IncludeHidden(t *testing.T) {
	db, mock, repo := setupAccountMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	now := time.Now()

	rows := sqlmock.NewRows([]string{
		"id", "linked_item_id", "user_id", "plaid_account_id",
		"name", "official_name", "type", "subtype", "mask",
		"current_balance", "available_balance", "currency_code",
		"nickname", "custom_color", "custom_icon", "custom_emoji",
		"display_order", "is_hidden",
		"created_at", "updated_at", "institution_name",
	}).
		AddRow(
			uuid.New().String(), uuid.New().String(), userID, "acc-1",
			"Checking", nil, "depository", "checking", "1234",
			1500.50, 1450.25, "USD",
			nil, nil, nil, nil,
			0, false,
			now, now, "Chase",
		).
		AddRow(
			uuid.New().String(), uuid.New().String(), userID, "acc-2",
			"Old Account", nil, "depository", "checking", "0000",
			0.00, 0.00, "USD",
			"Closed Account", nil, nil, nil,
			nil, true, // This one is hidden
			now, now, "Bank of America",
		)

	// Note: This query includes hidden accounts (no is_hidden filter)
	expectedQuery := `
		SELECT a.id, a.linked_item_id, a.user_id, a.plaid_account_id,
		       a.name, a.official_name, a.type, a.subtype, a.mask,
		       a.current_balance, a.available_balance, a.currency_code,
		       a.nickname, a.custom_color, a.custom_icon, a.custom_emoji,
		       a.display_order, a.is_hidden,
		       a.created_at, a.updated_at, l.institution_name
		FROM accounts a
		LEFT JOIN linked_items l ON a.linked_item_id = l.id
		WHERE a.user_id = $1 ORDER BY COALESCE(a.display_order, 999999) ASC, a.created_at ASC
	`

	mock.ExpectQuery(regexp.QuoteMeta(expectedQuery)).
		WithArgs(userID).
		WillReturnRows(rows)

	// Execute with includeHidden=true
	accounts, err := repo.GetAccountsWithInstitutionFiltered(ctx, userID, true)

	// Assert
	require.NoError(t, err)
	assert.Len(t, accounts, 2)
	assert.False(t, accounts[0].IsHidden)
	assert.True(t, accounts[1].IsHidden)
	assert.Equal(t, "Closed Account", *accounts[1].Nickname)
	assert.NoError(t, mock.ExpectationsWereMet())
}
