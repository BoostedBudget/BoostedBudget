package operations

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/queue/jobs"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPaddleWebhookService is a mock implementation of services.PaddleWebhookService
type mockPaddleWebhookService struct {
	verifyResult bool
	verifyErr    error
}

func (m *mockPaddleWebhookService) VerifyWebhook(signature, body string) (bool, error) {
	if m.verifyErr != nil {
		return false, m.verifyErr
	}
	return m.verifyResult, nil
}

// mockPaddleWebhookRepository is a mock implementation of repository.PaddleWebhookRepository
type mockPaddleWebhookRepository struct {
	events        []domain.PaddleWebhookEvent
	createErr     error
	getByEventErr error
	existing      *domain.PaddleWebhookEvent
}

func (m *mockPaddleWebhookRepository) Create(ctx context.Context, event *domain.PaddleWebhookEvent) error {
	if m.createErr != nil {
		return m.createErr
	}
	event.ID = "test-webhook-id-123"
	m.events = append(m.events, *event)
	return nil
}

func (m *mockPaddleWebhookRepository) GetByEventID(ctx context.Context, eventID string) (*domain.PaddleWebhookEvent, error) {
	if m.getByEventErr != nil {
		return nil, m.getByEventErr
	}
	return m.existing, nil
}

// Implement other required methods with no-ops (only testing Create and GetByEventID)
func (m *mockPaddleWebhookRepository) GetByID(ctx context.Context, id string) (*domain.PaddleWebhookEvent, error) { return nil, nil }
func (m *mockPaddleWebhookRepository) MarkProcessed(ctx context.Context, id string) error                           { return nil }
func (m *mockPaddleWebhookRepository) MarkFailed(ctx context.Context, id string, errorMsg string) error             { return nil }
func (m *mockPaddleWebhookRepository) GetUnprocessed(ctx context.Context, limit int) ([]domain.PaddleWebhookEvent, error) { return nil, nil }
func (m *mockPaddleWebhookRepository) GetUnprocessedByType(ctx context.Context, eventType string, limit int) ([]domain.PaddleWebhookEvent, error) { return nil, nil }
func (m *mockPaddleWebhookRepository) Update(ctx context.Context, event *domain.PaddleWebhookEvent) error { return nil }
func (m *mockPaddleWebhookRepository) GetRecentWebhooks(ctx context.Context, limit int) ([]domain.PaddleWebhookEvent, error) { return nil, nil }

// mockJobQueue is a mock implementation of queue.JobQueue
type mockJobQueue struct {
	enqueuedJobs []jobs.SubscriptionWebhookArgs
	enqueueErr   error
}

func (m *mockJobQueue) Enqueue(ctx context.Context, job queue.Job, opts ...queue.EnqueueOption) (*queue.JobResult, error) {
	if m.enqueueErr != nil {
		return nil, m.enqueueErr
	}

	if subscriptionArgs, ok := job.(jobs.SubscriptionWebhookArgs); ok {
		m.enqueuedJobs = append(m.enqueuedJobs, subscriptionArgs)
	}

	return &queue.JobResult{
		ID:    123,
		Kind:  job.Kind(),
		Queue: "default",
	}, nil
}

func (m *mockJobQueue) EnqueueTx(ctx context.Context, tx pgx.Tx, job queue.Job, opts ...queue.EnqueueOption) (*queue.JobResult, error) {
	return m.Enqueue(ctx, job, opts...)
}

func (m *mockJobQueue) EnqueueMany(ctx context.Context, jobs []queue.Job, opts ...queue.EnqueueOption) ([]*queue.JobResult, error) {
	results := make([]*queue.JobResult, 0, len(jobs))
	for _, job := range jobs {
		result, err := m.Enqueue(ctx, job, opts...)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

func (m *mockJobQueue) EnqueueManyTx(ctx context.Context, tx pgx.Tx, jobs []queue.Job, opts ...queue.EnqueueOption) ([]*queue.JobResult, error) {
	return m.EnqueueMany(ctx, jobs, opts...)
}

// setupTestHandler creates a handler with mock dependencies
func setupTestHandler() (*PaddleWebhookHandler, *mockPaddleWebhookService, *mockPaddleWebhookRepository, *mockJobQueue) {
	mockService := &mockPaddleWebhookService{verifyResult: true}
	mockRepo := &mockPaddleWebhookRepository{}
	mockQueue := &mockJobQueue{}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError, // Suppress logs during tests
	}))

	handler := NewPaddleWebhookHandler(PaddleWebhookHandlerDeps{
		WebhookService: mockService,
		WebhookRepo:    mockRepo,
		JobQueue:       mockQueue,
		Logger:         logger,
	})

	return handler, mockService, mockRepo, mockQueue
}

