package observability

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"log/slog"
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
