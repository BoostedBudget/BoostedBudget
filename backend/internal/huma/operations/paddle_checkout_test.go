package operations

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"

	paddle "github.com/PaddleHQ/paddle-go-sdk/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// mockPaddleCheckoutService is a mock implementation of services.PaddleService
type mockPaddleCheckoutService struct {
	mock.Mock
}

func (m *mockPaddleCheckoutService) CreateCustomer(ctx context.Context, userID, email string) (string, error) {
	args := m.Called(ctx, userID, email)
	return args.String(0), args.Error(1)
}

func (m *mockPaddleCheckoutService) GetCustomer(ctx context.Context, customerID string) (*paddle.Customer, error) {
	args := m.Called(ctx, customerID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*paddle.Customer), args.Error(1)
}

func (m *mockPaddleCheckoutService) CreateCheckoutSession(ctx context.Context, customerID, priceID, email, successURL string) (checkoutURL, transactionID string, err error) {
	args := m.Called(ctx, customerID, priceID, email, successURL)
	return args.String(0), args.String(1), args.Error(2)
}

func (m *mockPaddleCheckoutService) GetSubscription(ctx context.Context, subscriptionID string) (*paddle.Subscription, error) {
	args := m.Called(ctx, subscriptionID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*paddle.Subscription), args.Error(1)
}

func (m *mockPaddleCheckoutService) UpdateSubscription(ctx context.Context, subscriptionID, newPriceID, prorationBillingMode string) (*paddle.Subscription, error) {
	args := m.Called(ctx, subscriptionID, newPriceID, prorationBillingMode)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*paddle.Subscription), args.Error(1)
}

func (m *mockPaddleCheckoutService) CancelSubscription(ctx context.Context, subscriptionID string) error {
	args := m.Called(ctx, subscriptionID)
	return args.Error(0)
}

func (m *mockPaddleCheckoutService) PreviewUpdate(ctx context.Context, subscriptionID, newPriceID string) (prorationAmount, currency, nextChargeDate string, err error) {
	args := m.Called(ctx, subscriptionID, newPriceID)
	return args.String(0), args.String(1), args.String(2), args.Error(3)
}

func (m *mockPaddleCheckoutService) GetClient() *paddle.SDK {
	return nil
}

// mockProfileRepo is a mock implementation of repository.ProfileRepository
type mockProfileRepo struct {
	mock.Mock
}

