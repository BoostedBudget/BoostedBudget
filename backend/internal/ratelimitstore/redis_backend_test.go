package ratelimitstore_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/ratelimitstore"
	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
)

func newTestBackend(fake *redisclient.Fake, opts ratelimitstore.Options) *ratelimitstore.RedisBackend {
	return ratelimitstore.NewRedisBackend(fake, opts)
}

func TestRedisBackend_KeyNamingIncludesVersionAndTier(t *testing.T) {
	fake := redisclient.NewFake("limiter")
	fake.RunScriptResult = []any{int64(1), int64(0), int64(9)}
	b := newTestBackend(fake, ratelimitstore.Options{RegularLimit: 10, AuthLimit: 5, Window: time.Minute})

	if _, err := b.Allow(context.Background(), domain.RateLimitTierRegular, "1.2.3.4"); err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if _, err := b.Allow(context.Background(), domain.RateLimitTierAuth, "1.2.3.4"); err != nil {
		t.Fatalf("Allow: %v", err)
	}

	calls := fake.ScriptCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 script calls, got %d", len(calls))
	}
	wantRegular := "rl:v2:regular:1.2.3.4"
	wantAuth := "rl:v2:auth:1.2.3.4"
	if got := calls[0].Keys; len(got) != 1 || got[0] != wantRegular {
		t.Errorf("regular key = %v, want [%s]", got, wantRegular)
	}
	if got := calls[1].Keys; len(got) != 1 || got[0] != wantAuth {
		t.Errorf("auth key = %v, want [%s]", got, wantAuth)
	}
}

func TestRedisBackend_SentinelIdentityGetsItsOwnDedicatedKeyWithoutTheIdentitySuffix(t *testing.T) {
	fake := redisclient.NewFake("limiter")
	fake.RunScriptResult = []any{int64(1), int64(0), int64(9)}
	b := newTestBackend(fake, ratelimitstore.Options{RegularLimit: 10, Window: time.Minute})

	if _, err := b.Allow(context.Background(), domain.RateLimitTierSentinel, "!rate-limit-identity-unresolved!"); err != nil {
		t.Fatalf("Allow: %v", err)
	}

	calls := fake.ScriptCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 script call, got %d", len(calls))
	}
	want := "rl:v2:sentinel"
	if got := calls[0].Keys; len(got) != 1 || got[0] != want {
		t.Errorf("sentinel key = %v, want [%s]", got, want)
	}
}

func TestRedisBackend_EmissionAndDvtDerivedFromLimitAndWindow(t *testing.T) {
	fake := redisclient.NewFake("limiter")
	fake.RunScriptResult = []any{int64(1), int64(0), int64(99)}
	b := newTestBackend(fake, ratelimitstore.Options{RegularLimit: 100, Window: 60 * time.Second})

	if _, err := b.Allow(context.Background(), domain.RateLimitTierRegular, "id"); err != nil {
		t.Fatalf("Allow: %v", err)
	}

	calls := fake.ScriptCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 script call, got %d", len(calls))
	}
	args := calls[0].Args
	if len(args) < 2 {
		t.Fatalf("expected at least 2 args, got %v", args)
	}
	wantEmission := int64(600000)
	wantDVT := int64(60000000)
	if got := toInt64(t, args[0]); got != wantEmission {
		t.Errorf("emission ARGV[1] = %d, want %d", got, wantEmission)
	}
	if got := toInt64(t, args[1]); got != wantDVT {
		t.Errorf("dvt ARGV[2] = %d, want %d", got, wantDVT)
	}
}

func TestRedisBackend_PassesNoClientTimestampToTheScript(t *testing.T) {
	fake := redisclient.NewFake("limiter")
	fake.RunScriptResult = []any{int64(1), int64(0), int64(9)}
	b := newTestBackend(fake, ratelimitstore.Options{RegularLimit: 10, Window: time.Minute})

	before := time.Now()
	if _, err := b.Allow(context.Background(), domain.RateLimitTierRegular, "id"); err != nil {
		t.Fatalf("Allow: %v", err)
	}

	calls := fake.ScriptCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 script call, got %d", len(calls))
	}
	args := calls[0].Args
	// Exactly 3 args: emission_interval_us, dvt_us, cost. No 4th "now" argument, and none of the
	// values are anywhere near a wall-clock microsecond timestamp (which today is ~1.7e15).
	if len(args) != 3 {
		t.Fatalf("expected exactly 3 args (emission, dvt, cost), got %d: %v", len(args), args)
	}
	const wallClockFloor = int64(1_000_000_000_000_000) // ~2001-09-09 in Unix microseconds
	for i, a := range args {
		v := toInt64(t, a)
		if v >= wallClockFloor {
			t.Errorf("arg[%d] = %d looks like a wall-clock timestamp (captured around %v) — the script must read its own clock via TIME, not receive one from the client", i, v, before)
		}
	}
	// The cost argument in particular must be the fixed constant 1, not derived from time.
	if got := toInt64(t, args[2]); got != 1 {
		t.Errorf("cost ARGV[3] = %d, want 1", got)
	}
}

