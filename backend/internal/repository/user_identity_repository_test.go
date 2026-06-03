package repository

import (
	"context"
	
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupMockIdentityDB creates a mock database and sqlmock instance for testing user identities
func setupMockIdentityDB(t *testing.T) (pgxmock.PgxPoolIface, UserIdentityRepository) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)

	repo := NewUserIdentityRepository(mock, newTestFieldEncryptor(t))
	return mock, repo
}

// TestGetByClerkID_Success tests successful retrieval of identity by Clerk ID
func TestGetByClerkID_Success(t *testing.T) {
	mock, repo := setupMockIdentityDB(t)
	defer mock.Close()

	ctx := context.Background()
	clerkUserID := "google-oauth2|123456789"
	identityID := uuid.New().String()
	userID := uuid.New().String()
	now := time.Now()

	// DB stores encrypted values; repo decrypts them after scan
	encClerkUserID := "ENC$TENC[" + clerkUserID + "]"

	// Expected identity data
	rows := pgxmock.NewRows([]string{
		"id", "user_id", "clerk_user_id", "provider", "provider_user_id",
		"email", "email_verified", "is_primary",
		"created_at", "updated_at", "last_used_at",
	}).AddRow(
		identityID, userID, encClerkUserID, "google-oauth2", "ENC$TENC[123456789]",
		"ENC$TENC[test@example.com]", true, true,
		now, now, &now,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, clerk_user_id, provider, provider_user_id, email, email_verified, is_primary, created_at, updated_at, last_used_at FROM user_identities WHERE clerk_user_id_blind_idx = $1`)).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(rows)

	// Execute
	identity, err := repo.GetByClerkID(ctx, clerkUserID)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, identity)
	assert.Equal(t, identityID, identity.ID)
	assert.Equal(t, userID, identity.UserID)
	assert.Equal(t, clerkUserID, identity.ClerkUserID)
	assert.Equal(t, "google-oauth2", identity.Provider)
	assert.Equal(t, "123456789", identity.ProviderUserID)
	assert.Equal(t, "test@example.com", identity.Email)
	assert.True(t, identity.EmailVerified)
	assert.True(t, identity.IsPrimary)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetByClerkID_NotFound tests when identity is not found
func TestGetByClerkID_NotFound(t *testing.T) {
	mock, repo := setupMockIdentityDB(t)
	defer mock.Close()

	ctx := context.Background()
	clerkUserID := "google-oauth2|nonexistent"

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, clerk_user_id, provider, provider_user_id, email, email_verified, is_primary, created_at, updated_at, last_used_at FROM user_identities WHERE clerk_user_id_blind_idx = $1`)).
		WithArgs(pgxmock.AnyArg()).
		WillReturnError(pgx.ErrNoRows)

	// Execute
	identity, err := repo.GetByClerkID(ctx, clerkUserID)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, ErrIdentityNotFound, err)
	assert.Nil(t, identity)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetByUserID_Success tests successful retrieval of all identities for a user
func TestGetByUserID_Success(t *testing.T) {
	mock, repo := setupMockIdentityDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	now := time.Now()

	// Expected multiple identities (Google + Microsoft)
	// DB stores encrypted values; repo decrypts them after scan
	rows := pgxmock.NewRows([]string{
		"id", "user_id", "clerk_user_id", "provider", "provider_user_id",
		"email", "email_verified", "is_primary",
		"created_at", "updated_at", "last_used_at",
	}).
		AddRow(
			uuid.New().String(), userID, "ENC$TENC[google-oauth2|123]", "google-oauth2", "ENC$TENC[123]",
			"ENC$TENC[user@gmail.com]", true, true,
			now, now, &now,
		).
		AddRow(
			uuid.New().String(), userID, "ENC$TENC[microsoft|456]", "microsoft", "ENC$TENC[456]",
			"ENC$TENC[user@outlook.com]", true, false,
			now, now, nil,
		)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, clerk_user_id, provider, provider_user_id, email, email_verified, is_primary, created_at, updated_at, last_used_at FROM user_identities WHERE user_id = $1 ORDER BY is_primary DESC, created_at ASC`)).
		WithArgs(userID).
		WillReturnRows(rows)

	// Execute
	identities, err := repo.GetByUserID(ctx, userID)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, identities)
	assert.Len(t, identities, 2)
	assert.Equal(t, "google-oauth2", identities[0].Provider)
	assert.True(t, identities[0].IsPrimary)
	assert.Equal(t, "microsoft", identities[1].Provider)
	assert.False(t, identities[1].IsPrimary)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetByUserID_NotFound tests when no identities exist for user
func TestGetByUserID_NotFound(t *testing.T) {
	mock, repo := setupMockIdentityDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()

	// Return empty result set
	rows := pgxmock.NewRows([]string{
		"id", "user_id", "clerk_user_id", "provider", "provider_user_id",
		"email", "email_verified", "is_primary",
		"created_at", "updated_at", "last_used_at",
	})

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, clerk_user_id, provider, provider_user_id, email, email_verified, is_primary, created_at, updated_at, last_used_at FROM user_identities WHERE user_id = $1 ORDER BY is_primary DESC, created_at ASC`)).
		WithArgs(userID).
		WillReturnRows(rows)

	// Execute
	identities, err := repo.GetByUserID(ctx, userID)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, identities)
	assert.Len(t, identities, 0)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCreate_Success tests successful identity creation
