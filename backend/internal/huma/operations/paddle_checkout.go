package operations

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// PaddleCheckoutHandler handles Paddle checkout and subscription management operations.
type PaddleCheckoutHandler struct {
	paddleService services.PaddleService
	profileRepo   repository.ProfileRepository
	config        *config.Config
	tierConfig    *config.TierConfig
	logger        *slog.Logger
}

// PaddleCheckoutHandlerDeps contains dependencies for PaddleCheckoutHandler.
type PaddleCheckoutHandlerDeps struct {
	PaddleService services.PaddleService
	ProfileRepo   repository.ProfileRepository
	Config        *config.Config
	TierConfig    *config.TierConfig
	Logger        *slog.Logger
}

// NewPaddleCheckoutHandler creates a new PaddleCheckoutHandler instance.
func NewPaddleCheckoutHandler(deps PaddleCheckoutHandlerDeps) *PaddleCheckoutHandler {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &PaddleCheckoutHandler{
		paddleService: deps.PaddleService,
		profileRepo:   deps.ProfileRepo,
		config:        deps.Config,
		tierConfig:    deps.TierConfig,
		logger:        logger,
	}
}

// --- Input/Output Types ---

// CreateCheckoutInput represents the input for creating a checkout session.
type CreateCheckoutInput struct {
	Body struct {
		Tier            string `json:"tier" doc:"Subscription tier (premium, household_2, etc.)" example:"premium" minLength:"1"`
		BillingInterval string `json:"billing_interval" doc:"Billing interval (month or year)" example:"month" enum:"month,year"`
	}
}

// CheckoutOutput represents the response for checkout session creation.
type CheckoutOutput struct {
	Body struct {
		CheckoutURL   string `json:"checkout_url" doc:"URL to open Paddle checkout (Paddle.js Overlay or redirect)"`
		TransactionID string `json:"transaction_id" doc:"Paddle transaction ID for tracking"`
	}
}

// PreviewUpdateInput represents the input for previewing a subscription update.
type PreviewUpdateInput struct {
	Body struct {
		NewTier         string `json:"new_tier" doc:"New subscription tier to preview" example:"household_2" minLength:"1"`
		BillingInterval string `json:"billing_interval" doc:"Billing interval for new tier (month or year)" example:"month" enum:"month,year"`
	}
}

// PreviewUpdateOutput represents the response for subscription update preview.
type PreviewUpdateOutput struct {
	Body struct {
		ProrationAmount string `json:"proration_amount" doc:"Amount to charge (positive) or credit (negative) immediately" example:"5.00"`
		Currency        string `json:"currency" doc:"Currency code (USD, EUR, etc.)" example:"USD"`
		NextChargeDate  string `json:"next_charge_date" doc:"Date of next regular charge" example:"2026-02-26"`
		NextChargeAmt   string `json:"next_charge_amount" doc:"Amount of next regular charge" example:"12.99"`
	}
}

// UpdateSubscriptionInput represents the input for updating a subscription.
type UpdateSubscriptionInput struct {
	Body struct {
		NewTier              string `json:"new_tier" doc:"New subscription tier" example:"household_2" minLength:"1"`
		BillingInterval      string `json:"billing_interval" doc:"Billing interval (month or year)" example:"month" enum:"month,year"`
		ProrationBillingMode string `json:"proration_billing_mode,omitempty" doc:"Proration mode (prorated_immediately, full_immediately, full_next_billing_period)" example:"prorated_immediately" enum:"prorated_immediately,full_immediately,full_next_billing_period"`
	}
}

// UpdateSubscriptionOutput represents the response for subscription update.
type UpdateSubscriptionOutput struct {
	Body struct {
		SubscriptionID string `json:"subscription_id" doc:"Paddle subscription ID" example:"sub_01h1234567890"`
		Status         string `json:"status" doc:"Subscription status" example:"active"`
		Message        string `json:"message" doc:"Human-readable update message" example:"Subscription upgraded successfully"`
	}
}

// CancelSubscriptionInput represents the input for canceling a subscription.
type CancelSubscriptionInput struct {
	// No body needed - uses user_id from JWT context
}