func TestRedisBackend_AllowedResultMapsToDecisionAllowed(t *testing.T) {
	fake := redisclient.NewFake("limiter")
	fake.RunScriptResult = []any{int64(1), int64(0), int64(9)}
	b := newTestBackend(fake, ratelimitstore.Options{RegularLimit: 10, Window: time.Minute})

	decision, err := b.Allow(context.Background(), domain.RateLimitTierRegular, "id")
	if err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if !decision.Allowed {
		t.Error("expected Allowed = true")
	}
	if decision.RetryAfter != 0 {
		t.Errorf("expected RetryAfter = 0 when allowed, got %v", decision.RetryAfter)
	}
}

func TestRedisBackend_DeniedResultMapsToDecisionDenied(t *testing.T) {
	fake := redisclient.NewFake("limiter")
	fake.RunScriptResult = []any{int64(0), int64(5000), int64(0)}
	b := newTestBackend(fake, ratelimitstore.Options{RegularLimit: 10, Window: time.Minute})

	decision, err := b.Allow(context.Background(), domain.RateLimitTierRegular, "id")
	if err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if decision.Allowed {
		t.Error("expected Allowed = false")
	}
	if decision.RetryAfter != 5*time.Second {
		t.Errorf("RetryAfter = %v, want 5s", decision.RetryAfter)
	}
}

func TestRedisBackend_RetryAfterIsRoundedUpToAtLeastOneSecond(t *testing.T) {
	fake := redisclient.NewFake("limiter")
	fake.RunScriptResult = []any{int64(0), int64(250), int64(0)}
	b := newTestBackend(fake, ratelimitstore.Options{RegularLimit: 10, Window: time.Minute})

	decision, err := b.Allow(context.Background(), domain.RateLimitTierRegular, "id")
	if err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if decision.Allowed {
		t.Fatal("expected Allowed = false")
	}
	if decision.RetryAfter != time.Second {
		t.Errorf("RetryAfter = %v, want 1s (rounded up from 250ms)", decision.RetryAfter)
	}
}

func TestRedisBackend_UnexpectedScriptReturnShapeIsAnErrorNotAnAllow(t *testing.T) {
	fake := redisclient.NewFake("limiter")
	// A malformed reply: a single integer instead of the expected 3-element array. A sloppy type
	// assertion (e.g. treating any non-error reply as "allowed") would silently disable the limiter
	// here.
	fake.RunScriptResult = int64(1)
	b := newTestBackend(fake, ratelimitstore.Options{RegularLimit: 10, Window: time.Minute})

	decision, err := b.Allow(context.Background(), domain.RateLimitTierRegular, "id")
	if err == nil {
		t.Fatal("expected an error for a malformed script reply, got nil")
	}
	if decision.Allowed {
		t.Error("expected Allowed = false on a malformed reply, got true")
	}
}

func TestRedisBackend_UnexpectedScriptReturnShapeWithWrongElementTypesIsAnError(t *testing.T) {
	fake := redisclient.NewFake("limiter")
	// Right shape (3 elements) but wrong element types — must still be rejected, not coerced.
	fake.RunScriptResult = []any{"yes", "no", "maybe"}
	b := newTestBackend(fake, ratelimitstore.Options{RegularLimit: 10, Window: time.Minute})

	decision, err := b.Allow(context.Background(), domain.RateLimitTierRegular, "id")
	if err == nil {
		t.Fatal("expected an error for a malformed script reply, got nil")
	}
	if decision.Allowed {
		t.Error("expected Allowed = false on a malformed reply, got true")
	}
}

