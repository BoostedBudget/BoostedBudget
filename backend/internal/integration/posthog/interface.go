package posthog

// Analytics is the interface for product analytics event capture.
// Services depend on this interface for easy mocking in tests.
type Analytics interface {
	Capture(distinctID, event string, properties map[string]interface{})
	Identify(distinctID string, properties map[string]interface{})
	Close()
}
