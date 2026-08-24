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
	// ErrLinkedItemNotDisconnected is returned by PurgeLinkedItem when the item's Plaid side has
	// not yet been stopped (plaid_removed_at IS NULL). Purging first would permanently destroy the
	// only local record of a Plaid Item that may still be billing, with no way to find or remove
	// it afterward — so a purge must be preceded by a successful disconnect (POST
	// /linked-items/{id}/disconnect), exactly as the closed-accounts surface always reaches it.
	// Issue #1569, Task 6.
	ErrLinkedItemNotDisconnected = errors.New("institution must be disconnected before it can be purged")
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
	// Issue #1495: also returned by MarkPlaidRemoved, whose zero-row case means something DIFFERENT
	// (the ids do not exist — that method has no status guard that could filter a live row out), so
	// the message is worded method-neutrally rather than naming MarkItemsDisconnected. The two
	// callers wrap it with their own method name and count, which is what a log actually shows.
	ErrNoLinkedItemsUpdated = errors.New("linked_items update affected zero rows (given ids already terminal, or no longer present?)")
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

	// PurgeLinkedItem permanently and transactionally destroys the linked item and every account
	// under it: balance_snapshots for those accounts (explicit — SET NULL, migration 000150), then
	// the linked_items row itself, which CASCADEs to its accounts (fk_accounts_linked_item_id,
	// unchanged since creation) and from there to holdings/investment_transactions/liabilities/
	// account_status_history for each. transactions survive (SET NULL on both account_id and
	// linked_item_id, migration 000035). Issue #1569, Task 6.
	//
	// Refuses with ErrLinkedItemNotDisconnected unless BOTH plaid_removed_at is already stamped
	// AND no account under the item is still status='active'. A purge must never be the FIRST
	// thing that happens to an item, or it destroys the only local record of a Plaid Item that
	// might still be billing; and the stamp alone is not proof the user disconnected anything,
	// because the billing cleanup sweep stamps errored/stale items without touching their
	// accounts (#1569 residual E9 — see the implementation for the full argument). Call the
	// disconnect endpoint first.
	//
	// Refuses with ErrPurgeBlockedBySavingsGoal, doing and changing nothing, while a savings goal
	// still references any account under this item (migration 000150's RESTRICT constraint).
	//
	// auditLog, when non-nil, writes a "linked_item.history_purged" entry inside the same
	// transaction via LogInTx — see AccountRepository.PurgeAccount's doc comment for why that is
	// safe here (no FOR UPDATE lock on any users row).
	//
	// Returns ErrLinkedItemNotFound if the item doesn't exist or isn't owned by userID.
	PurgeLinkedItem(ctx context.Context, linkedItemID, userID string, auditLog AuditLogRepository) error

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

	// GetItemsPendingCleanup retrieves items where cleanup is scheduled and past due —
	// cleanup_scheduled_at IS NOT NULL AND < NOW(), excluding 'disconnected' ONLY. It is the
	// stranded-cleanup backstop's selection query (Issue #1486 follow-up part C). 'replaced' is
	// deliberately INCLUDED (such an item is still live and still billing at Plaid) while
	// 'disconnected' is deliberately excluded; see the implementation's doc comment, which is where
	// that asymmetry with the sweep's candidacy predicate is argued.
	GetItemsPendingCleanup(ctx context.Context) ([]domain.LinkedItem, error)

	// MarkForCleanup marks an item for cleanup with notification and scheduled cleanup timestamp.
	MarkForCleanup(ctx context.Context, itemID string, cleanupAt time.Time) error

	// ClearCleanupSchedule clears the cleanup schedule for an item. NOT called on reconnection:
	// repairing an item through Plaid Link update mode leaves cleanup_scheduled_at set. See
	// cleanupCandidateReasonPredicate for the full list of production callers and why that matters.
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

	// MarkPlaidRemoved stamps linked_items.plaid_removed_at (migration 000131) for the given
	// SPECIFIC linked items — the durable record that Plaid /item/remove has succeeded for them
	// (including Plaid answering ITEM_NOT_FOUND, which means the Item is already gone and billing
	// has already stopped). Issue #1495.
	//
	// WHY THIS EXISTS ALONGSIDE MarkItemsDisconnected, which looks like it already says this.
	// It does not, for one row shape, and that row shape is the defect: MarkItemsDisconnected's
	// WHERE guard REFUSES to overwrite 'replaced' (deliberately — overwriting it would destroy the
	// replaced_at/replaced_by_id re-link chain; pinned by
	// TestLinkedItemRepository_MarkItemsDisconnected_AlreadyReplacedIsNotReverted), yet a
	// 'replaced' item's Plaid Item is STILL LIVE AND STILL BILLING because the re-link path
	// (services/account_service.go) never calls /item/remove. status is a single slot and the
	// re-link fact owns it, so before this column there was nowhere to record "Plaid is done" for
	// such a row. This method has NO status filter at all and therefore CAN stamp a 'replaced'
	// row — that is the entire point. See migration 000131's comment for the full rationale.
	//
	// Idempotent, and the FIRST stamp wins: the value is written as
	// COALESCE(plaid_removed_at, <now>), so a re-run neither errors nor moves an
	// already-recorded instant later. A nil or empty ids is a no-op (not an error).
	//
	// Returns ErrNoLinkedItemsUpdated (errors.Is-checkable) if the UPDATE matched zero rows despite
	// a non-empty ids. Unlike MarkItemsDisconnected, where zero rows is the ROUTINE
	// already-terminal case, zero rows here can only mean the ids do not exist (the row was
	// deleted concurrently) — there is no status guard left to filter them out. Callers must still
	// not escalate it into a permanent retry loop; see RemoveItem/cleanupItems for how each treats
	// it.
	MarkPlaidRemoved(ctx context.Context, ids []string) error

	// MarkPlaidRemovalFailed records, on the given items, that a Plaid /item/remove attempt failed
	// with an error we judge UNRECOVERABLE — writing plaid_removal_failed_at and
	// plaid_removal_error_code (migration 000133). Issue #1494.
	//
	// THIS IS THE OPPOSITE FACT FROM MarkPlaidRemoved, not a variant of it, and conflating the two
	// would recreate the defect #1495 closed. plaid_removed_at means "the Plaid side is DONE, billing
	// has stopped". This means "the credential we hold cannot remove this Item, so retrying with it
	// cannot change the outcome" — the Item may still exist and may still bill. The only thing this
	// mark is allowed to unlock is DeleteUserCascade's C2 guard (uncleanedLinkedItemExistsClause),
	// and only because the cascade first copies the row into plaid_orphaned_items so the obligation
	// survives the erasure. It must never be written for an item that might yet be removable: see
	// services.terminalPlaidRemovalFailure for the deliberately narrow code set, and for why
	// anything unrecognised stays recoverable.
	//
	// SPLIT SEMANTICS, and the timestamp half is load-bearing. plaid_removal_failed_at is written
	// COALESCE(existing, now) — FIRST occurrence wins, exactly like MarkPlaidRemoved — because C2 does
	// not unblock on the mark's presence but on the verdict having PERSISTED for a cooling-off window
	// (uncleanedLinkedItemPredicate). Overwriting it on every attempt would push that deadline forward on
	// each daily retry and the guard would block forever, which is #1494 again with extra steps.
	// plaid_removal_error_code IS overwritten, so it names the most recent code — that one has no history
	// worth keeping and an operator wants the latest answer Plaid gave.
	//
	// GUARDED BY `plaid_removed_at IS NULL`: a row already proved removed must never acquire a
	// failure verdict afterwards (a late-arriving error from a retry that raced a success would
	// otherwise contradict the stronger fact). This makes the zero-row case AMBIGUOUS BY DESIGN, and
	// callers must treat it as benign: it means either the row no longer exists, or it has already
	// been proved removed. Both are outcomes in which no caller needs this mark.
	//
	// Returns ErrNoLinkedItemsUpdated (errors.Is-checkable) for that zero-row case, despite a
	// non-empty ids. A nil or empty ids is a no-op (not an error).
	MarkPlaidRemovalFailed(ctx context.Context, ids []string, errorCode string) error

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
// every mock implementation of it (enumerate them with
// `grep -rnE '^func \([a-zA-Z_]+ \*?[A-Za-z0-9_]+\) GetItemsForCleanupPage\(' internal/`; no total is
// asserted here because three counting methods gave three different answers during review, and the
// receiver name is not always `m`). Returning the concrete
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