func TestRedisBackend_NilReplyIsAnErrorNotAnAllow(t *testing.T) {
	fake := redisclient.NewFake("limiter")
	fake.RunScriptResult = nil
	b := newTestBackend(fake, ratelimitstore.Options{RegularLimit: 10, Window: time.Minute})

	decision, err := b.Allow(context.Background(), domain.RateLimitTierRegular, "id")
	if err == nil {
		t.Fatal("expected an error for a nil script reply, got nil")
	}
	if decision.Allowed {
		t.Error("expected Allowed = false on a nil reply, got true")
	}
}

func TestRedisBackend_ScriptErrorIsReturnedNotSilentlyAllowed(t *testing.T) {
	fake := redisclient.NewFake("limiter")
	wantErr := errors.New("redis: connection refused")
	fake.RunScriptErr = wantErr
	b := newTestBackend(fake, ratelimitstore.Options{RegularLimit: 10, Window: time.Minute})

	decision, err := b.Allow(context.Background(), domain.RateLimitTierRegular, "id")
	if err == nil {
		t.Fatal("expected the script error to propagate, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("expected wrapped error to match %v, got %v", wantErr, err)
	}
	if decision.Allowed {
		t.Error("expected Allowed = false when the script call itself errors, got true")
	}
}

func TestRedisBackend_SentinelLimitDefaultsToRegularLimitWhenZero(t *testing.T) {
	fake := redisclient.NewFake("limiter")
	fake.RunScriptResult = []any{int64(1), int64(0), int64(9)}
	b := newTestBackend(fake, ratelimitstore.Options{RegularLimit: 42, Window: 60 * time.Second})

	if _, err := b.Allow(context.Background(), domain.RateLimitTierSentinel, ""); err != nil {
		t.Fatalf("Allow: %v", err)
	}

	calls := fake.ScriptCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 script call, got %d", len(calls))
	}
	// emission = window_us / limit; if SentinelLimit defaulted to RegularLimit (42), emission
	// should match what RegularLimit alone would produce.
	wantEmission := (60 * time.Second).Microseconds() / 42
	if got := toInt64(t, calls[0].Args[0]); got != wantEmission {
		t.Errorf("sentinel emission = %d, want %d (SentinelLimit should default to RegularLimit)", got, wantEmission)
	}
}

// TestRedisBackend_IPv6IdentitiesAggregateToA64BitPrefix is the #1531 L4 regression. The Redis
// backend runs on a `noeviction` instance: unlike the in-memory store (rate_limit.go's
// ipLimiterStore), it has no MaxEntries-equivalent cap, and clientIdentity keeps the full /128
// IPv6 address. An attacker with one routed /64 (a single home/mobile allocation in practice) can
// send from effectively unlimited distinct /128 addresses, each minting its own permanent Redis
// key, until memory fills and every SET starts OOMing -- which trips the breaker and silently
// drops the ENTIRE FLEET to per-machine limiting. Aggregating IPv6 identities to their /64 (as
// Cloudflare's own rate limiter does) bounds key cardinality to one bucket per routed prefix,
// matching how these addresses are actually allocated.
func TestRedisBackend_IPv6IdentitiesAggregateToA64BitPrefix(t *testing.T) {
	fake := redisclient.NewFake("limiter")
	fake.RunScriptResult = []any{int64(1), int64(0), int64(9)}
	b := newTestBackend(fake, ratelimitstore.Options{RegularLimit: 10, Window: time.Minute})

	// Two different /128 addresses within the SAME routed /64.
	if _, err := b.Allow(context.Background(), domain.RateLimitTierRegular, "2001:db8:1234:5678::1"); err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if _, err := b.Allow(context.Background(), domain.RateLimitTierRegular, "2001:db8:1234:5678:ffff:ffff:ffff:ffff"); err != nil {
		t.Fatalf("Allow: %v", err)
	}

	calls := fake.ScriptCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 script calls, got %d", len(calls))
	}
	key0, key1 := calls[0].Keys[0], calls[1].Keys[0]
	if key0 != key1 {
		t.Errorf("two /128 addresses in the same /64 must share one bucket key, got %q and %q", key0, key1)
	}
}

