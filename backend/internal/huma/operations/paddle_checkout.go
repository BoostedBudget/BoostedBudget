package operations

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	paddle "github.com/PaddleHQ/paddle-go-sdk/v4"
	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	humaerrors "github.com/maxnussbaum/BoostedBudget/internal/huma/errors"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// checkoutEntitlementReader is the narrow capability the checkout handler needs to
// detect EFFECTIVE household coverage (#1367 decision #9). Deliberately checks
// resolved coverage, not the raw subscription_entitlements row: a raw source='household'
// row can outlive actual coverage (e.g. the owner has gone past_due/cancelled — Phase 7
// owns cleaning that row up), and blocking checkout on the raw row would trap a
// no-longer-covered member who legitimately needs to self-subscribe (Codex review
// finding). Satisfied structurally by *services.HouseholdEntitlementService.
type checkoutEntitlementReader interface {
	IsCoveredByHousehold(ctx context.Context, userID string) (bool, error)
}

// checkoutHouseholdSeatReader is the narrow capability HandleUpdateSubscription needs to
// enforce the in-app downgrade seat guard (#1371 Task 5): find whether the caller owns a
// household, and count its active members + pending invites against the post-change
// effective tier's seat allowance. repository.HouseholdRepository satisfies this
// structurally.
type checkoutHouseholdSeatReader interface {
	GetHouseholdForUser(ctx context.Context, userID string) (household *domain.Household, isOwner bool, found bool, err error)
	CountActiveMembers(ctx context.Context, householdID string) (int, error)
	CountPendingInvites(ctx context.Context, householdID string) (int, error)
}

// PaddleCheckoutHandler handles Paddle checkout and subscription management operations.
type PaddleCheckoutHandler struct {
	rcBilling       revenueCatBillingReader
	paddleService   services.PaddleService
	profileRepo     repository.ProfileRepository
	config          *config.Config
	tierConfig      *config.TierConfig
	logger          *slog.Logger
	entitlementRepo checkoutEntitlementReader   // #1367 — Phase 3: nil skips the covered-member guard
	householdRepo   checkoutHouseholdSeatReader // #1371 — Task 5: nil skips the downgrade seat guard
}

// PaddleCheckoutHandlerDeps contains dependencies for PaddleCheckoutHandler.
type PaddleCheckoutHandlerDeps struct {
	PaddleService services.PaddleService
	ProfileRepo   repository.ProfileRepository
	Config        *config.Config
	TierConfig    *config.TierConfig
	Logger        *slog.Logger
	// EntitlementRepo is optional (nil-safe) — when set, blocks a caller EFFECTIVELY
	// covered by a household plan from starting their own Paddle checkout (#1367
	// decision #9: prevents paid+covered overlap / refund liability). Checks resolved
	// coverage, not the raw entitlement row, so a member is freed to self-subscribe the
	// moment their household owner stops actually covering them. Should always be wired
	// in production; nil only to avoid breaking call sites unrelated to households.
	EntitlementRepo checkoutEntitlementReader
	// HouseholdRepo is optional (nil-safe) — when set, blocks a household owner from
	// downgrading their subscription tier in-app if the new EFFECTIVE tier (honoring an
	// active manual_tier_override) doesn't have enough seats for their current
	// active+pending household (#1371 Task 5). Non-owners and solo users are unaffected.
	// Should always be wired in production; nil only to avoid breaking call sites
	// unrelated to households.
	HouseholdRepo checkoutHouseholdSeatReader
}

// NewPaddleCheckoutHandler creates a new PaddleCheckoutHandler instance.
func NewPaddleCheckoutHandler(deps PaddleCheckoutHandlerDeps) *PaddleCheckoutHandler {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &PaddleCheckoutHandler{
		paddleService:   deps.PaddleService,
		profileRepo:     deps.ProfileRepo,
		config:          deps.Config,
		tierConfig:      deps.TierConfig,
		logger:          logger,
		entitlementRepo: deps.EntitlementRepo,
		householdRepo:   deps.HouseholdRepo,
	}
}

// --- Input/Output Types ---