// PurgeLinkedItem implements LinkedItemRepository.PurgeLinkedItem — see that method's doc comment
// for the full contract.
func (r *linkedItemRepository) PurgeLinkedItem(ctx context.Context, linkedItemID, userID string, auditLog AuditLogRepository) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after a successful commit is a no-op; its error is not actionable

	// Lock the row and read plaid_removed_at before doing anything destructive. FOR UPDATE here
	// is a row lock on linked_items, not users — it cannot participate in the users-row lock
	// cycle LogInTx's doc comment warns about.
	var plaidRemovedAt *time.Time
	err = tx.QueryRow(ctx,
		`SELECT plaid_removed_at FROM linked_items WHERE id = $1 AND user_id = $2 FOR UPDATE`,
		linkedItemID, userID,
	).Scan(&plaidRemovedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrLinkedItemNotFound
		}
		return fmt.Errorf("purge linked item %s: %w", linkedItemID, err)
	}
	if plaidRemovedAt == nil {
		return ErrLinkedItemNotDisconnected
	}

	// ⚠ plaid_removed_at is not by itself proof the USER disconnected this institution — #1569
	// residual E9. The billing cleanup sweep stamps it too, for items that are merely in an error
	// state or stale (cleanupCandidateReasonPredicate above).
	//
	// ⚠⚠ WHAT THIS SECOND LEG DOES AND NO LONGER DOES (#1569 task 20, A2). It used to also mean
	// "a background job cannot reach this precondition", because PlaidItemCleanupService left every
	// account under a swept item at status='active' and so this count was never zero for one.
	// THAT IS NO LONGER TRUE, and deliberately so: leaving those accounts 'active' was a defect —
	// they had stopped syncing, so their balances were frozen figures rendered as current ones with
	// no "as of" date. The service now transitions them to 'removed' with
	// removed_reason = 'link_removed', which means a swept item CAN now satisfy both legs of this
	// guard without the user ever having pressed Disconnect.
	//
	// Accepted, with the reasoning stated rather than assumed. This guard is a PRECONDITION, not an
	// authorisation: nothing here is reachable except through the explicit, type-to-confirm
	// user-initiated purge operation, and the precondition it encodes ("the connection must be
	// stopped before its history may be destroyed") is genuinely satisfied for a swept item — the
	// Plaid Item is gone and cannot be revived. What changed is only that the user is no longer
	// asked to press Disconnect on a connection this product already severed. If a future task wants
	// "the USER stopped it" specifically, the queryable fact for that is now
	// accounts.removed_reason = 'user_disconnect' versus 'link_removed' (migration 000167), NOT the
	// account count — and it should be added as a third leg here rather than inferred.
	//
	// This is the institution-level equal of PurgeAccount's `status == active -> refuse` guard
	// (account_repository.go), and it shares its sentinel with the stamp check above because the
	// user-facing answer is identical: disconnect first. That is not a dead end —
	// DisconnectInstitution is idempotent against an already-removed Plaid item (ITEM_NOT_FOUND
	// maps to success) and marks the accounts removed on its way through. Note that its
	// MarkRemovedByLinkedItemID call is scoped `AND status = 'active'`, so running it over an
	// already-swept item leaves the existing 'link_removed' reasons in place rather than rewriting
	// them to 'user_disconnect' — the earlier, machine-written cause is the true one and is kept.
	//
	// Read inside the same transaction, after the FOR UPDATE on linked_items. That lock does not
	// cover the accounts rows, so a disconnect committing concurrently could still flip one to
	// 'removed' between this SELECT and the deletes — which only ever turns a permitted purge into
	// a refused one, never the reverse.
	var liveAccounts int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM accounts WHERE linked_item_id = $1 AND status = 'active'`,
		linkedItemID,
	).Scan(&liveAccounts); err != nil {
		return fmt.Errorf("purge linked item %s: counting live accounts: %w", linkedItemID, err)
	}
	if liveAccounts > 0 {
		return ErrLinkedItemNotDisconnected
	}

	// Both replaced_by_id columns are `REFERENCES ...(id)` with no explicit ON DELETE action
	// (defaults to NO ACTION, migrations 000032/000033). If an older item/account was replaced by
	// one under this item on a re-link, the deletes below would fail with a dangling foreign-key
	// violation as an opaque error. Mirrors DeleteUserCascade's identical fix
	// (profile_repository.go) for the same two columns, scoped here to just this item's accounts
	// and this item itself rather than a whole user. Nulling unconditionally is harmless: both
	// columns are nullable and carry no meaning once their target is gone.
	if _, err := tx.Exec(ctx, `
		UPDATE accounts SET replaced_by_id = NULL
		WHERE replaced_by_id IN (SELECT id FROM accounts WHERE linked_item_id = $1)
	`, linkedItemID); err != nil {
		return fmt.Errorf("purge linked item %s: clearing dangling accounts.replaced_by_id references: %w", linkedItemID, err)
	}
	if _, err := tx.Exec(ctx, `UPDATE linked_items SET replaced_by_id = NULL WHERE replaced_by_id = $1`, linkedItemID); err != nil {
		return fmt.Errorf("purge linked item %s: clearing dangling linked_items.replaced_by_id references: %w", linkedItemID, err)
	}

	if _, err := tx.Exec(ctx, `
		DELETE FROM balance_snapshots
		WHERE account_id IN (SELECT id FROM accounts WHERE linked_item_id = $1)
	`, linkedItemID); err != nil {
		return fmt.Errorf("purge linked item %s: deleting balance snapshots: %w", linkedItemID, err)
	}

	ct, err := tx.Exec(ctx, `DELETE FROM linked_items WHERE id = $1 AND user_id = $2`, linkedItemID, userID)
	if err != nil {
		if isForeignKeyViolation(err, "fk_savings_goals_account_id") {
			return ErrPurgeBlockedBySavingsGoal
		}
		return fmt.Errorf("purge linked item %s: %w", linkedItemID, err)
	}
	if ct.RowsAffected() == 0 {
		return ErrLinkedItemNotFound
	}

	if auditLog != nil {
		if err := auditLog.LogInTx(ctx, tx, "linked_item.history_purged", "linked_item", linkedItemID); err != nil {
			return fmt.Errorf("purge linked item %s: audit log: %w", linkedItemID, err)
		}
	}

	return tx.Commit(ctx)
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

// cleanupCandidateSelectQuery is the SELECT + WHERE of the cleanup sweep. It was extracted (Issue
// #1469) so the paged GetItemsForCleanupPage could not drift from the unbounded GetItemsForCleanup it
// replaced; that unbounded method was deleted in #1488, and GetItemsForCleanupPage is now the sole
// consumer — but the constant stays split out because ClaimItemForCleanup shares its inner
// cleanupCandidateReasonPredicate (#1486). $1 is the errorDays interval, $2 the staleDays interval; a
// caller appending its own clauses must start numbering at $3.
//
// Issue #1495: the single `li.status NOT IN ('replaced', 'disconnected')` clause was SPLIT into two,
// because it was doing two unrelated jobs under one spelling:
//
//   - the 'disconnected' half was an "is the Plaid side already done?" test, and status is not a
//     sound answer to that question (a re-link writes 'replaced' without calling Plaid, and
//     MarkItemsDisconnected refuses to overwrite 'replaced', so "done" was unrecordable for those
//     rows). It is now `li.plaid_removed_at IS NULL`, the column that records the fact directly.
//     Strictly wider AND strictly narrower than before, both in the right direction: a 'replaced'
//     row whose Plaid side IS done is now excluded (it was already excluded, by the other half), and
//     a 'disconnected' row whose Plaid side is somehow NOT recorded as done is now a candidate again
//     instead of being permanently invisible.
//   - the 'replaced' half is CANDIDACY, not Plaid state, and is unchanged in effect: the SWEEP still
//     does not schedule cleanups for re-link leftovers (ClaimItemForCleanup and
//     GetItemsPendingCleanup, the recovery paths, deliberately do not share this exclusion — see
//     cleanupCandidateReasonPredicate's second bullet). It is now spelled NULL-safely
//     (`status IS NULL OR status <> 'replaced'`) to match every sibling liveness predicate in this
//     file; the previous bare `NOT IN` form silently EXCLUDED a NULL-status row from the sweep under
//     SQL three-valued logic, the opposite of how every other predicate here treats NULL.
//     Unreachable in practice since 000126 made status NOT NULL; corrected anyway, for the same
//     reason its siblings are written that way.
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
	  AND li.plaid_removed_at IS NULL
	  AND (li.status IS NULL OR li.status <> 'replaced')
	  AND ` + cleanupCandidateReasonPredicate + `
`

