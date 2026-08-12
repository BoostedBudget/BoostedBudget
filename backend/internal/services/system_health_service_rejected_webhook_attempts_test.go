package services

// Issue #1514, requirement #5 (Phase 2 gap fix): restores operator visibility into
// invalid_webhook_attempts — the audit signal for rejected (unsigned or invalid-signature)
// webhook deliveries, which Phase 1 made write-only. See domain.RejectedWebhookAttemptMetrics for
// what this measures and, critically, how it differs from domain.PaddleStatusMetrics.FailedWebhooks24h
// (processing failures of ALREADY-VERIFIED webhooks — an unrelated failure mode).
//
// ⚠ WHAT THESE TESTS DELIBERATELY DO NOT DO: assert that the response HAS a
// rejected_webhook_attempts_24h field. That passes against a hardcoded zero. Every number
// asserted below is seeded through the mock repository, and the mandatory zero-state sibling
// proves a real "nothing happened" reads as an available, verified zero rather than either an
// unavailable gap or a copy-pasted non-zero constant.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockInvalidWebhookAttemptStatsReader mocks services.InvalidWebhookAttemptStatsReader — the
// narrowed (read-only) view of repository.InvalidWebhookAttemptRepository this service depends on.
type MockInvalidWebhookAttemptStatsReader struct {
	mock.Mock
}

func (m *MockInvalidWebhookAttemptStatsReader) GetStats24h(ctx context.Context) (map[string]int64, *time.Time, error) {
	args := m.Called(ctx)
	var byProvider map[string]int64
	if args.Get(0) != nil {
		byProvider = args.Get(0).(map[string]int64)
	}
	var mostRecentAt *time.Time
	if args.Get(1) != nil {
		mostRecentAt = args.Get(1).(*time.Time)
	}
	return byProvider, mostRecentAt, args.Error(2)
}

// newRejectedWebhookAttemptsHealthService builds the service with only the collaborators
// /admin/health needs for this section, matching the construction style of the sibling Issue
// #1458/#1497/#1504 health tests (system_health_service_deletion_residue_test.go).
func newRejectedWebhookAttemptsHealthService(t *testing.T, repo *MockInvalidWebhookAttemptStatsReader) SystemHealthService {
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
	// Assigned only when non-nil: assigning a typed-nil *MockInvalidWebhookAttemptStatsReader would
	// produce a non-nil interface holding a nil pointer, sailing past the service's `== nil` guard
	// and panicking on first use instead of exercising the unwired-repo fail-closed path.
	if repo != nil {
		svc.invalidWebhookAttemptRepo = repo
	}
	return svc
}

// TestGetSystemHealth_RejectedWebhookAttempts_SeededFloodIsVisible is the RED test for requirement
// #5: a forged-webhook flood recorded by InvalidWebhookAttemptRepository.Record must reach an
// operator SOMEWHERE. Before this method existed, GetStats24h did not exist on the interface at
// all and this assertion could not even compile against pre-fix code.
func TestGetSystemHealth_RejectedWebhookAttempts_SeededFloodIsVisible(t *testing.T) {
	repo := new(MockInvalidWebhookAttemptStatsReader)
	latest := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	repo.On("GetStats24h", mock.Anything).
		Return(map[string]int64{"paddle": 40, "plaid": 5}, &latest, nil)

	result, err := newRejectedWebhookAttemptsHealthService(t, repo).GetSystemHealth(context.Background())

	require.NoError(t, err)
	assert.Equal(t, int64(45), result.RejectedWebhookAttempts24h.Total24h,
		"the total must be the sum across every provider, not just one")
	assert.Equal(t, map[string]int64{"paddle": 40, "plaid": 5}, result.RejectedWebhookAttempts24h.ByProvider,
		"the per-provider breakdown must survive intact — an operator needs to know WHICH provider is under attack")
	require.NotNil(t, result.RejectedWebhookAttempts24h.MostRecentAt)
	assert.True(t, result.RejectedWebhookAttempts24h.MostRecentAt.Equal(latest))
	assert.True(t, result.RejectedWebhookAttempts24h.Available)
	repo.AssertExpectations(t)
}

