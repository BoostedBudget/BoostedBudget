package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// mockPaddleWebhookRepository is a mock for PaddleWebhookRepository
type mockPaddleWebhookRepository struct {
	mock.Mock
}

func (m *mockPaddleWebhookRepository) GetByID(ctx context.Context, id string) (*domain.PaddleWebhookEvent, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.PaddleWebhookEvent), args.Error(1)
}

func (m *mockPaddleWebhookRepository) MarkProcessed(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *mockPaddleWebhookRepository) MarkFailed(ctx context.Context, id string, errorMsg string) error {
	args := m.Called(ctx, id, errorMsg)
	return args.Error(0)
}

// mockSubscriptionProfileRepository is a mock for SubscriptionProfileRepository
type mockSubscriptionProfileRepository struct {
	mock.Mock
}

func (m *mockSubscriptionProfileRepository) GetUserByID(ctx context.Context, id string) (*domain.User, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

func (m *mockSubscriptionProfileRepository) UpdatePaddleCustomerID(ctx context.Context, userID, paddleCustomerID string) error {
	args := m.Called(ctx, userID, paddleCustomerID)
	return args.Error(0)
}

func (m *mockSubscriptionProfileRepository) UpdateSubscriptionFromPaddle(ctx context.Context, userID, tier, status string, startedAt, endsAt *time.Time) error {
	args := m.Called(ctx, userID, tier, status, startedAt, endsAt)
	return args.Error(0)
}

// mockClerkService is a mock for ClerkService
type mockClerkService struct {
	mock.Mock
}

func (m *mockClerkService) UpdateUserMetadata(ctx context.Context, clerkUserID string, metadata map[string]interface{}) error {
	args := m.Called(ctx, clerkUserID, metadata)
	return args.Error(0)
}

// loadWebhookFixture loads a webhook fixture from testdata
func loadWebhookFixture(t *testing.T, filename string) *domain.PaddleWebhookEvent {
	t.Helper()

	// Path relative to backend/ root (tests run from package directory, so go up 3 levels)
	testdataPath := filepath.Join("..", "..", "..", "testdata", "paddle", "webhooks", filename)
	data, err := os.ReadFile(testdataPath)
	require.NoError(t, err, "Failed to read fixture file: %s", filename)

	var webhookPayload map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &webhookPayload))

	eventID, _ := webhookPayload["event_id"].(string)
	eventType, _ := webhookPayload["event_type"].(string)

	return &domain.PaddleWebhookEvent{
		ID:             "test-webhook-id-123",
		EventID:        eventID,
		EventType:      eventType,
		Payload:        json.RawMessage(data),
		SignatureValid: true,
		Processed:      false,
	}
}

// setupWorkerTest creates a worker with mock dependencies
func setupWorkerTest(t *testing.T) (*SubscriptionWebhookWorker, *mockPaddleWebhookRepository, *mockSubscriptionProfileRepository, *mockClerkService) {
	t.Helper()

	mockWebhookRepo := new(mockPaddleWebhookRepository)
	mockProfileRepo := new(mockSubscriptionProfileRepository)
	mockClerk := new(mockClerkService)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	worker := NewSubscriptionWebhookWorker(SubscriptionWebhookWorkerDeps{
		PaddleWebhookRepo: mockWebhookRepo,
		ProfileRepo:       mockProfileRepo,
		ClerkService:      mockClerk,
		Logger:            logger,
	})

	return worker, mockWebhookRepo, mockProfileRepo, mockClerk
}

// createRiverJob creates a mock River job for testing
func createRiverJob(args SubscriptionWebhookArgs) *river.Job[SubscriptionWebhookArgs] {
	now := time.Now()
	return &river.Job[SubscriptionWebhookArgs]{
		JobRow: &rivertype.JobRow{
			ID:          123,
			Attempt:     1,
			CreatedAt:   now,
			Kind:        args.Kind(),
			MaxAttempts: 25,
			Priority:    1,
		},
		Args: args,
	}
}

