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

// PaddleCheckoutHandler handles Paddle checkout and subscription management operations.
type PaddleCheckoutHandler struct {
	rcBilling       revenueCatBillingReader
	paddleService   services.PaddleService
	profileRepo     repository.ProfileRepository
	config          *config.Config
	tierConfig      *config.TierConfig
	logger          *slog.Logger
	entitlementRepo checkoutEntitlementReader // #1367 — Phase 3: nil skips the covered-member guard
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
// lookups above in getPaddlePriceIDFromConfig).
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
// block that used to be pasted separately in HandleGetCurrentSubscription (status axis) — the
// call site already used the exact same isConfiguredOverrideTier check and log message, so
// callerContext is the only thing that varies.
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
