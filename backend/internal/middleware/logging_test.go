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

	// Set request ID in context
	c.Set("request_id", "test-request-123")

	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "test")
	}

	middleware := StructuredLogging(&logBuffer)
	h := middleware(handler)

	err := h(c)
	assert.NoError(t, err)

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

	// Set user ID in context (from auth middleware)
	c.Set("user_id", "user_test-user")

	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "test")
	}

	middleware := StructuredLogging(&logBuffer)
	h := middleware(handler)

	err := h(c)
	assert.NoError(t, err)

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

	// Handler that returns an error
	handler := func(c echo.Context) error {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}

	middleware := StructuredLogging(&logBuffer)
	h := middleware(handler)

	err := h(c)
	assert.Error(t, err)

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

	logOutput := logBuffer.String()
	// Query params should be logged
	assert.Contains(t, logOutput, "query")
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

	// Should extract IP from RemoteAddr
	assert.Contains(t, logEntry, "remote_ip")
	assert.True(t, strings.Contains(logEntry["remote_ip"].(string), "192.168.1.100"))
}
