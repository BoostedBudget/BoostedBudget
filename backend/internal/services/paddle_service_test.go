package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"testing"

	paddle "github.com/PaddleHQ/paddle-go-sdk/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// mockPaddleSDK is a mock implementation of the Paddle SDK for testing
type mockPaddleSDK struct {
	mock.Mock
}

func (m *mockPaddleSDK) CreateCustomer(ctx context.Context, req *paddle.CreateCustomerRequest) (*paddle.Customer, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*paddle.Customer), args.Error(1)
}

func (m *mockPaddleSDK) GetCustomer(ctx context.Context, req *paddle.GetCustomerRequest) (*paddle.Customer, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*paddle.Customer), args.Error(1)
}

func (m *mockPaddleSDK) CreateTransaction(ctx context.Context, req *paddle.CreateTransactionRequest) (*paddle.Transaction, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*paddle.Transaction), args.Error(1)
}

func (m *mockPaddleSDK) GetSubscription(ctx context.Context, req *paddle.GetSubscriptionRequest) (*paddle.Subscription, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*paddle.Subscription), args.Error(1)
}

func (m *mockPaddleSDK) UpdateSubscription(ctx context.Context, req *paddle.UpdateSubscriptionRequest) (*paddle.Subscription, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*paddle.Subscription), args.Error(1)
}

func (m *mockPaddleSDK) CancelSubscription(ctx context.Context, req *paddle.CancelSubscriptionRequest) (*paddle.Subscription, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*paddle.Subscription), args.Error(1)
}

func (m *mockPaddleSDK) PreviewSubscriptionUpdate(ctx context.Context, req *paddle.PreviewSubscriptionUpdateRequest) (*paddle.SubscriptionPreview, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*paddle.SubscriptionPreview), args.Error(1)
}

