package middleware

import (
	"log/slog"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
)

// sensitiveQueryParams lists URL query parameter names whose values must be redacted from logs.
var sensitiveQueryParams = map[string]struct{}{
	"token":         {},
	"password":      {},
	"key":           {},
	"secret":        {},
	"authorization": {},
	"access_token":  {},
	"refresh_token": {},
	"api_key":       {},
	"apikey":        {},
}

// redactQueryParams replaces values of sensitive query parameters with [REDACTED].
// The comparison is case-insensitive. Non-sensitive parameters are returned unchanged.
// Builds the result string manually to avoid URL-encoding the [REDACTED] marker.
func redactQueryParams(rawQuery string) string {
	if rawQuery == "" {
		return rawQuery
	}

	pairs := strings.Split(rawQuery, "&")
	hasRedaction := false

	for i, pair := range pairs {
		k, _, found := strings.Cut(pair, "=")
		if !found {
			continue
		}
		if _, sensitive := sensitiveQueryParams[strings.ToLower(k)]; sensitive {
			pairs[i] = k + "=[REDACTED]"
			hasRedaction = true
		}
	}

	if !hasRedaction {
		return rawQuery
	}
	return strings.Join(pairs, "&")
}

// StructuredLogging creates an Echo middleware that logs HTTP requests using slog.
//
// The middleware logs the following fields:
// - type: "http_request"
// - method: HTTP method (GET, POST, etc.)
// - path: Request path
// - status: HTTP status code
// - duration_ms: Request duration in milliseconds
// - request_id: Unique request identifier (from context, set by RequestID middleware)
// - user_id: Authenticated user ID (from context, set by Clerk middleware)
// - remote_ip: Client IP address
// - user_agent: Client user agent
// - query: Query parameters (if present, sensitive values redacted)
// - error: Error message (if request failed)
//
// Usage:
//
//	e := echo.New()
//	e.Use(RequestID())  // Should be first to generate request IDs
//	e.Use(StructuredLogging(logger))
func StructuredLogging(logger *slog.Logger) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()
			req := c.Request()
			ctx := req.Context()

			err := next(c)

			duration := time.Since(start)
			status := c.Response().Status
			level := slog.LevelInfo
			if status >= 500 {
				level = slog.LevelError
			} else if status >= 400 {
				level = slog.LevelWarn
			} else if err != nil {
				// Error returned but status not yet written (e.g. Echo error handler not run)
				if httpErr, ok := err.(*echo.HTTPError); ok && httpErr.Code >= 500 {
					level = slog.LevelError
				} else if err != nil {
					level = slog.LevelWarn
				}
			}

			attrs := []slog.Attr{
				slog.String("type", "http_request"),
				slog.String("method", req.Method),
				slog.String("path", req.URL.Path),
				slog.Int("status", status),
				slog.Int64("duration_ms", duration.Milliseconds()),
			}

			if requestID := observability.RequestIDFromContext(ctx); requestID != "" {
				attrs = append(attrs, slog.String("request_id", requestID))
			}
			if userID := observability.UserIDFromContext(ctx); userID != "" {
				attrs = append(attrs, slog.String("user_id", userID))
			}
			if remoteAddr := req.RemoteAddr; remoteAddr != "" {
				if idx := strings.LastIndex(remoteAddr, ":"); idx != -1 {
					attrs = append(attrs, slog.String("remote_ip", remoteAddr[:idx]))
				}
			}
			if ua := req.UserAgent(); ua != "" {
				attrs = append(attrs, slog.String("user_agent", ua))
			}
			if q := req.URL.RawQuery; q != "" {
				attrs = append(attrs, slog.String("query", redactQueryParams(q)))
			}
			if err != nil {
				attrs = append(attrs, slog.String("error", err.Error()))
			}

			logger.LogAttrs(ctx, level, "HTTP request processed", attrs...)

			return err
		}
	}
}
