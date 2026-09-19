package operations

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

type revenueCatSubscriptionProfiles interface {
	GetUserByID(context.Context, string) (*domain.User, error)
}
type revenueCatSubscriptionRefresh interface {
	Refresh(context.Context, string) (domain.RevenueCatState, error)
}
type RevenueCatSubscriptionHandler struct {
	profiles   revenueCatSubscriptionProfiles
	summary    profileSubscriptionSummary
	refresh    revenueCatSubscriptionRefresh
	checkout   repository.RevenueCatCheckoutRepository
	tierConfig *config.TierConfig
}

func (h *RevenueCatSubscriptionHandler) WithCheckout(checkout repository.RevenueCatCheckoutRepository) *RevenueCatSubscriptionHandler {
	h.checkout = checkout
	return h
}

func (h *RevenueCatSubscriptionHandler) WithTierConfig(tierConfig *config.TierConfig) *RevenueCatSubscriptionHandler {
	h.tierConfig = tierConfig
	return h
}

type RevenueCatCheckoutOutput struct {
	Body domain.RevenueCatCheckoutIntent
}

type RevenueCatCheckoutReleaseInput struct {
	inputs.AuthenticatedInput
	IntentID string `path:"intent_id" format:"uuid"`
}
type RevenueCatCheckoutReleaseOutput struct {
	Body struct {
		Status string `json:"status"`
	}
}

// Release closes only this account's coordination lease, never a subscription.
func (h *RevenueCatSubscriptionHandler) ReleaseCheckout(ctx context.Context, input *RevenueCatCheckoutReleaseInput) (*RevenueCatCheckoutReleaseOutput, error) {
	if input.UserID == "" {
		return nil, huma.Error401Unauthorized("Sign in to close checkout.")
	}
	if h.checkout == nil {
		return nil, huma.Error503ServiceUnavailable("Checkout is unavailable. Try again shortly.")
	}
	if err := h.checkout.Release(ctx, input.UserID, input.IntentID); err != nil {
		return nil, huma.Error503ServiceUnavailable("Checkout could not be closed. Try again shortly.")
	}
	out := &RevenueCatCheckoutReleaseOutput{}
	out.Body.Status = "closed"
	return out, nil
}

type RevenueCatCheckoutBeginInput struct {
	inputs.AuthenticatedInput
	Body struct {
		Tier            string `json:"tier" enum:"premium,household_2,household_3,household_4,household_5"`
		BillingInterval string `json:"billing_interval" enum:"month,year"`
	}
}

func (h *RevenueCatSubscriptionHandler) BeginCheckout(ctx context.Context, input *RevenueCatCheckoutBeginInput) (*RevenueCatCheckoutOutput, error) {
	if input.UserID == "" {
		return nil, huma.Error401Unauthorized("Sign in to subscribe.")
	}
	if h.checkout == nil || h.refresh == nil || h.tierConfig == nil {
		return nil, huma.Error503ServiceUnavailable("Checkout is unavailable. Try again shortly.")
	}
	if _, err := h.tierConfig.GetTier(domain.TierName(input.Body.Tier)); err != nil {
		return nil, huma.Error400BadRequest("Choose a valid subscription tier.")
	}
	if err := validateBillingInterval(input.Body.BillingInterval); err != nil {
		return nil, err
	}
	pkg, ok := domain.RevenueCatPackageKey(input.Body.Tier, input.Body.BillingInterval)
	if !ok {
		return nil, huma.Error400BadRequest("Choose a valid subscription tier and billing interval.")
	}
	if _, err := h.refresh.Refresh(ctx, input.UserID); err != nil {
		return nil, huma.Error503ServiceUnavailable("Your existing subscription could not be checked. Try again shortly.")
	}
	intent, err := h.checkout.Begin(ctx, input.UserID, pkg)
	if errors.Is(err, domain.ErrRevenueCatExistingSubscription) {
		return nil, huma.Error409Conflict("You already have a paid subscription. Manage that subscription instead.")
	}
	if errors.Is(err, domain.ErrRevenueCatCheckoutInProgress) {
		return nil, huma.Error409Conflict("A checkout is already open for this account. Finish or close it before starting another.")
	}
	if err != nil {
		return nil, huma.Error503ServiceUnavailable("Checkout could not be started. Try again shortly.")
	}
	return &RevenueCatCheckoutOutput{Body: intent}, nil
}

