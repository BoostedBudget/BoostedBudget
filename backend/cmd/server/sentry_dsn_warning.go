package main

// Issue #1531 gap round 2 — the most important alert in the design may page nobody.
//
// redis_limiter_fallback_activations_total is "the metric that actually matters" per Issue #1531:
// it fires exactly when the fleet has silently reverted to the broken per-machine (N×) fallback
// behaviour. The metric itself is correctly edge-triggered. The DELIVERY path is the problem:
//
//   - Grafana: this repo has no alert-rule file at all (a pre-existing convention). Nothing
//     in-repo will ever create the alert this metric implies.
//   - Sentry: the fallback logs at Error, and the slog->Sentry tee (internal/observability/logger.go)
//     forwards Error+ -- but ONLY if cfg.SentryDSN != "". If SENTRY_DSN is unset in
//     production/staging, sentry.Init with an empty DSN disables silently, and the whole chain is
//     dead with no warning.
//
// So the fix here is NOT another alert-rule file (that would be alerting theatre — the repo
// deliberately has none, and inventing one nothing consumes would not close the gap) and NOT a
// validate() hard failure (refusing to boot over a missing observability DSN trades a silent
// alerting gap for a loud availability outage — the wrong swap for the one dependency whose entire
// job is telling you about outages). It is a startup ERROR-level log, loud enough to be noticed in
// aggregation/dashboards, that names the concrete consequence rather than a generic warning.
//
// Follows the same shape as clerkGateWarning (clerk_gate_warning.go, Issue #1450): the decision and
// message live in a small, directly-testable function; main() only calls it and logs the result.
// main() itself is not unit-testable (real config, real Sentry init), so wiring is verified
// separately via a narrow text check against main.go's source (see this package's test file
// convention, e.g. issue1450_clerk_gate_warning_test.go's header).

// sentryDSNWarningMessage is the warning's headline. Exported as a constant so the test asserts the
// exact string main() logs rather than a substring that could drift apart from it.
const sentryDSNWarningMessage = "SENTRY_DSN is unset — rate-limiter degradation and other " +
	"Error-level events will not reach Sentry; nothing will page"

// sentryDSNWarning reports whether the missing-Sentry-DSN warning is warranted for the given
// environment, and returns the message and structured attributes to log when it is.
//
// Warranted only when BOTH:
//   - environment is "production" or "staging" (a missing DSN in local dev/test is expected and
//     not worth an ERROR-level log every time a developer runs the binary), AND
//   - sentryDSN == "" (a configured DSN means the tee is live; nothing to warn about).
func sentryDSNWarning(environment, sentryDSN string) (msg string, attrs []any, warranted bool) {
	if sentryDSN != "" {
		return "", nil, false
	}
	if environment != "production" && environment != "staging" {
		return "", nil, false
	}

	return sentryDSNWarningMessage, []any{
		"environment", environment,
		"consequence_alerting", "the redis_limiter_fallback_activations_total fallback-activation " +
			"log (Error level) will never reach Sentry — a Redis outage can silently revert the " +
			"fleet to the broken per-machine rate limit for an unbounded time with no page",
		"consequence_scope", "ALL Error-level logs are affected, not just the rate limiter",
		"remediation", "set SENTRY_DSN",
	}, true
}