// cleanupCandidateReasonPredicate is the four-legged OR that answers "is there still a REASON to
// clean this item up?" — extracted from cleanupCandidateSelectQuery (Issue #1486) so the sweep that
// SCHEDULES a cleanup and ClaimItemForCleanup, which re-verifies it at the moment the irreversible
// Plaid /item/remove is about to run, share one piece of text and cannot drift. $1 is the errorDays
// interval, $2 the staleDays interval, exactly as in cleanupCandidateSelectQuery; a caller splicing
// this into a statement of its own must start its remaining parameters at $3, and must alias the
// linked_items row as `li` (the NOT EXISTS leg is correlated on li.id).
//
// What is deliberately NOT in here, and why the split falls exactly at this line:
//
//   - `li.cleanup_scheduled_at IS NULL` is a CANDIDACY condition for the sweep only ("not already
//     scheduled"). It is false by construction for every item ClaimItemForCleanup is ever asked
//     about, because MarkForCleanup set it — so a recheck that reused it verbatim would deny every
//     claim, and a recheck of "is it still scheduled" would be no recheck at all. Issue #1486 follow-up part E:
//     there are FOUR production callers of ClearCleanupSchedule, not two, and only two are in
//     queue/jobs/item_cleanup.go (one in ItemCleanupWorker.Work itself, one in the
//     releaseDeclinedCleanupSchedule helper both of that worker's decline points share — Codex
//     pass-5 Medium 2) — see Work's #1486 comment for the enumeration, and
//     jobs.TestClearCleanupScheduleProductionCallers_MatchTheDocumentedSet, which fails if it drifts
//     again (grep `\.ClearCleanupSchedule(`). The half this bullet actually rests on is unchanged and
//     still true: NOTHING clears cleanup_scheduled_at on reconnection, so a
//     reconnected, fully repaired item still carries its schedule.
//     ClaimItemForCleanup therefore requires the INVERSE (IS NOT NULL) and adds it itself.
//   - The sweep's own `li.plaid_removed_at IS NULL AND (li.status IS NULL OR li.status <> 'replaced')`
//     pair is also sweep-only (Issue #1495 split what was one `li.status NOT IN ('replaced',
//     'disconnected')` clause; see cleanupCandidateSelectQuery for why), and including the 'replaced'
//     half in a recheck would be actively harmful: the re-link path (services/account_service.go,
//     "re-link detected, soft-deleting old linked item") marks the OLD item 'replaced' WITHOUT calling
//     Plaid /item/remove, so a 'replaced' item's Plaid item is still live and still billing. Declining
//     to remove it would strand it forever — the sweep excludes 'replaced' too, so it would never be
//     re-selected. Re-checking `plaid_removed_at IS NULL` would be harmless but pointless here for the
//     same reason the 'disconnected' exclusion was: the item RemoveItem is about to remove has not
//     been stamped yet by definition, and a second /item/remove against an already-removed Item is a
//     wasted API call that PlaidItemCleanupService already maps to success, not a lost one.
const cleanupCandidateReasonPredicate = `(
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
	  )`

