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

func TestRevenueCatPackageKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		tier            string
		billingInterval string
		wantKey         string
		wantOK          bool
	}{
		{name: "premium monthly", tier: "premium", billingInterval: "month", wantKey: "premium_monthly", wantOK: true},
		{name: "premium annual", tier: "premium", billingInterval: "year", wantKey: "premium_annual", wantOK: true},
		{name: "household_2 monthly", tier: "household_2", billingInterval: "month", wantKey: "household_2_monthly", wantOK: true},
		{name: "household_2 annual", tier: "household_2", billingInterval: "year", wantKey: "household_2_annual", wantOK: true},
		{name: "household_3 monthly", tier: "household_3", billingInterval: "month", wantKey: "household_3_monthly", wantOK: true},
		{name: "household_3 annual", tier: "household_3", billingInterval: "year", wantKey: "household_3_annual", wantOK: true},
		{name: "household_4 monthly", tier: "household_4", billingInterval: "month", wantKey: "household_4_monthly", wantOK: true},
		{name: "household_4 annual", tier: "household_4", billingInterval: "year", wantKey: "household_4_annual", wantOK: true},
		{name: "household_5 monthly", tier: "household_5", billingInterval: "month", wantKey: "household_5_monthly", wantOK: true},
		{name: "household_5 annual", tier: "household_5", billingInterval: "year", wantKey: "household_5_annual", wantOK: true},
		{name: "unknown tier enterprise", tier: "enterprise", billingInterval: "month", wantKey: "", wantOK: false},
		{name: "empty tier", tier: "", billingInterval: "month", wantKey: "", wantOK: false},
		{name: "unknown interval weekly", tier: "premium", billingInterval: "weekly", wantKey: "", wantOK: false},
		{name: "empty interval", tier: "premium", billingInterval: "", wantKey: "", wantOK: false},
		{name: "interval is case-sensitive", tier: "premium", billingInterval: "MONTH", wantKey: "", wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gotKey, gotOK := RevenueCatPackageKey(tc.tier, tc.billingInterval)

			require.Equal(t, tc.wantKey, gotKey)
			require.Equal(t, tc.wantOK, gotOK)
		})
	}
}
