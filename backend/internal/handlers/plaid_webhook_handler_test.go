package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Mock WebhookService for testing
type mockWebhookService struct {
	verifyResult bool
	verifyError  error
}

func (m *mockWebhookService) VerifyWebhook(ctx context.Context, rawBody []byte, signature string) (bool, error) {
	return m.verifyResult, m.verifyError
}

// Mock WebhookRepository for testing
type mockWebhookRepository struct {
	createError        error
	markProcessedError error
	createdEvents      []*domain.WebhookEvent
}

func (m *mockWebhookRepository) Create(ctx context.Context, event *domain.WebhookEvent) error {
	if m.createError != nil {
		return m.createError
	}
	m.createdEvents = append(m.createdEvents, event)
	return nil
}

func (m *mockWebhookRepository) MarkProcessed(ctx context.Context, id string) error {
	return m.markProcessedError
}

func (m *mockWebhookRepository) GetUnprocessed(ctx context.Context) ([]domain.WebhookEvent, error) {
	return nil, nil
}

func (m *mockWebhookRepository) GetByItemID(ctx context.Context, itemID string, limit int) ([]domain.WebhookEvent, error) {
	return nil, nil
}

// TestHandlePlaidWebhook_TransactionsDefaultUpdate tests handling of transaction update webhooks
func TestHandlePlaidWebhook_TransactionsDefaultUpdate(t *testing.T) {
	// Given: A webhook handler
	handler := NewPlaidWebhookHandler()

	// And: A TRANSACTIONS DEFAULT_UPDATE webhook payload
	payload := map[string]interface{}{
		"webhook_type":         "TRANSACTIONS",
		"webhook_code":         "DEFAULT_UPDATE",
		"item_id":              "test-item-id-123",
		"new_transactions":     10,
		"removed_transactions": []string{},
	}

	payloadBytes, err := json.Marshal(payload)
	require.NoError(t, err, "Failed to marshal payload")

	// When: POST /webhooks/plaid with transaction update
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/plaid", bytes.NewReader(payloadBytes))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err = handler.HandlePlaidWebhook(c)

	// Then: Returns 200 OK
	assert.NoError(t, err, "Expected no error")
	assert.Equal(t, http.StatusOK, rec.Code, "Expected 200 OK")

	// And: Response confirms receipt
	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err, "Failed to unmarshal response")
	assert.Equal(t, "ok", response["status"], "Expected status ok")
}

// TestHandlePlaidWebhook_ItemLoginRequired tests handling of item login required webhooks
func TestHandlePlaidWebhook_ItemLoginRequired(t *testing.T) {
	// Given: A webhook handler
	handler := NewPlaidWebhookHandler()

	// And: An ITEM LOGIN_REQUIRED webhook payload
	payload := map[string]interface{}{
		"webhook_type": "ITEM",
		"webhook_code": "LOGIN_REQUIRED",
		"item_id":      "test-item-id-456",
		"error": map[string]interface{}{
			"error_code": "ITEM_LOGIN_REQUIRED",
			"error_message": "the login details of this item have changed",
		},
	}

	payloadBytes, err := json.Marshal(payload)
	require.NoError(t, err, "Failed to marshal payload")

	// When: POST /webhooks/plaid with item login required
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/plaid", bytes.NewReader(payloadBytes))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err = handler.HandlePlaidWebhook(c)

	// Then: Returns 200 OK
	assert.NoError(t, err, "Expected no error")
	assert.Equal(t, http.StatusOK, rec.Code, "Expected 200 OK")
}

