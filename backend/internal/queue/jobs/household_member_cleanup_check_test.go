// Package jobs contains River job definitions and workers for async processing.
// Issue #1371 — Phase 7: Household member purge check job tests.
package jobs

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockHouseholdMemberCleanupRepository mocks the HouseholdMemberCleanupRepository interface.
type MockHouseholdMemberCleanupRepository struct {
	mock.Mock
}

func (m *MockHouseholdMemberCleanupRepository) ListMembershipsPastPurgeWindow(ctx context.Context, purgeAfterDays int) ([]*domain.HouseholdMember, error) {
	args := m.Called(ctx, purgeAfterDays)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.HouseholdMember), args.Error(1)
}

// MockHouseholdMemberPurgeService mocks the HouseholdMemberPurgeService interface.
type MockHouseholdMemberPurgeService struct {
	mock.Mock
}

func (m *MockHouseholdMemberPurgeService) PurgeRemovedMembership(ctx context.Context, householdID, membershipID string) error {
	args := m.Called(ctx, householdID, membershipID)
	return args.Error(0)
}

// Test helpers

func newTestHouseholdMemberCleanupJob(args HouseholdMemberCleanupCheckArgs) *river.Job[HouseholdMemberCleanupCheckArgs] {
	return &river.Job[HouseholdMemberCleanupCheckArgs]{
		JobRow: &rivertype.JobRow{
			ID:      1,
			Attempt: 1,
		},
		Args: args,
	}
}

func newTestHouseholdMemberCleanupWorker(repo HouseholdMemberCleanupRepository, purgeSvc HouseholdMemberPurgeService) *HouseholdMemberCleanupCheckWorker {
	return NewHouseholdMemberCleanupCheckWorker(HouseholdMemberCleanupCheckWorkerDeps{
		Repo:     repo,
		PurgeSvc: purgeSvc,
		Logger:   slog.Default(),
	})
}

// --- Kind ---

func TestHouseholdMemberCleanupCheckArgs_Kind(t *testing.T) {
	args := HouseholdMemberCleanupCheckArgs{}
	assert.Equal(t, "household_member_cleanup_check", args.Kind())
}

// --- Purge tests ---

func TestHouseholdMemberCleanupCheck_CandidateFound_PurgesByID(t *testing.T) {
	ctx := context.Background()

	removedAt := time.Now().Add(-31 * 24 * time.Hour)
	candidates := []*domain.HouseholdMember{
		{ID: "member-1", HouseholdID: "hh-1", UserID: "user-1", Status: domain.HouseholdMemberStatusRemoved, RemovedAt: &removedAt},
	}

	mockRepo := new(MockHouseholdMemberCleanupRepository)
	mockPurge := new(MockHouseholdMemberPurgeService)

	mockRepo.On("ListMembershipsPastPurgeWindow", ctx, 30).Return(candidates, nil)
	mockPurge.On("PurgeRemovedMembership", ctx, "hh-1", "member-1").Return(nil)

	worker := newTestHouseholdMemberCleanupWorker(mockRepo, mockPurge)
	job := newTestHouseholdMemberCleanupJob(HouseholdMemberCleanupCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockPurge.AssertExpectations(t)
	mockRepo.AssertNumberOfCalls(t, "ListMembershipsPastPurgeWindow", 1)
}

func TestHouseholdMemberCleanupCheck_NoCandidates_NoOp(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockHouseholdMemberCleanupRepository)
	mockPurge := new(MockHouseholdMemberPurgeService)

	mockRepo.On("ListMembershipsPastPurgeWindow", ctx, 30).Return([]*domain.HouseholdMember{}, nil)

	worker := newTestHouseholdMemberCleanupWorker(mockRepo, mockPurge)
	job := newTestHouseholdMemberCleanupJob(HouseholdMemberCleanupCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockPurge.AssertNotCalled(t, "PurgeRemovedMembership")
}

func TestHouseholdMemberCleanupCheck_PurgeError_ContinuesProcessing(t *testing.T) {
	ctx := context.Background()

	removedAt := time.Now().Add(-45 * 24 * time.Hour)
	candidates := []*domain.HouseholdMember{
		{ID: "member-fail", HouseholdID: "hh-1", UserID: "user-fail", Status: domain.HouseholdMemberStatusRemoved, RemovedAt: &removedAt},
		{ID: "member-ok", HouseholdID: "hh-2", UserID: "user-ok", Status: domain.HouseholdMemberStatusRemoved, RemovedAt: &removedAt},
	}

	mockRepo := new(MockHouseholdMemberCleanupRepository)
	mockPurge := new(MockHouseholdMemberPurgeService)

	mockRepo.On("ListMembershipsPastPurgeWindow", ctx, 30).Return(candidates, nil)
	mockPurge.On("PurgeRemovedMembership", ctx, "hh-1", "member-fail").Return(errors.New("purge failed"))
	mockPurge.On("PurgeRemovedMembership", ctx, "hh-2", "member-ok").Return(nil)

	worker := newTestHouseholdMemberCleanupWorker(mockRepo, mockPurge)
	job := newTestHouseholdMemberCleanupJob(HouseholdMemberCleanupCheckArgs{TriggerSource: "periodic"})

	// Job continues processing all candidates even when one fails, then returns an
	// error so River retries.
	err := worker.Work(ctx, job)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "1 of 2")
	mockPurge.AssertNumberOfCalls(t, "PurgeRemovedMembership", 2)
}

