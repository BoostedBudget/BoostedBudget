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

	// LastKnownBalance / LastKnownBalanceAt are the HISTORICAL half of the closed-account story
	// (Issue #1591, on #1569's branch). They are NOT a current balance and must never be treated
	// as one.
	//
	// THE TWO FIELDS ARE ONE FACT. A dated figure is the only kind of figure a closed account may
	// carry, so the amount is meaningless without the instant: #1569 T7 deleted a caption that
	// rendered "Last known balance: N/A … as of Jun 4, 2025" — a date label with nothing to label
	// — and the mirror-image failure (a number with no date) is the same defect pointed the other
	// way. They are populated together or not at all; see lastKnownBalanceLateral in
	// repository/account_liveness_sql.go, whose single subquery reads both from the same
	// balance_snapshots row.
	//
	// ⚠ NOT SUMMABLE, NOT RANGEABLE, NOT SORTABLE against CurrentBalance. CurrentBalance answers
	// "what is in this account now"; these answer "what did we last see, and when". Mixing them in
	// one total, one range filter or one sort order restates a frozen figure as a live one, which
	// is exactly the orphaned balance the ruling in accountBalanceProjection forbids. The frontend
	// keeps that separation in utils.ts (balanceMatchesRange / compareBalanceValues read
	// current_balance and nothing else, pinned by a test).
	//
	// ⚠ POPULATED BY EXACTLY ONE READ: GetAccountsWithInstitutionFiltered, and only when its caller
	// opts in with AccountFilterOptions.IncludeLastKnownBalance. Every other account read leaves
	// both nil. That is deliberate — see the three-decision note on lastKnownBalanceLateral — and
	// it is SAFE in the only direction that matters: nil renders as "no last-known balance on
	// record", which is true of a read that did not ask for one.
	//
	// NULL, NOT ZERO, for the same reason CurrentBalance is: `omitempty` on a pointer drops the key
	// from the JSON entirely, so a closed account with no snapshot on record has NO last-known
	// balance rather than a $0.00 one. Zero here is a real recorded zero — an account that was
	// genuinely emptied before it closed — and the two must stay distinguishable.
	LastKnownBalance   *float64   `json:"last_known_balance,omitempty" db:"-"`
	LastKnownBalanceAt *time.Time `json:"last_known_balance_at,omitempty" db:"-"`
	InstitutionName  string     `json:"institution_name,omitempty" db:"institution_name"` // From linked_items JOIN (optional)

	// Linked item status (Issue #679, #680: Shows 'disconnected' when subscription expires)
	LinkedItemStatus string     `json:"linked_item_status,omitempty" db:"linked_item_status"` // From linked_items JOIN (optional)

	// Stable reference for re-link handling (Issue #171)
	UserAccountID *string `json:"user_account_id,omitempty" db:"user_account_id"` // FK to user_accounts

	// Soft-delete fields for re-link handling (Issue #171)
	Status       AccountStatus `json:"status" db:"status" enum:"active,replaced,expired,removed"` // Issue #1569: enumerated so orval emits a union, not `string`
	ReplacedAt   *time.Time    `json:"replaced_at,omitempty" db:"replaced_at"`   // When this account was replaced
	// RemovedAt is when the user deliberately disconnected this account (status = 'removed').
	// NULL for every other status. Issue #1569, migration 000149.
	RemovedAt *time.Time `json:"removed_at,omitempty" db:"removed_at"`
	// RemovedReason records WHO removed this account — see the RemovalReason constants. It is the
	// single input to one decision: whether restoreProviderRemovedAccounts may return the account
	// to 'active'. Issue #1569 Task 5, migration 000154.
	//
	// ⚠ POPULATED BY EXACTLY ONE READ: GetByLinkedItemIDIncludingRetained, the sync's account read
	// and the only caller that needs it. Every other query leaves it nil. That is deliberate rather
	// than lazy, and it is SAFE in the only direction that matters: nil is read as "not
	// provider-removed", so a stale or unpopulated value can only ever cause a restore to be
	// SKIPPED, never performed. The authoritative check is the SQL predicate inside
	// accountRepository.RestoreProviderRemovedAccount, which re-reads this column under FOR UPDATE
	// and refuses anything that is not 'provider_absent'; this field exists so the service can skip
	// the round trip for the overwhelmingly common case, not so it can decide.
	//
	// Not serialised (`json:"-"`): domain.Account IS an API response type (huma/operations/plaid.go),
	// and exposing this would change api/openapi.yaml and the generated frontend client, which is
	// out of scope for this task. Surfacing "your bank closed this account" to a user is worth
	// doing; it is not this.
	RemovedReason *string `json:"-" db:"removed_reason"`
	// ExpiredAt is when the 90-day fingerprint TTL was released, moving this account to
	// status = 'expired' (AccountRepository.ExpireAccountsByFingerprint). NULL for every other
	// status. Issue #1569 Task 6, migration 000155.
	//
	// Not serialised (`json:"-"`), same reasoning as RemovedReason above: domain.Account IS an API
	// response type (huma/operations/plaid.go), and this branch's task is a data-correctness fix
	// (add the timestamp, audit the write, stop the false-cause interval), not a new user-facing
	// field. No production path currently reads this back into the struct either — it is written
	// by ExpireAccountsByFingerprint's UPDATE and otherwise exists for the same reason removed_at/
	// replaced_at do: a durable, queryable record of when a terminal transition happened.
	ExpiredAt *time.Time `json:"-" db:"expired_at"`
	// ⚠ DEAD COLUMN — never written non-NULL by any production path (#1537 round 14). Its only
	// writer is accountRepository.UpdateStatus, and that method's sole production caller
	// (services/account_service.go:510, the re-link soft-delete) passes a literal nil. The only other
	// production statement touching the column (repository/profile_repository.go:1709) sets it to NULL
	// to clear dangling FKs during DeleteUserCascade, and no migration backfills it.
	// repository/account_repository_test.go:1095-1096 asserts a non-nil value round-trips, but that
	// passes only because the test injects one directly — it is NOT evidence of a live writer.
	// linked_items.replaced_by_id IS populated (services/account_service.go:492); the two same-named
	// columns are easy to conflate. Do not build re-link chain traversal on this field without wiring
	// the write first — see accountChainKey in services/dashboard_service.go for a baseline
	// double-count that cannot be fixed here. Same unwired-affordance class as the dead repository
	// methods removed in #1565.
	ReplacedByID *string       `json:"replaced_by_id,omitempty" db:"replaced_by_id"` // UUID of replacement account

	// Cross-user fingerprint fields (Issue #1010: enforce Plaid item uniqueness across users)
	PersistentAccountID         *string `json:"-" db:"persistent_account_id"`          // Plaid's stable cross-session ID (encrypted)
	PersistentAccountIDBlindIdx *string `json:"-" db:"persistent_account_id_blind_idx"` // Blind index for UNIQUE constraint
	FallbackFingerprintBlindIdx *string `json:"-" db:"fallback_fingerprint_blind_idx"`  // Blind index: institution_id|mask|type|subtype

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
	// AccountStatusExpired indicates the fingerprint hold was released after the 90-day TTL
	// (Issue #1010: applies when a disconnected item's grace period expires and another user links)
	AccountStatusExpired AccountStatus = "expired"
	// AccountStatusRemoved indicates the account is no longer connected. It has THREE causes as of
	// Issue #1569 Task 20 — the user deliberately disconnected it, Plaid stopped returning it (the
	// bank closed it), or one of this product's own flows removed the Plaid Item — distinguished by
	// RemovalReason, NOT by this status. Distinct from
	// AccountStatusReplaced (superseded by a re-link) and AccountStatusExpired (TTL release).
	// Requires RemovedAt to be set — see migration 000149's accounts_removed_at_consistency CHECK.
	AccountStatusRemoved AccountStatus = "removed"
)

