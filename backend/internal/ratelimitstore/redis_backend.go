// Package ratelimitstore implements the fleet-wide (Redis-backed) rate limiter used by Issue
// #1531. It is deliberately its own package rather than living in internal/middleware: keeping
// go-redis out of internal/middleware means that package's existing hermetic test suite stays
// network-free and fast. Task 5 declares the consumer-side interface in internal/middleware over
// domain types; RedisBackend implements it here. This package imports domain and redisclient, and
// MUST NOT import internal/middleware — that dependency direction is Task 5's to own.
//
// # Algorithm: GCRA, and why not the alternatives
//
//   - Fixed-window INCR+EXPIRE: rejected. Two commands means either a non-atomic pair — where a
//     crash between them leaves a key with no TTL, i.e. a permanent bucket and a permanent
//     lockout for that identity — or a Lua script anyway. It also permits nearly 2x the limit
//     across a window boundary, reintroducing the exact "effective limits up to 2x" defect this
//     task exists to fix.
//   - Sorted-set sliding-window log: rejected. O(limit) members per identity and three commands
//     per request. On Upstash's per-command billing that is the expensive option, and memory
//     grows as limit x identities on the one instance provisioned noeviction, which cannot afford
//     unbounded growth.
//   - GCRA (leaky bucket, theoretical arrival time in one key): chosen. One key, one integer, one
//     round-trip, one write. Mathematically equivalent to golang.org/x/time/rate with
//     burst == limit, which is exactly what the existing in-memory limiter builds
//     (internal/middleware/rate_limit.go). That equivalence is load-bearing: because the Redis
//     primary and the in-memory fallback are the same limiter, switching between them is a change
//     of state scope (fleet-wide <-> per-machine), not a change of behaviour. It also yields an
//     exact Retry-After instead of a fixed window length.
//
// # The Lua script's four load-bearing properties
//
//  1. Clock skew is structurally impossible. The script reads redis.call('TIME') — the *Redis
//     server's* clock. No client timestamp is ever passed in, so two machines with 400ms of NTP
//     drift agree perfectly. (TIME is non-deterministic and needs effects replication: fine on
//     Redis >= 5, i.e. Upstash and redis:7-alpine. That is this package's floor.)
//  2. Every write sets a TTL, on every path. There is exactly one SET and it always carries PX.
//     Both the create path (no prior key) and the update path (existing key) go through it.
//  3. Partial failure cannot leave partial state. There is exactly one write and it is the last
//     statement, so any abort happens strictly before it. A future "let me add one more
//     redis.call" change to this script is a semantics change — treat it as one.
//  4. Client-side partial failure is biased toward availability. If the script succeeds
//     server-side but the reply times out, budget was consumed while the caller falls back to
//     memory and allows. At most a small over-count plus one allow — the correct direction under
//     "never fail closed". This is a deliberate availability bias, not a harmless accounting
//     detail.
package ratelimitstore

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	redis "github.com/redis/go-redis/v9"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
)

// gcraScript implements GCRA (generic cell rate algorithm) as a single atomic Redis write.
//
// KEYS[1] = bucket key
// ARGV[1] = emission_interval_us  (window_us / limit)
// ARGV[2] = dvt_us                (burst * emission_interval_us; burst == limit)
// ARGV[3] = cost                  (always 1 today)
// returns  {allowed(0|1), retry_after_ms, remaining}
const gcraScriptSource = `
local emission = tonumber(ARGV[1])
local dvt      = tonumber(ARGV[2])
local cost     = tonumber(ARGV[3])

local t   = redis.call('TIME')
local now = (tonumber(t[1]) * 1000000) + tonumber(t[2])

local tat = tonumber(redis.call('GET', KEYS[1])) or now
if tat < now then tat = now end

local new_tat  = tat + (emission * cost)
local allow_at = new_tat - dvt

if now < allow_at then
  local remaining = math.floor(((now + dvt) - tat) / emission)
  if remaining < 0 then remaining = 0 end
  return {0, math.ceil((allow_at - now) / 1000), remaining}
end

local ttl_ms = math.ceil((new_tat - now) / 1000) + 1000
redis.call('SET', KEYS[1], new_tat, 'PX', ttl_ms)

local remaining = math.floor(((now + dvt) - new_tat) / emission)
if remaining < 0 then remaining = 0 end
return {1, 0, remaining}
`

// gcraScript is the compiled script handed to redisclient.Client.RunScript, which runs it via
// EVALSHA with automatic EVAL fallback on NOSCRIPT (e.g. after a SCRIPT FLUSH).
var gcraScript = redis.NewScript(gcraScriptSource)

