package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupSecurityHeadersEcho(env string) (*echo.Echo, *httptest.ResponseRecorder) {
	e := echo.New()
	e.Use(middleware.SecurityHeaders(env))
	e.GET("/test", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"ok": "true"})
	})
	rr := httptest.NewRecorder()
	return e, rr
}

func TestSecurityHeaders_AllRequiredHeaders(t *testing.T) {
	e, rr := setupSecurityHeadersEcho("development")
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	e.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)

	headers := rr.Header()
	assert.Equal(t, "nosniff", headers.Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", headers.Get("X-Frame-Options"))
	assert.Equal(t, "0", headers.Get("X-XSS-Protection"))
	assert.Equal(t, "strict-origin-when-cross-origin", headers.Get("Referrer-Policy"))
	assert.Contains(t, headers.Get("Content-Security-Policy"), "frame-ancestors 'none'")
	assert.Contains(t, headers.Get("Permissions-Policy"), "geolocation=()")
	assert.Contains(t, headers.Get("Permissions-Policy"), "microphone=()")
	assert.Contains(t, headers.Get("Permissions-Policy"), "camera=()")
}

func TestSecurityHeaders_HSTSOnlyInProduction(t *testing.T) {
	tests := []struct {
		env      string
		wantHSTS bool
	}{
		{"production", true},
		{"staging", true},
		{"development", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.env, func(t *testing.T) {
			e, rr := setupSecurityHeadersEcho(tt.env)
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			e.ServeHTTP(rr, req)

			hsts := rr.Header().Get("Strict-Transport-Security")
			if tt.wantHSTS {
				assert.Contains(t, hsts, "max-age=")
				assert.Contains(t, hsts, "includeSubDomains")
			} else {
				assert.Empty(t, hsts, "HSTS should not be set in %s", tt.env)
			}
		})
	}
}

func TestSecurityHeaders_CacheControlNoStore(t *testing.T) {
	e := echo.New()
	e.Use(middleware.SecurityHeaders("development"))
	e.GET("/authenticated", func(c echo.Context) error {
		// Simulate an authenticated endpoint by setting the user_id context value
		c.Set("user_id", "some-user-id")
		return c.JSON(http.StatusOK, map[string]string{"data": "sensitive"})
	})
	e.GET("/public", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"data": "public"})
	})

	t.Run("authenticated response has no-store", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/authenticated", nil)
		e.ServeHTTP(rr, req)
		assert.Equal(t, "no-store", rr.Header().Get("Cache-Control"))
	})

	t.Run("public response has no-store too", func(t *testing.T) {
		// All API responses should have no-store for a financial app
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/public", nil)
		e.ServeHTTP(rr, req)
		assert.Equal(t, "no-store", rr.Header().Get("Cache-Control"))
	})
}

func TestSecurityHeaders_CSPDoesNotBreakJSON(t *testing.T) {
	e, rr := setupSecurityHeadersEcho("production")
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	e.ServeHTTP(rr, req)

	// JSON response should still work with CSP set
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Header().Get("Content-Type"), "application/json")
	// CSP should be set
	csp := rr.Header().Get("Content-Security-Policy")
	assert.NotEmpty(t, csp)
	assert.Contains(t, csp, "default-src 'none'")
}

func TestSecurityHeaders_XContentTypeOptions(t *testing.T) {
	e, rr := setupSecurityHeadersEcho("production")
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	e.ServeHTTP(rr, req)
	assert.Equal(t, "nosniff", rr.Header().Get("X-Content-Type-Options"),
		"must prevent MIME-type sniffing")
}

func TestSecurityHeaders_XFrameOptions(t *testing.T) {
	e, rr := setupSecurityHeadersEcho("production")
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	e.ServeHTTP(rr, req)
	assert.Equal(t, "DENY", rr.Header().Get("X-Frame-Options"),
		"must prevent clickjacking via iframes")
}
