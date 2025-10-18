package middleware

import (
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// HTTP request metrics
	httpRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "path", "status"},
	)

	httpRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path", "status"},
	)

	httpRequestsInFlight = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "http_requests_in_flight",
			Help: "Current number of HTTP requests being served",
		},
	)
)

// MetricsMiddleware creates middleware that records Prometheus metrics for HTTP requests
//
// Metrics recorded:
// - http_requests_total: Counter of total requests (labeled by method, path, status)
// - http_request_duration_seconds: Histogram of request durations
// - http_requests_in_flight: Gauge of current in-flight requests
//
// Usage:
//
//	e := echo.New()
//	e.Use(MetricsMiddleware())
func MetricsMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Increment in-flight requests
			httpRequestsInFlight.Inc()
			defer httpRequestsInFlight.Dec()

			start := time.Now()
			req := c.Request()
			path := c.Path() // Use route path, not actual path with params

			// Execute handler
			err := next(c)

			// Calculate duration
			duration := time.Since(start).Seconds()

			// Get status code
			status := c.Response().Status
			if status == 0 {
				// If status not set, determine from error
				if err != nil {
					if he, ok := err.(*echo.HTTPError); ok {
						status = he.Code
					} else {
						status = 500
					}
				} else {
					status = 200
				}
			}

			// Record metrics
			labels := prometheus.Labels{
				"method": req.Method,
				"path":   path,
				"status": strconv.Itoa(status),
			}

			httpRequestsTotal.With(labels).Inc()
			httpRequestDuration.With(labels).Observe(duration)

			return err
		}
	}
}
