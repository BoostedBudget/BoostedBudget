package operations

import (
	"context"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/require"
)

type profileRevenueCatSummary struct{ summary services.RevenueCatSummary }

func (s profileRevenueCatSummary) Summary(context.Context, *domain.User) services.RevenueCatSummary {
	return s.summary
}

func TestProfileRevenueCatDoesNotAdvertiseLegacyAccessDuringOutage(t *testing.T) {
	tier := "household_5"
	end := time.Now().Add(time.Hour)
	user := &domain.User{ID: "user", SubscriptionStatus: "active", SubscriptionTier: tier, ManualTierOverride: &tier, TrialEndsAt: &end, IsComped: true, CompTier: tier}
	h := NewProfileHandler(&mockProfileService{user: user})
	h.SetRevenueCatSummary(profileRevenueCatSummary{})
	out, err := h.GetProfile(context.Background(), &GetProfileInput{})
	require.NoError(t, err)
	require.Equal(t, "unresolved", out.Body.EffectiveStatus)
	require.NotNil(t, out.Body.AccessResolved)
	require.False(t, *out.Body.AccessResolved)
	require.False(t, out.Body.IsComped)
	require.Empty(t, out.Body.CompTier)
	require.Nil(t, out.Body.TrialEndsAt)
	require.Nil(t, out.Body.ManualTierOverride)
	require.Equal(t, "active", user.SubscriptionStatus, "response enrichment must not mutate repository objects")
}

func TestProfileRevenueCatUsesAggregateAccessButKeepsOwnTrialAndComp(t *testing.T) {
	end := time.Now().Add(48 * time.Hour)
	summary := services.RevenueCatSummary{AccessResolved: true, Effective: &domain.EffectiveEntitlement{Tier: "household_3", Status: "active", Source: domain.EntitlementSourceHousehold}, Own: &domain.RevenueCatAccess{Tier: "premium", Status: "trial", Source: "trial"}, Trial: &domain.RevenueCatAccess{Tier: "premium", Status: "trial", Source: "trial", ExpiresAt: &end}}
	h := NewProfileHandler(&mockProfileService{user: &domain.User{ID: "user"}})
	h.SetRevenueCatSummary(profileRevenueCatSummary{summary: summary})
	out, err := h.UpdateProfile(context.Background(), &UpdateProfileInput{})
	require.NoError(t, err)
	require.Equal(t, "household_3", out.Body.EffectiveTier)
	require.Equal(t, "active", out.Body.EffectiveStatus)
	require.Equal(t, "household", out.Body.AccessSource)
	require.Equal(t, "trial", out.Body.SubscriptionStatus)
	require.Equal(t, &end, out.Body.TrialEndsAt)
	require.NotNil(t, out.Body.TrialDaysRemaining)
}

func TestProfileRevenueCatTrialCountdownRemainsVisibleAlongsidePaidOrComp(t *testing.T) {
	for _, source := range []string{"paid", "comp"} {
		t.Run(source, func(t *testing.T) {
			end := time.Now().Add(48 * time.Hour)
			summary := services.RevenueCatSummary{AccessResolved: true, Effective: &domain.EffectiveEntitlement{Tier: "premium", Status: "active"}, Own: &domain.RevenueCatAccess{Tier: "premium", Status: "active", Source: source}, Trial: &domain.RevenueCatAccess{Tier: "premium", Status: "trial", Source: "trial", ExpiresAt: &end}}
			h := NewProfileHandler(&mockProfileService{user: &domain.User{ID: "user"}})
			h.SetRevenueCatSummary(profileRevenueCatSummary{summary: summary})
			out, err := h.GetProfile(context.Background(), &GetProfileInput{})
			require.NoError(t, err)
			require.Equal(t, "active", out.Body.SubscriptionStatus)
			require.NotNil(t, out.Body.TrialDaysRemaining)
			require.GreaterOrEqual(t, *out.Body.TrialDaysRemaining, 1)
		})
	}
}
