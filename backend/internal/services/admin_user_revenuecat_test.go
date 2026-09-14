package services

import (
	"context"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type adminUserRCSummary struct{ result RevenueCatSummary }

func (s adminUserRCSummary) Summary(context.Context, *domain.User) RevenueCatSummary { return s.result }

func TestAdminUserRevenueCatReadsCannotAdvertiseLegacyOverrides(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	tier := "household_5"
	for _, detail := range []bool{false, true} {
		repo := new(MockAdminUserRepository)
		svc := NewAdminUserService(repo, nil, nil, 6, nil)
		svc.SetRevenueCatSummary(adminUserRCSummary{})
		item := domain.AdminUserListItem{ID: "target", SubscriptionTier: tier, SubscriptionStatus: "active", ManualTierOverride: &tier}
		if detail {
			repo.On("GetUserDetail", mock.Anything, "target").Return(&domain.AdminUserDetail{AdminUserListItem: item}, nil)
			out, err := svc.GetUserDetail(ctx, "target", now)
			require.NoError(t, err)
			item = out.AdminUserListItem
		} else {
			repo.On("ListUsers", mock.Anything, domain.AdminUserFilters{}, 20, 0).Return([]domain.AdminUserListItem{item}, 1, nil)
			out, err := svc.ListUsers(ctx, domain.AdminUserFilters{}, 1, 20, now)
			require.NoError(t, err)
			item = out.Users[0]
		}
		require.Equal(t, "unresolved", item.EffectiveStatus)
		require.Nil(t, item.ManualTierOverride)
		require.NotNil(t, item.AccessResolved)
		require.False(t, *item.AccessResolved)
		repo.AssertExpectations(t)
	}
}

func TestAdminUserRevenueCatKeepsFilteredProjectionSeparateFromLiveEffectiveAccess(t *testing.T) {
	repo := new(MockAdminUserRepository)
	svc := NewAdminUserService(repo, nil, nil, 6, nil)
	svc.SetRevenueCatSummary(adminUserRCSummary{result: RevenueCatSummary{AccessResolved: true, Own: &domain.RevenueCatAccess{Tier: "household_3", Status: "active"}, Effective: &domain.EffectiveEntitlement{Tier: "household_3", Status: "active"}}})
	filters := domain.AdminUserFilters{Tier: "premium", SortBy: "subscription_tier", SortDir: "asc"}
	repo.On("ListUsers", mock.Anything, filters, 20, 0).Return([]domain.AdminUserListItem{{ID: "target", SubscriptionTier: "premium", SubscriptionStatus: "trial"}}, 1, nil)
	out, err := svc.ListUsers(context.Background(), filters, 1, 20, time.Now())
	require.NoError(t, err)
	require.Equal(t, "premium", out.Users[0].SubscriptionTier, "filtered/sorted observed projection must not be replaced after pagination")
	require.Equal(t, "trial", out.Users[0].SubscriptionStatus)
	require.Equal(t, "household_3", out.Users[0].EffectiveTier, "live authority is exposed separately")
	require.Equal(t, "active", out.Users[0].EffectiveStatus)
}
