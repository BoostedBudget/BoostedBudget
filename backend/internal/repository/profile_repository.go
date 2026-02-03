package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

var (
	// ErrUserNotFound is returned when a user is not found in the database
	ErrUserNotFound = errors.New("user not found")
	// ErrDuplicateEmail is returned when attempting to create/update a user with an email that already exists
	ErrDuplicateEmail = errors.New("email already exists")
)

// ProfileRepository defines the interface for user profile data access
type ProfileRepository interface {
	// GetUserByClerkID retrieves a user by their Clerk user ID
	GetUserByClerkID(ctx context.Context, clerkUserID string) (*domain.User, error)

	// GetUserByID retrieves a user by their internal UUID
	GetUserByID(ctx context.Context, id string) (*domain.User, error)

	// GetUserByPaddleSubscriptionID retrieves a user by their Paddle subscription ID
	GetUserByPaddleSubscriptionID(ctx context.Context, subscriptionID string) (*domain.User, error)

	// CreateUser creates a new user in the database
	CreateUser(ctx context.Context, user *domain.User) error

	// CreateUserTx creates a new user in the database within a transaction
	CreateUserTx(ctx context.Context, tx pgx.Tx, user *domain.User) error

	// UpdateUser updates an existing user's information
	UpdateUser(ctx context.Context, user *domain.User) error

	// UpdateLastLogin updates the last_login_at timestamp for a user
	UpdateLastLogin(ctx context.Context, clerkUserID string) error

	// Paddle-related methods (Issue #45, #46: Paddle Payment Integration)

	// UpdatePaddleCustomerID links a Paddle customer ID to a user
	UpdatePaddleCustomerID(ctx context.Context, userID, paddleCustomerID string) error

	// UpdateSubscriptionFromPaddle updates subscription details from Paddle webhooks
	UpdateSubscriptionFromPaddle(ctx context.Context, userID, tier, status string, startedAt, endsAt *time.Time, subscriptionID *string) error

	// Trial-related methods (Issue #679)

	// UpdateTrialStatus updates trial-related fields for a user
	UpdateTrialStatus(ctx context.Context, userID string, status string, trialStartedAt, trialEndsAt *time.Time, hadTrial bool) error

	// UpdateSubscriptionStatus updates just the subscription status
	UpdateSubscriptionStatus(ctx context.Context, userID string, status string) error

	// GetUsersWithExpiredTrials returns users with trial status but past trial_ends_at
	GetUsersWithExpiredTrials(ctx context.Context) ([]*domain.User, error)

	// GetUsersWithExpiringTrials returns users whose trial ends within N days
	GetUsersWithExpiringTrials(ctx context.Context, withinDays int) ([]*domain.User, error)

	// Deletion-related methods

	// MarkForDeletion marks a user for deletion with a scheduled date
	MarkForDeletion(ctx context.Context, userID string, scheduledAt time.Time) error

	// CancelDeletion cancels a pending deletion request
	CancelDeletion(ctx context.Context, userID string) error

	// GetUsersScheduledForDeletion returns users whose deletion date has passed
	GetUsersScheduledForDeletion(ctx context.Context) ([]*domain.User, error)

	// DeleteUserCascade performs application-level cascade deletion of all user data
	DeleteUserCascade(ctx context.Context, userID string) error
}

// profileRepository implements ProfileRepository using PostgreSQL
type profileRepository struct {
	db DB
}

// NewProfileRepository creates a new ProfileRepository instance
func NewProfileRepository(db DB) ProfileRepository {
	return &profileRepository{db: db}
}

