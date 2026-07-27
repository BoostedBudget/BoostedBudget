package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

var (
	ErrUserNotFound   = errors.New("user not found")
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

	// GetUserByEmail retrieves a user by email, checking both the blind index (encrypted rows)
	// and plaintext LOWER(email) (pre-encryption rows). Use this as a fallback when the
	// clerk_user_id blind index lookup fails for unmigrated rows (#1001).
	GetUserByEmail(ctx context.Context, email string) (*domain.User, error)

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

	// GetEffectiveSubscriptionStatusByUserID retrieves the user's EFFECTIVE subscription
	// status — "active" when a non-expired manual tier override (admin comp) is in effect,
	// otherwise the raw subscription_status (Issue #1413: an admin comp must actually unlock
	// access, not just relabel the tier while every access gate keeps reading the stale,
	// untouched status). Used by read-only middleware (#930) to avoid a full user fetch on
	// every request.
	GetEffectiveSubscriptionStatusByUserID(ctx context.Context, userID string) (string, error)

	// Retention tracking methods (#930: 6-month read-only before auto-deletion)

	// SetRetentionDates sets the retention_started_at and retention_expires_at for a user.
	SetRetentionDates(ctx context.Context, userID string, startedAt, expiresAt time.Time) error

	// ClearRetentionDates clears all retention tracking fields (called on resubscription).
	ClearRetentionDates(ctx context.Context, userID string) error

	// SetRetentionNotice30dSent marks the 30-day pre-deletion notice as sent.
	SetRetentionNotice30dSent(ctx context.Context, userID string) error

	// SetRetentionNotice7dSent marks the 7-day pre-deletion notice as sent.
	SetRetentionNotice7dSent(ctx context.Context, userID string) error

	// GetUsersNeedingRetention30dNotice returns users whose retention_expires_at is within 30 days
	// and whose 30-day notice has not yet been sent.
	GetUsersNeedingRetention30dNotice(ctx context.Context) ([]*domain.User, error)

	// GetUsersNeedingRetention7dNotice returns users whose retention_expires_at is within 7 days
	// and whose 7-day notice has not yet been sent.
	GetUsersNeedingRetention7dNotice(ctx context.Context) ([]*domain.User, error)

	// GetUsersWithExpiredRetention returns users whose retention_expires_at has passed
	// and who are not already deleted or pending deletion.
	GetUsersWithExpiredRetention(ctx context.Context) ([]*domain.User, error)

	// GetUsersWithExpiredManualTierOverride returns users whose manual_tier_override is set and
	// whose manual_tier_ends_at has strictly passed (Issue #1413 Task 9 — backs the daily job
	// that expires lapsed admin comps).
	GetUsersWithExpiredManualTierOverride(ctx context.Context) ([]*domain.User, error)

	// GetUsersWithExpiredTrials returns users with trial status but past trial_ends_at
	GetUsersWithExpiredTrials(ctx context.Context) ([]*domain.User, error)

	// GetUsersWithExpiringTrials returns users whose trial ends within N days
	GetUsersWithExpiringTrials(ctx context.Context, withinDays int) ([]*domain.User, error)

	// GetUsersWithActivePaddleSubscriptions returns users with a non-null paddle_subscription_id
	// and subscription_status not in ('inactive', 'cancelled')
	GetUsersWithActivePaddleSubscriptions(ctx context.Context) ([]*domain.User, error)

	// Deletion-related methods

	// MarkForDeletion marks a user for deletion with a scheduled date
	MarkForDeletion(ctx context.Context, userID string, scheduledAt time.Time) error

	// CancelDeletion cancels a pending deletion request
	CancelDeletion(ctx context.Context, userID string) error

	// GetUsersScheduledForDeletion returns users whose deletion date has passed
	GetUsersScheduledForDeletion(ctx context.Context) ([]*domain.User, error)

	// DeleteUserCascade performs application-level cascade deletion of all user data
	DeleteUserCascade(ctx context.Context, userID string) error

	// Dunning: past_due tracking methods (Issue #898)

	// SetPastDueStartedAt sets the past_due_started_at timestamp for a user.
	// Only sets the timestamp if it is currently NULL (does not reset on repeated calls).
	SetPastDueStartedAt(ctx context.Context, userID string, t time.Time) error

	// ClearPastDueStartedAt clears the past_due_started_at timestamp for a user (called on resubscription).
	ClearPastDueStartedAt(ctx context.Context, userID string) error

	// GetUsersInPastDue returns users with subscription_status='past_due' and non-nil past_due_started_at.
	// Used by the dunning cron job to find candidates for grace-expiry processing (Issue #898).
	GetUsersInPastDue(ctx context.Context) ([]domain.User, error)

	// CancelIfPastDue atomically flips subscription_status to 'cancelled' only if the user is
	// still 'past_due' AND carries no currently-active manual tier override. Returns (true, nil)
	// if the flip happened, (false, nil) if the user has already recovered (payment arrived
	// between Plaid disconnect and this point) or an admin granted/extended a comp in that same
	// window. Issue #898: CAS guard against recovery race condition. Issue #1413 High finding H6
	// (Codex review round 3): predicate extended to also close the comp race — a prior read-only
	// check of HasActiveManualOverride() cannot, by itself, protect the final status flip.
	CancelIfPastDue(ctx context.Context, userID string) (bool, error)

	// ClaimPlaidDisconnectIfPastDue atomically verifies, at the moment of the claim, that a user
	// is still 'past_due' and carries no currently-active manual tier override, before the day-14
	// pre-emptive Plaid disconnect proceeds. Unlike CancelIfPastDue, this path has no state
	// transition of its own to hang a CAS on, so the claim self-assigns past_due_started_at to
	// its own current value purely to obtain the same atomic, row-level check via a single
	// UPDATE ... WHERE statement, without adding a new column. Returns (true, nil) if the claim
	// succeeded (safe to disconnect), (false, nil) if the user has recovered or been comped since
	// the caller's last read (Issue #1413 High finding H6, Codex review round 3).
	ClaimPlaidDisconnectIfPastDue(ctx context.Context, userID string) (bool, error)

	// ExpireTrialIfNoOverride atomically flips subscription_status to 'inactive' only if the
	// user is still 'trial' AND carries no currently-active manual tier override. Returns
	// (true, nil) if the flip happened, (false, nil) if an admin granted/extended a comp between
	// the caller's earlier HasActiveManualOverride() read and this call, or the user's status has
	// otherwise already moved on (Issue #1413 High finding H5, Codex review round 3 — the earlier
	// read-only check cannot, by itself, protect the write against a comp granted in that window).
	ExpireTrialIfNoOverride(ctx context.Context, userID string) (bool, error)

	// UpdateUserEmailTx updates a user's email, blind index, and resets email_verified=false
	// within an existing transaction. Returns ErrDuplicateEmail if the email is already in use.
	UpdateUserEmailTx(ctx context.Context, tx pgx.Tx, userID, plainEmail string) error
}

