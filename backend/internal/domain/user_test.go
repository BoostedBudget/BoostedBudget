package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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

	assert.NotEmpty(t, user.ClerkUserID)
	assert.Equal(t, "clerk_123", user.ClerkUserID)
	assert.Equal(t, "test@example.com", user.Email)
	assert.True(t, user.EmailVerified)
	assert.Equal(t, "premium", user.SubscriptionTier)
	assert.Equal(t, "trial", user.SubscriptionStatus)
	assert.Equal(t, DefaultRegion, user.PrimaryRegion)
	assert.True(t, user.IsActive)
	assert.False(t, user.IsDeleted)
}
