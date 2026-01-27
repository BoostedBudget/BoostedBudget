// Package services provides business logic and external service integrations.
// This file implements PaddleService for Paddle payment processing integration.
package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/PaddleHQ/paddle-go-sdk/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
)

var (
	// ErrPaddleNotConfigured is returned when required Paddle credentials (API key) are missing.
	ErrPaddleNotConfigured = errors.New("paddle API key not configured")

	// ErrInvalidPaddleEnv is returned when the PADDLE_ENVIRONMENT value is not recognized.
	// Valid values: "sandbox" (testing), "production" (live).
	ErrInvalidPaddleEnv = errors.New("invalid paddle environment")

	// ErrCustomerNotFound is returned when attempting operations on a non-existent Paddle customer.
	ErrCustomerNotFound = errors.New("paddle customer not found")

	// ErrSubscriptionNotFound is returned when attempting operations on a non-existent subscription.
	ErrSubscriptionNotFound = errors.New("paddle subscription not found")
)

// PaddleService defines the interface for Paddle payment operations.
// Provides methods for customer management, subscription lifecycle, and checkout sessions.
type PaddleService interface {
	// CreateCustomer creates a new Paddle customer for the specified user.
	// Returns the Paddle customer ID (e.g., "ctm_01h...") to store in database.
	CreateCustomer(ctx context.Context, userID, email string) (string, error)

	// GetCustomer retrieves a Paddle customer by ID.
	GetCustomer(ctx context.Context, customerID string) (*paddle.Customer, error)

	// CreateCheckoutSession creates a checkout session for subscription purchase.
	// Returns checkout URL for Paddle.js Overlay or redirect.
	CreateCheckoutSession(
		ctx context.Context,
		customerID, priceID, email, successURL string,
	) (checkoutURL, transactionID string, err error)

	// GetSubscription retrieves subscription details from Paddle.
	GetSubscription(ctx context.Context, subscriptionID string) (*paddle.Subscription, error)

	// UpdateSubscription changes a subscription to a different price tier.
	// Handles proration automatically based on Paddle's billing rules.
	UpdateSubscription(
		ctx context.Context,
		subscriptionID, newPriceID, prorationBillingMode string,
	) (*paddle.Subscription, error)

	// CancelSubscription cancels a subscription at the end of current billing period.
	CancelSubscription(ctx context.Context, subscriptionID string) error

	// PreviewUpdate calculates proration for a subscription tier change.
	// Shows what the customer will be charged/credited immediately.
	PreviewUpdate(
		ctx context.Context,
		subscriptionID, newPriceID string,
	) (prorationAmount, currency, nextChargeDate string, err error)

	// GetClient returns the underlying Paddle SDK.
	// Useful for operations not covered by this service interface.
	GetClient() *paddle.SDK
}

// paddleService implements PaddleService with a configured Paddle SDK client
type paddleService struct {
	client *paddle.SDK
	config *config.Config
	logger *slog.Logger
}

// NewPaddleService creates a new PaddleService with validated configuration.
//
// Validates that required credentials are present and initializes the Paddle client
// with the appropriate environment (sandbox or production). Returns an error if:
//   - PaddleAPIKey is empty (ErrPaddleNotConfigured)
//   - PaddleEnvironment is not "sandbox" or "production" (ErrInvalidPaddleEnv)
//
// On success, returns a fully initialized PaddleService ready for API calls.
func NewPaddleService(cfg *config.Config, logger *slog.Logger) (PaddleService, error) {
	// Validate required credentials
	if cfg.PaddleAPIKey == "" {
		return nil, fmt.Errorf("%w: PADDLE_API_KEY is required", ErrPaddleNotConfigured)
	}

	// Validate environment
	if cfg.PaddleEnvironment != "sandbox" && cfg.PaddleEnvironment != "production" {
		return nil, fmt.Errorf("%w: must be 'sandbox' or 'production', got '%s'",
			ErrInvalidPaddleEnv, cfg.PaddleEnvironment)
	}

	// Initialize Paddle SDK
	var client *paddle.SDK
	var err error

	if cfg.PaddleEnvironment == "production" {
		client, err = paddle.New(cfg.PaddleAPIKey)
	} else {
		client, err = paddle.NewSandbox(cfg.PaddleAPIKey)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to initialize Paddle SDK: %w", err)
	}

	logger.Info("Paddle service initialized",
		"environment", cfg.PaddleEnvironment,
	)

	return &paddleService{
		client: client,
		config: cfg,
		logger: logger,
	}, nil
}