func (m *mockProfileRepo) GetUserByID(ctx context.Context, id string) (*domain.User, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

func (m *mockProfileRepo) UpdatePaddleCustomerID(ctx context.Context, userID, customerID string) error {
	args := m.Called(ctx, userID, customerID)
	return args.Error(0)
}

// Implement other required methods with no-ops
func (m *mockProfileRepo) GetUserByClerkID(ctx context.Context, clerkID string) (*domain.User, error) {
	return nil, nil
}
func (m *mockProfileRepo) GetUserByEmail(ctx context.Context, email string) (*domain.User, error) {
	return nil, nil
}
func (m *mockProfileRepo) CreateUser(ctx context.Context, user *domain.User) error {
	return nil
}
func (m *mockProfileRepo) UpdateLastLogin(ctx context.Context, userID string) error {
	return nil
}
func (m *mockProfileRepo) UpdateTrialStatus(ctx context.Context, userID string, status string, trialStartedAt, trialEndsAt *time.Time, hadTrial bool) error {
	return nil
}
func (m *mockProfileRepo) GetUsersWithExpiredTrials(ctx context.Context) ([]*domain.User, error) {
	return nil, nil
}
func (m *mockProfileRepo) GetUsersWithExpiringTrials(ctx context.Context, withinDays int) ([]*domain.User, error) {
	return nil, nil
}
func (m *mockProfileRepo) UpdateSubscriptionStatus(ctx context.Context, userID string, status string) error {
	return nil
}
func (m *mockProfileRepo) CancelDeletion(ctx context.Context, userID string) error {
	return nil
}
func (m *mockProfileRepo) UpdatePaddleCustomerID(ctx context.Context, userID, paddleCustomerID string) error {
	return nil
}
func (m *mockProfileRepo) UpdateSubscriptionFromPaddle(ctx context.Context, userID, tier, status string, startedAt, endsAt *time.Time) error {
	return nil
}

// setupTestCheckoutHandler creates a handler with mock dependencies
func setupTestCheckoutHandler(t *testing.T) (*PaddleCheckoutHandler, *mockPaddleCheckoutService, *mockProfileRepo) {
	t.Helper()

	mockService := new(mockPaddleCheckoutService)
	mockRepo := new(mockProfileRepo)

	// Load tier config
	tierConfig, err := config.NewTierConfig("")
	require.NoError(t, err)

	cfg := &config.Config{
		FrontendURL: "https://app.example.com",
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	handler := NewPaddleCheckoutHandler(PaddleCheckoutHandlerDeps{
		PaddleService: mockService,
		ProfileRepo:   mockRepo,
		Config:        cfg,
		TierConfig:    tierConfig,
		Logger:        logger,
	})

	return handler, mockService, mockRepo
}

// Helper to create context with user_id
func contextWithUserID(userID string) context.Context {
	return context.WithValue(context.Background(), "user_id", userID)
}

// TestHandleCreateCheckoutSession_Success tests successful checkout creation for premium tier
func TestHandleCreateCheckoutSession_Success(t *testing.T) {
	handler, mockService, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	customerID := "ctm_01h1234567890"
	user := &domain.User{
		ID:                "user_123",
		Email:             "test@example.com",
		PaddleCustomerID:  &customerID,
		SubscriptionTier:  "free",
		SubscriptionStatus: "active",
	}

	input := &CreateCheckoutInput{}
	input.Body.Tier = "premium"
	input.Body.BillingInterval = "month"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)
	mockService.On("CreateCheckoutSession",
		ctx,
		customerID,
		mock.AnythingOfType("string"), // price ID
		"test@example.com",
		"https://app.example.com/subscription/success",
	).Return("https://checkout.paddle.com/txn_123", "txn_123", nil)

	output, err := handler.HandleCreateCheckoutSession(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Contains(t, output.Body.CheckoutURL, "checkout.paddle.com")
	assert.Equal(t, "txn_123", output.Body.TransactionID)

	mockRepo.AssertExpectations(t)
	mockService.AssertExpectations(t)
}

// TestHandleCreateCheckoutSession_AllTiers tests checkout for all subscription tiers
func TestHandleCreateCheckoutSession_AllTiers(t *testing.T) {
	tiers := []string{"premium", "household_2", "household_3", "household_4", "household_5"}

	for _, tier := range tiers {
		t.Run(tier, func(t *testing.T) {
			handler, mockService, mockRepo := setupTestCheckoutHandler(t)
			ctx := contextWithUserID("user_123")

			customerID := "ctm_01h1234567890"
			user := &domain.User{
				ID:               "user_123",
				Email:            "test@example.com",
				PaddleCustomerID: &customerID,
			}

			input := &CreateCheckoutInput{}
			input.Body.Tier = tier
			input.Body.BillingInterval = "month"

			mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)
			mockService.On("CreateCheckoutSession", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
				Return("https://checkout.paddle.com/txn_123", "txn_123", nil)

			output, err := handler.HandleCreateCheckoutSession(ctx, input)

			require.NoError(t, err)
			assert.NotNil(t, output)
		})
	}
}

// TestHandleCreateCheckoutSession_BillingIntervals tests monthly and yearly billing
func TestHandleCreateCheckoutSession_BillingIntervals(t *testing.T) {
	testCases := []struct {
		name            string
		billingInterval string
		expectError     bool
	}{
		{"Monthly", "month", false},
		{"Yearly", "year", false},
		{"Invalid", "weekly", true},
		{"Empty", "", true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			handler, mockService, mockRepo := setupTestCheckoutHandler(t)
			ctx := contextWithUserID("user_123")

			customerID := "ctm_01h1234567890"
			user := &domain.User{
				ID:               "user_123",
				Email:            "test@example.com",
				PaddleCustomerID: &customerID,
			}

			input := &CreateCheckoutInput{}
			input.Body.Tier = "premium"
			input.Body.BillingInterval = tc.billingInterval

			if !tc.expectError {
				mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)
				mockService.On("CreateCheckoutSession", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return("https://checkout.paddle.com/txn_123", "txn_123", nil)
			}

			output, err := handler.HandleCreateCheckoutSession(ctx, input)

			if tc.expectError {
				assert.Error(t, err)
				assert.Nil(t, output)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, output)
			}
		})
	}
}

