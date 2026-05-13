package jobs

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockTransactionRepositoryForCategoryRefresh is a minimal mock for the
// effective category refresh worker.
type MockTransactionRepositoryForCategoryRefresh struct {
	mock.Mock
}

func (m *MockTransactionRepositoryForCategoryRefresh) UpdateEffectiveCategoryNameByCustomCategoryID(
	ctx context.Context, userID, customCategoryID, newName string,
) (int64, error) {
	args := m.Called(ctx, userID, customCategoryID, newName)
	return int64(args.Int(0)), args.Error(1)
}

func newTestEffectiveCategoryRefreshJob(args EffectiveCategoryRefreshArgs) *river.Job[EffectiveCategoryRefreshArgs] {
	return &river.Job[EffectiveCategoryRefreshArgs]{
		JobRow: &rivertype.JobRow{
			ID:      1,
			Kind:    "effective_category_refresh",
			Queue:   "default",
			Attempt: 1,
		},
		Args: args,
	}
}

func TestEffectiveCategoryRefreshArgs_Kind(t *testing.T) {
	args := EffectiveCategoryRefreshArgs{}
	assert.Equal(t, "effective_category_refresh", args.Kind())
}

func TestEffectiveCategoryRefreshWorker_Work_Success(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	customCategoryID := uuid.New().String()
	newName := "Dining Out"

	mockRepo := new(MockTransactionRepositoryForCategoryRefresh)
	mockRepo.On("UpdateEffectiveCategoryNameByCustomCategoryID", ctx, userID, customCategoryID, newName).
		Return(5, nil)

	worker := NewEffectiveCategoryRefreshWorker(EffectiveCategoryRefreshWorkerDeps{
		TransactionRepo: mockRepo,
		Logger:          slog.Default(),
	})

	job := newTestEffectiveCategoryRefreshJob(EffectiveCategoryRefreshArgs{
		UserID:           userID,
		CustomCategoryID: customCategoryID,
		NewName:          newName,
	})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

func TestEffectiveCategoryRefreshWorker_Work_ZeroRowsUpdated(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	customCategoryID := uuid.New().String()
	newName := "Travel"

	mockRepo := new(MockTransactionRepositoryForCategoryRefresh)
	mockRepo.On("UpdateEffectiveCategoryNameByCustomCategoryID", ctx, userID, customCategoryID, newName).
		Return(0, nil)

	worker := NewEffectiveCategoryRefreshWorker(EffectiveCategoryRefreshWorkerDeps{
		TransactionRepo: mockRepo,
		Logger:          slog.Default(),
	})

	job := newTestEffectiveCategoryRefreshJob(EffectiveCategoryRefreshArgs{
		UserID:           userID,
		CustomCategoryID: customCategoryID,
		NewName:          newName,
	})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

func TestEffectiveCategoryRefreshWorker_Work_RepoError(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	customCategoryID := uuid.New().String()
	newName := "Groceries"

	dbErr := errors.New("database connection lost")

	mockRepo := new(MockTransactionRepositoryForCategoryRefresh)
	mockRepo.On("UpdateEffectiveCategoryNameByCustomCategoryID", ctx, userID, customCategoryID, newName).
		Return(0, dbErr)

	worker := NewEffectiveCategoryRefreshWorker(EffectiveCategoryRefreshWorkerDeps{
		TransactionRepo: mockRepo,
		Logger:          slog.Default(),
	})

	job := newTestEffectiveCategoryRefreshJob(EffectiveCategoryRefreshArgs{
		UserID:           userID,
		CustomCategoryID: customCategoryID,
		NewName:          newName,
	})

	err := worker.Work(ctx, job)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "effective category refresh failed")
	assert.ErrorIs(t, err, dbErr)
	mockRepo.AssertExpectations(t)
}

func TestNewEffectiveCategoryRefreshWorker_DefaultLogger(t *testing.T) {
	mockRepo := new(MockTransactionRepositoryForCategoryRefresh)

	worker := NewEffectiveCategoryRefreshWorker(EffectiveCategoryRefreshWorkerDeps{
		TransactionRepo: mockRepo,
	})

	assert.NotNil(t, worker.logger)
}
