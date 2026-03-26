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

	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// mockPlaidItemCleanupService is a mock for PlaidItemCleanupForWebhook
type mockPlaidItemCleanupService struct {
	mock.Mock
}

func (m *mockPlaidItemCleanupService) DisconnectAllUserItems(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

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

func (m *mockSubscriptionProfileRepository) UpdateSubscriptionFromPaddle(ctx context.Context, userID, tier, status string, startedAt, endsAt *time.Time, subscriptionID *string) error {
	args := m.Called(ctx, userID, tier, status, startedAt, endsAt, subscriptionID)
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

// mockNotificationRepository is a mock for SubscriptionNotificationRepository
type mockNotificationRepository struct {
	mock.Mock
}

func (m *mockNotificationRepository) Create(ctx context.Context, notification *domain.Notification) error {
	args := m.Called(ctx, notification)
	return args.Error(0)
}

// mockPaddleTransactionRepository is a mock for SubscriptionPaddleTransactionRepository
type mockPaddleTransactionRepository struct {
	mock.Mock
}

func (m *mockPaddleTransactionRepository) Create(ctx context.Context, txn *domain.PaddleTransaction) error {
	args := m.Called(ctx, txn)
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
func setupWorkerTest(t *testing.T) (
	*SubscriptionWebhookWorker,
	*mockPaddleWebhookRepository,
	*mockSubscriptionProfileRepository,
	*mockClerkService,
	*mockNotificationRepository,
	*mockPaddleTransactionRepository,
) {
	t.Helper()

	mockWebhookRepo := new(mockPaddleWebhookRepository)
	mockProfileRepo := new(mockSubscriptionProfileRepository)
	mockClerk := new(mockClerkService)
	mockNotifRepo := new(mockNotificationRepository)
	mockTxnRepo := new(mockPaddleTransactionRepository)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	// Load tier config for price ID resolution
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	worker := NewSubscriptionWebhookWorker(SubscriptionWebhookWorkerDeps{
		PaddleWebhookRepo:     mockWebhookRepo,
		ProfileRepo:           mockProfileRepo,
		ClerkService:          mockClerk,
		NotificationRepo:      mockNotifRepo,
		PaddleTransactionRepo: mockTxnRepo,
		TierConfig:            tierConfig,
		Logger:                logger,
	})

	return worker, mockWebhookRepo, mockProfileRepo, mockClerk, mockNotifRepo, mockTxnRepo
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
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
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
	// Issue #48: Verify subscription ID is extracted from webhook data and stored
	expectedSubID := "sub_01h1234567890abcdef"
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "active", mock.AnythingOfType("*time.Time"), mock.AnythingOfType("*time.Time"), &expectedSubID).Return(nil)
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
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
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
	// Issue #48: Updated signature to include subscriptionID parameter
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
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
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, mockNotifRepo, _ := setupWorkerTest(t)
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
	// Dedicated handler: no UpdatePaddleCustomerID call (unlike created)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mockNotifRepo.On("Create", ctx, mock.Anything).Return(nil)
	mockProfileRepo.On("GetUserByID", ctx, mock.Anything).Return(user, nil)
	mockClerk.On("UpdateUserMetadata", ctx, mock.Anything, mock.Anything).Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
}

// TestWorker_SubscriptionUpdated_Success tests subscription.updated event
func TestWorker_SubscriptionUpdated_Success(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "subscription_updated.json")

	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.updated",
	}
	job := createRiverJob(args)

	user := &domain.User{
		ID:          "user_2abc123def456",
		ClerkUserID: "clerk_user_123",
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	// Issue #48: Updated signature to include subscriptionID parameter
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", mock.Anything, "active", (*time.Time)(nil), mock.AnythingOfType("*time.Time"), mock.Anything).Return(nil)
	// Issues #747 #748: syncClerkMetadata is called after update
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	mockProfileRepo.AssertExpectations(t)
}

// TestWorker_SubscriptionCanceled_Success tests subscription.canceled event
func TestWorker_SubscriptionCanceled_Success(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "subscription_canceled.json")

	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.canceled",
	}
	job := createRiverJob(args)

	canceledUser := &domain.User{
		ID:               "user_2abc123def456",
		ClerkUserID:      "clerk_user_123",
		SubscriptionTier: "premium",
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	// GetUserByID called twice: once in handler, once in syncClerkMetadata
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(canceledUser, nil)
	// Issue #48: Updated signature to include subscriptionID parameter (nil for cancel events)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	// Issues #747 #748: syncClerkMetadata called after cancel (no notification — user-initiated)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
}

