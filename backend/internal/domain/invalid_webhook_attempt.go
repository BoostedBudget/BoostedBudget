package domain

import "time"

// InvalidWebhookAttempt is bounded, non-identifying metadata recorded for a webhook delivery
// rejected for having a missing or cryptographically invalid signature. Issue #1514.
//
// Deliberately does NOT carry the raw request body or any attacker-chosen identifier (e.g. a
// Paddle event_id) — only enough to audit that a rejection happened: when, from which provider,
// how large the body was, and a SHA-256 digest of it. The backing table
// (invalid_webhook_attempts) enforces a hard row cap so this signal can never grow unboundedly
// under sustained attack; see repository.InvalidWebhookAttemptRepository.
type InvalidWebhookAttempt struct {
	ID         string    `db:"id" json:"id"`
	Provider   string    `db:"provider" json:"provider"`
	BodyLength int       `db:"body_length" json:"body_length"`
	BodySHA256 string    `db:"body_sha256" json:"body_sha256"`
	CreatedAt  time.Time `db:"created_at" json:"created_at"`
}