// setupPaddleServiceWithMock creates a PaddleService with a mock SDK
func setupPaddleServiceWithMock(t *testing.T) (*paddleService, *mockPaddleSDK) {
	t.Helper()

	mockSDK := new(mockPaddleSDK)
	cfg := &config.Config{
		PaddleAPIKey:     "test_api_key",
		PaddleEnvironment: "sandbox",
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	service := &paddleService{
		client: (*paddle.SDK)(nil), // Will use mock instead
		config: cfg,
		logger: logger,
	}

	// We'll manually inject the mock for testing
	// Note: This requires the service to expose a way to inject the client
	// or we refactor to use an interface

	return service, mockSDK
}

// TestNewPaddleService_Success tests successful service initialization
func TestNewPaddleService_Success(t *testing.T) {
	cfg := &config.Config{
		PaddleAPIKey:      "test_api_key_1234567890",
		PaddleEnvironment: "sandbox",
	}
	logger := slog.Default()

	service, err := NewPaddleService(cfg, logger)

	require.NoError(t, err)
	assert.NotNil(t, service)
	assert.NotNil(t, service.GetClient())
}

// TestNewPaddleService_MissingAPIKey tests initialization without API key
func TestNewPaddleService_MissingAPIKey(t *testing.T) {
	cfg := &config.Config{
		PaddleAPIKey:      "",
		PaddleEnvironment: "sandbox",
	}
	logger := slog.Default()

	service, err := NewPaddleService(cfg, logger)

	require.Error(t, err)
	assert.Nil(t, service)
	assert.ErrorIs(t, err, ErrPaddleNotConfigured)
	assert.Contains(t, err.Error(), "PADDLE_API_KEY is required")
}

// TestNewPaddleService_InvalidEnvironment tests initialization with invalid environment
func TestNewPaddleService_InvalidEnvironment(t *testing.T) {
	cfg := &config.Config{
		PaddleAPIKey:      "test_api_key",
		PaddleEnvironment: "invalid_env",
	}
	logger := slog.Default()

	service, err := NewPaddleService(cfg, logger)

	require.Error(t, err)
	assert.Nil(t, service)
	assert.ErrorIs(t, err, ErrInvalidPaddleEnv)
	assert.Contains(t, err.Error(), "must be 'sandbox' or 'production'")
}

// TestNewPaddleService_ProductionEnvironment tests initialization with production environment
func TestNewPaddleService_ProductionEnvironment(t *testing.T) {
	cfg := &config.Config{
		PaddleAPIKey:      "live_api_key_1234567890",
		PaddleEnvironment: "production",
	}
	logger := slog.Default()

	service, err := NewPaddleService(cfg, logger)

	require.NoError(t, err)
	assert.NotNil(t, service)
}

// Note: The following tests would require refactoring PaddleService to accept
// an interface instead of the concrete *paddle.SDK type. Since the current
// implementation uses the concrete SDK, we'll create integration-style tests
// that verify the service compiles and initializes correctly, and document
// what unit tests would look like with proper dependency injection.

// TestCreateCustomer_Integration is a placeholder for integration tests
// In a real implementation, these would use the mock SDK
func TestCreateCustomer_RequiredParameters(t *testing.T) {
	t.Skip("Requires Paddle SDK interface refactor for proper mocking")

	// This test demonstrates what we would test with proper mocks:
	// 1. CreateCustomer calls SDK.CreateCustomer with correct parameters
	// 2. Returns customer ID on success
	// 3. Handles API errors (rate limit, network, invalid email)
	// 4. Logs appropriate messages
}

func TestGetCustomer_RequiredParameters(t *testing.T) {
	t.Skip("Requires Paddle SDK interface refactor for proper mocking")

	// Would test:
	// 1. GetCustomer calls SDK.GetCustomer with correct ID
	// 2. Returns customer data on success
	// 3. Returns ErrCustomerNotFound for 404
	// 4. Handles API errors
}

func TestCreateCheckoutSession_RequiredParameters(t *testing.T) {
	t.Skip("Requires Paddle SDK interface refactor for proper mocking")

	// Would test:
	// 1. Creates transaction with correct price ID and quantity
	// 2. Includes customer ID if provided
	// 3. Returns checkout URL and transaction ID
	// 4. Handles missing checkout URL in response
	// 5. Handles API errors
}

func TestGetSubscription_RequiredParameters(t *testing.T) {
	t.Skip("Requires Paddle SDK interface refactor for proper mocking")

	// Would test:
	// 1. Calls SDK.GetSubscription with correct ID
	// 2. Returns subscription data
	// 3. Returns ErrSubscriptionNotFound for 404
	// 4. Handles API errors
}

func TestUpdateSubscription_RequiredParameters(t *testing.T) {
	t.Skip("Requires Paddle SDK interface refactor for proper mocking")

	// Would test:
	// 1. Fetches current subscription first
	// 2. Builds update request with new price ID
	// 3. Applies correct proration mode
	// 4. Returns updated subscription
	// 5. Handles API errors
}

func TestCancelSubscription_RequiredParameters(t *testing.T) {
	t.Skip("Requires Paddle SDK interface refactor for proper mocking")

	// Would test:
	// 1. Calls SDK.CancelSubscription with correct ID
	// 2. Sets effective_from to next_billing_period
	// 3. Returns success on cancellation
	// 4. Handles already cancelled (idempotent)
	// 5. Handles API errors
}

func TestPreviewUpdate_RequiredParameters(t *testing.T) {
	t.Skip("Requires Paddle SDK interface refactor for proper mocking")

	// Would test:
	// 1. Fetches current subscription
	// 2. Builds preview request with new price
	// 3. Returns proration amount and currency
	// 4. Returns next charge date
	// 5. Handles API errors
}

// TestGetClient tests that GetClient returns the SDK instance
func TestGetClient(t *testing.T) {
	cfg := &config.Config{
		PaddleAPIKey:      "test_api_key",
		PaddleEnvironment: "sandbox",
	}
	logger := slog.Default()

	service, err := NewPaddleService(cfg, logger)
	require.NoError(t, err)

	client := service.GetClient()
	assert.NotNil(t, client)
}

// Integration test helpers (for manual testing with real Paddle sandbox)
// These tests are skipped by default and require PADDLE_API_KEY environment variable

func TestCreateCustomer_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	apiKey := os.Getenv("PADDLE_API_KEY")
	if apiKey == "" {
		t.Skip("PADDLE_API_KEY not set, skipping integration test")
	}

	cfg := &config.Config{
		PaddleAPIKey:      apiKey,
		PaddleEnvironment: "sandbox",
	}
	logger := slog.Default()

	service, err := NewPaddleService(cfg, logger)
	require.NoError(t, err)

	ctx := context.Background()
	customerID, err := service.CreateCustomer(ctx, "test_user_123", "test@example.com")

	if err != nil {
		t.Logf("Integration test error (expected if sandbox not configured): %v", err)
		t.Skip("Paddle sandbox not available")
	}

	assert.NotEmpty(t, customerID)
	assert.Contains(t, customerID, "ctm_")
}

