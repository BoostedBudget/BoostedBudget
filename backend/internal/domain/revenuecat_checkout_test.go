package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRevenueCatCheckoutRequiresFreshBillingAndNoExistingPaidSubscription(t *testing.T) {
	now := time.Now()
	base := RevenueCatState{AccessUpdatedAt: now, BillingUpdatedAt: &now, Grants: []RevenueCatGrant{{Key: "comp_household_5"}, {Key: "trial_premium"}}}
	require.NoError(t, base.CanStartCheckout(now), "comp and card-free trial don't prohibit subscribing")
	for _, status := range []string{"active", "trialing", "in_billing_retry", "paused", "incomplete", "unknown"} {
		state := base
		state.Billing = []RevenueCatBilling{{Store: "paddle", Status: status}}
		require.ErrorIs(t, state.CanStartCheckout(now), ErrRevenueCatExistingSubscription)
	}
	state := base
	state.Billing = []RevenueCatBilling{{Store: "paddle", Status: "expired"}, {Store: "promotional", Status: "active"}}
	require.NoError(t, state.CanStartCheckout(now))
	state.Grants = append(state.Grants, RevenueCatGrant{Key: "paid_premium"})
	require.ErrorIs(t, state.CanStartCheckout(now), ErrRevenueCatExistingSubscription)
	state = base
	state.BillingError = "billing_unavailable"
	require.ErrorIs(t, state.CanStartCheckout(now), ErrRevenueCatUnresolved)
	state = base
	stale := now.Add(-RevenueCatMaxAge)
	state.BillingUpdatedAt = &stale
	require.ErrorIs(t, state.CanStartCheckout(now), ErrRevenueCatUnresolved)
}
