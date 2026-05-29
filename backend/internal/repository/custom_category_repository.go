package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// CustomCategoryRepository defines the interface for custom category data access
type CustomCategoryRepository interface {
	// Create creates a new custom category in the database
	Create(ctx context.Context, category *domain.CustomCategory) error

	// GetByID retrieves a category by its ID
	GetByID(ctx context.Context, id string) (*domain.CustomCategory, error)

	// GetByUserID retrieves all categories for a specific user
	GetByUserID(ctx context.Context, userID string) ([]*domain.CustomCategory, error)

	// GetByName retrieves a category by user ID and name
	GetByName(ctx context.Context, userID, name string) (*domain.CustomCategory, error)

	// Update updates an existing category
	Update(ctx context.Context, category *domain.CustomCategory) error

	// Delete removes a category from the database
	Delete(ctx context.Context, id string) error

	// CountByUserID counts the total number of categories for a user
	CountByUserID(ctx context.Context, userID string) (int, error)

	// IncrementUsageCount increments the usage count for a category
	IncrementUsageCount(ctx context.Context, userID, categoryName string) error

	// DecrementUsageCount decrements the usage count for a category
	DecrementUsageCount(ctx context.Context, userID, categoryName string) error

	// GetNamesByIDs returns a map of id→name for the given IDs scoped to userID.
	// IDs not owned by userID or not found are omitted from the result.
	// Returns an empty map (not an error) when ids is empty.
	GetNamesByIDs(ctx context.Context, userID string, ids []string) (map[string]string, error)

	// GetChildren returns the direct children of the given parent category (Issue #1203).
	GetChildren(ctx context.Context, parentID string) ([]*domain.CustomCategory, error)

	// HasChildren reports whether the given category has any direct children.
	HasChildren(ctx context.Context, id string) (bool, error)

	// ClearParentForChildren reparents all direct children of parentID to top-level
	// (sets their parent_id to NULL). Used before deleting a parent category.
	ClearParentForChildren(ctx context.Context, parentID string) error

	// GetCategoryUsage returns live counts of transactions, budget categories,
	// rules, and child categories referencing the given category (Issue #1203).
	GetCategoryUsage(ctx context.Context, userID, categoryID string) (*domain.CategoryUsage, error)

	// DeleteWithReassignment deletes a custom category in a single transaction,
	// first reverting referencing transactions to their Plaid PFC + unreviewed,
	// clearing/deleting referencing rules, and reparenting children to top-level
	// (Issue #1203). Budget categories are removed via the ON DELETE CASCADE FK.
	DeleteWithReassignment(ctx context.Context, userID, categoryID string) error
}

// customCategoryRepository implements CustomCategoryRepository using PostgreSQL
type customCategoryRepository struct {
	db DB
}

// NewCustomCategoryRepository creates a new CustomCategoryRepository instance
func NewCustomCategoryRepository(db DB) CustomCategoryRepository {
	return &customCategoryRepository{db: db}
}

// Create creates a new custom category in the database
func (r *customCategoryRepository) Create(ctx context.Context, category *domain.CustomCategory) error {
	// Generate UUID if not already set
	if category.ID == "" {
		category.ID = uuid.New().String()
	}

	// Set timestamps
	now := time.Now()
	category.CreatedAt = now
	category.UpdatedAt = now

	query := `
		INSERT INTO custom_categories (id, user_id, name, parent_pfc_primary, parent_id, icon, color, usage_count, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`

	_, err := r.db.Exec(ctx, query,
		category.ID,
		category.UserID,
		category.Name,
		category.ParentPFCPrimary,
		category.ParentID,
		category.Icon,
		category.Color,
		category.UsageCount,
		category.CreatedAt,
		category.UpdatedAt,
	)

	if err != nil {
		return mapCategoryWriteError(err)
	}

	return nil
}

// mapCategoryWriteError translates DB constraint/trigger violations on
// custom_categories writes into domain errors (Issue #1203).
func mapCategoryWriteError(err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "unique_user_category_name") || strings.Contains(msg, "duplicate key"):
		return domain.ErrCategoryAlreadyExists
	case strings.Contains(msg, "no grandchildren"):
		return domain.ErrCategoryParentHasParent
	case strings.Contains(msg, "cannot be nested"):
		return domain.ErrCategoryHasChildren
	case strings.Contains(msg, "same user") ||
		strings.Contains(msg, "custom_category_single_parent") ||
		strings.Contains(msg, "custom_category_no_self_parent"):
		return domain.ErrCategoryInvalidParent
	default:
		return err
	}
}

// GetByID retrieves a category by its ID
func (r *customCategoryRepository) GetByID(ctx context.Context, id string) (*domain.CustomCategory, error) {
	query := `
		SELECT id, user_id, name, parent_pfc_primary, parent_id, icon, color, usage_count, created_at, updated_at
		FROM custom_categories
		WHERE id = $1
	`

	category := &domain.CustomCategory{}
	err := r.db.QueryRow(ctx, query, id).Scan(
		&category.ID,
		&category.UserID,
		&category.Name,
		&category.ParentPFCPrimary,
		&category.ParentID,
		&category.Icon,
		&category.Color,
		&category.UsageCount,
		&category.CreatedAt,
		&category.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrCategoryNotFound
		}
		return nil, err
	}

	return category, nil
}

