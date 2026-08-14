package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
)

// activeSecondsAccountingInterval is how often a still-degraded fallbackLimiter rolls its
// "elapsed while degraded" checkpoint forward into RedisLimiterFallbackActiveSeconds, so a
// long-running outage that never recovers still shows accumulated time instead of reporting
// nothing until (if ever) a recovery event occurs. 15s is coarse enough that it never matters on
// the hot path (this runs on its own goroutine, not per-request) and fine enough that "how long
// has this outage lasted so far" is never more than 15s stale.
const activeSecondsAccountingInterval = 15 * time.Second

// fallbackLimiter decorates a primary RateLimitBackend (in production, the Redis-backed GCRA
// limiter built in internal/ratelimitstore) with an always-available in-memory fallback, so a
// Redis outage degrades the rate limiter to per-machine, in-process buckets instead of failing
// the request path open (no limiting at all) or closed (every request rejected).
//
// # The lossy switch is ACCEPTED and DOCUMENTED, not engineered around
//
// Memory buckets have independent state per machine, so a client mid-budget on the Redis-backed
// fleet-wide bucket gets a FRESH budget on switchover to memory (and again on switchback). This is
// deliberate: transferring state would require reading the very Redis that is unavailable. The
// error direction here is toward availability, not correctness of the budget — the resulting
// burst is bounded by the configured limit, per machine, per switch, and is fully observable via
// RedisLimiterFallbackActivationsTotal (how many times), RedisLimiterBackendDegraded (is it
// happening right now), and a slog.Error (Sentry) event at the moment of each activation. This is
// a one-PR-at-a-time codebase; unifying the two backends' state is out of scope here.
//
// # Retry-After inconsistency, also documented rather than "fixed"
//
// The in-memory backend (see memoryBackend) always reports Retry-After == the configured window
// duration (e.g. 60s), exactly as the pre-#1531 limiter did — this preserves
// TestIPRateLimiter_429HasRetryAfterHeader and every other existing Retry-After assertion
// unmodified. The Redis backend (ratelimitstore.RedisBackend) reports the true GCRA-computed
// value, which is usually much smaller (often ~1-3s). The consequence: a client can observe
// Retry-After jump from ~3s to 60s the instant a fallback occurs. That inconsistency is a known,
// accepted follow-up (unifying the two Retry-After semantics is explicitly out of scope for this
// task) rather than something smuggled into this change.
//
// # context.WithoutCancel is load-bearing SECURITY, not tidiness
//
// If the primary's Allow call inherited the request's own context, a client that disconnects
// mid-request cancels that context, which would surface as a primary error indistinguishable from
// a genuine Redis failure -- and would feed the circuit breaker and the fallback/degraded state
// exactly like one. An attacker who opens and immediately aborts a handful of connections could
// then force the limiter into per-machine memory mode on demand, deliberately switching off the
// fleet-wide limit they are themselves subject to. context.WithoutCancel(ctx) detaches the probe
// from the caller's cancellation (see internal/health/check.go for the identical problem solved
// the same way for dependency health checks); the explicit context.WithTimeout that follows is
// what re-introduces a bound, since WithoutCancel strips the parent deadline too.
//
// # Activation counting is EDGE-triggered, not level-triggered
//
// RedisLimiterFallbackActivationsTotal increments once when the primary transitions from healthy
// to failing, not once per failing request. Level-triggering would emit millions of increments
// during a single outage, put an extra atomic write on the hot path, make "how many distinct
// outages happened" unanswerable, and -- via the slog->Sentry tee (observability/logger.go) --
// flood Sentry with millions of identical events for one incident. RedisCommandErrorsTotal, by
// contrast, IS level-triggered (once per failing call) precisely so "how many failing calls" and
// "how many distinct outages" stay independently answerable.
//
// # A panicking primary is treated EXACTLY like a primary that returns an error
//
// Allow recovers any panic from f.primary.Allow and converts it into an ordinary error on the
// same path as a returned error: RedisCommandErrorsTotal.Inc(), markDegraded() (edge-triggered),
// delegate to the memory fallback. Without this, a panicking primary would unwind straight past
// this decorator to IPRateLimiter's own outer recover() (see rate_limit.go) -- which fails the
// REQUEST open (memory limiter never even consulted, zero enforcement) rather than falling back
// to memory, and does so with no edge-triggering, so a *sustained* panic would emit one
// logger.Error per request for the life of the outage, reintroducing the exact Sentry-flood
// problem edge-triggering exists to prevent, through a side door. See
// TestFallbackLimiter_NeverAllowsUnlimitedWhenPrimaryPanics.
type fallbackLimiter struct {
	primary  RateLimitBackend // may be nil: "never configured" (constructor sets degraded)
	fallback RateLimitBackend // always the in-memory backend
	timeout  time.Duration
	logger   *slog.Logger

	degraded atomic.Bool

	mu            sync.Mutex
	degradedSince time.Time // last instant "elapsed while degraded" was accounted from; zero when healthy
}

