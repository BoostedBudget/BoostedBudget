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

// UserTagRepository defines the interface for user tag data access
type UserTagRepository interface {
	// Create creates a new user tag in the database
	Create(ctx context.Context, tag *domain.UserTag) error

	// GetByID retrieves a tag by its ID
	GetByID(ctx context.Context, id string) (*domain.UserTag, error)

	// GetByUserID retrieves all tags for a specific user
	GetByUserID(ctx context.Context, userID string) ([]*domain.UserTag, error)

	// GetByName retrieves a tag by user ID and name
	GetByName(ctx context.Context, userID, name string) (*domain.UserTag, error)

	// Update updates an existing tag
	Update(ctx context.Context, tag *domain.UserTag) error

	// Delete removes a tag from the database
	Delete(ctx context.Context, id string) error

	// CountByUserID counts the total number of tags for a user
	CountByUserID(ctx context.Context, userID string) (int, error)

	// IncrementUsageCount increments the usage count for a tag
	IncrementUsageCount(ctx context.Context, userID, tagName string) error

	// DecrementUsageCount decrements the usage count for a tag
	DecrementUsageCount(ctx context.Context, userID, tagName string) error
}

// userTagRepository implements UserTagRepository using PostgreSQL
type userTagRepository struct {
	db DB
}

// NewUserTagRepository creates a new UserTagRepository instance
func NewUserTagRepository(db DB) UserTagRepository {
	return &userTagRepository{db: db}
}

// Create creates a new user tag in the database
func (r *userTagRepository) Create(ctx context.Context, tag *domain.UserTag) error {
	// Generate UUID if not already set
	if tag.ID == "" {
		tag.ID = uuid.New().String()
	}

	// Set timestamps
	now := time.Now()
	tag.CreatedAt = now
	tag.UpdatedAt = now

	query := `
		INSERT INTO user_tags (id, user_id, name, color, usage_count, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	_, err := r.db.Exec(ctx, query,
		tag.ID,
		tag.UserID,
		tag.Name,
		tag.Color,
		tag.UsageCount,
		tag.CreatedAt,
		tag.UpdatedAt,
	)

	if err != nil {
		// Check for unique constraint violations
		if strings.Contains(err.Error(), "unique_user_tag_name") ||
			strings.Contains(err.Error(), "duplicate key") {
			return domain.ErrTagAlreadyExists
		}
		return err
	}

	return nil
}

// GetByID retrieves a tag by its ID
func (r *userTagRepository) GetByID(ctx context.Context, id string) (*domain.UserTag, error) {
	query := `
		SELECT id, user_id, name, color, usage_count, created_at, updated_at
		FROM user_tags
		WHERE id = $1
	`

	tag := &domain.UserTag{}
	err := r.db.QueryRow(ctx, query, id).Scan(
		&tag.ID,
		&tag.UserID,
		&tag.Name,
		&tag.Color,
		&tag.UsageCount,
		&tag.CreatedAt,
		&tag.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrTagNotFound
		}
		return nil, err
	}

	return tag, nil
}

// GetByUserID retrieves all tags for a specific user
func (r *userTagRepository) GetByUserID(ctx context.Context, userID string) ([]*domain.UserTag, error) {
	query := `
		SELECT id, user_id, name, color, usage_count, created_at, updated_at
		FROM user_tags
		WHERE user_id = $1
		ORDER BY name ASC
	`

	rows, err := r.db.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []*domain.UserTag
	for rows.Next() {
		tag := &domain.UserTag{}
		err := rows.Scan(
			&tag.ID,
			&tag.UserID,
			&tag.Name,
			&tag.Color,
			&tag.UsageCount,
			&tag.CreatedAt,
			&tag.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return tags, nil
}

// GetByName retrieves a tag by user ID and name
func (r *userTagRepository) GetByName(ctx context.Context, userID, name string) (*domain.UserTag, error) {
	query := `
		SELECT id, user_id, name, color, usage_count, created_at, updated_at
		FROM user_tags
		WHERE user_id = $1 AND name = $2
	`

	tag := &domain.UserTag{}
	err := r.db.QueryRow(ctx, query, userID, name).Scan(
		&tag.ID,
		&tag.UserID,
		&tag.Name,
		&tag.Color,
		&tag.UsageCount,
		&tag.CreatedAt,
		&tag.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrTagNotFound
		}
		return nil, err
	}

	return tag, nil
}

// Update updates an existing tag
func (r *userTagRepository) Update(ctx context.Context, tag *domain.UserTag) error {
	tag.UpdatedAt = time.Now()

	query := `
		UPDATE user_tags
		SET name = $1, color = $2, usage_count = $3, updated_at = $4
		WHERE id = $5
	`

	ct, err := r.db.Exec(ctx, query,
		tag.Name,
		tag.Color,
		tag.UsageCount,
		tag.UpdatedAt,
		tag.ID,
	)
	if err != nil {
		// Check for unique constraint violations
		if strings.Contains(err.Error(), "unique_user_tag_name") ||
			strings.Contains(err.Error(), "duplicate key") {
			return domain.ErrTagAlreadyExists
		}
		return err
	}

	if ct.RowsAffected() == 0 {
		return domain.ErrTagNotFound
	}

	return nil
}

// Delete removes a tag from the database
func (r *userTagRepository) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM user_tags WHERE id = $1`

	ct, err := r.db.Exec(ctx, query, id)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return domain.ErrTagNotFound
	}

	return nil
}

// CountByUserID counts the total number of tags for a user
func (r *userTagRepository) CountByUserID(ctx context.Context, userID string) (int, error) {
	query := `SELECT COUNT(*) FROM user_tags WHERE user_id = $1`

	var count int
	err := r.db.QueryRow(ctx, query, userID).Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

// IncrementUsageCount increments the usage count for a tag
func (r *userTagRepository) IncrementUsageCount(ctx context.Context, userID, tagName string) error {
	query := `
		UPDATE user_tags
		SET usage_count = usage_count + 1, updated_at = $1
		WHERE user_id = $2 AND name = $3
	`

	ct, err := r.db.Exec(ctx, query, time.Now(), userID, tagName)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return domain.ErrTagNotFound
	}

	return nil
}

// DecrementUsageCount decrements the usage count for a tag
func (r *userTagRepository) DecrementUsageCount(ctx context.Context, userID, tagName string) error {
	query := `
		UPDATE user_tags
		SET usage_count = GREATEST(usage_count - 1, 0), updated_at = $1
		WHERE user_id = $2 AND name = $3
	`

	ct, err := r.db.Exec(ctx, query, time.Now(), userID, tagName)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return domain.ErrTagNotFound
	}

	return nil
}
