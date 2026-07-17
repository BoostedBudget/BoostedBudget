package services

import (
	"context"
	"errors"
	"testing"
	"time"

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

// CreateHouseholdEntitlement and ExpireActiveEntitlement (#1367 — Phase 3) are not
// exercised by these Phase 2 resolver tests; stubbed to satisfy the interface.
func (m *mockEntitlementRepo) CreateHouseholdEntitlement(ctx context.Context, userID, tier, status, payerUserID, householdID string) error {
	args := m.Called(ctx, userID, tier, status, payerUserID, householdID)
	return args.Error(0)
}

func (m *mockEntitlementRepo) ExpireActiveEntitlement(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

// ExpireActiveHouseholdEntitlement (#1371 — Phase 7 foundation) is not exercised by
// these Phase 2 resolver tests; stubbed to satisfy the expanded EntitlementRepository
// interface.
func (m *mockEntitlementRepo) ExpireActiveHouseholdEntitlement(ctx context.Context, userID, householdID string) error {
	args := m.Called(ctx, userID, householdID)
	return args.Error(0)
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

// The methods below (#1367 — Phase 3 writes) are not exercised by these Phase 2
// resolver tests; stubbed to satisfy the expanded HouseholdRepository interface.
func (m *mockHouseholdRepo) CreateHousehold(ctx context.Context, h *domain.Household) error {
	args := m.Called(ctx, h)
	return args.Error(0)
}

func (m *mockHouseholdRepo) LockHouseholdForUpdate(ctx context.Context, householdID string) (string, error) {
	args := m.Called(ctx, householdID)
	return args.String(0), args.Error(1)
}

// LockActiveMembershipForUser (#1368 — Phase 4 write) is not exercised by these
// Phase 2 resolver tests; stubbed to satisfy the expanded HouseholdRepository interface.
func (m *mockHouseholdRepo) LockActiveMembershipForUser(ctx context.Context, userID string) (*domain.Household, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Household), args.Error(1)
}

func (m *mockHouseholdRepo) UpdateHouseholdName(ctx context.Context, householdID, ownerUserID, name string) error {
	args := m.Called(ctx, householdID, ownerUserID, name)
	return args.Error(0)
}

func (m *mockHouseholdRepo) DeleteHousehold(ctx context.Context, householdID, ownerUserID string) error {
	args := m.Called(ctx, householdID, ownerUserID)
	return args.Error(0)
}

func (m *mockHouseholdRepo) InsertMember(ctx context.Context, member *domain.HouseholdMember) error {
	args := m.Called(ctx, member)
	return args.Error(0)
}

func (m *mockHouseholdRepo) ListMembers(ctx context.Context, householdID string) ([]domain.HouseholdMember, error) {
	args := m.Called(ctx, householdID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.HouseholdMember), args.Error(1)
}

func (m *mockHouseholdRepo) GetActiveMember(ctx context.Context, householdID, userID string) (*domain.HouseholdMember, error) {
	args := m.Called(ctx, householdID, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.HouseholdMember), args.Error(1)
}

func (m *mockHouseholdRepo) RemoveMember(ctx context.Context, householdID, userID string) error {
	args := m.Called(ctx, householdID, userID)
	return args.Error(0)
}

// LockHouseholdByID and DeleteRemovedMember (#1371 — Phase 7 foundation) are not
// exercised by these Phase 2 resolver tests; stubbed to satisfy the expanded
// HouseholdRepository interface.
func (m *mockHouseholdRepo) LockHouseholdByID(ctx context.Context, householdID string) (*domain.Household, error) {
	args := m.Called(ctx, householdID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Household), args.Error(1)
}

func (m *mockHouseholdRepo) DeleteRemovedMember(ctx context.Context, membershipID string) (string, string, error) {
	args := m.Called(ctx, membershipID)
	return args.String(0), args.String(1), args.Error(2)
}

// GetHouseholdByID (#1371 — Round 9 Gap Analysis fix) is not exercised by these Phase 2
// resolver tests; stubbed to satisfy the expanded HouseholdRepository interface.
func (m *mockHouseholdRepo) GetHouseholdByID(ctx context.Context, householdID string) (*domain.Household, error) {
	args := m.Called(ctx, householdID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Household), args.Error(1)
}

// SetOverCapacity, ClearOverCapacity, MarkOverCapacityStartNoticeSent,
// MarkOverCapacityWarn3dSent, ListHouseholdsWithMembers, GetRemovedMemberForUpdate,
// RestoreMember (#1371 — Phase 7, Task 1), and ListMembershipsPastPurgeWindow
// (#1371 — Phase 7, Task 2) are not exercised by these Phase 2 resolver tests;
// stubbed to satisfy the expanded HouseholdRepository interface.
func (m *mockHouseholdRepo) SetOverCapacity(ctx context.Context, householdID string, since time.Time) (time.Time, error) {
	args := m.Called(ctx, householdID, since)
	if args.Get(0) == nil {
		return time.Time{}, args.Error(1)
	}
	return args.Get(0).(time.Time), args.Error(1)
}

func (m *mockHouseholdRepo) ClearOverCapacity(ctx context.Context, householdID string) error {
	args := m.Called(ctx, householdID)
	return args.Error(0)
}

func (m *mockHouseholdRepo) ClearOverCapacityIfAnchorMatches(ctx context.Context, householdID string, expectedOverCapacitySince time.Time) error {
	args := m.Called(ctx, householdID, expectedOverCapacitySince)
	return args.Error(0)
}

func (m *mockHouseholdRepo) MarkOverCapacityStartNoticeSent(ctx context.Context, householdID string, sentAt time.Time, expectedOverCapacitySince time.Time) error {
	args := m.Called(ctx, householdID, sentAt, expectedOverCapacitySince)
	return args.Error(0)
}

func (m *mockHouseholdRepo) MarkOverCapacityWarn3dSent(ctx context.Context, householdID string, sentAt time.Time, expectedOverCapacitySince time.Time) error {
	args := m.Called(ctx, householdID, sentAt, expectedOverCapacitySince)
	return args.Error(0)
}

func (m *mockHouseholdRepo) ListHouseholdsWithMembers(ctx context.Context) ([]*domain.Household, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Household), args.Error(1)
}

func (m *mockHouseholdRepo) GetRemovedMemberForUpdate(ctx context.Context, householdID, userID string, withinDays int) (*domain.HouseholdMember, error) {
	args := m.Called(ctx, householdID, userID, withinDays)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.HouseholdMember), args.Error(1)
}

