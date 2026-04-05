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
