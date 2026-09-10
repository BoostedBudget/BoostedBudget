package services

// #1631 M8b: the forked-security-identity drift count on /admin/health.
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

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockForkedSecurityIdentityCounter mocks the one-verb read the health derivation needs. Narrow
// on purpose, matching MockPlaidOrphanedItemLister and the other narrowed-interface mocks in this
// package: a read-only health surface must not gain write access to `securities` just because it
// needs a count.
type MockForkedSecurityIdentityCounter struct {
	mock.Mock
}

func (m *MockForkedSecurityIdentityCounter) CountForkedIdentityGroups(ctx context.Context) (int64, error) {
	args := m.Called(ctx)
	return args.Get(0).(int64), args.Error(1)
}

// newForkedIdentityHealthService builds the service with only the collaborators /admin/health
// needs, matching the construction style of the other gauge test files in this package (e.g.
// newResidueHealthService).
func newForkedIdentityHealthService(t *testing.T, repo *MockForkedSecurityIdentityCounter) SystemHealthService {
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
	// *MockForkedSecurityIdentityCounter assigned unconditionally would produce a non-nil
	// interface holding a nil pointer, sailing past the `== nil` guard the unwired-repo test below
	// depends on.
	if repo != nil {
		svc.forkedIdentityRepo = repo
	}
	return svc
}

// TestGetSystemHealth_ForkedSecurityIdentities_SeededGroupsAreVisible is the RED test: a forked
// identity group reaches an operator, where before this task nothing anywhere counted them.
func TestGetSystemHealth_ForkedSecurityIdentities_SeededGroupsAreVisible(t *testing.T) {
	repo := new(MockForkedSecurityIdentityCounter)
	repo.On("CountForkedIdentityGroups", mock.Anything).Return(int64(2), nil)

	result, err := newForkedIdentityHealthService(t, repo).GetSystemHealth(context.Background())

	require.NoError(t, err)
	assert.Equal(t, int64(2), result.ForkedSecurityIdentities.GroupCount,
		"two forked-identity groups (e.g. AGG and IEFA/IEMG/VCIT-shaped forks) must reach the operator surface intact")
	assert.True(t, result.ForkedSecurityIdentities.Available,
		"a successful query is a verified reading, not an unknown")
	repo.AssertExpectations(t)
}

// TestGetSystemHealth_ForkedSecurityIdentities_NoForksReportsAVerifiedZero is the mandatory
// sibling: without it, the seeded-2 test above could pass against a hardcoded "2" constant.
func TestGetSystemHealth_ForkedSecurityIdentities_NoForksReportsAVerifiedZero(t *testing.T) {
	repo := new(MockForkedSecurityIdentityCounter)
	repo.On("CountForkedIdentityGroups", mock.Anything).Return(int64(0), nil)

	result, err := newForkedIdentityHealthService(t, repo).GetSystemHealth(context.Background())

	require.NoError(t, err)
	assert.Equal(t, int64(0), result.ForkedSecurityIdentities.GroupCount)
	assert.True(t, result.ForkedSecurityIdentities.Available,
		"a genuine zero must never be confused with an unavailable reading")
	repo.AssertExpectations(t)
}

// TestGetSystemHealth_ForkedSecurityIdentities_QueryError_IsUnavailableNotAConfidentZero pins the
// same "unknown is not zero" rule every other gauge in this package enforces.
func TestGetSystemHealth_ForkedSecurityIdentities_QueryError_IsUnavailableNotAConfidentZero(t *testing.T) {
	repo := new(MockForkedSecurityIdentityCounter)
	repo.On("CountForkedIdentityGroups", mock.Anything).Return(int64(0), errors.New("db unavailable"))

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
