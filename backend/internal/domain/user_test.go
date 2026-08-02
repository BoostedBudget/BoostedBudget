package domain

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeTrialDaysRemaining_ActiveTrial(t *testing.T) {
	// User on trial with 30 days remaining
	trialEndsAt := time.Now().Add(30 * 24 * time.Hour)
	user := &User{
		SubscriptionStatus: "trial",
		TrialEndsAt:        &trialEndsAt,
	}

	user.ComputeTrialDaysRemaining()

	assert.NotNil(t, user.TrialDaysRemaining)
	// Allow for 1 day variance due to timing
	assert.InDelta(t, 30, *user.TrialDaysRemaining, 1)
}

func TestComputeTrialDaysRemaining_ExpiredTrial(t *testing.T) {
	// User on trial but trial has expired
	trialEndsAt := time.Now().Add(-24 * time.Hour)
	user := &User{
		SubscriptionStatus: "trial",
		TrialEndsAt:        &trialEndsAt,
	}

	user.ComputeTrialDaysRemaining()

	assert.NotNil(t, user.TrialDaysRemaining)
	assert.Equal(t, 0, *user.TrialDaysRemaining)
}

func TestComputeTrialDaysRemaining_NotOnTrial(t *testing.T) {
	// User with active subscription (not trial)
	trialEndsAt := time.Now().Add(30 * 24 * time.Hour)
	user := &User{
		SubscriptionStatus: "active",
		TrialEndsAt:        &trialEndsAt,
	}

	user.ComputeTrialDaysRemaining()

	assert.Nil(t, user.TrialDaysRemaining)
}

func TestComputeTrialDaysRemaining_NoTrialEndsAt(t *testing.T) {
	// User on trial but no trial_ends_at set (shouldn't happen but handle gracefully)
	user := &User{
		SubscriptionStatus: "trial",
		TrialEndsAt:        nil,
	}

	user.ComputeTrialDaysRemaining()

	assert.Nil(t, user.TrialDaysRemaining)
}

func TestComputeTrialDaysRemaining_InactiveTrial(t *testing.T) {
	// User with inactive status
	trialEndsAt := time.Now().Add(30 * 24 * time.Hour)
	user := &User{
		SubscriptionStatus: "inactive",
		TrialEndsAt:        &trialEndsAt,
	}

	user.ComputeTrialDaysRemaining()

	assert.Nil(t, user.TrialDaysRemaining)
}

func TestNewUser_DefaultValues(t *testing.T) {
	user := NewUser("clerk_123", "test@example.com", true)

	// Verify UUID is generated
	assert.NotEmpty(t, user.ID, "NewUser should generate a UUID for ID")
	assert.NotEmpty(t, user.ClerkUserID)
	assert.Equal(t, "clerk_123", user.ClerkUserID)
	assert.Equal(t, "test@example.com", user.Email)
	assert.True(t, user.EmailVerified)
	assert.Equal(t, "premium", user.SubscriptionTier)
	assert.Equal(t, "trial", user.SubscriptionStatus)
	assert.Equal(t, "iad", user.PrimaryRegion)
	assert.True(t, user.IsActive)
	assert.False(t, user.IsDeleted)
}

