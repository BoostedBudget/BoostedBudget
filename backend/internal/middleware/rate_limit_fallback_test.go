package middleware

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRateLimitBackend is a test double implementing RateLimitBackend with a caller-supplied
// Allow behaviour and a call counter, so tests can assert exactly how many times (if any) a given
// backend was invoked.
type fakeRateLimitBackend struct {
	name      string
	allowFunc func(ctx context.Context, tier domain.RateLimitTier, key string) (domain.RateLimitDecision, error)

	mu    sync.Mutex
	calls int
}

func (f *fakeRateLimitBackend) Name() string { return f.name }

func (f *fakeRateLimitBackend) Allow(ctx context.Context, tier domain.RateLimitTier, key string) (domain.RateLimitDecision, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	return f.allowFunc(ctx, tier, key)
}

func (f *fakeRateLimitBackend) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// panicBackend is a RateLimitBackend that panics the instant Allow is called — used to prove a
// given code path never touches it at all, not merely that it wasn't observed to run.
type panicBackend struct{ name string }

func (p *panicBackend) Name() string { return p.name }
func (p *panicBackend) Allow(context.Context, domain.RateLimitTier, string) (domain.RateLimitDecision, error) {
	panic(p.name + " backend must never be called on this path")
}

func alwaysAllow() func(context.Context, domain.RateLimitTier, string) (domain.RateLimitDecision, error) {
	return func(context.Context, domain.RateLimitTier, string) (domain.RateLimitDecision, error) {
		return domain.RateLimitDecision{Allowed: true}, nil
	}
}

func alwaysError(err error) func(context.Context, domain.RateLimitTier, string) (domain.RateLimitDecision, error) {
	return func(context.Context, domain.RateLimitTier, string) (domain.RateLimitDecision, error) {
		return domain.RateLimitDecision{}, err
	}
}