// CancelSubscriptionOutput represents the response for subscription cancellation.
type CancelSubscriptionOutput struct {
	Body struct {
		SubscriptionID string `json:"subscription_id" doc:"Cancelled Paddle subscription ID" example:"sub_01h1234567890"`
		Message        string `json:"message" doc:"Cancellation confirmation message" example:"Subscription will be cancelled at end of billing period"`
		EndsAt         string `json:"ends_at,omitempty" doc:"Date subscription ends" example:"2026-03-01"`
	}
}

// GetBillingPortalURLInput represents the input for getting billing portal URL.
type GetBillingPortalURLInput struct {
	// No body needed - uses user_id from JWT context
}

// GetBillingPortalURLOutput represents the response for billing portal URL.
type GetBillingPortalURLOutput struct {
	Body struct {
		PortalURL string `json:"portal_url" doc:"URL to Paddle billing portal" example:"https://buy.paddle.com/customer/portal"`
	}
}

// GetCurrentSubscriptionInput represents the input for getting current subscription.
type GetCurrentSubscriptionInput struct {
	// No body needed - uses user_id from JWT context
}

// GetCurrentSubscriptionOutput represents the response for current subscription.
type GetCurrentSubscriptionOutput struct {
	Body struct {
		SubscriptionID     string `json:"subscription_id" doc:"Paddle subscription ID" example:"sub_01h1234567890"`
		Status             string `json:"status" doc:"Subscription status" example:"active"`
		Tier               string `json:"tier" doc:"Current subscription tier" example:"premium"`
		BillingInterval    string `json:"billing_interval" doc:"Billing interval (month/year)" example:"month"`
		CurrentPeriodEnd   string `json:"current_period_end" doc:"End of current billing period" example:"2026-03-01"`
		NextBillingDate    string `json:"next_billing_date,omitempty" doc:"Next billing date" example:"2026-03-01"`
		Amount             string `json:"amount" doc:"Subscription amount" example:"9.99"`
		Currency           string `json:"currency" doc:"Currency code" example:"USD"`
	}
}

var validSubscriptionTiers = map[string]bool{
	"premium":     true,
	"household_2": true,
	"household_3": true,
	"household_4": true,
	"household_5": true,
}

// HandleCreateCheckoutSession handles POST /api/v1/subscription/checkout.
// Creates a Paddle checkout session for the user to purchase a subscription.
func (h *PaddleCheckoutHandler) HandleCreateCheckoutSession(
	ctx context.Context,
	input *CreateCheckoutInput,
) (*CheckoutOutput, error) {
	// Get user from context
	user, err := h.getUserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	h.logger.Info("creating Paddle checkout session",
		"user_id", user.ID,
		"tier", input.Body.Tier,
		"billing_interval", input.Body.BillingInterval,
	)

	// Validate tier
	if !validSubscriptionTiers[input.Body.Tier] {
		return nil, huma.Error400BadRequest("Invalid tier")
	}

	// Validate billing interval
	if input.Body.BillingInterval != "month" && input.Body.BillingInterval != "year" {
		return nil, huma.Error400BadRequest("Billing interval must be 'month' or 'year'")
	}

	// Get or create Paddle customer ID
	customerID := user.PaddleCustomerID
	if customerID == nil || *customerID == "" {
		newCustomerID, err := h.paddleService.CreateCustomer(ctx, user.ID, user.Email)
		if err != nil {
			h.logger.Error("failed to create Paddle customer",
				"user_id", user.ID,
				"error", err,
			)
			return nil, huma.Error500InternalServerError("Failed to create customer")
		}

		// Update user with Paddle customer ID (non-critical if fails)
		if err := h.profileRepo.UpdatePaddleCustomerID(ctx, user.ID, newCustomerID); err != nil {
			h.logger.Error("failed to update paddle_customer_id",
				"user_id", user.ID,
				"error", err,
			)
		}

		customerID = &newCustomerID
	}

	// Get Paddle price ID for tier and billing interval from config
	priceID, err := h.getPaddlePriceIDFromConfig(input.Body.Tier, input.Body.BillingInterval)
	if err != nil {
		h.logger.Error("failed to get Paddle price ID",
			"tier", input.Body.Tier,
			"billing_interval", input.Body.BillingInterval,
			"error", err,
		)
		return nil, huma.Error500InternalServerError("Price ID not configured for this tier")
	}

	// Build success URL
	successURL := fmt.Sprintf("%s/subscription/success", h.config.FrontendURL)

	// Create checkout session
	checkoutURL, transactionID, err := h.paddleService.CreateCheckoutSession(
		ctx,
		*customerID,
		priceID,
		user.Email,
		successURL,
	)
	if err != nil {
		h.logger.Error("failed to create checkout session",
			"user_id", user.ID,
			"customer_id", *customerID,
			"price_id", priceID,
			"error", err,
		)
		return nil, huma.Error500InternalServerError("Failed to create checkout session")
	}

	h.logger.Info("checkout session created successfully",
		"user_id", user.ID,
		"transaction_id", transactionID,
		"tier", input.Body.Tier,
	)

	return &CheckoutOutput{
		Body: struct {
			CheckoutURL   string `json:"checkout_url" doc:"URL to open Paddle checkout (Paddle.js Overlay or redirect)"`
			TransactionID string `json:"transaction_id" doc:"Paddle transaction ID for tracking"`
		}{
			CheckoutURL:   checkoutURL,
			TransactionID: transactionID,
		},
	}, nil
}

