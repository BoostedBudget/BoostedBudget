package domain

import (
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestRevenueCatBootstrapPreservesFixedLegacyExpiries(t *testing.T) {
	now := time.Now()
	end := now.Add(12 * time.Hour)
	comp := "household_3"
	user := &User{ID: "user", HadTrial: true, TrialEndsAt: &end, ManualTierOverride: &comp}
	ops, err := PlanRevenueCatBootstrap(user, now)
	require.NoError(t, err)
	require.Len(t, ops, 2)
	require.Equal(t, &end, ops[0].ExpiresAt)
	require.Nil(t, ops[1].ExpiresAt)
	require.Equal(t, "trial_premium", ops[0].Key)
	require.Equal(t, "comp_household_3", ops[1].Key)
	expired := now.Add(-time.Hour)
	user.TrialEndsAt = &expired
	ops, err = PlanRevenueCatBootstrap(user, now)
	require.NoError(t, err)
	require.Len(t, ops, 1)
}
func TestRevenueCatBootstrapRefusesUnexpectedBillingAndMalformedGrant(t *testing.T) {
	sub := "sub_unexpected"
	bad := "unknown"
	for _, user := range []*User{{PaddleSubscriptionID: &sub}, {PaddleCustomerID: &sub}, {SubscriptionStatus: "trial"}, {ManualTierOverride: &bad}} {
		_, err := PlanRevenueCatBootstrap(user, time.Now())
		require.Error(t, err)
	}
}
func TestRevenueCatBootstrapNeverInventsTrialOrRevivesDeletedAccount(t *testing.T) {
	for _, user := range []*User{{ID: "never-trial"}, {ID: "consumed", HadTrial: true}, {ID: "deleted", IsDeleted: true}} {
		ops, err := PlanRevenueCatBootstrap(user, time.Now())
		require.NoError(t, err)
		require.Empty(t, ops)
	}
}