// newFallbackLimiter constructs a fallbackLimiter. If primary is nil, the limiter runs on
// fallback only and is reported as degraded (RedisLimiterBackendDegraded = 1) from construction,
// WITHOUT incrementing RedisLimiterFallbackActivationsTotal -- a primary that was never configured
// is not a degradation *event*, just a static topology fact.
//
// done, when non-nil, stops the background goroutine that rolls RedisLimiterFallbackActiveSeconds
// forward while degraded (see accumulateActiveSecondsWhileDegraded). A nil done blocks forever in
// that goroutine's select, i.e. it runs for the life of the process -- the same "never stops
// unless told to" convention IPRateLimiter's own cleanup goroutine uses for cfg.Done.
func newFallbackLimiter(primary, fallback RateLimitBackend, timeout time.Duration, logger *slog.Logger, done <-chan struct{}) *fallbackLimiter {
	if logger == nil {
		logger = slog.Default()
	}
	f := &fallbackLimiter{primary: primary, fallback: fallback, timeout: timeout, logger: logger}
	if primary == nil {
		f.degraded.Store(true)
		observability.RedisLimiterBackendDegraded.WithLabelValues().Set(1)
	}
	go f.accumulateActiveSecondsWhileDegraded(done)
	return f
}

// Name identifies this composite backend for logs/metrics.
func (f *fallbackLimiter) Name() string { return "fallback" }

// Allow tries the primary backend first (bounded by f.timeout, detached from ctx's own
// cancellation/deadline -- see the doc comment above) and falls back to the in-memory backend on
// any error OR panic from the primary (see the doc comment's "a panicking primary" section). It
// never returns an error itself in practice: the in-memory fallback (golang.org/x/time/rate under
// the hood) cannot fail, so this always resolves to a real decision -- never fail-open (unlimited)
// nor fail-closed (every request rejected).
func (f *fallbackLimiter) Allow(ctx context.Context, tier domain.RateLimitTier, key string) (domain.RateLimitDecision, error) {
	if f.primary == nil {
		return f.fallback.Allow(ctx, tier, key)
	}

	probeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), f.timeout)
	defer cancel()

	decision, err := f.callPrimary(probeCtx, tier, key)
	if err != nil {
		observability.RedisCommandErrorsTotal.WithLabelValues("limiter", "eval").Inc()
		f.markDegraded()
		return f.fallback.Allow(ctx, tier, key)
	}

	f.markRecovered()
	return decision, nil
}

// callPrimary invokes f.primary.Allow and converts any panic into an ordinary error, so a
// misbehaving backend plugged into this decorator is handled identically to one that returns an
// error -- see the type doc comment's "a panicking primary" section. Calibration: every type
// assertion in internal/ratelimitstore.RedisBackend is comma-ok, so the Redis backend cannot
// panic from its own code today; this is a defensive invariant on the decorator's documented
// contract for ANY RateLimitBackend plugged into it, not a fix for a live exploit.
func (f *fallbackLimiter) callPrimary(ctx context.Context, tier domain.RateLimitTier, key string) (decision domain.RateLimitDecision, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("ratelimit: primary backend panicked: %v", r)
		}
	}()
	return f.primary.Allow(ctx, tier, key)
}

