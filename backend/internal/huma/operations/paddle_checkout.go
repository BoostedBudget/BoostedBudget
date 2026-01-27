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
// Issue #45: Paddle Payment Service Implementation
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

// --- Helper Functions ---

// validSubscriptionTiers returns a map of valid subscription tiers
func validSubscriptionTiers() map[string]bool {
	return map[string]bool{
		"premium":     true,
		"household_2": true,
		"household_3": true,
		"household_4": true,
		"household_5": true,
	}
}

// --- Handler Methods ---

// HandleCreateCheckoutSession handles POST /api/v1/subscription/checkout.
// Creates a Paddle checkout session for the user to purchase a subscription.
//
// Flow:
//  1. Validate tier and billing interval
//  2. Get/create Paddle customer ID for user
//  3. Look up Paddle price ID from subscription_tiers.yaml
//  4. Call Paddle API to create checkout session
//  5. Return checkout URL for frontend
//
// Issue #45: Paddle Payment Service Implementation
func (h *PaddleCheckoutHandler) HandleCreateCheckoutSession(
	ctx context.Context,
	input *CreateCheckoutInput,
) (*CheckoutOutput, error) {
	// Get user ID from JWT claims (set by middleware)
	userID, ok := ctx.Value("user_id").(string)
	if !ok || userID == "" {
		return nil, huma.Error401Unauthorized("User not authenticated")
	}

	h.logger.Info("creating Paddle checkout session",
		"user_id", userID,
		"tier", input.Body.Tier,
		"billing_interval", input.Body.BillingInterval,
	)

	// Validate tier
	if !validSubscriptionTiers()[input.Body.Tier] {
		return nil, huma.Error400BadRequest("Invalid tier")
	}

	// Validate billing interval
	if input.Body.BillingInterval != "month" && input.Body.BillingInterval != "year" {
		return nil, huma.Error400BadRequest("Billing interval must be 'month' or 'year'")
	}

	// Get user from database
	user, err := h.profileRepo.GetUserByID(ctx, userID)
	if err != nil {
		h.logger.Error("failed to get user",
			"user_id", userID,
			"error", err,
		)
		return nil, huma.Error500InternalServerError("Failed to get user details")
	}

	// Get or create Paddle customer ID
	customerID := user.PaddleCustomerID
	if customerID == nil || *customerID == "" {
		// Create new Paddle customer
		newCustomerID, err := h.paddleService.CreateCustomer(ctx, userID, user.Email)
		if err != nil {
			h.logger.Error("failed to create Paddle customer",
				"user_id", userID,
				"error", err,
			)
			return nil, huma.Error500InternalServerError("Failed to create customer")
		}

		// Update user with Paddle customer ID
		if err := h.profileRepo.UpdatePaddleCustomerID(ctx, userID, newCustomerID); err != nil {
			h.logger.Error("failed to update paddle_customer_id",
				"user_id", userID,
				"error", err,
			)
			// Continue anyway - we have the customer ID
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
			"user_id", userID,
			"customer_id", *customerID,
			"price_id", priceID,
			"error", err,
		)
		return nil, huma.Error500InternalServerError("Failed to create checkout session")
	}

	h.logger.Info("checkout session created successfully",
		"user_id", userID,
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

// HandlePreviewSubscriptionUpdate handles GET /api/v1/subscription/preview-update.
// Calculates proration for upgrading/downgrading subscription tier.
//
// Flow:
//  1. Validate user has active subscription
//  2. Look up Paddle price ID for new tier
//  3. Call Paddle API to preview update
//  4. Return proration details
//
// Issue #45: Paddle Payment Service Implementation
func (h *PaddleCheckoutHandler) HandlePreviewSubscriptionUpdate(
	ctx context.Context,
	input *PreviewUpdateInput,
) (*PreviewUpdateOutput, error) {
	// Get user ID from JWT claims
	userID, ok := ctx.Value("user_id").(string)
	if !ok || userID == "" {
		return nil, huma.Error401Unauthorized("User not authenticated")
	}

	h.logger.Info("previewing subscription update",
		"user_id", userID,
		"new_tier", input.Body.NewTier,
	)

	// Validate tier
	if !validSubscriptionTiers()[input.Body.NewTier] {
		return nil, huma.Error400BadRequest("Invalid tier")
	}

	// Get user from database
	user, err := h.profileRepo.GetUserByID(ctx, userID)
	if err != nil {
		h.logger.Error("failed to get user",
			"user_id", userID,
			"error", err,
		)
		return nil, huma.Error500InternalServerError("Failed to get user details")
	}

	// Check if user has active subscription
	if user.SubscriptionStatus != "active" {
		return nil, huma.Error400BadRequest("No active subscription to update")
	}

	// TODO: Get user's current subscription ID from Paddle
	// For now, return error
	return nil, huma.Error501NotImplemented("Preview update not yet implemented")

	// Get Paddle price ID for new tier
	// TODO: Load from subscription_tiers.yaml
	// newPriceID := getPaddlePriceID(input.Body.NewTier, input.Body.BillingInterval)
	// if newPriceID == "" {
	// 	return nil, huma.Error500InternalServerError("Price ID not configured for this tier")
	// }

	// Preview subscription update
	// prorationAmount, currency, nextChargeDate, err := h.paddleService.PreviewUpdate(
	// 	ctx,
	// 	subscriptionID,
	// 	newPriceID,
	// )
	// if err != nil {
	// 	h.logger.Error("failed to preview subscription update",
	// 		"user_id", userID,
	// 		"error", err,
	// 	)
	// 	return nil, huma.Error500InternalServerError("Failed to preview update")
	// }

	// return &PreviewUpdateOutput{
	// 	Body: struct {
	// 		ProrationAmount string `json:"proration_amount" doc:"Amount to charge (positive) or credit (negative) immediately" example:"5.00"`
	// 		Currency        string `json:"currency" doc:"Currency code (USD, EUR, etc.)" example:"USD"`
	// 		NextChargeDate  string `json:"next_charge_date" doc:"Date of next regular charge" example:"2026-02-26"`
	// 		NextChargeAmt   string `json:"next_charge_amount" doc:"Amount of next regular charge" example:"12.99"`
	// 	}{
	// 		ProrationAmount: prorationAmount,
	// 		Currency:        currency,
	// 		NextChargeDate:  nextChargeDate,
	// 		NextChargeAmt:   "12.99", // TODO: Calculate from new tier
	// 	},
	// }, nil
}

// getPaddlePriceIDFromConfig returns the Paddle price ID for a given tier and billing interval
// from the subscription_tiers.yaml configuration file.
//
// Issue #45: Paddle Payment Service Implementation
// Loads actual Paddle price IDs from config instead of hardcoded placeholders
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
}
