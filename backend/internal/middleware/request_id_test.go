package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

func TestRequestID_GeneratesID(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := func(c echo.Context) error {
		// Request ID should be set in context
		requestID := c.Get("request_id")
		assert.NotNil(t, requestID)
		assert.NotEmpty(t, requestID.(string))
		return c.String(http.StatusOK, "test")
	}

	middleware := RequestID()
	h := middleware(handler)

	err := h(c)
	assert.NoError(t, err)
}

func TestRequestID_AddsHeader(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "test")
	}

	middleware := RequestID()
	h := middleware(handler)

	err := h(c)
	assert.NoError(t, err)

	// Response should include X-Request-ID header
	requestID := rec.Header().Get("X-Request-ID")
	assert.NotEmpty(t, requestID)
}

func TestRequestID_UsesExistingHeader(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	// Set existing request ID in request header
	existingID := "existing-request-123"
	req.Header.Set("X-Request-ID", existingID)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	var capturedID string
	handler := func(c echo.Context) error {
		capturedID = c.Get("request_id").(string)
		return c.String(http.StatusOK, "test")
	}

	middleware := RequestID()
	h := middleware(handler)

	err := h(c)
	assert.NoError(t, err)

	// Should use existing request ID
	assert.Equal(t, existingID, capturedID)
	assert.Equal(t, existingID, rec.Header().Get("X-Request-ID"))
}

func TestRequestID_UniqueIDs(t *testing.T) {
	// Generate multiple request IDs and verify they're unique
	e := echo.New()
	requestIDs := make(map[string]bool)

	for i := 0; i < 100; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		handler := func(c echo.Context) error {
			requestID := c.Get("request_id").(string)
			requestIDs[requestID] = true
			return c.String(http.StatusOK, "test")
		}

		middleware := RequestID()
		h := middleware(handler)
		h(c)
	}

	// All request IDs should be unique
	assert.Equal(t, 100, len(requestIDs))
}

func TestRequestID_Format(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	var capturedID string
	handler := func(c echo.Context) error {
		capturedID = c.Get("request_id").(string)
		return c.String(http.StatusOK, "test")
	}

	middleware := RequestID()
	h := middleware(handler)

	err := h(c)
	assert.NoError(t, err)

	// Request ID should have expected format (UUID v4)
	// Format: xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx
	assert.Regexp(t, `^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`, capturedID)
}

func TestGetRequestID(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Set request ID in context
	expectedID := "test-id-123"
	c.Set("request_id", expectedID)

	// Helper function should retrieve it
	actualID := GetRequestID(c)
	assert.Equal(t, expectedID, actualID)
}

func TestGetRequestID_NotSet(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Without setting request ID
	actualID := GetRequestID(c)
	// Should return empty string if not set
	assert.Equal(t, "", actualID)
}
