package services

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	rc "github.com/maxnussbaum/BoostedBudget/internal/integration/revenuecat"
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

// rcStatusProfile returns a real user row carrying a persisted subscription_status, so the
// service can distinguish a genuine active→inactive transition from a repeated inactive refresh.
type rcStatusProfile struct{ status string }

func (p rcStatusProfile) GetUserByID(context.Context, string) (*domain.User, error) {
	return &domain.User{ID: "local-uuid", ClerkUserID: "clerk_identity", SubscriptionStatus: p.status}, nil
}

func newRCGatewayServiceWithStore(authority *rcGatewayAuthority, store *rcTestStore, provider *rcTestProvider, status string) (*RevenueCatService, *rcGatewayTransport) {
	transport := &rcGatewayTransport{}
	service := NewRevenueCatService(store, provider, rcStatusProfile{status: status})
	service.SetAnalytics(phclient.NewGateway(transport, authority, slog.New(slog.NewTextHandler(io.Discard, nil))))
	return service, transport
}

// rcForbiddenAccessKeys are the identifiers/reasons the access-resolution events must never carry.
var rcForbiddenAccessKeys = []string{
	"reason", "actor_id", "email", "amount", "account_id", "transaction_id",
	"item_id", "customer_id", "plaid_item_id", "user_id", "tier",
}

// TestRevenueCatAccessInactiveAndRetentionArmedThroughRealGateway drives a genuine active→inactive
// access-resolution flip through the REAL Gateway + AuthorityResolver. A mock would bypass contract
// validation, so a wrong contract would silently drop and this test would not catch it.
func TestRevenueCatAccessInactiveAndRetentionArmedThroughRealGateway(t *testing.T) {
	expired := time.Now().Add(-time.Hour)
	store := &rcTestStore{state: domain.RevenueCatState{AccessUpdatedAt: time.Now().Add(-time.Hour)}}
	provider := &rcTestProvider{grants: []rc.Entitlement{{LookupKey: "trial_premium", ExpiresAt: &expired}}}
	s, transport := newRCGatewayServiceWithStore(&rcGatewayAuthority{allowed: true, subject: "canonical-subject"}, store, provider, "active")

	_, err := s.Refresh(context.Background(), "target-user")
	require.NoError(t, err)
	require.Equal(t,
		[]string{phclient.EventRevenueCatAccessInactive, phclient.EventRevenueCatRetentionArmed},
		rcCapturedNames(transport),
		"a persisted active→inactive flip must emit the access outcome first, then the arming")
	require.Equal(t, map[string]interface{}{"pending_operations": int64(0)}, transport.captures[0].props)
	require.Equal(t, map[string]interface{}{"deferred_for_pending_operations": false}, transport.captures[1].props)
	for _, capture := range transport.captures {
		require.Equal(t, "canonical-subject", capture.subject)
		for _, forbidden := range rcForbiddenAccessKeys {
			require.NotContains(t, capture.props, forbidden)
		}
	}
}

// TestRevenueCatAccessStaysActiveWithFutureExpiryEmitsNothing is the failure QA: a future
// ExpiresAt must keep the user active, so neither the retention-armed nor the access-inactive
// event may fire.
func TestRevenueCatAccessStaysActiveWithFutureExpiryEmitsNothing(t *testing.T) {
	future := time.Now().Add(24 * time.Hour)
	store := &rcTestStore{state: domain.RevenueCatState{AccessUpdatedAt: time.Now().Add(-time.Hour)}}
	provider := &rcTestProvider{grants: []rc.Entitlement{{LookupKey: "trial_premium", ExpiresAt: &future}}}
	s, transport := newRCGatewayServiceWithStore(&rcGatewayAuthority{allowed: true, subject: "canonical-subject"}, store, provider, "active")

	_, err := s.Refresh(context.Background(), "target-user")
	require.NoError(t, err)
	require.Empty(t, transport.captures, "an unexpired grant must not arm retention or emit access loss")
}

// TestRevenueCatRepeatedInactiveRefreshDoesNotReEmit pins the transition semantics: a user already
// persisted inactive must not re-fire the events on a later refresh.
func TestRevenueCatRepeatedInactiveRefreshDoesNotReEmit(t *testing.T) {
	store := &rcTestStore{state: domain.RevenueCatState{AccessUpdatedAt: time.Now().Add(-time.Hour)}}
	provider := &rcTestProvider{}
	s, transport := newRCGatewayServiceWithStore(&rcGatewayAuthority{allowed: true, subject: "canonical-subject"}, store, provider, "inactive")

	_, err := s.Refresh(context.Background(), "target-user")
	require.NoError(t, err)
	require.Empty(t, transport.captures, "an already-inactive user must not re-emit the transition")
}

// TestRevenueCatErasureRefreshEmitsNoAccessEvents pins the consent/erasure boundary: a user being
// erased never produces a product event (the authority query would drop it, and it would recreate
// the person the erasure outbox just deleted).
func TestRevenueCatErasureRefreshEmitsNoAccessEvents(t *testing.T) {
	now := time.Now()
	store := &rcTestStore{state: domain.RevenueCatState{AccessUpdatedAt: now.Add(-time.Hour), ErasureStartedAt: &now}}
	provider := &rcTestProvider{}
	s, transport := newRCGatewayServiceWithStore(&rcGatewayAuthority{allowed: true, subject: "canonical-subject"}, store, provider, "active")

	_, err := s.Refresh(context.Background(), "target-user")
	require.NoError(t, err)
	require.Empty(t, transport.captures, "a user being erased must never emit a product event")
}

// TestRevenueCatPendingOperationDefersArming pins the persistence guard mirror: a queued pending
// operation makes the repository's users UPDATE a no-op, so the arming event must report the
// deferral rather than silently claiming the clock was armed.
func TestRevenueCatPendingOperationDefersArming(t *testing.T) {
	expired := time.Now().Add(-time.Hour)
	store := &rcTestStore{state: domain.RevenueCatState{AccessUpdatedAt: time.Now().Add(-time.Hour), PendingOperations: 1}}
	provider := &rcTestProvider{grants: []rc.Entitlement{{LookupKey: "trial_premium", ExpiresAt: &expired}}}
	s, transport := newRCGatewayServiceWithStore(&rcGatewayAuthority{allowed: true, subject: "canonical-subject"}, store, provider, "active")

	_, err := s.Refresh(context.Background(), "target-user")
	require.NoError(t, err)
	require.Equal(t,
		[]string{phclient.EventRevenueCatAccessInactive, phclient.EventRevenueCatRetentionArmed},
		rcCapturedNames(transport))
	require.Equal(t, map[string]interface{}{"pending_operations": int64(1)}, transport.captures[0].props)
	require.Equal(t, map[string]interface{}{"deferred_for_pending_operations": true}, transport.captures[1].props)
}
