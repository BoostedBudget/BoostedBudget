package domain

import (
	"errors"
	"time"
)

// LinkedItemStatus represents the connection/soft-delete status of a linked item
type LinkedItemStatus string

const (
	// LinkedItemStatusActive indicates the item is currently syncing
	LinkedItemStatusActive LinkedItemStatus = "active"
	// LinkedItemStatusError indicates a general connection error
	LinkedItemStatusError LinkedItemStatus = "error"
	// LinkedItemStatusLoginRequired indicates the user needs to re-authenticate
	LinkedItemStatusLoginRequired LinkedItemStatus = "login_required"
	// LinkedItemStatusPendingExpiration indicates the item access is expiring soon
	LinkedItemStatusPendingExpiration LinkedItemStatus = "pending_expiration"
	// LinkedItemStatusRevoked indicates the user revoked access at their bank
	LinkedItemStatusRevoked LinkedItemStatus = "revoked"
	// LinkedItemStatusReplaced indicates the item was replaced by a re-link (Issue #171)
	LinkedItemStatusReplaced LinkedItemStatus = "replaced"
	// LinkedItemStatusDisconnected indicates the user manually disconnected (Issue #171)
	LinkedItemStatusDisconnected LinkedItemStatus = "disconnected"
)

// LinkedItem represents a connection to a financial institution from any provider.
// Supports Plaid (current), Teller, Finicity, Yodlee (future), and manual accounts (future).
//
// Issue #171: Added soft-delete fields for re-link handling.
// When a user re-links a bank, the old LinkedItem is marked as 'replaced'
// and a new LinkedItem is created with the new Plaid item_id.
type LinkedItem struct {
	ID                   string     `json:"id" db:"id"`
	UserID               string     `json:"user_id" db:"user_id"`
	ItemID               string     `json:"item_id" db:"item_id"` // Provider's item/connection ID
	AccessTokenEncrypted string     `json:"-" db:"access_token_encrypted"` // Never expose in JSON
	InstitutionID        string     `json:"institution_id,omitempty" db:"institution_id"`
	InstitutionName      string     `json:"institution_name,omitempty" db:"institution_name"`
	Provider             string     `json:"provider" db:"provider"` // plaid, teller, finicity, yodlee, manual
	Cursor               *string    `json:"cursor,omitempty" db:"cursor"` // For transaction sync pagination
	LastSyncedAt         *time.Time `json:"last_synced_at,omitempty" db:"last_synced_at"`

	// Error tracking (Issue #257)
	Status        LinkedItemStatus `json:"status" db:"status"` // active, error, login_required, pending_expiration, revoked, replaced, disconnected
	ErrorCode     *string          `json:"error_code,omitempty" db:"error_code"` // Plaid error code (e.g., ITEM_LOGIN_REQUIRED)
	ErrorMessage  *string          `json:"error_message,omitempty" db:"error_message"` // Human-readable error message
	LastErrorAt   *time.Time       `json:"last_error_at,omitempty" db:"last_error_at"` // Timestamp of most recent error
	RetryCount    int              `json:"retry_count" db:"retry_count"` // Number of failed sync attempts
	NextRetryAt   *time.Time       `json:"next_retry_at,omitempty" db:"next_retry_at"` // Scheduled time for next retry

	// Soft-delete fields for re-link handling (Issue #171)
	ReplacedAt   *time.Time `json:"replaced_at,omitempty" db:"replaced_at"`     // When this item was replaced
	ReplacedByID *string    `json:"replaced_by_id,omitempty" db:"replaced_by_id"` // UUID of replacement linked_item

	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// Validate checks that the LinkedItem has all required fields
func (l *LinkedItem) Validate() error {
	if l.UserID == "" {
		return errors.New("user_id is required")
	}
	if l.ItemID == "" {
		return errors.New("item_id is required")
	}
	if l.AccessTokenEncrypted == "" {
		return errors.New("access_token is required")
	}
	return nil
}