// cleanupCandidateSortKey is the ASC-NULLS-LAST ordering of the cleanup sweep, re-expressed with
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
// Issue #1469. The WHERE clause is the unbounded GetItemsForCleanup's, unchanged
// (cleanupCandidateSelectQuery), plus the keyset predicate. (That unbounded method was deleted in
// #1488 once this one had taken over its last caller.)
//
// Why this exists: GetItemsForCleanup had no LIMIT, and its single caller,
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
// runs, since Issue #1477, the caller PERSISTS the timestamp key a capped run stopped at (the
// sweep_cursors table — repository/sweep_cursor_repository.go) and passes it back here as the next
// run's `after`, so a head of UNMARKABLE rows larger than the cap no longer consumes every later
// run's whole budget. Before #1477 it did, and nothing past the cap was EVER reached for as long as
// they kept failing.
//
// ⚠ The starving head here must be UNMARKABLE, i.e. MarkForCleanup itself failing — a database
// failure. It is NOT a Plaid failure: a successfully marked row sets cleanup_scheduled_at and stops
// matching this predicate immediately, so however badly its downstream /item/remove goes, it is not
// in this query's result set any more.
//
// The replacement guarantee is BOUNDED, not absolute — it is stated once, in full, in
// repository/sweep_cursor_repository.go's header. Do not restate it as "no starvation".
//
// No index can serve this sort key, exactly as none could serve the unbounded query's: the closest
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
// cleanup queries need. Extracted (Issue #1469) so the unbounded GetItemsForCleanup and
// GetItemsForCleanupPage could not drift in their scan order or their decryption behaviour; the
// unbounded form is gone (#1488) and GetItemsForCleanupPage is now the only caller.
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

