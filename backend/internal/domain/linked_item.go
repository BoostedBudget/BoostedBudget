package domain

import (
	"errors"
	"time"
)

// LinkedItem represents a connection to a financial institution from any provider.
// Supports Plaid (current), Teller, Finicity, Yodlee (future), and manual accounts (future).
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
	Status        string     `json:"status" db:"status"` // active, error, login_required, pending_expiration, revoked
	ErrorCode     *string    `json:"error_code,omitempty" db:"error_code"` // Plaid error code (e.g., ITEM_LOGIN_REQUIRED)
	ErrorMessage  *string    `json:"error_message,omitempty" db:"error_message"` // Human-readable error message
	LastErrorAt   *time.Time `json:"last_error_at,omitempty" db:"last_error_at"` // Timestamp of most recent error
	RetryCount    int        `json:"retry_count" db:"retry_count"` // Number of failed sync attempts
	NextRetryAt   *time.Time `json:"next_retry_at,omitempty" db:"next_retry_at"` // Scheduled time for next retry

	CreatedAt            time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at" db:"updated_at"`
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
