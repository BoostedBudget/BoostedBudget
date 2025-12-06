package workers

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockDeletionServiceForWorker for testing
type MockDeletionServiceForWorker struct {
	mock.Mock
}

func (m *MockDeletionServiceForWorker) RequestDeletion(ctx context.Context, userID, email string) (string, time.Time, error) {
	args := m.Called(ctx, userID, email)
	return args.String(0), args.Get(1).(time.Time), args.Error(2)
}

func (m *MockDeletionServiceForWorker) CancelDeletion(ctx context.Context, token string) error {
	args := m.Called(ctx, token)
	return args.Error(0)
}

func (m *MockDeletionServiceForWorker) ExecuteDeletion(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

// MockProfileRepoForWorker for testing
type MockProfileRepoForWorker struct {
	mock.Mock
}

func (m *MockProfileRepoForWorker) GetUsersScheduledForDeletion(ctx context.Context) ([]*domain.User, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.User), args.Error(1)
}

func (m *MockProfileRepoForWorker) GetUserByID(ctx context.Context, userID string) (*domain.User, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

func (m *MockProfileRepoForWorker) GetUserByAuth0ID(ctx context.Context, auth0UserID string) (*domain.User, error) {
	args := m.Called(ctx, auth0UserID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

func (m *MockProfileRepoForWorker) CreateUser(ctx context.Context, user *domain.User) error {
	args := m.Called(ctx, user)
	return args.Error(0)
}

func (m *MockProfileRepoForWorker) CreateUserTx(ctx context.Context, tx *sql.Tx, user *domain.User) error {
	args := m.Called(ctx, tx, user)
	return args.Error(0)
}

func (m *MockProfileRepoForWorker) UpdateUser(ctx context.Context, user *domain.User) error {
	args := m.Called(ctx, user)
	return args.Error(0)
}

func (m *MockProfileRepoForWorker) UpdateLastLogin(ctx context.Context, auth0UserID string) error {
	args := m.Called(ctx, auth0UserID)
	return args.Error(0)
}

func (m *MockProfileRepoForWorker) MarkForDeletion(ctx context.Context, userID string, scheduledAt time.Time) error {
	args := m.Called(ctx, userID, scheduledAt)
	return args.Error(0)
}

func (m *MockProfileRepoForWorker) CancelDeletion(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

func (m *MockProfileRepoForWorker) DeleteUserCascade(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

// TestWorker_FindsScheduledUsers tests that worker finds users scheduled for deletion
func TestWorker_FindsScheduledUsers(t *testing.T) {
	ctx := context.Background()
	mockService := new(MockDeletionServiceForWorker)
	mockRepo := new(MockProfileRepoForWorker)

	worker := NewDeletionWorker(mockService, mockRepo, 1*time.Hour)

	scheduledUsers := []*domain.User{
		{
			ID:                  "user-1",
			Email:               "user1@example.com",
			DeletionScheduledAt: timePtr(time.Now().Add(-1 * time.Hour)),
			IsDeleted:           false,
		},
		{
			ID:                  "user-2",
			Email:               "user2@example.com",
			DeletionScheduledAt: timePtr(time.Now().Add(-2 * time.Hour)),
			IsDeleted:           false,
		},
	}

	mockRepo.On("GetUsersScheduledForDeletion", ctx).Return(scheduledUsers, nil)
	mockService.On("ExecuteDeletion", ctx, "user-1").Return(nil)
	mockService.On("ExecuteDeletion", ctx, "user-2").Return(nil)

	err := worker.ProcessScheduledDeletions(ctx)

	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockService.AssertExpectations(t)
	mockService.AssertNumberOfCalls(t, "ExecuteDeletion", 2)
}

// TestWorker_ProcessesUsers tests that worker processes each user correctly
func TestWorker_ProcessesUsers(t *testing.T) {
	ctx := context.Background()
	mockService := new(MockDeletionServiceForWorker)
	mockRepo := new(MockProfileRepoForWorker)

	worker := NewDeletionWorker(mockService, mockRepo, 1*time.Hour)

	user := &domain.User{
		ID:                  "user-123",
		Email:               "user@example.com",
		DeletionScheduledAt: timePtr(time.Now().Add(-1 * time.Hour)),
		IsDeleted:           false,
	}

	mockRepo.On("GetUsersScheduledForDeletion", ctx).Return([]*domain.User{user}, nil)
	mockService.On("ExecuteDeletion", ctx, user.ID).Return(nil)

	err := worker.ProcessScheduledDeletions(ctx)

	assert.NoError(t, err)
	mockService.AssertCalled(t, "ExecuteDeletion", ctx, user.ID)
}

// TestWorker_HandlesErrors tests graceful error handling
func TestWorker_HandlesErrors(t *testing.T) {
	ctx := context.Background()
	mockService := new(MockDeletionServiceForWorker)
	mockRepo := new(MockProfileRepoForWorker)

	worker := NewDeletionWorker(mockService, mockRepo, 1*time.Hour)

	users := []*domain.User{
		{
			ID:                  "user-1",
			Email:               "user1@example.com",
			DeletionScheduledAt: timePtr(time.Now().Add(-1 * time.Hour)),
			IsDeleted:           false,
		},
		{
			ID:                  "user-2",
			Email:               "user2@example.com",
			DeletionScheduledAt: timePtr(time.Now().Add(-2 * time.Hour)),
			IsDeleted:           false,
		},
	}

	mockRepo.On("GetUsersScheduledForDeletion", ctx).Return(users, nil)
	mockService.On("ExecuteDeletion", ctx, "user-1").Return(errors.New("deletion failed"))
	mockService.On("ExecuteDeletion", ctx, "user-2").Return(nil)

	// Worker should continue processing even if one deletion fails
	err := worker.ProcessScheduledDeletions(ctx)

	// Should not error - worker logs errors and continues
	assert.NoError(t, err)
	mockService.AssertCalled(t, "ExecuteDeletion", ctx, "user-1")
	mockService.AssertCalled(t, "ExecuteDeletion", ctx, "user-2")
}

// TestWorker_RespectsContext tests context cancellation
func TestWorker_RespectsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	mockService := new(MockDeletionServiceForWorker)
	mockRepo := new(MockProfileRepoForWorker)

	worker := NewDeletionWorker(mockService, mockRepo, 100*time.Millisecond)

	// Cancel context immediately
	cancel()

	// Start worker (should respect cancellation)
	done := make(chan bool)
	go func() {
		worker.Start(ctx)
		done <- true
	}()

	// Wait with timeout
	select {
	case <-done:
		// Worker stopped gracefully
		assert.True(t, true)
	case <-time.After(1 * time.Second):
		t.Fatal("Worker did not respect context cancellation")
	}
}

// TestWorker_GracefulShutdown tests graceful worker shutdown
func TestWorker_GracefulShutdown(t *testing.T) {
	// Use longer timeouts to account for race detection overhead in CI
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	mockService := new(MockDeletionServiceForWorker)
	mockRepo := new(MockProfileRepoForWorker)

	worker := NewDeletionWorker(mockService, mockRepo, 100*time.Millisecond)

	// Mock to avoid actual calls during shutdown test
	mockRepo.On("GetUsersScheduledForDeletion", mock.Anything).Return([]*domain.User{}, nil).Maybe()

	// Start worker
	done := make(chan bool)
	go func() {
		worker.Start(ctx)
		done <- true
	}()

	// Wait for context timeout (worker should shut down)
	// Use longer timeout to account for race detection overhead in CI
	select {
	case <-done:
		// Worker shut down gracefully
		assert.True(t, true)
	case <-time.After(5 * time.Second):
		t.Fatal("Worker did not shut down gracefully")
	}
}

// Helper function to create time pointer
func timePtr(t time.Time) *time.Time {
	return &t
}