// TestHandlePlaidWebhook_ItemError tests handling of item error webhooks
func TestHandlePlaidWebhook_ItemError(t *testing.T) {
	// Given: A webhook handler
	handler := NewPlaidWebhookHandler()

	// And: An ITEM ERROR webhook payload
	payload := map[string]interface{}{
		"webhook_type": "ITEM",
		"webhook_code": "ERROR",
		"item_id":      "test-item-id-789",
		"error": map[string]interface{}{
			"error_code":    "ITEM_LOCKED",
			"error_message": "the item is locked",
		},
	}

	payloadBytes, err := json.Marshal(payload)
	require.NoError(t, err, "Failed to marshal payload")

	// When: POST /webhooks/plaid with item error
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/plaid", bytes.NewReader(payloadBytes))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err = handler.HandlePlaidWebhook(c)

	// Then: Returns 200 OK (acknowledge receipt even for errors)
	assert.NoError(t, err, "Expected no error")
	assert.Equal(t, http.StatusOK, rec.Code, "Expected 200 OK")
}

// TestHandlePlaidWebhook_InvalidJSON tests handling of malformed JSON
func TestHandlePlaidWebhook_InvalidJSON(t *testing.T) {
	// Given: A webhook handler with dependencies
	webhookService := &mockWebhookService{}
	webhookRepo := &mockWebhookRepository{}
	handler := NewPlaidWebhookHandlerWithDeps(webhookService, webhookRepo)

	// And: Invalid JSON payload
	invalidJSON := []byte(`{"webhook_type": "TRANSACTIONS", "incomplete":`)

	// When: POST /webhooks/plaid with invalid JSON
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/plaid", bytes.NewReader(invalidJSON))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := handler.HandlePlaidWebhook(c)

	// Then: Returns 200 OK (to prevent Plaid retry storms)
	// Invalid JSON is logged but doesn't fail the webhook
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code, "Expected 200 OK to prevent retry storms")

	// And: Response indicates error
	var response map[string]interface{}
	jsonErr := json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, jsonErr, "Response should be valid JSON")
	assert.Equal(t, "error", response["status"], "Expected error status in response")
}

// TestHandlePlaidWebhook_MissingWebhookType tests handling of missing webhook_type field
func TestHandlePlaidWebhook_MissingWebhookType(t *testing.T) {
	// Given: A webhook handler
	handler := NewPlaidWebhookHandler()

	// And: Payload missing webhook_type
	payload := map[string]interface{}{
		"webhook_code": "DEFAULT_UPDATE",
		"item_id":      "test-item-id",
	}

	payloadBytes, err := json.Marshal(payload)
	require.NoError(t, err, "Failed to marshal payload")

	// When: POST /webhooks/plaid without webhook_type
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/plaid", bytes.NewReader(payloadBytes))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err = handler.HandlePlaidWebhook(c)

	// Then: Returns 400 Bad Request
	assert.Error(t, err, "Expected error for missing webhook_type")
	httpErr, ok := err.(*echo.HTTPError)
	require.True(t, ok, "Expected HTTPError")
	assert.Equal(t, http.StatusBadRequest, httpErr.Code, "Expected 400 Bad Request")
}

// TDD Phase 5: Enhanced Webhook Handler Tests - Issue #202
// Following RED-GREEN-REFACTOR: Write tests first for signature verification and DB storage

// TestHandlePlaidWebhook_ValidSignatureWithDBStorage tests signature verification and database storage
func TestHandlePlaidWebhook_ValidSignatureWithDBStorage(t *testing.T) {
	// Given: Mock services
	webhookService := &mockWebhookService{
		verifyResult: true,
		verifyError:  nil,
	}
	webhookRepo := &mockWebhookRepository{}

	handler := NewPlaidWebhookHandlerWithDeps(webhookService, webhookRepo)

	// And: A valid webhook payload
	payload := map[string]interface{}{
		"webhook_type":     "TRANSACTIONS",
		"webhook_code":     "DEFAULT_UPDATE",
		"item_id":          "test-item-123",
		"new_transactions": 5,
	}
	payloadBytes, err := json.Marshal(payload)
	require.NoError(t, err)

	// And: Valid signature header
	signature := "valid-jwt-signature"

	// When: POST /webhooks/plaid with valid signature
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/plaid", bytes.NewReader(payloadBytes))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req.Header.Set("Plaid-Verification", signature)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err = handler.HandlePlaidWebhook(c)

	// Then: Returns 200 OK
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	// And: Webhook stored in database
	assert.Len(t, webhookRepo.createdEvents, 1)
	storedEvent := webhookRepo.createdEvents[0]
	assert.Equal(t, "TRANSACTIONS", storedEvent.WebhookType)
	assert.Equal(t, "DEFAULT_UPDATE", *storedEvent.WebhookCode)
	assert.Equal(t, "test-item-123", *storedEvent.ItemID)
	assert.True(t, storedEvent.SignatureValid)
	assert.Equal(t, signature, *storedEvent.Signature)
	assert.NotNil(t, storedEvent.Payload)
}