func (m *mockHouseholdRepo) RestoreMember(ctx context.Context, membershipID string) (time.Time, error) {
	args := m.Called(ctx, membershipID)
	return args.Get(0).(time.Time), args.Error(1)
}

func (m *mockHouseholdRepo) ListMembershipsPastPurgeWindow(ctx context.Context, purgeAfterDays int) ([]*domain.HouseholdMember, error) {
	args := m.Called(ctx, purgeAfterDays)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.HouseholdMember), args.Error(1)
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

// TestResolveEffectiveEntitlement_OwnerCancelled_MemberFallsBackToOwn closes a
// gap found by requirements-gap-analysis: the issue's TDD checklist names
// "Owner cancels -> members revert to own subscription_tier" explicitly, but
// only the past_due branch of IsReadOnlySubscription() was previously tested.
// cancelled and past_due share the same read-only branch in the resolver, but
// the checklist names cancelled specifically, so it gets its own test.
func TestResolveEffectiveEntitlement_OwnerCancelled_MemberFallsBackToOwn(t *testing.T) {
	entRepo := &mockEntitlementRepo{}
	entRepo.On("GetActiveEntitlement", mock.Anything, "member-1").Return(&domain.SubscriptionEntitlement{
		Source: domain.EntitlementSourceHousehold, Tier: "household_2", Status: "active",
		PayerUserID: strPtr("owner-1"), HouseholdID: strPtr("household-1"),
	}, nil)
	userRepo := &mockUserLoader{}
	userRepo.On("GetUserByID", mock.Anything, "owner-1").Return(&domain.User{
		ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "cancelled",
	}, nil)
	svc := NewHouseholdEntitlementService(entRepo, userRepo, &mockHouseholdRepo{}, nil)

	member := &domain.User{ID: "member-1", SubscriptionTier: "premium", SubscriptionStatus: "trial"}
	eff, err := svc.ResolveEffectiveEntitlement(context.Background(), member)
	require.NoError(t, err)
	assert.Equal(t, "premium", eff.Tier, "member should revert to own tier when owner is cancelled")
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

// TestResolveEffectiveEntitlement_MemberManualOverride_LosesToActiveHouseholdCoverage
// ratifies the product decision (Gap Analysis Round 2, GT-2): household coverage wins
// over a covered member's OWN manual_tier_override while the owner is active. This was
// previously the mechanical result of Phase 2's implementation order, never an explicit
// decision — see the deferred-from-Phase-2 comment on #1367. A member's own admin-granted
// comp only re-surfaces via the owner-lapsed fallback (see the sibling test below).
func TestResolveEffectiveEntitlement_MemberManualOverride_LosesToActiveHouseholdCoverage(t *testing.T) {
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

	member := &domain.User{
		ID: "member-1", SubscriptionTier: "premium", SubscriptionStatus: "trial",
		ManualTierOverride: strPtr("household_5"), // member's own comp — must NOT win here
	}
	eff, err := svc.ResolveEffectiveEntitlement(context.Background(), member)
	require.NoError(t, err)
	assert.Equal(t, "household_2", eff.Tier, "owner coverage wins over the member's own manual override while the owner is active")
	assert.Equal(t, domain.EntitlementSourceHousehold, eff.Source)
}

// TestResolveEffectiveEntitlement_MemberManualOverride_AppliesWhenOwnerLapses is the
// counterpart to the test above: once the owner loses active coverage, the member falls
// back to their OWN effective tier — which honors their own manual_tier_override, since
// ComputeEffectiveTier() already applies it. No special-casing needed in the resolver;
// this test locks in that the existing owner-lapsed fallback path composes correctly with
// a member's own override.
func TestResolveEffectiveEntitlement_MemberManualOverride_AppliesWhenOwnerLapses(t *testing.T) {
	entRepo := &mockEntitlementRepo{}
	entRepo.On("GetActiveEntitlement", mock.Anything, "member-1").Return(&domain.SubscriptionEntitlement{
		Source: domain.EntitlementSourceHousehold, Tier: "household_2", Status: "active",
		PayerUserID: strPtr("owner-1"), HouseholdID: strPtr("household-1"),
	}, nil)
	userRepo := &mockUserLoader{}
	userRepo.On("GetUserByID", mock.Anything, "owner-1").Return(&domain.User{
		ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "cancelled",
	}, nil)
	svc := NewHouseholdEntitlementService(entRepo, userRepo, &mockHouseholdRepo{}, nil)

	member := &domain.User{
		ID: "member-1", SubscriptionTier: "premium", SubscriptionStatus: "trial",
		ManualTierOverride: strPtr("household_5"),
	}
	eff, err := svc.ResolveEffectiveEntitlement(context.Background(), member)
	require.NoError(t, err)
	assert.Equal(t, "household_5", eff.Tier, "member's own manual override applies once the owner lapses")
	assert.Equal(t, domain.EntitlementSourceOwn, eff.Source)
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

// ─── IsCoveredByHousehold (#1367 — Phase 3: read-only middleware household-awareness) ─

func TestIsCoveredByHousehold_ActiveHouseholdEntitlement_True(t *testing.T) {
	tierConfig := testTierConfig(t)
	member := &domain.User{ID: "member-1", SubscriptionTier: "premium", SubscriptionStatus: "inactive"}
	owner := &domain.User{ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "active"}

	userRepo := &mockUserLoader{}
	userRepo.On("GetUserByID", mock.Anything, "member-1").Return(member, nil)
	userRepo.On("GetUserByID", mock.Anything, "owner-1").Return(owner, nil)

	entRepo := &mockEntitlementRepo{}
	payerID := "owner-1"
	entRepo.On("GetActiveEntitlement", mock.Anything, "member-1").Return(&domain.SubscriptionEntitlement{
		Source: domain.EntitlementSourceHousehold, PayerUserID: &payerID,
	}, nil)

	svc := NewHouseholdEntitlementService(entRepo, userRepo, &mockHouseholdRepo{}, tierConfig)

	covered, err := svc.IsCoveredByHousehold(context.Background(), "member-1")
	require.NoError(t, err)
	assert.True(t, covered, "member with an active household entitlement whose owner is active should be covered")
}

func TestIsCoveredByHousehold_OwnerReadOnly_False(t *testing.T) {
	tierConfig := testTierConfig(t)
	member := &domain.User{ID: "member-1", SubscriptionTier: "premium", SubscriptionStatus: "inactive"}
	owner := &domain.User{ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "past_due"}

	userRepo := &mockUserLoader{}
	userRepo.On("GetUserByID", mock.Anything, "member-1").Return(member, nil)
	userRepo.On("GetUserByID", mock.Anything, "owner-1").Return(owner, nil)

	entRepo := &mockEntitlementRepo{}
	payerID := "owner-1"
	entRepo.On("GetActiveEntitlement", mock.Anything, "member-1").Return(&domain.SubscriptionEntitlement{
		Source: domain.EntitlementSourceHousehold, PayerUserID: &payerID,
	}, nil)

	svc := NewHouseholdEntitlementService(entRepo, userRepo, &mockHouseholdRepo{}, tierConfig)

	covered, err := svc.IsCoveredByHousehold(context.Background(), "member-1")
	require.NoError(t, err)
	assert.False(t, covered, "member whose owner lost active coverage should not be treated as covered")
}

func TestIsCoveredByHousehold_SoloUser_False(t *testing.T) {
	tierConfig := testTierConfig(t)
	solo := &domain.User{ID: "user-1", SubscriptionTier: "premium", SubscriptionStatus: "inactive"}

	userRepo := &mockUserLoader{}
	userRepo.On("GetUserByID", mock.Anything, "user-1").Return(solo, nil)

	entRepo := &mockEntitlementRepo{}
	entRepo.On("GetActiveEntitlement", mock.Anything, "user-1").Return(nil, repository.ErrEntitlementNotFound)

	svc := NewHouseholdEntitlementService(entRepo, userRepo, &mockHouseholdRepo{}, tierConfig)

	covered, err := svc.IsCoveredByHousehold(context.Background(), "user-1")
	require.NoError(t, err)
	assert.False(t, covered)
}
