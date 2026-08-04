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

func (m *mockEntitlementRepo) GetActiveEntitlement(ctx context.Context, userID string, asOf time.Time) (*domain.SubscriptionEntitlement, error) {
	args := m.Called(ctx, userID, asOf)
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
	entRepo.On("GetActiveEntitlement", mock.Anything, "user-1", mock.Anything).Return(nil, repository.ErrEntitlementNotFound)
	svc := NewHouseholdEntitlementService(entRepo, &mockUserLoader{}, &mockHouseholdRepo{}, nil)

	user := &domain.User{ID: "user-1", SubscriptionTier: "premium", SubscriptionStatus: "active"}
	eff, err := svc.ResolveEffectiveEntitlement(context.Background(), user)
	require.NoError(t, err)
	assert.Equal(t, "premium", eff.Tier)
	assert.Equal(t, "active", eff.Status)
	assert.Equal(t, domain.EntitlementSourceOwn, eff.Source)
	entRepo.AssertExpectations(t)
}

// TestResolveEffectiveEntitlementAt_UsesPassedInstant_NotInternalClock is the resolver-level pin
// for Batch S2 Task 1, complementing the higher-level (SubscriptionService)
// TestGetEffectiveSubscriptionAt_ProductionWiring_RealResolver_TierDerivedFromAsOf_NotResolversOwnClock
// (subscription_service_test.go): calls ResolveEffectiveEntitlementAt directly with an instant far
// in the PAST — before which an override was still live, but which has long since expired
// relative to the real wall clock this test actually runs under. A correct implementation must
// resolve the override tier because it was live AS OF the passed instant; an implementation that
// (bug-for-bug) ignored the parameter and read time.Now() internally would instead see the
// long-expired override and fall back to the raw tier.
func TestResolveEffectiveEntitlementAt_UsesPassedInstant_NotInternalClock(t *testing.T) {
	entRepo := &mockEntitlementRepo{}
	pastInstant := time.Now().Add(-48 * time.Hour)
	entRepo.On("GetActiveEntitlement", mock.Anything, "user-past-instant", pastInstant).Return(nil, repository.ErrEntitlementNotFound)
	svc := NewHouseholdEntitlementService(entRepo, &mockUserLoader{}, &mockHouseholdRepo{}, nil)

	override := "household_3"
	overrideEndsAt := pastInstant.Add(1 * time.Hour) // live AT pastInstant, long expired by now()
	user := &domain.User{
		ID:                 "user-past-instant",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		ManualTierOverride: &override,
		ManualTierEndsAt:   &overrideEndsAt,
	}

	eff, err := svc.ResolveEffectiveEntitlementAt(context.Background(), user, pastInstant)
	require.NoError(t, err)
	assert.Equal(t, "household_3", eff.Tier,
		"must resolve the override tier AS OF the passed instant (still live then), not fall back "+
			"to the raw tier because the override has since expired relative to the real wall clock")
	assert.Equal(t, domain.EntitlementSourceOwn, eff.Source)
	entRepo.AssertExpectations(t) // proves GetActiveEntitlement was called WITH pastInstant, not time.Now()
}

