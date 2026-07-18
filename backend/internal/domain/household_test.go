package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func assertValidateError(t *testing.T, err error, wantErr string) {
	t.Helper()
	if wantErr == "" {
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	} else if err == nil {
		t.Errorf("expected error %q, got nil", wantErr)
	} else if err.Error() != wantErr {
		t.Errorf("expected error %q, got %q", wantErr, err.Error())
	}
}

// ─── Household ───────────────────────────────────────────────────────────────

func TestHousehold_Validate(t *testing.T) {
	t.Parallel()
	ownerID := "owner-uuid"

	cases := []struct {
		name    string
		h       Household
		wantErr string
	}{
		{
			name:    "valid minimal",
			h:       Household{OwnerUserID: ownerID, Name: "My Household"},
			wantErr: "",
		},
		{
			name:    "missing owner_user_id",
			h:       Household{Name: "Test"},
			wantErr: "owner_user_id is required",
		},
		{
			name:    "empty name",
			h:       Household{OwnerUserID: ownerID, Name: ""},
			wantErr: "name is required",
		},
		{
			name:    "whitespace only name",
			h:       Household{OwnerUserID: ownerID, Name: "   "},
			wantErr: "name is required",
		},
		{
			name:    "name too long (ASCII)",
			h:       Household{OwnerUserID: ownerID, Name: strings.Repeat("a", 256)},
			wantErr: "name must not exceed 255 characters",
		},
		{
			name:    "name exactly 255 chars (ASCII)",
			h:       Household{OwnerUserID: ownerID, Name: strings.Repeat("a", 255)},
			wantErr: "",
		},
		{
			name:    "name exactly 255 runes (multibyte) is valid",
			h:       Household{OwnerUserID: ownerID, Name: strings.Repeat("é", 255)},
			wantErr: "",
		},
		{
			name:    "name 256 runes (multibyte) is too long",
			h:       Household{OwnerUserID: ownerID, Name: strings.Repeat("é", 256)},
			wantErr: "name must not exceed 255 characters",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertValidateError(t, tc.h.Validate(), tc.wantErr)
		})
	}
}

// ─── HouseholdMember ─────────────────────────────────────────────────────────

func TestHouseholdMember_Validate(t *testing.T) {
	t.Parallel()
	now := time.Now()

	cases := []struct {
		name    string
		m       HouseholdMember
		wantErr string
	}{
		{
			name:    "valid owner",
			m:       HouseholdMember{HouseholdID: "hh-1", UserID: "u-1", Role: HouseholdRoleOwner, Status: HouseholdMemberStatusActive},
			wantErr: "",
		},
		{
			name:    "valid member",
			m:       HouseholdMember{HouseholdID: "hh-1", UserID: "u-2", Role: HouseholdRoleMember, Status: HouseholdMemberStatusActive},
			wantErr: "",
		},
		{
			name:    "valid removed with timestamp",
			m:       HouseholdMember{HouseholdID: "hh-1", UserID: "u-1", Role: HouseholdRoleMember, Status: HouseholdMemberStatusRemoved, RemovedAt: &now},
			wantErr: "",
		},
		{
			name:    "missing household_id",
			m:       HouseholdMember{UserID: "u-1", Role: HouseholdRoleOwner, Status: HouseholdMemberStatusActive},
			wantErr: "household_id is required",
		},
		{
			name:    "missing user_id",
			m:       HouseholdMember{HouseholdID: "hh-1", Role: HouseholdRoleOwner, Status: HouseholdMemberStatusActive},
			wantErr: "user_id is required",
		},
		{
			name:    "invalid role",
			m:       HouseholdMember{HouseholdID: "hh-1", UserID: "u-1", Role: "admin", Status: HouseholdMemberStatusActive},
			wantErr: "role must be one of: owner, member",
		},
		{
			name:    "invalid status",
			m:       HouseholdMember{HouseholdID: "hh-1", UserID: "u-1", Role: HouseholdRoleMember, Status: "banned"},
			wantErr: "status must be one of: active, removed",
		},
		{
			name:    "active with removed_at set",
			m:       HouseholdMember{HouseholdID: "hh-1", UserID: "u-1", Role: HouseholdRoleMember, Status: HouseholdMemberStatusActive, RemovedAt: &now},
			wantErr: "removed_at must be nil when status is active",
		},
		{
			name:    "removed without removed_at",
			m:       HouseholdMember{HouseholdID: "hh-1", UserID: "u-1", Role: HouseholdRoleMember, Status: HouseholdMemberStatusRemoved},
			wantErr: "removed_at is required when status is removed",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertValidateError(t, tc.m.Validate(), tc.wantErr)
		})
	}
}

