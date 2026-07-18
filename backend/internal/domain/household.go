package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// ─── Role / status / visibility enums ────────────────────────────────────────

// HouseholdRole represents the role of a member within a household.
type HouseholdRole string

const (
	HouseholdRoleOwner  HouseholdRole = "owner"
	HouseholdRoleMember HouseholdRole = "member"
)

// HouseholdMemberStatus represents the lifecycle state of a household membership row.
type HouseholdMemberStatus string

const (
	HouseholdMemberStatusActive  HouseholdMemberStatus = "active"
	HouseholdMemberStatusRemoved HouseholdMemberStatus = "removed"
)

// HouseholdInviteStatus represents the lifecycle state of a household invite.
type HouseholdInviteStatus string

const (
	HouseholdInviteStatusPending  HouseholdInviteStatus = "pending"
	HouseholdInviteStatusAccepted HouseholdInviteStatus = "accepted"
	HouseholdInviteStatusDeclined HouseholdInviteStatus = "declined"
	HouseholdInviteStatusRevoked  HouseholdInviteStatus = "revoked"
	HouseholdInviteStatusExpired  HouseholdInviteStatus = "expired"
)

// ShareVisibility represents whether an account is shared or hidden in the household aggregate.
type ShareVisibility string

const (
	ShareVisibilityShared ShareVisibility = "shared"
	ShareVisibilityHidden ShareVisibility = "hidden"
)

// ─── Household ───────────────────────────────────────────────────────────────

// Household represents a named group of users sharing a single subscription plan.
// The owner is the paying subscriber; members are covered under the owner's tier.
// One household per owner (enforced by UNIQUE(owner_user_id) in the DB).
//
// Issue #1365 — Phase 1: schema + domain foundation.
type Household struct {
	ID          string    `json:"id" db:"id"`
	OwnerUserID string    `json:"owner_user_id" db:"owner_user_id"`
	Name        string    `json:"name" db:"name"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`

	// OverCapacitySince is the grace-period anchor timestamp. When set, the
	// household currently has more active members than its plan's seat limit
	// allows, and a 14-day grace clock is running before a background worker
	// auto-offboards the most-recently-joined excess members. Nil when the
	// household is within its seat limit. #1371 — Phase 7.
	OverCapacitySince *time.Time `json:"over_capacity_since,omitempty" db:"over_capacity_since"`

	// OverCapacityStartNoticeSentAt is an idempotency marker: non-nil once the
	// "you're over capacity" warning email has been sent for the current grace
	// period. #1371 — Phase 7.
	OverCapacityStartNoticeSentAt *time.Time `json:"over_capacity_start_notice_sent_at,omitempty" db:"over_capacity_start_notice_sent_at"`

	// OverCapacityWarn3dSentAt is an idempotency marker: non-nil once the
	// "3 days left" warning email has been sent for the current grace period.
	// #1371 — Phase 7.
	OverCapacityWarn3dSentAt *time.Time `json:"over_capacity_warn3d_sent_at,omitempty" db:"over_capacity_warn3d_sent_at"`
}

// Validate checks that the Household has all required fields and valid values.
func (h *Household) Validate() error {
	if h.OwnerUserID == "" {
		return errors.New("owner_user_id is required")
	}
	trimmed := strings.TrimSpace(h.Name)
	if trimmed == "" {
		return errors.New("name is required")
	}
	if utf8.RuneCountInString(trimmed) > 255 {
		return errors.New("name must not exceed 255 characters")
	}
	return nil
}

// ─── HouseholdMember ─────────────────────────────────────────────────────────

// HouseholdMember represents one person's membership in a household.
// Removed rows are retained for history/rejoin tracking; removed_at records when.
// The partial unique index (user_id WHERE status='active') + the mandatory owner
// row guarantee that every user occupies at most one active household at a time.
//
// Issue #1365 — Phase 1.
type HouseholdMember struct {
	ID                      string                `json:"id" db:"id"`
	HouseholdID             string                `json:"household_id" db:"household_id"`
	UserID                  string                `json:"user_id" db:"user_id"`
	Role                    HouseholdRole         `json:"role" db:"role"`
	Status                  HouseholdMemberStatus `json:"status" db:"status"`
	JoinedAt                time.Time             `json:"joined_at" db:"joined_at"`
	RemovedAt               *time.Time            `json:"removed_at,omitempty" db:"removed_at"`
	PriorSubscriptionTier   *string               `json:"prior_subscription_tier,omitempty" db:"prior_subscription_tier"`
	PriorSubscriptionStatus *string               `json:"prior_subscription_status,omitempty" db:"prior_subscription_status"`
	CreatedAt               time.Time             `json:"created_at" db:"created_at"`
	UpdatedAt               time.Time             `json:"updated_at" db:"updated_at"`
}

