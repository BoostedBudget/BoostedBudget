package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// AdminUserRepository defines the interface for admin user data access
type AdminUserRepository interface {
	// ListUsers returns a paginated list of users with counts for admin view
	ListUsers(ctx context.Context, filters domain.AdminUserFilters, limit, offset int) ([]domain.AdminUserListItem, int, error)

	// GetUserDetail returns detailed user information with full statistics
	GetUserDetail(ctx context.Context, userID string) (*domain.AdminUserDetail, error)

	// UpdateUserTier manually overrides a user's subscription tier, with an optional expiration.
	// Returns the target user's clerk_user_id so the caller can sync Clerk metadata.
	UpdateUserTier(ctx context.Context, userID string, tier string, reason string, expiresAt *time.Time) (string, error)
}

// adminUserRepository implements AdminUserRepository using PostgreSQL
type adminUserRepository struct {
	db  DB
	enc *appcrypto.FieldEncryptor
}

// NewAdminUserRepository creates a new AdminUserRepository instance
func NewAdminUserRepository(db DB, enc *appcrypto.FieldEncryptor) AdminUserRepository {
	return &adminUserRepository{db: db, enc: enc}
}

// ListUsers returns a paginated list of users with aggregated counts
// Supports filtering by email search and subscription tier
func (r *adminUserRepository) ListUsers(ctx context.Context, filters domain.AdminUserFilters, limit, offset int) ([]domain.AdminUserListItem, int, error) {
	// Build WHERE clause and args once to avoid duplication
	whereClause, args, argIndex := r.buildFilterClause(filters)

	// Count query
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM users u WHERE u.is_deleted = FALSE%s", whereClause)
	var totalCount int
	err := r.db.QueryRow(ctx, countQuery, args...).Scan(&totalCount)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count users: %w", err)
	}

	// Main query with counts via subqueries
	query := fmt.Sprintf(`
		SELECT u.id, u.email, u.subscription_tier, u.subscription_status,
		       u.manual_tier_override, u.manual_tier_ends_at, u.primary_region,
		       u.created_at, u.last_login_at, u.is_active,
		       COALESCE(la.count, 0) as linked_accounts_count,
		       COALESCE(r.count, 0) as automation_rules_count,
		       COALESCE(ref.count, 0) as referrals_count
		FROM users u
		LEFT JOIN (
			SELECT user_id, COUNT(*) as count
			FROM linked_items
			WHERE status = 'active'
			GROUP BY user_id
		) la ON la.user_id = u.id
		LEFT JOIN (
			SELECT user_id, COUNT(*) as count
			FROM rules
			GROUP BY user_id
		) r ON r.user_id = u.id
		LEFT JOIN (
			SELECT referrer_id, COUNT(*) as count
			FROM referrals
			WHERE status = 'completed'
			GROUP BY referrer_id
		) ref ON ref.referrer_id = u.id
		WHERE u.is_deleted = FALSE%s
		ORDER BY u.created_at DESC LIMIT $%d OFFSET $%d`, whereClause, argIndex, argIndex+1)
	args = append(args, limit, offset)

	// Execute main query
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list users: %w", err)
	}
	defer rows.Close()

	var users []domain.AdminUserListItem
	for rows.Next() {
		var user domain.AdminUserListItem
		err := rows.Scan(
			&user.ID,
			&user.Email,
			&user.SubscriptionTier,
			&user.SubscriptionStatus,
			&user.ManualTierOverride,
			&user.ManualTierEndsAt,
			&user.PrimaryRegion,
			&user.CreatedAt,
			&user.LastLoginAt,
			&user.IsActive,
			&user.LinkedAccountsCount,
			&user.AutomationRulesCount,
			&user.ReferralsCount,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan user: %w", err)
		}

		decryptedEmail, err := r.enc.DecryptString(user.Email)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to decrypt email: %w", err)
		}
		user.Email = decryptedEmail

		users = append(users, user)
	}

	if err = rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("rows iteration error: %w", err)
	}

	return users, totalCount, nil
}