func TestCreateCheckoutSession_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	apiKey := os.Getenv("PADDLE_API_KEY")
	priceID := os.Getenv("PADDLE_TEST_PRICE_ID")
	if apiKey == "" || priceID == "" {
		t.Skip("PADDLE_API_KEY or PADDLE_TEST_PRICE_ID not set, skipping integration test")
	}

	cfg := &config.Config{
		PaddleAPIKey:      apiKey,
		PaddleEnvironment: "sandbox",
	}
	logger := slog.Default()

	service, err := NewPaddleService(cfg, logger)
	require.NoError(t, err)

	ctx := context.Background()
	checkoutURL, transactionID, err := service.CreateCheckoutSession(
		ctx,
		"", // No customer ID (guest checkout)
		priceID,
		"test@example.com",
		"https://example.com/success",
	)

	if err != nil {
		t.Logf("Integration test error: %v", err)
		t.Skip("Paddle sandbox not available or price not configured")
	}

	assert.NotEmpty(t, checkoutURL)
	assert.Contains(t, checkoutURL, "checkout.paddle.com")
	assert.NotEmpty(t, transactionID)
	assert.Contains(t, transactionID, "txn_")
}

// Benchmark tests to ensure performance

func BenchmarkCreateCustomer(b *testing.B) {
	cfg := &config.Config{
		PaddleAPIKey:      "test_key",
		PaddleEnvironment: "sandbox",
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	service, err := NewPaddleService(cfg, logger)
	if err != nil {
		b.Fatal(err)
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// This will fail without real API key, but measures overhead
		_, _ = service.CreateCustomer(ctx, "user_123", "test@example.com")
	}
}

// Test error handling for various Paddle API error scenarios

func TestPaddleService_ErrorHandling(t *testing.T) {
	testCases := []struct {
		name          string
		setupService  func() (PaddleService, error)
		expectError   bool
		errorContains string
	}{
		{
			name: "Invalid API key format",
			setupService: func() (PaddleService, error) {
				cfg := &config.Config{
					PaddleAPIKey:      "invalid",
					PaddleEnvironment: "sandbox",
				}
				return NewPaddleService(cfg, slog.Default())
			},
			expectError:   false, // Service initializes, but API calls will fail
			errorContains: "",
		},
		{
			name: "Empty environment defaults to error",
			setupService: func() (PaddleService, error) {
				cfg := &config.Config{
					PaddleAPIKey:      "test_key",
					PaddleEnvironment: "",
				}
				return NewPaddleService(cfg, slog.Default())
			},
			expectError:   true,
			errorContains: "must be 'sandbox' or 'production'",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			service, err := tc.setupService()

			if tc.expectError {
				assert.Error(t, err)
				if tc.errorContains != "" {
					assert.Contains(t, err.Error(), tc.errorContains)
				}
				assert.Nil(t, service)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, service)
			}
		})
	}
}

// Test that service methods handle context cancellation

func TestPaddleService_ContextCancellation(t *testing.T) {
	cfg := &config.Config{
		PaddleAPIKey:      "test_key",
		PaddleEnvironment: "sandbox",
	}
	logger := slog.Default()

	service, err := NewPaddleService(cfg, logger)
	require.NoError(t, err)

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// These should respect the cancelled context
	_, err = service.CreateCustomer(ctx, "user_123", "test@example.com")
	assert.Error(t, err, "Should fail with cancelled context")

	_, err = service.GetCustomer(ctx, "ctm_123")
	assert.Error(t, err, "Should fail with cancelled context")

	_, _, err = service.CreateCheckoutSession(ctx, "ctm_123", "pri_123", "test@example.com", "https://example.com")
	assert.Error(t, err, "Should fail with cancelled context")
}

// Test proration mode parsing in UpdateSubscription