// GetClient returns the initialized Paddle SDK.
func (s *paddleService) GetClient() *paddle.SDK {
	return s.client
}

// CreateCustomer creates a new Paddle customer.
func (s *paddleService) CreateCustomer(ctx context.Context, userID, email string) (string, error) {
	s.logger.Info("Creating Paddle customer",
		"user_id", userID,
		"email", email,
	)

	// Create customer request
	req := &paddle.CreateCustomerRequest{
		Email: email,
		CustomData: map[string]interface{}{
			"user_id": userID,
		},
	}

	// Call Paddle API
	customer, err := s.client.CreateCustomer(ctx, req)
	if err != nil {
		s.logger.Error("Failed to create Paddle customer",
			"user_id", userID,
			"error", err,
		)
		return "", fmt.Errorf("failed to create Paddle customer: %w", err)
	}

	s.logger.Info("Paddle customer created successfully",
		"user_id", userID,
		"customer_id", customer.ID,
	)

	return customer.ID, nil
}

// GetCustomer retrieves a Paddle customer by ID.
func (s *paddleService) GetCustomer(ctx context.Context, customerID string) (*paddle.Customer, error) {
	s.logger.Debug("Fetching Paddle customer",
		"customer_id", customerID,
	)

	req := &paddle.GetCustomerRequest{
		CustomerID: customerID,
	}

	customer, err := s.client.GetCustomer(ctx, req)
	if err != nil {
		s.logger.Error("Failed to fetch Paddle customer",
			"customer_id", customerID,
			"error", err,
		)
		return nil, fmt.Errorf("failed to fetch Paddle customer: %w", err)
	}

	return customer, nil
}

// CreateCheckoutSession creates a Paddle checkout session for subscription purchase.
// Issue #45: Paddle Payment Service Implementation
func (s *paddleService) CreateCheckoutSession(
	ctx context.Context,
	customerID, priceID, email, successURL string,
) (checkoutURL, transactionID string, err error) {
	s.logger.Info("creating Paddle checkout session",
		"customer_id", customerID,
		"price_id", priceID,
	)

	// Build transaction item from catalog (subscription price)
	item := paddle.NewCreateTransactionItemsTransactionItemFromCatalog(
		&paddle.TransactionItemFromCatalog{
			PriceID:  priceID,
			Quantity: 1,
		},
	)

	// Create checkout request
	req := &paddle.CreateTransactionRequest{
		Items: []paddle.CreateTransactionItems{*item},
	}

	// Set customer ID if provided
	if customerID != "" {
		req.CustomerID = &customerID
	}

	// Call Paddle API to create transaction (which includes checkout URL)
	transaction, err := s.client.CreateTransaction(ctx, req)
	if err != nil {
		s.logger.Error("failed to create Paddle transaction",
			"customer_id", customerID,
			"price_id", priceID,
			"error", err,
		)
		return "", "", fmt.Errorf("failed to create Paddle transaction: %w", err)
	}

	// Extract checkout URL
	if transaction.Checkout == nil || transaction.Checkout.URL == nil {
		s.logger.Error("Paddle transaction missing checkout URL",
			"transaction_id", transaction.ID,
		)
		return "", "", fmt.Errorf("Paddle transaction missing checkout URL")
	}

	s.logger.Info("Paddle checkout session created successfully",
		"transaction_id", transaction.ID,
		"checkout_url", *transaction.Checkout.URL,
	)

	return *transaction.Checkout.URL, transaction.ID, nil
}

