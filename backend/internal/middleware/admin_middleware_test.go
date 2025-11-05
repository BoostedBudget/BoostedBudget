package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/stretchr/testify/assert"
)

// TestAdminMiddleware_StagingEnvironment_AllowsAccess verifies that admin middleware
// allows requests to proceed when running in staging environment.
func TestAdminMiddleware_StagingEnvironment_AllowsAccess(t *testing.T) {
	// Arrange
	cfg := &config.Config{
		Environment: "staging",
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Create a handler that should be reached
	handlerCalled := false
	handler := func(c echo.Context) error {
		handlerCalled = true
		return c.String(http.StatusOK, "success")
	}

	// Wrap with AdminMiddleware
	middlewareFunc := AdminMiddleware(cfg)
	h := middlewareFunc(handler)

	// Act
	err := h(c)

	// Assert
	assert.NoError(t, err)
	assert.True(t, handlerCalled, "Handler should be called when environment is staging")
	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestAdminMiddleware_ProductionEnvironment_Returns404 verifies that admin middleware
// returns 404 Not Found when running in production environment (security through obscurity).
func TestAdminMiddleware_ProductionEnvironment_Returns404(t *testing.T) {
	// Arrange
	cfg := &config.Config{
		Environment: "production",
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Create a handler that should NOT be reached
	handlerCalled := false
	handler := func(c echo.Context) error {
		handlerCalled = true
		return c.String(http.StatusOK, "success")
	}

	// Wrap with AdminMiddleware
	middlewareFunc := AdminMiddleware(cfg)
	h := middlewareFunc(handler)

	// Act
	err := h(c)

	// Assert
	assert.Error(t, err)
	assert.False(t, handlerCalled, "Handler should NOT be called when environment is production")

	// Check that error is 404 Not Found
	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok, "Error should be an HTTPError")
	assert.Equal(t, http.StatusNotFound, httpErr.Code)
	assert.Equal(t, "Endpoint not available", httpErr.Message)
}

// TestAdminMiddleware_DevelopmentEnvironment_Returns404 verifies that admin middleware
// returns 404 Not Found when running in development environment (staging-only endpoint).
func TestAdminMiddleware_DevelopmentEnvironment_Returns404(t *testing.T) {
	// Arrange
	cfg := &config.Config{
		Environment: "development",
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Create a handler that should NOT be reached
	handlerCalled := false
	handler := func(c echo.Context) error {
		handlerCalled = true
		return c.String(http.StatusOK, "success")
	}

	// Wrap with AdminMiddleware
	middlewareFunc := AdminMiddleware(cfg)
	h := middlewareFunc(handler)

	// Act
	err := h(c)

	// Assert
	assert.Error(t, err)
	assert.False(t, handlerCalled, "Handler should NOT be called when environment is development")

	// Check that error is 404 Not Found
	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok, "Error should be an HTTPError")
	assert.Equal(t, http.StatusNotFound, httpErr.Code)
	assert.Equal(t, "Endpoint not available", httpErr.Message)
}
