package observability

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitTracer_EmptyEndpoint(t *testing.T) {
	shutdown, err := InitTracer(OTELConfig{
		Endpoint:    "",
		ServiceName: "test-service",
		Version:     "0.0.1",
		Environment: "test",
	})
	require.NoError(t, err, "empty endpoint should return no-op shutdown without error")
	require.NotNil(t, shutdown)

	// no-op shutdown should succeed
	err = shutdown(context.Background())
	assert.NoError(t, err)
}

func TestInitTracer_NonEmptyEndpoint(t *testing.T) {
	// Attempting to connect to a non-existent endpoint; the connection attempt
	// is deferred so New() itself may succeed. We just verify no panic.
	shutdown, err := InitTracer(OTELConfig{
		Endpoint:    "localhost:4318",
		ServiceName: "test-service",
		Version:     "0.0.1",
		Environment: "test",
		SampleRate:  1.0,
	})
	// err may or may not be nil depending on whether the connection is attempted synchronously.
	// The important thing is no panic.
	if err == nil {
		require.NotNil(t, shutdown)
		// Attempt to shut down (may fail if no connection, that's fine)
		_ = shutdown(context.Background())
	}
}

func TestInitTracer_DefaultSampleRate(t *testing.T) {
	// SampleRate 0 should default to 1.0 — no panic
	shutdown, err := InitTracer(OTELConfig{
		Endpoint:   "",
		SampleRate: 0,
	})
	require.NoError(t, err)
	require.NotNil(t, shutdown)
}
