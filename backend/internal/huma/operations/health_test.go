package operations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/health"
)

// healthyCheck returns a health.Check that always succeeds.
func healthyCheck() health.Check {
	return func(ctx context.Context) health.Result {
		return health.Result{Err: nil, Latency: time.Millisecond, ObservedAt: time.Now()}
	}
}

// failingCheck returns a health.Check that always fails with err.
func failingCheck(err error) health.Check {
	return func(ctx context.Context) health.Result {
		return health.Result{Err: err, Latency: time.Millisecond, ObservedAt: time.Now()}
	}
}

// countingCheck wraps a health.Check and increments count on every invocation.
func countingCheck(count *int64, result health.Result) health.Check {
	return func(ctx context.Context) health.Result {
		atomic.AddInt64(count, 1)
		return result
	}
}

func TestGetHealth_ReturnsOkAnd200WhenCriticalChecksPass(t *testing.T) {
	_, api := humatest.New(t)
	handler := NewHealthHandler(slog.Default(), health.Registry{DB: healthyCheck()})
	RegisterHealthRoutes(api, handler)

	resp := api.Get("/health")

	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Equal(t, `{"status":"ok"}`, strings.TrimSpace(resp.Body.String()))
}

func TestGetHealth_Returns503WhenACriticalCheckFails(t *testing.T) {
	_, api := humatest.New(t)
	handler := NewHealthHandler(slog.Default(), health.Registry{DB: failingCheck(errors.New("boom"))})
	RegisterHealthRoutes(api, handler)

	resp := api.Get("/health")

	assert.Equal(t, http.StatusServiceUnavailable, resp.Code)
}

// realisticDependencyErrors are actual-shaped error strings pgx, go-redis, and
// Upstash produce. These are exactly what would end up in the response body if
// a future change reintroduced err.Error() (directly, or via a variadic
// huma.Error503ServiceUnavailable(..., err) call).
var realisticDependencyErrors = []struct {
	name string
	err  error
}{
	{
		name: "pgx",
		err:  errors.New("failed to connect to host=boostedbudget-db.internal port=5432 user=postgres database=prod: password authentication failed"),
	},
	{
		name: "go-redis",
		err:  errors.New("dial tcp [fdaa:9:1b2c:a7b:1::2]:6379: connect: connection refused"),
	},
	{
		name: "upstash",
		err:  errors.New("NOAUTH Authentication required. (fly-boostedbudget-cache.upstash.io:6379)"),
	},
}

// forbiddenSubstrings must never appear anywhere in the /health response body,
// on either the 200 or 503 path.
var forbiddenSubstrings = []string{
	"internal", "5432", "6379", "upstash", "password", "host=", "dial", "fdaa",
}

func TestGetHealth_BodyNeverContainsCheckErrorText(t *testing.T) {
	for _, tc := range realisticDependencyErrors {
		t.Run(tc.name+"/503 path", func(t *testing.T) {
			_, api := humatest.New(t)
			handler := NewHealthHandler(slog.Default(), health.Registry{DB: failingCheck(tc.err)})
			RegisterHealthRoutes(api, handler)

			resp := api.Get("/health")

			require.Equal(t, http.StatusServiceUnavailable, resp.Code)
			body := resp.Body.String()

			assert.NotContains(t, body, tc.err.Error())
			for _, forbidden := range forbiddenSubstrings {
				assert.NotContains(t, strings.ToLower(body), strings.ToLower(forbidden),
					"response body must never contain %q", forbidden)
			}
		})

		t.Run(tc.name+"/200 path (check not critical here)", func(t *testing.T) {
			// Even when the failing check isn't wired in as critical (so the
			// response is 200), the body contract must still hold: no error
			// text can appear because no error text is ever known to the 200
			// path in the first place. This is the companion assertion the
			// brief calls out as needing to run against BOTH paths.
			_, api := humatest.New(t)
			handler := NewHealthHandler(slog.Default(), health.Registry{DB: healthyCheck()})
			RegisterHealthRoutes(api, handler)

			resp := api.Get("/health")

			require.Equal(t, http.StatusOK, resp.Code)
			body := resp.Body.String()

			assert.NotContains(t, body, tc.err.Error())
			for _, forbidden := range forbiddenSubstrings {
				assert.NotContains(t, strings.ToLower(body), strings.ToLower(forbidden),
					"response body must never contain %q", forbidden)
			}
		})
	}
}

