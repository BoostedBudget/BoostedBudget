package operations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockWebhookService is a mock implementation of services.WebhookService
type mockWebhookService struct {
	verifyResult bool
	verifyErr    error
}

func (m *mockWebhookService) VerifyWebhook(ctx context.Context, rawBody []byte, signature string) (bool, error) {
	if m.verifyErr != nil {
		return false, m.verifyErr
	}
	return m.verifyResult, nil
}

// mockWebhookRepository is a mock implementation of repository.WebhookRepository
type mockWebhookRepository struct {
	events     []domain.WebhookEvent
	createErr  error
	processErr error
}

func (m *mockWebhookRepository) Create(ctx context.Context, event *domain.WebhookEvent) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.events = append(m.events, *event)
	return nil
}

func (m *mockWebhookRepository) MarkProcessed(ctx context.Context, id string) error {
	return m.processErr
}

func (m *mockWebhookRepository) GetUnprocessed(ctx context.Context) ([]domain.WebhookEvent, error) {
	return m.events, nil
}

func (m *mockWebhookRepository) GetUnprocessedByType(ctx context.Context, webhookType string, limit int) ([]domain.WebhookEvent, error) {
	return m.events, nil
}

func (m *mockWebhookRepository) IncrementRetryCount(ctx context.Context, id string) error {
	return nil
}

func (m *mockWebhookRepository) GetByID(ctx context.Context, id string) (*domain.WebhookEvent, error) {
	return nil, nil
}

func (m *mockWebhookRepository) GetByItemID(ctx context.Context, itemID string, limit int) ([]domain.WebhookEvent, error) {
	return nil, nil
}

func (m *mockWebhookRepository) Update(ctx context.Context, event *domain.WebhookEvent) error {
	return nil
}

// TestHandlePlaidWebhook_NilRepo_AcksWithoutPersisting_DocumentedUnreachable pins the current
// behaviour of the `h.webhookRepo != nil` guard in HandlePlaidWebhook (Issue #1514 item 3): with a
// nil WebhookRepo, a verified webhook is acked 200 without ever being persisted. This configuration
// is unreachable in the wired deployment — see the doc comment above the guard in plaid_webhook.go
// referencing backend/cmd/server/main.go:2001-2043, where both registration branches always
// construct and inject a non-nil repository before the route is ever registered. This test exists
// so that IF that wiring invariant is ever broken, the resulting behaviour change here is a
// deliberate, reviewed choice rather than a silent regression.
func TestHandlePlaidWebhook_NilRepo_AcksWithoutPersisting_DocumentedUnreachable(t *testing.T) {
	mockService := &mockWebhookService{verifyResult: true}

	handler := NewPlaidWebhookHandler(PlaidWebhookHandlerDeps{
		WebhookService: mockService,
		WebhookRepo:    nil,
	})

	payload := PlaidWebhookPayload{
		WebhookType: "TRANSACTIONS",
		WebhookCode: "DEFAULT_UPDATE",
		ItemID:      "item-123",
	}
	rawBody, _ := json.Marshal(payload)

	input := &PlaidWebhookInput{
		PlaidVerification: "valid-jwt-signature",
		RawBody:           rawBody,
	}

	output, err := handler.HandlePlaidWebhook(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "ok", output.Body.Status)
}

// TestHandlePlaidWebhook_TransactionsUpdate tests handling of TRANSACTIONS webhook.
func TestHandlePlaidWebhook_TransactionsUpdate(t *testing.T) {
	mockService := &mockWebhookService{verifyResult: true}
	mockRepo := &mockWebhookRepository{}

	handler := NewPlaidWebhookHandler(PlaidWebhookHandlerDeps{
		WebhookService: mockService,
		WebhookRepo:    mockRepo,
	})

	payload := PlaidWebhookPayload{
		WebhookType:     "TRANSACTIONS",
		WebhookCode:     "DEFAULT_UPDATE",
		ItemID:          "item-123",
		NewTransactions: 5,
	}
	rawBody, _ := json.Marshal(payload)

	input := &PlaidWebhookInput{
		PlaidVerification: "valid-jwt-signature",
		RawBody:           rawBody,
	}

	output, err := handler.HandlePlaidWebhook(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "ok", output.Body.Status)
	assert.Contains(t, output.Body.Message, "update")

	// Verify webhook was stored
	assert.Len(t, mockRepo.events, 1)
	assert.Equal(t, "TRANSACTIONS", mockRepo.events[0].WebhookType)
}

