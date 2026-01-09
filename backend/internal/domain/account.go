package domain

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

// Account represents a bank account linked through Plaid.
// One LinkedItem can have multiple accounts (checking, savings, credit, etc.)
//
// Issue #171: Added UserAccountID and soft-delete fields for re-link handling.
// The UserAccountID links to a stable user_accounts record that survives re-links.
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
	InstitutionName  string     `json:"institution_name,omitempty" db:"institution_name"` // From linked_items JOIN (optional)

	// Stable reference for re-link handling (Issue #171)
	UserAccountID *string `json:"user_account_id,omitempty" db:"user_account_id"` // FK to user_accounts

	// Soft-delete fields for re-link handling (Issue #171)
	Status       AccountStatus `json:"status" db:"status"`                       // 'active' or 'replaced'
	ReplacedAt   *time.Time    `json:"replaced_at,omitempty" db:"replaced_at"`   // When this account was replaced
	ReplacedByID *string       `json:"replaced_by_id,omitempty" db:"replaced_by_id"` // UUID of replacement account

	// Customization fields (Issue #307)
	// Note: Customizations are moving to user_accounts table for stability across re-links
	// These fields are kept for backward compatibility but user_accounts is the source of truth
	Nickname     *string `json:"nickname,omitempty" db:"nickname"`           // User-defined display name
	CustomColor  *string `json:"custom_color,omitempty" db:"custom_color"`   // Hex color #RRGGBB
	CustomIcon   *string `json:"custom_icon,omitempty" db:"custom_icon"`     // Predefined icon identifier
	CustomEmoji  *string `json:"custom_emoji,omitempty" db:"custom_emoji"`   // UTF-8 emoji
	DisplayOrder *int    `json:"display_order,omitempty" db:"display_order"` // Sort order
	IsHidden     bool    `json:"is_hidden" db:"is_hidden"`                   // Hide from default views

	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// AccountStatus represents the soft-delete status of an account
type AccountStatus string

const (
	// AccountStatusActive indicates the account is currently active and syncing
	AccountStatusActive AccountStatus = "active"
	// AccountStatusReplaced indicates the account was replaced by a re-link
	AccountStatusReplaced AccountStatus = "replaced"
)

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

// ValidAccountIcons contains the allowed icon identifiers for account customization
var ValidAccountIcons = map[string]bool{
	"wallet": true, "piggy-bank": true, "credit-card": true, "bank": true, "building": true,
	"briefcase": true, "dollar": true, "euro": true, "pound": true, "yen": true,
	"chart": true, "trending-up": true, "trending-down": true, "lock": true, "unlock": true,
	"star": true, "heart": true, "home": true, "car": true, "plane": true,
	"shopping-cart": true, "gift": true, "coffee": true, "utensils": true, "graduation-cap": true,
}

// hexColorRegex validates hex color format #RRGGBB
var hexColorRegex = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// UpdateAccountRequest represents the request body for updating account customization
type UpdateAccountRequest struct {
	Nickname     *string `json:"nickname"`      // nil = no change, empty string = clear
	CustomColor  *string `json:"custom_color"`  // nil = no change, empty string = clear
	CustomIcon   *string `json:"custom_icon"`   // nil = no change, empty string = clear
	CustomEmoji  *string `json:"custom_emoji"`  // nil = no change, empty string = clear
	DisplayOrder *int    `json:"display_order"` // nil = no change
	IsHidden     *bool   `json:"is_hidden"`     // nil = no change
}

// Validate validates the UpdateAccountRequest
func (r *UpdateAccountRequest) Validate() error {
	// Validate nickname length (after trimming)
	if r.Nickname != nil {
		trimmed := strings.TrimSpace(*r.Nickname)
		if len(trimmed) > 255 {
			return errors.New("nickname must not exceed 255 characters")
		}
	}

	// Validate hex color format
	if r.CustomColor != nil && *r.CustomColor != "" {
		if !hexColorRegex.MatchString(*r.CustomColor) {
			return errors.New("custom_color must be a valid hex color (format: #RRGGBB)")
		}
	}

	// Validate icon is in allowed set
	if r.CustomIcon != nil && *r.CustomIcon != "" {
		if !ValidAccountIcons[*r.CustomIcon] {
			return errors.New("custom_icon must be one of the allowed icons")
		}
	}

	// Validate emoji (basic length check, allow up to 10 chars for compound emojis)
	if r.CustomEmoji != nil && len(*r.CustomEmoji) > 10 {
		return errors.New("custom_emoji must not exceed 10 characters")
	}

	return nil
}

// ReorderAccountsRequest represents the request body for bulk reordering accounts
type ReorderAccountsRequest struct {
	AccountOrders []AccountOrder `json:"account_orders" validate:"required,min=1"`
}

// AccountOrder represents a single account ID with its new display order
type AccountOrder struct {
	AccountID    string `json:"account_id" validate:"required,uuid"`
	DisplayOrder int    `json:"display_order" validate:"gte=0"`
}
