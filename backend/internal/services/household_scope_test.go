package services

import (
	"context"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// ─── #1369 — Phase 5: household aggregation scope resolution ────────────────
//
// ActiveMemberUserIDs and ResolveScope are the service-layer primitives Phase 5
// uses to turn a `scope=household` request into a validated domain.HouseholdScope
// (household ID + active member user IDs), so repositories never need to trust a
// caller-supplied scope directly (Codex adversarial review, P2 finding).

func TestHouseholdService_ActiveMemberUserIDs_ReturnsActiveMemberIDs(t *testing.T) {
	t.Parallel()
	hhRepo := &mockHouseholdRepo{}
	hhRepo.On("ListMembers", mock.Anything, "hh-1").Return([]domain.HouseholdMember{
		{UserID: "owner-1", Role: domain.HouseholdRoleOwner, Status: domain.HouseholdMemberStatusActive},
		{UserID: "member-2", Role: domain.HouseholdRoleMember, Status: domain.HouseholdMemberStatusActive},
	}, nil)
	svc := NewHouseholdService(nil, hhRepo, &mockHouseholdInviteRepo{}, &mockHouseholdShareRepo{},
		&mockEntitlementRepo{}, nil, &mockUserLoader{}, testTierConfig(t), &MockEmailService{}, nil)

	ids, err := svc.ActiveMemberUserIDs(context.Background(), "hh-1")
	require.NoError(t, err)
	require.Equal(t, []string{"owner-1", "member-2"}, ids)
}

func TestHouseholdService_ResolveScope_EmptyScopeParam_ReturnsNilScope(t *testing.T) {
	t.Parallel()
	svc := NewHouseholdService(nil, &mockHouseholdRepo{}, &mockHouseholdInviteRepo{}, &mockHouseholdShareRepo{},
		&mockEntitlementRepo{}, nil, &mockUserLoader{}, testTierConfig(t), &MockEmailService{}, nil)

	scope, err := svc.ResolveScope(context.Background(), "user-1", "")
	require.NoError(t, err)
	require.Nil(t, scope)
}

func TestHouseholdService_ResolveScope_Personal_ReturnsNilScope(t *testing.T) {
	t.Parallel()
	svc := NewHouseholdService(nil, &mockHouseholdRepo{}, &mockHouseholdInviteRepo{}, &mockHouseholdShareRepo{},
		&mockEntitlementRepo{}, nil, &mockUserLoader{}, testTierConfig(t), &MockEmailService{}, nil)

	scope, err := svc.ResolveScope(context.Background(), "user-1", "personal")
	require.NoError(t, err)
	require.Nil(t, scope)
}

func TestHouseholdService_ResolveScope_Household_NotAMember_ReturnsErrHouseholdNotFound(t *testing.T) {
	t.Parallel()
	hhRepo := &mockHouseholdRepo{}
	hhRepo.On("GetHouseholdForUser", mock.Anything, "user-1").Return(nil, false, false, nil)
	svc := NewHouseholdService(nil, hhRepo, &mockHouseholdInviteRepo{}, &mockHouseholdShareRepo{},
		&mockEntitlementRepo{}, nil, &mockUserLoader{}, testTierConfig(t), &MockEmailService{}, nil)

	scope, err := svc.ResolveScope(context.Background(), "user-1", "household")
	require.ErrorIs(t, err, domain.ErrHouseholdNotFound)
	require.Nil(t, scope)
}

func TestHouseholdService_ResolveScope_Household_ResolvesActiveMembers(t *testing.T) {
	t.Parallel()
	hh := &domain.Household{ID: "hh-1", OwnerUserID: "owner-1"}
	hhRepo := &mockHouseholdRepo{}
	hhRepo.On("GetHouseholdForUser", mock.Anything, "member-2").Return(hh, false, true, nil)
	hhRepo.On("ListMembers", mock.Anything, "hh-1").Return([]domain.HouseholdMember{
		{UserID: "owner-1", Role: domain.HouseholdRoleOwner, Status: domain.HouseholdMemberStatusActive},
		{UserID: "member-2", Role: domain.HouseholdRoleMember, Status: domain.HouseholdMemberStatusActive},
	}, nil)
	svc := NewHouseholdService(nil, hhRepo, &mockHouseholdInviteRepo{}, &mockHouseholdShareRepo{},
		&mockEntitlementRepo{}, nil, &mockUserLoader{}, testTierConfig(t), &MockEmailService{}, nil)

	scope, err := svc.ResolveScope(context.Background(), "member-2", "household")
	require.NoError(t, err)
	require.NotNil(t, scope)
	require.Equal(t, "hh-1", scope.HouseholdID)
	require.Equal(t, []string{"owner-1", "member-2"}, scope.MemberUserIDs)
}
