package redisclient

import (
	"errors"
	"sync"
	"time"
)

// ErrBreakerOpen is returned by breaker.call while the breaker is open (or half-open with a probe
// already in flight), instead of invoking the wrapped function.
var ErrBreakerOpen = errors.New("redis: circuit breaker open")

// BreakerOptions configures a breaker. Zero values fall back to the documented defaults.
type BreakerOptions struct {
	// FailureThreshold is the number of CONSECUTIVE failures (in the Closed state) required to
	// trip the breaker open. Default: 5.
	FailureThreshold int
	// Cooldown is how long the breaker stays Open before allowing a single HalfOpen probe.
	// Default: 5s.
	Cooldown time.Duration
	// Now is a test seam for the clock. Nil means time.Now.
	Now func() time.Time
}

func (o BreakerOptions) withDefaults() BreakerOptions {
	if o.FailureThreshold <= 0 {
		o.FailureThreshold = 5
	}
	if o.Cooldown <= 0 {
		o.Cooldown = 5 * time.Second
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return o
}

type breakerState int

const (
	stateClosed breakerState = iota
	stateOpen
	stateHalfOpen
)

// breaker implements a Closed -> Open -> HalfOpen -> (Closed | Open) circuit breaker.
//
// States:
//   - Closed: calls pass through. A success resets the consecutive-failure count to 0. A failure
//     increments it; reaching FailureThreshold trips the breaker to Open.
//   - Open: calls are short-circuited with ErrBreakerOpen without invoking the wrapped function,
//     until Cooldown has elapsed since the breaker opened.
//   - HalfOpen: exactly one probe call is allowed through. Concurrent callers arriving while a
//     probe is in flight are short-circuited. The probe's outcome decides the next state: success
//     closes the breaker (and resets the failure count); failure reopens it for a full Cooldown.
type breaker struct {
	opts BreakerOptions

	mu                  sync.Mutex
	state               breakerState
	consecutiveFailures int
	openedAt            time.Time
	halfOpenProbeActive bool
}

func newBreaker(opts BreakerOptions) *breaker {
	return &breaker{opts: opts.withDefaults(), state: stateClosed}
}

// call invokes fn if the breaker permits it, and records the outcome. If the breaker is Open (or
// HalfOpen with a probe already in flight), fn is NOT called and ErrBreakerOpen is returned.
func (b *breaker) call(fn func() error) error {
	if !b.allow() {
		return ErrBreakerOpen
	}

	err := fn()
	b.recordResult(err)
	return err
}

// allow decides whether the caller may proceed, transitioning state as a side effect: an Open
// breaker whose Cooldown has elapsed transitions to HalfOpen and grants exactly the calling
// goroutine the single permitted probe (the mutex serializes concurrent callers so only one wins).
func (b *breaker) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case stateClosed:
		return true
	case stateOpen:
		if b.opts.Now().Sub(b.openedAt) < b.opts.Cooldown {
			return false
		}
		b.state = stateHalfOpen
		b.halfOpenProbeActive = true
		return true
	case stateHalfOpen:
		if b.halfOpenProbeActive {
			return false
		}
		b.halfOpenProbeActive = true
		return true
	default:
		return false
	}
}

func (b *breaker) recordResult(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case stateHalfOpen:
		b.halfOpenProbeActive = false
		if err == nil {
			b.state = stateClosed
			b.consecutiveFailures = 0
		} else {
			b.state = stateOpen
			b.openedAt = b.opts.Now()
		}
	case stateClosed:
		if err == nil {
			b.consecutiveFailures = 0
			return
		}
		b.consecutiveFailures++
		if b.consecutiveFailures >= b.opts.FailureThreshold {
			b.state = stateOpen
			b.openedAt = b.opts.Now()
		}
	case stateOpen:
		// Unreachable via call(): allow() never returns true while Open (it transitions to
		// HalfOpen first), so recordResult is never invoked in this state through the normal
		// call() path. Kept as an explicit, intentional no-op — not dead code to "clean up" — so a
		// future direct call to recordResult (bypassing allow()) fails safe instead of panicking
		// on an unhandled case.
	}
}
