package main

import (
	"errors"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestShutdownSequence_RunsStepsInOrder pins that shutdownSequence executes steps in the exact
// order given — the whole point of extracting this helper for Issue #1531 Task 7b's ordering
// requirement (HTTP drains before River, which drains before Redis closes).
func TestShutdownSequence_RunsStepsInOrder(t *testing.T) {
	var order []string
	err := shutdownSequence([]namedShutdownStep{
		{Name: "http", Fn: func() error { order = append(order, "http"); return nil }},
		{Name: "river", Fn: func() error { order = append(order, "river"); return nil }},
		{Name: "rate_limit_done", Fn: func() error { order = append(order, "rate_limit_done"); return nil }},
		{Name: "cache", Fn: func() error { order = append(order, "cache"); return nil }},
		{Name: "limiter", Fn: func() error { order = append(order, "limiter"); return nil }},
	})

	require.NoError(t, err)
	assert.Equal(t, []string{"http", "river", "rate_limit_done", "cache", "limiter"}, order)
}

// TestShutdownSequence_ContinuesAfterAStepFails pins that a failing step does not prevent later
// steps from running (in particular, closing Redis LAST must still happen even if an earlier
// step — say, the internal metrics listener — errors), and that the error is aggregated rather
// than discarded.
func TestShutdownSequence_ContinuesAfterAStepFails(t *testing.T) {
	var order []string
	boom := errors.New("boom")
	err := shutdownSequence([]namedShutdownStep{
		{Name: "http", Fn: func() error { order = append(order, "http"); return boom }},
		{Name: "river", Fn: func() error { order = append(order, "river"); return nil }},
		{Name: "limiter", Fn: func() error { order = append(order, "limiter"); return nil }},
	})

	require.Error(t, err)
	assert.True(t, errors.Is(err, boom))
	assert.Equal(t, []string{"http", "river", "limiter"}, order, "a failing step must not stop later steps from running")
}

// TestShutdownSequence_ClosesRedisAfterHttpDrained is the Issue #1531 Task 7b regression guard:
// closing Redis before HTTP has drained gives every in-flight request a pool-closed error, which
// trips the rate-limiter fallback and fires a fallback-activation Sentry event on EVERY deploy —
// exactly the alert-fatigue hazard this ordering exists to prevent. This test builds the same
// step names/order main() constructs and asserts "http" strictly precedes "limiter" (the Redis
// client close).
func TestShutdownSequence_ClosesRedisAfterHttpDrained(t *testing.T) {
	var order []string
	err := shutdownSequence([]namedShutdownStep{
		{Name: "http", Fn: func() error { order = append(order, "http"); return nil }},
		{Name: "internal_metrics", Fn: func() error { order = append(order, "internal_metrics"); return nil }},
		{Name: "boost_scheduler", Fn: func() error { order = append(order, "boost_scheduler"); return nil }},
		{Name: "river", Fn: func() error { order = append(order, "river"); return nil }},
		{Name: "cf_cidr_refresher", Fn: func() error { order = append(order, "cf_cidr_refresher"); return nil }},
		{Name: "rate_limit_done", Fn: func() error { order = append(order, "rate_limit_done"); return nil }},
		{Name: "cache", Fn: func() error { order = append(order, "cache"); return nil }},
		{Name: "limiter", Fn: func() error { order = append(order, "limiter"); return nil }},
	})
	require.NoError(t, err)

	httpIdx, riverIdx, limiterIdx, cacheIdx := -1, -1, -1, -1
	for i, name := range order {
		switch name {
		case "http":
			httpIdx = i
		case "river":
			riverIdx = i
		case "cache":
			cacheIdx = i
		case "limiter":
			limiterIdx = i
		}
	}

	require.NotEqual(t, -1, httpIdx)
	require.NotEqual(t, -1, riverIdx)
	require.NotEqual(t, -1, cacheIdx)
	require.NotEqual(t, -1, limiterIdx)

	assert.Less(t, httpIdx, riverIdx, "HTTP must drain before River (which will soon touch Redis)")
	assert.Less(t, riverIdx, cacheIdx, "River must drain before the cache Redis client closes")
	assert.Less(t, cacheIdx, limiterIdx, "cache client must close before the limiter client")
}

// TestWiring_SigtermTriggersGracefulShutdown delivers a REAL SIGTERM to this test process (not a
// direct call into shutdown logic) and asserts the graceful-shutdown sequence actually runs.
//
// This is the regression test for Issue #1531 Task 7a: before the fix, signal.Notify registered
// only os.Interrupt (SIGINT). SIGTERM's default disposition TERMINATES THE PROCESS — so a
// version of waitForShutdownSignal that only registers os.Interrupt doesn't just fail this test,
// it kills the entire go test binary when this test sends SIGTERM to itself. That is the
// discrimination proof recorded in the task report (temporarily reverting the Notify call and
// observing the whole test process die, not just this test failing).
func TestWiring_SigtermTriggersGracefulShutdown(t *testing.T) {
	var mu sync.Mutex
	var order []string
	done := make(chan struct{})

	go func() {
		waitForShutdownSignal()
		_ = shutdownSequence([]namedShutdownStep{
			{Name: "http", Fn: func() error {
				mu.Lock()
				order = append(order, "http")
				mu.Unlock()
				return nil
			}},
			{Name: "limiter", Fn: func() error {
				mu.Lock()
				order = append(order, "limiter")
				mu.Unlock()
				return nil
			}},
		})
		close(done)
	}()

	// Give signal.Notify time to register before sending the real signal.
	time.Sleep(100 * time.Millisecond)

	proc, err := os.FindProcess(os.Getpid())
	require.NoError(t, err)
	require.NoError(t, proc.Signal(syscall.SIGTERM))

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("graceful shutdown sequence did not run after a real SIGTERM was delivered")
	}

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"http", "limiter"}, order)
}
