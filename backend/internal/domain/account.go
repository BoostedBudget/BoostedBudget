package domain

import (
	"errors"
	"time"
)

// Account represents a bank account linked through Plaid.
// One LinkedItem can have multiple accounts (checking, savings, credit, etc.)
type Account struct {
	ID               string     `json:"id" db:"id"`
	LinkedItemID     string     `json:"linked_item_id" db:"linked_item_id"`
	UserID           string     `json:"user_id" db:"user_id"`
	PlaidAccountID   string     `json:"plaid_account_id" db:"plaid_account_id"` // Plaid's account_id
	Name             string     `json:"name" db:"name"`                           // User-friendly name
	OfficialName     *string    `json:"official_name,omitempty" db:"official_name"` // Bank's official name
	Type             string     `json:"type" db:"type"`                // depository, credit, loan, investment, other
	Subtype          *string    `json:"subtype,omitempty" db:"subtype"`          // checking, savings, credit card, etc.
	Mask             *string    `json:"mask,omitempty" db:"mask"`                // Last 4 digits
	CurrentBalance   *float64   `json:"current_balance,omitempty" db:"current_balance"`
	AvailableBalance *float64   `json:"available_balance,omitempty" db:"available_balance"`
	CurrencyCode     string     `json:"currency_code" db:"currency_code"` // ISO 4217 currency code
	CreatedAt        time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at" db:"updated_at"`
}

// Validate checks that the Account has all required fields and valid types
func (a *Account) Validate() error {
	if a.UserID == "" {
		return errors.New("user_id is required")
	}
	if a.LinkedItemID == "" {
		return errors.New("linked_item_id is required")
	}
	if a.PlaidAccountID == "" {
		return errors.New("plaid_account_id is required")
	}
	if a.Name == "" {
		return errors.New("name is required")
	}
	if a.Type == "" {
		return errors.New("type is required")
	}

	// Validate account type
	validTypes := map[string]bool{
		"depository": true,
		"credit":     true,
		"loan":       true,
		"investment": true,
		"other":      true,
	}
	if !validTypes[a.Type] {
		return errors.New("invalid account type: must be one of depository, credit, loan, investment, other")
	}

	return nil
}