// GetUserByClerkID retrieves a user by their Clerk user ID
func (r *profileRepository) GetUserByClerkID(ctx context.Context, clerkUserID string) (*domain.User, error) {
	query := `
		SELECT id, clerk_user_id, email, email_verified,
		       subscription_tier, subscription_status, subscription_started_at, subscription_ends_at,
		       trial_started_at, trial_ends_at, had_trial,
		       paddle_customer_id, paddle_subscription_id, primary_region,
		       is_active, is_deleted,
		       deletion_requested_at, deletion_scheduled_at,
		       created_at, updated_at, last_login_at
		FROM users
		WHERE clerk_user_id = $1 AND is_deleted = FALSE
	`

	var user domain.User
	err := r.db.QueryRow(ctx, query, clerkUserID).Scan(
		&user.ID,
		&user.ClerkUserID,
		&user.Email,
		&user.EmailVerified,
		&user.SubscriptionTier,
		&user.SubscriptionStatus,
		&user.SubscriptionStartedAt,
		&user.SubscriptionEndsAt,
		&user.TrialStartedAt,
		&user.TrialEndsAt,
		&user.HadTrial,
		&user.PaddleCustomerID,
		&user.PaddleSubscriptionID,
		&user.PrimaryRegion,
		&user.IsActive,
		&user.IsDeleted,
		&user.DeletionRequestedAt,
		&user.DeletionScheduledAt,
		&user.CreatedAt,
		&user.UpdatedAt,
		&user.LastLoginAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}

	return &user, nil
}

// GetUserByID retrieves a user by their internal UUID
func (r *profileRepository) GetUserByID(ctx context.Context, id string) (*domain.User, error) {
	query := `
		SELECT id, clerk_user_id, email, email_verified,
		       subscription_tier, subscription_status, subscription_started_at, subscription_ends_at,
		       trial_started_at, trial_ends_at, had_trial,
		       paddle_customer_id, paddle_subscription_id, primary_region,
		       is_active, is_deleted,
		       deletion_requested_at, deletion_scheduled_at,
		       created_at, updated_at, last_login_at
		FROM users
		WHERE id = $1 AND is_deleted = FALSE
	`

	var user domain.User
	err := r.db.QueryRow(ctx, query, id).Scan(
		&user.ID,
		&user.ClerkUserID,
		&user.Email,
		&user.EmailVerified,
		&user.SubscriptionTier,
		&user.SubscriptionStatus,
		&user.SubscriptionStartedAt,
		&user.SubscriptionEndsAt,
		&user.TrialStartedAt,
		&user.TrialEndsAt,
		&user.HadTrial,
		&user.PaddleCustomerID,
		&user.PaddleSubscriptionID,
		&user.PrimaryRegion,
		&user.IsActive,
		&user.IsDeleted,
		&user.DeletionRequestedAt,
		&user.DeletionScheduledAt,
		&user.CreatedAt,
		&user.UpdatedAt,
		&user.LastLoginAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}

	return &user, nil
}

// GetUserByPaddleSubscriptionID retrieves a user by their Paddle subscription ID
func (r *profileRepository) GetUserByPaddleSubscriptionID(ctx context.Context, subscriptionID string) (*domain.User, error) {
	query := `
		SELECT id, clerk_user_id, email, email_verified,
		       subscription_tier, subscription_status, subscription_started_at, subscription_ends_at,
		       trial_started_at, trial_ends_at, had_trial,
		       paddle_customer_id, paddle_subscription_id, primary_region,
		       is_active, is_deleted,
		       deletion_requested_at, deletion_scheduled_at,
		       created_at, updated_at, last_login_at
		FROM users
		WHERE paddle_subscription_id = $1 AND is_deleted = FALSE
	`

	var user domain.User
	err := r.db.QueryRow(ctx, query, subscriptionID).Scan(
		&user.ID,
		&user.ClerkUserID,
		&user.Email,
		&user.EmailVerified,
		&user.SubscriptionTier,
		&user.SubscriptionStatus,
		&user.SubscriptionStartedAt,
		&user.SubscriptionEndsAt,
		&user.TrialStartedAt,
		&user.TrialEndsAt,
		&user.HadTrial,
		&user.PaddleCustomerID,
		&user.PaddleSubscriptionID,
		&user.PrimaryRegion,
		&user.IsActive,
		&user.IsDeleted,
		&user.DeletionRequestedAt,
		&user.DeletionScheduledAt,
		&user.CreatedAt,
		&user.UpdatedAt,
		&user.LastLoginAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}

	return &user, nil
}