// TestHandlePlaidWebhook_ItemError tests handling of ITEM error webhook.
func TestHandlePlaidWebhook_ItemError(t *testing.T) {
	mockService := &mockWebhookService{verifyResult: true}
	mockRepo := &mockWebhookRepository{}

	handler := NewPlaidWebhookHandler(PlaidWebhookHandlerDeps{
		WebhookService: mockService,
		WebhookRepo:    mockRepo,
	})

	payload := PlaidWebhookPayload{
		WebhookType: "ITEM",
		WebhookCode: "LOGIN_REQUIRED",
		ItemID:      "item-123",
	}
	rawBody, _ := json.Marshal(payload)

	input := &PlaidWebhookInput{
		PlaidVerification: "valid-jwt-signature",
		RawBody:           rawBody,
	}

	output, err := handler.HandlePlaidWebhook(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "ok", output.Body.Status)
	assert.Contains(t, output.Body.Message, "Login required")
}

// TestHandlePlaidWebhook_InvalidJSON tests handling of invalid JSON payload — with a VALID
// signature, since Issue #1514 checks the signature first and this test targets the JSON-parsing
// branch specifically.
func TestHandlePlaidWebhook_InvalidJSON(t *testing.T) {
	mockService := &mockWebhookService{verifyResult: true}
	mockRepo := &mockWebhookRepository{}

	handler := NewPlaidWebhookHandler(PlaidWebhookHandlerDeps{
		WebhookService: mockService,
		WebhookRepo:    mockRepo,
	})

	input := &PlaidWebhookInput{
		PlaidVerification: "valid-jwt-signature",
		RawBody:           []byte("not valid json"),
	}

	output, err := handler.HandlePlaidWebhook(context.Background(), input)

	require.NoError(t, err) // Should still return 200 OK
	require.NotNil(t, output)
	assert.Equal(t, "error", output.Body.Status)
	assert.Contains(t, output.Body.Message, "Invalid JSON")
}

