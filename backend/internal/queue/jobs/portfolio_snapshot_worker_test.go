package jobs

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// --- mockHoldingRepo ---

type mockHoldingRepo struct {
	mock.Mock
}

func (m *mockHoldingRepo) GetDistinctUserIDsWithHoldings(ctx context.Context) ([]string, error) {
	args := m.Called(ctx)
	ids, _ := args.Get(0).([]string)
	return ids, args.Error(1)
}

func (m *mockHoldingRepo) ReplaceForAccount(ctx context.Context, accountID, userID string, holdings []domain.Holding) error {
	panic("not implemented")
}

func (m *mockHoldingRepo) GetByUserID(ctx context.Context, userID string) ([]domain.Holding, error) {
	panic("not implemented")
}

func (m *mockHoldingRepo) GetByAccountID(ctx context.Context, accountID string) ([]domain.Holding, error) {
	panic("not implemented")
}

func (m *mockHoldingRepo) GetByUserIDWithSecurities(ctx context.Context, userID string, accountID *string) ([]domain.Holding, error) {
	panic("not implemented")
}

func (m *mockHoldingRepo) GetByUserAccountID(ctx context.Context, userAccountID string) ([]domain.Holding, error) {
	panic("not implemented")
}

// --- mockSnapshotRepo ---

type mockSnapshotRepo struct {
	mock.Mock
}

func (m *mockSnapshotRepo) Upsert(ctx context.Context, userID string, snapshotDate time.Time) (*domain.PortfolioSnapshot, error) {
	args := m.Called(ctx, userID, snapshotDate)
	snap, _ := args.Get(0).(*domain.PortfolioSnapshot)
	return snap, args.Error(1)
}

func (m *mockSnapshotRepo) GetByUserDateRange(ctx context.Context, userID string, start, end time.Time) ([]domain.PortfolioSnapshot, error) {
	panic("not implemented")
}

func (m *mockSnapshotRepo) GetLatestBefore(ctx context.Context, userID string, date time.Time) (*domain.PortfolioSnapshot, error) {
	panic("not implemented")
}

// --- helpers ---

func newTestPortfolioSnapshotJob(args PortfolioSnapshotArgs) *river.Job[PortfolioSnapshotArgs] {
	return &river.Job[PortfolioSnapshotArgs]{
		JobRow: &rivertype.JobRow{
			ID:      1,
			Attempt: 1,
			Queue:   "default",
			Kind:    "portfolio_snapshot",
		},
		Args: args,
	}
}

// --- Tests ---

func TestPortfolioSnapshotArgs_Kind(t *testing.T) {
	assert.Equal(t, "portfolio_snapshot", PortfolioSnapshotArgs{}.Kind())
}

func TestNewPortfolioSnapshotWorker_NilLogger_DoesNotPanic(t *testing.T) {
	holdingRepo := new(mockHoldingRepo)
	snapshotRepo := new(mockSnapshotRepo)

	// Passing nil Logger should not panic; the constructor falls back to slog.Default()
	worker := NewPortfolioSnapshotWorker(PortfolioSnapshotWorkerDeps{
		HoldingRepo:  holdingRepo,
		SnapshotRepo: snapshotRepo,
		Logger:       nil,
	})

	assert.NotNil(t, worker)
}

func TestNewPortfolioSnapshotWorker_WithLogger(t *testing.T) {
	holdingRepo := new(mockHoldingRepo)
	snapshotRepo := new(mockSnapshotRepo)

	worker := NewPortfolioSnapshotWorker(PortfolioSnapshotWorkerDeps{
		HoldingRepo:  holdingRepo,
		SnapshotRepo: snapshotRepo,
		Logger:       slog.Default(),
	})

	assert.NotNil(t, worker)
}

