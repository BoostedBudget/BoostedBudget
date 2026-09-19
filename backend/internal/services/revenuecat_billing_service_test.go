package services

import (
	"context"
	paddle "github.com/PaddleHQ/paddle-go-sdk/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/require"
	"testing"
)

type rcBillingSummary struct{ value RevenueCatSummary }

func (s rcBillingSummary) Summary(context.Context, *domain.User) RevenueCatSummary { return s.value }

type rcBillingPaddle struct {
	sub   *paddle.Subscription
	calls int
}

func (p *rcBillingPaddle) GetSubscription(_ context.Context, id string) (*paddle.Subscription, error) {
	p.calls++
	return p.sub, nil
}

type rcBillingTransactions struct {
	calls int
	subID string
}

type rcErasurePaddle struct{ ids []string }

func (p *rcErasurePaddle) GetSubscription(_ context.Context, id string) (*paddle.Subscription, error) {
	p.ids = append(p.ids, id)
	return &paddle.Subscription{ID: id, CustomerID: "ctm_" + id, Status: paddle.SubscriptionStatusActive}, nil
}
func TestRevenueCatBillingErasureResolvesAllActualSubscriptionsIncludingExpiredProjection(t *testing.T) {
	p := &rcErasurePaddle{}
	tx := &rcBillingTransactions{subID: "sub_one"}
	billing := []domain.RevenueCatBilling{
		{Store: "paddle", Status: "expired", StoreSubscriptionIdentifier: "txn_one"},
		{Store: "paddle", Status: "active", StoreSubscriptionIdentifier: "sub_two"},
		{Store: "paddle", Status: "active", StoreSubscriptionIdentifier: "sub_one"},
		{Store: "promotional", Status: "active"},
	}
	subs, err := NewRevenueCatBillingService(nil, p, tx).ResolveAllForErasure(context.Background(), billing)
	require.NoError(t, err)
	require.Len(t, subs, 2)
	require.ElementsMatch(t, []string{"sub_one", "sub_two"}, p.ids)
}

func (p *rcBillingTransactions) GetTransaction(context.Context, *paddle.GetTransactionRequest) (*paddle.Transaction, error) {
	p.calls++
	return &paddle.Transaction{SubscriptionID: &p.subID}, nil
}

func TestRevenueCatBillingResolvesTransactionIdentityAndDeduplicatesPlanChanges(t *testing.T) {
	p := &rcBillingPaddle{sub: &paddle.Subscription{ID: "sub_paid", CustomerID: "ctm_paid"}}
	tx := &rcBillingTransactions{subID: "sub_paid"}
	summary := rcBillingSummary{RevenueCatSummary{BillingAvailable: true, Comp: &domain.RevenueCatAccess{Tier: "household_5"}, Billing: []domain.RevenueCatBilling{
		{Store: "paddle", Status: "active", StoreSubscriptionIdentifier: "txn_one"},
		{Store: "paddle", Status: "active", StoreSubscriptionIdentifier: "sub_paid"},
	}}}
	service := NewRevenueCatBillingService(summary, p, tx)
	sub, got, err := service.Resolve(context.Background(), &domain.User{ID: "user"})
	require.NoError(t, err)
	require.Equal(t, "sub_paid", sub.ID)
	require.NotNil(t, got.Comp)
	require.Equal(t, 1, p.calls)
	require.Equal(t, 1, tx.calls)
}
func TestRevenueCatBillingNeverUsesLegacySubscriptionDuringOutage(t *testing.T) {
	p := &rcBillingPaddle{}
	legacy := "sub_legacy"
	_, _, err := NewRevenueCatBillingService(rcBillingSummary{}, p, nil).Resolve(context.Background(), &domain.User{ID: "user", PaddleSubscriptionID: &legacy})
	require.ErrorIs(t, err, domain.ErrRevenueCatUnresolved)
	require.Zero(t, p.calls)
}
func TestRevenueCatBillingDuplicateSubscriptionsRequireOperatorResolution(t *testing.T) {
	p := &rcBillingPaddle{}
	summary := rcBillingSummary{RevenueCatSummary{BillingAvailable: true, Billing: []domain.RevenueCatBilling{
		{Store: "paddle", Status: "in_billing_retry", StoreSubscriptionIdentifier: "sub_one"},
		{Store: "paddle", Status: "active", StoreSubscriptionIdentifier: "sub_two"},
	}}}
	_, _, err := NewRevenueCatBillingService(summary, p, nil).Resolve(context.Background(), &domain.User{ID: "user"})
	require.ErrorIs(t, err, ErrRevenueCatMultipleSubscriptions)
	require.Zero(t, p.calls)
}
func TestRevenueCatBillingCompOnlyHasNoPaidSubscription(t *testing.T) {
	summary := rcBillingSummary{RevenueCatSummary{BillingAvailable: true, Comp: &domain.RevenueCatAccess{Tier: "premium"}, Billing: []domain.RevenueCatBilling{{Store: "promotional", Status: "active"}}}}
	_, got, err := NewRevenueCatBillingService(summary, &rcBillingPaddle{}, nil).Resolve(context.Background(), &domain.User{ID: "user"})
	require.ErrorIs(t, err, ErrSubscriptionNotFound)
	require.NotNil(t, got.Comp)
}

func TestRevenueCatBillingManagementURLUsesResolvedPaddleRecord(t *testing.T) {
	p := &rcBillingPaddle{sub: &paddle.Subscription{ID: "sub_paid", CustomerID: "ctm_paid"}}
	summary := rcBillingSummary{RevenueCatSummary{BillingAvailable: true, Billing: []domain.RevenueCatBilling{
		{Store: "paddle", Status: "active", StoreSubscriptionIdentifier: "sub_paid", ManagementURL: "https://rc.example/manage"},
	}}}
	url, err := NewRevenueCatBillingService(summary, p, nil).ManagementURL(context.Background(), &domain.User{ID: "user"})
	require.NoError(t, err)
	require.Equal(t, "https://rc.example/manage", url)
}

func TestRevenueCatBillingManagementURLUnresolvedWhenRevenueCatHasNone(t *testing.T) {
	p := &rcBillingPaddle{sub: &paddle.Subscription{ID: "sub_paid", CustomerID: "ctm_paid"}}
	summary := rcBillingSummary{RevenueCatSummary{BillingAvailable: true, Billing: []domain.RevenueCatBilling{
		{Store: "paddle", Status: "active", StoreSubscriptionIdentifier: "sub_paid"},
	}}}
	_, err := NewRevenueCatBillingService(summary, p, nil).ManagementURL(context.Background(), &domain.User{ID: "user"})
	require.ErrorIs(t, err, domain.ErrRevenueCatUnresolved)
}

func TestRevenueCatBillingManagementURLIgnoresExpiredRecord(t *testing.T) {
	p := &rcBillingPaddle{sub: &paddle.Subscription{ID: "sub_paid", CustomerID: "ctm_paid"}}
	summary := rcBillingSummary{RevenueCatSummary{BillingAvailable: true, Billing: []domain.RevenueCatBilling{
		{Store: "paddle", Status: "expired", StoreSubscriptionIdentifier: "sub_paid", ManagementURL: "https://rc.example/stale"},
		{Store: "paddle", Status: "active", StoreSubscriptionIdentifier: "sub_paid", ManagementURL: "https://rc.example/live"},
	}}}
	url, err := NewRevenueCatBillingService(summary, p, nil).ManagementURL(context.Background(), &domain.User{ID: "user"})
	require.NoError(t, err)
	require.Equal(t, "https://rc.example/live", url)
}
