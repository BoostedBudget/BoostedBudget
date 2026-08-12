package repository

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// invalidWebhookAttemptCap bounds invalid_webhook_attempts to at most this many rows PER
// PROVIDER (not a single shared budget across providers), subject to the transient-overshoot and
// multi-instance caveats documented on invalidWebhookAttemptTrimInterval below — read that
// comment before treating this as an unconditional guarantee.
//
// Issue #1514: unsigned/invalid-signature webhook deliveries are recorded here (bounded metadata
// only — never the raw payload, never an attacker-chosen identifier) instead of the full
// paddle_webhook_events / webhook_events tables. Fixed-size rows are still unbounded in count, so
// Record periodically trims a provider's rows back to this cap (see invalidWebhookAttemptTrimInterval).
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
// Bound, PRECISELY stated (Codex review round 3 — the round-2 doc comment overstated this as an
// unconditional "NEVER exceeds cap+interval-1", which is wrong on two axes; G3 below adds a THIRD
// axis the round-3 comment itself overstated — read point 3 before treating this as complete):
//
//  1. Single-process, instantaneous vs. completed-call-boundary: the trim-triggering call's own
//     INSERT commits (as its own, separate, unlocked Exec) BEFORE that same call's trim runs. So
//     for the brief window between that INSERT committing and that call's own trim committing, a
//     concurrent reader on another connection (e.g. a plain `SELECT COUNT(*)`) can observe up to
//     invalidWebhookAttemptCap + invalidWebhookAttemptTrimInterval rows — not interval-1. Once
//     that one call finishes (trim included) and at every OTHER point where no Record call for
//     this provider is currently in flight, the bound is the tighter
//     invalidWebhookAttemptCap + invalidWebhookAttemptTrimInterval - 1 — because the (interval-1)
//     calls since the last trim have each landed a row with nothing yet to trim them.
//
//  2. Multi-instance: insertCounters is IN-PROCESS state (a struct field, not backed by the DB or
//     shared across processes) — deliberately: making it shared/DB-backed would reintroduce the
//     exact per-request DB cost this design removes. This repo's `fly.toml` runs with
//     `auto_start_machines = true` and no explicit machine cap (see fly.toml:44-50), so N
//     concurrently-running instances is a live configuration, not a hypothetical — and each
//     instance's counter starts at zero, independently, including across rolling deploys. With N
//     instances all serving rejections for the SAME provider, each instance's own local schedule
//     can be arbitrarily out of phase with every other instance's, so the worst case across the
//     whole fleet is roughly:
//
//     invalidWebhookAttemptCap + N × (invalidWebhookAttemptTrimInterval - 1)
//
//     This is NOT a global guarantee the in-process counter can make on its own — it's the
//     accepted, explicitly-sized residual (a few hundred extra rows on a 10,000-row cap at
//     realistic fleet sizes) of choosing a cheap per-process heuristic over a DB-backed or shared
//     one. It stays small and self-correcting because ANY single instance's trim, whenever it
//     fires, drains the WHOLE provider's row count (not just that instance's own inserts) back
//     down to invalidWebhookAttemptCap — so the fleet-wide count cannot drift upward indefinitely;
//     it is bounded by whichever instance is "most out of phase" at any moment, not by cumulative
//     attack volume.
//
//  3. Restarts (Issue #1514 G3 — this is the correction to what round 3 got wrong): point 2 says
//     "each instance's counter starts at zero... including across rolling deploys" but never
//     followed that to its conclusion — an instance that restarts before its own counter ever
//     reaches invalidWebhookAttemptTrimInterval never trims AT ALL during its lifetime, and Fly
//     restarts machines on every deploy, scale event, and host migration. Nothing about points 1-2
//     assumes trims eventually fire; under sustained restart pressure (an attacker sending fewer
//     than invalidWebhookAttemptTrimInterval invalid attempts per process lifetime, or ordinary
//     traffic doing the same), they might not, and the "self-correcting" claim above silently
//     assumed they would. Two changes close this:
//
//     (a) NewInvalidWebhookAttemptRepository's returned instance forces an eager trim on the
//     FIRST Record call for each provider (see startupTrimEnabled below), regardless of the
//     interval counter — so no instance can complete its entire lifetime without trimming at
//     least once. This does NOT touch the steady-state per-request cost: it fires exactly once
//     per (instance, provider) pair, not per request, and structs built directly (bypassing the
//     constructor — every existing unit/integration test that exercises the interval-amortised
//     schedule in isolation) default to false and are completely unaffected.
//
//     (b) trim() additionally deletes rows older than the configured retention window (see
//     retentionDays / effectiveRetentionWindow), independent of row count entirely. Even in the
//     pathological case where a provider's rows are spread across many short-lived instances that
//     each land only a handful of rows, age-based retention reclaims them on a purely time-based
//     schedule — it does not depend on any instance ever reaching the count-based trim at all.
//
//     With (a) and (b), the residual guarantee is: any given process either never inserts a row
//     for a provider (nothing to bound), or its first insert for that provider forces a trim
//     within that same call — so points 1 and 2's bounds hold from that first call onward for the
//     remainder of that instance's life.
//
//     Residual case (b) does NOT eliminate — read as a disclosure, not a hedge: trim()'s age-DELETE
//     only ever runs from inside Record, which only ever runs in response to an incoming rejected
//     webhook. A QUIESCENT provider — one that stops receiving invalid attempts entirely, with or
//     without a restart — has no future Record call to trigger the age-based sweep, so its
//     already-written rows persist past the retention window indefinitely, until (if ever) that
//     provider is rejected again. This is the same class of gap G3 exists to fix (a trim that
//     depends on a future call happening), just on the age axis instead of the count axis, and this
//     codebase does not currently have a call-independent (e.g. cron/periodic-job) sweep to close
//     it. TestInvalidWebhookAttemptCap_AgeRetention_Integration proves the age-DELETE logic is
//     correct when it does run; it does not and cannot prove the sweep runs on a wall-clock
//     schedule independent of traffic, because it isn't one.
//
// TestInvalidWebhookAttemptCap_ConcurrentInserts_Integration still asserts the tight,
// interval=1 (every-call-trims) bound unmodified, driving this same Record()/trim() code.
// TestInvalidWebhookAttemptCap_AmortisedTrim_HardCeiling_Integration asserts the single-process,
// completed-call-boundary bound from point 1 above (cap+interval-1) — using sequential (not
// concurrent) calls specifically so the exact resulting count is deterministic, not merely
// probable — and additionally asserts the count DID exceed the tight cap, so the test cannot pass
// vacuously if amortisation were silently disabled.
// TestInvalidWebhookAttemptCap_RestartHole_Integration asserts point 3(a) — that rows do not grow
// without bound across many restarts that individually never reach the interval boundary.
// TestInvalidWebhookAttemptCap_AgeRetention_Integration asserts point 3(b) in isolation.
const invalidWebhookAttemptTrimInterval = 50

