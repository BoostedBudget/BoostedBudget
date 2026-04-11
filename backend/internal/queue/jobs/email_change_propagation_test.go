package jobs

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type MockEmailPropagationProfileRepo struct {
	mock.Mock
}

func (m *MockEmailPropagationProfileRepo) GetUserByID(ctx context.Context, id string) (*domain.User, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

type MockEmailPropagationClerkService struct {
	mock.Mock
}

func (m *MockEmailPropagationClerkService) UpdateUserEmail(ctx context.Context, clerkUserID, newEmail string) error {
	args := m.Called(ctx, clerkUserID, newEmail)
	return args.Error(0)
}

type MockEmailPropagationPaddleService struct {
	mock.Mock
}

func (m *MockEmailPropagationPaddleService) UpdateCustomerEmail(ctx context.Context, customerID, email string) error {
	args := m.Called(ctx, customerID, email)
	return args.Error(0)
}

func makeEmailPropJob(userID, clerkUserID string) *river.Job[EmailChangePropagationArgs] {
	return &river.Job[EmailChangePropagationArgs]{
		JobRow: &rivertype.JobRow{},
		Args: EmailChangePropagationArgs{
			UserID:      userID,
			ClerkUserID: clerkUserID,
		},
	}
}

func TestEmailChangePropagationWorker_HappyPath(t *testing.T) {
	userID := uuid.New().String()
	paddleCustomerID := "ctm_abc123"
	mockRepo := new(MockEmailPropagationProfileRepo)
	mockClerk := new(MockEmailPropagationClerkService)
	mockPaddle := new(MockEmailPropagationPaddleService)

	worker := NewEmailChangePropagationWorker(EmailChangePropagationWorkerDeps{
		ProfileRepo:   mockRepo,
		ClerkService:  mockClerk,
		PaddleService: mockPaddle,
	})

	user := &domain.User{
		ID:               userID,
		ClerkUserID:      "user_clerk123",
		Email:            "new@example.com",
		PaddleCustomerID: &paddleCustomerID,
	}

	mockRepo.On("GetUserByID", mock.Anything, userID).Return(user, nil)
	mockClerk.On("UpdateUserEmail", mock.Anything, "user_clerk123", "new@example.com").Return(nil)
	mockPaddle.On("UpdateCustomerEmail", mock.Anything, paddleCustomerID, "new@example.com").Return(nil)

	err := worker.Work(context.Background(), makeEmailPropJob(userID, "user_clerk123"))
	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockClerk.AssertExpectations(t)
	mockPaddle.AssertExpectations(t)
}

func TestEmailChangePropagationWorker_NoPaddleCustomer(t *testing.T) {
	userID := uuid.New().String()
	mockRepo := new(MockEmailPropagationProfileRepo)
	mockClerk := new(MockEmailPropagationClerkService)
	mockPaddle := new(MockEmailPropagationPaddleService)

	worker := NewEmailChangePropagationWorker(EmailChangePropagationWorkerDeps{
		ProfileRepo:   mockRepo,
		ClerkService:  mockClerk,
		PaddleService: mockPaddle,
	})

	user := &domain.User{
		ID:               userID,
		ClerkUserID:      "user_clerk123",
		Email:            "new@example.com",
		PaddleCustomerID: nil,
	}

	mockRepo.On("GetUserByID", mock.Anything, userID).Return(user, nil)
	mockClerk.On("UpdateUserEmail", mock.Anything, "user_clerk123", "new@example.com").Return(nil)

	err := worker.Work(context.Background(), makeEmailPropJob(userID, "user_clerk123"))
	assert.NoError(t, err)
	mockPaddle.AssertNotCalled(t, "UpdateCustomerEmail")
}

func TestEmailChangePropagationWorker_ClerkFailure_ReturnsError(t *testing.T) {
	userID := uuid.New().String()
	mockRepo := new(MockEmailPropagationProfileRepo)
	mockClerk := new(MockEmailPropagationClerkService)
	mockPaddle := new(MockEmailPropagationPaddleService)

	worker := NewEmailChangePropagationWorker(EmailChangePropagationWorkerDeps{
		ProfileRepo:   mockRepo,
		ClerkService:  mockClerk,
		PaddleService: mockPaddle,
	})

	user := &domain.User{
		ID:          userID,
		ClerkUserID: "user_clerk123",
		Email:       "new@example.com",
	}

	mockRepo.On("GetUserByID", mock.Anything, userID).Return(user, nil)
	mockClerk.On("UpdateUserEmail", mock.Anything, "user_clerk123", "new@example.com").
		Return(errors.New("clerk api error"))

	err := worker.Work(context.Background(), makeEmailPropJob(userID, "user_clerk123"))
	assert.Error(t, err)
}
