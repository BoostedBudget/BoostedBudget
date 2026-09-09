package repository

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/pashagolub/pgxmock/v4"
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
			(id, linked_item_id, user_id, user_account_id, plaid_account_id, plaid_account_id_blind_idx, name, official_name,
			 type, subtype, mask, current_balance, available_balance, currency_code,
			 persistent_account_id_blind_idx, fallback_fingerprint_blind_idx,
			 created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
		`)).WithArgs(
			acc.ID,
			acc.LinkedItemID,
			acc.UserID,
			acc.UserAccountID, // nil
			pgxmock.AnyArg(),  // plaid_account_id (encrypted by repo)
			pgxmock.AnyArg(),  // plaid_account_id_blind_idx
			pgxmock.AnyArg(),  // name (encrypted by repo)
			pgxmock.AnyArg(),  // official_name (encrypted by repo)
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
	err := repo.CreateBatch(ctx, userID, accounts)

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
		"status", "user_account_id", "replaced_by_id", "replaced_at",
		"exclude_investment_sync",
		"created_at", "updated_at",
	}).
		AddRow(
			uuid.New().String(), linkedItemID, uuid.New().String(), "acc-1",
			"Checking", strPtr("Chase Total Checking"), "depository", strPtr("checking"), strPtr("1234"),
			&balance1, &available1, "USD",
			"active", nil, nil, nil,
			false,
			now, now,
		).
		AddRow(
			uuid.New().String(), linkedItemID, uuid.New().String(), "acc-2",
			"Savings", nil, "depository", strPtr("savings"), strPtr("5678"),
			&balance2, &available2, "USD",
			"active", nil, nil, nil,
			false,
			now, now,
		)

	// The balance columns come from accountBalanceProjection rather than being written out here:
	// a hand-copied projection is a restatement that can drift from the query, and #1569 task 21
	// (item D) changed exactly these two columns in ten places at once. Calling the renderer means
	// this expectation cannot disagree with what the method emits.
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT a.id, a.linked_item_id, a.user_id, a.plaid_account_id,
		       a.name, a.official_name, a.type, a.subtype, a.mask,
		       ` + accountBalanceProjection("a") + `, COALESCE(a.currency_code, 'USD') AS currency_code,
		       a.status, a.user_account_id, a.replaced_by_id, a.replaced_at,
		       COALESCE(a_ua.exclude_investment_sync, FALSE) AS exclude_investment_sync,
		       a.created_at, a.updated_at
		FROM accounts a` + accountCustomizationJoin("a") + `
		WHERE a.linked_item_id = $1 AND a.status = 'active'
		ORDER BY a.created_at ASC
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
		"exclude_investment_sync",
		"created_at", "updated_at",
	}).AddRow(
		uuid.New().String(), uuid.New().String(), userID, "acc-1",
		"Checking", nil, "depository", strPtr("checking"), strPtr("1234"),
		&balance, &available, "USD",
		"active", nil, nil, nil,
		false,
		now, now,
	)

	// #1618b: aliased `a` and given accountCustomizationJoin so this read (dashboard_service.go's
	// net-worth/balance-summary path, account_chain_resolver.go, savings_goal_service.go) surfaces
	// exclude_investment_sync instead of reading it back as an unconditional false.
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT a.id, a.linked_item_id, a.user_id, a.plaid_account_id,
		       a.name, a.official_name, a.type, a.subtype, a.mask,
		       ` + accountBalanceProjection("a") + `, COALESCE(a.currency_code, 'USD') AS currency_code,
		       a.status, a.user_account_id, a.replaced_by_id, a.replaced_at,
		       COALESCE(a_ua.exclude_investment_sync, FALSE) AS exclude_investment_sync,
		       a.created_at, a.updated_at
		FROM accounts a` + accountCustomizationJoin("a") + `
		WHERE a.user_id = $1 AND a.status = 'active'
		ORDER BY a.created_at ASC
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
		AND status = 'active'
	`)).WithArgs(userID).WillReturnRows(rows)

	// Execute
	count, err := repo.CountByUserID(ctx, userID)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, expectedCount, count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_CountByUserID_ExcludesRemoved pins the SQL TEXT that keeps a retained
