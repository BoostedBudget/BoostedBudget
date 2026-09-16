package posthog

import "context"

// TypedEvent is implemented by generated payloads. Callers cannot choose a raw event name or
// unreviewed property bag when using the product analytics gateway.
type TypedEvent interface {
	isGeneratedEvent()
	EventName() EventName
	Properties() map[string]interface{}
}

// ProductAnalytics is the narrow interface for new backend product event producers.
type ProductAnalytics interface {
	CaptureEvent(ctx context.Context, internalUserID string, event TypedEvent)
}

// PropertyContract and EventContract are populated only by generated code from
// config/posthog_events.yaml. They intentionally contain no user data.
type PropertyContract struct {
	Kind     string
	Required bool
	Values   []string
	Minimum  interface{}
	Maximum  interface{}
}

type EventContract struct {
	Consent                    string
	Status                     string
	LegacyPropertiesUnreviewed bool
	Properties                 map[string]PropertyContract
}

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
