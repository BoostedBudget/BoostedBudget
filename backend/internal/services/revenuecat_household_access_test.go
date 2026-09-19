package services

import (
	"context"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type ownAccessFunc func(context.Context, string, time.Time) (domain.RevenueCatAccess, error)

func (f ownAccessFunc) ResolveOwnAccessAt(ctx context.Context, id string, at time.Time) (domain.RevenueCatAccess, error) {
	return f(ctx, id, at)
}

func TestRevenueCatHouseholdOwnAccessIgnoresLegacyGrant(t *testing.T) {
	now := time.Now()
	repo := &mockEntitlementRepo{}
	repo.On("GetActiveEntitlement", mock.Anything, "member", now).Return(nil, repository.ErrEntitlementNotFound)
	svc := NewHouseholdEntitlementService(repo, nil, nil, nil)
	svc.SetOwnAccessResolver(ownAccessFunc(func(_ context.Context, id string, at time.Time) (domain.RevenueCatAccess, error) {
		require.Equal(t, "member", id)
		require.Equal(t, now, at)
		return domain.RevenueCatAccess{Tier: "premium", Status: "inactive", Source: "none"}, nil
	}))
	legacy := "household_5"
	got, err := svc.ResolveEffectiveEntitlementAt(context.Background(), &domain.User{ID: "member", SubscriptionStatus: "active", ManualTierOverride: &legacy}, now)
	require.NoError(t, err)
	require.Equal(t, "premium", got.Tier)
	require.Equal(t, "inactive", got.Status)
}

func TestRevenueCatHouseholdUsesLiveOwnerBeforeOwnFallback(t *testing.T) {
	for _, ownerStatus := range []string{"active", "inactive", "unresolved"} {
		t.Run(ownerStatus, func(t *testing.T) {
			now := time.Now()
			ownerID, householdID := "owner", "household"
			repo := &mockEntitlementRepo{}
			repo.On("GetActiveEntitlement", mock.Anything, "member", now).Return(&domain.SubscriptionEntitlement{
				Source: domain.EntitlementSourceHousehold, PayerUserID: &ownerID, HouseholdID: &householdID,
			}, nil)
			svc := NewHouseholdEntitlementService(repo, nil, nil, nil)
			ownCalls := 0
			svc.SetOwnAccessResolver(ownAccessFunc(func(_ context.Context, id string, at time.Time) (domain.RevenueCatAccess, error) {
				require.Equal(t, now, at)
				if id == "owner" {
					if ownerStatus == "unresolved" {
						return domain.RevenueCatAccess{}, domain.ErrRevenueCatUnresolved
					}
					return domain.RevenueCatAccess{Tier: "household_2", Status: ownerStatus, Source: "paid"}, nil
				}
				ownCalls++
				return domain.RevenueCatAccess{Tier: "household_5", Status: "active", Source: "comp"}, nil
			}))
			got, err := svc.ResolveEffectiveEntitlementAt(context.Background(), &domain.User{ID: "member"}, now)
			if ownerStatus == "unresolved" {
				require.ErrorIs(t, err, domain.ErrRevenueCatUnresolved)
				require.Zero(t, ownCalls)
				return
			}
			require.NoError(t, err)
			if ownerStatus == "active" {
				require.Equal(t, "household_2", got.Tier)
				require.Equal(t, domain.EntitlementSourceHousehold, got.Source)
				require.Zero(t, ownCalls)
			} else {
				require.Equal(t, "household_5", got.Tier)
				require.Equal(t, domain.EntitlementSourceOwn, got.Source)
				require.Equal(t, 1, ownCalls)
			}
		})
	}
}

func TestRevenueCatHouseholdMiddlewareReaderRejectsUnresolvedStatus(t *testing.T) {
	for _, status := range []string{"active", "inactive", ""} {
		t.Run("status_"+status, func(t *testing.T) {
			repo := &mockEntitlementRepo{}
			repo.On("GetActiveEntitlement", mock.Anything, "member", mock.Anything).Return(nil, repository.ErrEntitlementNotFound)
			svc := NewHouseholdEntitlementService(repo, nil, nil, nil)
			svc.SetOwnAccessResolver(ownAccessFunc(func(context.Context, string, time.Time) (domain.RevenueCatAccess, error) {
				return domain.RevenueCatAccess{Tier: "premium", Status: status}, nil
			}))
			got, err := svc.GetEffectiveSubscriptionStatusByUserID(context.Background(), "member")
			if status == "" {
				require.ErrorIs(t, err, domain.ErrRevenueCatUnresolved)
			} else {
				require.NoError(t, err)
				require.Equal(t, status, got)
			}
		})
	}
}