// invalidWebhookAttemptRetentionDefaultDays is the fallback age-based retention window (Issue
// #1514 G3, point 3(b) above) used by any repository not constructed with an explicit override —
// i.e. the production default when NewInvalidWebhookAttemptRepository's caller passes 0 or a
// negative value. 30 days mirrors this project's existing GDPR deletion-grace convention
// (config.Config.DeletionGracePeriodDays) rather than inventing a new, unrelated number: rejected
// webhook attempts are a short-lived operational audit signal, not something with an independent
// retention requirement, so reusing an existing, already-justified window keeps the number of
// retention policies in this codebase from growing needlessly. Configurable via
// INVALID_WEBHOOK_ATTEMPT_RETENTION_DAYS (config.Config.InvalidWebhookAttemptRetentionDays),
// bounded by config.InvalidWebhookAttemptRetentionMaxDays.
const invalidWebhookAttemptRetentionDefaultDays = 30

// InvalidWebhookAttemptRepository records bounded metadata about rejected (unsigned or
// invalid-signature) webhook deliveries. This is the audit signal that survives now that invalid
// attempts are no longer persisted into the full webhook tables (see paddle_webhook.go,
// plaid_webhook.go).
type InvalidWebhookAttemptRepository interface {
	// Record inserts one invalid-attempt row (provider, body length, SHA-256 digest of the body —
	// never the body itself). Trimming that provider's rows back toward invalidWebhookAttemptCap
	// happens periodically, not on every call — see invalidWebhookAttemptTrimInterval.
	Record(ctx context.Context, provider string, bodyLength int, bodySHA256 string) error

	// GetStats24h reads back the ONLY surviving audit signal for rejected (unsigned or
	// invalid-signature) webhook deliveries over the trailing 24 hours: a count per provider, and
	// the most recent attempt's timestamp across all providers (nil if there were none).
	//
	// Issue #1514, requirement #5 (Phase 2 gap fix). Phase 1 satisfied the parenthetical of
	// "replace the invalid-attempt audit signal the admin UI relies on (bounded metadata + a
	// counter)" by writing Record above, but never added a way to READ it back — this repository
	// was write-only until this method existed, and /admin/webhooks (which reads
	// paddle_webhook_events, a table these rows are never written to) could no longer show a
	// forged-webhook flood at all. This is the read half that closes that gap.
	//
	// ⚠ NOT THE SAME THING AS domain.PaddleStatusMetrics.FailedWebhooks24h. That field counts
	// paddle_webhook_events rows with processing_error IS NOT NULL — i.e. webhooks that PASSED
	// signature verification and then failed while being processed. This method counts webhooks
	// that were REJECTED before ever being written to that full webhook table, because their
	// signature did not verify (or was absent) — they are still persisted, into this bounded,
	// metadata-only table instead. A forgery flood and a downstream processing bug are unrelated
	// failure modes; wiring both to the same dashboard number would hide whichever one isn't
	// currently spiking. See domain.RejectedWebhookAttemptMetrics for the read side of this
	// distinction.
	GetStats24h(ctx context.Context) (byProvider map[string]int64, mostRecentAt *time.Time, err error)
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
	// retentionDays overrides invalidWebhookAttemptRetentionDefaultDays when non-zero (Issue #1514
	// G3). Age-based retention is orthogonal to the count-based cap above: it deletes rows purely
	// on elapsed time, so it still reclaims rows even for a provider whose instance never reaches
	// the count-based trim interval at all.
	retentionDays int
	// insertCounters tracks, per provider, how many successful inserts this repository instance
	// has performed — the in-process heuristic that decides when to pay for the lock+trim step.
	// Deliberately NOT the source of truth for row count (the DB is); losing this counter (e.g. on
	// process restart) only delays the next trim, it can never cause an insert to be lost or the
	// cap check itself to be wrong once a trim does run.
	insertCounters sync.Map // map[string]*atomic.Int64
	// startupTrimEnabled marks this instance as one whose very first Record call for any given
	// provider should force an eager trim regardless of the interval counter (Issue #1514 G3,
	// point 3(a) on invalidWebhookAttemptTrimInterval's doc comment — closes the restart hole).
	//
	// Deliberately a plain bool, not atomic: it is written exactly once, by
	// NewInvalidWebhookAttemptRepository, before the returned instance is ever handed to a caller
	// or a second goroutine — so there is no concurrent-write race to guard against, only
	// concurrent reads of an already-settled value. Zero value (false) is deliberate: only the
	// production constructor sets it true. Every existing unit/integration test that builds this
	// struct directly (bypassing the constructor) to exercise the interval-amortised trim schedule
	// in isolation gets false and is completely unaffected by this change.
	startupTrimEnabled bool
}

