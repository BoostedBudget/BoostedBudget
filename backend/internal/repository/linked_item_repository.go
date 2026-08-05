package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

var (
	// ErrLinkedItemNotFound is returned when a linked item is not found in the database
	ErrLinkedItemNotFound = errors.New("linked item not found")
	// ErrDuplicateLinkedItem is returned when attempting to create a linked item that already exists
	ErrDuplicateLinkedItem = errors.New("linked item already exists for this user")
	// ErrNoLinkedItemsUpdated is returned by MarkItemsDisconnected when its UPDATE affected ZERO
	// rows despite being given a non-empty ids slice (Issue #1413 Medium finding, PR #1443 Codex
	// review round 4): before this fix, RowsAffected was never inspected, so a write that silently
	// touched nothing (every given id already 'disconnected'/'replaced', excluded by this query's
	// own WHERE guard — see MarkItemsDisconnected's doc comment) was indistinguishable from a
	// genuine success to both a caller and a test.
	//
	// Deliberately still a case NOT to escalate to a hard failure: cleanupItems (plaid_item_
	// cleanup_service.go), the only production caller, does not filter 'replaced' items out of
	// the ids it passes here (only 'disconnected' items are skipped before ever reaching this
	// write) — RemoveAllUserItems's GetByUserID fetch returns items in ANY status, so a lone
	// already-'replaced' item legitimately reaching this call (e.g. Plaid's ITEM_NOT_FOUND mapped
	// to success for an item replaced by a re-link before this pass ever ran) is a reachable,
	// expected no-op, not a bug — see TestLinkedItemRepository_MarkItemsDisconnected_
	// AlreadyReplacedIsNotReverted. Callers should treat this as a logged anomaly (errors.Is-
	// checkable) rather than a genuine per-item failure — turning an idempotent no-op into a hard
	// abort would reproduce the exact false-positive failure mode Issue #1413 #1453's sibling
	// finding (verifyNoNewLinkedItemAppeared) exists to avoid.
	ErrNoLinkedItemsUpdated = errors.New("MarkItemsDisconnected: update affected zero rows (all given ids already disconnected/replaced?)")
)

// normalizeLinkedItemStatus treats a NULL linked_items.status value as
// domain.LinkedItemStatusActive — Issue #1413 round-8 Blocker 1: the column is nullable
// (migration 000013 added an 'active' default but no NOT NULL constraint), so a row inserted
// before that default existed can have a genuinely NULL status while still being a fully live
// connection (the same rule GetUsersNeedingPlaidDisconnectRepair's SQL and this repository's own
// GetActiveItemsByUserID/MarkItemsDisconnected WHERE clauses already apply). domain.LinkedItem.Status
// is a bare LinkedItemStatus (string) field, not a pointer, so scanning a SQL NULL directly into it
// fails outright ("cannot scan NULL into *string") rather than merely misclassifying it — callers
// must scan the raw column into a *string and pass it through this helper instead of scanning
// directly into &item.Status.
func normalizeLinkedItemStatus(raw *string) domain.LinkedItemStatus {
	if raw == nil {
		return domain.LinkedItemStatusActive
	}
	return domain.LinkedItemStatus(*raw)
}

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

	// Issue #679, #680: Trial expiration - mark items as disconnected to preserve history

	// MarkAllUserItemsDisconnected marks all linked items for a user as 'disconnected'.
	// This is used when a subscription expires to preserve transaction history while
	// stopping Plaid syncing. Unlike Delete, this does NOT cascade delete accounts/transactions.
	MarkAllUserItemsDisconnected(ctx context.Context, userID string) error

	// MarkItemsDisconnected marks the given SPECIFIC linked items (by their internal UUIDs) as
	// 'disconnected', leaving any item NOT in ids untouched. Issue #1413 H2: unlike
	// MarkAllUserItemsDisconnected (which marks every item for a user regardless of whether ITS
	// OWN Plaid /item/remove call actually succeeded), this persists per-item cleanup outcome so
	// a retry after a partial failure only re-attempts the items still outstanding — a failed
	// item's status stays untouched (still visible to GetActiveItemsByUserID / still present in
	// GetByUserID with a non-disconnected status) instead of being silently marked done alongside
	// its successful siblings. A nil or empty ids is a no-op (not an error).
	//
	// Returns ErrNoLinkedItemsUpdated (errors.Is-checkable) if the UPDATE affected zero rows
	// despite a non-empty ids — see that sentinel's doc comment for why this is a reachable,
	// legitimate no-op (not automatically a bug) and must not be conflated with a genuine
	// database error.
	MarkItemsDisconnected(ctx context.Context, ids []string) error

	// GetActiveItemsByUserID retrieves all active (non-replaced, non-disconnected) linked items for a user
	GetActiveItemsByUserID(ctx context.Context, userID string) ([]domain.LinkedItem, error)

	// UpdateInvestmentsLastSyncedDate updates the date through which investment transactions
	// have been synced. Called after a successful investment transactions sync.
	// Issue #659: Plaid Investments
	UpdateInvestmentsLastSyncedDate(ctx context.Context, id string, date time.Time) error

	// UpdateLiabilitiesLastSyncedAt updates the timestamp through which liabilities
	// have been synced. Called after a successful liabilities sync.
	// Issue #910: Plaid Liabilities
	UpdateLiabilitiesLastSyncedAt(ctx context.Context, id string, t time.Time) error
}

// linkedItemRepository implements LinkedItemRepository using PostgreSQL
type linkedItemRepository struct {
	db  DB
	enc *appcrypto.FieldEncryptor
}

