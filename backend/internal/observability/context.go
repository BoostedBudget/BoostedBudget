package observability

import (
	"context"
	"log/slog"
)

type contextKey string

const (
	contextKeyRequestID contextKey = "request_id"
	contextKeyUserID    contextKey = "user_id"
)

// WithRequestID stores a request ID in the context.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, contextKeyRequestID, id)
}

// RequestIDFromContext retrieves the request ID from context.
// Returns empty string if not present.
func RequestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(contextKeyRequestID).(string); ok {
		return v
	}
	return ""
}

// WithUserID stores a user ID in the context.
func WithUserID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, contextKeyUserID, id)
}

// UserIDFromContext retrieves the user ID from context.
// Returns empty string if not present.
func UserIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(contextKeyUserID).(string); ok {
		return v
	}
	return ""
}

// LoggerFromContext returns a logger enriched with request_id and user_id from ctx.
// Falls back to slog.Default() if no logger is in context.
func LoggerFromContext(ctx context.Context) *slog.Logger {
	logger := slog.Default()
	if id := RequestIDFromContext(ctx); id != "" {
		logger = logger.With("request_id", id)
	}
	if id := UserIDFromContext(ctx); id != "" {
		logger = logger.With("user_id", id)
	}
	return logger
}