// CreateCheckoutInput represents the input for creating a checkout session.
type CreateCheckoutInput struct {
	inputs.AuthenticatedInput
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
	inputs.AuthenticatedInput
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
	inputs.AuthenticatedInput
	Body struct {
		NewTier                string `json:"new_tier" doc:"New subscription tier" example:"household_2" minLength:"1"`
		BillingInterval        string `json:"billing_interval" doc:"Billing interval (month or year)" example:"month" enum:"month,year"`
		ProrationBillingMode   string `json:"proration_billing_mode,omitempty" doc:"Proration mode (prorated_immediately, full_immediately, full_next_billing_period)" example:"prorated_immediately" enum:"prorated_immediately,full_immediately,full_next_billing_period"`
		CurrentBillingInterval string `json:"current_billing_interval,omitempty" doc:"Current billing interval, used server-side to detect and reject no-op updates" example:"month" enum:"month,year"`
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
	inputs.AuthenticatedInput
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
	inputs.AuthenticatedInput
}

// GetBillingPortalURLOutput represents the response for billing portal URL.
type GetBillingPortalURLOutput struct {
	Body struct {
		PortalURL string `json:"portal_url" doc:"URL to Paddle billing portal" example:"https://buy.paddle.com/customer/portal"`
	}
}

// GetCurrentSubscriptionInput represents the input for getting current subscription.
type GetCurrentSubscriptionInput struct {
	inputs.AuthenticatedInput
}

// GetCurrentSubscriptionOutput represents the response for current subscription.
type GetCurrentSubscriptionOutput struct {
	Body struct {
		Authority          *services.RevenueCatSummary `json:"authority,omitempty" doc:"Confirmed access independent of paid billing details"`
		SubscriptionID     string                      `json:"subscription_id" doc:"Paddle subscription ID" example:"sub_01h1234567890"`
		Status             string                      `json:"status" doc:"Subscription status (active, trial, trialing, cancelled, past_due)" example:"active"`
		Tier               string                      `json:"tier" doc:"Current subscription tier" example:"premium"`
		BillingInterval    string                      `json:"billing_interval" doc:"Billing interval (month/year)" example:"month"`
		CurrentPeriodEnd   string                      `json:"current_period_end,omitempty" doc:"End of current billing period" example:"2026-03-01"`
		NextBillingDate    string                      `json:"next_billing_date,omitempty" doc:"Next billing date" example:"2026-03-01"`
		Amount             string                      `json:"amount" doc:"Subscription amount" example:"9.99"`
		Currency           string                      `json:"currency" doc:"Currency code" example:"USD"`
		TrialEndsAt        string                      `json:"trial_ends_at,omitempty" doc:"Trial end date (for trial users)" example:"2026-04-08"`
		TrialDaysRemaining *int                        `json:"trial_days_remaining,omitempty" doc:"Days remaining in trial (for trial users)" example:"45"`
		CancelAtPeriodEnd  bool                        `json:"cancel_at_period_end" doc:"Whether the subscription is scheduled to cancel at end of current period"`
		ScheduledChangeAt  string                      `json:"scheduled_change_at,omitempty" doc:"RFC 3339 datetime when the scheduled cancel takes effect" example:"2026-05-01T00:00:00Z"`
		IsComped           bool                        `json:"is_comped" doc:"True when access is granted by an admin comp rather than a paid subscription"`
		CompEndsAt         string                      `json:"comp_ends_at,omitempty" doc:"RFC 3339 datetime when complimentary access ends; absent means indefinite"`
	}
}

var validSubscriptionTiers = map[string]bool{
	"premium":     true,
	"household_2": true,
	"household_3": true,
	"household_4": true,
	"household_5": true,
}

// validateUserAuth checks if user is authenticated
func validateUserAuth(userID string) error {
	if userID == "" {
		return huma.Error401Unauthorized("User not authenticated")
	}
	return nil
}

// validateBillingInterval checks if billing interval is valid
func validateBillingInterval(interval string) error {
	if interval != "month" && interval != "year" {
		return huma.Error400BadRequest("Billing interval must be 'month' or 'year'")
	}
	return nil
}

// isSubscriptionModifiable checks if a subscription status allows modifications
//
// #1413 PinnedRaw: every call site (HandlePreviewSubscriptionUpdate, HandleUpdateSubscription,
// HandleCancelSubscription) deliberately passes user.SubscriptionStatus (raw), never
// ComputeEffectiveStatus(). This function answers "is there a real Paddle subscription to
// mutate" — a comped user with no real subscription must still be rejected here (there is
// nothing for Paddle to preview/update/cancel), even though their effective status is "active".
// Passing effective status would turn an honest "No active subscription to update" 400 into a
// confusing attempt to mutate a Paddle subscription that doesn't exist. See
// TestHandlePreviewSubscriptionUpdate_CompedUser_NoRealSubscription_Rejected,
// TestHandleUpdateSubscription_CompedUser_NoRealSubscription_Rejected, and
// TestHandleCancelSubscription_CompedUser_NoRealSubscription_Rejected for the pin.
func isSubscriptionModifiable(status string) bool {
	return status == "active" || status == "trialing"
}

// HandleCreateCheckoutSession handles POST /api/v1/subscription/checkout.
// Creates a Paddle checkout session for the user to purchase a subscription.
func (h *PaddleCheckoutHandler) HandleCreateCheckoutSession(
	ctx context.Context,
	input *CreateCheckoutInput,
) (*CheckoutOutput, error) {
	if err := validateUserAuth(input.UserID); err != nil {
		return nil, err
	}
	if h.rcBilling != nil {
		return nil, huma.Error409Conflict("Start checkout through the subscription paywall.")
	}

	// Get user from database
	user, err := h.profileRepo.GetUserByID(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to get user %s: %w", input.UserID, err))
	}

	// #1367 decision #9: block a caller already covered by a household plan from
	// starting their own Paddle checkout. This is the dominant reverse-overlap path —
	// a covered member's own subscription_status may itself be read-only/lapsed (it's
	// never mutated while covered), so this must be checked independently of the
	// active/trialing checks below.
	if h.entitlementRepo != nil {
		covered, err := h.entitlementRepo.IsCoveredByHousehold(ctx, user.ID)
		if err != nil {
			return nil, humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to check household coverage for user %s: %w", user.ID, err))
		}
		if covered {
			return nil, humaerrors.LoggingMapError(ctx, domain.ErrAlreadyCoveredByHousehold)
		}
	}

	// Prevent duplicate subscriptions
	//
	// #1413 PinnedRaw: these two checks deliberately read the RAW user.SubscriptionStatus, not
	// ComputeEffectiveStatus(). A comped user's raw status is whatever their real Paddle
	// subscription last left it (typically "inactive" or "cancelled") while their EFFECTIVE
	// status is forced to "active" by the override — if this read effective status, a comped
	// user could never reach checkout at all, and would be hard-locked out of ever buying a real
	// subscription the moment the comp lapses (issue #1413 AC 4). See
	// TestHandleCreateCheckoutSession_CompedUser_RawInactive_AllowedToCheckout /
	// TestHandleCreateCheckoutSession_CompedUser_RawCancelled_AllowedToCheckout for the pin.
	if user.SubscriptionStatus == "active" {
		h.logger.Warn("active user attempted duplicate purchase", "user_id", user.ID, "requested_tier", input.Body.Tier)
		return nil, huma.Error409Conflict("You already have an active subscription. Use the upgrade flow to change plans.")
	}

	if user.SubscriptionStatus == "trialing" {
		h.logger.Warn("trialing user attempted duplicate purchase", "user_id", user.ID, "requested_tier", input.Body.Tier)
		return nil, huma.Error409Conflict("You already have a pending subscription that will activate when your trial ends.")
	}

	h.logger.Info("creating Paddle checkout session",
		"user_id", user.ID,
		"tier", input.Body.Tier,
		"billing_interval", input.Body.BillingInterval,
	)

	if !validSubscriptionTiers[input.Body.Tier] {
		return nil, huma.Error400BadRequest("Invalid tier")
	}

	if err := validateBillingInterval(input.Body.BillingInterval); err != nil {
		return nil, err
	}

	// Get or create Paddle customer ID
	customerID := user.PaddleCustomerID
	if customerID == nil || *customerID == "" {
		newCustomerID, err := h.paddleService.EnsureCustomer(ctx, user.ID, user.Email)
		if err != nil {
			return nil, humaerrors.LoggingMapError(ctx, fmt.Errorf("stage=ensure_customer: failed to resolve Paddle customer for user %s: %w", user.ID, err))
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
		return nil, humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to get Paddle price ID for tier %s/%s: %w", input.Body.Tier, input.Body.BillingInterval, err))
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
		user.ID, // Pass userID for custom_data in Paddle transaction
	)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to create checkout session for user %s: %w", user.ID, err))
	}

	h.logger.Info("checkout session created successfully",
		"user_id", user.ID,
		"transaction_id", transactionID,
		"tier", input.Body.Tier,
	)

	out := &CheckoutOutput{}
	out.Body.CheckoutURL = checkoutURL
	out.Body.TransactionID = transactionID
	return out, nil
}

// HandlePreviewSubscriptionUpdate handles POST /api/v1/subscription/preview-update.
// Calculates proration for upgrading/downgrading subscription tier.
func (h *PaddleCheckoutHandler) HandlePreviewSubscriptionUpdate(
	ctx context.Context,
	input *PreviewUpdateInput,
) (*PreviewUpdateOutput, error) {
	if err := validateUserAuth(input.UserID); err != nil {
		return nil, err
	}

	// Get user from database
	user, err := h.profileRepo.GetUserByID(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to get user %s: %w", input.UserID, err))
	}

	h.logger.Info("previewing subscription update",
		"user_id", user.ID,
		"new_tier", input.Body.NewTier,
	)

	if !validSubscriptionTiers[input.Body.NewTier] {
		return nil, huma.Error400BadRequest("Invalid tier")
	}

	if h.rcBilling == nil && !isSubscriptionModifiable(user.SubscriptionStatus) {
		return nil, huma.Error400BadRequest("No active subscription to update")
	}

	if err := validateBillingInterval(input.Body.BillingInterval); err != nil {
		return nil, err
	}

	// Get active subscription ID
	subscriptionID, err := h.getActiveSubscriptionID(ctx, user.ID)
	if err != nil {
		h.logger.Error("failed to get active subscription", "user_id", user.ID, "error", err)
		if h.rcBilling != nil {
			return nil, err
		}
		return nil, huma.Error400BadRequest("No active subscription found")
	}

	// Get price ID for new tier and interval
	priceID, err := h.getPaddlePriceIDFromConfig(input.Body.NewTier, input.Body.BillingInterval)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to get Paddle price ID for tier %s/%s: %w", input.Body.NewTier, input.Body.BillingInterval, err))
	}

	// Call Paddle service to preview update
	prorationAmount, currency, nextChargeDate, err := h.paddleService.PreviewUpdate(ctx, subscriptionID, priceID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to preview subscription update for user %s subscription %s: %w", user.ID, subscriptionID, err))
	}

	out := &PreviewUpdateOutput{}
	out.Body.ProrationAmount = prorationAmount
	out.Body.Currency = currency
	out.Body.NextChargeDate = nextChargeDate
	out.Body.NextChargeAmt = "0.00"

	h.logger.Info("subscription update preview generated",
		"user_id", user.ID,
		"proration_amount", prorationAmount,
		"next_charge_date", nextChargeDate,
	)

	return out, nil
}

