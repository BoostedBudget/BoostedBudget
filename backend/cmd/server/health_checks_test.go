package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// alwaysHealthyPinger satisfies dbPinger without a real database connection, for tests that need
// createDBCheck to report healthy without exercising Task 7's nil-pool handling itself.
type alwaysHealthyPinger struct{}

func (alwaysHealthyPinger) Ping(ctx context.Context) error { return nil }

// TestCreateDBCheck_ReturnsErrorWhenPoolIsNil pins Issue #1531 Task 7c's nil-handling contract: a
// nil pool is an ERROR, not a healthy no-op. An API with no database is not healthy — this is a
// deliberate behaviour change for a bare local `go run` with no DATABASE_URL, which will now 503
// /health instead of silently reporting "ok".
func TestCreateDBCheck_ReturnsErrorWhenPoolIsNil(t *testing.T) {
	check := createDBCheck(nil)

	result := check(context.Background())

	require.False(t, result.Healthy())
	assert.True(t, errors.Is(result.Err, errDBNotConfigured))
}

// TestCreateRedisCheck_ReturnsNotConfiguredWhenClientIsNil pins the nil-client contract for the
// NON-CRITICAL Redis check.
func TestCreateRedisCheck_ReturnsNotConfiguredWhenClientIsNil(t *testing.T) {
	check := createRedisCheck(nil)

	result := check(context.Background())

	require.False(t, result.Healthy())
	assert.True(t, errors.Is(result.Err, errRedisNotConfigured))
}

// TestCreateClerkCheck_ReturnsNotConfiguredWhenSecretKeyIsEmpty pins the nil-secret contract for
// the non-critical Clerk check.
func TestCreateClerkCheck_ReturnsNotConfiguredWhenSecretKeyIsEmpty(t *testing.T) {
	check := createClerkCheck("", nil)

	result := check(context.Background())

	require.False(t, result.Healthy())
	assert.True(t, errors.Is(result.Err, errClerkNotConfigured))
}

// TestCreateClerkCheck_DoesNotCallClerkMoreThanOncePerTTL is the acceptance criterion for
// memoizing Clerk specifically: it is a rate-limited third-party call, and this check's whole
// point is to avoid hammering it. An httptest server counts hits; two calls to the returned Check
// within the TTL window must dispatch exactly one HTTP request.
func TestCreateClerkCheck_DoesNotCallClerkMoreThanOncePerTTL(t *testing.T) {
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		assert.Equal(t, "Bearer test-secret", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	check := createClerkCheckWithURL("test-secret", ts.Client(), ts.URL)

	first := check(context.Background())
	second := check(context.Background())

	require.True(t, first.Healthy())
	require.True(t, second.Healthy())
	assert.Equal(t, int32(1), hits.Load(), "Clerk must not be probed more than once per TTL window")
}

// TestHealthChecks_DbAndRedisUseATTLBetweenFiveAndTenSeconds is the direct acceptance criterion
// from Issue #1531 Task 7c's table: DB and Redis both memoize for "5s" — pinned here as a bound
// (>=5s, <=10s) rather than an exact literal, so a deliberate future tightening within that
// documented range doesn't spuriously fail this test.
func TestHealthChecks_DbAndRedisUseATTLBetweenFiveAndTenSeconds(t *testing.T) {
	for _, tc := range []struct {
		name string
		ttl  time.Duration
	}{
		{"db", dbCheckTTL},
		{"redis", redisCheckTTL},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.GreaterOrEqual(t, tc.ttl, 5*time.Second)
			assert.LessOrEqual(t, tc.ttl, 10*time.Second)
		})
	}
}

// TestCreateRedisCheck_ProbesTheRealClient exercises the non-nil-client path against
// redisclient.Fake, so the "just returns not-configured" path above isn't the only one covered.
func TestCreateRedisCheck_ProbesTheRealClient(t *testing.T) {
	fake := redisclient.NewFake("limiter")
	check := createRedisCheck(fake)

	result := check(context.Background())
	assert.True(t, result.Healthy())
	assert.Equal(t, 1, fake.PingCalls())

	fake2 := redisclient.NewFake("limiter")
	fake2.PingErr = errors.New("dial tcp 10.1.2.3:6379: connect: connection refused")
	check2 := createRedisCheck(fake2)
	result2 := check2(context.Background())
	assert.False(t, result2.Healthy())
}
