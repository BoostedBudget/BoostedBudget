package middleware

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPanicRecovery_RecoversPanic(t *testing.T) {
	var logBuffer bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuffer, &slog.HandlerOptions{Level: slog.LevelDebug}))

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := func(c echo.Context) error {
		panic("test panic")
	}

	mw := PanicRecovery(logger, "development")
	h := mw(handler)

	err := h(c)
	assert.NoError(t, err, "PanicRecovery should handle the panic and not return an error")

	assert.Equal(t, http.StatusInternalServerError, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, float64(http.StatusInternalServerError), body["status"])
	assert.Equal(t, "Internal Server Error", body["title"])
	assert.Equal(t, "An unexpected error occurred", body["detail"])
}

func TestPanicRecovery_IncludesPanicInNonProduction(t *testing.T) {
	var logBuffer bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuffer, nil))

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := func(c echo.Context) error {
		panic("secret panic detail")
	}

	mw := PanicRecovery(logger, "development")
	h := mw(handler)
	h(c)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "secret panic detail", body["panic"])
}

func TestPanicRecovery_HidesPanicInProduction(t *testing.T) {
	var logBuffer bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuffer, nil))

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := func(c echo.Context) error {
		panic("secret panic detail")
	}

	mw := PanicRecovery(logger, "production")
	h := mw(handler)
	h(c)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.NotContains(t, body, "panic", "panic detail must be hidden in production")
	assert.NotContains(t, rec.Body.String(), "secret panic detail")
}

func TestPanicRecovery_LogsPanicDetails(t *testing.T) {
	var logBuffer bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuffer, &slog.HandlerOptions{Level: slog.LevelDebug}))

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := func(c echo.Context) error {
		panic("logged panic message")
	}

	mw := PanicRecovery(logger, "development")
	h := mw(handler)
	h(c)

	logOutput := logBuffer.String()
	assert.Contains(t, logOutput, "panic recovered")
	assert.Contains(t, logOutput, "logged panic message")
}

func TestPanicRecovery_NoPanic_PassesThrough(t *testing.T) {
	var logBuffer bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuffer, nil))

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "all good")
	}

	mw := PanicRecovery(logger, "production")
	h := mw(handler)

	err := h(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, logBuffer.String(), "no log output expected when there is no panic")
}

func TestPanicRecovery_RFC9457_Shape(t *testing.T) {
	var logBuffer bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuffer, nil))

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := func(c echo.Context) error {
		panic("any panic")
	}

	mw := PanicRecovery(logger, "development")
	h := mw(handler)
	h(c)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

	// Verify RFC 9457 required fields are present
	assert.Contains(t, body, "status")
	assert.Contains(t, body, "title")
	assert.Contains(t, body, "detail")
}