// markDegraded records a primary failure. The activation counter, gauge, and error log only fire
// on the false->true transition (edge-triggered) -- see the package/type doc comment.
func (f *fallbackLimiter) markDegraded() {
	if !f.degraded.CompareAndSwap(false, true) {
		return
	}
	observability.RedisLimiterFallbackActivationsTotal.Inc()
	observability.RedisLimiterBackendDegraded.WithLabelValues().Set(1)
	f.mu.Lock()
	f.degradedSince = time.Now()
	f.mu.Unlock()
	// Deliberately no error text, URL, or other primary-supplied detail in this log line: it must
	// never be able to leak a Redis connection string or credential (see
	// TestFallbackLimiter_LogsNeverContainTheRedisUrlOrPassword). logger.Error (not Warn) matters:
	// the slog->Sentry tee (observability/logger.go:34-36) only forwards Error-level records, and
	// this is exactly the signal that should page someone.
	f.logger.Error("rate limiter fell back to in-memory backend: primary backend unavailable")
}

// markRecovered records a primary recovery. Only fires on the true->false transition. Also
// accounts whatever time elapsed since degradedSince was last rolled forward (either by
// markDegraded's initial timestamp or by an accumulateActiveSecondsWhileDegraded tick) into
// RedisLimiterFallbackActiveSeconds, so a short outage that recovers between ticks still gets its
// full duration counted, not just whole tick-interval chunks.
func (f *fallbackLimiter) markRecovered() {
	if !f.degraded.CompareAndSwap(true, false) {
		return
	}
	observability.RedisLimiterBackendDegraded.WithLabelValues().Set(0)

	f.mu.Lock()
	since := f.degradedSince
	f.degradedSince = time.Time{}
	f.mu.Unlock()
	if !since.IsZero() {
		observability.RedisLimiterFallbackActiveSeconds.Add(time.Since(since).Seconds())
	}

	// Warn, not Error: recovery is good news and must not generate a Sentry event, or the
	// incident timeline in Sentry would show a stream of noise for every recovery on top of the
	// one activation event that actually mattered.
	f.logger.Warn("rate limiter recovered onto redis")
}

// accumulateActiveSecondsWhileDegraded runs on its own goroutine (never on the request hot path)
// and periodically rolls RedisLimiterFallbackActiveSeconds forward while the limiter is currently
// degraded, so an outage that never recovers before a process restart still reports accumulated
// downtime instead of nothing at all (a gap markRecovered's on-recovery-only accounting could not
// close, since it never runs if recovery never happens). Stops when done is closed; a nil done
// runs for the life of the process, matching IPRateLimiter's own cleanup-goroutine convention.
func (f *fallbackLimiter) accumulateActiveSecondsWhileDegraded(done <-chan struct{}) {
	ticker := time.NewTicker(activeSecondsAccountingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			f.accountElapsedIfDegraded()
		case <-done:
			return
		}
	}
}

// accountElapsedIfDegraded adds the time elapsed since degradedSince was last rolled forward to
// RedisLimiterFallbackActiveSeconds, then advances degradedSince to now -- so a subsequent call
// (from the next tick, or from markRecovered) only ever accounts the NEW elapsed slice, never
// double-counting. A zero degradedSince (not currently degraded, or concurrently just recovered)
// is a no-op: read and reset happen together under f.mu so a concurrent markDegraded/markRecovered
// can never observe or produce an inconsistent value.
func (f *fallbackLimiter) accountElapsedIfDegraded() {
	f.mu.Lock()
	since := f.degradedSince
	if since.IsZero() {
		f.mu.Unlock()
		return
	}
	now := time.Now()
	f.degradedSince = now
	f.mu.Unlock()

	observability.RedisLimiterFallbackActiveSeconds.Add(now.Sub(since).Seconds())
}