// ─── HouseholdInvite ─────────────────────────────────────────────────────────

func TestHouseholdInvite_Validate(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(24 * time.Hour)
	past := time.Now().Add(-1 * time.Hour)
	acceptedAt := time.Now()

	cases := []struct {
		name    string
		inv     HouseholdInvite
		wantErr string
	}{
		{
			name: "valid pending",
			inv: HouseholdInvite{
				HouseholdID:   "hh-1",
				InviterUserID: "u-1",
				InviteeEmail:  "bob@example.com",
				TokenHash:     "tok",
				Status:        HouseholdInviteStatusPending,
				ExpiresAt:     future,
			},
			wantErr: "",
		},
		{
			name: "valid accepted with accepted_at",
			inv: HouseholdInvite{
				HouseholdID:   "hh-1",
				InviterUserID: "u-1",
				InviteeEmail:  "bob@example.com",
				TokenHash:     "tok",
				Status:        HouseholdInviteStatusAccepted,
				ExpiresAt:     past, // past expiry OK for non-pending
				AcceptedAt:    &acceptedAt,
			},
			wantErr: "",
		},
		{
			name: "valid declined with past expiry",
			inv: HouseholdInvite{
				HouseholdID:   "hh-1",
				InviterUserID: "u-1",
				InviteeEmail:  "bob@example.com",
				TokenHash:     "tok",
				Status:        HouseholdInviteStatusDeclined,
				ExpiresAt:     past,
			},
			wantErr: "",
		},
		{
			name: "missing household_id",
			inv: HouseholdInvite{
				InviterUserID: "u-1",
				InviteeEmail:  "bob@example.com",
				TokenHash:     "tok",
				Status:        HouseholdInviteStatusPending,
				ExpiresAt:     future,
			},
			wantErr: "household_id is required",
		},
		{
			name: "missing inviter_user_id",
			inv: HouseholdInvite{
				HouseholdID:  "hh-1",
				InviteeEmail: "bob@example.com",
				TokenHash:    "tok",
				Status:       HouseholdInviteStatusPending,
				ExpiresAt:    future,
			},
			wantErr: "inviter_user_id is required",
		},
		{
			name: "missing invitee_email",
			inv: HouseholdInvite{
				HouseholdID:   "hh-1",
				InviterUserID: "u-1",
				TokenHash:     "tok",
				Status:        HouseholdInviteStatusPending,
				ExpiresAt:     future,
			},
			wantErr: "invitee_email is required",
		},
		{
			name: "invalid status",
			inv: HouseholdInvite{
				HouseholdID:   "hh-1",
				InviterUserID: "u-1",
				InviteeEmail:  "bob@example.com",
				TokenHash:     "tok",
				Status:        "sent",
				ExpiresAt:     future,
			},
			wantErr: "status must be one of: pending, accepted, declined, revoked, expired",
		},
		{
			name: "pending with past expires_at",
			inv: HouseholdInvite{
				HouseholdID:   "hh-1",
				InviterUserID: "u-1",
				InviteeEmail:  "bob@example.com",
				TokenHash:     "tok",
				Status:        HouseholdInviteStatusPending,
				ExpiresAt:     past,
			},
			wantErr: "expires_at must be in the future for pending invites",
		},
		{
			name: "accepted without accepted_at",
			inv: HouseholdInvite{
				HouseholdID:   "hh-1",
				InviterUserID: "u-1",
				InviteeEmail:  "bob@example.com",
				TokenHash:     "tok",
				Status:        HouseholdInviteStatusAccepted,
				ExpiresAt:     past,
			},
			wantErr: "accepted_at is required when status is accepted",
		},
		{
			name: "missing token_hash",
			inv: HouseholdInvite{
				HouseholdID:   "hh-1",
				InviterUserID: "u-1",
				InviteeEmail:  "bob@example.com",
				Status:        HouseholdInviteStatusPending,
				ExpiresAt:     future,
			},
			wantErr: "token_hash is required",
		},
		{
			name: "whitespace only invitee_email",
			inv: HouseholdInvite{
				HouseholdID:   "hh-1",
				InviterUserID: "u-1",
				InviteeEmail:  "   ",
				TokenHash:     "tok",
				Status:        HouseholdInviteStatusPending,
				ExpiresAt:     future,
			},
			wantErr: "invitee_email is required",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertValidateError(t, tc.inv.Validate(), tc.wantErr)
		})
	}
}