// getActiveSubscriptionID retrieves user's active Paddle subscription ID from database.
// Returns error if user has no subscription ID or if user fetch fails.
func (h *PaddleCheckoutHandler) getActiveSubscriptionID(ctx context.Context, userID string) (string, error) {
	user, err := h.profileRepo.GetUserByID(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("failed to fetch user: %w", err)
	}
	if h.rcBilling != nil {
		sub, _, err := h.rcBilling.Resolve(ctx, user)
		if err != nil {
			return "", revenueCatBillingError(err)
		}
		return sub.ID, nil
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
	switch billingInterval {
	case "month":
		priceID = tier.Pricing.PaddlePriceIDMonthly
	case "year":
		priceID = tier.Pricing.PaddlePriceIDYearly
	default:
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
	if err := validateUserAuth(input.UserID); err != nil {
		return nil, err
	}

	// Get user from database
	user, err := h.profileRepo.GetUserByID(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to get user %s: %w", input.UserID, err))
	}

	h.logger.Info("updating subscription",
		"user_id", user.ID,
		"new_tier", input.Body.NewTier,
	)

	if !validSubscriptionTiers[input.Body.NewTier] {
		return nil, huma.Error400BadRequest("Invalid tier")
	}

	if h.rcBilling == nil && !isSubscriptionModifiable(user.SubscriptionStatus) {
		return nil, huma.Error400BadRequest("No active subscription to update")
	}

	if h.rcBilling == nil && input.Body.CurrentBillingInterval != "" &&
		input.Body.NewTier == user.SubscriptionTier &&
		input.Body.BillingInterval == input.Body.CurrentBillingInterval {
		return nil, huma.Error400BadRequest("No change: subscription is already on the requested plan and billing interval")
	}

	priceID, err := h.getPaddlePriceIDFromConfig(input.Body.NewTier, input.Body.BillingInterval)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to get Paddle price ID for tier %s/%s: %w", input.Body.NewTier, input.Body.BillingInterval, err))
	}

	// #1371 Task 5 — in-app downgrade seat guard: block a household owner from
	// downgrading to a tier that doesn't have enough seats for their current household.
	// Nil-safe (mirrors entitlementRepo above) and a no-op for non-owners/solo users.
	if h.householdRepo != nil {
		if err := h.checkDowngradeSeatCapacity(ctx, user, input.Body.NewTier); err != nil {
			return nil, err
		}
	}

	prorationMode := input.Body.ProrationBillingMode
	if prorationMode == "" {
		prorationMode = "prorated_immediately"
	}

	subscriptionID, err := h.getActiveSubscriptionID(ctx, user.ID)
	if err != nil {
		h.logger.Error("failed to get subscription ID", "user_id", user.ID, "error", err)
		if h.rcBilling != nil {
			return nil, err
		}
		return nil, huma.Error400BadRequest("No active subscription found")
	}

	updatedSub, err := h.paddleService.UpdateSubscription(ctx, subscriptionID, priceID, prorationMode)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to update subscription %s for user %s: %w", subscriptionID, user.ID, err))
	}

	h.logger.Info("subscription updated successfully", "subscription_id", updatedSub.ID)

	message := "Subscription updated successfully"
	currentTier, currentErr := h.tierConfig.GetTier(domain.TierName(user.SubscriptionTier))
	newTier, newErr := h.tierConfig.GetTier(domain.TierName(input.Body.NewTier))
	if currentErr == nil && newErr == nil {
		switch {
		case newTier.Pricing.PriceMonthly > currentTier.Pricing.PriceMonthly:
			message = "Subscription upgraded successfully"
		case newTier.Pricing.PriceMonthly < currentTier.Pricing.PriceMonthly:
			message = "Subscription downgraded successfully"
		}
	}

	out := &UpdateSubscriptionOutput{}
	out.Body.SubscriptionID = updatedSub.ID
	out.Body.Status = string(updatedSub.Status)
	out.Body.Message = message
	return out, nil
}