// HandlePreviewSubscriptionUpdate handles POST /api/v1/subscription/preview-update.
// Calculates proration for upgrading/downgrading subscription tier.
func (h *PaddleCheckoutHandler) HandlePreviewSubscriptionUpdate(
	ctx context.Context,
	input *PreviewUpdateInput,
) (*PreviewUpdateOutput, error) {
	user, err := h.getUserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	h.logger.Info("previewing subscription update",
		"user_id", user.ID,
		"new_tier", input.Body.NewTier,
	)

	if !validSubscriptionTiers[input.Body.NewTier] {
		return nil, huma.Error400BadRequest("Invalid tier")
	}

	if user.SubscriptionStatus != "active" {
		return nil, huma.Error400BadRequest("No active subscription to update")
	}

	return nil, huma.Error501NotImplemented("Preview update not yet implemented")
}

// getUserFromContext extracts user ID from context and retrieves user details.
// Returns the user and appropriate HTTP errors if user is not authenticated or not found.
func (h *PaddleCheckoutHandler) getUserFromContext(ctx context.Context) (*domain.User, error) {
	userID, ok := ctx.Value("user_id").(string)
	if !ok || userID == "" {
		return nil, huma.Error401Unauthorized("User not authenticated")
	}

	user, err := h.profileRepo.GetUserByID(ctx, userID)
	if err != nil {
		h.logger.Error("failed to get user",
			"user_id", userID,
			"error", err,
		)
		return nil, huma.Error500InternalServerError("Failed to get user details")
	}

	return user, nil
}

// getActiveSubscriptionID retrieves user's active Paddle subscription ID from database.
// Returns error if user has no subscription ID or if user fetch fails.
func (h *PaddleCheckoutHandler) getActiveSubscriptionID(ctx context.Context, userID string) (string, error) {
	user, err := h.profileRepo.GetUserByID(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("failed to fetch user: %w", err)
	}

	if user.PaddleSubscriptionID == nil || *user.PaddleSubscriptionID == "" {
		return "", fmt.Errorf("no active subscription found")
	}

	return *user.PaddleSubscriptionID, nil
}

