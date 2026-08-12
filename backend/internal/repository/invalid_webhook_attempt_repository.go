package repository

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
)

// invalidWebhookAttemptCap bounds invalid_webhook_attempts to at most this many rows PER
// PROVIDER (not a single shared budget across providers).
//
// Issue #1514: unsigned/invalid-signature webhook deliveries are recorded here (bounded metadata
// only — never the raw payload, never an attacker-chosen identifier) instead of the full
// paddle_webhook_events / webhook_events tables. Fixed-size rows are still unbounded in count, so
// Record periodically trims a provider's rows back to this cap (see invalidWebhookAttemptTrimInterval)
// — an attacker sending unlimited invalid attempts can never grow this table past
// invalidWebhookAttemptCap + invalidWebhookAttemptTrimInterval - 1 rows for their provider (the
// explicit hard ceiling; see trim scheduling doc below).
//
// Per-provider (Codex adversarial review finding, Medium): a flood against one provider (e.g.
// Paddle) must not be able to trim away another provider's (Plaid) unrelated audit history — the
// trim query below is scoped to one provider rather than capping the table as a single shared pool.
const invalidWebhookAttemptCap = 10000

// invalidWebhookAttemptTrimInterval controls how often Record pays for the advisory-lock+DELETE
// trim step, per provider.
//
// Issue #1514 (Codex review round 2, Important 2 — the per-request-cost half of the finding):
// per-provider lock keys and per-provider trim scoping (round 1) fixed CROSS-provider contention,
// but every rejected webhook for a SINGLE targeted provider still paid a full
// BEGIN → advisory-lock → INSERT → ranked DELETE → COMMIT, fully serialized against every other
// rejection for that same provider — exactly the realistic single-provider flood shape. That is
// still "attacker input triggers synchronized DB work," just with a smaller blast radius.
//
// Record now does the INSERT unconditionally with NO transaction and NO lock (see Record) — that
// plain, unlocked, uncontended INSERT is the steady-state cost of a rejected webhook. The
// (comparatively expensive) advisory-lock+trim step only runs once every
// invalidWebhookAttemptTrimInterval calls FOR THAT PROVIDER, tracked by an in-process atomic
// counter (invalidWebhookAttemptRepository.insertCounters) — so (interval-1)/interval of all
// rejections pay nothing beyond the insert.
//
// Explicit hard ceiling (required by the round-2 review ruling): the counter is a strictly
// increasing, atomically-incremented sequence per provider, so trims fire at exactly every
// Nth call in insertion order — never less often. Between two trims, at most
// invalidWebhookAttemptTrimInterval-1 rows can accumulate beyond the cap the previous trim
// enforced. The row count for a provider can therefore transiently exceed
// invalidWebhookAttemptCap, but NEVER exceeds:
//
//	invalidWebhookAttemptCap + invalidWebhookAttemptTrimInterval - 1
//
// This is proven by TestInvalidWebhookAttemptCap_ConcurrentInserts_Integration, which still
// asserts the tighter (interval=1, i.e. every-call-trims) bound unmodified, plus
// TestInvalidWebhookAttemptCap_AmortisedTrim_HardCeiling_Integration, which asserts the looser
// (interval>1) bound explicitly.
const invalidWebhookAttemptTrimInterval = 50

// InvalidWebhookAttemptRepository records bounded metadata about rejected (unsigned or
// invalid-signature) webhook deliveries. This is the audit signal that survives now that invalid
// attempts are no longer persisted into the full webhook tables (see paddle_webhook.go,
// plaid_webhook.go).
type InvalidWebhookAttemptRepository interface {
	// Record inserts one invalid-attempt row (provider, body length, SHA-256 digest of the body —
	// never the body itself). Trimming that provider's rows back toward invalidWebhookAttemptCap
	// happens periodically, not on every call — see invalidWebhookAttemptTrimInterval.
	Record(ctx context.Context, provider string, bodyLength int, bodySHA256 string) error
}

