package repository

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupAccountMockDB creates a mock database and sqlmock instance for account repository testing
func setupAccountMockDB(t *testing.T) (pgxmock.PgxPoolIface, AccountRepository) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)

	repo := NewAccountRepository(mock, newTestFieldEncryptor(t))
	return mock, repo
}

// TestAccountRepository_Create tests successful creation of a single account
func TestAccountRepository_Create(t *testing.T) {
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

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
		(id, linked_item_id, user_id, plaid_account_id, plaid_account_id_blind_idx, name, official_name,
		 type, subtype, mask, current_balance, available_balance, currency_code,
		 persistent_account_id_blind_idx, fallback_fingerprint_blind_idx,
		 created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
	`)).WithArgs(
		account.ID,
		account.LinkedItemID,
		account.UserID,
		pgxmock.AnyArg(), // plaid_account_id (encrypted by repo)
		pgxmock.AnyArg(), // plaid_account_id_blind_idx
		pgxmock.AnyArg(), // name (encrypted by repo)
		pgxmock.AnyArg(), // official_name (encrypted by repo)
		account.Type,
		account.Subtype,
		pgxmock.AnyArg(), // mask (encrypted by repo)
		account.CurrentBalance,
		account.AvailableBalance,
		account.CurrencyCode,
		account.PersistentAccountIDBlindIdx, // nil
		account.FallbackFingerprintBlindIdx, // nil
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
	).WillReturnResult(pgxmock.NewResult("INSERT", 1))

	// Execute
	err := repo.Create(ctx, account)

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_CreateBatch tests batch creation of multiple accounts
func TestAccountRepository_CreateBatch(t *testing.T) {
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

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
			(id, linked_item_id, user_id, plaid_account_id, plaid_account_id_blind_idx, name, official_name,
			 type, subtype, mask, current_balance, available_balance, currency_code,
			 persistent_account_id_blind_idx, fallback_fingerprint_blind_idx,
			 created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		`)).WithArgs(
			acc.ID,
			acc.LinkedItemID,
			acc.UserID,
			pgxmock.AnyArg(), // plaid_account_id (encrypted by repo)
			pgxmock.AnyArg(), // plaid_account_id_blind_idx
			pgxmock.AnyArg(), // name (encrypted by repo)
			pgxmock.AnyArg(), // official_name (encrypted by repo)
			acc.Type,
			acc.Subtype,
			pgxmock.AnyArg(), // mask (encrypted by repo)
			acc.CurrentBalance,
			acc.AvailableBalance,
			acc.CurrencyCode,
			acc.PersistentAccountIDBlindIdx, // nil
			acc.FallbackFingerprintBlindIdx, // nil
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
		).WillReturnResult(pgxmock.NewResult("INSERT", 1))
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
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	linkedItemID := uuid.New().String()
	now := time.Now()

	// Helper for nullable float64 values
	balance1 := 1500.50
	available1 := 1450.25
	balance2 := 5000.00
	available2 := 5000.00

	rows := pgxmock.NewRows([]string{
		"id", "linked_item_id", "user_id", "plaid_account_id",
		"name", "official_name", "type", "subtype", "mask",
		"current_balance", "available_balance", "currency_code",
		"created_at", "updated_at",
	}).
		AddRow(
			uuid.New().String(), linkedItemID, uuid.New().String(), "acc-1",
			"Checking", strPtr("Chase Total Checking"), "depository", strPtr("checking"), strPtr("1234"),
			&balance1, &available1, "USD",
			now, now,
		).
		AddRow(
			uuid.New().String(), linkedItemID, uuid.New().String(), "acc-2",
			"Savings", nil, "depository", strPtr("savings"), strPtr("5678"),
			&balance2, &available2, "USD",
			now, now,
		)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, linked_item_id, user_id, plaid_account_id,
		       name, official_name, type, subtype, mask,
		       current_balance, available_balance, currency_code,
		       created_at, updated_at
		FROM accounts
		WHERE linked_item_id = $1 AND status = 'active'
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
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	now := time.Now()

	// Helper for nullable float64 values
	balance := 1000.00
	available := 950.00

	rows := pgxmock.NewRows([]string{
		"id", "linked_item_id", "user_id", "plaid_account_id",
		"name", "official_name", "type", "subtype", "mask",
		"current_balance", "available_balance", "currency_code",
		"status", "user_account_id", "replaced_by_id", "replaced_at",
		"created_at", "updated_at",
	}).AddRow(
		uuid.New().String(), uuid.New().String(), userID, "acc-1",
		"Checking", nil, "depository", strPtr("checking"), strPtr("1234"),
		&balance, &available, "USD",
		"active", nil, nil, nil,
		now, now,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, linked_item_id, user_id, plaid_account_id,
		       name, official_name, type, subtype, mask,
		       current_balance, available_balance, currency_code,
		       status, user_account_id, replaced_by_id, replaced_at,
		       created_at, updated_at
		FROM accounts
		WHERE user_id = $1 AND status = 'active'
		ORDER BY created_at ASC
	`)).WithArgs(userID).WillReturnRows(rows)

	// Execute
	accounts, err := repo.GetByUserID(ctx, userID)

	// Assert
	assert.NoError(t, err)
	assert.Len(t, accounts, 1)
	assert.Equal(t, userID, accounts[0].UserID)
	assert.Equal(t, domain.AccountStatusActive, accounts[0].Status)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_CountByUserID tests counting accounts for subscription limit checks
func TestAccountRepository_CountByUserID(t *testing.T) {
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	expectedCount := 5

	rows := pgxmock.NewRows([]string{"count"}).AddRow(expectedCount)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT COUNT(*)
		FROM accounts
		WHERE user_id = $1
		AND status != 'expired'
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
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

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
	// Need to match the full INSERT query with arguments for pgxmock v4
	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO accounts
		(id, linked_item_id, user_id, plaid_account_id, plaid_account_id_blind_idx, name, official_name,
		 type, subtype, mask, current_balance, available_balance, currency_code,
		 persistent_account_id_blind_idx, fallback_fingerprint_blind_idx,
		 created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
	`)).WithArgs(
		pgxmock.AnyArg(), // id
		pgxmock.AnyArg(), // linked_item_id
		pgxmock.AnyArg(), // user_id
		pgxmock.AnyArg(), // plaid_account_id
		pgxmock.AnyArg(), // plaid_account_id_blind_idx
		pgxmock.AnyArg(), // name
		pgxmock.AnyArg(), // official_name
		pgxmock.AnyArg(), // type
		pgxmock.AnyArg(), // subtype
		pgxmock.AnyArg(), // mask
		pgxmock.AnyArg(), // current_balance
		pgxmock.AnyArg(), // available_balance
		pgxmock.AnyArg(), // currency_code
		pgxmock.AnyArg(), // persistent_account_id_blind_idx
		pgxmock.AnyArg(), // fallback_fingerprint_blind_idx
		pgxmock.AnyArg(), // created_at
		pgxmock.AnyArg(), // updated_at
	).WillReturnError(errors.New("connection done")) // Simulating constraint error

	// Execute
	err := repo.Create(ctx, account)

	// Assert
	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_GetAccountsWithInstitution tests retrieval of accounts with institution names
