package domain

import "time"

// RateLimitTier distinguishes the rate-limit bucket a request belongs to. Each tier gets its own
// limit and its own Redis key namespace (see ratelimitstore's key-naming doc comment) so a rolling
// deploy, a misconfigured limit, or an incident on one tier never bleeds into another.
type RateLimitTier int

const (
	// RateLimitTierRegular is the default per-identity limit applied to ordinary API traffic.
	RateLimitTierRegular RateLimitTier = iota
	// RateLimitTierAuth is the stricter per-identity limit applied to sensitive paths
	// (webhooks, QA auth bypass) — see middleware.isAuthEndpoint.
	RateLimitTierAuth
	// RateLimitTierSentinel is the fleet-wide (not per-identity) bucket used when a request's
	// client identity could not be resolved. See middleware.identitySentinelFallback for why this
	// exists and why it deliberately shares ONE bucket across every anomalous request rather than
	// bucketing per-source.
	RateLimitTierSentinel
)

// String returns the lowercase tier name used in Redis keys, logs, and metric labels. An
// out-of-range value returns "unknown" rather than panicking or returning an empty string, since a
// bad value here would otherwise become an unlabeled or malformed Redis key.
func (t RateLimitTier) String() string {
	switch t {
	case RateLimitTierRegular:
		return "regular"
	case RateLimitTierAuth:
		return "auth"
	case RateLimitTierSentinel:
		return "sentinel"
	default:
		return "unknown"
	}
}

// RateLimitDecision is the outcome of a single rate-limit check, returned by every backend
// (Redis-backed and in-memory fallback alike) so callers can treat them interchangeably.
type RateLimitDecision struct {
	// Allowed is true if the request may proceed.
	Allowed bool
	// RetryAfter is how long the caller should wait before retrying when Allowed is false. Zero
	// when Allowed is true. Backends must round this up to at least 1 second when denying — a
	// Retry-After of 0 invites an immediate retry storm.
	RetryAfter time.Duration
}
