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
	Database bool `json:"database"`
	Redis    bool `json:"redis"`
}

// GetHealth handles GET /health
func (h *HealthHandler) GetHealth(c echo.Context) error {
	response := HealthResponse{
		Version:   h.Version,
		Timestamp: time.Now().Format(time.RFC3339),
		Checks: HealthChecks{
			Database: h.DBCheck() == nil,
			Redis:    h.RedisCheck() == nil,
		},
	}

	// Determine overall status
	if !response.Checks.Database {
		response.Status = "unhealthy"
		return c.JSON(http.StatusServiceUnavailable, response)
	}

	if !response.Checks.Redis {
		response.Status = "degraded"
		return c.JSON(http.StatusOK, response)
	}

	response.Status = "healthy"
	return c.JSON(http.StatusOK, response)
}
