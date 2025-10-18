package handlers

import (
	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// MetricsHandler handles Prometheus metrics requests
type MetricsHandler struct{}

// GetMetrics handles GET /metrics
// Returns Prometheus metrics in text format
func (h *MetricsHandler) GetMetrics(c echo.Context) error {
	// Use Prometheus HTTP handler
	handler := promhttp.Handler()
	handler.ServeHTTP(c.Response().Writer, c.Request())
	return nil
}