func TestCreate_Success(t *testing.T) {
	mock, repo := setupMockIdentityDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	identity := domain.NewUserIdentity(userID, "google-oauth2|123456", "test@gmail.com", true, true)

	// Expect INSERT query — clerk_user_id, provider_user_id, and email are now encrypted;
	// clerk_user_id_blind_idx is the new 12th argument
	mock.ExpectExec(`INSERT INTO user_identities \( id, user_id, clerk_user_id, provider, provider_user_id, email, email_verified, is_primary, created_at, updated_at, last_used_at, clerk_user_id_blind_idx \) VALUES`).
		WithArgs(
			pgxmock.AnyArg(), // id (generated UUID)
			identity.UserID,
			pgxmock.AnyArg(), // clerk_user_id (encrypted)
			identity.Provider,
			pgxmock.AnyArg(), // provider_user_id (encrypted)
			pgxmock.AnyArg(), // email (encrypted)
			identity.EmailVerified,
			identity.IsPrimary,
			pgxmock.AnyArg(), // created_at
			pgxmock.AnyArg(), // updated_at
			identity.LastUsedAt,
			pgxmock.AnyArg(), // clerk_user_id_blind_idx
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	// Execute
	err := repo.Create(ctx, identity)

	// Assert
	assert.NoError(t, err)
	assert.NotEmpty(t, identity.ID) // UUID should be generated
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCreate_DuplicateClerkUserID tests handling of duplicate clerk_user_id constraint
func TestCreate_DuplicateClerkUserID(t *testing.T) {
	mock, repo := setupMockIdentityDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	identity := domain.NewUserIdentity(userID, "google-oauth2|123456", "test@gmail.com", true, true)

	// Simulate unique constraint violation on clerk_user_id
	// clerk_user_id, provider_user_id, and email are now encrypted; clerk_user_id_blind_idx added
	mock.ExpectExec(`INSERT INTO user_identities`).
		WithArgs(
			pgxmock.AnyArg(), // id
			identity.UserID,
			pgxmock.AnyArg(), // clerk_user_id (encrypted)
			identity.Provider,
			pgxmock.AnyArg(), // provider_user_id (encrypted)
			pgxmock.AnyArg(), // email (encrypted)
			identity.EmailVerified,
			identity.IsPrimary,
			pgxmock.AnyArg(), // created_at
			pgxmock.AnyArg(), // updated_at
			identity.LastUsedAt,
			pgxmock.AnyArg(), // clerk_user_id_blind_idx
		).
		WillReturnError(errors.New("pq: duplicate key value violates unique constraint \"user_identities_clerk_user_id_key\""))

	// Execute
	err := repo.Create(ctx, identity)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, ErrDuplicateIdentity, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUpdateLastUsed_Success tests successful last_used_at update
func TestUpdateLastUsed_Success(t *testing.T) {
	mock, repo := setupMockIdentityDB(t)
	defer mock.Close()

	ctx := context.Background()
	clerkUserID := "google-oauth2|123456"

	// Expect UPDATE query — WHERE now uses blind index, not plaintext clerk_user_id
	mock.ExpectExec(`UPDATE user_identities SET last_used_at = \$1, updated_at = \$2 WHERE clerk_user_id_blind_idx = \$3`).
		WithArgs(
			pgxmock.AnyArg(), // last_used_at (current time)
			pgxmock.AnyArg(), // updated_at (current time)
			pgxmock.AnyArg(), // blind index of clerkUserID
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	// Execute
	err := repo.UpdateLastUsed(ctx, clerkUserID)

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUpdateLastUsed_NotFound tests updating last_used_at for non-existent identity
func TestUpdateLastUsed_NotFound(t *testing.T) {
	mock, repo := setupMockIdentityDB(t)
	defer mock.Close()

	ctx := context.Background()
	clerkUserID := "google-oauth2|nonexistent"

	// Expect UPDATE query that affects 0 rows — WHERE now uses blind index
	mock.ExpectExec(`UPDATE user_identities SET last_used_at = \$1, updated_at = \$2 WHERE clerk_user_id_blind_idx = \$3`).
		WithArgs(
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(), // blind index of clerkUserID
		).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	// Execute
	err := repo.UpdateLastUsed(ctx, clerkUserID)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, ErrIdentityNotFound, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUserIdentityRepository_UpdatePrimaryIdentityEmailTx_Success tests successful email update
// on the primary identity within an existing transaction.
func TestUserIdentityRepository_UpdatePrimaryIdentityEmailTx_Success(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	mock, repo := setupMockIdentityDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	plainEmail := "newemail@example.com"

	mock.ExpectBegin()
	tx, err := mock.Begin(ctx)
	require.NoError(t, err)

	mock.ExpectExec(`UPDATE user_identities SET email = \$1, email_verified = false, updated_at = \$2 WHERE user_id = \$3 AND is_primary = true`).
		WithArgs(
			pgxmock.AnyArg(), // encrypted email
			pgxmock.AnyArg(), // updated_at
			userID,
		).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err = repo.UpdatePrimaryIdentityEmailTx(ctx, tx, userID, plainEmail)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUserIdentityRepository_UpdatePrimaryIdentityEmailTx_NoIdentity tests that 0 rows affected
// is treated as a no-op (not an error) when the user has no primary identity.
func TestUserIdentityRepository_UpdatePrimaryIdentityEmailTx_NoIdentity(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	mock, repo := setupMockIdentityDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	plainEmail := "newemail@example.com"

	mock.ExpectBegin()
	tx, err := mock.Begin(ctx)
	require.NoError(t, err)

	mock.ExpectExec(`UPDATE user_identities SET email = \$1, email_verified = false, updated_at = \$2 WHERE user_id = \$3 AND is_primary = true`).
		WithArgs(
			pgxmock.AnyArg(), // encrypted email
			pgxmock.AnyArg(), // updated_at
			userID,
		).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err = repo.UpdatePrimaryIdentityEmailTx(ctx, tx, userID, plainEmail)

	// 0 rows affected is a no-op — no error expected
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}
