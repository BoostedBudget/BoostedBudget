package middleware

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
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

// StructuredLogger provides structured logging with JSON format
type StructuredLogger struct {
	writer io.Writer
}

// NewStructuredLogger creates a new structured logger that writes to the specified writer
// If writer is nil, it defaults to os.Stdout
func NewStructuredLogger(writer io.Writer) *StructuredLogger {
	if writer == nil {
		writer = os.Stdout
	}
	return &StructuredLogger{writer: writer}
}

// Info logs an informational message with optional fields
func (l *StructuredLogger) Info(message string, fields map[string]interface{}) {
	l.log("info", message, fields)
}

// Error logs an error message with optional fields
func (l *StructuredLogger) Error(message string, fields map[string]interface{}) {
	l.log("error", message, fields)
}

// Warn logs a warning message with optional fields
func (l *StructuredLogger) Warn(message string, fields map[string]interface{}) {
	l.log("warn", message, fields)
}

// Debug logs a debug message with optional fields
func (l *StructuredLogger) Debug(message string, fields map[string]interface{}) {
	l.log("debug", message, fields)
}

// log writes a structured log entry in JSON format
func (l *StructuredLogger) log(level, message string, fields map[string]interface{}) {
	entry := make(map[string]interface{})
	entry["level"] = level
	entry["message"] = message
	entry["timestamp"] = time.Now().UTC().Format(time.RFC3339)

	// Add custom fields
	if fields != nil {
		for k, v := range fields {
			entry[k] = v
		}
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(entry)
	if err != nil {
		// Fallback to standard logging if JSON marshal fails
		os.Stderr.WriteString("Failed to marshal log entry: " + err.Error() + "\n")
		return
	}

	// Write to output
	l.writer.Write(jsonData)
	l.writer.Write([]byte("\n"))
}

// StructuredLogging creates an Echo middleware that logs HTTP requests in structured JSON format
//
// The middleware logs the following fields:
// - type: "http_request"
// - method: HTTP method (GET, POST, etc.)
// - path: Request path
// - status: HTTP status code
// - duration_ms: Request duration in milliseconds
// - request_id: Unique request identifier (if set by RequestID middleware)
// - user_id: Authenticated user ID (if set by Clerk middleware)
// - remote_ip: Client IP address
// - user_agent: Client user agent
// - query: Query parameters (if present)
// - error: Error message (if request failed)
// - level: Log level (info or error)
// - timestamp: ISO 8601 timestamp
//
// Usage:
//
//	e := echo.New()
//	e.Use(RequestID())  // Should be first to generate request IDs
//	e.Use(StructuredLogging(os.Stdout))
func StructuredLogging(writer io.Writer) echo.MiddlewareFunc {
	logger := NewStructuredLogger(writer)

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()
			req := c.Request()

			// Execute the handler
			err := next(c)

			// Calculate duration
			duration := time.Since(start)

			// Build log entry
			fields := map[string]interface{}{
				"type":        "http_request",
				"method":      req.Method,
				"path":        req.URL.Path,
				"status":      c.Response().Status,
				"duration_ms": duration.Milliseconds(),
			}

			// Add request ID if available
			if requestID := c.Get("request_id"); requestID != nil {
				fields["request_id"] = requestID
			}

			// Add user ID if available (from auth middleware)
			if userID := c.Get("user_id"); userID != nil {
				fields["user_id"] = userID
			}

			// Add remote IP (extract from RemoteAddr)
			if remoteAddr := req.RemoteAddr; remoteAddr != "" {
				// RemoteAddr is in format "IP:port", extract just IP
				if idx := strings.LastIndex(remoteAddr, ":"); idx != -1 {
					fields["remote_ip"] = remoteAddr[:idx]
				} else {
					fields["remote_ip"] = remoteAddr
				}
			}

			// Add user agent
			if userAgent := req.UserAgent(); userAgent != "" {
				fields["user_agent"] = userAgent
			}

			// Add query parameters if present (sensitive values redacted)
			if query := req.URL.RawQuery; query != "" {
				fields["query"] = redactQueryParams(query)
			}

			// Determine log level and add error if present
			level := "info"
			if err != nil {
				level = "error"
				fields["error"] = err.Error()
			}

			// Log the request
			logger.log(level, "HTTP request processed", fields)

			return err
		}
	}
}