// TestHandlePaddleWebhook_ValidSignature_EnqueuesJob tests successful webhook processing with valid signature
func TestHandlePaddleWebhook_ValidSignature_EnqueuesJob(t *testing.T) {
	handler, mockService, mockRepo, mockQueue := setupTestHandler()
	mockService.verifyResult = true
	mockRepo.getByEventErr = repository.ErrPaddleWebhookNotFound // Not duplicate

	payload := PaddleWebhookPayload{
		EventID:   "evt_01h1234567890",
		EventType: "subscription.created",
		Data: map[string]interface{}{
			"id": "sub_123",
		},
	}
	rawBody, _ := json.Marshal(payload)

	input := &PaddleWebhookInput{
		PaddleSignature: "ts=1614280549;h1=valid_signature",
		RawBody:         rawBody,
	}

	output, err := handler.HandlePaddleWebhook(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "ok", output.Body.Status)
	assert.Contains(t, output.Body.Message, "Webhook received")

	// Verify webhook was stored
	assert.Len(t, mockRepo.events, 1)
	assert.Equal(t, "evt_01h1234567890", mockRepo.events[0].EventID)
	assert.Equal(t, "subscription.created", mockRepo.events[0].EventType)
	assert.True(t, mockRepo.events[0].SignatureValid)

	// Verify job was enqueued
	assert.Len(t, mockQueue.enqueuedJobs, 1)
	assert.Equal(t, "evt_01h1234567890", mockQueue.enqueuedJobs[0].EventID)
	assert.Equal(t, "subscription.created", mockQueue.enqueuedJobs[0].EventType)
}

// TestHandlePaddleWebhook_InvalidSignature_NoJobEnqueued tests that invalid signature prevents job enqueue
func TestHandlePaddleWebhook_InvalidSignature_NoJobEnqueued(t *testing.T) {
	handler, mockService, mockRepo, mockQueue := setupTestHandler()
	mockService.verifyResult = false
	mockRepo.getByEventErr = repository.ErrPaddleWebhookNotFound

	payload := PaddleWebhookPayload{
		EventID:   "evt_01h1234567890",
		EventType: "subscription.created",
		Data:      map[string]interface{}{},
	}
	rawBody, _ := json.Marshal(payload)

	input := &PaddleWebhookInput{
		PaddleSignature: "ts=1614280549;h1=invalid_signature",
		RawBody:         rawBody,
	}

	output, err := handler.HandlePaddleWebhook(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "ok", output.Body.Status)

	// Verify webhook was stored
	assert.Len(t, mockRepo.events, 1)
	assert.False(t, mockRepo.events[0].SignatureValid)

	// Verify job was NOT enqueued
	assert.Empty(t, mockQueue.enqueuedJobs, "Job should not be enqueued with invalid signature")
}

// TestHandlePaddleWebhook_MissingSignature_StillProcesses tests webhook with missing signature
func TestHandlePaddleWebhook_MissingSignature_StillProcesses(t *testing.T) {
	handler, _, mockRepo, mockQueue := setupTestHandler()
	mockRepo.getByEventErr = repository.ErrPaddleWebhookNotFound

	payload := PaddleWebhookPayload{
		EventID:   "evt_01h1234567890",
		EventType: "transaction.completed",
		Data:      map[string]interface{}{},
	}
	rawBody, _ := json.Marshal(payload)

	input := &PaddleWebhookInput{
		PaddleSignature: "", // No signature
		RawBody:         rawBody,
	}

	output, err := handler.HandlePaddleWebhook(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "ok", output.Body.Status)

	// Verify webhook was stored
	assert.Len(t, mockRepo.events, 1)
	assert.False(t, mockRepo.events[0].SignatureValid)

	// Verify job was NOT enqueued (no valid signature)
	assert.Empty(t, mockQueue.enqueuedJobs)
}