// checkDowngradeSeatCapacity blocks a household owner from downgrading their subscription
// tier in-app if the new tier doesn't have enough seats for their current household
// (#1371 Task 5). It computes the owner's POST-CHANGE EFFECTIVE tier — simulating what
// ComputeEffectiveTier() would resolve to if SubscriptionTier were set to newTier,
// without mutating the caller's real record or writing to the DB — so an owner whose
// active manual_tier_override keeps them on a higher effective tier than the raw
// requested tier is correctly NOT blocked (they aren't actually downgrading in effective
// terms). Returns nil (no-op) if the caller doesn't own a household; seats are an owner
// concept, so a member changing their own tier is never affected. Does not touch the
// over-capacity/grace mechanism (Task 6) — this is a synchronous pre-flight check only.
func (h *PaddleCheckoutHandler) checkDowngradeSeatCapacity(ctx context.Context, user *domain.User, newTier string) error {
	household, isOwner, found, err := h.householdRepo.GetHouseholdForUser(ctx, user.ID)
	if err != nil {
		return humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to look up household for user %s: %w", user.ID, err))
	}
	if !found || !isOwner {
		return nil
	}

	// Simulate the post-change effective tier: copy the user, set the hypothetical new
	// SubscriptionTier, and let ComputeEffectiveTier() apply the existing
	// manual_tier_override logic on top of it (same pointer fields, no DB write).
	simulatedUser := *user
	simulatedUser.SubscriptionTier = newTier
	// Issue #1413 (this task — tier axis): manual_tier_override is an unconstrained VARCHAR(50)
	// with no CHECK constraint, so it can hold a value naming no configured tier. Validate it via
	// isConfiguredOverrideTier (this handler's existing seam, reused rather than adding a third
	// mechanism) before trusting it, falling back to the simulated RAW tier (newTier) — this is
	// the site's own doc comment's "clean, no half-applied billing" 500 turned into a clean 409
	// instead.
	effectiveTier, overrideValue, hasOverride := simulatedUser.EffectiveTierWithActiveOverride()
	if hasOverride && h.rejectedOverride(user.ID, overrideValue, "seat-capacity check") {
		effectiveTier = simulatedUser.SubscriptionTier
	}
	if h.rcBilling != nil {
		_, summary, billingErr := h.rcBilling.Resolve(ctx, user)
		if billingErr != nil {
			return revenueCatBillingError(billingErr)
		}
		if summary.Own == nil || summary.PendingOperations > 0 {
			return revenueCatBillingError(domain.ErrRevenueCatUnresolved)
		}
		grants := []domain.RevenueCatGrant{{Key: "paid_" + newTier}}
		if summary.Comp != nil {
			grants = append(grants, domain.RevenueCatGrant{Key: "comp_" + summary.Comp.Tier, ExpiresAt: summary.Comp.ExpiresAt})
		}
		now := time.Now()
		access, accessErr := (domain.RevenueCatState{AccessUpdatedAt: now, Grants: grants}).ResolveAccess(now)
		if accessErr != nil {
			return revenueCatBillingError(accessErr)
		}
		effectiveTier = access.Tier
	}

	tier, err := h.tierConfig.GetTier(domain.TierName(effectiveTier))
	if err != nil {
		return humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to get tier config for effective tier %s: %w", effectiveTier, err))
	}

	activeCount, err := h.householdRepo.CountActiveMembers(ctx, household.ID)
	if err != nil {
		return humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to count active members for household %s: %w", household.ID, err))
	}
	pendingCount, err := h.householdRepo.CountPendingInvites(ctx, household.ID)
	if err != nil {
		return humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to count pending invites for household %s: %w", household.ID, err))
	}

	totalMembers := activeCount + pendingCount
	if totalMembers <= tier.Pricing.UsersIncluded {
		return nil
	}

	shortfall := totalMembers - tier.Pricing.UsersIncluded
	h.logger.Warn("blocked in-app downgrade: insufficient seats for current household",
		"user_id", user.ID,
		"household_id", household.ID,
		"requested_tier", newTier,
		"effective_tier", effectiveTier,
		"active_members", activeCount,
		"pending_invites", pendingCount,
		"seats_included", tier.Pricing.UsersIncluded,
		"shortfall", shortfall,
	)

	memberWord := "members"
	if shortfall == 1 {
		memberWord = "member"
	}
	return huma.Error409Conflict(fmt.Sprintf("Remove %d %s before downgrading", shortfall, memberWord))
}

