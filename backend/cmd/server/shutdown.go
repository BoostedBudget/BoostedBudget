package main

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

// namedShutdownStep is one step of an ordered graceful-shutdown sequence. Name identifies the
// step in aggregated error output and in tests; Fn performs the actual work.
type namedShutdownStep struct {
	Name string
	Fn   func() error
}

// shutdownSequence runs steps IN ORDER and CONTINUES past a failing step, aggregating every
// error via errors.Join (nil if every step succeeded). This is deliberate: a failure in an early
// step (say, the internal metrics listener refusing to close) must never prevent later steps —
// most importantly closing Redis LAST — from running. See main()'s Issue #1531 Task 7b shutdown
// wiring for the exact required order and why it matters: closing Redis before HTTP has drained
// gives every in-flight request a pool-closed error, which trips the rate limiter's fallback and
// fires a fallback-activation Sentry event on every single deploy — training everyone to ignore
// the one alert that matters most.
func shutdownSequence(steps []namedShutdownStep) error {
	var errs []error
	for _, step := range steps {
		if err := step.Fn(); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", step.Name, err))
		}
	}
	return errors.Join(errs...)
}

// waitForShutdownSignal blocks until the process receives SIGINT or SIGTERM.
//
// ⚠ Issue #1531 Task 7a — SIGTERM, not just SIGINT. Both fly.toml and fly.staging.toml declare
// kill_signal = "SIGTERM" (default disposition: terminate immediately), but signal.Notify used to
// register os.Interrupt (SIGINT) alone. That meant the ENTIRE graceful-shutdown sequence below
// had never once run in production: every Fly deploy hard-killed in-flight HTTP requests and
// River jobs, then Fly SIGKILLed after kill_timeout. This is unrelated to Redis but is a
// prerequisite for every shutdown-ordering refinement in this task being anything but dead code
// on the only platform that matters.
//
// signal.Stop is deferred so a caller (in particular, a test that calls this repeatedly) doesn't
// accumulate stale registrations on a shared channel across calls.
func waitForShutdownSignal() {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(quit)
	<-quit
}
