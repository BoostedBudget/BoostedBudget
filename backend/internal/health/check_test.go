package health

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeClock is a goroutine-safe, manually-advanced clock used as the Now seam
// so tests can cross TTL boundaries deterministically instead of sleeping.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock(start time.Time) *fakeClock {
	return &fakeClock{now: start}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func TestMemoize_FirstCallInvokesUnderlyingCheck(t *testing.T) {
	var calls int32
	check := Memoize(Options{Name: "t", TTL: time.Minute, Timeout: time.Second}, func(ctx context.Context) error {
		atomic.AddInt32(&calls, 1)
		return nil
	})

	res := check(context.Background())
	if !res.Healthy() {
		t.Fatalf("expected healthy result, got err=%v", res.Err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 call, got %d", got)
	}
}

func TestMemoize_SecondCallWithinTTLDoesNotReinvoke(t *testing.T) {
	var calls int32
	clock := newFakeClock(time.Now())
	check := Memoize(Options{Name: "t", TTL: time.Minute, Timeout: time.Second, Now: clock.Now}, func(ctx context.Context) error {
		atomic.AddInt32(&calls, 1)
		return nil
	})

	for i := 0; i < 100; i++ {
		check(context.Background())
	}

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 call after 100 within TTL, got %d", got)
	}
}

func TestMemoize_CallAfterTTLReinvokes(t *testing.T) {
	var calls int32
	clock := newFakeClock(time.Now())
	check := Memoize(Options{Name: "t", TTL: time.Minute, Timeout: time.Second, Now: clock.Now}, func(ctx context.Context) error {
		atomic.AddInt32(&calls, 1)
		return nil
	})

	check(context.Background())
	clock.Advance(2 * time.Minute)
	check(context.Background())

	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected 2 calls after TTL elapsed, got %d", got)
	}
}

func TestMemoize_ConcurrentCallersShareASingleInvocation(t *testing.T) {
	var calls int32
	check := Memoize(Options{Name: "t", TTL: time.Minute, Timeout: time.Second}, func(ctx context.Context) error {
		atomic.AddInt32(&calls, 1)
		time.Sleep(50 * time.Millisecond)
		return nil
	})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			check(context.Background())
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 call for 50 concurrent callers, got %d", got)
	}
}

func TestMemoize_CachesFailureAsWellAsSuccess(t *testing.T) {
	var calls int32
	clock := newFakeClock(time.Now())
	probeErr := errors.New("probe failed")
	check := Memoize(Options{Name: "t", TTL: time.Minute, Timeout: time.Second, Now: clock.Now}, func(ctx context.Context) error {
		atomic.AddInt32(&calls, 1)
		return probeErr
	})

	var lastRes Result
	for i := 0; i < 100; i++ {
		lastRes = check(context.Background())
	}

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 call for 100 calls with failing probe, got %d", got)
	}
	if lastRes.Healthy() {
		t.Fatalf("expected unhealthy result")
	}
	if !errors.Is(lastRes.Err, probeErr) {
		t.Fatalf("expected cached error to be probeErr, got %v", lastRes.Err)
	}
}

func TestMemoize_SlowCheckIsBoundedByItsOwnTimeout(t *testing.T) {
	timeout := 50 * time.Millisecond
	check := Memoize(Options{Name: "t", TTL: time.Minute, Timeout: timeout}, func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})

	start := time.Now()
	res := check(context.Background())
	elapsed := time.Since(start)

	if res.Healthy() {
		t.Fatalf("expected unhealthy result from timed-out probe")
	}
	if !errors.Is(res.Err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline-exceeded error, got %v", res.Err)
	}
	// Bounded by its own timeout: allow generous slack for scheduler jitter,
	// but it must not have hung indefinitely or returned near-instantly.
	if elapsed < timeout {
		t.Fatalf("expected elapsed >= timeout (%v), got %v", timeout, elapsed)
	}
	if elapsed > timeout+2*time.Second {
		t.Fatalf("expected elapsed to be close to timeout (%v), got %v", timeout, elapsed)
	}
}

