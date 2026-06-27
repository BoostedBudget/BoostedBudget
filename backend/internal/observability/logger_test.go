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
	// Both handlers at Info level. Debug should be disabled, Info should be enabled.
	primary := slog.NewJSONHandler(nil, &slog.HandlerOptions{Level: slog.LevelInfo})
	secondary := slog.NewJSONHandler(nil, &slog.HandlerOptions{Level: slog.LevelInfo})
	tee := &teeHandler{primary: primary, secondary: secondary}

	ctx := t.Context()
	assert.False(t, tee.Enabled(ctx, slog.LevelDebug))
	assert.True(t, tee.Enabled(ctx, slog.LevelInfo))
	assert.True(t, tee.Enabled(ctx, slog.LevelWarn))
}

func TestTeeHandler_Enabled_PrimaryDisabledSecondaryEnabled(t *testing.T) {
	// primary at Error, secondary at Debug → Enabled should return true for Info
	// because secondary is enabled for Info
	primary := slog.NewJSONHandler(nil, &slog.HandlerOptions{Level: slog.LevelError})
	secondary := slog.NewJSONHandler(nil, &slog.HandlerOptions{Level: slog.LevelDebug})
	tee := &teeHandler{primary: primary, secondary: secondary}

	ctx := t.Context()
	assert.True(t, tee.Enabled(ctx, slog.LevelInfo))
}

func TestTeeHandler_Handle(t *testing.T) {
	// Use two valid JSON handlers writing to io.Discard so Handle doesn't panic
	primary := slog.NewJSONHandler(io.Discard, nil)
	secondary := slog.NewJSONHandler(io.Discard, nil)
	tee := &teeHandler{primary: primary, secondary: secondary}

	ctx := t.Context()
	// Handle via tee directly — just verify it doesn't panic or error
	err := tee.Handle(ctx, slog.Record{})
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
