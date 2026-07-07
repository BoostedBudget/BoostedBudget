package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
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
	if len(trimmed) > 255 {
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
	ID                     string                `json:"id" db:"id"`
	HouseholdID            string                `json:"household_id" db:"household_id"`
	InviterUserID          string                `json:"inviter_user_id" db:"inviter_user_id"`
	InviteeEmail           string                `json:"invitee_email" db:"invitee_email"` // plaintext in memory; ciphertext at rest
	InviteeEmailBlindIdx   string                `json:"-" db:"invitee_email_blind_idx"`   // repo-internal HMAC; never serialised to JSON
	TokenHash              string                `json:"-" db:"token_hash"`
	Status                 HouseholdInviteStatus `json:"status" db:"status"`
	ExpiresAt              time.Time             `json:"expires_at" db:"expires_at"`
	AcceptedUserID         *string               `json:"accepted_user_id,omitempty" db:"accepted_user_id"`
	AcceptedAt             *time.Time            `json:"accepted_at,omitempty" db:"accepted_at"`
	CreatedAt              time.Time             `json:"created_at" db:"created_at"`
	UpdatedAt              time.Time             `json:"updated_at" db:"updated_at"`
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
// physical_account_blind_idx is a repo-internal HMAC column for joint-account dedup
// and is not surfaced in this struct.
// owner_user_id is not stored here; it is derived from user_accounts.user_id at query time.
//
// Issue #1365 — Phase 1.
type HouseholdAccountShare struct {
	ID            string          `json:"id" db:"id"`
	HouseholdID   string          `json:"household_id" db:"household_id"`
	UserAccountID string          `json:"user_account_id" db:"user_account_id"`
	Visibility    ShareVisibility `json:"visibility" db:"visibility"`
	SharedAt      time.Time       `json:"shared_at" db:"shared_at"`
	RevokedAt     *time.Time      `json:"revoked_at,omitempty" db:"revoked_at"`
	CreatedAt     time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at" db:"updated_at"`
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