// keyVersion namespaces every key this backend writes. It matters because rolling deploys run two
// versions of this code simultaneously for minutes; sharing a key across two meanings during that
// window (e.g. a limit change, or a units change) is a real bug, not a theoretical one. Bump it
// whenever the script's ARGV semantics or the key's stored value's meaning changes.
//
// GDPR/GCRA round-6 finding P2: bumped v1 -> v2 here. L4 (IPv6 /64 aggregation) changed exactly
// what this contract says to watch for — `rl:v1:regular:2001:db8::` used to denote ONE address and
// now denotes a whole /64 — without bumping the version. During a rolling deploy this would
// fragment the shared budget across old (/128) and new (/64) writers, and a client whose address
// IS the /64 base would silently inherit the aggregated meaning. Low severity pre-launch (zero
// production machines, and the fragmentation is transient), but the constant's own contract says
// to bump it, and that contract exists because someone already thought through rolling deploys.
const keyVersion = "v2"

// Options configures a RedisBackend.
type Options struct {
	// RegularLimit is the max requests per Window for RateLimitTierRegular.
	RegularLimit int
	// AuthLimit is the max requests per Window for RateLimitTierAuth.
	AuthLimit int
	// SentinelLimit is the max requests per Window for RateLimitTierSentinel. Defaults to
	// RegularLimit when zero.
	SentinelLimit int
	// Window is the rate-limit window. Default: 1 minute.
	Window time.Duration
}

func (o Options) withDefaults() Options {
	if o.Window <= 0 {
		o.Window = time.Minute
	}
	if o.SentinelLimit <= 0 {
		o.SentinelLimit = o.RegularLimit
	}
	return o
}

// RedisBackend is a fleet-wide GCRA rate limiter backed by Redis. Multiple RedisBackend instances
// (e.g. one per machine) constructed against the same Redis share one budget per identity — that
// is the entire point of Issue #1531: the previous in-memory-only limiter kept per-process state,
// so N machines meant up to Nx the configured limit.
type RedisBackend struct {
	client redisclient.Client
	opts   Options
}

// NewRedisBackend constructs a RedisBackend. c is typically produced by redisclient.New, but any
// redisclient.Client (including redisclient.Fake, for unit tests) works.
func NewRedisBackend(c redisclient.Client, opts Options) *RedisBackend {
	return &RedisBackend{client: c, opts: opts.withDefaults()}
}

// Name identifies this backend for logs and metric labels.
func (b *RedisBackend) Name() string { return "redis" }

// limitFor returns the configured limit for tier.
func (b *RedisBackend) limitFor(tier domain.RateLimitTier) int {
	switch tier {
	case domain.RateLimitTierAuth:
		return b.opts.AuthLimit
	case domain.RateLimitTierSentinel:
		return b.opts.SentinelLimit
	default:
		return b.opts.RegularLimit
	}
}

// bucketKey builds the Redis key for tier/key. The sentinel tier gets a dedicated, identity-free
// key ("rl:v2:sentinel") rather than a per-identity key under the regular namespace, so it is
// greppable in the Upstash console during the exact incident it exists to signal, and so it can be
// given its own limit later without touching key parsing.
func (b *RedisBackend) bucketKey(tier domain.RateLimitTier, key string) string {
	if tier == domain.RateLimitTierSentinel {
		return fmt.Sprintf("rl:%s:sentinel", keyVersion)
	}
	return fmt.Sprintf("rl:%s:%s:%s", keyVersion, tier.String(), aggregateIPv6Identity(key))
}

// aggregateIPv6Identity is the Issue #1531 L4 fix: it bounds Redis key cardinality by aggregating
// IPv6 identities to their /64 (as Cloudflare's own rate limiter does), so an attacker with one
// routed /64 -- a single home/mobile allocation in practice -- cannot mint unbounded distinct
// Redis keys by rotating through /128 addresses within it. This matters specifically because this
// backend runs on a `noeviction` Redis instance with no cap equivalent to the in-memory store's
// MaxEntries: unbounded key growth eventually OOMs the instance, every SET starts failing, the
// breaker trips, and the ENTIRE FLEET silently drops to per-machine limiting — making the cheapest
// way to disable the fleet-wide limiter the exact traffic pattern it exists to stop.
//
// IPv4 must be, and is, completely unaffected — including v4-mapped-in-v6 addresses
// (::ffff:a.b.c.d). A naive "mask anything that parses as an IP to /64" implementation would take
// the first 64 bits of a v4-mapped address, which are the SAME fixed prefix
// (0000:0000:0000:0000:0000:ffff::) for every possible IPv4 address, and collapse every v4-mapped
// client in the world onto one bucket — addr.Is4() and addr.Is4In6() are both checked, and either
// one short-circuits back to the identity function entirely unmodified.
//
// key values this function cannot parse as an IP (notably identitySentinelFallback's literal
// string, though the sentinel tier never reaches this function at all — see bucketKey above) pass
// through unchanged: aggregation is strictly additive risk mitigation, never a source of new
// collisions for values that were never addresses in the first place.
func aggregateIPv6Identity(key string) string {
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
	prefix := netip.PrefixFrom(addr, 64)
	return prefix.Masked().Addr().String()
}