type invalidWebhookAttemptRepository struct {
	db DB
	// trimInterval overrides invalidWebhookAttemptTrimInterval when non-zero. Exists so tests
	// (same package) can construct a repository directly with a small interval to make trim
	// scheduling deterministic and fast to exercise, without changing the production default.
	trimInterval int
	// cap overrides invalidWebhookAttemptCap when non-zero. Same rationale as trimInterval: lets
	// integration tests exercise the real Record()/trim() path against a small, fast-to-reach
	// bound instead of the production 10,000-row cap.
	cap int
	// insertCounters tracks, per provider, how many successful inserts this repository instance
	// has performed — the in-process heuristic that decides when to pay for the lock+trim step.
	// Deliberately NOT the source of truth for row count (the DB is); losing this counter (e.g. on
	// process restart) only delays the next trim, it can never cause an insert to be lost or the
	// cap check itself to be wrong once a trim does run.
	insertCounters sync.Map // map[string]*atomic.Int64
}

// NewInvalidWebhookAttemptRepository creates a new InvalidWebhookAttemptRepository instance.
func NewInvalidWebhookAttemptRepository(db DB) InvalidWebhookAttemptRepository {
	return &invalidWebhookAttemptRepository{
		db:           db,
		trimInterval: invalidWebhookAttemptTrimInterval,
		cap:          invalidWebhookAttemptCap,
	}
}

// effectiveTrimInterval returns the configured trim interval, falling back to the production
// default for any repository not built via the constructor above with an explicit override (e.g.
// a zero-value struct).
func (r *invalidWebhookAttemptRepository) effectiveTrimInterval() int64 {
	if r.trimInterval <= 0 {
		return invalidWebhookAttemptTrimInterval
	}
	return int64(r.trimInterval)
}

// effectiveCap returns the configured per-provider cap, falling back to the production default
// for any repository not built via the constructor above with an explicit override.
func (r *invalidWebhookAttemptRepository) effectiveCap() int {
	if r.cap <= 0 {
		return invalidWebhookAttemptCap
	}
	return r.cap
}

// nextInsertCount atomically increments and returns this provider's insert counter.
func (r *invalidWebhookAttemptRepository) nextInsertCount(provider string) int64 {
	v, _ := r.insertCounters.LoadOrStore(provider, new(atomic.Int64))
	return v.(*atomic.Int64).Add(1)
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

// Record inserts a bounded invalid-attempt row. The insert itself is a single, unlocked,
// autocommitting Exec — NO transaction, NO advisory lock — because this call is triggered
// directly by unauthenticated attacker input, and that steady-state cost must stay cheap
// regardless of attack volume (Issue #1514, Codex review round 2, Important 2).
//
// The (comparatively expensive) trim — advisory lock + bounded DELETE — runs only once every
// effectiveTrimInterval() calls for this provider, per nextInsertCount's atomic counter. See the
// invalidWebhookAttemptTrimInterval doc comment for the exact hard ceiling this guarantees.
func (r *invalidWebhookAttemptRepository) Record(ctx context.Context, provider string, bodyLength int, bodySHA256 string) error {
	if _, err := r.db.Exec(ctx, insertInvalidWebhookAttemptQuery, provider, bodyLength, bodySHA256); err != nil {
		return fmt.Errorf("failed to insert invalid webhook attempt: %w", err)
	}

	if r.nextInsertCount(provider)%r.effectiveTrimInterval() != 0 {
		return nil
	}

	return r.trim(ctx, provider)
}

// trim runs the advisory-lock+DELETE step that enforces invalidWebhookAttemptCap for one
// provider. Both statements run inside one transaction so the cap can never be exceeded even
// under concurrent trims for the same provider (see invalidWebhookAttemptLockKeyQuery). Called
// only periodically by Record — see invalidWebhookAttemptTrimInterval.
func (r *invalidWebhookAttemptRepository) trim(ctx context.Context, provider string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin invalid webhook attempt trim transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if _, err := tx.Exec(ctx, invalidWebhookAttemptLockKeyQuery, provider); err != nil {
		return fmt.Errorf("failed to acquire invalid webhook attempt lock: %w", err)
	}

	if _, err := tx.Exec(ctx, trimInvalidWebhookAttemptsQuery, r.effectiveCap(), provider); err != nil {
		return fmt.Errorf("failed to trim invalid webhook attempts: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit invalid webhook attempt trim transaction: %w", err)
	}

	return nil
}
