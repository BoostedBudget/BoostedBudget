package middleware

// Issue #1609 Item 4: an error response must be logged with its REAL status code.
//
// Found in production logs:
//
//	{"level":"WARN","path":"/","status":200,"error":"code=404, message=Not Found"}
//
// status 200 and a 404 error in the same record. StructuredLogging captures
// `status := c.Response().Status` immediately after next(c), but for an error that
// propagates as *echo.HTTPError, Echo's error handler writes the real status only
// AFTER the middleware chain unwinds — so the field keeps the unwritten default 200.
//
// The existing code already knows about this case: the comment at logging.go reads
// "Error returned but status not yet written (e.g. Echo error handler not run)" and
// uses it to escalate the LOG LEVEL. It just never applied the same knowledge to the
// status FIELD. Level right, number wrong.
//
// Impact: any dashboard, alert or log query counting by `status` silently undercounts
// 4xx/5xx, because every error that propagates rather than being written by the
// handler is recorded as 200.

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

// TestStructuredLogging_PropagatedHTTPErrorLogsRealStatus is the core regression test.
// A handler that RETURNS an *echo.HTTPError (rather than writing a response) must be
// logged with that error's code.
func TestStructuredLogging_PropagatedHTTPErrorLogsRealStatus(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus string
	}{
		{"404 not found", echo.NewHTTPError(http.StatusNotFound, "Not Found"), "status=404"},
		{"429 rate limited", echo.NewHTTPError(http.StatusTooManyRequests, "rate limit exceeded"), "status=429"},
		{"500 internal", echo.NewHTTPError(http.StatusInternalServerError, "boom"), "status=500"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var logBuffer bytes.Buffer
			logger := newTestLogger(&logBuffer)

			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			// Return the error WITHOUT writing a response — this is what Huma handlers,
			// IPRateLimiter and Echo's router-level 404 all do. Echo's HTTPErrorHandler
			// writes the status later, outside this middleware.
			handler := func(c echo.Context) error { return tc.err }

			h := StructuredLogging(logger)(handler)
			err := h(c)
			require.Error(t, err, "middleware must propagate the handler error unchanged")

			logOutput := logBuffer.String()
			require.Contains(t, logOutput, tc.wantStatus,
				"a propagated *echo.HTTPError must be logged with its real status, not the "+
					"unwritten default 200. Any dashboard counting by status undercounts "+
					"4xx/5xx otherwise. Got: %s", logOutput)
			require.NotContains(t, logOutput, "status=200",
				"the stale unwritten default must not appear. Got: %s", logOutput)
		})
	}
}

// TestStructuredLogging_WrittenResponseStatusUnchanged pins the case that already
// worked, so the fix cannot regress it: when the handler WRITES the response itself,
// c.Response().Status is authoritative and must still be used.
func TestStructuredLogging_WrittenResponseStatusUnchanged(t *testing.T) {
	var logBuffer bytes.Buffer
	logger := newTestLogger(&logBuffer)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// CloudflareOnly rejects this way — c.String writes the status immediately.
	handler := func(c echo.Context) error { return c.String(http.StatusForbidden, "Forbidden") }

	h := StructuredLogging(logger)(handler)
	require.NoError(t, h(c))

	require.Contains(t, logBuffer.String(), "status=403",
		"a handler-written status must still be logged verbatim: %s", logBuffer.String())
}

// TestStructuredLogging_NonHTTPErrorStillLogsWrittenStatus covers a plain (non-echo)
// error: there is no code to read from it, so the written status remains the best
// available value and must not be clobbered.
func TestStructuredLogging_NonHTTPErrorStillLogsWrittenStatus(t *testing.T) {
	var logBuffer bytes.Buffer
	logger := newTestLogger(&logBuffer)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := func(c echo.Context) error { return echo.ErrTeapot }

	h := StructuredLogging(logger)(handler)
	require.Error(t, h(c))

	require.Contains(t, logBuffer.String(), "status=418",
		"echo.ErrTeapot is an *echo.HTTPError and must log its own code: %s", logBuffer.String())
}