// HandleCancelSubscription handles POST /api/v1/subscription/cancel.
// Cancels user's subscription at end of billing period.
func (h *PaddleCheckoutHandler) HandleCancelSubscription(
	ctx context.Context,
	input *CancelSubscriptionInput,
) (*CancelSubscriptionOutput, error) {
	if err := validateUserAuth(input.UserID); err != nil {
		return nil, err
	}

	// Get user from database
	user, err := h.profileRepo.GetUserByID(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to get user %s: %w", input.UserID, err))
	}

	h.logger.Info("canceling subscription", "user_id", user.ID)

	if h.rcBilling == nil && !isSubscriptionModifiable(user.SubscriptionStatus) {
		return nil, huma.Error400BadRequest("No active subscription to cancel")
	}

	subscriptionID, err := h.getActiveSubscriptionID(ctx, user.ID)
	if err != nil {
		h.logger.Error("failed to get subscription ID", "user_id", user.ID, "error", err)
		if h.rcBilling != nil {
			return nil, err
		}
		return nil, huma.Error400BadRequest("No active subscription found")
	}

	if err := h.paddleService.CancelSubscription(ctx, subscriptionID); err != nil {
		return nil, humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to cancel subscription %s for user %s: %w", subscriptionID, user.ID, err))
	}

	h.logger.Info("subscription cancelled successfully", "user_id", user.ID, "subscription_id", subscriptionID)

	out := &CancelSubscriptionOutput{}
	out.Body.SubscriptionID = subscriptionID
	out.Body.Message = "Subscription will be cancelled at end of billing period"

	// Get subscription to populate EndsAt
	sub, subErr := h.paddleService.GetSubscription(ctx, subscriptionID)
	if subErr == nil && sub.CurrentBillingPeriod != nil {
		out.Body.EndsAt = sub.CurrentBillingPeriod.EndsAt
	}

	return out, nil
}

