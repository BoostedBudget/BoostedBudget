package services

// #1631 M8b: the forked-security-identity section on /admin/health.
//
// A group is TWO OR MORE `securities` rows sharing a non-blank institution_security_id under
// DIFFERENT plaid_security_id values — the shape a Plaid corporate action reissuing
// plaid_security_id produces (see domain.ForkedSecurityIdentity's doc comment). Detection only:
// nothing here merges anything.
//
// ⚠ WHAT THESE TESTS DELIBERATELY DO NOT DO: assert that the response HAS a
// forked_security_identities field. That passes against a hardcoded zero — the same vacuous-gate
// species every other health-gauge test file in this package guards against. The seeded count
// below is non-zero specifically so a constant cannot satisfy it.
//
// ⚠ #1631 M8b I2 fix: this reads FindForkedIdentities (the full group listing), not
// CountForkedIdentityGroups — a bare count told an operator a fork existed with no way to find
// out which securities forked. TestGetSystemHealth_ForkedSecurityIdentities_SeededGroupsAreVisible
// pins that the MEMBER DETAIL, not just the count, reaches the response.

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// MockForkedSecurityIdentityLister mocks the one-verb read the health derivation needs. Narrow
// on purpose, matching MockPlaidOrphanedItemLister and the other narrowed-interface mocks in this
// package: a read-only health surface must not gain write access to `securities` just because it
// needs the fork listing.
type MockForkedSecurityIdentityLister struct {
	mock.Mock
}

func (m *MockForkedSecurityIdentityLister) FindForkedIdentities(ctx context.Context) ([]domain.ForkedSecurityIdentity, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.ForkedSecurityIdentity), args.Error(1)
}

// newForkedIdentityHealthService builds the service with only the collaborators /admin/health
// needs, matching the construction style of the other gauge test files in this package (e.g.
// newResidueHealthService).
func newForkedIdentityHealthService(t *testing.T, repo *MockForkedSecurityIdentityLister) SystemHealthService {
	t.Helper()
	mockHealthRepo := new(MockSystemHealthRepository)
	mockDeadLetterRepo := new(MockDeadLetterJobRepository)
	mockPool := new(MockPool)
	setupFullHealthMocks(mockHealthRepo, mockDeadLetterRepo, mockPool, 0, 0, 0)
	svc := &systemHealthService{
		healthRepo:     mockHealthRepo,
		deadLetterRepo: mockDeadLetterRepo,
		pool:           mockPool,
	}
	// Assigned only when non-nil, matching newResidueHealthService: a typed-nil
	// *MockForkedSecurityIdentityLister assigned unconditionally would produce a non-nil
	// interface holding a nil pointer, sailing past the `== nil` guard the unwired-repo test below
	// depends on.
	if repo != nil {
		svc.forkedIdentityRepo = repo
	}
	return svc
}