// 'removed' account (Issue #1569: accounts are retained rather than deleted) out of the count.
//
// ⚠ It cannot pin the BEHAVIOUR: pgxmock never executes the query, and the row count below is one
// this test made up. The behavioural proof — including the 'replaced' case this text used to let
// through, residual E10 — is
// TestAccountRepository_CountByUserID_Integration_CountsOneRowPerRealAccount, against Postgres.
func TestAccountRepository_CountByUserID_ExcludesRemoved(t *testing.T) {
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	expectedCount := 2 // 2 active, 1 removed — removed must not count

	rows := pgxmock.NewRows([]string{"count"}).AddRow(expectedCount)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT COUNT(*)
		FROM accounts
		WHERE user_id = $1
		AND status = 'active'
	`)).WithArgs(userID).WillReturnRows(rows)

	count, err := repo.CountByUserID(ctx, userID)

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
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO accounts
		(id, linked_item_id, user_id, user_account_id, plaid_account_id, plaid_account_id_blind_idx, name, official_name,
		 type, subtype, mask, current_balance, available_balance, currency_code,
		 persistent_account_id_blind_idx, fallback_fingerprint_blind_idx,
		 created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
	`)).WithArgs(
		pgxmock.AnyArg(), // id
		pgxmock.AnyArg(), // linked_item_id
		pgxmock.AnyArg(), // user_id
		pgxmock.AnyArg(), // user_account_id
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
	mock.ExpectRollback()

	// Execute — CreateBatch is the only production insert path (#1565).
	err := repo.CreateBatch(ctx, account.UserID, []domain.Account{*account})

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
		"status", "user_account_id", "replaced_by_id", "replaced_at", "removed_at",
		"exclude_investment_sync",
		"created_at", "updated_at", "institution_name",
	}).
		AddRow(
			uuid.New().String(), linkedItemID1, userID, "plaid-acc-1",
			"Chase Checking", strPtr("Chase Total Checking®"), "depository", strPtr("checking"), strPtr("1234"),
			&bal1, &avail1, "USD",
			"active", nil, nil, nil, nil,
			false,
			time.Now(), time.Now(), "Chase",
		).
		AddRow(
			uuid.New().String(), linkedItemID1, userID, "plaid-acc-2",
			"Chase Savings", strPtr("Chase Savings℠"), "depository", strPtr("savings"), strPtr("5678"),
			&bal2, &avail2, "USD",
			"active", nil, nil, nil, nil,
			false,
			time.Now(), time.Now(), "Chase",
		).
		AddRow(
			uuid.New().String(), linkedItemID2, userID, "plaid-acc-3",
			"Wells Fargo Checking", nil, "depository", strPtr("checking"), strPtr("9012"),
			&bal3, &avail3, "USD",
			"active", nil, nil, nil, nil,
			false,
			time.Now(), time.Now(), "Wells Fargo",
		)

	expectedQuery := `
		SELECT a.id, a.linked_item_id, a.user_id, a.plaid_account_id,
		       a.name, a.official_name, a.type, a.subtype, a.mask,
		       ` + accountBalanceProjection("a") + `, COALESCE(a.currency_code, 'USD') AS currency_code,
		       a.status, a.user_account_id, a.replaced_by_id, a.replaced_at, a.removed_at,
		       COALESCE(a_ua.exclude_investment_sync, FALSE) AS exclude_investment_sync,
		       a.created_at, a.updated_at, COALESCE(l.institution_name, '') AS institution_name
		FROM accounts a
		LEFT JOIN linked_items l ON a.linked_item_id = l.id` + accountCustomizationJoin("a") + `
		WHERE a.user_id = $1 AND a.status = 'active'
		ORDER BY a.created_at ASC, a.id ASC
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
		"status", "user_account_id", "replaced_by_id", "replaced_at", "removed_at",
		"exclude_investment_sync",
		"created_at", "updated_at", "institution_name",
	})

	expectedQuery := `
		SELECT a.id, a.linked_item_id, a.user_id, a.plaid_account_id,
		       a.name, a.official_name, a.type, a.subtype, a.mask,
		       ` + accountBalanceProjection("a") + `, COALESCE(a.currency_code, 'USD') AS currency_code,
		       a.status, a.user_account_id, a.replaced_by_id, a.replaced_at, a.removed_at,
		       COALESCE(a_ua.exclude_investment_sync, FALSE) AS exclude_investment_sync,
		       a.created_at, a.updated_at, COALESCE(l.institution_name, '') AS institution_name
		FROM accounts a
		LEFT JOIN linked_items l ON a.linked_item_id = l.id` + accountCustomizationJoin("a") + `
		WHERE a.user_id = $1 AND a.status = 'active'
		ORDER BY a.created_at ASC, a.id ASC
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
		"status", "user_account_id", "replaced_by_id", "replaced_at", "removed_at",
		"exclude_investment_sync",
		"created_at", "updated_at", "institution_name",
	}).AddRow(
		uuid.New().String(), uuid.New().String(), userID, "plaid-acc-active",
		"Active Checking", nil, "depository", strPtr("checking"), strPtr("1234"),
		&bal, &bal, "USD",
		"active", nil, nil, nil, nil,
		false,
		time.Now(), time.Now(), "Chase",
	)
	// Note: expired account not in rows — DB filters it via WHERE a.status != 'expired'

	expectedQuery := `
		SELECT a.id, a.linked_item_id, a.user_id, a.plaid_account_id,
		       a.name, a.official_name, a.type, a.subtype, a.mask,
		       ` + accountBalanceProjection("a") + `, COALESCE(a.currency_code, 'USD') AS currency_code,
		       a.status, a.user_account_id, a.replaced_by_id, a.replaced_at, a.removed_at,
		       COALESCE(a_ua.exclude_investment_sync, FALSE) AS exclude_investment_sync,
		       a.created_at, a.updated_at, COALESCE(l.institution_name, '') AS institution_name
		FROM accounts a
		LEFT JOIN linked_items l ON a.linked_item_id = l.id` + accountCustomizationJoin("a") + `
		WHERE a.user_id = $1 AND a.status = 'active'
		ORDER BY a.created_at ASC, a.id ASC
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
		"exclude_investment_sync",
		"user_account_id", "status", "removed_at", "replaced_at", "expired_at", "replaced_by_id",
		"created_at", "updated_at",
	}).AddRow(
		accountID, uuid.New().String(), userID, "plaid-acc-1",
		"Checking", strPtr("Chase Total Checking"), "depository", strPtr("checking"), strPtr("1234"),
		&balance, &available, "USD",
		strPtr("My Checking"), strPtr("#3B82F6"), strPtr("wallet"), strPtr("💰"),
		&displayOrder, false,
		false,
		&userAccountID, domain.AccountStatusActive, nil, nil, nil, nil,
		now, now,
	)

	expectedQuery := `
		SELECT a.id, a.linked_item_id, a.user_id, a.plaid_account_id,
		       a.name, a.official_name, a.type, a.subtype, a.mask,
		       ` + accountBalanceProjection("a") + `, COALESCE(a.currency_code, 'USD') AS currency_code,
		       ` + accountCustomizationProjection("a") + `,
		       COALESCE(a_ua.exclude_investment_sync, FALSE) AS exclude_investment_sync,
		       a.user_account_id, a.status, a.removed_at, a.replaced_at, a.expired_at, a.replaced_by_id,
		       a.created_at, a.updated_at
		FROM accounts a` + accountCustomizationJoin("a") + `
		WHERE a.id = $1 AND a.user_id = $2
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
	// Issue #1569: GetByIDAndUser previously did not select `status` at all, so any caller
	// (e.g. savings_progress_service.go) always saw the zero value regardless of the account's
	// real status — a retained (replaced/expired/removed) account was indistinguishable from
	// active.
	assert.Equal(t, domain.AccountStatusActive, account.Status)
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
		SELECT a.id, a.linked_item_id, a.user_id, a.plaid_account_id,
		       a.name, a.official_name, a.type, a.subtype, a.mask,
		       ` + accountBalanceProjection("a") + `, COALESCE(a.currency_code, 'USD') AS currency_code,
		       ` + accountCustomizationProjection("a") + `,
		       COALESCE(a_ua.exclude_investment_sync, FALSE) AS exclude_investment_sync,
		       a.user_account_id, a.status, a.removed_at, a.replaced_at, a.expired_at, a.replaced_by_id,
		       a.created_at, a.updated_at
		FROM accounts a` + accountCustomizationJoin("a") + `
		WHERE a.id = $1 AND a.user_id = $2
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

	mock.ExpectBegin()
	// #1609: every order first resolves whether the account is chained, because the target column
	// follows the chain. Both accounts here are UNCHAINED (NULL user_account_id), so both take the
	// legacy accounts leg — the no-regression half of the routing.
	//
	// ⚠ TWO PASSES, and the expectation order pins that. Fix round 1 (F2) moved the writes behind
	// a full resolve pass, because a discarded duplicate must still be validated and because the
	// collapse cannot know a target's lowest order until every entry has been resolved.
	for _, order := range orders {
		mock.ExpectQuery(regexp.QuoteMeta(displayOrderResolveChainQuery)).
			WithArgs(order.AccountID, userID).
			WillReturnRows(pgxmock.NewRows([]string{"user_account_id"}).AddRow(nil))
	}
	for _, order := range orders {
		mock.ExpectExec(regexp.QuoteMeta(displayOrderAccountsSideQuery)).
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

	mock.ExpectBegin()
	// The first account resolves (unchained: a NULL chain id).
	mock.ExpectQuery(regexp.QuoteMeta(displayOrderResolveChainQuery)).
		WithArgs(orders[0].AccountID, userID).
		WillReturnRows(pgxmock.NewRows([]string{"user_account_id"}).AddRow(nil))
	// The second account does not exist, so the RESOLVE finds no row — #1609 moved the
	// not-found detection ahead of the UPDATE so that "no such account" stays distinguishable
	// from "this account is simply not chained".
	//
	// ⚠ NO Exec is expected at all. Fix round 1 (F2) put every resolve ahead of every write, so a
	// rejected batch never reaches the write pass — the rollback has nothing to undo rather than
	// one statement's worth. That is a strictly narrower window, and pgxmock's ordered
	// expectations are what pin it: an interleaved implementation fails here.
	mock.ExpectQuery(regexp.QuoteMeta(displayOrderResolveChainQuery)).
		WithArgs(orders[1].AccountID, userID).
		WillReturnError(pgx.ErrNoRows)
	mock.ExpectRollback()

	// Execute
	err := repo.UpdateDisplayOrders(ctx, userID, orders)

	// Assert
	assert.ErrorIs(t, err, ErrAccountNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_UpdateDisplayOrders_ChainedAccountTargetsUserAccounts pins the STATEMENT
// SHAPE of the chained leg: the UPDATE goes to user_accounts, keyed by the chain id the resolve
// returned, and still carries the actor's user_id.
//
// This is a shape assertion only. What the write actually does — that the value comes back out of
// the chain-authoritative read, and that the legacy accounts column is left alone — is not
// something pgxmock can see (it returns rows the test author invented), so it is pinned by
// account_display_order_routing_integration_test.go against real Postgres.
func TestAccountRepository_UpdateDisplayOrders_ChainedAccountTargetsUserAccounts(t *testing.T) {
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()
	userAccountID := uuid.New().String()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(displayOrderResolveChainQuery)).
		WithArgs(accountID, userID).
		WillReturnRows(pgxmock.NewRows([]string{"user_account_id"}).AddRow(&userAccountID))
	mock.ExpectExec(regexp.QuoteMeta(displayOrderChainSideQuery)).
		WithArgs(3, pgxmock.AnyArg(), userAccountID, userID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectCommit()

	err := repo.UpdateDisplayOrders(ctx, userID, []domain.AccountOrder{
		{AccountID: accountID, DisplayOrder: 3},
	})

	assert.NoError(t, err)
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
		"exclude_investment_sync",
		"user_account_id", "status", "replaced_at", "replaced_by_id", "removed_at", "expired_at",
		"created_at", "updated_at", "institution_name", "linked_item_status",
	}).
		AddRow(
			uuid.New().String(), uuid.New().String(), userID, "acc-1",
			"Checking", nil, "depository", strPtr("checking"), strPtr("1234"),
			&bal1, &avail1, "USD",
			strPtr("My Checking"), strPtr("#3B82F6"), strPtr("wallet"), nil,
			&order0, false,
			false,
			nil, "active", nil, nil, nil, nil,
			now, now, strPtr("Chase"), strPtr("active"),
		).
		AddRow(
			uuid.New().String(), uuid.New().String(), userID, "acc-2",
			"Savings", nil, "depository", strPtr("savings"), strPtr("5678"),
			&bal2, &avail2, "USD",
			nil, nil, nil, nil,
			&order1, false,
			false,
			nil, "active", nil, nil, nil, nil,
			now, now, strPtr("Chase"), strPtr("active"),
		)

	// Note: This query excludes hidden accounts (AND a.is_hidden = FALSE)
	// Issue #679, #680: Added user_account_id, status, replaced_at, replaced_by_id, l.status columns
	expectedQuery := `
		SELECT a.id, a.linked_item_id, a.user_id, a.plaid_account_id,
		       a.name, a.official_name, a.type, a.subtype, a.mask,
		       ` + accountBalanceProjection("a") + `, COALESCE(a.currency_code, 'USD') AS currency_code,
		       ` + accountCustomizationProjection("a") + `,
		       COALESCE(a_ua.exclude_investment_sync, FALSE) AS exclude_investment_sync,
		       a.user_account_id, a.status, a.replaced_at, a.replaced_by_id, a.removed_at, a.expired_at,
		       a.created_at, a.updated_at, l.institution_name, l.status
		FROM accounts a
		LEFT JOIN linked_items l ON a.linked_item_id = l.id` + accountCustomizationJoin("a") + `
		WHERE a.user_id = $1 AND a.status = 'active' AND ` + accountHiddenExpr("a") + ` = FALSE ORDER BY COALESCE(` + accountDisplayOrderExpr("a") + `, 999999) ASC, a.created_at ASC, a.id ASC
	`

	mock.ExpectQuery(regexp.QuoteMeta(expectedQuery)).
		WithArgs(userID).
		WillReturnRows(rows)

	// Execute with includeHidden=false
	accounts, err := repo.GetAccountsWithInstitutionFiltered(ctx, userID, AccountFilterOptions{IncludeHidden: false})

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
		"exclude_investment_sync",
		"user_account_id", "status", "replaced_at", "replaced_by_id", "removed_at", "expired_at",
		"created_at", "updated_at", "institution_name", "linked_item_status",
	}).
		AddRow(
			uuid.New().String(), uuid.New().String(), userID, "acc-1",
			"Checking", nil, "depository", strPtr("checking"), strPtr("1234"),
			&bal1, &avail1, "USD",
			nil, nil, nil, nil,
			&order0, false,
			false,
			nil, "active", nil, nil, nil, nil,
			now, now, strPtr("Chase"), strPtr("active"),
		).
		AddRow(
			uuid.New().String(), uuid.New().String(), userID, "acc-2",
			"Old Account", nil, "depository", strPtr("checking"), strPtr("0000"),
			&bal2, &avail2, "USD",
			strPtr("Closed Account"), nil, nil, nil,
			nil, true, // This one is hidden
			false,
			nil, "active", nil, nil, nil, nil,
			now, now, strPtr("Bank of America"), strPtr("active"),
		)

	// Note: This query includes hidden accounts (no is_hidden filter)
	// Issue #679, #680: Added user_account_id, status, replaced_at, replaced_by_id, l.status columns
	expectedQuery := `
		SELECT a.id, a.linked_item_id, a.user_id, a.plaid_account_id,
		       a.name, a.official_name, a.type, a.subtype, a.mask,
		       ` + accountBalanceProjection("a") + `, COALESCE(a.currency_code, 'USD') AS currency_code,
		       ` + accountCustomizationProjection("a") + `,
		       COALESCE(a_ua.exclude_investment_sync, FALSE) AS exclude_investment_sync,
		       a.user_account_id, a.status, a.replaced_at, a.replaced_by_id, a.removed_at, a.expired_at,
		       a.created_at, a.updated_at, l.institution_name, l.status
		FROM accounts a
		LEFT JOIN linked_items l ON a.linked_item_id = l.id` + accountCustomizationJoin("a") + `
		WHERE a.user_id = $1 AND a.status = 'active' ORDER BY COALESCE(` + accountDisplayOrderExpr("a") + `, 999999) ASC, a.created_at ASC, a.id ASC
	`

	mock.ExpectQuery(regexp.QuoteMeta(expectedQuery)).
		WithArgs(userID).
		WillReturnRows(rows)

	// Execute with includeHidden=true
	accounts, err := repo.GetAccountsWithInstitutionFiltered(ctx, userID, AccountFilterOptions{IncludeHidden: true})

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
		"exclude_investment_sync",
		"created_at", "updated_at",
	}).
		AddRow(
			uuid.New().String(), uuid.New().String(), uuid.New().String(), "acc-1",
			"Checking", strPtr("Chase Total Checking"), "depository", strPtr("checking"), strPtr("1234"),
			&bal1, &avail1, "USD",
			&userAccountID, "active", nil, nil,
			nil, nil, nil, nil, nil, false,
			false,
			now, now,
		)

	userID := uuid.New().String()

	mock.ExpectQuery("SELECT.*FROM accounts a.*WHERE a\\.user_account_id.*AND a\\.status.*AND a\\.user_id").
		WithArgs(userAccountID, "active", userID).
		WillReturnRows(rows)

	// Execute - this method should exist for stable queries across re-links
	accounts, err := repo.GetByUserAccountAndStatus(ctx, userAccountID, "active", userID)

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
		"exclude_investment_sync",
		"created_at", "updated_at",
	}).
		AddRow(
			uuid.New().String(), uuid.New().String(), uuid.New().String(), "acc-old",
			"Checking", strPtr("Chase Total Checking"), "depository", strPtr("checking"), strPtr("1234"),
			&bal1, &avail1, "USD",
			&userAccountID, "replaced", &replacedAt, &replacedByID,
			nil, nil, nil, nil, nil, false,
			false,
			now, now,
		)

	userID := uuid.New().String()

	mock.ExpectQuery("SELECT.*FROM accounts a.*WHERE a\\.user_account_id.*AND a\\.status.*AND a\\.user_id").
		WithArgs(userAccountID, "replaced", userID).
		WillReturnRows(rows)

	// Execute
	accounts, err := repo.GetByUserAccountAndStatus(ctx, userAccountID, "replaced", userID)

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
		"exclude_investment_sync",
		"created_at", "updated_at",
	})

	userID := uuid.New().String()

	mock.ExpectQuery("SELECT.*FROM accounts a.*WHERE a\\.user_account_id.*AND a\\.status.*AND a\\.user_id").
		WithArgs(userAccountID, "active", userID).
		WillReturnRows(rows)

	// Execute
	accounts, err := repo.GetByUserAccountAndStatus(ctx, userAccountID, "active", userID)

	// Assert - should return empty slice (not an error) when no match found
	assert.NoError(t, err)
	assert.Empty(t, accounts)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_GetByUserAccountAndStatus_ScopesToUserID pins Issue #1569 R11 T2: this