// TestHandlePaddleWebhook_DuplicateEvent_ReturnsSuccess tests idempotency
func TestHandlePaddleWebhook_DuplicateEvent_ReturnsSuccess(t *testing.T) {
	handler, mockService, mockRepo, mockQueue := setupTestHandler()
	mockService.verifyResult = true

	// Set up existing webhook
	mockRepo.existing = &domain.PaddleWebhookEvent{
		ID:        "existing-webhook-id",
		EventID:   "evt_01h1234567890",
		EventType: "subscription.created",
		Processed: true,
	}
	mockRepo.getByEventErr = nil // Webhook found

	payload := PaddleWebhookPayload{
		EventID:   "evt_01h1234567890",
		EventType: "subscription.created",
		Data:      map[string]interface{}{},
	}
	rawBody, _ := json.Marshal(payload)

	input := &PaddleWebhookInput{
		PaddleSignature: "ts=1614280549;h1=valid_signature",
		RawBody:         rawBody,
	}

	output, err := handler.HandlePaddleWebhook(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "ok", output.Body.Status)
	assert.Contains(t, output.Body.Message, "already received")

	// Verify NO new webhook was stored
	assert.Empty(t, mockRepo.events, "Should not create duplicate webhook")

	// Verify NO job was enqueued
	assert.Empty(t, mockQueue.enqueuedJobs, "Should not enqueue duplicate")
}

// TestHandlePaddleWebhook_PendingDuplicate tests duplicate webhook still being processed
func TestHandlePaddleWebhook_PendingDuplicate(t *testing.T) {
	handler, mockService, mockRepo, mockQueue := setupTestHandler()
	mockService.verifyResult = true

	// Set up existing webhook that's pending
	mockRepo.existing = &domain.PaddleWebhookEvent{
		ID:        "existing-webhook-id",
		EventID:   "evt_01h1234567890",
		EventType: "subscription.created",
		Processed: false, // Still processing
	}
	mockRepo.getByEventErr = nil

	payload := PaddleWebhookPayload{
		EventID:   "evt_01h1234567890",
		EventType: "subscription.created",
		Data:      map[string]interface{}{},
	}
	rawBody, _ := json.Marshal(payload)

	input := &PaddleWebhookInput{
		PaddleSignature: "ts=1614280549;h1=valid_signature",
		RawBody:         rawBody,
	}

	output, err := handler.HandlePaddleWebhook(context.Background(), input)

	require.NoError(t, err)
	assert.Equal(t, "ok", output.Body.Status)
	assert.Empty(t, mockRepo.events)
	assert.Empty(t, mockQueue.enqueuedJobs)
}

// TestHandlePaddleWebhook_EnqueueFails_StillReturnsOK tests that enqueue failure doesn't fail webhook
func TestHandlePaddleWebhook_EnqueueFails_StillReturnsOK(t *testing.T) {
	handler, mockService, mockRepo, mockQueue := setupTestHandler()
	mockService.verifyResult = true
	mockRepo.getByEventErr = repository.ErrPaddleWebhookNotFound
	mockQueue.enqueueErr = errors.New("queue is full")

	payload := PaddleWebhookPayload{
		EventID:   "evt_01h1234567890",
		EventType: "subscription.created",
		Data:      map[string]interface{}{},
	}
	rawBody, _ := json.Marshal(payload)

	input := &PaddleWebhookInput{
		PaddleSignature: "ts=1614280549;h1=valid_signature",
		RawBody:         rawBody,
	}

	output, err := handler.HandlePaddleWebhook(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "ok", output.Body.Status)

	// Verify webhook was still stored
	assert.Len(t, mockRepo.events, 1)
}

// TestHandlePaddleWebhook_ValidSubscriptionCreated tests subscription.created event
func TestHandlePaddleWebhook_ValidSubscriptionCreated(t *testing.T) {
	handler, mockService, mockRepo, mockQueue := setupTestHandler()
	mockService.verifyResult = true
	mockRepo.getByEventErr = repository.ErrPaddleWebhookNotFound

	payload := PaddleWebhookPayload{
		EventID:   "evt_01h1234567890",
		EventType: "subscription.created",
		Data: map[string]interface{}{
			"id":     "sub_123",
			"status": "active",
		},
	}
	rawBody, _ := json.Marshal(payload)

	input := &PaddleWebhookInput{
		PaddleSignature: "ts=1614280549;h1=valid_signature",
		RawBody:         rawBody,
	}

	output, err := handler.HandlePaddleWebhook(context.Background(), input)

	require.NoError(t, err)
	assert.Equal(t, "ok", output.Body.Status)
	assert.Len(t, mockRepo.events, 1)
	assert.Equal(t, "subscription.created", mockRepo.events[0].EventType)
	assert.Len(t, mockQueue.enqueuedJobs, 1)
}

