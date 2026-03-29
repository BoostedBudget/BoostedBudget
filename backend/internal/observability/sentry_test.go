package observability

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitSentry_EmptyDSN(t *testing.T) {
	err := InitSentry(SentryConfig{
		DSN:         "",
		Environment: "test",
		Release:     "0.0.1",
	})
	require.NoError(t, err, "empty DSN should be a no-op and return nil")
}

func TestInitSentry_InvalidDSN(t *testing.T) {
	err := InitSentry(SentryConfig{
		DSN:         "not-a-valid-dsn",
		Environment: "test",
		Release:     "0.0.1",
	})
	assert.Error(t, err, "invalid DSN should return an error")
}

func TestInitSentry_DefaultSampleRate(t *testing.T) {
	// SampleRate 0 should default to 1.0 — verify no panic
	err := InitSentry(SentryConfig{
		DSN:        "",
		SampleRate: 0,
	})
	require.NoError(t, err)
}
