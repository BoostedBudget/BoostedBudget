// Package jobs contains River job definitions and workers for async processing.
package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Mocks
// =============================================================================

// mockSavingsStatsAggregateRepo mocks SavingsStatsAggregateRepository.
type mockSavingsStatsAggregateRepo struct {
	mock.Mock
}

func (m *mockSavingsStatsAggregateRepo) GetAggregateSavingsStats(ctx context.Context) (totalAmount float64, userCount int, completedCount int, err error) {
	args := m.Called(ctx)
	return args.Get(0).(float64), args.Int(1), args.Int(2), args.Error(3)
}

// mockSavingsImpactRepo mocks SavingsImpactWriteRepository.
type mockSavingsImpactRepo struct {
	mock.Mock
}

func (m *mockSavingsImpactRepo) WriteSnapshot(ctx context.Context, snapshot *domain.SavingsImpactSnapshot) error {
	args := m.Called(ctx, snapshot)
	return args.Error(0)
}

// =============================================================================
// Test helpers
// =============================================================================

func newTestSavingsStatsJob(args SavingsStatsArgs) *river.Job[SavingsStatsArgs] {
	return &river.Job[SavingsStatsArgs]{
		JobRow: &rivertype.JobRow{
			ID:      42,
			Attempt: 1,
		},
		Args: args,
	}
}

// =============================================================================
// Kind test
// =============================================================================

func TestSavingsStatsArgs_Kind(t *testing.T) {
	assert.Equal(t, "savings_stats", SavingsStatsArgs{}.Kind())
}

// =============================================================================
// Worker tests
// =============================================================================

// TestSavingsStatsWorker_WritesSnapshot verifies that the worker queries the
// aggregate stats and calls WriteSnapshot with the correct values.
func TestSavingsStatsWorker_WritesSnapshot(t *testing.T) {
	ctx := context.Background()

	aggRepo := new(mockSavingsStatsAggregateRepo)
	impactRepo := new(mockSavingsImpactRepo)

	aggRepo.On("GetAggregateSavingsStats", ctx).
		Return(1500.75, 42, 87, nil)

	impactRepo.On("WriteSnapshot", ctx, mock.MatchedBy(func(s *domain.SavingsImpactSnapshot) bool {
		return s.TotalAmount == 1500.75 &&
			s.UserCount == 42 &&
			s.CompletedCount == 87
	})).Return(nil)

	worker := NewSavingsStatsWorker(SavingsStatsWorkerDeps{
		AggregateRepo:    aggRepo,
		SavingsImpactRepo: impactRepo,
	})

	job := newTestSavingsStatsJob(SavingsStatsArgs{TriggerSource: "test"})
	err := worker.Work(ctx, job)

	require.NoError(t, err)
	aggRepo.AssertExpectations(t)
	impactRepo.AssertExpectations(t)
}

// TestSavingsStatsWorker_IdempotentSameDay verifies that calling the worker twice
// on the same day still calls WriteSnapshot both times (the repo handles the upsert).
func TestSavingsStatsWorker_IdempotentSameDay(t *testing.T) {
	ctx := context.Background()

	aggRepo := new(mockSavingsStatsAggregateRepo)
	impactRepo := new(mockSavingsImpactRepo)

	aggRepo.On("GetAggregateSavingsStats", ctx).
		Return(2000.00, 50, 100, nil).Twice()

	impactRepo.On("WriteSnapshot", ctx, mock.AnythingOfType("*domain.SavingsImpactSnapshot")).
		Return(nil).Twice()

	worker := NewSavingsStatsWorker(SavingsStatsWorkerDeps{
		AggregateRepo:    aggRepo,
		SavingsImpactRepo: impactRepo,
	})

	job := newTestSavingsStatsJob(SavingsStatsArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)
	require.NoError(t, err)

	err = worker.Work(ctx, job)
	require.NoError(t, err)

	aggRepo.AssertExpectations(t)
	impactRepo.AssertExpectations(t)
}

// TestSavingsStatsWorker_EmptyResults verifies that when there are no completed
// suggestions, the worker writes a snapshot with zero values.
func TestSavingsStatsWorker_EmptyResults(t *testing.T) {
	ctx := context.Background()

	aggRepo := new(mockSavingsStatsAggregateRepo)
	impactRepo := new(mockSavingsImpactRepo)

	aggRepo.On("GetAggregateSavingsStats", ctx).
		Return(0.0, 0, 0, nil)

	impactRepo.On("WriteSnapshot", ctx, mock.MatchedBy(func(s *domain.SavingsImpactSnapshot) bool {
		return s.TotalAmount == 0.0 &&
			s.UserCount == 0 &&
			s.CompletedCount == 0
	})).Return(nil)

	worker := NewSavingsStatsWorker(SavingsStatsWorkerDeps{
		AggregateRepo:    aggRepo,
		SavingsImpactRepo: impactRepo,
	})

	job := newTestSavingsStatsJob(SavingsStatsArgs{TriggerSource: "periodic"})
	err := worker.Work(ctx, job)

	require.NoError(t, err)
	aggRepo.AssertExpectations(t)
	impactRepo.AssertExpectations(t)
}