// profileRepository implements ProfileRepository using PostgreSQL
type profileRepository struct {
	db  DB
	enc *appcrypto.FieldEncryptor
}

// NewProfileRepository creates a new ProfileRepository instance
func NewProfileRepository(db DB, enc *appcrypto.FieldEncryptor) ProfileRepository {
	return &profileRepository{db: db, enc: enc}
}

const userSelectQuery = `
	SELECT id, clerk_user_id, email, email_verified,
	       subscription_tier, subscription_status, subscription_started_at, subscription_ends_at,
	       trial_started_at, trial_ends_at, had_trial,
	       paddle_customer_id, paddle_subscription_id, primary_region,
	       is_active, is_deleted,
	       deletion_requested_at, deletion_scheduled_at,
	       created_at, updated_at, last_login_at,
	       manual_tier_override, manual_tier_ends_at,
	       retention_started_at, retention_expires_at, retention_notice_30d_sent_at, retention_notice_7d_sent_at,
	       timezone, timezone_source,
	       past_due_started_at
	FROM users`

// queryUser is a helper that scans a user from a query row
func (r *profileRepository) queryUser(ctx context.Context, query string, args ...interface{}) (*domain.User, error) {
	var user domain.User
	err := r.db.QueryRow(ctx, query, args...).Scan(
		&user.ID, &user.ClerkUserID, &user.Email, &user.EmailVerified,
		&user.SubscriptionTier, &user.SubscriptionStatus, &user.SubscriptionStartedAt, &user.SubscriptionEndsAt,
		&user.TrialStartedAt, &user.TrialEndsAt, &user.HadTrial,
		&user.PaddleCustomerID, &user.PaddleSubscriptionID, &user.PrimaryRegion,
		&user.IsActive, &user.IsDeleted,
		&user.DeletionRequestedAt, &user.DeletionScheduledAt,
		&user.CreatedAt, &user.UpdatedAt, &user.LastLoginAt,
		&user.ManualTierOverride, &user.ManualTierEndsAt,
		&user.RetentionStartedAt, &user.RetentionExpiresAt, &user.RetentionNotice30dSentAt, &user.RetentionNotice7dSentAt,
		&user.Timezone, &user.TimezoneSource,
		&user.PastDueStartedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}

	decryptedClerkUserID, err := r.enc.DecryptString(user.ClerkUserID)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt clerk_user_id: %w", err)
	}
	user.ClerkUserID = decryptedClerkUserID

	decryptedEmail, err := r.enc.DecryptString(user.Email)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt email: %w", err)
	}
	user.Email = decryptedEmail

	decryptedPaddleCustomerID, err := r.enc.DecryptNullableString(user.PaddleCustomerID)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt paddle_customer_id: %w", err)
	}
	user.PaddleCustomerID = decryptedPaddleCustomerID

	decryptedPaddleSubscriptionID, err := r.enc.DecryptNullableString(user.PaddleSubscriptionID)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt paddle_subscription_id: %w", err)
	}
	user.PaddleSubscriptionID = decryptedPaddleSubscriptionID

	return &user, nil
}

// queryUsers is a helper that scans multiple users from a query
func (r *profileRepository) queryUsers(ctx context.Context, query string, args ...interface{}) ([]*domain.User, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*domain.User
	for rows.Next() {
		var user domain.User
		if err := rows.Scan(
			&user.ID, &user.ClerkUserID, &user.Email, &user.EmailVerified,
			&user.SubscriptionTier, &user.SubscriptionStatus, &user.SubscriptionStartedAt, &user.SubscriptionEndsAt,
			&user.TrialStartedAt, &user.TrialEndsAt, &user.HadTrial,
			&user.PaddleCustomerID, &user.PaddleSubscriptionID, &user.PrimaryRegion,
			&user.IsActive, &user.IsDeleted,
			&user.DeletionRequestedAt, &user.DeletionScheduledAt,
			&user.CreatedAt, &user.UpdatedAt, &user.LastLoginAt,
			&user.ManualTierOverride, &user.ManualTierEndsAt,
			&user.RetentionStartedAt, &user.RetentionExpiresAt, &user.RetentionNotice30dSentAt, &user.RetentionNotice7dSentAt,
			&user.Timezone, &user.TimezoneSource,
			&user.PastDueStartedAt,
		); err != nil {
			return nil, err
		}

		decryptedClerkUserID, err := r.enc.DecryptString(user.ClerkUserID)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt clerk_user_id: %w", err)
		}
		user.ClerkUserID = decryptedClerkUserID

		decryptedEmail, err := r.enc.DecryptString(user.Email)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt email: %w", err)
		}
		user.Email = decryptedEmail

		decryptedPaddleCustomerID, err := r.enc.DecryptNullableString(user.PaddleCustomerID)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt paddle_customer_id: %w", err)
		}
		user.PaddleCustomerID = decryptedPaddleCustomerID

		decryptedPaddleSubscriptionID, err := r.enc.DecryptNullableString(user.PaddleSubscriptionID)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt paddle_subscription_id: %w", err)
		}
		user.PaddleSubscriptionID = decryptedPaddleSubscriptionID

		users = append(users, &user)
	}
	return users, rows.Err()
}

// GetUserByClerkID retrieves a user by their Clerk user ID
func (r *profileRepository) GetUserByClerkID(ctx context.Context, clerkUserID string) (*domain.User, error) {
	blindIdx := r.enc.BlindIndex(clerkUserID)
	return r.queryUser(ctx, userSelectQuery+" WHERE clerk_user_id_blind_idx = $1 AND is_deleted = FALSE", blindIdx)
}

