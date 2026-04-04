package domain

import (
	"errors"
	"time"
)

// Notification represents a user notification for item errors, authentication requirements, and other events.
// Supports in-app notifications and email notifications (email via EmailService).
// Issue #257: Item Error Handler
type Notification struct {
	ID          string     `json:"id" db:"id"`
	UserID      string     `json:"user_id" db:"user_id"`
	Type        string     `json:"type" db:"type"` // item_error, login_required, pending_expiration, permission_revoked, transaction_sync, budget_alert
	Title       string     `json:"title" db:"title"`
	Message     string     `json:"message" db:"message"`
	ActionURL   *string    `json:"action_url,omitempty" db:"action_url"` // URL for user to take action (e.g., re-authenticate)
	ActionLabel *string    `json:"action_label,omitempty" db:"action_label"` // Label for action button (e.g., "Reconnect Bank")
	Read        bool       `json:"read" db:"read"`
	SentEmail   bool       `json:"sent_email" db:"sent_email"`
	EmailSentAt *time.Time `json:"email_sent_at,omitempty" db:"email_sent_at"`
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at" db:"updated_at"`
}

// Validate checks that the Notification has all required fields
func (n *Notification) Validate() error {
	if n.UserID == "" {
		return errors.New("user_id is required")
	}
	if n.Type == "" {
		return errors.New("type is required")
	}
	if n.Title == "" {
		return errors.New("title is required")
	}
	if n.Message == "" {
		return errors.New("message is required")
	}

	validTypes := map[string]bool{
		"item_error":              true,
		"login_required":          true,
		"pending_expiration":      true,
		"permission_revoked":      true,
		"transaction_sync":        true,
		"budget_alert":            true,
		"payment_failed":          true,
		"subscription_past_due":   true,
		"subscription_cancelled":  true,
		"subscription_resumed":    true,
		"subscription_activated":  true,
		"subscription_trialing":   true,
		"transaction_past_due":    true,
	}
	if !validTypes[n.Type] {
		return errors.New("invalid notification type")
	}

	return nil
}