// TestWorker_SubscriptionCreated_Success tests successful subscription.created processing
func TestWorker_SubscriptionCreated_Success(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk := setupWorkerTest(t)
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "subscription_created.json")

	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.created",
	}
	job := createRiverJob(args)

	user := &domain.User{
		ID:          "user_2abc123def456",
		ClerkUserID: "user_2abc123def456",
		Email:       "test@example.com",
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("UpdatePaddleCustomerID", ctx, "user_2abc123def456", "ctm_01h1234567890abcdef").Return(nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "active", mock.AnythingOfType("*time.Time"), mock.AnythingOfType("*time.Time")).Return(nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockClerk.On("UpdateUserMetadata", ctx, "user_2abc123def456", mock.MatchedBy(func(metadata map[string]interface{}) bool {
		return metadata["subscription_tier"] == "premium" && metadata["subscription_status"] == "active"
	})).Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	mockProfileRepo.AssertExpectations(t)
	mockClerk.AssertExpectations(t)
}

// TestWorker_SubscriptionCreated_UpdatesClerkMetadata tests Clerk metadata sync
func TestWorker_SubscriptionCreated_UpdatesClerkMetadata(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk := setupWorkerTest(t)
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "subscription_created.json")

	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.created",
	}
	job := createRiverJob(args)

	user := &domain.User{
		ID:          "user_2abc123def456",
		ClerkUserID: "clerk_user_123",
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("UpdatePaddleCustomerID", ctx, mock.Anything, mock.Anything).Return(nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)

	// Verify Clerk metadata update is called
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.MatchedBy(func(metadata map[string]interface{}) bool {
		tier, hasTier := metadata["subscription_tier"]
		status, hasStatus := metadata["subscription_status"]
		return hasTier && hasStatus && tier == "premium" && status == "active"
	})).Return(nil)

	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockClerk.AssertExpectations(t)
}

// TestWorker_SubscriptionActivated_Success tests subscription.activated event
func TestWorker_SubscriptionActivated_Success(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk := setupWorkerTest(t)
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "subscription_activated.json")

	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.activated",
	}
	job := createRiverJob(args)

	user := &domain.User{
		ID:          "user_2abc123def456",
		ClerkUserID: "user_2abc123def456",
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("UpdatePaddleCustomerID", ctx, mock.Anything, mock.Anything).Return(nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mockProfileRepo.On("GetUserByID", ctx, mock.Anything).Return(user, nil)
	mockClerk.On("UpdateUserMetadata", ctx, mock.Anything, mock.Anything).Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
}

// TestWorker_SubscriptionUpdated_Success tests subscription.updated event
func TestWorker_SubscriptionUpdated_Success(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, _ := setupWorkerTest(t)
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "subscription_updated.json")

	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.updated",
	}
	job := createRiverJob(args)

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", mock.Anything, "active", (*time.Time)(nil), mock.AnythingOfType("*time.Time")).Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	mockProfileRepo.AssertExpectations(t)
}

// TestWorker_SubscriptionCanceled_Success tests subscription.canceled event
func TestWorker_SubscriptionCanceled_Success(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, _ := setupWorkerTest(t)
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "subscription_canceled.json")

	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.canceled",
	}
	job := createRiverJob(args)

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(&domain.User{
		ID:               "user_2abc123def456",
		SubscriptionTier: "premium",
	}, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil)).Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
}

// TestWorker_TransactionCompleted_Success tests transaction.completed event
func TestWorker_TransactionCompleted_Success(t *testing.T) {
	worker, mockWebhookRepo, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "transaction_completed.json")

	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "transaction.completed",
	}
	job := createRiverJob(args)

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
}

// TestWorker_TransactionPaymentFailed_Success tests transaction.payment_failed event
func TestWorker_TransactionPaymentFailed_Success(t *testing.T) {
	worker, mockWebhookRepo, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "transaction_payment_failed.json")

	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "transaction.payment_failed",
	}
	job := createRiverJob(args)

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
}

// TestWorker_WebhookNotFound_CancelsJob tests missing webhook handling
func TestWorker_WebhookNotFound_CancelsJob(t *testing.T) {
	worker, mockWebhookRepo, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	args := SubscriptionWebhookArgs{
		WebhookID: "nonexistent-webhook-id",
		EventID:   "evt_nonexistent",
		EventType: "subscription.created",
	}
	job := createRiverJob(args)

	mockWebhookRepo.On("GetByID", ctx, "nonexistent-webhook-id").
		Return(nil, repository.ErrPaddleWebhookNotFound)

	err := worker.Work(ctx, job)

	// Should return nil to cancel the job (permanent failure)
	assert.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
}

// TestWorker_AlreadyProcessed_Idempotent tests idempotency
func TestWorker_AlreadyProcessed_Idempotent(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, _ := setupWorkerTest(t)
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "subscription_created.json")
	webhook.Processed = true // Already processed

	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.created",
	}
	job := createRiverJob(args)

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)

	err := worker.Work(ctx, job)

	// Should succeed without processing
	require.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	// Profile repo should NOT be called
	mockProfileRepo.AssertNotCalled(t, "UpdatePaddleCustomerID")
	mockProfileRepo.AssertNotCalled(t, "UpdateSubscriptionFromPaddle")
}

