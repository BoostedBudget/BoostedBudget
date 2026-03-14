package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/stretchr/testify/assert"
)

// newTestLogger creates an slog.Logger backed by a text handler writing to buf.
func newTestLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func TestStructuredLogging(t *testing.T) {
	var logBuffer bytes.Buffer
	logger := newTestLogger(&logBuffer)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "test response")
	}

	mw := StructuredLogging(logger)
	h := mw(handler)

	err := h(c)
	assert.NoError(t, err)

	logOutput := logBuffer.String()
	assert.Contains(t, logOutput, "http_request")
	assert.Contains(t, logOutput, "GET")
	assert.Contains(t, logOutput, "/test")
	assert.Contains(t, logOutput, "200")
	assert.Contains(t, logOutput, "duration_ms")
}

func TestStructuredLogging_WithRequestID(t *testing.T) {
	var logBuffer bytes.Buffer
	logger := newTestLogger(&logBuffer)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	// Store request ID in context.Context via observability
	ctx := observability.WithRequestID(req.Context(), "test-request-123")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "test")
	}

	mw := StructuredLogging(logger)
	h := mw(handler)
	assert.NoError(t, h(c))

	assert.Contains(t, logBuffer.String(), "test-request-123")
}

func TestStructuredLogging_WithUserID(t *testing.T) {
	var logBuffer bytes.Buffer
	logger := newTestLogger(&logBuffer)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	// Store user ID in context.Context via observability
	ctx := observability.WithUserID(req.Context(), "user_test-user")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "test")
	}

	mw := StructuredLogging(logger)
	h := mw(handler)
	assert.NoError(t, h(c))

	assert.Contains(t, logBuffer.String(), "user_test-user")
}

func TestStructuredLogging_WithError(t *testing.T) {
	var logBuffer bytes.Buffer
	logger := newTestLogger(&logBuffer)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := func(c echo.Context) error {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}

	mw := StructuredLogging(logger)
	h := mw(handler)
	assert.Error(t, h(c))

	logOutput := logBuffer.String()
	assert.Contains(t, logOutput, "WARN")
	assert.Contains(t, logOutput, "error")
}

func TestStructuredLogging_DifferentMethods(t *testing.T) {
	methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH"}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			var logBuffer bytes.Buffer
			logger := newTestLogger(&logBuffer)

			e := echo.New()
			req := httptest.NewRequest(method, "/test", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			handler := func(c echo.Context) error {
				return c.String(http.StatusOK, "test")
			}

			mw := StructuredLogging(logger)
			h := mw(handler)
			h(c)

			assert.Contains(t, logBuffer.String(), method)
		})
	}
}

func TestStructuredLogging_QueryParams(t *testing.T) {
	var logBuffer bytes.Buffer
	logger := newTestLogger(&logBuffer)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test?foo=bar&page=1", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "test")
	}

	mw := StructuredLogging(logger)
	h := mw(handler)
	h(c)

	assert.Contains(t, logBuffer.String(), "query")
}

func TestStructuredLogging_UserAgent(t *testing.T) {
	var logBuffer bytes.Buffer
	logger := newTestLogger(&logBuffer)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 Test")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "test")
	}

	mw := StructuredLogging(logger)
	h := mw(handler)
	h(c)

	assert.Contains(t, logBuffer.String(), "Mozilla/5.0 Test")
}

func TestRedactQueryParams_SensitiveParamsAreRedacted(t *testing.T) {
	sensitiveParams := []string{
		"token=abc123",
		"password=secret",
		"key=myapikey",
		"secret=topsecret",
		"authorization=bearer_xyz",
		"access_token=at_123",
		"refresh_token=rt_456",
		"api_key=api_xyz",
		"apikey=apikeyval",
	}

	for _, param := range sensitiveParams {
		t.Run(param, func(t *testing.T) {
			result := redactQueryParams(param)
			assert.Contains(t, result, "[REDACTED]", "sensitive param should be redacted: %s", param)
			// The original value should not appear
			parts := strings.SplitN(param, "=", 2)
			if len(parts) == 2 {
				assert.NotContains(t, result, parts[1], "original value should not appear in output")
			}
		})
	}
}

func TestRedactQueryParams_NonSensitiveParamsPreserved(t *testing.T) {
	query := "page=2&limit=10&sort=asc&filter=active"
	result := redactQueryParams(query)
	assert.Equal(t, query, result, "non-sensitive params should be unchanged")
}

func TestRedactQueryParams_MixedParams(t *testing.T) {
	query := "page=1&token=secret123&limit=10&api_key=mykey&sort=asc"
	result := redactQueryParams(query)

	assert.Contains(t, result, "page=1")
	assert.Contains(t, result, "limit=10")
	assert.Contains(t, result, "sort=asc")
	assert.Contains(t, result, "token=[REDACTED]")
	assert.Contains(t, result, "api_key=[REDACTED]")
	assert.NotContains(t, result, "secret123")
	assert.NotContains(t, result, "mykey")
}

func TestRedactQueryParams_CaseInsensitive(t *testing.T) {
	tests := []struct {
		query string
	}{
		{"TOKEN=abc"},
		{"Password=xyz"},
		{"API_KEY=test"},
		{"ACCESS_TOKEN=bearer"},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			result := redactQueryParams(tt.query)
			assert.Contains(t, result, "[REDACTED]", "case-insensitive match should redact: %s", tt.query)
		})
	}
}

func TestRedactQueryParams_EmptyString(t *testing.T) {
	result := redactQueryParams("")
	assert.Equal(t, "", result)
}

func TestRedactQueryParams_NoQueryParams(t *testing.T) {
	result := redactQueryParams("no-equals-sign")
	assert.Equal(t, "no-equals-sign", result)
}

func TestStructuredLogging_SensitiveQueryParamsRedacted(t *testing.T) {
	var logBuffer bytes.Buffer
	logger := newTestLogger(&logBuffer)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test?token=supersecret&page=1", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "test")
	}

	mw := StructuredLogging(logger)
	h := mw(handler)
	h(c)

	logOutput := logBuffer.String()
	assert.NotContains(t, logOutput, "supersecret", "raw token value must not appear in logs")
	assert.Contains(t, logOutput, "[REDACTED]", "redacted marker must appear in logs")
}

func TestStructuredLogging_RemoteAddr(t *testing.T) {
	var logBuffer bytes.Buffer
	logger := newTestLogger(&logBuffer)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "test")
	}

	mw := StructuredLogging(logger)
	h := mw(handler)
	h(c)

	logOutput := logBuffer.String()
	assert.Contains(t, logOutput, "remote_ip")
	assert.Contains(t, logOutput, "192.168.1.100")
}

func TestStructuredLogging_500StatusIsError(t *testing.T) {
	var logBuffer bytes.Buffer
	logger := newTestLogger(&logBuffer)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := func(c echo.Context) error {
		c.Response().WriteHeader(http.StatusInternalServerError)
		return nil
	}

	mw := StructuredLogging(logger)
	h := mw(handler)
	h(c)

	assert.Contains(t, logBuffer.String(), "ERROR")
}
