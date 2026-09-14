package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/require"
)

type cleanupStates map[string]domain.RevenueCatState

func (s cleanupStates) Read(_ context.Context, id string) (domain.RevenueCatState, error) {
	v, ok := s[id]
	if !ok {
		return v, errors.New("unavailable")
	}
	return v, nil
}

type cleanupEntitlements struct {
	ent *domain.SubscriptionEntitlement
	err error
}

func (e cleanupEntitlements) GetActiveEntitlement(context.Context, string, time.Time) (*domain.SubscriptionEntitlement, error) {
	return e.ent, e.err
}

type cleanupAccess struct {
	calls []string
	err   error
}

func (a *cleanupAccess) ResolveOwnAccessAt(_ context.Context, id string, _ time.Time) (domain.RevenueCatAccess, error) {
	a.calls = append(a.calls, id)
	return domain.RevenueCatAccess{}, a.err
}

func TestRevenueCatCleanupGuard_RequiresOwnAndHouseholdConfirmation(t *testing.T) {
	now := time.Now()
	owner := "owner"
	household := "household"
	for _, tc := range []struct {
		name         string
		ownPending   int
		ownerPending int
		ownerActive  bool
		absentOwner  bool
		ownStale     bool
		want         bool
	}{
		{name: "confirmed lapse", want: true}, {name: "own grant pending", ownPending: 1}, {name: "owner grant pending", ownerPending: 1}, {name: "household active", ownerActive: true}, {name: "owner unavailable", absentOwner: true}, {name: "own stale", ownStale: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			states := cleanupStates{"member": {AccessUpdatedAt: now, PendingOperations: tc.ownPending}, owner: {AccessUpdatedAt: now, PendingOperations: tc.ownerPending}}
			if tc.ownerActive {
				v := states[owner]
				v.Grants = []domain.RevenueCatGrant{{Key: "paid_household_2"}}
				states[owner] = v
			}
			if tc.absentOwner {
				delete(states, owner)
			}
			if tc.ownStale {
				v := states["member"]
				v.AccessUpdatedAt = now.Add(-time.Hour)
				states["member"] = v
			}
			access := &cleanupAccess{}
			g := NewRevenueCatCleanupGuard(access, states, cleanupEntitlements{ent: &domain.SubscriptionEntitlement{Source: domain.EntitlementSourceHousehold, PayerUserID: &owner, HouseholdID: &household}})
			got, _ := g.CanCleanup(context.Background(), "member")
			require.Equal(t, tc.want, got)
		})
	}
}
func TestRevenueCatCleanupGuard_NoHouseholdAndProviderFailure(t *testing.T) {
	states := cleanupStates{"member": {AccessUpdatedAt: time.Now()}}
	access := &cleanupAccess{}
	g := NewRevenueCatCleanupGuard(access, states, cleanupEntitlements{err: repository.ErrEntitlementNotFound})
	ok, err := g.CanCleanup(context.Background(), "member")
	require.NoError(t, err)
	require.True(t, ok)
	access.err = errors.New("provider unavailable")
	ok, err = g.CanCleanup(context.Background(), "member")
	require.Error(t, err)
	require.False(t, ok)
}

func TestRevenueCatCleanupGuard_MalformedHouseholdReadDefers(t *testing.T) {
	g := NewRevenueCatCleanupGuard(&cleanupAccess{}, cleanupStates{"member": {AccessUpdatedAt: time.Now()}}, cleanupEntitlements{})
	ok, err := g.CanCleanup(context.Background(), "member")
	require.False(t, ok)
	require.Error(t, err)
}

type cleanupJoiningEntitlements struct{ calls int }

func (e *cleanupJoiningEntitlements) GetActiveEntitlement(context.Context, string, time.Time) (*domain.SubscriptionEntitlement, error) {
	e.calls++
	if e.calls == 1 {
		return nil, repository.ErrEntitlementNotFound
	}
	owner, hh := "new-owner", "new-household"
	return &domain.SubscriptionEntitlement{Source: domain.EntitlementSourceHousehold, PayerUserID: &owner, HouseholdID: &hh}, nil
}
func TestRevenueCatCleanupGuard_HouseholdJoinedDuringRefreshDefers(t *testing.T) {
	g := NewRevenueCatCleanupGuard(&cleanupAccess{}, cleanupStates{"member": {AccessUpdatedAt: time.Now()}}, &cleanupJoiningEntitlements{})
	ok, _ := g.CanCleanup(context.Background(), "member")
	require.False(t, ok)
}