func TestAccountRepository_GetAccountsWithInstitution(t *testing.T) {
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	linkedItemID1 := uuid.New().String()
	linkedItemID2 := uuid.New().String()

	// Helper for nullable float64 values
	bal1, avail1 := 1500.50, 1450.25
	bal2, avail2 := 5000.00, 5000.00
	bal3, avail3 := 750.00, 700.00

	// Mock the LEFT JOIN query
	rows := pgxmock.NewRows([]string{
		"id", "linked_item_id", "user_id", "plaid_account_id",
		"name", "official_name", "type", "subtype", "mask",
		"current_balance", "available_balance", "currency_code",
		"status", "user_account_id", "replaced_by_id", "replaced_at",
		"created_at", "updated_at", "institution_name",
	}).
		AddRow(
			uuid.New().String(), linkedItemID1, userID, "plaid-acc-1",
			"Chase Checking", strPtr("Chase Total Checking®"), "depository", strPtr("checking"), strPtr("1234"),
			&bal1, &avail1, "USD",
			"active", nil, nil, nil,
			time.Now(), time.Now(), "Chase",
		).
		AddRow(
			uuid.New().String(), linkedItemID1, userID, "plaid-acc-2",
			"Chase Savings", strPtr("Chase Savings℠"), "depository", strPtr("savings"), strPtr("5678"),
			&bal2, &avail2, "USD",
			"active", nil, nil, nil,
			time.Now(), time.Now(), "Chase",
		).
		AddRow(
			uuid.New().String(), linkedItemID2, userID, "plaid-acc-3",
			"Wells Fargo Checking", nil, "depository", strPtr("checking"), strPtr("9012"),
			&bal3, &avail3, "USD",
			"active", nil, nil, nil,
			time.Now(), time.Now(), "Wells Fargo",
		)

	expectedQuery := `
		SELECT a.id, a.linked_item_id, a.user_id, a.plaid_account_id,
		       a.name, a.official_name, a.type, a.subtype, a.mask,
		       a.current_balance, a.available_balance, a.currency_code,
		       a.status, a.user_account_id, a.replaced_by_id, a.replaced_at,
		       a.created_at, a.updated_at, l.institution_name
		FROM accounts a
		LEFT JOIN linked_items l ON a.linked_item_id = l.id
		WHERE a.user_id = $1 AND a.status = 'active'
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
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()

	// Mock empty result
	rows := pgxmock.NewRows([]string{
		"id", "linked_item_id", "user_id", "plaid_account_id",
		"name", "official_name", "type", "subtype", "mask",
		"current_balance", "available_balance", "currency_code",
		"status", "user_account_id", "replaced_by_id", "replaced_at",
		"created_at", "updated_at", "institution_name",
	})

	expectedQuery := `
		SELECT a.id, a.linked_item_id, a.user_id, a.plaid_account_id,
		       a.name, a.official_name, a.type, a.subtype, a.mask,
		       a.current_balance, a.available_balance, a.currency_code,
		       a.status, a.user_account_id, a.replaced_by_id, a.replaced_at,
		       a.created_at, a.updated_at, l.institution_name
		FROM accounts a
		LEFT JOIN linked_items l ON a.linked_item_id = l.id
		WHERE a.user_id = $1 AND a.status = 'active'
		ORDER BY a.created_at ASC
	`

	mock.ExpectQuery(regexp.QuoteMeta(expectedQuery)).
		WithArgs(userID).
		WillReturnRows(rows)

	// Execute
	accounts, err := repo.GetAccountsWithInstitution(ctx, userID)

	// Assert
	require.NoError(t, err)
	assert.Empty(t, accounts) // Nil slices behave identically to empty slices in Go

	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_GetAccountsWithInstitution_ExcludesExpired verifies that expired
// accounts are not returned from listing queries (Issue #1010).
func TestAccountRepository_GetAccountsWithInstitution_ExcludesExpired(t *testing.T) {
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	bal := 500.00

	// Only the active account is returned — the expired one is filtered out by the query
	rows := pgxmock.NewRows([]string{
		"id", "linked_item_id", "user_id", "plaid_account_id",
		"name", "official_name", "type", "subtype", "mask",
		"current_balance", "available_balance", "currency_code",
		"status", "user_account_id", "replaced_by_id", "replaced_at",
		"created_at", "updated_at", "institution_name",
	}).AddRow(
		uuid.New().String(), uuid.New().String(), userID, "plaid-acc-active",
		"Active Checking", nil, "depository", strPtr("checking"), strPtr("1234"),
		&bal, &bal, "USD",
		"active", nil, nil, nil,
		time.Now(), time.Now(), "Chase",
	)
	// Note: expired account not in rows — DB filters it via WHERE a.status != 'expired'

	expectedQuery := `
		SELECT a.id, a.linked_item_id, a.user_id, a.plaid_account_id,
		       a.name, a.official_name, a.type, a.subtype, a.mask,
		       a.current_balance, a.available_balance, a.currency_code,
		       a.status, a.user_account_id, a.replaced_by_id, a.replaced_at,
		       a.created_at, a.updated_at, l.institution_name
		FROM accounts a
		LEFT JOIN linked_items l ON a.linked_item_id = l.id
		WHERE a.user_id = $1 AND a.status = 'active'
		ORDER BY a.created_at ASC
	`

	mock.ExpectQuery(regexp.QuoteMeta(expectedQuery)).
		WithArgs(userID).
		WillReturnRows(rows)

	accounts, err := repo.GetAccountsWithInstitution(ctx, userID)

	require.NoError(t, err)
	assert.Len(t, accounts, 1)
	assert.Equal(t, "Active Checking", accounts[0].Name)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_Delete_Success tests successful account deletion with ownership validation
func TestAccountRepository_Delete_Success(t *testing.T) {
	// Given: Mock DB and account repository
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	accountID := uuid.New().String()
	userID := uuid.New().String()

	// Expect: DELETE query with ownership validation (WHERE id = $1 AND user_id = $2)
	expectedQuery := `DELETE FROM accounts WHERE id = $1 AND user_id = $2`
	mock.ExpectExec(regexp.QuoteMeta(expectedQuery)).
		WithArgs(accountID, userID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1)) // 1 row affected

	// When: Deleting account
	err := repo.Delete(ctx, accountID, userID)

	// Then: No error, deletion successful
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_Delete_NotFound tests deletion when account doesn't exist
func TestAccountRepository_Delete_NotFound(t *testing.T) {
	// Given: Mock DB and account repository
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	accountID := uuid.New().String()
	userID := uuid.New().String()

	// Expect: DELETE query returns 0 rows affected (account not found)
	expectedQuery := `DELETE FROM accounts WHERE id = $1 AND user_id = $2`
	mock.ExpectExec(regexp.QuoteMeta(expectedQuery)).
		WithArgs(accountID, userID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0)) // 0 rows affected

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
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	accountID := uuid.New().String()
	wrongUserID := uuid.New().String() // Different user

	// Expect: DELETE query with wrong user_id returns 0 rows affected
	expectedQuery := `DELETE FROM accounts WHERE id = $1 AND user_id = $2`
	mock.ExpectExec(regexp.QuoteMeta(expectedQuery)).
		WithArgs(accountID, wrongUserID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0)) // 0 rows affected (ownership check fails)

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
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	accountID := uuid.New().String()
	userID := uuid.New().String()

	// Expect: DELETE query returns database error
	expectedQuery := `DELETE FROM accounts WHERE id = $1 AND user_id = $2`
	mock.ExpectExec(regexp.QuoteMeta(expectedQuery)).
		WithArgs(accountID, userID).
		WillReturnError(errors.New("connection done")) // Database connection error

	// When: Attempting to delete account with DB error
	err := repo.Delete(ctx, accountID, userID)

	// Then: Returns database error
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "connection done")
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
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	accountID := uuid.New().String()
	userID := uuid.New().String()
	userAccountID := uuid.New().String()
	now := time.Now()

	// Helper for nullable float64 values
	balance, available := 1500.50, 1450.25
	displayOrder := 1

	rows := pgxmock.NewRows([]string{
		"id", "linked_item_id", "user_id", "plaid_account_id",
		"name", "official_name", "type", "subtype", "mask",
		"current_balance", "available_balance", "currency_code",
		"nickname", "custom_color", "custom_icon", "custom_emoji",
		"display_order", "is_hidden",
		"user_account_id",
		"created_at", "updated_at",
	}).AddRow(
		accountID, uuid.New().String(), userID, "plaid-acc-1",
		"Checking", strPtr("Chase Total Checking"), "depository", strPtr("checking"), strPtr("1234"),
		&balance, &available, "USD",
		strPtr("My Checking"), strPtr("#3B82F6"), strPtr("wallet"), strPtr("💰"),
		&displayOrder, false,
		&userAccountID,
		now, now,
	)

	expectedQuery := `
		SELECT id, linked_item_id, user_id, plaid_account_id,
		       name, official_name, type, subtype, mask,
		       current_balance, available_balance, currency_code,
		       nickname, custom_color, custom_icon, custom_emoji,
		       display_order, is_hidden,
		       user_account_id,
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
	assert.Equal(t, userAccountID, *account.UserAccountID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_GetByIDAndUser_NotFound tests when account doesn't exist
func TestAccountRepository_GetByIDAndUser_NotFound(t *testing.T) {
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	accountID := uuid.New().String()
	userID := uuid.New().String()

	expectedQuery := `
		SELECT id, linked_item_id, user_id, plaid_account_id,
		       name, official_name, type, subtype, mask,
		       current_balance, available_balance, currency_code,
		       nickname, custom_color, custom_icon, custom_emoji,
		       display_order, is_hidden,
		       user_account_id,
		       created_at, updated_at
		FROM accounts
		WHERE id = $1 AND user_id = $2
	`

	mock.ExpectQuery(regexp.QuoteMeta(expectedQuery)).
		WithArgs(accountID, userID).
		WillReturnError(pgx.ErrNoRows)

	// Execute
	account, err := repo.GetByIDAndUser(ctx, accountID, userID)

	// Assert
	assert.Nil(t, account)
	assert.ErrorIs(t, err, ErrAccountNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_Update_Success tests successful account update
func TestAccountRepository_Update_Success(t *testing.T) {
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

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
			account.CustomEmoji, account.DisplayOrder, account.IsHidden, pgxmock.AnyArg(),
			accountID, userID,
		).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	// Execute
	err := repo.Update(ctx, account, userID)

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_Update_NotFound tests update when account doesn't exist
func TestAccountRepository_Update_NotFound(t *testing.T) {
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

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
			account.CustomEmoji, account.DisplayOrder, account.IsHidden, pgxmock.AnyArg(),
			accountID, userID,
		).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0)) // 0 rows affected

	// Execute
	err := repo.Update(ctx, account, userID)

	// Assert
	assert.ErrorIs(t, err, ErrAccountNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_UpdateDisplayOrders_Success tests bulk display order update
func TestAccountRepository_UpdateDisplayOrders_Success(t *testing.T) {
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

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
			WithArgs(order.DisplayOrder, pgxmock.AnyArg(), order.AccountID, userID).
			WillReturnResult(pgxmock.NewResult("UPDATE", 1))
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
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

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
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

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
		WithArgs(orders[0].DisplayOrder, pgxmock.AnyArg(), orders[0].AccountID, userID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	// Second update returns 0 rows (not found)
	mock.ExpectExec(regexp.QuoteMeta(expectedQuery)).
		WithArgs(orders[1].DisplayOrder, pgxmock.AnyArg(), orders[1].AccountID, userID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))
	mock.ExpectRollback()

	// Execute
	err := repo.UpdateDisplayOrders(ctx, userID, orders)

	// Assert
	assert.ErrorIs(t, err, ErrAccountNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_GetAccountsWithInstitutionFiltered_ExcludeHidden tests filtering hidden accounts
func TestAccountRepository_GetAccountsWithInstitutionFiltered_ExcludeHidden(t *testing.T) {
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	now := time.Now()

	// Helper for nullable float64 values
	bal1, avail1 := 1500.50, 1450.25
	bal2, avail2 := 5000.00, 5000.00
	order0, order1 := 0, 1

	// Issue #679, #680: Added user_account_id, status, replaced_at, replaced_by_id, linked_item_status columns
	rows := pgxmock.NewRows([]string{
		"id", "linked_item_id", "user_id", "plaid_account_id",
		"name", "official_name", "type", "subtype", "mask",
		"current_balance", "available_balance", "currency_code",
		"nickname", "custom_color", "custom_icon", "custom_emoji",
		"display_order", "is_hidden",
		"user_account_id", "status", "replaced_at", "replaced_by_id",
		"created_at", "updated_at", "institution_name", "linked_item_status",
	}).
		AddRow(
			uuid.New().String(), uuid.New().String(), userID, "acc-1",
			"Checking", nil, "depository", strPtr("checking"), strPtr("1234"),
			&bal1, &avail1, "USD",
			strPtr("My Checking"), strPtr("#3B82F6"), strPtr("wallet"), nil,
			&order0, false,
			nil, "active", nil, nil,
			now, now, "Chase", "active",
		).
		AddRow(
			uuid.New().String(), uuid.New().String(), userID, "acc-2",
			"Savings", nil, "depository", strPtr("savings"), strPtr("5678"),
			&bal2, &avail2, "USD",
			nil, nil, nil, nil,
			&order1, false,
			nil, "active", nil, nil,
			now, now, "Chase", "active",
		)

	// Note: This query excludes hidden accounts (AND a.is_hidden = FALSE)
	// Issue #679, #680: Added user_account_id, status, replaced_at, replaced_by_id, l.status columns
	expectedQuery := `
		SELECT a.id, a.linked_item_id, a.user_id, a.plaid_account_id,
		       a.name, a.official_name, a.type, a.subtype, a.mask,
		       a.current_balance, a.available_balance, a.currency_code,
		       a.nickname, a.custom_color, a.custom_icon, a.custom_emoji,
		       a.display_order, a.is_hidden,
		       a.user_account_id, a.status, a.replaced_at, a.replaced_by_id,
		       a.created_at, a.updated_at, l.institution_name, l.status
		FROM accounts a
		LEFT JOIN linked_items l ON a.linked_item_id = l.id
		WHERE a.user_id = $1 AND a.status = 'active' AND a.is_hidden = FALSE ORDER BY COALESCE(a.display_order, 999999) ASC, a.created_at ASC
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
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	now := time.Now()

	// Helper for nullable float64 values
	bal1, avail1 := 1500.50, 1450.25
	bal2, avail2 := 0.00, 0.00
	order0 := 0

	// Issue #679, #680: Added user_account_id, status, replaced_at, replaced_by_id, linked_item_status columns
	rows := pgxmock.NewRows([]string{
		"id", "linked_item_id", "user_id", "plaid_account_id",
		"name", "official_name", "type", "subtype", "mask",
		"current_balance", "available_balance", "currency_code",
		"nickname", "custom_color", "custom_icon", "custom_emoji",
		"display_order", "is_hidden",
		"user_account_id", "status", "replaced_at", "replaced_by_id",
		"created_at", "updated_at", "institution_name", "linked_item_status",
	}).
		AddRow(
			uuid.New().String(), uuid.New().String(), userID, "acc-1",
			"Checking", nil, "depository", strPtr("checking"), strPtr("1234"),
			&bal1, &avail1, "USD",
			nil, nil, nil, nil,
			&order0, false,
			nil, "active", nil, nil,
			now, now, "Chase", "active",
		).
		AddRow(
			uuid.New().String(), uuid.New().String(), userID, "acc-2",
			"Old Account", nil, "depository", strPtr("checking"), strPtr("0000"),
			&bal2, &avail2, "USD",
			strPtr("Closed Account"), nil, nil, nil,
			nil, true, // This one is hidden
			nil, "active", nil, nil,
			now, now, "Bank of America", "active",
		)

	// Note: This query includes hidden accounts (no is_hidden filter)
	// Issue #679, #680: Added user_account_id, status, replaced_at, replaced_by_id, l.status columns
	expectedQuery := `
		SELECT a.id, a.linked_item_id, a.user_id, a.plaid_account_id,
		       a.name, a.official_name, a.type, a.subtype, a.mask,
		       a.current_balance, a.available_balance, a.currency_code,
		       a.nickname, a.custom_color, a.custom_icon, a.custom_emoji,
		       a.display_order, a.is_hidden,
		       a.user_account_id, a.status, a.replaced_at, a.replaced_by_id,
		       a.created_at, a.updated_at, l.institution_name, l.status
		FROM accounts a
		LEFT JOIN linked_items l ON a.linked_item_id = l.id
		WHERE a.user_id = $1 AND a.status = 'active' ORDER BY COALESCE(a.display_order, 999999) ASC, a.created_at ASC
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

// ============================================================================
// Issue #171: Re-link Handling Tests
// ============================================================================

// TestAccountRepository_GetByUserAccountAndStatus_Active tests finding active accounts by user_account_id
func TestAccountRepository_GetByUserAccountAndStatus_Active(t *testing.T) {
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userAccountID := uuid.New().String()
	now := time.Now()

	bal1 := 1500.50
	avail1 := 1450.25

	// Expected columns including Issue #171 fields
	rows := pgxmock.NewRows([]string{
		"id", "linked_item_id", "user_id", "plaid_account_id",
		"name", "official_name", "type", "subtype", "mask",
		"current_balance", "available_balance", "currency_code",
		"user_account_id", "status", "replaced_at", "replaced_by_id",
		"nickname", "custom_color", "custom_icon", "custom_emoji", "display_order", "is_hidden",
		"created_at", "updated_at",
	}).
		AddRow(
			uuid.New().String(), uuid.New().String(), uuid.New().String(), "acc-1",
			"Checking", strPtr("Chase Total Checking"), "depository", strPtr("checking"), strPtr("1234"),
			&bal1, &avail1, "USD",
			&userAccountID, "active", nil, nil,
			nil, nil, nil, nil, nil, false,
			now, now,
		)

	mock.ExpectQuery("SELECT.*FROM accounts.*WHERE user_account_id.*AND status").
		WithArgs(userAccountID, "active").
		WillReturnRows(rows)

	// Execute - this method should exist for stable queries across re-links
	accounts, err := repo.GetByUserAccountAndStatus(ctx, userAccountID, "active")

	// Assert
	assert.NoError(t, err)
	assert.Len(t, accounts, 1)
	assert.Equal(t, userAccountID, *accounts[0].UserAccountID)
	assert.Equal(t, domain.AccountStatusActive, accounts[0].Status)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_GetByUserAccountAndStatus_Replaced tests finding replaced accounts
func TestAccountRepository_GetByUserAccountAndStatus_Replaced(t *testing.T) {
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userAccountID := uuid.New().String()
	replacedByID := uuid.New().String()
	now := time.Now()
	replacedAt := now.Add(-time.Hour)

	bal1 := 1500.50
	avail1 := 1450.25

	rows := pgxmock.NewRows([]string{
		"id", "linked_item_id", "user_id", "plaid_account_id",
		"name", "official_name", "type", "subtype", "mask",
		"current_balance", "available_balance", "currency_code",
		"user_account_id", "status", "replaced_at", "replaced_by_id",
		"nickname", "custom_color", "custom_icon", "custom_emoji", "display_order", "is_hidden",
		"created_at", "updated_at",
	}).
		AddRow(
			uuid.New().String(), uuid.New().String(), uuid.New().String(), "acc-old",
			"Checking", strPtr("Chase Total Checking"), "depository", strPtr("checking"), strPtr("1234"),
			&bal1, &avail1, "USD",
			&userAccountID, "replaced", &replacedAt, &replacedByID,
			nil, nil, nil, nil, nil, false,
			now, now,
		)

	mock.ExpectQuery("SELECT.*FROM accounts.*WHERE user_account_id.*AND status").
		WithArgs(userAccountID, "replaced").
		WillReturnRows(rows)

	// Execute
	accounts, err := repo.GetByUserAccountAndStatus(ctx, userAccountID, "replaced")

	// Assert
	assert.NoError(t, err)
	assert.Len(t, accounts, 1)
	assert.Equal(t, domain.AccountStatusReplaced, accounts[0].Status)
	assert.NotNil(t, accounts[0].ReplacedAt)
	assert.NotNil(t, accounts[0].ReplacedByID)
	assert.Equal(t, replacedByID, *accounts[0].ReplacedByID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_GetByUserAccountAndStatus_NotFound tests when no matching accounts exist
func TestAccountRepository_GetByUserAccountAndStatus_NotFound(t *testing.T) {
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userAccountID := uuid.New().String()

	rows := pgxmock.NewRows([]string{
		"id", "linked_item_id", "user_id", "plaid_account_id",
		"name", "official_name", "type", "subtype", "mask",
		"current_balance", "available_balance", "currency_code",
		"user_account_id", "status", "replaced_at", "replaced_by_id",
		"nickname", "custom_color", "custom_icon", "custom_emoji", "display_order", "is_hidden",
		"created_at", "updated_at",
	})

	mock.ExpectQuery("SELECT.*FROM accounts.*WHERE user_account_id.*AND status").
		WithArgs(userAccountID, "active").
		WillReturnRows(rows)

	// Execute
	accounts, err := repo.GetByUserAccountAndStatus(ctx, userAccountID, "active")

	// Assert - should return empty slice (not an error) when no match found
	assert.NoError(t, err)
	assert.Empty(t, accounts)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_GetByUserAccountAndStatus_MultipleAccounts tests accounts across re-links
func TestAccountRepository_GetByUserAccountAndStatus_MultipleAccounts(t *testing.T) {
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userAccountID := uuid.New().String()
	now := time.Now()
	replacedAt := now.Add(-time.Hour)

	bal1 := 1500.50
	avail1 := 1450.25
	bal2 := 1600.00
	avail2 := 1550.00

	// Mock two accounts with same user_account_id - one replaced, one active
	// This simulates a re-link scenario
	rows := pgxmock.NewRows([]string{
		"id", "linked_item_id", "user_id", "plaid_account_id",
		"name", "official_name", "type", "subtype", "mask",
		"current_balance", "available_balance", "currency_code",
		"user_account_id", "status", "replaced_at", "replaced_by_id",
		"nickname", "custom_color", "custom_icon", "custom_emoji", "display_order", "is_hidden",
		"created_at", "updated_at",
	}).
		AddRow(
			uuid.New().String(), uuid.New().String(), uuid.New().String(), "acc-old",
			"Checking", strPtr("Chase Total Checking"), "depository", strPtr("checking"), strPtr("1234"),
			&bal1, &avail1, "USD",
			&userAccountID, "replaced", &replacedAt, nil,
			nil, nil, nil, nil, nil, false,
			now.Add(-2*time.Hour), now,
		).
		AddRow(
			uuid.New().String(), uuid.New().String(), uuid.New().String(), "acc-new",
			"Checking", strPtr("Chase Total Checking"), "depository", strPtr("checking"), strPtr("1234"),
			&bal2, &avail2, "USD",
			&userAccountID, "active", nil, nil,
			nil, nil, nil, nil, nil, false,
			now, now,
		)

	// When querying without status filter (nil), return all
	mock.ExpectQuery("SELECT.*FROM accounts.*WHERE user_account_id").
		WithArgs(userAccountID).
		WillReturnRows(rows)

	// Execute - get all accounts for a user_account (both active and replaced)
	accounts, err := repo.GetByUserAccountID(ctx, userAccountID)

	// Assert
	assert.NoError(t, err)
	assert.Len(t, accounts, 2)
	// First should be replaced (older)
	assert.Equal(t, domain.AccountStatusReplaced, accounts[0].Status)
	// Second should be active (newer)
	assert.Equal(t, domain.AccountStatusActive, accounts[1].Status)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_UpdateStatus tests updating account status for soft-delete
func TestAccountRepository_UpdateStatus(t *testing.T) {
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	accountID := uuid.New().String()
	userID := uuid.New().String()
	replacedByID := uuid.New().String()

	// Expect update to set status, replaced_at, and replaced_by_id
	mock.ExpectExec("UPDATE accounts.*SET.*status.*replaced_at.*replaced_by_id").
		WithArgs(
			pgxmock.AnyArg(), // status
			pgxmock.AnyArg(), // replaced_at
			pgxmock.AnyArg(), // replaced_by_id
			pgxmock.AnyArg(), // updated_at
			pgxmock.AnyArg(), // id
			pgxmock.AnyArg(), // user_id
		).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	// Execute
	err := repo.UpdateStatus(ctx, accountID, userID, domain.AccountStatusReplaced, &replacedByID)

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_UpdateStatus_NotFound tests updating status for non-existent account
func TestAccountRepository_UpdateStatus_NotFound(t *testing.T) {
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	accountID := uuid.New().String()
	userID := uuid.New().String()

	mock.ExpectExec("UPDATE accounts.*SET.*status.*replaced_at.*replaced_by_id").
		WithArgs(
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
		).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	// Execute
	err := repo.UpdateStatus(ctx, accountID, userID, domain.AccountStatusReplaced, nil)

	// Assert
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrAccountNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// ============================================================================
// Issue #668: Plaid Item Management for Cost Optimization Tests
// ============================================================================

// TestAccountRepository_CountActiveByLinkedItemID tests counting active accounts for an item
func TestAccountRepository_CountActiveByLinkedItemID(t *testing.T) {
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	linkedItemID := uuid.New().String()
	expectedCount := 3

	rows := pgxmock.NewRows([]string{"count"}).AddRow(expectedCount)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT COUNT(*)
		FROM accounts
		WHERE linked_item_id = $1 AND status = 'active'
	`)).WithArgs(linkedItemID).WillReturnRows(rows)

	// Execute
	count, err := repo.CountActiveByLinkedItemID(ctx, linkedItemID)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, expectedCount, count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_CountActiveByLinkedItemID_ZeroAccounts tests count when item has no active accounts
func TestAccountRepository_CountActiveByLinkedItemID_ZeroAccounts(t *testing.T) {
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	linkedItemID := uuid.New().String()

	rows := pgxmock.NewRows([]string{"count"}).AddRow(0)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT COUNT(*)
		FROM accounts
		WHERE linked_item_id = $1 AND status = 'active'
	`)).WithArgs(linkedItemID).WillReturnRows(rows)

	// Execute
	count, err := repo.CountActiveByLinkedItemID(ctx, linkedItemID)

	// Assert - count should be 0, no error
	assert.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_CountActiveByLinkedItemID_ExcludesReplaced tests that replaced accounts are not counted
func TestAccountRepository_CountActiveByLinkedItemID_ExcludesReplaced(t *testing.T) {
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	linkedItemID := uuid.New().String()

	// Only 2 active accounts (3 total - 1 replaced = 2 active)
	expectedCount := 2

	rows := pgxmock.NewRows([]string{"count"}).AddRow(expectedCount)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT COUNT(*)
		FROM accounts
		WHERE linked_item_id = $1 AND status = 'active'
	`)).WithArgs(linkedItemID).WillReturnRows(rows)

	// Execute
	count, err := repo.CountActiveByLinkedItemID(ctx, linkedItemID)

	// Assert - should exclude replaced accounts
	assert.NoError(t, err)
	assert.Equal(t, expectedCount, count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_CountActiveByLinkedItemID_DBError tests database error handling
func TestAccountRepository_CountActiveByLinkedItemID_DBError(t *testing.T) {
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	linkedItemID := uuid.New().String()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT COUNT(*)
		FROM accounts
		WHERE linked_item_id = $1 AND status = 'active'
	`)).WithArgs(linkedItemID).WillReturnError(errors.New("database connection error"))

	// Execute
	count, err := repo.CountActiveByLinkedItemID(ctx, linkedItemID)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, 0, count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// =============================================================================
// Issue #1010: Cross-user fingerprint uniqueness tests
// =============================================================================

// TestAccountRepository_CheckFingerprintOwnership_ReturnsOwner verifies that when
// another user owns an account with the given fingerprint, ownership info is returned.
func TestAccountRepository_CheckFingerprintOwnership_ReturnsOwner(t *testing.T) {
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	fingerprint := "test-fingerprint-blind-idx"
	requestingUserID := uuid.New().String()
	ownerUserID := uuid.New().String()
	linkedItemID := uuid.New().String()
	now := time.Now().Add(-30 * 24 * time.Hour) // 30 days ago

	rows := pgxmock.NewRows([]string{"user_id", "linked_item_id", "status", "status_changed_at"}).
		AddRow(ownerUserID, linkedItemID, "active", &now)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT a.user_id, a.linked_item_id, li.status, li.status_changed_at
		FROM accounts a
		JOIN linked_items li ON a.linked_item_id = li.id
		WHERE (a.persistent_account_id_blind_idx = $1 OR a.fallback_fingerprint_blind_idx = $1)
		  AND a.status = 'active'
		  AND a.user_id != $2
		LIMIT 1
	`)).WithArgs(fingerprint, requestingUserID).WillReturnRows(rows)

	ownership, err := repo.CheckFingerprintOwnership(ctx, fingerprint, requestingUserID)

	require.NoError(t, err)
	require.NotNil(t, ownership)
	assert.Equal(t, ownerUserID, ownership.UserID)
	assert.Equal(t, linkedItemID, ownership.LinkedItemID)
	assert.Equal(t, domain.LinkedItemStatusActive, ownership.ItemStatus)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_CheckFingerprintOwnership_NotFound verifies that nil is
// returned when no other user owns the fingerprint.
func TestAccountRepository_CheckFingerprintOwnership_NotFound(t *testing.T) {
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	fingerprint := "test-fingerprint-blind-idx"
	requestingUserID := uuid.New().String()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT a.user_id, a.linked_item_id, li.status, li.status_changed_at
		FROM accounts a
		JOIN linked_items li ON a.linked_item_id = li.id
		WHERE (a.persistent_account_id_blind_idx = $1 OR a.fallback_fingerprint_blind_idx = $1)
		  AND a.status = 'active'
		  AND a.user_id != $2
		LIMIT 1
	`)).WithArgs(fingerprint, requestingUserID).WillReturnRows(pgxmock.NewRows([]string{"user_id", "linked_item_id", "status", "status_changed_at"}))

	ownership, err := repo.CheckFingerprintOwnership(ctx, fingerprint, requestingUserID)

	require.NoError(t, err)
	assert.Nil(t, ownership)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_ExpireAccountsByFingerprint verifies that accounts matching
// a fingerprint are marked as 'expired' to release the DB uniqueness constraint.
func TestAccountRepository_ExpireAccountsByFingerprint(t *testing.T) {
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	fingerprint := "test-fingerprint-blind-idx"

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE accounts
		SET status = 'expired', updated_at = NOW()
		WHERE (persistent_account_id_blind_idx = $1 OR fallback_fingerprint_blind_idx = $1)
		  AND status = 'active'
	`)).WithArgs(fingerprint).WillReturnResult(pgxmock.NewResult("UPDATE", 2))

	err := repo.ExpireAccountsByFingerprint(ctx, fingerprint)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_GetByUserID_ExcludesReplaced verifies that replaced accounts
// are not returned by GetByUserID (Issue #1109: re-link chain transparency).
func TestAccountRepository_GetByUserID_ExcludesReplaced(t *testing.T) {
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	now := time.Now()
	balance := 1000.00

	// Only the active account row is returned — DB filters replaced via WHERE status = 'active'
	rows := pgxmock.NewRows([]string{
		"id", "linked_item_id", "user_id", "plaid_account_id",
		"name", "official_name", "type", "subtype", "mask",
		"current_balance", "available_balance", "currency_code",
		"status", "user_account_id", "replaced_by_id", "replaced_at",
		"created_at", "updated_at",
	}).AddRow(
		uuid.New().String(), uuid.New().String(), userID, "acc-active",
		"Active Checking", nil, "depository", strPtr("checking"), strPtr("1234"),
		&balance, &balance, "USD",
		"active", nil, nil, nil,
		now, now,
	)
	// Note: replaced account not in rows — DB filters it via WHERE status = 'active'

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, linked_item_id, user_id, plaid_account_id,
		       name, official_name, type, subtype, mask,
		       current_balance, available_balance, currency_code,
		       status, user_account_id, replaced_by_id, replaced_at,
		       created_at, updated_at
		FROM accounts
		WHERE user_id = $1 AND status = 'active'
		ORDER BY created_at ASC
	`)).WithArgs(userID).WillReturnRows(rows)

	accounts, err := repo.GetByUserID(ctx, userID)

	require.NoError(t, err)
	assert.Len(t, accounts, 1)
	assert.Equal(t, "Active Checking", accounts[0].Name)
	assert.Equal(t, domain.AccountStatusActive, accounts[0].Status)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_GetAccountsWithInstitution_ExcludesReplaced verifies that
// replaced accounts are not returned (Issue #1109).
func TestAccountRepository_GetAccountsWithInstitution_ExcludesReplaced(t *testing.T) {
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	bal := 500.00

	// Only the active account is returned — replaced one filtered by WHERE status = 'active'
	rows := pgxmock.NewRows([]string{
		"id", "linked_item_id", "user_id", "plaid_account_id",
		"name", "official_name", "type", "subtype", "mask",
		"current_balance", "available_balance", "currency_code",
		"status", "user_account_id", "replaced_by_id", "replaced_at",
		"created_at", "updated_at", "institution_name",
	}).AddRow(
		uuid.New().String(), uuid.New().String(), userID, "plaid-acc-active",
		"Active Checking", nil, "depository", strPtr("checking"), strPtr("1234"),
		&bal, &bal, "USD",
		"active", nil, nil, nil,
		time.Now(), time.Now(), "Chase",
	)

	expectedQuery := `
		SELECT a.id, a.linked_item_id, a.user_id, a.plaid_account_id,
		       a.name, a.official_name, a.type, a.subtype, a.mask,
		       a.current_balance, a.available_balance, a.currency_code,
		       a.status, a.user_account_id, a.replaced_by_id, a.replaced_at,
		       a.created_at, a.updated_at, l.institution_name
		FROM accounts a
		LEFT JOIN linked_items l ON a.linked_item_id = l.id
		WHERE a.user_id = $1 AND a.status = 'active'
		ORDER BY a.created_at ASC
	`

	mock.ExpectQuery(regexp.QuoteMeta(expectedQuery)).
		WithArgs(userID).
		WillReturnRows(rows)

	accounts, err := repo.GetAccountsWithInstitution(ctx, userID)

	require.NoError(t, err)
	assert.Len(t, accounts, 1)
	assert.Equal(t, "Active Checking", accounts[0].Name)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_GetByLinkedItemID_ExcludesReplaced verifies that replaced
// accounts sharing the same linked_item_id are not returned (Issue #1109).
func TestAccountRepository_GetByLinkedItemID_ExcludesReplaced(t *testing.T) {
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	linkedItemID := uuid.New().String()
	now := time.Now()
	balance := 1500.50
	available := 1450.25

	// Only the active account is returned — replaced one filtered by WHERE status = 'active'
	rows := pgxmock.NewRows([]string{
		"id", "linked_item_id", "user_id", "plaid_account_id",
		"name", "official_name", "type", "subtype", "mask",
		"current_balance", "available_balance", "currency_code",
		"created_at", "updated_at",
	}).AddRow(
		uuid.New().String(), linkedItemID, uuid.New().String(), "acc-active",
		"Active Checking", nil, "depository", strPtr("checking"), strPtr("1234"),
		&balance, &available, "USD",
		now, now,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, linked_item_id, user_id, plaid_account_id,
		       name, official_name, type, subtype, mask,
		       current_balance, available_balance, currency_code,
		       created_at, updated_at
		FROM accounts
		WHERE linked_item_id = $1 AND status = 'active'
		ORDER BY created_at ASC
	`)).WithArgs(linkedItemID).WillReturnRows(rows)

	accounts, err := repo.GetByLinkedItemID(ctx, linkedItemID)

	require.NoError(t, err)
	assert.Len(t, accounts, 1)
	assert.Equal(t, "Active Checking", accounts[0].Name)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_CountByUserID_ExcludesExpired verifies that expired accounts
// are not counted towards a user's account quota (Issue #1010).
func TestAccountRepository_CountByUserID_ExcludesExpired(t *testing.T) {
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	expectedCount := 2 // 2 active, 1 expired — only active/replaced counted

	rows := pgxmock.NewRows([]string{"count"}).AddRow(expectedCount)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT COUNT(*)
		FROM accounts
		WHERE user_id = $1
		AND status != 'expired'
	`)).WithArgs(userID).WillReturnRows(rows)

	count, err := repo.CountByUserID(ctx, userID)

	assert.NoError(t, err)
	assert.Equal(t, expectedCount, count)
	assert.NoError(t, mock.ExpectationsWereMet())
}
