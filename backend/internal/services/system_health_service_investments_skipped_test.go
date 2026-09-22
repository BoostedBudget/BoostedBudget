package services

// Issue #1707: the investments-skipped-at-link gauge on /admin/health.
//
// WHAT THIS GAUGE MEANS: a linked_items row whose Plaid link was created while Plaid had not
// granted investment access. The column is a link-time latch, so the count is an UPPER BOUND on
// the currently-affected population — see domain.InvestmentsSkippedAtLinkMetrics.
//
// ⚠ WHAT THESE TESTS DELIBERATELY DO NOT DO: assert that the response merely HAS an
// investments_skipped_at_link field. That passes against a hardcoded zero. Every number asserted
// here is a number the repository was seeded to return, and the sibling below proves the surface
// reports a real zero when there is none, so a constant cannot satisfy both.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// stubInvestmentsSkippedGauge stubs the OTHER gauges GetSystemHealth reads on every call so the
// investments-skipped assertions are not coupled to them. Their own tests cover them.
func stubInvestmentsSkippedGauge(repo *MockJobHealthRepository) {
	stubBacklogQueries(repo)
	repo.On("GetDeletionResidueStats", mock.Anything).Return(0, time.Duration(0), nil).Maybe()
}

// TestGetSystemHealth_InvestmentsSkippedAtLink_SeededCountIsVisible is the RED test: a seeded
// count reaches an operator on the response.
func TestGetSystemHealth_InvestmentsSkippedAtLink_SeededCountIsVisible(t *testing.T) {
	jobHealthRepo := new(MockJobHealthRepository)
	stubInvestmentsSkippedGauge(jobHealthRepo)
	jobHealthRepo.On("GetInvestmentsSkippedAtLinkCount", mock.Anything).Return(int64(4200), nil)

	result, err := newResidueHealthService(t, jobHealthRepo).GetSystemHealth(context.Background())

	require.NoError(t, err)
	assert.Equal(t, int64(4200), result.InvestmentsSkippedAtLink.Count,
		"the seeded count must arrive on the surface intact")
	assert.True(t, result.InvestmentsSkippedAtLink.Available,
		"a successful query is a verified reading, not an unknown")
	jobHealthRepo.AssertExpectations(t)
}

// TestGetSystemHealth_InvestmentsSkippedAtLink_UnwiredRepo_IsUnavailableNotAConfidentZero closes
// the nil-repository gap: an older test construction or a future wiring mistake in main.go reaches
// this code. Fail-closed: unwired reads as unknown, never as a confident zero.
func TestGetSystemHealth_InvestmentsSkippedAtLink_UnwiredRepo_IsUnavailableNotAConfidentZero(t *testing.T) {
	result, err := newResidueHealthService(t, nil).GetSystemHealth(context.Background())

	require.NoError(t, err)
	assert.False(t, result.InvestmentsSkippedAtLink.Available,
		"nobody wired the repository, so nothing has been verified — a confident 0 here would be the defect this convention exists to prevent")
	assert.Equal(t, int64(0), result.InvestmentsSkippedAtLink.Count)
}

// TestGetSystemHealth_InvestmentsSkippedAtLink_QueryError_IsUnavailableNotAConfidentZero proves a
// query failure is NEVER reported as a confident zero, in two separate places:
//
//	(a) the getter itself returns a non-nil error and an unavailable zero struct; and
//	(b) GetSystemHealth SWALLOWS that error by design (one failed gauge must not fail the whole
//	    health response) and presents the gauge as unavailable — never as a confident 0.
func TestGetSystemHealth_InvestmentsSkippedAtLink_QueryError_IsUnavailableNotAConfidentZero(t *testing.T) {
	t.Run("the getter returns a wrapped error and an unavailable zero struct", func(t *testing.T) {
		jobHealthRepo := new(MockJobHealthRepository)
		jobHealthRepo.On("GetInvestmentsSkippedAtLinkCount", mock.Anything).
			Return(int64(0), errors.New("boom"))

		svc := &systemHealthService{jobHealthRepo: jobHealthRepo}
		metrics, err := svc.getInvestmentsSkippedAtLinkMetrics(context.Background())

		require.Error(t, err, "a query failure must surface as an error, not be swallowed into a zero")
		assert.Contains(t, err.Error(), "read investments-skipped-at-link stats")
		assert.False(t, metrics.Available, "a failed query has no number to report")
		assert.Equal(t, int64(0), metrics.Count)
		jobHealthRepo.AssertExpectations(t)
	})

	t.Run("GetSystemHealth still succeeds and presents the gauge as unavailable", func(t *testing.T) {
		jobHealthRepo := new(MockJobHealthRepository)
		stubInvestmentsSkippedGauge(jobHealthRepo)
		jobHealthRepo.On("GetInvestmentsSkippedAtLinkCount", mock.Anything).
			Return(int64(0), errors.New("boom"))

		result, err := newResidueHealthService(t, jobHealthRepo).GetSystemHealth(context.Background())

		require.NoError(t, err, "one failed gauge must not fail the whole health response")
		assert.False(t, result.InvestmentsSkippedAtLink.Available,
			"a failed query must render as unavailable, never as a confident 0")
		assert.Equal(t, int64(0), result.InvestmentsSkippedAtLink.Count)
		jobHealthRepo.AssertExpectations(t)
	})
}
