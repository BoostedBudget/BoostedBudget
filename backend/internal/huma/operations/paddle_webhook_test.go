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
func (m *mockPaddleWebhookRepository) ListWebhookEvents(ctx context.Context, filters domain.WebhookEventFilters, limit, offset int) ([]domain.PaddleWebhookEvent, int, error) { return nil, 0, nil }
func (m *mockPaddleWebhookRepository) GetWebhookStats(ctx context.Context, timeRangeHours int) (*domain.WebhookStatsResponse, error) { return nil, nil }
func (m *mockPaddleWebhookRepository) ResetProcessingStatus(ctx context.Context, id string) error { return nil }

// mockJobQueue is a mock implementation of queue.JobQueue
type mockJobQueue struct {
	enqueuedJobs []jobs.SubscriptionWebhookArgs
	enqueueCount int
	enqueueErr   error
}

func (m *mockJobQueue) Enqueue(ctx context.Context, job queue.Job, opts ...queue.EnqueueOption) (*queue.JobResult, error) {
	if m.enqueueErr != nil {
		return nil, m.enqueueErr
	}

	m.enqueueCount++

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

// TestHandlePaddleWebhook_InvalidSignature_RejectedBeforePersistence tests that a cryptographically
// invalid signature is rejected with a non-2xx error and NEVER persisted to the full webhook table
// (Issue #1514: fail closed before persistence — closes the idempotency-poisoning vector, since an
// invalid attempt carrying an attacker-chosen event_id never reaches the UNIQUE event_id column).
func TestHandlePaddleWebhook_InvalidSignature_RejectedBeforePersistence(t *testing.T) {
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

	require.Error(t, err, "invalid signature must be rejected with a non-2xx response")
	assert.Nil(t, output)

	// Verify the full webhook row was NEVER persisted, even with an attacker-chosen event_id.
	assert.Empty(t, mockRepo.events, "invalid-signature webhook must not be persisted to the full webhook table")
	assert.Empty(t, mockQueue.enqueuedJobs, "invalid-signature webhook must never be enqueued")
}

// TestHandlePaddleWebhook_MissingSignature_RejectedBeforePersistence tests webhook with missing
// signature header is rejected outright — never persisted, never enqueued (Issue #1514).
func TestHandlePaddleWebhook_MissingSignature_RejectedBeforePersistence(t *testing.T) {
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

	require.Error(t, err, "missing signature must be rejected with a non-2xx response")
	assert.Nil(t, output)

	assert.Empty(t, mockRepo.events, "unsigned webhook must not be persisted to the full webhook table")
	assert.Empty(t, mockQueue.enqueuedJobs)
}

// TestHandlePaddleWebhook_InvalidAttempt_RecordsBoundedAuditMetadata verifies that rejecting an
// invalid/unsigned webhook still records bounded metadata (Issue #1514 item 2) via the
// InvalidAttemptRepo — never the raw payload or the attacker-chosen event_id.
func TestHandlePaddleWebhook_InvalidAttempt_RecordsBoundedAuditMetadata(t *testing.T) {
	handler, mockService, _, _ := setupTestHandler()
	mockService.verifyResult = false
	invalidRepo := &mockInvalidWebhookAttemptRepository{}
	handler.invalidAttemptRepo = invalidRepo

	payload := PaddleWebhookPayload{
		EventID:   "evt_attacker_chosen",
		EventType: "subscription.created",
		Data:      map[string]interface{}{},
	}
	rawBody, _ := json.Marshal(payload)

	input := &PaddleWebhookInput{
		PaddleSignature: "ts=1614280549;h1=invalid_signature",
		RawBody:         rawBody,
	}

	_, err := handler.HandlePaddleWebhook(context.Background(), input)
	require.Error(t, err)

	require.Len(t, invalidRepo.calls, 1)
	assert.Equal(t, "paddle", invalidRepo.calls[0].provider)
	assert.Equal(t, len(rawBody), invalidRepo.calls[0].bodyLength)
	assert.NotContains(t, invalidRepo.calls[0].bodySHA256, "evt_attacker_chosen", "must never leak the raw event_id into the audit record")
}

// TestHandlePaddleWebhook_IdempotencyPoisoningClosed proves that an attacker-chosen event_id on an
// invalid webhook can no longer suppress a later LEGITIMATE webhook bearing the same event_id
// (Issue #1514 item — closes the idempotency-poisoning vector described in the security brief).
func TestHandlePaddleWebhook_IdempotencyPoisoningClosed(t *testing.T) {
	handler, mockService, mockRepo, mockQueue := setupTestHandler()
	mockRepo.getByEventErr = repository.ErrPaddleWebhookNotFound

	poisonedEventID := "evt_future_legit_event"
	payload := PaddleWebhookPayload{
		EventID:   poisonedEventID,
		EventType: "subscription.created",
		Data:      map[string]interface{}{},
	}
	rawBody, _ := json.Marshal(payload)

	// Step 1: attacker sends an invalid-signature webhook carrying the event_id they expect a
	// future legitimate webhook to use.
	mockService.verifyResult = false
	attackerInput := &PaddleWebhookInput{
		PaddleSignature: "ts=1614280549;h1=forged",
		RawBody:         rawBody,
	}
	_, err := handler.HandlePaddleWebhook(context.Background(), attackerInput)
	require.Error(t, err)
	require.Empty(t, mockRepo.events, "attacker's invalid attempt must not occupy the event_id")

	// Step 2: the real Paddle webhook, bearing the SAME event_id, arrives later with a valid
	// signature. It must be processed normally — not suppressed as a "duplicate".
	mockService.verifyResult = true
	legitInput := &PaddleWebhookInput{
		PaddleSignature: "ts=1614280600;h1=genuine",
		RawBody:         rawBody,
	}
	output, err := handler.HandlePaddleWebhook(context.Background(), legitInput)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "ok", output.Body.Status)
	require.Len(t, mockRepo.events, 1, "legitimate webhook must be stored")
	assert.Equal(t, poisonedEventID, mockRepo.events[0].EventID)
	assert.True(t, mockRepo.events[0].SignatureValid)
	assert.Len(t, mockQueue.enqueuedJobs, 1, "legitimate webhook must be enqueued for processing")
}