// GetByUserID retrieves all categories for a specific user
func (r *customCategoryRepository) GetByUserID(ctx context.Context, userID string) ([]*domain.CustomCategory, error) {
	query := `
		SELECT id, user_id, name, parent_pfc_primary, parent_id, icon, color, usage_count, created_at, updated_at
		FROM custom_categories
		WHERE user_id = $1
		ORDER BY name ASC
	`

	rows, err := r.db.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var categories []*domain.CustomCategory
	for rows.Next() {
		category := &domain.CustomCategory{}
		err := rows.Scan(
			&category.ID,
			&category.UserID,
			&category.Name,
			&category.ParentPFCPrimary,
			&category.ParentID,
			&category.Icon,
			&category.Color,
			&category.UsageCount,
			&category.CreatedAt,
			&category.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		categories = append(categories, category)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return categories, nil
}

// GetByName retrieves a category by user ID and name
func (r *customCategoryRepository) GetByName(ctx context.Context, userID, name string) (*domain.CustomCategory, error) {
	query := `
		SELECT id, user_id, name, parent_pfc_primary, parent_id, icon, color, usage_count, created_at, updated_at
		FROM custom_categories
		WHERE user_id = $1 AND name = $2
	`

	category := &domain.CustomCategory{}
	err := r.db.QueryRow(ctx, query, userID, name).Scan(
		&category.ID,
		&category.UserID,
		&category.Name,
		&category.ParentPFCPrimary,
		&category.ParentID,
		&category.Icon,
		&category.Color,
		&category.UsageCount,
		&category.CreatedAt,
		&category.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrCategoryNotFound
		}
		return nil, err
	}

	return category, nil
}

// Update updates an existing category
func (r *customCategoryRepository) Update(ctx context.Context, category *domain.CustomCategory) error {
	category.UpdatedAt = time.Now()

	query := `
		UPDATE custom_categories
		SET name = $1, parent_pfc_primary = $2, parent_id = $3, icon = $4, color = $5, usage_count = $6, updated_at = $7
		WHERE id = $8
	`

	ct, err := r.db.Exec(ctx, query,
		category.Name,
		category.ParentPFCPrimary,
		category.ParentID,
		category.Icon,
		category.Color,
		category.UsageCount,
		category.UpdatedAt,
		category.ID,
	)
	if err != nil {
		return mapCategoryWriteError(err)
	}

	if ct.RowsAffected() == 0 {
		return domain.ErrCategoryNotFound
	}

	return nil
}

// Delete removes a category from the database
func (r *customCategoryRepository) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM custom_categories WHERE id = $1`

	ct, err := r.db.Exec(ctx, query, id)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return domain.ErrCategoryNotFound
	}

	return nil
}

// CountByUserID counts the total number of categories for a user
func (r *customCategoryRepository) CountByUserID(ctx context.Context, userID string) (int, error) {
	query := `SELECT COUNT(*) FROM custom_categories WHERE user_id = $1`

	var count int
	err := r.db.QueryRow(ctx, query, userID).Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

// IncrementUsageCount increments the usage count for a category
func (r *customCategoryRepository) IncrementUsageCount(ctx context.Context, userID, categoryName string) error {
	query := `
		UPDATE custom_categories
		SET usage_count = usage_count + 1, updated_at = $1
		WHERE user_id = $2 AND name = $3
	`

	ct, err := r.db.Exec(ctx, query, time.Now(), userID, categoryName)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return domain.ErrCategoryNotFound
	}

	return nil
}

// DecrementUsageCount decrements the usage count for a category
func (r *customCategoryRepository) DecrementUsageCount(ctx context.Context, userID, categoryName string) error {
	query := `
		UPDATE custom_categories
		SET usage_count = GREATEST(usage_count - 1, 0), updated_at = $1
		WHERE user_id = $2 AND name = $3
	`

	ct, err := r.db.Exec(ctx, query, time.Now(), userID, categoryName)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return domain.ErrCategoryNotFound
	}

	return nil
}

// GetNamesByIDs returns a map of id→name for the given IDs scoped to userID.
// Dedupes the input slice. Returns an empty map for empty input without hitting the DB.
// IDs not found or owned by a different user are omitted from the result.
func (r *customCategoryRepository) GetNamesByIDs(ctx context.Context, userID string, ids []string) (map[string]string, error) {
	if len(ids) == 0 {
		return map[string]string{}, nil
	}

	// Dedupe
	seen := make(map[string]struct{}, len(ids))
	unique := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			unique = append(unique, id)
		}
	}

	query := `
		SELECT id, name
		FROM custom_categories
		WHERE id = ANY($1::uuid[])
		  AND user_id = $2
	`

	rows, err := r.db.Query(ctx, query, unique, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]string, len(unique))
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		result[id] = name
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

// GetChildren returns the direct children of the given parent category.
func (r *customCategoryRepository) GetChildren(ctx context.Context, parentID string) ([]*domain.CustomCategory, error) {
	query := `
		SELECT id, user_id, name, parent_pfc_primary, parent_id, icon, color, usage_count, created_at, updated_at
		FROM custom_categories
		WHERE parent_id = $1
		ORDER BY name ASC
	`

	rows, err := r.db.Query(ctx, query, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var categories []*domain.CustomCategory
	for rows.Next() {
		category := &domain.CustomCategory{}
		err := rows.Scan(
			&category.ID,
			&category.UserID,
			&category.Name,
			&category.ParentPFCPrimary,
			&category.ParentID,
			&category.Icon,
			&category.Color,
			&category.UsageCount,
			&category.CreatedAt,
			&category.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		categories = append(categories, category)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return categories, nil
}

// HasChildren reports whether the given category has any direct children.
func (r *customCategoryRepository) HasChildren(ctx context.Context, id string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM custom_categories WHERE parent_id = $1)`

	var exists bool
	if err := r.db.QueryRow(ctx, query, id).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

// ClearParentForChildren reparents all direct children of parentID to top-level.
func (r *customCategoryRepository) ClearParentForChildren(ctx context.Context, parentID string) error {
	query := `UPDATE custom_categories SET parent_id = NULL, updated_at = $1 WHERE parent_id = $2`
	_, err := r.db.Exec(ctx, query, time.Now(), parentID)
	return err
}

// GetCategoryUsage returns live reference counts for a custom category.
func (r *customCategoryRepository) GetCategoryUsage(ctx context.Context, userID, categoryID string) (*domain.CategoryUsage, error) {
	usage := &domain.CategoryUsage{}

	if err := r.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM transactions WHERE user_id = $1 AND custom_category_override_id = $2`,
		userID, categoryID,
	).Scan(&usage.TransactionCount); err != nil {
		return nil, err
	}

	if err := r.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM budget_categories bc
		 JOIN budgets b ON b.id = bc.budget_id
		 WHERE b.user_id = $1 AND bc.custom_category_id = $2`,
		userID, categoryID,
	).Scan(&usage.BudgetCount); err != nil {
		return nil, err
	}

	if err := r.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM categorization_rules WHERE user_id = $1 AND target_custom_category_id = $2`,
		userID, categoryID,
	).Scan(&usage.RuleCount); err != nil {
		return nil, err
	}

	if err := r.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM custom_categories WHERE user_id = $1 AND parent_id = $2`,
		userID, categoryID,
	).Scan(&usage.ChildCount); err != nil {
		return nil, err
	}

	return usage, nil
}

