package operations

import (
	"context"
	"testing"

	paddle "github.com/PaddleHQ/paddle-go-sdk/v4"
	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type checkoutRCBilling struct {
	sub     *paddle.Subscription
	summary services.RevenueCatSummary
	err     error
	url     string
	urlErr  error
}

func (b checkoutRCBilling) Resolve(context.Context, *domain.User) (*paddle.Subscription, services.RevenueCatSummary, error) {
	return b.sub, b.summary, b.err
}

func (b checkoutRCBilling) ManagementURL(context.Context, *domain.User) (string, error) {
	return b.url, b.urlErr
}

// The billing portal is sourced from the RevenueCat-resolved management URL, and it
// remains reachable when access is comp/inactive (billing self-service is independent
// of the access authority).
func TestPaddleRevenueCatBillingPortalUsesResolvedManagementURL(t *testing.T) {
	h, _, profiles := setupTestCheckoutHandler(t)
	profiles.On("GetUserByID", mock.Anything, "user").Return(&domain.User{ID: "user", SubscriptionStatus: "inactive"}, nil)
	h.WithRevenueCatBilling(checkoutRCBilling{
		sub:     &paddle.Subscription{ID: "sub_paid", CustomerID: "ctm_paid"},
		summary: services.RevenueCatSummary{Comp: &domain.RevenueCatAccess{Tier: "household_5"}},
		url:     "https://rc.example/manage",
	})
	out, err := h.HandleGetBillingPortalURL(context.Background(), &GetBillingPortalURLInput{AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user"}})
	require.NoError(t, err)
	require.Equal(t, "https://rc.example/manage", out.Body.PortalURL)
}

// Failure mode: an unresolved management URL is a clean 503, never a nil deref and
// never a fallback Paddle mutation.
func TestPaddleRevenueCatBillingPortalUnavailableIsClean503(t *testing.T) {
	h, p, profiles := setupTestCheckoutHandler(t)
	profiles.On("GetUserByID", mock.Anything, "user").Return(&domain.User{ID: "user"}, nil)
	h.WithRevenueCatBilling(checkoutRCBilling{urlErr: domain.ErrRevenueCatUnresolved})
	_, err := h.HandleGetBillingPortalURL(context.Background(), &GetBillingPortalURLInput{AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user"}})
	var status huma.StatusError
	require.ErrorAs(t, err, &status)
	require.Equal(t, 503, status.GetStatus())
	require.Empty(t, p.Calls)
}

func TestPaddleRevenueCatBillingPortalMissingSubscriptionIs404(t *testing.T) {
	h, _, profiles := setupTestCheckoutHandler(t)
	profiles.On("GetUserByID", mock.Anything, "user").Return(&domain.User{ID: "user"}, nil)
	h.WithRevenueCatBilling(checkoutRCBilling{urlErr: services.ErrSubscriptionNotFound})
	_, err := h.HandleGetBillingPortalURL(context.Background(), &GetBillingPortalURLInput{AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user"}})
	var status huma.StatusError
	require.ErrorAs(t, err, &status)
	require.Equal(t, 404, status.GetStatus())
}

func TestRevenueCatRetiresLegacyCheckoutCreation(t *testing.T) {
	h, p, _ := setupTestCheckoutHandler(t)
	h.WithRevenueCatBilling(checkoutRCBilling{})
	_, err := h.HandleCreateCheckoutSession(context.Background(), &CreateCheckoutInput{AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user"}})
	require.Error(t, err)
	require.Empty(t, p.Calls)
}

func TestRevenueCatCurrentShowsActualPaidTierAndSeparateComp(t *testing.T) {
	h, _, profiles := setupTestCheckoutHandler(t)
	profiles.On("GetUserByID", mock.Anything, "user").Return(&domain.User{ID: "user", SubscriptionTier: "household_5"}, nil)
	tier, err := h.tierConfig.GetTier(domain.TierPremium)
	require.NoError(t, err)
	sub := &paddle.Subscription{ID: "sub_paid", Status: paddle.SubscriptionStatusActive}
	// Fixtures use the production response price shape rather than the user's access tier.
	sub.Items = []paddle.SubscriptionItem{{Price: paddle.Price{ID: *tier.Pricing.PaddlePriceIDMonthly}}}
	h.WithRevenueCatBilling(checkoutRCBilling{sub: sub, summary: services.RevenueCatSummary{Comp: &domain.RevenueCatAccess{Tier: "household_5"}}})
	out, err := h.HandleGetCurrentSubscription(context.Background(), &GetCurrentSubscriptionInput{AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user"}})
	require.NoError(t, err)
	require.Equal(t, "premium", out.Body.Tier)
	require.True(t, out.Body.IsComped)
	require.NotNil(t, out.Body.Authority)
}
