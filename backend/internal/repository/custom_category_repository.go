package repository

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/google/uuid"
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
}

// customCategoryRepository implements CustomCategoryRepository using PostgreSQL
type customCategoryRepository struct {
	db *sql.DB
}

// NewCustomCategoryRepository creates a new CustomCategoryRepository instance
func NewCustomCategoryRepository(db *sql.DB) CustomCategoryRepository {
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
		INSERT INTO custom_categories (id, user_id, name, parent_category, icon, color, usage_count, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	_, err := r.db.ExecContext(ctx, query,
		category.ID,
		category.UserID,
		category.Name,
		category.ParentCategory,
		category.Icon,
		category.Color,
		category.UsageCount,
		category.CreatedAt,
		category.UpdatedAt,
	)

	if err != nil {
		// Check for unique constraint violations
		if strings.Contains(err.Error(), "unique_user_category_name") ||
			strings.Contains(err.Error(), "duplicate key") {
			return domain.ErrCategoryAlreadyExists
		}
		return err
	}

	return nil
}

// GetByID retrieves a category by its ID
func (r *customCategoryRepository) GetByID(ctx context.Context, id string) (*domain.CustomCategory, error) {
	query := `
		SELECT id, user_id, name, parent_category, icon, color, usage_count, created_at, updated_at
		FROM custom_categories
		WHERE id = $1
	`

	category := &domain.CustomCategory{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&category.ID,
		&category.UserID,
		&category.Name,
		&category.ParentCategory,
		&category.Icon,
		&category.Color,
		&category.UsageCount,
		&category.CreatedAt,
		&category.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, domain.ErrCategoryNotFound
		}
		return nil, err
	}

	return category, nil
}

// GetByUserID retrieves all categories for a specific user
func (r *customCategoryRepository) GetByUserID(ctx context.Context, userID string) ([]*domain.CustomCategory, error) {
	query := `
		SELECT id, user_id, name, parent_category, icon, color, usage_count, created_at, updated_at
		FROM custom_categories
		WHERE user_id = $1
		ORDER BY name ASC
	`

	rows, err := r.db.QueryContext(ctx, query, userID)
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
			&category.ParentCategory,
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
		SELECT id, user_id, name, parent_category, icon, color, usage_count, created_at, updated_at
		FROM custom_categories
		WHERE user_id = $1 AND name = $2
	`

	category := &domain.CustomCategory{}
	err := r.db.QueryRowContext(ctx, query, userID, name).Scan(
		&category.ID,
		&category.UserID,
		&category.Name,
		&category.ParentCategory,
		&category.Icon,
		&category.Color,
		&category.UsageCount,
		&category.CreatedAt,
		&category.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
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
		SET name = $1, parent_category = $2, icon = $3, color = $4, usage_count = $5, updated_at = $6
		WHERE id = $7
	`

	result, err := r.db.ExecContext(ctx, query,
		category.Name,
		category.ParentCategory,
		category.Icon,
		category.Color,
		category.UsageCount,
		category.UpdatedAt,
		category.ID,
	)
	if err != nil {
		// Check for unique constraint violations
		if strings.Contains(err.Error(), "unique_user_category_name") ||
			strings.Contains(err.Error(), "duplicate key") {
			return domain.ErrCategoryAlreadyExists
		}
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return domain.ErrCategoryNotFound
	}

	return nil
}

// Delete removes a category from the database
func (r *customCategoryRepository) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM custom_categories WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return domain.ErrCategoryNotFound
	}

	return nil
}

// CountByUserID counts the total number of categories for a user
func (r *customCategoryRepository) CountByUserID(ctx context.Context, userID string) (int, error) {
	query := `SELECT COUNT(*) FROM custom_categories WHERE user_id = $1`

	var count int
	err := r.db.QueryRowContext(ctx, query, userID).Scan(&count)
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

	result, err := r.db.ExecContext(ctx, query, time.Now(), userID, categoryName)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
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

	result, err := r.db.ExecContext(ctx, query, time.Now(), userID, categoryName)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return domain.ErrCategoryNotFound
	}

	return nil
}