func TestPortfolioSnapshotWorker_Work_NoUsers(t *testing.T) {
	ctx := context.Background()

	holdingRepo := new(mockHoldingRepo)
	holdingRepo.On("GetDistinctUserIDsWithHoldings", ctx).
		Return([]string{}, nil)

	snapshotRepo := new(mockSnapshotRepo)

	worker := NewPortfolioSnapshotWorker(PortfolioSnapshotWorkerDeps{
		HoldingRepo:  holdingRepo,
		SnapshotRepo: snapshotRepo,
		Logger:       slog.Default(),
	})

	job := newTestPortfolioSnapshotJob(PortfolioSnapshotArgs{TriggerSource: "test"})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	holdingRepo.AssertExpectations(t)
	snapshotRepo.AssertExpectations(t)
}

func TestPortfolioSnapshotWorker_Work_Success(t *testing.T) {
	ctx := context.Background()

	holdingRepo := new(mockHoldingRepo)
	holdingRepo.On("GetDistinctUserIDsWithHoldings", ctx).
		Return([]string{"user-1"}, nil)

	snapshotRepo := new(mockSnapshotRepo)
	// Match any snapshotDate since it's computed inside Work as time.Now().UTC().Truncate(24h)
	snapshotRepo.On("Upsert", ctx, "user-1", mock.AnythingOfType("time.Time")).
		Return(&domain.PortfolioSnapshot{}, nil)

	worker := NewPortfolioSnapshotWorker(PortfolioSnapshotWorkerDeps{
		HoldingRepo:  holdingRepo,
		SnapshotRepo: snapshotRepo,
		Logger:       slog.Default(),
	})

	job := newTestPortfolioSnapshotJob(PortfolioSnapshotArgs{TriggerSource: "test"})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	holdingRepo.AssertExpectations(t)
	snapshotRepo.AssertExpectations(t)
}

func TestPortfolioSnapshotWorker_Work_EnumerateError(t *testing.T) {
	ctx := context.Background()
	dbErr := errors.New("db error")

	holdingRepo := new(mockHoldingRepo)
	holdingRepo.On("GetDistinctUserIDsWithHoldings", ctx).
		Return(nil, dbErr)

	snapshotRepo := new(mockSnapshotRepo)

	worker := NewPortfolioSnapshotWorker(PortfolioSnapshotWorkerDeps{
		HoldingRepo:  holdingRepo,
		SnapshotRepo: snapshotRepo,
		Logger:       slog.Default(),
	})

	job := newTestPortfolioSnapshotJob(PortfolioSnapshotArgs{TriggerSource: "test"})

	err := worker.Work(ctx, job)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "enumerate")
	holdingRepo.AssertExpectations(t)
}

func TestPortfolioSnapshotWorker_Work_UpsertError(t *testing.T) {
	ctx := context.Background()
	upsertErr := errors.New("upsert failed")

	holdingRepo := new(mockHoldingRepo)
	holdingRepo.On("GetDistinctUserIDsWithHoldings", ctx).
		Return([]string{"user-1", "user-2"}, nil)

	snapshotRepo := new(mockSnapshotRepo)
	// Both upserts fail
	snapshotRepo.On("Upsert", ctx, mock.AnythingOfType("string"), mock.AnythingOfType("time.Time")).
		Return((*domain.PortfolioSnapshot)(nil), upsertErr)

	worker := NewPortfolioSnapshotWorker(PortfolioSnapshotWorkerDeps{
		HoldingRepo:  holdingRepo,
		SnapshotRepo: snapshotRepo,
		Logger:       slog.Default(),
	})

	job := newTestPortfolioSnapshotJob(PortfolioSnapshotArgs{TriggerSource: "test"})

	err := worker.Work(ctx, job)

	require.Error(t, err)
	// "per-user writes" rather than "upserts" since #1569 T6: the same counter now also covers
	// a failed holding_snapshots capture, so the wording no longer names only the upsert.
	assert.Contains(t, err.Error(), "2/2 per-user writes failed")
	holdingRepo.AssertExpectations(t)
	snapshotRepo.AssertExpectations(t)
}