// CreateUser creates a new user in the database
func (r *profileRepository) CreateUser(ctx context.Context, user *domain.User) error {
	// Generate UUID if not already set
	if user.ID == "" {
		user.ID = uuid.New().String()
	}

	// Set timestamps
	now := time.Now()
	user.CreatedAt = now
	user.UpdatedAt = now

	query := `
		INSERT INTO users (
			id, clerk_user_id, email, email_verified,
			subscription_tier, subscription_status, subscription_started_at, subscription_ends_at,
			trial_started_at, trial_ends_at, had_trial,
			paddle_customer_id, paddle_subscription_id, primary_region, is_active, is_deleted,
			deletion_requested_at, deletion_scheduled_at,
			created_at, updated_at, last_login_at
		) VALUES (
			$1, $2, $3, $4,
			$5, $6, $7, $8,
			$9, $10, $11,
			$12, $13, $14, $15, $16,
			$17, $18,
			$19, $20, $21
		)
	`

	_, err := r.db.Exec(ctx, query,
		user.ID,
		user.ClerkUserID,
		user.Email,
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
		user.CreatedAt,
		user.UpdatedAt,
		user.LastLoginAt,
	)

	if err != nil {
		// Check for unique constraint violations
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
			if strings.Contains(err.Error(), "email") {
				return ErrDuplicateEmail
			}
		}
		return err
	}

	return nil
}

// CreateUserTx creates a new user in the database within a transaction
func (r *profileRepository) CreateUserTx(ctx context.Context, tx pgx.Tx, user *domain.User) error {
	// Generate UUID if not already set
	if user.ID == "" {
		user.ID = uuid.New().String()
	}

	// Set timestamps
	now := time.Now()
	user.CreatedAt = now
	user.UpdatedAt = now

	query := `
		INSERT INTO users (
			id, clerk_user_id, email, email_verified,
			subscription_tier, subscription_status, subscription_started_at, subscription_ends_at,
			trial_started_at, trial_ends_at, had_trial,
			paddle_customer_id, paddle_subscription_id, primary_region, is_active, is_deleted,
			deletion_requested_at, deletion_scheduled_at,
			created_at, updated_at, last_login_at
		) VALUES (
			$1, $2, $3, $4,
			$5, $6, $7, $8,
			$9, $10, $11,
			$12, $13, $14, $15, $16,
			$17, $18,
			$19, $20, $21
		)
	`

	_, err := tx.Exec(ctx, query,
		user.ID,
		user.ClerkUserID,
		user.Email,
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
		user.CreatedAt,
		user.UpdatedAt,
		user.LastLoginAt,
	)

	if err != nil {
		// Check for unique constraint violations
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
			if strings.Contains(err.Error(), "email") {
				return ErrDuplicateEmail
			}
		}
		return err
	}

	return nil
}

// UpdateUser updates an existing user's information
// Only updates allowed fields: email, email_verified, primary_region
// Subscription fields are managed separately through payment webhooks
func (r *profileRepository) UpdateUser(ctx context.Context, user *domain.User) error {
	// Update timestamp
	user.UpdatedAt = time.Now()

	query := `
		UPDATE users
		SET email = $1,
		    email_verified = $2,
		    primary_region = $3,
		    updated_at = $4
		WHERE id = $5
	`

	ct, err := r.db.Exec(ctx, query,
		user.Email,
		user.EmailVerified,
		user.PrimaryRegion,
		user.UpdatedAt,
		user.ID,
	)

	if err != nil {
		// Check for unique constraint violations
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
			if strings.Contains(err.Error(), "email") {
				return ErrDuplicateEmail
			}
		}
		return err
	}

	// Check if any rows were affected
	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	return nil
}

// UpdateLastLogin updates the last_login_at timestamp for a user
func (r *profileRepository) UpdateLastLogin(ctx context.Context, clerkUserID string) error {
	now := time.Now()

	query := `
		UPDATE users
		SET last_login_at = $1,
		    updated_at = $2
		WHERE clerk_user_id = $3
	`

	ct, err := r.db.Exec(ctx, query, now, now, clerkUserID)
	if err != nil {
		return err
	}

	// Check if any rows were affected
	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	return nil
}