// TestUser_PaddleSubscriptionID_JSONMarshaling tests JSON marshaling/unmarshaling
func TestUser_PaddleSubscriptionID_JSONMarshaling(t *testing.T) {
	subscriptionID := "sub_01h12345"
	user := &User{
		ID:                   "user-123",
		ClerkUserID:          "clerk_123",
		Email:                "test@example.com",
		PaddleSubscriptionID: &subscriptionID,
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(user)
	require.NoError(t, err)

	// Verify paddle_subscription_id is in JSON
	assert.Contains(t, string(jsonData), "paddle_subscription_id")
	assert.Contains(t, string(jsonData), "sub_01h12345")

	// Unmarshal back
	var unmarshaled User
	err = json.Unmarshal(jsonData, &unmarshaled)
	require.NoError(t, err)

	// Verify field is preserved
	assert.NotNil(t, unmarshaled.PaddleSubscriptionID)
	assert.Equal(t, "sub_01h12345", *unmarshaled.PaddleSubscriptionID)
}

// TestUser_PaddleSubscriptionID_NullHandling tests null value handling
func TestUser_PaddleSubscriptionID_NullHandling(t *testing.T) {
	user := &User{
		ID:                   "user-123",
		ClerkUserID:          "clerk_123",
		Email:                "test@example.com",
		PaddleSubscriptionID: nil, // No subscription ID
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(user)
	require.NoError(t, err)

	// Unmarshal back
	var unmarshaled User
	err = json.Unmarshal(jsonData, &unmarshaled)
	require.NoError(t, err)

	// Verify null is handled correctly
	assert.Nil(t, unmarshaled.PaddleSubscriptionID)
}

// TestComputeEffectiveTier_WithOverride tests that an active override returns the override tier
func TestComputeEffectiveTier_WithOverride(t *testing.T) {
	override := "household_2"
	user := &User{
		SubscriptionTier:   "premium",
		ManualTierOverride: &override,
		ManualTierEndsAt:   nil, // permanent override
	}

	result := user.ComputeEffectiveTier()

	assert.Equal(t, "household_2", result)
}

// TestComputeEffectiveTier_WithExpiredOverride tests that an expired override returns the base tier
func TestComputeEffectiveTier_WithExpiredOverride(t *testing.T) {
	override := "household_2"
	expired := time.Now().Add(-24 * time.Hour)
	user := &User{
		SubscriptionTier:   "premium",
		ManualTierOverride: &override,
		ManualTierEndsAt:   &expired,
	}

	result := user.ComputeEffectiveTier()

	assert.Equal(t, "premium", result)
}

// TestComputeEffectiveTier_WithPermanentOverride tests nil ends_at returns override tier
func TestComputeEffectiveTier_WithPermanentOverride(t *testing.T) {
	override := "household_3"
	user := &User{
		SubscriptionTier:   "premium",
		ManualTierOverride: &override,
		ManualTierEndsAt:   nil,
	}

	result := user.ComputeEffectiveTier()

	assert.Equal(t, "household_3", result)
}

// TestComputeEffectiveTier_NoOverride tests that nil override returns base tier
func TestComputeEffectiveTier_NoOverride(t *testing.T) {
	user := &User{
		SubscriptionTier:   "premium",
		ManualTierOverride: nil,
	}

	result := user.ComputeEffectiveTier()

	assert.Equal(t, "premium", result)
}

// TestComputeEffectiveTier_FutureExpiry tests that a future ends_at still returns override
func TestComputeEffectiveTier_FutureExpiry(t *testing.T) {
	override := "household_2"
	future := time.Now().Add(30 * 24 * time.Hour)
	user := &User{
		SubscriptionTier:   "premium",
		ManualTierOverride: &override,
		ManualTierEndsAt:   &future,
	}

	result := user.ComputeEffectiveTier()

	assert.Equal(t, "household_2", result)
}

// TestHasActiveManualOverride tests the extracted override predicate across the same matrix
// used for ComputeEffectiveTier: nil override, empty-string override, no expiry, future expiry,
// and past expiry.
func TestHasActiveManualOverride(t *testing.T) {
	override := "household_2"
	empty := ""
	future := time.Now().Add(24 * time.Hour)
	past := time.Now().Add(-24 * time.Hour)

	tests := []struct {
		name     string
		user     *User
		expected bool
	}{
		{"nil override", &User{ManualTierOverride: nil}, false},
		{"empty string override", &User{ManualTierOverride: &empty}, false},
		{"no expiry (permanent override)", &User{ManualTierOverride: &override, ManualTierEndsAt: nil}, true},
		{"future expiry", &User{ManualTierOverride: &override, ManualTierEndsAt: &future}, true},
		{"past expiry", &User{ManualTierOverride: &override, ManualTierEndsAt: &past}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.user.HasActiveManualOverride())
		})
	}
}

// TestHasAnyManualOverride tests the ANY-override predicate (live OR already lapsed) needed by
// belt-and-braces guards that must agree with the retention queries' "exclude on any override"
// rule (noManualOverrideAtAllPredicate in profile_repository.go), rather than
// HasActiveManualOverride's "still live right now" rule. Issue #1413 Critical finding C1: a comp
// that has lapsed but not yet been cleared by the daily expiry job must still count here, unlike
// HasActiveManualOverride where a past expiry means "no active override."
func TestHasAnyManualOverride(t *testing.T) {
	override := "household_2"
	empty := ""
	future := time.Now().Add(24 * time.Hour)
	past := time.Now().Add(-24 * time.Hour)

	tests := []struct {
		name     string
		user     *User
		expected bool
	}{
		{"nil override", &User{ManualTierOverride: nil}, false},
		{"empty string override", &User{ManualTierOverride: &empty}, false},
		{"no expiry (permanent override)", &User{ManualTierOverride: &override, ManualTierEndsAt: nil}, true},
		{"future expiry (live)", &User{ManualTierOverride: &override, ManualTierEndsAt: &future}, true},
		{"past expiry (lapsed but uncleared) — unlike HasActiveManualOverride, still true", &User{ManualTierOverride: &override, ManualTierEndsAt: &past}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.user.HasAnyManualOverride())
		})
	}
}