// GetUserByID retrieves a user by their internal UUID
func (r *profileRepository) GetUserByID(ctx context.Context, id string) (*domain.User, error) {
	return r.queryUser(ctx, userSelectQuery+" WHERE id = $1 AND is_deleted = FALSE", id)
}

// GetUserByPaddleSubscriptionID retrieves a user by their Paddle subscription ID
func (r *profileRepository) GetUserByPaddleSubscriptionID(ctx context.Context, subscriptionID string) (*domain.User, error) {
	blindIdx := r.enc.BlindIndex(subscriptionID)
	return r.queryUser(ctx, userSelectQuery+" WHERE paddle_subscription_id_blind_idx = $1 AND is_deleted = FALSE", blindIdx)
}

// GetUserByEmail retrieves a user by email using a dual lookup:
// 1. By blind index (for encrypted rows post-migration)
// 2. By LOWER(email) plaintext (for unmigrated rows from before encryption)
// This prevents duplicate accounts when encrypt-migrate hasn't been run yet (#1001).
func (r *profileRepository) GetUserByEmail(ctx context.Context, email string) (*domain.User, error) {
	normalized := appcrypto.NormalizeEmailForBlindIndex(email)
	return r.queryUser(ctx,
		userSelectQuery+" WHERE (email_blind_idx = $1 OR LOWER(email) = $2) AND is_deleted = FALSE",
		r.enc.BlindIndex(normalized), normalized)
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

	encryptedEmail, err := r.enc.EncryptString(user.Email)
	if err != nil {
		return fmt.Errorf("failed to encrypt email: %w", err)
	}
	emailBlindIdx := r.enc.BlindIndex(appcrypto.NormalizeEmailForBlindIndex(user.Email))

	encryptedClerkUserID, err := r.enc.EncryptString(user.ClerkUserID)
	if err != nil {
		return fmt.Errorf("failed to encrypt clerk_user_id: %w", err)
	}
	clerkUserIDBlindIdx := r.enc.BlindIndex(user.ClerkUserID)

	encryptedPaddleCustomerID, err := r.enc.EncryptNullableString(user.PaddleCustomerID)
	if err != nil {
		return fmt.Errorf("failed to encrypt paddle_customer_id: %w", err)
	}
	encryptedPaddleSubscriptionID, err := r.enc.EncryptNullableString(user.PaddleSubscriptionID)
	if err != nil {
		return fmt.Errorf("failed to encrypt paddle_subscription_id: %w", err)
	}
	paddleSubscriptionIDBlindIdx := r.enc.BlindIndexNullable(user.PaddleSubscriptionID)

	query := `
		INSERT INTO users (
			id, clerk_user_id, email, email_verified,
			subscription_tier, subscription_status, subscription_started_at, subscription_ends_at,
			trial_started_at, trial_ends_at, had_trial,
			paddle_customer_id, paddle_subscription_id, primary_region, is_active, is_deleted,
			deletion_requested_at, deletion_scheduled_at,
			created_at, updated_at, last_login_at,
			email_blind_idx, clerk_user_id_blind_idx, paddle_subscription_id_blind_idx
		) VALUES (
			$1, $2, $3, $4,
			$5, $6, $7, $8,
			$9, $10, $11,
			$12, $13, $14, $15, $16,
			$17, $18,
			$19, $20, $21,
			$22, $23, $24
		)
	`

	_, err = r.db.Exec(ctx, query,
		user.ID,
		encryptedClerkUserID,
		encryptedEmail,
		user.EmailVerified,
		user.SubscriptionTier,
		user.SubscriptionStatus,
		user.SubscriptionStartedAt,
		user.SubscriptionEndsAt,
		user.TrialStartedAt,
		user.TrialEndsAt,
		user.HadTrial,
		encryptedPaddleCustomerID,
		encryptedPaddleSubscriptionID,
		user.PrimaryRegion,
		user.IsActive,
		user.IsDeleted,
		user.DeletionRequestedAt,
		user.DeletionScheduledAt,
		user.CreatedAt,
		user.UpdatedAt,
		user.LastLoginAt,
		emailBlindIdx,
		clerkUserIDBlindIdx,
		paddleSubscriptionIDBlindIdx,
	)

	return r.checkDuplicateError(err)
}