// Standard SELECT columns for linked_items (used across all repository methods).
// Order matters: MUST match the order in Scan calls.
const linkedItemSelectCols = `id, user_id, item_id, access_token_encrypted,
       institution_id, institution_name, provider, cursor, last_synced_at,
       status, error_code, error_message, last_error_at, retry_count, next_retry_at,
       replaced_at, replaced_by_id,
       created_at, updated_at,
       investments_last_synced_date, liabilities_last_synced_at,
       status_changed_at`

// NewLinkedItemRepository returns the CONCRETE *linkedItemRepository, not the LinkedItemRepository
// interface — Issue #1469. It used to return the interface, which meant a method absent from that
// wide interface was unreachable through the value main.go holds, so the paged
// GetItemsForCleanupPage below could only be reached by ALSO adding it to the wide interface and to
// every one of its mock implementations (enumerate them with
// `grep -rn '^func (m \*[A-Za-z]*) GetItemsForCleanup(' internal/`; no total is asserted here
// because three counting methods gave three different answers during review). Returning the concrete
// type (exactly as NewProfileRepository already does) keeps the paged sweep method on the concrete
// repository plus the ONE narrow consumer interface that needs it
// (jobs.LinkedItemCleanupRepository), which is the placement the sibling scheduled-deletion fix
// established. Every existing caller assigning the result to a LinkedItemRepository variable or
// parameter still compiles unchanged — and the package-level `var _ LinkedItemRepository =
// (*linkedItemRepository)(nil)` assertion below keeps that a COMPILE-TIME guarantee rather than an
// incidental one, restoring the proof the old interface return type used to provide for free.
// Compile-time proof that the concrete type still satisfies the wide interface. Issue #1469 changed
// NewLinkedItemRepository's return type from LinkedItemRepository to *linkedItemRepository, which
// silently removed the compiler's own check that every interface method is implemented — a later
// signature drift would then only surface at whichever call site happens to assign the result to an
// interface. This restores it explicitly and costs nothing at runtime.
var _ LinkedItemRepository = (*linkedItemRepository)(nil)

func NewLinkedItemRepository(db DB, enc *appcrypto.FieldEncryptor) *linkedItemRepository { //nolint:revive // deliberate: see doc comment (mirrors NewProfileRepository)
	return &linkedItemRepository{db: db, enc: enc}
}

// encryptLinkedItemPIIFields encrypts PII fields on the item before writing to the database.
// It computes blind indexes from plaintext values (before encryption).
// The access token is encrypted from item.AccessToken (plaintext) into item.AccessTokenEncrypted.
// If item.AccessToken is empty but item.AccessTokenEncrypted is set (legacy), the existing
// encrypted value is preserved.
func (r *linkedItemRepository) encryptLinkedItemPIIFields(item *domain.LinkedItem) (itemIDBlind, institutionIDBlind string, err error) {
	// Compute blind index from plaintext BEFORE encrypting (order matters)
	itemIDBlind = r.enc.BlindIndex(item.ItemID)
	if item.InstitutionID != "" {
		institutionIDBlind = r.enc.BlindIndex(item.InstitutionID)
	}

	// Encrypt item_id
	encItemID, err := r.enc.EncryptString(item.ItemID)
	if err != nil {
		return "", "", fmt.Errorf("encrypt item_id: %w", err)
	}
	item.ItemID = encItemID

	// Encrypt institution_id (if set)
	if item.InstitutionID != "" {
		encInstID, err := r.enc.EncryptString(item.InstitutionID)
		if err != nil {
			return "", "", fmt.Errorf("encrypt institution_id: %w", err)
		}
		item.InstitutionID = encInstID
	}

	// Encrypt institution_name (if set)
	if item.InstitutionName != "" {
		encInstName, err := r.enc.EncryptString(item.InstitutionName)
		if err != nil {
			return "", "", fmt.Errorf("encrypt institution_name: %w", err)
		}
		item.InstitutionName = encInstName
	}

	// Encrypt access token: prefer plaintext AccessToken, fall back to existing encrypted value
	if item.AccessToken != "" {
		encToken, err := r.enc.EncryptString(item.AccessToken)
		if err != nil {
			return "", "", fmt.Errorf("encrypt access_token: %w", err)
		}
		item.AccessTokenEncrypted = encToken
	}
	// If item.AccessToken == "" and item.AccessTokenEncrypted != "", keep existing encrypted value (legacy)

	return itemIDBlind, institutionIDBlind, nil
}