// query previously had NO user_id predicate at all, so any caller supplying a raw user_account_id
// (a client-facing stable identifier) could read another user's accounts. Asserting the SQL text
// contains "AND user_id" AND that the third bind parameter is the userID proves the predicate was
// added, not merely that the query still runs.
func TestAccountRepository_GetByUserAccountAndStatus_ScopesToUserID(t *testing.T) {
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userAccountID := uuid.New().String()
	userID := uuid.New().String()

	rows := pgxmock.NewRows([]string{
		"id", "linked_item_id", "user_id", "plaid_account_id",
		"name", "official_name", "type", "subtype", "mask",
		"current_balance", "available_balance", "currency_code",
		"user_account_id", "status", "replaced_at", "replaced_by_id",
		"nickname", "custom_color", "custom_icon", "custom_emoji", "display_order", "is_hidden",
		"exclude_investment_sync",
		"created_at", "updated_at",
	})

	// pgxmock.WithArgs matches positionally: if the implementation dropped the user_id bind
	// parameter (or put it in the wrong position), this expectation would not match and the
	// query would error — that failure IS the assertion.
	mock.ExpectQuery(`SELECT.*FROM accounts a.*WHERE a\.user_account_id = \$1 AND a\.status = \$2 AND a\.user_id = \$3`).
		WithArgs(userAccountID, "active", userID).
		WillReturnRows(rows)

	_, err := repo.GetByUserAccountAndStatus(ctx, userAccountID, "active", userID)

	assert.NoError(t, err)
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
		"exclude_investment_sync",
		"created_at", "updated_at",
	}).
		AddRow(
			uuid.New().String(), uuid.New().String(), uuid.New().String(), "acc-old",
			"Checking", strPtr("Chase Total Checking"), "depository", strPtr("checking"), strPtr("1234"),
			&bal1, &avail1, "USD",
			&userAccountID, "replaced", &replacedAt, nil,
			nil, nil, nil, nil, nil, false,
			false,
			now.Add(-2*time.Hour), now,
		).
		AddRow(
			uuid.New().String(), uuid.New().String(), uuid.New().String(), "acc-new",
			"Checking", strPtr("Chase Total Checking"), "depository", strPtr("checking"), strPtr("1234"),
			&bal2, &avail2, "USD",
			&userAccountID, "active", nil, nil,
			nil, nil, nil, nil, nil, false,
			false,
			now, now,
		)

	// When querying without status filter (nil), return all
	mock.ExpectQuery("SELECT.*FROM accounts a.*WHERE a\\.user_account_id").
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

