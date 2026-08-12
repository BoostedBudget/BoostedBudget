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

// InvalidWebhookAttemptRepository records bounded metadata about rejected (unsigned or
// invalid-signature) webhook deliveries. This is the audit signal that survives now that invalid
// attempts are no longer persisted into the full webhook tables (see paddle_webhook.go,
// plaid_webhook.go).
type InvalidWebhookAttemptRepository interface {
	// Record inserts one invalid-attempt row (provider, body length, SHA-256 digest of the body —
	// never the body itself) and trims that provider's rows back to invalidWebhookAttemptCap, in
	// the same transaction.
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
// recent rows for ONE provider ($2) — by created_at, tie-broken by id for determinism when
// timestamps collide.
//
// Issue #1514 (Codex review round 1, Important 2): this used to rank EVERY row in the table via
// PARTITION BY provider, i.e. a full-table scan on every single insert — non-trivial synchronized
// DB work triggered directly by unauthenticated attacker input, self-inflicting a throughput
// bottleneck under exactly the flood the cap exists to survive. Scoping the ranking to
// `WHERE provider = $2` up front makes each call's cost O(rows for this one provider), which is
// itself bounded by invalidWebhookAttemptCap (this same query enforces that bound) — so the cost
// per request is a small constant, independent of total attack volume or the other provider's
// row count. Mathematically equivalent to the old PARTITION BY provider ranking restricted to one
// partition, just without scanning the rest of the table to get there.
const trimInvalidWebhookAttemptsQuery = `
	DELETE FROM invalid_webhook_attempts
	WHERE provider = $2 AND id IN (
		SELECT id FROM (
			SELECT id, ROW_NUMBER() OVER (
				ORDER BY created_at DESC, id DESC
			) AS rn
			FROM invalid_webhook_attempts
			WHERE provider = $2
		) ranked
		WHERE ranked.rn > $1
	)
`

// invalidWebhookAttemptLockKeyQuery derives a PER-PROVIDER pg_advisory_xact_lock key from the
// provider string, matching idempotency_repository.go's existing md5-hash-to-bigint convention
// for a namespaced lock. Issue #1514 (Codex review round 1, Important 2): the original
// implementation used a single lock key shared across ALL providers, so a Paddle attack flood
// fully serialized against legitimate Plaid rejections (and vice versa) — an unrelated provider's
// request-handling cost was held hostage by the one under attack. A lock keyed by provider still
// serializes concurrent Record calls for the SAME provider (needed so the trim above never ranks
// against a row count that doesn't yet reflect a concurrent sibling insert — see the concurrency
// integration test), while leaving Paddle and Plaid rejections free to proceed independently.
const invalidWebhookAttemptLockKeyQuery = `SELECT pg_advisory_xact_lock(('x'||substr(md5('invalid_webhook_attempt:'||$1),1,16))::bit(64)::bigint)`

// Record inserts a bounded invalid-attempt row and trims that provider's rows back to the cap,
// both inside one transaction so the cap can never be exceeded even under concurrent inserts for
// the same provider. The trim is scoped to this call's own provider (see
// trimInvalidWebhookAttemptsQuery), and the advisory lock is keyed per-provider (see
// invalidWebhookAttemptLockKeyQuery) — bounding both the DB work and the lock contention this
// unauthenticated-input-triggered call can impose to O(this one provider's capped row count),
// regardless of overall attack volume or the other provider's traffic.
func (r *invalidWebhookAttemptRepository) Record(ctx context.Context, provider string, bodyLength int, bodySHA256 string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin invalid webhook attempt transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if _, err := tx.Exec(ctx, invalidWebhookAttemptLockKeyQuery, provider); err != nil {
		return fmt.Errorf("failed to acquire invalid webhook attempt lock: %w", err)
	}

	if _, err := tx.Exec(ctx, insertInvalidWebhookAttemptQuery, provider, bodyLength, bodySHA256); err != nil {
		return fmt.Errorf("failed to insert invalid webhook attempt: %w", err)
	}

	if _, err := tx.Exec(ctx, trimInvalidWebhookAttemptsQuery, invalidWebhookAttemptCap, provider); err != nil {
		return fmt.Errorf("failed to trim invalid webhook attempts: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit invalid webhook attempt transaction: %w", err)
	}

	return nil
}