// GetItemsPendingCleanup retrieves items where cleanup is scheduled and past due. It is the
// selection query for ItemCleanupCheckWorker's stranded-cleanup backstop (Issue #1486 follow-up
// part C) — before that it had no production caller at all.
//
// SELECTS ON plaid_removed_at, NOT ON status (Issue #1495), and that single change dissolves both
// halves of the mess this predicate used to document:
//
//   - It used to exclude 'disconnected' and NOT 'replaced' — a deliberate asymmetry with the SWEEP's
//     candidacy predicate (cleanupCandidateSelectQuery), because a 'replaced' item is STILL LIVE AND
//     STILL BILLING at Plaid (the re-link path in services/account_service.go marks the OLD item
//     'replaced' WITHOUT ever calling /item/remove) and this query is its ONLY recovery path: the
//     candidate sweep requires cleanup_scheduled_at IS NULL and excludes 'replaced' on top of that.
//     That reasoning is unchanged and still load-bearing — a 'replaced' row with no recorded Plaid
//     removal is still selected here. Pinned by
//     TestGetItemsPendingCleanup_Integration_ReplacedItemIsRecoverable.
//   - But those exclusions did NOT add up to "every selected row still needs Plaid work", and could
//     not be made to: MarkItemsDisconnected must not overwrite 'replaced', so a 'replaced' row whose
//     Plaid side WAS already done had nowhere to say so and was re-selected on every run, costing one
//     /item/remove per run that answers ITEM_NOT_FOUND. The prior version of this comment called that
//     an accepted, self-healing limitation and named the fix: a dedicated plaid_removed_at column.
//     That column now exists (migration 000131) and this predicate reads it, so the loop is closed at
//     its cause rather than waiting on ItemCleanupWorker.Work's ClearCleanupSchedule to end it.
//
// 'disconnected' is no longer named here, and it does not need to be: MarkPlaidRemoved is called
// immediately BEFORE MarkItemsDisconnected on every success path (RemoveItem and cleanupItems), and
// migration 000132 backfilled pre-existing 'disconnected' rows, so a row carrying that status carries
// a stamp too and is excluded by `plaid_removed_at IS NULL` for the reason that actually matters.
//
// NULL-safety: `plaid_removed_at IS NULL` is a two-valued IS NULL test, so the three-valued-logic
// hole that required the old `status IS NULL OR` arm (#1444 — a bare `status NOT IN (...)` is UNKNOWN
// rather than TRUE for a NULL-status row, silently excluding it from this backstop) cannot arise
// here at all. The arm was not dropped as a simplification; the predicate it guarded is gone. Its
// sibling in cleanupCandidateSelectQuery, which still reads status, keeps the NULL-safe spelling.
//
// Issue #1413 round-10 (#1444): NULL-safe status SCAN as well (see normalizeLinkedItemStatus), for
// consistency with every other scan site in this file.
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
		  AND plaid_removed_at IS NULL
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