// checkDuplicateError maps database constraint errors to domain errors
func (r *profileRepository) checkDuplicateError(err error) error {
	if err == nil {
		return nil
	}
	if (strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint")) &&
		(strings.Contains(err.Error(), "unique_users_email_blind") || strings.Contains(err.Error(), "email")) {
		return ErrDuplicateEmail
	}
	return err
}

// insertPrefixIndicesTx inserts prefix blind indices for a user in a single multi-row statement.
// ON CONFLICT DO NOTHING ensures idempotency — safe to call even if rows already exist.
// Returns immediately (no-op) when blindIndices is empty.
func (r *profileRepository) insertPrefixIndicesTx(
	ctx context.Context, tx pgx.Tx, userID string, blindIndices []string,
) error {
	if len(blindIndices) == 0 {
		return nil
	}
	// Single multi-row INSERT avoids per-row round trips and pgx.SendBatch footguns.
	// Parameterised as: INSERT INTO ... VALUES ($1, $2), ($1, $3), ... ON CONFLICT DO NOTHING
	// $1 is reused for user_id across all rows; $2..$N are the prefix blind indices.
	args := make([]any, 0, 1+len(blindIndices))
	args = append(args, userID)
	placeholders := make([]string, 0, len(blindIndices))
	for i, idx := range blindIndices {
		args = append(args, idx)
		placeholders = append(placeholders, fmt.Sprintf("($1, $%d)", i+2))
	}
	query := fmt.Sprintf(
		"INSERT INTO user_email_prefix_indices (user_id, prefix_blind_idx) VALUES %s ON CONFLICT DO NOTHING",
		strings.Join(placeholders, ", "),
	)
	_, err := tx.Exec(ctx, query, args...)
	return err
}

// CreateUserTx creates a new user in the database within a transaction
func (r *profileRepository) CreateUserTx(ctx context.Context, tx pgx.Tx, user *domain.User) error {
	if user.ID == "" {
		user.ID = uuid.New().String()
	}

	now := time.Now()
	user.CreatedAt = now
	user.UpdatedAt = now

	encryptedEmail, err := r.enc.EncryptString(user.Email)
	if err != nil {
		return fmt.Errorf("failed to encrypt email: %w", err)
	}
	emailBlindIdx := r.enc.BlindIndex(appcrypto.NormalizeEmailForBlindIndex(user.Email))

	encryptedClerkUserID, err := r.enc.EncryptString(user.ClerkUserID)
	if err != nil {
		return fmt.Errorf("failed to encrypt clerk_user_id: %w", err)
	}
	clerkUserIDBlindIdx := r.enc.BlindIndex(user.ClerkUserID)

	encryptedPaddleCustomerID, err := r.enc.EncryptNullableString(user.PaddleCustomerID)
	if err != nil {
		return fmt.Errorf("failed to encrypt paddle_customer_id: %w", err)
	}
	encryptedPaddleSubscriptionID, err := r.enc.EncryptNullableString(user.PaddleSubscriptionID)
	if err != nil {
		return fmt.Errorf("failed to encrypt paddle_subscription_id: %w", err)
	}
	paddleSubscriptionIDBlindIdx := r.enc.BlindIndexNullable(user.PaddleSubscriptionID)

	query := `
		INSERT INTO users (
			id, clerk_user_id, email, email_verified,
			subscription_tier, subscription_status, subscription_started_at, subscription_ends_at,
			trial_started_at, trial_ends_at, had_trial,
			paddle_customer_id, paddle_subscription_id, primary_region, is_active, is_deleted,
			deletion_requested_at, deletion_scheduled_at,
			created_at, updated_at, last_login_at,
			email_blind_idx, clerk_user_id_blind_idx, paddle_subscription_id_blind_idx
		) VALUES (
			$1, $2, $3, $4,
			$5, $6, $7, $8,
			$9, $10, $11,
			$12, $13, $14, $15, $16,
			$17, $18,
			$19, $20, $21,
			$22, $23, $24
		)
	`

	_, err = tx.Exec(ctx, query,
		user.ID, encryptedClerkUserID, encryptedEmail, user.EmailVerified,
		user.SubscriptionTier, user.SubscriptionStatus, user.SubscriptionStartedAt, user.SubscriptionEndsAt,
		user.TrialStartedAt, user.TrialEndsAt, user.HadTrial,
		encryptedPaddleCustomerID, encryptedPaddleSubscriptionID, user.PrimaryRegion, user.IsActive, user.IsDeleted,
		user.DeletionRequestedAt, user.DeletionScheduledAt,
		user.CreatedAt, user.UpdatedAt, user.LastLoginAt,
		emailBlindIdx, clerkUserIDBlindIdx, paddleSubscriptionIDBlindIdx,
	)
	if err := r.checkDuplicateError(err); err != nil {
		return err
	}

	// Insert prefix blind indices so admin starts-with search works (#1178)
	indices := r.enc.EmailPrefixBlindIndices(user.Email)
	return r.insertPrefixIndicesTx(ctx, tx, user.ID, indices)
}

// UpdateUser updates an existing user's information
// Only updates allowed fields: email, email_verified, primary_region, timezone, timezone_source
// Subscription fields are managed separately through payment webhooks
func (r *profileRepository) UpdateUser(ctx context.Context, user *domain.User) error {
	user.UpdatedAt = time.Now()

	encryptedEmail, err := r.enc.EncryptString(user.Email)
	if err != nil {
		return fmt.Errorf("failed to encrypt email: %w", err)
	}
	emailBlindIdx := r.enc.BlindIndex(appcrypto.NormalizeEmailForBlindIndex(user.Email))

	query := `
		UPDATE users
		SET email = $1,
		    email_verified = $2,
		    primary_region = $3,
		    updated_at = $4,
		    email_blind_idx = $5,
		    timezone = $6,
		    timezone_source = $7
		WHERE id = $8
	`

	ct, err := r.db.Exec(ctx, query,
		encryptedEmail, user.EmailVerified, user.PrimaryRegion, user.UpdatedAt, emailBlindIdx,
		user.Timezone, user.TimezoneSource, user.ID,
	)

	if err != nil {
		return r.checkDuplicateError(err)
	}

	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

// UpdateUserEmailTx updates a user's email, blind index, and resets email_verified=false
// within an existing transaction. Returns ErrDuplicateEmail if the email is already in use.
func (r *profileRepository) UpdateUserEmailTx(ctx context.Context, tx pgx.Tx, userID, plainEmail string) error {
	encryptedEmail, err := r.enc.EncryptString(plainEmail)
	if err != nil {
		return fmt.Errorf("failed to encrypt email: %w", err)
	}
	emailBlindIdx := r.enc.BlindIndex(appcrypto.NormalizeEmailForBlindIndex(plainEmail))
	now := time.Now()

	query := `
		UPDATE users
		SET email = $1,
		    email_blind_idx = $2,
		    email_verified = false,
		    updated_at = $3
		WHERE id = $4
	`
	ct, err := tx.Exec(ctx, query, encryptedEmail, emailBlindIdx, now, userID)
	if err != nil {
		return r.checkDuplicateError(err)
	}
	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	// Delete stale prefix rows, then insert rows for the new email.
	// CRITICAL: This code appends AFTER the UPDATE users above, maintaining the
	// lock-ordering invariant (users row locked first → prefix rows second),
	// which prevents deadlocks with the startup backfill.
	if _, err := tx.Exec(ctx,
		"DELETE FROM user_email_prefix_indices WHERE user_id = $1", userID); err != nil {
		return fmt.Errorf("delete old prefix indices: %w", err)
	}
	indices := r.enc.EmailPrefixBlindIndices(plainEmail)
	return r.insertPrefixIndicesTx(ctx, tx, userID, indices)
}

// UpdateLastLogin updates the last_login_at timestamp for a user
func (r *profileRepository) UpdateLastLogin(ctx context.Context, clerkUserID string) error {
	now := time.Now()

	blindIdx := r.enc.BlindIndex(clerkUserID)

	query := `
		UPDATE users
		SET last_login_at = $1,
		    updated_at = $2
		WHERE clerk_user_id_blind_idx = $3
	`

	ct, err := r.db.Exec(ctx, query, now, now, blindIdx)
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

	encryptedPaddleCustomerID, err := r.enc.EncryptString(paddleCustomerID)
	if err != nil {
		return fmt.Errorf("failed to encrypt paddle_customer_id: %w", err)
	}

	query := `
		UPDATE users
		SET paddle_customer_id = $1,
		    updated_at = $2
		WHERE id = $3 AND is_deleted = FALSE
	`

	ct, err := r.db.Exec(ctx, query, encryptedPaddleCustomerID, now, userID)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	return nil
}

// UpdateSubscriptionFromPaddle updates subscription details from Paddle webhooks.
// Applies status normalization (#933) as a defense-in-depth backstop before DB write.
// When subscriptionID is nil, preserves existing values (not included in SET clause).
func (r *profileRepository) UpdateSubscriptionFromPaddle(
	ctx context.Context,
	userID, tier, status string,
	startedAt, endsAt *time.Time,
	subscriptionID *string,
) error {
	// #933: Normalize status at repo layer as defense-in-depth backstop.
	status = domain.NormalizeSubscriptionStatus(status)

	now := time.Now()

	var query string
	var args []interface{}

	if subscriptionID != nil {
		// Overwrite both paddle_subscription_id columns.
		enc, err := r.enc.EncryptString(*subscriptionID)
		if err != nil {
			return fmt.Errorf("failed to encrypt paddle_subscription_id: %w", err)
		}
		blind := r.enc.BlindIndex(*subscriptionID)
		query = `UPDATE users SET subscription_tier = $1, subscription_status = $2, subscription_started_at = $3, subscription_ends_at = $4, paddle_subscription_id = $5, paddle_subscription_id_blind_idx = $6, updated_at = $7 WHERE id = $8 AND is_deleted = FALSE`
		args = []interface{}{tier, status, startedAt, endsAt, enc, blind, now, userID}
	} else {
		// Preserve existing paddle_subscription_id values (omit from SET clause).
		query = `UPDATE users SET subscription_tier = $1, subscription_status = $2, subscription_started_at = $3, subscription_ends_at = $4, updated_at = $5 WHERE id = $6 AND is_deleted = FALSE`
		args = []interface{}{tier, status, startedAt, endsAt, now, userID}
	}

	ct, err := r.db.Exec(ctx, query, args...)
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
	query := userSelectQuery + `
		WHERE deletion_scheduled_at IS NOT NULL
		  AND deletion_scheduled_at <= $1
		  AND is_deleted = FALSE
		ORDER BY deletion_scheduled_at ASC
	`
	return r.queryUsers(ctx, query, time.Now())
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

	// Step 1: Delete all boosts
	_, err = tx.Exec(ctx, `DELETE FROM boosts WHERE user_id = $1`, userID)
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

	// Step 10: Mark user as deleted — encrypt the tombstone values and clear blind indexes
	deletedEmail := fmt.Sprintf("deleted_%s@deleted.local", userID)
	deletedClerkUserID := fmt.Sprintf("deleted_%s", userID)

	encryptedDeletedEmail, err := r.enc.EncryptString(deletedEmail)
	if err != nil {
		return fmt.Errorf("failed to encrypt deleted email: %w", err)
	}

	encryptedDeletedClerkUserID, err := r.enc.EncryptString(deletedClerkUserID)
	if err != nil {
		return fmt.Errorf("failed to encrypt deleted clerk_user_id: %w", err)
	}

	_, err = tx.Exec(ctx, `
		UPDATE users
		SET is_deleted = TRUE,
		    is_active = FALSE,
		    email = $1,
		    clerk_user_id = $2,
		    paddle_customer_id = NULL,
		    paddle_subscription_id = NULL,
		    email_blind_idx = NULL,
		    clerk_user_id_blind_idx = NULL,
		    paddle_subscription_id_blind_idx = NULL,
		    deletion_requested_at = NULL,
		    deletion_scheduled_at = NULL,
		    updated_at = $3
		WHERE id = $4
	`, encryptedDeletedEmail, encryptedDeletedClerkUserID, now, userID)
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

// GetEffectiveSubscriptionStatusByUserID retrieves the EFFECTIVE subscription status for a
// user — "active" when a non-expired manual tier override (admin comp) is in effect,
// otherwise the raw subscription_status (Issue #1413). Used by the read-only middleware
// (#930) for efficient per-request status checks.
//
// The override predicate is computed in Go via domain.User.ComputeEffectiveStatus rather
// than in SQL, so this row-level read shares exactly one definition of "is the override
// active" with every other caller of ComputeEffectiveStatus/ComputeEffectiveTier.
//
// NOTE: this rename (from GetSubscriptionStatusByUserID) is deliberate, not cosmetic — the
// old name described a raw column read; this method returns a computed value. Keeping the
// old name would risk a future "simplification" silently reverting to the raw column.
func (r *profileRepository) GetEffectiveSubscriptionStatusByUserID(ctx context.Context, userID string) (string, error) {
	var u domain.User
	err := r.db.QueryRow(ctx,
		`SELECT subscription_status, manual_tier_override, manual_tier_ends_at FROM users WHERE id = $1 AND is_deleted = FALSE`,
		userID,
	).Scan(&u.SubscriptionStatus, &u.ManualTierOverride, &u.ManualTierEndsAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrUserNotFound
	}
	if err != nil {
		return "", err
	}
	return u.ComputeEffectiveStatus(), nil
}

// GetUsersWithExpiredTrials returns users with trial status but past trial_ends_at
// Issue #680: Trial expiration cron job
func (r *profileRepository) GetUsersWithExpiredTrials(ctx context.Context) ([]*domain.User, error) {
	query := userSelectQuery + `
		WHERE subscription_status = 'trial'
		  AND trial_ends_at IS NOT NULL
		  AND trial_ends_at <= $1
		  AND is_deleted = FALSE
		ORDER BY trial_ends_at ASC
	`
	return r.queryUsers(ctx, query, time.Now())
}

// GetUsersWithExpiringTrials returns users whose trial ends within N days
// Issue #680: Trial expiration notification (3 days before)
func (r *profileRepository) GetUsersWithExpiringTrials(ctx context.Context, withinDays int) ([]*domain.User, error) {
	now := time.Now()
	threshold := now.Add(time.Duration(withinDays) * 24 * time.Hour)

	query := userSelectQuery + `
		WHERE subscription_status = 'trial'
		  AND trial_ends_at IS NOT NULL
		  AND trial_ends_at > $1
		  AND trial_ends_at <= $2
		  AND is_deleted = FALSE
		ORDER BY trial_ends_at ASC
	`
	return r.queryUsers(ctx, query, now, threshold)
}

// GetUsersWithActivePaddleSubscriptions returns users with active Paddle subscriptions
// Issue #762: Paddle admin tools reconciliation
func (r *profileRepository) GetUsersWithActivePaddleSubscriptions(ctx context.Context) ([]*domain.User, error) {
	query := userSelectQuery + ` WHERE paddle_subscription_id_blind_idx IS NOT NULL AND subscription_status NOT IN ('inactive', 'cancelled') AND is_deleted = FALSE`
	users, err := r.queryUsers(ctx, query)
	if err != nil {
		return nil, err
	}
	if users == nil {
		return []*domain.User{}, nil
	}
	return users, nil
}

// SetRetentionDates sets the retention_started_at and retention_expires_at for a user.
// Called when a user's subscription is cancelled or trial expires (#930).
func (r *profileRepository) SetRetentionDates(ctx context.Context, userID string, startedAt, expiresAt time.Time) error {
	now := time.Now()
	ct, err := r.db.Exec(ctx, `
		UPDATE users
		SET retention_started_at = $1,
		    retention_expires_at = $2,
		    updated_at = $3
		WHERE id = $4 AND is_deleted = FALSE`,
		startedAt, expiresAt, now, userID,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

// ClearRetentionDates clears all retention tracking fields on resubscription (#930).
func (r *profileRepository) ClearRetentionDates(ctx context.Context, userID string) error {
	now := time.Now()
	ct, err := r.db.Exec(ctx, `
		UPDATE users
		SET retention_started_at = NULL,
		    retention_expires_at = NULL,
		    retention_notice_30d_sent_at = NULL,
		    retention_notice_7d_sent_at = NULL,
		    updated_at = $1
		WHERE id = $2 AND is_deleted = FALSE`,
		now, userID,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

// SetRetentionNotice30dSent marks the 30-day pre-deletion notice as sent (#930).
func (r *profileRepository) SetRetentionNotice30dSent(ctx context.Context, userID string) error {
	now := time.Now()
	ct, err := r.db.Exec(ctx, `
		UPDATE users
		SET retention_notice_30d_sent_at = $1,
		    updated_at = $1
		WHERE id = $2 AND is_deleted = FALSE`,
		now, userID,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

// SetRetentionNotice7dSent marks the 7-day pre-deletion notice as sent (#930).
func (r *profileRepository) SetRetentionNotice7dSent(ctx context.Context, userID string) error {
	now := time.Now()
	ct, err := r.db.Exec(ctx, `
		UPDATE users
		SET retention_notice_7d_sent_at = $1,
		    updated_at = $1
		WHERE id = $2 AND is_deleted = FALSE`,
		now, userID,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

// noActiveManualOverridePredicate excludes users with a currently-active admin manual tier
// override ("comp") from set-based retention queries (Issue #1413). A comped user must never
// be warned about, or auto-deleted for, retention expiry.
//
// This is the SQL-side twin of domain.User.HasActiveManualOverride() — that Go predicate
// governs row-level reads, this one governs set-based queries where fetching every candidate
// row just to filter in Go would be wasteful. The two must agree on every case; see
// TestNoActiveManualOverridePredicate_Integration_ParityWithGo (a real-database test) for the
// guard that keeps them from drifting apart.
//
// Boundary note: `manual_tier_ends_at >= NOW()` deliberately matches Go's
// `!ends_at.Before(now)` (i.e. "not yet expired" is true AT the expiry instant, not only
// strictly before it). Using `>` here would disagree with the Go predicate at the exact
// expiry instant.
//
// As of the Issue #1413 C1 fix this predicate had no production caller — the three retention
// queries below use noManualOverrideAtAllPredicate instead (see that constant's comment for why),
// and TestNoActiveManualOverridePredicate_Integration_ParityWithGo /
// TestNoActiveManualOverridePredicate_Integration_ExactExpiryBoundary kept exercising it purely
// so the exact production string wouldn't silently drift from a hand-copied restatement. As of
// the Issue #1413 Codex review round 3 fix (High findings H5/H6), it now has real production
// callers too — CancelIfPastDue, ClaimPlaidDisconnectIfPastDue, and ExpireTrialIfNoOverride all
// splice this constant into a conditional UPDATE's WHERE clause, per the
// claim-then-act pattern: this is the one place the "still live right now" rule (as opposed to
// noManualOverrideAtAllPredicate's "not even a lapsed one") is actually the correct rule to
// enforce, because these are point-in-time transitions guarded against a comp granted in a race
// window, not the retention-notice/hard-delete queries' cross-job-ordering hazard.
const noActiveManualOverridePredicate = `
	  AND NOT (
	      manual_tier_override IS NOT NULL
	      AND manual_tier_override <> ''
	      AND (manual_tier_ends_at IS NULL OR manual_tier_ends_at >= NOW())
	  )
`

// noManualOverrideAtAllPredicate excludes a user with ANY non-empty manual_tier_override —
// live OR already-lapsed — from the retention notice/hard-delete queries (Issue #1413 Critical
// finding C1, Codex review).
//
// Why this must differ from noActiveManualOverridePredicate above: a comp *pauses* the
// retention clock while live, and is re-armed by the separate, independently-scheduled
// manual_tier_override_expiry_check daily job the moment the comp lapses. That job has NO
// ordering guarantee relative to the retention_cleanup_check job that runs these queries. If
// retention selection used the "only exclude while still live" rule
// (noActiveManualOverridePredicate), a user whose comp lapsed moments before the expiry job
// re-arms their clock would be selected here using a stale retention_expires_at from BEFORE the
// comp even started — hard-deleted with no 30-day/7-day warning ever sent, in the race window
// between the two jobs.
//
// Excluding on ANY non-empty override, live or lapsed, closes that window: the worst case is a
// delay of up to one job cycle (until the expiry job clears the column and re-arms a fresh
// window), never a wrongful deletion. See
// TestRetentionQueries_Integration_ExpiredOverride_ExcludedUntilCleared for proof this is a
// delay, not a permanent exemption, and
// TestGetUsersNeedingRetention30dNotice_Integration_ExcludesActiveComp (and its 7d/expired
// siblings) for proof an ordinary never-comped user is unaffected.
//
// Deliberately a SEPARATE predicate from noActiveManualOverridePredicate, which stays exactly as
// it was — it is the SQL twin of domain.User.HasActiveManualOverride() (row-level reads) and a
// parity test guards that pairing. Two predicates, two different jobs, two different correctness
// requirements: one governs "is the comp live right now", the other governs "is it safe to
// notice/delete this account".
const noManualOverrideAtAllPredicate = `
	  AND (manual_tier_override IS NULL OR manual_tier_override = '')
`

// GetUsersNeedingRetention30dNotice returns users whose retention_expires_at is within 30 days
// and who have not yet received a 30-day pre-deletion warning (#930).
func (r *profileRepository) GetUsersNeedingRetention30dNotice(ctx context.Context) ([]*domain.User, error) {
	query := userSelectQuery + `
		WHERE retention_expires_at IS NOT NULL
		  AND retention_expires_at - INTERVAL '30 days' <= NOW()
		  AND retention_expires_at > NOW()
		  AND retention_notice_30d_sent_at IS NULL
		  AND subscription_status IN ('inactive', 'cancelled')
		  AND is_deleted = FALSE
		  AND deletion_requested_at IS NULL
	` + noManualOverrideAtAllPredicate + `
		ORDER BY retention_expires_at ASC
	`
	users, err := r.queryUsers(ctx, query)
	if err != nil {
		return nil, err
	}
	if users == nil {
		return []*domain.User{}, nil
	}
	return users, nil
}

// GetUsersNeedingRetention7dNotice returns users whose retention_expires_at is within 7 days
// and who have not yet received a 7-day pre-deletion warning (#930).
func (r *profileRepository) GetUsersNeedingRetention7dNotice(ctx context.Context) ([]*domain.User, error) {
	query := userSelectQuery + `
		WHERE retention_expires_at IS NOT NULL
		  AND retention_expires_at - INTERVAL '7 days' <= NOW()
		  AND retention_expires_at > NOW()
		  AND retention_notice_7d_sent_at IS NULL
		  AND subscription_status IN ('inactive', 'cancelled')
		  AND is_deleted = FALSE
		  AND deletion_requested_at IS NULL
	` + noManualOverrideAtAllPredicate + `
		ORDER BY retention_expires_at ASC
	`
	users, err := r.queryUsers(ctx, query)
	if err != nil {
		return nil, err
	}
	if users == nil {
		return []*domain.User{}, nil
	}
	return users, nil
}

// GetUsersWithExpiredRetention returns users whose retention_expires_at has passed
// and who are not already deleted or pending deletion (#930).
func (r *profileRepository) GetUsersWithExpiredRetention(ctx context.Context) ([]*domain.User, error) {
	query := userSelectQuery + `
		WHERE retention_expires_at IS NOT NULL
		  AND retention_expires_at <= NOW()
		  AND subscription_status IN ('inactive', 'cancelled')
		  AND is_deleted = FALSE
		  AND deletion_requested_at IS NULL
	` + noManualOverrideAtAllPredicate + `
		ORDER BY retention_expires_at ASC
	`
	users, err := r.queryUsers(ctx, query)
	if err != nil {
		return nil, err
	}
	if users == nil {
		return []*domain.User{}, nil
	}
	return users, nil
}

// expiredManualTierOverridePredicate selects users with a manual tier override that has
// strictly passed (Issue #1413 Task 9 — the daily job that expires lapsed admin comps,
// performing the exact same clear-override/re-arm-retention/audit transition
// AdminUserRepository.RemoveUserTierOverride (Issue #1414 / Task 7) performs for
// admin-initiated removal, rather than duplicating that logic here).
//
// Boundary note: strictly past (`<`, not `<=`) to agree with
// domain.User.HasActiveManualOverride()'s `!ends_at.Before(now)` semantics — an override ending
// exactly now is still ACTIVE per the read path, so this predicate must not select it (see
// TestGetUsersWithExpiredManualTierOverride_Integration_ExactExpiryBoundary).
//
// manual_tier_ends_at IS NOT NULL additionally excludes PERMANENT overrides (no expiry date
// set) — those never lapse on their own and must only ever be cleared by an explicit admin
// removal.
const expiredManualTierOverridePredicate = `
	  manual_tier_override IS NOT NULL
	  AND manual_tier_override <> ''
	  AND manual_tier_ends_at IS NOT NULL
	  AND manual_tier_ends_at < NOW()
	  AND is_deleted = FALSE
`

// GetUsersWithExpiredManualTierOverride returns users whose manual_tier_override is set and
// whose manual_tier_ends_at has strictly passed (Issue #1413 Task 9).
func (r *profileRepository) GetUsersWithExpiredManualTierOverride(ctx context.Context) ([]*domain.User, error) {
	query := userSelectQuery + `
		WHERE ` + expiredManualTierOverridePredicate + `
		ORDER BY manual_tier_ends_at ASC
	`
	users, err := r.queryUsers(ctx, query)
	if err != nil {
		return nil, err
	}
	if users == nil {
		return []*domain.User{}, nil
	}
	return users, nil
}

// SetPastDueStartedAt sets the past_due_started_at timestamp for a user.
// Only sets the timestamp if it is currently NULL (does not reset on repeated calls).
// Issue #898: dunning job grace-period tracking.
func (r *profileRepository) SetPastDueStartedAt(ctx context.Context, userID string, t time.Time) error {
	_, err := r.db.Exec(ctx, `
		UPDATE users SET past_due_started_at = $2, updated_at = NOW()
		WHERE id = $1 AND past_due_started_at IS NULL`,
		userID, t,
	)
	if err != nil {
		return err
	}
	// Either not found or already set — both are acceptable (idempotent).
	return nil
}

// ClearPastDueStartedAt clears the past_due_started_at timestamp for a user.
// Called on resubscription so the grace period resets cleanly.
// Issue #898: dunning job grace-period tracking.
func (r *profileRepository) ClearPastDueStartedAt(ctx context.Context, userID string) error {
	ct, err := r.db.Exec(ctx, `
		UPDATE users SET past_due_started_at = NULL, updated_at = NOW()
		WHERE id = $1`,
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

// GetUsersInPastDue returns users with subscription_status='past_due' and non-nil past_due_started_at.
// Called by the daily dunning cron job to find candidates for grace-expiry or Plaid-disconnect processing.
// Issue #898: dunning job grace-period tracking.
func (r *profileRepository) GetUsersInPastDue(ctx context.Context) ([]domain.User, error) {
	query := userSelectQuery + `
		WHERE subscription_status = 'past_due'
		  AND past_due_started_at IS NOT NULL
		  AND is_deleted = FALSE
		ORDER BY past_due_started_at ASC
	`
	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []domain.User
	for rows.Next() {
		var user domain.User
		if err := rows.Scan(
			&user.ID, &user.ClerkUserID, &user.Email, &user.EmailVerified,
			&user.SubscriptionTier, &user.SubscriptionStatus, &user.SubscriptionStartedAt, &user.SubscriptionEndsAt,
			&user.TrialStartedAt, &user.TrialEndsAt, &user.HadTrial,
			&user.PaddleCustomerID, &user.PaddleSubscriptionID, &user.PrimaryRegion,
			&user.IsActive, &user.IsDeleted,
			&user.DeletionRequestedAt, &user.DeletionScheduledAt,
			&user.CreatedAt, &user.UpdatedAt, &user.LastLoginAt,
			&user.ManualTierOverride, &user.ManualTierEndsAt,
			&user.RetentionStartedAt, &user.RetentionExpiresAt, &user.RetentionNotice30dSentAt, &user.RetentionNotice7dSentAt,
			&user.Timezone, &user.TimezoneSource,
			&user.PastDueStartedAt,
		); err != nil {
			return nil, err
		}

		decryptedClerkUserID, err := r.enc.DecryptString(user.ClerkUserID)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt clerk_user_id: %w", err)
		}
		user.ClerkUserID = decryptedClerkUserID

		decryptedEmail, err := r.enc.DecryptString(user.Email)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt email: %w", err)
		}
		user.Email = decryptedEmail

		decryptedPaddleCustomerID, err := r.enc.DecryptNullableString(user.PaddleCustomerID)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt paddle_customer_id: %w", err)
		}
		user.PaddleCustomerID = decryptedPaddleCustomerID

		decryptedPaddleSubscriptionID, err := r.enc.DecryptNullableString(user.PaddleSubscriptionID)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt paddle_subscription_id: %w", err)
		}
		user.PaddleSubscriptionID = decryptedPaddleSubscriptionID

		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if users == nil {
		return []domain.User{}, nil
	}
	return users, nil
}

// CancelIfPastDue atomically flips subscription_status to 'cancelled' only if the user is still
// 'past_due' and carries no currently-active manual tier override. Returns (true, nil) if the
// flip happened, (false, nil) if the user has already recovered (e.g. payment arrived between
// Plaid disconnect and this call) or an admin granted/extended a comp in that same window.
// Issue #898: CAS guard against recovery race condition. Issue #1413 High finding H6 (Codex
// review round 3): predicate extended with noActiveManualOverridePredicate — a prior read-only
// HasActiveManualOverride() check in the caller cannot, by itself, protect this write, since the
// two are not atomic.
func (r *profileRepository) CancelIfPastDue(ctx context.Context, userID string) (bool, error) {
	ct, err := r.db.Exec(ctx, `
		UPDATE users
		SET subscription_status = 'cancelled', updated_at = NOW()
		WHERE id = $1 AND subscription_status = 'past_due'
	`+noActiveManualOverridePredicate,
		userID)
	if err != nil {
		return false, fmt.Errorf("CancelIfPastDue: %w", err)
	}
	return ct.RowsAffected() > 0, nil
}

// ClaimPlaidDisconnectIfPastDue atomically verifies — at the moment of the claim — that a user is
// still 'past_due' and carries no currently-active manual tier override, before the day-14
// pre-emptive Plaid disconnect (processPlaidDisconnectWindow) proceeds. This path has no
// persisted state transition of its own to hang a CAS on the way CancelIfPastDue's cancellation
// flip does, so the claim SETs past_due_started_at to its own current value — a no-op write that
// still takes a row-level lock and evaluates the WHERE predicate atomically against committed
// state — rather than adding a new column purely to serve as a claim marker. Returns (true, nil)
// if the claim succeeded (safe to disconnect), (false, nil) if the user has recovered or been
// comped since the caller's last read. Issue #1413 High finding H6, Codex review round 3.
func (r *profileRepository) ClaimPlaidDisconnectIfPastDue(ctx context.Context, userID string) (bool, error) {
	ct, err := r.db.Exec(ctx, `
		UPDATE users
		SET past_due_started_at = past_due_started_at, updated_at = NOW()
		WHERE id = $1 AND subscription_status = 'past_due' AND past_due_started_at IS NOT NULL
	`+noActiveManualOverridePredicate,
		userID)
	if err != nil {
		return false, fmt.Errorf("ClaimPlaidDisconnectIfPastDue: %w", err)
	}
	return ct.RowsAffected() > 0, nil
}

// ExpireTrialIfNoOverride atomically flips subscription_status to 'inactive' only if the user is
// still 'trial' and carries no currently-active manual tier override. Returns (true, nil) if the
// flip happened, (false, nil) if an admin granted/extended a comp between the caller's earlier
// HasActiveManualOverride() read and this call, or the user's status has otherwise already moved
// on. Issue #1413 High finding H5, Codex review round 3: follows CancelIfPastDue's shape — the
// predicate that authorises the transition lives in the UPDATE's WHERE clause, evaluated against
// committed state at the moment of the write, not in an earlier, non-atomic read-then-check.
func (r *profileRepository) ExpireTrialIfNoOverride(ctx context.Context, userID string) (bool, error) {
	ct, err := r.db.Exec(ctx, `
		UPDATE users
		SET subscription_status = 'inactive', updated_at = NOW()
		WHERE id = $1 AND subscription_status = 'trial'
	`+noActiveManualOverridePredicate,
		userID)
	if err != nil {
		return false, fmt.Errorf("ExpireTrialIfNoOverride: %w", err)
	}
	return ct.RowsAffected() > 0, nil
}
