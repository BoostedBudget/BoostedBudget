// Package health provides memoized, singleflight-deduplicated dependency health
// checks that can be shared between multiple consumers.
//
// It exists because two call sites need the SAME memoized check instances: the
// public liveness/readiness handler and the admin system-health dashboard. If
// each consumer built its own probe, an admin page load would fire a second
// Redis PING (or DB ping, or Clerk call) that the liveness probe already paid
// for a moment earlier. Sharing a *Registry* of Check values built once (in
// cmd/server) and passed down avoids that duplicate load — including during
// the outage where duplicate load hurts the most.
//
// Two behaviors are load-bearing and easy to "simplify" away by a future
// reader, so they are called out explicitly:
//
//  1. Failures are cached exactly like successes, for the same TTL. A memoizer
//     that only caches success responses turns a struggling dependency into an
//     amplifier: every caller during the outage re-probes it, multiplying load
//     on the exact thing that is already failing. Caching the failure Result
//     means one probe pays the cost and everyone else gets the (unhealthy)
//     answer for TTL, same as they would in the healthy case.
//
//  2. The underlying probe runs under
//     context.WithTimeout(context.WithoutCancel(ctx), opts.Timeout)
//     — NOT under the caller's ctx directly. This is because singleflight
//     shares one in-flight call across every caller with the same key, and it
//     picks the FIRST caller's context to drive that shared call. If that
//     first caller had a short deadline (or gets cancelled — e.g. an HTTP
//     request whose client disconnected), a naive implementation would cancel
//     the probe out from under every other caller waiting on the same result,
//     even ones with plenty of time left. context.WithoutCancel deliberately
//     detaches the probe from the caller's cancellation and deadline; the
//     explicit WithTimeout that follows is what re-introduces a bound, sourced
//     from Options.Timeout instead of from whichever caller happened to be
//     first. Do not delete the WithoutCancel call — that is not redundant
//     defensive coding, it is the fix for a real cross-caller poisoning bug.
package health

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// Result is the outcome of a single probe invocation.
type Result struct {
	// Err is nil on success. A non-nil Err (including context deadline
	// exceeded from a timed-out probe) means the dependency is unhealthy.
	Err error
	// Latency is the duration of the PROBE itself, not of the call that
	// happened to observe it. A cache hit reports the original probe's
	// latency, not ~0.
	Latency time.Duration
	// ObservedAt is the instant the probe that produced this Result started,
	// not the instant it was read from cache. Consumers surface this so a
	// dashboard can say how stale a cached answer is.
	ObservedAt time.Time
}

// Healthy reports whether the probe succeeded.
func (r Result) Healthy() bool { return r.Err == nil }

// Check is a memoized health probe: calling it either returns a cached Result
// or runs the underlying probe (deduplicated via singleflight) and caches the
// outcome.
type Check func(ctx context.Context) Result

// Options configures Memoize.
type Options struct {
	// Name identifies this check. It is used as the singleflight key (so it
	// must be unique per logical check) and may be surfaced in errors.
	Name string
	// TTL is how long a probe Result is reused before the next call
	// re-invokes the underlying probe. TTL <= 0 means "never cache": every
	// call re-invokes the probe (concurrent callers still share a single
	// in-flight invocation via singleflight).
	TTL time.Duration
	// Timeout bounds a single probe invocation. Timeout <= 0 means no timeout
	// wrapper is applied — the probe runs for as long as it likes (or until
	// the process's own context is cancelled upstream).
	Timeout time.Duration
	// Now is a test seam for the clock. Nil means time.Now.
	Now func() time.Time
}

// Memoize wraps fn as a memoized, singleflight-deduplicated Check per opts.
//
// See the package doc comment for why failures are cached like successes and
// why the probe runs under context.WithoutCancel(ctx) rather than ctx itself.
func Memoize(opts Options, fn func(ctx context.Context) error) Check {
	now := opts.Now
	if now == nil {
		now = time.Now
	}

	var (
		mu        sync.Mutex
		cached    Result
		haveCache bool
		expiresAt time.Time
		group     singleflight.Group
	)

	return func(ctx context.Context) Result {
		if opts.TTL > 0 {
			mu.Lock()
			if haveCache && now().Before(expiresAt) {
				r := cached
				mu.Unlock()
				return r
			}
			mu.Unlock()
		}

		v, _, _ := group.Do(opts.Name, func() (any, error) {
			probeCtx := context.WithoutCancel(ctx)
			var cancel context.CancelFunc
			if opts.Timeout > 0 {
				probeCtx, cancel = context.WithTimeout(probeCtx, opts.Timeout)
				defer cancel()
			}

			start := now()
			probeErr := fn(probeCtx)
			if probeErr != nil {
				probeErr = fmt.Errorf("health check %q: %w", opts.Name, probeErr)
			}
			result := Result{
				Err:        probeErr,
				Latency:    now().Sub(start),
				ObservedAt: start,
			}

			mu.Lock()
			cached = result
			haveCache = true
			if opts.TTL > 0 {
				expiresAt = now().Add(opts.TTL)
			}
			mu.Unlock()

			// The singleflight error return is intentionally left nil: probe
			// failures are carried inside Result.Err (and cached like a
			// success), not surfaced as a singleflight error.
			return result, nil
		})

		return v.(Result)
	}
}

// Registry holds the shared, memoized Check instances built once (typically
// in cmd/server) and handed to every consumer that needs to read dependency
// health, so they observe the same cached probes instead of each firing their
// own.
//
// A nil field means that dependency is not configured for this deployment;
// Registry itself makes no decision about what a nil Check means to a
// particular consumer — that's the consumer's call.
type Registry struct {
	DB    Check
	Redis Check
	Clerk Check
}
