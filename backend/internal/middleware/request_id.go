package middleware

import (
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
)

// RequestID is a middleware that generates a unique request ID for each request
// and adds it to both the context and response header.
//
// If the request already has an X-Request-ID header, it will use that value instead.
// This allows for request tracing across multiple services.
//
// The request ID is stored in the Echo context as "request_id" and in context.Context
// via observability.WithRequestID for propagation to services/workers.
//
// Usage:
//
//	e := echo.New()
//	e.Use(RequestID())
func RequestID() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Check if request ID already exists in header
			requestID := c.Request().Header.Get("X-Request-ID")

			// Generate new UUID if not present
			if requestID == "" {
				requestID = uuid.New().String()
			}

			// Store in Echo context for use by handlers and other middleware
			c.Set("request_id", requestID)

			// Add to response header for client tracking
			c.Response().Header().Set("X-Request-ID", requestID)

			// Also store in context.Context for propagation to services/workers
			ctx := observability.WithRequestID(c.Request().Context(), requestID)
			c.SetRequest(c.Request().WithContext(ctx))

			return next(c)
		}
	}
}

// GetRequestID extracts the request ID from the Echo context
// Returns empty string if request ID is not set
func GetRequestID(c echo.Context) string {
	if id, ok := c.Get("request_id").(string); ok {
		return id
	}
	return ""
}
