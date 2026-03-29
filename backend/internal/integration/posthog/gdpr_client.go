package posthog

import (
	"context"
	"log/slog"
)

// ConsentChecker checks whether a user has given analytics consent.
type ConsentChecker interface {
	HasAnalyticsConsent(ctx context.Context, userID string) (bool, error)
}

// GDPRClient wraps Analytics and gates user-behavior events behind consent checks.
// Use Capture for system/operational events (not gated).
// Use CaptureIfConsented for user-behavior events.
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
		g.logger.Warn("posthog: failed to check analytics consent, skipping event", "user_id", userID, "event", event, "error", err)
		return
	}

	if consented {
		g.analytics.Capture(userID, event, properties)
	}
}
