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

const (
	// paddleBillingPortalProdURL is the Paddle customer portal URL for production
	paddleBillingPortalProdURL = "https://buy.paddle.com/customer/portal"

	// paddleBillingPortalSandboxURL is the Paddle customer portal URL for sandbox
	paddleBillingPortalSandboxURL = "https://sandbox-buy.paddle.com/customer/portal"
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

	// ErrPaddleCustomerOwnershipMismatch is returned by EnsureCustomer when an archived Paddle customer
	// is found by email but its custom_data.user_id does not match the calling user. Reusing the customer
	// would attach a different user's billing history to the current user.
	ErrPaddleCustomerOwnershipMismatch = errors.New("archived paddle customer belongs to a different user")
)

// PaddleService defines the interface for Paddle payment operations.
// Provides methods for customer management, subscription lifecycle, and checkout sessions.
type PaddleService interface {
	// CreateCustomer creates a new Paddle customer for the specified user.
	// Returns the Paddle customer ID (e.g., "ctm_01h...") to store in database.
	CreateCustomer(ctx context.Context, userID, email string) (string, error)

	// EnsureCustomer returns a Paddle customer ID for the specified user email.
	// If a local record is missing but Paddle already has a customer for the email,
	// the existing customer is reused instead of treating the conflict as fatal.
	EnsureCustomer(ctx context.Context, userID, email string) (string, error)

	// GetCustomer retrieves a Paddle customer by ID.
	GetCustomer(ctx context.Context, customerID string) (*paddle.Customer, error)

	// CreateCheckoutSession creates a checkout session for subscription purchase.
	// Returns checkout URL for Paddle.js Overlay or redirect.
	// userID is passed to Paddle as custom_data for webhook processing.
	CreateCheckoutSession(
		ctx context.Context,
		customerID, priceID, email, successURL, userID string,
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

	// CancelSubscriptionImmediately cancels a subscription effective immediately (no grace period).
	// Issue #1006: Paddle cleanup during account deletion.
	CancelSubscriptionImmediately(ctx context.Context, subscriptionID string) error

	// PreviewUpdate calculates proration for a subscription tier change.
	PreviewUpdate(
		ctx context.Context,
		subscriptionID, newPriceID string,
	) (prorationAmount, currency, nextChargeDate string, err error)

	// GetBillingPortalURL generates a URL for the Paddle billing portal session.
	GetBillingPortalURL(ctx context.Context, customerID string) (string, error)

	// UpdateCustomerEmail updates the email address of an existing Paddle customer.
	UpdateCustomerEmail(ctx context.Context, customerID, email string) error

	// ArchiveAndAnonymizeCustomer archives the customer and anonymizes their email (GDPR PII erasure).
	// Issue #1006: Paddle cleanup during account deletion.
	ArchiveAndAnonymizeCustomer(ctx context.Context, customerID, anonymizedEmail string) error

	// GetClient returns the underlying Paddle SDK.
	// Useful for operations not covered by this service interface.
	GetClient() *paddle.SDK
}

type paddleClient interface {
	CreateCustomer(ctx context.Context, req *paddle.CreateCustomerRequest) (*paddle.Customer, error)
	ListCustomersByEmail(ctx context.Context, email string) ([]*paddle.Customer, error)
	GetCustomer(ctx context.Context, req *paddle.GetCustomerRequest) (*paddle.Customer, error)
	UpdateCustomer(ctx context.Context, req *paddle.UpdateCustomerRequest) (*paddle.Customer, error)
	CreateTransaction(ctx context.Context, req *paddle.CreateTransactionRequest) (*paddle.Transaction, error)
	GetSubscription(ctx context.Context, req *paddle.GetSubscriptionRequest) (*paddle.Subscription, error)
	UpdateSubscription(ctx context.Context, req *paddle.UpdateSubscriptionRequest) (*paddle.Subscription, error)
	CancelSubscription(ctx context.Context, req *paddle.CancelSubscriptionRequest) (*paddle.Subscription, error)
	PreviewSubscriptionUpdate(ctx context.Context, req *paddle.PreviewSubscriptionUpdateRequest) (*paddle.SubscriptionPreview, error)
}

type paddleSDKClient struct {
	sdk *paddle.SDK
}

func (c *paddleSDKClient) CreateCustomer(ctx context.Context, req *paddle.CreateCustomerRequest) (*paddle.Customer, error) {
	return c.sdk.CreateCustomer(ctx, req)
}

func (c *paddleSDKClient) ListCustomersByEmail(ctx context.Context, email string) ([]*paddle.Customer, error) {
	collection, err := c.sdk.ListCustomers(ctx, &paddle.ListCustomersRequest{
		Email:  []string{email},
		Status: []string{string(paddle.StatusActive), string(paddle.StatusArchived)},
	})
	if err != nil {
		return nil, err
	}

	customers := make([]*paddle.Customer, 0, 1)
	if err := collection.Iter(ctx, func(customer *paddle.Customer) (bool, error) {
		customers = append(customers, customer)
		return true, nil
	}); err != nil {
		return nil, err
	}

	return customers, nil
}

func (c *paddleSDKClient) GetCustomer(ctx context.Context, req *paddle.GetCustomerRequest) (*paddle.Customer, error) {
	return c.sdk.GetCustomer(ctx, req)
}

func (c *paddleSDKClient) UpdateCustomer(ctx context.Context, req *paddle.UpdateCustomerRequest) (*paddle.Customer, error) {
	return c.sdk.UpdateCustomer(ctx, req)
}

func (c *paddleSDKClient) CreateTransaction(ctx context.Context, req *paddle.CreateTransactionRequest) (*paddle.Transaction, error) {
	return c.sdk.CreateTransaction(ctx, req)
}

func (c *paddleSDKClient) GetSubscription(ctx context.Context, req *paddle.GetSubscriptionRequest) (*paddle.Subscription, error) {
	return c.sdk.GetSubscription(ctx, req)
}

func (c *paddleSDKClient) UpdateSubscription(ctx context.Context, req *paddle.UpdateSubscriptionRequest) (*paddle.Subscription, error) {
	return c.sdk.UpdateSubscription(ctx, req)
}

func (c *paddleSDKClient) CancelSubscription(ctx context.Context, req *paddle.CancelSubscriptionRequest) (*paddle.Subscription, error) {
	return c.sdk.CancelSubscription(ctx, req)
}

func (c *paddleSDKClient) PreviewSubscriptionUpdate(ctx context.Context, req *paddle.PreviewSubscriptionUpdateRequest) (*paddle.SubscriptionPreview, error) {
	return c.sdk.PreviewSubscriptionUpdate(ctx, req)
}

// paddleService implements PaddleService with a configured Paddle SDK client
type paddleService struct {
	client    paddleClient
	rawClient *paddle.SDK
	config    *config.Config
	logger    *slog.Logger
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
		client:    &paddleSDKClient{sdk: client},
		rawClient: client,
		config:    cfg,
		logger:    logger,
	}, nil
}