// TestGetSystemHealth_ForkedSecurityIdentities_SeededGroupsAreVisible is the RED test: forked
// identity groups AND their member detail reach an operator, where before this task nothing
// anywhere counted them (and the count-only precursor of this gauge gave an operator no way to
// find out which securities forked).
func TestGetSystemHealth_ForkedSecurityIdentities_SeededGroupsAreVisible(t *testing.T) {
	repo := new(MockForkedSecurityIdentityLister)
	ticker := "AGG"
	groups := []domain.ForkedSecurityIdentity{
		{
			InstitutionSecurityID: "inst-agg",
			Members: []domain.ForkedSecurityIdentityMember{
				{SecurityID: "sec-1", PlaidSecurityID: "plaid-agg-old", Name: "iShares Core U.S. Aggregate Bond ETF", TickerSymbol: &ticker},
				{SecurityID: "sec-2", PlaidSecurityID: "plaid-agg-new", Name: "iShares Core U.S. Aggregate Bond ETF", TickerSymbol: &ticker},
			},
		},
		{
			InstitutionSecurityID: "inst-iefa",
			Members: []domain.ForkedSecurityIdentityMember{
				{SecurityID: "sec-3", PlaidSecurityID: "plaid-iefa"},
				{SecurityID: "sec-4", PlaidSecurityID: "plaid-iemg"},
			},
		},
	}
	repo.On("FindForkedIdentities", mock.Anything).Return(groups, nil)

	result, err := newForkedIdentityHealthService(t, repo).GetSystemHealth(context.Background())

	require.NoError(t, err)
	assert.Equal(t, int64(2), result.ForkedSecurityIdentities.GroupCount,
		"two forked-identity groups (e.g. AGG and IEFA/IEMG-shaped forks) must reach the operator surface intact")
	require.Len(t, result.ForkedSecurityIdentities.Groups, 2,
		"an operator must be able to see WHICH securities forked, not just how many groups exist")
	assert.Equal(t, "plaid-agg-old", result.ForkedSecurityIdentities.Groups[0].Members[0].PlaidSecurityID)
	assert.Equal(t, "plaid-agg-new", result.ForkedSecurityIdentities.Groups[0].Members[1].PlaidSecurityID)
	assert.True(t, result.ForkedSecurityIdentities.Available,
		"a successful query is a verified reading, not an unknown")
	repo.AssertExpectations(t)
}

// TestGetSystemHealth_ForkedSecurityIdentities_NoForksReportsAVerifiedZero is the mandatory
// sibling: without it, the seeded-2 test above could pass against a hardcoded "2" constant.
func TestGetSystemHealth_ForkedSecurityIdentities_NoForksReportsAVerifiedZero(t *testing.T) {
	repo := new(MockForkedSecurityIdentityLister)
	repo.On("FindForkedIdentities", mock.Anything).Return([]domain.ForkedSecurityIdentity{}, nil)

	result, err := newForkedIdentityHealthService(t, repo).GetSystemHealth(context.Background())

	require.NoError(t, err)
	assert.Equal(t, int64(0), result.ForkedSecurityIdentities.GroupCount)
	assert.Empty(t, result.ForkedSecurityIdentities.Groups)
	assert.True(t, result.ForkedSecurityIdentities.Available,
		"a genuine zero must never be confused with an unavailable reading")
	repo.AssertExpectations(t)
}

// TestGetSystemHealth_ForkedSecurityIdentities_QueryError_IsUnavailableNotAConfidentZero pins the
// same "unknown is not zero" rule every other gauge in this package enforces.
func TestGetSystemHealth_ForkedSecurityIdentities_QueryError_IsUnavailableNotAConfidentZero(t *testing.T) {
	repo := new(MockForkedSecurityIdentityLister)
	repo.On("FindForkedIdentities", mock.Anything).Return(nil, errors.New("db unavailable"))

	result, err := newForkedIdentityHealthService(t, repo).GetSystemHealth(context.Background())

	require.NoError(t, err, "one failed gauge must not fail the whole health response")
	assert.False(t, result.ForkedSecurityIdentities.Available,
		"a failed query has no number to report; reporting zero would be a false all-clear")
	assert.Equal(t, int64(0), result.ForkedSecurityIdentities.GroupCount)
	repo.AssertExpectations(t)
}

// TestGetSystemHealth_ForkedSecurityIdentities_UnwiredRepo_IsUnavailableNotAConfidentZero closes
// the same gap for a nil repository (an older test construction, or a future main.go wiring
// mistake). Fail-closed: unwired reads as unknown.
func TestGetSystemHealth_ForkedSecurityIdentities_UnwiredRepo_IsUnavailableNotAConfidentZero(t *testing.T) {
	result, err := newForkedIdentityHealthService(t, nil).GetSystemHealth(context.Background())

	require.NoError(t, err)
	assert.False(t, result.ForkedSecurityIdentities.Available,
		"nobody wired the repository, so nothing has been verified")
	assert.Equal(t, int64(0), result.ForkedSecurityIdentities.GroupCount)
}
