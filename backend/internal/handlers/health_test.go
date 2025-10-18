package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

// TestHealthHandler_AllServicesHealthy tests when all services are healthy
func TestHealthHandler_AllServicesHealthy(t *testing.T) {
	// Setup
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Mock dependencies - all healthy
	handler := &HealthHandler{
		DBCheck:      func() error { return nil },
		RedisCheck:   func() error { return nil },
		Auth0Check:   func() error { return nil },
		Version:      "0.1.0",
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

	// Verify individual service checks
	assert.NotNil(t, response.Checks.Database)
	assert.Equal(t, "up", response.Checks.Database.Status)
	assert.GreaterOrEqual(t, response.Checks.Database.ResponseTimeMs, int64(0))

	assert.NotNil(t, response.Checks.Redis)
	assert.Equal(t, "up", response.Checks.Redis.Status)
	assert.GreaterOrEqual(t, response.Checks.Redis.ResponseTimeMs, int64(0))

	assert.NotNil(t, response.Checks.Auth0)
	assert.Equal(t, "up", response.Checks.Auth0.Status)
	assert.GreaterOrEqual(t, response.Checks.Auth0.ResponseTimeMs, int64(0))
}

// TestHealthHandler_DatabaseDown tests when database is down (critical service)
func TestHealthHandler_DatabaseDown(t *testing.T) {
	// Setup
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Mock dependencies with DB failure
	handler := &HealthHandler{
		DBCheck:      func() error { return errors.New("connection refused") },
		RedisCheck:   func() error { return nil },
		Auth0Check:   func() error { return nil },
		Version:      "0.1.0",
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
	assert.Equal(t, "down", response.Checks.Database.Status)
	assert.Equal(t, "connection refused", response.Checks.Database.Message)
	assert.Equal(t, "up", response.Checks.Redis.Status)
	assert.Equal(t, "up", response.Checks.Auth0.Status)
}

// TestHealthHandler_RedisDown tests when Redis is down (non-critical)
func TestHealthHandler_RedisDown(t *testing.T) {
	// Setup
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Mock dependencies with Redis failure
	handler := &HealthHandler{
		DBCheck:      func() error { return nil },
		RedisCheck:   func() error { return errors.New("connection timeout") },
		Auth0Check:   func() error { return nil },
		Version:      "0.1.0",
	}

	// Execute
	err := handler.GetHealth(c)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code) // Redis is non-critical

	// Parse response
	var response HealthResponse
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	assert.NoError(t, err)

	// Verify response structure
	assert.Equal(t, "degraded", response.Status)
	assert.Equal(t, "up", response.Checks.Database.Status)
	assert.Equal(t, "down", response.Checks.Redis.Status)
	assert.Equal(t, "connection timeout", response.Checks.Redis.Message)
	assert.Equal(t, "up", response.Checks.Auth0.Status)
}

// TestHealthHandler_Auth0Down tests when Auth0 is down (non-critical)
func TestHealthHandler_Auth0Down(t *testing.T) {
	// Setup
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Mock dependencies with Auth0 failure
	handler := &HealthHandler{
		DBCheck:      func() error { return nil },
		RedisCheck:   func() error { return nil },
		Auth0Check:   func() error { return errors.New("timeout") },
		Version:      "0.1.0",
	}

	// Execute
	err := handler.GetHealth(c)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code) // Auth0 is non-critical

	// Parse response
	var response HealthResponse
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	assert.NoError(t, err)

	// Verify response structure
	assert.Equal(t, "degraded", response.Status)
	assert.Equal(t, "up", response.Checks.Database.Status)
	assert.Equal(t, "up", response.Checks.Redis.Status)
	assert.Equal(t, "down", response.Checks.Auth0.Status)
	assert.Equal(t, "timeout", response.Checks.Auth0.Message)
}

// TestHealthHandler_MultipleServicesDown tests when multiple non-critical services are down
func TestHealthHandler_MultipleServicesDown(t *testing.T) {
	// Setup
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Mock dependencies with multiple failures
	handler := &HealthHandler{
		DBCheck:      func() error { return nil },
		RedisCheck:   func() error { return errors.New("redis error") },
		Auth0Check:   func() error { return errors.New("auth0 error") },
		Version:      "0.1.0",
	}

	// Execute
	err := handler.GetHealth(c)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code) // DB is up, so system is degraded

	// Parse response
	var response HealthResponse
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	assert.NoError(t, err)

	// Verify response structure
	assert.Equal(t, "degraded", response.Status)
	assert.Equal(t, "up", response.Checks.Database.Status)
	assert.Equal(t, "down", response.Checks.Redis.Status)
	assert.Equal(t, "down", response.Checks.Auth0.Status)
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
		DBCheck:      func() error { return errors.New("database error") },
		RedisCheck:   func() error { return errors.New("redis error") },
		Auth0Check:   func() error { return errors.New("auth0 error") },
		Version:      "0.1.0",
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
	assert.Equal(t, "down", response.Checks.Database.Status)
	assert.Equal(t, "down", response.Checks.Redis.Status)
	assert.Equal(t, "down", response.Checks.Auth0.Status)
}
