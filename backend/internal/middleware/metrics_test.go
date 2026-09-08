package middleware

// Issue #1609 Item 4 (companion fix): MetricsMiddleware has the identical defect that
// logging.go was fixed for. It reads `status := c.Response().Status` and only falls
// back to the error's code when status == 0 — but Echo initialises Response.Status to
// http.StatusOK (200) when it creates the response, so it is NEVER 0 by the time this
// runs. The fallback branch is dead code, and every propagated *echo.HTTPError (router
// 404s, Huma handler errors) is recorded in http_requests_total /
// http_request_duration_seconds with status="200" instead of its real code.
//
// Impact: a Prometheus alert on 4xx/5xx rate built on these metrics will not fire.

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

// TestMetricsMiddleware_PropagatedHTTPErrorRecordsRealStatus is the core regression
// test. A handler that RETURNS an *echo.HTTPError (rather than writing a response)
// must have its metrics recorded with that error's code, not the unwritten 200
// default.
func TestMetricsMiddleware_PropagatedHTTPErrorRecordsRealStatus(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/test")
	// e.NewContext alone leaves Response.Status at its zero value; the real request
	// path (Echo's pooled context via c.Reset -> Response.reset) sets it to
	// http.StatusOK before any handler runs. Mirror that here so this test actually
	// exercises the "status is always 200, never 0" fact the fix depends on.
	c.Response().Status = http.StatusOK

	// Return the error WITHOUT writing a response — this is what Huma handlers and
	// Echo's router-level 404 both do. Echo's HTTPErrorHandler writes the status later,
	// outside this middleware.
	handler := func(c echo.Context) error {
		return echo.NewHTTPError(http.StatusNotFound, "Not Found")
	}

	h := MetricsMiddleware()(handler)
	err := h(c)
	require.Error(t, err, "middleware must propagate the handler error unchanged")

	got := testutil.ToFloat64(httpRequestsTotal.WithLabelValues(http.MethodGet, "/test", "404"))
	require.Equal(t, float64(1), got,
		"a propagated *echo.HTTPError must be counted under its real status, not the "+
			"unwritten default 200")

	gotStale := testutil.ToFloat64(httpRequestsTotal.WithLabelValues(http.MethodGet, "/test", "200"))
	require.Equal(t, float64(0), gotStale,
		"the stale unwritten default (200) must not have been incremented for this request")
}

// TestMetricsMiddleware_WrittenResponseStatusUnchanged pins the case that already
// worked, so the fix cannot regress it: when the handler WRITES the response itself,
// c.Response().Status is authoritative and must still be used.
func TestMetricsMiddleware_WrittenResponseStatusUnchanged(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/cf-only", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/cf-only")

	// CloudflareOnly rejects this way — c.String writes the status immediately.
	handler := func(c echo.Context) error { return c.String(http.StatusForbidden, "Forbidden") }

	h := MetricsMiddleware()(handler)
	require.NoError(t, h(c))

	got := testutil.ToFloat64(httpRequestsTotal.WithLabelValues(http.MethodGet, "/cf-only", "403"))
	require.Equal(t, float64(1), got,
		"a handler-written status must still be recorded verbatim")
}

// TestMetricsMiddleware_NonHTTPErrorStillRecordsWrittenStatus covers a plain
// (non-echo) error: there is no code to read from it, so the written status remains
// the best available value and must not be clobbered.
func TestMetricsMiddleware_NonHTTPErrorStillRecordsWrittenStatus(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/teapot", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/teapot")
	// See the comment in TestMetricsMiddleware_PropagatedHTTPErrorRecordsRealStatus:
	// mirror Echo's real per-request initial state (Status == 200, not the
	// e.NewContext zero value) so this test exercises the actual bug.
	c.Response().Status = http.StatusOK

	handler := func(c echo.Context) error { return echo.ErrTeapot }

	h := MetricsMiddleware()(handler)
	require.Error(t, h(c))

	got := testutil.ToFloat64(httpRequestsTotal.WithLabelValues(http.MethodGet, "/teapot", "418"))
	require.Equal(t, float64(1), got,
		"echo.ErrTeapot is an *echo.HTTPError and must be recorded under its own code")
}
