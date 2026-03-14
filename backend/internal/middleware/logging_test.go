package middleware

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

func TestStructuredLogging(t *testing.T) {
	// Create a buffer to capture log output
	var logBuffer bytes.Buffer

	// Create Echo instance
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Create test handler
	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "test response")
	}

	// Apply structured logging middleware
	middleware := StructuredLogging(&logBuffer)
	h := middleware(handler)

	// Execute request
	err := h(c)
	assert.NoError(t, err)

	// Parse logged JSON
	var logEntry map[string]interface{}
	err = json.Unmarshal(logBuffer.Bytes(), &logEntry)
	assert.NoError(t, err, "Log output should be valid JSON")

	// Verify log fields
	assert.Equal(t, "http_request", logEntry["type"])
	assert.Equal(t, "GET", logEntry["method"])
	assert.Equal(t, "/test", logEntry["path"])
	assert.Equal(t, float64(200), logEntry["status"])
	assert.Contains(t, logEntry, "duration_ms")
	assert.Contains(t, logEntry, "timestamp")
	// Note: request_id is only present if RequestID middleware is used
	// This test doesn't use it, so we don't check for it here
}

func TestStructuredLogging_WithRequestID(t *testing.T) {
	var logBuffer bytes.Buffer

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("request_id", "test-request-123")

	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "test")
	}

	middleware := StructuredLogging(&logBuffer)
	h := middleware(handler)
	assert.NoError(t, h(c))

	var logEntry map[string]interface{}
	json.Unmarshal(logBuffer.Bytes(), &logEntry)
	assert.Equal(t, "test-request-123", logEntry["request_id"])
}

func TestStructuredLogging_WithUserID(t *testing.T) {
	var logBuffer bytes.Buffer

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user_test-user")

	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "test")
	}

	middleware := StructuredLogging(&logBuffer)
	h := middleware(handler)
	assert.NoError(t, h(c))

	var logEntry map[string]interface{}
	json.Unmarshal(logBuffer.Bytes(), &logEntry)
	assert.Equal(t, "user_test-user", logEntry["user_id"])
}

func TestStructuredLogging_WithError(t *testing.T) {
	var logBuffer bytes.Buffer

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := func(c echo.Context) error {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}

	middleware := StructuredLogging(&logBuffer)
	h := middleware(handler)
	assert.Error(t, h(c))

	var logEntry map[string]interface{}
	json.Unmarshal(logBuffer.Bytes(), &logEntry)
	assert.Equal(t, "error", logEntry["level"])
	assert.Contains(t, logEntry, "error")
}

func TestStructuredLogging_DifferentMethods(t *testing.T) {
	methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH"}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			var logBuffer bytes.Buffer

			e := echo.New()
			req := httptest.NewRequest(method, "/test", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			handler := func(c echo.Context) error {
				return c.String(http.StatusOK, "test")
			}

			middleware := StructuredLogging(&logBuffer)
			h := middleware(handler)
			h(c)

			var logEntry map[string]interface{}
			json.Unmarshal(logBuffer.Bytes(), &logEntry)
			assert.Equal(t, method, logEntry["method"])
		})
	}
}

func TestLogInfo(t *testing.T) {
	var logBuffer bytes.Buffer
	logger := NewStructuredLogger(&logBuffer)

	logger.Info("test message", map[string]interface{}{
		"key1": "value1",
		"key2": 123,
	})

	var logEntry map[string]interface{}
	err := json.Unmarshal(logBuffer.Bytes(), &logEntry)
	assert.NoError(t, err)

	assert.Equal(t, "info", logEntry["level"])
	assert.Equal(t, "test message", logEntry["message"])
	assert.Equal(t, "value1", logEntry["key1"])
	assert.Equal(t, float64(123), logEntry["key2"])
	assert.Contains(t, logEntry, "timestamp")
}

func TestLogError(t *testing.T) {
	var logBuffer bytes.Buffer
	logger := NewStructuredLogger(&logBuffer)

	logger.Error("error occurred", map[string]interface{}{
		"error_code": "ERR_TEST",
	})

	var logEntry map[string]interface{}
	err := json.Unmarshal(logBuffer.Bytes(), &logEntry)
	assert.NoError(t, err)

	assert.Equal(t, "error", logEntry["level"])
	assert.Equal(t, "error occurred", logEntry["message"])
	assert.Equal(t, "ERR_TEST", logEntry["error_code"])
}

func TestLogWarn(t *testing.T) {
	var logBuffer bytes.Buffer
	logger := NewStructuredLogger(&logBuffer)

	logger.Warn("warning message", nil)

	var logEntry map[string]interface{}
	err := json.Unmarshal(logBuffer.Bytes(), &logEntry)
	assert.NoError(t, err)

	assert.Equal(t, "warn", logEntry["level"])
	assert.Equal(t, "warning message", logEntry["message"])
}

func TestLogDebug(t *testing.T) {
	var logBuffer bytes.Buffer
	logger := NewStructuredLogger(&logBuffer)

	logger.Debug("debug message", map[string]interface{}{
		"details": "some details",
	})

	var logEntry map[string]interface{}
	err := json.Unmarshal(logBuffer.Bytes(), &logEntry)
	assert.NoError(t, err)

	assert.Equal(t, "debug", logEntry["level"])
	assert.Equal(t, "debug message", logEntry["message"])
	assert.Equal(t, "some details", logEntry["details"])
}

func TestStructuredLogging_QueryParams(t *testing.T) {
	var logBuffer bytes.Buffer

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test?foo=bar&page=1", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "test")
	}

	middleware := StructuredLogging(&logBuffer)
	h := middleware(handler)
	h(c)

	assert.Contains(t, logBuffer.String(), "query")
}

func TestStructuredLogging_UserAgent(t *testing.T) {
	var logBuffer bytes.Buffer

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 Test")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "test")
	}

	middleware := StructuredLogging(&logBuffer)
	h := middleware(handler)
	h(c)

	var logEntry map[string]interface{}
	json.Unmarshal(logBuffer.Bytes(), &logEntry)
	assert.Equal(t, "Mozilla/5.0 Test", logEntry["user_agent"])
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

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test?token=supersecret&page=1", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "test")
	}

	mw := StructuredLogging(&logBuffer)
	h := mw(handler)
	h(c)

	logOutput := logBuffer.String()
	assert.NotContains(t, logOutput, "supersecret", "raw token value must not appear in logs")
	assert.Contains(t, logOutput, "[REDACTED]", "redacted marker must appear in logs")
}

func TestStructuredLogging_RemoteAddr(t *testing.T) {
	var logBuffer bytes.Buffer

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "test")
	}

	middleware := StructuredLogging(&logBuffer)
	h := middleware(handler)
	h(c)

	var logEntry map[string]interface{}
	json.Unmarshal(logBuffer.Bytes(), &logEntry)

	assert.Contains(t, logEntry, "remote_ip")
	assert.True(t, strings.Contains(logEntry["remote_ip"].(string), "192.168.1.100"))
}