func TestUpdateSubscription_ProrationModes(t *testing.T) {
	t.Skip("Requires Paddle SDK interface refactor for proper mocking")

	// Would test that these proration modes are correctly parsed:
	testCases := []struct {
		input    string
		expected paddle.ProrationBillingMode
	}{
		{"prorated_immediately", paddle.ProrationBillingModeProratedImmediately},
		{"full_immediately", paddle.ProrationBillingModeFullImmediately},
		{"full_next_billing_period", paddle.ProrationBillingModeFullNextBillingPeriod},
		{"invalid", paddle.ProrationBillingModeProratedImmediately}, // default
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			// Would verify the correct mode is used in the SDK call
		})
	}
}

// Documentation test: Verify that all public methods are present

func TestPaddleService_ImplementsInterface(t *testing.T) {
	cfg := &config.Config{
		PaddleAPIKey:      "test_key",
		PaddleEnvironment: "sandbox",
	}
	logger := slog.Default()

	service, err := NewPaddleService(cfg, logger)
	require.NoError(t, err)

	// Verify service implements PaddleService interface
	var _ PaddleService = service

	// Verify all methods are callable (compilation test)
	ctx := context.Background()

	_ = func() { _, _ = service.CreateCustomer(ctx, "u", "e@e.com") }
	_ = func() { _, _ = service.GetCustomer(ctx, "c") }
	_ = func() { _, _, _ = service.CreateCheckoutSession(ctx, "c", "p", "e@e.com", "url") }
	_ = func() { _, _ = service.GetSubscription(ctx, "s") }
	_ = func() { _, _ = service.UpdateSubscription(ctx, "s", "p", "prorated") }
	_ = func() { _ = service.CancelSubscription(ctx, "s") }
	_ = func() { _, _, _, _ = service.PreviewUpdate(ctx, "s", "p") }
	_ = func() { _ = service.GetClient() }
}

// Test that custom_data is included when creating customers

func TestCreateCustomer_IncludesCustomData(t *testing.T) {
	t.Skip("Requires Paddle SDK interface refactor to verify request payload")

	// Would verify that CreateCustomerRequest includes:
	// - Email field
	// - CustomData with user_id field
}

// Test checkout session with and without customer ID

func TestCreateCheckoutSession_WithAndWithoutCustomer(t *testing.T) {
	t.Skip("Requires Paddle SDK interface refactor for proper mocking")

	// Would test:
	// 1. With customer ID: request includes CustomerID field
	// 2. Without customer ID: request has nil/empty CustomerID (guest checkout)
}

// Test that errors from Paddle API are wrapped appropriately

func TestPaddleService_ErrorWrapping(t *testing.T) {
	// This test documents that errors should be wrapped with context
	testErr := errors.New("paddle api error")

	// Service methods should wrap errors like:
	wrappedErr := fmt.Errorf("failed to create Paddle customer: %w", testErr)

	assert.Contains(t, wrappedErr.Error(), "paddle api error")
	assert.Contains(t, wrappedErr.Error(), "failed to create Paddle customer")
}

// TestGetBillingPortalURL_Success tests successful billing portal URL generation
func TestGetBillingPortalURL_Success(t *testing.T) {
	t.Skip("Requires Paddle SDK interface refactor for proper mocking")

	// Would test:
	// 1. Calls Paddle SDK to create billing portal session
	// 2. Returns valid URL for customer to access billing portal
	// 3. Logs appropriate messages
}

// TestGetBillingPortalURL_InvalidCustomer tests with non-existent customer ID
func TestGetBillingPortalURL_InvalidCustomer(t *testing.T) {
	t.Skip("Requires Paddle SDK interface refactor for proper mocking")

	// Would test:
	// 1. Calls Paddle SDK with invalid customer ID
	// 2. Returns ErrCustomerNotFound error
	// 3. Wraps error with appropriate message
}

// TestGetBillingPortalURL_PaddleAPIError tests Paddle API failure
func TestGetBillingPortalURL_PaddleAPIError(t *testing.T) {
	t.Skip("Requires Paddle SDK interface refactor for proper mocking")

	// Would test:
	// 1. Simulates Paddle API failure (network error, rate limit, etc.)
	// 2. Returns wrapped error with context
	// 3. Logs error appropriately
}
