package posthog

import (
	"log/slog"

	posthoggo "github.com/posthog/posthog-go"
)

// Client wraps the posthog-go client and implements the Analytics interface.
// Returns nil from NewClient when apiKey is empty — all methods are nil-safe.
type Client struct {
	inner  posthoggo.Client
	logger *slog.Logger
}

// NewClient creates a PostHog client. Returns nil if apiKey is empty (disabled).
func NewClient(apiKey, host string, logger *slog.Logger) *Client {
	if apiKey == "" {
		return nil
	}
	if host == "" {
		host = "https://us.i.posthog.com"
	}
	inner, err := posthoggo.NewWithConfig(apiKey, posthoggo.Config{
		Endpoint: host,
	})
	if err != nil {
		logger.Error("failed to initialize PostHog client", "error", err)
		return nil
	}
	return &Client{inner: inner, logger: logger}
}

// Capture sends a capture event. No-op when client is nil.
func (c *Client) Capture(distinctID, event string, properties map[string]interface{}) {
	if c == nil {
		return
	}
	if err := c.inner.Enqueue(posthoggo.Capture{
		DistinctId: distinctID,
		Event:      event,
		Properties: posthoggo.Properties(properties),
	}); err != nil {
		c.logger.Error("posthog capture failed", "event", event, "error", err)
	}
}

// Identify sends an identify call. No-op when client is nil.
func (c *Client) Identify(distinctID string, properties map[string]interface{}) {
	if c == nil {
		return
	}
	if err := c.inner.Enqueue(posthoggo.Identify{
		DistinctId: distinctID,
		Properties: posthoggo.Properties(properties),
	}); err != nil {
		c.logger.Error("posthog identify failed", "error", err)
	}
}

// Close flushes buffered events and closes the client. No-op when client is nil.
func (c *Client) Close() {
	if c == nil {
		return
	}
	if err := c.inner.Close(); err != nil {
		c.logger.Error("posthog close failed", "error", err)
	}
}
