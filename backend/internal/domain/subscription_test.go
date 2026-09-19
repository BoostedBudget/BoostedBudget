package domain

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestNormalizeSubscriptionStatus verifies the canonical Paddle→domain status mapping.
// #933: paused and canceled (single-l) both map to cancelled; all other values pass through.
func TestNormalizeSubscriptionStatus(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"paused maps to cancelled", "paused", "cancelled"},
		{"canceled (single-l) maps to cancelled", "canceled", "cancelled"},
		{"active passthrough", "active", "active"},
		{"trialing passthrough", "trialing", "trialing"},
		{"past_due passthrough", "past_due", "past_due"},
		{"trial passthrough", "trial", "trial"},
		{"inactive passthrough", "inactive", "inactive"},
		{"cancelled (double-l) passthrough", "cancelled", "cancelled"},
		{"empty passthrough", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeSubscriptionStatus(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSubscriptionStatus_IsActiveSubscription(t *testing.T) {
	tests := []struct {
		name     string
		status   SubscriptionStatus
		expected bool
	}{
		{
			name:     "active status should be active",
			status:   SubscriptionStatusActive,
			expected: true,
		},
		{
			name:     "trial status should be active",
			status:   SubscriptionStatusTrial,
			expected: true,
		},
		{
			name:     "trialing status should be active",
			status:   SubscriptionStatusTrialing,
			expected: true,
		},
		{
			name:     "inactive status should not be active",
			status:   SubscriptionStatusInactive,
			expected: false,
		},
		{
			name:     "cancelled status should not be active",
			status:   SubscriptionStatusCancelled,
			expected: false,
		},
		{
			name:     "past_due status should not be active",
			status:   SubscriptionStatusPastDue,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.status.IsActiveSubscription()
			assert.Equal(t, tt.expected, result)
		})
	}
}

// ─── SubscriptionEntitlement ─────────────────────────────────────────────────

func TestSubscriptionEntitlement_Validate(t *testing.T) {
	t.Parallel()
	now := time.Now()
	later := now.Add(30 * 24 * time.Hour)
	hhID := "hh-uuid"
	payerID := "payer-uuid"

	cases := []struct {
		name    string
		e       SubscriptionEntitlement
		wantErr string
	}{
		{
			name: "valid own_subscription",
			e: SubscriptionEntitlement{
				UserID: "u-1", Source: EntitlementSourceOwn, Tier: string(TierPremium),
				Status: string(SubscriptionStatusActive), EffectiveFrom: now,
			},
			wantErr: "",
		},
		{
			name: "valid household entitlement",
			e: SubscriptionEntitlement{
				UserID: "u-1", Source: EntitlementSourceHousehold, Tier: string(TierHousehold2),
				Status: string(SubscriptionStatusActive), EffectiveFrom: now,
				HouseholdID: &hhID, PayerUserID: &payerID,
			},
			wantErr: "",
		},
		{
			name: "valid with effective_until",
			e: SubscriptionEntitlement{
				UserID: "u-1", Source: EntitlementSourceOwn, Tier: string(TierPremium),
				Status: string(SubscriptionStatusActive), EffectiveFrom: now, EffectiveUntil: &later,
			},
			wantErr: "",
		},
		{
			name:    "missing user_id",
			e:       SubscriptionEntitlement{Source: EntitlementSourceOwn, Tier: string(TierPremium), Status: string(SubscriptionStatusActive), EffectiveFrom: now},
			wantErr: "user_id is required",
		},
		{
			name:    "missing tier",
			e:       SubscriptionEntitlement{UserID: "u-1", Source: EntitlementSourceOwn, Status: string(SubscriptionStatusActive), EffectiveFrom: now},
			wantErr: "tier is required",
		},
		{
			name:    "missing status",
			e:       SubscriptionEntitlement{UserID: "u-1", Source: EntitlementSourceOwn, Tier: string(TierPremium), EffectiveFrom: now},
			wantErr: "status is required",
		},
		{
			name:    "invalid source",
			e:       SubscriptionEntitlement{UserID: "u-1", Source: "employer", Tier: string(TierPremium), Status: string(SubscriptionStatusActive), EffectiveFrom: now},
			wantErr: "source must be one of: own_subscription, household",
		},
		{
			name: "household source without household_id",
			e: SubscriptionEntitlement{
				UserID: "u-1", Source: EntitlementSourceHousehold, Tier: string(TierHousehold2),
				Status: string(SubscriptionStatusActive), EffectiveFrom: now, PayerUserID: &payerID,
			},
			wantErr: "household_id and payer_user_id are required when source is household",
		},
		{
			name: "household source without payer_user_id",
			e: SubscriptionEntitlement{
				UserID: "u-1", Source: EntitlementSourceHousehold, Tier: string(TierHousehold2),
				Status: string(SubscriptionStatusActive), EffectiveFrom: now, HouseholdID: &hhID,
			},
			wantErr: "household_id and payer_user_id are required when source is household",
		},
		{
			name: "own_subscription source with household_id",
			e: SubscriptionEntitlement{
				UserID: "u-1", Source: EntitlementSourceOwn, Tier: string(TierPremium),
				Status: string(SubscriptionStatusActive), EffectiveFrom: now, HouseholdID: &hhID,
			},
			wantErr: "household_id and payer_user_id must be nil when source is own_subscription",
		},
		{
			name:    "invalid status",
			e:       SubscriptionEntitlement{UserID: "u-1", Source: EntitlementSourceOwn, Tier: string(TierPremium), Status: "bogus", EffectiveFrom: now},
			wantErr: "status must be one of: active, inactive, trial, trialing, cancelled, past_due",
		},
		{
			name: "effective_until not after effective_from",
			e: SubscriptionEntitlement{
				UserID: "u-1", Source: EntitlementSourceOwn, Tier: string(TierPremium),
				Status: string(SubscriptionStatusActive), EffectiveFrom: later, EffectiveUntil: &now,
			},
			wantErr: "effective_until must be after effective_from",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.e.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("expected error %q, got nil", tc.wantErr)
				} else if err.Error() != tc.wantErr {
					t.Errorf("expected error %q, got %q", tc.wantErr, err.Error())
				}
			}
		})
	}
}

