package services

// #1652 todo 8 — the grant-lifecycle transitions.
//
// Four catalog-declared events describe transitions that reach the user's state WITHOUT the admin
// operation queue: a comp grant expiring, a paid grant lapsing, a trial starting, and a trial
// ending. The admin-initiated comp GRANT is deliberately NOT among them — it already fires the
// admin queue's revenuecat_operation_queued/applied stream plus the target-user override event, so
// adding a fifth event there would duplicate a transition that already has an emitter.
//
// Every case below drives the REAL Gateway + AuthorityResolver, not a mock: a contract violation
// (an unbounded value, a property key containing an identifier, a missing required property) is
// silently dropped by the gateway in production, and a mock-backed test would stay green through
// exactly that defect.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	rc "github.com/maxnussbaum/BoostedBudget/internal/integration/revenuecat"
	"github.com/stretchr/testify/require"
)

// rcLifecycleStaleState is a persisted projection old enough to force an authoritative refresh.
func rcLifecycleStaleState(grants ...domain.RevenueCatGrant) domain.RevenueCatState {
	return domain.RevenueCatState{AccessUpdatedAt: time.Now().Add(-time.Hour), Grants: grants}
}

// requireLifecycleCaptureHasNoForbiddenKeys asserts the lifecycle events carry none of the
// identifiers or reasons this instrumentation must never emit.
func requireLifecycleCaptureHasNoForbiddenKeys(t *testing.T, capture rcGatewayCapture) {
	t.Helper()
	for _, forbidden := range rcForbiddenAccessKeys {
		require.NotContains(t, capture.props, forbidden)
	}
}

func TestRevenueCatCompExpiryThroughRealGateway(t *testing.T) {
	store := &rcTestStore{state: rcLifecycleStaleState(domain.RevenueCatGrant{Key: "comp_household_3"})}
	s, transport := newRCGatewayServiceWithStore(&rcGatewayAuthority{allowed: true, subject: "canonical-subject"}, store, &rcTestProvider{}, "active")

	_, err := s.Refresh(context.Background(), "target-user")
	require.NoError(t, err)
	require.Equal(t,
		[]string{phclient.EventRevenueCatCompExpired, phclient.EventRevenueCatAccessRevoked, phclient.EventRevenueCatAccessInactive, phclient.EventRevenueCatRetentionArmed},
		rcCapturedNames(transport),
		"a lapsed comp grant must emit the comp expiry first, then the revocation, the generic access outcome and its retention arming")
	require.Equal(t, map[string]interface{}{"entitlement": "comp_household_3"}, transport.captures[0].props)
	require.Equal(t, map[string]interface{}{"entitlement": "comp_household_3", "source": "webhook"}, transport.captures[1].props)
	require.Equal(t, map[string]interface{}{"pending_operations": int64(0)}, transport.captures[2].props)
	require.Equal(t, map[string]interface{}{"deferred_for_pending_operations": false}, transport.captures[3].props)
	for _, capture := range transport.captures {
		require.Equal(t, "canonical-subject", capture.subject)
		requireLifecycleCaptureHasNoForbiddenKeys(t, capture)
	}
}

func TestRevenueCatPaidGrantLapseThroughRealGateway(t *testing.T) {
	future := time.Now().Add(30 * 24 * time.Hour)
	store := &rcTestStore{state: rcLifecycleStaleState(domain.RevenueCatGrant{Key: "paid_household_2", ExpiresAt: &future})}
	s, transport := newRCGatewayServiceWithStore(&rcGatewayAuthority{allowed: true, subject: "canonical-subject"}, store, &rcTestProvider{}, "active")

	_, err := s.Refresh(context.Background(), "target-user")
	require.NoError(t, err)
	require.Equal(t,
		[]string{phclient.EventRevenueCatPaidGrantLapsed, phclient.EventRevenueCatAccessRevoked, phclient.EventRevenueCatAccessInactive, phclient.EventRevenueCatRetentionArmed},
		rcCapturedNames(transport),
		"a lapsed paid grant must emit the lapse first, then the revocation, the generic access outcome and its retention arming")
	require.Equal(t, map[string]interface{}{"entitlement": "paid_household_2"}, transport.captures[0].props)
	require.Equal(t, map[string]interface{}{"entitlement": "paid_household_2", "source": "refund"}, transport.captures[1].props)
	for _, capture := range transport.captures {
		require.Equal(t, "canonical-subject", capture.subject)
		requireLifecycleCaptureHasNoForbiddenKeys(t, capture)
	}
}

func TestRevenueCatTrialStartedThroughRealGateway(t *testing.T) {
	end := time.Now().Add(60 * 24 * time.Hour)
	store := &rcTestStore{ops: map[string]domain.RevenueCatOperation{
		"op": {ID: "op", UserID: "target-user", Class: "trial", Key: "trial_premium", ExpiresAt: &end, Stage: "pending"},
	}}
	s, transport := newRCGatewayServiceWithStore(&rcGatewayAuthority{allowed: true, subject: "canonical-subject"}, store, &rcTestProvider{}, "inactive")

	require.NoError(t, s.ProcessOperation(context.Background(), "target-user", "op"))
	require.Equal(t,
		[]string{phclient.EventRevenueCatOperationApplied, phclient.EventRevenueCatTrialStarted},
		rcCapturedNames(transport),
		"an applied trial operation must emit the trial lifecycle boundary alongside the operation-queue event")
	require.Equal(t, map[string]interface{}{"entitlement": "trial_premium"}, transport.captures[1].props)
	for _, capture := range transport.captures {
		require.Equal(t, "canonical-subject", capture.subject)
		requireLifecycleCaptureHasNoForbiddenKeys(t, capture)
	}
}