// ClaimItemForCleanup re-verifies, in ONE statement at the moment of the claim, that a single item
// is STILL a cleanup candidate, and reports whether the caller may proceed with the irreversible
// Plaid /item/remove. Issue #1486.
//
// Returns (true, nil) when the item is still scheduled AND still matches
// cleanupCandidateReasonPredicate (see that constant for why the recheck is eligibility and not
// merely "is it still scheduled"), and (false, nil) — a LEGITIMATE, EXPECTED outcome and NOT an
// error — when it is not: the user repaired the connection through Plaid Link update mode, accounts
// came back, the row was already removed, or the schedule was cancelled. Callers MUST treat
// (false, nil) as success-with-nothing-to-do and MUST NOT call RemoveItem; they must fail closed on
// a non-nil error rather than proceeding, because "we could not tell" is not "still eligible".
//
// errorDays/staleDays must be the SAME thresholds the sweep used when it scheduled the item
// (queue/jobs/item_cleanup_check.go keeps both workers on one pair of defaults for exactly this
// reason) — a recheck run with a stricter threshold than the sweep would deny claims the sweep
// legitimately created and strand still-billing items.
//
// Why an UPDATE whose RowsAffected is the answer, mirroring ClaimForDeletion
// (profile_repository.go), rather than a SELECT: the write takes a row lock, so a concurrent
// repair transaction that has already written the row but not yet committed blocks this statement
// instead of being invisible to it. Under READ COMMITTED an UPDATE re-evaluates its WHERE against
// the freshly committed row once the lock is granted, so the repair correctly denies the claim; a
// plain SELECT would have read the pre-repair snapshot and granted it, and the resulting
// /item/remove cannot be undone. `SET updated_at = NOW()` is redundant with the
// update_linked_items_updated_at BEFORE UPDATE trigger (which assigns NEW.updated_at
// unconditionally) and is written out anyway to keep the statement's intent readable. A DENIED claim
// writes nothing at all: the WHERE matches no row, so RowsAffected is 0 and neither the SET nor the
// trigger runs — the re-verification leaves no trace on the row when it says no.
//
// This is a re-verification, NOT a lease: it grants no exclusivity that outlives the statement, and
// nothing else supplies that exclusivity either. Do NOT credit ItemCleanupUniqueOpts with it: its
// `ByArgs: true` keys on Kind PLUS the full Args, and Args carries Reason — so a sweep-enqueued
// Reason:"stale" job and an accountService.DeleteAccount-enqueued Reason:"zero_accounts" job for the
// SAME item are distinct unique keys and can coexist. What actually makes that benign is that the
// loser of the race has nothing left to fail on, via two SEPARATE idempotency paths:
//
//   - Plaid-level: since Issue #1486 follow-up part B, PlaidItemCleanupService.RemoveItem propagates
//     every genuine /item/remove error to the worker — it no longer swallows them. But in THIS race,
//     the loser calls /item/remove only after the winner's call already succeeded, so Plaid answers
//     the loser with ITEM_NOT_FOUND — the one outcome isPlaidItemAlreadyGone still treats as success
//     (RemoveItem returns nil) rather than propagating. It never reaches the worker as an error.
//   - Repository-level: if the winner already deleted the local row, RemoveItem's GetByID returns
//     ErrLinkedItemNotFound, and THAT is what ItemCleanupWorker.Work's IsItemNotFoundError branch
//     recognises (IsItemNotFoundError is errors.Is(err, repository.ErrLinkedItemNotFound) — it has
//     nothing to do with Plaid's ITEM_NOT_FOUND code). The subsequent row Delete is a safe no-op.
//
// Two claims can therefore both be granted; the second one's work is simply redundant, not harmful.
func (r *linkedItemRepository) ClaimItemForCleanup(ctx context.Context, itemID string, errorDays, staleDays int) (bool, error) {
	ct, err := r.db.Exec(ctx, `
		UPDATE linked_items li
		SET updated_at = NOW()
		WHERE li.id = $3
		  AND li.cleanup_scheduled_at IS NOT NULL
		  AND `+cleanupCandidateReasonPredicate,
		strings.TrimSpace(fmt.Sprintf("%d days", errorDays)),
		strings.TrimSpace(fmt.Sprintf("%d days", staleDays)),
		itemID,
	)
	if err != nil {
		return false, fmt.Errorf("ClaimItemForCleanup: %w", err)
	}
	return ct.RowsAffected() > 0, nil
}