// TestHouseholdMemberCleanupCheck_NoMultiTableSQL proves — empirically, via mock
// assertions — that the worker calls NOTHING on the repository beyond the single
// scan query (ListMembershipsPastPurgeWindow). All multi-table deletion logic must
// live in HouseholdService.PurgeRemovedMembership (Task 0), never in this worker.
func TestHouseholdMemberCleanupCheck_NoMultiTableSQL(t *testing.T) {
	ctx := context.Background()

	removedAt := time.Now().Add(-31 * 24 * time.Hour)
	candidates := []*domain.HouseholdMember{
		{ID: "member-1", HouseholdID: "hh-1", UserID: "user-1", Status: domain.HouseholdMemberStatusRemoved, RemovedAt: &removedAt},
	}

	mockRepo := new(MockHouseholdMemberCleanupRepository)
	mockPurge := new(MockHouseholdMemberPurgeService)

	mockRepo.On("ListMembershipsPastPurgeWindow", ctx, 30).Return(candidates, nil)
	mockPurge.On("PurgeRemovedMembership", ctx, "hh-1", "member-1").Return(nil)

	worker := newTestHouseholdMemberCleanupWorker(mockRepo, mockPurge)
	job := newTestHouseholdMemberCleanupJob(HouseholdMemberCleanupCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	// The mock.Mock base only registers expectations for methods explicitly set up
	// via .On(...) — MockHouseholdMemberCleanupRepository has exactly one method
	// (ListMembershipsPastPurgeWindow), so this call count assertion together with
	// the interface's single-method shape proves no other repository method exists
	// for the worker to call.
	mockRepo.AssertNumberOfCalls(t, "ListMembershipsPastPurgeWindow", 1)
	mockRepo.AssertExpectations(t)
}

// TestHouseholdMemberCleanupCheckUniqueOpts proves uniqueness is enforced GLOBALLY across
// all non-terminal states of this job kind, with no ByPeriod time-window escape hatch —
// see the identical rationale on TestHouseholdOverCapacityCheckUniqueOpts (Round 8 Codex
// finding: a period-bucketed window could let a second instance run concurrently with a
// still-running prior instance that bridged into the next bucket).
func TestHouseholdMemberCleanupCheckUniqueOpts(t *testing.T) {
	opts := HouseholdMemberCleanupCheckUniqueOpts()

	assert.False(t, opts.ByArgs)
	assert.False(t, opts.ByQueue, "ByQueue must stay false — uniqueness must be global across queues, not scoped per-queue")
	assert.Zero(t, opts.ByPeriod, "ByPeriod must be unset — global non-terminal-state uniqueness, not a time-bucketed window a long-running job could bridge past")
	// ElementsMatch (not just Contains) proves ALL 5 non-terminal states are covered —
	// Contains-only assertions on a subset would silently pass even if a state (e.g.
	// JobStatePending) were dropped, leaving a real dedup gap undetected (Round 8 Codex
	// finding on the drift-guard test's own blind spot).
	assert.ElementsMatch(t, []queue.JobState{
		queue.JobStateAvailable,
		queue.JobStateScheduled,
		queue.JobStateRunning,
		queue.JobStateRetryable,
		queue.JobStatePending,
	}, opts.ByState)
}

// TestNewHouseholdMemberCleanupCheckWorker_DefaultLogger tests that nil logger uses default.
func TestNewHouseholdMemberCleanupCheckWorker_DefaultLogger(t *testing.T) {
	mockRepo := new(MockHouseholdMemberCleanupRepository)
	mockPurge := new(MockHouseholdMemberPurgeService)

	worker := NewHouseholdMemberCleanupCheckWorker(HouseholdMemberCleanupCheckWorkerDeps{
		Repo:     mockRepo,
		PurgeSvc: mockPurge,
		// Logger nil — should use default
	})

	assert.NotNil(t, worker.logger)
}

// ─── Issue #1448: main.go hand-duplicated this UniqueOpts shape instead of calling the
// helper, and RunOnStart was false with no ID set. ────────────────────────────────────────

func TestHouseholdMemberCleanupCheckInsertOpts_Queue(t *testing.T) {
	opts := HouseholdMemberCleanupCheckInsertOpts()
	require.NotNil(t, opts)
	assert.Equal(t, "low", opts.Queue)
}

func TestHouseholdMemberCleanupCheckInsertOpts_CarriesUniqueOpts(t *testing.T) {
	opts := HouseholdMemberCleanupCheckInsertOpts()
	want := HouseholdMemberCleanupCheckUniqueOpts()

	assert.Equal(t, want.ByArgs, opts.UniqueOpts.ByArgs)
	assert.Zero(t, opts.UniqueOpts.ByPeriod)
	require.NotEmpty(t, opts.UniqueOpts.ByState)
}

func TestHouseholdMemberCleanupCheckPeriodicOpts_RunOnStart(t *testing.T) {
	opts := HouseholdMemberCleanupCheckPeriodicOpts()
	require.NotNil(t, opts)
	assert.True(t, opts.RunOnStart)
}

func TestHouseholdMemberCleanupCheckPeriodicOpts_ID(t *testing.T) {
	opts := HouseholdMemberCleanupCheckPeriodicOpts()
	assert.Equal(t, "household_member_cleanup_check", opts.ID)
}

func TestHouseholdMemberCleanupCheckPeriodicOpts_PairedWithUniqueOpts(t *testing.T) {
	opts := HouseholdMemberCleanupCheckPeriodicOpts()
	require.True(t, opts.RunOnStart)

	unique := HouseholdMemberCleanupCheckUniqueOpts()
	require.NotEmpty(t, unique.ByState)
}