// getPaddlePriceIDFromConfig returns the Paddle price ID for a given tier and billing interval
// from the subscription_tiers.yaml configuration file.
func (h *PaddleCheckoutHandler) getPaddlePriceIDFromConfig(tierName, billingInterval string) (string, error) {
	// Get tier from config
	tier, err := h.tierConfig.GetTier(domain.TierName(tierName))
	if err != nil {
		return "", fmt.Errorf("tier not found: %w", err)
	}

	// Get appropriate price ID based on billing interval
	var priceID *string
	if billingInterval == "month" {
		priceID = tier.Pricing.PaddlePriceIDMonthly
	} else if billingInterval == "year" {
		priceID = tier.Pricing.PaddlePriceIDYearly
	} else {
		return "", fmt.Errorf("invalid billing interval: %s (must be 'month' or 'year')", billingInterval)
	}

	// Check if price ID is configured
	if priceID == nil || *priceID == "" {
		return "", fmt.Errorf("paddle price ID not configured for tier '%s' with billing interval '%s' (check config/subscription_tiers.yaml)", tierName, billingInterval)
	}

	return *priceID, nil
}

// HandleUpdateSubscription handles POST /api/v1/subscription/update.
// Updates user's subscription to a different tier or billing interval.
func (h *PaddleCheckoutHandler) HandleUpdateSubscription(
	ctx context.Context,
	input *UpdateSubscriptionInput,
) (*UpdateSubscriptionOutput, error) {
	user, err := h.getUserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	h.logger.Info("updating subscription",
		"user_id", user.ID,
		"new_tier", input.Body.NewTier,
	)

	if !validSubscriptionTiers[input.Body.NewTier] {
		return nil, huma.Error400BadRequest("Invalid tier")
	}

	if user.SubscriptionStatus != "active" {
		return nil, huma.Error400BadRequest("No active subscription to update")
	}

	priceID, err := h.getPaddlePriceIDFromConfig(input.Body.NewTier, input.Body.BillingInterval)
	if err != nil {
		h.logger.Error("failed to get price ID", "user_id", user.ID, "tier", input.Body.NewTier, "error", err)
		return nil, huma.Error500InternalServerError("Price ID not configured")
	}

	prorationMode := input.Body.ProrationBillingMode
	if prorationMode == "" {
		prorationMode = "prorated_immediately"
	}

	subscriptionID, err := h.getActiveSubscriptionID(ctx, user.ID)
	if err != nil {
		h.logger.Error("failed to get subscription ID", "user_id", user.ID, "error", err)
		return nil, huma.Error400BadRequest("No active subscription found")
	}

	updatedSub, err := h.paddleService.UpdateSubscription(ctx, subscriptionID, priceID, prorationMode)
	if err != nil {
		h.logger.Error("failed to update subscription", "user_id", user.ID, "error", err)
		return nil, huma.Error500InternalServerError("Failed to update subscription")
	}

	h.logger.Info("subscription updated successfully", "subscription_id", updatedSub.ID)

	return &UpdateSubscriptionOutput{
		Body: struct {
			SubscriptionID string `json:"subscription_id" doc:"Paddle subscription ID" example:"sub_01h1234567890"`
			Status         string `json:"status" doc:"Subscription status" example:"active"`
			Message        string `json:"message" doc:"Human-readable update message" example:"Subscription upgraded successfully"`
		}{
			SubscriptionID: updatedSub.ID,
			Status:         string(updatedSub.Status),
			Message:        "Subscription updated successfully",
		},
	}, nil
}

// HandleCancelSubscription handles POST /api/v1/subscription/cancel.
// Cancels user's subscription at end of billing period.
func (h *PaddleCheckoutHandler) HandleCancelSubscription(
	ctx context.Context,
	input *CancelSubscriptionInput,
) (*CancelSubscriptionOutput, error) {
	user, err := h.getUserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	h.logger.Info("canceling subscription", "user_id", user.ID)

	if user.SubscriptionStatus != "active" {
		return nil, huma.Error400BadRequest("No active subscription to cancel")
	}

	subscriptionID, err := h.getActiveSubscriptionID(ctx, user.ID)
	if err != nil {
		h.logger.Error("failed to get subscription ID", "user_id", user.ID, "error", err)
		return nil, huma.Error400BadRequest("No active subscription found")
	}

	if err := h.paddleService.CancelSubscription(ctx, subscriptionID); err != nil {
		h.logger.Error("failed to cancel subscription", "user_id", user.ID, "error", err)
		return nil, huma.Error500InternalServerError("Failed to cancel subscription")
	}

	h.logger.Info("subscription cancelled successfully", "user_id", user.ID, "subscription_id", subscriptionID)

	return &CancelSubscriptionOutput{
		Body: struct {
			SubscriptionID string `json:"subscription_id" doc:"Cancelled Paddle subscription ID" example:"sub_01h1234567890"`
			Message        string `json:"message" doc:"Cancellation confirmation message" example:"Subscription will be cancelled at end of billing period"`
			EndsAt         string `json:"ends_at,omitempty" doc:"Date subscription ends" example:"2026-03-01"`
		}{
			SubscriptionID: subscriptionID,
			Message:        "Subscription will be cancelled at end of billing period",
			EndsAt:         "",
		},
	}, nil
}