func alwaysPanic(msg string) func(context.Context, domain.RateLimitTier, string) (domain.RateLimitDecision, error) {
	return func(context.Context, domain.RateLimitTier, string) (domain.RateLimitDecision, error) {
		panic(msg)
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(discardWriter{}, nil))
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestFallbackLimiter_UsesPrimaryWhenPrimarySucceeds(t *testing.T) {
	primary := &fakeRateLimitBackend{name: "redis", allowFunc: alwaysAllow()}
	fallback := &panicBackend{name: "memory"}
	fl := newFallbackLimiter(primary, fallback, time.Second, discardLogger(), nil)

	decision, err := fl.Allow(context.Background(), domain.RateLimitTierRegular, "k")
	require.NoError(t, err)
	assert.True(t, decision.Allowed)
	assert.Equal(t, 1, primary.callCount())
}

func TestFallbackLimiter_FallsBackToMemoryWhenPrimaryErrors(t *testing.T) {
	primary := &fakeRateLimitBackend{name: "redis", allowFunc: alwaysError(errors.New("boom"))}
	fallback := &fakeRateLimitBackend{name: "memory", allowFunc: func(context.Context, domain.RateLimitTier, string) (domain.RateLimitDecision, error) {
		return domain.RateLimitDecision{Allowed: true}, nil
	}}
	fl := newFallbackLimiter(primary, fallback, time.Second, discardLogger(), nil)

	decision, err := fl.Allow(context.Background(), domain.RateLimitTierRegular, "k")
	require.NoError(t, err)
	assert.True(t, decision.Allowed)
	assert.Equal(t, 1, fallback.callCount())
}

func TestFallbackLimiter_FallsBackToMemoryWhenPrimaryExceedsTheTimeout(t *testing.T) {
	primary := &fakeRateLimitBackend{name: "redis", allowFunc: func(ctx context.Context, _ domain.RateLimitTier, _ string) (domain.RateLimitDecision, error) {
		<-ctx.Done() // primary respects the timeout context and reports back once it fires
		return domain.RateLimitDecision{}, ctx.Err()
	}}
	fallback := &fakeRateLimitBackend{name: "memory", allowFunc: alwaysAllow()}
	fl := newFallbackLimiter(primary, fallback, 10*time.Millisecond, discardLogger(), nil)

	decision, err := fl.Allow(context.Background(), domain.RateLimitTierRegular, "k")
	require.NoError(t, err)
	assert.True(t, decision.Allowed)
	assert.Equal(t, 1, fallback.callCount())
}

// TestFallbackLimiter_NeverAllowsUnlimitedWhenPrimaryIsDown proves the fallback still enforces a
// real limit — a permanently-failing primary must not turn into an unlimited pass-through.
func TestFallbackLimiter_NeverAllowsUnlimitedWhenPrimaryIsDown(t *testing.T) {
	primary := &fakeRateLimitBackend{name: "redis", allowFunc: alwaysError(errors.New("down"))}
	store := newIPLimiterStore(5, time.Minute, 100)
	fallback := &memoryBackend{regular: store, auth: store, sentinel: store, window: time.Minute}
	fl := newFallbackLimiter(primary, fallback, time.Second, discardLogger(), nil)

	allowed := 0
	for i := 0; i < 1000; i++ {
		decision, err := fl.Allow(context.Background(), domain.RateLimitTierRegular, "same-key")
		require.NoError(t, err)
		if decision.Allowed {
			allowed++
		}
	}
	assert.Equal(t, 5, allowed, "fallback must enforce the same 5-request limit, never fail open to unlimited")
}

// TestFallbackLimiter_NeverAllowsUnlimitedWhenPrimaryPanics is the panic-equivalent of
// TestFallbackLimiter_NeverAllowsUnlimitedWhenPrimaryIsDown above. Fix-round-1 finding: without a
// recover() inside fallbackLimiter.Allow around the primary call, a panicking primary propagates
// straight past this decorator to IPRateLimiter's own outer recover(), which fails the REQUEST
// open (memory limiter never consulted at all) rather than falling back to memory — a systemic,
// indefinite fail-open for the life of the outage, not a one-off. It also has no edge-triggering,
// so a sustained panic would emit one logger.Error per request, flooding Sentry through the slog
// tee. This test proves BOTH properties hold when the primary panics on every call: the memory
// limit is still enforced across 1000 requests, AND the activation counter increments exactly
// once (not 1000 times) for the one continuous outage.
func TestFallbackLimiter_NeverAllowsUnlimitedWhenPrimaryPanics(t *testing.T) {
	primary := &fakeRateLimitBackend{name: "redis", allowFunc: alwaysPanic("redis client exploded")}
	store := newIPLimiterStore(5, time.Minute, 100)
	fallback := &memoryBackend{regular: store, auth: store, sentinel: store, window: time.Minute}
	fl := newFallbackLimiter(primary, fallback, time.Second, discardLogger(), nil)

	before := testutil.ToFloat64(observability.RedisLimiterFallbackActivationsTotal)

	allowed := 0
	require.NotPanics(t, func() {
		for i := 0; i < 1000; i++ {
			decision, err := fl.Allow(context.Background(), domain.RateLimitTierRegular, "panic-key")
			require.NoError(t, err)
			if decision.Allowed {
				allowed++
			}
		}
	})

	after := testutil.ToFloat64(observability.RedisLimiterFallbackActivationsTotal)

	assert.Equal(t, 5, allowed, "a panicking primary must still enforce the same 5-request memory limit, never fail open to unlimited")
	assert.Equal(t, float64(1), after-before, "1000 requests against one continuous panicking outage must increment the activation counter exactly once, not once per request")
}

// TestFallbackLimiter_NeverRejectsEverythingWhenPrimaryIsDown proves the fallback still allows
// the first N requests — a permanently-failing primary must not turn into fail-closed either.
func TestFallbackLimiter_NeverRejectsEverythingWhenPrimaryIsDown(t *testing.T) {
	primary := &fakeRateLimitBackend{name: "redis", allowFunc: alwaysError(errors.New("down"))}
	store := newIPLimiterStore(5, time.Minute, 100)
	fallback := &memoryBackend{regular: store, auth: store, sentinel: store, window: time.Minute}
	fl := newFallbackLimiter(primary, fallback, time.Second, discardLogger(), nil)

	for i := 0; i < 5; i++ {
		decision, err := fl.Allow(context.Background(), domain.RateLimitTierRegular, "another-key")
		require.NoError(t, err)
		assert.True(t, decision.Allowed, "request %d should still be allowed while primary is down", i+1)
	}
}

func TestFallbackLimiter_ActivationCounterIncrementsOncePerOutageNotOncePerRequest(t *testing.T) {
	primary := &fakeRateLimitBackend{name: "redis", allowFunc: alwaysError(errors.New("down"))}
	fallback := &fakeRateLimitBackend{name: "memory", allowFunc: alwaysAllow()}
	fl := newFallbackLimiter(primary, fallback, time.Second, discardLogger(), nil)

	before := testutil.ToFloat64(observability.RedisLimiterFallbackActivationsTotal)
	for i := 0; i < 500; i++ {
		_, _ = fl.Allow(context.Background(), domain.RateLimitTierRegular, "k")
	}
	after := testutil.ToFloat64(observability.RedisLimiterFallbackActivationsTotal)

	assert.Equal(t, float64(1), after-before, "500 requests against one continuous outage must increment the activation counter exactly once")
}

func TestFallbackLimiter_RecoveryDoesNotIncrementTheActivationCounter(t *testing.T) {
	failing := true
	primary := &fakeRateLimitBackend{name: "redis", allowFunc: func(context.Context, domain.RateLimitTier, string) (domain.RateLimitDecision, error) {
		if failing {
			return domain.RateLimitDecision{}, errors.New("down")
		}
		return domain.RateLimitDecision{Allowed: true}, nil
	}}
	fallback := &fakeRateLimitBackend{name: "memory", allowFunc: alwaysAllow()}
	fl := newFallbackLimiter(primary, fallback, time.Second, discardLogger(), nil)

	before := testutil.ToFloat64(observability.RedisLimiterFallbackActivationsTotal)
	_, _ = fl.Allow(context.Background(), domain.RateLimitTierRegular, "k") // fails -> activation #1
	afterActivation := testutil.ToFloat64(observability.RedisLimiterFallbackActivationsTotal)
	assert.Equal(t, float64(1), afterActivation-before)

	failing = false
	for i := 0; i < 10; i++ {
		_, _ = fl.Allow(context.Background(), domain.RateLimitTierRegular, "k") // recovers, then stays healthy
	}
	afterRecovery := testutil.ToFloat64(observability.RedisLimiterFallbackActivationsTotal)
	assert.Equal(t, afterActivation, afterRecovery, "recovering (and staying healthy) must not increment the activation counter")
}

func TestFallbackLimiter_SecondOutageAfterRecoveryIncrementsAgain(t *testing.T) {
	var failing atomic.Bool
	failing.Store(true)
	primary := &fakeRateLimitBackend{name: "redis", allowFunc: func(context.Context, domain.RateLimitTier, string) (domain.RateLimitDecision, error) {
		if failing.Load() {
			return domain.RateLimitDecision{}, errors.New("down")
		}
		return domain.RateLimitDecision{Allowed: true}, nil
	}}
	fallback := &fakeRateLimitBackend{name: "memory", allowFunc: alwaysAllow()}
	fl := newFallbackLimiter(primary, fallback, time.Second, discardLogger(), nil)

	before := testutil.ToFloat64(observability.RedisLimiterFallbackActivationsTotal)
	_, _ = fl.Allow(context.Background(), domain.RateLimitTierRegular, "k") // outage #1
	failing.Store(false)
	_, _ = fl.Allow(context.Background(), domain.RateLimitTierRegular, "k") // recovers
	failing.Store(true)
	_, _ = fl.Allow(context.Background(), domain.RateLimitTierRegular, "k") // outage #2
	after := testutil.ToFloat64(observability.RedisLimiterFallbackActivationsTotal)

	assert.Equal(t, float64(2), after-before, "a second, distinct outage after recovery must increment the activation counter again")
}

func TestFallbackLimiter_DegradedGaugeTracksCurrentStateNotHistory(t *testing.T) {
	var failing atomic.Bool
	primary := &fakeRateLimitBackend{name: "redis", allowFunc: func(context.Context, domain.RateLimitTier, string) (domain.RateLimitDecision, error) {
		if failing.Load() {
			return domain.RateLimitDecision{}, errors.New("down")
		}
		return domain.RateLimitDecision{Allowed: true}, nil
	}}
	fallback := &fakeRateLimitBackend{name: "memory", allowFunc: alwaysAllow()}
	fl := newFallbackLimiter(primary, fallback, time.Second, discardLogger(), nil)

	// The gauge is a single process-wide series shared with every other test in this file, so its
	// value coming into this test is whatever the LAST transition anywhere left it at -- this test
	// does not assume a starting value. Instead it drives THIS instance through a full
	// degrade->recover->degrade cycle and asserts the gauge reflects only the latest transition at
	// each step, never anything from earlier in the cycle (or from another instance's history).
	failing.Store(true)
	_, _ = fl.Allow(context.Background(), domain.RateLimitTierRegular, "k")
	assert.Equal(t, float64(1), testutil.ToFloat64(observability.RedisLimiterBackendDegraded.WithLabelValues()))

	failing.Store(false)
	_, _ = fl.Allow(context.Background(), domain.RateLimitTierRegular, "k")
	assert.Equal(t, float64(0), testutil.ToFloat64(observability.RedisLimiterBackendDegraded.WithLabelValues()))

	failing.Store(true)
	_, _ = fl.Allow(context.Background(), domain.RateLimitTierRegular, "k")
	assert.Equal(t, float64(1), testutil.ToFloat64(observability.RedisLimiterBackendDegraded.WithLabelValues()))
}

func TestFallbackLimiter_NilPrimaryIsDegradedWithoutCountingAnActivation(t *testing.T) {
	fallback := &fakeRateLimitBackend{name: "memory", allowFunc: alwaysAllow()}

	before := testutil.ToFloat64(observability.RedisLimiterFallbackActivationsTotal)
	fl := newFallbackLimiter(nil, fallback, time.Second, discardLogger(), nil)
	after := testutil.ToFloat64(observability.RedisLimiterFallbackActivationsTotal)

	assert.Equal(t, float64(1), testutil.ToFloat64(observability.RedisLimiterBackendDegraded.WithLabelValues()),
		"never-configured primary must be reported as degraded from construction")
	assert.Equal(t, before, after, "never-configured is not a degradation EVENT, so the activation counter must not move")

	decision, err := fl.Allow(context.Background(), domain.RateLimitTierRegular, "k")
	require.NoError(t, err)
	assert.True(t, decision.Allowed)
	assert.Equal(t, 1, fallback.callCount())
}

// TestFallbackLimiter_ClientDisconnectDoesNotCountAsARedisFailure is the security-critical test
// described in the task brief: if the Redis call inherited the request's own context, a client
// that disconnects (cancelling that context) would look identical to a genuine Redis failure and
// would trip the fallback — letting an attacker force per-machine mode on demand by opening and
// aborting a handful of connections. context.WithoutCancel must prevent this.
//
// To prove it: the context passed into Allow is already cancelled BEFORE the call is made (worst
// case of "client disconnected"). The primary here checks ctx.Err() and returns an error only if
// its context is not cancelled -- sorry, precisely the opposite: if the primary observes
// cancellation, that models an under-hardened redis client bailing out early; a correctly
// hardened implementation (this fallbackLimiter) must prevent the primary from ever observing
// that cancellation. The fallback is a panicBackend, so if the primary is (incorrectly) starved by
// the cancelled context and errors out, the fallback panics and the test fails loudly rather than
// silently passing.
func TestFallbackLimiter_ClientDisconnectDoesNotCountAsARedisFailure(t *testing.T) {
	primary := &fakeRateLimitBackend{name: "redis", allowFunc: func(ctx context.Context, _ domain.RateLimitTier, _ string) (domain.RateLimitDecision, error) {
		if ctx.Err() != nil {
			return domain.RateLimitDecision{}, ctx.Err()
		}
		return domain.RateLimitDecision{Allowed: true}, nil
	}}
	fallback := &panicBackend{name: "memory"}
	fl := newFallbackLimiter(primary, fallback, time.Second, discardLogger(), nil)

	reqCtx, cancel := context.WithCancel(context.Background())
	cancel() // the "client" has already disconnected before the limiter even runs

	decision, err := fl.Allow(reqCtx, domain.RateLimitTierRegular, "k")
	require.NoError(t, err, "primary must not observe the caller's cancellation, so it must succeed here")
	assert.True(t, decision.Allowed)
	assert.Equal(t, 1, primary.callCount())

	before := testutil.ToFloat64(observability.RedisLimiterFallbackActivationsTotal)
	after := testutil.ToFloat64(observability.RedisLimiterFallbackActivationsTotal)
	assert.Equal(t, before, after, "a client disconnect must never be recorded as a Redis outage")
}

// slogCapture is a minimal slog.Handler that records every record's level and rendered message so
// tests can assert on log level and content without depending on a specific text/json format.
type slogCapture struct {
	mu      sync.Mutex
	records []slog.Record
}

func (s *slogCapture) Enabled(context.Context, slog.Level) bool { return true }
func (s *slogCapture) Handle(_ context.Context, r slog.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, r)
	return nil
}
func (s *slogCapture) WithAttrs(attrs []slog.Attr) slog.Handler { return s }
func (s *slogCapture) WithGroup(string) slog.Handler            { return s }