// GetSubscription retrieves subscription details from Paddle.
func (s *paddleService) GetSubscription(ctx context.Context, subscriptionID string) (*paddle.Subscription, error) {
	s.logger.Debug("Fetching Paddle subscription",
		"subscription_id", subscriptionID,
	)

	req := &paddle.GetSubscriptionRequest{
		SubscriptionID: subscriptionID,
	}

	subscription, err := s.client.GetSubscription(ctx, req)
	if err != nil {
		s.logger.Error("Failed to fetch subscription",
			"subscription_id", subscriptionID,
			"error", err,
		)
		return nil, fmt.Errorf("failed to fetch subscription: %w", err)
	}

	return subscription, nil
}

// UpdateSubscription changes a subscription to a different price tier.
// Issue #45: Paddle Payment Service Implementation
func (s *paddleService) UpdateSubscription(
	ctx context.Context,
	subscriptionID, newPriceID, prorationBillingMode string,
) (*paddle.Subscription, error) {
	s.logger.Info("updating Paddle subscription",
		"subscription_id", subscriptionID,
		"new_price_id", newPriceID,
		"proration_mode", prorationBillingMode,
	)

	// Fetch current subscription to get existing items
	currentSub, err := s.GetSubscription(ctx, subscriptionID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch current subscription: %w", err)
	}

	// Parse proration billing mode
	var prorationMode paddle.ProrationBillingMode
	switch prorationBillingMode {
	case "prorated_immediately":
		prorationMode = paddle.ProrationBillingModeProratedImmediately
	case "full_immediately":
		prorationMode = paddle.ProrationBillingModeFullImmediately
	case "full_next_billing_period":
		prorationMode = paddle.ProrationBillingModeFullNextBillingPeriod
	default:
		prorationMode = paddle.ProrationBillingModeProratedImmediately
	}

	// Wrap proration mode in PatchField
	patchedProrationMode := paddle.NewPatchField(prorationMode)

	// Build updated items array with new price ID using union type
	var updatedItems []paddle.UpdateSubscriptionItems
	var catalogItem *paddle.SubscriptionUpdateItemFromCatalog
	if len(currentSub.Items) > 0 {
		// Update first item with new price ID
		catalogItem = &paddle.SubscriptionUpdateItemFromCatalog{
			PriceID:  newPriceID,
			Quantity: currentSub.Items[0].Quantity,
		}
	} else {
		// No existing items - create new item
		catalogItem = &paddle.SubscriptionUpdateItemFromCatalog{
			PriceID:  newPriceID,
			Quantity: 1,
		}
	}
	updatedItems = append(updatedItems, *paddle.NewUpdateSubscriptionItemsSubscriptionUpdateItemFromCatalog(catalogItem))

	// Wrap items in PatchField
	patchedItems := paddle.NewPatchField(updatedItems)

	// Create update request
	req := &paddle.UpdateSubscriptionRequest{
		SubscriptionID:       subscriptionID,
		Items:                patchedItems,
		ProrationBillingMode: patchedProrationMode,
	}

	// Call Paddle API
	updatedSub, err := s.client.UpdateSubscription(ctx, req)
	if err != nil {
		s.logger.Error("failed to update Paddle subscription",
			"subscription_id", subscriptionID,
			"error", err,
		)
		return nil, fmt.Errorf("failed to update Paddle subscription: %w", err)
	}

	s.logger.Info("Paddle subscription updated successfully",
		"subscription_id", subscriptionID,
		"status", updatedSub.Status,
	)

	return updatedSub, nil
}