// TestSavingsStatsWorker_AggregateQueryError verifies that an error from the
// aggregate query is returned and WriteSnapshot is NOT called.
func TestSavingsStatsWorker_AggregateQueryError(t *testing.T) {
	ctx := context.Background()

	aggRepo := new(mockSavingsStatsAggregateRepo)
	impactRepo := new(mockSavingsImpactRepo)

	aggRepo.On("GetAggregateSavingsStats", ctx).
		Return(0.0, 0, 0, errMockJobs("db error"))

	worker := NewSavingsStatsWorker(SavingsStatsWorkerDeps{
		AggregateRepo:    aggRepo,
		SavingsImpactRepo: impactRepo,
	})

	job := newTestSavingsStatsJob(SavingsStatsArgs{TriggerSource: "periodic"})
	err := worker.Work(ctx, job)

	require.Error(t, err)
	impactRepo.AssertNotCalled(t, "WriteSnapshot")
	aggRepo.AssertExpectations(t)
}

// TestSavingsStatsWorker_WriteSnapshotError verifies that an error from WriteSnapshot
// is returned to River for retry.
func TestSavingsStatsWorker_WriteSnapshotError(t *testing.T) {
	ctx := context.Background()

	aggRepo := new(mockSavingsStatsAggregateRepo)
	impactRepo := new(mockSavingsImpactRepo)

	aggRepo.On("GetAggregateSavingsStats", ctx).
		Return(100.0, 1, 2, nil)

	impactRepo.On("WriteSnapshot", ctx, mock.AnythingOfType("*domain.SavingsImpactSnapshot")).
		Return(errMockJobs("write failed"))

	worker := NewSavingsStatsWorker(SavingsStatsWorkerDeps{
		AggregateRepo:    aggRepo,
		SavingsImpactRepo: impactRepo,
	})

	job := newTestSavingsStatsJob(SavingsStatsArgs{TriggerSource: "periodic"})
	err := worker.Work(ctx, job)

	require.Error(t, err)
	aggRepo.AssertExpectations(t)
	impactRepo.AssertExpectations(t)
}

// TestSavingsStatsWorker_SnapshotTimestamp verifies that the snapshot ComputedAt
// is set to a recent time (within 5 seconds of now).
func TestSavingsStatsWorker_SnapshotTimestamp(t *testing.T) {
	ctx := context.Background()
	before := time.Now()

	aggRepo := new(mockSavingsStatsAggregateRepo)
	impactRepo := new(mockSavingsImpactRepo)

	aggRepo.On("GetAggregateSavingsStats", ctx).
		Return(100.0, 1, 2, nil)

	var captured *domain.SavingsImpactSnapshot
	impactRepo.On("WriteSnapshot", ctx, mock.MatchedBy(func(s *domain.SavingsImpactSnapshot) bool {
		captured = s
		return true
	})).Return(nil)

	worker := NewSavingsStatsWorker(SavingsStatsWorkerDeps{
		AggregateRepo:    aggRepo,
		SavingsImpactRepo: impactRepo,
	})

	job := newTestSavingsStatsJob(SavingsStatsArgs{})
	err := worker.Work(ctx, job)
	after := time.Now()

	require.NoError(t, err)
	require.NotNil(t, captured)
	assert.True(t, !captured.ComputedAt.Before(before), "ComputedAt should be >= before")
	assert.True(t, !captured.ComputedAt.After(after), "ComputedAt should be <= after")
}

// errMockJobs is a simple test error.
type errMockJobs string

func (e errMockJobs) Error() string { return string(e) }

// ─── Issue #1448: main.go's comment referenced a "SavingsStatsUniqueOpts" that did not
// exist anywhere in the repo, and RunOnStart was false with no ID set. ────────────────────

func TestSavingsStatsUniqueOpts(t *testing.T) {
	opts := SavingsStatsUniqueOpts()

	assert.Equal(t, 24*time.Hour, opts.ByPeriod)
	assert.False(t, opts.ByArgs)
	assert.Empty(t, opts.ByState)
}

func TestSavingsStatsInsertOpts_Queue(t *testing.T) {
	opts := SavingsStatsInsertOpts()
	require.NotNil(t, opts)
	assert.Equal(t, "low", opts.Queue)
}

func TestSavingsStatsInsertOpts_CarriesUniqueOpts(t *testing.T) {
	opts := SavingsStatsInsertOpts()
	want := SavingsStatsUniqueOpts()

	assert.Equal(t, want.ByArgs, opts.UniqueOpts.ByArgs)
	assert.Equal(t, want.ByPeriod, opts.UniqueOpts.ByPeriod)
	assert.False(t, opts.UniqueOpts.ByQueue, "uniqueness must stay global across queues")
	assert.Empty(t, opts.UniqueOpts.ByState,
		"SavingsStatsUniqueOpts() sets no ByState (ByPeriod-only dedup) — an unexpected "+
			"ByState here means the conversion helper stopped preserving that")
}

func TestSavingsStatsPeriodicOpts_RunOnStart(t *testing.T) {
	opts := SavingsStatsPeriodicOpts()
	require.NotNil(t, opts)
	assert.True(t, opts.RunOnStart)
}

func TestSavingsStatsPeriodicOpts_ID(t *testing.T) {
	opts := SavingsStatsPeriodicOpts()
	assert.Equal(t, "savings_stats", opts.ID)
}

func TestSavingsStatsPeriodicOpts_PairedWithUniqueOpts(t *testing.T) {
	opts := SavingsStatsPeriodicOpts()
	require.True(t, opts.RunOnStart)

	unique := SavingsStatsUniqueOpts()
	require.NotZero(t, unique.ByPeriod,
		"RunOnStart: true's catch-up insert is only safe when deduped by a non-zero ByPeriod window")
}