// TestWorker_UnknownEventType_SkipsProcessing tests unknown event handling
func TestWorker_UnknownEventType_SkipsProcessing(t *testing.T) {
	worker, mockWebhookRepo, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	webhook := &domain.PaddleWebhookEvent{
		ID:             "test-webhook-id-123",
		EventID:        "evt_unknown_type",
		EventType:      "subscription.unknown_event",
		Payload:        json.RawMessage(`{"data": {}}`),
		SignatureValid: true,
		Processed:      false,
	}

	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   "evt_unknown_type",
		EventType: "subscription.unknown_event",
	}
	job := createRiverJob(args)

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	// Should succeed (mark as processed even though not handled)
	require.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
}

// TestWorker_ProcessingError_MarksAsFailed tests error handling
func TestWorker_ProcessingError_MarksAsFailed(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, _ := setupWorkerTest(t)
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "subscription_created.json")

	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.created",
	}
	job := createRiverJob(args)

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("UpdatePaddleCustomerID", ctx, mock.Anything, mock.Anything).
		Return(errors.New("database connection failed"))
	mockWebhookRepo.On("MarkFailed", ctx, "test-webhook-id-123", mock.MatchedBy(func(errorMsg string) bool {
		return errorMsg != ""
	})).Return(nil)

	err := worker.Work(ctx, job)

	// Should return error for retry
	require.Error(t, err)
	assert.Contains(t, err.Error(), "webhook processing failed")
	mockWebhookRepo.AssertExpectations(t)
	mockProfileRepo.AssertExpectations(t)
}

// TestWorker_DatabaseError_ReturnsError tests temporary error handling
func TestWorker_DatabaseError_ReturnsError(t *testing.T) {
	worker, mockWebhookRepo, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   "evt_123",
		EventType: "subscription.created",
	}
	job := createRiverJob(args)

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").
		Return(nil, errors.New("database connection timeout"))

	err := worker.Work(ctx, job)

	// Should return error to trigger retry
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch Paddle webhook")
}

// TestWorker_MalformedPayload_ReturnsError tests invalid JSON handling
func TestWorker_MalformedPayload_ReturnsError(t *testing.T) {
	worker, mockWebhookRepo, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	webhook := &domain.PaddleWebhookEvent{
		ID:             "test-webhook-id-123",
		EventID:        "evt_malformed",
		EventType:      "subscription.created",
		Payload:        json.RawMessage(`{invalid json`),
		SignatureValid: true,
		Processed:      false,
	}

	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   "evt_malformed",
		EventType: "subscription.created",
	}
	job := createRiverJob(args)

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockWebhookRepo.On("MarkFailed", ctx, "test-webhook-id-123", mock.Anything).Return(nil)

	err := worker.Work(ctx, job)

	require.Error(t, err)
	mockWebhookRepo.AssertExpectations(t)
}

// TestWorker_MissingUserID_ReturnsError tests validation
func TestWorker_MissingUserID_ReturnsError(t *testing.T) {
	worker, mockWebhookRepo, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	// Create webhook without user_id in custom_data
	webhookJSON := `{
		"event_id": "evt_test",
		"event_type": "subscription.created",
		"data": {
			"id": "sub_123",
			"customer_id": "ctm_123",
			"custom_data": {}
		}
	}`

	webhook := &domain.PaddleWebhookEvent{
		ID:             "test-webhook-id-123",
		EventID:        "evt_test",
		EventType:      "subscription.created",
		Payload:        json.RawMessage(webhookJSON),
		SignatureValid: true,
		Processed:      false,
	}

	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   "evt_test",
		EventType: "subscription.created",
	}
	job := createRiverJob(args)

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockWebhookRepo.On("MarkFailed", ctx, "test-webhook-id-123", mock.MatchedBy(func(errorMsg string) bool {
		return errorMsg != ""
	})).Return(nil)

	err := worker.Work(ctx, job)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing user_id")
}

// TestNewSubscriptionWebhookWorker tests worker initialization
func TestNewSubscriptionWebhookWorker(t *testing.T) {
	mockWebhookRepo := new(mockPaddleWebhookRepository)
	mockProfileRepo := new(mockSubscriptionProfileRepository)
	mockClerk := new(mockClerkService)

	worker := NewSubscriptionWebhookWorker(SubscriptionWebhookWorkerDeps{
		PaddleWebhookRepo: mockWebhookRepo,
		ProfileRepo:       mockProfileRepo,
		ClerkService:      mockClerk,
		Logger:            nil, // Should default to slog.Default()
	})

	assert.NotNil(t, worker)
	assert.NotNil(t, worker.logger, "Logger should be initialized")
}

// TestSubscriptionWebhookArgs_Kind tests the job kind identifier
func TestSubscriptionWebhookArgs_Kind(t *testing.T) {
	args := SubscriptionWebhookArgs{}
	assert.Equal(t, "subscription_webhook", args.Kind())
}