// UpdatePaddleCustomerID links a Paddle customer ID to a user
// Issue #45: Paddle Payment Service Implementation
func (r *profileRepository) UpdatePaddleCustomerID(ctx context.Context, userID, paddleCustomerID string) error {
	now := time.Now()

	query := `
		UPDATE users
		SET paddle_customer_id = $1,
		    updated_at = $2
		WHERE id = $3 AND is_deleted = FALSE
	`

	ct, err := r.db.Exec(ctx, query, paddleCustomerID, now, userID)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	return nil
}

// UpdateSubscriptionFromPaddle updates subscription details from Paddle webhooks
// Issue #46: Paddle Webhook Handler Implementation
// Issue #48: Subscription Management UI (added subscriptionID parameter)
// Updates tier, status, billing dates, and subscription ID when subscription changes
func (r *profileRepository) UpdateSubscriptionFromPaddle(
	ctx context.Context,
	userID, tier, status string,
	startedAt, endsAt *time.Time,
	subscriptionID *string,
) error {
	now := time.Now()

	query := `
		UPDATE users
		SET subscription_tier = $1,
		    subscription_status = $2,
		    subscription_started_at = $3,
		    subscription_ends_at = $4,
		    paddle_subscription_id = $5,
		    updated_at = $6
		WHERE id = $7 AND is_deleted = FALSE
	`

	ct, err := r.db.Exec(ctx, query,
		tier,
		status,
		startedAt,
		endsAt,
		subscriptionID,
		now,
		userID,
	)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	return nil
}

// MarkForDeletion marks a user for deletion with a scheduled date
func (r *profileRepository) MarkForDeletion(ctx context.Context, userID string, scheduledAt time.Time) error {
	now := time.Now()

	query := `
		UPDATE users
		SET deletion_requested_at = $1,
		    deletion_scheduled_at = $2,
		    updated_at = $3
		WHERE id = $4 AND is_deleted = FALSE
	`

	ct, err := r.db.Exec(ctx, query, now, scheduledAt, now, userID)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	return nil
}

// CancelDeletion cancels a pending deletion request
func (r *profileRepository) CancelDeletion(ctx context.Context, userID string) error {
	now := time.Now()

	query := `
		UPDATE users
		SET deletion_requested_at = NULL,
		    deletion_scheduled_at = NULL,
		    updated_at = $1
		WHERE id = $2 AND is_deleted = FALSE
	`

	ct, err := r.db.Exec(ctx, query, now, userID)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	return nil
}