// NewInvalidWebhookAttemptRepository creates a new InvalidWebhookAttemptRepository instance.
// retentionDays configures the age-based retention window (Issue #1514 G3); pass 0 to use
// invalidWebhookAttemptRetentionDefaultDays (production callers should pass
// config.Config.InvalidWebhookAttemptRetentionDays).
func NewInvalidWebhookAttemptRepository(db DB, retentionDays int) InvalidWebhookAttemptRepository {
	return &invalidWebhookAttemptRepository{
		db:                 db,
		trimInterval:       invalidWebhookAttemptTrimInterval,
		cap:                invalidWebhookAttemptCap,
		retentionDays:      retentionDays,
		startupTrimEnabled: true,
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

// effectiveRetentionWindow returns the configured age-based retention window, falling back to
// invalidWebhookAttemptRetentionDefaultDays for any repository not built via the constructor with
// an explicit override (e.g. a zero-value struct, or one built directly by a test).
func (r *invalidWebhookAttemptRepository) effectiveRetentionWindow() time.Duration {
	days := r.retentionDays
	if days <= 0 {
		days = invalidWebhookAttemptRetentionDefaultDays
	}
	return time.Duration(days) * 24 * time.Hour
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

// trimInvalidWebhookAttemptsByAgeQuery deletes rows for ONE provider ($1) older than the supplied
// cutoff timestamp ($2). Issue #1514 G3: this is the age-based half of the retention bound — it
// runs unconditionally inside trim() (same advisory-lock transaction as the count-based DELETE
// above), so it inherits trim()'s existing amortised/startup-only call schedule and never adds
// per-request cost. The cutoff is computed once in Go (time.Now().Add(-window)) rather than as a
// SQL `NOW() - INTERVAL` expression so integration tests can assert exact keep/delete behavior
// against explicit, non-relative created_at fixture values.
const trimInvalidWebhookAttemptsByAgeQuery = `
	DELETE FROM invalid_webhook_attempts
	WHERE provider = $1 AND created_at < $2
`

// Record inserts a bounded invalid-attempt row. The insert itself is a single, unlocked,
// autocommitting Exec — NO transaction, NO advisory lock — because this call is triggered
// directly by unauthenticated attacker input, and that steady-state cost must stay cheap
// regardless of attack volume (Issue #1514, Codex review round 2, Important 2).
//
// The (comparatively expensive) trim — advisory lock + bounded DELETE, count-based and age-based
// — runs only once every effectiveTrimInterval() calls for this provider (per nextInsertCount's
// atomic counter), OR once, unconditionally, on the first call for this provider through an
// instance built via NewInvalidWebhookAttemptRepository (Issue #1514 G3 — closes the restart hole
// where an instance's counter never reaches the interval boundary). See the
// invalidWebhookAttemptTrimInterval doc comment for the exact hard ceiling this guarantees.
func (r *invalidWebhookAttemptRepository) Record(ctx context.Context, provider string, bodyLength int, bodySHA256 string) error {
	if _, err := r.db.Exec(ctx, insertInvalidWebhookAttemptQuery, provider, bodyLength, bodySHA256); err != nil {
		return fmt.Errorf("failed to insert invalid webhook attempt: %w", err)
	}

	count := r.nextInsertCount(provider)
	// count == 1 is this provider's first-ever insert through THIS instance. When
	// startupTrimEnabled (set only by the production constructor), force a trim here regardless
	// of the interval counter — Issue #1514 G3: without this, an instance that restarts before
	// its counter reaches effectiveTrimInterval() never trims at all during its lifetime. This
	// adds cost to exactly one call per (instance, provider) pair, never per request.
	isForcedStartupTrim := r.startupTrimEnabled && count == 1
	if !isForcedStartupTrim && count%r.effectiveTrimInterval() != 0 {
		return nil
	}

	return r.trim(ctx, provider)
}

// trim runs the advisory-lock+DELETE step that enforces invalidWebhookAttemptCap AND the
// age-based retention window (Issue #1514 G3) for one provider. All statements run inside one
// transaction so neither bound can be violated even under concurrent trims for the same provider
// (see invalidWebhookAttemptLockKeyQuery). Called only periodically by Record (amortised interval)
// or once at startup per (instance, provider) — see invalidWebhookAttemptTrimInterval.
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

	// Age-based retention (Issue #1514 G3, point 3(b)): independent of the count-based cap above
	// — bounds growth on elapsed time alone, so a provider whose instance never reaches the
	// count-based trim interval still has its old rows reclaimed whenever a trim DOES run (the
	// startup-forced trim above guarantees at least one per instance).
	cutoff := time.Now().UTC().Add(-r.effectiveRetentionWindow())
	if _, err := tx.Exec(ctx, trimInvalidWebhookAttemptsByAgeQuery, provider, cutoff); err != nil {
		return fmt.Errorf("failed to trim invalid webhook attempts by age: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit invalid webhook attempt trim transaction: %w", err)
	}

	return nil
}

// statsByProviderSinceQuery is GetStats24h's SQL: one round trip, GROUP BY provider, with
// created_at filtered to the trailing 24h window computed by Postgres itself (`NOW() - INTERVAL
// '24 hours'`, matching SystemHealthRepository.GetPaddleWebhookStats24h's sibling query) rather
// than a cutoff computed in Go and bound as a parameter — Codex adversarial review caught that an
// app-clock cutoff can drift from the DB clock that actually wrote created_at (via its DEFAULT
// CURRENT_TIMESTAMP, see migration 000146), silently widening or narrowing the window on clock
// skew. Computing it in SQL keeps both sides of the comparison on the same clock.
//
// Deliberately NOT filtered on provider (this repository has no per-provider scoping need for
// reads, unlike Record's per-provider cap/trim) — index
// idx_invalid_webhook_attempts_created_at_provider (migration 000148) leads with created_at for
// exactly this shape: one bounded range scan from now back to the cutoff, with provider carried
// in the same index entry so no heap access is needed.
//
// Returns one row per provider that has ANY attempt since the cutoff — a provider with zero
// attempts in the window is simply absent from the result set, not a zero row; GetStats24h fills
// in the zero for any provider callers care about.
const statsByProviderSinceQuery = `
	SELECT provider, COUNT(*), MAX(created_at)
	FROM invalid_webhook_attempts
	WHERE created_at >= NOW() - INTERVAL '24 hours'
	GROUP BY provider
`

// GetStats24h implements the interface doc comment above. Single query; the per-provider counts
// and the overall most-recent timestamp are both derived from the one result set in Go rather
// than with a second round trip.
//
// ⚠ Total24h (the sum callers derive from this method's byProvider map) is bounded by however
// many rows Record's periodic trim has left standing for each provider — see
// invalidWebhookAttemptCap. Under a sustained flood that outpaces the trim schedule, the reported
// count can plateau at the cap well below the TRUE number of rejected attempts in the window; it
// is "how many rejected attempts are currently on record", not a lifetime/uncapped counter. The
// Prometheus counter recordInvalidWebhookAttempt increments (webhook_security.go) is the uncapped
// total, for whoever needs that instead.
func (r *invalidWebhookAttemptRepository) GetStats24h(ctx context.Context) (map[string]int64, *time.Time, error) {
	rows, err := r.db.Query(ctx, statsByProviderSinceQuery)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query invalid webhook attempt stats: %w", err)
	}
	defer rows.Close()

	byProvider := make(map[string]int64)
	var mostRecentAt *time.Time
	for rows.Next() {
		var provider string
		var count int64
		var maxCreatedAt time.Time
		if err := rows.Scan(&provider, &count, &maxCreatedAt); err != nil {
			return nil, nil, fmt.Errorf("failed to scan invalid webhook attempt stats row: %w", err)
		}
		byProvider[provider] = count
		if mostRecentAt == nil || maxCreatedAt.After(*mostRecentAt) {
			mostRecentAt = &maxCreatedAt
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("failed to iterate invalid webhook attempt stats rows: %w", err)
	}

	return byProvider, mostRecentAt, nil
}
