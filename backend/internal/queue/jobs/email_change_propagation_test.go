package jobs

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type MockAnalytics struct {
	mock.Mock
}

func (m *MockAnalytics) Capture(distinctID, event string, properties map[string]interface{}) {
	m.Called(distinctID, event, properties)
}

func (m *MockAnalytics) Identify(distinctID string, properties map[string]interface{}) {
	m.Called(distinctID, properties)
}

func (m *MockAnalytics) Close() {
	m.Called()
}

var _ phclient.Analytics = (*MockAnalytics)(nil)

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

func TestEmailChangePropagationWorker_NilPaddleService(t *testing.T) {
	userID := uuid.New().String()
	paddleCustomerID := "cus_abc123"
	mockRepo := new(MockEmailPropagationProfileRepo)
	mockClerk := new(MockEmailPropagationClerkService)

	worker := NewEmailChangePropagationWorker(EmailChangePropagationWorkerDeps{
		ProfileRepo:   mockRepo,
		ClerkService:  mockClerk,
		PaddleService: nil, // explicitly nil — no Paddle configured
	})

	user := &domain.User{
		ID:               userID,
		ClerkUserID:      "user_clerk123",
		Email:            "new@example.com",
		PaddleCustomerID: &paddleCustomerID,
	}

	mockRepo.On("GetUserByID", mock.Anything, userID).Return(user, nil)
	mockClerk.On("UpdateUserEmail", mock.Anything, "user_clerk123", "new@example.com").Return(nil)

	err := worker.Work(context.Background(), makeEmailPropJob(userID, "user_clerk123"))
	assert.NoError(t, err)
	// Clerk was called
	mockClerk.AssertCalled(t, "UpdateUserEmail", mock.Anything, "user_clerk123", "new@example.com")
	// Paddle was NOT called (service is nil — verified by no mock setup and AssertExpectations)
	mockRepo.AssertExpectations(t)
	mockClerk.AssertExpectations(t)
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

func TestEmailChangePropagationWorker_FiresAnalyticsEvent(t *testing.T) {
	userID := uuid.New().String()
	paddleCustomerID := "ctm_analytics123"
	mockRepo := new(MockEmailPropagationProfileRepo)
	mockClerk := new(MockEmailPropagationClerkService)
	mockPaddle := new(MockEmailPropagationPaddleService)
	mockAnalytics := new(MockAnalytics)

	worker := NewEmailChangePropagationWorker(EmailChangePropagationWorkerDeps{
		ProfileRepo:   mockRepo,
		ClerkService:  mockClerk,
		PaddleService: mockPaddle,
	})
	worker.SetAnalytics(mockAnalytics)

	user := &domain.User{
		ID:               userID,
		ClerkUserID:      "user_clerk123",
		Email:            "new@example.com",
		PaddleCustomerID: &paddleCustomerID,
	}

	mockRepo.On("GetUserByID", mock.Anything, userID).Return(user, nil)
	mockClerk.On("UpdateUserEmail", mock.Anything, "user_clerk123", "new@example.com").Return(nil)
	mockPaddle.On("UpdateCustomerEmail", mock.Anything, paddleCustomerID, "new@example.com").Return(nil)
	mockAnalytics.On("Capture", userID, phclient.EventEmailChanged, mock.MatchedBy(func(props map[string]interface{}) bool {
		email, _ := props["email"].(string)
		propagatedToClerk, _ := props["propagated_to_clerk"].(bool)
		propagatedToPaddle, _ := props["propagated_to_paddle"].(bool)
		return email == "new@example.com" && propagatedToClerk && propagatedToPaddle
	})).Return()

	err := worker.Work(context.Background(), makeEmailPropJob(userID, "user_clerk123"))
	assert.NoError(t, err)
	mockAnalytics.AssertExpectations(t)
}

func TestEmailChangePropagationWorker_NilAnalytics_NoPanic(t *testing.T) {
	userID := uuid.New().String()
	paddleCustomerID := "ctm_nopanic123"
	mockRepo := new(MockEmailPropagationProfileRepo)
	mockClerk := new(MockEmailPropagationClerkService)
	mockPaddle := new(MockEmailPropagationPaddleService)

	// No SetAnalytics call — analytics remains nil
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

	assert.NotPanics(t, func() {
		err := worker.Work(context.Background(), makeEmailPropJob(userID, "user_clerk123"))
		assert.NoError(t, err)
	})
}