func (s *slogCapture) all() []slog.Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]slog.Record, len(s.records))
	copy(out, s.records)
	return out
}

func (s *slogCapture) rendered() string {
	var sb []byte
	for _, r := range s.all() {
		sb = append(sb, []byte(r.Message)...)
		sb = append(sb, ' ')
		r.Attrs(func(a slog.Attr) bool {
			sb = append(sb, []byte(a.Key)...)
			sb = append(sb, '=')
			sb = append(sb, []byte(a.Value.String())...)
			sb = append(sb, ' ')
			return true
		})
	}
	return string(sb)
}

func TestFallbackLimiter_ActivationIsLoggedAtErrorLevelSoSentryReceivesIt(t *testing.T) {
	capture := &slogCapture{}
	logger := slog.New(capture)

	primary := &fakeRateLimitBackend{name: "redis", allowFunc: alwaysError(errors.New("down"))}
	fallback := &fakeRateLimitBackend{name: "memory", allowFunc: alwaysAllow()}
	fl := newFallbackLimiter(primary, fallback, time.Second, logger, nil)

	_, _ = fl.Allow(context.Background(), domain.RateLimitTierRegular, "k")

	foundError := false
	for _, r := range capture.all() {
		if r.Level == slog.LevelError {
			foundError = true
		}
	}
	assert.True(t, foundError, "activation must be logged at slog.LevelError so the existing slog->Sentry tee captures it")
}