// TestAccountRepository_UpdateStatus tests updating account status for soft-delete. Issue #1569,
// Task 2: UpdateStatus now locks and checks the row's CURRENT status before writing, so the mock
// must satisfy the SELECT ... FOR UPDATE precondition check (returning 'active') before the UPDATE.
func TestAccountRepository_UpdateStatus(t *testing.T) {
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	accountID := uuid.New().String()
	userID := uuid.New().String()
	replacedByID := uuid.New().String()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT status FROM accounts WHERE id = $1 AND user_id = $2 FOR UPDATE`)).
		WithArgs(accountID, userID).
		WillReturnRows(pgxmock.NewRows([]string{"status"}).AddRow("active"))

	// Expect update to set status, replaced_at, replaced_by_id, removed_at and removed_reason
	// (Issue #1569 Task 5: the cause is stamped in the same statement as removed_at, so the two
	// can never disagree — migration 000154's accounts_removed_reason_valid CHECK requires it).
	// replaced_at/removed_at are now CASE-WHEN NOW() expressions keyed off $1 (a root-cause fix
	// for TestAccountRepository_Reads_Integration_ReturnRemovedAt's clock-split flake), so they
	// are no longer bound parameters — only status, replaced_by_id, removed_reason, id, user_id
	// are. updated_at is never bound: the migration-000007 trigger owns it.
	mock.ExpectExec("UPDATE accounts.*SET.*status.*replaced_at.*replaced_by_id.*removed_at.*removed_reason").
		WithArgs(
			pgxmock.AnyArg(), // status
			pgxmock.AnyArg(), // replaced_by_id
			pgxmock.AnyArg(), // removed_reason
			pgxmock.AnyArg(), // id
			pgxmock.AnyArg(), // user_id
		).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectCommit()

	// Execute
	err := repo.UpdateStatus(ctx, accountID, userID, domain.AccountStatusReplaced, &replacedByID)

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_UpdateStatus_NotFound tests updating status for non-existent account: the
// row-lock SELECT itself finds no matching row.
func TestAccountRepository_UpdateStatus_NotFound(t *testing.T) {
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	accountID := uuid.New().String()
	userID := uuid.New().String()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT status FROM accounts WHERE id = $1 AND user_id = $2 FOR UPDATE`)).
		WithArgs(accountID, userID).
		WillReturnError(pgx.ErrNoRows)
	mock.ExpectRollback()

	// Execute
	err := repo.UpdateStatus(ctx, accountID, userID, domain.AccountStatusReplaced, nil)

	// Assert
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrAccountNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_UpdateStatus_AlreadyNonActive is the RED test for Issue #1569, Task 2: a
// row that is no longer 'active' (already replaced or expired — a GENUINELY different transition
// than the one being requested) must refuse the write entirely — no UPDATE statement should even
// be issued — rather than clobbering an already-terminal status or its chain metadata.
// 'removed' is deliberately NOT covered here: fix round 1 split it into the idempotency case
// (ErrAccountAlreadyRemoved), covered separately below.
func TestAccountRepository_UpdateStatus_AlreadyNonActive(t *testing.T) {
	for _, currentStatus := range []string{"replaced", "expired"} {
		t.Run(currentStatus, func(t *testing.T) {
			mock, repo := setupAccountMockDB(t)
			defer mock.Close()

			ctx := context.Background()
			accountID := uuid.New().String()
			userID := uuid.New().String()

			mock.ExpectBegin()
			mock.ExpectQuery(regexp.QuoteMeta(`SELECT status FROM accounts WHERE id = $1 AND user_id = $2 FOR UPDATE`)).
				WithArgs(accountID, userID).
				WillReturnRows(pgxmock.NewRows([]string{"status"}).AddRow(currentStatus))
			mock.ExpectRollback()
			// Deliberately NO mock.ExpectExec for the UPDATE — issuing it at all would be the bug.

			// Execute
			err := repo.UpdateStatus(ctx, accountID, userID, domain.AccountStatusRemoved, nil)

			// Assert
			assert.Error(t, err)
			assert.ErrorIs(t, err, ErrAccountAlreadyDisconnected)
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// TestAccountRepository_UpdateStatus_AlreadyRemoved is the RED test for fix round 1 of Task 2: an
// already-'removed' row is the IDEMPOTENCY case, not a hard refusal — UpdateStatus must return the
// distinct ErrAccountAlreadyRemoved sentinel (so DisconnectAccount can treat it as success) while
// still never issuing the UPDATE.
func TestAccountRepository_UpdateStatus_AlreadyRemoved(t *testing.T) {
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	accountID := uuid.New().String()
	userID := uuid.New().String()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT status FROM accounts WHERE id = $1 AND user_id = $2 FOR UPDATE`)).
		WithArgs(accountID, userID).
		WillReturnRows(pgxmock.NewRows([]string{"status"}).AddRow("removed"))
	mock.ExpectRollback()
	// Deliberately NO mock.ExpectExec for the UPDATE — idempotent success must still mean no write.

	// Execute
	err := repo.UpdateStatus(ctx, accountID, userID, domain.AccountStatusRemoved, nil)

	// Assert
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrAccountAlreadyRemoved)
	assert.NotErrorIs(t, err, ErrAccountAlreadyDisconnected,
		"the idempotency case must NOT also satisfy the hard-refusal sentinel")
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
		AddRow(ownerUserID, linkedItemID, strPtr("active"), &now)

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

// TestAccountRepository_CheckFingerprintOwnership_NullItemStatus_NoError is a regression test for
// Issue #1444 (Site B): li.status is scanned into FingerprintOwnership.ItemStatus, typed
// domain.LinkedItemStatus (a bare string type, not a pointer) — see linked_items.status's
// nullable column. Live caller: account_service.go's LinkAccounts cross-user uniqueness gate
// (Issue #1010) — a NULL status here must not fail the scan, since the row it came from is the
// security-relevant "does someone else already own this fingerprint" check.
//
// ⚠ #1626 Phase 2 pgxmock sweep: the require.NoError below, taken alone, is NOT a NULL-scan proof
// — pgxmock cannot distinguish this scan from a reverted, unfixed one (it never errors on a nil
// AddRow cell scanned into a plain type, pointer intermediate or not). What makes this test sound
// despite that is the assertion AFTER it: normalizeLinkedItemStatus's "active" mapping only comes
// out right if the *string intermediate genuinely stayed nil, which is mutation-confirmed —
// reverting the intermediate to a plain string (so the pointer always points at "" instead) makes
// that assertion fail even though pgxmock still reports no error. See account_repository.go's
// CheckFingerprintOwnership for the intermediate this depends on.
func TestAccountRepository_CheckFingerprintOwnership_NullItemStatus_NoError(t *testing.T) {
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	fingerprint := "test-fingerprint-blind-idx"
	requestingUserID := uuid.New().String()
	ownerUserID := uuid.New().String()
	linkedItemID := uuid.New().String()

	rows := pgxmock.NewRows([]string{"user_id", "linked_item_id", "status", "status_changed_at"}).
		AddRow(ownerUserID, linkedItemID, nil, nil)

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

	// pgxmock cannot prove this NoError alone; see the doc comment above for why the assertion
	// after it is what actually pins the fix.
	require.NoError(t, err, "a NULL li.status must not fail the scan")
	require.NotNil(t, ownership)
	assert.Equal(t, ownerUserID, ownership.UserID)
	assert.Equal(t, domain.LinkedItemStatusActive, ownership.ItemStatus, "a NULL linked_item status must be treated as active, per normalizeLinkedItemStatus's established rule")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_ExpireAccountsByFingerprint verifies that accounts matching a fingerprint
// are marked as 'expired' (stamping expired_at, Issue #1569 Task 6) to release the DB uniqueness
// constraint, and that the caller gets back the IDs of every account actually flipped — the
// repository's own RETURNING, not a second SELECT — so the service layer can audit-log each one.
func TestAccountRepository_ExpireAccountsByFingerprint(t *testing.T) {
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	fingerprint := "test-fingerprint-blind-idx"
	expiredID1 := uuid.New().String()
	expiredID2 := uuid.New().String()

	mock.ExpectQuery(regexp.QuoteMeta(`
		UPDATE accounts
		SET status = 'expired', expired_at = NOW(), updated_at = NOW()
		WHERE (persistent_account_id_blind_idx = $1 OR fallback_fingerprint_blind_idx = $1)
		  AND status = 'active'
		RETURNING id
	`)).WithArgs(fingerprint).WillReturnRows(
		pgxmock.NewRows([]string{"id"}).AddRow(expiredID1).AddRow(expiredID2),
	)

	expiredIDs, err := repo.ExpireAccountsByFingerprint(ctx, fingerprint)

	assert.NoError(t, err)
	assert.ElementsMatch(t, []string{expiredID1, expiredID2}, expiredIDs,
		"must return every account the UPDATE actually flipped, via RETURNING, so the service can audit-log each one")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_ExpireAccountsByFingerprint_NoMatch verifies a no-op (nothing active
// matches the fingerprint) returns an empty slice and no error, not a sentinel or a panic — a bulk
// statement has no single "current status" to compare against, so silent success is correct here.
func TestAccountRepository_ExpireAccountsByFingerprint_NoMatch(t *testing.T) {
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	fingerprint := "unclaimed-fingerprint-blind-idx"

	mock.ExpectQuery(regexp.QuoteMeta(`
		UPDATE accounts
		SET status = 'expired', expired_at = NOW(), updated_at = NOW()
		WHERE (persistent_account_id_blind_idx = $1 OR fallback_fingerprint_blind_idx = $1)
		  AND status = 'active'
		RETURNING id
	`)).WithArgs(fingerprint).WillReturnRows(pgxmock.NewRows([]string{"id"}))

	expiredIDs, err := repo.ExpireAccountsByFingerprint(ctx, fingerprint)

	assert.NoError(t, err)
	assert.Empty(t, expiredIDs)
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
		"exclude_investment_sync",
		"created_at", "updated_at",
	}).AddRow(
		uuid.New().String(), uuid.New().String(), userID, "acc-active",
		"Active Checking", nil, "depository", strPtr("checking"), strPtr("1234"),
		&balance, &balance, "USD",
		"active", nil, nil, nil,
		false,
		now, now,
	)
	// Note: replaced account not in rows — DB filters it via WHERE status = 'active'

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT a.id, a.linked_item_id, a.user_id, a.plaid_account_id,
		       a.name, a.official_name, a.type, a.subtype, a.mask,
		       ` + accountBalanceProjection("a") + `, COALESCE(a.currency_code, 'USD') AS currency_code,
		       a.status, a.user_account_id, a.replaced_by_id, a.replaced_at,
		       COALESCE(a_ua.exclude_investment_sync, FALSE) AS exclude_investment_sync,
		       a.created_at, a.updated_at
		FROM accounts a` + accountCustomizationJoin("a") + `
		WHERE a.user_id = $1 AND a.status = 'active'
		ORDER BY a.created_at ASC
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
		"status", "user_account_id", "replaced_by_id", "replaced_at", "removed_at",
		"exclude_investment_sync",
		"created_at", "updated_at", "institution_name",
	}).AddRow(
		uuid.New().String(), uuid.New().String(), userID, "plaid-acc-active",
		"Active Checking", nil, "depository", strPtr("checking"), strPtr("1234"),
		&bal, &bal, "USD",
		"active", nil, nil, nil, nil,
		false,
		time.Now(), time.Now(), "Chase",
	)

	expectedQuery := `
		SELECT a.id, a.linked_item_id, a.user_id, a.plaid_account_id,
		       a.name, a.official_name, a.type, a.subtype, a.mask,
		       ` + accountBalanceProjection("a") + `, COALESCE(a.currency_code, 'USD') AS currency_code,
		       a.status, a.user_account_id, a.replaced_by_id, a.replaced_at, a.removed_at,
		       COALESCE(a_ua.exclude_investment_sync, FALSE) AS exclude_investment_sync,
		       a.created_at, a.updated_at, COALESCE(l.institution_name, '') AS institution_name
		FROM accounts a
		LEFT JOIN linked_items l ON a.linked_item_id = l.id` + accountCustomizationJoin("a") + `
		WHERE a.user_id = $1 AND a.status = 'active'
		ORDER BY a.created_at ASC, a.id ASC
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
		"status", "user_account_id", "replaced_by_id", "replaced_at",
		"exclude_investment_sync",
		"created_at", "updated_at",
	}).AddRow(
		uuid.New().String(), linkedItemID, uuid.New().String(), "acc-active",
		"Active Checking", nil, "depository", strPtr("checking"), strPtr("1234"),
		&balance, &available, "USD",
		"active", nil, nil, nil,
		false,
		now, now,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT a.id, a.linked_item_id, a.user_id, a.plaid_account_id,
		       a.name, a.official_name, a.type, a.subtype, a.mask,
		       ` + accountBalanceProjection("a") + `, COALESCE(a.currency_code, 'USD') AS currency_code,
		       a.status, a.user_account_id, a.replaced_by_id, a.replaced_at,
		       COALESCE(a_ua.exclude_investment_sync, FALSE) AS exclude_investment_sync,
		       a.created_at, a.updated_at
		FROM accounts a` + accountCustomizationJoin("a") + `
		WHERE a.linked_item_id = $1 AND a.status = 'active'
		ORDER BY a.created_at ASC
	`)).WithArgs(linkedItemID).WillReturnRows(rows)

	accounts, err := repo.GetByLinkedItemID(ctx, linkedItemID)

	require.NoError(t, err)
	assert.Len(t, accounts, 1)
	assert.Equal(t, "Active Checking", accounts[0].Name)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_GetByLinkedItemIDIncludingRetained_IncludesAllStatuses verifies the
// separate, sync-only read (Issue #1569 Task 1b) has NO status predicate — unlike
// GetByLinkedItemID above, which callers must keep using for holdings/liability syncs.
func TestAccountRepository_GetByLinkedItemIDIncludingRetained_IncludesAllStatuses(t *testing.T) {
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	linkedItemID := uuid.New().String()
	now := time.Now()
	balance := 1500.50
	available := 1450.25

	// removed_reason is selected by THIS read alone (Issue #1569 Task 5): it is the only caller
	// that needs the cause, because it feeds the provider-restore decision.
	rows := pgxmock.NewRows([]string{
		"id", "linked_item_id", "user_id", "plaid_account_id",
		"name", "official_name", "type", "subtype", "mask",
		"current_balance", "available_balance", "currency_code",
		"status", "user_account_id", "replaced_by_id", "replaced_at",
		"removed_reason",
		"exclude_investment_sync",
		"created_at", "updated_at",
	}).AddRow(
		uuid.New().String(), linkedItemID, uuid.New().String(), "acc-active",
		"Active Checking", nil, "depository", strPtr("checking"), strPtr("1234"),
		&balance, &available, "USD",
		"active", nil, nil, nil,
		nil,
		false,
		now, now,
	).AddRow(
		uuid.New().String(), linkedItemID, uuid.New().String(), "acc-removed",
		"Removed Card", nil, "credit", strPtr("credit card"), strPtr("5678"),
		nil, nil, "USD",
		"removed", nil, nil, nil,
		strPtr("provider_absent"),
		true,
		now, now,
	)

	// #1618b: aliased `a` and given accountCustomizationJoin so this read (used by
	// investment_sync_service.SyncInvestmentTransactions — the very subsystem this flag governs)
	// surfaces exclude_investment_sync instead of reading it back as an unconditional false. A LEFT
	// JOIN adds a column, never removes a row: both statuses are still returned.
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT a.id, a.linked_item_id, a.user_id, a.plaid_account_id,
		       a.name, a.official_name, a.type, a.subtype, a.mask,
		       ` + accountBalanceProjection("a") + `, COALESCE(a.currency_code, 'USD') AS currency_code,
		       a.status, a.user_account_id, a.replaced_by_id, a.replaced_at,
		       a.removed_reason,
		       COALESCE(a_ua.exclude_investment_sync, FALSE) AS exclude_investment_sync,
		       a.created_at, a.updated_at
		FROM accounts a` + accountCustomizationJoin("a") + `
		WHERE a.linked_item_id = $1
		ORDER BY a.created_at ASC
	`)).WithArgs(linkedItemID).WillReturnRows(rows)

	accounts, err := repo.GetByLinkedItemIDIncludingRetained(ctx, linkedItemID)

	require.NoError(t, err)
	require.Len(t, accounts, 2, "removed accounts must be included, unlike GetByLinkedItemID")
	assert.Equal(t, "Active Checking", accounts[0].Name)
	assert.Equal(t, "Removed Card", accounts[1].Name)
	assert.Equal(t, domain.AccountStatusRemoved, accounts[1].Status)
	// The scan target must line up: the removal cause reaches the domain object, and an active
	// account's NULL stays nil rather than shifting a neighbouring column into it.
	assert.Nil(t, accounts[0].RemovedReason)
	require.NotNil(t, accounts[1].RemovedReason)
	assert.Equal(t, "provider_absent", *accounts[1].RemovedReason)
	assert.False(t, accounts[0].ExcludeInvestmentSync)
	assert.True(t, accounts[1].ExcludeInvestmentSync)
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
		AND status = 'active'
	`)).WithArgs(userID).WillReturnRows(rows)

	count, err := repo.CountByUserID(ctx, userID)

	assert.NoError(t, err)
	assert.Equal(t, expectedCount, count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_GetAccountsWithInstitutionFiltered_IncludeReplacedExcludesExpired verifies
// that IncludeReplaced=true returns active+replaced accounts but never expired ones (Issue #1109 W4-C).
func TestAccountRepository_GetAccountsWithInstitutionFiltered_IncludeReplacedExcludesExpired(t *testing.T) {
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	now := time.Now()
	replacedAt := now.Add(-time.Hour)

	bal1, avail1 := 1200.00, 1200.00
	bal2, avail2 := 900.00, 900.00
	order0 := 0

	rows := pgxmock.NewRows([]string{
		"id", "linked_item_id", "user_id", "plaid_account_id",
		"name", "official_name", "type", "subtype", "mask",
		"current_balance", "available_balance", "currency_code",
		"nickname", "custom_color", "custom_icon", "custom_emoji",
		"display_order", "is_hidden",
		"exclude_investment_sync",
		"user_account_id", "status", "replaced_at", "replaced_by_id", "removed_at", "expired_at",
		"created_at", "updated_at", "institution_name", "linked_item_status",
	}).
		AddRow(
			uuid.New().String(), uuid.New().String(), userID, "acc-active",
			"Checking", nil, "depository", strPtr("checking"), strPtr("1234"),
			&bal1, &avail1, "USD",
			nil, nil, nil, nil,
			&order0, false,
			false,
			nil, "active", nil, nil, nil, nil,
			now, now, strPtr("Chase"), strPtr("active"),
		).
		AddRow(
			uuid.New().String(), uuid.New().String(), userID, "acc-replaced",
			"Old Checking", nil, "depository", strPtr("checking"), strPtr("0000"),
			&bal2, &avail2, "USD",
			nil, nil, nil, nil,
			nil, false,
			false,
			nil, "replaced", &replacedAt, nil, nil, nil,
			now, now, strPtr("Chase"), strPtr("active"),
		)
	// Note: expired account is NOT included in the mock result rows.
	// The SQL query with IN ('active', 'replaced') ensures it never reaches the scanner.

	expectedQuery := `
		SELECT a.id, a.linked_item_id, a.user_id, a.plaid_account_id,
		       a.name, a.official_name, a.type, a.subtype, a.mask,
		       ` + accountBalanceProjection("a") + `, COALESCE(a.currency_code, 'USD') AS currency_code,
		       ` + accountCustomizationProjection("a") + `,
		       COALESCE(a_ua.exclude_investment_sync, FALSE) AS exclude_investment_sync,
		       a.user_account_id, a.status, a.replaced_at, a.replaced_by_id, a.removed_at, a.expired_at,
		       a.created_at, a.updated_at, l.institution_name, l.status
		FROM accounts a
		LEFT JOIN linked_items l ON a.linked_item_id = l.id` + accountCustomizationJoin("a") + `
		WHERE a.user_id = $1 AND a.status IN ('active', 'replaced') AND ` + accountHiddenExpr("a") + ` = FALSE ORDER BY COALESCE(` + accountDisplayOrderExpr("a") + `, 999999) ASC, a.created_at ASC, a.id ASC
	`

	mock.ExpectQuery(regexp.QuoteMeta(expectedQuery)).
		WithArgs(userID).
		WillReturnRows(rows)

	accounts, err := repo.GetAccountsWithInstitutionFiltered(ctx, userID, AccountFilterOptions{IncludeReplaced: true})

	require.NoError(t, err)
	assert.Len(t, accounts, 2, "only active and replaced accounts should be returned, not expired")
	statuses := []string{string(accounts[0].Status), string(accounts[1].Status)}
	assert.Contains(t, statuses, "active")
	assert.Contains(t, statuses, "replaced")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_GetAccountsWithInstitutionFiltered_NullLinkedItemJoinColumns_NoError is a
// regression test for Issue #1444 (Site A): l.institution_name and l.status are both nullable
// columns in linked_items (see domain.Account.InstitutionName / LinkedItemStatus's own "From
// linked_items JOIN (optional)" comments), scanned via a LEFT JOIN into bare (non-pointer) Go
// string fields. Although the LEFT JOIN itself can never miss a row (accounts.linked_item_id is
// UUID NOT NULL with an FK — migrations/000007_create_accounts_table.up.sql), a genuinely NULL
// l.status value is reachable in production today (rows created before migration 000013's
// 'active' default existed) and a NULL l.institution_name is schema-legal
// (migrations/000006_create_linked_items_table.up.sql: institution_name VARCHAR(255), no NOT
// NULL). Before the #1444 fix, rows.Scan into &account.InstitutionName / &account.LinkedItemStatus
// fails outright when either column is NULL, since neither Go field is a pointer or
// sql.NullString.
func TestAccountRepository_GetAccountsWithInstitutionFiltered_NullLinkedItemJoinColumns_NoError(t *testing.T) {
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	now := time.Now()
	order0 := 0

	rows := pgxmock.NewRows([]string{
		"id", "linked_item_id", "user_id", "plaid_account_id",
		"name", "official_name", "type", "subtype", "mask",
		"current_balance", "available_balance", "currency_code",
		"nickname", "custom_color", "custom_icon", "custom_emoji",
		"display_order", "is_hidden",
		"exclude_investment_sync",
		"user_account_id", "status", "replaced_at", "replaced_by_id", "removed_at", "expired_at",
		"created_at", "updated_at", "institution_name", "linked_item_status",
	}).
		AddRow(
			uuid.New().String(), uuid.New().String(), userID, "acc-null-join",
			"Checking", nil, "depository", strPtr("checking"), strPtr("1234"),
			nil, nil, "USD",
			nil, nil, nil, nil,
			&order0, false,
			false,
			nil, "active", nil, nil, nil, nil,
			now, now, nil, nil, // l.institution_name = NULL, l.status = NULL
		)

	expectedQuery := `
		SELECT a.id, a.linked_item_id, a.user_id, a.plaid_account_id,
		       a.name, a.official_name, a.type, a.subtype, a.mask,
		       ` + accountBalanceProjection("a") + `, COALESCE(a.currency_code, 'USD') AS currency_code,
		       ` + accountCustomizationProjection("a") + `,
		       COALESCE(a_ua.exclude_investment_sync, FALSE) AS exclude_investment_sync,
		       a.user_account_id, a.status, a.replaced_at, a.replaced_by_id, a.removed_at, a.expired_at,
		       a.created_at, a.updated_at, l.institution_name, l.status
		FROM accounts a
		LEFT JOIN linked_items l ON a.linked_item_id = l.id` + accountCustomizationJoin("a") + `
		WHERE a.user_id = $1 AND a.status = 'active' AND ` + accountHiddenExpr("a") + ` = FALSE ORDER BY COALESCE(` + accountDisplayOrderExpr("a") + `, 999999) ASC, a.created_at ASC, a.id ASC
	`

	mock.ExpectQuery(regexp.QuoteMeta(expectedQuery)).
		WithArgs(userID).
		WillReturnRows(rows)

	accounts, err := repo.GetAccountsWithInstitutionFiltered(ctx, userID, AccountFilterOptions{IncludeHidden: false})

	// ⚠ #1626 Phase 2 pgxmock sweep: pgxmock cannot prove this NoError alone — it never errors
	// scanning a nil AddRow cell into a plain string, intermediate or not. What actually pins the
	// #1444 fix is the LinkedItemStatus assertion below: mutation-confirmed, reverting the
	// *string intermediates in GetAccountsWithInstitutionFiltered back to scanning straight into
	// account.InstitutionName/LinkedItemStatus makes that assertion fail ("" != "active") even
	// though this NoError still passes.
	require.NoError(t, err, "a NULL l.institution_name/l.status must not fail the scan")
	require.Len(t, accounts, 1)
	assert.Equal(t, "", accounts[0].InstitutionName, "NULL institution_name should surface as the empty string")
	assert.Equal(t, "active", accounts[0].LinkedItemStatus, "a NULL linked_item status must be treated as active, per normalizeLinkedItemStatus's established rule")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_CreateBatch_CrossTenantRow_Rejected verifies CreateBatch refuses a slice
// containing a row for a user other than the actor, before opening a transaction (#1566).
func TestAccountRepository_CreateBatch_CrossTenantRow_Rejected(t *testing.T) {
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	actorID := uuid.New().String()
	victimID := uuid.New().String()
	linkedItemID := uuid.New().String()

	// No mock expectations are registered: any DB call at all is a failure.
	err := repo.CreateBatch(ctx, actorID, []domain.Account{
		{ID: uuid.New().String(), LinkedItemID: linkedItemID, UserID: actorID, PlaidAccountID: "a1", Name: "Checking", Type: "depository", CurrencyCode: "USD"},
		{ID: uuid.New().String(), LinkedItemID: linkedItemID, UserID: victimID, PlaidAccountID: "a2", Name: "Savings", Type: "depository", CurrencyCode: "USD"},
	})

	require.ErrorIs(t, err, ErrAccountBatchTenantMismatch)
	assert.Contains(t, err.Error(), victimID, "error must name the offending user_id")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAccountRepository_CreateBatch_EmptyRowUserID_Rejected verifies a blank UserID is rejected
// rather than silently inheriting the actor.
func TestAccountRepository_CreateBatch_EmptyRowUserID_Rejected(t *testing.T) {
	mock, repo := setupAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	actorID := uuid.New().String()

	err := repo.CreateBatch(ctx, actorID, []domain.Account{
		{ID: uuid.New().String(), LinkedItemID: uuid.New().String(), PlaidAccountID: "a1", Name: "Checking", Type: "depository", CurrencyCode: "USD"},
	})

	require.ErrorIs(t, err, ErrAccountBatchTenantMismatch)
	assert.NoError(t, mock.ExpectationsWereMet())
}
