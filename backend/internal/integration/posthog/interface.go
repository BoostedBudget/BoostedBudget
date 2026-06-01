package posthog

import "context"

// Analytics is the interface for product analytics event capture.
// Services depend on this interface for easy mocking in tests.
type Analytics interface {
	Capture(distinctID, event string, properties map[string]interface{})
	Identify(distinctID string, properties map[string]interface{})
	Close()
}

// GDPRAnalytics is the interface for GDPR-consent-gated user-behavior event capture.
// Use this for user-initiated actions (e.g. accept/reject suggestion).
// *GDPRClient implements this interface via CaptureIfConsented.
type GDPRAnalytics interface {
	CaptureIfConsented(ctx context.Context, userID, event string, properties map[string]interface{})
}
