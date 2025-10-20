package domain

import (
	"errors"
	"time"
)

// LinkedItem represents a Plaid item (connection to a financial institution)
// that has been linked to a user's account.
type LinkedItem struct {
	ID                   string     `json:"id" db:"id"`
	UserID               string     `json:"user_id" db:"user_id"`
	ItemID               string     `json:"item_id" db:"item_id"` // Plaid's item_id
	AccessTokenEncrypted string     `json:"-" db:"access_token_encrypted"` // Never expose in JSON
	InstitutionID        string     `json:"institution_id,omitempty" db:"institution_id"`
	InstitutionName      string     `json:"institution_name,omitempty" db:"institution_name"`
	Cursor               *string    `json:"cursor,omitempty" db:"cursor"` // For transaction sync pagination
	LastSyncedAt         *time.Time `json:"last_synced_at,omitempty" db:"last_synced_at"`
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