// GetUsersScheduledForDeletion returns users whose deletion date has passed
func (r *profileRepository) GetUsersScheduledForDeletion(ctx context.Context) ([]*domain.User, error) {
	now := time.Now()

	query := `
		SELECT id, clerk_user_id, email, email_verified,
		       subscription_tier, subscription_status, subscription_started_at, subscription_ends_at,
		       trial_started_at, trial_ends_at, had_trial,
		       paddle_customer_id, primary_region, is_active, is_deleted,
		       deletion_requested_at, deletion_scheduled_at,
		       created_at, updated_at, last_login_at
		FROM users
		WHERE deletion_scheduled_at IS NOT NULL
		  AND deletion_scheduled_at <= $1
		  AND is_deleted = FALSE
		ORDER BY deletion_scheduled_at ASC
	`

	rows, err := r.db.Query(ctx, query, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*domain.User
	for rows.Next() {
		var user domain.User
		err := rows.Scan(
			&user.ID,
			&user.ClerkUserID,
			&user.Email,
			&user.EmailVerified,
			&user.SubscriptionTier,
			&user.SubscriptionStatus,
			&user.SubscriptionStartedAt,
			&user.SubscriptionEndsAt,
			&user.TrialStartedAt,
			&user.TrialEndsAt,
			&user.HadTrial,
			&user.PaddleCustomerID,
			&user.PrimaryRegion,
			&user.IsActive,
			&user.IsDeleted,
			&user.DeletionRequestedAt,
			&user.DeletionScheduledAt,
			&user.CreatedAt,
			&user.UpdatedAt,
			&user.LastLoginAt,
		)
		if err != nil {
			return nil, err
		}
		users = append(users, &user)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return users, nil
}

// DeleteUserCascade performs application-level cascade deletion of all user data
// This implements the 10-step deletion process to maintain audit trail
func (r *profileRepository) DeleteUserCascade(ctx context.Context, userID string) error {
	// Start a transaction for atomicity
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	now := time.Now()

	// Step 1: Delete all budget rules
	_, err = tx.Exec(ctx, `DELETE FROM rules WHERE user_id = $1`, userID)
	if err != nil {
		return err
	}

	// Step 2: Delete all transactions
	_, err = tx.Exec(ctx, `DELETE FROM transactions WHERE user_id = $1`, userID)
	if err != nil {
		return err
	}

	// Step 3: Delete all linked items (bank connections)
	// NOTE: This does NOT call Plaid /item/remove - that's handled by DeletionService
	// before calling this method to stop Plaid billing
	_, err = tx.Exec(ctx, `DELETE FROM linked_items WHERE user_id = $1`, userID)
	if err != nil {
		return err
	}

	// Step 4: Delete all accounts
	_, err = tx.Exec(ctx, `DELETE FROM accounts WHERE user_id = $1`, userID)
	if err != nil {
		return err
	}

	// Step 5: Delete all recurring patterns
	_, err = tx.Exec(ctx, `DELETE FROM recurring_patterns WHERE user_id = $1`, userID)
	if err != nil {
		return err
	}

	// Step 6: Delete all user preferences
	_, err = tx.Exec(ctx, `DELETE FROM user_preferences WHERE user_id = $1`, userID)
	if err != nil {
		return err
	}

	// Step 7: Delete all notifications
	_, err = tx.Exec(ctx, `DELETE FROM notifications WHERE user_id = $1`, userID)
	if err != nil {
		return err
	}

	// Step 8: Anonymize audit logs (keep for legal/compliance, but remove PII)
	_, err = tx.Exec(ctx, `
		UPDATE audit_logs
		SET metadata = jsonb_set(
			COALESCE(metadata, '{}'::jsonb),
			'{anonymized}',
			'true'::jsonb
		)
		WHERE user_id = $1
	`, userID)
	if err != nil {
		return err
	}

	// Step 9: Delete user sessions
	_, err = tx.Exec(ctx, `DELETE FROM user_sessions WHERE user_id = $1`, userID)
	if err != nil {
		return err
	}

	// Step 10: Mark user as deleted
	_, err = tx.Exec(ctx, `
		UPDATE users
		SET is_deleted = TRUE,
		    is_active = FALSE,
		    email = CONCAT('deleted_', id, '@deleted.local'),
		    clerk_user_id = CONCAT('deleted_', id),
		    paddle_customer_id = NULL,
		    deletion_requested_at = NULL,
		    deletion_scheduled_at = NULL,
		    updated_at = $1
		WHERE id = $2
	`, now, userID)
	if err != nil {
		return err
	}

	// Commit the transaction
	if err = tx.Commit(ctx); err != nil {
		return err
	}

	return nil
}

// UpdateTrialStatus updates trial-related fields for a user
// Issue #679: Trial service and auto-grant logic
func (r *profileRepository) UpdateTrialStatus(ctx context.Context, userID string, status string, trialStartedAt, trialEndsAt *time.Time, hadTrial bool) error {
	now := time.Now()

	query := `
		UPDATE users
		SET subscription_status = $1,
		    subscription_tier = 'premium',
		    trial_started_at = $2,
		    trial_ends_at = $3,
		    had_trial = $4,
		    updated_at = $5
		WHERE id = $6 AND is_deleted = FALSE
	`

	ct, err := r.db.Exec(ctx, query, status, trialStartedAt, trialEndsAt, hadTrial, now, userID)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	return nil
}

// UpdateSubscriptionStatus updates just the subscription status for a user
// Issue #679, #680: Trial expiration
func (r *profileRepository) UpdateSubscriptionStatus(ctx context.Context, userID string, status string) error {
	now := time.Now()

	query := `
		UPDATE users
		SET subscription_status = $1,
		    updated_at = $2
		WHERE id = $3 AND is_deleted = FALSE
	`

	ct, err := r.db.Exec(ctx, query, status, now, userID)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	return nil
}

// GetUsersWithExpiredTrials returns users with trial status but past trial_ends_at
// Issue #680: Trial expiration cron job
func (r *profileRepository) GetUsersWithExpiredTrials(ctx context.Context) ([]*domain.User, error) {
	now := time.Now()

	query := `
		SELECT id, clerk_user_id, email, email_verified,
		       subscription_tier, subscription_status, subscription_started_at, subscription_ends_at,
		       trial_started_at, trial_ends_at, had_trial,
		       paddle_customer_id, primary_region, is_active, is_deleted,
		       deletion_requested_at, deletion_scheduled_at,
		       created_at, updated_at, last_login_at
		FROM users
		WHERE subscription_status = 'trial'
		  AND trial_ends_at IS NOT NULL
		  AND trial_ends_at <= $1
		  AND is_deleted = FALSE
		ORDER BY trial_ends_at ASC
	`

	rows, err := r.db.Query(ctx, query, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*domain.User
	for rows.Next() {
		var user domain.User
		err := rows.Scan(
			&user.ID,
			&user.ClerkUserID,
			&user.Email,
			&user.EmailVerified,
			&user.SubscriptionTier,
			&user.SubscriptionStatus,
			&user.SubscriptionStartedAt,
			&user.SubscriptionEndsAt,
			&user.TrialStartedAt,
			&user.TrialEndsAt,
			&user.HadTrial,
			&user.PaddleCustomerID,
			&user.PrimaryRegion,
			&user.IsActive,
			&user.IsDeleted,
			&user.DeletionRequestedAt,
			&user.DeletionScheduledAt,
			&user.CreatedAt,
			&user.UpdatedAt,
			&user.LastLoginAt,
		)
		if err != nil {
			return nil, err
		}
		users = append(users, &user)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return users, nil
}

// GetUsersWithExpiringTrials returns users whose trial ends within N days
// Issue #680: Trial expiration notification (3 days before)
func (r *profileRepository) GetUsersWithExpiringTrials(ctx context.Context, withinDays int) ([]*domain.User, error) {
	now := time.Now()
	threshold := now.Add(time.Duration(withinDays) * 24 * time.Hour)

	query := `
		SELECT id, clerk_user_id, email, email_verified,
		       subscription_tier, subscription_status, subscription_started_at, subscription_ends_at,
		       trial_started_at, trial_ends_at, had_trial,
		       paddle_customer_id, primary_region, is_active, is_deleted,
		       deletion_requested_at, deletion_scheduled_at,
		       created_at, updated_at, last_login_at
		FROM users
		WHERE subscription_status = 'trial'
		  AND trial_ends_at IS NOT NULL
		  AND trial_ends_at > $1
		  AND trial_ends_at <= $2
		  AND is_deleted = FALSE
		ORDER BY trial_ends_at ASC
	`

	rows, err := r.db.Query(ctx, query, now, threshold)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*domain.User
	for rows.Next() {
		var user domain.User
		err := rows.Scan(
			&user.ID,
			&user.ClerkUserID,
			&user.Email,
			&user.EmailVerified,
			&user.SubscriptionTier,
			&user.SubscriptionStatus,
			&user.SubscriptionStartedAt,
			&user.SubscriptionEndsAt,
			&user.TrialStartedAt,
			&user.TrialEndsAt,
			&user.HadTrial,
			&user.PaddleCustomerID,
			&user.PrimaryRegion,
			&user.IsActive,
			&user.IsDeleted,
			&user.DeletionRequestedAt,
			&user.DeletionScheduledAt,
			&user.CreatedAt,
			&user.UpdatedAt,
			&user.LastLoginAt,
		)
		if err != nil {
			return nil, err
		}
		users = append(users, &user)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return users, nil
}