// TestHandlePlaidWebhook_InvalidSignatureStillStoresAndReturns200 tests invalid signature handling
func TestHandlePlaidWebhook_InvalidSignatureStillStoresAndReturns200(t *testing.T) {
	// Given: Mock services with signature verification failure
	webhookService := &mockWebhookService{
		verifyResult: false,
		verifyError:  fmt.Errorf("JWT verification failed"),
	}
	webhookRepo := &mockWebhookRepository{}

	handler := NewPlaidWebhookHandlerWithDeps(webhookService, webhookRepo)

	// And: A webhook payload
	payload := map[string]interface{}{
		"webhook_type": "TRANSACTIONS",
		"webhook_code": "DEFAULT_UPDATE",
		"item_id":      "test-item-456",
	}
	payloadBytes, err := json.Marshal(payload)
	require.NoError(t, err)

	// And: Invalid signature header
	signature := "invalid-jwt-signature"

	// When: POST /webhooks/plaid with invalid signature
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/plaid", bytes.NewReader(payloadBytes))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req.Header.Set("Plaid-Verification", signature)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err = handler.HandlePlaidWebhook(c)

	// Then: STILL returns 200 OK (to prevent Plaid retry storms)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	// And: Webhook stored with signature_valid=false
	assert.Len(t, webhookRepo.createdEvents, 1)
	storedEvent := webhookRepo.createdEvents[0]
	assert.False(t, storedEvent.SignatureValid, "Expected signature_valid to be false")
}

// TestHandlePlaidWebhook_MissingSignatureHeader tests missing signature handling
func TestHandlePlaidWebhook_MissingSignatureHeader(t *testing.T) {
	// Given: Mock services
	webhookService := &mockWebhookService{}
	webhookRepo := &mockWebhookRepository{}

	handler := NewPlaidWebhookHandlerWithDeps(webhookService, webhookRepo)

	// And: A webhook payload
	payload := map[string]interface{}{
		"webhook_type": "ITEM",
		"webhook_code": "ERROR",
		"item_id":      "test-item-789",
	}
	payloadBytes, err := json.Marshal(payload)
	require.NoError(t, err)

	// When: POST /webhooks/plaid WITHOUT Plaid-Verification header
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/plaid", bytes.NewReader(payloadBytes))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	// No Plaid-Verification header
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err = handler.HandlePlaidWebhook(c)

	// Then: Returns 200 OK (graceful handling)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	// And: Webhook stored with signature_valid=false and nil signature
	assert.Len(t, webhookRepo.createdEvents, 1)
	storedEvent := webhookRepo.createdEvents[0]
	assert.False(t, storedEvent.SignatureValid)
	assert.Nil(t, storedEvent.Signature, "Expected nil signature when header missing")
}

