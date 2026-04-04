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

// setupUserAccountMockDB creates a mock database and pgxmock instance for user account repository testing
func setupUserAccountMockDB(t *testing.T) (pgxmock.PgxPoolIface, UserAccountRepository) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)

	repo := NewUserAccountRepository(mock, newTestFieldEncryptor(t))
	return mock, repo
}

// =============================================================================
// Basic CRUD Tests
// =============================================================================

// TestUserAccountRepository_Create_Success tests successful creation of a user account
func TestUserAccountRepository_Create_Success(t *testing.T) {
	mock, repo := setupUserAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	now := time.Now()

	userAccount := &domain.UserAccount{
		ID:              uuid.New().String(),
		UserID:          uuid.New().String(),
		InstitutionID:   "ins_chase",
		Mask:            strPtr("1234"),
		AccountNameHint: strPtr("Chase Checking"),
		Nickname:        strPtr("My Checking"),
		CustomColor:     strPtr("#3B82F6"),
		CustomIcon:      strPtr("wallet"),
		CustomEmoji:     strPtr("💰"),
		DisplayOrder:    intPtr(1),
		IsHidden:        false,
		PrimaryType:     strPtr("depository"),
		PrimarySubtype:  strPtr("checking"),
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO user_accounts
		(id, user_id, institution_id, institution_id_blind_idx, mask, mask_blind_idx, account_name_hint,
		 nickname, custom_color, custom_icon, custom_emoji,
		 display_order, is_hidden, primary_type, primary_subtype,
		 created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
	`)).WithArgs(
		userAccount.ID,
		userAccount.UserID,
		pgxmock.AnyArg(), // institution_id (encrypted by repo)
		pgxmock.AnyArg(), // institution_id_blind_idx
		pgxmock.AnyArg(), // mask (encrypted by repo)
		pgxmock.AnyArg(), // mask_blind_idx
		pgxmock.AnyArg(), // account_name_hint (encrypted by repo)
		userAccount.Nickname,
		userAccount.CustomColor,
		userAccount.CustomIcon,
		userAccount.CustomEmoji,
		userAccount.DisplayOrder,
		userAccount.IsHidden,
		userAccount.PrimaryType,
		userAccount.PrimarySubtype,
		pgxmock.AnyArg(), // created_at
		pgxmock.AnyArg(), // updated_at
	).WillReturnResult(pgxmock.NewResult("INSERT", 1))

	// Execute
	err := repo.Create(ctx, userAccount)

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUserAccountRepository_Create_DuplicateConstraint tests unique constraint violation
func TestUserAccountRepository_Create_DuplicateConstraint(t *testing.T) {
	mock, repo := setupUserAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()

	userAccount := &domain.UserAccount{
		ID:            uuid.New().String(),
		UserID:        uuid.New().String(),
		InstitutionID: "ins_chase",
		Mask:          strPtr("1234"),
	}

	// Simulate unique constraint violation on (user_id, institution_id, mask)
	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO user_accounts
		(id, user_id, institution_id, institution_id_blind_idx, mask, mask_blind_idx, account_name_hint,
		 nickname, custom_color, custom_icon, custom_emoji,
		 display_order, is_hidden, primary_type, primary_subtype,
		 created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
	`)).WithArgs(
		pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
		pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
		pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
		pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
		pgxmock.AnyArg(),
	).WillReturnError(errors.New("duplicate key value violates unique constraint \"unique_user_account_identity\""))

	// Execute
	err := repo.Create(ctx, userAccount)

	// Assert
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrDuplicateUserAccount)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUserAccountRepository_GetByID_Success tests successful retrieval by ID
func TestUserAccountRepository_GetByID_Success(t *testing.T) {
	mock, repo := setupUserAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userAccountID := uuid.New().String()
	userID := uuid.New().String()
	now := time.Now()
	order := 1

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "institution_id", "mask", "account_name_hint",
		"nickname", "custom_color", "custom_icon", "custom_emoji",
		"display_order", "is_hidden", "primary_type", "primary_subtype",
		"created_at", "updated_at",
	}).AddRow(
		userAccountID, userID, "ins_chase", strPtr("1234"), strPtr("Chase Checking"),
		strPtr("My Checking"), strPtr("#3B82F6"), strPtr("wallet"), strPtr("💰"),
		&order, false, strPtr("depository"), strPtr("checking"),
		now, now,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, user_id, institution_id, mask, account_name_hint,
		       nickname, custom_color, custom_icon, custom_emoji,
		       display_order, is_hidden, primary_type, primary_subtype,
		       created_at, updated_at
		FROM user_accounts
		WHERE id = $1
	`)).WithArgs(userAccountID).WillReturnRows(rows)

	// Execute
	ua, err := repo.GetByID(ctx, userAccountID)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, userAccountID, ua.ID)
	assert.Equal(t, userID, ua.UserID)
	assert.Equal(t, "ins_chase", ua.InstitutionID)
	assert.Equal(t, "1234", *ua.Mask)
	assert.Equal(t, "My Checking", *ua.Nickname)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUserAccountRepository_GetByID_NotFound tests when user account doesn't exist
func TestUserAccountRepository_GetByID_NotFound(t *testing.T) {
	mock, repo := setupUserAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userAccountID := uuid.New().String()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, user_id, institution_id, mask, account_name_hint,
		       nickname, custom_color, custom_icon, custom_emoji,
		       display_order, is_hidden, primary_type, primary_subtype,
		       created_at, updated_at
		FROM user_accounts
		WHERE id = $1
	`)).WithArgs(userAccountID).WillReturnError(pgx.ErrNoRows)

	// Execute
	ua, err := repo.GetByID(ctx, userAccountID)

	// Assert
	assert.Nil(t, ua)
	assert.ErrorIs(t, err, ErrUserAccountNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUserAccountRepository_GetByUserID_Success tests retrieval of all user accounts for a user
func TestUserAccountRepository_GetByUserID_Success(t *testing.T) {
	mock, repo := setupUserAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	now := time.Now()
	order1, order2 := 0, 1

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "institution_id", "mask", "account_name_hint",
		"nickname", "custom_color", "custom_icon", "custom_emoji",
		"display_order", "is_hidden", "primary_type", "primary_subtype",
		"created_at", "updated_at",
	}).
		AddRow(
			uuid.New().String(), userID, "ins_chase", strPtr("1234"), strPtr("Chase Checking"),
			strPtr("Checking"), nil, nil, nil,
			&order1, false, strPtr("depository"), strPtr("checking"),
			now, now,
		).
		AddRow(
			uuid.New().String(), userID, "ins_chase", strPtr("5678"), strPtr("Chase Savings"),
			strPtr("Savings"), nil, nil, nil,
			&order2, false, strPtr("depository"), strPtr("savings"),
			now, now,
		)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, user_id, institution_id, mask, account_name_hint,
		       nickname, custom_color, custom_icon, custom_emoji,
		       display_order, is_hidden, primary_type, primary_subtype,
		       created_at, updated_at
		FROM user_accounts
		WHERE user_id = $1
		ORDER BY COALESCE(display_order, 999999) ASC, created_at ASC
	`)).WithArgs(userID).WillReturnRows(rows)

	// Execute
	accounts, err := repo.GetByUserID(ctx, userID)

	// Assert
	require.NoError(t, err)
	assert.Len(t, accounts, 2)
	assert.Equal(t, "Checking", *accounts[0].Nickname)
	assert.Equal(t, "Savings", *accounts[1].Nickname)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUserAccountRepository_GetByUserID_Empty tests when user has no accounts
func TestUserAccountRepository_GetByUserID_Empty(t *testing.T) {
	mock, repo := setupUserAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "institution_id", "mask", "account_name_hint",
		"nickname", "custom_color", "custom_icon", "custom_emoji",
		"display_order", "is_hidden", "primary_type", "primary_subtype",
		"created_at", "updated_at",
	})

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, user_id, institution_id, mask, account_name_hint,
		       nickname, custom_color, custom_icon, custom_emoji,
		       display_order, is_hidden, primary_type, primary_subtype,
		       created_at, updated_at
		FROM user_accounts
		WHERE user_id = $1
		ORDER BY COALESCE(display_order, 999999) ASC, created_at ASC
	`)).WithArgs(userID).WillReturnRows(rows)

	// Execute
	accounts, err := repo.GetByUserID(ctx, userID)

	// Assert
	require.NoError(t, err)
	assert.Empty(t, accounts)
	assert.NotNil(t, accounts) // Should return empty slice, not nil
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUserAccountRepository_Update_Success tests successful update of user account
func TestUserAccountRepository_Update_Success(t *testing.T) {
	mock, repo := setupUserAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userAccountID := uuid.New().String()
	userID := uuid.New().String()

	userAccount := &domain.UserAccount{
		ID:           userAccountID,
		UserID:       userID,
		Nickname:     strPtr("Updated Name"),
		CustomColor:  strPtr("#10B981"),
		CustomIcon:   strPtr("piggy-bank"),
		CustomEmoji:  strPtr("🐷"),
		DisplayOrder: intPtr(2),
		IsHidden:     true,
	}

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE user_accounts
		SET nickname = $1, custom_color = $2, custom_icon = $3,
		    custom_emoji = $4, display_order = $5, is_hidden = $6, updated_at = $7
		WHERE id = $8 AND user_id = $9
	`)).WithArgs(
		userAccount.Nickname,
		userAccount.CustomColor,
		userAccount.CustomIcon,
		userAccount.CustomEmoji,
		userAccount.DisplayOrder,
		userAccount.IsHidden,
		pgxmock.AnyArg(), // updated_at
		userAccountID,
		userID,
	).WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	// Execute
	err := repo.Update(ctx, userAccount)

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUserAccountRepository_Update_NotFound tests update when user account doesn't exist
func TestUserAccountRepository_Update_NotFound(t *testing.T) {
	mock, repo := setupUserAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userAccountID := uuid.New().String()
	userID := uuid.New().String()

	userAccount := &domain.UserAccount{
		ID:       userAccountID,
		UserID:   userID,
		Nickname: strPtr("Test"),
	}

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE user_accounts
		SET nickname = $1, custom_color = $2, custom_icon = $3,
		    custom_emoji = $4, display_order = $5, is_hidden = $6, updated_at = $7
		WHERE id = $8 AND user_id = $9
	`)).WithArgs(
		userAccount.Nickname,
		userAccount.CustomColor,
		userAccount.CustomIcon,
		userAccount.CustomEmoji,
		userAccount.DisplayOrder,
		userAccount.IsHidden,
		pgxmock.AnyArg(),
		userAccountID,
		userID,
	).WillReturnResult(pgxmock.NewResult("UPDATE", 0)) // 0 rows affected

	// Execute
	err := repo.Update(ctx, userAccount)

	// Assert
	assert.ErrorIs(t, err, ErrUserAccountNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// =============================================================================
// Matching Logic Tests (Critical for Re-link)
// =============================================================================

// TestUserAccountRepository_FindByInstitutionAndMask_ExactMatch tests exact match
func TestUserAccountRepository_FindByInstitutionAndMask_ExactMatch(t *testing.T) {
	mock, repo := setupUserAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	institutionID := "ins_chase"
	mask := "1234"
	now := time.Now()
	order := 1

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "institution_id", "mask", "account_name_hint",
		"nickname", "custom_color", "custom_icon", "custom_emoji",
		"display_order", "is_hidden", "primary_type", "primary_subtype",
		"created_at", "updated_at",
	}).AddRow(
		uuid.New().String(), userID, institutionID, strPtr(mask), strPtr("Chase Checking"),
		strPtr("My Checking"), nil, nil, nil,
		&order, false, strPtr("depository"), strPtr("checking"),
		now, now,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, user_id, institution_id, mask, account_name_hint,
		       nickname, custom_color, custom_icon, custom_emoji,
		       display_order, is_hidden, primary_type, primary_subtype,
		       created_at, updated_at
		FROM user_accounts
		WHERE user_id = $1 AND institution_id_blind_idx = $2 AND mask_blind_idx = $3
	`)).WithArgs(userID, pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnRows(rows)

	// Execute
	ua, err := repo.FindByInstitutionAndMask(ctx, userID, institutionID, &mask)

	// Assert
	require.NoError(t, err)
	assert.NotNil(t, ua)
	assert.Equal(t, institutionID, ua.InstitutionID)
	assert.Equal(t, mask, *ua.Mask)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUserAccountRepository_FindByInstitutionAndMask_NoMatch_DifferentInstitution tests no match when institution differs
func TestUserAccountRepository_FindByInstitutionAndMask_NoMatch_DifferentInstitution(t *testing.T) {
	mock, repo := setupUserAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	institutionID := "ins_wells_fargo" // Different institution
	mask := "1234"

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, user_id, institution_id, mask, account_name_hint,
		       nickname, custom_color, custom_icon, custom_emoji,
		       display_order, is_hidden, primary_type, primary_subtype,
		       created_at, updated_at
		FROM user_accounts
		WHERE user_id = $1 AND institution_id_blind_idx = $2 AND mask_blind_idx = $3
	`)).WithArgs(userID, pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnError(pgx.ErrNoRows)

	// Execute
	ua, err := repo.FindByInstitutionAndMask(ctx, userID, institutionID, &mask)

	// Assert
	assert.Nil(t, ua)
	assert.NoError(t, err) // No error, just no match
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUserAccountRepository_FindByInstitutionAndMask_NoMatch_DifferentMask tests no match when mask differs
func TestUserAccountRepository_FindByInstitutionAndMask_NoMatch_DifferentMask(t *testing.T) {
	mock, repo := setupUserAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	institutionID := "ins_chase"
	mask := "9999" // Different mask

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, user_id, institution_id, mask, account_name_hint,
		       nickname, custom_color, custom_icon, custom_emoji,
		       display_order, is_hidden, primary_type, primary_subtype,
		       created_at, updated_at
		FROM user_accounts
		WHERE user_id = $1 AND institution_id_blind_idx = $2 AND mask_blind_idx = $3
	`)).WithArgs(userID, pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnError(pgx.ErrNoRows)

	// Execute
	ua, err := repo.FindByInstitutionAndMask(ctx, userID, institutionID, &mask)

	// Assert
	assert.Nil(t, ua)
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUserAccountRepository_FindByInstitutionAndMask_NullMask tests matching with NULL mask
func TestUserAccountRepository_FindByInstitutionAndMask_NullMask(t *testing.T) {
	mock, repo := setupUserAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	institutionID := "ins_manual"

	// Query for NULL mask uses different SQL
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, user_id, institution_id, mask, account_name_hint,
		       nickname, custom_color, custom_icon, custom_emoji,
		       display_order, is_hidden, primary_type, primary_subtype,
		       created_at, updated_at
		FROM user_accounts
		WHERE user_id = $1 AND institution_id_blind_idx = $2 AND mask_blind_idx IS NULL
	`)).WithArgs(userID, pgxmock.AnyArg()).WillReturnError(pgx.ErrNoRows)

	// Execute
	ua, err := repo.FindByInstitutionAndMask(ctx, userID, institutionID, nil)

	// Assert
	assert.Nil(t, ua)
	assert.NoError(t, err) // No error, just no match
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUserAccountRepository_FindByInstitutionAndMask_NullMask_Match tests matching NULL mask when account exists
func TestUserAccountRepository_FindByInstitutionAndMask_NullMask_Match(t *testing.T) {
	mock, repo := setupUserAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	institutionID := "ins_manual"
	now := time.Now()

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "institution_id", "mask", "account_name_hint",
		"nickname", "custom_color", "custom_icon", "custom_emoji",
		"display_order", "is_hidden", "primary_type", "primary_subtype",
		"created_at", "updated_at",
	}).AddRow(
		uuid.New().String(), userID, institutionID, nil, strPtr("Manual Account"),
		strPtr("My Manual"), nil, nil, nil,
		nil, false, nil, nil,
		now, now,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, user_id, institution_id, mask, account_name_hint,
		       nickname, custom_color, custom_icon, custom_emoji,
		       display_order, is_hidden, primary_type, primary_subtype,
		       created_at, updated_at
		FROM user_accounts
		WHERE user_id = $1 AND institution_id_blind_idx = $2 AND mask_blind_idx IS NULL
	`)).WithArgs(userID, pgxmock.AnyArg()).WillReturnRows(rows)

	// Execute
	ua, err := repo.FindByInstitutionAndMask(ctx, userID, institutionID, nil)

	// Assert
	require.NoError(t, err)
	assert.NotNil(t, ua)
	assert.Equal(t, institutionID, ua.InstitutionID)
	assert.Nil(t, ua.Mask)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// =============================================================================
// Database Error Tests
// =============================================================================

// TestUserAccountRepository_Create_DatabaseError tests handling of database errors during creation
func TestUserAccountRepository_Create_DatabaseError(t *testing.T) {
	mock, repo := setupUserAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()

	userAccount := &domain.UserAccount{
		ID:            uuid.New().String(),
		UserID:        uuid.New().String(),
		InstitutionID: "ins_chase",
	}

	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO user_accounts
		(id, user_id, institution_id, institution_id_blind_idx, mask, mask_blind_idx, account_name_hint,
		 nickname, custom_color, custom_icon, custom_emoji,
		 display_order, is_hidden, primary_type, primary_subtype,
		 created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
	`)).WithArgs(
		pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
		pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
		pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
		pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
		pgxmock.AnyArg(),
	).WillReturnError(errors.New("connection refused"))

	// Execute
	err := repo.Create(ctx, userAccount)

	// Assert
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUserAccountRepository_GetByID_DatabaseError tests handling of database errors during retrieval
func TestUserAccountRepository_GetByID_DatabaseError(t *testing.T) {
	mock, repo := setupUserAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userAccountID := uuid.New().String()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, user_id, institution_id, mask, account_name_hint,
		       nickname, custom_color, custom_icon, custom_emoji,
		       display_order, is_hidden, primary_type, primary_subtype,
		       created_at, updated_at
		FROM user_accounts
		WHERE id = $1
	`)).WithArgs(userAccountID).WillReturnError(errors.New("database timeout"))

	// Execute
	ua, err := repo.GetByID(ctx, userAccountID)

	// Assert
	assert.Nil(t, ua)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database timeout")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// =============================================================================
// GetByIDAndUser Tests (Ownership Validation)
// =============================================================================

// TestUserAccountRepository_GetByIDAndUser_Success tests retrieval with ownership validation
func TestUserAccountRepository_GetByIDAndUser_Success(t *testing.T) {
	mock, repo := setupUserAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userAccountID := uuid.New().String()
	userID := uuid.New().String()
	now := time.Now()

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "institution_id", "mask", "account_name_hint",
		"nickname", "custom_color", "custom_icon", "custom_emoji",
		"display_order", "is_hidden", "primary_type", "primary_subtype",
		"created_at", "updated_at",
	}).AddRow(
		userAccountID, userID, "ins_chase", strPtr("1234"), strPtr("Chase Checking"),
		nil, nil, nil, nil,
		nil, false, strPtr("depository"), strPtr("checking"),
		now, now,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, user_id, institution_id, mask, account_name_hint,
		       nickname, custom_color, custom_icon, custom_emoji,
		       display_order, is_hidden, primary_type, primary_subtype,
		       created_at, updated_at
		FROM user_accounts
		WHERE id = $1 AND user_id = $2
	`)).WithArgs(userAccountID, userID).WillReturnRows(rows)

	// Execute
	ua, err := repo.GetByIDAndUser(ctx, userAccountID, userID)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, userAccountID, ua.ID)
	assert.Equal(t, userID, ua.UserID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUserAccountRepository_GetByIDAndUser_NotOwner tests when user doesn't own the account
func TestUserAccountRepository_GetByIDAndUser_NotOwner(t *testing.T) {
	mock, repo := setupUserAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userAccountID := uuid.New().String()
	wrongUserID := uuid.New().String() // Different user

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, user_id, institution_id, mask, account_name_hint,
		       nickname, custom_color, custom_icon, custom_emoji,
		       display_order, is_hidden, primary_type, primary_subtype,
		       created_at, updated_at
		FROM user_accounts
		WHERE id = $1 AND user_id = $2
	`)).WithArgs(userAccountID, wrongUserID).WillReturnError(pgx.ErrNoRows)

	// Execute
	ua, err := repo.GetByIDAndUser(ctx, userAccountID, wrongUserID)

	// Assert
	assert.Nil(t, ua)
	assert.ErrorIs(t, err, ErrUserAccountNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// =============================================================================
// UpdatePrimaryTypeAndSubtype Tests
// =============================================================================

// TestUserAccountRepository_UpdatePrimaryTypeAndSubtype_Success tests updating type info
func TestUserAccountRepository_UpdatePrimaryTypeAndSubtype_Success(t *testing.T) {
	mock, repo := setupUserAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userAccountID := uuid.New().String()
	primaryType := "credit"
	primarySubtype := "credit card"

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE user_accounts
		SET primary_type = $1, primary_subtype = $2, updated_at = $3
		WHERE id = $4
	`)).WithArgs(
		primaryType,
		primarySubtype,
		pgxmock.AnyArg(), // updated_at
		userAccountID,
	).WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	// Execute
	err := repo.UpdatePrimaryTypeAndSubtype(ctx, userAccountID, primaryType, primarySubtype)

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUserAccountRepository_UpdatePrimaryTypeAndSubtype_NotFound tests update when not found
func TestUserAccountRepository_UpdatePrimaryTypeAndSubtype_NotFound(t *testing.T) {
	mock, repo := setupUserAccountMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userAccountID := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE user_accounts
		SET primary_type = $1, primary_subtype = $2, updated_at = $3
		WHERE id = $4
	`)).WithArgs(
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
		userAccountID,
	).WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	// Execute
	err := repo.UpdatePrimaryTypeAndSubtype(ctx, userAccountID, "depository", "checking")

	// Assert
	assert.ErrorIs(t, err, ErrUserAccountNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}