// emissionAndDVT derives the script's ARGV[1] (emission_interval_us) and ARGV[2] (dvt_us) from
// limit and window. burst == limit by construction (see package doc comment on why that equates
// this algorithm to golang.org/x/time/rate's burst semantics).
func emissionAndDVT(limit int, window time.Duration) (emissionUs int64, dvtUs int64) {
	windowUs := window.Microseconds()
	emissionUs = windowUs / int64(limit)
	dvtUs = emissionUs * int64(limit)
	return emissionUs, dvtUs
}

// Allow evaluates the GCRA script for tier/key and returns whether the request is allowed.
//
// No client timestamp is ever sent to Redis: the only ARGV values are emission_interval_us,
// dvt_us, and cost, all derived purely from configuration. The script itself reads the Redis
// server's own clock via TIME. See the package doc comment's "clock skew is structurally
// impossible" property.
//
// An unexpected script reply shape (wrong type, wrong length, or nil) is always returned as an
// error, never coerced into an allow — a sloppy type assertion here is the one place that could
// silently turn the limiter off entirely.
func (b *RedisBackend) Allow(ctx context.Context, tier domain.RateLimitTier, key string) (domain.RateLimitDecision, error) {
	limit := b.limitFor(tier)
	if limit <= 0 {
		return domain.RateLimitDecision{}, fmt.Errorf("ratelimitstore: no positive limit configured for tier %q", tier.String())
	}

	emissionUs, dvtUs := emissionAndDVT(limit, b.opts.Window)
	redisKey := b.bucketKey(tier, key)

	result, err := b.client.RunScript(ctx, gcraScript, []string{redisKey}, emissionUs, dvtUs, int64(1))
	if err != nil {
		return domain.RateLimitDecision{}, fmt.Errorf("ratelimitstore: run script for tier %q: %w", tier.String(), err)
	}

	return decisionFromScriptResult(result)
}

// decisionFromScriptResult validates and converts the raw script reply into a domain.RateLimitDecision.
// It requires exactly the shape the script documents: a 3+-element slice whose first two elements
// are integers. Anything else — nil, wrong type, wrong length — is an error.
func decisionFromScriptResult(result any) (domain.RateLimitDecision, error) {
	elems, ok := result.([]any)
	if !ok || len(elems) < 2 {
		return domain.RateLimitDecision{}, fmt.Errorf("ratelimitstore: unexpected script reply shape (want a >=2 element array, got %T: %#v)", result, result)
	}

	allowedRaw, ok := asInt64(elems[0])
	if !ok {
		return domain.RateLimitDecision{}, fmt.Errorf("ratelimitstore: unexpected script reply element 0 (allowed) type %T: %#v", elems[0], elems[0])
	}
	retryAfterMsRaw, ok := asInt64(elems[1])
	if !ok {
		return domain.RateLimitDecision{}, fmt.Errorf("ratelimitstore: unexpected script reply element 1 (retry_after_ms) type %T: %#v", elems[1], elems[1])
	}

	allowed := allowedRaw != 0
	var retryAfter time.Duration
	if !allowed {
		// Retry-After must round up to at least 1 second: a Retry-After of 0 (or a sub-second
		// value) invites an immediate retry storm.
		retryAfter = time.Duration(retryAfterMsRaw) * time.Millisecond
		if retryAfter < time.Second {
			retryAfter = time.Second
		}
	}

	return domain.RateLimitDecision{Allowed: allowed, RetryAfter: retryAfter}, nil
}

// asInt64 accepts the integer-ish types a Redis script reply (via go-redis) or a test fake might
// plausibly hand back, and rejects everything else (notably strings) — the malformed-reply tests
// pin that a string element must be an error, not a coerced value.
func asInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int:
		return int64(n), true
	case int32:
		return int64(n), true
	default:
		return 0, false
	}
}