func TestSubscriptionStatus_IsReadOnlySubscription(t *testing.T) {
	tests := []struct {
		name     string
		status   SubscriptionStatus
		expected bool
	}{
		{
			name:     "active status should not be read-only",
			status:   SubscriptionStatusActive,
			expected: false,
		},
		{
			name:     "trial status should not be read-only",
			status:   SubscriptionStatusTrial,
			expected: false,
		},
		{
			name:     "trialing status should not be read-only",
			status:   SubscriptionStatusTrialing,
			expected: false,
		},
		{
			name:     "inactive status should be read-only",
			status:   SubscriptionStatusInactive,
			expected: true,
		},
		{
			name:     "cancelled status should be read-only",
			status:   SubscriptionStatusCancelled,
			expected: true,
		},
		{
			name:     "past_due status should be read-only",
			status:   SubscriptionStatusPastDue,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.status.IsReadOnlySubscription()
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestSubscriptionStatus_IsEligibleForPlaidDisconnectRepair is the RED test for Issue #1413
// round-8 High 4 (Codex review): this method must be NARROWER than IsReadOnlySubscription() above
// — specifically, past_due must be excluded, even though it IS read-only — because past_due has
// its own dunning timeline (the deleted past_due_service.go, #1652) that this repair pass's 72-hour grace window must
// not bypass.
func TestSubscriptionStatus_IsEligibleForPlaidDisconnectRepair(t *testing.T) {
	tests := []struct {
		name     string
		status   SubscriptionStatus
		expected bool
	}{
		{name: "active status is not eligible", status: SubscriptionStatusActive, expected: false},
		{name: "trial status is not eligible", status: SubscriptionStatusTrial, expected: false},
		{name: "trialing status is not eligible", status: SubscriptionStatusTrialing, expected: false},
		{name: "inactive status is eligible", status: SubscriptionStatusInactive, expected: true},
		{name: "cancelled status is eligible", status: SubscriptionStatusCancelled, expected: true},
		{
			name:     "past_due status is READ-ONLY but must NOT be eligible — it has its own dunning timeline",
			status:   SubscriptionStatusPastDue,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.status.IsEligibleForPlaidDisconnectRepair()
			assert.Equal(t, tt.expected, result)
			// Falsifiability: IsEligibleForPlaidDisconnectRepair must never be a bare alias for
			// IsReadOnlySubscription() — if it were, this line would catch past_due disagreeing.
			if tt.status == SubscriptionStatusPastDue {
				assert.True(t, tt.status.IsReadOnlySubscription(), "sanity check: past_due must still be read-only")
				assert.NotEqual(t, tt.status.IsReadOnlySubscription(), result,
					"past_due must diverge between IsReadOnlySubscription and IsEligibleForPlaidDisconnectRepair")
			}
		})
	}
}

func TestEffectiveEntitlement_ZeroValueIsOwnSource(t *testing.T) {
	var e EffectiveEntitlement
	if e.Source != "" {
		t.Fatalf("expected zero-value Source to be empty, got %q", e.Source)
	}
}

func TestHouseholdEntitlementSummary_JSONTags(t *testing.T) {
	s := HouseholdEntitlementSummary{
		Tier:          "household_2",
		UsersIncluded: 2,
		SeatsActive:   2,
		SeatsPending:  1,
		IsOwner:       true,
		Source:        EntitlementSourceHousehold,
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"tier":"household_2","users_included":2,"seats_active":2,"seats_pending":1,"is_owner":true,"source":"household"}`
	if string(b) != want {
		t.Fatalf("got %s, want %s", b, want)
	}
}

// TestEffectiveSubscription_UsersIncludedJSONTag closes a gap found by
// requirements-gap-analysis: UsersIncluded's population is tested at the
// service layer, but its JSON serialization was never asserted anywhere.
// Uses a map-decode rather than an exact-string match so this test doesn't
// become brittle to unrelated changes in the nested Limits/Features structs.
func TestEffectiveSubscription_UsersIncludedJSONTag(t *testing.T) {
	s := EffectiveSubscription{
		UserID:        "user-1",
		Tier:          TierHousehold2,
		UsersIncluded: 2,
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got, ok := decoded["users_included"]
	if !ok {
		t.Fatalf("expected \"users_included\" key in JSON output, got %s", b)
	}
	if got != float64(2) {
		t.Fatalf("expected users_included=2, got %v", got)
	}
}
