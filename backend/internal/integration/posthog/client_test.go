package posthog_test

import (
	"log/slog"
	"os"
	"testing"

	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestNewClient_EmptyAPIKey_ReturnsNil(t *testing.T) {
	client := phclient.NewClient("", "", testLogger())
	assert.Nil(t, client)
}

func TestNewClient_WithAPIKey_ReturnsClient(t *testing.T) {
	client := phclient.NewClient("phc_test_key", "https://us.i.posthog.com", testLogger())
	require.NotNil(t, client)
	client.Close()
}

func TestCapture_NilClient_NoOp(t *testing.T) {
	var client *phclient.Client
	// Must not panic
	assert.NotPanics(t, func() {
		client.Capture("user_123", "test_event", nil)
	})
}

func TestIdentify_NilClient_NoOp(t *testing.T) {
	var client *phclient.Client
	assert.NotPanics(t, func() {
		client.Identify("user_123", map[string]interface{}{"name": "test"})
	})
}

func TestClose_NilClient_NoOp(t *testing.T) {
	var client *phclient.Client
	assert.NotPanics(t, func() {
		client.Close()
	})
}

func TestClient_Implements_Analytics_Interface(t *testing.T) {
	// Compile-time check: *Client must implement Analytics
	var _ phclient.Analytics = (*phclient.Client)(nil)
}