// HandleGetBillingPortalURL handles GET /api/v1/subscription/billing-portal.
// Returns URL to Paddle billing portal for managing payment methods.
func (h *PaddleCheckoutHandler) HandleGetBillingPortalURL(
	ctx context.Context,
	input *GetBillingPortalURLInput,
) (*GetBillingPortalURLOutput, error) {
	if err := validateUserAuth(input.UserID); err != nil {
		return nil, err
	}
	if h.rcBilling != nil {
		return h.revenueCatBillingPortal(ctx, input.UserID)
	}

	// Get user from database
	user, err := h.profileRepo.GetUserByID(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to get user %s: %w", input.UserID, err))
	}

	h.logger.Info("getting billing portal URL", "user_id", user.ID)

	if user.PaddleCustomerID == nil || *user.PaddleCustomerID == "" {
		return nil, huma.Error400BadRequest("No Paddle customer found")
	}

	portalURL, err := h.paddleService.GetBillingPortalURL(ctx, *user.PaddleCustomerID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to get billing portal URL for user %s: %w", user.ID, err))
	}

	h.logger.Info("billing portal URL generated", "user_id", user.ID)

	out := &GetBillingPortalURLOutput{}
	out.Body.PortalURL = portalURL
	return out, nil
}

// HandleGetCurrentSubscription handles GET /api/v1/subscription/current.
// Fetches current subscription details from Paddle API.
func (h *PaddleCheckoutHandler) HandleGetCurrentSubscription(
	ctx context.Context,
	input *GetCurrentSubscriptionInput,
) (*GetCurrentSubscriptionOutput, error) {
	if err := validateUserAuth(input.UserID); err != nil {
		return nil, err
	}
	if h.rcBilling != nil {
		return h.revenueCatCurrent(ctx, input.UserID)
	}

	// Get user from database
	user, err := h.profileRepo.GetUserByID(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to get user %s: %w", input.UserID, err))
	}

	h.logger.Info("getting current subscription", "user_id", user.ID, "status", user.SubscriptionStatus)

	// isComped reflects a currently-active admin manual tier override (Issue #1413). A comp
	// dominates a trial and grants synthetic access when no real Paddle subscription exists,
	// so it must be computed before the trial/status branches below decide how to respond.
	//
	// Round-5 finding: manual_tier_override is an unconstrained VARCHAR(50) with no CHECK
	// constraint. Use EffectiveStatusWithActiveOverride (not HasActiveManualOverride alone) and
	// reject a malformed override the same way GetEffectiveSubscriptionStatusByUserID does on
	// the access-granting path (profile_repository.go) — this REPRESENTATION path must never
	// return a synthetic active-comp payload the read-only middleware would then 403 on write.
	_, overrideValue, isComped := user.EffectiveStatusWithActiveOverride()
	if isComped && h.rejectedOverride(user.ID, overrideValue, "subscription/current response") {
		isComped = false
	}

	// Handle trial users separately — unless a comp is in effect, in which case the comp wins.
	if user.SubscriptionStatus == "trial" && !isComped {
		user.ComputeTrialDaysRemaining()

		// Fallback: If TrialEndsAt is nil but status is "trial", compute from CreatedAt + 60 days
		if user.TrialEndsAt == nil {
			defaultTrialEnd := user.CreatedAt.AddDate(0, 0, 60)
			user.TrialEndsAt = &defaultTrialEnd
			user.ComputeTrialDaysRemaining()
			h.logger.Warn("trial_ends_at was nil for trial user, computed fallback",
				"user_id", user.ID,
				"created_at", user.CreatedAt,
				"computed_trial_ends_at", defaultTrialEnd,
				"trial_days_remaining", user.TrialDaysRemaining,
			)
		}

		out := &GetCurrentSubscriptionOutput{}
		out.Body.Status = "trial"
		// Issue #1413 (this task — tier axis): this branch is only reached when !isComped (see
		// the guard above), so isComped/overrideValue already reflect whether the override was
		// live AND validly-configured — reusing them here (rather than an unvalidated
		// ComputeEffectiveTier() call) means Tier can never disagree with IsComped about
		// whether the override in effect is trustworthy.
		out.Body.Tier = user.SubscriptionTier
		out.Body.Amount = "0.00"
		out.Body.Currency = "USD"
		out.Body.TrialDaysRemaining = user.TrialDaysRemaining
		if user.TrialEndsAt != nil {
			trialDate := user.TrialEndsAt.Format("2006-01-02")
			out.Body.TrialEndsAt = trialDate
			out.Body.CurrentPeriodEnd = trialDate
		}
		h.logger.Info("returning trial subscription info", "user_id", user.ID, "trial_days_remaining", user.TrialDaysRemaining)
		return out, nil
	}

	// Users without a Paddle subscription (e.g., inactive/never subscribed) get a 404 —
	// unless a comp grants them synthetic access (Issue #1413).
	// Cancelled and past_due users still have a subscription in Paddle we can fetch.
	switch user.SubscriptionStatus {
	case "active", "trialing", "cancelled", "past_due":
		// proceed to Paddle lookup below
	default:
		return h.compedOrNotFound(user, isComped)
	}

	subscriptionID, err := h.getActiveSubscriptionID(ctx, user.ID)
	if err != nil {
		h.logger.Error("no Paddle subscription ID for user", "user_id", user.ID, "error", err)
		return h.compedOrNotFound(user, isComped)
	}

	subscription, err := h.paddleService.GetSubscription(ctx, subscriptionID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to get subscription %s for user %s: %w", subscriptionID, user.ID, err))
	}

	h.logger.Info("subscription retrieved", "user_id", user.ID, "subscription_id", subscription.ID)

	out := &GetCurrentSubscriptionOutput{}
	out.Body.SubscriptionID = subscription.ID
	out.Body.Status = string(subscription.Status)
	// Issue #1413 (this task — tier axis): reuse isComped/overrideValue (already validated
	// above) instead of an unvalidated ComputeEffectiveTier() call — a malformed override must
	// fall back to the raw subscription tier here too, never surface the garbage string.
	if isComped {
		out.Body.Tier = overrideValue
	} else {
		out.Body.Tier = user.SubscriptionTier
	}
	out.Body.BillingInterval = string(subscription.BillingCycle.Interval)
	if subscription.CurrentBillingPeriod != nil {
		out.Body.CurrentPeriodEnd = subscription.CurrentBillingPeriod.EndsAt
		out.Body.NextBillingDate = subscription.CurrentBillingPeriod.EndsAt
	}
	if len(subscription.Items) > 0 {
		out.Body.Amount = subscription.Items[0].Price.UnitPrice.Amount
	}
	out.Body.Currency = string(subscription.CurrencyCode)
	if subscription.ScheduledChange != nil && subscription.ScheduledChange.Action == paddle.ScheduledChangeActionCancel {
		out.Body.CancelAtPeriodEnd = true
		out.Body.ScheduledChangeAt = subscription.ScheduledChange.EffectiveAt
	}
	// A comp layered on top of a real Paddle subscription (e.g. past_due/cancelled): billing
	// truth from Paddle is preserved above unchanged, the comp is only annotated (Issue #1413).
	out.Body.IsComped = isComped
	if isComped && user.ManualTierEndsAt != nil {
		out.Body.CompEndsAt = user.ManualTierEndsAt.Format(time.RFC3339)
	}
	return out, nil
}

