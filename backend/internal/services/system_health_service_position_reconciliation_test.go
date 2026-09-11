package services

// #1631 M8b I1: the position-reconciliation drift gauge on /admin/health.
//
// Before this fix, GetPositionReconciliation had zero production callers — a diagnostic built
// but reported to nobody. This wires a GLOBAL summary (every user's active holdings, counts only)
// onto /admin/health, mirroring the forked-identity section.
//
// ⚠ WHAT THESE TESTS DELIBERATELY DO NOT DO: assert that the response HAS a
// position_reconciliation field. That passes against a hardcoded zero — the same vacuous-gate
// species every other health-gauge test file in this package guards against.
//
// ⚠ DriftCount is NOT a confirmed-problem count — see domain.PositionReconciliationReport's
// "NOT YET FULLY ACTIONABLE" section. These tests only pin that the repository's classification
// reaches the surface intact, not that a non-zero DriftCount means something is broken.

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// MockPositionReconciliationSummarizer mocks the one-verb read the health derivation needs.
// Narrow on purpose, matching every other narrowed-interface mock in this package.
type MockPositionReconciliationSummarizer struct {
	mock.Mock
}

func (m *MockPositionReconciliationSummarizer) PositionReconciliationSummary(ctx context.Context) (*domain.PositionReconciliationSummary, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.PositionReconciliationSummary), args.Error(1)
}

// newPositionReconciliationHealthService builds the service with only the collaborators
// /admin/health needs, matching the construction style of the other gauge test files in this
// package (e.g. newResidueHealthService, newForkedIdentityHealthService).
func newPositionReconciliationHealthService(t *testing.T, repo *MockPositionReconciliationSummarizer) SystemHealthService {
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
	// Assigned only when non-nil — a typed-nil *MockPositionReconciliationSummarizer assigned
	// unconditionally would produce a non-nil interface holding a nil pointer, sailing past the
	// `== nil` guard the unwired-repo test below depends on.
	if repo != nil {
		svc.positionReconciliationRepo = repo
	}
	return svc
}

// TestGetSystemHealth_PositionReconciliation_SeededSummaryIsVisible is the RED test: a
// GetPositionReconciliation caller finally exists — this gauge — where before this fix the
// diagnostic had zero production callers at all.
func TestGetSystemHealth_PositionReconciliation_SeededSummaryIsVisible(t *testing.T) {
	repo := new(MockPositionReconciliationSummarizer)
	seeded := &domain.PositionReconciliationSummary{
		DriftCount:               3,
		SuppressedPreWindowCount: 40,
		TotalPositionsChecked:    120,
		Available:                true,
	}
	repo.On("PositionReconciliationSummary", mock.Anything).Return(seeded, nil)

	result, err := newPositionReconciliationHealthService(t, repo).GetSystemHealth(context.Background())

	require.NoError(t, err)
	assert.Equal(t, int64(3), result.PositionReconciliation.DriftCount)
	assert.Equal(t, int64(40), result.PositionReconciliation.SuppressedPreWindowCount)
	assert.Equal(t, int64(120), result.PositionReconciliation.TotalPositionsChecked)
	assert.True(t, result.PositionReconciliation.Available,
		"a successful query is a verified reading, not an unknown")
	repo.AssertExpectations(t)
}

// TestGetSystemHealth_PositionReconciliation_NoDriftReportsAVerifiedZero is the mandatory
// sibling: without it, the seeded test above could pass against hardcoded constants.
func TestGetSystemHealth_PositionReconciliation_NoDriftReportsAVerifiedZero(t *testing.T) {
	repo := new(MockPositionReconciliationSummarizer)
	repo.On("PositionReconciliationSummary", mock.Anything).Return(&domain.PositionReconciliationSummary{Available: true}, nil)

	result, err := newPositionReconciliationHealthService(t, repo).GetSystemHealth(context.Background())

	require.NoError(t, err)
	assert.Equal(t, int64(0), result.PositionReconciliation.DriftCount)
	assert.True(t, result.PositionReconciliation.Available,
		"a genuine zero must never be confused with an unavailable reading")
	repo.AssertExpectations(t)
}

// TestGetSystemHealth_PositionReconciliation_QueryError_IsUnavailableNotAConfidentZero pins the
// same "unknown is not zero" rule every other gauge in this package enforces.
func TestGetSystemHealth_PositionReconciliation_QueryError_IsUnavailableNotAConfidentZero(t *testing.T) {
	repo := new(MockPositionReconciliationSummarizer)
	repo.On("PositionReconciliationSummary", mock.Anything).Return(nil, errors.New("db unavailable"))

	result, err := newPositionReconciliationHealthService(t, repo).GetSystemHealth(context.Background())

	require.NoError(t, err, "one failed gauge must not fail the whole health response")
	assert.False(t, result.PositionReconciliation.Available,
		"a failed query has no number to report; reporting zero would be a false all-clear")
	assert.Equal(t, int64(0), result.PositionReconciliation.DriftCount)
	repo.AssertExpectations(t)
}

// TestGetSystemHealth_PositionReconciliation_UnwiredRepo_IsUnavailableNotAConfidentZero closes
// the same gap for a nil repository. Fail-closed: unwired reads as unknown.
func TestGetSystemHealth_PositionReconciliation_UnwiredRepo_IsUnavailableNotAConfidentZero(t *testing.T) {
	result, err := newPositionReconciliationHealthService(t, nil).GetSystemHealth(context.Background())

	require.NoError(t, err)
	assert.False(t, result.PositionReconciliation.Available,
		"nobody wired the repository, so nothing has been verified")
	assert.Equal(t, int64(0), result.PositionReconciliation.DriftCount)
}