// TestHandlePaddleWebhook_ValidTransactionCompleted tests transaction.completed event
func TestHandlePaddleWebhook_ValidTransactionCompleted(t *testing.T) {
	handler, mockService, mockRepo, mockQueue := setupTestHandler()
	mockService.verifyResult = true
	mockRepo.getByEventErr = repository.ErrPaddleWebhookNotFound

	payload := PaddleWebhookPayload{
		EventID:   "evt_01h9876543210",
		EventType: "transaction.completed",
		Data: map[string]interface{}{
			"id":     "txn_123",
			"status": "completed",
		},
	}
	rawBody, _ := json.Marshal(payload)

	input := &PaddleWebhookInput{
		PaddleSignature: "ts=1614280549;h1=valid_signature",
		RawBody:         rawBody,
	}

	output, err := handler.HandlePaddleWebhook(context.Background(), input)

	require.NoError(t, err)
	assert.Equal(t, "ok", output.Body.Status)
	assert.Len(t, mockRepo.events, 1)
	assert.Equal(t, "transaction.completed", mockRepo.events[0].EventType)
	assert.Len(t, mockQueue.enqueuedJobs, 1)
}

// TestHandlePaddleWebhook_InvalidJSON tests handling of malformed JSON
func TestHandlePaddleWebhook_InvalidJSON(t *testing.T) {
	handler, _, mockRepo, mockQueue := setupTestHandler()

	input := &PaddleWebhookInput{
		PaddleSignature: "ts=1614280549;h1=valid_signature",
		RawBody:         []byte("not valid json {{{"),
	}

	output, err := handler.HandlePaddleWebhook(context.Background(), input)

	require.NoError(t, err, "Should return OK even with invalid JSON")
	require.NotNil(t, output)
	assert.Equal(t, "error", output.Body.Status)
	assert.Contains(t, output.Body.Message, "Invalid JSON")

	// Should not store or enqueue
	assert.Empty(t, mockRepo.events)
	assert.Empty(t, mockQueue.enqueuedJobs)
}

// TestHandlePaddleWebhook_MissingEventID tests webhook with missing event_id
func TestHandlePaddleWebhook_MissingEventID(t *testing.T) {
	handler, mockService, mockRepo, mockQueue := setupTestHandler()
	mockService.verifyResult = true

	payload := PaddleWebhookPayload{
		EventID:   "", // Missing
		EventType: "subscription.created",
		Data:      map[string]interface{}{},
	}
	rawBody, _ := json.Marshal(payload)

	input := &PaddleWebhookInput{
		PaddleSignature: "ts=1614280549;h1=valid_signature",
		RawBody:         rawBody,
	}

	output, err := handler.HandlePaddleWebhook(context.Background(), input)

	require.Error(t, err, "Should return error for missing event_id")
	assert.Nil(t, output)

	// Should not store or enqueue
	assert.Empty(t, mockRepo.events)
	assert.Empty(t, mockQueue.enqueuedJobs)
}

// TestHandlePaddleWebhook_MissingEventType tests webhook with missing event_type
func TestHandlePaddleWebhook_MissingEventType(t *testing.T) {
	handler, mockService, mockRepo, mockQueue := setupTestHandler()
	mockService.verifyResult = true

	payload := PaddleWebhookPayload{
		EventID:   "evt_01h1234567890",
		EventType: "", // Missing
		Data:      map[string]interface{}{},
	}
	rawBody, _ := json.Marshal(payload)

	input := &PaddleWebhookInput{
		PaddleSignature: "ts=1614280549;h1=valid_signature",
		RawBody:         rawBody,
	}

	output, err := handler.HandlePaddleWebhook(context.Background(), input)

	require.Error(t, err, "Should return error for missing event_type")
	assert.Nil(t, output)

	// Should not store or enqueue
	assert.Empty(t, mockRepo.events)
	assert.Empty(t, mockQueue.enqueuedJobs)
}