// ClearCleanupSchedule clears the cleanup schedule for an item. Despite the name's suggestion, it is
// NOT called on reconnection — a user repairing an item through Plaid Link update mode leaves
// cleanup_scheduled_at set, which is exactly why ClaimItemForCleanup re-verifies ELIGIBILITY instead
// of re-reading the schedule. See cleanupCandidateReasonPredicate.
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
		return fmt.Errorf("%w (MarkItemsDisconnected): 0 of %d given id(s) matched", ErrNoLinkedItemsUpdated, len(ids))
	}
	return nil
}

// MarkPlaidRemoved stamps plaid_removed_at for the given items — see the interface method's doc
// comment (Issue #1495) for why this cannot be folded into MarkItemsDisconnected.
//
// NO STATUS FILTER, deliberately, and this is the one line the whole change rests on: a 'replaced'
// row MUST be stampable here, because that is precisely the row MarkItemsDisconnected refuses and
// therefore the row that had no way to record "Plaid is done" at all. Adding a status guard here
// would recreate the deadlock this column exists to break. Proved against real Postgres by
// TestLinkedItemRepository_MarkPlaidRemoved_Integration_StampsReplacedRowWithoutRevertingStatus.
//
// COALESCE(plaid_removed_at, $1) rather than a bare assignment, plus no `plaid_removed_at IS NULL`
// in the WHERE: the two together make the write idempotent (an already-stamped row keeps its
// earlier, closer-to-the-truth instant) while keeping RowsAffected a statement about whether the
// ROWS EXIST rather than about whether they were already stamped. With the WHERE guard instead, a
// legitimate re-run would report zero rows and be indistinguishable from a vanished row.
func (r *linkedItemRepository) MarkPlaidRemoved(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	now := time.Now()

	query := `
		UPDATE linked_items
		SET plaid_removed_at = COALESCE(plaid_removed_at, $1),
		    updated_at = $1
		WHERE id = ANY($2)
	`

	tag, err := r.db.Exec(ctx, query, now, ids)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w (MarkPlaidRemoved): 0 of %d given id(s) matched", ErrNoLinkedItemsUpdated, len(ids))
	}
	return nil
}

