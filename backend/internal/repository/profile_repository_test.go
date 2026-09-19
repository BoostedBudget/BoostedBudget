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
		"retention_started_at", "retention_expires_at", "retention_notice_30d_sent_at", "retention_notice_7d_sent_at",
		"timezone", "timezone_source",
		"past_due_started_at", "plaid_cleanup_pending_at",
	}).AddRow(
		userID, clerkUserID, "test@example.com", true,
		"free", "active", nil, nil,
		nil, nil, false,
		nil, nil, "iad", true, false,
		nil, nil,
		now, now, &now,
		nil, nil,
		nil, nil, nil, nil,
		"UTC", "default",
		nil, nil,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, clerk_user_id, email, email_verified, subscription_tier, subscription_status, subscription_started_at, subscription_ends_at, trial_started_at, trial_ends_at, had_trial, paddle_customer_id, paddle_subscription_id, primary_region, is_active, is_deleted, deletion_requested_at, deletion_scheduled_at, created_at, updated_at, last_login_at, manual_tier_override, manual_tier_ends_at, retention_started_at, retention_expires_at, retention_notice_30d_sent_at, retention_notice_7d_sent_at, timezone, timezone_source, past_due_started_at, plaid_cleanup_pending_at FROM users WHERE clerk_user_id_blind_idx = $1 AND is_deleted = FALSE`)).
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
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, clerk_user_id, email, email_verified, subscription_tier, subscription_status, subscription_started_at, subscription_ends_at, trial_started_at, trial_ends_at, had_trial, paddle_customer_id, paddle_subscription_id, primary_region, is_active, is_deleted, deletion_requested_at, deletion_scheduled_at, created_at, updated_at, last_login_at, manual_tier_override, manual_tier_ends_at, retention_started_at, retention_expires_at, retention_notice_30d_sent_at, retention_notice_7d_sent_at, timezone, timezone_source, past_due_started_at, plaid_cleanup_pending_at FROM users WHERE clerk_user_id_blind_idx = $1 AND is_deleted = FALSE`)).
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
		"retention_started_at", "retention_expires_at", "retention_notice_30d_sent_at", "retention_notice_7d_sent_at",
		"timezone", "timezone_source",
		"past_due_started_at", "plaid_cleanup_pending_at",
	}).AddRow(
		userID, clerkUserID, "test@example.com", true,
		"premium", "active", &now, nil,
		nil, nil, false,
		stringPtr("cus_ABC123"), nil, "iad", true, false,
		nil, nil,
		now, now, &now,
		nil, nil,
		nil, nil, nil, nil,
		"UTC", "default",
		nil, nil,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, clerk_user_id, email, email_verified, subscription_tier, subscription_status, subscription_started_at, subscription_ends_at, trial_started_at, trial_ends_at, had_trial, paddle_customer_id, paddle_subscription_id, primary_region, is_active, is_deleted, deletion_requested_at, deletion_scheduled_at, created_at, updated_at, last_login_at, manual_tier_override, manual_tier_ends_at, retention_started_at, retention_expires_at, retention_notice_30d_sent_at, retention_notice_7d_sent_at, timezone, timezone_source, past_due_started_at, plaid_cleanup_pending_at FROM users WHERE id = $1 AND is_deleted = FALSE`)).
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
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, clerk_user_id, email, email_verified, subscription_tier, subscription_status, subscription_started_at, subscription_ends_at, trial_started_at, trial_ends_at, had_trial, paddle_customer_id, paddle_subscription_id, primary_region, is_active, is_deleted, deletion_requested_at, deletion_scheduled_at, created_at, updated_at, last_login_at, manual_tier_override, manual_tier_ends_at, retention_started_at, retention_expires_at, retention_notice_30d_sent_at, retention_notice_7d_sent_at, timezone, timezone_source, past_due_started_at, plaid_cleanup_pending_at FROM users WHERE id = $1 AND is_deleted = FALSE`)).
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
	// email and clerk_user_id are encrypted; paddle fields are encrypted; blind indexes added as $22, $23, $24
	mock.ExpectExec(`INSERT INTO users \( id, clerk_user_id, email, email_verified, subscription_tier, subscription_status, subscription_started_at, subscription_ends_at, trial_started_at, trial_ends_at, had_trial, paddle_customer_id, paddle_subscription_id, primary_region, is_active, is_deleted, deletion_requested_at, deletion_scheduled_at, created_at, updated_at, last_login_at, email_blind_idx, clerk_user_id_blind_idx, paddle_subscription_id_blind_idx \) VALUES`).
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
			(*string)(nil), // paddle_customer_id (encrypted, nil → nil)
			(*string)(nil), // paddle_subscription_id (encrypted, nil → nil)
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
			(*string)(nil),   // paddle_subscription_id_blind_idx (nil since subscription ID is nil)
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
			(*string)(nil), // paddle_customer_id (encrypted, nil → nil)
			(*string)(nil), // paddle_subscription_id (encrypted, nil → nil)
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
			(*string)(nil),   // paddle_subscription_id_blind_idx (nil since subscription ID is nil)
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

	// Expect UPDATE query — email is encrypted ($1), email_blind_idx added ($5), timezone=$6, timezone_source=$7, id=$8
	mock.ExpectExec(`UPDATE users SET email = \$1, email_verified = \$2, primary_region = \$3, updated_at = \$4, email_blind_idx = \$5, timezone = \$6, timezone_source = \$7 WHERE id = \$8`).
		WithArgs(
			pgxmock.AnyArg(), // email (encrypted)
			user.EmailVerified,
			user.PrimaryRegion,
			pgxmock.AnyArg(), // updated_at
			pgxmock.AnyArg(), // email_blind_idx
			user.Timezone,
			user.TimezoneSource,
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

	// Expect UPDATE query that affects 0 rows — email is encrypted, email_blind_idx added, timezone fields added
	mock.ExpectExec(`UPDATE users SET email = \$1, email_verified = \$2, primary_region = \$3, updated_at = \$4, email_blind_idx = \$5, timezone = \$6, timezone_source = \$7 WHERE id = \$8`).
		WithArgs(
			pgxmock.AnyArg(), // email (encrypted)
			user.EmailVerified,
			user.PrimaryRegion,
			pgxmock.AnyArg(), // updated_at
			pgxmock.AnyArg(), // email_blind_idx
			user.Timezone,
			user.TimezoneSource,
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
		"retention_started_at", "retention_expires_at", "retention_notice_30d_sent_at", "retention_notice_7d_sent_at",
		"timezone", "timezone_source",
		"past_due_started_at", "plaid_cleanup_pending_at",
	}).AddRow(
		userID, clerkUserID, "test@example.com", true,
		"premium", "trial", nil, nil,
		nil, nil, false,
		nil, nil, "iad", true, false,
		nil, nil,
		now, now, &now,
		&override, nil,
		nil, nil, nil, nil,
		"UTC", "default",
		nil, nil,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, clerk_user_id, email, email_verified, subscription_tier, subscription_status, subscription_started_at, subscription_ends_at, trial_started_at, trial_ends_at, had_trial, paddle_customer_id, paddle_subscription_id, primary_region, is_active, is_deleted, deletion_requested_at, deletion_scheduled_at, created_at, updated_at, last_login_at, manual_tier_override, manual_tier_ends_at, retention_started_at, retention_expires_at, retention_notice_30d_sent_at, retention_notice_7d_sent_at, timezone, timezone_source, past_due_started_at, plaid_cleanup_pending_at FROM users WHERE clerk_user_id_blind_idx = $1 AND is_deleted = FALSE`)).
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
		"retention_started_at", "retention_expires_at", "retention_notice_30d_sent_at", "retention_notice_7d_sent_at",
		"timezone", "timezone_source",
		"past_due_started_at", "plaid_cleanup_pending_at",
	}).AddRow(
		userID, clerkUserID, "test@example.com", true,
		"premium", "active", &now, nil,
		nil, nil, false,
		nil, nil, "iad", true, false,
		nil, nil,
		now, now, &now,
		&override, &overrideEndsAt,
		nil, nil, nil, nil,
		"UTC", "default",
		nil, nil,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, clerk_user_id, email, email_verified, subscription_tier, subscription_status, subscription_started_at, subscription_ends_at, trial_started_at, trial_ends_at, had_trial, paddle_customer_id, paddle_subscription_id, primary_region, is_active, is_deleted, deletion_requested_at, deletion_scheduled_at, created_at, updated_at, last_login_at, manual_tier_override, manual_tier_ends_at, retention_started_at, retention_expires_at, retention_notice_30d_sent_at, retention_notice_7d_sent_at, timezone, timezone_source, past_due_started_at, plaid_cleanup_pending_at FROM users WHERE id = $1 AND is_deleted = FALSE`)).
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
		"retention_started_at", "retention_expires_at", "retention_notice_30d_sent_at", "retention_notice_7d_sent_at",
		"timezone", "timezone_source",
		"past_due_started_at", "plaid_cleanup_pending_at",
	}).AddRow(
		userID, "clerk_123", "test@example.com", true,
		"premium", "active", &now, nil,
		nil, nil, false,
		stringPtr("cus_123"), stringPtr(subscriptionID), "iad",
		true, false,
		nil, nil,
		now, now, &now,
		nil, nil,
		nil, nil, nil, nil,
		"UTC", "default",
		nil, nil,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, clerk_user_id, email, email_verified, subscription_tier, subscription_status, subscription_started_at, subscription_ends_at, trial_started_at, trial_ends_at, had_trial, paddle_customer_id, paddle_subscription_id, primary_region, is_active, is_deleted, deletion_requested_at, deletion_scheduled_at, created_at, updated_at, last_login_at, manual_tier_override, manual_tier_ends_at, retention_started_at, retention_expires_at, retention_notice_30d_sent_at, retention_notice_7d_sent_at, timezone, timezone_source, past_due_started_at, plaid_cleanup_pending_at FROM users WHERE paddle_subscription_id_blind_idx = $1 AND is_deleted = FALSE`)).
		WithArgs(pgxmock.AnyArg()). // blind index (HMAC of subscriptionID)
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

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, clerk_user_id, email, email_verified, subscription_tier, subscription_status, subscription_started_at, subscription_ends_at, trial_started_at, trial_ends_at, had_trial, paddle_customer_id, paddle_subscription_id, primary_region, is_active, is_deleted, deletion_requested_at, deletion_scheduled_at, created_at, updated_at, last_login_at, manual_tier_override, manual_tier_ends_at, retention_started_at, retention_expires_at, retention_notice_30d_sent_at, retention_notice_7d_sent_at, timezone, timezone_source, past_due_started_at, plaid_cleanup_pending_at FROM users WHERE paddle_subscription_id_blind_idx = $1 AND is_deleted = FALSE`)).
		WithArgs(pgxmock.AnyArg()). // blind index (HMAC of subscriptionID)
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

	// status is "active" (not eligible for Plaid-disconnect repair), so plaid_cleanup_pending_at is
	// unconditionally cleared — Issue #1413 round-10. updated_at is NOW() in the SQL itself (a CI
	// clock-skew fix), not a Go-computed query parameter, so there is one fewer bound parameter than
	// before that fix.
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE users SET subscription_tier = $1, subscription_status = $2, subscription_started_at = $3, subscription_ends_at = $4, paddle_subscription_id = $5, paddle_subscription_id_blind_idx = $6, updated_at = NOW(), plaid_cleanup_pending_at = NULL WHERE id = $7 AND is_deleted = FALSE`)).
		WithArgs(tier, status, pgxmock.AnyArg(), (*time.Time)(nil), pgxmock.AnyArg(), pgxmock.AnyArg(), userID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	// Execute
	err := repo.UpdateSubscriptionFromPaddle(ctx, userID, tier, status, &now, (*time.Time)(nil), &subscriptionID)

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUpdateSubscriptionFromPaddle_WithNullSubscriptionID verifies that when subscriptionID is nil
// the paddle_subscription_id and paddle_subscription_id_blind_idx columns are OMITTED from the SET
// clause (preserving existing values) rather than being overwritten with NULL.
// #933: nil = preserve semantics; fixes the cancel-path ID-clearing bug.
func TestUpdateSubscriptionFromPaddle_WithNullSubscriptionID(t *testing.T) {
	mock, repo := setupMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	tier := "premium"
	status := "active"
	now := time.Now()

	// When subscriptionID is nil, the query OMITS the paddle_subscription_id columns entirely
	// (4 bound params, not 6 — no paddle_subscription_id or paddle_subscription_id_blind_idx). status
	// is "active" (not eligible for Plaid-disconnect repair), so plaid_cleanup_pending_at is
	// unconditionally cleared — Issue #1413 round-10. updated_at is NOW() in the SQL itself (a CI
	// clock-skew fix), not a Go-computed query parameter.
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE users SET subscription_tier = $1, subscription_status = $2, subscription_started_at = $3, subscription_ends_at = $4, updated_at = NOW(), plaid_cleanup_pending_at = NULL WHERE id = $5 AND is_deleted = FALSE`)).
		WithArgs(tier, status, pgxmock.AnyArg(), (*time.Time)(nil), userID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	// Execute
	err := repo.UpdateSubscriptionFromPaddle(ctx, userID, tier, status, &now, (*time.Time)(nil), (*string)(nil))

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUpdateSubscriptionFromPaddle_NormalizesPausedStatus verifies that UpdateSubscriptionFromPaddle
// normalizes invalid statuses ("paused", "canceled") to "cancelled" at the repo layer.
// #933: repo-layer normalization defends all callers, not just the webhook worker.
func TestUpdateSubscriptionFromPaddle_NormalizesPausedStatus(t *testing.T) {
	cases := []struct {
		name        string
		inputStatus string
		wantStatus  string
	}{
		{"paused maps to cancelled", "paused", "cancelled"},
		{"canceled single-l maps to cancelled", "canceled", "cancelled"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mock, repo := setupMockDB(t)
			defer mock.Close()

			ctx := context.Background()
			userID := uuid.New().String()

			// Verify normalized status appears in SQL. wantStatus is always "cancelled" here
			// (eligible for Plaid-disconnect repair), so plaid_cleanup_pending_at is started via
			// COALESCE — Issue #1413 round-10 — only when the row's OLD subscription_status wasn't
			// ALREADY repair-eligible (round-12 Fix 4: a genuine transition, not a redundant
			// terminal event for a user already ineligible). Issue #1413 round-13: no longer gated
			// on manual_tier_override at all — see UpdateSubscriptionFromPaddle's doc comment.
			// updated_at is NOW() in the SQL itself (a CI clock-skew fix), not a shared Go-computed
			// query parameter. Issue #1413 Finding 2 (High, Codex review, PR #1443 follow-up) ROUND 23:
			// the query is now a `WITH candidate AS (... FOR NO KEY UPDATE) UPDATE ... FROM candidate`
			// shape (see UpdateSubscriptionFromPaddle's own doc comment) so the lock acquisition
			// happens as its own earlier step, and the COALESCE fallback is clock_timestamp() (real
			// statement-execution wall time), not NOW() — a bare NOW()/clock_timestamp() substitution
			// without this restructuring does not actually fix the lock-wait backdating bug pgxmock
			// cannot exercise at all (see StampMissingPlaidCleanupMarkers's doc comment for the full,
			// empirically-verified mechanism; the real-database proof lives in
			// profile_repository_finding2_lock_contention_integration_test.go, not here).
			mock.ExpectExec(regexp.QuoteMeta(`WITH candidate AS (SELECT id FROM users WHERE id = $5 AND is_deleted = FALSE FOR NO KEY UPDATE) UPDATE users SET subscription_tier = $1, subscription_status = $2, subscription_started_at = $3, subscription_ends_at = $4, updated_at = NOW(), plaid_cleanup_pending_at = CASE WHEN NOT (subscription_status IN ('inactive', 'cancelled')) THEN COALESCE(plaid_cleanup_pending_at, clock_timestamp()) ELSE plaid_cleanup_pending_at END FROM candidate WHERE users.id = candidate.id`)).
				WithArgs("premium", tc.wantStatus, pgxmock.AnyArg(), (*time.Time)(nil), userID).
				WillReturnResult(pgxmock.NewResult("UPDATE", 1))

			err := repo.UpdateSubscriptionFromPaddle(ctx, userID, "premium", tc.inputStatus, nil, nil, nil)

			assert.NoError(t, err)
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// userSelectQueryForTest is the expected SELECT query used in tests for profile lookups
const userSelectQueryForTest = `SELECT id, clerk_user_id, email, email_verified, subscription_tier, subscription_status, subscription_started_at, subscription_ends_at, trial_started_at, trial_ends_at, had_trial, paddle_customer_id, paddle_subscription_id, primary_region, is_active, is_deleted, deletion_requested_at, deletion_scheduled_at, created_at, updated_at, last_login_at, manual_tier_override, manual_tier_ends_at, retention_started_at, retention_expires_at, retention_notice_30d_sent_at, retention_notice_7d_sent_at, timezone, timezone_source, past_due_started_at, plaid_cleanup_pending_at FROM users`

// activePaddleSubsWhereClause is the WHERE clause used by GetUsersWithActivePaddleSubscriptions
// (uses blind index column since paddle_subscription_id is now encrypted)
const activePaddleSubsWhereClause = ` WHERE paddle_subscription_id_blind_idx IS NOT NULL AND subscription_status NOT IN ('inactive', 'cancelled') AND is_deleted = FALSE`

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
		"retention_started_at", "retention_expires_at", "retention_notice_30d_sent_at", "retention_notice_7d_sent_at",
		"timezone", "timezone_source",
		"past_due_started_at", "plaid_cleanup_pending_at",
	}).AddRow(
		userID1, "clerk_1", "user1@example.com", true,
		"premium", "active", &now, nil,
		nil, nil, false,
		stringPtr("cus_1"), &subID1, "iad", true, false,
		nil, nil, now, now, &now, nil, nil, nil, nil, nil, nil, "UTC", "default", nil, nil,
	).AddRow(
		userID2, "clerk_2", "user2@example.com", true,
		"premium", "active", &now, nil,
		nil, nil, false,
		stringPtr("cus_2"), &subID2, "iad", true, false,
		nil, nil, now, now, &now, nil, nil, nil, nil, nil, nil, "UTC", "default", nil, nil,
	)

	mock.ExpectQuery(regexp.QuoteMeta(userSelectQueryForTest + activePaddleSubsWhereClause)).
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

	mock.ExpectQuery(regexp.QuoteMeta(userSelectQueryForTest + activePaddleSubsWhereClause)).
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

	mock.ExpectQuery(regexp.QuoteMeta(userSelectQueryForTest + activePaddleSubsWhereClause)).
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

	mock.ExpectQuery(regexp.QuoteMeta(userSelectQueryForTest + activePaddleSubsWhereClause)).
		WillReturnRows(rows)

	users, err := repo.GetUsersWithActivePaddleSubscriptions(ctx)

	require.NoError(t, err)
	assert.Empty(t, users)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// #1001: GetUserByEmail — dual lookup (blind index OR plaintext) for encryption migration safety

const getUserByEmailQuery = `SELECT id, clerk_user_id, email, email_verified, subscription_tier, subscription_status, subscription_started_at, subscription_ends_at, trial_started_at, trial_ends_at, had_trial, paddle_customer_id, paddle_subscription_id, primary_region, is_active, is_deleted, deletion_requested_at, deletion_scheduled_at, created_at, updated_at, last_login_at, manual_tier_override, manual_tier_ends_at, retention_started_at, retention_expires_at, retention_notice_30d_sent_at, retention_notice_7d_sent_at, timezone, timezone_source, past_due_started_at, plaid_cleanup_pending_at FROM users WHERE (email_blind_idx = $1 OR LOWER(email) = $2) AND is_deleted = FALSE`

// TestGetUserByEmail_FoundByBlindIndex verifies lookup finds an encrypted row via blind index
func TestGetUserByEmail_FoundByBlindIndex(t *testing.T) {
	mock, repo := setupMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	email := "user@example.com"
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
		"retention_started_at", "retention_expires_at", "retention_notice_30d_sent_at", "retention_notice_7d_sent_at",
		"timezone", "timezone_source",
		"past_due_started_at", "plaid_cleanup_pending_at",
	}).AddRow(
		userID, "clerk_abc", "ENC$TENC[user@example.com]", true, // full encrypted form: ENC$ prefix + test-encrypted value
		"premium", "trial", nil, nil,
		nil, nil, true,
		nil, nil, "iad", true, false,
		nil, nil,
		now, now, nil,
		nil, nil,
		nil, nil, nil, nil,
		"UTC", "default",
		nil, nil,
	)

	mock.ExpectQuery(regexp.QuoteMeta(getUserByEmailQuery)).
		WithArgs(pgxmock.AnyArg(), "user@example.com").
		WillReturnRows(rows)

	user, err := repo.GetUserByEmail(ctx, email)

	require.NoError(t, err)
	assert.NotNil(t, user)
	assert.Equal(t, userID, user.ID)
	assert.Equal(t, "user@example.com", user.Email) // DecryptString passthrough
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetUserByEmail_FoundPlaintext verifies lookup finds a pre-encryption plaintext row via LOWER(email)
func TestGetUserByEmail_FoundPlaintext(t *testing.T) {
	mock, repo := setupMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	email := "User@Example.com" // mixed case — should be lowercased
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
		"retention_started_at", "retention_expires_at", "retention_notice_30d_sent_at", "retention_notice_7d_sent_at",
		"timezone", "timezone_source",
		"past_due_started_at", "plaid_cleanup_pending_at",
	}).AddRow(
		userID, "clerk_old", "user@example.com", true, // plaintext email in DB (pre-encryption)
		"premium", "active", nil, nil,
		nil, nil, false,
		nil, nil, "iad", true, false,
		nil, nil,
		now, now, nil,
		nil, nil,
		nil, nil, nil, nil,
		"UTC", "default",
		nil, nil,
	)

	mock.ExpectQuery(regexp.QuoteMeta(getUserByEmailQuery)).
		WithArgs(pgxmock.AnyArg(), "user@example.com"). // normalized to lowercase
		WillReturnRows(rows)

	user, err := repo.GetUserByEmail(ctx, email)

	require.NoError(t, err)
	assert.NotNil(t, user)
	assert.Equal(t, userID, user.ID)
	assert.Equal(t, "user@example.com", user.Email)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetUserByEmail_NotFound verifies ErrUserNotFound is returned when no match
