// Package operations provides Huma operation handlers for the BoostedBudget API.
package operations

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// HealthInput represents the input for the health check endpoint.
// This endpoint requires no parameters.
type HealthInput struct{}

// HealthOutput represents the health check response.
type HealthOutput struct {
	Body HealthResponse
}

// HealthResponse represents the health check response body.
type HealthResponse struct {
	Status    string       `json:"status" doc:"Overall system health status: healthy, degraded, or unhealthy"`
	Version   string       `json:"version" doc:"API version"`
	Timestamp string       `json:"timestamp" doc:"ISO 8601 timestamp of the health check"`
	Checks    HealthChecks `json:"checks" doc:"Individual service health checks"`
}

// HealthChecks represents individual service health checks.
type HealthChecks struct {
	Database *ServiceCheck `json:"database" doc:"Database connection status"`
	Redis    *ServiceCheck `json:"redis" doc:"Redis cache status"`
	Auth0    *ServiceCheck `json:"auth0" doc:"Auth0 authentication service status"`
}

// ServiceCheck represents the health status of a single service.
type ServiceCheck struct {
	Status         string `json:"status" doc:"Service status: up or down"`
	Message        string `json:"message,omitempty" doc:"Error message if service is down"`
	ResponseTimeMs int64  `json:"response_time_ms" doc:"Response time in milliseconds"`
}

// HealthError is a custom error that carries the health response for 503 cases.
type HealthError struct {
	StatusCode int
	Response   HealthResponse
}

// Error implements the error interface.
func (e *HealthError) Error() string {
	return "system unhealthy"
}

// GetStatus returns the HTTP status code.
func (e *HealthError) GetStatus() int {
	return e.StatusCode
}

// HealthHandler handles health check requests.
type HealthHandler struct {
	DBCheck    func() error
	RedisCheck func() error
	Auth0Check func() error
	Version    string
}

// NewHealthHandler creates a new health handler with the given dependencies.
func NewHealthHandler(dbCheck, redisCheck, auth0Check func() error, version string) *HealthHandler {
	return &HealthHandler{
		DBCheck:    dbCheck,
		RedisCheck: redisCheck,
		Auth0Check: auth0Check,
		Version:    version,
	}
}

// GetHealth handles GET /health requests.
func (h *HealthHandler) GetHealth(ctx context.Context, input *HealthInput) (*HealthOutput, error) {
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
		return nil, huma.Error503ServiceUnavailable("System unhealthy: database connection failed")
	}

	// Redis and Auth0 are non-critical - if down, system is degraded
	if redisCheck.Status == "down" || auth0Check.Status == "down" {
		response.Status = "degraded"
	} else {
		response.Status = "healthy"
	}

	return &HealthOutput{Body: response}, nil
}

// performCheck executes a health check function and returns the ServiceCheck result.
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

// RegisterHealthRoutes registers health check routes with the Huma API.
func RegisterHealthRoutes(api huma.API, handler *HealthHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "getHealth",
		Method:      http.MethodGet,
		Path:        "/health",
		Summary:     "Health check",
		Description: "Returns the health status of the API and its dependencies (database, Redis, Auth0).",
		Tags:        []string{"Health"},
	}, handler.GetHealth)
}