// TestHandlePaddleWebhook_Always200OK tests that webhook always returns 200 to prevent retry storms
func TestHandlePaddleWebhook_Always200OK(t *testing.T) {
	testCases := []struct {
		name          string
		setupRepo     func(*mockPaddleWebhookRepository)
		setupService  func(*mockPaddleWebhookService)
		setupQueue    func(*mockJobQueue)
		expectSuccess bool
	}{
		{
			name:          "Valid signature",
			setupRepo:     func(r *mockPaddleWebhookRepository) { r.getByEventErr = repository.ErrPaddleWebhookNotFound },
			setupService:  func(s *mockPaddleWebhookService) { s.verifyResult = true },
			setupQueue:    func(q *mockJobQueue) {},
			expectSuccess: true,
		},
		{
			name:          "Repository create fails",
			setupRepo:     func(r *mockPaddleWebhookRepository) { r.createErr = errors.New("db error"); r.getByEventErr = repository.ErrPaddleWebhookNotFound },
			setupService:  func(s *mockPaddleWebhookService) { s.verifyResult = true },
			setupQueue:    func(q *mockJobQueue) {},
			expectSuccess: true, // Still returns OK
		},
		{
			name:          "Queue enqueue fails",
			setupRepo:     func(r *mockPaddleWebhookRepository) { r.getByEventErr = repository.ErrPaddleWebhookNotFound },
			setupService:  func(s *mockPaddleWebhookService) { s.verifyResult = true },
			setupQueue:    func(q *mockJobQueue) { q.enqueueErr = errors.New("queue full") },
			expectSuccess: true, // Still returns OK
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			handler, mockService, mockRepo, mockQueue := setupTestHandler()
			tc.setupRepo(mockRepo)
			tc.setupService(mockService)
			tc.setupQueue(mockQueue)

			payload := PaddleWebhookPayload{
				EventID:   "evt_01h1234567890",
				EventType: "subscription.created",
				Data:      map[string]interface{}{},
			}
			rawBody, _ := json.Marshal(payload)

			input := &PaddleWebhookInput{
				PaddleSignature: "ts=1614280549;h1=signature",
				RawBody:         rawBody,
			}

			output, err := handler.HandlePaddleWebhook(context.Background(), input)

			if tc.expectSuccess {
				require.NoError(t, err)
				require.NotNil(t, output)
				assert.Equal(t, "ok", output.Body.Status)
			}
		})
	}
}

// TestHandlePaddleWebhook_SignatureVerificationError tests signature verification error handling
func TestHandlePaddleWebhook_SignatureVerificationError(t *testing.T) {
	handler, mockService, mockRepo, mockQueue := setupTestHandler()
	mockService.verifyErr = errors.New("signature parsing error")
	mockRepo.getByEventErr = repository.ErrPaddleWebhookNotFound

	payload := PaddleWebhookPayload{
		EventID:   "evt_01h1234567890",
		EventType: "subscription.created",
		Data:      map[string]interface{}{},
	}
	rawBody, _ := json.Marshal(payload)

	input := &PaddleWebhookInput{
		PaddleSignature: "malformed_signature",
		RawBody:         rawBody,
	}

	output, err := handler.HandlePaddleWebhook(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "ok", output.Body.Status)

	// Verify webhook was stored with signature_valid = false
	assert.Len(t, mockRepo.events, 1)
	assert.False(t, mockRepo.events[0].SignatureValid)

	// Verify NO job was enqueued (invalid signature)
	assert.Empty(t, mockQueue.enqueuedJobs)
}

// TestNewPaddleWebhookHandler tests handler initialization
func TestNewPaddleWebhookHandler(t *testing.T) {
	mockService := &mockPaddleWebhookService{}
	mockRepo := &mockPaddleWebhookRepository{}
	mockQueue := &mockJobQueue{}

	handler := NewPaddleWebhookHandler(PaddleWebhookHandlerDeps{
		WebhookService: mockService,
		WebhookRepo:    mockRepo,
		JobQueue:       mockQueue,
		Logger:         nil, // Should default to slog.Default()
	})

	assert.NotNil(t, handler)
	assert.NotNil(t, handler.logger, "Logger should be initialized")
}