// TestGetSystemHealth_RejectedWebhookAttempts_NoAttemptsReportsAVerifiedZero is the MANDATORY
// sibling: without it, a hardcoded non-zero constant could satisfy the test above. A quiet 24
// hours must read as an available, genuine zero.
func TestGetSystemHealth_RejectedWebhookAttempts_NoAttemptsReportsAVerifiedZero(t *testing.T) {
	repo := new(MockInvalidWebhookAttemptStatsReader)
	repo.On("GetStats24h", mock.Anything).Return(map[string]int64{}, (*time.Time)(nil), nil)

	result, err := newRejectedWebhookAttemptsHealthService(t, repo).GetSystemHealth(context.Background())

	require.NoError(t, err)
	assert.Equal(t, int64(0), result.RejectedWebhookAttempts24h.Total24h)
	assert.Empty(t, result.RejectedWebhookAttempts24h.ByProvider)
	assert.Nil(t, result.RejectedWebhookAttempts24h.MostRecentAt)
	assert.True(t, result.RejectedWebhookAttempts24h.Available,
		"a genuinely quiet 24 hours must be indistinguishable from a healthy zero, not from an unknown")
	repo.AssertExpectations(t)
}

// TestGetSystemHealth_RejectedWebhookAttempts_QueryError_IsUnavailableNotAConfidentZero is the
// same unknown-is-not-zero rule the sibling sections enforce (#1458, #1497, #1504), applied here —
// and it is the exact operational hazard requirement #5 exists to close: a query failure must
// never render as "0 attacks" during an actual flood.
func TestGetSystemHealth_RejectedWebhookAttempts_QueryError_IsUnavailableNotAConfidentZero(t *testing.T) {
	repo := new(MockInvalidWebhookAttemptStatsReader)
	repo.On("GetStats24h", mock.Anything).Return(nil, nil, errors.New("db unavailable"))

	result, err := newRejectedWebhookAttemptsHealthService(t, repo).GetSystemHealth(context.Background())

	require.NoError(t, err, "one failed gauge must not fail the whole health response")
	assert.False(t, result.RejectedWebhookAttempts24h.Available,
		"a failed query has no number to report; reporting a green zero here is exactly the false reassurance requirement #5 exists to remove")
	assert.Equal(t, int64(0), result.RejectedWebhookAttempts24h.Total24h)
	repo.AssertExpectations(t)
}

// TestGetSystemHealth_RejectedWebhookAttempts_UnwiredRepo_IsUnavailableNotAConfidentZero closes
// the same gap for a nil repository — how main.go would reach this code if it were ever wired
// wrong, or how an older test construction of this struct reaches it today.
func TestGetSystemHealth_RejectedWebhookAttempts_UnwiredRepo_IsUnavailableNotAConfidentZero(t *testing.T) {
	result, err := newRejectedWebhookAttemptsHealthService(t, nil).GetSystemHealth(context.Background())

	require.NoError(t, err)
	assert.False(t, result.RejectedWebhookAttempts24h.Available)
	assert.Equal(t, int64(0), result.RejectedWebhookAttempts24h.Total24h)
}

// TestGetSystemHealth_RejectedWebhookAttempts_IndependentOfFailedWebhooks24h pins the distinction
// the domain doc comment calls out explicitly: a FailedWebhooks24h-moving event (a Paddle webhook
// that verified and then failed processing) must not move RejectedWebhookAttempts24h, and vice
// versa. This is the "most likely future regression" the brief calls out — a future change that
// wires both gauges to the same source.
func TestGetSystemHealth_RejectedWebhookAttempts_IndependentOfFailedWebhooks24h(t *testing.T) {
	mockHealthRepo := new(MockSystemHealthRepository)
	mockDeadLetterRepo := new(MockDeadLetterJobRepository)
	mockPool := new(MockPool)
	// FailedWebhooks24h source: total=20, failed=7 processing failures on ALREADY-VERIFIED webhooks.
	setupFullHealthMocks(mockHealthRepo, mockDeadLetterRepo, mockPool, 0, 20, 7)

	repo := new(MockInvalidWebhookAttemptStatsReader)
	// RejectedWebhookAttempts24h source: a completely different number (99) from a completely
	// different table (invalid_webhook_attempts).
	repo.On("GetStats24h", mock.Anything).Return(map[string]int64{"paddle": 99}, (*time.Time)(nil), nil)

	svc := &systemHealthService{
		healthRepo:                mockHealthRepo,
		deadLetterRepo:            mockDeadLetterRepo,
		pool:                      mockPool,
		invalidWebhookAttemptRepo: repo,
	}

	result, err := svc.GetSystemHealth(context.Background())
	require.NoError(t, err)

	assert.Equal(t, int64(7), result.PaddleStatus.FailedWebhooks24h,
		"FailedWebhooks24h must keep reporting processing failures of verified webhooks")
	assert.Equal(t, int64(99), result.RejectedWebhookAttempts24h.Total24h,
		"RejectedWebhookAttempts24h must report signature-rejected attempts from its own independent source")
	assert.NotEqual(t, result.PaddleStatus.FailedWebhooks24h, result.RejectedWebhookAttempts24h.Total24h,
		"the two numbers must be free to diverge — if a future change makes them equal by construction, that IS the conflation this test exists to catch")
}
