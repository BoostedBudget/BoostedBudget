package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// AdminUserRepository defines the interface for admin user data access
type AdminUserRepository interface {
	// ListUsers returns a paginated list of users with counts for admin view
	ListUsers(ctx context.Context, filters domain.AdminUserFilters, limit, offset int) ([]domain.AdminUserListItem, int, error)

	// GetUserDetail returns detailed user information with full statistics
	GetUserDetail(ctx context.Context, userID string) (*domain.AdminUserDetail, error)
}

// adminUserRepository implements AdminUserRepository using PostgreSQL
type adminUserRepository struct {
	db DB
}

// NewAdminUserRepository creates a new AdminUserRepository instance
func NewAdminUserRepository(db DB) AdminUserRepository {
	return &adminUserRepository{db: db}
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
		       u.primary_region, u.created_at, u.last_login_at, u.is_active,
		       COALESCE(la.count, 0) as linked_accounts_count,
		       COALESCE(r.count, 0) as automation_rules_count
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
			&user.PrimaryRegion,
			&user.CreatedAt,
			&user.LastLoginAt,
			&user.IsActive,
			&user.LinkedAccountsCount,
			&user.AutomationRulesCount,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan user: %w", err)
		}
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
		       u.primary_region, u.created_at, u.last_login_at, u.is_active,
		       COALESCE(la.count, 0) as linked_accounts_count,
		       COALESCE(r.count, 0) as automation_rules_count,
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
		&detail.PrimaryRegion,
		&detail.CreatedAt,
		&detail.LastLoginAt,
		&detail.IsActive,
		&detail.LinkedAccountsCount,
		&detail.AutomationRulesCount,
		&detail.TransactionsCount,
		&detail.BudgetsCount,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user detail: %w", err)
	}

	return &detail, nil
}

// buildFilterClause constructs the WHERE clause and arguments for admin user filters.
// Returns the WHERE clause (with leading space), args, and the next arg index.
func (r *adminUserRepository) buildFilterClause(filters domain.AdminUserFilters) (string, []any, int) {
	var clause string
	args := []any{}
	argIndex := 1

	if filters.Search != "" {
		clause += fmt.Sprintf(" AND u.email ILIKE $%d", argIndex)
		args = append(args, "%"+filters.Search+"%")
		argIndex++
	}

	if filters.Tier != "" {
		clause += fmt.Sprintf(" AND u.subscription_tier = $%d", argIndex)
		args = append(args, filters.Tier)
		argIndex++
	}

	return clause, args, argIndex
}
