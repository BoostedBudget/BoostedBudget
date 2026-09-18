//go:build integration

package services

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	rc "github.com/maxnussbaum/BoostedBudget/internal/integration/revenuecat"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/require"
)

// rcRefundProvider returns a paid grant until the refund lands, then returns nothing — exactly
// what RevenueCat's authoritative active_entitlements read does once a refund removes the
// entitlement.
type rcRefundProvider struct{ refunded bool }

func (p *rcRefundProvider) EnsureCustomer(context.Context, string) error { return nil }
func (p *rcRefundProvider) FetchEntitlements(context.Context, string) ([]rc.Entitlement, error) {
	if p.refunded {
		return nil, nil
	}
	end := time.Now().Add(30 * 24 * time.Hour)
	return []rc.Entitlement{{LookupKey: "paid_premium", ExpiresAt: &end}}, nil
}
func (p *rcRefundProvider) FetchSubscriptions(context.Context, string) ([]rc.Subscription, error) {
	return nil, nil
}
func (p *rcRefundProvider) Grant(context.Context, string, string, *time.Time) error { return nil }
func (p *rcRefundProvider) Revoke(context.Context, string, string) error             { return nil }

// TestRevenueCatRefundRevokesAccess is the issue #1652 AC6 proof (propagation half). A refund
// arrives as a RevenueCat webhook, which enqueues RevenueCatSyncWorker -> RevenueCatService.Refresh
// (the exact call this test makes). Refresh re-reads RevenueCat's authoritative entitlements; once
// the refund has removed the entitlement the projection is empty, access resolves inactive, and
// the retention/Plaid-cleanup clock is armed by the same Save path.
func TestRevenueCatRefundRevokesAccess(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL required")
	}
	fx := newRCConcurrencyFixture(t, url)
	ctx := context.Background()
	p := &rcRefundProvider{}
	s := NewRevenueCatService(fx.repo, p, rcPoolProfile{fx.pool})

	_, err := s.Refresh(ctx, fx.id)
	require.NoError(t, err)
	var status string
	require.NoError(t, fx.pool.QueryRow(ctx, `SELECT subscription_status FROM users WHERE id=$1`, fx.id).Scan(&status))
	require.Equal(t, "active", status, "the paid entitlement must grant access before the refund")

	p.refunded = true
	_, err = s.Refresh(ctx, fx.id)
	require.NoError(t, err)

	var started, marker *time.Time
	require.NoError(t, fx.pool.QueryRow(ctx,
		`SELECT subscription_status, retention_started_at, plaid_cleanup_pending_at FROM users WHERE id=$1`,
		fx.id).Scan(&status, &started, &marker))
	require.Equal(t, "inactive", status, "a refund must revoke access")
	require.NotNil(t, started, "the refund must arm the retention clock")
	require.NotNil(t, marker, "the refund must arm the Plaid cleanup marker")
}

// newRCRefundRevocationIntegration seeds a persisted paid projection and drives the refund through
// the real repository + the REAL Gateway, so the revocation event's contract validation and
// canonical-identity resolution are exercised rather than mocked.
func newRCRefundRevocationIntegration(t *testing.T) (rcConcurrencyFixture, *rcRefundProvider, *rcGatewayTransport, *RevenueCatService) {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL required")
	}
	fx := newRCConcurrencyFixture(t, url)
	ctx := context.Background()
	require.NoError(t, fx.repo.WithCustomer(ctx, fx.id, func(s repository.RevenueCatSession) error {
		return s.Save(ctx, domain.RevenueCatState{
			AccessUpdatedAt: time.Now(),
			Grants:          []domain.RevenueCatGrant{{Key: "paid_premium"}},
		})
	}))
	transport := &rcGatewayTransport{}
	p := &rcRefundProvider{}
	s := NewRevenueCatService(fx.repo, p, rcLifecycleProfile{fixture: fx})
	s.SetAnalytics(phclient.NewGateway(transport, &rcGatewayAuthority{allowed: true, subject: "canonical-subject"}, slog.Default()))
	return fx, p, transport, s
}

// TestRevenueCatRefundRevocationEmitsThroughRealDBAndGateway is the issue #1652 AC6 proof
// (instrumentation half). The prior paid grant is persisted into revenuecat_customers.grants, read
// back by the refresh's prior-grant snapshot, and classified after the projection flips inactive —
// the half a mock store cannot prove, because the snapshot reads the column the Save overwrites.
func TestRevenueCatRefundRevocationEmitsThroughRealDBAndGateway(t *testing.T) {
	fx, p, transport, s := newRCRefundRevocationIntegration(t)
	ctx := context.Background()

	_, err := s.Refresh(ctx, fx.id)
	require.NoError(t, err)
	require.Empty(t, transport.captures, "an unrefunded paid grant is not a revocation")

	p.refunded = true
	_, err = s.Refresh(ctx, fx.id)
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

	var status string
	var started, marker *time.Time
	require.NoError(t, fx.pool.QueryRow(ctx,
		`SELECT subscription_status, retention_started_at, plaid_cleanup_pending_at FROM users WHERE id=$1`,
		fx.id).Scan(&status, &started, &marker))
	require.Equal(t, "inactive", status, "a refund must revoke access")
	require.NotNil(t, started, "the refund must arm the retention clock")
	require.NotNil(t, marker, "the refund must arm the Plaid cleanup marker")
}

// TestRevenueCatRefundRevocationReplayEmitsNoDuplicate is the plan's failure QA: re-running the same
// revocation (a redelivered refund webhook, or the repair worker) must not re-fire the event. The
// cached projection is deliberately aged so the refresh actually re-runs, exercising the
// priorStatus guard rather than the freshness short-circuit.
func TestRevenueCatRefundRevocationReplayEmitsNoDuplicate(t *testing.T) {
	fx, p, transport, s := newRCRefundRevocationIntegration(t)
	ctx := context.Background()

	p.refunded = true
	_, err := s.Refresh(ctx, fx.id)
	require.NoError(t, err)
	require.Equal(t, []string{phclient.EventRevenueCatAccessRevoked}, rcCapturedNames(transport)[1:2])

	_, err = fx.pool.Exec(ctx,
		`UPDATE revenuecat_customers SET access_updated_at = now() - interval '1 hour' WHERE user_id=$1`, fx.id)
	require.NoError(t, err)
	before := len(transport.captures)
	_, err = s.Refresh(ctx, fx.id)
	require.NoError(t, err)
	require.Equal(t, before, len(transport.captures),
		"a replay of an already-inactive revocation must not re-emit the revocation event")
}