// TestRedisBackend_IPv6IdentitiesInDifferentPrefixesGetDifferentBuckets pins the other half of the
// /64 aggregation: it must not become so coarse that unrelated /64s share a bucket.
func TestRedisBackend_IPv6IdentitiesInDifferentPrefixesGetDifferentBuckets(t *testing.T) {
	fake := redisclient.NewFake("limiter")
	fake.RunScriptResult = []any{int64(1), int64(0), int64(9)}
	b := newTestBackend(fake, ratelimitstore.Options{RegularLimit: 10, Window: time.Minute})

	if _, err := b.Allow(context.Background(), domain.RateLimitTierRegular, "2001:db8:1111::1"); err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if _, err := b.Allow(context.Background(), domain.RateLimitTierRegular, "2001:db8:2222::1"); err != nil {
		t.Fatalf("Allow: %v", err)
	}

	calls := fake.ScriptCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 script calls, got %d", len(calls))
	}
	key0, key1 := calls[0].Keys[0], calls[1].Keys[0]
	if key0 == key1 {
		t.Errorf("two DIFFERENT /64 prefixes must not share a bucket key, both got %q", key0)
	}
}

// TestRedisBackend_IPv4IdentitiesAreNeverAggregated is the other required half of L4: a /64 mask
// must never touch IPv4, including a v4-mapped-in-v6 address (::ffff:a.b.c.d) -- a naive
// "aggregate everything that parses as an IP to /64" implementation would collapse unrelated IPv4
// clients whose first 64 bits of the mapped representation happen to match into one bucket. Two
// different IPv4 addresses (one plain, one v4-mapped-in-v6) must each keep their own, unaggregated
// key.
func TestRedisBackend_IPv4IdentitiesAreNeverAggregated(t *testing.T) {
	fake := redisclient.NewFake("limiter")
	fake.RunScriptResult = []any{int64(1), int64(0), int64(9)}
	b := newTestBackend(fake, ratelimitstore.Options{RegularLimit: 10, Window: time.Minute})

	if _, err := b.Allow(context.Background(), domain.RateLimitTierRegular, "203.0.113.1"); err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if _, err := b.Allow(context.Background(), domain.RateLimitTierRegular, "203.0.113.2"); err != nil {
		t.Fatalf("Allow: %v", err)
	}

	calls := fake.ScriptCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 script calls, got %d", len(calls))
	}

	if calls[0].Keys[0] != "rl:v2:regular:203.0.113.1" {
		t.Errorf("plain IPv4 key = %q, want unaggregated rl:v2:regular:203.0.113.1", calls[0].Keys[0])
	}
	if calls[0].Keys[0] == calls[1].Keys[0] {
		t.Errorf("two different plain IPv4 addresses must not share a bucket key, both got %q", calls[0].Keys[0])
	}
}

// TestRedisBackend_V4MappedInV6IdentitiesAreNeverAggregated is L4's specific carve-out: a naive
// "mask anything that parses as an IP to /64" implementation would take the first 64 bits of a
// v4-mapped-in-v6 address (::ffff:a.b.c.d) — which are the SAME fixed prefix
// (0000:0000:0000:0000:0000:ffff::) for every possible IPv4 address — and collapse every v4-mapped
// client in the world onto one bucket. Two different v4-mapped-in-v6 addresses must therefore
// still get different keys, exactly as their plain-IPv4 counterparts would.
func TestRedisBackend_V4MappedInV6IdentitiesAreNeverAggregated(t *testing.T) {
	fake := redisclient.NewFake("limiter")
	fake.RunScriptResult = []any{int64(1), int64(0), int64(9)}
	b := newTestBackend(fake, ratelimitstore.Options{RegularLimit: 10, Window: time.Minute})

	if _, err := b.Allow(context.Background(), domain.RateLimitTierRegular, "::ffff:203.0.113.1"); err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if _, err := b.Allow(context.Background(), domain.RateLimitTierRegular, "::ffff:203.0.113.2"); err != nil {
		t.Fatalf("Allow: %v", err)
	}

	calls := fake.ScriptCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 script calls, got %d", len(calls))
	}
	if calls[0].Keys[0] == calls[1].Keys[0] {
		t.Errorf("two different v4-mapped-in-v6 addresses must not share a bucket key "+
			"(a naive /64 mask would collapse ALL v4-mapped addresses onto one bucket, since their "+
			"first 64 bits are always identical) — both got %q", calls[0].Keys[0])
	}
}

func TestRedisBackend_Name(t *testing.T) {
	fake := redisclient.NewFake("limiter")
	b := newTestBackend(fake, ratelimitstore.Options{RegularLimit: 10, Window: time.Minute})
	if got := b.Name(); got != "redis" {
		t.Errorf("Name() = %q, want %q", got, "redis")
	}
}

func toInt64(t *testing.T, v any) int64 {
	t.Helper()
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	default:
		t.Fatalf("expected numeric arg, got %T (%v)", v, v)
		return 0
	}
}
