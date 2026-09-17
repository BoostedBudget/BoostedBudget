package services

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/stretchr/testify/require"
)

// rcGatewayCapture is the transport boundary: the real Gateway has already resolved canonical
// identity, validated the generated contract and normalized every property by the time it lands.
type rcGatewayCapture struct {
	subject string
	event   string
	props   map[string]interface{}
}

type rcGatewayTransport struct{ captures []rcGatewayCapture }

func (t *rcGatewayTransport) Capture(subject, event string, props map[string]interface{}) {
	t.captures = append(t.captures, rcGatewayCapture{subject: subject, event: event, props: props})
}
func (*rcGatewayTransport) Identify(string, map[string]interface{}) {}
func (*rcGatewayTransport) Close()                                  {}

type rcGatewayAuthority struct {
	allowed bool
	err     error
	subject string
	users   []string
}

func (a *rcGatewayAuthority) WithConsentedSubject(_ context.Context, userID string, emit func(string)) (bool, error) {
	a.users = append(a.users, userID)
	if a.err != nil {
		return false, a.err
	}
	if a.allowed {
		emit(a.subject)
	}
	return a.allowed, nil
}

func newRCGatewayService(authority *rcGatewayAuthority) (*RevenueCatService, *rcGatewayTransport) {
	transport := &rcGatewayTransport{}
	service := NewRevenueCatService(&rcTestStore{ops: map[string]domain.RevenueCatOperation{}}, &rcTestProvider{}, rcTestProfile{})
	service.SetAnalytics(phclient.NewGateway(transport, authority, slog.New(slog.NewTextHandler(io.Discard, nil))))
	return service, transport
}

func rcCapturedNames(transport *rcGatewayTransport) []string {
	names := make([]string, 0, len(transport.captures))
	for _, capture := range transport.captures {
		names = append(names, capture.event)
	}
	return names
}

func TestRevenueCatAnalyticsDistinguishesQueuedFromAppliedAndOmitsReason(t *testing.T) {
	s, transport := newRCGatewayService(&rcGatewayAuthority{allowed: true, subject: "canonical-subject"})
	op, err := s.RequestGrant(context.Background(), "u", "comp", "premium", nil, false, "admin", "private sensitive explanation")
	require.NoError(t, err)
	require.Equal(t, []string{phclient.EventRevenueCatOperationQueued, phclient.EventAdminSubscriptionOverrideAppliedServer}, rcCapturedNames(transport))
	require.NoError(t, s.ProcessOperation(context.Background(), "u", op.ID))
	require.Equal(t, []string{phclient.EventRevenueCatOperationQueued, phclient.EventAdminSubscriptionOverrideAppliedServer, phclient.EventRevenueCatOperationApplied}, rcCapturedNames(transport))
	require.NoError(t, s.ProcessOperation(context.Background(), "u", op.ID))
	require.Len(t, transport.captures, 3)
	for _, capture := range transport.captures {
		require.Equal(t, "canonical-subject", capture.subject)
		require.NotContains(t, capture.props, "reason")
		require.NotContains(t, capture.props, "actor_id")
	}
}

func TestRevenueCatAdminOverrideIsScopedToAdminInitiatedGrants(t *testing.T) {
	s, transport := newRCGatewayService(&rcGatewayAuthority{allowed: true, subject: "canonical-subject"})

	op, err := s.RequestGrant(context.Background(), "target-user", "comp", "premium", nil, false, "acting-admin", "private reason")
	require.NoError(t, err)
	require.Equal(t, []string{phclient.EventRevenueCatOperationQueued, phclient.EventAdminSubscriptionOverrideAppliedServer}, rcCapturedNames(transport))
	require.Equal(t, map[string]interface{}{"tier": "premium", "had_expiry": false}, transport.captures[1].props)
	require.NotContains(t, transport.captures[1].props, "actor_id")
	require.NotContains(t, transport.captures[1].props, "reason")

	// ProcessOperation is the shared sink for every operation, including system-initiated ones.
	// It must never emit the admin-override event.
	require.NoError(t, s.ProcessOperation(context.Background(), "target-user", op.ID))
	require.Equal(t, []string{phclient.EventRevenueCatOperationApplied}, rcCapturedNames(transport)[2:])

	// A revoke emits the removed variant.
	_, err = s.RequestGrant(context.Background(), "target-user", "comp", "premium", nil, true, "acting-admin", "private reason")
	require.NoError(t, err)
	names := rcCapturedNames(transport)
	require.Equal(t, phclient.EventAdminSubscriptionOverrideRemovedServer, names[len(names)-1])
}

func TestRevenueCatAnalyticsChecksTargetConsentForEveryOperationEvent(t *testing.T) {
	for _, tc := range []struct {
		name            string
		allowed, failed bool
		authorityErr    error
	}{
		{name: "opted out"},
		{name: "consented", allowed: true},
		{name: "failed and opted out", failed: true},
		{name: "failed and consented", allowed: true, failed: true},
		{name: "authority lookup fails", authorityErr: errors.New("privacy unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			authority := &rcGatewayAuthority{allowed: tc.allowed, err: tc.authorityErr, subject: "canonical-subject"}
			p := &rcTestProvider{}
			s := NewRevenueCatService(&rcTestStore{ops: map[string]domain.RevenueCatOperation{}}, p, rcTestProfile{})
			transport := &rcGatewayTransport{}
			s.SetAnalytics(phclient.NewGateway(transport, authority, slog.New(slog.NewTextHandler(io.Discard, nil))))

			op, err := s.RequestGrant(ctx, "target-user", "comp", "premium", nil, false, "acting-admin", "private reason")
			require.NoError(t, err)
			if tc.failed {
				p.fetchErr = errors.New("provider unavailable")
			}
			err = s.ProcessOperation(ctx, "target-user", op.ID)
			require.Equal(t, tc.failed, err != nil)
			require.Equal(t, []string{"target-user", "target-user", "target-user"}, authority.users)
			if !tc.allowed || tc.authorityErr != nil {
				require.Empty(t, transport.captures)
				return
			}
			event := phclient.EventRevenueCatOperationApplied
			if tc.failed {
				event = phclient.EventRevenueCatOperationFailed
			}
			require.Equal(t, []string{phclient.EventRevenueCatOperationQueued, phclient.EventAdminSubscriptionOverrideAppliedServer, event}, rcCapturedNames(transport))
		})
	}
}

func TestRevenueCatAnalyticsUnconfiguredIsNoOp(t *testing.T) {
	r := &rcTestStore{ops: map[string]domain.RevenueCatOperation{}}
	s := NewRevenueCatService(r, &rcTestProvider{}, rcTestProfile{})
	s.SetAnalytics(nil)
	op, err := s.RequestGrant(context.Background(), "target", "comp", "premium", nil, false, "admin", "reason")
	require.NoError(t, err)
	require.NoError(t, s.ProcessOperation(context.Background(), "target", op.ID))
}