// HandleGetBillingPortalURL handles GET /api/v1/subscription/billing-portal.
// Returns URL to Paddle billing portal for managing payment methods.
func (h *PaddleCheckoutHandler) HandleGetBillingPortalURL(
	ctx context.Context,
	input *GetBillingPortalURLInput,
) (*GetBillingPortalURLOutput, error) {
	user, err := h.getUserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	h.logger.Info("getting billing portal URL", "user_id", user.ID)

	if user.PaddleCustomerID == nil || *user.PaddleCustomerID == "" {
		return nil, huma.Error400BadRequest("No Paddle customer found")
	}

	portalURL, err := h.paddleService.GetBillingPortalURL(ctx, *user.PaddleCustomerID)
	if err != nil {
		h.logger.Error("failed to get billing portal URL", "user_id", user.ID, "error", err)
		return nil, huma.Error500InternalServerError("Failed to get billing portal URL")
	}

	h.logger.Info("billing portal URL generated", "user_id", user.ID)

	return &GetBillingPortalURLOutput{
		Body: struct {
			PortalURL string `json:"portal_url" doc:"URL to Paddle billing portal" example:"https://buy.paddle.com/customer/portal"`
		}{
			PortalURL: portalURL,
		},
	}, nil
}

// HandleGetCurrentSubscription handles GET /api/v1/subscription/current.
// Fetches current subscription details from Paddle API.
func (h *PaddleCheckoutHandler) HandleGetCurrentSubscription(
	ctx context.Context,
	input *GetCurrentSubscriptionInput,
) (*GetCurrentSubscriptionOutput, error) {
	user, err := h.getUserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	h.logger.Info("getting current subscription", "user_id", user.ID)

	if user.SubscriptionStatus != "active" {
		return nil, huma.Error400BadRequest("No active subscription")
	}

	subscriptionID, err := h.getActiveSubscriptionID(ctx, user.ID)
	if err != nil {
		h.logger.Error("failed to get subscription ID", "user_id", user.ID, "error", err)
		return nil, huma.Error400BadRequest("No active subscription found")
	}

	subscription, err := h.paddleService.GetSubscription(ctx, subscriptionID)
	if err != nil {
		h.logger.Error("failed to get subscription", "user_id", user.ID, "error", err)
		return nil, huma.Error500InternalServerError("Failed to get subscription")
	}

	h.logger.Info("subscription retrieved", "user_id", user.ID, "subscription_id", subscription.ID)

	// Extract billing interval from subscription (if available)
	billingInterval := ""
	if subscription.BillingCycle.Interval != "" {
		billingInterval = string(subscription.BillingCycle.Interval)
	}

	// Extract amount from first subscription item (if available)
	amount := ""
	if len(subscription.Items) > 0 && subscription.Items[0].Price.UnitPrice.Amount != "" {
		amount = subscription.Items[0].Price.UnitPrice.Amount
	}

	return &GetCurrentSubscriptionOutput{
		Body: struct {
			SubscriptionID     string `json:"subscription_id" doc:"Paddle subscription ID" example:"sub_01h1234567890"`
			Status             string `json:"status" doc:"Subscription status" example:"active"`
			Tier               string `json:"tier" doc:"Current subscription tier" example:"premium"`
			BillingInterval    string `json:"billing_interval" doc:"Billing interval (month/year)" example:"month"`
			CurrentPeriodEnd   string `json:"current_period_end" doc:"End of current billing period" example:"2026-03-01"`
			NextBillingDate    string `json:"next_billing_date,omitempty" doc:"Next billing date" example:"2026-03-01"`
			Amount             string `json:"amount" doc:"Subscription amount" example:"9.99"`
			Currency           string `json:"currency" doc:"Currency code" example:"USD"`
		}{
			SubscriptionID:     subscription.ID,
			Status:             string(subscription.Status),
			Tier:               user.SubscriptionTier,
			BillingInterval:    billingInterval,
			CurrentPeriodEnd:   subscription.CurrentBillingPeriod.EndsAt,
			NextBillingDate:    subscription.CurrentBillingPeriod.EndsAt,
			Amount:             amount,
			Currency:           string(subscription.CurrencyCode),
		},
	}, nil
}