// GetUserDetail returns detailed user information with full statistics
func (r *adminUserRepository) GetUserDetail(ctx context.Context, userID string) (*domain.AdminUserDetail, error) {
	query := `
		SELECT u.id, u.email, u.subscription_tier, u.subscription_status,
		       u.manual_tier_override, u.manual_tier_ends_at, u.primary_region,
		       u.created_at, u.last_login_at, u.is_active,
		       COALESCE(la.count, 0) as linked_accounts_count,
		       COALESCE(r.count, 0) as automation_rules_count,
		       COALESCE(ref.count, 0) as referrals_count,
		       COALESCE(t.count, 0) as transactions_count,
		       COALESCE(b.count, 0) as budgets_count
		FROM users u
		LEFT JOIN (
			SELECT user_id, COUNT(*) as count
			FROM linked_items
			WHERE status = 'active'
			GROUP BY user_id
		) la ON la.user_id = u.id
		LEFT JOIN (
			SELECT user_id, COUNT(*) as count
			FROM rules
			GROUP BY user_id
		) r ON r.user_id = u.id
		LEFT JOIN (
			SELECT referrer_id, COUNT(*) as count
			FROM referrals
			WHERE status = 'completed'
			GROUP BY referrer_id
		) ref ON ref.referrer_id = u.id
		LEFT JOIN (
			SELECT user_id, COUNT(*) as count
			FROM transactions
			GROUP BY user_id
		) t ON t.user_id = u.id
		LEFT JOIN (
			SELECT user_id, COUNT(*) as count
			FROM budgets
			GROUP BY user_id
		) b ON b.user_id = u.id
		WHERE u.id = $1 AND u.is_deleted = FALSE
	`

	var detail domain.AdminUserDetail
	err := r.db.QueryRow(ctx, query, userID).Scan(
		&detail.ID,
		&detail.Email,
		&detail.SubscriptionTier,
		&detail.SubscriptionStatus,
		&detail.ManualTierOverride,
		&detail.ManualTierEndsAt,
		&detail.PrimaryRegion,
		&detail.CreatedAt,
		&detail.LastLoginAt,
		&detail.IsActive,
		&detail.LinkedAccountsCount,
		&detail.AutomationRulesCount,
		&detail.ReferralsCount,
		&detail.TransactionsCount,
		&detail.BudgetsCount,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user detail: %w", err)
	}

	decryptedEmail, err := r.enc.DecryptString(detail.Email)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt email: %w", err)
	}
	detail.Email = decryptedEmail

	return &detail, nil
}

// UpdateUserTier updates the manual tier override fields inside the users table and creates an audit log.
// Returns the target user's clerk_user_id so the caller can sync Clerk metadata for the correct user.
func (r *adminUserRepository) UpdateUserTier(ctx context.Context, userID string, tier string, reason string, expiresAt *time.Time) (string, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Update the user table and retrieve clerk_user_id for the Clerk metadata sync
	updateQuery := `
		UPDATE users
		SET manual_tier_override = $1, manual_tier_ends_at = $2, updated_at = NOW()
		WHERE id = $3 AND is_deleted = FALSE
		RETURNING clerk_user_id
	`
	var clerkUserID string
	err = tx.QueryRow(ctx, updateQuery, tier, expiresAt, userID).Scan(&clerkUserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrUserNotFound
		}
		return "", fmt.Errorf("failed to update user tier: %w", err)
	}

	// Decrypt the returned clerk_user_id (it is encrypted in the DB)
	clerkUserID, err = r.enc.DecryptString(clerkUserID)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt clerk_user_id: %w", err)
	}

	// Insert into audit_logs using the correct column names
	auditQuery := `
		INSERT INTO audit_logs (user_id, action, resource_type, resource_id, changes)
		VALUES ($1, 'update_manual_tier', 'user', $1, $2)
	`

	detailsMap := map[string]interface{}{
		"tier":   tier,
		"reason": reason,
	}
	if expiresAt != nil {
		detailsMap["expires_at"] = expiresAt.Format("2006-01-02T15:04:05Z")
	}
	detailsBytes, err := json.Marshal(detailsMap)
	if err != nil {
		return "", fmt.Errorf("failed to marshal audit details: %w", err)
	}

	encryptedChanges, err := r.enc.EncryptJSON(json.RawMessage(detailsBytes))
	if err != nil {
		return "", fmt.Errorf("failed to encrypt audit changes: %w", err)
	}

	_, err = tx.Exec(ctx, auditQuery, userID, encryptedChanges)
	if err != nil {
		return "", fmt.Errorf("failed to create audit log: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("failed to commit transaction: %w", err)
	}
	return clerkUserID, nil
}

// buildFilterClause constructs the WHERE clause and arguments for admin user filters.
// Returns the WHERE clause (with leading space), args, and the next arg index.
func (r *adminUserRepository) buildFilterClause(filters domain.AdminUserFilters) (string, []any, int) {
	var clause string
	args := []any{}
	argIndex := 1

	if filters.Search != "" {
		clause += fmt.Sprintf(" AND u.email_blind_idx = $%d", argIndex)
		args = append(args, r.enc.BlindIndex(strings.ToLower(strings.TrimSpace(filters.Search))))
		argIndex++
	}

	if filters.Tier != "" {
		clause += fmt.Sprintf(" AND u.subscription_tier = $%d", argIndex)
		args = append(args, filters.Tier)
		argIndex++
	}

	return clause, args, argIndex
}
