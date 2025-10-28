// Package handlers provides HTTP request handlers for the BoostedBudget API.
// This file implements webhook handlers for receiving Plaid notifications.
//
// Related: Issue #202 - Plaid Webhook Integration with JWT Verification
package handlers

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// PlaidWebhookHandler handles Plaid webhook HTTP requests
type PlaidWebhookHandler struct {
	webhookService services.WebhookService
	webhookRepo    repository.WebhookRepository
}

// NewPlaidWebhookHandler creates a new PlaidWebhookHandler instance (backward compatible)
func NewPlaidWebhookHandler() *PlaidWebhookHandler {
	return &PlaidWebhookHandler{}
}

// NewPlaidWebhookHandlerWithDeps creates a new PlaidWebhookHandler with dependencies
func NewPlaidWebhookHandlerWithDeps(webhookService services.WebhookService, webhookRepo repository.WebhookRepository) *PlaidWebhookHandler {
	return &PlaidWebhookHandler{
		webhookService: webhookService,
		webhookRepo:    webhookRepo,
	}
}

// PlaidWebhookPayload represents the structure of Plaid webhook notifications
// Reference: https://plaid.com/docs/api/webhooks/
type PlaidWebhookPayload struct {
	WebhookType string                 `json:"webhook_type" validate:"required"`
	WebhookCode string                 `json:"webhook_code" validate:"required"`
	ItemID      string                 `json:"item_id"`
	Error       map[string]interface{} `json:"error,omitempty"`
	// Additional fields vary by webhook type
	NewTransactions     int      `json:"new_transactions,omitempty"`
	RemovedTransactions []string `json:"removed_transactions,omitempty"`
}

// HandlePlaidWebhook handles POST /webhooks/plaid
// Receives and processes webhook notifications from Plaid
//
// Issue #202: Enhanced with JWT signature verification and database storage
//
// Webhook Types:
//   - TRANSACTIONS: New transactions available, historical updates
//   - ITEM: Connection status changes (LOGIN_REQUIRED, ERROR, etc.)
//
// Security: Verifies JWT signatures using ES256 (ECDSA P-256)
// Reference: https://plaid.com/docs/api/webhooks/#webhook-verification
func (h *PlaidWebhookHandler) HandlePlaidWebhook(c echo.Context) error {
	// CRITICAL: Read raw body FIRST before any parsing
	// Raw body is needed for SHA-256 hash verification in JWT
	rawBody, err := io.ReadAll(c.Request().Body)
	if err != nil {
		c.Logger().Errorf("Failed to read webhook body: %v", err)
		// Still return 200 OK to prevent retry storms
		return c.JSON(http.StatusOK, map[string]interface{}{
			"status":  "error",
			"message": "Failed to read body",
		})
	}

	// Extract signature from Plaid-Verification header
	signature := c.Request().Header.Get("Plaid-Verification")

	// Verify signature if services are available
	signatureValid := false
	if h.webhookService != nil && signature != "" {
		valid, err := h.webhookService.VerifyWebhook(c.Request().Context(), rawBody, signature)
		if err != nil {
			// Log verification failure but DON'T fail the webhook
			// This ensures audit trail even for invalid signatures
			c.Logger().Warnf("Webhook signature verification failed: %v", err)
		} else if valid {
			signatureValid = true
			c.Logger().Infof("Webhook signature verified successfully")
		}
	} else if signature == "" {
		c.Logger().Warnf("Webhook received without Plaid-Verification header")
	}

	// Parse webhook payload from raw body
	var payload PlaidWebhookPayload
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		c.Logger().Errorf("Failed to parse Plaid webhook payload: %v", err)
		// Still return 200 OK to prevent retry storms
		return c.JSON(http.StatusOK, map[string]interface{}{
			"status":  "error",
			"message": "Invalid JSON payload",
		})
	}

	// Validate required fields
	if payload.WebhookType == "" {
		c.Logger().Errorf("Webhook payload validation failed: missing webhook_type")
		return echo.NewHTTPError(http.StatusBadRequest, "Missing required fields")
	}
	if payload.WebhookCode == "" {
		c.Logger().Errorf("Webhook payload validation failed: missing webhook_code")
		return echo.NewHTTPError(http.StatusBadRequest, "Missing required fields")
	}

	// Log webhook receipt
	c.Logger().Infof("Received Plaid webhook: type=%s code=%s item_id=%s signature_valid=%v",
		payload.WebhookType, payload.WebhookCode, payload.ItemID, signatureValid)

	// Store webhook in database for audit trail
	if h.webhookRepo != nil {
		webhookEvent := &domain.WebhookEvent{
			WebhookType:    payload.WebhookType,
			WebhookCode:    &payload.WebhookCode,
			ItemID:         stringPtr(payload.ItemID),
			NewTransactions: intPtr(payload.NewTransactions),
			Payload:        json.RawMessage(rawBody),
			Signature:      stringPtr(signature),
			SignatureValid: signatureValid,
			Processed:      false,
		}

		// Extract error code if present
		if payload.Error != nil {
			if errorCode, ok := payload.Error["error_code"].(string); ok {
				webhookEvent.ErrorCode = &errorCode
			}
		}

		if err := h.webhookRepo.Create(c.Request().Context(), webhookEvent); err != nil {
			// Log database error but DON'T fail the webhook
			// Plaid should not retry due to our database issues
			c.Logger().Errorf("Failed to store webhook in database: %v", err)
		} else {
			c.Logger().Infof("Webhook stored in database: id=%s", webhookEvent.ID)
		}
	}

	// Handle different webhook types
	switch payload.WebhookType {
	case "TRANSACTIONS":
		return h.handleTransactionsWebhook(c, &payload)
	case "ITEM":
		return h.handleItemWebhook(c, &payload)
	default:
		// Log unknown webhook types but still acknowledge receipt (200 OK)
		c.Logger().Warnf("Received unknown webhook type: %s", payload.WebhookType)
	}

	// Always return 200 OK to acknowledge receipt
	// Plaid will retry webhooks that don't receive 200 response
	return c.JSON(http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"message": "Webhook received",
	})
}

