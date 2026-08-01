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
	assert.Contains(t, err.Error(), "2/2 upserts failed")
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

	assert.Equal(t, want.ByPeriod, opts.UniqueOpts.ByPeriod)
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
