package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

var (
	// ErrLinkedItemNotFound is returned when a linked item is not found in the database
	ErrLinkedItemNotFound = errors.New("linked item not found")
	// ErrDuplicateLinkedItem is returned when attempting to create a linked item that already exists
	ErrDuplicateLinkedItem = errors.New("linked item already exists for this user")
)

// LinkedItemRepository defines the interface for linked item data access
type LinkedItemRepository interface {
	// Create creates a new linked item in the database
	Create(ctx context.Context, item *domain.LinkedItem) error

	// GetByID retrieves a linked item by its UUID
	GetByID(ctx context.Context, id string) (*domain.LinkedItem, error)

	// GetByUserID retrieves all linked items for a specific user
	GetByUserID(ctx context.Context, userID string) ([]domain.LinkedItem, error)

	// GetByItemID retrieves a linked item by its Plaid item ID
	GetByItemID(ctx context.Context, itemID string) (*domain.LinkedItem, error)

	// UpdateCursor updates the transaction sync cursor for a linked item
	UpdateCursor(ctx context.Context, id, cursor string) error

	// Delete removes a linked item from the database
	Delete(ctx context.Context, id string) error
}

// linkedItemRepository implements LinkedItemRepository using PostgreSQL
type linkedItemRepository struct {
	db *sql.DB
}

// NewLinkedItemRepository creates a new LinkedItemRepository instance
func NewLinkedItemRepository(db *sql.DB) LinkedItemRepository {
	return &linkedItemRepository{db: db}
}

// Create creates a new linked item in the database
func (r *linkedItemRepository) Create(ctx context.Context, item *domain.LinkedItem) error {
	// Generate UUID if not already set
	if item.ID == "" {
		item.ID = uuid.New().String()
	}

	// Set timestamps
	now := time.Now()
	item.CreatedAt = now
	item.UpdatedAt = now

	query := `
		INSERT INTO linked_items
		(id, user_id, item_id, access_token_encrypted, institution_id, institution_name, provider, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	_, err := r.db.ExecContext(ctx, query,
		item.ID,
		item.UserID,
		item.ItemID,
		item.AccessTokenEncrypted,
		item.InstitutionID,
		item.InstitutionName,
		item.Provider,
		item.CreatedAt,
		item.UpdatedAt,
	)

	if err != nil {
		// Check for unique constraint violations
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
			if strings.Contains(err.Error(), "unique_user_item") {
				return ErrDuplicateLinkedItem
			}
		}
		return err
	}

	return nil
}

// GetByID retrieves a linked item by its UUID
func (r *linkedItemRepository) GetByID(ctx context.Context, id string) (*domain.LinkedItem, error) {
	query := `
		SELECT id, user_id, item_id, access_token_encrypted,
		       institution_id, institution_name, provider,
		       cursor, last_synced_at,
		       created_at, updated_at
		FROM linked_items
		WHERE id = $1
	`

	var item domain.LinkedItem
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&item.ID,
		&item.UserID,
		&item.ItemID,
		&item.AccessTokenEncrypted,
		&item.InstitutionID,
		&item.InstitutionName,
		&item.Provider,
		&item.Cursor,
		&item.LastSyncedAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, ErrLinkedItemNotFound
	}
	if err != nil {
		return nil, err
	}

	return &item, nil
}

// GetByUserID retrieves all linked items for a specific user
func (r *linkedItemRepository) GetByUserID(ctx context.Context, userID string) ([]domain.LinkedItem, error) {
	query := `
		SELECT id, user_id, item_id, access_token_encrypted,
		       institution_id, institution_name, provider,
		       cursor, last_synced_at,
		       created_at, updated_at
		FROM linked_items
		WHERE user_id = $1
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []domain.LinkedItem
	for rows.Next() {
		var item domain.LinkedItem
		err := rows.Scan(
			&item.ID,
			&item.UserID,
			&item.ItemID,
			&item.AccessTokenEncrypted,
			&item.InstitutionID,
			&item.InstitutionName,
			&item.Provider,
			&item.Cursor,
			&item.LastSyncedAt,
			&item.CreatedAt,
			&item.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	// Return empty slice instead of nil if no items found
	if items == nil {
		items = []domain.LinkedItem{}
	}

	return items, nil
}

// GetByItemID retrieves a linked item by its Plaid item ID
func (r *linkedItemRepository) GetByItemID(ctx context.Context, itemID string) (*domain.LinkedItem, error) {
	query := `
		SELECT id, user_id, item_id, access_token_encrypted,
		       institution_id, institution_name, provider,
		       cursor, last_synced_at,
		       created_at, updated_at
		FROM linked_items
		WHERE item_id = $1
	`

	var item domain.LinkedItem
	err := r.db.QueryRowContext(ctx, query, itemID).Scan(
		&item.ID,
		&item.UserID,
		&item.ItemID,
		&item.AccessTokenEncrypted,
		&item.InstitutionID,
		&item.InstitutionName,
		&item.Provider,
		&item.Cursor,
		&item.LastSyncedAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, ErrLinkedItemNotFound
	}
	if err != nil {
		return nil, err
	}

	return &item, nil
}

// UpdateCursor updates the transaction sync cursor for a linked item
func (r *linkedItemRepository) UpdateCursor(ctx context.Context, id, cursor string) error {
	now := time.Now()

	query := `
		UPDATE linked_items
		SET cursor = $1,
		    last_synced_at = $2,
		    updated_at = $3
		WHERE id = $4
	`

	result, err := r.db.ExecContext(ctx, query, cursor, now, now, id)
	if err != nil {
		return err
	}

	// Check if any rows were affected
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return ErrLinkedItemNotFound
	}

	return nil
}

// Delete removes a linked item from the database
func (r *linkedItemRepository) Delete(ctx context.Context, id string) error {
	query := `
		DELETE FROM linked_items
		WHERE id = $1
	`

	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}

	// Check if any rows were affected
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return ErrLinkedItemNotFound
	}

	return nil
}