// TestHandleCreateCheckoutSession_InvalidTier tests validation of tier names
func TestHandleCreateCheckoutSession_InvalidTier(t *testing.T) {
	handler, _, _ := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	input := &CreateCheckoutInput{}
	input.Body.Tier = "invalid_tier"
	input.Body.BillingInterval = "month"

	output, err := handler.HandleCreateCheckoutSession(ctx, input)

	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "Invalid tier")
}

// TestHandleCreateCheckoutSession_Unauthenticated tests missing user_id in context
func TestHandleCreateCheckoutSession_Unauthenticated(t *testing.T) {
	handler, _, _ := setupTestCheckoutHandler(t)
	ctx := context.Background() // No user_id

	input := &CreateCheckoutInput{}
	input.Body.Tier = "premium"
	input.Body.BillingInterval = "month"

	output, err := handler.HandleCreateCheckoutSession(ctx, input)

	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "not authenticated")
}

// TestHandleCreateCheckoutSession_NoCustomer_CreatesNew tests customer creation
func TestHandleCreateCheckoutSession_NoCustomer_CreatesNew(t *testing.T) {
	handler, mockService, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	user := &domain.User{
		ID:               "user_123",
		Email:            "test@example.com",
		PaddleCustomerID: nil, // No customer ID yet
	}

	input := &CreateCheckoutInput{}
	input.Body.Tier = "premium"
	input.Body.BillingInterval = "month"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)
	mockService.On("CreateCustomer", ctx, "user_123", "test@example.com").
		Return("ctm_new_customer", nil)
	mockRepo.On("UpdatePaddleCustomerID", ctx, "user_123", "ctm_new_customer").
		Return(nil)
	mockService.On("CreateCheckoutSession", ctx, "ctm_new_customer", mock.Anything, mock.Anything, mock.Anything).
		Return("https://checkout.paddle.com/txn_123", "txn_123", nil)

	output, err := handler.HandleCreateCheckoutSession(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Contains(t, output.Body.CheckoutURL, "checkout.paddle.com")

	mockRepo.AssertExpectations(t)
	mockService.AssertExpectations(t)
}

// TestHandleCreateCheckoutSession_PaddleAPIError tests handling of Paddle API failures
func TestHandleCreateCheckoutSession_PaddleAPIError(t *testing.T) {
	handler, mockService, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	customerID := "ctm_01h1234567890"
	user := &domain.User{
		ID:               "user_123",
		Email:            "test@example.com",
		PaddleCustomerID: &customerID,
	}

	input := &CreateCheckoutInput{}
	input.Body.Tier = "premium"
	input.Body.BillingInterval = "month"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)
	mockService.On("CreateCheckoutSession", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return("", "", errors.New("Paddle API rate limit exceeded"))

	output, err := handler.HandleCreateCheckoutSession(ctx, input)

	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "Failed to create checkout session")

	mockRepo.AssertExpectations(t)
	mockService.AssertExpectations(t)
}

// TestHandleCreateCheckoutSession_UserNotFound tests missing user in database
func TestHandleCreateCheckoutSession_UserNotFound(t *testing.T) {
	handler, _, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_nonexistent")

	input := &CreateCheckoutInput{}
	input.Body.Tier = "premium"
	input.Body.BillingInterval = "month"

	mockRepo.On("GetUserByID", ctx, "user_nonexistent").
		Return(nil, errors.New("user not found"))

	output, err := handler.HandleCreateCheckoutSession(ctx, input)

	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "Failed to get user details")

	mockRepo.AssertExpectations(t)
}