// Helper functions for pointer conversion
func stringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func intPtr(i int) *int {
	if i == 0 {
		return nil
	}
	return &i
}

// handleTransactionsWebhook processes TRANSACTIONS webhook events
// Events: DEFAULT_UPDATE, INITIAL_UPDATE, HISTORICAL_UPDATE, TRANSACTIONS_REMOVED
func (h *PlaidWebhookHandler) handleTransactionsWebhook(c echo.Context, payload *PlaidWebhookPayload) error {
	c.Logger().Infof("Processing TRANSACTIONS webhook: code=%s item_id=%s new_count=%d",
		payload.WebhookCode, payload.ItemID, payload.NewTransactions)

	// Issue #256: Transaction Sync Service - Background Processing Flow
	//
	// This handler stores the webhook in webhook_events table (see HandlePlaidWebhook above).
	// Background processing occurs asynchronously via WebhookProcessor worker:
	//
	// 1. WebhookProcessor polls webhook_events every 30 seconds for unprocessed TRANSACTIONS webhooks
	// 2. For each webhook, TransactionSyncService.ProcessWebhook() is called:
	//    a. Fetches linked item by item_id (to get access token and cursor)
	//    b. TODO: Call Plaid TransactionsSync API with cursor-based pagination
	//    c. TODO: Store Added/Modified transactions, delete Removed transactions
	//    d. Updates cursor after successful sync
	//    e. Marks webhook as processed (or retries on error with exponential backoff)
	// 3. This approach ensures webhook responses are fast (<200ms) while expensive Plaid API
	//    calls happen in background, with automatic retries on transient failures
	//
	// See: backend/internal/workers/webhook_processor.go
	// See: backend/internal/services/transaction_sync_service.go

	switch payload.WebhookCode {
	case "DEFAULT_UPDATE":
		// New transaction data available since last fetch
		c.Logger().Infof("DEFAULT_UPDATE: %d new transactions for item %s",
			payload.NewTransactions, payload.ItemID)
	case "INITIAL_UPDATE":
		// Historical transactions are ready (after initial Link connection)
		c.Logger().Infof("INITIAL_UPDATE: Historical transactions ready for item %s", payload.ItemID)
	case "HISTORICAL_UPDATE":
		// Historical transactions have been updated
		c.Logger().Infof("HISTORICAL_UPDATE: Historical data updated for item %s", payload.ItemID)
	case "TRANSACTIONS_REMOVED":
		// Transactions have been removed
		c.Logger().Infof("TRANSACTIONS_REMOVED: %d transactions removed for item %s",
			len(payload.RemovedTransactions), payload.ItemID)
	default:
		c.Logger().Warnf("Unknown TRANSACTIONS webhook code: %s", payload.WebhookCode)
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"message": "Transaction webhook processed",
	})
}

// handleItemWebhook processes ITEM webhook events
// Events: ERROR, PENDING_EXPIRATION, USER_PERMISSION_REVOKED, WEBHOOK_UPDATE_ACKNOWLEDGED
func (h *PlaidWebhookHandler) handleItemWebhook(c echo.Context, payload *PlaidWebhookPayload) error {
	c.Logger().Infof("Processing ITEM webhook: code=%s item_id=%s",
		payload.WebhookCode, payload.ItemID)

	// TODO: Implement item state management
	// 1. Update linked_items table with error state
	// 2. Notify user of connection issues
	// 3. Provide re-authentication link if LOGIN_REQUIRED

	switch payload.WebhookCode {
	case "ERROR":
		// Item encountered an error
		c.Logger().Errorf("ITEM ERROR for %s: %v", payload.ItemID, payload.Error)
	case "LOGIN_REQUIRED":
		// User needs to re-authenticate (credentials changed, MFA required, etc.)
		c.Logger().Warnf("LOGIN_REQUIRED for item %s: User needs to re-authenticate", payload.ItemID)
	case "PENDING_EXPIRATION":
		// Item access will expire soon
		c.Logger().Warnf("PENDING_EXPIRATION for item %s: Access expiring soon", payload.ItemID)
	case "USER_PERMISSION_REVOKED":
		// User revoked access at their bank
		c.Logger().Warnf("USER_PERMISSION_REVOKED for item %s", payload.ItemID)
	case "WEBHOOK_UPDATE_ACKNOWLEDGED":
		// Webhook URL update was acknowledged
		c.Logger().Infof("WEBHOOK_UPDATE_ACKNOWLEDGED for item %s", payload.ItemID)
	default:
		c.Logger().Warnf("Unknown ITEM webhook code: %s", payload.WebhookCode)
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"message": "Item webhook processed",
	})
}