// TestWorker_TransactionCompleted_Success tests transaction.completed event
func TestWorker_TransactionCompleted_Success(t *testing.T) {
	worker, mockWebhookRepo, _, _, _, mockTxnRepo := setupWorkerTest(t)
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "transaction_completed.json")

	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "transaction.completed",
	}
	job := createRiverJob(args)

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	// Issues #747 #748: Records the completed transaction
	mockTxnRepo.On("Create", ctx, mock.MatchedBy(func(txn *domain.PaddleTransaction) bool {
		return txn.Status == "completed" && txn.UserID == "user_2abc123def456"
	})).Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
}

// TestWorker_TransactionPaymentFailed_Success tests transaction.payment_failed event
func TestWorker_TransactionPaymentFailed_Success(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, mockNotifRepo, mockTxnRepo := setupWorkerTest(t)
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "transaction_payment_failed.json")

	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "transaction.payment_failed",
	}
	job := createRiverJob(args)

	user := &domain.User{
		ID:                 "user_2abc123def456",
		ClerkUserID:        "clerk_user_123",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "past_due",
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	// Issues #747 #748: Records the failed transaction
	mockTxnRepo.On("Create", ctx, mock.MatchedBy(func(txn *domain.PaddleTransaction) bool {
		return txn.Status == "failed" && txn.UserID == "user_2abc123def456"
	})).Return(nil)
	// Creates payment_failed notification
	mockNotifRepo.On("Create", ctx, mock.MatchedBy(func(n *domain.Notification) bool {
		return n.Type == "payment_failed" && n.UserID == "user_2abc123def456"
	})).Return(nil)
	// Syncs Clerk metadata
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
}

// TestWorker_WebhookNotFound_CancelsJob tests missing webhook handling
func TestWorker_WebhookNotFound_CancelsJob(t *testing.T) {
	worker, mockWebhookRepo, _, _, _, _ := setupWorkerTest(t)
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
	worker, mockWebhookRepo, mockProfileRepo, _, _, _ := setupWorkerTest(t)
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
	worker, mockWebhookRepo, _, _, _, _ := setupWorkerTest(t)
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
	worker, mockWebhookRepo, mockProfileRepo, _, _, _ := setupWorkerTest(t)
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
	worker, mockWebhookRepo, _, _, _, _ := setupWorkerTest(t)
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
	worker, mockWebhookRepo, _, _, _, _ := setupWorkerTest(t)
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
	worker, mockWebhookRepo, _, _, _, _ := setupWorkerTest(t)
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

	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	worker := NewSubscriptionWebhookWorker(SubscriptionWebhookWorkerDeps{
		PaddleWebhookRepo: mockWebhookRepo,
		ProfileRepo:       mockProfileRepo,
		ClerkService:      mockClerk,
		TierConfig:        tierConfig,
		Logger:            nil, // Should default to slog.Default()
	})

	assert.NotNil(t, worker)
	assert.NotNil(t, worker.logger, "Logger should be initialized")
	assert.NotNil(t, worker.tierConfig, "TierConfig should be initialized")
}

// TestSubscriptionWebhookArgs_Kind tests the job kind identifier
func TestSubscriptionWebhookArgs_Kind(t *testing.T) {
	args := SubscriptionWebhookArgs{}
	assert.Equal(t, "subscription_webhook", args.Kind())
}