// TestHandleCreateCheckoutSession_CreateCustomerFails tests failure in customer creation
func TestHandleCreateCheckoutSession_CreateCustomerFails(t *testing.T) {
	handler, mockService, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	user := &domain.User{
		ID:               "user_123",
		Email:            "test@example.com",
		PaddleCustomerID: nil,
	}

	input := &CreateCheckoutInput{}
	input.Body.Tier = "premium"
	input.Body.BillingInterval = "month"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)
	mockService.On("CreateCustomer", ctx, "user_123", "test@example.com").
		Return("", errors.New("Paddle API error"))

	output, err := handler.HandleCreateCheckoutSession(ctx, input)

	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "Failed to create customer")

	mockRepo.AssertExpectations(t)
	mockService.AssertExpectations(t)
}

// TestHandlePreviewSubscriptionUpdate_NotImplemented tests preview update endpoint
func TestHandlePreviewSubscriptionUpdate_NotImplemented(t *testing.T) {
	handler, _, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	user := &domain.User{
		ID:                 "user_123",
		Email:              "test@example.com",
		SubscriptionStatus: "active",
	}

	input := &PreviewUpdateInput{}
	input.Body.NewTier = "household_2"
	input.Body.BillingInterval = "month"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)

	output, err := handler.HandlePreviewSubscriptionUpdate(ctx, input)

	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "not yet implemented")

	mockRepo.AssertExpectations(t)
}

// TestHandlePreviewSubscriptionUpdate_Unauthenticated tests missing user_id
func TestHandlePreviewSubscriptionUpdate_Unauthenticated(t *testing.T) {
	handler, _, _ := setupTestCheckoutHandler(t)
	ctx := context.Background()

	input := &PreviewUpdateInput{}
	input.Body.NewTier = "household_2"
	input.Body.BillingInterval = "month"

	output, err := handler.HandlePreviewSubscriptionUpdate(ctx, input)

	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "not authenticated")
}

// TestHandlePreviewSubscriptionUpdate_InvalidTier tests tier validation
func TestHandlePreviewSubscriptionUpdate_InvalidTier(t *testing.T) {
	handler, _, _ := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	input := &PreviewUpdateInput{}
	input.Body.NewTier = "invalid_tier"
	input.Body.BillingInterval = "month"

	output, err := handler.HandlePreviewSubscriptionUpdate(ctx, input)

	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "Invalid tier")
}

// TestHandlePreviewSubscriptionUpdate_NoActiveSubscription tests preview without active subscription
func TestHandlePreviewSubscriptionUpdate_NoActiveSubscription(t *testing.T) {
	handler, _, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	user := &domain.User{
		ID:                 "user_123",
		Email:              "test@example.com",
		SubscriptionStatus: "cancelled", // Not active
	}

	input := &PreviewUpdateInput{}
	input.Body.NewTier = "household_2"
	input.Body.BillingInterval = "month"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)

	output, err := handler.HandlePreviewSubscriptionUpdate(ctx, input)

	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "No active subscription")

	mockRepo.AssertExpectations(t)
}

// TestNewPaddleCheckoutHandler tests handler initialization
func TestNewPaddleCheckoutHandler(t *testing.T) {
	mockService := new(mockPaddleCheckoutService)
	mockRepo := new(mockProfileRepo)
	tierConfig, err := config.NewTierConfig("")
	require.NoError(t, err)

	cfg := &config.Config{
		FrontendURL: "https://app.example.com",
	}

	handler := NewPaddleCheckoutHandler(PaddleCheckoutHandlerDeps{
		PaddleService: mockService,
		ProfileRepo:   mockRepo,
		Config:        cfg,
		TierConfig:    tierConfig,
		Logger:        nil, // Should default to slog.Default()
	})

	assert.NotNil(t, handler)
	assert.NotNil(t, handler.logger, "Logger should be initialized")
}

// TestValidSubscriptionTiers tests tier validation helper
func TestValidSubscriptionTiers(t *testing.T) {
	validTiers := validSubscriptionTiers()

	assert.True(t, validTiers["premium"])
	assert.True(t, validTiers["household_2"])
	assert.True(t, validTiers["household_3"])
	assert.True(t, validTiers["household_4"])
	assert.True(t, validTiers["household_5"])
	assert.False(t, validTiers["free"]) // free tier not in checkout
	assert.False(t, validTiers["invalid"])
}