// isConfiguredOverrideTier reports whether tierName names a real, currently-configured
// subscription tier. HandleGetCurrentSubscription uses it so a malformed manual_tier_override
// can't produce a synthetic active-comp payload — the same rule
// GetEffectiveSubscriptionStatusByUserID enforces via its injected overrideTierValidator on the
// access-granting path (profile_repository.go's SetOverrideTierValidator), reused here directly
// via this handler's own tierConfig rather than a second func(string) bool seam, since this file
// already validates tier names the same way at every other call site (see e.g. the tierConfig
// lookups above in getPaddlePriceIDFromConfig/HandleUpdateSubscription).
//
// Nil-safe: h.tierConfig is always wired in production (main.go os.Exit(1)s at startup if the
// tier config fails to load — see SetOverrideTierValidator's doc comment in
// profile_repository.go for the identical reasoning), so a nil tierConfig only occurs in a
// handler built without one (e.g. a test that never exercises comp-adjacent code), in which case
// validation is skipped — permissive, not fail-closed — matching that same nil-safe convention.
func (h *PaddleCheckoutHandler) isConfiguredOverrideTier(tierName string) bool {
	if h.tierConfig == nil {
		return true
	}
	_, err := h.tierConfig.GetTier(domain.TierName(tierName))
	return err == nil
}

