package middleware

// SetCleanupGoroutineStoppedHookForTest wires (or, passed nil, clears) a test-only hook that
// IPRateLimiter's background cleanup goroutine calls immediately before it returns, after cfg.Done
// fires. It exists so a black-box test (package middleware_test) can observe THIS SPECIFIC
// goroutine's exit — e.g. via a channel the test owns and closes from the hook — instead of
// depending on the process-global runtime.NumGoroutine(), which counts every goroutine in the test
// binary (including ones started by unrelated tests or the Go runtime itself) and is therefore
// order-dependent and flaky under `-race`, `t.Parallel()`, or simply a slow-to-settle GC.
//
// Exported only via this _test.go file, so it is compiled into test binaries alone and is not part
// of this package's production API surface.
func SetCleanupGoroutineStoppedHookForTest(fn func()) {
	cleanupGoroutineStopped = fn
}
