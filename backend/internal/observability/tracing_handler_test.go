package observability

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTracingHandler_AddsRequestID(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	handler := NewTracingHandler(base)
	logger := slog.New(handler)

	ctx := WithRequestID(context.Background(), "req-test-123")
	logger.InfoContext(ctx, "test message")

	output := buf.String()
	assert.Contains(t, output, "req-test-123", "output should contain the request_id")
	assert.Contains(t, output, "request_id")
}

func TestTracingHandler_AddsUserID(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	handler := NewTracingHandler(base)
	logger := slog.New(handler)

	ctx := WithUserID(context.Background(), "user-abc")
	logger.InfoContext(ctx, "test message")

	output := buf.String()
	assert.Contains(t, output, "user-abc")
	assert.Contains(t, output, "user_id")
}

func TestTracingHandler_EmptyContext(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	handler := NewTracingHandler(base)
	logger := slog.New(handler)

	logger.InfoContext(context.Background(), "no ids here")

	output := buf.String()
	assert.NotContains(t, output, "request_id", "should not add request_id when context has none")
	assert.NotContains(t, output, "user_id", "should not add user_id when context has none")
	assert.NotContains(t, output, "trace_id", "should not add trace_id when no span is active")
}

func TestTracingHandler_NoOtelSpan(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	handler := NewTracingHandler(base)
	logger := slog.New(handler)

	ctx := WithRequestID(context.Background(), "req-no-span")
	logger.InfoContext(ctx, "message without otel span")

	output := buf.String()
	assert.NotContains(t, output, "trace_id", "should not add trace_id without active span")
	assert.NotContains(t, output, "span_id", "should not add span_id without active span")
}

func TestTracingHandler_Enabled(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	handler := NewTracingHandler(base)

	ctx := context.Background()
	assert.False(t, handler.Enabled(ctx, slog.LevelDebug))
	assert.False(t, handler.Enabled(ctx, slog.LevelInfo))
	assert.True(t, handler.Enabled(ctx, slog.LevelWarn))
	assert.True(t, handler.Enabled(ctx, slog.LevelError))
}

func TestTracingHandler_WithAttrs(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	handler := NewTracingHandler(base)
	enriched := handler.WithAttrs([]slog.Attr{slog.String("component", "test")})
	require.NotNil(t, enriched)

	logger := slog.New(enriched)
	logger.InfoContext(context.Background(), "with attrs")
	assert.Contains(t, buf.String(), "component")
}

func TestTracingHandler_WithGroup(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	handler := NewTracingHandler(base)
	grouped := handler.WithGroup("mygroup")
	require.NotNil(t, grouped)

	logger := slog.New(grouped)
	logger.With("key", "val").InfoContext(context.Background(), "grouped")
	assert.True(t, strings.Contains(buf.String(), "mygroup") || strings.Contains(buf.String(), "key"))
}