// ─── HouseholdAccountShare ───────────────────────────────────────────────────

func TestHouseholdAccountShare_Validate(t *testing.T) {
	t.Parallel()
	sharedAt := time.Now()
	revokedAt := sharedAt.Add(time.Hour)
	tooEarly := sharedAt.Add(-time.Second)

	cases := []struct {
		name    string
		s       HouseholdAccountShare
		wantErr string
	}{
		{
			name:    "valid shared",
			s:       HouseholdAccountShare{HouseholdID: "hh-1", UserAccountID: "ua-1", Visibility: ShareVisibilityShared, SharedAt: sharedAt},
			wantErr: "",
		},
		{
			name:    "valid hidden",
			s:       HouseholdAccountShare{HouseholdID: "hh-1", UserAccountID: "ua-1", Visibility: ShareVisibilityHidden, SharedAt: sharedAt},
			wantErr: "",
		},
		{
			name:    "valid revoked",
			s:       HouseholdAccountShare{HouseholdID: "hh-1", UserAccountID: "ua-1", Visibility: ShareVisibilityShared, SharedAt: sharedAt, RevokedAt: &revokedAt},
			wantErr: "",
		},
		{
			name:    "missing household_id",
			s:       HouseholdAccountShare{UserAccountID: "ua-1", Visibility: ShareVisibilityShared, SharedAt: sharedAt},
			wantErr: "household_id is required",
		},
		{
			name:    "missing user_account_id",
			s:       HouseholdAccountShare{HouseholdID: "hh-1", Visibility: ShareVisibilityShared, SharedAt: sharedAt},
			wantErr: "user_account_id is required",
		},
		{
			name:    "invalid visibility",
			s:       HouseholdAccountShare{HouseholdID: "hh-1", UserAccountID: "ua-1", Visibility: "public", SharedAt: sharedAt},
			wantErr: "visibility must be one of: shared, hidden",
		},
		{
			name:    "revoked_at before shared_at",
			s:       HouseholdAccountShare{HouseholdID: "hh-1", UserAccountID: "ua-1", Visibility: ShareVisibilityShared, SharedAt: sharedAt, RevokedAt: &tooEarly},
			wantErr: "revoked_at must not be before shared_at",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertValidateError(t, tc.s.Validate(), tc.wantErr)
		})
	}
}

// ─── HouseholdShareConsent ───────────────────────────────────────────────────

func TestHouseholdShareConsent_Validate(t *testing.T) {
	t.Parallel()
	now := time.Now()

	cases := []struct {
		name    string
		c       HouseholdShareConsent
		wantErr string
	}{
		{
			name:    "valid with snapshot",
			c:       HouseholdShareConsent{HouseholdID: "hh-1", UserID: "u-1", ConsentVersion: "v1", ConsentedAt: now, Snapshot: json.RawMessage(`{"accounts":["ua-1"]}`)},
			wantErr: "",
		},
		{
			name:    "valid empty snapshot",
			c:       HouseholdShareConsent{HouseholdID: "hh-1", UserID: "u-1", ConsentVersion: "v1", ConsentedAt: now, Snapshot: json.RawMessage(`{}`)},
			wantErr: "",
		},
		{
			name:    "missing household_id",
			c:       HouseholdShareConsent{UserID: "u-1", ConsentVersion: "v1", ConsentedAt: now, Snapshot: json.RawMessage(`{}`)},
			wantErr: "household_id is required",
		},
		{
			name:    "missing user_id",
			c:       HouseholdShareConsent{HouseholdID: "hh-1", ConsentVersion: "v1", ConsentedAt: now, Snapshot: json.RawMessage(`{}`)},
			wantErr: "user_id is required",
		},
		{
			name:    "missing consent_version",
			c:       HouseholdShareConsent{HouseholdID: "hh-1", UserID: "u-1", ConsentedAt: now, Snapshot: json.RawMessage(`{}`)},
			wantErr: "consent_version is required",
		},
		{
			name:    "nil snapshot",
			c:       HouseholdShareConsent{HouseholdID: "hh-1", UserID: "u-1", ConsentVersion: "v1", ConsentedAt: now, Snapshot: nil},
			wantErr: "snapshot is required",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertValidateError(t, tc.c.Validate(), tc.wantErr)
		})
	}
}

