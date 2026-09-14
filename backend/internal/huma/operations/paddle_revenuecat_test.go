package operations

import (
	"context"
	paddle "github.com/PaddleHQ/paddle-go-sdk/v4"
	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"testing"
)

type checkoutRCBilling struct {
	sub     *paddle.Subscription
	summary services.RevenueCatSummary
	err     error
}

func (b checkoutRCBilling) Resolve(context.Context, *domain.User) (*paddle.Subscription, services.RevenueCatSummary, error) {
	return b.sub, b.summary, b.err
}
func TestRevenueCatBillingControlsSurviveCompAndInactiveAccess(t *testing.T) {
	h, p, profiles := setupTestCheckoutHandler(t)
	profiles.On("GetUserByID", mock.Anything, "user").Return(&domain.User{ID: "user", SubscriptionStatus: "inactive"}, nil)
	h.WithRevenueCatBilling(checkoutRCBilling{sub: &paddle.Subscription{ID: "sub_paid", CustomerID: "ctm_paid"}, summary: services.RevenueCatSummary{Comp: &domain.RevenueCatAccess{Tier: "household_5"}}})
	p.On("GetBillingPortalURL", mock.Anything, "ctm_paid").Return("https://portal.example", nil)
	out, err := h.HandleGetBillingPortalURL(context.Background(), &GetBillingPortalURLInput{AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user"}})
	require.NoError(t, err)
	require.Equal(t, "https://portal.example", out.Body.PortalURL)
	p.On("CancelSubscription", mock.Anything, "sub_paid").Return(nil)
	p.On("GetSubscription", mock.Anything, "sub_paid").Return(&paddle.Subscription{ID: "sub_paid"}, nil)
	_, err = h.HandleCancelSubscription(context.Background(), &CancelSubscriptionInput{AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user"}})
	require.NoError(t, err)
	p.AssertExpectations(t)
}
func TestRevenueCatRetiresLegacyCheckoutCreation(t *testing.T) {
	h, p, _ := setupTestCheckoutHandler(t)
	h.WithRevenueCatBilling(checkoutRCBilling{})
	_, err := h.HandleCreateCheckoutSession(context.Background(), &CreateCheckoutInput{AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user"}})
	require.Error(t, err)
	require.Empty(t, p.Calls)
}

func TestRevenueCatBillingOutageIsUnavailableNotMissingSubscription(t *testing.T) {
	h, _, profiles := setupTestCheckoutHandler(t)
	profiles.On("GetUserByID", mock.Anything, "user").Return(&domain.User{ID: "user"}, nil)
	h.WithRevenueCatBilling(checkoutRCBilling{err: domain.ErrRevenueCatUnresolved})
	_, err := h.HandleCancelSubscription(context.Background(), &CancelSubscriptionInput{AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user"}})
	var status huma.StatusError
	require.ErrorAs(t, err, &status)
	require.Equal(t, 503, status.GetStatus())
}

func TestRevenueCatSeatPreflightUsesProposedPaidPlusConfirmedComp(t *testing.T) {
	for _, compTier := range []string{"premium", "household_5"} {
		t.Run(compTier, func(t *testing.T) {
			h, _, _, seats := setupTestCheckoutHandlerWithHouseholdRepo(t)
			h.WithRevenueCatBilling(checkoutRCBilling{sub: &paddle.Subscription{ID: "sub_paid"}, summary: services.RevenueCatSummary{Own: &domain.RevenueCatAccess{Tier: compTier}, Comp: &domain.RevenueCatAccess{Tier: compTier}}})
			seats.On("GetHouseholdForUser", mock.Anything, "owner").Return(&domain.Household{ID: "hh"}, true, true, nil)
			seats.On("CountActiveMembers", mock.Anything, "hh").Return(4, nil)
			seats.On("CountPendingInvites", mock.Anything, "hh").Return(0, nil)
			legacy := "household_5"
			err := h.checkDowngradeSeatCapacity(context.Background(), &domain.User{ID: "owner", ManualTierOverride: &legacy}, "household_3")
			if compTier == "premium" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
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