// TestHandlePlaidWebhook_DatabaseError tests database error handling
func TestHandlePlaidWebhook_DatabaseErrorStillReturns200(t *testing.T) {
	// Given: Mock services with database error
	webhookService := &mockWebhookService{
		verifyResult: true,
		verifyError:  nil,
	}
	webhookRepo := &mockWebhookRepository{
		createError: fmt.Errorf("database connection failed"),
	}

	handler := NewPlaidWebhookHandlerWithDeps(webhookService, webhookRepo)

	// And: A valid webhook payload
	payload := map[string]interface{}{
		"webhook_type": "TRANSACTIONS",
		"webhook_code": "DEFAULT_UPDATE",
		"item_id":      "test-item-999",
	}
	payloadBytes, err := json.Marshal(payload)
	require.NoError(t, err)

	// When: POST /webhooks/plaid with database error
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/plaid", bytes.NewReader(payloadBytes))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req.Header.Set("Plaid-Verification", "valid-signature")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err = handler.HandlePlaidWebhook(c)

	// Then: STILL returns 200 OK (to prevent Plaid retry storms)
	// Database errors are logged but don't fail the webhook
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestHandlePlaidWebhook_RawBodyPreservation tests that raw body is preserved for hash verification
func TestHandlePlaidWebhook_RawBodyPreservation(t *testing.T) {
	// Given: Mock services
	webhookService := &mockWebhookService{
		verifyResult: true,
		verifyError:  nil,
	}
	webhookRepo := &mockWebhookRepository{}
	handler := NewPlaidWebhookHandlerWithDeps(webhookService, webhookRepo)

	// And: A webhook payload with specific whitespace
	payloadBytes := []byte(`{"webhook_type":"TRANSACTIONS","webhook_code":"DEFAULT_UPDATE"}`)

	// When: POST /webhooks/plaid
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/plaid", bytes.NewReader(payloadBytes))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req.Header.Set("Plaid-Verification", "signature")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := handler.HandlePlaidWebhook(c)

	// Then: Webhook processed successfully
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	// And: Raw body preserved in database (stored as json.RawMessage)
	assert.Len(t, webhookRepo.createdEvents, 1)
	storedEvent := webhookRepo.createdEvents[0]
	assert.Equal(t, payloadBytes, []byte(storedEvent.Payload), "Raw body must be preserved for hash verification")
}

// TestHandlePlaidWebhook_CompletePayloadStorage tests all fields stored correctly
func TestHandlePlaidWebhook_CompletePayloadStorage(t *testing.T) {
	// Given: Mock services
	webhookService := &mockWebhookService{verifyResult: true}
	webhookRepo := &mockWebhookRepository{}
	handler := NewPlaidWebhookHandlerWithDeps(webhookService, webhookRepo)

	// And: A complex webhook payload with multiple fields
	payload := map[string]interface{}{
		"webhook_type":         "TRANSACTIONS",
		"webhook_code":         "DEFAULT_UPDATE",
		"item_id":              "test-item-complex",
		"new_transactions":     10,
		"removed_transactions": []string{"tx1", "tx2"},
		"error": map[string]interface{}{
			"error_code":    "ITEM_LOGIN_REQUIRED",
			"error_message": "test error",
		},
	}
	payloadBytes, err := json.Marshal(payload)
	require.NoError(t, err)

	// When: POST /webhooks/plaid
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/plaid", bytes.NewReader(payloadBytes))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req.Header.Set("Plaid-Verification", "sig")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err = handler.HandlePlaidWebhook(c)

	// Then: All fields stored correctly in database
	assert.NoError(t, err)
	assert.Len(t, webhookRepo.createdEvents, 1)
	storedEvent := webhookRepo.createdEvents[0]

	assert.Equal(t, "TRANSACTIONS", storedEvent.WebhookType)
	assert.Equal(t, "DEFAULT_UPDATE", *storedEvent.WebhookCode)
	assert.Equal(t, "test-item-complex", *storedEvent.ItemID)
	assert.Equal(t, 10, *storedEvent.NewTransactions)
	assert.NotNil(t, storedEvent.Payload)

	// Verify complete payload stored as JSON
	var storedPayload map[string]interface{}
	err = json.Unmarshal(storedEvent.Payload, &storedPayload)
	require.NoError(t, err)
	assert.Equal(t, "DEFAULT_UPDATE", storedPayload["webhook_code"])
}