// Validate checks that the HouseholdMember has all required fields and valid values.
func (m *HouseholdMember) Validate() error {
	if m.HouseholdID == "" {
		return errors.New("household_id is required")
	}
	if m.UserID == "" {
		return errors.New("user_id is required")
	}
	switch m.Role {
	case HouseholdRoleOwner, HouseholdRoleMember:
	default:
		return errors.New("role must be one of: owner, member")
	}
	switch m.Status {
	case HouseholdMemberStatusActive, HouseholdMemberStatusRemoved:
	default:
		return errors.New("status must be one of: active, removed")
	}
	// Mirrors DB CHECK: active→nil, removed→required
	active := m.Status == HouseholdMemberStatusActive
	if active && m.RemovedAt != nil {
		return errors.New("removed_at must be nil when status is active")
	}
	if !active && m.RemovedAt == nil {
		return errors.New("removed_at is required when status is removed")
	}
	return nil
}

// ─── HouseholdInvite ─────────────────────────────────────────────────────────

// HouseholdInvite represents an invitation sent to a prospective household member.
// InviteeEmail holds the plaintext email; the repository layer encrypts it (AES-GCM)
// and derives InviteeEmailBlindIdx (HMAC-SHA256) — both are persisted at the DB boundary.
// AcceptedAt provides a durable acceptance timestamp that survives GDPR user deletion
// (accepted_user_id is SET NULL on user deletion; accepted_at is not).
//
// Issue #1365 — Phase 1.
type HouseholdInvite struct {
	ID                   string                `json:"id" db:"id"`
	HouseholdID          string                `json:"household_id" db:"household_id"`
	InviterUserID        string                `json:"inviter_user_id" db:"inviter_user_id"`
	InviteeEmail         string                `json:"invitee_email" db:"invitee_email"` // plaintext in memory; ciphertext at rest
	InviteeEmailBlindIdx string                `json:"-" db:"invitee_email_blind_idx"`   // repo-internal HMAC; never serialised to JSON
	TokenHash            string                `json:"-" db:"token_hash"`
	Status               HouseholdInviteStatus `json:"status" db:"status"`
	ExpiresAt            time.Time             `json:"expires_at" db:"expires_at"`
	AcceptedUserID       *string               `json:"accepted_user_id,omitempty" db:"accepted_user_id"`
	AcceptedAt           *time.Time            `json:"accepted_at,omitempty" db:"accepted_at"`
	CreatedAt            time.Time             `json:"created_at" db:"created_at"`
	UpdatedAt            time.Time             `json:"updated_at" db:"updated_at"`
}

// Validate checks that the HouseholdInvite has all required fields and valid values.
func (inv *HouseholdInvite) Validate() error {
	if inv.HouseholdID == "" {
		return errors.New("household_id is required")
	}
	if inv.InviterUserID == "" {
		return errors.New("inviter_user_id is required")
	}
	if strings.TrimSpace(inv.InviteeEmail) == "" {
		return errors.New("invitee_email is required")
	}
	if inv.TokenHash == "" {
		return errors.New("token_hash is required")
	}
	switch inv.Status {
	case HouseholdInviteStatusPending,
		HouseholdInviteStatusAccepted,
		HouseholdInviteStatusDeclined,
		HouseholdInviteStatusRevoked,
		HouseholdInviteStatusExpired:
	default:
		return errors.New("status must be one of: pending, accepted, declined, revoked, expired")
	}
	if inv.Status == HouseholdInviteStatusPending && !inv.ExpiresAt.After(time.Now()) {
		return errors.New("expires_at must be in the future for pending invites")
	}
	if inv.Status == HouseholdInviteStatusAccepted && inv.AcceptedAt == nil {
		return errors.New("accepted_at is required when status is accepted")
	}
	return nil
}

// ─── HouseholdAccountShare ───────────────────────────────────────────────────