func TestGetUserByEmail_NotFound(t *testing.T) {
	mock, repo := setupMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	email := "nobody@example.com"

	mock.ExpectQuery(regexp.QuoteMeta(getUserByEmailQuery)).
		WithArgs(pgxmock.AnyArg(), "nobody@example.com").
		WillReturnError(pgx.ErrNoRows)

	user, err := repo.GetUserByEmail(ctx, email)

	assert.Nil(t, user)
	assert.ErrorIs(t, err, ErrUserNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestProfileRepository_UpdateUserEmailTx_Success verifies that UpdateUserEmailTx:
//  1. Updates the email columns on the users row (existing behaviour)
//  2. Deletes the old prefix rows for the user
//  3. Inserts new prefix rows for the updated email
func TestProfileRepository_UpdateUserEmailTx_Success(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	mock, repo := setupMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	newEmail := "newemail@example.com"

	mock.ExpectBegin()
	// 1. UPDATE users (existing behaviour — must stay first for lock-ordering invariant)
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE users SET email = $1, email_blind_idx = $2, email_verified = false, updated_at = $3 WHERE id = $4`)).
		WithArgs(
			pgxmock.AnyArg(), // email (encrypted)
			pgxmock.AnyArg(), // email_blind_idx
			pgxmock.AnyArg(), // updated_at
			userID,
		).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	// 2. DELETE old prefix rows
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM user_email_prefix_indices WHERE user_id = $1`)).
		WithArgs(userID).
		WillReturnResult(pgxmock.NewResult("DELETE", 0))
	// 3. INSERT new prefix rows.
	// "newemail@example.com" = 20 runes → 18 prefix indices (lengths 3..20) + 1 user_id arg = 19 args.
	mock.ExpectExec(`INSERT INTO user_email_prefix_indices`).
		WithArgs(
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	tx, err := mock.Begin(ctx)
	require.NoError(t, err)

	err = repo.UpdateUserEmailTx(ctx, tx, userID, newEmail)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestProfileRepository_UpdateUserEmailTx_DuplicateEmail verifies that UpdateUserEmailTx
// returns ErrDuplicateEmail when the new email collides with an existing user's email.
func TestProfileRepository_UpdateUserEmailTx_DuplicateEmail(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	mock, repo := setupMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	existingEmail := "existing@example.com"

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE users SET email = $1, email_blind_idx = $2, email_verified = false, updated_at = $3 WHERE id = $4`)).
		WithArgs(
			pgxmock.AnyArg(), // email (encrypted)
			pgxmock.AnyArg(), // email_blind_idx
			pgxmock.AnyArg(), // updated_at
			userID,
		).
		WillReturnError(errors.New("pq: duplicate key value violates unique constraint \"unique_users_email_blind\""))

	tx, err := mock.Begin(ctx)
	require.NoError(t, err)

	err = repo.UpdateUserEmailTx(ctx, tx, userID, existingEmail)

	assert.ErrorIs(t, err, ErrDuplicateEmail)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestProfileRepository_CreateUserTx_InsertsEmailPrefixRows verifies that CreateUserTx
// inserts prefix blind index rows after the user INSERT, within the same transaction.
func TestProfileRepository_CreateUserTx_InsertsEmailPrefixRows(t *testing.T) {
	mock, repo := setupMockDB(t)
	defer mock.Close()
	ctx := context.Background()

	user := &domain.User{
		ClerkUserID:        "clerk_test_prefix",
		Email:              "prefix@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}

	mock.ExpectBegin()
	// Expect the users INSERT (24 args match the CreateUserTx query)
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO users (`)).
		WithArgs(
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	// Expect the prefix indices INSERT.
	// "prefix@example.com" → 18 runes → 16 indices (3..18) + 1 user_id arg = 17 args total.
	mock.ExpectExec(`INSERT INTO user_email_prefix_indices`).
		WithArgs(
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(),
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	tx, err := mock.Begin(ctx)
	require.NoError(t, err)

	err = repo.CreateUserTx(ctx, tx, user)
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestProfileRepository_CreateUserTx_ShortEmail_NoPrefixInsert verifies that an email
// too short to generate prefix rows (< 3 rune chars) does NOT issue a prefix INSERT.
func TestProfileRepository_CreateUserTx_ShortEmail_NoPrefixInsert(t *testing.T) {
	mock, repo := setupMockDB(t)
	defer mock.Close()
	ctx := context.Background()

	user := &domain.User{
		ClerkUserID:        "clerk_test_short",
		Email:              "ab", // only 2 chars — below minPrefixLen
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO users (`)).
		WithArgs(
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	// No ExpectExec for prefix indices — if the implementation incorrectly inserts, this will FAIL.

	tx, err := mock.Begin(ctx)
	require.NoError(t, err)

	err = repo.CreateUserTx(ctx, tx, user)
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestProfileRepository_CreateUserTx_PrefixInsertError verifies that a database error
// from insertPrefixIndicesTx is propagated by CreateUserTx rather than swallowed.
func TestProfileRepository_CreateUserTx_PrefixInsertError(t *testing.T) {
	mock, repo := setupMockDB(t)
	defer mock.Close()
	ctx := context.Background()

	user := &domain.User{
		ClerkUserID:        "clerk_test_prefix_err",
		Email:              "prefix@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}

	dbErr := errors.New("connection reset by peer")

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO users (`)).
		WithArgs(
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	// "prefix@example.com" → 18 runes → 16 indices (3..18) + 1 user_id arg = 17 args total.
	mock.ExpectExec(`INSERT INTO user_email_prefix_indices`).
		WithArgs(
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(),
		).
		WillReturnError(dbErr)

	tx, err := mock.Begin(ctx)
	require.NoError(t, err)

	err = repo.CreateUserTx(ctx, tx, user)
	require.Error(t, err)
	assert.ErrorIs(t, err, dbErr)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestProfileRepository_UpdateTimezone verifies that UpdateUser persists Timezone and TimezoneSource.
// Covers Issue #972: after UpdateProfile with Timezone="America/Los_Angeles" + TimezoneSource="user",
// the UpdateUser query includes both new fields.
func TestProfileRepository_UpdateTimezone(t *testing.T) {
	mock, repo := setupMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	user := &domain.User{
		ID:             userID,
		ClerkUserID:    "user_tz_test",
		Email:          "tz@example.com",
		EmailVerified:  true,
		PrimaryRegion:  "iad",
		IsActive:       true,
		Timezone:       "America/Los_Angeles",
		TimezoneSource: "user",
	}

	// Expect UPDATE query with timezone=$6, timezone_source=$7, id=$8
	mock.ExpectExec(`UPDATE users SET email = \$1, email_verified = \$2, primary_region = \$3, updated_at = \$4, email_blind_idx = \$5, timezone = \$6, timezone_source = \$7 WHERE id = \$8`).
		WithArgs(
			pgxmock.AnyArg(),      // email (encrypted)
			user.EmailVerified,    // email_verified
			user.PrimaryRegion,    // primary_region
			pgxmock.AnyArg(),      // updated_at
			pgxmock.AnyArg(),      // email_blind_idx
			"America/Los_Angeles", // timezone
			"user",                // timezone_source
			userID,                // id
		).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.UpdateUser(ctx, user)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSetPastDueStartedAt_SetsWhenNull verifies the conditional SET (only when NULL).
func TestSetPastDueStartedAt_SetsWhenNull(t *testing.T) {
	mock, repo := setupMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	ts := time.Now()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE users SET past_due_started_at = $2, updated_at = NOW() WHERE id = $1 AND past_due_started_at IS NULL`)).
		WithArgs(userID, ts).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.SetPastDueStartedAt(ctx, userID, ts)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSetPastDueStartedAt_IdempotentWhenAlreadySet verifies that zero RowsAffected is not an error.
func TestSetPastDueStartedAt_IdempotentWhenAlreadySet(t *testing.T) {
	mock, repo := setupMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	ts := time.Now()

	// past_due_started_at already set → WHERE clause matches nothing → 0 rows affected
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE users SET past_due_started_at = $2, updated_at = NOW() WHERE id = $1 AND past_due_started_at IS NULL`)).
		WithArgs(userID, ts).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := repo.SetPastDueStartedAt(ctx, userID, ts)

	// Must be nil — 0 rows is idempotent, not an error
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSetPastDueStartedAt_PropagatesDBError verifies DB errors are returned.
func TestSetPastDueStartedAt_PropagatesDBError(t *testing.T) {
	mock, repo := setupMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	ts := time.Now()
	dbErr := errors.New("db connection lost")

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE users SET past_due_started_at = $2, updated_at = NOW() WHERE id = $1 AND past_due_started_at IS NULL`)).
		WithArgs(userID, ts).
		WillReturnError(dbErr)

	err := repo.SetPastDueStartedAt(ctx, userID, ts)

	assert.ErrorIs(t, err, dbErr)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestClearPastDueStartedAt_Success verifies the clear sets the column to NULL.
func TestClearPastDueStartedAt_Success(t *testing.T) {
	mock, repo := setupMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE users SET past_due_started_at = NULL, updated_at = NOW() WHERE id = $1`)).
		WithArgs(userID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.ClearPastDueStartedAt(ctx, userID)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestClearPastDueStartedAt_NotFound verifies ErrUserNotFound when no rows affected.
func TestClearPastDueStartedAt_NotFound(t *testing.T) {
	mock, repo := setupMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE users SET past_due_started_at = NULL, updated_at = NOW() WHERE id = $1`)).
		WithArgs(userID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := repo.ClearPastDueStartedAt(ctx, userID)

	assert.ErrorIs(t, err, ErrUserNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestClearPastDueStartedAt_PropagatesDBError verifies DB errors are returned.
func TestClearPastDueStartedAt_PropagatesDBError(t *testing.T) {
	mock, repo := setupMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	dbErr := errors.New("db connection lost")

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE users SET past_due_started_at = NULL, updated_at = NOW() WHERE id = $1`)).
		WithArgs(userID).
		WillReturnError(dbErr)

	err := repo.ClearPastDueStartedAt(ctx, userID)

	assert.ErrorIs(t, err, dbErr)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetUserByClerkID_WithPastDueStartedAt verifies that PastDueStartedAt is correctly
// scanned from the SELECT result when present (column order guard).
func TestGetUserByClerkID_WithPastDueStartedAt(t *testing.T) {
	mock, repo := setupMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	clerkUserID := "user_past_due_test"
	userID := uuid.New().String()
	now := time.Now()
	pastDueTs := now.Add(-48 * time.Hour)

	rows := pgxmock.NewRows([]string{
		"id", "clerk_user_id", "email", "email_verified",
		"subscription_tier", "subscription_status", "subscription_started_at", "subscription_ends_at",
		"trial_started_at", "trial_ends_at", "had_trial",
		"paddle_customer_id", "paddle_subscription_id", "primary_region", "is_active", "is_deleted",
		"deletion_requested_at", "deletion_scheduled_at",
		"created_at", "updated_at", "last_login_at",
		"manual_tier_override", "manual_tier_ends_at",
		"retention_started_at", "retention_expires_at", "retention_notice_30d_sent_at", "retention_notice_7d_sent_at",
		"timezone", "timezone_source",
		"past_due_started_at", "plaid_cleanup_pending_at",
	}).AddRow(
		userID, clerkUserID, "pd@example.com", true,
		"premium", "past_due", nil, nil,
		nil, nil, false,
		nil, nil, "iad", true, false,
		nil, nil,
		now, now, nil,
		nil, nil,
		nil, nil, nil, nil,
		"UTC", "default",
		&pastDueTs, nil,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, clerk_user_id, email, email_verified, subscription_tier, subscription_status, subscription_started_at, subscription_ends_at, trial_started_at, trial_ends_at, had_trial, paddle_customer_id, paddle_subscription_id, primary_region, is_active, is_deleted, deletion_requested_at, deletion_scheduled_at, created_at, updated_at, last_login_at, manual_tier_override, manual_tier_ends_at, retention_started_at, retention_expires_at, retention_notice_30d_sent_at, retention_notice_7d_sent_at, timezone, timezone_source, past_due_started_at, plaid_cleanup_pending_at FROM users WHERE clerk_user_id_blind_idx = $1 AND is_deleted = FALSE`)).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(rows)

	user, err := repo.GetUserByClerkID(ctx, clerkUserID)

	require.NoError(t, err)
	assert.NotNil(t, user)
	assert.Equal(t, "past_due", user.SubscriptionStatus)
	assert.NotNil(t, user.PastDueStartedAt)
	assert.WithinDuration(t, pastDueTs, *user.PastDueStartedAt, time.Second)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// ─── #1413: GetEffectiveSubscriptionStatusByUserID ──────────────────────────
// An admin "comp" sets manual_tier_override/manual_tier_ends_at but leaves the raw
// subscription_status column untouched. Every access gate must read the EFFECTIVE status
// (override-aware) rather than the stale raw column, or a comped user stays locked out.

// TestGetEffectiveSubscriptionStatusByUserID_NoOverride_ReturnsRawStatus verifies that a
// user with no manual override gets back their raw subscription_status unchanged.
func TestGetEffectiveSubscriptionStatusByUserID_NoOverride_ReturnsRawStatus(t *testing.T) {
	mock, repo := setupMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()

	rows := pgxmock.NewRows([]string{"subscription_status", "manual_tier_override", "manual_tier_ends_at"}).
		AddRow("inactive", nil, nil)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT subscription_status, manual_tier_override, manual_tier_ends_at FROM users WHERE id = $1 AND is_deleted = FALSE`)).
		WithArgs(userID).
		WillReturnRows(rows)

	status, err := repo.GetEffectiveSubscriptionStatusByUserID(ctx, userID)

	assert.NoError(t, err)
	assert.Equal(t, "inactive", status)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetEffectiveSubscriptionStatusByUserID_ActiveOverride_ReturnsActive verifies that a
// user with a non-expired manual tier override resolves to "active" even though the raw
// subscription_status row says "inactive" — this is the #1413 regression case: an admin
// comp must actually unlock access, not just relabel the tier.
func TestGetEffectiveSubscriptionStatusByUserID_ActiveOverride_ReturnsActive(t *testing.T) {
	mock, repo := setupMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	futureEnd := time.Now().Add(24 * time.Hour)

	rows := pgxmock.NewRows([]string{"subscription_status", "manual_tier_override", "manual_tier_ends_at"}).
		AddRow("inactive", stringPtr("premium"), &futureEnd)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT subscription_status, manual_tier_override, manual_tier_ends_at FROM users WHERE id = $1 AND is_deleted = FALSE`)).
		WithArgs(userID).
		WillReturnRows(rows)

	status, err := repo.GetEffectiveSubscriptionStatusByUserID(ctx, userID)

	assert.NoError(t, err)
	assert.Equal(t, "active", status)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetEffectiveSubscriptionStatusByUserID_ExpiredOverride_ReturnsRawStatus verifies that
// an expired manual tier override does NOT force the status to active — the raw status is
// returned as-is once the comp period has lapsed.
func TestGetEffectiveSubscriptionStatusByUserID_ExpiredOverride_ReturnsRawStatus(t *testing.T) {
	mock, repo := setupMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	pastEnd := time.Now().Add(-24 * time.Hour)

	rows := pgxmock.NewRows([]string{"subscription_status", "manual_tier_override", "manual_tier_ends_at"}).
		AddRow("cancelled", stringPtr("premium"), &pastEnd)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT subscription_status, manual_tier_override, manual_tier_ends_at FROM users WHERE id = $1 AND is_deleted = FALSE`)).
		WithArgs(userID).
		WillReturnRows(rows)

	status, err := repo.GetEffectiveSubscriptionStatusByUserID(ctx, userID)

	assert.NoError(t, err)
	assert.Equal(t, "cancelled", status)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetEffectiveSubscriptionStatusByUserID_NoRows_ReturnsErrUserNotFound verifies that a
// missing/deleted user surfaces ErrUserNotFound rather than a raw pgx error.
func TestGetEffectiveSubscriptionStatusByUserID_NoRows_ReturnsErrUserNotFound(t *testing.T) {
	mock, repo := setupMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT subscription_status, manual_tier_override, manual_tier_ends_at FROM users WHERE id = $1 AND is_deleted = FALSE`)).
		WithArgs(userID).
		WillReturnError(pgx.ErrNoRows)

	status, err := repo.GetEffectiveSubscriptionStatusByUserID(ctx, userID)

	assert.Error(t, err)
	assert.Equal(t, ErrUserNotFound, err)
	assert.Equal(t, "", status)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// ─── Issue #1413 G2: malformed manual_tier_override must not grant access ──────────────────
//
// manual_tier_override is an unconstrained VARCHAR(50) with no CHECK constraint, so it can hold
// a value that names no configured tier at all — a legacy row, a direct SQL write, or a data
// migration. (#1652 deleted the admin override service path's write-time validation, so nothing
// on the write side checks this any more.) Before this fix,
// GetEffectiveSubscriptionStatusByUserID only checked "non-empty and not expired" and would
// return "active" for ANY such value, which the read-only middleware (clerk.go) treats as full
// write access. The fix validates the override value against an injected tier-name validator on
// this access path ONLY — the retention/deletion queries' noManualOverrideAtAllPredicate (which
// must keep treating any non-empty override, valid or not, as "hands off" — see
// profile_repository_manual_override_retention_integration_test.go) is deliberately untouched.

// fakeOverrideTierValidator returns a validator func accepting only the given tier names,
// standing in for a real *config.TierConfig-backed check without this package needing to import
// config or call config.LoadTierConfig() (a process-wide sync.Once singleton that must never be
// invoked from a test — see CLAUDE.md).
func fakeOverrideTierValidator(validTiers ...string) func(string) bool {
	allowed := make(map[string]bool, len(validTiers))
	for _, t := range validTiers {
		allowed[t] = true
	}
	return func(tier string) bool { return allowed[tier] }
}

// TestGetEffectiveSubscriptionStatusByUserID_MalformedOverride_DoesNotGrantActive is the RED
// test for G2: a user with manual_tier_override = 'not_a_tier' (garbage — not a configured tier
// name) and a raw "inactive" status must NOT resolve to "active". Before the fix, this returned
// "active" for any non-empty, non-expired override string regardless of validity — the exact
// security hole G2 closes.
func TestGetEffectiveSubscriptionStatusByUserID_MalformedOverride_DoesNotGrantActive(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewProfileRepository(mock, newTestFieldEncryptor(t))
	repo.SetOverrideTierValidator(fakeOverrideTierValidator("premium", "household_2", "household_3", "household_4", "household_5"))

	ctx := context.Background()
	userID := uuid.New().String()

	rows := pgxmock.NewRows([]string{"subscription_status", "manual_tier_override", "manual_tier_ends_at"}).
		AddRow("inactive", stringPtr("not_a_tier"), nil)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT subscription_status, manual_tier_override, manual_tier_ends_at FROM users WHERE id = $1 AND is_deleted = FALSE`)).
		WithArgs(userID).
		WillReturnRows(rows)

	status, err := repo.GetEffectiveSubscriptionStatusByUserID(ctx, userID)

	assert.NoError(t, err)
	assert.NotEqual(t, "active", status, "a manual_tier_override naming no configured tier must never grant the active effective status")
	assert.Equal(t, "inactive", status, "an invalid override must fall back to the raw subscription_status, not some other sentinel")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetEffectiveSubscriptionStatusByUserID_ValidOverride_WithValidator_GrantsActive is the
// positive control demanded alongside the RED test above: an over-tight validator that denies
// every comp — even a real, correctly-configured one — must not sail through unnoticed. With a
// validator wired, a genuinely valid override tier (e.g. "household_3") must still resolve to
// "active".
func TestGetEffectiveSubscriptionStatusByUserID_ValidOverride_WithValidator_GrantsActive(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewProfileRepository(mock, newTestFieldEncryptor(t))
	repo.SetOverrideTierValidator(fakeOverrideTierValidator("premium", "household_2", "household_3", "household_4", "household_5"))

	ctx := context.Background()
	userID := uuid.New().String()

	rows := pgxmock.NewRows([]string{"subscription_status", "manual_tier_override", "manual_tier_ends_at"}).
		AddRow("inactive", stringPtr("household_3"), nil)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT subscription_status, manual_tier_override, manual_tier_ends_at FROM users WHERE id = $1 AND is_deleted = FALSE`)).
		WithArgs(userID).
		WillReturnRows(rows)

	status, err := repo.GetEffectiveSubscriptionStatusByUserID(ctx, userID)

	assert.NoError(t, err)
	assert.Equal(t, "active", status, "a validly-configured override tier must still grant active status")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetEffectiveSubscriptionStatusByUserID_MalformedOverride_NoValidatorConfigured_GrantsActive
// states the explicit decision for "what happens if the tier config is unavailable at read
// time": when no validator has been injected at all (SetOverrideTierValidator never called —
// e.g. some future construction path that forgets to wire one), validation is SKIPPED, not
// deny-by-default. main.go's actual wiring loads the tier config once at process startup and
// os.Exit(1)s the whole process if that fails, so in production the validator is either present
// for the entire process lifetime or the process never starts — there is no "config blip" that
// can leave a running server without one. Failing closed here regardless (denying every comp
// whenever a validator merely happens not to be wired) would turn any future construction site
// that forgets to call SetOverrideTierValidator into an instant, silent outage for every
// currently-comped user. This mirrors the nil-safe convention the now-deleted admin write-time
// check used before #1652.
func TestGetEffectiveSubscriptionStatusByUserID_MalformedOverride_NoValidatorConfigured_GrantsActive(t *testing.T) {
	mock, repo := setupMockDB(t) // no SetOverrideTierValidator call — validator left nil
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()

	rows := pgxmock.NewRows([]string{"subscription_status", "manual_tier_override", "manual_tier_ends_at"}).
		AddRow("inactive", stringPtr("not_a_tier"), nil)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT subscription_status, manual_tier_override, manual_tier_ends_at FROM users WHERE id = $1 AND is_deleted = FALSE`)).
		WithArgs(userID).
		WillReturnRows(rows)

	status, err := repo.GetEffectiveSubscriptionStatusByUserID(ctx, userID)

	assert.NoError(t, err)
	assert.Equal(t, "active", status)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// ─── Issue #1453: GetUsersWithMalformedTierOverride — the monitoring query ─────────────────────

// malformedTierOverrideWhereClauseForTest mirrors malformedTierOverrideWhereClause's exact text
// (profile_repository.go), the same convention activePaddleSubsWhereClause above already uses for
// a hand-verified copy of a production WHERE-clause constant.
const malformedTierOverrideWhereClauseForTest = ` WHERE manual_tier_override IS NOT NULL AND manual_tier_override <> '' AND is_deleted = FALSE ORDER BY id`

// TestGetUsersWithMalformedTierOverride_FiltersToOnlyMalformedRows is the RED test for Issue #1453's
// monitoring query: given two candidate rows (both non-empty overrides — the SQL WHERE clause's own
// job), one naming a configured tier and one naming garbage, only the garbage one must come back.
// The SQL layer deliberately does NOT distinguish them (see the method's own doc comment for why —
// no hand-copied tier list in SQL); the Go-side filter against r.overrideTierValidator is what this
// test actually exercises.
func TestGetUsersWithMalformedTierOverride_FiltersToOnlyMalformedRows(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewProfileRepository(mock, newTestFieldEncryptor(t))
	repo.SetOverrideTierValidator(fakeOverrideTierValidator("premium", "household_2", "household_3", "household_4", "household_5"))

	ctx := context.Background()
	validID := uuid.New().String()
	malformedID := uuid.New().String()
	now := time.Now()

	rows := pgxmock.NewRows([]string{
		"id", "clerk_user_id", "email", "email_verified",
		"subscription_tier", "subscription_status", "subscription_started_at", "subscription_ends_at",
		"trial_started_at", "trial_ends_at", "had_trial",
		"paddle_customer_id", "paddle_subscription_id", "primary_region", "is_active", "is_deleted",
		"deletion_requested_at", "deletion_scheduled_at",
		"created_at", "updated_at", "last_login_at",
		"manual_tier_override", "manual_tier_ends_at",
		"retention_started_at", "retention_expires_at", "retention_notice_30d_sent_at", "retention_notice_7d_sent_at",
		"timezone", "timezone_source",
		"past_due_started_at", "plaid_cleanup_pending_at",
	}).AddRow(
		validID, "clerk_valid", "valid@example.com", true,
		"premium", "cancelled", &now, nil,
		nil, nil, false,
		stringPtr("cus_valid"), nil, "iad", true, false,
		nil, nil, now, now, &now, stringPtr("household_2"), nil, nil, nil, nil, nil, "UTC", "default", nil, nil,
	).AddRow(
		malformedID, "clerk_malformed", "malformed@example.com", true,
		"premium", "cancelled", &now, nil,
		nil, nil, false,
		stringPtr("cus_malformed"), nil, "iad", true, false,
		nil, nil, now, now, &now, stringPtr("not_a_tier"), nil, nil, nil, nil, nil, "UTC", "default", nil, nil,
	)

	mock.ExpectQuery(regexp.QuoteMeta(userSelectQueryForTest + malformedTierOverrideWhereClauseForTest)).
		WillReturnRows(rows)

	malformed, err := repo.GetUsersWithMalformedTierOverride(ctx)

	require.NoError(t, err)
	require.Len(t, malformed, 1, "only the row naming no configured tier must be returned")
	assert.Equal(t, malformedID, malformed[0].ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetUsersWithMalformedTierOverride_NoValidatorConfigured_ReturnsNilNotEmpty proves the
// deliberate (nil, nil) contract when no validator has been wired: "unknown whether any rows are
// malformed" must not collapse into the same shape as "checked, found zero" — see the method's own
// doc comment. Mirrors GetEffectiveSubscriptionStatusByUserID's identical nil-validator convention.
func TestGetUsersWithMalformedTierOverride_NoValidatorConfigured_ReturnsNilNotEmpty(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewProfileRepository(mock, newTestFieldEncryptor(t)) // no SetOverrideTierValidator call
	ctx := context.Background()

	malformed, err := repo.GetUsersWithMalformedTierOverride(ctx)

	require.NoError(t, err)
	assert.Nil(t, malformed, "no validator wired means 'unknown', not 'zero found' — must not silently report all-clear")
	assert.NoError(t, mock.ExpectationsWereMet(), "no query should run at all when no validator is configured")
}

// TestGetUsersWithMalformedTierOverride_AllValid_ReturnsEmptySlice proves the positive control: when
// every candidate row names a configured tier, the result is an empty (non-nil) slice, not nil —
// distinguishing "checked, found zero" from the no-validator (nil, nil) case above.
func TestGetUsersWithMalformedTierOverride_AllValid_ReturnsEmptySlice(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewProfileRepository(mock, newTestFieldEncryptor(t))
	repo.SetOverrideTierValidator(fakeOverrideTierValidator("premium", "household_2", "household_3", "household_4", "household_5"))

	ctx := context.Background()
	validID := uuid.New().String()
	now := time.Now()

	rows := pgxmock.NewRows([]string{
		"id", "clerk_user_id", "email", "email_verified",
		"subscription_tier", "subscription_status", "subscription_started_at", "subscription_ends_at",
		"trial_started_at", "trial_ends_at", "had_trial",
		"paddle_customer_id", "paddle_subscription_id", "primary_region", "is_active", "is_deleted",
		"deletion_requested_at", "deletion_scheduled_at",
		"created_at", "updated_at", "last_login_at",
		"manual_tier_override", "manual_tier_ends_at",
		"retention_started_at", "retention_expires_at", "retention_notice_30d_sent_at", "retention_notice_7d_sent_at",
		"timezone", "timezone_source",
		"past_due_started_at", "plaid_cleanup_pending_at",
	}).AddRow(
		validID, "clerk_valid", "valid@example.com", true,
		"premium", "cancelled", &now, nil,
		nil, nil, false,
		stringPtr("cus_valid"), nil, "iad", true, false,
		nil, nil, now, now, &now, stringPtr("household_3"), nil, nil, nil, nil, nil, "UTC", "default", nil, nil,
	)

	mock.ExpectQuery(regexp.QuoteMeta(userSelectQueryForTest + malformedTierOverrideWhereClauseForTest)).
		WillReturnRows(rows)

	malformed, err := repo.GetUsersWithMalformedTierOverride(ctx)

	require.NoError(t, err)
	assert.NotNil(t, malformed, "checked-and-found-zero must be an empty slice, not nil")
	assert.Empty(t, malformed)
	assert.NoError(t, mock.ExpectationsWereMet())
}