func TestFallbackLimiter_LogsNeverContainTheRedisUrlOrPassword(t *testing.T) {
	capture := &slogCapture{}
	logger := slog.New(capture)

	secret := "hunter2SuperSecretPassword"
	url := "redis://default:" + secret + "@upstash.example.com:6379"
	primary := &fakeRateLimitBackend{name: "redis", allowFunc: alwaysError(errors.New("dial tcp " + url + ": connection refused"))}
	fallback := &fakeRateLimitBackend{name: "memory", allowFunc: alwaysAllow()}
	fl := newFallbackLimiter(primary, fallback, time.Second, logger, nil)

	_, _ = fl.Allow(context.Background(), domain.RateLimitTierRegular, "k")

	rendered := capture.rendered()
	assert.NotContains(t, rendered, secret)
	assert.NotContains(t, rendered, "upstash.example.com")
}

func TestFallbackLimiter_CommandErrorCounterIsLevelTriggeredUnlikeTheActivationCounter(t *testing.T) {
	primary := &fakeRateLimitBackend{name: "redis", allowFunc: alwaysError(errors.New("down"))}
	fallback := &fakeRateLimitBackend{name: "memory", allowFunc: alwaysAllow()}
	fl := newFallbackLimiter(primary, fallback, time.Second, discardLogger(), nil)

	beforeCmd := testutil.ToFloat64(observability.RedisCommandErrorsTotal.WithLabelValues("limiter", "eval"))
	beforeActivation := testutil.ToFloat64(observability.RedisLimiterFallbackActivationsTotal)

	for i := 0; i < 5; i++ {
		_, _ = fl.Allow(context.Background(), domain.RateLimitTierRegular, "k")
	}

	afterCmd := testutil.ToFloat64(observability.RedisCommandErrorsTotal.WithLabelValues("limiter", "eval"))
	afterActivation := testutil.ToFloat64(observability.RedisLimiterFallbackActivationsTotal)

	assert.Equal(t, float64(5), afterCmd-beforeCmd, "the command-error counter is level-triggered: once per failing call")
	assert.Equal(t, float64(1), afterActivation-beforeActivation, "the activation counter is edge-triggered: once per outage")
}

