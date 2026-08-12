package repository

import (
	"context"
	"fmt"
)

// invalidWebhookAttemptCap bounds invalid_webhook_attempts to at most this many rows PER
// PROVIDER (not a single shared budget across providers).
//
// Issue #1514: unsigned/invalid-signature webhook deliveries are recorded here (bounded metadata
// only — never the raw payload, never an attacker-chosen identifier) instead of the full
// paddle_webhook_events / webhook_events tables. Fixed-size rows are still unbounded in count, so
// every Record call trims the table back to this cap in the same transaction as the insert — an
// attacker sending unlimited invalid attempts can never grow this table past
// invalidWebhookAttemptCap rows for their provider.
//
// Per-provider (Codex adversarial review finding, Medium): a flood against one provider (e.g.
// Paddle) must not be able to trim away another provider's (Plaid) unrelated audit history — the
// trim query below partitions by provider rather than capping the table as a single shared pool.
const invalidWebhookAttemptCap = 10000

// invalidWebhookAttemptLockKey is a fixed pg_advisory_xact_lock key serializing every Record
// call. Issue #1514 (Codex adversarial review finding, Medium): without this, two concurrent
// transactions can each insert-then-trim using a row count/order that doesn't yet reflect the
// other's insert, letting the row count exceed invalidWebhookAttemptCap under load. The lock is
// released automatically when the surrounding transaction commits or rolls back (same pattern as
// idempotency_repository.go's per-key advisory lock). A single global key is fine here — unlike
// that per-key case, this lock only ever guards this one table's insert+trim critical section, so
// there is nothing to differentiate by argument.
const invalidWebhookAttemptLockKey = 151400 // arbitrary, unique to this feature (#1514, table 00)

// InvalidWebhookAttemptRepository records bounded metadata about rejected (unsigned or
// invalid-signature) webhook deliveries. This is the audit signal that survives now that invalid
// attempts are no longer persisted into the full webhook tables (see paddle_webhook.go,
// plaid_webhook.go).
type InvalidWebhookAttemptRepository interface {
	// Record inserts one invalid-attempt row (provider, body length, SHA-256 digest of the body —
	// never the body itself) and trims the table back to invalidWebhookAttemptCap rows, in the
	// same transaction.
	Record(ctx context.Context, provider string, bodyLength int, bodySHA256 string) error
}

type invalidWebhookAttemptRepository struct {
	db DB
}

// NewInvalidWebhookAttemptRepository creates a new InvalidWebhookAttemptRepository instance.
func NewInvalidWebhookAttemptRepository(db DB) InvalidWebhookAttemptRepository {
	return &invalidWebhookAttemptRepository{db: db}
}

const insertInvalidWebhookAttemptQuery = `
	INSERT INTO invalid_webhook_attempts (provider, body_length, body_sha256)
	VALUES ($1, $2, $3)
`

// trimInvalidWebhookAttemptsQuery deletes every row beyond the invalidWebhookAttemptCap most
// recent rows PER PROVIDER (by created_at, tie-broken by id for determinism when timestamps
// collide) — partitioning by provider so a flood on one provider cannot evict another provider's
// unrelated audit history.
const trimInvalidWebhookAttemptsQuery = `
	DELETE FROM invalid_webhook_attempts
	WHERE id IN (
		SELECT id FROM (
			SELECT id, ROW_NUMBER() OVER (
				PARTITION BY provider
				ORDER BY created_at DESC, id DESC
			) AS rn
			FROM invalid_webhook_attempts
		) ranked
		WHERE ranked.rn > $1
	)
`

// Record inserts a bounded invalid-attempt row and trims the table back to the per-provider cap,
// both inside one transaction so the cap can never be exceeded even under concurrent inserts. A
// pg_advisory_xact_lock serializes concurrent Record calls (across all providers) for the
// duration of the transaction, so no two callers ever compute the trim's row ranking against a
// row count/order that doesn't yet reflect each other's insert.
func (r *invalidWebhookAttemptRepository) Record(ctx context.Context, provider string, bodyLength int, bodySHA256 string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin invalid webhook attempt transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", invalidWebhookAttemptLockKey); err != nil {
		return fmt.Errorf("failed to acquire invalid webhook attempt lock: %w", err)
	}

	if _, err := tx.Exec(ctx, insertInvalidWebhookAttemptQuery, provider, bodyLength, bodySHA256); err != nil {
		return fmt.Errorf("failed to insert invalid webhook attempt: %w", err)
	}

	if _, err := tx.Exec(ctx, trimInvalidWebhookAttemptsQuery, invalidWebhookAttemptCap); err != nil {
		return fmt.Errorf("failed to trim invalid webhook attempts: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit invalid webhook attempt transaction: %w", err)
	}

	return nil
}