// TestHasActiveManualOverrideAt_BoundarySemantics locks in the `!ManualTierEndsAt.Before(now)`
// semantics at the exact expiry boundary, using a fixed `now` passed explicitly to the -At
// variant. Because -At takes the timestamp as a parameter rather than reading the clock, an
// exact tie (ends_at == now) is fully deterministic here — the flakiness that would come from
// trying to hit "exactly now" against a live clock does not apply.
//
// The exact-tie case is the one that matters: the ±1ms cases below pass identically whether the
// comparison is `>=` or `>`, so they cannot pin down which semantic is intended. Only the tie
// distinguishes them. This mirrors, on the Go side, what
// TestNoActiveManualOverridePredicate_Integration_ExactExpiryBoundary pins on the SQL side —
// together they are what keeps the two expressions of this rule from drifting apart.
func TestHasActiveManualOverrideAt_BoundarySemantics(t *testing.T) {
	override := "household_2"
	now := time.Now()

	t.Run("ends_at exactly equal to now is still active", func(t *testing.T) {
		endsAt := now
		user := &User{ManualTierOverride: &override, ManualTierEndsAt: &endsAt}
		assert.True(t, user.hasActiveManualOverrideAt(now),
			"an override ending exactly at now must still count as active: "+
				"!Before(now) is true on a tie, which is why the SQL predicate uses >= and not >")
	})

	t.Run("ends_at 1ms after now is still active", func(t *testing.T) {
		endsAt := now.Add(time.Millisecond)
		user := &User{ManualTierOverride: &override, ManualTierEndsAt: &endsAt}
		assert.True(t, user.hasActiveManualOverrideAt(now))
	})

	t.Run("ends_at 1ms before now is expired", func(t *testing.T) {
		endsAt := now.Add(-time.Millisecond)
		user := &User{ManualTierOverride: &override, ManualTierEndsAt: &endsAt}
		assert.False(t, user.hasActiveManualOverrideAt(now))
	})
}

// TestHasActiveManualOverrideAt_SubMicrosecondTruncationIsLoadBearing proves WHY
// AdminUserService.UpdateUserTier floors a caller-supplied expiresAt to microsecond precision
// before it is both persisted and evaluated in Go (Issue #1413 round-7 gap J5).
//
// Postgres stores manual_tier_ends_at as timestamptz — MICROSECOND precision. Go's time.Time
// carries NANOSECONDS, and expiresAt arrives as a JSON-decoded *time.Time with no precision
// constraint. Left untruncated, the in-memory value and its own persisted form are DIFFERENT
// instants, and this test picks the one moment where that difference is observable: an instant
// strictly between the truncated value and the raw one. There, the two disagree about whether the
// very same override is still active.
//
// This is the deterministic, constructible counterpart to the test that is NOT constructible — a
// FENCED cross-system (Go-predicate vs SQL-predicate) integration test, where the shared comparison
// instant must come from a single SELECT NOW() and is therefore itself microsecond-aligned, which
// makes floor-truncation monotonic with respect to it and the two sides unable to disagree. See
// profile_repository_manual_override_retention_integration_test.go for that argument. The property
// worth pinning is this one, and it lives here.
//
// Added after Codex adversarial review of the round-7 diff observed, correctly, that the original
// "not constructible" claim was stated too broadly.
func TestHasActiveManualOverrideAt_SubMicrosecondTruncationIsLoadBearing(t *testing.T) {
	override := "household_2"

	// 789ns past a microsecond boundary — representable in Go, not in timestamptz.
	raw := time.Date(2027, 3, 4, 5, 6, 7, 123456789, time.UTC)
	truncated := raw.Truncate(time.Microsecond)
	require.NotEqual(t, raw, truncated, "fixture must actually carry sub-microsecond precision")

	// An instant inside the discarded remainder: after what Postgres would store, at or before what
	// Go was handed. This is the ONLY window where the two representations disagree.
	now := truncated.Add(500 * time.Nanosecond)
	require.True(t, now.After(truncated) && !now.After(raw), "fixture must sit strictly inside the truncated remainder")

	rawUser := &User{ManualTierOverride: &override, ManualTierEndsAt: &raw}
	truncatedUser := &User{ManualTierOverride: &override, ManualTierEndsAt: &truncated}

	assert.True(t, rawUser.hasActiveManualOverrideAt(now),
		"the untruncated in-memory value still reads as ACTIVE at this instant")
	assert.False(t, truncatedUser.hasActiveManualOverrideAt(now),
		"its own persisted (floored) form reads as EXPIRED at the same instant — this divergence is "+
			"exactly what truncating once at the service boundary eliminates, by making the value "+
			"handed to Go and the value written to Postgres the same instant by construction")
}