// TestFallbackLimiter_ActiveSecondsAccumulatesWhileStillDegradedWithoutRecovery is the fix-round-1
// regression test for the previously-flagged concern: RedisLimiterFallbackActiveSeconds used to
// only accumulate on recovery, so an outage that never recovers (e.g. before a process restart)
// contributed nothing -- under-reporting exactly the worst incidents. This test drives a
// degradation and then calls accountElapsedIfDegraded directly (what the background ticker calls
// periodically) WITHOUT ever recovering, and asserts the counter moved anyway.
func TestFallbackLimiter_ActiveSecondsAccumulatesWhileStillDegradedWithoutRecovery(t *testing.T) {
	primary := &fakeRateLimitBackend{name: "redis", allowFunc: alwaysError(errors.New("down"))}
	fallback := &fakeRateLimitBackend{name: "memory", allowFunc: alwaysAllow()}
	fl := newFallbackLimiter(primary, fallback, time.Second, discardLogger(), nil)

	_, _ = fl.Allow(context.Background(), domain.RateLimitTierRegular, "k") // degrade; sets degradedSince = now

	// Simulate time having passed since degradation, without ever recovering, by backdating the
	// internal accounting checkpoint directly (white-box: same package).
	fl.mu.Lock()
	fl.degradedSince = fl.degradedSince.Add(-5 * time.Second)
	fl.mu.Unlock()

	before := testutil.ToFloat64(observability.RedisLimiterFallbackActiveSeconds)
	fl.accountElapsedIfDegraded() // what the periodic background ticker calls
	after := testutil.ToFloat64(observability.RedisLimiterFallbackActiveSeconds)

	assert.GreaterOrEqual(t, after-before, float64(5),
		"active-seconds must accumulate for an outage that never recovers, not just on recovery")
	assert.Equal(t, float64(1), testutil.ToFloat64(observability.RedisLimiterBackendDegraded.WithLabelValues()),
		"sanity: this fallbackLimiter must still be degraded -- no recovery occurred in this test")
}

func TestFallbackLimiter_IsSafeUnderConcurrentUse(t *testing.T) {
	var toggle atomic.Int64
	primary := &fakeRateLimitBackend{name: "redis", allowFunc: func(context.Context, domain.RateLimitTier, string) (domain.RateLimitDecision, error) {
		if toggle.Add(1)%2 == 0 {
			return domain.RateLimitDecision{}, errors.New("intermittent")
		}
		return domain.RateLimitDecision{Allowed: true}, nil
	}}
	store := newIPLimiterStore(1000000, time.Minute, 1000)
	fallback := &memoryBackend{regular: store, auth: store, sentinel: store, window: time.Minute}
	fl := newFallbackLimiter(primary, fallback, 5*time.Millisecond, discardLogger(), nil)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_, _ = fl.Allow(context.Background(), domain.RateLimitTierRegular, "concurrent-key")
			}
		}(i)
	}
	wg.Wait()
}
