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

	// Update updates a linked item's fields (e.g., status, error details)
	// Issue #257: Item Error Handler - updates error tracking fields
	Update(ctx context.Context, item *domain.LinkedItem) error

	// Delete removes a linked item from the database
	Delete(ctx context.Context, id string) error

	// GetByIDAndUser retrieves a linked item by ID with ownership validation.
	// Returns ErrLinkedItemNotFound if the item doesn't exist or belongs to a different user.
	GetByIDAndUser(ctx context.Context, id, userID string) (*domain.LinkedItem, error)

	// DeleteByUser removes a linked item with ownership validation.
	// The item is only deleted if both the item ID and user ID match (WHERE id = $1 AND user_id = $2).
	// This prevents users from deleting items they don't own.
	//
	// Returns ErrLinkedItemNotFound if:
	//   - The item doesn't exist
	//   - The item exists but belongs to a different user (ownership check fails)
	DeleteByUser(ctx context.Context, id, userID string) error

	// GetByIDForUpdate retrieves a linked item by ID with a row-level exclusive lock.
	// Use this in sync workers to prevent concurrent syncs for the same item.
	// The lock is held until the transaction commits or rolls back.
	//
	// IMPORTANT: This must be called within a transaction. The lock is released
	// when the transaction ends (commit or rollback).
	//
	// Returns ErrLinkedItemNotFound if the item doesn't exist.
	GetByIDForUpdate(ctx context.Context, tx pgx.Tx, id string) (*domain.LinkedItem, error)

	// GetByUserAndInstitution retrieves a linked item by user ID, institution ID, and status.
	// This is used for re-link detection to find existing active items at the same institution.
	// Issue #171: Plaid Re-Link Handling
	//
	// Returns nil, nil if no matching item found (not an error).
	GetByUserAndInstitution(ctx context.Context, userID, institutionID, status string) (*domain.LinkedItem, error)

	// Issue #668: Plaid Item Management for Cost Optimization - Cleanup tracking methods

	// GetItemsForCleanup retrieves items that are candidates for cleanup.
	// Criteria: items in error/login_required state > errorDays, OR revoked status, OR not synced > staleDays.
	// Only returns items that haven't been notified yet (cleanup_scheduled_at IS NULL).
	GetItemsForCleanup(ctx context.Context, errorDays, staleDays int) ([]domain.LinkedItem, error)

	// GetItemsPendingCleanup retrieves items where cleanup is scheduled and past due.
	// Returns items where cleanup_scheduled_at < NOW().
	GetItemsPendingCleanup(ctx context.Context) ([]domain.LinkedItem, error)

	// MarkForCleanup marks an item for cleanup with notification and scheduled cleanup timestamp.
	MarkForCleanup(ctx context.Context, itemID string, cleanupAt time.Time) error

	// ClearCleanupSchedule clears the cleanup schedule for an item (e.g., if user reconnected).
	ClearCleanupSchedule(ctx context.Context, itemID string) error
}

// linkedItemRepository implements LinkedItemRepository using PostgreSQL
type linkedItemRepository struct {
	db DB
}

