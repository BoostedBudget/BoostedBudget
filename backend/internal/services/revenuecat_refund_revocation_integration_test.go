//go:build integration

package services

import (
	"context"
	"os"
	"testing"
	"time"

	rc "github.com/maxnussbaum/BoostedBudget/internal/integration/revenuecat"
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