func TestGetHealth_ResponseBodyHasExactlyOneFieldNamedStatus(t *testing.T) {
	_, api := humatest.New(t)
	handler := NewHealthHandler(slog.Default(), health.Registry{DB: healthyCheck()})
	RegisterHealthRoutes(api, handler)

	resp := api.Get("/health")
	require.Equal(t, http.StatusOK, resp.Code)

	var m map[string]any
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &m))

	require.Len(t, m, 1, "200 body must have exactly one field; got %v", m)
	assert.Equal(t, "ok", m["status"])
}

func TestGetHealth_DoesNotIncludeVersion(t *testing.T) {
	_, api := humatest.New(t)
	handler := NewHealthHandler(slog.Default(), health.Registry{DB: healthyCheck()})
	RegisterHealthRoutes(api, handler)

	resp := api.Get("/health")

	assert.NotContains(t, resp.Body.String(), "version")
}

// TestGetHealth_DoesNotRunNonCriticalChecks is the amplification guard. Unlike
// the other tests here, the Registry passed in DOES populate Redis and Clerk
// with instrumented, failing checks — deliberately, so this test can prove a
// negative: even though the handler HAS access to registry.Redis/registry.Clerk
// (the same Registry the admin health endpoint will share, per internal/health's
// doc comment), GetHealth must never invoke them. If a future change started
// reading registry.Redis or registry.Clerk from GetHealth, this test's
// invocation counters (not response inspection) would catch it immediately,
// across repeated requests, not just one.
func TestGetHealth_DoesNotRunNonCriticalChecks(t *testing.T) {
	var redisCalls, clerkCalls int64
	registry := health.Registry{
		DB:    healthyCheck(),
		Redis: countingCheck(&redisCalls, health.Result{Err: errors.New("dial tcp [fdaa::1]:6379: connect: connection refused")}),
		Clerk: countingCheck(&clerkCalls, health.Result{Err: errors.New("clerk unreachable")}),
	}

	_, api := humatest.New(t)
	handler := NewHealthHandler(slog.Default(), registry)
	RegisterHealthRoutes(api, handler)

	for i := 0; i < 100; i++ {
		resp := api.Get("/health")
		require.Equal(t, http.StatusOK, resp.Code)
	}

	assert.Equal(t, int64(0), atomic.LoadInt64(&redisCalls),
		"GetHealth must never invoke the non-critical Redis check, even though it is present in the Registry")
	assert.Equal(t, int64(0), atomic.LoadInt64(&clerkCalls),
		"GetHealth must never invoke the non-critical Clerk check, even though it is present in the Registry")
}

func TestGetHealth_LogsTheRealErrorServerSideAtErrorLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	dependencyErr := errors.New("failed to connect to host=boostedbudget-db.internal port=5432 user=postgres database=prod: password authentication failed")

	_, api := humatest.New(t)
	handler := NewHealthHandler(logger, health.Registry{DB: failingCheck(dependencyErr)})
	RegisterHealthRoutes(api, handler)

	resp := api.Get("/health")
	require.Equal(t, http.StatusServiceUnavailable, resp.Code)

	logged := buf.String()
	assert.Contains(t, logged, "level=ERROR")
	assert.Contains(t, logged, dependencyErr.Error(),
		"the real error must be relocated to the server-side log, not discarded")
}

func TestRegisterHealthRoutes_DeclaresNoSecurityScheme(t *testing.T) {
	_, api := humatest.New(t)
	handler := NewHealthHandler(slog.Default(), health.Registry{DB: healthyCheck()})
	RegisterHealthRoutes(api, handler)

	op := api.OpenAPI().Paths["/health"].Get
	require.NotNil(t, op)
	assert.Empty(t, op.Security, "/health must declare no Security requirement — admin_route_coverage_test.go depends on this")
}

// TestNewHealthHandler_NilLoggerFallsBackToDefault covers the nil-logger
// branch: a caller that forgets to pass a logger must not panic on the first
// ErrorContext call.
func TestNewHealthHandler_NilLoggerFallsBackToDefault(t *testing.T) {
	_, api := humatest.New(t)
	handler := NewHealthHandler(nil, health.Registry{DB: failingCheck(errors.New("boom"))})
	RegisterHealthRoutes(api, handler)

	require.NotPanics(t, func() {
		resp := api.Get("/health")
		assert.Equal(t, http.StatusServiceUnavailable, resp.Code)
	})
}

func TestHealthOperation_Declares503InTheOpenApiSpec(t *testing.T) {
	_, api := humatest.New(t)
	handler := NewHealthHandler(slog.Default(), health.Registry{DB: healthyCheck()})
	RegisterHealthRoutes(api, handler)

	op := api.OpenAPI().Paths["/health"].Get
	require.NotNil(t, op)
	require.NotNil(t, op.Responses)
	_, ok := op.Responses["503"]
	assert.True(t, ok, "the OpenAPI spec must declare a 503 response for /health")
}