// ─── Phase 3 sentinel errors (#1367) ─────────────────────────────────────────

func TestHouseholdSentinelErrors_AreDistinctAndNonNil(t *testing.T) {
	t.Parallel()

	errs := []error{
		ErrHouseholdNotFound,
		ErrAlreadyOwnsHousehold,
		ErrNotHouseholdOwner,
		ErrHouseholdHasActiveMembers,
		ErrHouseholdSeatsFull,
		ErrInviteNotFound,
		ErrInviteNotPending,
		ErrInviteExpired,
		ErrInviteEmailMismatch,
		ErrInvitePendingExists,
		ErrCannotInviteSelf,
		ErrAlreadyInHousehold,
		ErrInvalidConsent,
		ErrMemberNotFound,
		ErrCannotRemoveOwner,
		ErrOwnerPlanInactive,
		ErrInviteeHasActivePaidSubscription,
		ErrAlreadyCoveredByHousehold,
	}

	seen := make(map[string]int, len(errs))
	for i, e := range errs {
		if e == nil {
			t.Fatalf("sentinel error at index %d is nil", i)
		}
		if e.Error() == "" {
			t.Fatalf("sentinel error at index %d has empty message", i)
		}
		if prev, ok := seen[e.Error()]; ok {
			t.Fatalf("sentinel error message %q reused at indices %d and %d", e.Error(), prev, i)
		}
		seen[e.Error()] = i
	}
}

func TestHouseholdShareConsentVersion_IsNonEmpty(t *testing.T) {
	t.Parallel()
	if strings.TrimSpace(HouseholdShareConsentVersion) == "" {
		t.Fatal("HouseholdShareConsentVersion must not be empty")
	}
}

// TestHouseholdShareConsentVersion_MatchesFrontendConstant pins the literal
// value of HouseholdShareConsentVersion. It MUST match the frontend's
// HOUSEHOLD_SHARE_CONSENT_VERSION constant
// (apps/web/src/features/household/householdShareConsent.ts) — issue #1372
// Task 1. If this test fails after an intentional change to the constant,
// update the frontend constant (and its own pinning test) to match.
func TestHouseholdShareConsentVersion_MatchesFrontendConstant(t *testing.T) {
	t.Parallel()
	const wantVersion = "household-share-v1"
	if HouseholdShareConsentVersion != wantVersion {
		t.Fatalf("HouseholdShareConsentVersion = %q, want %q (must match frontend householdShareConsent.ts)", HouseholdShareConsentVersion, wantVersion)
	}
}

func TestInviteeHasActivePaidSubscriptionError_MatchesSentinelViaErrorsIs(t *testing.T) {
	t.Parallel()
	when := time.Now().Add(48 * time.Hour)
	err := &InviteeHasActivePaidSubscriptionError{EligibleAfter: &when}

	if !errors.Is(err, ErrInviteeHasActivePaidSubscription) {
		t.Fatal("errors.Is must match the base sentinel through Unwrap")
	}
	if err.EligibleAfter == nil || !err.EligibleAfter.Equal(when) {
		t.Fatal("EligibleAfter must carry the date it was constructed with")
	}
}

func TestInviteeHasActivePaidSubscriptionError_NilDateMatchesSentinel(t *testing.T) {
	t.Parallel()
	err := &InviteeHasActivePaidSubscriptionError{EligibleAfter: nil}

	if !errors.Is(err, ErrInviteeHasActivePaidSubscription) {
		t.Fatal("errors.Is must match even when no date is known (advisory-only per decision #6)")
	}
}
