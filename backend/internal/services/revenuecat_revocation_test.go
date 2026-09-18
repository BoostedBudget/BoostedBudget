package services

// #1652 todo 9 — the refund/revocation transition.
//
// A refund arrives as a RevenueCat CANCELLATION webhook, which enqueues RevenueCatSyncWorker ->
// RevenueCatService.Refresh (the exact call these tests drive). The refresh re-reads RevenueCat's
// authoritative entitlements; once the refund has removed the entitlement the projection is empty,
// access resolves inactive, and revenuecat_access_revoked names the entitlement that was revoked
// and the bounded source of the revocation.
//
// Every case drives the REAL Gateway + AuthorityResolver, not a mock: a contract violation (an
// unbounded value, a property key containing an identifier, a missing required property) is
// silently dropped by the gateway in production, and a mock-backed test would stay green through
// exactly that defect.
//
// Bootstrap/import boundary (todo 9's D2): cmd/revenuecat-bootstrap is a short-lived prelaunch CLI
// with no analytics client, and the Gateway's contract is per-subject — CaptureEvent resolves ONE
// user's canonical Clerk identity and consent (repository/analytics_authority_repository.go:13-23).
// A batch-completion event has no single subject, so there is deliberately NO per-batch product
// event and NO event per imported user. The product half of the import — "a user's access
// established by import" — is emitted where it actually happens: Apply persists durable operations
// (repository/revenuecat_bootstrap_repository.go:91-95) and processing them emits the
// grant-lifecycle events through the same Gateway, once per user.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/stretchr/testify/require"
)

func TestRevenueCatRefundRevocationThroughRealGateway(t *testing.T) {
	// The prior projection was paid-backed and the fresh authoritative read no longer reports it —
	// exactly what RevenueCat returns once a refund has removed the entitlement.
	store := &rcTestStore{state: rcLifecycleStaleState(domain.RevenueCatGrant{Key: "paid_premium"})}
	s, transport := newRCGatewayServiceWithStore(&rcGatewayAuthority{allowed: true, subject: "canonical-subject"}, store, &rcTestProvider{}, "active")

	_, err := s.Refresh(context.Background(), "target-user")
	require.NoError(t, err)
	require.Equal(t,
		[]string{phclient.EventRevenueCatPaidGrantLapsed, phclient.EventRevenueCatAccessRevoked, phclient.EventRevenueCatAccessInactive, phclient.EventRevenueCatRetentionArmed},
		rcCapturedNames(transport),
		"a refund must name the revoked paid entitlement before the generic access outcome")
	require.Equal(t,
		map[string]interface{}{"entitlement": "paid_premium", "source": "refund"},
		transport.captures[1].props,
		"a paid entitlement removed by the provider read is the refund source")
	for _, capture := range transport.captures {
		require.Equal(t, "canonical-subject", capture.subject)
		requireLifecycleCaptureHasNoForbiddenKeys(t, capture)
	}
}

func TestRevenueCatNonPaidRevocationIsWebhookThroughRealGateway(t *testing.T) {
	store := &rcTestStore{state: rcLifecycleStaleState(domain.RevenueCatGrant{Key: "comp_household_3"})}
	s, transport := newRCGatewayServiceWithStore(&rcGatewayAuthority{allowed: true, subject: "canonical-subject"}, store, &rcTestProvider{}, "active")

	_, err := s.Refresh(context.Background(), "target-user")
	require.NoError(t, err)
	require.Equal(t, phclient.EventRevenueCatAccessRevoked, rcCapturedNames(transport)[1])
	require.Equal(t,
		map[string]interface{}{"entitlement": "comp_household_3", "source": "webhook"},
		transport.captures[1].props,
		"a non-paid entitlement removed by a provider state change is the webhook source")
	requireLifecycleCaptureHasNoForbiddenKeys(t, transport.captures[1])
}

