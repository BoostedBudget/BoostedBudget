package services

import (
	"context"
	"errors"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockEntitlementRepo struct{ mock.Mock }

func (m *mockEntitlementRepo) GetActiveEntitlement(ctx context.Context, userID string) (*domain.SubscriptionEntitlement, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.SubscriptionEntitlement), args.Error(1)
}

type mockUserLoader struct{ mock.Mock }

func (m *mockUserLoader) GetUserByID(ctx context.Context, id string) (*domain.User, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

type mockHouseholdRepo struct{ mock.Mock }

func (m *mockHouseholdRepo) GetHouseholdForUser(ctx context.Context, userID string) (*domain.Household, bool, bool, error) {
	args := m.Called(ctx, userID)
	var h *domain.Household
	if args.Get(0) != nil {
		h = args.Get(0).(*domain.Household)
	}
	return h, args.Bool(1), args.Bool(2), args.Error(3)
}

func (m *mockHouseholdRepo) CountActiveMembers(ctx context.Context, householdID string) (int, error) {
	args := m.Called(ctx, householdID)
	return args.Int(0), args.Error(1)
}

func (m *mockHouseholdRepo) CountPendingInvites(ctx context.Context, householdID string) (int, error) {
	args := m.Called(ctx, householdID)
	return args.Int(0), args.Error(1)
}

func TestResolveEffectiveEntitlement_NoRow_FallsBackToOwn(t *testing.T) {
	entRepo := &mockEntitlementRepo{}
	entRepo.On("GetActiveEntitlement", mock.Anything, "user-1").Return(nil, repository.ErrEntitlementNotFound)
	svc := NewHouseholdEntitlementService(entRepo, &mockUserLoader{}, &mockHouseholdRepo{}, nil)

	user := &domain.User{ID: "user-1", SubscriptionTier: "premium", SubscriptionStatus: "active"}
	eff, err := svc.ResolveEffectiveEntitlement(context.Background(), user)
	require.NoError(t, err)
	assert.Equal(t, "premium", eff.Tier)
	assert.Equal(t, "active", eff.Status)
	assert.Equal(t, domain.EntitlementSourceOwn, eff.Source)
	entRepo.AssertExpectations(t)
}

func TestResolveEffectiveEntitlement_OwnSubscriptionRow_UsesOwn(t *testing.T) {
	entRepo := &mockEntitlementRepo{}
	entRepo.On("GetActiveEntitlement", mock.Anything, "user-1").Return(&domain.SubscriptionEntitlement{
		Source: domain.EntitlementSourceOwn, Tier: "premium", Status: "active",
	}, nil)
	svc := NewHouseholdEntitlementService(entRepo, &mockUserLoader{}, &mockHouseholdRepo{}, nil)

	user := &domain.User{ID: "user-1", SubscriptionTier: "premium", SubscriptionStatus: "active"}
	eff, err := svc.ResolveEffectiveEntitlement(context.Background(), user)
	require.NoError(t, err)
	assert.Equal(t, domain.EntitlementSourceOwn, eff.Source)
}

func TestResolveEffectiveEntitlement_HouseholdRow_OwnerActive_UsesOwnerTier(t *testing.T) {
	entRepo := &mockEntitlementRepo{}
	entRepo.On("GetActiveEntitlement", mock.Anything, "member-1").Return(&domain.SubscriptionEntitlement{
		Source: domain.EntitlementSourceHousehold, Tier: "household_2", Status: "active",
		PayerUserID: strPtr("owner-1"), HouseholdID: strPtr("household-1"),
	}, nil)
	userRepo := &mockUserLoader{}
	userRepo.On("GetUserByID", mock.Anything, "owner-1").Return(&domain.User{
		ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "active",
	}, nil)
	svc := NewHouseholdEntitlementService(entRepo, userRepo, &mockHouseholdRepo{}, nil)

	member := &domain.User{ID: "member-1", SubscriptionTier: "premium", SubscriptionStatus: "trial"}
	eff, err := svc.ResolveEffectiveEntitlement(context.Background(), member)
	require.NoError(t, err)
	assert.Equal(t, "household_2", eff.Tier)
	assert.Equal(t, "active", eff.Status)
	assert.Equal(t, domain.EntitlementSourceHousehold, eff.Source)
}

func TestResolveEffectiveEntitlement_HouseholdRow_OwnerTrialing_UsesOwnerTier(t *testing.T) {
	entRepo := &mockEntitlementRepo{}
	entRepo.On("GetActiveEntitlement", mock.Anything, "member-1").Return(&domain.SubscriptionEntitlement{
		Source: domain.EntitlementSourceHousehold, Tier: "household_2", Status: "trialing",
		PayerUserID: strPtr("owner-1"), HouseholdID: strPtr("household-1"),
	}, nil)
	userRepo := &mockUserLoader{}
	userRepo.On("GetUserByID", mock.Anything, "owner-1").Return(&domain.User{
		ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "trialing",
	}, nil)
	svc := NewHouseholdEntitlementService(entRepo, userRepo, &mockHouseholdRepo{}, nil)

	member := &domain.User{ID: "member-1", SubscriptionTier: "premium", SubscriptionStatus: "trial"}
	eff, err := svc.ResolveEffectiveEntitlement(context.Background(), member)
	require.NoError(t, err)
	assert.Equal(t, "household_2", eff.Tier, "owner's trialing status is not read-only, so member should get owner's tier")
	assert.Equal(t, "trialing", eff.Status)
	assert.Equal(t, domain.EntitlementSourceHousehold, eff.Source)
}

func TestResolveEffectiveEntitlement_OwnerPastDue_MemberFallsBackToOwn(t *testing.T) {
	entRepo := &mockEntitlementRepo{}
	entRepo.On("GetActiveEntitlement", mock.Anything, "member-1").Return(&domain.SubscriptionEntitlement{
		Source: domain.EntitlementSourceHousehold, Tier: "household_2", Status: "active",
		PayerUserID: strPtr("owner-1"), HouseholdID: strPtr("household-1"),
	}, nil)
	userRepo := &mockUserLoader{}
	userRepo.On("GetUserByID", mock.Anything, "owner-1").Return(&domain.User{
		ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "past_due",
	}, nil)
	svc := NewHouseholdEntitlementService(entRepo, userRepo, &mockHouseholdRepo{}, nil)

	member := &domain.User{ID: "member-1", SubscriptionTier: "premium", SubscriptionStatus: "trial"}
	eff, err := svc.ResolveEffectiveEntitlement(context.Background(), member)
	require.NoError(t, err)
	assert.Equal(t, "premium", eff.Tier, "member should revert to own tier when owner is past_due")
	assert.Equal(t, domain.EntitlementSourceOwn, eff.Source)
}

func TestResolveEffectiveEntitlement_OwnerManualOverrideHonored(t *testing.T) {
	entRepo := &mockEntitlementRepo{}
	entRepo.On("GetActiveEntitlement", mock.Anything, "member-1").Return(&domain.SubscriptionEntitlement{
		Source: domain.EntitlementSourceHousehold, Tier: "household_2", Status: "active",
		PayerUserID: strPtr("owner-1"), HouseholdID: strPtr("household-1"),
	}, nil)
	userRepo := &mockUserLoader{}
	userRepo.On("GetUserByID", mock.Anything, "owner-1").Return(&domain.User{
		ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "active",
		ManualTierOverride: strPtr("household_5"),
	}, nil)
	svc := NewHouseholdEntitlementService(entRepo, userRepo, &mockHouseholdRepo{}, nil)

	member := &domain.User{ID: "member-1", SubscriptionTier: "premium", SubscriptionStatus: "trial"}
	eff, err := svc.ResolveEffectiveEntitlement(context.Background(), member)
	require.NoError(t, err)
	assert.Equal(t, "household_5", eff.Tier, "owner's active manual override should be honored live")
}

func TestResolveEffectiveEntitlement_RepositoryError_Propagates(t *testing.T) {
	entRepo := &mockEntitlementRepo{}
	entRepo.On("GetActiveEntitlement", mock.Anything, "user-1").Return(nil, errors.New("db timeout"))
	svc := NewHouseholdEntitlementService(entRepo, &mockUserLoader{}, &mockHouseholdRepo{}, nil)

	user := &domain.User{ID: "user-1", SubscriptionTier: "premium", SubscriptionStatus: "active"}
	_, err := svc.ResolveEffectiveEntitlement(context.Background(), user)
	require.Error(t, err, "real DB errors must propagate so the caller can decide fail-open policy")
}

func TestResolveEffectiveEntitlement_OwnerLookupError_Propagates(t *testing.T) {
	entRepo := &mockEntitlementRepo{}
	entRepo.On("GetActiveEntitlement", mock.Anything, "member-1").Return(&domain.SubscriptionEntitlement{
		Source: domain.EntitlementSourceHousehold, Tier: "household_2", Status: "active",
		PayerUserID: strPtr("owner-1"), HouseholdID: strPtr("household-1"),
	}, nil)
	userRepo := &mockUserLoader{}
	userRepo.On("GetUserByID", mock.Anything, "owner-1").Return(nil, errors.New("db timeout"))
	svc := NewHouseholdEntitlementService(entRepo, userRepo, &mockHouseholdRepo{}, nil)

	member := &domain.User{ID: "member-1", SubscriptionTier: "premium", SubscriptionStatus: "trial"}
	_, err := svc.ResolveEffectiveEntitlement(context.Background(), member)
	require.Error(t, err, "owner-lookup errors must propagate, not silently fall back to the member's own tier")
}

func TestGetEntitlementSummary_Solo_NoHousehold(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)
	entRepo := &mockEntitlementRepo{}
	entRepo.On("GetActiveEntitlement", mock.Anything, "user-1").Return(nil, repository.ErrEntitlementNotFound)
	householdRepo := &mockHouseholdRepo{}
	householdRepo.On("GetHouseholdForUser", mock.Anything, "user-1").Return(nil, false, false, nil)
	svc := NewHouseholdEntitlementService(entRepo, &mockUserLoader{}, householdRepo, tierConfig)

	user := &domain.User{ID: "user-1", SubscriptionTier: "premium", SubscriptionStatus: "active"}
	summary, err := svc.GetEntitlementSummary(context.Background(), user)
	require.NoError(t, err)
	assert.Equal(t, "premium", summary.Tier)
	assert.Equal(t, 0, summary.SeatsActive)
	assert.Equal(t, 0, summary.SeatsPending)
	assert.False(t, summary.IsOwner)
	assert.Equal(t, domain.EntitlementSourceOwn, summary.Source)
}

func TestGetEntitlementSummary_Owner_WithSeats(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)
	entRepo := &mockEntitlementRepo{}
	entRepo.On("GetActiveEntitlement", mock.Anything, "owner-1").Return(nil, repository.ErrEntitlementNotFound)
	householdRepo := &mockHouseholdRepo{}
	householdRepo.On("GetHouseholdForUser", mock.Anything, "owner-1").
		Return(&domain.Household{ID: "household-1"}, true, true, nil)
	householdRepo.On("CountActiveMembers", mock.Anything, "household-1").Return(2, nil)
	householdRepo.On("CountPendingInvites", mock.Anything, "household-1").Return(1, nil)
	svc := NewHouseholdEntitlementService(entRepo, &mockUserLoader{}, householdRepo, tierConfig)

	owner := &domain.User{ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "active"}
	summary, err := svc.GetEntitlementSummary(context.Background(), owner)
	require.NoError(t, err)
	assert.Equal(t, "household_2", summary.Tier)
	assert.Equal(t, 2, summary.UsersIncluded)
	assert.Equal(t, 2, summary.SeatsActive)
	assert.Equal(t, 1, summary.SeatsPending)
	assert.True(t, summary.IsOwner)
}
