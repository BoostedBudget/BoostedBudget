package jobs

import (
	"context"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type capacityOwnAccess struct {
	err    error
	access domain.RevenueCatAccess
	calls  int
}

type changingCapacityAccess struct{ calls int }

func (a *changingCapacityAccess) ResolveOwnAccessAt(context.Context, string, time.Time) (domain.RevenueCatAccess, error) {
	a.calls++
	if a.calls == 2 {
		return domain.RevenueCatAccess{}, domain.ErrRevenueCatUnresolved
	}
	return domain.RevenueCatAccess{Tier: "household_2", Status: "active"}, nil
}
func TestRevenueCatHouseholdCapacityRechecksBeforeMutation(t *testing.T) {
	w, repo, profiles, _, _ := setupOverCapacityCheckWorkerTest(t)
	profiles.On("GetUserByID", mock.Anything, "owner").Return(&domain.User{ID: "owner", SubscriptionStatus: "active", SubscriptionTier: "household_5"}, nil).Twice()
	repo.On("CountActiveMembers", mock.Anything, "household").Return(3, nil).Once()
	access := &changingCapacityAccess{}
	w.SetOwnAccessResolver(access)
	err := w.processHousehold(context.Background(), 1, &domain.Household{ID: "household", OwnerUserID: "owner"})
	require.ErrorIs(t, err, domain.ErrRevenueCatUnresolved)
	require.Equal(t, 2, access.calls)
	profiles.AssertExpectations(t)
	repo.AssertExpectations(t)
}

func (a *capacityOwnAccess) ResolveOwnAccessAt(context.Context, string, time.Time) (domain.RevenueCatAccess, error) {
	a.calls++
	return a.access, a.err
}
func TestRevenueCatHouseholdCapacityUsesConfirmedOwner(t *testing.T) {
	for _, unresolved := range []bool{false, true} {
		t.Run(map[bool]string{false: "confirmed lapse", true: "unresolved"}[unresolved], func(t *testing.T) {
			profiles := &mockHouseholdOverCapacityUserRepository{}
			repo := &mockHouseholdOverCapacityCheckRepository{}
			profiles.On("GetUserByID", mock.Anything, "owner").Return(&domain.User{ID: "owner", SubscriptionStatus: "active", SubscriptionTier: "household_5"}, nil)
			access := &capacityOwnAccess{access: domain.RevenueCatAccess{Tier: "premium", Status: "inactive"}}
			if unresolved {
				access.err = domain.ErrRevenueCatUnresolved
			} else {
				repo.On("ClearOverCapacity", mock.Anything, "household").Return(nil).Once()
			}
			w := &HouseholdOverCapacityCheckWorker{userRepo: profiles, repo: repo}
			w.SetOwnAccessResolver(access)
			now := time.Now()
			err := w.processHousehold(context.Background(), 1, &domain.Household{ID: "household", OwnerUserID: "owner", OverCapacitySince: &now})
			if unresolved {
				require.ErrorIs(t, err, domain.ErrRevenueCatUnresolved)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, 1, access.calls)
			repo.AssertExpectations(t)
		})
	}
}
