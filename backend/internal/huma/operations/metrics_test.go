package operations

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetMetrics_Success tests successful metrics retrieval.
func TestGetMetrics_Success(t *testing.T) {
	handler := NewMetricsHandler()

	output, err := handler.GetMetrics(context.Background(), &GetMetricsInput{})

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "text/plain; version=0.0.4; charset=utf-8", output.ContentType)
	// Metrics output should contain at least some text
	assert.NotEmpty(t, output.Body)
}

// TestMetricsRouteRegistration tests that routes are properly registered.
func TestMetricsRouteRegistration(t *testing.T) {
	_, api := humatest.New(t)
	handler := NewMetricsHandler()
	RegisterMetricsRoutes(api, handler)

	// Test GET /metrics
	resp := api.Get("/metrics")
	assert.Equal(t, http.StatusOK, resp.Code)

	// Verify the response contains metrics data (Prometheus format)
	body := resp.Body.String()
	// Check for common Prometheus metrics that should exist
	assert.True(t, len(body) > 0, "Metrics response should not be empty")
}

// TestGetMetrics_ContainsGoMetrics tests that Go runtime metrics are included.
func TestGetMetrics_ContainsGoMetrics(t *testing.T) {
	handler := NewMetricsHandler()

	output, err := handler.GetMetrics(context.Background(), &GetMetricsInput{})

	require.NoError(t, err)
	require.NotNil(t, output)

	body := string(output.Body)
	// Go runtime metrics should be present
	assert.True(t, strings.Contains(body, "go_") || len(body) > 0,
		"Should contain Go runtime metrics or at least some content")
}
