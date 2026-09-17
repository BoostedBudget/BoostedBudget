package posthog

import (
	"log/slog"
	"time"

	posthoggo "github.com/posthog/posthog-go"
)

// Client wraps the posthog-go client and implements the Analytics interface.
// Returns nil from NewClient when apiKey is empty — all methods are nil-safe.
type Client struct {
	inner  enqueueCloser
	logger *slog.Logger
}

type enqueueCloser interface {
	Enqueue(posthoggo.Message) error
	Close() error
}

type deliveryCallback struct{}

func (deliveryCallback) Success(posthoggo.APIMessage) {}
func (deliveryCallback) Failure(posthoggo.APIMessage, error) {
	analyticsDeliveryFailuresTotal.WithLabelValues("delivery").Inc()
}

// NewClient creates a PostHog client. Returns nil if apiKey is empty (disabled).
func NewClient(apiKey, host string, logger *slog.Logger) *Client {
	if apiKey == "" {
		return nil
	}
	if host == "" {
		host = "https://us.i.posthog.com"
	}
	if logger == nil {
		logger = slog.Default()
	}
	inner, err := posthoggo.NewWithConfig(apiKey, posthoggo.Config{
		Endpoint:        host,
		Callback:        deliveryCallback{},
		ShutdownTimeout: 5 * time.Second,
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
		analyticsDeliveryFailuresTotal.WithLabelValues("enqueue").Inc()
		c.logger.Error("posthog capture enqueue failed", "event", boundedEventLabel(EventName(event)), "error", err)
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
		analyticsDeliveryFailuresTotal.WithLabelValues("shutdown").Inc()
		c.logger.Error("posthog close failed", "error", err)
	}
}