func TestResolveEffectiveEntitlement_OwnSubscriptionRow_UsesOwn(t *testing.T) {
	entRepo := &mockEntitlementRepo{}
	entRepo.On("GetActiveEntitlement", mock.Anything, "user-1", mock.Anything).Return(&domain.SubscriptionEntitlement{
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
	entRepo.On("GetActiveEntitlement", mock.Anything, "member-1", mock.Anything).Return(&domain.SubscriptionEntitlement{
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
	entRepo.On("GetActiveEntitlement", mock.Anything, "member-1", mock.Anything).Return(&domain.SubscriptionEntitlement{
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
	entRepo.On("GetActiveEntitlement", mock.Anything, "member-1", mock.Anything).Return(&domain.SubscriptionEntitlement{
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
	entRepo.On("GetActiveEntitlement", mock.Anything, "member-1", mock.Anything).Return(&domain.SubscriptionEntitlement{
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
	entRepo.On("GetActiveEntitlement", mock.Anything, "member-1", mock.Anything).Return(&domain.SubscriptionEntitlement{
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
	entRepo.On("GetActiveEntitlement", mock.Anything, "member-1", mock.Anything).Return(&domain.SubscriptionEntitlement{
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
	entRepo.On("GetActiveEntitlement", mock.Anything, "member-1", mock.Anything).Return(&domain.SubscriptionEntitlement{
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

// ─── Issue #1413 — effective status/comp reaches the household resolver ──────

// TestResolveEffectiveEntitlement_CompedSoloUser_StatusIsActive proves the fix for the
// asymmetry bug in the `own` EffectiveEntitlement built at the top of
// ResolveEffectiveEntitlement: Tier already used ComputeEffectiveTier(), but Status used
// the raw SubscriptionStatus — so a solo user admin-comped via manual_tier_override (raw
// status still "inactive", e.g. lapsed real subscription) got the comped TIER but not the
// comped STATUS, leaving every status-gated access check reading "inactive". Status must
// now be "active" whenever ComputeEffectiveStatus() says so.
func TestResolveEffectiveEntitlement_CompedSoloUser_StatusIsActive(t *testing.T) {
	entRepo := &mockEntitlementRepo{}
	entRepo.On("GetActiveEntitlement", mock.Anything, "user-1", mock.Anything).Return(nil, repository.ErrEntitlementNotFound)
	svc := NewHouseholdEntitlementService(entRepo, &mockUserLoader{}, &mockHouseholdRepo{}, nil)

	futureEnd := time.Now().Add(30 * 24 * time.Hour)
	user := &domain.User{
		ID: "user-1", SubscriptionTier: "premium", SubscriptionStatus: "inactive",
		ManualTierOverride: strPtr("household_4"), ManualTierEndsAt: &futureEnd,
	}
	eff, err := svc.ResolveEffectiveEntitlement(context.Background(), user)
	require.NoError(t, err)
	assert.Equal(t, "household_4", eff.Tier)
	assert.Equal(t, "active", eff.Status, "a comped solo user's raw inactive status must not leak through — the comp must unlock access, not just relabel the tier")
	assert.Equal(t, domain.EntitlementSourceOwn, eff.Source)
}

// ─── Issue #1413 (this task) — tier axis: malformed manual_tier_override must not 500 ────────

// TestResolveEffectiveEntitlement_SoloUser_MalformedOverride_TierFallsBackToRaw is the RED test
// for the SOLO-user case (no active entitlement row — the common case): the owner's raw status
// is ALREADY "active" here (unlike the owner-mutation-bypass tests above, where the malformed
// override's rejection short-circuits via the STATUS axis before the Tier line is ever reached),
// so this specifically exercises the TIER axis leak that survives even when the status axis is
// fine. Before this fix, the "own" branch called domain.User.ComputeEffectiveTier()
// unconditionally, so eff.Tier carried the raw garbage string straight into
// tierConfig.GetTier() at the GetEntitlementSummary/SubscriptionService call sites — the root
// cause of the /households/me/entitlement 500 for the majority (non-household) case, and the
// worst-affected user (per the issue) is exactly this one: actively paying, so the read-only
// middleware waves every request through, and only then does tier resolution 500.
func TestResolveEffectiveEntitlement_SoloUser_MalformedOverride_TierFallsBackToRaw(t *testing.T) {
	entRepo := &mockEntitlementRepo{}
	entRepo.On("GetActiveEntitlement", mock.Anything, "user-1", mock.Anything).Return(nil, repository.ErrEntitlementNotFound)
	svc := NewHouseholdEntitlementService(entRepo, &mockUserLoader{}, &mockHouseholdRepo{}, nil)
	svc.SetOverrideTierValidator(func(tier string) bool { return tier == "household_2" || tier == "premium" })

	futureEnd := time.Now().Add(30 * 24 * time.Hour)
	user := &domain.User{
		ID: "user-1", SubscriptionTier: "premium", SubscriptionStatus: "active", // raw status already active
		ManualTierOverride: strPtr("not-a-real-tier"), ManualTierEndsAt: &futureEnd,
	}
	eff, err := svc.ResolveEffectiveEntitlement(context.Background(), user)
	require.NoError(t, err)
	assert.Equal(t, "premium", eff.Tier, "a malformed override must fall back to the raw tier, not the garbage override string")
	assert.Equal(t, domain.EntitlementSourceOwn, eff.Source)
}

// TestResolveEffectiveEntitlement_SoloUser_ValidOverride_TierAppliesOverride is the POSITIVE
// CONTROL for the fix above, with the exact same validator wired: a VALID override must still
// resolve to the override tier. An over-tight fix that makes every comp fall back to the raw
// tier would silently un-comp every legitimately comped user, which is worse than the bug.
func TestResolveEffectiveEntitlement_SoloUser_ValidOverride_TierAppliesOverride(t *testing.T) {
	entRepo := &mockEntitlementRepo{}
	entRepo.On("GetActiveEntitlement", mock.Anything, "user-1", mock.Anything).Return(nil, repository.ErrEntitlementNotFound)
	svc := NewHouseholdEntitlementService(entRepo, &mockUserLoader{}, &mockHouseholdRepo{}, nil)
	svc.SetOverrideTierValidator(func(tier string) bool { return tier == "household_3" || tier == "premium" })

	futureEnd := time.Now().Add(30 * 24 * time.Hour)
	user := &domain.User{
		ID: "user-1", SubscriptionTier: "premium", SubscriptionStatus: "active",
		ManualTierOverride: strPtr("household_3"), ManualTierEndsAt: &futureEnd,
	}
	eff, err := svc.ResolveEffectiveEntitlement(context.Background(), user)
	require.NoError(t, err)
	assert.Equal(t, "household_3", eff.Tier, "a valid override must still resolve to the override tier")
	assert.Equal(t, domain.EntitlementSourceOwn, eff.Source)
}

// TestResolveEffectiveEntitlement_OwnerActiveRawStatus_MalformedOverride_TierFallsBackToRaw is
// the household-owner sibling of the solo-user RED test above: the owner's RAW status is
// "active" (never forced there by the override — it was already active), so the STATUS axis
// gate (ownerHasOverride && validator rejects -> ownerStatus reverts to raw) is a no-op here:
// "active" reverting to "active" changes nothing, and IsReadOnlySubscription("active") is false
// either way. The member therefore stays covered by the household (correctly), but before this
// fix the returned eff.Tier still carried owner.ComputeEffectiveTier()'s unvalidated garbage
// value straight through to the caller's tierConfig.GetTier() call — this is what made a
// malformed OWNER override 500 every member's own entitlement lookup even when the owner was
// actively paying, not just lapsed.
func TestResolveEffectiveEntitlement_OwnerActiveRawStatus_MalformedOverride_TierFallsBackToRaw(t *testing.T) {
	entRepo := &mockEntitlementRepo{}
	entRepo.On("GetActiveEntitlement", mock.Anything, "member-1", mock.Anything).Return(&domain.SubscriptionEntitlement{
		Source: domain.EntitlementSourceHousehold, Tier: "household_2", Status: "active",
		PayerUserID: strPtr("owner-1"), HouseholdID: strPtr("household-1"),
	}, nil)
	userRepo := &mockUserLoader{}
	futureEnd := time.Now().Add(30 * 24 * time.Hour)
	userRepo.On("GetUserByID", mock.Anything, "owner-1").Return(&domain.User{
		ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "active", // raw status already active
		ManualTierOverride: strPtr("not-a-real-tier"), ManualTierEndsAt: &futureEnd,
	}, nil)
	svc := NewHouseholdEntitlementService(entRepo, userRepo, &mockHouseholdRepo{}, nil)
	svc.SetOverrideTierValidator(func(tier string) bool { return tier == "household_2" || tier == "premium" })

	member := &domain.User{ID: "member-1", SubscriptionTier: "premium", SubscriptionStatus: "trial"}
	eff, err := svc.ResolveEffectiveEntitlement(context.Background(), member)
	require.NoError(t, err)
	assert.Equal(t, domain.EntitlementSourceHousehold, eff.Source, "owner's raw status is genuinely active, so the member stays covered")
	assert.Equal(t, "household_2", eff.Tier, "a malformed owner override must fall back to the owner's raw tier, not the garbage string")
}

// TestGetEntitlementSummary_SoloUser_MalformedOverride_NotError is the GetEntitlementSummary-
// level RED test: this is the exact method GET /api/v1/households/me/entitlement calls, and
// before this fix a malformed override reached its tierConfig.GetTier(eff.Tier) call as an
// unrecognized name and 500'd for every user hitting this dashboard-path GET — not just
// household owners, but any solo user with a corrupt override row.
func TestGetEntitlementSummary_SoloUser_MalformedOverride_NotError(t *testing.T) {
	entRepo := &mockEntitlementRepo{}
	entRepo.On("GetActiveEntitlement", mock.Anything, "user-1", mock.Anything).Return(nil, repository.ErrEntitlementNotFound)
	householdRepo := &mockHouseholdRepo{}
	householdRepo.On("GetHouseholdForUser", mock.Anything, "user-1").Return(nil, false, false, nil)
	svc := NewHouseholdEntitlementService(entRepo, &mockUserLoader{}, householdRepo, fakeTierConfig())
	svc.SetOverrideTierValidator(func(tier string) bool {
		_, err := fakeTierConfig().GetTier(domain.TierName(tier))
		return err == nil
	})

	futureEnd := time.Now().Add(30 * 24 * time.Hour)
	user := &domain.User{
		ID: "user-1", SubscriptionTier: "premium", SubscriptionStatus: "active",
		ManualTierOverride: strPtr("not-a-real-tier"), ManualTierEndsAt: &futureEnd,
	}
	summary, err := svc.GetEntitlementSummary(context.Background(), user)
	require.NoError(t, err, "a malformed override must not 500 GET /households/me/entitlement")
	assert.Equal(t, "premium", summary.Tier, "must fall back to the raw tier")
}

// TestResolveEffectiveEntitlement_CompedOwnerRawInactive_MemberCoveredAtCompTier proves
// the fix for the household-side asymmetry: the owner-lapse gate previously read
// owner.SubscriptionStatus (raw) directly, so a comped household owner whose real
// subscription had lapsed (raw "inactive") was treated as read-only, and every member
// silently fell back to their own tier — even though the owner was just comped to
// household_4. The gate must now read owner.ComputeEffectiveStatus(), so an active comp
// keeps the owner's coverage live for their members. Covers both: (a) the member stays
// covered (Source == household, not Own) and (b) the member resolves to the owner's comp
// tier, not the owner's real (lapsed) tier.
func TestResolveEffectiveEntitlement_CompedOwnerRawInactive_MemberCoveredAtCompTier(t *testing.T) {
	entRepo := &mockEntitlementRepo{}
	entRepo.On("GetActiveEntitlement", mock.Anything, "member-1", mock.Anything).Return(&domain.SubscriptionEntitlement{
		Source: domain.EntitlementSourceHousehold, Tier: "household_2", Status: "active",
		PayerUserID: strPtr("owner-1"), HouseholdID: strPtr("household-1"),
	}, nil)
	userRepo := &mockUserLoader{}
	futureEnd := time.Now().Add(30 * 24 * time.Hour)
	userRepo.On("GetUserByID", mock.Anything, "owner-1").Return(&domain.User{
		ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "inactive", // real subscription lapsed
		ManualTierOverride: strPtr("household_4"), ManualTierEndsAt: &futureEnd, // admin comp, still active
	}, nil)
	svc := NewHouseholdEntitlementService(entRepo, userRepo, &mockHouseholdRepo{}, nil)

	member := &domain.User{ID: "member-1", SubscriptionTier: "premium", SubscriptionStatus: "trial"}
	eff, err := svc.ResolveEffectiveEntitlement(context.Background(), member)
	require.NoError(t, err)
	assert.Equal(t, domain.EntitlementSourceHousehold, eff.Source, "a comped owner's raw-inactive status must not fall the member back to their own tier")
	assert.Equal(t, "household_4", eff.Tier, "member should resolve to the owner's COMP tier, not the owner's real (lapsed) tier")
	// The returned Status must agree with the returned Tier. Reporting "tier household_4,
	// status inactive" is self-contradictory, and a Tier-effective/Status-raw pairing is the
	// exact asymmetry that caused #1413. No consumer reads Status today, which is precisely
	// why it would rot unnoticed until one did.
	assert.Equal(t, "active", eff.Status, "a comped owner's coverage must report an effective status consistent with its effective tier")
}

// ─── Issue #1413 — household-owner mutation bypass: malformed owner override ────────────

// TestResolveEffectiveEntitlement_OwnerMalformedOverride_MemberFallsBackToOwn is the
// regression test for the mutation bypass this task closes: manual_tier_override is an
// unconstrained VARCHAR(50) with no CHECK constraint, so it can hold a value naming no
// configured tier (a legacy row, a direct SQL write, a write made while tierConfig was nil).
// Earlier work on this branch (G2/round-5) stopped such a value from granting write access on
// the AFFECTED USER'S OWN request path (profile_repository.go, profile.go) — but this
// resolver's owner-lapse gate kept reading the bare, unvalidated ComputeEffectiveStatus(), so a
// garbage OWNER override made every MEMBER of the household look covered, even though the
// owner's real (raw) subscription is read-only. With a validator wired that rejects
// "not-a-real-tier", the owner's raw past_due status must win and the member must revert to
// their own tier/source.
func TestResolveEffectiveEntitlement_OwnerMalformedOverride_MemberFallsBackToOwn(t *testing.T) {
	entRepo := &mockEntitlementRepo{}
	entRepo.On("GetActiveEntitlement", mock.Anything, "member-1", mock.Anything).Return(&domain.SubscriptionEntitlement{
		Source: domain.EntitlementSourceHousehold, Tier: "household_2", Status: "active",
		PayerUserID: strPtr("owner-1"), HouseholdID: strPtr("household-1"),
	}, nil)
	userRepo := &mockUserLoader{}
	userRepo.On("GetUserByID", mock.Anything, "owner-1").Return(&domain.User{
		ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "past_due", // owner's REAL billing status is read-only
		ManualTierOverride: strPtr("not-a-real-tier"), // garbage — no CHECK constraint stops this
	}, nil)
	svc := NewHouseholdEntitlementService(entRepo, userRepo, &mockHouseholdRepo{}, nil)
	svc.SetOverrideTierValidator(func(tier string) bool { return tier == "household_2" || tier == "premium" })

	member := &domain.User{ID: "member-1", SubscriptionTier: "premium", SubscriptionStatus: "trial"}
	eff, err := svc.ResolveEffectiveEntitlement(context.Background(), member)
	require.NoError(t, err)
	assert.Equal(t, "premium", eff.Tier, "member should revert to own tier when the owner's override is malformed")
	assert.Equal(t, domain.EntitlementSourceOwn, eff.Source, "a malformed owner override must not confer household coverage on members")
}

// TestResolveEffectiveEntitlement_OwnerValidOverride_MemberStillCovered is the POSITIVE
// CONTROL for the fix above, and the single most important assertion in this test file: with
// the exact same validator wired, a VALID live owner override must still cover the household's
// members. This is the whole point of requirement 5 of this work — an over-tight fix that also
// rejects legitimate comps would lock out paying households, which is worse than the bypass it
// closes.
func TestResolveEffectiveEntitlement_OwnerValidOverride_MemberStillCovered(t *testing.T) {
	entRepo := &mockEntitlementRepo{}
	entRepo.On("GetActiveEntitlement", mock.Anything, "member-1", mock.Anything).Return(&domain.SubscriptionEntitlement{
		Source: domain.EntitlementSourceHousehold, Tier: "household_2", Status: "active",
		PayerUserID: strPtr("owner-1"), HouseholdID: strPtr("household-1"),
	}, nil)
	userRepo := &mockUserLoader{}
	futureEnd := time.Now().Add(30 * 24 * time.Hour)
	userRepo.On("GetUserByID", mock.Anything, "owner-1").Return(&domain.User{
		ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "inactive", // real subscription lapsed
		ManualTierOverride: strPtr("household_2"), ManualTierEndsAt: &futureEnd, // valid, currently-configured comp tier
	}, nil)
	svc := NewHouseholdEntitlementService(entRepo, userRepo, &mockHouseholdRepo{}, nil)
	svc.SetOverrideTierValidator(func(tier string) bool { return tier == "household_2" || tier == "premium" })

	member := &domain.User{ID: "member-1", SubscriptionTier: "premium", SubscriptionStatus: "trial"}
	eff, err := svc.ResolveEffectiveEntitlement(context.Background(), member)
	require.NoError(t, err)
	assert.Equal(t, domain.EntitlementSourceHousehold, eff.Source, "a VALID owner override must still cover members — over-tightening here would lock out paying households")
	assert.Equal(t, "household_2", eff.Tier)
	assert.Equal(t, "active", eff.Status)
}

// TestResolveEffectiveEntitlement_NilValidator_MalformedOverride_PreFixBehaviorPreserved pins
// the documented nil-validator semantics (see SetOverrideTierValidator's doc comment): if the
// validator is never wired, a garbage owner override must still cover members exactly as it
// did before this fix. main.go os.Exit(1)s at startup if the tier config fails to load, so
// failing closed on a nil validator would turn a future wiring oversight into an instant
// outage for every currently-comped household, not just a silent regression.
func TestResolveEffectiveEntitlement_NilValidator_MalformedOverride_PreFixBehaviorPreserved(t *testing.T) {
	entRepo := &mockEntitlementRepo{}
	entRepo.On("GetActiveEntitlement", mock.Anything, "member-1", mock.Anything).Return(&domain.SubscriptionEntitlement{
		Source: domain.EntitlementSourceHousehold, Tier: "household_2", Status: "active",
		PayerUserID: strPtr("owner-1"), HouseholdID: strPtr("household-1"),
	}, nil)
	userRepo := &mockUserLoader{}
	userRepo.On("GetUserByID", mock.Anything, "owner-1").Return(&domain.User{
		ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "past_due",
		ManualTierOverride: strPtr("not-a-real-tier"),
	}, nil)
	svc := NewHouseholdEntitlementService(entRepo, userRepo, &mockHouseholdRepo{}, nil) // SetOverrideTierValidator never called

	member := &domain.User{ID: "member-1", SubscriptionTier: "premium", SubscriptionStatus: "trial"}
	eff, err := svc.ResolveEffectiveEntitlement(context.Background(), member)
	require.NoError(t, err)
	assert.Equal(t, domain.EntitlementSourceHousehold, eff.Source, "nil validator must skip validation, not deny — see SetOverrideTierValidator doc comment")
}

// TestResolveEffectiveEntitlement_OwnerOverrideExpired_MemberFallsBackToOwnTier proves an
// owner's EXPIRED comp does not leak coverage: once ManualTierEndsAt is in the past,
// ComputeEffectiveStatus() returns the owner's raw (read-only) status unchanged, so the
// owner-lapse gate correctly falls the member back to their own tier — exactly as it would
// for an owner who was never comped at all. See this test's accompanying revert-log entry:
// because an EXPIRED override makes ComputeEffectiveStatus() mathematically identical to
// the raw SubscriptionStatus, this scenario cannot be flipped by reverting the raw-vs-
// effective status site alone — it instead guards against a DIFFERENT plausible bug: an
// implementation that checks override PRESENCE (ManualTierOverride != nil) rather than
// override LIVENESS (HasActiveManualOverride/ComputeEffectiveStatus), which would
// incorrectly keep an expired comp's coverage alive.
func TestResolveEffectiveEntitlement_OwnerOverrideExpired_MemberFallsBackToOwnTier(t *testing.T) {
	entRepo := &mockEntitlementRepo{}
	entRepo.On("GetActiveEntitlement", mock.Anything, "member-1", mock.Anything).Return(&domain.SubscriptionEntitlement{
		Source: domain.EntitlementSourceHousehold, Tier: "household_2", Status: "active",
		PayerUserID: strPtr("owner-1"), HouseholdID: strPtr("household-1"),
	}, nil)
	userRepo := &mockUserLoader{}
	pastEnd := time.Now().Add(-24 * time.Hour)
	userRepo.On("GetUserByID", mock.Anything, "owner-1").Return(&domain.User{
		ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "inactive",
		ManualTierOverride: strPtr("household_4"), ManualTierEndsAt: &pastEnd, // comp EXPIRED
	}, nil)
	svc := NewHouseholdEntitlementService(entRepo, userRepo, &mockHouseholdRepo{}, nil)

	member := &domain.User{ID: "member-1", SubscriptionTier: "premium", SubscriptionStatus: "trial"}
	eff, err := svc.ResolveEffectiveEntitlement(context.Background(), member)
	require.NoError(t, err)
	assert.Equal(t, "premium", eff.Tier, "member should revert to own tier once the owner's comp has expired")
	assert.Equal(t, domain.EntitlementSourceOwn, eff.Source)
}

// TestResolveEffectiveEntitlement_MemberManualOverride_LosesToActiveHouseholdCoverage_StatusLevel
// is the status-level sibling of
// TestResolveEffectiveEntitlement_MemberManualOverride_LosesToActiveHouseholdCoverage: this
// task introduces effective STATUS into the resolver for the first time (previously only
// Tier used ComputeEffectiveTier()), so the ratified precedence rule (Gap Analysis Round 2,
// GT-2, #1367 — household coverage always wins over a covered member's own
// manual_tier_override while the owner is active) must still hold for Status too. A
// member's own comp (raw "inactive" + active manual override -> effective "active") must
// NOT leak into the returned EffectiveEntitlement while active household coverage applies —
// both Tier AND Status must reflect the OWNER's household coverage, never the member's own
// computed values.
func TestResolveEffectiveEntitlement_MemberManualOverride_LosesToActiveHouseholdCoverage_StatusLevel(t *testing.T) {
	entRepo := &mockEntitlementRepo{}
	entRepo.On("GetActiveEntitlement", mock.Anything, "member-1", mock.Anything).Return(&domain.SubscriptionEntitlement{
		Source: domain.EntitlementSourceHousehold, Tier: "household_2", Status: "trialing",
		PayerUserID: strPtr("owner-1"), HouseholdID: strPtr("household-1"),
	}, nil)
	userRepo := &mockUserLoader{}
	userRepo.On("GetUserByID", mock.Anything, "owner-1").Return(&domain.User{
		ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "trialing", // owner genuinely active (not comped)
	}, nil)
	svc := NewHouseholdEntitlementService(entRepo, userRepo, &mockHouseholdRepo{}, nil)

	futureEnd := time.Now().Add(30 * 24 * time.Hour)
	member := &domain.User{
		ID: "member-1", SubscriptionTier: "premium", SubscriptionStatus: "inactive",
		ManualTierOverride: strPtr("household_5"), ManualTierEndsAt: &futureEnd, // member's OWN comp — must NOT win here
	}
	eff, err := svc.ResolveEffectiveEntitlement(context.Background(), member)
	require.NoError(t, err)
	assert.Equal(t, "household_2", eff.Tier, "owner coverage wins over the member's own comp tier while the owner is active")
	assert.Equal(t, "trialing", eff.Status, "owner coverage wins over the member's own comped ('active') status while the owner is active")
	assert.Equal(t, domain.EntitlementSourceHousehold, eff.Source)
}

func TestResolveEffectiveEntitlement_RepositoryError_Propagates(t *testing.T) {
	entRepo := &mockEntitlementRepo{}
	entRepo.On("GetActiveEntitlement", mock.Anything, "user-1", mock.Anything).Return(nil, errors.New("db timeout"))
	svc := NewHouseholdEntitlementService(entRepo, &mockUserLoader{}, &mockHouseholdRepo{}, nil)

	user := &domain.User{ID: "user-1", SubscriptionTier: "premium", SubscriptionStatus: "active"}
	_, err := svc.ResolveEffectiveEntitlement(context.Background(), user)
	require.Error(t, err, "real DB errors must propagate so the caller can decide fail-open policy")
}

func TestResolveEffectiveEntitlement_OwnerLookupError_Propagates(t *testing.T) {
	entRepo := &mockEntitlementRepo{}
	entRepo.On("GetActiveEntitlement", mock.Anything, "member-1", mock.Anything).Return(&domain.SubscriptionEntitlement{
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
	entRepo.On("GetActiveEntitlement", mock.Anything, "user-1", mock.Anything).Return(nil, repository.ErrEntitlementNotFound)
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
	entRepo.On("GetActiveEntitlement", mock.Anything, "owner-1", mock.Anything).Return(nil, repository.ErrEntitlementNotFound)
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
	entRepo.On("GetActiveEntitlement", mock.Anything, "member-1", mock.Anything).Return(&domain.SubscriptionEntitlement{
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
	entRepo.On("GetActiveEntitlement", mock.Anything, "member-1", mock.Anything).Return(&domain.SubscriptionEntitlement{
		Source: domain.EntitlementSourceHousehold, PayerUserID: &payerID,
	}, nil)

	svc := NewHouseholdEntitlementService(entRepo, userRepo, &mockHouseholdRepo{}, tierConfig)

	covered, err := svc.IsCoveredByHousehold(context.Background(), "member-1")
	require.NoError(t, err)
	assert.False(t, covered, "member whose owner lost active coverage should not be treated as covered")
}

// TestIsCoveredByHousehold_OwnerMalformedOverride_False is the IsCoveredByHousehold-level
// sibling of TestResolveEffectiveEntitlement_OwnerMalformedOverride_MemberFallsBackToOwn —
// this is the exact boolean the read-only middleware (read_only.go) consults to decide
// whether to permit a member's mutation, so pinning it directly here is closer to the real
// bypass than asserting on ResolveEffectiveEntitlement's Source field alone.
func TestIsCoveredByHousehold_OwnerMalformedOverride_False(t *testing.T) {
	member := &domain.User{ID: "member-1", SubscriptionTier: "premium", SubscriptionStatus: "inactive"}
	owner := &domain.User{
		ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "past_due",
		ManualTierOverride: strPtr("not-a-real-tier"),
	}

	userRepo := &mockUserLoader{}
	userRepo.On("GetUserByID", mock.Anything, "member-1").Return(member, nil)
	userRepo.On("GetUserByID", mock.Anything, "owner-1").Return(owner, nil)

	entRepo := &mockEntitlementRepo{}
	payerID := "owner-1"
	entRepo.On("GetActiveEntitlement", mock.Anything, "member-1", mock.Anything).Return(&domain.SubscriptionEntitlement{
		Source: domain.EntitlementSourceHousehold, PayerUserID: &payerID,
	}, nil)

	svc := NewHouseholdEntitlementService(entRepo, userRepo, &mockHouseholdRepo{}, nil)
	svc.SetOverrideTierValidator(func(tier string) bool { return tier == "household_2" })

	covered, err := svc.IsCoveredByHousehold(context.Background(), "member-1")
	require.NoError(t, err)
	assert.False(t, covered, "a malformed owner override must not confer household coverage — closes the #1413 owner mutation bypass")
}

// TestIsCoveredByHousehold_OwnerValidOverride_True is the IsCoveredByHousehold-level positive
// control (requirement 5): the SAME validator, with a VALID live owner override, must still
// report the member as covered.
func TestIsCoveredByHousehold_OwnerValidOverride_True(t *testing.T) {
	member := &domain.User{ID: "member-1", SubscriptionTier: "premium", SubscriptionStatus: "inactive"}
	futureEnd := time.Now().Add(30 * 24 * time.Hour)
	owner := &domain.User{
		ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "inactive",
		ManualTierOverride: strPtr("household_2"), ManualTierEndsAt: &futureEnd,
	}

	userRepo := &mockUserLoader{}
	userRepo.On("GetUserByID", mock.Anything, "member-1").Return(member, nil)
	userRepo.On("GetUserByID", mock.Anything, "owner-1").Return(owner, nil)

	entRepo := &mockEntitlementRepo{}
	payerID := "owner-1"
	entRepo.On("GetActiveEntitlement", mock.Anything, "member-1", mock.Anything).Return(&domain.SubscriptionEntitlement{
		Source: domain.EntitlementSourceHousehold, PayerUserID: &payerID,
	}, nil)

	svc := NewHouseholdEntitlementService(entRepo, userRepo, &mockHouseholdRepo{}, nil)
	svc.SetOverrideTierValidator(func(tier string) bool { return tier == "household_2" })

	covered, err := svc.IsCoveredByHousehold(context.Background(), "member-1")
	require.NoError(t, err)
	assert.True(t, covered, "a VALID owner override must still cover members — positive control per requirement 5")
}

func TestIsCoveredByHousehold_SoloUser_False(t *testing.T) {
	tierConfig := testTierConfig(t)
	solo := &domain.User{ID: "user-1", SubscriptionTier: "premium", SubscriptionStatus: "inactive"}

	userRepo := &mockUserLoader{}
	userRepo.On("GetUserByID", mock.Anything, "user-1").Return(solo, nil)

	entRepo := &mockEntitlementRepo{}
	entRepo.On("GetActiveEntitlement", mock.Anything, "user-1", mock.Anything).Return(nil, repository.ErrEntitlementNotFound)

	svc := NewHouseholdEntitlementService(entRepo, userRepo, &mockHouseholdRepo{}, tierConfig)

	covered, err := svc.IsCoveredByHousehold(context.Background(), "user-1")
	require.NoError(t, err)
	assert.False(t, covered)
}