// RemovalReason records WHO removed an account, for rows with status 'removed'. It exists because
// exactly one decision depends on it: automatic restore (Issue #1569 Task 5, fix round 2).
//
// ⚠ Only RemovalReasonProviderAbsent may ever be auto-restored. A user disconnect must never be
// undone by a sync — the user asked for the account to be gone, and resurrecting it because the
// bank still reports it would invert this issue's whole principle. An UNSET reason (NULL in the
// column, "" here) is NOT provider-removed and NOT restorable: every pre-migration-000154 'removed'
// row predates the provider path, so it was a user disconnect.
type RemovalReason string

const (
	// RemovalReasonUserDisconnect: the user asked, via DisconnectAccount or DisconnectInstitution.
	RemovalReasonUserDisconnect RemovalReason = "user_disconnect"
	// RemovalReasonProviderAbsent: a confirmed-complete Plaid /accounts/get stopped returning this
	// account, i.e. the bank closed it. The only reason eligible for automatic restore.
	RemovalReasonProviderAbsent RemovalReason = "provider_absent"
	// RemovalReasonLinkRemoved: one of this product's own flows removed the account's Plaid Item, so
	// the account can never sync again even though the bank still reports it. Issue #1569 Task 20.
	//
	// Neither of the other two values is true of these rows, and reaching for either is a real
	// hazard rather than a naming preference. 'user_disconnect' would falsify the user's intent —
	// the orphaned-item cleanup sweep selects an item merely for sitting in 'login_required' past
	// errorDays. 'provider_absent' would be worse: it is the ONE value RestoreAccount accepts, so
	// these accounts would become eligible for automatic resurrection under a Plaid Item that no
	// longer exists.
	//
	// Deliberately NOT specialised per flow (swept / subscription cancelled / erasure). WHY the link
	// was removed is already recorded per item on linked_items and in audit_logs; duplicating it here
	// would be the false-cause mistake migration 000153 removed from account_status_history.reason.
	RemovalReasonLinkRemoved RemovalReason = "link_removed"
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
