package observability

import (
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLogger_NoExternalSinks(t *testing.T) {
	cfg := LogConfig{
		Level:        "info",
		Environment:  "test",
		ServiceName:  "test-service",
		Version:      "0.0.1",
		SentryDSN:    "",
		OTELEndpoint: "",
	}
	logger := NewLogger(cfg)
	require.NotNil(t, logger, "NewLogger should not return nil")
	// Should not panic when logging
	logger.Info("test message")
}

func TestNewLogger_WithSentryDSN(t *testing.T) {
	cfg := LogConfig{
		Level:       "info",
		Environment: "test",
		ServiceName: "test-service",
		Version:     "0.0.1",
		SentryDSN:   "https://fake@sentry.io/123",
	}
	// Should not panic even with a fake DSN (Sentry handler is initialized lazily)
	logger := NewLogger(cfg)
	require.NotNil(t, logger)
}

func TestNewLogger_WithOTELEndpoint(t *testing.T) {
	cfg := LogConfig{
		Level:        "debug",
		Environment:  "test",
		ServiceName:  "test-service",
		Version:      "0.0.1",
		OTELEndpoint: "localhost:4318",
	}
	logger := NewLogger(cfg)
	require.NotNil(t, logger)
}

func TestTeeHandler_Enabled(t *testing.T) {
	tee := &teeHandler{
		primary:   slog.NewJSONHandler(nil, &slog.HandlerOptions{Level: slog.LevelInfo}),
		secondary: slog.NewJSONHandler(nil, &slog.HandlerOptions{Level: slog.LevelInfo}),
	}

	ctx := t.Context()
	assert.False(t, tee.Enabled(ctx, slog.LevelDebug))
	assert.True(t, tee.Enabled(ctx, slog.LevelInfo))
	assert.True(t, tee.Enabled(ctx, slog.LevelWarn))
}

func TestTeeHandler_Enabled_PrimaryDisabledSecondaryEnabled(t *testing.T) {
	tee := &teeHandler{
		primary:   slog.NewJSONHandler(nil, &slog.HandlerOptions{Level: slog.LevelError}),
		secondary: slog.NewJSONHandler(nil, &slog.HandlerOptions{Level: slog.LevelDebug}),
	}

	assert.True(t, tee.Enabled(t.Context(), slog.LevelInfo))
}

func TestTeeHandler_Handle(t *testing.T) {
	tee := &teeHandler{
		primary:   slog.NewJSONHandler(io.Discard, nil),
		secondary: slog.NewJSONHandler(io.Discard, nil),
	}

	err := tee.Handle(t.Context(), slog.Record{})
	assert.NoError(t, err)
}

func TestTeeHandler_WithGroup(t *testing.T) {
	logger := NewLogger(LogConfig{
		Level:       "info",
		ServiceName: "test",
	})
	// WithGroup should not panic and should return a non-nil logger
	grouped := logger.WithGroup("mygroup")
	require.NotNil(t, grouped)
	// Logging on the grouped logger should not panic
	grouped.Info("grouped message", "key", "val")
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"info", slog.LevelInfo},
		{"", slog.LevelInfo},
		{"unknown", slog.LevelInfo},
		{"INFO", slog.LevelInfo}, // case-sensitive — defaults to info
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseLogLevel(tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}
