package domain

import (
	"net/netip"
	"time"
)

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

// AggregateIPv6Identity canonicalises a client identity for rate-limit bucketing: an IPv6 address
// collapses to its /64, everything else (IPv4, IPv4-in-IPv6, or a non-address string such as the
// sentinel key) is returned unchanged.
//
// It lives in domain rather than in ratelimitstore because BOTH the authoritative Redis backend
// and the middleware's in-process cost pre-check must bucket by the SAME identity, and the
// middleware package deliberately cannot import ratelimitstore (that package's doc comment keeps
// go-redis out of middleware's hermetic test suite). Two hand-written copies is how they would
// come to disagree — which is exactly the defect two adversarial reviewers independently found
// when the pre-check used the raw key while Redis aggregated: an attacker rotating /128s inside a
// single routed /64 minted a fresh full pre-check bucket per address, so the cost bound the
// pre-check exists to provide did not apply to the identity Redis actually enforces.
//
// /64 is the smallest routed IPv6 allocation in practice — a single home or mobile subscriber —
// and is what Cloudflare's own rate limiter aggregates to. Bucketing per /128 would let one
// subscriber mint 2^64 distinct buckets.
//
// ⚠ The Is4()/Is4In6() short-circuit is load-bearing, not defensive tidiness: netip parses an
// IPv4-mapped address as 6-ish, and masking one to /64 would collapse EVERY IPv4 client onto a
// single bucket.
func AggregateIPv6Identity(key string) string {
	addr, err := netip.ParseAddr(key)
	if err != nil {
		return key
	}
	if addr.Is4() || addr.Is4In6() {
		return key
	}
	if !addr.Is6() {
		return key
	}
	return netip.PrefixFrom(addr, 64).Masked().Addr().String()
}