// TestComputeEffectiveTier_Characterization is a characterization test proving the
// ComputeEffectiveTier refactor (delegating onto HasActiveManualOverride) is
// behaviour-preserving across the same matrix already covered by the individual
// TestComputeEffectiveTier_* tests above. 12 production call sites depend on this method
// keeping identical behaviour.
func TestComputeEffectiveTier_Characterization(t *testing.T) {
	override := "household_2"
	empty := ""
	future := time.Now().Add(24 * time.Hour)
	past := time.Now().Add(-24 * time.Hour)

	tests := []struct {
		name     string
		user     *User
		expected string
	}{
		{
			name:     "nil override returns base tier",
			user:     &User{SubscriptionTier: "premium", ManualTierOverride: nil},
			expected: "premium",
		},
		{
			name:     "empty string override returns base tier",
			user:     &User{SubscriptionTier: "premium", ManualTierOverride: &empty},
			expected: "premium",
		},
		{
			name:     "permanent override returns override tier",
			user:     &User{SubscriptionTier: "premium", ManualTierOverride: &override, ManualTierEndsAt: nil},
			expected: "household_2",
		},
		{
			name:     "future expiry returns override tier",
			user:     &User{SubscriptionTier: "premium", ManualTierOverride: &override, ManualTierEndsAt: &future},
			expected: "household_2",
		},
		{
			name:     "past expiry returns base tier",
			user:     &User{SubscriptionTier: "premium", ManualTierOverride: &override, ManualTierEndsAt: &past},
			expected: "premium",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.user.ComputeEffectiveTier())
		})
	}
}

// TestComputeEffectiveStatus_NoOverride verifies that with no active override, every known
// subscription status (plus the empty-string edge case) passes through unchanged.
func TestComputeEffectiveStatus_NoOverride(t *testing.T) {
	statuses := []string{"active", "inactive", "trial", "trialing", "cancelled", "past_due", ""}

	for _, status := range statuses {
		name := status
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			user := &User{SubscriptionStatus: status}
			assert.Equal(t, status, user.ComputeEffectiveStatus())
		})
	}
}

// TestComputeEffectiveStatus_WithActiveOverride verifies that any active manual tier override
// forces the effective status to "active", regardless of the underlying raw status — this is
// the core fix for Issue #1413 (a comped user must not stay locked out by a stale status).
func TestComputeEffectiveStatus_WithActiveOverride(t *testing.T) {
	override := "household_2"
	statuses := []string{"inactive", "cancelled", "past_due", "trial"}

	for _, status := range statuses {
		t.Run(status, func(t *testing.T) {
			user := &User{
				SubscriptionStatus: status,
				ManualTierOverride: &override,
				ManualTierEndsAt:   nil,
			}
			assert.Equal(t, string(SubscriptionStatusActive), user.ComputeEffectiveStatus())
		})
	}
}

// TestComputeEffectiveStatus_WithExpiredOverride verifies an expired override falls back to
// the raw status, mirroring ComputeEffectiveTier's expiry behaviour.
func TestComputeEffectiveStatus_WithExpiredOverride(t *testing.T) {
	override := "household_2"
	expired := time.Now().Add(-24 * time.Hour)
	user := &User{
		SubscriptionStatus: "past_due",
		ManualTierOverride: &override,
		ManualTierEndsAt:   &expired,
	}

	assert.Equal(t, "past_due", user.ComputeEffectiveStatus())
}

// TestEffectiveTierAndStatusAt_AgreeAtSharedBoundary proves the -At() variants make tier and
// status agree at a shared boundary timestamp. Evaluating both against the SAME `now` value
// (rather than each calling time.Now() independently) guarantees they can never disagree about
// whether the override is active, even nanoseconds from expiry.
func TestEffectiveTierAndStatusAt_AgreeAtSharedBoundary(t *testing.T) {
	override := "household_2"
	now := time.Now()

	t.Run("1ms before expiry: both see the override as active", func(t *testing.T) {
		endsAt := now.Add(time.Millisecond)
		user := &User{
			SubscriptionTier:   "premium",
			SubscriptionStatus: "past_due",
			ManualTierOverride: &override,
			ManualTierEndsAt:   &endsAt,
		}

		assert.Equal(t, "household_2", user.computeEffectiveTierAt(now))
		assert.Equal(t, string(SubscriptionStatusActive), user.computeEffectiveStatusAt(now))
	})

	t.Run("1ms after expiry: both see the override as expired", func(t *testing.T) {
		endsAt := now.Add(-time.Millisecond)
		user := &User{
			SubscriptionTier:   "premium",
			SubscriptionStatus: "past_due",
			ManualTierOverride: &override,
			ManualTierEndsAt:   &endsAt,
		}

		assert.Equal(t, "premium", user.computeEffectiveTierAt(now))
		assert.Equal(t, "past_due", user.computeEffectiveStatusAt(now))
	})
}