// rejectedOverride reports whether overrideValue fails isConfiguredOverrideTier, logging the
// shared warning shape when it does. Consolidates the identical "validate, warn, fall back"
// block that used to be pasted separately in checkDowngradeSeatCapacity (tier axis) and
// HandleGetCurrentSubscription (status axis) — both call sites already used the exact same
// isConfiguredOverrideTier check and log message, differing only in which axis the caller resets
// on rejection, so callerContext is the only thing that still varies between them.
func (h *PaddleCheckoutHandler) rejectedOverride(userID, overrideValue, callerContext string) bool {
	if h.isConfiguredOverrideTier(overrideValue) {
		return false
	}
	h.logger.Warn("rejecting manual tier override in "+callerContext+": value does not name a configured tier",
		"user_id", userID, "manual_tier_override", overrideValue)
	return true
}

// compedOrNotFound returns the synthetic comped response when isComped is true, otherwise the
// standard 404 — the shared outcome for both "no Paddle subscription at all" branches in
// HandleGetCurrentSubscription (unrecognized status, and a missing PaddleSubscriptionID).
func (h *PaddleCheckoutHandler) compedOrNotFound(user *domain.User, isComped bool) (*GetCurrentSubscriptionOutput, error) {
	if isComped {
		return h.compedSubscriptionResponse(user), nil
	}
	return nil, huma.Error404NotFound("No subscription found")
}

// compedSubscriptionResponse builds the synthetic 200 response for a comped user (Issue
// #1413) who has no real Paddle subscription to return — i.e. the paths that would
// otherwise 404. status is reported as "active" (not a new enum value) so existing
// allow-list consumers keep working; is_comped carries the real signal.
//
// next_billing_date, current_period_end, and amount are deliberately left absent/zero: the
// frontend computes next_billing_date ?? current_period_end, so populating either would
// render a fake upcoming charge for someone who is not being charged.
func (h *PaddleCheckoutHandler) compedSubscriptionResponse(user *domain.User) *GetCurrentSubscriptionOutput {
	out := &GetCurrentSubscriptionOutput{}
	out.Body.Status = "active"
	out.Body.Tier = user.ComputeEffectiveTier()
	out.Body.Currency = "USD"
	out.Body.IsComped = true
	if user.ManualTierEndsAt != nil {
		out.Body.CompEndsAt = user.ManualTierEndsAt.Format(time.RFC3339)
	}
	return out
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
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.HandleCreateCheckoutSession)

	// POST /api/v1/subscription/preview-update - Preview subscription update
	huma.Register(api, huma.Operation{
		OperationID: "preview-paddle-subscription-update",
		Method:      "POST",
		Path:        "/api/v1/subscription/preview-update",
		Summary:     "Preview subscription tier change",
		Description: "Calculates proration amount when upgrading or downgrading subscription tier. Shows immediate charge/credit and next billing details.",
		Tags:        []string{"Subscription", "Paddle"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.HandlePreviewSubscriptionUpdate)

	// POST /api/v1/subscription/update - Update subscription
	huma.Register(api, huma.Operation{
		OperationID: "update-paddle-subscription",
		Method:      "POST",
		Path:        "/api/v1/subscription/update",
		Summary:     "Update subscription tier",
		Description: "Updates subscription to a different tier or billing interval. Handles proration automatically.",
		Tags:        []string{"Subscription", "Paddle"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.HandleUpdateSubscription)

	// POST /api/v1/subscription/cancel - Cancel subscription
	huma.Register(api, huma.Operation{
		OperationID: "cancel-paddle-subscription",
		Method:      "POST",
		Path:        "/api/v1/subscription/cancel",
		Summary:     "Cancel subscription",
		Description: "Cancels subscription at end of current billing period. User retains access until period ends.",
		Tags:        []string{"Subscription", "Paddle"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.HandleCancelSubscription)

	// GET /api/v1/subscription/billing-portal - Get billing portal URL
	huma.Register(api, huma.Operation{
		OperationID: "get-billing-portal-url",
		Method:      "GET",
		Path:        "/api/v1/subscription/billing-portal",
		Summary:     "Get billing portal URL",
		Description: "Returns URL to Paddle billing portal where users can manage payment methods and view billing history.",
		Tags:        []string{"Subscription", "Paddle"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.HandleGetBillingPortalURL)

	// GET /api/v1/subscription/current - Get current subscription
	huma.Register(api, huma.Operation{
		OperationID: "get-current-subscription",
		Method:      "GET",
		Path:        "/api/v1/subscription/current",
		Summary:     "Get current subscription",
		Description: "Fetches current subscription details from Paddle API with live status, billing info, and tier information.",
		Tags:        []string{"Subscription", "Paddle"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.HandleGetCurrentSubscription)
}