// decryptLinkedItemPIIFields decrypts PII fields on the item after reading from the database.
// It also populates item.AccessToken by decrypting item.AccessTokenEncrypted.
func (r *linkedItemRepository) decryptLinkedItemPIIFields(item *domain.LinkedItem) error {
	// Decrypt item_id
	plainItemID, err := r.enc.DecryptString(item.ItemID)
	if err != nil {
		return fmt.Errorf("decrypt item_id: %w", err)
	}
	item.ItemID = plainItemID

	// Decrypt institution_id
	if item.InstitutionID != "" {
		plainInstID, err := r.enc.DecryptString(item.InstitutionID)
		if err != nil {
			return fmt.Errorf("decrypt institution_id: %w", err)
		}
		item.InstitutionID = plainInstID
	}

	// Decrypt institution_name
	if item.InstitutionName != "" {
		plainInstName, err := r.enc.DecryptString(item.InstitutionName)
		if err != nil {
			return fmt.Errorf("decrypt institution_name: %w", err)
		}
		item.InstitutionName = plainInstName
	}

	// Decrypt access token and populate AccessToken field
	if item.AccessTokenEncrypted != "" {
		plainToken, err := r.enc.DecryptString(item.AccessTokenEncrypted)
		if err != nil {
			return fmt.Errorf("decrypt access_token: %w", err)
		}
		item.AccessToken = plainToken
	}

	return nil
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
	item.StatusChangedAt = &now // Issue #1010: track when status was set

	// Encrypt PII fields and compute blind indexes before writing
	itemIDBlind, institutionIDBlind, err := r.encryptLinkedItemPIIFields(item)
	if err != nil {
		return fmt.Errorf("failed to encrypt linked item fields: %w", err)
	}

	query := `
		INSERT INTO linked_items
		(id, user_id, item_id, access_token_encrypted, institution_id, institution_name, provider, created_at, updated_at, item_id_blind_idx, institution_id_blind_idx, status_changed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`

	_, err = r.db.Exec(ctx, query,
		item.ID,
		item.UserID,
		item.ItemID,
		item.AccessTokenEncrypted,
		item.InstitutionID,
		item.InstitutionName,
		item.Provider,
		item.CreatedAt,
		item.UpdatedAt,
		itemIDBlind,
		institutionIDBlind,
		item.StatusChangedAt,
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
	query := `SELECT ` + linkedItemSelectCols + ` FROM linked_items WHERE id = $1`

	var item domain.LinkedItem
	var statusRaw, institutionIDRaw, institutionNameRaw *string
	err := r.db.QueryRow(ctx, query, id).Scan(
		&item.ID,
		&item.UserID,
		&item.ItemID,
		&item.AccessTokenEncrypted,
		&institutionIDRaw,
		&institutionNameRaw,
		&item.Provider,
		&item.Cursor,
		&item.LastSyncedAt,
		&statusRaw,
		&item.ErrorCode,
		&item.ErrorMessage,
		&item.LastErrorAt,
		&item.RetryCount,
		&item.NextRetryAt,
		&item.ReplacedAt,
		&item.ReplacedByID,
		&item.CreatedAt,
		&item.UpdatedAt,
		&item.InvestmentsLastSyncedDate,
		&item.LiabilitiesLastSyncedAt,
		&item.StatusChangedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrLinkedItemNotFound
	}
	if err != nil {
		return nil, err
	}
	item.InstitutionID = nilOrEmpty(institutionIDRaw)
	item.InstitutionName = nilOrEmpty(institutionNameRaw)
	item.Status = normalizeLinkedItemStatus(statusRaw)

	if err := r.decryptLinkedItemPIIFields(&item); err != nil {
		return nil, err
	}

	return &item, nil
}

// GetByUserID retrieves all linked items for a specific user
// Issue #171: Updated to include status, error tracking, and soft-delete fields
//
// Issue #1413 round-10 (closing most of #1444): this method has NO status predicate at all — it
// is reachable via the hard-deletion fallback (PlaidItemCleanupService.RemoveAllUserItems), which
// is exactly why a NULL-status row here used to panic ("cannot scan NULL into *string") instead of
// merely being misclassified: normalizeLinkedItemStatus (see its own doc comment) treats NULL as
// domain.LinkedItemStatusActive, matching GetActiveItemsByUserID/GetByID's established fix.
func (r *linkedItemRepository) GetByUserID(ctx context.Context, userID string) ([]domain.LinkedItem, error) {
	query := `SELECT ` + linkedItemSelectCols + ` FROM linked_items WHERE user_id = $1 ORDER BY created_at DESC`

	rows, err := r.db.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []domain.LinkedItem
	for rows.Next() {
		var item domain.LinkedItem
		var statusRaw, institutionIDRaw, institutionNameRaw *string
		err := rows.Scan(
			&item.ID,
			&item.UserID,
			&item.ItemID,
			&item.AccessTokenEncrypted,
			&institutionIDRaw,
			&institutionNameRaw,
			&item.Provider,
			&item.Cursor,
			&item.LastSyncedAt,
			&statusRaw,
			&item.ErrorCode,
			&item.ErrorMessage,
			&item.LastErrorAt,
			&item.RetryCount,
			&item.NextRetryAt,
			&item.ReplacedAt,
			&item.ReplacedByID,
			&item.CreatedAt,
			&item.UpdatedAt,
			&item.InvestmentsLastSyncedDate,
			&item.LiabilitiesLastSyncedAt,
			&item.StatusChangedAt,
		)
		if err != nil {
			return nil, err
		}
		item.InstitutionID = nilOrEmpty(institutionIDRaw)
		item.InstitutionName = nilOrEmpty(institutionNameRaw)
		item.Status = normalizeLinkedItemStatus(statusRaw)
		if err := r.decryptLinkedItemPIIFields(&item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	if len(items) == 0 {
		return []domain.LinkedItem{}, nil
	}
	return items, nil
}

// GetByItemID retrieves a linked item by its Plaid item ID
//
// Issue #1413 round-10 (#1444): NULL-safe status scan (see normalizeLinkedItemStatus) — this
// method has no status predicate, so a genuinely NULL-status row is reachable here.
func (r *linkedItemRepository) GetByItemID(ctx context.Context, itemID string) (*domain.LinkedItem, error) {
	blindIdx := r.enc.BlindIndex(itemID)
	query := `SELECT ` + linkedItemSelectCols + ` FROM linked_items WHERE item_id_blind_idx = $1`

	var item domain.LinkedItem
	var statusRaw, institutionIDRaw, institutionNameRaw *string
	err := r.db.QueryRow(ctx, query, blindIdx).Scan(
		&item.ID,
		&item.UserID,
		&item.ItemID,
		&item.AccessTokenEncrypted,
		&institutionIDRaw,
		&institutionNameRaw,
		&item.Provider,
		&item.Cursor,
		&item.LastSyncedAt,
		&statusRaw,
		&item.ErrorCode,
		&item.ErrorMessage,
		&item.LastErrorAt,
		&item.RetryCount,
		&item.NextRetryAt,
		&item.ReplacedAt,
		&item.ReplacedByID,
		&item.CreatedAt,
		&item.UpdatedAt,
		&item.InvestmentsLastSyncedDate,
		&item.LiabilitiesLastSyncedAt,
		&item.StatusChangedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrLinkedItemNotFound
	}
	if err != nil {
		return nil, err
	}
	item.InstitutionID = nilOrEmpty(institutionIDRaw)
	item.InstitutionName = nilOrEmpty(institutionNameRaw)
	item.Status = normalizeLinkedItemStatus(statusRaw)

	if err := r.decryptLinkedItemPIIFields(&item); err != nil {
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

	// Encrypt PII fields and compute blind indexes before writing.
	// institution_id and institution_name are decrypted when read, so must be re-encrypted on write.
	// item_id is not updated here (set once on Create).
	institutionIDBlind := ""
	if item.InstitutionID != "" {
		institutionIDBlind = r.enc.BlindIndex(item.InstitutionID)
		encInstID, err := r.enc.EncryptString(item.InstitutionID)
		if err != nil {
			return fmt.Errorf("encrypt institution_id: %w", err)
		}
		item.InstitutionID = encInstID
	}
	if item.InstitutionName != "" {
		encInstName, err := r.enc.EncryptString(item.InstitutionName)
		if err != nil {
			return fmt.Errorf("encrypt institution_name: %w", err)
		}
		item.InstitutionName = encInstName
	}

	query := `
		UPDATE linked_items
		SET institution_id = $2,
		    institution_id_blind_idx = $3,
		    institution_name = $4,
		    provider = $5,
		    cursor = $6,
		    last_synced_at = $7,
		    status = $8,
		    error_code = $9,
		    error_message = $10,
		    last_error_at = $11,
		    retry_count = $12,
		    next_retry_at = $13,
		    replaced_at = $14,
		    replaced_by_id = $15,
		    updated_at = $16,
		    status_changed_at = $17
		WHERE id = $1
	`

	ct, err := r.db.Exec(ctx, query,
		item.ID,
		item.InstitutionID,
		institutionIDBlind,
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
		item.StatusChangedAt,
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

// Delete removes a linked item from the database.
//
// Safety: only deletes if no accounts (any status) still reference this item.
// The accounts.linked_item_id FK is ON DELETE CASCADE — deleting while replaced
// accounts still exist would cascade-delete historical balance snapshots.
// This guard is a no-op (returns nil, 0 rows affected) when accounts remain.
func (r *linkedItemRepository) Delete(ctx context.Context, id string) error {
	query := `
		DELETE FROM linked_items
		WHERE id = $1
		AND NOT EXISTS (SELECT 1 FROM accounts WHERE linked_item_id = $1)
	`
	_, err := r.db.Exec(ctx, query, id)
	return err
}

// GetByIDAndUser retrieves a linked item by ID with ownership validation.
//
// Security: This method validates ownership via the WHERE clause (id AND user_id must match).
// A user can only retrieve items they own. If the item doesn't exist or belongs to
// a different user, sql.ErrNoRows is returned and mapped to ErrLinkedItemNotFound.
//
// This is used by handlers to ensure users can only access their own linked items.
// Issue #171: Updated to include status, error tracking, and soft-delete fields
//
// Issue #1413 round-10 (#1444): NULL-safe status scan (see normalizeLinkedItemStatus) — this
// method has no status predicate, so a genuinely NULL-status row is reachable here.
func (r *linkedItemRepository) GetByIDAndUser(ctx context.Context, id, userID string) (*domain.LinkedItem, error) {
	query := `SELECT ` + linkedItemSelectCols + ` FROM linked_items WHERE id = $1 AND user_id = $2`

	var item domain.LinkedItem
	var statusRaw, institutionIDRaw, institutionNameRaw *string
	err := r.db.QueryRow(ctx, query, id, userID).Scan(
		&item.ID,
		&item.UserID,
		&item.ItemID,
		&item.AccessTokenEncrypted,
		&institutionIDRaw,
		&institutionNameRaw,
		&item.Provider,
		&item.Cursor,
		&item.LastSyncedAt,
		&statusRaw,
		&item.ErrorCode,
		&item.ErrorMessage,
		&item.LastErrorAt,
		&item.RetryCount,
		&item.NextRetryAt,
		&item.ReplacedAt,
		&item.ReplacedByID,
		&item.CreatedAt,
		&item.UpdatedAt,
		&item.InvestmentsLastSyncedDate,
		&item.LiabilitiesLastSyncedAt,
		&item.StatusChangedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrLinkedItemNotFound
	}
	if err != nil {
		return nil, err
	}
	item.InstitutionID = nilOrEmpty(institutionIDRaw)
	item.InstitutionName = nilOrEmpty(institutionNameRaw)
	item.Status = normalizeLinkedItemStatus(statusRaw)

	if err := r.decryptLinkedItemPIIFields(&item); err != nil {
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
//
// Issue #1413 round-10 (#1444): NULL-safe status scan (see normalizeLinkedItemStatus) — this
// method has no status predicate, so a genuinely NULL-status row is reachable here.
func (r *linkedItemRepository) GetByIDForUpdate(ctx context.Context, tx pgx.Tx, id string) (*domain.LinkedItem, error) {
	query := `SELECT ` + linkedItemSelectCols + ` FROM linked_items WHERE id = $1 FOR UPDATE`

	var item domain.LinkedItem
	var statusRaw, institutionIDRaw, institutionNameRaw *string
	err := tx.QueryRow(ctx, query, id).Scan(
		&item.ID,
		&item.UserID,
		&item.ItemID,
		&item.AccessTokenEncrypted,
		&institutionIDRaw,
		&institutionNameRaw,
		&item.Provider,
		&item.Cursor,
		&item.LastSyncedAt,
		&statusRaw,
		&item.ErrorCode,
		&item.ErrorMessage,
		&item.LastErrorAt,
		&item.RetryCount,
		&item.NextRetryAt,
		&item.ReplacedAt,
		&item.ReplacedByID,
		&item.CreatedAt,
		&item.UpdatedAt,
		&item.InvestmentsLastSyncedDate,
		&item.LiabilitiesLastSyncedAt,
		&item.StatusChangedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrLinkedItemNotFound
	}
	if err != nil {
		return nil, err
	}
	item.InstitutionID = nilOrEmpty(institutionIDRaw)
	item.InstitutionName = nilOrEmpty(institutionNameRaw)
	item.Status = normalizeLinkedItemStatus(statusRaw)

	if err := r.decryptLinkedItemPIIFields(&item); err != nil {
		return nil, err
	}

	return &item, nil
}

// GetByUserAndInstitution retrieves a linked item by user ID, institution ID, and status.
// This is used for re-link detection to find existing active items at the same institution.
// Issue #171: Plaid Re-Link Handling
//
// Returns nil, nil if no matching item found (not an error).
//
// Issue #1413 round-10 (#1444): NULL-safe status scan (see normalizeLinkedItemStatus) for
// consistency with every other scan site in this file, even though the `status = $3` predicate
// above structurally excludes a NULL-status row already (SQL's `NULL = 'x'` is never TRUE).
func (r *linkedItemRepository) GetByUserAndInstitution(ctx context.Context, userID, institutionID, status string) (*domain.LinkedItem, error) {
	institutionIDBlind := r.enc.BlindIndex(institutionID)
	query := `SELECT ` + linkedItemSelectCols + ` FROM linked_items WHERE user_id = $1 AND institution_id_blind_idx = $2 AND status = $3 ORDER BY created_at DESC LIMIT 1`

	var item domain.LinkedItem
	var statusRaw, institutionIDRaw, institutionNameRaw *string
	err := r.db.QueryRow(ctx, query, userID, institutionIDBlind, status).Scan(
		&item.ID,
		&item.UserID,
		&item.ItemID,
		&item.AccessTokenEncrypted,
		&institutionIDRaw,
		&institutionNameRaw,
		&item.Provider,
		&item.Cursor,
		&item.LastSyncedAt,
		&statusRaw,
		&item.ErrorCode,
		&item.ErrorMessage,
		&item.LastErrorAt,
		&item.RetryCount,
		&item.NextRetryAt,
		&item.ReplacedAt,
		&item.ReplacedByID,
		&item.CreatedAt,
		&item.UpdatedAt,
		&item.InvestmentsLastSyncedDate,
		&item.LiabilitiesLastSyncedAt,
		&item.StatusChangedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		// No match found - this is not an error, just no existing item
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	item.InstitutionID = nilOrEmpty(institutionIDRaw)
	item.InstitutionName = nilOrEmpty(institutionNameRaw)
	item.Status = normalizeLinkedItemStatus(statusRaw)

	if err := r.decryptLinkedItemPIIFields(&item); err != nil {
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
//
// Issue #1413 round-10 (#1444): NULL-safe status scan (see normalizeLinkedItemStatus) for
// consistency with every other scan site in this file, even though `status NOT IN (...)` above
// already structurally excludes a NULL-status row (SQL three-valued logic).
//
// Issue #1469: this query is UNBOUNDED — an ORDER BY but no LIMIT, and no id tie-breaker. It has NO
// remaining production caller: ItemCleanupCheckWorker.Work (the only one it ever had) now calls
// GetItemsForCleanupPage below. It is retained ONLY because it is still declared on the wide
// LinkedItemRepository interface, which several test files' mocks implement (enumerate them with
// `grep -rn '^func (m \*[A-Za-z]*) GetItemsForCleanup(' internal/` and count what it prints — no
// total is asserted here, because three counting methods gave three answers during review), and because
// TestGetItemsForCleanupPage_Integration_MatchesUnpagedPredicate uses it as the oracle that the
// paged form did not drop a predicate leg. Removing it is recommended as a separate change — see
// GetItemsForCleanupPage's doc comment.
func (r *linkedItemRepository) GetItemsForCleanup(ctx context.Context, errorDays, staleDays int) ([]domain.LinkedItem, error) {
	query := cleanupCandidateSelectQuery + `
		ORDER BY li.last_error_at ASC NULLS LAST, li.last_synced_at ASC NULLS LAST
	`
	return r.queryCleanupCandidates(ctx, query,
		strings.TrimSpace(fmt.Sprintf("%d days", errorDays)),
		strings.TrimSpace(fmt.Sprintf("%d days", staleDays)),
	)
}

// cleanupCandidateSelectQuery is the SELECT + WHERE shared verbatim by GetItemsForCleanup and
// GetItemsForCleanupPage (Issue #1469), so the paged form cannot drift from the predicate it is
// replacing. $1 is the errorDays interval, $2 the staleDays interval; a caller appending its own
// clauses must start numbering at $3.
const cleanupCandidateSelectQuery = `
	SELECT li.id, li.user_id, li.item_id, li.access_token_encrypted,
	       li.institution_id, li.institution_name, li.provider, li.cursor, li.last_synced_at,
	       li.status, li.error_code, li.error_message, li.last_error_at, li.retry_count, li.next_retry_at,
	       li.replaced_at, li.replaced_by_id,
	       li.cleanup_notified_at, li.cleanup_scheduled_at,
	       li.created_at, li.updated_at, li.investments_last_synced_date, li.liabilities_last_synced_at,
	       li.status_changed_at
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
`

// cleanupCandidateSortKey is the ASC-NULLS-LAST ordering of GetItemsForCleanup, re-expressed with
// COALESCE so the identical expression can appear inside a row-comparison keyset predicate — a
// tuple comparison has no per-column NULLS FIRST/LAST clause, so `(a, b, c) > (x, y, z)` cannot
// express "NULLs last" any other way. For timestamptz the two forms are equivalent: 'infinity'
// sorts after every finite value, and nothing in this codebase ever writes an infinite
// last_error_at/last_synced_at (every writer passes a Go time.Time or NULL).
//
// Coalescing is also what makes the row comparison usable at all: PostgreSQL row-wise comparison
// yields NULL (not true/false) as soon as a compared element is NULL, so a keyset predicate over the
// raw nullable columns would silently return no rows for any cursor whose components include a NULL.
// Both the column side (here) and the parameter side (GetItemsForCleanupPage) are coalesced.
//
// Every component is ASC; each is spelled out so a future edit cannot silently apply a single
// trailing ASC/DESC to the last column only.
const cleanupCandidateSortKey = `COALESCE(li.last_error_at, 'infinity'::timestamptz) ASC, COALESCE(li.last_synced_at, 'infinity'::timestamptz) ASC, li.id ASC`

// cleanupCandidateSortKeyTuple is cleanupCandidateSortKey's expressions WITHOUT the per-column ASC
// keywords, for use inside the row-comparison keyset predicate (a tuple element cannot carry an
// ASC/DESC keyword). Kept adjacent to its sibling so the two cannot drift.
const cleanupCandidateSortKeyTuple = `COALESCE(li.last_error_at, 'infinity'::timestamptz), COALESCE(li.last_synced_at, 'infinity'::timestamptz), li.id`

// itemCleanupSweepDefaultPageSize is the fallback for a non-positive limit passed to
// GetItemsForCleanupPage. Mirrors retentionSweepDefaultPageSize / GetUsersScheduledForDeletionPage's
// 500.
const itemCleanupSweepDefaultPageSize = 500

// ItemCleanupSweepCursor is a keyset-pagination position for GetItemsForCleanupPage, captured from
// the last row of the previous page, matching that page's
// ORDER BY (last_error_at, last_synced_at, id) ASC with NULLs last. A nil cursor starts from the
// beginning.
//
// LastErrorAt and LastSyncedAt are POINTERS because both columns are nullable and a NULL is a
// legitimate cursor position: the "zero active accounts" leg of the predicate matches items that
// have never errored and never synced, so a page can genuinely end on a row where both are NULL.
// A nil field is sent as SQL NULL and coalesced to 'infinity' by the query, which is exactly where
// NULLS LAST puts it.
type ItemCleanupSweepCursor struct {
	LastErrorAt  *time.Time
	LastSyncedAt *time.Time
	ItemID       string
}

// GetItemsForCleanupPage returns up to `limit` cleanup candidates, ordered by
// (last_error_at, last_synced_at, id) ASC with NULLs last, for deterministic keyset pagination —
// Issue #1469. The WHERE clause is GetItemsForCleanup's, unchanged
// (cleanupCandidateSelectQuery), plus the keyset predicate.
//
// Why this exists: GetItemsForCleanup has no LIMIT, and its single caller,
// ItemCleanupCheckWorker.Work (queue/jobs/item_cleanup_check.go), consumed the whole slice — marking
// every row for cleanup and enqueueing an ItemCleanup job per row, which calls Plaid /item/remove.
// That is irreversible (the user must re-link the institution through Plaid Link), and
// ItemCleanupCheckPeriodicOpts registers the job with RunOnStart: true, so the first run after every
// deploy acted on however many rows the table happened to hold. Same defect class as #1478 and as
// the already-fixed GetUsersScheduledForDeletionPage.
//
// Why the li.id tie-breaker is load-bearing here specifically: BOTH pre-existing sort columns are
// nullable, and the "zero active accounts" predicate leg matches items that have never errored and
// never synced — so many rows can share (NULL, NULL) as their entire sort key. Keyset pagination
// over a non-unique sort key skips and repeats rows. li.id (a uuid, NOT NULL, unique) makes the
// order total. This is why the fix is not a bare `LIMIT n`, quite apart from the starvation argument.
//
// Why keyset pagination and not a bare LIMIT: an oldest-ranked row whose MarkForCleanup keeps
// failing stays a candidate (the predicate requires cleanup_scheduled_at IS NULL, and a failed mark
// leaves it NULL), so under a bare LIMIT the same oldest n rows would occupy every page of every run
// and nothing ranked past them would ever be looked at. The cursor lets the caller step past
// whatever it just saw within ONE run.
//
// The guarantee this buys, stated bounded by the cap (do NOT restate it as "no starvation" — that
// claim shipped on maxUsersPerDeletionSweep and is only true below the cap): within a single run the
// caller advances across pages until the repository returns a short page (exhausted) or the caller's
// total-processed cap (maxItemsPerCleanupSweep, queue/jobs/item_cleanup_check.go) is reached. Across
// runs there is NO persisted cursor (#1477) — every run restarts from nil. So while the number of
// candidates that are NOT getting marked stays BELOW the cap, each run steps past them and later
// rows are reached; at or above that count, every run re-spends its entire budget on the same
// unmarkable oldest rows and never reaches anything past the cap, for as long as they keep failing.
// Successfully marked rows do not have this problem: MarkForCleanup sets cleanup_scheduled_at, after
// which the row does not match this predicate at all.
//
// No index can serve this sort key, exactly as none could serve GetItemsForCleanup's: the closest
// one, idx_linked_items_cleanup_candidates (status, last_error_at) WHERE status IN
// ('error','login_required','revoked') AND cleanup_scheduled_at IS NULL, is partial on a status set
// this predicate deliberately exceeds (the stale and zero-active-accounts legs match other statuses)
// and leads on status, not on the sort expression — so each page is a fresh scan-and-sort. That is
// unchanged by this fix, not introduced by it, and linked_items is a small table (roughly one row
// per user per linked institution). It is worth a dedicated expression index if this table ever
// grows large.
func (r *linkedItemRepository) GetItemsForCleanupPage(ctx context.Context, errorDays, staleDays int, after *ItemCleanupSweepCursor, limit int) ([]domain.LinkedItem, error) {
	if limit <= 0 {
		limit = itemCleanupSweepDefaultPageSize
	}

	args := []interface{}{
		strings.TrimSpace(fmt.Sprintf("%d days", errorDays)),
		strings.TrimSpace(fmt.Sprintf("%d days", staleDays)),
	}

	query := cleanupCandidateSelectQuery
	if after != nil {
		// COALESCE on the parameter side too, so a NULL cursor component compares as 'infinity' —
		// the same position NULLS LAST gives it in the ORDER BY.
		query += `
		  AND (` + cleanupCandidateSortKeyTuple + `) > (
		      COALESCE($3::timestamptz, 'infinity'::timestamptz),
		      COALESCE($4::timestamptz, 'infinity'::timestamptz),
		      $5::uuid
		  )
	`
		args = append(args, after.LastErrorAt, after.LastSyncedAt, after.ItemID)
	}
	query += `
		ORDER BY ` + cleanupCandidateSortKey + `
		LIMIT $` + strconv.Itoa(len(args)+1) + `
	`
	args = append(args, limit)

	return r.queryCleanupCandidates(ctx, query, args...)
}

// queryCleanupCandidates runs a cleanupCandidateSelectQuery-shaped query and scans every row into a
// domain.LinkedItem, applying the same NULL-safe institution/status handling and PII decryption both
// cleanup queries need. Extracted (Issue #1469) so GetItemsForCleanup and GetItemsForCleanupPage
// cannot drift in their scan order or their decryption behaviour.
func (r *linkedItemRepository) queryCleanupCandidates(ctx context.Context, query string, args ...interface{}) ([]domain.LinkedItem, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []domain.LinkedItem
	for rows.Next() {
		var item domain.LinkedItem
		var statusRaw, institutionIDRaw, institutionNameRaw *string
		err := rows.Scan(
			&item.ID,
			&item.UserID,
			&item.ItemID,
			&item.AccessTokenEncrypted,
			&institutionIDRaw,
			&institutionNameRaw,
			&item.Provider,
			&item.Cursor,
			&item.LastSyncedAt,
			&statusRaw,
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
			&item.InvestmentsLastSyncedDate,
			&item.LiabilitiesLastSyncedAt,
			&item.StatusChangedAt,
		)
		if err != nil {
			return nil, err
		}
		item.InstitutionID = nilOrEmpty(institutionIDRaw)
		item.InstitutionName = nilOrEmpty(institutionNameRaw)
		item.Status = normalizeLinkedItemStatus(statusRaw)
		if err := r.decryptLinkedItemPIIFields(&item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	if len(items) == 0 {
		return []domain.LinkedItem{}, nil
	}
	return items, nil
}

// GetItemsPendingCleanup retrieves items where cleanup is scheduled and past due.
//
// Issue #1413 round-10 (#1444): NULL-safe status scan (see normalizeLinkedItemStatus) for
// consistency with every other scan site in this file, even though `status NOT IN (...)` above
// already structurally excludes a NULL-status row (SQL three-valued logic).
func (r *linkedItemRepository) GetItemsPendingCleanup(ctx context.Context) ([]domain.LinkedItem, error) {
	query := `
		SELECT id, user_id, item_id, access_token_encrypted,
		       institution_id, institution_name, provider, cursor, last_synced_at,
		       status, error_code, error_message, last_error_at, retry_count, next_retry_at,
		       replaced_at, replaced_by_id,
		       cleanup_notified_at, cleanup_scheduled_at,
		       created_at, updated_at, investments_last_synced_date, liabilities_last_synced_at,
		       status_changed_at
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
		var statusRaw, institutionIDRaw, institutionNameRaw *string
		err := rows.Scan(
			&item.ID,
			&item.UserID,
			&item.ItemID,
			&item.AccessTokenEncrypted,
			&institutionIDRaw,
			&institutionNameRaw,
			&item.Provider,
			&item.Cursor,
			&item.LastSyncedAt,
			&statusRaw,
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
			&item.InvestmentsLastSyncedDate,
			&item.LiabilitiesLastSyncedAt,
			&item.StatusChangedAt,
		)
		if err != nil {
			return nil, err
		}
		item.InstitutionID = nilOrEmpty(institutionIDRaw)
		item.InstitutionName = nilOrEmpty(institutionNameRaw)
		item.Status = normalizeLinkedItemStatus(statusRaw)
		if err := r.decryptLinkedItemPIIFields(&item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	if len(items) == 0 {
		return []domain.LinkedItem{}, nil
	}
	return items, nil
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

// MarkAllUserItemsDisconnected marks all linked items for a user as 'disconnected'.
// This preserves transaction history while indicating the Plaid connection is no longer active.
// Issue #679, #680: Trial expiration
//
// Issue #1413 round-8 Blocker 1: WHERE guard treats a NULL status as live too, matching
// GetActiveItemsByUserID/MarkItemsDisconnected below and GetUsersNeedingPlaidDisconnectRepair
// (profile_repository.go) — a bare `status NOT IN (...)` evaluates to SQL's UNKNOWN (not TRUE) for
// a NULL status, silently leaving such a row untouched.
func (r *linkedItemRepository) MarkAllUserItemsDisconnected(ctx context.Context, userID string) error {
	now := time.Now()

	query := `
		UPDATE linked_items
		SET status = 'disconnected',
		    updated_at = $1,
		    status_changed_at = $1
		WHERE user_id = $2
		  AND (status IS NULL OR status NOT IN ('disconnected', 'replaced'))
	`

	_, err := r.db.Exec(ctx, query, now, userID)
	return err
}

// MarkItemsDisconnected marks only the given linked items as 'disconnected' — see the interface
// doc comment for why this differs from MarkAllUserItemsDisconnected (Issue #1413 H2).
//
// Issue #1413 round-8 Blocker 1: WHERE guard treats a NULL status as live too — see
// MarkAllUserItemsDisconnected's doc comment above for why a bare `status NOT IN (...)` silently
// excludes a NULL-status row under SQL three-valued logic.
//
// Issue #1413 Medium finding (PR #1443 Codex review round 4): the UPDATE's RowsAffected is now
// inspected — previously only the query error was returned, so a write that matched zero rows
// (every given id already excluded by the WHERE guard above) succeeded silently, indistinguishable
// from a genuine write to both a caller and a test. See ErrNoLinkedItemsUpdated's doc comment for
// why this is surfaced as an errors.Is-checkable sentinel rather than folded into a hard failure.
func (r *linkedItemRepository) MarkItemsDisconnected(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	now := time.Now()

	query := `
		UPDATE linked_items
		SET status = 'disconnected',
		    updated_at = $1,
		    status_changed_at = $1
		WHERE id = ANY($2)
		  AND (status IS NULL OR status NOT IN ('disconnected', 'replaced'))
	`

	tag, err := r.db.Exec(ctx, query, now, ids)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: 0 of %d given id(s) matched", ErrNoLinkedItemsUpdated, len(ids))
	}
	return nil
}

// GetActiveItemsByUserID retrieves all active (non-replaced, non-disconnected) linked items for a user
//
// Issue #1413 round-8 Blocker 1: the WHERE guard now treats a NULL status as live too, matching
// GetUsersNeedingPlaidDisconnectRepair's (profile_repository.go) NULL-aware predicate. Before this
// fix, a bare `status NOT IN (...)` silently excluded a NULL-status row (SQL three-valued logic:
// NULL NOT IN (...) is UNKNOWN, not TRUE) — the repair query would select the user, but this fetch
// would return zero items, so PlaidItemCleanupService.cleanupItems reported success and the item
// leaked forever, re-selected by the repair query every day with no effect. See
// TestPlaidCleanupService_Integration_NullStatusItem_ActuallyDisconnectedAndMarked
// (internal/services) for the end-to-end proof.
func (r *linkedItemRepository) GetActiveItemsByUserID(ctx context.Context, userID string) ([]domain.LinkedItem, error) {
	query := `SELECT ` + linkedItemSelectCols + ` FROM linked_items WHERE user_id = $1 AND (status IS NULL OR status NOT IN ('disconnected', 'replaced')) ORDER BY created_at DESC`

	rows, err := r.db.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []domain.LinkedItem
	for rows.Next() {
		var item domain.LinkedItem
		var statusRaw, institutionIDRaw, institutionNameRaw *string
		err := rows.Scan(
			&item.ID,
			&item.UserID,
			&item.ItemID,
			&item.AccessTokenEncrypted,
			&institutionIDRaw,
			&institutionNameRaw,
			&item.Provider,
			&item.Cursor,
			&item.LastSyncedAt,
			&statusRaw,
			&item.ErrorCode,
			&item.ErrorMessage,
			&item.LastErrorAt,
			&item.RetryCount,
			&item.NextRetryAt,
			&item.ReplacedAt,
			&item.ReplacedByID,
			&item.CreatedAt,
			&item.UpdatedAt,
			&item.InvestmentsLastSyncedDate,
			&item.LiabilitiesLastSyncedAt,
			&item.StatusChangedAt,
		)
		if err != nil {
			return nil, err
		}
		item.InstitutionID = nilOrEmpty(institutionIDRaw)
		item.InstitutionName = nilOrEmpty(institutionNameRaw)
		item.Status = normalizeLinkedItemStatus(statusRaw)
		if err := r.decryptLinkedItemPIIFields(&item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	if len(items) == 0 {
		return []domain.LinkedItem{}, nil
	}
	return items, nil
}

// UpdateInvestmentsLastSyncedDate updates the date through which investment transactions
// have been synced. Called after a successful investment transactions sync (Issue #659).
func (r *linkedItemRepository) UpdateInvestmentsLastSyncedDate(ctx context.Context, id string, date time.Time) error {
	query := `UPDATE linked_items SET investments_last_synced_date = $1 WHERE id = $2`
	ct, err := r.db.Exec(ctx, query, date, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrLinkedItemNotFound
	}
	return nil
}

// UpdateLiabilitiesLastSyncedAt updates the liabilities last synced timestamp.
// Called after a successful liabilities sync (Issue #910).
func (r *linkedItemRepository) UpdateLiabilitiesLastSyncedAt(ctx context.Context, id string, t time.Time) error {
	query := `UPDATE linked_items SET liabilities_last_synced_at = $1 WHERE id = $2`
	ct, err := r.db.Exec(ctx, query, t, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrLinkedItemNotFound
	}
	return nil
}
