package middleware

import (
	"errors"
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

			// Issue #1609 Item 4 (companion fix to logging.go): when the handler
			// RETURNS an *echo.HTTPError instead of writing a response, Echo's
			// HTTPErrorHandler writes the real status only after this middleware
			// chain unwinds — so c.Response().Status still holds the unwritten
			// default. Every propagated error was affected — router 404s, Huma
			// handler errors — and recorded here as status="200", so a Prometheus
			// alert built on http_requests_total's 4xx/5xx rate would never fire.
			//
			// Prefer the error's own code whenever the response has not been
			// committed. c.Response().Committed is the authoritative "did anything
			// get written" signal; a handler that wrote its own status (e.g.
			// CloudflareOnly's c.String(403, ...)) keeps that status verbatim.
			//
			// (The old `if status == 0` fallback that used to live here never ran:
			// Echo's Response.reset sets Status to http.StatusOK before any handler
			// runs, so it is never 0 by the time this executes. Removed rather than
			// kept as dead code.)
			if err != nil && !c.Response().Committed {
				var httpErr *echo.HTTPError
				if errors.As(err, &httpErr) && httpErr.Code != 0 {
					status = httpErr.Code
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