func TestMemoize_CallerCancellationDoesNotPoisonTheSharedResult(t *testing.T) {
	var calls int32
	release := make(chan struct{})
	// started is closed the instant the shared probe begins running. Because
	// singleflight registers the in-flight call BEFORE invoking the function
	// (see golang.org/x/sync/singleflight: g.m[key] = c happens before
	// g.doCall), and the probe below cannot return until release is closed
	// or its own ctx is done, "started closed" is a deterministic proof that
	// caller A's singleflight.Do has already registered the in-flight call
	// and that registration is still live (fn hasn't returned yet).
	started := make(chan struct{})
	// The probe MUST observe ctx, or this test proves nothing: a caller's
	// cancellation can only "poison" the shared result if the probe actually
	// reacts to ctx.Done(). If A's raw (already-cancelled) ctx were plumbed
	// through instead of context.WithoutCancel(ctx), this select would fire
	// the ctx.Done() branch immediately and return context.Canceled.
	check := Memoize(Options{Name: "t", TTL: time.Minute, Timeout: time.Second}, func(ctx context.Context) error {
		atomic.AddInt32(&calls, 1)
		close(started)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-release:
			return nil
		}
	})

	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before use

	var wg sync.WaitGroup
	var resB Result
	// bCalling is closed by caller B's goroutine immediately before it makes
	// the joining call, and joinScheduled is a WaitGroup B also signals at
	// the same point. main waits on the channel (a real synchronization
	// point, not a guessed duration) and then yields the processor
	// repeatedly with runtime.Gosched() so the scheduler gets every
	// opportunity to actually run B's call (a handful of non-blocking
	// instructions: cache-miss check, then singleflight's own mutex-guarded
	// map lookup) before release is closed. This replaces the previous
	// fixed-duration time.Sleep(10ms)/time.Sleep(30ms) pair, which assumed a
	// duration was "surely enough" rather than synchronizing on anything
	// real; under CI scheduler pressure that assumption is exactly what can
	// invert and flake.
	bCalling := make(chan struct{})

	wg.Add(2)
	go func() {
		defer wg.Done()
		check(cancelledCtx) // caller A: already-cancelled context; becomes the singleflight leader
	}()
	go func() {
		defer wg.Done()
		<-started // A's Do call has registered the in-flight probe; fn is blocked below
		close(bCalling)
		resB = check(context.Background()) // caller B: live context, joins A's in-flight probe
	}()

	<-started
	<-bCalling
	for i := 0; i < 1000; i++ {
		runtime.Gosched()
	}
	close(release)
	wg.Wait()

	if !resB.Healthy() {
		t.Fatalf("expected caller B to get a real healthy result, got err=%v", resB.Err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected exactly 1 shared invocation, got %d", got)
	}
}

func TestMemoize_ResultCarriesProbeLatencyNotCacheHitLatency(t *testing.T) {
	// Uses the real clock (Now left nil) rather than the fake clock: this
	// test needs Latency to reflect an actual elapsed sleep, and a fake clock
	// frozen in place would report 0 regardless of how long fn took.
	check := Memoize(Options{Name: "t", TTL: time.Minute, Timeout: time.Second}, func(ctx context.Context) error {
		time.Sleep(30 * time.Millisecond)
		return nil
	})

	first := check(context.Background())
	second := check(context.Background()) // cache hit, within TTL

	if first.Latency < 30*time.Millisecond {
		t.Fatalf("expected first-call latency >= 30ms, got %v", first.Latency)
	}
	if second.Latency < 30*time.Millisecond {
		t.Fatalf("expected cache-hit to carry probe latency >= 30ms, got %v", second.Latency)
	}
}

func TestMemoize_ResultCarriesObservedAt(t *testing.T) {
	clock := newFakeClock(time.Now())
	check := Memoize(Options{Name: "t", TTL: time.Minute, Timeout: time.Second, Now: clock.Now}, func(ctx context.Context) error {
		return nil
	})

	first := check(context.Background())
	probeInstant := first.ObservedAt

	clock.Advance(10 * time.Second) // still within TTL
	second := check(context.Background())

	if !second.ObservedAt.Equal(probeInstant) {
		t.Fatalf("expected cached ObservedAt to equal probe instant %v, got %v", probeInstant, second.ObservedAt)
	}
}

func TestMemoize_ZeroTTLAlwaysReinvokes(t *testing.T) {
	var calls int32
	check := Memoize(Options{Name: "t", TTL: 0, Timeout: time.Second}, func(ctx context.Context) error {
		atomic.AddInt32(&calls, 1)
		return nil
	})

	for i := 1; i <= 5; i++ {
		check(context.Background())
		if got := atomic.LoadInt32(&calls); got != int32(i) {
			t.Fatalf("expected %d calls after %d invocations with TTL=0, got %d", i, i, got)
		}
	}
}

func TestMemoize_IsSafeUnderConcurrentUse(t *testing.T) {
	var calls int32
	clock := newFakeClock(time.Now())
	var clockMu sync.Mutex
	check := Memoize(Options{Name: "t", TTL: 20 * time.Millisecond, Timeout: time.Second, Now: func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		return clock.Now()
	}}, func(ctx context.Context) error {
		atomic.AddInt32(&calls, 1)
		return nil
	})

	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			check(context.Background())
			if i%10 == 0 {
				clockMu.Lock()
				clock.Advance(5 * time.Millisecond)
				clockMu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got < 1 {
		t.Fatalf("expected at least 1 call, got %d", got)
	}
}
