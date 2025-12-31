package repository

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupMockDB creates a mock database and sqlmock instance for testing
func setupMockDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock, ProfileRepository) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)

	repo := NewProfileRepository(db)
	return db, mock, repo
}

// TestGetUserByClerkID_Success tests successful retrieval of user by Clerk ID
func TestGetUserByClerkID_Success(t *testing.T) {
	db, mock, repo := setupMockDB(t)
	defer db.Close()

	ctx := context.Background()
	clerkUserID := "user_507f1f77bcf86cd799439011"
	userID := uuid.New().String()
	now := time.Now()

	// Expected user data
	rows := sqlmock.NewRows([]string{
		"id", "clerk_user_id", "email", "email_verified",
		"subscription_tier", "subscription_status", "subscription_started_at", "subscription_ends_at",
		"stripe_customer_id", "primary_region", "is_active", "is_deleted",
		"deletion_requested_at", "deletion_scheduled_at",
		"created_at", "updated_at", "last_login_at",
	}).AddRow(
		userID, clerkUserID, "test@example.com", true,
		"free", "active", nil, nil,
		nil, "iad", true, false,
		nil, nil,
		now, now, &now,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, clerk_user_id, email, email_verified, subscription_tier, subscription_status, subscription_started_at, subscription_ends_at, stripe_customer_id, primary_region, is_active, is_deleted, deletion_requested_at, deletion_scheduled_at, created_at, updated_at, last_login_at FROM users WHERE clerk_user_id = $1 AND is_deleted = FALSE`)).
		WithArgs(clerkUserID).
		WillReturnRows(rows)

	// Execute
	user, err := repo.GetUserByClerkID(ctx, clerkUserID)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, user)
	assert.Equal(t, userID, user.ID)
	assert.Equal(t, clerkUserID, user.ClerkUserID)
	assert.Equal(t, "test@example.com", user.Email)
	assert.Equal(t, "free", user.SubscriptionTier)
	assert.Equal(t, "iad", user.PrimaryRegion)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetUserByClerkID_NotFound tests when user is not found
func TestGetUserByClerkID_NotFound(t *testing.T) {
	db, mock, repo := setupMockDB(t)
	defer db.Close()

	ctx := context.Background()
	clerkUserID := "user_nonexistent"

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, clerk_user_id, email, email_verified, subscription_tier, subscription_status, subscription_started_at, subscription_ends_at, stripe_customer_id, primary_region, is_active, is_deleted, deletion_requested_at, deletion_scheduled_at, created_at, updated_at, last_login_at FROM users WHERE clerk_user_id = $1 AND is_deleted = FALSE`)).
		WithArgs(clerkUserID).
		WillReturnError(sql.ErrNoRows)

	// Execute
	user, err := repo.GetUserByClerkID(ctx, clerkUserID)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, ErrUserNotFound, err)
	assert.Nil(t, user)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetUserByID_Success tests successful retrieval of user by ID
func TestGetUserByID_Success(t *testing.T) {
	db, mock, repo := setupMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	clerkUserID := "user_507f1f77bcf86cd799439011"
	now := time.Now()

	rows := sqlmock.NewRows([]string{
		"id", "clerk_user_id", "email", "email_verified",
		"subscription_tier", "subscription_status", "subscription_started_at", "subscription_ends_at",
		"stripe_customer_id", "primary_region", "is_active", "is_deleted",
		"deletion_requested_at", "deletion_scheduled_at",
		"created_at", "updated_at", "last_login_at",
	}).AddRow(
		userID, clerkUserID, "test@example.com", true,
		"premium", "active", &now, nil,
		stringPtr("cus_ABC123"), "iad", true, false,
		nil, nil,
		now, now, &now,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, clerk_user_id, email, email_verified, subscription_tier, subscription_status, subscription_started_at, subscription_ends_at, stripe_customer_id, primary_region, is_active, is_deleted, deletion_requested_at, deletion_scheduled_at, created_at, updated_at, last_login_at FROM users WHERE id = $1 AND is_deleted = FALSE`)).
		WithArgs(userID).
		WillReturnRows(rows)

	// Execute
	user, err := repo.GetUserByID(ctx, userID)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, user)
	assert.Equal(t, userID, user.ID)
	assert.Equal(t, "premium", user.SubscriptionTier)
	assert.NotNil(t, user.StripeCustomerID)
	assert.Equal(t, "cus_ABC123", *user.StripeCustomerID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetUserByID_NotFound tests when user is not found by ID
func TestGetUserByID_NotFound(t *testing.T) {
	db, mock, repo := setupMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, clerk_user_id, email, email_verified, subscription_tier, subscription_status, subscription_started_at, subscription_ends_at, stripe_customer_id, primary_region, is_active, is_deleted, deletion_requested_at, deletion_scheduled_at, created_at, updated_at, last_login_at FROM users WHERE id = $1 AND is_deleted = FALSE`)).
		WithArgs(userID).
		WillReturnError(sql.ErrNoRows)

	// Execute
	user, err := repo.GetUserByID(ctx, userID)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, ErrUserNotFound, err)
	assert.Nil(t, user)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCreateUser_Success tests successful user creation