func NewRevenueCatSubscriptionHandler(profiles revenueCatSubscriptionProfiles, summary profileSubscriptionSummary, refresh revenueCatSubscriptionRefresh) *RevenueCatSubscriptionHandler {
	return &RevenueCatSubscriptionHandler{profiles: profiles, summary: summary, refresh: refresh}
}

type RevenueCatSubscriptionInput struct{ inputs.AuthenticatedInput }
type RevenueCatSubscriptionOutput struct{ Body services.RevenueCatSummary }

func (h *RevenueCatSubscriptionHandler) Get(ctx context.Context, input *RevenueCatSubscriptionInput) (*RevenueCatSubscriptionOutput, error) {
	if input.UserID == "" {
		return nil, huma.Error401Unauthorized("Sign in to view your subscription.")
	}
	if h.profiles == nil || h.summary == nil {
		return nil, huma.Error503ServiceUnavailable("Subscription details are unavailable. Try again shortly.")
	}
	user, err := h.profiles.GetUserByID(ctx, input.UserID)
	if err != nil || user == nil || user.IsDeleted {
		return nil, huma.Error404NotFound("Account not found.")
	}
	return &RevenueCatSubscriptionOutput{Body: h.summary.Summary(ctx, user)}, nil
}

// Refresh uses only the authenticated identity. SDK purchase results and browser
// claims are never accepted as evidence of access.
func (h *RevenueCatSubscriptionHandler) Refresh(ctx context.Context, input *RevenueCatSubscriptionInput) (*RevenueCatSubscriptionOutput, error) {
	if input.UserID == "" {
		return nil, huma.Error401Unauthorized("Sign in to refresh your subscription.")
	}
	if h.refresh == nil {
		return nil, huma.Error503ServiceUnavailable("Subscription synchronization is unavailable. Try again shortly.")
	}
	if _, err := h.refresh.Refresh(ctx, input.UserID); err != nil {
		return nil, huma.Error503ServiceUnavailable("Your subscription could not be confirmed yet. Try again shortly.")
	}
	return h.Get(ctx, input)
}

func RegisterRevenueCatSubscriptionRoutes(api huma.API, handler *RevenueCatSubscriptionHandler) {
	huma.Register(api, huma.Operation{OperationID: "releaseRevenueCatCheckout", Method: "DELETE", Path: "/api/v1/subscription/checkout-intents/{intent_id}", Summary: "Close this account's checkout coordination lease", Tags: []string{"Subscription"}, Security: []map[string][]string{{"clerk": {}}}}, handler.ReleaseCheckout)
	huma.Register(api, huma.Operation{OperationID: "beginRevenueCatCheckout", Method: "POST", Path: "/api/v1/subscription/checkout-intents", Summary: "Coordinate a RevenueCat checkout for the signed-in account", Tags: []string{"Subscription"}, Security: []map[string][]string{{"clerk": {}}}}, handler.BeginCheckout)
	huma.Register(api, huma.Operation{OperationID: "getRevenueCatSubscription", Method: "GET", Path: "/api/v1/subscription/authority", Summary: "Get confirmed subscription access and separate billing details", Tags: []string{"Subscription"}, Security: []map[string][]string{{"clerk": {}}}}, handler.Get)
	huma.Register(api, huma.Operation{OperationID: "refreshRevenueCatSubscription", Method: "POST", Path: "/api/v1/subscription/authority/refresh", Summary: "Refresh subscription confirmation", Tags: []string{"Subscription"}, Security: []map[string][]string{{"clerk": {}}}}, handler.Refresh)
}