// HouseholdAccountShare represents a grant that makes one member's account visible
// in the household aggregate. Keyed to user_accounts (survives Plaid relinks).
// PhysicalAccountBlindIdx is a repo-internal HMAC column for joint-account dedup;
// it is excluded from JSON responses but included for safe struct scanning.
// owner_user_id is not stored here; it is derived from user_accounts.user_id at query time.
//
// Issue #1365 — Phase 1.
type HouseholdAccountShare struct {
	ID                      string          `json:"id" db:"id"`
	HouseholdID             string          `json:"household_id" db:"household_id"`
	UserAccountID           string          `json:"user_account_id" db:"user_account_id"`
	Visibility              ShareVisibility `json:"visibility" db:"visibility"`
	SharedAt                time.Time       `json:"shared_at" db:"shared_at"`
	RevokedAt               *time.Time      `json:"revoked_at,omitempty" db:"revoked_at"`
	PhysicalAccountBlindIdx *string         `json:"-" db:"physical_account_blind_idx"`
	CreatedAt               time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt               time.Time       `json:"updated_at" db:"updated_at"`
}

// Validate checks that the HouseholdAccountShare has all required fields and valid values.
func (s *HouseholdAccountShare) Validate() error {
	if s.HouseholdID == "" {
		return errors.New("household_id is required")
	}
	if s.UserAccountID == "" {
		return errors.New("user_account_id is required")
	}
	switch s.Visibility {
	case ShareVisibilityShared, ShareVisibilityHidden:
	default:
		return errors.New("visibility must be one of: shared, hidden")
	}
	if s.RevokedAt != nil && s.RevokedAt.Before(s.SharedAt) {
		return errors.New("revoked_at must not be before shared_at")
	}
	return nil
}

// HouseholdScope carries a validated household-aggregation request: which household to
// aggregate and which active members' accounts are in scope. A nil *HouseholdScope on a
// filter struct means "personal" — today's single-user behavior, unchanged.
//
// Repositories must treat a populated HouseholdScope as pre-validated: MemberUserIDs is
// resolved by HouseholdService.ResolveScope from the caller's own active membership, never
// accepted directly from request input. Issue #1369 — Phase 5.
type HouseholdScope struct {
	HouseholdID   string
	MemberUserIDs []string
}

// UserAccountShareView projects one of the caller's own user_accounts alongside its
// current household_account_shares grant (if any) within a specific household. Used by
// GET /api/v1/households/me/account-shares (#1368 — Phase 4) to render a per-account
// sharing toggle, including accounts that have never been explicitly shared/hidden.
//
// Visibility is nil when no active grant row exists yet for this account (e.g. linked
// after the member joined the household) — the caller must not assume "shared" or
// "hidden" in that case; Phase 5/6 decide the effective display default.
type UserAccountShareView struct {
	UserAccountID string `json:"user_account_id"`
	// Visibility intentionally has no `omitempty` — nil must serialize as the JSON
	// literal `null`, not be omitted, so API consumers can distinguish "no grant yet"
	// from a missing/older response field (#1368 Codex implementation review, Medium-1).
	Visibility *ShareVisibility `json:"visibility"`
	RevokedAt  *time.Time       `json:"revoked_at,omitempty"`
}

// ─── Phase 3 sentinel errors ─────────────────────────────────────────────────
//
// Returned by repository/service write paths (#1367) and mapped to HTTP status
// codes in internal/huma/errors/mapper.go. Repositories translate Postgres 23505
// constraint violations to these sentinels so the mapper never sees a raw
// constraint error.
var (
	ErrHouseholdNotFound         = errors.New("household not found")
	ErrAlreadyOwnsHousehold      = errors.New("user already owns a household")
	ErrNotHouseholdOwner         = errors.New("user is not the household owner")
	ErrHouseholdHasActiveMembers = errors.New("household has active non-owner members")
	ErrHouseholdSeatsFull        = errors.New("no seats available on this plan")
	ErrInviteNotFound            = errors.New("invite not found")
	ErrInviteNotPending          = errors.New("invite is not pending")
	ErrInviteExpired             = errors.New("invite has expired")
	ErrInviteEmailMismatch       = errors.New("invite was sent to a different email address")
	ErrInvitePendingExists       = errors.New("a pending invite already exists for this email")
	ErrCannotInviteSelf          = errors.New("cannot invite yourself")
	ErrAlreadyInHousehold        = errors.New("user already belongs to a household")
	ErrInvalidConsent            = errors.New("consent is required")
	ErrMemberNotFound            = errors.New("member not found")
	ErrCannotRemoveOwner         = errors.New("cannot remove the household owner")
	ErrOwnerPlanInactive         = errors.New("household owner's plan is not currently active")

	// ErrOverCapacityAnchorMismatch is returned by HouseholdService.OffboardExcessMembers
	// when the household row it locks no longer carries the same over_capacity_since the
	// caller's own removal decision was based on — either a different (fresher) anchor, or
	// nil (grace cleared entirely). This means a concurrent clear-then-re-flag cycle (e.g.
	// a subscription webhook) landed in the race window between the caller's fresh read
	// and OffboardExcessMembers's own lock acquisition, so the caller's decision is stale
	// and must not be acted on. Deliberately NOT mapped to any HTTP status in
	// internal/huma/errors/mapper.go — OffboardExcessMembers has exactly one caller,
	// household_over_capacity_check.go's autoOffboard (a background worker), never an HTTP
	// handler. #1371 — Round 9 Gap Analysis fix (third Codex adversarial pass).
	ErrOverCapacityAnchorMismatch = errors.New("over-capacity anchor mismatch")

	// ErrInviteeHasActivePaidSubscription is returned when an invitee has a live Paddle
	// billing relationship (active/trialing/past_due). Immediate free household coverage
	// would create paid-and-covered overlap, whose only fair unwind is a partial refund —
	// unsupported. Callers should surface the invitee's SubscriptionEndsAt as an advisory
	// eligibility hint. See #1367 decision #6 and the Phase 7 (#1371) scheduled-join follow-up.
	ErrInviteeHasActivePaidSubscription = errors.New("invitee has an active paid subscription")

	// ErrAlreadyCoveredByHousehold is returned by the checkout guard when a caller already
	// has an active source='household' entitlement and attempts to start their own paid
	// subscription. See #1367 decision #9.
	ErrAlreadyCoveredByHousehold = errors.New("user is already covered by a household plan")

	// ErrAccountNotOwned is returned when a caller attempts to set the household-share
	// visibility of a user_account they do not own. Mapped to 403 (not 404) — the caller
	// is authenticated and in a household, they simply lack rights over this specific
	// account. #1368 — Phase 4.
	ErrAccountNotOwned = errors.New("user account is not owned by the caller")
)

