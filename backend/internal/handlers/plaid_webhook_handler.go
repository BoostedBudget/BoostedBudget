// Package handlers provides HTTP request handlers for the BoostedBudget API.
// This file implements webhook handlers for receiving Plaid notifications.
//
// Related: Issue #198 - Fix Plaid OAuth callback and webhook integration
package handlers

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// PlaidWebhookHandler handles Plaid webhook HTTP requests
type PlaidWebhookHandler struct{}

// NewPlaidWebhookHandler creates a new PlaidWebhookHandler instance
func NewPlaidWebhookHandler() *PlaidWebhookHandler {
	return &PlaidWebhookHandler{}
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
// Webhook Types:
//   - TRANSACTIONS: New transactions available, historical updates
//   - ITEM: Connection status changes (LOGIN_REQUIRED, ERROR, etc.)
//
// Security Note: In production, should verify webhook signatures
// Reference: https://plaid.com/docs/api/webhooks/#webhook-verification
func (h *PlaidWebhookHandler) HandlePlaidWebhook(c echo.Context) error {
	// Parse webhook payload
	var payload PlaidWebhookPayload
	if err := c.Bind(&payload); err != nil {
		c.Logger().Errorf("Failed to parse Plaid webhook payload: %v", err)
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid webhook payload")
	}

	// Validate required fields manually (Echo validator may not be registered in all contexts)
	if payload.WebhookType == "" {
		c.Logger().Errorf("Webhook payload validation failed: missing webhook_type")
		return echo.NewHTTPError(http.StatusBadRequest, "Missing required fields")
	}
	if payload.WebhookCode == "" {
		c.Logger().Errorf("Webhook payload validation failed: missing webhook_code")
		return echo.NewHTTPError(http.StatusBadRequest, "Missing required fields")
	}

	// Log webhook receipt for debugging
	c.Logger().Infof("Received Plaid webhook: type=%s code=%s item_id=%s",
		payload.WebhookType, payload.WebhookCode, payload.ItemID)

	// Handle different webhook types
	switch payload.WebhookType {
	case "TRANSACTIONS":
		return h.handleTransactionsWebhook(c, &payload)
	case "ITEM":
		return h.handleItemWebhook(c, &payload)
	default:
		// Log unknown webhook types but still acknowledge receipt (200 OK)
		// This prevents Plaid from retrying unknown webhook types
		c.Logger().Warnf("Received unknown webhook type: %s", payload.WebhookType)
	}

	// Always return 200 OK to acknowledge receipt
	// Plaid will retry webhooks that don't receive 200 response
	return c.JSON(http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"message": "Webhook received",
	})
}

// handleTransactionsWebhook processes TRANSACTIONS webhook events
// Events: DEFAULT_UPDATE, INITIAL_UPDATE, HISTORICAL_UPDATE, TRANSACTIONS_REMOVED
func (h *PlaidWebhookHandler) handleTransactionsWebhook(c echo.Context, payload *PlaidWebhookPayload) error {
	c.Logger().Infof("Processing TRANSACTIONS webhook: code=%s item_id=%s new_count=%d",
		payload.WebhookCode, payload.ItemID, payload.NewTransactions)

	// TODO: Implement transaction sync logic
	// 1. Fetch new transactions using /transactions/get endpoint
	// 2. Store in database
	// 3. Trigger automation rules based on new transactions
	// 4. Notify user if configured

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
