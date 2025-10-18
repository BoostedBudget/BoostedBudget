package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

// TestHealthHandler_AllServicesHealthy tests the health endpoint when all services are healthy
func TestHealthHandler_AllServicesHealthy(t *testing.T) {
	// Setup
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Mock dependencies (will be injected in production)
	handler := &HealthHandler{
		DBCheck:    func() error { return nil },    // DB is healthy
		RedisCheck: func() error { return nil },    // Redis is healthy
		Version:    "0.1.0",
	}

	// Execute
	err := handler.GetHealth(c)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Parse response
	var response HealthResponse
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	assert.NoError(t, err)

	// Verify response structure
	assert.Equal(t, "healthy", response.Status)
	assert.Equal(t, "0.1.0", response.Version)
	assert.NotEmpty(t, response.Timestamp)
	assert.True(t, response.Checks.Database)
	assert.True(t, response.Checks.Redis)
}

// TestHealthHandler_DatabaseDown tests the health endpoint when database is down
func TestHealthHandler_DatabaseDown(t *testing.T) {
	// Setup
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Mock dependencies with DB failure
	handler := &HealthHandler{
		DBCheck:    func() error { return assert.AnError }, // DB is down
		RedisCheck: func() error { return nil },             // Redis is healthy
		Version:    "0.1.0",
	}

	// Execute
	err := handler.GetHealth(c)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

	// Parse response
	var response HealthResponse
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	assert.NoError(t, err)

	// Verify response structure
	assert.Equal(t, "unhealthy", response.Status)
	assert.False(t, response.Checks.Database)
	assert.True(t, response.Checks.Redis)
}

// TestHealthHandler_RedisDown tests the health endpoint when Redis is down
func TestHealthHandler_RedisDown(t *testing.T) {
	// Setup
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Mock dependencies with Redis failure
	handler := &HealthHandler{
		DBCheck:    func() error { return nil },             // DB is healthy
		RedisCheck: func() error { return assert.AnError }, // Redis is down
		Version:    "0.1.0",
	}

	// Execute
	err := handler.GetHealth(c)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code) // Redis is non-critical, still return 200

	// Parse response
	var response HealthResponse
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	assert.NoError(t, err)

	// Verify response structure
	assert.Equal(t, "degraded", response.Status)
	assert.True(t, response.Checks.Database)
	assert.False(t, response.Checks.Redis)
}

// TestHealthHandler_AllServicesDown tests when all services are down
func TestHealthHandler_AllServicesDown(t *testing.T) {
	// Setup
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Mock dependencies with all failures
	handler := &HealthHandler{
		DBCheck:    func() error { return assert.AnError },
		RedisCheck: func() error { return assert.AnError },
		Version:    "0.1.0",
	}

	// Execute
	err := handler.GetHealth(c)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

	// Parse response
	var response HealthResponse
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	assert.NoError(t, err)

	// Verify response structure
	assert.Equal(t, "unhealthy", response.Status)
	assert.False(t, response.Checks.Database)
	assert.False(t, response.Checks.Redis)
}
