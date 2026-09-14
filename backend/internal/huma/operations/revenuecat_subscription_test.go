package operations

import (
	"context"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/require"
)

type subscriptionRCProfiles struct{ id string }

func (p *subscriptionRCProfiles) GetUserByID(_ context.Context, id string) (*domain.User, error) {
	p.id = id
	return &domain.User{ID: id}, nil
}

type subscriptionRCCheckout struct {
	userID     string
	releasedID string
	err        error
}

func (r *subscriptionRCCheckout) Begin(_ context.Context, userID string) (domain.RevenueCatCheckoutIntent, error) {
	r.userID = userID
	return domain.RevenueCatCheckoutIntent{ID: "intent", Offering: "web", ExpiresAt: time.Now().Add(time.Minute)}, r.err
}
func (r *subscriptionRCCheckout) Release(_ context.Context, userID, id string) error {
	r.userID = userID
	r.releasedID = id
	return r.err
}

func TestRevenueCatCheckoutAdmissionRefreshesServerStateBeforeOpening(t *testing.T) {
	r := &subscriptionRCRefresh{}
	c := &subscriptionRCCheckout{}
	h := NewRevenueCatSubscriptionHandler(&subscriptionRCProfiles{}, profileRevenueCatSummary{}, r).WithCheckout(c)
	out, err := h.BeginCheckout(context.Background(), &RevenueCatSubscriptionInput{AuthenticatedInput: inputs.AuthenticatedInput{UserID: "authenticated"}})
	require.NoError(t, err)
	require.Equal(t, "authenticated", r.id)
	require.Equal(t, "authenticated", c.userID)
	require.Equal(t, "web", out.Body.Offering)
	c.err = domain.ErrRevenueCatExistingSubscription
	_, err = h.BeginCheckout(context.Background(), &RevenueCatSubscriptionInput{AuthenticatedInput: inputs.AuthenticatedInput{UserID: "authenticated"}})
	var status huma.StatusError
	require.ErrorAs(t, err, &status)
	require.Equal(t, 409, status.GetStatus())
}

func TestRevenueCatCheckoutAdmissionDoesNotOpenDuringRefreshFailure(t *testing.T) {
	c := &subscriptionRCCheckout{}
	h := NewRevenueCatSubscriptionHandler(&subscriptionRCProfiles{}, profileRevenueCatSummary{}, &subscriptionRCRefresh{err: domain.ErrRevenueCatUnresolved}).WithCheckout(c)
	_, err := h.BeginCheckout(context.Background(), &RevenueCatSubscriptionInput{AuthenticatedInput: inputs.AuthenticatedInput{UserID: "authenticated"}})
	require.Error(t, err)
	require.Empty(t, c.userID)
}

func TestRevenueCatCheckoutReleaseUsesAuthenticatedAccount(t *testing.T) {
	c := &subscriptionRCCheckout{}
	h := NewRevenueCatSubscriptionHandler(nil, nil, nil).WithCheckout(c)
	_, err := h.ReleaseCheckout(context.Background(), &RevenueCatCheckoutReleaseInput{AuthenticatedInput: inputs.AuthenticatedInput{UserID: "authenticated"}, IntentID: "intent"})
	require.NoError(t, err)
	require.Equal(t, "authenticated", c.userID)
	require.Equal(t, "intent", c.releasedID)
}

type subscriptionRCRefresh struct {
	id  string
	err error
}

func (r *subscriptionRCRefresh) Refresh(_ context.Context, id string) (domain.RevenueCatState, error) {
	r.id = id
	return domain.RevenueCatState{}, r.err
}

func TestRevenueCatSubscriptionSummaryPreservesUnknownState(t *testing.T) {
	p := &subscriptionRCProfiles{}
	h := NewRevenueCatSubscriptionHandler(p, profileRevenueCatSummary{}, &subscriptionRCRefresh{})
	out, err := h.Get(context.Background(), &RevenueCatSubscriptionInput{AuthenticatedInput: inputs.AuthenticatedInput{UserID: "authenticated-user"}})
	require.NoError(t, err)
	require.False(t, out.Body.AccessResolved)
	require.Equal(t, "authenticated-user", p.id)
}
func TestRevenueCatSubscriptionRefreshRequiresAuthentication(t *testing.T) {
	r := &subscriptionRCRefresh{}
	h := NewRevenueCatSubscriptionHandler(&subscriptionRCProfiles{}, profileRevenueCatSummary{}, r)
	_, err := h.Refresh(context.Background(), &RevenueCatSubscriptionInput{})
	var status huma.StatusError
	require.ErrorAs(t, err, &status)
	require.Equal(t, 401, status.GetStatus())
	require.Empty(t, r.id)
}
func TestRevenueCatSubscriptionRefreshDoesNotAcceptClientAccess(t *testing.T) {
	r := &subscriptionRCRefresh{}
	h := NewRevenueCatSubscriptionHandler(&subscriptionRCProfiles{}, profileRevenueCatSummary{summary: services.RevenueCatSummary{AccessResolved: false}}, r)
	out, err := h.Refresh(context.Background(), &RevenueCatSubscriptionInput{AuthenticatedInput: inputs.AuthenticatedInput{UserID: "authenticated-user"}})
	require.NoError(t, err)
	require.Equal(t, "authenticated-user", r.id)
	require.False(t, out.Body.AccessResolved, "a successful refresh call is not itself access")
}
