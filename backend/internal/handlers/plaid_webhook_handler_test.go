package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	// Given: A webhook handler
	handler := NewPlaidWebhookHandler()

	// And: Invalid JSON payload
	invalidJSON := []byte(`{"webhook_type": "TRANSACTIONS", "incomplete":`)

	// When: POST /webhooks/plaid with invalid JSON
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/plaid", bytes.NewReader(invalidJSON))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := handler.HandlePlaidWebhook(c)

	// Then: Returns 400 Bad Request
	assert.Error(t, err, "Expected error for invalid JSON")
	httpErr, ok := err.(*echo.HTTPError)
	require.True(t, ok, "Expected HTTPError")
	assert.Equal(t, http.StatusBadRequest, httpErr.Code, "Expected 400 Bad Request")
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