// NewLinkedItemRepository creates a new LinkedItemRepository instance
func NewLinkedItemRepository(db DB) LinkedItemRepository {
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

	_, err := r.db.Exec(ctx, query,
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
// Issue #171: Updated to include status, error tracking, and soft-delete fields
func (r *linkedItemRepository) GetByID(ctx context.Context, id string) (*domain.LinkedItem, error) {
	query := `
		SELECT id, user_id, item_id, access_token_encrypted,
		       institution_id, institution_name, provider, cursor, last_synced_at,
		       status, error_code, error_message, last_error_at, retry_count, next_retry_at,
		       replaced_at, replaced_by_id,
		       created_at, updated_at
		FROM linked_items
		WHERE id = $1
	`

	var item domain.LinkedItem
	err := r.db.QueryRow(ctx, query, id).Scan(
		&item.ID,
		&item.UserID,
		&item.ItemID,
		&item.AccessTokenEncrypted,
		&item.InstitutionID,
		&item.InstitutionName,
		&item.Provider,
		&item.Cursor,
		&item.LastSyncedAt,
		&item.Status,
		&item.ErrorCode,
		&item.ErrorMessage,
		&item.LastErrorAt,
		&item.RetryCount,
		&item.NextRetryAt,
		&item.ReplacedAt,
		&item.ReplacedByID,
		&item.CreatedAt,
		&item.UpdatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrLinkedItemNotFound
	}
	if err != nil {
		return nil, err
	}

	return &item, nil
}

// GetByUserID retrieves all linked items for a specific user
// Issue #171: Updated to include status, error tracking, and soft-delete fields
func (r *linkedItemRepository) GetByUserID(ctx context.Context, userID string) ([]domain.LinkedItem, error) {
	query := `
		SELECT id, user_id, item_id, access_token_encrypted,
		       institution_id, institution_name, provider,
		       cursor, last_synced_at,
		       status, error_code, error_message, last_error_at,
		       retry_count, next_retry_at,
		       replaced_at, replaced_by_id,
		       created_at, updated_at
		FROM linked_items
		WHERE user_id = $1
		ORDER BY created_at DESC
	`

	rows, err := r.db.Query(ctx, query, userID)
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
			&item.Status,
			&item.ErrorCode,
			&item.ErrorMessage,
			&item.LastErrorAt,
			&item.RetryCount,
			&item.NextRetryAt,
			&item.ReplacedAt,
			&item.ReplacedByID,
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

	if items != nil {
		return items, nil
	}
	return []domain.LinkedItem{}, nil
}

// GetByItemID retrieves a linked item by its Plaid item ID
func (r *linkedItemRepository) GetByItemID(ctx context.Context, itemID string) (*domain.LinkedItem, error) {
	// Updated to 19 columns for Issue #171 - Plaid Re-Link Handling
	query := `
		SELECT id, user_id, item_id, access_token_encrypted,
		       institution_id, institution_name, provider, cursor, last_synced_at,
		       status, error_code, error_message, last_error_at, retry_count, next_retry_at,
		       replaced_at, replaced_by_id,
		       created_at, updated_at
		FROM linked_items
		WHERE item_id = $1
	`

	var item domain.LinkedItem
	err := r.db.QueryRow(ctx, query, itemID).Scan(
		&item.ID,
		&item.UserID,
		&item.ItemID,
		&item.AccessTokenEncrypted,
		&item.InstitutionID,
		&item.InstitutionName,
		&item.Provider,
		&item.Cursor,
		&item.LastSyncedAt,
		&item.Status,
		&item.ErrorCode,
		&item.ErrorMessage,
		&item.LastErrorAt,
		&item.RetryCount,
		&item.NextRetryAt,
		&item.ReplacedAt,
		&item.ReplacedByID,
		&item.CreatedAt,
		&item.UpdatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
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

	ct, err := r.db.Exec(ctx, query, cursor, now, now, id)
	if err != nil {
		return err
	}

	// Check if any rows were affected
	if ct.RowsAffected() == 0 {
		return ErrLinkedItemNotFound
	}

	return nil
}

// Update updates a linked item's fields (e.g., status, error details)
// Issue #257: Item Error Handler - updates error tracking fields
// Issue #171: Added replaced_at and replaced_by_id for soft-delete support
func (r *linkedItemRepository) Update(ctx context.Context, item *domain.LinkedItem) error {
	now := time.Now()
	item.UpdatedAt = now

	query := `
		UPDATE linked_items
		SET institution_id = $2,
		    institution_name = $3,
		    provider = $4,
		    cursor = $5,
		    last_synced_at = $6,
		    status = $7,
		    error_code = $8,
		    error_message = $9,
		    last_error_at = $10,
		    retry_count = $11,
		    next_retry_at = $12,
		    replaced_at = $13,
		    replaced_by_id = $14,
		    updated_at = $15
		WHERE id = $1
	`

	ct, err := r.db.Exec(ctx, query,
		item.ID,
		item.InstitutionID,
		item.InstitutionName,
		item.Provider,
		item.Cursor,
		item.LastSyncedAt,
		item.Status,
		item.ErrorCode,
		item.ErrorMessage,
		item.LastErrorAt,
		item.RetryCount,
		item.NextRetryAt,
		item.ReplacedAt,
		item.ReplacedByID,
		item.UpdatedAt,
	)
	if err != nil {
		return err
	}

	// Check if any rows were affected
	if ct.RowsAffected() == 0 {
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

	ct, err := r.db.Exec(ctx, query, id)
	if err != nil {
		return err
	}

	// Check if any rows were affected
	if ct.RowsAffected() == 0 {
		return ErrLinkedItemNotFound
	}

	return nil
}

// GetByIDAndUser retrieves a linked item by ID with ownership validation.
//
// Security: This method validates ownership via the WHERE clause (id AND user_id must match).
// A user can only retrieve items they own. If the item doesn't exist or belongs to
// a different user, sql.ErrNoRows is returned and mapped to ErrLinkedItemNotFound.
//
// This is used by handlers to ensure users can only access their own linked items.
// Issue #171: Updated to include status, error tracking, and soft-delete fields
func (r *linkedItemRepository) GetByIDAndUser(ctx context.Context, id, userID string) (*domain.LinkedItem, error) {
	query := `
		SELECT id, user_id, item_id, access_token_encrypted,
		       institution_id, institution_name, provider, cursor, last_synced_at,
		       status, error_code, error_message, last_error_at, retry_count, next_retry_at,
		       replaced_at, replaced_by_id,
		       created_at, updated_at
		FROM linked_items
		WHERE id = $1 AND user_id = $2
	`

	var item domain.LinkedItem
	err := r.db.QueryRow(ctx, query, id, userID).Scan(
		&item.ID,
		&item.UserID,
		&item.ItemID,
		&item.AccessTokenEncrypted,
		&item.InstitutionID,
		&item.InstitutionName,
		&item.Provider,
		&item.Cursor,
		&item.LastSyncedAt,
		&item.Status,
		&item.ErrorCode,
		&item.ErrorMessage,
		&item.LastErrorAt,
		&item.RetryCount,
		&item.NextRetryAt,
		&item.ReplacedAt,
		&item.ReplacedByID,
		&item.CreatedAt,
		&item.UpdatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrLinkedItemNotFound
	}
	if err != nil {
		return nil, err
	}

	return &item, nil
}

// DeleteByUser removes a linked item with ownership validation.
//
// Security: This method validates ownership via the WHERE clause (id AND user_id must match).
// A user can only delete items they own. If the item doesn't exist or belongs to
// a different user, the DELETE will affect 0 rows and ErrLinkedItemNotFound is returned.
//
// Use case: Called when a user disconnects an institution from the UI.
// Note: CASCADE DELETE automatically removes all associated accounts.
func (r *linkedItemRepository) DeleteByUser(ctx context.Context, id, userID string) error {
	query := `
		DELETE FROM linked_items
		WHERE id = $1 AND user_id = $2
	`

	ct, err := r.db.Exec(ctx, query, id, userID)
	if err != nil {
		return err
	}

	// Check if any rows were affected (validates ownership and existence)
	if ct.RowsAffected() == 0 {
		// Item not found OR user doesn't own it (both cases return same error for security)
		return ErrLinkedItemNotFound
	}

	return nil
}

// GetByIDForUpdate retrieves a linked item by ID with a row-level exclusive lock.
// This prevents concurrent sync operations on the same linked item.
//
// The FOR UPDATE clause acquires an exclusive row-level lock that:
//   - Blocks other transactions attempting to SELECT FOR UPDATE on the same row
//   - Blocks other transactions attempting to UPDATE or DELETE the row
//   - Is held until the transaction commits or rolls back
//
// Usage pattern:
//
//	tx, _ := db.Begin(ctx)
//	defer tx.Rollback(ctx)
//	item, err := repo.GetByIDForUpdate(ctx, tx, linkedItemID)
//	// ... perform sync operations ...
//	tx.Commit(ctx)
//
// Returns ErrLinkedItemNotFound if the item doesn't exist.
func (r *linkedItemRepository) GetByIDForUpdate(ctx context.Context, tx pgx.Tx, id string) (*domain.LinkedItem, error) {
	query := `
		SELECT id, user_id, item_id, access_token_encrypted,
		       institution_id, institution_name, provider,
		       cursor, last_synced_at,
		       status, error_code, error_message, last_error_at,
		       retry_count, next_retry_at,
		       replaced_at, replaced_by_id,
		       created_at, updated_at
		FROM linked_items
		WHERE id = $1
		FOR UPDATE
	`

	var item domain.LinkedItem
	err := tx.QueryRow(ctx, query, id).Scan(
		&item.ID,
		&item.UserID,
		&item.ItemID,
		&item.AccessTokenEncrypted,
		&item.InstitutionID,
		&item.InstitutionName,
		&item.Provider,
		&item.Cursor,
		&item.LastSyncedAt,
		&item.Status,
		&item.ErrorCode,
		&item.ErrorMessage,
		&item.LastErrorAt,
		&item.RetryCount,
		&item.NextRetryAt,
		&item.ReplacedAt,
		&item.ReplacedByID,
		&item.CreatedAt,
		&item.UpdatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrLinkedItemNotFound
	}
	if err != nil {
		return nil, err
	}

	return &item, nil
}

// GetByUserAndInstitution retrieves a linked item by user ID, institution ID, and status.
// This is used for re-link detection to find existing active items at the same institution.
// Issue #171: Plaid Re-Link Handling
//
// Returns nil, nil if no matching item found (not an error).
func (r *linkedItemRepository) GetByUserAndInstitution(ctx context.Context, userID, institutionID, status string) (*domain.LinkedItem, error) {
	query := `
		SELECT id, user_id, item_id, access_token_encrypted,
		       institution_id, institution_name, provider,
		       cursor, last_synced_at,
		       status, error_code, error_message, last_error_at,
		       retry_count, next_retry_at,
		       replaced_at, replaced_by_id,
		       created_at, updated_at
		FROM linked_items
		WHERE user_id = $1 AND institution_id = $2 AND status = $3
	`

	var item domain.LinkedItem
	err := r.db.QueryRow(ctx, query, userID, institutionID, status).Scan(
		&item.ID,
		&item.UserID,
		&item.ItemID,
		&item.AccessTokenEncrypted,
		&item.InstitutionID,
		&item.InstitutionName,
		&item.Provider,
		&item.Cursor,
		&item.LastSyncedAt,
		&item.Status,
		&item.ErrorCode,
		&item.ErrorMessage,
		&item.LastErrorAt,
		&item.RetryCount,
		&item.NextRetryAt,
		&item.ReplacedAt,
		&item.ReplacedByID,
		&item.CreatedAt,
		&item.UpdatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		// No match found - this is not an error, just no existing item
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &item, nil
}

// Issue #668: Plaid Item Management for Cost Optimization - Cleanup tracking methods

// GetItemsForCleanup retrieves items that are candidates for cleanup.
// Criteria:
//   - Items in error/login_required state > errorDays
//   - Items with revoked status (immediate cleanup candidate)
//   - Items not synced > staleDays
//   - Items with zero active accounts (no connected accounts remaining)
//
// Only returns items that haven't been scheduled yet (cleanup_scheduled_at IS NULL).
// Issue #668: Plaid Item Management for Cost Optimization
func (r *linkedItemRepository) GetItemsForCleanup(ctx context.Context, errorDays, staleDays int) ([]domain.LinkedItem, error) {
	query := `
		SELECT li.id, li.user_id, li.item_id, li.access_token_encrypted,
		       li.institution_id, li.institution_name, li.provider, li.cursor, li.last_synced_at,
		       li.status, li.error_code, li.error_message, li.last_error_at, li.retry_count, li.next_retry_at,
		       li.replaced_at, li.replaced_by_id,
		       li.cleanup_notified_at, li.cleanup_scheduled_at,
		       li.created_at, li.updated_at
		FROM linked_items li
		WHERE li.cleanup_scheduled_at IS NULL
		  AND li.status NOT IN ('replaced', 'disconnected')
		  AND (
		      -- Items in error state for > errorDays
		      (li.status IN ('error', 'login_required') AND li.last_error_at < NOW() - $1::interval)
		      -- OR items with revoked status (immediate cleanup candidate)
		      OR li.status = 'revoked'
		      -- OR items not synced for > staleDays
		      OR (li.last_synced_at IS NOT NULL AND li.last_synced_at < NOW() - $2::interval)
		      -- OR items with zero active accounts (immediate cleanup candidate)
		      OR NOT EXISTS (
		          SELECT 1 FROM accounts a
		          WHERE a.linked_item_id = li.id AND a.status = 'active'
		      )
		  )
		ORDER BY li.last_error_at ASC NULLS LAST, li.last_synced_at ASC NULLS LAST
	`

	rows, err := r.db.Query(ctx, query,
		strings.TrimSpace(fmt.Sprintf("%d days", errorDays)),
		strings.TrimSpace(fmt.Sprintf("%d days", staleDays)),
	)
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
			&item.Status,
			&item.ErrorCode,
			&item.ErrorMessage,
			&item.LastErrorAt,
			&item.RetryCount,
			&item.NextRetryAt,
			&item.ReplacedAt,
			&item.ReplacedByID,
			&item.CleanupNotifiedAt,
			&item.CleanupScheduledAt,
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

	if items != nil {
		return items, nil
	}
	return []domain.LinkedItem{}, nil
}

// GetItemsPendingCleanup retrieves items where cleanup is scheduled and past due.
func (r *linkedItemRepository) GetItemsPendingCleanup(ctx context.Context) ([]domain.LinkedItem, error) {
	query := `
		SELECT id, user_id, item_id, access_token_encrypted,
		       institution_id, institution_name, provider, cursor, last_synced_at,
		       status, error_code, error_message, last_error_at, retry_count, next_retry_at,
		       replaced_at, replaced_by_id,
		       cleanup_notified_at, cleanup_scheduled_at,
		       created_at, updated_at
		FROM linked_items
		WHERE cleanup_scheduled_at IS NOT NULL
		  AND cleanup_scheduled_at < NOW()
		  AND status NOT IN ('replaced', 'disconnected')
		ORDER BY cleanup_scheduled_at ASC
	`

	rows, err := r.db.Query(ctx, query)
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
			&item.Status,
			&item.ErrorCode,
			&item.ErrorMessage,
			&item.LastErrorAt,
			&item.RetryCount,
			&item.NextRetryAt,
			&item.ReplacedAt,
			&item.ReplacedByID,
			&item.CleanupNotifiedAt,
			&item.CleanupScheduledAt,
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

	if items != nil {
		return items, nil
	}
	return []domain.LinkedItem{}, nil
}

// MarkForCleanup marks an item for cleanup with notification and scheduled cleanup timestamp.
func (r *linkedItemRepository) MarkForCleanup(ctx context.Context, itemID string, cleanupAt time.Time) error {
	now := time.Now()

	query := `
		UPDATE linked_items
		SET cleanup_notified_at = $1,
		    cleanup_scheduled_at = $2,
		    updated_at = $3
		WHERE id = $4
		  AND cleanup_scheduled_at IS NULL
	`

	ct, err := r.db.Exec(ctx, query, now, cleanupAt, now, itemID)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		// Item not found or already scheduled for cleanup
		return ErrLinkedItemNotFound
	}

	return nil
}

// ClearCleanupSchedule clears the cleanup schedule for an item (e.g., if user reconnected).
func (r *linkedItemRepository) ClearCleanupSchedule(ctx context.Context, itemID string) error {
	now := time.Now()

	query := `
		UPDATE linked_items
		SET cleanup_notified_at = NULL,
		    cleanup_scheduled_at = NULL,
		    updated_at = $1
		WHERE id = $2
	`

	_, err := r.db.Exec(ctx, query, now, itemID)
	return err
}