// InviteeHasActivePaidSubscriptionError wraps ErrInviteeHasActivePaidSubscription with
// the invitee's advisory eligibility date, so the HTTP layer can tell the client when
// they'll be able to join (#1367 decision #6, Codex review Medium finding 5).
// EligibleAfter may be nil — Paddle webhooks don't always keep SubscriptionEndsAt
// current, so callers must treat this as a hint, never authoritative. errors.Is still
// matches the base sentinel via Unwrap.
type InviteeHasActivePaidSubscriptionError struct {
	EligibleAfter *time.Time
}

func (e *InviteeHasActivePaidSubscriptionError) Error() string {
	return ErrInviteeHasActivePaidSubscription.Error()
}

func (e *InviteeHasActivePaidSubscriptionError) Unwrap() error {
	return ErrInviteeHasActivePaidSubscription
}

// HouseholdShareConsentVersion is the consent-copy version recorded on every
// household_share_consents row written by the accept flow. Bump this constant
// whenever the consent language changes materially. Finalized wording/versioning
// scheme was completed by issue #1372. The canonical copy source is
// apps/web/src/features/household/householdShareConsent.ts (which exports
// HOUSEHOLD_SHARE_CONSENT_VERSION as the frontend counterpart). The two
// version strings must stay in sync; TestHouseholdShareConsentVersion_MatchesFrontendConstant
// pins this literal value.
const HouseholdShareConsentVersion = "household-share-v1"

// ─── HouseholdShareConsent ───────────────────────────────────────────────────

// HouseholdShareConsent is an append-only audit record capturing the moment a user
// explicitly consented to household data sharing. Snapshot holds the list of accounts
// (and their visibility) that were shared at consent time.
// This table has no updated_at column — rows are never mutated.
//
// Issue #1365 — Phase 1.
type HouseholdShareConsent struct {
	ID             string          `json:"id" db:"id"`
	HouseholdID    string          `json:"household_id" db:"household_id"`
	UserID         string          `json:"user_id" db:"user_id"`
	ConsentVersion string          `json:"consent_version" db:"consent_version"`
	ConsentedAt    time.Time       `json:"consented_at" db:"consented_at"`
	Snapshot       json.RawMessage `json:"snapshot" db:"snapshot"`
	CreatedAt      time.Time       `json:"created_at" db:"created_at"`
}

// Validate checks that the HouseholdShareConsent has all required fields.
func (c *HouseholdShareConsent) Validate() error {
	if c.HouseholdID == "" {
		return errors.New("household_id is required")
	}
	if c.UserID == "" {
		return errors.New("user_id is required")
	}
	if c.ConsentVersion == "" {
		return errors.New("consent_version is required")
	}
	if len(c.Snapshot) == 0 {
		return errors.New("snapshot is required")
	}
	return nil
}