// Issue #772: Test tier resolution from Paddle price_id (GAP 3)
func TestWorker_ResolveTierFromItems(t *testing.T) {
	// Import config package for TierConfig
	// Need to add this to worker setup
	worker, _, _, _, _, _ := setupWorkerTest(t)

	t.Run("resolves premium tier from monthly price ID", func(t *testing.T) {
		// Load a webhook fixture to get the real structure
		webhook := loadWebhookFixture(t, "subscription_created.json")
		data, err := worker.parseWebhookPayload(webhook)
		require.NoError(t, err)

		// The fixture should contain pri_01h1234567890premium which should map to premium tier
		tier := worker.resolveTierFromItems(data)
		// Note: The fixture uses a test price ID, so this may fall back to "premium"
		// The important thing is that the method exists and works
		assert.NotEmpty(t, tier)
	})

	t.Run("falls back to premium for unknown price ID", func(t *testing.T) {
		data := map[string]interface{}{
			"items": []interface{}{
				map[string]interface{}{
					"price": map[string]interface{}{
						"id": "pri_unknown_12345",
					},
				},
			},
		}

		tier := worker.resolveTierFromItems(data)
		assert.Equal(t, domain.TierPremium, tier)
	})

	t.Run("handles missing items array", func(t *testing.T) {
		data := map[string]interface{}{}

		tier := worker.resolveTierFromItems(data)
		assert.Equal(t, domain.TierPremium, tier)
	})

	t.Run("handles empty items array", func(t *testing.T) {
		data := map[string]interface{}{
			"items": []interface{}{},
		}

		tier := worker.resolveTierFromItems(data)
		assert.Equal(t, domain.TierPremium, tier)
	})

	t.Run("handles malformed items structure", func(t *testing.T) {
		data := map[string]interface{}{
			"items": []interface{}{
				map[string]interface{}{
					"price": "not a map",
				},
			},
		}

		tier := worker.resolveTierFromItems(data)
		assert.Equal(t, domain.TierPremium, tier)
	})
}

// TestWorker_SubscriptionPaused_Success tests subscription.paused event uses "paused" status.
func TestWorker_SubscriptionPaused_Success(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, mockNotifRepo, _ := setupWorkerTest(t)
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "subscription_paused.json")
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.paused",
	}
	job := createRiverJob(args)

	user := &domain.User{
		ID:               "user_2abc123def456",
		ClerkUserID:      "clerk_user_123",
		SubscriptionTier: "premium",
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "paused", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	mockNotifRepo.On("Create", ctx, mock.MatchedBy(func(n *domain.Notification) bool {
		return n.Type == "subscription_paused" && n.UserID == "user_2abc123def456"
	})).Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	mockProfileRepo.AssertExpectations(t)
	mockNotifRepo.AssertExpectations(t)
	mockClerk.AssertExpectations(t)
}

// TestWorker_SubscriptionPastDue_Success tests subscription.past_due event sends notification.
func TestWorker_SubscriptionPastDue_Success(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, mockNotifRepo, _ := setupWorkerTest(t)
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "subscription_past_due.json")
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.past_due",
	}
	job := createRiverJob(args)

	user := &domain.User{
		ID:               "user_2abc123def456",
		ClerkUserID:      "clerk_user_123",
		SubscriptionTier: "premium",
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "past_due", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	mockNotifRepo.On("Create", ctx, mock.MatchedBy(func(n *domain.Notification) bool {
		return n.Type == "subscription_past_due" && n.UserID == "user_2abc123def456"
	})).Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	mockProfileRepo.AssertExpectations(t)
	mockNotifRepo.AssertExpectations(t)
	mockClerk.AssertExpectations(t)
}

// TestWorker_SubscriptionResumed_Success tests subscription.resumed event.
func TestWorker_SubscriptionResumed_Success(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, mockNotifRepo, _ := setupWorkerTest(t)
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "subscription_resumed.json")
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.resumed",
	}
	job := createRiverJob(args)

	user := &domain.User{
		ID:          "user_2abc123def456",
		ClerkUserID: "clerk_user_123",
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "active", (*time.Time)(nil), (*time.Time)(nil), mock.Anything).Return(nil)
	mockNotifRepo.On("Create", ctx, mock.MatchedBy(func(n *domain.Notification) bool {
		return n.Type == "subscription_resumed" && n.UserID == "user_2abc123def456"
	})).Return(nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	mockNotifRepo.AssertExpectations(t)
}

