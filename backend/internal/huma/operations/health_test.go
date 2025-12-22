package operations

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHealthHandler_AllServicesHealthy tests when all services are healthy.
func TestHealthHandler_AllServicesHealthy(t *testing.T) {
	// Create test API
	_, api := humatest.New(t)

	// Create handler with all healthy checks
	handler := NewHealthHandler(
		func() error { return nil }, // DB healthy
		func() error { return nil }, // Redis healthy
		func() error { return nil }, // Auth0 healthy
		"0.1.0",
	)

	// Register routes
	RegisterHealthRoutes(api, handler)

	// Execute request
	resp := api.Get("/health")

	// Assert status code
	assert.Equal(t, http.StatusOK, resp.Code)

	// Parse response body
	var response HealthResponse
	err := json.Unmarshal(resp.Body.Bytes(), &response)
	require.NoError(t, err)

	// Verify response structure
	assert.Equal(t, "healthy", response.Status)
	assert.Equal(t, "0.1.0", response.Version)
	assert.NotEmpty(t, response.Timestamp)

	// Verify individual service checks
	require.NotNil(t, response.Checks.Database)
	assert.Equal(t, "up", response.Checks.Database.Status)
	assert.GreaterOrEqual(t, response.Checks.Database.ResponseTimeMs, int64(0))

	require.NotNil(t, response.Checks.Redis)
	assert.Equal(t, "up", response.Checks.Redis.Status)
	assert.GreaterOrEqual(t, response.Checks.Redis.ResponseTimeMs, int64(0))

	require.NotNil(t, response.Checks.Auth0)
	assert.Equal(t, "up", response.Checks.Auth0.Status)
	assert.GreaterOrEqual(t, response.Checks.Auth0.ResponseTimeMs, int64(0))
}

// TestHealthHandler_DatabaseDown tests when database is down (critical service).
func TestHealthHandler_DatabaseDown(t *testing.T) {
	// Create test API
	_, api := humatest.New(t)

	// Create handler with DB failure
	handler := NewHealthHandler(
		func() error { return errors.New("connection refused") }, // DB down
		func() error { return nil },                              // Redis healthy
		func() error { return nil },                              // Auth0 healthy
		"0.1.0",
	)

	// Register routes
	RegisterHealthRoutes(api, handler)

	// Execute request
	resp := api.Get("/health")

	// Assert 503 status for database failure
	assert.Equal(t, http.StatusServiceUnavailable, resp.Code)
}

// TestHealthHandler_RedisDown tests when Redis is down (non-critical).
func TestHealthHandler_RedisDown(t *testing.T) {
	// Create test API
	_, api := humatest.New(t)

	// Create handler with Redis failure
	handler := NewHealthHandler(
		func() error { return nil },                                // DB healthy
		func() error { return errors.New("connection timeout") },   // Redis down
		func() error { return nil },                                // Auth0 healthy
		"0.1.0",
	)

	// Register routes
	RegisterHealthRoutes(api, handler)

	// Execute request
	resp := api.Get("/health")

	// Assert 200 status (Redis is non-critical)
	assert.Equal(t, http.StatusOK, resp.Code)

	// Parse response body
	var response HealthResponse
	err := json.Unmarshal(resp.Body.Bytes(), &response)
	require.NoError(t, err)

	// Verify degraded status
	assert.Equal(t, "degraded", response.Status)
	assert.Equal(t, "up", response.Checks.Database.Status)
	assert.Equal(t, "down", response.Checks.Redis.Status)
	assert.Equal(t, "connection timeout", response.Checks.Redis.Message)
	assert.Equal(t, "up", response.Checks.Auth0.Status)
}

// TestHealthHandler_Auth0Down tests when Auth0 is down (non-critical).
func TestHealthHandler_Auth0Down(t *testing.T) {
	// Create test API
	_, api := humatest.New(t)

	// Create handler with Auth0 failure
	handler := NewHealthHandler(
		func() error { return nil },                     // DB healthy
		func() error { return nil },                     // Redis healthy
		func() error { return errors.New("timeout") },   // Auth0 down
		"0.1.0",
	)

	// Register routes
	RegisterHealthRoutes(api, handler)

	// Execute request
	resp := api.Get("/health")

	// Assert 200 status (Auth0 is non-critical)
	assert.Equal(t, http.StatusOK, resp.Code)

	// Parse response body
	var response HealthResponse
	err := json.Unmarshal(resp.Body.Bytes(), &response)
	require.NoError(t, err)

	// Verify degraded status
	assert.Equal(t, "degraded", response.Status)
	assert.Equal(t, "up", response.Checks.Database.Status)
	assert.Equal(t, "up", response.Checks.Redis.Status)
	assert.Equal(t, "down", response.Checks.Auth0.Status)
	assert.Equal(t, "timeout", response.Checks.Auth0.Message)
}

// TestHealthHandler_MultipleServicesDown tests when multiple non-critical services are down.
func TestHealthHandler_MultipleServicesDown(t *testing.T) {
	// Create test API
	_, api := humatest.New(t)

	// Create handler with multiple failures
	handler := NewHealthHandler(
		func() error { return nil },                        // DB healthy
		func() error { return errors.New("redis error") },  // Redis down
		func() error { return errors.New("auth0 error") },  // Auth0 down
		"0.1.0",
	)

	// Register routes
	RegisterHealthRoutes(api, handler)

	// Execute request
	resp := api.Get("/health")

	// Assert 200 status (DB is up, so system is degraded)
	assert.Equal(t, http.StatusOK, resp.Code)

	// Parse response body
	var response HealthResponse
	err := json.Unmarshal(resp.Body.Bytes(), &response)
	require.NoError(t, err)

	// Verify degraded status
	assert.Equal(t, "degraded", response.Status)
	assert.Equal(t, "up", response.Checks.Database.Status)
	assert.Equal(t, "down", response.Checks.Redis.Status)
	assert.Equal(t, "down", response.Checks.Auth0.Status)
}

// TestHealthHandler_AllServicesDown tests when all services are down.
func TestHealthHandler_AllServicesDown(t *testing.T) {
	// Create test API
	_, api := humatest.New(t)

	// Create handler with all failures
	handler := NewHealthHandler(
		func() error { return errors.New("database error") }, // DB down
		func() error { return errors.New("redis error") },    // Redis down
		func() error { return errors.New("auth0 error") },    // Auth0 down
		"0.1.0",
	)

	// Register routes
	RegisterHealthRoutes(api, handler)

	// Execute request
	resp := api.Get("/health")

	// Assert 503 status (DB is critical)
	assert.Equal(t, http.StatusServiceUnavailable, resp.Code)
}