// ─── Issue #1448: main.go's periodic registration had this dedup only as an inline
// literal, with no named helper, RunOnStart: false, and no ID. ────────────────────────────

func TestPortfolioSnapshotUniqueOpts(t *testing.T) {
	opts := PortfolioSnapshotUniqueOpts()

	assert.Equal(t, 24*time.Hour, opts.ByPeriod)
	assert.False(t, opts.ByArgs)
	assert.Empty(t, opts.ByState)
}

func TestPortfolioSnapshotInsertOpts_Queue(t *testing.T) {
	opts := PortfolioSnapshotInsertOpts()
	require.NotNil(t, opts)
	assert.Equal(t, "low", opts.Queue)
}

func TestPortfolioSnapshotInsertOpts_CarriesUniqueOpts(t *testing.T) {
	opts := PortfolioSnapshotInsertOpts()
	want := PortfolioSnapshotUniqueOpts()

	assert.Equal(t, want.ByArgs, opts.UniqueOpts.ByArgs)
	assert.Equal(t, want.ByPeriod, opts.UniqueOpts.ByPeriod)
	assert.False(t, opts.UniqueOpts.ByQueue, "uniqueness must stay global across queues")
	assert.Empty(t, opts.UniqueOpts.ByState,
		"PortfolioSnapshotUniqueOpts() sets no ByState (ByPeriod-only dedup) — an unexpected "+
			"ByState here means the conversion helper stopped preserving that")
}

func TestPortfolioSnapshotPeriodicOpts_RunOnStart(t *testing.T) {
	opts := PortfolioSnapshotPeriodicOpts()
	require.NotNil(t, opts)
	assert.True(t, opts.RunOnStart)
}

func TestPortfolioSnapshotPeriodicOpts_ID(t *testing.T) {
	opts := PortfolioSnapshotPeriodicOpts()
	assert.Equal(t, "portfolio_snapshot", opts.ID)
}

func TestPortfolioSnapshotPeriodicOpts_PairedWithUniqueOpts(t *testing.T) {
	opts := PortfolioSnapshotPeriodicOpts()
	require.True(t, opts.RunOnStart)

	unique := PortfolioSnapshotUniqueOpts()
	require.NotZero(t, unique.ByPeriod)
}

// --- #1569 T6: the daily run must also record row-level history ---

type mockHoldingSnapshotRepo struct {
	mock.Mock
}

func (m *mockHoldingSnapshotRepo) CaptureForUser(ctx context.Context, userID string) error {
	return m.Called(ctx, userID).Error(0)
}

func (m *mockHoldingSnapshotRepo) GetFinalCapturesForUserOnDate(ctx context.Context, userID string, asOf time.Time) ([]domain.HoldingSnapshot, error) {
	panic("not implemented")
}

func (m *mockHoldingSnapshotRepo) GetByAccountDateRange(ctx context.Context, accountID string, start, end time.Time) ([]domain.HoldingSnapshot, error) {
	panic("not implemented")
}

