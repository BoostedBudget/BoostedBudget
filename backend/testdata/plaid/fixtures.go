// Package plaidtestdata provides test fixtures for Plaid API responses.
// Use these fixtures in unit tests to mock Plaid API behavior without network calls.
//
// Example usage:
//
//	resp := plaidtestdata.MustLoadTransactionsSyncResponse("single_page")
//	payload, err := plaidtestdata.LoadWebhookPayload("transactions", "default_update")
//	plaidErr, err := plaidtestdata.LoadPlaidError("rate_limit_exceeded")
package plaidtestdata

import (
	"embed"
	"encoding/json"
	"fmt"

	"github.com/plaid/plaid-go/v23/plaid"
)

//go:embed webhooks/*/*.json responses/*/*.json errors/*.json
var fixtures embed.FS

// LoadTransactionsSyncResponse loads a TransactionsSync response fixture by name.
// Available fixtures are listed in ListAvailableFixtures().
// Returns error if fixture not found or JSON is invalid.
func LoadTransactionsSyncResponse(name string) (*plaid.TransactionsSyncResponse, error) {
	path := fmt.Sprintf("responses/transactions_sync/%s.json", name)
	data, err := fixtures.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("fixture not found: %s: %w", path, err)
	}

	var resp plaid.TransactionsSyncResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("invalid fixture JSON: %s: %w", path, err)
	}

	return &resp, nil
}

// MustLoadTransactionsSyncResponse is like LoadTransactionsSyncResponse but panics on error.
// Use in tests where fixture loading failure should fail fast.
func MustLoadTransactionsSyncResponse(name string) *plaid.TransactionsSyncResponse {
	resp, err := LoadTransactionsSyncResponse(name)
	if err != nil {
		panic(err)
	}
	return resp
}

// LoadWebhookPayload loads a raw webhook payload for testing.
// webhookType: "transactions" or "item"
// webhookCode: varies by type (see ListAvailableFixtures() for options).
// Returns raw JSON bytes for flexible testing.
func LoadWebhookPayload(webhookType, webhookCode string) ([]byte, error) {
	path := fmt.Sprintf("webhooks/%s/%s.json", webhookType, webhookCode)
	data, err := fixtures.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("webhook fixture not found: %s: %w", path, err)
	}
	return data, nil
}

// MustLoadWebhookPayload is like LoadWebhookPayload but panics on error.
func MustLoadWebhookPayload(webhookType, webhookCode string) []byte {
	data, err := LoadWebhookPayload(webhookType, webhookCode)
	if err != nil {
		panic(err)
	}
	return data
}

// WebhookPayload represents a parsed Plaid webhook payload.
type WebhookPayload struct {
	WebhookType         string `json:"webhook_type"`
	WebhookCode         string `json:"webhook_code"`
	ItemID              string `json:"item_id"`
	Error               *Error `json:"error,omitempty"`
	NewTransactions     int    `json:"new_transactions,omitempty"`
	RemovedTransactions int    `json:"removed_transactions,omitempty"`
	Environment         string `json:"environment,omitempty"`
}

// Error represents a Plaid error in webhook payloads.
type Error struct {
	ErrorType    string `json:"error_type"`
	ErrorCode    string `json:"error_code"`
	ErrorMessage string `json:"error_message"`
}

// LoadWebhookPayloadParsed loads and parses a webhook payload into a struct.
func LoadWebhookPayloadParsed(webhookType, webhookCode string) (*WebhookPayload, error) {
	data, err := LoadWebhookPayload(webhookType, webhookCode)
	if err != nil {
		return nil, err
	}

	var payload WebhookPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("invalid webhook JSON: %w", err)
	}

	return &payload, nil
}

// LoadPlaidError loads a Plaid error response fixture.
// Available error fixtures are listed in ListAvailableFixtures().
// Returns a plaid.PlaidError struct for error handling tests.
func LoadPlaidError(errorCode string) (*plaid.PlaidError, error) {
	path := fmt.Sprintf("errors/%s.json", errorCode)
	data, err := fixtures.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("error fixture not found: %s: %w", path, err)
	}

	var plaidErr plaid.PlaidError
	if err := json.Unmarshal(data, &plaidErr); err != nil {
		return nil, fmt.Errorf("invalid error fixture JSON: %s: %w", path, err)
	}

	return &plaidErr, nil
}

// MustLoadPlaidError is like LoadPlaidError but panics on error.
func MustLoadPlaidError(errorCode string) *plaid.PlaidError {
	plaidErr, err := LoadPlaidError(errorCode)
	if err != nil {
		panic(err)
	}
	return plaidErr
}

// ListAvailableFixtures returns all available fixture names for each category.
// Useful for documentation and validation.
func ListAvailableFixtures() map[string][]string {
	return map[string][]string{
		"transactions_sync": {
			"single_page",
			"multi_page_1",
			"multi_page_2",
			"multi_page_final",
			"empty_response",
			"large_batch",
		},
		"webhooks/transactions": {
			"default_update",
			"initial_update",
			"historical_update",
			"sync_updates_available",
			"transactions_removed",
		},
		"webhooks/item": {
			"error",
			"login_required",
			"pending_expiration",
			"user_permission_revoked",
		},
		"errors": {
			"rate_limit_exceeded",
			"invalid_access_token",
			"item_login_required",
			"internal_server_error",
			"invalid_cursor",
		},
	}
}
