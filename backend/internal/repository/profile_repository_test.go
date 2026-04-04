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

// setupMockDB creates a mock database and sqlmock instance for testing
func setupMockDB(t *testing.T) (pgxmock.PgxPoolIface, ProfileRepository) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)

	repo := NewProfileRepository(mock, newTestFieldEncryptor(t))
	return mock, repo
}

// TestGetUserByClerkID_Success tests successful retrieval of user by Clerk ID
func TestGetUserByClerkID_Success(t *testing.T) {
	mock, repo := setupMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	clerkUserID := "user_507f1f77bcf86cd799439011"
	userID := uuid.New().String()
	now := time.Now()

	rows := pgxmock.NewRows([]string{
		"id", "clerk_user_id", "email", "email_verified",
		"subscription_tier", "subscription_status", "subscription_started_at", "subscription_ends_at",
		"trial_started_at", "trial_ends_at", "had_trial",
		"paddle_customer_id", "paddle_subscription_id", "primary_region", "is_active", "is_deleted",
		"deletion_requested_at", "deletion_scheduled_at",
		"created_at", "updated_at", "last_login_at",
		"manual_tier_override", "manual_tier_ends_at",
	}).AddRow(
		userID, clerkUserID, "test@example.com", true,
		"free", "active", nil, nil,
		nil, nil, false,
		nil, nil, "iad", true, false,
		nil, nil,
		now, now, &now,
		nil, nil,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, clerk_user_id, email, email_verified, subscription_tier, subscription_status, subscription_started_at, subscription_ends_at, trial_started_at, trial_ends_at, had_trial, paddle_customer_id, paddle_subscription_id, primary_region, is_active, is_deleted, deletion_requested_at, deletion_scheduled_at, created_at, updated_at, last_login_at, manual_tier_override, manual_tier_ends_at FROM users WHERE clerk_user_id_blind_idx = $1 AND is_deleted = FALSE`)).
		WithArgs(pgxmock.AnyArg()).
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
	mock, repo := setupMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	clerkUserID := "user_nonexistent"

	// Issue #679, #680: Updated query to include trial columns
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, clerk_user_id, email, email_verified, subscription_tier, subscription_status, subscription_started_at, subscription_ends_at, trial_started_at, trial_ends_at, had_trial, paddle_customer_id, paddle_subscription_id, primary_region, is_active, is_deleted, deletion_requested_at, deletion_scheduled_at, created_at, updated_at, last_login_at, manual_tier_override, manual_tier_ends_at FROM users WHERE clerk_user_id_blind_idx = $1 AND is_deleted = FALSE`)).
		WithArgs(pgxmock.AnyArg()).
		WillReturnError(pgx.ErrNoRows)

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
	mock, repo := setupMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	clerkUserID := "user_507f1f77bcf86cd799439011"
	now := time.Now()

	rows := pgxmock.NewRows([]string{
		"id", "clerk_user_id", "email", "email_verified",
		"subscription_tier", "subscription_status", "subscription_started_at", "subscription_ends_at",
		"trial_started_at", "trial_ends_at", "had_trial",
		"paddle_customer_id", "paddle_subscription_id", "primary_region", "is_active", "is_deleted",
		"deletion_requested_at", "deletion_scheduled_at",
		"created_at", "updated_at", "last_login_at",
		"manual_tier_override", "manual_tier_ends_at",
	}).AddRow(
		userID, clerkUserID, "test@example.com", true,
		"premium", "active", &now, nil,
		nil, nil, false,
		stringPtr("cus_ABC123"), nil, "iad", true, false,
		nil, nil,
		now, now, &now,
		nil, nil,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, clerk_user_id, email, email_verified, subscription_tier, subscription_status, subscription_started_at, subscription_ends_at, trial_started_at, trial_ends_at, had_trial, paddle_customer_id, paddle_subscription_id, primary_region, is_active, is_deleted, deletion_requested_at, deletion_scheduled_at, created_at, updated_at, last_login_at, manual_tier_override, manual_tier_ends_at FROM users WHERE id = $1 AND is_deleted = FALSE`)).
		WithArgs(userID).
		WillReturnRows(rows)

	// Execute
	user, err := repo.GetUserByID(ctx, userID)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, user)
	assert.Equal(t, userID, user.ID)
	assert.Equal(t, "premium", user.SubscriptionTier)
	assert.NotNil(t, user.PaddleCustomerID)
	assert.Equal(t, "cus_ABC123", *user.PaddleCustomerID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetUserByID_NotFound tests when user is not found by ID
func TestGetUserByID_NotFound(t *testing.T) {
	mock, repo := setupMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()

	// Issue #679, #680: Updated query to include trial columns
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, clerk_user_id, email, email_verified, subscription_tier, subscription_status, subscription_started_at, subscription_ends_at, trial_started_at, trial_ends_at, had_trial, paddle_customer_id, paddle_subscription_id, primary_region, is_active, is_deleted, deletion_requested_at, deletion_scheduled_at, created_at, updated_at, last_login_at, manual_tier_override, manual_tier_ends_at FROM users WHERE id = $1 AND is_deleted = FALSE`)).
		WithArgs(userID).
		WillReturnError(pgx.ErrNoRows)

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
	mock, repo := setupMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	user := domain.NewUser("user_newuser", "newuser@example.com", true)

	// Expect INSERT query (match full query for consistency)
	// email and clerk_user_id are encrypted; blind indexes are added as $22, $23
	mock.ExpectExec(`INSERT INTO users \( id, clerk_user_id, email, email_verified, subscription_tier, subscription_status, subscription_started_at, subscription_ends_at, trial_started_at, trial_ends_at, had_trial, paddle_customer_id, paddle_subscription_id, primary_region, is_active, is_deleted, deletion_requested_at, deletion_scheduled_at, created_at, updated_at, last_login_at, email_blind_idx, clerk_user_id_blind_idx \) VALUES`).
		WithArgs(
			pgxmock.AnyArg(), // id (generated UUID)
			pgxmock.AnyArg(), // clerk_user_id (encrypted)
			pgxmock.AnyArg(), // email (encrypted)
			user.EmailVerified,
			user.SubscriptionTier,
			user.SubscriptionStatus,
			user.SubscriptionStartedAt,
			user.SubscriptionEndsAt,
			user.TrialStartedAt,
			user.TrialEndsAt,
			user.HadTrial,
			user.PaddleCustomerID,
			user.PaddleSubscriptionID,
			user.PrimaryRegion,
			user.IsActive,
			user.IsDeleted,
			user.DeletionRequestedAt,
			user.DeletionScheduledAt,
			pgxmock.AnyArg(), // created_at
			pgxmock.AnyArg(), // updated_at
			user.LastLoginAt,
			pgxmock.AnyArg(), // email_blind_idx
			pgxmock.AnyArg(), // clerk_user_id_blind_idx
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	// Execute
	err := repo.CreateUser(ctx, user)

	// Assert
	assert.NoError(t, err)
	assert.NotEmpty(t, user.ID) // UUID should be generated
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCreateUser_DuplicateEmail tests handling of duplicate email constraint
func TestCreateUser_DuplicateEmail(t *testing.T) {
	mock, repo := setupMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	user := domain.NewUser("user_newuser", "existing@example.com", true)

	// Simulate unique constraint violation on email
	mock.ExpectExec(`INSERT INTO users`).
		WithArgs(
			pgxmock.AnyArg(), // id
			pgxmock.AnyArg(), // clerk_user_id (encrypted)
			pgxmock.AnyArg(), // email (encrypted)
			user.EmailVerified,
			user.SubscriptionTier,
			user.SubscriptionStatus,
			user.SubscriptionStartedAt,
			user.SubscriptionEndsAt,
			user.TrialStartedAt,
			user.TrialEndsAt,
			user.HadTrial,
			user.PaddleCustomerID,
			user.PaddleSubscriptionID,
			user.PrimaryRegion,
			user.IsActive,
			user.IsDeleted,
			user.DeletionRequestedAt,
			user.DeletionScheduledAt,
			pgxmock.AnyArg(), // created_at
			pgxmock.AnyArg(), // updated_at
			user.LastLoginAt,
			pgxmock.AnyArg(), // email_blind_idx
			pgxmock.AnyArg(), // clerk_user_id_blind_idx
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
	mock, repo := setupMockDB(t)
	defer mock.Close()

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

	// Expect UPDATE query — email is encrypted ($1), email_blind_idx added ($5), id moves to $6
	mock.ExpectExec(`UPDATE users SET email = \$1, email_verified = \$2, primary_region = \$3, updated_at = \$4, email_blind_idx = \$5 WHERE id = \$6`).
		WithArgs(
			pgxmock.AnyArg(), // email (encrypted)
			user.EmailVerified,
			user.PrimaryRegion,
			pgxmock.AnyArg(), // updated_at
			pgxmock.AnyArg(), // email_blind_idx
			user.ID,
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	// Execute
	err := repo.UpdateUser(ctx, user)

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUpdateUser_NotFound tests updating non-existent user
func TestUpdateUser_NotFound(t *testing.T) {
	mock, repo := setupMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	user := &domain.User{
		ID:            uuid.New().String(),
		ClerkUserID:   "user_nonexistent",
		Email:         "test@example.com",
		PrimaryRegion: "iad",
	}

	// Expect UPDATE query that affects 0 rows — email is encrypted, email_blind_idx added
	mock.ExpectExec(`UPDATE users SET email = \$1, email_verified = \$2, primary_region = \$3, updated_at = \$4, email_blind_idx = \$5 WHERE id = \$6`).
		WithArgs(
			pgxmock.AnyArg(), // email (encrypted)
			user.EmailVerified,
			user.PrimaryRegion,
			pgxmock.AnyArg(), // updated_at
			pgxmock.AnyArg(), // email_blind_idx
			user.ID,
		).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	// Execute
	err := repo.UpdateUser(ctx, user)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, ErrUserNotFound, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUpdateLastLogin_Success tests successful last login update
func TestUpdateLastLogin_Success(t *testing.T) {
	mock, repo := setupMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	clerkUserID := "user_507f1f77bcf86cd799439011"

	// Expect UPDATE query — WHERE uses blind index instead of plaintext clerk_user_id
	mock.ExpectExec(`UPDATE users SET last_login_at = \$1, updated_at = \$2 WHERE clerk_user_id_blind_idx = \$3`).
		WithArgs(
			pgxmock.AnyArg(), // last_login_at (current time)
			pgxmock.AnyArg(), // updated_at (current time)
			pgxmock.AnyArg(), // clerk_user_id_blind_idx
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	// Execute
	err := repo.UpdateLastLogin(ctx, clerkUserID)

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUpdateLastLogin_NotFound tests updating last login for non-existent user
func TestUpdateLastLogin_NotFound(t *testing.T) {
	mock, repo := setupMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	clerkUserID := "user_nonexistent"

	// Expect UPDATE query that affects 0 rows — WHERE uses blind index
	mock.ExpectExec(`UPDATE users SET last_login_at = \$1, updated_at = \$2 WHERE clerk_user_id_blind_idx = \$3`).
		WithArgs(
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(), // clerk_user_id_blind_idx
		).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

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

// TestGetUserByClerkID_WithManualOverride tests that manual_tier_override is returned
func TestGetUserByClerkID_WithManualOverride(t *testing.T) {
	mock, repo := setupMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	clerkUserID := "user_507f1f77bcf86cd799439011"
	userID := uuid.New().String()
	now := time.Now()
	override := "household_2"

	rows := pgxmock.NewRows([]string{
		"id", "clerk_user_id", "email", "email_verified",
		"subscription_tier", "subscription_status", "subscription_started_at", "subscription_ends_at",
		"trial_started_at", "trial_ends_at", "had_trial",
		"paddle_customer_id", "paddle_subscription_id", "primary_region", "is_active", "is_deleted",
		"deletion_requested_at", "deletion_scheduled_at",
		"created_at", "updated_at", "last_login_at",
		"manual_tier_override", "manual_tier_ends_at",
	}).AddRow(
		userID, clerkUserID, "test@example.com", true,
		"premium", "trial", nil, nil,
		nil, nil, false,
		nil, nil, "iad", true, false,
		nil, nil,
		now, now, &now,
		&override, nil,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, clerk_user_id, email, email_verified, subscription_tier, subscription_status, subscription_started_at, subscription_ends_at, trial_started_at, trial_ends_at, had_trial, paddle_customer_id, paddle_subscription_id, primary_region, is_active, is_deleted, deletion_requested_at, deletion_scheduled_at, created_at, updated_at, last_login_at, manual_tier_override, manual_tier_ends_at FROM users WHERE clerk_user_id_blind_idx = $1 AND is_deleted = FALSE`)).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(rows)

	user, err := repo.GetUserByClerkID(ctx, clerkUserID)

	assert.NoError(t, err)
	assert.NotNil(t, user)
	assert.NotNil(t, user.ManualTierOverride)
	assert.Equal(t, "household_2", *user.ManualTierOverride)
	assert.Nil(t, user.ManualTierEndsAt)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetUserByID_WithManualOverride tests that manual_tier_override is returned by ID lookup
func TestGetUserByID_WithManualOverride(t *testing.T) {
	mock, repo := setupMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	clerkUserID := "user_507f1f77bcf86cd799439011"
	now := time.Now()
	override := "household_3"
	overrideEndsAt := now.Add(30 * 24 * time.Hour)

	rows := pgxmock.NewRows([]string{
		"id", "clerk_user_id", "email", "email_verified",
		"subscription_tier", "subscription_status", "subscription_started_at", "subscription_ends_at",
		"trial_started_at", "trial_ends_at", "had_trial",
		"paddle_customer_id", "paddle_subscription_id", "primary_region", "is_active", "is_deleted",
		"deletion_requested_at", "deletion_scheduled_at",
		"created_at", "updated_at", "last_login_at",
		"manual_tier_override", "manual_tier_ends_at",
	}).AddRow(
		userID, clerkUserID, "test@example.com", true,
		"premium", "active", &now, nil,
		nil, nil, false,
		nil, nil, "iad", true, false,
		nil, nil,
		now, now, &now,
		&override, &overrideEndsAt,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, clerk_user_id, email, email_verified, subscription_tier, subscription_status, subscription_started_at, subscription_ends_at, trial_started_at, trial_ends_at, had_trial, paddle_customer_id, paddle_subscription_id, primary_region, is_active, is_deleted, deletion_requested_at, deletion_scheduled_at, created_at, updated_at, last_login_at, manual_tier_override, manual_tier_ends_at FROM users WHERE id = $1 AND is_deleted = FALSE`)).
		WithArgs(userID).
		WillReturnRows(rows)

	user, err := repo.GetUserByID(ctx, userID)

	assert.NoError(t, err)
	assert.NotNil(t, user)
	assert.NotNil(t, user.ManualTierOverride)
	assert.Equal(t, "household_3", *user.ManualTierOverride)
	assert.NotNil(t, user.ManualTierEndsAt)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetUserByPaddleSubscriptionID_Success tests successful retrieval by subscription ID
func TestGetUserByPaddleSubscriptionID_Success(t *testing.T) {
	mock, repo := setupMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	subscriptionID := "sub_01h12345"
	userID := uuid.New().String()
	now := time.Now()

	rows := pgxmock.NewRows([]string{
		"id", "clerk_user_id", "email", "email_verified",
		"subscription_tier", "subscription_status", "subscription_started_at", "subscription_ends_at",
		"trial_started_at", "trial_ends_at", "had_trial",
		"paddle_customer_id", "paddle_subscription_id", "primary_region",
		"is_active", "is_deleted",
		"deletion_requested_at", "deletion_scheduled_at",
		"created_at", "updated_at", "last_login_at",
		"manual_tier_override", "manual_tier_ends_at",
	}).AddRow(
		userID, "clerk_123", "test@example.com", true,
		"premium", "active", &now, nil,
		nil, nil, false,
		stringPtr("cus_123"), stringPtr(subscriptionID), "iad",
		true, false,
		nil, nil,
		now, now, &now,
		nil, nil,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, clerk_user_id, email, email_verified, subscription_tier, subscription_status, subscription_started_at, subscription_ends_at, trial_started_at, trial_ends_at, had_trial, paddle_customer_id, paddle_subscription_id, primary_region, is_active, is_deleted, deletion_requested_at, deletion_scheduled_at, created_at, updated_at, last_login_at, manual_tier_override, manual_tier_ends_at FROM users WHERE paddle_subscription_id = $1 AND is_deleted = FALSE`)).
		WithArgs(subscriptionID).
		WillReturnRows(rows)

	// Execute
	user, err := repo.GetUserByPaddleSubscriptionID(ctx, subscriptionID)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, user)
	assert.Equal(t, userID, user.ID)
	assert.NotNil(t, user.PaddleSubscriptionID)
	assert.Equal(t, subscriptionID, *user.PaddleSubscriptionID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetUserByPaddleSubscriptionID_NotFound tests handling of non-existent subscription ID
func TestGetUserByPaddleSubscriptionID_NotFound(t *testing.T) {
	mock, repo := setupMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	subscriptionID := "sub_nonexistent"

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, clerk_user_id, email, email_verified, subscription_tier, subscription_status, subscription_started_at, subscription_ends_at, trial_started_at, trial_ends_at, had_trial, paddle_customer_id, paddle_subscription_id, primary_region, is_active, is_deleted, deletion_requested_at, deletion_scheduled_at, created_at, updated_at, last_login_at, manual_tier_override, manual_tier_ends_at FROM users WHERE paddle_subscription_id = $1 AND is_deleted = FALSE`)).
		WithArgs(subscriptionID).
		WillReturnError(pgx.ErrNoRows)

	// Execute
	user, err := repo.GetUserByPaddleSubscriptionID(ctx, subscriptionID)

	// Assert
	assert.Error(t, err)
	assert.Nil(t, user)
	assert.Equal(t, ErrUserNotFound, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUpdateSubscriptionFromPaddle_WithSubscriptionID tests updating with subscription ID
func TestUpdateSubscriptionFromPaddle_WithSubscriptionID(t *testing.T) {
	mock, repo := setupMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	tier := "premium"
	status := "active"
	subscriptionID := "sub_01h12345"
	now := time.Now()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE users SET subscription_tier = $1, subscription_status = $2, subscription_started_at = $3, subscription_ends_at = $4, paddle_subscription_id = $5, updated_at = $6 WHERE id = $7 AND is_deleted = FALSE`)).
		WithArgs(tier, status, pgxmock.AnyArg(), (*time.Time)(nil), &subscriptionID, pgxmock.AnyArg(), userID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	// Execute
	err := repo.UpdateSubscriptionFromPaddle(ctx, userID, tier, status, &now, (*time.Time)(nil), &subscriptionID)

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUpdateSubscriptionFromPaddle_WithNullSubscriptionID tests updating with null subscription ID
func TestUpdateSubscriptionFromPaddle_WithNullSubscriptionID(t *testing.T) {
	mock, repo := setupMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	tier := "premium"
	status := "active"
	now := time.Now()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE users SET subscription_tier = $1, subscription_status = $2, subscription_started_at = $3, subscription_ends_at = $4, paddle_subscription_id = $5, updated_at = $6 WHERE id = $7 AND is_deleted = FALSE`)).
		WithArgs(tier, status, pgxmock.AnyArg(), (*time.Time)(nil), (*string)(nil), pgxmock.AnyArg(), userID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	// Execute
	err := repo.UpdateSubscriptionFromPaddle(ctx, userID, tier, status, &now, (*time.Time)(nil), (*string)(nil))

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// userSelectQueryForTest is the expected SELECT query used in tests for profile lookups
const userSelectQueryForTest = `SELECT id, clerk_user_id, email, email_verified, subscription_tier, subscription_status, subscription_started_at, subscription_ends_at, trial_started_at, trial_ends_at, had_trial, paddle_customer_id, paddle_subscription_id, primary_region, is_active, is_deleted, deletion_requested_at, deletion_scheduled_at, created_at, updated_at, last_login_at, manual_tier_override, manual_tier_ends_at FROM users`

// makeUserRow creates a pgxmock row for a user with the given parameters
func makeUserRow(t *testing.T, userID, clerkID, email string, paddleSubID *string) *pgxmock.Rows {
	t.Helper()
	now := time.Now()
	rows := pgxmock.NewRows([]string{
		"id", "clerk_user_id", "email", "email_verified",
		"subscription_tier", "subscription_status", "subscription_started_at", "subscription_ends_at",
		"trial_started_at", "trial_ends_at", "had_trial",
		"paddle_customer_id", "paddle_subscription_id", "primary_region", "is_active", "is_deleted",
		"deletion_requested_at", "deletion_scheduled_at",
		"created_at", "updated_at", "last_login_at",
		"manual_tier_override", "manual_tier_ends_at",
	}).AddRow(
		userID, clerkID, email, true,
		"premium", "active", &now, nil,
		nil, nil, false,
		stringPtr("cus_123"), paddleSubID, "iad", true, false,
		nil, nil,
		now, now, &now,
		nil, nil,
	)
	return rows
}

// TestGetUsersWithActivePaddleSubscriptions_WithResults tests returning users with active subscriptions
func TestGetUsersWithActivePaddleSubscriptions_WithResults(t *testing.T) {
	mock, repo := setupMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID1 := uuid.New().String()
	userID2 := uuid.New().String()
	subID1 := "sub_01h111"
	subID2 := "sub_01h222"

	now := time.Now()
	rows := pgxmock.NewRows([]string{
		"id", "clerk_user_id", "email", "email_verified",
		"subscription_tier", "subscription_status", "subscription_started_at", "subscription_ends_at",
		"trial_started_at", "trial_ends_at", "had_trial",
		"paddle_customer_id", "paddle_subscription_id", "primary_region", "is_active", "is_deleted",
		"deletion_requested_at", "deletion_scheduled_at",
		"created_at", "updated_at", "last_login_at",
		"manual_tier_override", "manual_tier_ends_at",
	}).AddRow(
		userID1, "clerk_1", "user1@example.com", true,
		"premium", "active", &now, nil,
		nil, nil, false,
		stringPtr("cus_1"), &subID1, "iad", true, false,
		nil, nil, now, now, &now, nil, nil,
	).AddRow(
		userID2, "clerk_2", "user2@example.com", true,
		"premium", "active", &now, nil,
		nil, nil, false,
		stringPtr("cus_2"), &subID2, "iad", true, false,
		nil, nil, now, now, &now, nil, nil,
	)

	mock.ExpectQuery(regexp.QuoteMeta(userSelectQueryForTest+` WHERE paddle_subscription_id IS NOT NULL AND subscription_status NOT IN ('inactive', 'cancelled') AND is_deleted = FALSE`)).
		WillReturnRows(rows)

	users, err := repo.GetUsersWithActivePaddleSubscriptions(ctx)

	require.NoError(t, err)
	assert.Len(t, users, 2)
	assert.Equal(t, userID1, users[0].ID)
	assert.Equal(t, userID2, users[1].ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetUsersWithActivePaddleSubscriptions_Empty tests that empty slice (not nil) is returned when no users
func TestGetUsersWithActivePaddleSubscriptions_Empty(t *testing.T) {
	mock, repo := setupMockDB(t)
	defer mock.Close()

	ctx := context.Background()

	rows := pgxmock.NewRows([]string{
		"id", "clerk_user_id", "email", "email_verified",
		"subscription_tier", "subscription_status", "subscription_started_at", "subscription_ends_at",
		"trial_started_at", "trial_ends_at", "had_trial",
		"paddle_customer_id", "paddle_subscription_id", "primary_region", "is_active", "is_deleted",
		"deletion_requested_at", "deletion_scheduled_at",
		"created_at", "updated_at", "last_login_at",
		"manual_tier_override", "manual_tier_ends_at",
	})

	mock.ExpectQuery(regexp.QuoteMeta(userSelectQueryForTest+` WHERE paddle_subscription_id IS NOT NULL AND subscription_status NOT IN ('inactive', 'cancelled') AND is_deleted = FALSE`)).
		WillReturnRows(rows)

	users, err := repo.GetUsersWithActivePaddleSubscriptions(ctx)

	require.NoError(t, err)
	assert.NotNil(t, users)
	assert.Empty(t, users)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetUsersWithActivePaddleSubscriptions_ExcludesDeleted verifies is_deleted = FALSE filter works
func TestGetUsersWithActivePaddleSubscriptions_ExcludesDeleted(t *testing.T) {
	mock, repo := setupMockDB(t)
	defer mock.Close()

	ctx := context.Background()

	// Query filters out deleted users via is_deleted = FALSE; mock returns empty result
	rows := pgxmock.NewRows([]string{
		"id", "clerk_user_id", "email", "email_verified",
		"subscription_tier", "subscription_status", "subscription_started_at", "subscription_ends_at",
		"trial_started_at", "trial_ends_at", "had_trial",
		"paddle_customer_id", "paddle_subscription_id", "primary_region", "is_active", "is_deleted",
		"deletion_requested_at", "deletion_scheduled_at",
		"created_at", "updated_at", "last_login_at",
		"manual_tier_override", "manual_tier_ends_at",
	})

	mock.ExpectQuery(regexp.QuoteMeta(userSelectQueryForTest+` WHERE paddle_subscription_id IS NOT NULL AND subscription_status NOT IN ('inactive', 'cancelled') AND is_deleted = FALSE`)).
		WillReturnRows(rows)

	users, err := repo.GetUsersWithActivePaddleSubscriptions(ctx)

	require.NoError(t, err)
	assert.Empty(t, users)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetUsersWithActivePaddleSubscriptions_ExcludesInactive verifies inactive status filter works
func TestGetUsersWithActivePaddleSubscriptions_ExcludesInactive(t *testing.T) {
	mock, repo := setupMockDB(t)
	defer mock.Close()

	ctx := context.Background()

	// Query filters out inactive/cancelled users; mock returns empty result
	rows := pgxmock.NewRows([]string{
		"id", "clerk_user_id", "email", "email_verified",
		"subscription_tier", "subscription_status", "subscription_started_at", "subscription_ends_at",
		"trial_started_at", "trial_ends_at", "had_trial",
		"paddle_customer_id", "paddle_subscription_id", "primary_region", "is_active", "is_deleted",
		"deletion_requested_at", "deletion_scheduled_at",
		"created_at", "updated_at", "last_login_at",
		"manual_tier_override", "manual_tier_ends_at",
	})

	mock.ExpectQuery(regexp.QuoteMeta(userSelectQueryForTest+` WHERE paddle_subscription_id IS NOT NULL AND subscription_status NOT IN ('inactive', 'cancelled') AND is_deleted = FALSE`)).
		WillReturnRows(rows)

	users, err := repo.GetUsersWithActivePaddleSubscriptions(ctx)

	require.NoError(t, err)
	assert.Empty(t, users)
	assert.NoError(t, mock.ExpectationsWereMet())
}
