package domain

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

// UserAccount represents a stable abstraction of a real-world bank account that
// survives Plaid Item re-links. When a user disconnects and re-connects a bank,
// Plaid assigns new item_id and account_id values. The UserAccount provides a
// stable reference that groups multiple Plaid accounts representing the same
// real-world account.
//
// Matching strategy: (user_id, institution_id, mask) identifies a unique real-world account.
//
// Issue #171 - Plaid Re-Link Handling
type UserAccount struct {
	ID              string  `json:"id" db:"id"`
	UserID          string  `json:"user_id" db:"user_id"`
	InstitutionID   string  `json:"institution_id" db:"institution_id"`         // Plaid institution ID (stable)
	Mask            *string `json:"mask,omitempty" db:"mask"`                   // Last 4 digits (stable)
	AccountNameHint *string `json:"account_name_hint,omitempty" db:"account_name_hint"` // Original name

	// User customization (stable across re-links)
	Nickname     *string `json:"nickname,omitempty" db:"nickname"`           // User-defined display name
	CustomColor  *string `json:"custom_color,omitempty" db:"custom_color"`   // Hex color #RRGGBB
	CustomIcon   *string `json:"custom_icon,omitempty" db:"custom_icon"`     // Predefined icon identifier
	CustomEmoji  *string `json:"custom_emoji,omitempty" db:"custom_emoji"`   // UTF-8 emoji
	DisplayOrder *int    `json:"display_order,omitempty" db:"display_order"` // Sort order
	IsHidden     bool    `json:"is_hidden" db:"is_hidden"`                   // Hide from default views

	// Account type info (from most recent Plaid account)
	PrimaryType    *string `json:"primary_type,omitempty" db:"primary_type"`       // depository, credit, etc.
	PrimarySubtype *string `json:"primary_subtype,omitempty" db:"primary_subtype"` // checking, savings, etc.

	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// Validate checks that the UserAccount has all required fields and valid values
func (ua *UserAccount) Validate() error {
	if ua.UserID == "" {
		return errors.New("user_id is required")
	}
	if ua.InstitutionID == "" {
		return errors.New("institution_id is required")
	}

	// Validate nickname length
	if ua.Nickname != nil {
		trimmed := strings.TrimSpace(*ua.Nickname)
		if len(trimmed) > 255 {
			return errors.New("nickname must not exceed 255 characters")
		}
	}

	// Validate hex color format
	if ua.CustomColor != nil && *ua.CustomColor != "" {
		if !hexColorRegex.MatchString(*ua.CustomColor) {
			return errors.New("custom_color must be a valid hex color (format: #RRGGBB)")
		}
	}

	// Validate icon is in allowed set
	if ua.CustomIcon != nil && *ua.CustomIcon != "" {
		if !ValidAccountIcons[*ua.CustomIcon] {
			return errors.New("custom_icon must be one of the allowed icons")
		}
	}

	// Validate emoji length
	if ua.CustomEmoji != nil && len(*ua.CustomEmoji) > 10 {
		return errors.New("custom_emoji must not exceed 10 characters")
	}

	// Validate primary type if set
	if ua.PrimaryType != nil && *ua.PrimaryType != "" {
		validTypes := map[string]bool{
			"depository": true,
			"credit":     true,
			"loan":       true,
			"investment": true,
			"brokerage":  true,
			"other":      true,
		}
		if !validTypes[*ua.PrimaryType] {
			return errors.New("primary_type must be one of: depository, credit, loan, investment, brokerage, other")
		}
	}

	return nil
}

// UpdateUserAccountRequest represents the request body for updating user account customization
type UpdateUserAccountRequest struct {
	Nickname     *string `json:"nickname"`      // nil = no change, empty string = clear
	CustomColor  *string `json:"custom_color"`  // nil = no change, empty string = clear
	CustomIcon   *string `json:"custom_icon"`   // nil = no change, empty string = clear
	CustomEmoji  *string `json:"custom_emoji"`  // nil = no change, empty string = clear
	DisplayOrder *int    `json:"display_order"` // nil = no change
	IsHidden     *bool   `json:"is_hidden"`     // nil = no change
}

// Validate validates the UpdateUserAccountRequest
func (r *UpdateUserAccountRequest) Validate() error {
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

// userAccountHexColorRegex validates hex color format #RRGGBB (reuse from account.go)
var userAccountHexColorRegex = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// UserAccountStatus represents the valid status values for a user account
type UserAccountStatus string

// Note: UserAccount itself doesn't have a status field. The status tracking
// is on the accounts table (which has status, replaced_at, replaced_by_id).
// UserAccount is always "active" as it represents the stable reference.