// TestHandlePaddleWebhook_ValidWebhookPersistenceFails_ReturnsNon2xx verifies that a signature-valid
// webhook we fail to persist returns a non-2xx response so Paddle retries delivery, instead of
// silently losing a real billing event forever (Issue #1514 item 5).
func TestHandlePaddleWebhook_ValidWebhookPersistenceFails_ReturnsNon2xx(t *testing.T) {
	handler, mockService, mockRepo, mockQueue := setupTestHandler()
	mockService.verifyResult = true
	mockRepo.getByEventErr = repository.ErrPaddleWebhookNotFound
	mockRepo.createErr = errors.New("db unavailable")

	payload := PaddleWebhookPayload{
		EventID:   "evt_persist_fail",
		EventType: "subscription.created",
		Data:      map[string]interface{}{},
	}
	rawBody, _ := json.Marshal(payload)

	input := &PaddleWebhookInput{
		PaddleSignature: "ts=1614280549;h1=valid",
		RawBody:         rawBody,
	}

	output, err := handler.HandlePaddleWebhook(context.Background(), input)

	require.Error(t, err, "a persistence failure for a VALID webhook must return a non-2xx response so Paddle retries")
	assert.Nil(t, output)
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

// TestHandlePaddleWebhook_ResultCodes exercises the response-code contract across scenarios:
// valid signature -> 2xx (unless persistence fails, which must be non-2xx per Issue #1514 item 5);
// enqueue failure alone -> still 2xx (the webhook itself was durably persisted).
func TestHandlePaddleWebhook_ResultCodes(t *testing.T) {
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
			expectSuccess: false, // Issue #1514: must return non-2xx so Paddle retries a lost VALID webhook
		},
		{
			name:          "Queue enqueue fails",
			setupRepo:     func(r *mockPaddleWebhookRepository) { r.getByEventErr = repository.ErrPaddleWebhookNotFound },
			setupService:  func(s *mockPaddleWebhookService) { s.verifyResult = true },
			setupQueue:    func(q *mockJobQueue) { q.enqueueErr = errors.New("queue full") },
			expectSuccess: true, // webhook itself was durably stored; enqueue is best-effort
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
			} else {
				require.Error(t, err)
				assert.Nil(t, output)
			}
		})
	}
}

// TestHandlePaddleWebhook_SignatureVerificationError tests signature verification error handling.
// Issue #1514: a signature-verification error means reject — never store, never enqueue.
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

	require.Error(t, err)
	assert.Nil(t, output)

	// Verify webhook was NOT stored
	assert.Empty(t, mockRepo.events)

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