func TestRevenueCatAdminRevokeEmitsRevocationThroughRealGateway(t *testing.T) {
	store := &rcTestStore{ops: map[string]domain.RevenueCatOperation{
		"op": {ID: "op", UserID: "target-user", Class: "comp", Key: "comp_premium", Revoke: true, Stage: "pending"},
	}}
	s, transport := newRCGatewayServiceWithStore(&rcGatewayAuthority{allowed: true, subject: "canonical-subject"}, store, &rcTestProvider{}, "active")

	require.NoError(t, s.ProcessOperation(context.Background(), "target-user", "op"))
	require.Equal(t,
		[]string{phclient.EventRevenueCatOperationApplied, phclient.EventRevenueCatAccessRevoked},
		rcCapturedNames(transport),
		"an applied admin revoke must emit the revocation alongside the operation-queue event")
	require.Equal(t,
		map[string]interface{}{"entitlement": "comp_premium", "source": "admin"},
		transport.captures[1].props)
	for _, capture := range transport.captures {
		require.Equal(t, "canonical-subject", capture.subject)
		requireLifecycleCaptureHasNoForbiddenKeys(t, capture)
	}
}

// TestRevenueCatFailedAdminRevokeEmitsNoRevocation pins the fail-closed boundary: a revoke that did
// not apply did not revoke access, so it must not emit the revocation event.
func TestRevenueCatFailedAdminRevokeEmitsNoRevocation(t *testing.T) {
	store := &rcTestStore{ops: map[string]domain.RevenueCatOperation{
		"op": {ID: "op", UserID: "target-user", Class: "comp", Key: "comp_premium", Revoke: true, Stage: "pending"},
	}}
	s, transport := newRCGatewayServiceWithStore(&rcGatewayAuthority{allowed: true, subject: "canonical-subject"}, store, &rcTestProvider{fetchErr: errors.New("provider unavailable")}, "active")

	require.Error(t, s.ProcessOperation(context.Background(), "target-user", "op"))
	for _, name := range rcCapturedNames(transport) {
		require.NotEqual(t, phclient.EventRevenueCatAccessRevoked, name, "a failed revoke must not claim access was revoked")
	}
}

// TestRevenueCatBootstrapImportedTrialEmitsThroughOperationQueue is todo 9's bootstrap proof. The
// CLI itself has no analytics client and no single subject for a batch, so it emits nothing; the
// imported user's access signal is emitted by the operation queue the import persists.
func TestRevenueCatBootstrapImportedTrialEmitsThroughOperationQueue(t *testing.T) {
	end := time.Now().Add(60 * 24 * time.Hour)
	planned, err := domain.PlanRevenueCatBootstrap(&domain.User{ID: "target-user", SubscriptionStatus: "trial", TrialEndsAt: &end}, time.Now())
	require.NoError(t, err)
	require.Len(t, planned, 1)
	require.Equal(t, "trial", planned[0].Class)
	require.Equal(t, "trial_premium", planned[0].Key)

	store := &rcTestStore{ops: map[string]domain.RevenueCatOperation{
		"op": {ID: "op", UserID: "target-user", Class: planned[0].Class, Key: planned[0].Key, ExpiresAt: planned[0].ExpiresAt, Stage: "pending"},
	}}
	s, transport := newRCGatewayServiceWithStore(&rcGatewayAuthority{allowed: true, subject: "canonical-subject"}, store, &rcTestProvider{}, "inactive")

	require.NoError(t, s.ProcessOperation(context.Background(), "target-user", "op"))
	require.Equal(t,
		[]string{phclient.EventRevenueCatOperationApplied, phclient.EventRevenueCatTrialStarted},
		rcCapturedNames(transport),
		"a bootstrap-imported trial's access signal is emitted once by the operation queue, never by the CLI")
	require.Equal(t, map[string]interface{}{"entitlement": "trial_premium"}, transport.captures[1].props)
	requireLifecycleCaptureHasNoForbiddenKeys(t, transport.captures[1])
}
