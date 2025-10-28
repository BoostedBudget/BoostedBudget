package domain

import (
	"encoding/json"
	"time"
)

// WebhookEvent represents a logged Plaid webhook notification.
// Stores complete webhook payload, verification results, and processing status.
//
// Issue #202: Plaid Webhook Integration
// Purpose: Audit trail, debugging, GDPR compliance, webhook replay capability
type WebhookEvent struct {
	// ID is the unique identifier for this webhook event
	ID string `db:"id" json:"id"`

	// WebhookType is the type of webhook (TRANSACTIONS, ITEM, AUTH, etc.)
	WebhookType string `db:"webhook_type" json:"webhook_type" validate:"required"`

	// WebhookCode is the specific event code (DEFAULT_UPDATE, ERROR, etc.)
	WebhookCode *string `db:"webhook_code" json:"webhook_code,omitempty"`

	// ItemID is the Plaid item_id (may be null for some webhook types)
	ItemID *string `db:"item_id" json:"item_id,omitempty"`

	// ErrorCode is the Plaid error code if webhook reports an error
	ErrorCode *string `db:"error_code" json:"error_code,omitempty"`

	// NewTransactions is the count of new transactions (TRANSACTIONS webhooks only)
	NewTransactions *int `db:"new_transactions" json:"new_transactions,omitempty"`

	// RemovedTransactions is the count of removed transactions (TRANSACTIONS webhooks only)
	RemovedTransactions *int `db:"removed_transactions" json:"removed_transactions,omitempty"`

	// Payload is the complete webhook body as JSON (forward compatible)
	Payload json.RawMessage `db:"payload" json:"payload" validate:"required"`

	// Signature is the JWT from the Plaid-Verification header
	Signature *string `db:"signature" json:"signature,omitempty"`

	// SignatureValid indicates if the JWT signature verification passed
	// False values should trigger security monitoring alerts
	SignatureValid bool `db:"signature_valid" json:"signature_valid"`

	// Processed indicates if this webhook has been processed
	// Used for idempotency and manual replay capability
	Processed bool `db:"processed" json:"processed"`

	// ProcessedAt is when the webhook was successfully processed
	ProcessedAt *time.Time `db:"processed_at" json:"processed_at,omitempty"`

	// Processing metadata (Issue #256 - Transaction Sync Service)
	// Used to track webhook processing status, retries, and errors

	// ProcessingStartedAt is when webhook processing began
	// Used for debugging stuck webhooks and monitoring processing duration
	ProcessingStartedAt *time.Time `db:"processing_started_at" json:"processing_started_at,omitempty"`

	// ProcessingCompletedAt is when webhook processing finished successfully
	// Should match ProcessedAt when processing succeeds
	ProcessingCompletedAt *time.Time `db:"processing_completed_at" json:"processing_completed_at,omitempty"`

	// ProcessingError contains error message if processing failed
	// Used for debugging and manual investigation of failed webhooks
	ProcessingError *string `db:"processing_error" json:"processing_error,omitempty"`

	// RetryCount tracks number of processing attempts (0-5)
	// Incremented after each failed attempt, used with exponential backoff
	RetryCount int `db:"retry_count" json:"retry_count"`

	// NextRetryAt is scheduled timestamp for next retry attempt
	// Calculated using exponential backoff: 1m, 5m, 15m, 1h, 6h
	// NULL if no retry scheduled or max retries (5) exceeded
	NextRetryAt *time.Time `db:"next_retry_at" json:"next_retry_at,omitempty"`

	// CreatedAt is when this webhook event was received
	CreatedAt time.Time `db:"created_at" json:"created_at"`

	// UpdatedAt is when this webhook event was last updated
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
}
