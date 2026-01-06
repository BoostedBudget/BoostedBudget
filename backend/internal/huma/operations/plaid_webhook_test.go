package operations

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
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

// TestHandlePlaidWebhook_InvalidJSON tests handling of invalid JSON payload.
func TestHandlePlaidWebhook_InvalidJSON(t *testing.T) {
	mockService := &mockWebhookService{}
	mockRepo := &mockWebhookRepository{}

	handler := NewPlaidWebhookHandler(PlaidWebhookHandlerDeps{
		WebhookService: mockService,
		WebhookRepo:    mockRepo,
	})

	input := &PlaidWebhookInput{
		PlaidVerification: "",
		RawBody:           []byte("not valid json"),
	}

	output, err := handler.HandlePlaidWebhook(context.Background(), input)

	require.NoError(t, err) // Should still return 200 OK
	require.NotNil(t, output)
	assert.Equal(t, "error", output.Body.Status)
	assert.Contains(t, output.Body.Message, "Invalid JSON")
}

// TestHandlePlaidWebhook_MissingFields tests handling of missing required fields.
func TestHandlePlaidWebhook_MissingFields(t *testing.T) {
	mockService := &mockWebhookService{}
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
		PlaidVerification: "",
		RawBody:           rawBody,
	}

	output, err := handler.HandlePlaidWebhook(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Missing required fields")
}

// TestHandlePlaidWebhook_InvalidSignature tests handling when signature is invalid.
func TestHandlePlaidWebhook_InvalidSignature(t *testing.T) {
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

	require.NoError(t, err) // Should still process (audit trail)
	require.NotNil(t, output)
	assert.Equal(t, "ok", output.Body.Status)

	// Webhook should still be stored with signatureValid = false
	assert.Len(t, mockRepo.events, 1)
	assert.False(t, mockRepo.events[0].SignatureValid)
}

// TestHandlePlaidWebhook_NoService tests handling when no webhook service is configured.
func TestHandlePlaidWebhook_NoService(t *testing.T) {
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

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "ok", output.Body.Status)
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

	// Test POST /webhooks/plaid with valid payload
	payload := map[string]interface{}{
		"webhook_type": "TRANSACTIONS",
		"webhook_code": "DEFAULT_UPDATE",
		"item_id":      "item-123",
	}

	resp := api.Post("/webhooks/plaid", "application/json", payload)
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

// TestHandlePlaidWebhook_DatabaseErrorReturns200 tests graceful degradation
// when database storage fails - should still return 200 OK.
func TestHandlePlaidWebhook_DatabaseErrorReturns200(t *testing.T) {
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

	require.NoError(t, err) // Should NOT fail
	require.NotNil(t, output)
	assert.Equal(t, "ok", output.Body.Status) // Should return OK despite DB error
}

// TestHandlePlaidWebhook_MissingSignatureStoresWithFalse tests that missing
// signature header still stores webhook but with signatureValid=false.
func TestHandlePlaidWebhook_MissingSignatureStoresWithFalse(t *testing.T) {
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

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Len(t, mockRepo.events, 1)
	assert.False(t, mockRepo.events[0].SignatureValid) // Should be false
}