// TestClerkPublicMetadata_Comped verifies a comped user's payload reflects the override tier
// and the forced "active" status.
func TestClerkPublicMetadata_Comped(t *testing.T) {
	override := "household_2"
	user := &User{
		SubscriptionTier:   "premium",
		SubscriptionStatus: "past_due",
		ManualTierOverride: &override,
		ManualTierEndsAt:   nil,
	}

	metadata := user.ClerkPublicMetadata()

	assert.Equal(t, "household_2", metadata["subscription_tier"])
	assert.Equal(t, string(SubscriptionStatusActive), metadata["subscription_status"])
}

// TestClerkPublicMetadata_NotComped verifies a non-comped user's payload reflects the raw tier
// and raw status unchanged.
func TestClerkPublicMetadata_NotComped(t *testing.T) {
	user := &User{
		SubscriptionTier:   "premium",
		SubscriptionStatus: "trial",
		ManualTierOverride: nil,
	}

	metadata := user.ClerkPublicMetadata()

	assert.Equal(t, "premium", metadata["subscription_tier"])
	assert.Equal(t, "trial", metadata["subscription_status"])
}

// TestClerkPublicMetadata_TierAndStatusAgreeAcrossExpiryBoundary closes the gap left by
// TestEffectiveTierAndStatusAt_AgreeAtSharedBoundary, which only exercises the private -At()
// helpers directly against a single, already-shared `now`. ClerkPublicMetadata() cannot have
// `now` injected — it reads the clock internally — so before the fix it computed
// subscription_tier and subscription_status from two independent time.Now() reads (via
// ComputeEffectiveTier() and ComputeEffectiveStatus()), which could disagree about whether the
// override was still live if a clock read straddled the expiry boundary between the two calls.
//
// Since we cannot force which side of the boundary the internal clock lands on, this test
// asserts the payload's internal consistency instead: whichever branch actually fired, the two
// fields must agree — a live override must report the override tier together with "active";
// an expired override must report the raw tier together with the raw status. Mixing (override
// tier with the stale status, or vice versa) fails regardless of which side of the boundary the
// internal read happened to land on.
func TestClerkPublicMetadata_TierAndStatusAgreeAcrossExpiryBoundary(t *testing.T) {
	override := "household_2"

	check := func(t *testing.T, offset time.Duration) {
		now := time.Now()
		endsAt := now.Add(offset)
		user := &User{
			SubscriptionTier:   "premium",
			SubscriptionStatus: "past_due",
			ManualTierOverride: &override,
			ManualTierEndsAt:   &endsAt,
		}

		metadata := user.ClerkPublicMetadata()
		tier := metadata["subscription_tier"]
		status := metadata["subscription_status"]

		switch tier {
		case "household_2":
			assert.Equal(t, string(SubscriptionStatusActive), status,
				"override tier reported live but status disagrees — tier and status were computed from different clock reads")
		case "premium":
			assert.Equal(t, "past_due", status,
				"override tier reported expired but status disagrees — tier and status were computed from different clock reads")
		default:
			t.Fatalf("unexpected subscription_tier %v", tier)
		}
	}

	t.Run("1ms before expiry", func(t *testing.T) {
		check(t, time.Millisecond)
	})

	t.Run("1ms after expiry", func(t *testing.T) {
		check(t, -time.Millisecond)
	})
}

// TestClerkPublicMetadata_TierKeyPresentAndNil asserts the "tier" key is present with an
// explicit nil value (not omitted). Clerk's PATCH /v1/users/{id}/metadata performs a merge, and
// an explicit null deletes the key server-side — omitting it entirely would leave the orphaned
// legacy "tier" key untouched. This is the easy-to-get-wrong case: a `map[string]interface{}`
// built without explicitly setting the key would silently fail to include it.
func TestClerkPublicMetadata_TierKeyPresentAndNil(t *testing.T) {
	user := &User{SubscriptionTier: "premium", SubscriptionStatus: "active"}

	metadata := user.ClerkPublicMetadata()

	tierValue, exists := metadata["tier"]
	assert.True(t, exists, `"tier" key must be present (even though nil) so Clerk's metadata merge deletes it`)
	assert.Nil(t, tierValue)
}

// TestUser_IsComped_AlwaysSerializes locks in that IsComped has no `omitempty` tag: the
// frontend must always receive `is_comped` in the JSON payload and never have to distinguish
// `false` from "field absent".
func TestUser_IsComped_AlwaysSerializes(t *testing.T) {
	user := &User{IsComped: false}

	jsonData, err := json.Marshal(user)
	require.NoError(t, err)

	assert.Contains(t, string(jsonData), `"is_comped":false`)
}