// This worker is the ONLY writer of portfolio_snapshots on a day with no Plaid sync — it upserts
// an aggregate for every user with holdings whether or not anything synced. holding_snapshots is
// otherwise written only by a sync, so without this call the two tables disagree about which days
// exist, and task 11 (which rebuilds portfolio_snapshots FROM holding_snapshots) would find a day
// with an aggregate and no rows to rebuild it from. Capturing here is what keeps holding_snapshots
// a day-for-day row-level decomposition of portfolio_snapshots.
func TestPortfolioSnapshotWorker_Work_CapturesHoldingHistoryForEveryUser(t *testing.T) {
	ctx := context.Background()

	holdingRepo := new(mockHoldingRepo)
	holdingRepo.On("GetDistinctUserIDsWithHoldings", ctx).Return([]string{"user-1", "user-2"}, nil)

	snapshotRepo := new(mockSnapshotRepo)
	snapshotRepo.On("Upsert", ctx, mock.Anything, mock.AnythingOfType("time.Time")).
		Return(&domain.PortfolioSnapshot{}, nil)

	historyRepo := new(mockHoldingSnapshotRepo)
	historyRepo.On("CaptureForUser", ctx, "user-1").Return(nil)
	historyRepo.On("CaptureForUser", ctx, "user-2").Return(nil)

	worker := NewPortfolioSnapshotWorker(PortfolioSnapshotWorkerDeps{
		HoldingRepo:         holdingRepo,
		SnapshotRepo:        snapshotRepo,
		HoldingSnapshotRepo: historyRepo,
		Logger:              slog.Default(),
	})

	require.NoError(t, worker.Work(ctx, newTestPortfolioSnapshotJob(PortfolioSnapshotArgs{TriggerSource: "test"})))
	historyRepo.AssertExpectations(t)
	snapshotRepo.AssertExpectations(t)
}

// A capture failure loses a day of history, which is the exact loss this task exists to stop — so
// it fails the job and lets River retry, the same way an upsert failure does. It must NOT abort
// the remaining users: one user's failure has never stopped the rest here.
func TestPortfolioSnapshotWorker_Work_CaptureError_FailsJobButStillProcessesEveryUser(t *testing.T) {
	ctx := context.Background()

	holdingRepo := new(mockHoldingRepo)
	holdingRepo.On("GetDistinctUserIDsWithHoldings", ctx).Return([]string{"user-1", "user-2"}, nil)

	snapshotRepo := new(mockSnapshotRepo)
	snapshotRepo.On("Upsert", ctx, mock.Anything, mock.AnythingOfType("time.Time")).
		Return(&domain.PortfolioSnapshot{}, nil)

	historyRepo := new(mockHoldingSnapshotRepo)
	historyRepo.On("CaptureForUser", ctx, "user-1").Return(errors.New("capture blew up"))
	historyRepo.On("CaptureForUser", ctx, "user-2").Return(nil)

	worker := NewPortfolioSnapshotWorker(PortfolioSnapshotWorkerDeps{
		HoldingRepo:         holdingRepo,
		SnapshotRepo:        snapshotRepo,
		HoldingSnapshotRepo: historyRepo,
		Logger:              slog.Default(),
	})

	err := worker.Work(ctx, newTestPortfolioSnapshotJob(PortfolioSnapshotArgs{TriggerSource: "test"}))
	require.Error(t, err)
	historyRepo.AssertExpectations(t)
	// user-2 was still reached — both the capture and the upsert.
	snapshotRepo.AssertCalled(t, "Upsert", ctx, "user-2", mock.AnythingOfType("time.Time"))
}

// The dependency is optional so the worker keeps running wherever it is not wired (and so the
// pre-existing tests above, which do not set it, still describe real behaviour rather than a
// configuration this build can no longer produce).
func TestPortfolioSnapshotWorker_Work_NilHoldingSnapshotRepo_DoesNotPanic(t *testing.T) {
	ctx := context.Background()

	holdingRepo := new(mockHoldingRepo)
	holdingRepo.On("GetDistinctUserIDsWithHoldings", ctx).Return([]string{"user-1"}, nil)

	snapshotRepo := new(mockSnapshotRepo)
	snapshotRepo.On("Upsert", ctx, "user-1", mock.AnythingOfType("time.Time")).
		Return(&domain.PortfolioSnapshot{}, nil)

	worker := NewPortfolioSnapshotWorker(PortfolioSnapshotWorkerDeps{
		HoldingRepo:  holdingRepo,
		SnapshotRepo: snapshotRepo,
		Logger:       slog.Default(),
	})

	require.NoError(t, worker.Work(ctx, newTestPortfolioSnapshotJob(PortfolioSnapshotArgs{TriggerSource: "test"})))
}