// CancelSubscription cancels a subscription at the end of the billing period.
// Issue #45: Paddle Payment Service Implementation
func (s *paddleService) CancelSubscription(ctx context.Context, subscriptionID string) error {
	s.logger.Info("canceling Paddle subscription",
		"subscription_id", subscriptionID,
	)

	// Cancel at end of billing period (not immediately)
	effectiveFrom := paddle.EffectiveFromNextBillingPeriod

	// Create cancel request
	req := &paddle.CancelSubscriptionRequest{
		SubscriptionID: subscriptionID,
		EffectiveFrom:  &effectiveFrom,
	}

	// Call Paddle API
	canceledSub, err := s.client.CancelSubscription(ctx, req)
	if err != nil {
		s.logger.Error("failed to cancel Paddle subscription",
			"subscription_id", subscriptionID,
			"error", err,
		)
		return fmt.Errorf("failed to cancel Paddle subscription: %w", err)
	}

	s.logger.Info("Paddle subscription canceled successfully",
		"subscription_id", subscriptionID,
		"status", canceledSub.Status,
		"scheduled_change", canceledSub.ScheduledChange,
	)

	return nil
}

// PreviewUpdate calculates proration for a subscription tier change.
// Issue #45: Paddle Payment Service Implementation
func (s *paddleService) PreviewUpdate(
	ctx context.Context,
	subscriptionID, newPriceID string,
) (prorationAmount, currency, nextChargeDate string, err error) {
	s.logger.Info("previewing Paddle subscription update",
		"subscription_id", subscriptionID,
		"new_price_id", newPriceID,
	)

	// Fetch current subscription
	currentSub, err := s.GetSubscription(ctx, subscriptionID)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to fetch current subscription: %w", err)
	}

	// Build updated items array using union type
	var updatedItems []paddle.PreviewSubscriptionUpdateItems
	var catalogItem *paddle.SubscriptionUpdateItemFromCatalog
	if len(currentSub.Items) > 0 {
		catalogItem = &paddle.SubscriptionUpdateItemFromCatalog{
			PriceID:  newPriceID,
			Quantity: currentSub.Items[0].Quantity,
		}
	} else {
		catalogItem = &paddle.SubscriptionUpdateItemFromCatalog{
			PriceID:  newPriceID,
			Quantity: 1,
		}
	}
	updatedItems = append(updatedItems, *paddle.NewPreviewSubscriptionUpdateItemsSubscriptionUpdateItemFromCatalog(catalogItem))

	// Wrap items and proration mode in PatchField
	patchedItems := paddle.NewPatchField(updatedItems)
	prorationMode := paddle.ProrationBillingModeProratedImmediately
	patchedProrationMode := paddle.NewPatchField(prorationMode)

	// Create preview request with prorated billing
	req := &paddle.PreviewSubscriptionUpdateRequest{
		SubscriptionID:       subscriptionID,
		Items:                patchedItems,
		ProrationBillingMode: patchedProrationMode,
	}

	// Call Paddle API
	preview, err := s.client.PreviewSubscriptionUpdate(ctx, req)
	if err != nil {
		s.logger.Error("failed to preview subscription update",
			"subscription_id", subscriptionID,
			"error", err,
		)
		return "", "", "", fmt.Errorf("failed to preview subscription update: %w", err)
	}

	// Extract proration amount from immediate transaction
	var prorationAmt, curr string
	if preview.ImmediateTransaction != nil {
		prorationAmt = preview.ImmediateTransaction.Details.Totals.Total
		curr = string(preview.CurrencyCode)
	}

	// Extract next charge date from next transaction
	var nextCharge string
	if preview.NextTransaction != nil {
		nextCharge = preview.NextTransaction.BillingPeriod.StartsAt
	}

	s.logger.Info("subscription update preview completed",
		"subscription_id", subscriptionID,
		"proration_amount", prorationAmt,
		"currency", curr,
		"next_charge_date", nextCharge,
	)

	return prorationAmt, curr, nextCharge, nil
}
