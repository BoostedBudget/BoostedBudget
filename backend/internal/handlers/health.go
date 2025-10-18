package handlers

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

// HealthHandler handles health check requests
type HealthHandler struct {
	DBCheck    func() error
	RedisCheck func() error
	Auth0Check func() error
	Version    string
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status    string       `json:"status"`
	Version   string       `json:"version"`
	Timestamp string       `json:"timestamp"`
	Checks    HealthChecks `json:"checks"`
}

// HealthChecks represents individual service health checks
type HealthChecks struct {
	Database *ServiceCheck `json:"database"`
	Redis    *ServiceCheck `json:"redis"`
	Auth0    *ServiceCheck `json:"auth0"`
}

// ServiceCheck represents the health status of a single service
type ServiceCheck struct {
	Status         string `json:"status"`
	Message        string `json:"message,omitempty"`
	ResponseTimeMs int64  `json:"response_time_ms"`
}

// GetHealth handles GET /health
func (h *HealthHandler) GetHealth(c echo.Context) error {
	// Check database
	dbCheck := h.performCheck(h.DBCheck)

	// Check Redis
	redisCheck := h.performCheck(h.RedisCheck)

	// Check Auth0
	auth0Check := h.performCheck(h.Auth0Check)

	response := HealthResponse{
		Version:   h.Version,
		Timestamp: time.Now().Format(time.RFC3339),
		Checks: HealthChecks{
			Database: dbCheck,
			Redis:    redisCheck,
			Auth0:    auth0Check,
		},
	}

	// Determine overall status
	// Database is critical - if down, system is unhealthy
	if dbCheck.Status == "down" {
		response.Status = "unhealthy"
		return c.JSON(http.StatusServiceUnavailable, response)
	}

	// Redis and Auth0 are non-critical - if down, system is degraded
	if redisCheck.Status == "down" || auth0Check.Status == "down" {
		response.Status = "degraded"
		return c.JSON(http.StatusOK, response)
	}

	response.Status = "healthy"
	return c.JSON(http.StatusOK, response)
}

// performCheck executes a health check function and returns the ServiceCheck result
func (h *HealthHandler) performCheck(checkFunc func() error) *ServiceCheck {
	start := time.Now()
	err := checkFunc()
	responseTime := time.Since(start).Milliseconds()

	if err != nil {
		return &ServiceCheck{
			Status:         "down",
			Message:        err.Error(),
			ResponseTimeMs: responseTime,
		}
	}

	return &ServiceCheck{
		Status:         "up",
		ResponseTimeMs: responseTime,
	}
}
