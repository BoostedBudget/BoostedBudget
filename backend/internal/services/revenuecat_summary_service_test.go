package services

import (
	"context"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/require"
)

type summaryOwn struct {
	access domain.RevenueCatAccess
	err    error
}

func (s summaryOwn) ResolveOwnAccessAt(context.Context, string, time.Time) (domain.RevenueCatAccess, error) {
	return s.access, s.err
}

type summaryState struct {
	state domain.RevenueCatState
	err   error
}

func (s summaryState) Read(context.Context, string) (domain.RevenueCatState, error) {
	return s.state, s.err
}

type summaryEffective struct {
	effective domain.EffectiveEntitlement
	err       error
}

func (s summaryEffective) ResolveEffectiveEntitlement(context.Context, *domain.User) (domain.EffectiveEntitlement, error) {
	return s.effective, s.err
}

func TestRevenueCatSummaryKeepsBillingVisibleWithComp(t *testing.T) {
	now := time.Now()
	end := now.Add(time.Hour)
	s := NewRevenueCatSummaryService(summaryOwn{access: domain.RevenueCatAccess{Tier: "household_5", Status: "active", Source: "comp"}}, summaryState{state: domain.RevenueCatState{AccessUpdatedAt: now, BillingUpdatedAt: &now, Grants: []domain.RevenueCatGrant{{Key: "comp_household_5", ExpiresAt: &end}, {Key: "paid_premium"}}, Billing: []domain.RevenueCatBilling{{ID: "billing-id", Status: "active"}}}}, summaryEffective{effective: domain.EffectiveEntitlement{Tier: "household_5", Status: "active", Source: domain.EntitlementSourceOwn}})
	out := s.Summary(context.Background(), &domain.User{ID: "user"})
	require.True(t, out.AccessResolved)
	require.True(t, out.BillingAvailable)
	require.Len(t, out.Billing, 1)
	require.Equal(t, "household_5", out.Comp.Tier)
	require.Equal(t, "comp", out.Own.Source)
	require.NotNil(t, out.Paid)
	require.Equal(t, "premium", out.Paid.Tier)
}

func TestRevenueCatSummaryNeverFallsBackToLegacyAccess(t *testing.T) {
	s := NewRevenueCatSummaryService(summaryOwn{err: domain.ErrRevenueCatUnresolved}, summaryState{err: domain.ErrRevenueCatUnresolved}, summaryEffective{err: domain.ErrRevenueCatUnresolved})
	out := s.Summary(context.Background(), &domain.User{ID: "user", SubscriptionStatus: "active", SubscriptionTier: "household_5"})
	require.False(t, out.AccessResolved)
	require.Nil(t, out.Effective)
	require.Nil(t, out.Own)
	require.False(t, out.BillingAvailable)
	require.Nil(t, out.Billing)
}

func TestRevenueCatSummaryHouseholdAccessSurvivesOwnOutage(t *testing.T) {
	now := time.Now()
	stale := now.Add(-domain.RevenueCatMaxAge)
	s := NewRevenueCatSummaryService(summaryOwn{err: domain.ErrRevenueCatUnresolved}, summaryState{state: domain.RevenueCatState{BillingUpdatedAt: &stale, Billing: []domain.RevenueCatBilling{{ID: "stale"}}}}, summaryEffective{effective: domain.EffectiveEntitlement{Tier: "household_2", Status: "active", Source: domain.EntitlementSourceHousehold}})
	out := s.Summary(context.Background(), &domain.User{ID: "user"})
	require.True(t, out.AccessResolved)
	require.Equal(t, domain.EntitlementSourceHousehold, out.Effective.Source)
	require.Nil(t, out.Own)
	require.False(t, out.BillingAvailable)
	require.Nil(t, out.Billing)
}