func TestRevenueCatTrialEndedExpiredThroughRealGateway(t *testing.T) {
	store := &rcTestStore{state: rcLifecycleStaleState(domain.RevenueCatGrant{Key: "trial_premium"})}
	s, transport := newRCGatewayServiceWithStore(&rcGatewayAuthority{allowed: true, subject: "canonical-subject"}, store, &rcTestProvider{}, "trial")

	_, err := s.Refresh(context.Background(), "target-user")
	require.NoError(t, err)
	require.Equal(t,
		[]string{phclient.EventRevenueCatTrialEnded, phclient.EventRevenueCatAccessRevoked, phclient.EventRevenueCatAccessInactive, phclient.EventRevenueCatRetentionArmed},
		rcCapturedNames(transport),
		"an expired trial must emit the trial boundary first, then the revocation, the generic access outcome and its retention arming")
	require.Equal(t, map[string]interface{}{"entitlement": "trial_premium", "converted": false}, transport.captures[0].props)
	require.Equal(t, map[string]interface{}{"entitlement": "trial_premium", "source": "webhook"}, transport.captures[1].props)
	for _, capture := range transport.captures {
		require.Equal(t, "canonical-subject", capture.subject)
		requireLifecycleCaptureHasNoForbiddenKeys(t, capture)
	}
}

func TestRevenueCatTrialEndedConvertedThroughRealGateway(t *testing.T) {
	future := time.Now().Add(30 * 24 * time.Hour)
	store := &rcTestStore{state: rcLifecycleStaleState(domain.RevenueCatGrant{Key: "trial_premium"})}
	provider := &rcTestProvider{grants: []rc.Entitlement{{LookupKey: "paid_premium", ExpiresAt: &future}}}
	s, transport := newRCGatewayServiceWithStore(&rcGatewayAuthority{allowed: true, subject: "canonical-subject"}, store, provider, "trial")

	_, err := s.Refresh(context.Background(), "target-user")
	require.NoError(t, err)
	require.Equal(t, []string{phclient.EventRevenueCatTrialEnded}, rcCapturedNames(transport),
		"a trial that converted to a paid grant ends without losing access, so only the trial boundary fires")
	require.Equal(t, map[string]interface{}{"entitlement": "trial_premium", "converted": true}, transport.captures[0].props)
	requireLifecycleCaptureHasNoForbiddenKeys(t, transport.captures[0])
}

// TestRevenueCatLifecycleStaysActiveEmitsNothing is the failure QA: a live grant keeps the user
// active, so no comp-expiry, paid-lapse, or trial event may fire.
func TestRevenueCatLifecycleStaysActiveEmitsNothing(t *testing.T) {
	future := time.Now().Add(30 * 24 * time.Hour)
	store := &rcTestStore{state: rcLifecycleStaleState(domain.RevenueCatGrant{Key: "comp_household_3"})}
	provider := &rcTestProvider{grants: []rc.Entitlement{{LookupKey: "comp_premium", ExpiresAt: &future}}}
	s, transport := newRCGatewayServiceWithStore(&rcGatewayAuthority{allowed: true, subject: "canonical-subject"}, store, provider, "active")

	_, err := s.Refresh(context.Background(), "target-user")
	require.NoError(t, err)
	require.Empty(t, transport.captures, "a live grant must not emit a lifecycle transition")
}

// TestRevenueCatLifecycleProviderOutageEmitsNothing pins the fail-closed boundary: when the
// provider read fails the projection is not refreshed, so no lifecycle event may claim it changed.
func TestRevenueCatLifecycleProviderOutageEmitsNothing(t *testing.T) {
	store := &rcTestStore{state: rcLifecycleStaleState(domain.RevenueCatGrant{Key: "comp_household_3"})}
	provider := &rcTestProvider{fetchErr: errors.New("provider unavailable")}
	s, transport := newRCGatewayServiceWithStore(&rcGatewayAuthority{allowed: true, subject: "canonical-subject"}, store, provider, "active")

	_, err := s.Refresh(context.Background(), "target-user")
	require.Error(t, err)
	require.Empty(t, transport.captures, "a failed provider read must not emit a lifecycle transition")
}

// TestRevenueCatLifecycleOptedOutEmitsNothing pins the consent boundary for the new events.
func TestRevenueCatLifecycleOptedOutEmitsNothing(t *testing.T) {
	store := &rcTestStore{state: rcLifecycleStaleState(domain.RevenueCatGrant{Key: "comp_household_3"})}
	s, transport := newRCGatewayServiceWithStore(&rcGatewayAuthority{allowed: false, subject: "canonical-subject"}, store, &rcTestProvider{}, "active")

	_, err := s.Refresh(context.Background(), "target-user")
	require.NoError(t, err)
	require.Empty(t, transport.captures, "an opted-out user must not emit a product event")
}

// TestRevenueCatLifecycleUnconfiguredAnalyticsIsNoOp pins the nil-safe wiring.
func TestRevenueCatLifecycleUnconfiguredAnalyticsIsNoOp(t *testing.T) {
	store := &rcTestStore{state: rcLifecycleStaleState(domain.RevenueCatGrant{Key: "comp_household_3"})}
	s := NewRevenueCatService(store, &rcTestProvider{}, rcStatusProfile{status: "active"})
	s.SetAnalytics(nil)

	_, err := s.Refresh(context.Background(), "target-user")
	require.NoError(t, err)
}