// TestHandlePlaidWebhook_MissingFields tests handling of missing required fields — with a VALID
// signature, so this exercises the field-validation branch rather than the signature check.
func TestHandlePlaidWebhook_MissingFields(t *testing.T) {
	mockService := &mockWebhookService{verifyResult: true}
	mockRepo := &mockWebhookRepository{}

	handler := NewPlaidWebhookHandler(PlaidWebhookHandlerDeps{
		WebhookService: mockService,
		WebhookRepo:    mockRepo,
	})

	payload := PlaidWebhookPayload{
		WebhookType: "", // Missing required field
		WebhookCode: "",
	}
	rawBody, _ := json.Marshal(payload)

	input := &PlaidWebhookInput{
		PlaidVerification: "valid-jwt-signature",
		RawBody:           rawBody,
	}

	output, err := handler.HandlePlaidWebhook(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Missing required fields")
}

// TestHandlePlaidWebhook_InvalidSignature_RejectedBeforePersistence tests that a cryptographically
// invalid signature is rejected outright — never persisted, never enqueued (Issue #1514).
func TestHandlePlaidWebhook_InvalidSignature_RejectedBeforePersistence(t *testing.T) {
	mockService := &mockWebhookService{verifyResult: false}
	mockRepo := &mockWebhookRepository{}

	handler := NewPlaidWebhookHandler(PlaidWebhookHandlerDeps{
		WebhookService: mockService,
		WebhookRepo:    mockRepo,
	})

	payload := PlaidWebhookPayload{
		WebhookType: "TRANSACTIONS",
		WebhookCode: "DEFAULT_UPDATE",
		ItemID:      "item-123",
	}
	rawBody, _ := json.Marshal(payload)

	input := &PlaidWebhookInput{
		PlaidVerification: "invalid-signature",
		RawBody:           rawBody,
	}

	output, err := handler.HandlePlaidWebhook(context.Background(), input)

	require.Error(t, err, "invalid signature must be rejected with a non-2xx response")
	assert.Nil(t, output)
	assert.Empty(t, mockRepo.events, "invalid-signature webhook must not be persisted to the full webhook table")
}

// TestHandlePlaidWebhook_NoService_Rejected tests that a missing webhook service is treated as
// "cannot verify" and fails closed — Issue #1514.
func TestHandlePlaidWebhook_NoService_Rejected(t *testing.T) {
	mockRepo := &mockWebhookRepository{}

	handler := NewPlaidWebhookHandler(PlaidWebhookHandlerDeps{
		WebhookRepo: mockRepo,
	})

	payload := PlaidWebhookPayload{
		WebhookType: "TRANSACTIONS",
		WebhookCode: "DEFAULT_UPDATE",
		ItemID:      "item-123",
	}
	rawBody, _ := json.Marshal(payload)

	input := &PlaidWebhookInput{
		PlaidVerification: "some-signature",
		RawBody:           rawBody,
	}

	output, err := handler.HandlePlaidWebhook(context.Background(), input)

	require.Error(t, err)
	assert.Nil(t, output)
	assert.Empty(t, mockRepo.events)
}

// TestPlaidWebhookRouteRegistration tests that routes are properly registered.
func TestPlaidWebhookRouteRegistration(t *testing.T) {
	mockService := &mockWebhookService{verifyResult: true}
	mockRepo := &mockWebhookRepository{}

	_, api := humatest.New(t)
	handler := NewPlaidWebhookHandler(PlaidWebhookHandlerDeps{
		WebhookService: mockService,
		WebhookRepo:    mockRepo,
	})
	RegisterPlaidWebhookRoutes(api, handler)

	// Test POST /webhooks/plaid with valid payload and a Plaid-Verification header (Issue #1514:
	// the request is rejected outright without one).
	payload := map[string]interface{}{
		"webhook_type": "TRANSACTIONS",
		"webhook_code": "DEFAULT_UPDATE",
		"item_id":      "item-123",
	}

	resp := api.Post("/webhooks/plaid", "Plaid-Verification: valid-jwt-signature", payload)
	assert.Equal(t, http.StatusOK, resp.Code)

	// Verify the response contains expected status
	body := resp.Body.String()
	assert.Contains(t, body, "ok")
}

// TestHandlePlaidWebhook_ValidSignatureStoresInDB tests that valid signature
// results in webhook stored with signatureValid=true.
func TestHandlePlaidWebhook_ValidSignatureStoresInDB(t *testing.T) {
	mockService := &mockWebhookService{verifyResult: true}
	mockRepo := &mockWebhookRepository{}

	handler := NewPlaidWebhookHandler(PlaidWebhookHandlerDeps{
		WebhookService: mockService,
		WebhookRepo:    mockRepo,
	})

	payload := PlaidWebhookPayload{
		WebhookType: "TRANSACTIONS",
		WebhookCode: "DEFAULT_UPDATE",
		ItemID:      "item-123",
	}
	rawBody, _ := json.Marshal(payload)

	input := &PlaidWebhookInput{
		PlaidVerification: "valid-jwt-signature",
		RawBody:           rawBody,
	}

	output, err := handler.HandlePlaidWebhook(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Len(t, mockRepo.events, 1)
	assert.True(t, mockRepo.events[0].SignatureValid)
}

// TestHandlePlaidWebhook_ValidWebhookPersistenceFails_ReturnsNon2xx verifies Issue #1514 item 5:
// a signature-valid webhook we fail to persist must return a non-2xx response so Plaid retries,
// instead of silently losing it forever.
func TestHandlePlaidWebhook_ValidWebhookPersistenceFails_ReturnsNon2xx(t *testing.T) {
	mockService := &mockWebhookService{verifyResult: true}
	mockRepo := &mockWebhookRepository{createErr: errors.New("database connection failed")}

	handler := NewPlaidWebhookHandler(PlaidWebhookHandlerDeps{
		WebhookService: mockService,
		WebhookRepo:    mockRepo,
	})

	payload := PlaidWebhookPayload{
		WebhookType: "TRANSACTIONS",
		WebhookCode: "DEFAULT_UPDATE",
		ItemID:      "item-123",
	}
	rawBody, _ := json.Marshal(payload)

	input := &PlaidWebhookInput{
		PlaidVerification: "valid-signature",
		RawBody:           rawBody,
	}

	output, err := handler.HandlePlaidWebhook(context.Background(), input)

	require.Error(t, err, "a persistence failure for a VALID webhook must return a non-2xx response so Plaid retries")
	assert.Nil(t, output)
}

// TestHandlePlaidWebhook_MissingSignature_RejectedBeforePersistence tests that a missing
// Plaid-Verification header is rejected outright — never persisted, never enqueued (Issue #1514).
func TestHandlePlaidWebhook_MissingSignature_RejectedBeforePersistence(t *testing.T) {
	mockService := &mockWebhookService{verifyResult: true}
	mockRepo := &mockWebhookRepository{}

	handler := NewPlaidWebhookHandler(PlaidWebhookHandlerDeps{
		WebhookService: mockService,
		WebhookRepo:    mockRepo,
	})

	payload := PlaidWebhookPayload{
		WebhookType: "ITEM",
		WebhookCode: "LOGIN_REQUIRED",
		ItemID:      "item-456",
	}
	rawBody, _ := json.Marshal(payload)

	input := &PlaidWebhookInput{
		PlaidVerification: "", // Empty - no signature
		RawBody:           rawBody,
	}

	output, err := handler.HandlePlaidWebhook(context.Background(), input)

	require.Error(t, err, "missing signature must be rejected with a non-2xx response")
	assert.Nil(t, output)
	assert.Empty(t, mockRepo.events)
}

// TestHandlePlaidWebhook_InvalidAttempt_RecordsBoundedAuditMetadata verifies that rejecting an
// invalid/unsigned Plaid webhook still records bounded metadata (Issue #1514 item 2).
func TestHandlePlaidWebhook_InvalidAttempt_RecordsBoundedAuditMetadata(t *testing.T) {
	mockService := &mockWebhookService{verifyResult: false}
	invalidRepo := &mockInvalidWebhookAttemptRepository{}

	handler := NewPlaidWebhookHandler(PlaidWebhookHandlerDeps{
		WebhookService:     mockService,
		InvalidAttemptRepo: invalidRepo,
	})

	payload := PlaidWebhookPayload{
		WebhookType: "TRANSACTIONS",
		WebhookCode: "DEFAULT_UPDATE",
		ItemID:      "item-attacker",
	}
	rawBody, _ := json.Marshal(payload)

	input := &PlaidWebhookInput{
		PlaidVerification: "invalid-signature",
		RawBody:           rawBody,
	}

	_, err := handler.HandlePlaidWebhook(context.Background(), input)
	require.Error(t, err)

	require.Len(t, invalidRepo.calls, 1)
	assert.Equal(t, "plaid", invalidRepo.calls[0].provider)
	assert.Equal(t, len(rawBody), invalidRepo.calls[0].bodyLength)
}

// TestHandlePlaidWebhook_VerifierUnavailable_TreatedAsRetryable_NotForgery verifies Issue #1514:
// a verifier-unavailable outcome (e.g. Plaid's JWK fetch failed) is retryable, not evidence of a
// forged request — it must return a non-2xx (so Plaid retries) WITHOUT recording an invalid
// attempt (that signal is reserved for genuinely rejected/forged requests).
func TestHandlePlaidWebhook_VerifierUnavailable_TreatedAsRetryable_NotForgery(t *testing.T) {
	mockService := &mockWebhookService{
		verifyErr: fmt.Errorf("%w: plaid api down", services.ErrWebhookVerifierUnavailable),
	}
	invalidRepo := &mockInvalidWebhookAttemptRepository{}

	handler := NewPlaidWebhookHandler(PlaidWebhookHandlerDeps{
		WebhookService:     mockService,
		InvalidAttemptRepo: invalidRepo,
	})

	payload := PlaidWebhookPayload{
		WebhookType: "TRANSACTIONS",
		WebhookCode: "DEFAULT_UPDATE",
		ItemID:      "item-retry",
	}
	rawBody, _ := json.Marshal(payload)

	input := &PlaidWebhookInput{
		PlaidVerification: "some-signature",
		RawBody:           rawBody,
	}

	output, err := handler.HandlePlaidWebhook(context.Background(), input)

	require.Error(t, err, "verifier-unavailable must return a non-2xx response so Plaid retries")
	assert.Nil(t, output)
	assert.Empty(t, invalidRepo.calls, "verifier-unavailable must NOT be recorded as an invalid attempt")
}

// TestHandlePlaidWebhook_InvalidSignatureDoesNotEnqueue tests that an invalid Plaid signature is
// rejected outright — no persistence, no enqueue (Issue #1514; supersedes the pre-fix "stored but
// not enqueued" audit-trail behavior).
func TestHandlePlaidWebhook_InvalidSignatureDoesNotEnqueue(t *testing.T) {
	mockService := &mockWebhookService{verifyResult: false}
	mockRepo := &mockWebhookRepository{}
	mockQueue := &mockJobQueue{}

	handler := NewPlaidWebhookHandler(PlaidWebhookHandlerDeps{
		WebhookService: mockService,
		WebhookRepo:    mockRepo,
		JobQueue:       mockQueue,
	})

	payload := PlaidWebhookPayload{
		WebhookType: "TRANSACTIONS",
		WebhookCode: "DEFAULT_UPDATE",
		ItemID:      "item-999",
	}
	rawBody, _ := json.Marshal(payload)

	output, err := handler.HandlePlaidWebhook(context.Background(), &PlaidWebhookInput{
		PlaidVerification: "invalid-signature",
		RawBody:           rawBody,
	})

	require.Error(t, err, "invalid signature must be rejected with a non-2xx response")
	assert.Nil(t, output)

	// NEVER persisted (Issue #1514 — closes the idempotency-poisoning vector)
	assert.Empty(t, mockRepo.events, "invalid-signature webhook must not be persisted")

	// NOT enqueued for processing
	assert.Equal(t, 0, mockQueue.enqueueCount, "must NOT enqueue processing for unverified webhook")
}

// TestHandlePlaidWebhook_ValidSignatureEnqueues tests that a valid Plaid signature
// causes the webhook to be stored AND enqueued for processing.
func TestHandlePlaidWebhook_ValidSignatureEnqueues(t *testing.T) {
	mockService := &mockWebhookService{verifyResult: true}
	mockRepo := &mockWebhookRepository{}
	mockQueue := &mockJobQueue{}

	handler := NewPlaidWebhookHandler(PlaidWebhookHandlerDeps{
		WebhookService: mockService,
		WebhookRepo:    mockRepo,
		JobQueue:       mockQueue,
	})

	payload := PlaidWebhookPayload{
		WebhookType: "TRANSACTIONS",
		WebhookCode: "DEFAULT_UPDATE",
		ItemID:      "item-888",
	}
	rawBody, _ := json.Marshal(payload)

	output, err := handler.HandlePlaidWebhook(context.Background(), &PlaidWebhookInput{
		PlaidVerification: "valid-signature",
		RawBody:           rawBody,
	})

	require.NoError(t, err)
	assert.Equal(t, "ok", output.Body.Status)

	// Stored and enqueued
	assert.Len(t, mockRepo.events, 1, "webhook must be stored")
	assert.True(t, mockRepo.events[0].SignatureValid)
	assert.Equal(t, 1, mockQueue.enqueueCount, "must enqueue processing for verified webhook")
}

// TestHandlePlaidWebhook_MissingSignatureDoesNotEnqueue tests that a missing
// Plaid-Verification header causes the webhook to be stored but NOT enqueued.
func TestHandlePlaidWebhook_MissingSignatureDoesNotEnqueue(t *testing.T) {
	mockService := &mockWebhookService{verifyResult: true}
	mockRepo := &mockWebhookRepository{}
	mockQueue := &mockJobQueue{}

	handler := NewPlaidWebhookHandler(PlaidWebhookHandlerDeps{
		WebhookService: mockService,
		WebhookRepo:    mockRepo,
		JobQueue:       mockQueue,
	})

	payload := PlaidWebhookPayload{
		WebhookType: "ITEM",
		WebhookCode: "LOGIN_REQUIRED",
		ItemID:      "item-777",
	}
	rawBody, _ := json.Marshal(payload)

	output, err := handler.HandlePlaidWebhook(context.Background(), &PlaidWebhookInput{
		PlaidVerification: "", // No signature header
		RawBody:           rawBody,
	})

	require.Error(t, err, "missing signature must be rejected with a non-2xx response")
	assert.Nil(t, output)

	assert.Empty(t, mockRepo.events, "unsigned webhook must not be persisted")
	assert.Equal(t, 0, mockQueue.enqueueCount, "must NOT enqueue when signature is missing")
}
