package domain

import (
	"errors"
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
	InstitutionID   string  `json:"institution_id" db:"institution_id"`                 // Plaid institution ID (stable)
	Mask            *string `json:"mask,omitempty" db:"mask"`                           // Last 4 digits (stable)
	AccountNameHint *string `json:"account_name_hint,omitempty" db:"account_name_hint"` // Original name

	// User customization (stable across re-links)
	Nickname     *string `json:"nickname,omitempty" db:"nickname"`           // User-defined display name
	CustomColor  *string `json:"custom_color,omitempty" db:"custom_color"`   // Hex color #RRGGBB
	CustomIcon   *string `json:"custom_icon,omitempty" db:"custom_icon"`     // Predefined icon identifier
	CustomEmoji  *string `json:"custom_emoji,omitempty" db:"custom_emoji"`   // UTF-8 emoji
	DisplayOrder *int    `json:"display_order,omitempty" db:"display_order"` // Sort order
	IsHidden     bool    `json:"is_hidden" db:"is_hidden"`                   // Hide from default views

	// ExcludeInvestmentSync hides this chain's investment_transactions from BOTH read surfaces —
	// the unified activity feed and GET /api/v1/investments/investment-transactions (#1609 Item 5,
	// migration 000187). It exists for a brokerage cash-management account that Plaid reports
	// through BOTH /transactions/sync and /investments/transactions/get, which otherwise shows
	// every real movement twice.
	//
	// ⚠ It is a READ FILTER AND NOTHING ELSE. No investment_transactions row is deleted,
	// tombstoned, or suppressed at ingestion, so clearing the flag makes exactly the same rows
	// reappear. Destroying them because a user reclassified an account would be side-effect
	// destruction (docs/decisions/2026-08-24-financial-history-retained-by-default.md), and the
	// reversibility is the reason no backfill or cleanup script exists: turning the flag on IS the
	// cleanup.
	//
	// It lives on the chain rather than on accounts so it survives a Plaid re-link, for the same
	// reason every other customization above does.
	ExcludeInvestmentSync bool `json:"exclude_investment_sync" db:"exclude_investment_sync"`

	// Account type info (from most recent Plaid account)
	PrimaryType    *string `json:"primary_type,omitempty" db:"primary_type"`       // depository, credit, etc.
	PrimarySubtype *string `json:"primary_subtype,omitempty" db:"primary_subtype"` // checking, savings, etc.

	// IdentityType and IdentitySubtype are the FROZEN identity of this chain, distinct from
	// PrimaryType/PrimarySubtype above. #1569 round 11 task 3: PrimaryType/PrimarySubtype are
	// explicitly "from most recent Plaid link" and are overwritten on every re-link
	// (normalizeExistingUserAccount -> UpdatePrimaryTypeAndSubtype) — they are descriptive,
	// last-writer-wins labels, not identity. Keying re-link matching on them would let a Plaid-
	// reported subtype change at re-link silently merge or split a chain (the exact
	// $9,000-for-$5,000 double-count bug this branch exists to prevent).
	//
	// IdentityType/IdentitySubtype are written ONCE, at mint (UserAccountService.MatchOrCreate's
	// Create path and database/backfill.go's synthesiseUserAccountID), and NEVER rewritten
	// afterward — there is deliberately no UpdateIdentity method. Guarded mechanically by
	// TestNoUpdateOfUserAccountsIdentityColumns (AST scan of every UPDATE user_accounts SQL
	// literal in non-test Go) and by a BEFORE UPDATE trigger
	// (trg_user_accounts_identity_immutable, migration 000177) that rejects any UPDATE changing
	// either column once set.
	//
	// Nothing reads these for matching yet (task 4 will change the match key); this task only
	// writes them. json:"-" because they are not yet part of the API surface — there is no
	// consumer of them outside the backend, and adding them to the Huma response body now would
	// drift the checked-in openapi.yaml (this task does not run generate-openapi).
	IdentityType    *string `json:"-" db:"identity_type"`
	IdentitySubtype *string `json:"-" db:"identity_subtype"`

	// HiddenSource records WHY this chain is hidden, for rows with IsHidden = true; NULL when
	// IsHidden = false. Added by migration 000185 (#1609) because two different things hide a
	// chain and current state cannot tell them apart: the SYSTEM hides a chain that lost its last
	// active account, and the USER hides it via PATCH /api/v1/user-accounts/{id}. Both have zero
	// active accounts by the time a re-link asks, so before this column normalizeExistingUserAccount
	// un-hid BOTH — silently destroying a deliberate user hide on the next re-link.
	//
	// Only HiddenSourceSystem is eligible for that automatic un-hide. HiddenSourceUser is the
	// user's own decision; HiddenSourceUnknown is 000185's conservative classification for rows it
	// genuinely could not tell apart; and a NULL cause on a hidden row is treated the same way as
	// unknown, for the same reason. Un-hiding an account the user hid is a privacy regression they
	// cannot even detect, whereas leaving a system hide in place is an annoyance they can undo.
	//
	// json:"-" for the same reason as IdentityType above: it is not part of the API surface, and
	// adding it to the response body would drift the checked-in openapi.yaml.
	HiddenSource *string `json:"-" db:"hidden_source"`

	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// The user_accounts.hidden_source vocabulary, constrained in SQL by
// user_accounts_valid_hidden_source (migration 000185). See UserAccount.HiddenSource.
const (
	// HiddenSourceUser — a human hid this chain via PATCH /api/v1/user-accounts/{id}. Never
	// un-hidden automatically.
	HiddenSourceUser = "user"
	// HiddenSourceSystem — hidden automatically because the chain lost its last active account,
	// so it would otherwise linger as a ghost. The ONLY cause a re-link may un-hide.
	HiddenSourceSystem = "system"
	// HiddenSourceUnknown — migration 000185 could not tell the two apart. Deliberately never
	// un-hidden automatically; do not "simplify" it into HiddenSourceSystem.
	HiddenSourceUnknown = "unknown"
)

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

	// ExcludeInvestmentSync — nil = no change. See UserAccount.ExcludeInvestmentSync: a read
	// filter for a brokerage account Plaid reports on both product endpoints (#1609 Item 5).
	// Fully reversible; setting it back to false restores every hidden row.
	ExcludeInvestmentSync *bool `json:"exclude_investment_sync"`
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

// UserAccountStatus represents the valid status values for a user account
type UserAccountStatus string

// Note: UserAccount itself doesn't have a status field. The status tracking
// is on the accounts table (which has status, replaced_at, replaced_by_id).
// UserAccount is always "active" as it represents the stable reference.
