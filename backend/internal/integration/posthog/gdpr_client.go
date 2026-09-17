package posthog

import (
	"context"
	"log/slog"
)

// ConsentChecker checks whether a user has given analytics consent.
type ConsentChecker interface {
	HasAnalyticsConsent(ctx context.Context, userID string) (bool, error)
}

// Compile-time assertion: *GDPRClient must satisfy GDPRAnalytics.
var _ GDPRAnalytics = (*GDPRClient)(nil)

// GDPRClient is retained for compatibility tests and non-production adapters. Production wiring
// uses Gateway, which additionally resolves canonical identity and rejects operational events.
type GDPRClient struct {
	analytics Analytics
	consent   ConsentChecker
	logger    *slog.Logger
}

// NewGDPRClient creates a GDPR-aware analytics client.
func NewGDPRClient(analytics Analytics, consent ConsentChecker, logger *slog.Logger) *GDPRClient {
	return &GDPRClient{analytics: analytics, consent: consent, logger: logger}
}

// CaptureIfConsented sends a capture event only when the user has granted analytics consent.
func (g *GDPRClient) CaptureIfConsented(ctx context.Context, userID, event string, properties map[string]interface{}) {
	if g.analytics == nil {
		return
	}

	consented, err := g.consent.HasAnalyticsConsent(ctx, userID)
	if err != nil {
		g.logger.Warn("posthog: analytics consent unavailable; event skipped")
		return
	}

	if consented {
		g.analytics.Capture(userID, event, properties)
	}
}
