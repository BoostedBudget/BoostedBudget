package middleware

import (
	"github.com/labstack/echo/v4"
)

// SecurityHeaders returns an Echo middleware that sets security-hardening HTTP response headers
// on every response. The environment parameter controls whether HSTS is included
// (production and staging only — never set on localhost/development).
func SecurityHeaders(environment string) echo.MiddlewareFunc {
	hstsEnabled := environment == "production" || environment == "staging"

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			h := c.Response().Header()

			// Prevent MIME-type sniffing attacks
			h.Set("X-Content-Type-Options", "nosniff")

			// Prevent clickjacking via iframes
			h.Set("X-Frame-Options", "DENY")

			// Disable legacy XSS filter (causes more problems than it solves in modern browsers)
			h.Set("X-XSS-Protection", "0")

			// Control how much referrer info is sent with requests
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")

			// Restrict resource loading; deny framing by any origin
			h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")

			// Disable access to sensitive browser APIs
			h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")

			// Never cache API responses (financial data)
			h.Set("Cache-Control", "no-store")

			// HSTS: only for production/staging (do NOT set on localhost — breaks local dev)
			if hstsEnabled {
				h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}

			return next(c)
		}
	}
}
