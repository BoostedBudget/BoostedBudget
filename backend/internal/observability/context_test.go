package observability

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithRequestID_RoundTrip(t *testing.T) {
	ctx := context.Background()
	ctx = WithRequestID(ctx, "req-123")
	assert.Equal(t, "req-123", RequestIDFromContext(ctx))
}

func TestRequestIDFromContext_Missing(t *testing.T) {
	ctx := context.Background()
	assert.Equal(t, "", RequestIDFromContext(ctx))
}

func TestWithUserID_RoundTrip(t *testing.T) {
	ctx := context.Background()
	ctx = WithUserID(ctx, "user-abc")
	assert.Equal(t, "user-abc", UserIDFromContext(ctx))
}

func TestUserIDFromContext_Missing(t *testing.T) {
	ctx := context.Background()
	assert.Equal(t, "", UserIDFromContext(ctx))
}

func TestLoggerFromContext_EmptyContext(t *testing.T) {
	ctx := context.Background()
	logger := LoggerFromContext(ctx)
	require.NotNil(t, logger, "logger should never be nil")
	// Should not panic when logging
	logger.Info("test")
}

func TestLoggerFromContext_WithRequestAndUserID(t *testing.T) {
	ctx := context.Background()
	ctx = WithRequestID(ctx, "req-456")
	ctx = WithUserID(ctx, "user-xyz")

	logger := LoggerFromContext(ctx)
	require.NotNil(t, logger)
	// Logger should not panic when used
	logger.Info("test with ids")
}

func TestContextKeys_DoNotCollide(t *testing.T) {
	ctx := context.Background()
	ctx = WithRequestID(ctx, "req-1")
	ctx = WithUserID(ctx, "usr-1")

	assert.Equal(t, "req-1", RequestIDFromContext(ctx))
	assert.Equal(t, "usr-1", UserIDFromContext(ctx))
}