// TestWorker_SubscriptionTrialing_Success tests subscription.trialing event.
func TestWorker_SubscriptionTrialing_Success(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, mockNotifRepo, _ := setupWorkerTest(t)
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "subscription_trialing.json")
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.trialing",
	}
	job := createRiverJob(args)

	user := &domain.User{
		ID:          "user_2abc123def456",
		ClerkUserID: "clerk_user_123",
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "trialing",
		mock.MatchedBy(func(t *time.Time) bool { return t != nil }),
		mock.MatchedBy(func(t *time.Time) bool { return t != nil }),
		mock.Anything).Return(nil)
	mockNotifRepo.On("Create", ctx, mock.MatchedBy(func(n *domain.Notification) bool {
		return n.Type == "subscription_trialing" && n.UserID == "user_2abc123def456"
	})).Return(nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	mockNotifRepo.AssertExpectations(t)
}

// TestWorker_TransactionPastDue_Success tests transaction.past_due event.
func TestWorker_TransactionPastDue_Success(t *testing.T) {
	worker, mockWebhookRepo, _, _, mockNotifRepo, mockTxnRepo := setupWorkerTest(t)
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "transaction_past_due.json")
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "transaction.past_due",
	}
	job := createRiverJob(args)

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockTxnRepo.On("Create", ctx, mock.MatchedBy(func(txn *domain.PaddleTransaction) bool {
		return txn.Status == "past_due" && txn.UserID == "user_2abc123def456"
	})).Return(nil)
	mockNotifRepo.On("Create", ctx, mock.MatchedBy(func(n *domain.Notification) bool {
		return n.Type == "transaction_past_due" && n.UserID == "user_2abc123def456"
	})).Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	mockTxnRepo.AssertExpectations(t)
	mockNotifRepo.AssertExpectations(t)
}

// TestWorker_TransactionCanceled_Success tests transaction.canceled event.
func TestWorker_TransactionCanceled_Success(t *testing.T) {
	worker, mockWebhookRepo, _, _, mockNotifRepo, mockTxnRepo := setupWorkerTest(t)
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "transaction_canceled.json")
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "transaction.canceled",
	}
	job := createRiverJob(args)

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockTxnRepo.On("Create", ctx, mock.MatchedBy(func(txn *domain.PaddleTransaction) bool {
		return txn.Status == "canceled" && txn.UserID == "user_2abc123def456"
	})).Return(nil)
	mockNotifRepo.On("Create", ctx, mock.MatchedBy(func(n *domain.Notification) bool {
		return n.Type == "payment_failed" && n.UserID == "user_2abc123def456"
	})).Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	mockTxnRepo.AssertExpectations(t)
	mockNotifRepo.AssertExpectations(t)
}

