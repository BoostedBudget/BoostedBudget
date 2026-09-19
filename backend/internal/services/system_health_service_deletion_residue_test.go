package services

// Issue #1504: the post-gate external-effect residue on /admin/health.
//
// WHAT A RESIDUE RECORD MEANS, because every assertion below only makes sense against it: a GDPR
// deletion was revoked AFTER the Paddle subscription-cancel call had already landed. The user keeps
// their account and every byte of their data — and has no subscription. Nothing automated can undo
// that, and nothing else in the product tells anybody it happened.
//
// ⚠ WHAT THESE TESTS DELIBERATELY DO NOT DO: assert that the response HAS a deletion_residue field.
// That passes against a hardcoded zero — the vacuous-gate species this branch has spent ten rounds
// removing. Every number asserted here is a number the repository was seeded to return, and the
// mandatory sibling below proves the surface reports a real zero when there is no residue, so a
// constant cannot satisfy both.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// newResidueHealthService builds the service with only the collaborators /admin/health needs, matching
// the construction style of the Issue #1458 and #1497 health tests.
func newResidueHealthService(t *testing.T, jobHealthRepo *MockJobHealthRepository) SystemHealthService {
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
	// Assigned only when non-nil, on purpose: assigning a typed nil *MockJobHealthRepository would
	// produce a NON-nil interface holding a nil pointer, which sails past every `== nil` guard in the
	// service and then panics. The unwired-repo test below depends on the field being a true nil
	// interface.
	if jobHealthRepo != nil {
		svc.jobHealthRepo = jobHealthRepo
	}
	return svc
}

// stubBacklogQueries stubs the two Issue #1458 backlog queries so the residue assertions are not
// coupled to them. Their own tests cover them.
func stubBacklogQueries(repo *MockJobHealthRepository) {
	repo.On("GetOutstandingPlaidCleanupStats", mock.Anything).Return(0, time.Duration(0), nil)
	repo.On("GetOldestExpiredCompGrantAge", mock.Anything).Return(time.Duration(0), nil)
}

// TestGetSystemHealth_DeletionResidue_SeededPopulationIsVisible is the brief's RED test: a residue
// record reaches an operator. Before this batch RecordDeletionResidue's row was read by nothing at
// all, in any surface.
func TestGetSystemHealth_DeletionResidue_SeededPopulationIsVisible(t *testing.T) {
	jobHealthRepo := new(MockJobHealthRepository)
	stubBacklogQueries(jobHealthRepo)
	jobHealthRepo.On("GetDeletionResidueStats", mock.Anything).Return(3, 90*time.Minute, nil)

	result, err := newResidueHealthService(t, jobHealthRepo).GetSystemHealth(context.Background())

	require.NoError(t, err)
	assert.Equal(t, int64(3), result.DeletionResidue.Records,
		"three residue records means three people are paying nothing for a product they still have; the number has to arrive on the surface intact")
	assert.Equal(t, int64(90*60), result.DeletionResidue.NewestRecordSeconds,
		"the age of the NEWEST record is what tells an operator whether this just happened")
	assert.True(t, result.DeletionResidue.Available,
		"a successful query is a verified reading, not an unknown")
	jobHealthRepo.AssertExpectations(t)
}

// TestGetSystemHealth_DeletionResidue_OrdinaryDeletionReportsAVerifiedZero is the MANDATORY sibling.
// Without it the surface could return the constant above and the test above would still pass.
//
// The name is the scenario: an ordinary successful deletion writes no residue (pinned at the write
// side by TestExecuteDeletion_OrdinarySuccess_RecordsNoResidue), and this is the corresponding fact
// about the surface — zero records, zero age, and Available TRUE, because a real zero here is the
// good news and must be distinguishable from a failure that cannot say.
func TestGetSystemHealth_DeletionResidue_OrdinaryDeletionReportsAVerifiedZero(t *testing.T) {
	jobHealthRepo := new(MockJobHealthRepository)
	stubBacklogQueries(jobHealthRepo)
	jobHealthRepo.On("GetDeletionResidueStats", mock.Anything).Return(0, time.Duration(0), nil)

	result, err := newResidueHealthService(t, jobHealthRepo).GetSystemHealth(context.Background())

	require.NoError(t, err)
	assert.Equal(t, int64(0), result.DeletionResidue.Records,
		"no residue must read as zero, not as the seeded number from the sibling test")
	assert.Equal(t, int64(0), result.DeletionResidue.NewestRecordSeconds)
	assert.True(t, result.DeletionResidue.Available,
		"a genuine zero is the single most valuable reading this gauge produces and must never be confused with an unavailable one")
	jobHealthRepo.AssertExpectations(t)
}

// TestGetSystemHealth_DeletionResidue_QueryError_IsUnavailableNotAConfidentZero is the same
// unknown-is-not-zero rule the Issue #1458 and #1497 sections already enforce, applied here — and it
// matters more here than on either of them, because the thing this gauge would be falsely denying is
// "somebody's subscription was cancelled and nobody has been told".
func TestGetSystemHealth_DeletionResidue_QueryError_IsUnavailableNotAConfidentZero(t *testing.T) {
	jobHealthRepo := new(MockJobHealthRepository)
	stubBacklogQueries(jobHealthRepo)
	jobHealthRepo.On("GetDeletionResidueStats", mock.Anything).
		Return(0, time.Duration(0), errors.New("db unavailable"))

	result, err := newResidueHealthService(t, jobHealthRepo).GetSystemHealth(context.Background())

	require.NoError(t, err, "one failed gauge must not fail the whole health response")
	assert.False(t, result.DeletionResidue.Available,
		"a failed query has no number to report; reporting its zero would be a green light nobody earned")
	assert.Equal(t, int64(0), result.DeletionResidue.Records)
	jobHealthRepo.AssertExpectations(t)
}

// TestGetSystemHealth_DeletionResidue_UnwiredRepo_IsUnavailableNotAConfidentZero closes the same gap
// for a nil repository, which is how an older test construction — or a future wiring mistake in
// main.go — reaches this code. Fail-closed: unwired reads as unknown.
func TestGetSystemHealth_DeletionResidue_UnwiredRepo_IsUnavailableNotAConfidentZero(t *testing.T) {
	result, err := newResidueHealthService(t, nil).GetSystemHealth(context.Background())

	require.NoError(t, err)
	assert.False(t, result.DeletionResidue.Available,
		"nobody wired the repository, so nothing has been verified — an /admin/health reporting a confident 0 here would be this branch's signature defect on its most compliance-sensitive surface")
	assert.Equal(t, int64(0), result.DeletionResidue.Records)
}