func TestCreateUser_Success(t *testing.T) {
	db, mock, repo := setupMockDB(t)
	defer db.Close()

	ctx := context.Background()
	user := domain.NewUser("user_newuser", "newuser@example.com", true)

	// Expect INSERT query (match full query for consistency)
	mock.ExpectExec(`INSERT INTO users \( id, clerk_user_id, email, email_verified, subscription_tier, subscription_status, subscription_started_at, subscription_ends_at, stripe_customer_id, primary_region, is_active, is_deleted, deletion_requested_at, deletion_scheduled_at, created_at, updated_at, last_login_at \) VALUES`).
		WithArgs(
			sqlmock.AnyArg(), // id (generated UUID)
			user.ClerkUserID,
			user.Email,
			user.EmailVerified,
			user.SubscriptionTier,
			user.SubscriptionStatus,
			user.SubscriptionStartedAt,
			user.SubscriptionEndsAt,
			user.StripeCustomerID,
			user.PrimaryRegion,
			user.IsActive,
			user.IsDeleted,
			user.DeletionRequestedAt,
			user.DeletionScheduledAt,
			sqlmock.AnyArg(), // created_at
			sqlmock.AnyArg(), // updated_at
			user.LastLoginAt,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Execute
	err := repo.CreateUser(ctx, user)

	// Assert
	assert.NoError(t, err)
	assert.NotEmpty(t, user.ID) // UUID should be generated
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCreateUser_DuplicateEmail tests handling of duplicate email constraint
func TestCreateUser_DuplicateEmail(t *testing.T) {
	db, mock, repo := setupMockDB(t)
	defer db.Close()

	ctx := context.Background()
	user := domain.NewUser("user_newuser", "existing@example.com", true)

	// Simulate unique constraint violation on email
	mock.ExpectExec(`INSERT INTO users`).
		WithArgs(
			sqlmock.AnyArg(),
			user.ClerkUserID,
			user.Email,
			user.EmailVerified,
			user.SubscriptionTier,
			user.SubscriptionStatus,
			user.SubscriptionStartedAt,
			user.SubscriptionEndsAt,
			user.StripeCustomerID,
			user.PrimaryRegion,
			user.IsActive,
			user.IsDeleted,
			user.DeletionRequestedAt,
			user.DeletionScheduledAt,
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			user.LastLoginAt,
		).
		WillReturnError(errors.New("pq: duplicate key value violates unique constraint \"users_email_key\""))

	// Execute
	err := repo.CreateUser(ctx, user)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, ErrDuplicateEmail, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUpdateUser_Success tests successful user update
func TestUpdateUser_Success(t *testing.T) {
	db, mock, repo := setupMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	user := &domain.User{
		ID:                 userID,
		ClerkUserID:        "user_507f1f77bcf86cd799439011",
		Email:              "updated@example.com",
		EmailVerified:      true,
		SubscriptionTier:   "free",
		SubscriptionStatus: "active",
		PrimaryRegion:      "eu-west-1",
		IsActive:           true,
		IsDeleted:          false,
	}

	// Expect UPDATE query
	mock.ExpectExec(`UPDATE users SET email = \$1, email_verified = \$2, primary_region = \$3, updated_at = \$4 WHERE id = \$5`).
		WithArgs(
			user.Email,
			user.EmailVerified,
			user.PrimaryRegion,
			sqlmock.AnyArg(), // updated_at
			user.ID,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Execute
	err := repo.UpdateUser(ctx, user)

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUpdateUser_NotFound tests updating non-existent user
func TestUpdateUser_NotFound(t *testing.T) {
	db, mock, repo := setupMockDB(t)
	defer db.Close()

	ctx := context.Background()
	user := &domain.User{
		ID:            uuid.New().String(),
		ClerkUserID:   "user_nonexistent",
		Email:         "test@example.com",
		PrimaryRegion: "iad",
	}

	// Expect UPDATE query that affects 0 rows
	mock.ExpectExec(`UPDATE users SET email = \$1, email_verified = \$2, primary_region = \$3, updated_at = \$4 WHERE id = \$5`).
		WithArgs(
			user.Email,
			user.EmailVerified,
			user.PrimaryRegion,
			sqlmock.AnyArg(),
			user.ID,
		).
		WillReturnResult(sqlmock.NewResult(0, 0))

	// Execute
	err := repo.UpdateUser(ctx, user)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, ErrUserNotFound, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUpdateLastLogin_Success tests successful last login update
func TestUpdateLastLogin_Success(t *testing.T) {
	db, mock, repo := setupMockDB(t)
	defer db.Close()

	ctx := context.Background()
	clerkUserID := "user_507f1f77bcf86cd799439011"

	// Expect UPDATE query
	mock.ExpectExec(`UPDATE users SET last_login_at = \$1, updated_at = \$2 WHERE clerk_user_id = \$3`).
		WithArgs(
			sqlmock.AnyArg(), // last_login_at (current time)
			sqlmock.AnyArg(), // updated_at (current time)
			clerkUserID,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Execute
	err := repo.UpdateLastLogin(ctx, clerkUserID)

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUpdateLastLogin_NotFound tests updating last login for non-existent user
func TestUpdateLastLogin_NotFound(t *testing.T) {
	db, mock, repo := setupMockDB(t)
	defer db.Close()

	ctx := context.Background()
	clerkUserID := "user_nonexistent"

	// Expect UPDATE query that affects 0 rows
	mock.ExpectExec(`UPDATE users SET last_login_at = \$1, updated_at = \$2 WHERE clerk_user_id = \$3`).
		WithArgs(
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			clerkUserID,
		).
		WillReturnResult(sqlmock.NewResult(0, 0))

	// Execute
	err := repo.UpdateLastLogin(ctx, clerkUserID)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, ErrUserNotFound, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// Helper function to create string pointer
func stringPtr(s string) *string {
	return &s
}
