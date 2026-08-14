package redisclient

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errBreakerTestFailure = errors.New("boom")

func countingFn(n *int32, err error) func() error {
	return func() error {
		atomic.AddInt32(n, 1)
		return err
	}
}

func TestBreaker_ClosedInitially(t *testing.T) {
	b := newBreaker(BreakerOptions{})

	var invocations int32
	err := b.call(countingFn(&invocations, nil))

	require.NoError(t, err)
	assert.EqualValues(t, 1, invocations, "a newly constructed breaker must be Closed and forward the call")
}

func TestBreaker_OpensAfterConsecutiveFailureThreshold(t *testing.T) {
	b := newBreaker(BreakerOptions{FailureThreshold: 3})

	var invocations int32
	for i := 0; i < 3; i++ {
		err := b.call(countingFn(&invocations, errBreakerTestFailure))
		require.ErrorIs(t, err, errBreakerTestFailure)
	}
	assert.EqualValues(t, 3, invocations)

	// The breaker should now be open: this call must NOT invoke fn.
	err := b.call(countingFn(&invocations, nil))
	require.ErrorIs(t, err, ErrBreakerOpen)
	assert.EqualValues(t, 3, invocations, "breaker should short-circuit once the threshold is reached")
}

func TestBreaker_SuccessResetsTheFailureCount(t *testing.T) {
	b := newBreaker(BreakerOptions{FailureThreshold: 5})

	var invocations int32
	for i := 0; i < 4; i++ {
		require.ErrorIs(t, b.call(countingFn(&invocations, errBreakerTestFailure)), errBreakerTestFailure)
	}
	require.NoError(t, b.call(countingFn(&invocations, nil)))
	for i := 0; i < 4; i++ {
		require.ErrorIs(t, b.call(countingFn(&invocations, errBreakerTestFailure)), errBreakerTestFailure)
	}

	assert.EqualValues(t, 9, invocations, "all 9 calls should have been forwarded")

	// A 10th failure would be the 5th CONSECUTIVE failure since the success reset the counter, so
	// the breaker must still be Closed and forward it (not short-circuit).
	err := b.call(countingFn(&invocations, errBreakerTestFailure))
	require.ErrorIs(t, err, errBreakerTestFailure)
	assert.EqualValues(t, 10, invocations, "success must have reset the consecutive-failure count")
}

func TestBreaker_OpenShortCircuitsWithoutCallingTheFunction(t *testing.T) {
	b := newBreaker(BreakerOptions{FailureThreshold: 1})

	var invocations int32
	require.ErrorIs(t, b.call(countingFn(&invocations, errBreakerTestFailure)), errBreakerTestFailure)
	require.EqualValues(t, 1, invocations)

	for i := 0; i < 10; i++ {
		err := b.call(countingFn(&invocations, nil))
		require.ErrorIs(t, err, ErrBreakerOpen)
	}

	assert.EqualValues(t, 1, invocations, "an open breaker must never invoke the wrapped function")
}

func TestBreaker_HalfOpenAllowsExactlyOneProbe(t *testing.T) {
	now := time.Now()
	var nowMu sync.Mutex
	clock := func() time.Time {
		nowMu.Lock()
		defer nowMu.Unlock()
		return now
	}
	advance := func(d time.Duration) {
		nowMu.Lock()
		now = now.Add(d)
		nowMu.Unlock()
	}

	b := newBreaker(BreakerOptions{FailureThreshold: 1, Cooldown: time.Second, Now: clock})

	var invocations int32
	require.ErrorIs(t, b.call(countingFn(&invocations, errBreakerTestFailure)), errBreakerTestFailure)
	advance(2 * time.Second) // past cooldown

	entered := make(chan struct{})
	release := make(chan struct{})
	probeDone := make(chan error, 1)
	go func() {
		probeDone <- b.call(func() error {
			atomic.AddInt32(&invocations, 1)
			close(entered)
			<-release
			return nil
		})
	}()

	<-entered // the probe is now in flight: breaker is HalfOpen with halfOpenProbeActive == true

	const concurrentAttempts = 100
	var wg sync.WaitGroup
	wg.Add(concurrentAttempts)
	for i := 0; i < concurrentAttempts; i++ {
		go func() {
			defer wg.Done()
			err := b.call(countingFn(&invocations, nil))
			assert.ErrorIs(t, err, ErrBreakerOpen, "only the original probe may run while one is in flight")
		}()
	}
	wg.Wait()

	close(release)
	require.NoError(t, <-probeDone)

	assert.EqualValues(t, 2, invocations, "exactly 1 probe invocation plus the 1 initial failing call")
}

func TestBreaker_HalfOpenFailureReopensForAFullCooldown(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }
	b := newBreaker(BreakerOptions{FailureThreshold: 1, Cooldown: 5 * time.Second, Now: clock})

	var invocations int32
	require.ErrorIs(t, b.call(countingFn(&invocations, errBreakerTestFailure)), errBreakerTestFailure)
	require.EqualValues(t, 1, invocations)

	now = now.Add(6 * time.Second) // past first cooldown
	require.ErrorIs(t, b.call(countingFn(&invocations, errBreakerTestFailure)), errBreakerTestFailure)
	require.EqualValues(t, 2, invocations, "the half-open probe should have been invoked and failed")

	// Less than a FULL cooldown since the probe's failure re-opened the breaker: must still be open.
	now = now.Add(3 * time.Second)
	err := b.call(countingFn(&invocations, nil))
	require.ErrorIs(t, err, ErrBreakerOpen)
	assert.EqualValues(t, 2, invocations, "breaker must stay open for a full cooldown after the failed probe")

	// Now past the FULL cooldown measured from the failed probe.
	now = now.Add(3 * time.Second)
	require.NoError(t, b.call(countingFn(&invocations, nil)))
	assert.EqualValues(t, 3, invocations)
}

func TestBreaker_HalfOpenSuccessCloses(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }
	b := newBreaker(BreakerOptions{FailureThreshold: 2, Cooldown: time.Second, Now: clock})

	var invocations int32
	require.ErrorIs(t, b.call(countingFn(&invocations, errBreakerTestFailure)), errBreakerTestFailure)
	require.ErrorIs(t, b.call(countingFn(&invocations, errBreakerTestFailure)), errBreakerTestFailure)

	now = now.Add(2 * time.Second)
	require.NoError(t, b.call(countingFn(&invocations, nil)), "half-open probe succeeds")

	// If the breaker truly closed (not merely allowed one more call), 1 failure below the
	// threshold must still leave it closed and forwarding calls.
	require.ErrorIs(t, b.call(countingFn(&invocations, errBreakerTestFailure)), errBreakerTestFailure)
	err := b.call(countingFn(&invocations, nil))
	require.NoError(t, err, "breaker should be fully Closed with a reset failure count after the successful probe")
}

func TestBreaker_IsSafeUnderConcurrentUse(t *testing.T) {
	b := newBreaker(BreakerOptions{FailureThreshold: 5, Cooldown: 10 * time.Millisecond})

	var wg sync.WaitGroup
	const goroutines = 100
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			var err error
			if i%2 == 0 {
				err = errBreakerTestFailure
			}
			_ = b.call(func() error { return err })
		}()
	}
	wg.Wait()
}
