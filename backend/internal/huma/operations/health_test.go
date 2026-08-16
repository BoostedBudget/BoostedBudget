package operations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	humaconfig "github.com/maxnussbaum/BoostedBudget/internal/huma"
	"github.com/maxnussbaum/BoostedBudget/internal/health"
)

// newProductionAPI builds a Huma API the same way cmd/server/main.go:313 does:
// echo + humaecho.NewV4 + humaconfig.NewAPIConfig. Unlike humatest.New(t),
// this installs huma.DefaultConfig's SchemaLinkTransformer, so tests built on
// this harness observe exactly what production emits on the wire -- including
// any `$schema` field the transformer injects. See #1531 L1: three prior
// "exactly one field" tests all passed under humatest.New's bare config while
// production (built via this exact path) emitted a second field.
func newProductionAPI() (*echo.Echo, huma.API) {
	e := echo.New()
	api := humaecho.NewV4(e, humaconfig.NewAPIConfig("test", "test"))
	return e, api
}

// prodGet issues a GET against the production-config echo router built by
// newProductionAPI, bypassing humatest's non-production config entirely.
func prodGet(e *echo.Echo, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

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
			// Built under newProductionAPI(), not humatest.New(t): this is the marquee leak
			// regression test for criterion 23, and a bare humatest config cannot observe
			// anything the real production pipeline (SchemaLinkTransformer et al.) adds to the
			// 503 body -- the same L1 gap already closed for the 200 path, applied here too. See
			// TestGetHealth_503BodyShapeUnderProductionConfig below for the shape (not just
			// content) this now exposes.
			e, api := newProductionAPI()
			handler := NewHealthHandler(slog.Default(), health.Registry{DB: failingCheck(tc.err)})
			RegisterHealthRoutes(api, handler)

			resp := prodGet(e, "/health")

			require.Equal(t, http.StatusServiceUnavailable, resp.Code)
			body := resp.Body.String()

			assert.NotContains(t, body, tc.err.Error())
			for _, forbidden := range forbiddenSubstrings {
				assert.NotContains(t, strings.ToLower(body), strings.ToLower(forbidden),
					"response body must never contain %q", forbidden)
			}
		})

		t.Run(tc.name+"/200 path (non-critical check failing)", func(t *testing.T) {
			// DB (the only CRITICAL check GetHealth reads) is healthy, so this
			// still resolves to 200. Redis is wired in as a NON-CRITICAL check
			// that is genuinely FAILING with tc.err -- a real, known error
			// string is present in the registry this handler holds. The
			// assertion below is therefore a real one: it proves that error
			// text never leaks into the public body, guarding against a future
			// change that starts serializing a component breakdown (including
			// non-critical ones) into the 200 response.
			_, api := humatest.New(t)
			handler := NewHealthHandler(slog.Default(), health.Registry{DB: healthyCheck(), Redis: failingCheck(tc.err)})
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

// TestGetHealth_ResponseBodyHasExactlyOneFieldNamedStatus_ProductionConfig is
// the #1531 L1 regression: the humatest.New(t)-based test above uses Huma's
// bare test config, which has no transformers, so it cannot observe
// huma.DefaultConfig's SchemaLinkTransformer -- the thing that actually runs
// in production (via humaconfig.NewAPIConfig, built the same way
// cmd/server/main.go:313 builds it). This test exercises that exact
// production config, so a `$schema` field leaking back into the OpenAPI
// registry for HealthStatusResponse fails it immediately.
func TestGetHealth_ResponseBodyHasExactlyOneFieldNamedStatus_ProductionConfig(t *testing.T) {
	e, api := newProductionAPI()
	handler := NewHealthHandler(slog.Default(), health.Registry{DB: healthyCheck()})
	RegisterHealthRoutes(api, handler)

	resp := prodGet(e, "/health")
	require.Equal(t, http.StatusOK, resp.Code)

	var m map[string]any
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &m))

	require.Len(t, m, 1, "production 200 body must have exactly one field; got %v", m)
	assert.Equal(t, "ok", m["status"])
	assert.Empty(t, resp.Header().Get("Link"),
		"production response must not carry a describedBy Link header either")
}

// TestGetHealth_503BodyShapeUnderProductionConfig pins the 503 body's field SHAPE, not just its
// content, under the real production Huma config (#1531 L1's gap, applied to the 503 path: see
// TestGetHealth_BodyNeverContainsCheckErrorText's "/503 path" subtest, which checks content but
// previously ran under humatest.New(t)'s bare config).
//
// ⚠ FOUND BY THIS SLICE, NOT FIXED HERE (scope): unlike the 200 body (HealthStatusResponse, which
// has its own Schema() override specifically to defeat SchemaLinkTransformer -- see that type's
// doc comment), huma.Error503ServiceUnavailable returns Huma's generic, un-overridden ErrorModel.
// Under the real production config this DOES pick up a `$schema` field and a `Link:
// .../schemas/ErrorModel.json; rel="describedBy"` header -- confirmed by running this exact test
// against the current code before writing the assertions below. That is a real gap in the same
// class L1 closed on the 200 path (a frameworkfingerprinting field on an unauthenticated,
// internet-reachable endpoint), but it is NOT specific to /health: huma.Error503ServiceUnavailable
// (and the sibling Error4xx/5xx helpers) are used by every erroring operation in this API, so
// fixing it here would mean either overriding Huma's global ErrorModel schema (a blast radius far
// outside this slice's scope) or giving /health its own bespoke error body type (a bigger, separate
// change). This test therefore pins the CURRENT shape as a known, tracked gap rather than silently
// asserting it away — see the field list below and file a follow-up issue for the fix.
func TestGetHealth_503BodyShapeUnderProductionConfig(t *testing.T) {
	e, api := newProductionAPI()
	handler := NewHealthHandler(slog.Default(), health.Registry{DB: failingCheck(errors.New("boom"))})
	RegisterHealthRoutes(api, handler)

	resp := prodGet(e, "/health")
	require.Equal(t, http.StatusServiceUnavailable, resp.Code)

	var m map[string]any
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &m))

	gotFields := make([]string, 0, len(m))
	for k := range m {
		gotFields = append(gotFields, k)
	}
	assert.ElementsMatch(t, []string{"title", "status", "detail", "$schema"}, gotFields,
		"503 body's field shape has changed under the real production config -- if this now has "+
			"FEWER fields (no more $schema), TestGetHealth_503BodyShapeUnderProductionConfig's "+
			"known-gap comment above is stale and should be updated/removed; if it has MORE "+
			"fields, that is a new leak and must not be accepted silently. got %v", m)
	assert.Equal(t, "Service Unavailable", m["title"])
	assert.Equal(t, float64(http.StatusServiceUnavailable), m["status"])
	assert.Equal(t, "service unavailable", m["detail"],
		"the underlying dependency error text must never reach this field -- only the fixed, "+
			"generic string GetHealth passes to huma.Error503ServiceUnavailable")
	assert.NotContains(t, m["detail"], "boom", "the injected dependency error text leaked into detail")

	link := resp.Header().Get("Link")
	assert.NotEmpty(t, link,
		"expected the known describedBy Link header (see this test's doc comment); if this is "+
			"now empty, the known gap has been fixed and the comment above should be updated")
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
