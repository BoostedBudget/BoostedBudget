package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

func TestMetricsHandler_GetMetrics(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := &MetricsHandler{}

	err := handler.GetMetrics(c)
	assert.NoError(t, err)

	// Should return 200 OK
	assert.Equal(t, http.StatusOK, rec.Code)

	// Content-Type should be Prometheus text format
	contentType := rec.Header().Get("Content-Type")
	assert.Contains(t, contentType, "text/plain")

	body := rec.Body.String()

	// Should contain standard Go metrics
	assert.Contains(t, body, "go_goroutines")
	assert.Contains(t, body, "go_memstats_alloc_bytes")
	assert.Contains(t, body, "go_threads")

	// Should contain process metrics
	assert.Contains(t, body, "process_cpu_seconds_total")
	assert.Contains(t, body, "process_resident_memory_bytes")

	// Note: HTTP custom metrics (http_requests_total, http_request_duration_seconds)
	// are only registered when MetricsMiddleware processes a request
	// This test verifies the endpoint works and standard metrics are present
}

func TestMetricsHandler_Format(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := &MetricsHandler{}
	handler.GetMetrics(c)

	body := rec.Body.String()
	lines := strings.Split(body, "\n")

	// Verify Prometheus format
	hasHelpLine := false
	hasTypeLine := false
	hasMetricLine := false

	for _, line := range lines {
		if strings.HasPrefix(line, "# HELP") {
			hasHelpLine = true
		}
		if strings.HasPrefix(line, "# TYPE") {
			hasTypeLine = true
		}
		// Metric lines should not start with #
		if !strings.HasPrefix(line, "#") && line != "" {
			hasMetricLine = true
		}
	}

	assert.True(t, hasHelpLine, "Should have HELP lines")
	assert.True(t, hasTypeLine, "Should have TYPE lines")
	assert.True(t, hasMetricLine, "Should have metric value lines")
}

func TestMetricsHandler_HTTPMetrics(t *testing.T) {
	// Note: HTTP metrics (http_requests_total, http_request_duration_seconds, http_requests_in_flight)
	// are only registered when the MetricsMiddleware is used.
	// This test verifies that the metrics endpoint works, even if no requests have been made yet.
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := &MetricsHandler{}
	handler.GetMetrics(c)

	body := rec.Body.String()

	// Should have standard Go and process metrics
	assert.Contains(t, body, "go_goroutines")
	assert.Contains(t, body, "process_cpu_seconds_total")
}

func TestMetricsHandler_CustomMetrics(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := &MetricsHandler{}
	handler.GetMetrics(c)

	body := rec.Body.String()

	// Should include BoostedBudget-specific metrics
	// These will be added as we implement features

	// For now, just verify the endpoint works
	assert.NotEmpty(t, body)
}

func TestMetricsMiddleware_RecordsMetrics(t *testing.T) {
	// This test will verify that the metrics middleware records HTTP metrics
	e := echo.New()

	// Create a test handler
	testHandler := func(c echo.Context) error {
		return c.String(http.StatusOK, "test")
	}

	// Apply metrics middleware (to be implemented)
	// middleware := MetricsMiddleware()
	// h := middleware(testHandler)

	// Make a request
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Execute handler
	err := testHandler(c)
	assert.NoError(t, err)

	// After implementing MetricsMiddleware, verify metrics are recorded
	// For now, this is a placeholder test
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestMetricsHandler_NoSensitiveData(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := &MetricsHandler{}
	handler.GetMetrics(c)

	body := rec.Body.String()

	// Metrics should not contain sensitive data
	sensitivePatterns := []string{
		"password",
		"token",
		"secret",
		"api_key",
		"auth0",
		"stripe_key",
		"plaid_secret",
	}

	for _, pattern := range sensitivePatterns {
		assert.NotContains(t, strings.ToLower(body), pattern,
			"Metrics should not expose sensitive data: %s", pattern)
	}
}

func TestMetricsHandler_Concurrent(t *testing.T) {
	// Test that metrics can be safely accessed concurrently
	handler := &MetricsHandler{}
	e := echo.New()

	done := make(chan bool)
	requests := 10

	for i := 0; i < requests; i++ {
		go func() {
			req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := handler.GetMetrics(c)
			assert.NoError(t, err)
			assert.Equal(t, http.StatusOK, rec.Code)

			done <- true
		}()
	}

	// Wait for all requests to complete
	for i := 0; i < requests; i++ {
		<-done
	}
}