// RegisterPaddleCheckoutRoutes registers Paddle checkout routes with the Huma API.
func RegisterPaddleCheckoutRoutes(api huma.API, handler *PaddleCheckoutHandler) {
	// POST /api/v1/subscription/checkout - Create checkout session
	huma.Register(api, huma.Operation{
		OperationID: "create-paddle-checkout",
		Method:      "POST",
		Path:        "/api/v1/subscription/checkout",
		Summary:     "Create Paddle checkout session",
		Description: "Creates a Paddle checkout session for purchasing a subscription. Returns checkout URL to open in Paddle.js Overlay or as redirect.",
		Tags:        []string{"Subscription", "Paddle"},
	}, handler.HandleCreateCheckoutSession)

	// POST /api/v1/subscription/preview-update - Preview subscription update
	huma.Register(api, huma.Operation{
		OperationID: "preview-paddle-subscription-update",
		Method:      "POST",
		Path:        "/api/v1/subscription/preview-update",
		Summary:     "Preview subscription tier change",
		Description: "Calculates proration amount when upgrading or downgrading subscription tier. Shows immediate charge/credit and next billing details.",
		Tags:        []string{"Subscription", "Paddle"},
	}, handler.HandlePreviewSubscriptionUpdate)

	// POST /api/v1/subscription/update - Update subscription
	huma.Register(api, huma.Operation{
		OperationID: "update-paddle-subscription",
		Method:      "POST",
		Path:        "/api/v1/subscription/update",
		Summary:     "Update subscription tier",
		Description: "Updates subscription to a different tier or billing interval. Handles proration automatically.",
		Tags:        []string{"Subscription", "Paddle"},
	}, handler.HandleUpdateSubscription)

	// POST /api/v1/subscription/cancel - Cancel subscription
	huma.Register(api, huma.Operation{
		OperationID: "cancel-paddle-subscription",
		Method:      "POST",
		Path:        "/api/v1/subscription/cancel",
		Summary:     "Cancel subscription",
		Description: "Cancels subscription at end of current billing period. User retains access until period ends.",
		Tags:        []string{"Subscription", "Paddle"},
	}, handler.HandleCancelSubscription)

	// GET /api/v1/subscription/billing-portal - Get billing portal URL
	huma.Register(api, huma.Operation{
		OperationID: "get-billing-portal-url",
		Method:      "GET",
		Path:        "/api/v1/subscription/billing-portal",
		Summary:     "Get billing portal URL",
		Description: "Returns URL to Paddle billing portal where users can manage payment methods and view billing history.",
		Tags:        []string{"Subscription", "Paddle"},
	}, handler.HandleGetBillingPortalURL)

	// GET /api/v1/subscription/current - Get current subscription
	huma.Register(api, huma.Operation{
		OperationID: "get-current-subscription",
		Method:      "GET",
		Path:        "/api/v1/subscription/current",
		Summary:     "Get current subscription",
		Description: "Fetches current subscription details from Paddle API with live status, billing info, and tier information.",
		Tags:        []string{"Subscription", "Paddle"},
	}, handler.HandleGetCurrentSubscription)
}
