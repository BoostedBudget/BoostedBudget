//go:build integration

package services

// #1652 todo 8 — the grant-lifecycle transitions against a real Postgres projection.
//
// The unit tests in revenuecat_grant_lifecycle_test.go drive the real Gateway over an in-memory
// store. These two drive the SAME transition through the real RevenueCatRepository: the prior grant
// is persisted into revenuecat_customers.grants, read back by the refresh's prior-grant snapshot,
// and classified after the projection flips inactive. That is the half a mock store cannot prove —
// the snapshot reads the column the Save is about to overwrite.

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

// rcLifecycleProfile resolves the persisted subscription status the lifecycle classifier compares
// against. rcPoolProfile reads only clerk_user_id, so it cannot serve this test.
type rcLifecycleProfile struct{ fixture rcConcurrencyFixture }

func (p rcLifecycleProfile) GetUserByID(ctx context.Context, id string) (*domain.User, error) {
	u := &domain.User{ID: id}
	err := p.fixture.pool.QueryRow(ctx,
		`SELECT clerk_user_id, subscription_status FROM users WHERE id=$1`, id).
		Scan(&u.ClerkUserID, &u.SubscriptionStatus)
	return u, err
}

// rcEmptyGrantProvider returns no entitlements — the fresh provider read that drops a lapsed grant.
type rcEmptyGrantProvider struct{}

func (rcEmptyGrantProvider) EnsureCustomer(context.Context, string) error { return nil }
func (rcEmptyGrantProvider) FetchEntitlements(context.Context, string) ([]rc.Entitlement, error) {
	return nil, nil
}
func (rcEmptyGrantProvider) FetchSubscriptions(context.Context, string) ([]rc.Subscription, error) {
	return nil, nil
}
func (rcEmptyGrantProvider) Grant(context.Context, string, string, *time.Time) error { return nil }
func (rcEmptyGrantProvider) Revoke(context.Context, string, string) error            { return nil }

func newRCGrantLifecycleIntegration(t *testing.T, key string) (rcConcurrencyFixture, *rcGatewayTransport, *RevenueCatService) {
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
			Grants:          []domain.RevenueCatGrant{{Key: key}},
		})
	}))
	transport := &rcGatewayTransport{}
	s := NewRevenueCatService(fx.repo, rcEmptyGrantProvider{}, rcLifecycleProfile{fixture: fx})
	s.SetAnalytics(phclient.NewGateway(transport, &rcGatewayAuthority{allowed: true, subject: "canonical-subject"}, slog.Default()))
	return fx, transport, s
}

func TestRevenueCatCompExpiryIntegrationThroughRealDBAndGateway(t *testing.T) {
	fx, transport, s := newRCGrantLifecycleIntegration(t, "comp_household_3")

	_, err := s.Refresh(context.Background(), fx.id)
	require.NoError(t, err)
	require.Equal(t,
		[]string{phclient.EventRevenueCatCompExpired, phclient.EventRevenueCatAccessRevoked, phclient.EventRevenueCatAccessInactive, phclient.EventRevenueCatRetentionArmed},
		rcCapturedNames(transport))
	require.Equal(t, map[string]interface{}{"entitlement": "comp_household_3"}, transport.captures[0].props)
	require.Equal(t, map[string]interface{}{"entitlement": "comp_household_3", "source": "webhook"}, transport.captures[1].props)
	for _, capture := range transport.captures {
		require.Equal(t, "canonical-subject", capture.subject)
		requireLifecycleCaptureHasNoForbiddenKeys(t, capture)
	}
}

func TestRevenueCatPaidGrantLapseIntegrationThroughRealDBAndGateway(t *testing.T) {
	fx, transport, s := newRCGrantLifecycleIntegration(t, "paid_premium")

	_, err := s.Refresh(context.Background(), fx.id)
	require.NoError(t, err)
	require.Equal(t,
		[]string{phclient.EventRevenueCatPaidGrantLapsed, phclient.EventRevenueCatAccessRevoked, phclient.EventRevenueCatAccessInactive, phclient.EventRevenueCatRetentionArmed},
		rcCapturedNames(transport))
	require.Equal(t, map[string]interface{}{"entitlement": "paid_premium"}, transport.captures[0].props)
	require.Equal(t, map[string]interface{}{"entitlement": "paid_premium", "source": "refund"}, transport.captures[1].props)
}