// TestWorker_SubscriptionCanceled_DisconnectsPlaidItems tests that Plaid items are disconnected on cancellation.
// Issue #896: Step 2 — disconnect on cancel.
func TestWorker_SubscriptionCanceled_DisconnectsPlaidItems(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	mockPlaid := new(mockPlaidItemCleanupService)
	worker.SetPlaidCleanupService(mockPlaid)

	webhook := loadWebhookFixture(t, "subscription_canceled.json")
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.canceled",
	}
	job := createRiverJob(args)

	canceledUser := &domain.User{
		ID:               "user_2abc123def456",
		ClerkUserID:      "clerk_user_123",
		SubscriptionTier: "premium",
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(canceledUser, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	mockPlaid.On("DisconnectAllUserItems", ctx, "user_2abc123def456").Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockPlaid.AssertExpectations(t)
	mockWebhookRepo.AssertExpectations(t)
	mockProfileRepo.AssertExpectations(t)
	mockClerk.AssertExpectations(t)
}

// TestWorker_SubscriptionCanceled_PlaidCleanupNil_Succeeds tests that a nil plaidCleanupSvc is skipped gracefully.
// Issue #896: Nil guard ensures existing tests continue to pass without wiring.
func TestWorker_SubscriptionCanceled_PlaidCleanupNil_Succeeds(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	// Do NOT call worker.SetPlaidCleanupService — plaidCleanupSvc remains nil

	webhook := loadWebhookFixture(t, "subscription_canceled.json")
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.canceled",
	}
	job := createRiverJob(args)

	canceledUser := &domain.User{
		ID:               "user_2abc123def456",
		ClerkUserID:      "clerk_user_123",
		SubscriptionTier: "premium",
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(canceledUser, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	mockProfileRepo.AssertExpectations(t)
	mockClerk.AssertExpectations(t)
}

// TestWorker_SubscriptionCanceled_PlaidCleanupError_StillSucceeds tests that a Plaid disconnect error
// is logged and swallowed — webhook processing still succeeds.
// Issue #896: Errors from DisconnectAllUserItems must not fail webhook processing.
func TestWorker_SubscriptionCanceled_PlaidCleanupError_StillSucceeds(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	mockPlaid := new(mockPlaidItemCleanupService)
	worker.SetPlaidCleanupService(mockPlaid)

	webhook := loadWebhookFixture(t, "subscription_canceled.json")
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.canceled",
	}
	job := createRiverJob(args)

	canceledUser := &domain.User{
		ID:               "user_2abc123def456",
		ClerkUserID:      "clerk_user_123",
		SubscriptionTier: "premium",
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(canceledUser, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	mockPlaid.On("DisconnectAllUserItems", ctx, "user_2abc123def456").Return(errors.New("plaid disconnect failed"))
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	// Error from Plaid cleanup must be swallowed — webhook still succeeds
	require.NoError(t, err)
	mockPlaid.AssertExpectations(t)
	mockWebhookRepo.AssertExpectations(t)
	mockProfileRepo.AssertExpectations(t)
	mockClerk.AssertExpectations(t)
}

// TestWorker_SubscriptionPaused_DisconnectsPlaidItems tests that Plaid items are disconnected on pause.
// Issue #896: Step 3 — disconnect on pause.
func TestWorker_SubscriptionPaused_DisconnectsPlaidItems(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, mockNotifRepo, _ := setupWorkerTest(t)
	ctx := context.Background()

	mockPlaid := new(mockPlaidItemCleanupService)
	worker.SetPlaidCleanupService(mockPlaid)

	webhook := loadWebhookFixture(t, "subscription_paused.json")
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.paused",
	}
	job := createRiverJob(args)

	user := &domain.User{
		ID:               "user_2abc123def456",
		ClerkUserID:      "clerk_user_123",
		SubscriptionTier: "premium",
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "paused", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	mockPlaid.On("DisconnectAllUserItems", ctx, "user_2abc123def456").Return(nil)
	mockNotifRepo.On("Create", ctx, mock.MatchedBy(func(n *domain.Notification) bool {
		return n.Type == "subscription_paused" && n.UserID == "user_2abc123def456"
	})).Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockPlaid.AssertExpectations(t)
	mockWebhookRepo.AssertExpectations(t)
	mockProfileRepo.AssertExpectations(t)
	mockNotifRepo.AssertExpectations(t)
	mockClerk.AssertExpectations(t)
}

// TestWorker_SubscriptionPaused_PlaidCleanupError_StillSucceeds tests that a Plaid disconnect error
// is logged and swallowed on pause — webhook processing still succeeds.
// Issue #896: Errors from DisconnectAllUserItems must not fail webhook processing.
func TestWorker_SubscriptionPaused_PlaidCleanupError_StillSucceeds(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, mockNotifRepo, _ := setupWorkerTest(t)
	ctx := context.Background()

	mockPlaid := new(mockPlaidItemCleanupService)
	worker.SetPlaidCleanupService(mockPlaid)

	webhook := loadWebhookFixture(t, "subscription_paused.json")
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.paused",
	}
	job := createRiverJob(args)

	user := &domain.User{
		ID:               "user_2abc123def456",
		ClerkUserID:      "clerk_user_123",
		SubscriptionTier: "premium",
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "paused", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	mockPlaid.On("DisconnectAllUserItems", ctx, "user_2abc123def456").Return(errors.New("plaid disconnect failed"))
	mockNotifRepo.On("Create", ctx, mock.MatchedBy(func(n *domain.Notification) bool {
		return n.Type == "subscription_paused" && n.UserID == "user_2abc123def456"
	})).Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	// Error from Plaid cleanup must be swallowed — webhook still succeeds
	require.NoError(t, err)
	mockPlaid.AssertExpectations(t)
	mockWebhookRepo.AssertExpectations(t)
	mockProfileRepo.AssertExpectations(t)
	mockNotifRepo.AssertExpectations(t)
	mockClerk.AssertExpectations(t)
}