// DeleteWithReassignment deletes a custom category and reassigns its references in
// one transaction (Issue #1203).
func (r *customCategoryRepository) DeleteWithReassignment(ctx context.Context, userID, categoryID string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 1. Revert transactions to their Plaid PFC category and back to unreviewed.
	if _, err := tx.Exec(ctx, `
		UPDATE transactions
		SET category_override_kind = NULL,
		    custom_category_override_id = NULL,
		    pfc_primary_override = NULL,
		    pfc_detailed_override = NULL,
		    category_source = 'plaid',
		    reviewed = false,
		    reviewed_at = NULL,
		    effective_category_name = pfc_primary,
		    updated_at = NOW()
		WHERE user_id = $1 AND custom_category_override_id = $2
	`, userID, categoryID); err != nil {
		return fmt.Errorf("failed to revert transactions: %w", err)
	}

	// 2. Delete rules whose ONLY action was this category (no tags remain).
	if _, err := tx.Exec(ctx, `
		DELETE FROM categorization_rules
		WHERE user_id = $1 AND target_custom_category_id = $2
		  AND (target_tags IS NULL OR jsonb_array_length(target_tags) = 0)
	`, userID, categoryID); err != nil {
		return fmt.Errorf("failed to delete category-only rules: %w", err)
	}

	// 3. Clear the category target on remaining (tag-bearing) rules so they stay valid.
	if _, err := tx.Exec(ctx, `
		UPDATE categorization_rules
		SET target_kind = NULL,
		    target_pfc_primary = NULL,
		    target_pfc_detailed = NULL,
		    target_custom_category_id = NULL,
		    updated_at = NOW()
		WHERE user_id = $1 AND target_custom_category_id = $2
	`, userID, categoryID); err != nil {
		return fmt.Errorf("failed to clear rule category targets: %w", err)
	}

	// 4. Reparent children to top-level (lifts the self-FK RESTRICT).
	if _, err := tx.Exec(ctx,
		`UPDATE custom_categories SET parent_id = NULL, updated_at = NOW() WHERE parent_id = $1`,
		categoryID,
	); err != nil {
		return fmt.Errorf("failed to reparent children: %w", err)
	}

	// 5. Delete the category (budget_categories rows cascade via FK).
	ct, err := tx.Exec(ctx,
		`DELETE FROM custom_categories WHERE id = $1 AND user_id = $2`,
		categoryID, userID,
	)
	if err != nil {
		return fmt.Errorf("failed to delete category: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return domain.ErrCategoryNotFound
	}

	return tx.Commit(ctx)
}