// TestUser_PaddleSubscriptionID_EmptyString tests empty string handling
func TestUser_PaddleSubscriptionID_EmptyString(t *testing.T) {
	emptyString := ""
	user := &User{
		ID:                   "user-123",
		ClerkUserID:          "clerk_123",
		Email:                "test@example.com",
		PaddleSubscriptionID: &emptyString,
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(user)
	require.NoError(t, err)

	// Unmarshal back
	var unmarshaled User
	err = json.Unmarshal(jsonData, &unmarshaled)
	require.NoError(t, err)

	// Verify empty string is preserved
	assert.NotNil(t, unmarshaled.PaddleSubscriptionID)
	assert.Equal(t, "", *unmarshaled.PaddleSubscriptionID)
}

// ─── Issue #1413 G2: EffectiveStatusWithActiveOverride ──────────────────────
// See the method's doc comment above (why domain can't validate the override itself, and why
// status/overrideValue share a single time.Now() read) for the full rationale.

// TestEffectiveStatusWithActiveOverride_NoOverride_ReturnsRawStatusAndNoOverride verifies that
// with no manual override at all, the raw status passes through and hasOverride is false.
func TestEffectiveStatusWithActiveOverride_NoOverride_ReturnsRawStatusAndNoOverride(t *testing.T) {
	user := &User{SubscriptionStatus: "inactive"}

	status, overrideValue, hasOverride := user.EffectiveStatusWithActiveOverride()

	assert.Equal(t, "inactive", status)
	assert.Equal(t, "", overrideValue)
	assert.False(t, hasOverride)
}

// TestEffectiveStatusWithActiveOverride_ActiveOverride_ReturnsActiveAndValue verifies that a
// live (non-expired) override reports status "active" alongside the override's raw string
// value, so a caller can validate it.
func TestEffectiveStatusWithActiveOverride_ActiveOverride_ReturnsActiveAndValue(t *testing.T) {
	override := "not_a_tier"
	user := &User{
		SubscriptionStatus: "inactive",
		ManualTierOverride: &override,
		ManualTierEndsAt:   nil,
	}

	status, overrideValue, hasOverride := user.EffectiveStatusWithActiveOverride()

	assert.Equal(t, string(SubscriptionStatusActive), status)
	assert.Equal(t, "not_a_tier", overrideValue)
	assert.True(t, hasOverride)
}

// TestEffectiveStatusWithActiveOverride_ExpiredOverride_ReturnsRawStatusAndNoOverride verifies
// an expired override falls back to the raw status with hasOverride false, mirroring
// ComputeEffectiveStatus's own expiry behavior.
func TestEffectiveStatusWithActiveOverride_ExpiredOverride_ReturnsRawStatusAndNoOverride(t *testing.T) {
	override := "premium"
	expired := time.Now().Add(-24 * time.Hour)
	user := &User{
		SubscriptionStatus: "cancelled",
		ManualTierOverride: &override,
		ManualTierEndsAt:   &expired,
	}

	status, overrideValue, hasOverride := user.EffectiveStatusWithActiveOverride()

	assert.Equal(t, "cancelled", status)
	assert.Equal(t, "", overrideValue)
	assert.False(t, hasOverride)
}

// TestEffectiveTierWithActiveOverride_NoOverride_ReturnsRawTierAndNoOverride mirrors
// TestEffectiveStatusWithActiveOverride_NoOverride_ReturnsRawStatusAndNoOverride for the tier axis
// (Issue #1413 — this task).
func TestEffectiveTierWithActiveOverride_NoOverride_ReturnsRawTierAndNoOverride(t *testing.T) {
	user := &User{SubscriptionTier: "premium"}

	tier, overrideValue, hasOverride := user.EffectiveTierWithActiveOverride()

	assert.Equal(t, "premium", tier)
	assert.Equal(t, "", overrideValue)
	assert.False(t, hasOverride)
}

// TestEffectiveTierWithActiveOverride_ActiveOverride_ReturnsOverrideTierAndValue verifies that a
// live (non-expired) override reports the override's raw string as BOTH the tier and the
// overrideValue, so a caller can validate it before trusting it.
func TestEffectiveTierWithActiveOverride_ActiveOverride_ReturnsOverrideTierAndValue(t *testing.T) {
	override := "not_a_tier"
	user := &User{
		SubscriptionTier:   "premium",
		ManualTierOverride: &override,
		ManualTierEndsAt:   nil,
	}

	tier, overrideValue, hasOverride := user.EffectiveTierWithActiveOverride()

	assert.Equal(t, "not_a_tier", tier)
	assert.Equal(t, "not_a_tier", overrideValue)
	assert.True(t, hasOverride)
}

// TestEffectiveTierWithActiveOverride_ExpiredOverride_ReturnsRawTierAndNoOverride verifies an
// expired override falls back to the raw tier with hasOverride false, mirroring
// ComputeEffectiveTier's own expiry behavior.
func TestEffectiveTierWithActiveOverride_ExpiredOverride_ReturnsRawTierAndNoOverride(t *testing.T) {
	override := "household_5"
	expired := time.Now().Add(-24 * time.Hour)
	user := &User{
		SubscriptionTier:   "premium",
		ManualTierOverride: &override,
		ManualTierEndsAt:   &expired,
	}

	tier, overrideValue, hasOverride := user.EffectiveTierWithActiveOverride()

	assert.Equal(t, "premium", tier)
	assert.Equal(t, "", overrideValue)
	assert.False(t, hasOverride)
}

// ─── Issue #1466 (Task 2, batch R4) — direct domain-level coverage for the -At variants ───────
//
// Before these tests, EffectiveStatusWithActiveOverrideAt had zero direct references anywhere in
// this file — its three sibling TestEffectiveStatusWithActiveOverride_* tests above all exercise
// the no-arg delegating form, and the -At path's only coverage anywhere was one sleep-based
// service-layer test (TestAdminUserService_GetUserDetail_EffectiveStatus_DerivedFromPassedInstant_NotLaterClockRead,
// admin_user_service_test.go). The tests below exercise both -At variants directly, at the
// no-override/active/expired matrix already covered for the no-arg forms, plus the exact-tie
// boundary semantics TestHasActiveManualOverrideAt_BoundarySemantics pins for
// hasActiveManualOverrideAt itself — asserted here at the EffectiveStatusWithActiveOverrideAt/
// EffectiveTierWithActiveOverrideAt level too, since both delegate straight through
// hasActiveManualOverrideAt and must agree with it exactly at the tie.

// TestEffectiveStatusWithActiveOverrideAt_NoOverride_ReturnsRawStatusAndNoOverride mirrors
// TestEffectiveStatusWithActiveOverride_NoOverride_ReturnsRawStatusAndNoOverride, calling the -At
// variant directly with an explicit instant instead of the no-arg delegating form.
func TestEffectiveStatusWithActiveOverrideAt_NoOverride_ReturnsRawStatusAndNoOverride(t *testing.T) {
	user := &User{SubscriptionStatus: "inactive"}

	status, overrideValue, hasOverride := user.EffectiveStatusWithActiveOverrideAt(time.Now())

	assert.Equal(t, "inactive", status)
	assert.Equal(t, "", overrideValue)
	assert.False(t, hasOverride)
}

// TestEffectiveStatusWithActiveOverrideAt_ActiveOverride_ReturnsActiveAndValue mirrors
// TestEffectiveStatusWithActiveOverride_ActiveOverride_ReturnsActiveAndValue at the -At level: a
// live override (relative to the PASSED instant, not an internal clock read) reports status
// "active" alongside the override's raw string value.
func TestEffectiveStatusWithActiveOverrideAt_ActiveOverride_ReturnsActiveAndValue(t *testing.T) {
	now := time.Now()
	override := "not_a_tier"
	futureEnd := now.Add(24 * time.Hour)
	user := &User{
		SubscriptionStatus: "inactive",
		ManualTierOverride: &override,
		ManualTierEndsAt:   &futureEnd,
	}

	status, overrideValue, hasOverride := user.EffectiveStatusWithActiveOverrideAt(now)

	assert.Equal(t, string(SubscriptionStatusActive), status)
	assert.Equal(t, "not_a_tier", overrideValue)
	assert.True(t, hasOverride)
}

// TestEffectiveStatusWithActiveOverrideAt_ExpiredOverride_ReturnsRawStatusAndNoOverride mirrors
// TestEffectiveStatusWithActiveOverride_ExpiredOverride_ReturnsRawStatusAndNoOverride at the -At
// level: an override that has already expired AS OF THE PASSED INSTANT falls back to the raw
// status with hasOverride false.
func TestEffectiveStatusWithActiveOverrideAt_ExpiredOverride_ReturnsRawStatusAndNoOverride(t *testing.T) {
	now := time.Now()
	override := "premium"
	expired := now.Add(-24 * time.Hour)
	user := &User{
		SubscriptionStatus: "cancelled",
		ManualTierOverride: &override,
		ManualTierEndsAt:   &expired,
	}

	status, overrideValue, hasOverride := user.EffectiveStatusWithActiveOverrideAt(now)

	assert.Equal(t, "cancelled", status)
	assert.Equal(t, "", overrideValue)
	assert.False(t, hasOverride)
}

// TestEffectiveStatusWithActiveOverrideAt_ExactTieBoundary_StillActive pins the exact-tie
// boundary at the EffectiveStatusWithActiveOverrideAt level: TestHasActiveManualOverrideAt_BoundarySemantics
// already locks in that `ManualTierEndsAt == now` counts as still active on
// hasActiveManualOverrideAt itself; this asserts EffectiveStatusWithActiveOverrideAt (which
// delegates straight through it) reports the SAME thing — status forced to active, override value
// surfaced, hasOverride true — rather than silently disagreeing with the lower-level predicate at
// the one instant that actually distinguishes `>=` from `>` semantics.
func TestEffectiveStatusWithActiveOverrideAt_ExactTieBoundary_StillActive(t *testing.T) {
	now := time.Now()
	override := "household_2"
	endsAt := now // exact tie
	user := &User{
		SubscriptionStatus: "inactive",
		ManualTierOverride: &override,
		ManualTierEndsAt:   &endsAt,
	}

	status, overrideValue, hasOverride := user.EffectiveStatusWithActiveOverrideAt(now)

	assert.Equal(t, string(SubscriptionStatusActive), status,
		"an override ending exactly at now must still count as active, matching hasActiveManualOverrideAt's own !Before(now) semantics")
	assert.Equal(t, "household_2", overrideValue)
	assert.True(t, hasOverride)
}

// TestEffectiveTierWithActiveOverrideAt_NoOverride_ReturnsRawTierAndNoOverride mirrors
// TestEffectiveTierWithActiveOverride_NoOverride_ReturnsRawTierAndNoOverride, calling the -At
// variant directly with an explicit instant.
func TestEffectiveTierWithActiveOverrideAt_NoOverride_ReturnsRawTierAndNoOverride(t *testing.T) {
	user := &User{SubscriptionTier: "premium"}

	tier, overrideValue, hasOverride := user.EffectiveTierWithActiveOverrideAt(time.Now())

	assert.Equal(t, "premium", tier)
	assert.Equal(t, "", overrideValue)
	assert.False(t, hasOverride)
}

// TestEffectiveTierWithActiveOverrideAt_ActiveOverride_ReturnsOverrideTierAndValue mirrors
// TestEffectiveTierWithActiveOverride_ActiveOverride_ReturnsOverrideTierAndValue at the -At level.
func TestEffectiveTierWithActiveOverrideAt_ActiveOverride_ReturnsOverrideTierAndValue(t *testing.T) {
	now := time.Now()
	override := "not_a_tier"
	futureEnd := now.Add(24 * time.Hour)
	user := &User{
		SubscriptionTier:   "premium",
		ManualTierOverride: &override,
		ManualTierEndsAt:   &futureEnd,
	}

	tier, overrideValue, hasOverride := user.EffectiveTierWithActiveOverrideAt(now)

	assert.Equal(t, "not_a_tier", tier)
	assert.Equal(t, "not_a_tier", overrideValue)
	assert.True(t, hasOverride)
}

// TestEffectiveTierWithActiveOverrideAt_ExpiredOverride_ReturnsRawTierAndNoOverride mirrors
// TestEffectiveTierWithActiveOverride_ExpiredOverride_ReturnsRawTierAndNoOverride at the -At
// level: an override expired AS OF THE PASSED INSTANT falls back to the raw tier.
func TestEffectiveTierWithActiveOverrideAt_ExpiredOverride_ReturnsRawTierAndNoOverride(t *testing.T) {
	now := time.Now()
	override := "household_5"
	expired := now.Add(-24 * time.Hour)
	user := &User{
		SubscriptionTier:   "premium",
		ManualTierOverride: &override,
		ManualTierEndsAt:   &expired,
	}

	tier, overrideValue, hasOverride := user.EffectiveTierWithActiveOverrideAt(now)

	assert.Equal(t, "premium", tier)
	assert.Equal(t, "", overrideValue)
	assert.False(t, hasOverride)
}

// TestEffectiveTierWithActiveOverrideAt_ExactTieBoundary_StillActive is the tier-axis twin of
// TestEffectiveStatusWithActiveOverrideAt_ExactTieBoundary_StillActive — same exact-tie fixture,
// asserting EffectiveTierWithActiveOverrideAt agrees with hasActiveManualOverrideAt's `>=`
// semantics too.
func TestEffectiveTierWithActiveOverrideAt_ExactTieBoundary_StillActive(t *testing.T) {
	now := time.Now()
	override := "household_2"
	endsAt := now // exact tie
	user := &User{
		SubscriptionTier:   "premium",
		ManualTierOverride: &override,
		ManualTierEndsAt:   &endsAt,
	}

	tier, overrideValue, hasOverride := user.EffectiveTierWithActiveOverrideAt(now)

	assert.Equal(t, "household_2", tier,
		"an override ending exactly at now must still count as active, matching hasActiveManualOverrideAt's own !Before(now) semantics")
	assert.Equal(t, "household_2", overrideValue)
	assert.True(t, hasOverride)
}
