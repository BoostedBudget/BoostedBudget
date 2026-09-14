package posthog_test

import (
	"context"
	"errors"
	"testing"

	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/stretchr/testify/assert"
)

// stubAnalytics is a test double for the Analytics interface.
type stubAnalytics struct {
	captures []captureCall
}

type captureCall struct {
	distinctID string
	event      string
}

func (s *stubAnalytics) Capture(distinctID, event string, _ map[string]interface{}) {
	s.captures = append(s.captures, captureCall{distinctID: distinctID, event: event})
}

func (s *stubAnalytics) Identify(_ string, _ map[string]interface{}) {}
func (s *stubAnalytics) Close()                                      {}

// stubConsent is a test double for the ConsentChecker interface.
type stubConsent struct {
	consented bool
	err       error
}

func (s *stubConsent) HasAnalyticsConsent(_ context.Context, _ string) (bool, error) {
	return s.consented, s.err
}

func TestGDPRClient_NoConsent_SkipsCapture(t *testing.T) {
	analytics := &stubAnalytics{}
	consent := &stubConsent{consented: false}
	client := phclient.NewGDPRClient(analytics, consent, testLogger())

	client.CaptureIfConsented(context.Background(), "user_123", "test_event", nil)

	assert.Empty(t, analytics.captures)
}

func TestGDPRClient_WithConsent_Captures(t *testing.T) {
	analytics := &stubAnalytics{}
	consent := &stubConsent{consented: true}
	client := phclient.NewGDPRClient(analytics, consent, testLogger())

	client.CaptureIfConsented(context.Background(), "user_123", "test_event", nil)

	assert.Len(t, analytics.captures, 1)
	assert.Equal(t, "user_123", analytics.captures[0].distinctID)
	assert.Equal(t, "test_event", analytics.captures[0].event)
}

func TestGDPRClient_ConsentCheckError_SkipsCapture(t *testing.T) {
	analytics := &stubAnalytics{}
	consent := &stubConsent{err: errors.New("db error")}
	client := phclient.NewGDPRClient(analytics, consent, testLogger())

	client.CaptureIfConsented(context.Background(), "user_123", "test_event", nil)

	assert.Empty(t, analytics.captures)
}

func TestGDPRClient_NilAnalytics_NoOp(t *testing.T) {
	consent := &stubConsent{consented: true}
	client := phclient.NewGDPRClient(nil, consent, testLogger())

	assert.NotPanics(t, func() {
		client.CaptureIfConsented(context.Background(), "user_123", "test_event", nil)
	})
}
