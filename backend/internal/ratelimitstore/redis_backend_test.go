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
	wantRegular := "rl:v1:regular:1.2.3.4"
	wantAuth := "rl:v1:auth:1.2.3.4"
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
	want := "rl:v1:sentinel"
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