// GetClient returns the initialized Paddle SDK.
func (s *paddleService) GetClient() *paddle.SDK {
	return s.rawClient
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
		if errors.Is(err, paddle.ErrCustomerAlreadyExists) {
			return "", fmt.Errorf("failed to create Paddle customer: %w", err)
		}

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

// EnsureCustomer returns a usable Paddle customer ID for the given user email.
func (s *paddleService) EnsureCustomer(ctx context.Context, userID, email string) (string, error) {
	customerID, err := s.CreateCustomer(ctx, userID, email)
	if err == nil {
		return customerID, nil
	}

	if !errors.Is(err, paddle.ErrCustomerAlreadyExists) {
		return "", err
	}

	s.logger.Warn("Paddle customer already exists; resolving by email",
		"user_id", userID,
		"email", email,
	)

	customers, lookupErr := s.client.ListCustomersByEmail(ctx, email)
	if lookupErr != nil {
		return "", fmt.Errorf("failed to look up existing Paddle customer by email: %w", lookupErr)
	}

	if len(customers) != 1 {
		return "", fmt.Errorf("expected exactly 1 Paddle customer for email %s, got %d", email, len(customers))
	}

	if customers[0].ID == "" {
		return "", fmt.Errorf("existing Paddle customer for email %s is missing an ID", email)
	}

	if customers[0].Status == paddle.StatusArchived {
		return s.unarchiveCustomerForUser(ctx, customers[0], userID, email)
	}

	// Active-customer reuse: Clerk enforces email uniqueness for active accounts, so an active
	// Paddle customer for this email belongs to the current user by construction. No ownership
	// check is needed here — the archived path is different because the email may have been
	// claimed by a new Clerk user after the original user deleted their account.
	return customers[0].ID, nil
}

// unarchiveCustomerForUser reactivates an archived Paddle customer, but only when the archived
// record's custom_data.user_id matches the current user. Reusing a customer belonging to a
// different prior user would expose that user's Paddle portal history to the caller.
func (s *paddleService) unarchiveCustomerForUser(ctx context.Context, customer *paddle.Customer, userID, email string) (string, error) {
	ownerID, _ := customer.CustomData["user_id"].(string)
	if ownerID == "" || ownerID != userID {
		s.logger.Warn("archived Paddle customer belongs to a different user; failing closed",
			"user_id", userID,
			"customer_id", customer.ID,
			"customer_owner_id", ownerID,
		)
		return "", fmt.Errorf("paddle customer %s: %w", customer.ID, ErrPaddleCustomerOwnershipMismatch)
	}

	_, err := s.client.UpdateCustomer(ctx, &paddle.UpdateCustomerRequest{
		CustomerID: customer.ID,
		Status:     paddle.NewPatchField(paddle.StatusActive),
	})
	if err != nil {
		return "", fmt.Errorf("failed to unarchive Paddle customer %s: %w", customer.ID, err)
	}

	s.logger.Info("Paddle customer unarchived and reused",
		"user_id", userID,
		"customer_id", customer.ID,
		"previous_status", "archived",
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
func (s *paddleService) CreateCheckoutSession(
	ctx context.Context,
	customerID, priceID, email, successURL, userID string,
) (checkoutURL, transactionID string, err error) {
	s.logger.Info("creating Paddle checkout session",
		"customer_id", customerID,
		"price_id", priceID,
		"user_id", userID,
	)

	item := paddle.NewCreateTransactionItemsTransactionItemFromCatalog(
		&paddle.TransactionItemFromCatalog{
			PriceID:  priceID,
			Quantity: 1,
		},
	)

	req := &paddle.CreateTransactionRequest{
		Items: []paddle.CreateTransactionItems{*item},
		CustomData: paddle.CustomData{
			"user_id": userID,
		},
	}

	if customerID != "" {
		req.CustomerID = &customerID
	}

	transaction, err := s.client.CreateTransaction(ctx, req)
	if err != nil {
		s.logger.Error("failed to create Paddle transaction",
			"customer_id", customerID,
			"price_id", priceID,
			"error", err,
		)
		return "", "", fmt.Errorf("failed to create Paddle transaction: %w", err)
	}

	if transaction.Checkout == nil || transaction.Checkout.URL == nil {
		s.logger.Error("Paddle transaction missing checkout URL",
			"transaction_id", transaction.ID,
		)
		return "", "", fmt.Errorf("Paddle transaction missing checkout URL")
	}

	s.logger.Info("Paddle checkout session created successfully",
		"transaction_id", transaction.ID,
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
func (s *paddleService) UpdateSubscription(
	ctx context.Context,
	subscriptionID, newPriceID, prorationBillingMode string,
) (*paddle.Subscription, error) {
	s.logger.Info("updating Paddle subscription",
		"subscription_id", subscriptionID,
		"new_price_id", newPriceID,
	)

	currentSub, err := s.GetSubscription(ctx, subscriptionID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch current subscription: %w", err)
	}

	prorationMode := parseProrationMode(prorationBillingMode)

	patchedProrationMode := paddle.NewPatchField(prorationMode)

	catalogItem := buildCatalogItem(newPriceID, currentSub)
	updatedItems := []paddle.UpdateSubscriptionItems{
		*paddle.NewUpdateSubscriptionItemsSubscriptionUpdateItemFromCatalog(catalogItem),
	}

	patchedItems := paddle.NewPatchField(updatedItems)

	req := &paddle.UpdateSubscriptionRequest{
		SubscriptionID:       subscriptionID,
		Items:                patchedItems,
		ProrationBillingMode: patchedProrationMode,
	}

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
func (s *paddleService) CancelSubscription(ctx context.Context, subscriptionID string) error {
	s.logger.Info("canceling Paddle subscription",
		"subscription_id", subscriptionID,
	)

	effectiveFrom := paddle.EffectiveFromNextBillingPeriod

	req := &paddle.CancelSubscriptionRequest{
		SubscriptionID: subscriptionID,
		EffectiveFrom:  &effectiveFrom,
	}

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
	)

	return nil
}

// CancelSubscriptionImmediately cancels a subscription effective immediately (used during account deletion).
// Issue #1006: Used during account deletion to stop billing with no grace period.
func (s *paddleService) CancelSubscriptionImmediately(ctx context.Context, subscriptionID string) error {
	s.logger.Info("canceling Paddle subscription immediately", "subscription_id", subscriptionID)

	effectiveFrom := paddle.EffectiveFromImmediately
	req := &paddle.CancelSubscriptionRequest{
		SubscriptionID: subscriptionID,
		EffectiveFrom:  &effectiveFrom,
	}

	canceledSub, err := s.client.CancelSubscription(ctx, req)
	if err != nil {
		// Already cancelled is idempotent success — Paddle returns this error on repeat cancels
		if errors.Is(err, paddle.ErrSubscriptionIsCanceledActionInvalid) {
			s.logger.Info("Paddle subscription already cancelled, treating as success",
				"subscription_id", subscriptionID,
			)
			return nil
		}
		s.logger.Error("failed to cancel Paddle subscription immediately",
			"subscription_id", subscriptionID,
			"error", err,
		)
		return fmt.Errorf("failed to cancel Paddle subscription immediately: %w", err)
	}

	s.logger.Info("Paddle subscription cancelled immediately",
		"subscription_id", subscriptionID,
		"status", canceledSub.Status,
	)

	return nil
}

// PreviewUpdate calculates proration for a subscription tier change.
func (s *paddleService) PreviewUpdate(
	ctx context.Context,
	subscriptionID, newPriceID string,
) (prorationAmount, currency, nextChargeDate string, err error) {
	s.logger.Info("previewing Paddle subscription update",
		"subscription_id", subscriptionID,
		"new_price_id", newPriceID,
	)

	currentSub, err := s.GetSubscription(ctx, subscriptionID)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to fetch current subscription: %w", err)
	}

	catalogItem := buildCatalogItem(newPriceID, currentSub)
	updatedItems := []paddle.PreviewSubscriptionUpdateItems{
		*paddle.NewPreviewSubscriptionUpdateItemsSubscriptionUpdateItemFromCatalog(catalogItem),
	}

	patchedItems := paddle.NewPatchField(updatedItems)
	prorationMode := paddle.ProrationBillingModeProratedImmediately
	patchedProrationMode := paddle.NewPatchField(prorationMode)

	req := &paddle.PreviewSubscriptionUpdateRequest{
		SubscriptionID:       subscriptionID,
		Items:                patchedItems,
		ProrationBillingMode: patchedProrationMode,
	}

	preview, err := s.client.PreviewSubscriptionUpdate(ctx, req)
	if err != nil {
		s.logger.Error("failed to preview subscription update",
			"subscription_id", subscriptionID,
			"error", err,
		)
		return "", "", "", fmt.Errorf("failed to preview subscription update: %w", err)
	}

	var prorationAmt, curr string
	if preview.ImmediateTransaction != nil {
		prorationAmt = preview.ImmediateTransaction.Details.Totals.Total
		curr = string(preview.CurrencyCode)
	}

	var nextCharge string
	if preview.NextTransaction != nil {
		nextCharge = preview.NextTransaction.BillingPeriod.StartsAt
	}

	s.logger.Info("subscription update preview completed",
		"subscription_id", subscriptionID,
		"proration_amount", prorationAmt,
		"currency", curr,
	)

	return prorationAmt, curr, nextCharge, nil
}

// buildCatalogItem constructs a subscription catalog item, defaulting quantity to 1 if no items exist.
func buildCatalogItem(priceID string, currentSub *paddle.Subscription) *paddle.SubscriptionUpdateItemFromCatalog {
	quantity := 1
	if len(currentSub.Items) > 0 {
		quantity = currentSub.Items[0].Quantity
	}
	return &paddle.SubscriptionUpdateItemFromCatalog{
		PriceID:  priceID,
		Quantity: quantity,
	}
}

// parseProrationMode converts a string to the corresponding ProrationBillingMode enum.
// Defaults to ProratedImmediately for unknown values.
func parseProrationMode(mode string) paddle.ProrationBillingMode {
	switch mode {
	case "prorated_immediately":
		return paddle.ProrationBillingModeProratedImmediately
	case "full_immediately":
		return paddle.ProrationBillingModeFullImmediately
	case "full_next_billing_period":
		return paddle.ProrationBillingModeFullNextBillingPeriod
	default:
		return paddle.ProrationBillingModeProratedImmediately
	}
}

// ArchiveAndAnonymizeCustomer archives the Paddle customer and anonymizes their email (GDPR PII erasure).
// Issue #1006: Used during account deletion to comply with GDPR data retention requirements.
func (s *paddleService) ArchiveAndAnonymizeCustomer(ctx context.Context, customerID, anonymizedEmail string) error {
	s.logger.Info("archiving and anonymizing Paddle customer", "customer_id", customerID)

	_, err := s.client.UpdateCustomer(ctx, &paddle.UpdateCustomerRequest{
		CustomerID: customerID,
		Email:      paddle.NewPatchField(anonymizedEmail),
		Status:     paddle.NewPatchField(paddle.StatusArchived),
	})
	if err != nil {
		// Already archived is idempotent success — Paddle returns this error on repeat archives
		if errors.Is(err, paddle.ErrEntityArchived) {
			s.logger.Info("Paddle customer already archived, treating as success",
				"customer_id", customerID,
			)
			return nil
		}
		s.logger.Error("failed to archive and anonymize Paddle customer",
			"customer_id", customerID,
			"error", err,
		)
		return fmt.Errorf("failed to archive and anonymize Paddle customer: %w", err)
	}

	s.logger.Info("Paddle customer archived and anonymized", "customer_id", customerID)
	return nil
}

// UpdateCustomerEmail updates the email address of an existing Paddle customer.
func (s *paddleService) UpdateCustomerEmail(ctx context.Context, customerID, email string) error {
	_, err := s.client.UpdateCustomer(ctx, &paddle.UpdateCustomerRequest{
		CustomerID: customerID,
		Email:      paddle.NewPatchField(email),
	})
	if err != nil {
		return fmt.Errorf("failed to update Paddle customer email: %w", err)
	}
	return nil
}

// GetBillingPortalURL generates a URL for the Paddle billing portal session.
// Returns the customer portal URL where users can manage payment methods and view billing history.
//
// Note: Paddle SDK v4 doesn't provide a dedicated portal session creation method.
// Users will be directed to Paddle's customer portal where they authenticate via email.
func (s *paddleService) GetBillingPortalURL(ctx context.Context, customerID string) (string, error) {
	s.logger.Info("generating Paddle billing portal URL",
		"customer_id", customerID,
	)

	// Validate customer exists
	if _, err := s.GetCustomer(ctx, customerID); err != nil {
		s.logger.Error("failed to validate customer for billing portal",
			"customer_id", customerID,
			"error", err,
		)
		return "", fmt.Errorf("failed to validate customer: %w", err)
	}

	// Select appropriate environment URL
	var portalURL string
	if s.config.PaddleEnvironment == "production" {
		portalURL = paddleBillingPortalProdURL
	} else {
		portalURL = paddleBillingPortalSandboxURL
	}

	s.logger.Info("Paddle billing portal URL generated",
		"customer_id", customerID,
		"environment", s.config.PaddleEnvironment,
	)

	return portalURL, nil
}
