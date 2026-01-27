package domain

import (
	"encoding/json"
	"time"
)

// PaddleWebhookEvent represents a logged Paddle webhook with signature verification and processing status.
// Issue #45, #46: Paddle Payment Integration
type PaddleWebhookEvent struct {
	ID                 string          `db:"id" json:"id"`
	EventID            string          `db:"event_id" json:"event_id" validate:"required"` // Paddle's unique event identifier
	EventType          string          `db:"event_type" json:"event_type" validate:"required"`
	Payload            json.RawMessage `db:"payload" json:"payload" validate:"required"` // Complete webhook body as JSON
	Signature          *string         `db:"signature" json:"signature,omitempty"` // HMAC-SHA256 signature
	SignatureValid     bool            `db:"signature_valid" json:"signature_valid"`
	Processed          bool            `db:"processed" json:"processed"` // Used for idempotency
	ProcessedAt        *time.Time      `db:"processed_at" json:"processed_at,omitempty"`
	ProcessingError    *string         `db:"processing_error" json:"processing_error,omitempty"`
	ProcessingAttempts int             `db:"processing_attempts" json:"processing_attempts"` // For retry logic
	CreatedAt          time.Time       `db:"created_at" json:"created_at"`
	UpdatedAt          time.Time       `db:"updated_at" json:"updated_at"`
}

// PaddleTransaction represents a payment transaction audit log entry.
// Issue #45: Paddle Payment Service Implementation
type PaddleTransaction struct {
	ID                 string          `db:"id" json:"id"`
	UserID             string          `db:"user_id" json:"user_id" validate:"required"`
	TransactionID      string          `db:"transaction_id" json:"transaction_id" validate:"required"` // Paddle's ID
	SubscriptionID     *string         `db:"subscription_id" json:"subscription_id,omitempty"` // Nullable for one-time payments
	CustomerID         *string         `db:"customer_id" json:"customer_id,omitempty"`
	Amount             float64         `db:"amount" json:"amount" validate:"required"` // In currency units
	Currency           string          `db:"currency" json:"currency" validate:"required"` // ISO 4217 code
	Status             string          `db:"status" json:"status" validate:"required"` // completed, failed, refunded, etc.
	PaymentMethodType  *string         `db:"payment_method_type" json:"payment_method_type,omitempty"`
	BillingPeriodStart *time.Time      `db:"billing_period_start" json:"billing_period_start,omitempty"`
	BillingPeriodEnd   *time.Time      `db:"billing_period_end" json:"billing_period_end,omitempty"`
	Payload            json.RawMessage `db:"payload" json:"payload" validate:"required"` // Complete transaction data as JSON
	CreatedAt          time.Time       `db:"created_at" json:"created_at"`
	UpdatedAt          time.Time       `db:"updated_at" json:"updated_at"`
}