// MarkPlaidRemovalFailed records an unrecoverable /item/remove failure — see the interface method's
// doc comment (Issue #1494) for why this fact must stay separate from plaid_removed_at.
//
// DOES NOT clear plaid_removed_at, and DOES NOT touch status. Both omissions are deliberate: the
// verdict recorded here is about our credential, not about the Item's existence, so it justifies no
// other state change. In particular it must not write 'disconnected' — that status is read by the
// cleanup loop's skip branch (services.cleanupItems), and a row that stopped being re-attempted
// while still owing Plaid work is the exact "state marked, work never happened" shape this branch
// has closed four times.
//
// The `plaid_removed_at IS NULL` guard is what makes the two columns non-contradictory in the one
// direction that matters: a proved-removed row can never later be labelled unremovable. The reverse
// IS allowed — MarkPlaidRemoved has no failure-column guard, so a row marked terminal here can
// still be stamped removed later if the classification turns out to have been too wide, and the
// failure columns are left in place as the only evidence that happened.
func (r *linkedItemRepository) MarkPlaidRemovalFailed(ctx context.Context, ids []string, errorCode string) error {
	if len(ids) == 0 {
		return nil
	}
	now := time.Now()

	query := `
		UPDATE linked_items
		SET plaid_removal_failed_at = COALESCE(plaid_removal_failed_at, $1),
		    plaid_removal_error_code = $2,
		    updated_at = $1
		WHERE id = ANY($3)
		  AND plaid_removed_at IS NULL
	`

	tag, err := r.db.Exec(ctx, query, now, errorCode, ids)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w (MarkPlaidRemovalFailed): 0 of %d given id(s) matched — row(s) gone, or already recorded as removed", ErrNoLinkedItemsUpdated, len(ids))
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
