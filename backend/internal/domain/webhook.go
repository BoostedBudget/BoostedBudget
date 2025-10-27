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

	// CreatedAt is when this webhook event was received
	CreatedAt time.Time `db:"created_at" json:"created_at"`

	// UpdatedAt is when this webhook event was last updated
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
}
