package operations

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	paddle "github.com/PaddleHQ/paddle-go-sdk/v4"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
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

func (m *mockPaddleCheckoutService) EnsureCustomer(ctx context.Context, userID, email string) (string, error) {
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

func (m *mockPaddleCheckoutService) CreateCheckoutSession(ctx context.Context, customerID, priceID, email, successURL, userID string) (string, string, error) {
	args := m.Called(ctx, customerID, priceID, email, successURL, userID)
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

func (m *mockPaddleCheckoutService) PreviewUpdate(ctx context.Context, subscriptionID, newPriceID string) (string, string, string, error) {
	args := m.Called(ctx, subscriptionID, newPriceID)
	return args.String(0), args.String(1), args.String(2), args.Error(3)
}

func (m *mockPaddleCheckoutService) GetBillingPortalURL(ctx context.Context, customerID string) (string, error) {
	args := m.Called(ctx, customerID)
	return args.String(0), args.Error(1)
}

func (m *mockPaddleCheckoutService) UpdateCustomerEmail(ctx context.Context, customerID, email string) error {
	args := m.Called(ctx, customerID, email)
	return args.Error(0)
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

func (m *mockProfileRepo) GetUserByPaddleSubscriptionID(ctx context.Context, subscriptionID string) (*domain.User, error) {
	args := m.Called(ctx, subscriptionID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

// Implement other required methods with no-ops (only testing GetUserByID and UpdatePaddleCustomerID)
func (m *mockProfileRepo) GetUserByClerkID(ctx context.Context, clerkID string) (*domain.User, error) {
	return nil, nil
}
func (m *mockProfileRepo) GetUserByEmail(ctx context.Context, email string) (*domain.User, error) {
	return nil, nil
}
func (m *mockProfileRepo) CreateUser(ctx context.Context, user *domain.User) error  { return nil }
func (m *mockProfileRepo) UpdateLastLogin(ctx context.Context, userID string) error { return nil }
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
func (m *mockProfileRepo) CancelDeletion(ctx context.Context, userID string) error { return nil }
func (m *mockProfileRepo) UpdateSubscriptionFromPaddle(ctx context.Context, userID, tier, status string, startedAt, endsAt *time.Time, subscriptionID *string) error {
	return nil
}
func (m *mockProfileRepo) CreateUserTx(ctx context.Context, tx pgx.Tx, user *domain.User) error {
	return nil
}
func (m *mockProfileRepo) DeleteUserCascade(ctx context.Context, userID string) error { return nil }
func (m *mockProfileRepo) GetUsersScheduledForDeletion(ctx context.Context) ([]*domain.User, error) {
	return nil, nil
}
func (m *mockProfileRepo) MarkForDeletion(ctx context.Context, userID string, scheduledAt time.Time) error {
	return nil
}
func (m *mockProfileRepo) UpdateUser(ctx context.Context, user *domain.User) error { return nil }
func (m *mockProfileRepo) GetUsersWithActivePaddleSubscriptions(ctx context.Context) ([]*domain.User, error) {
	return nil, nil
}
func (m *mockProfileRepo) GetSubscriptionStatusByUserID(ctx context.Context, userID string) (string, error) {
	return "", nil
}
func (m *mockProfileRepo) SetRetentionDates(ctx context.Context, userID string, startedAt, expiresAt time.Time) error {
	return nil
}
func (m *mockProfileRepo) ClearRetentionDates(ctx context.Context, userID string) error { return nil }
func (m *mockProfileRepo) SetRetentionNotice30dSent(ctx context.Context, userID string) error {
	return nil
}
func (m *mockProfileRepo) SetRetentionNotice7dSent(ctx context.Context, userID string) error {
	return nil
}
func (m *mockProfileRepo) GetUsersNeedingRetention30dNotice(ctx context.Context) ([]*domain.User, error) {
	return nil, nil
}
func (m *mockProfileRepo) GetUsersNeedingRetention7dNotice(ctx context.Context) ([]*domain.User, error) {
	return nil, nil
}
func (m *mockProfileRepo) GetUsersWithExpiredRetention(ctx context.Context) ([]*domain.User, error) {
	return nil, nil
}

func (m *mockProfileRepo) UpdateUserEmailTx(ctx context.Context, tx pgx.Tx, userID, plainEmail string) error {
	args := m.Called(ctx, tx, userID, plainEmail)
	return args.Error(0)
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

// Helper to create context with user_id using the correct typed context key
func contextWithUserID(userID string) context.Context {
	return context.WithValue(context.Background(), inputs.UserIDKey, userID)
}

// TestHandleCreateCheckoutSession_Success tests successful checkout creation for premium tier
func TestHandleCreateCheckoutSession_Success(t *testing.T) {
	handler, mockService, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	customerID := "ctm_01h1234567890"
	user := &domain.User{
		ID:                 "user_123",
		Email:              "test@example.com",
		PaddleCustomerID:   &customerID,
		SubscriptionTier:   "premium",
		SubscriptionStatus: "trial", // Trial user purchasing for first time
	}

	input := &CreateCheckoutInput{}
	input.UserID = "user_123"
	input.Body.Tier = "premium"
	input.Body.BillingInterval = "month"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)
	mockService.On("CreateCheckoutSession",
		ctx,
		customerID,
		mock.AnythingOfType("string"), // price ID
		"test@example.com",
		"https://app.example.com/subscription/success",
		"user_123", // userID for custom_data
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
			input.UserID = "user_123"
			input.Body.Tier = tier
			input.Body.BillingInterval = "month"

			mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)
			mockService.On("CreateCheckoutSession", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
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
			input.UserID = "user_123"
			input.Body.Tier = "premium"
			input.Body.BillingInterval = tc.billingInterval

			// Always mock GetUserByID since handler calls getUserFromContext first
			mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)

			if !tc.expectError {
				mockService.On("CreateCheckoutSession", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
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
	handler, _, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	customerID := "ctm_01h1234567890"
	user := &domain.User{
		ID:               "user_123",
		Email:            "test@example.com",
		PaddleCustomerID: &customerID,
	}

	input := &CreateCheckoutInput{}
	input.UserID = "user_123"
	input.Body.Tier = "invalid_tier"
	input.Body.BillingInterval = "month"

	// Mock GetUserByID since handler calls getUserFromContext first
	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)

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
	input.UserID = "user_123"
	input.Body.Tier = "premium"
	input.Body.BillingInterval = "month"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)
	mockService.On("EnsureCustomer", ctx, "user_123", "test@example.com").
		Return("ctm_new_customer", nil)
	mockRepo.On("UpdatePaddleCustomerID", ctx, "user_123", "ctm_new_customer").
		Return(nil)
	mockService.On("CreateCheckoutSession", ctx, "ctm_new_customer", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return("https://checkout.paddle.com/txn_123", "txn_123", nil)

	output, err := handler.HandleCreateCheckoutSession(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Contains(t, output.Body.CheckoutURL, "checkout.paddle.com")

	mockRepo.AssertExpectations(t)
	mockService.AssertExpectations(t)
}

func TestHandleCreateCheckoutSession_NoCustomer_ReusesExistingPaddleCustomer(t *testing.T) {
	handler, mockService, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	user := &domain.User{
		ID:               "user_123",
		Email:            "test@example.com",
		PaddleCustomerID: nil,
	}

	input := &CreateCheckoutInput{}
	input.UserID = "user_123"
	input.Body.Tier = "premium"
	input.Body.BillingInterval = "month"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)
	mockService.On("EnsureCustomer", ctx, "user_123", "test@example.com").
		Return("ctm_existing_customer", nil)
	mockRepo.On("UpdatePaddleCustomerID", ctx, "user_123", "ctm_existing_customer").
		Return(nil)
	mockService.On("CreateCheckoutSession", ctx, "ctm_existing_customer", mock.Anything, "test@example.com", "https://app.example.com/subscription/success", "user_123").
		Return("https://checkout.paddle.com/txn_existing", "txn_existing", nil)

	output, err := handler.HandleCreateCheckoutSession(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "txn_existing", output.Body.TransactionID)

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
	input.UserID = "user_123"
	input.Body.Tier = "premium"
	input.Body.BillingInterval = "month"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)
	mockService.On("CreateCheckoutSession", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return("", "", errors.New("Paddle API rate limit exceeded"))

	output, err := handler.HandleCreateCheckoutSession(ctx, input)

	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "Internal server error")

	mockRepo.AssertExpectations(t)
	mockService.AssertExpectations(t)
}

// TestHandleCreateCheckoutSession_UserNotFound tests missing user in database
func TestHandleCreateCheckoutSession_UserNotFound(t *testing.T) {
	handler, _, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_nonexistent")

	input := &CreateCheckoutInput{}
	input.UserID = "user_nonexistent"
	input.Body.Tier = "premium"
	input.Body.BillingInterval = "month"

	mockRepo.On("GetUserByID", ctx, "user_nonexistent").
		Return(nil, errors.New("user not found"))

	output, err := handler.HandleCreateCheckoutSession(ctx, input)

	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "Internal server error")

	mockRepo.AssertExpectations(t)
}

// TestHandleCreateCheckoutSession_EnsureCustomerFails tests failure resolving a Paddle customer
func TestHandleCreateCheckoutSession_EnsureCustomerFails(t *testing.T) {
	handler, mockService, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	user := &domain.User{
		ID:               "user_123",
		Email:            "test@example.com",
		PaddleCustomerID: nil,
	}

	input := &CreateCheckoutInput{}
	input.UserID = "user_123"
	input.Body.Tier = "premium"
	input.Body.BillingInterval = "month"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)
	mockService.On("EnsureCustomer", ctx, "user_123", "test@example.com").
		Return("", errors.New("Paddle API error"))

	output, err := handler.HandleCreateCheckoutSession(ctx, input)

	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "Internal server error")

	mockRepo.AssertExpectations(t)
	mockService.AssertExpectations(t)
}

// TestHandlePreviewSubscriptionUpdate_Success tests successful preview update
func TestHandlePreviewSubscriptionUpdate_Success(t *testing.T) {
	handler, mockService, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	subscriptionID := "sub_01h1234567890"
	user := &domain.User{
		ID:                   "user_123",
		Email:                "test@example.com",
		SubscriptionStatus:   "active",
		SubscriptionTier:     "premium",
		PaddleSubscriptionID: &subscriptionID,
	}

	input := &PreviewUpdateInput{}
	input.UserID = "user_123"
	input.Body.NewTier = "household_2"
	input.Body.BillingInterval = "month"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil).Once()
	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil).Once()

	// Mock PreviewUpdate service call
	mockService.On("PreviewUpdate", ctx, subscriptionID, mock.AnythingOfType("string")).
		Return("5.00", "USD", "2026-03-01", nil)

	output, err := handler.HandlePreviewSubscriptionUpdate(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "5.00", output.Body.ProrationAmount)
	assert.Equal(t, "USD", output.Body.Currency)
	assert.Equal(t, "2026-03-01", output.Body.NextChargeDate)

	mockRepo.AssertExpectations(t)
	mockService.AssertExpectations(t)
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
	handler, _, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	user := &domain.User{
		ID:    "user_123",
		Email: "test@example.com",
	}

	input := &PreviewUpdateInput{}
	input.UserID = "user_123"
	input.Body.NewTier = "invalid_tier"
	input.Body.BillingInterval = "month"

	// Mock GetUserByID since handler calls getUserFromContext first
	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)

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
	input.UserID = "user_123"
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
	assert.True(t, validSubscriptionTiers["premium"])
	assert.True(t, validSubscriptionTiers["household_2"])
	assert.True(t, validSubscriptionTiers["household_3"])
	assert.True(t, validSubscriptionTiers["household_4"])
	assert.True(t, validSubscriptionTiers["household_5"])
	assert.False(t, validSubscriptionTiers["free"]) // free tier not in checkout
	assert.False(t, validSubscriptionTiers["invalid"])
}

// --- getActiveSubscriptionID Tests ---

// TestGetActiveSubscriptionID_Success tests successful retrieval of subscription ID
func TestGetActiveSubscriptionID_Success(t *testing.T) {
	handler, _, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	subscriptionID := "sub_01h1234567890"
	user := &domain.User{
		ID:                   "user_123",
		PaddleSubscriptionID: &subscriptionID,
	}

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)

	result, err := handler.getActiveSubscriptionID(ctx, "user_123")

	require.NoError(t, err)
	assert.Equal(t, subscriptionID, result)

	mockRepo.AssertExpectations(t)
}

// TestGetActiveSubscriptionID_NoSubscriptionID tests error when user has no subscription ID
func TestGetActiveSubscriptionID_NoSubscriptionID(t *testing.T) {
	handler, _, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	user := &domain.User{
		ID:                   "user_123",
		PaddleSubscriptionID: nil, // No subscription ID
	}

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)

	result, err := handler.getActiveSubscriptionID(ctx, "user_123")

	require.Error(t, err)
	assert.Empty(t, result)
	assert.Contains(t, err.Error(), "no active subscription found")

	mockRepo.AssertExpectations(t)
}

// TestGetActiveSubscriptionID_EmptySubscriptionID tests error when subscription ID is empty string
func TestGetActiveSubscriptionID_EmptySubscriptionID(t *testing.T) {
	handler, _, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	emptyID := ""
	user := &domain.User{
		ID:                   "user_123",
		PaddleSubscriptionID: &emptyID,
	}

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)

	result, err := handler.getActiveSubscriptionID(ctx, "user_123")

	require.Error(t, err)
	assert.Empty(t, result)
	assert.Contains(t, err.Error(), "no active subscription found")

	mockRepo.AssertExpectations(t)
}

// TestGetActiveSubscriptionID_UserNotFound tests error when user fetch fails
func TestGetActiveSubscriptionID_UserNotFound(t *testing.T) {
	handler, _, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	mockRepo.On("GetUserByID", ctx, "user_123").Return(nil, errors.New("user not found"))

	result, err := handler.getActiveSubscriptionID(ctx, "user_123")

	require.Error(t, err)
	assert.Empty(t, result)
	assert.Contains(t, err.Error(), "failed to fetch user")

	mockRepo.AssertExpectations(t)
}

// --- HandleUpdateSubscription Tests ---

// TestHandleUpdateSubscription_Upgrade tests successful upgrade to higher tier
func TestHandleUpdateSubscription_Upgrade(t *testing.T) {
	handler, mockService, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	customerID := "ctm_01h1234567890"
	subscriptionID := "sub_01h1234567890"
	user := &domain.User{
		ID:                   "user_123",
		Email:                "test@example.com",
		SubscriptionTier:     "premium",
		SubscriptionStatus:   "active",
		PaddleCustomerID:     &customerID,
		PaddleSubscriptionID: &subscriptionID, // Added subscription ID
	}

	// Input to upgrade from premium to household_2
	input := &UpdateSubscriptionInput{}
	input.UserID = "user_123"
	input.Body.NewTier = "household_2"
	input.Body.BillingInterval = "month"

	// Mock: getUserFromContext calls GetUserByID first
	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil).Once()
	// Mock: getActiveSubscriptionID calls GetUserByID again
	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil).Once()

	mockService.On("UpdateSubscription",
		ctx,
		subscriptionID,
		mock.AnythingOfType("string"), // new price ID
		"prorated_immediately",
	).Return(&paddle.Subscription{
		ID:     subscriptionID,
		Status: paddle.SubscriptionStatusActive,
	}, nil)

	output, err := handler.HandleUpdateSubscription(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, subscriptionID, output.Body.SubscriptionID)
	assert.Equal(t, "active", output.Body.Status)

	mockRepo.AssertExpectations(t)
	mockService.AssertExpectations(t)
}

// TestHandleUpdateSubscription_Downgrade tests successful downgrade to lower tier
func TestHandleUpdateSubscription_Downgrade(t *testing.T) {
	handler, mockService, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	customerID := "ctm_01h1234567890"
	subscriptionID := "sub_01h1234567890"
	user := &domain.User{
		ID:                   "user_123",
		Email:                "test@example.com",
		SubscriptionTier:     "household_5",
		SubscriptionStatus:   "active",
		PaddleCustomerID:     &customerID,
		PaddleSubscriptionID: &subscriptionID, // Added subscription ID
	}

	// Input to downgrade from household_5 to premium
	input := &UpdateSubscriptionInput{}
	input.UserID = "user_123"
	input.Body.NewTier = "premium"
	input.Body.BillingInterval = "month"
	input.Body.ProrationBillingMode = "full_next_billing_period"

	// Mock: getUserFromContext calls GetUserByID
	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil).Once()
	// Mock: getActiveSubscriptionID calls GetUserByID again
	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil).Once()

	mockService.On("UpdateSubscription",
		ctx,
		subscriptionID,
		mock.AnythingOfType("string"), // new price ID
		"full_next_billing_period",
	).Return(&paddle.Subscription{
		ID:     subscriptionID,
		Status: paddle.SubscriptionStatusActive,
	}, nil)

	output, err := handler.HandleUpdateSubscription(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, subscriptionID, output.Body.SubscriptionID)

	mockRepo.AssertExpectations(t)
	mockService.AssertExpectations(t)
}

// TestHandleUpdateSubscription_NoActiveSubscription tests update without active subscription
func TestHandleUpdateSubscription_NoActiveSubscription(t *testing.T) {
	handler, _, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	user := &domain.User{
		ID:                   "user_123",
		Email:                "test@example.com",
		SubscriptionStatus:   "cancelled",
		PaddleCustomerID:     nil,
		PaddleSubscriptionID: nil, // No subscription ID
	}

	input := &UpdateSubscriptionInput{}
	input.UserID = "user_123"
	input.Body.NewTier = "household_2"
	input.Body.BillingInterval = "month"

	// Mock: getUserFromContext calls GetUserByID
	// Handler returns early when SubscriptionStatus != "active", so no second call
	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil).Once()

	output, err := handler.HandleUpdateSubscription(ctx, input)

	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "No active subscription")

	mockRepo.AssertExpectations(t)
}

// --- HandleCancelSubscription Tests ---

// TestHandleCancelSubscription_Success tests successful subscription cancellation
func TestHandleCancelSubscription_Success(t *testing.T) {
	handler, mockService, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	customerID := "ctm_01h1234567890"
	subscriptionID := "sub_01h1234567890"
	user := &domain.User{
		ID:                   "user_123",
		Email:                "test@example.com",
		SubscriptionTier:     "premium",
		SubscriptionStatus:   "active",
		PaddleCustomerID:     &customerID,
		PaddleSubscriptionID: &subscriptionID, // Added subscription ID
	}

	input := &CancelSubscriptionInput{}
	input.UserID = "user_123"

	// Mock: getUserFromContext calls GetUserByID
	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil).Once()
	// Mock: getActiveSubscriptionID calls GetUserByID again
	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil).Once()

	mockService.On("CancelSubscription", ctx, subscriptionID).Return(nil)

	// Mock GetSubscription to provide EndsAt data
	mockService.On("GetSubscription", ctx, subscriptionID).
		Return(&paddle.Subscription{
			ID:                   subscriptionID,
			Status:               paddle.SubscriptionStatusActive,
			CurrentBillingPeriod: &paddle.TimePeriod{EndsAt: "2026-03-15"},
		}, nil)

	output, err := handler.HandleCancelSubscription(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Contains(t, output.Body.Message, "cancelled")
	assert.Equal(t, "2026-03-15", output.Body.EndsAt)

	mockRepo.AssertExpectations(t)
	mockService.AssertExpectations(t)
}

// TestHandleCancelSubscription_NoActiveSubscription tests cancellation without active subscription
func TestHandleCancelSubscription_NoActiveSubscription(t *testing.T) {
	handler, _, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	user := &domain.User{
		ID:                   "user_123",
		Email:                "test@example.com",
		SubscriptionStatus:   "cancelled",
		PaddleCustomerID:     nil,
		PaddleSubscriptionID: nil, // No subscription ID
	}

	input := &CancelSubscriptionInput{}
	input.UserID = "user_123"

	// Mock: getUserFromContext calls GetUserByID
	// Handler returns early when SubscriptionStatus != "active", so no second call
	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil).Once()

	output, err := handler.HandleCancelSubscription(ctx, input)

	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "No active subscription")

	mockRepo.AssertExpectations(t)
}

// --- HandleGetBillingPortalURL Tests ---

// TestHandleGetBillingPortalURL_Success tests successful billing portal URL generation
func TestHandleGetBillingPortalURL_Success(t *testing.T) {
	handler, mockService, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	customerID := "ctm_01h1234567890"
	user := &domain.User{
		ID:               "user_123",
		Email:            "test@example.com",
		PaddleCustomerID: &customerID,
	}

	input := &GetBillingPortalURLInput{}
	input.UserID = "user_123"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)
	mockService.On("GetBillingPortalURL", ctx, customerID).
		Return("https://sandbox-buy.paddle.com/customer/portal", nil)

	output, err := handler.HandleGetBillingPortalURL(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Contains(t, output.Body.PortalURL, "paddle.com")

	mockRepo.AssertExpectations(t)
	mockService.AssertExpectations(t)
}

// TestHandleGetBillingPortalURL_TrialingUser tests that trialing users can access billing portal
func TestHandleGetBillingPortalURL_TrialingUser(t *testing.T) {
	handler, mockService, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	customerID := "ctm_01h1234567890"
	subscriptionID := "sub_01h1234567890"
	user := &domain.User{
		ID:                   "user_123",
		Email:                "test@example.com",
		SubscriptionStatus:   "trialing", // Trialing user
		PaddleCustomerID:     &customerID,
		PaddleSubscriptionID: &subscriptionID,
	}

	input := &GetBillingPortalURLInput{}
	input.UserID = "user_123"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)
	mockService.On("GetBillingPortalURL", ctx, customerID).
		Return("https://sandbox-buy.paddle.com/customer/portal", nil)

	output, err := handler.HandleGetBillingPortalURL(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Contains(t, output.Body.PortalURL, "paddle.com")

	mockRepo.AssertExpectations(t)
	mockService.AssertExpectations(t)
}

// --- HandleGetCurrentSubscription Tests ---

// TestHandleGetCurrentSubscription_Success tests successful subscription retrieval
func TestHandleGetCurrentSubscription_Success(t *testing.T) {
	handler, mockService, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	customerID := "ctm_01h1234567890"
	subscriptionID := "sub_01h1234567890"
	user := &domain.User{
		ID:                   "user_123",
		Email:                "test@example.com",
		SubscriptionTier:     "premium",
		SubscriptionStatus:   "active",
		PaddleCustomerID:     &customerID,
		PaddleSubscriptionID: &subscriptionID, // Added subscription ID
	}

	input := &GetCurrentSubscriptionInput{}
	input.UserID = "user_123"

	// Mock: getUserFromContext calls GetUserByID
	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil).Once()
	// Mock: getActiveSubscriptionID calls GetUserByID again
	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil).Once()

	mockService.On("GetSubscription", ctx, subscriptionID).
		Return(&paddle.Subscription{
			ID:                   subscriptionID,
			Status:               paddle.SubscriptionStatusActive,
			CurrentBillingPeriod: &paddle.TimePeriod{EndsAt: "2026-03-01"},
			CurrencyCode:         "USD",
		}, nil)

	output, err := handler.HandleGetCurrentSubscription(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, subscriptionID, output.Body.SubscriptionID)
	assert.Equal(t, "active", output.Body.Status)

	mockRepo.AssertExpectations(t)
	mockService.AssertExpectations(t)
}

// TestHandleGetCurrentSubscription_TrialUser tests subscription retrieval for trial users
func TestHandleGetCurrentSubscription_TrialUser(t *testing.T) {
	handler, _, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	// Trial user with trial_ends_at set
	trialEndsAt := time.Now().Add(45 * 24 * time.Hour) // 45 days from now
	user := &domain.User{
		ID:                 "user_123",
		Email:              "test@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "trial",
		TrialEndsAt:        &trialEndsAt,
	}

	input := &GetCurrentSubscriptionInput{}
	input.UserID = "user_123"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)

	output, err := handler.HandleGetCurrentSubscription(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "trial", output.Body.Status)
	assert.Equal(t, "premium", output.Body.Tier)
	assert.Equal(t, "0.00", output.Body.Amount)
	assert.Equal(t, "USD", output.Body.Currency)
	assert.NotEmpty(t, output.Body.TrialEndsAt)
	assert.NotNil(t, output.Body.TrialDaysRemaining)
	assert.GreaterOrEqual(t, *output.Body.TrialDaysRemaining, 44) // Allow for time rounding
	assert.LessOrEqual(t, *output.Body.TrialDaysRemaining, 45)

	mockRepo.AssertExpectations(t)
}

// TestHandleGetCurrentSubscription_TrialingUser tests subscription retrieval for trialing users (paid subscription during trial)
func TestHandleGetCurrentSubscription_TrialingUser(t *testing.T) {
	handler, mockService, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	// Trialing user: has purchased subscription but trial period hasn't ended yet
	customerID := "ctm_01h1234567890"
	subscriptionID := "sub_01h1234567890"
	trialEndsAt := time.Now().Add(30 * 24 * time.Hour) // 30 days from now
	user := &domain.User{
		ID:                   "user_123",
		Email:                "test@example.com",
		SubscriptionTier:     "premium",
		SubscriptionStatus:   "trialing", // Paddle's trialing status
		PaddleCustomerID:     &customerID,
		PaddleSubscriptionID: &subscriptionID,
		TrialEndsAt:          &trialEndsAt, // Trial period still active
	}

	input := &GetCurrentSubscriptionInput{}
	input.UserID = "user_123"

	// Mock: getUserFromContext calls GetUserByID
	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil).Once()
	// Mock: getActiveSubscriptionID calls GetUserByID again
	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil).Once()

	mockService.On("GetSubscription", ctx, subscriptionID).
		Return(&paddle.Subscription{
			ID:                   subscriptionID,
			Status:               paddle.SubscriptionStatusTrialing,
			CurrentBillingPeriod: &paddle.TimePeriod{EndsAt: "2026-03-15"},
			CurrencyCode:         "USD",
		}, nil)

	output, err := handler.HandleGetCurrentSubscription(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, subscriptionID, output.Body.SubscriptionID)
	assert.Equal(t, "trialing", output.Body.Status)
	assert.Equal(t, "premium", output.Body.Tier)
	assert.Equal(t, "USD", output.Body.Currency)

	mockRepo.AssertExpectations(t)
	mockService.AssertExpectations(t)
}

// TestHandleCreateCheckoutSession_CustomerCreationErrorDetail tests detailed error message when customer resolution fails
func TestHandleCreateCheckoutSession_CustomerCreationErrorDetail(t *testing.T) {
	handler, mockService, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	user := &domain.User{
		ID:               "user_123",
		Email:            "test@example.com",
		PaddleCustomerID: nil,
	}

	input := &CreateCheckoutInput{}
	input.UserID = "user_123"
	input.Body.Tier = "premium"
	input.Body.BillingInterval = "month"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)
	mockService.On("EnsureCustomer", ctx, "user_123", "test@example.com").
		Return("", errors.New("request_error.customer_email_invalid: Email address is invalid"))

	output, err := handler.HandleCreateCheckoutSession(ctx, input)

	require.Error(t, err)
	assert.Nil(t, output)
	// Should return structured error with detail
	assert.Contains(t, err.Error(), "Internal server error")

	mockRepo.AssertExpectations(t)
	mockService.AssertExpectations(t)
}

// TestHandleCreateCheckoutSession_CheckoutSessionErrorDetail tests detailed error message when checkout session creation fails
func TestHandleCreateCheckoutSession_CheckoutSessionErrorDetail(t *testing.T) {
	handler, mockService, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	customerID := "ctm_01h1234567890"
	user := &domain.User{
		ID:               "user_123",
		Email:            "test@example.com",
		PaddleCustomerID: &customerID,
	}

	input := &CreateCheckoutInput{}
	input.UserID = "user_123"
	input.Body.Tier = "premium"
	input.Body.BillingInterval = "month"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)
	mockService.On("CreateCheckoutSession", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return("", "", errors.New("request_error.transaction_default_checkout_url_not_set: A Default Payment Link has not yet been defined within the Paddle Dashboard"))

	output, err := handler.HandleCreateCheckoutSession(ctx, input)

	require.Error(t, err)
	assert.Nil(t, output)
	// Should return structured error with detail
	assert.Contains(t, err.Error(), "Internal server error")

	mockRepo.AssertExpectations(t)
	mockService.AssertExpectations(t)
}

// TestHandleCreateCheckoutSession_ActiveUser_RejectsDuplicate tests that active users cannot purchase again
func TestHandleCreateCheckoutSession_ActiveUser_RejectsDuplicate(t *testing.T) {
	handler, _, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	customerID := "ctm_01h1234567890"
	subscriptionID := "sub_01h1234567890"
	user := &domain.User{
		ID:                   "user_123",
		Email:                "test@example.com",
		SubscriptionTier:     "premium",
		SubscriptionStatus:   "active", // Already active
		PaddleCustomerID:     &customerID,
		PaddleSubscriptionID: &subscriptionID,
	}

	input := &CreateCheckoutInput{}
	input.UserID = "user_123"
	input.Body.Tier = "premium"
	input.Body.BillingInterval = "month"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)

	output, err := handler.HandleCreateCheckoutSession(ctx, input)

	require.Error(t, err)
	require.Nil(t, output)
	assert.Contains(t, err.Error(), "already have an active subscription")

	mockRepo.AssertExpectations(t)
}

// TestHandleCreateCheckoutSession_TrialingUser_RejectsDuplicate tests that trialing users cannot purchase again
func TestHandleCreateCheckoutSession_TrialingUser_RejectsDuplicate(t *testing.T) {
	handler, _, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	customerID := "ctm_01h1234567890"
	subscriptionID := "sub_01h1234567890"
	user := &domain.User{
		ID:                   "user_123",
		Email:                "test@example.com",
		SubscriptionTier:     "premium",
		SubscriptionStatus:   "trialing", // Trialing (paid subscription during trial period)
		PaddleCustomerID:     &customerID,
		PaddleSubscriptionID: &subscriptionID,
	}

	input := &CreateCheckoutInput{}
	input.UserID = "user_123"
	input.Body.Tier = "household_2"
	input.Body.BillingInterval = "year"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)

	output, err := handler.HandleCreateCheckoutSession(ctx, input)

	require.Error(t, err)
	require.Nil(t, output)
	assert.Contains(t, err.Error(), "already have a pending subscription")

	mockRepo.AssertExpectations(t)
}

// TestHandleCreateCheckoutSession_TrialUser_Allowed tests that trial users can purchase
func TestHandleCreateCheckoutSession_TrialUser_Allowed(t *testing.T) {
	handler, mockService, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	customerID := "ctm_01h1234567890"
	user := &domain.User{
		ID:                 "user_123",
		Email:              "test@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "trial", // Free trial (not trialing)
		PaddleCustomerID:   &customerID,
	}

	input := &CreateCheckoutInput{}
	input.UserID = "user_123"
	input.Body.Tier = "premium"
	input.Body.BillingInterval = "month"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)
	mockService.On("CreateCheckoutSession", ctx, customerID, mock.Anything, user.Email, mock.Anything, user.ID).
		Return("https://checkout.paddle.com/...", "txn_123", nil)

	output, err := handler.HandleCreateCheckoutSession(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Contains(t, output.Body.CheckoutURL, "checkout.paddle.com")

	mockRepo.AssertExpectations(t)
	mockService.AssertExpectations(t)
}

// TestHandleCreateCheckoutSession_CancelledUser_Allowed tests that cancelled users can re-subscribe
func TestHandleCreateCheckoutSession_CancelledUser_Allowed(t *testing.T) {
	handler, mockService, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	customerID := "ctm_01h1234567890"
	user := &domain.User{
		ID:                 "user_123",
		Email:              "test@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "cancelled", // Previously cancelled
		PaddleCustomerID:   &customerID,
	}

	input := &CreateCheckoutInput{}
	input.UserID = "user_123"
	input.Body.Tier = "premium"
	input.Body.BillingInterval = "year"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)
	mockService.On("CreateCheckoutSession", ctx, customerID, mock.Anything, user.Email, mock.Anything, user.ID).
		Return("https://checkout.paddle.com/...", "txn_456", nil)

	output, err := handler.HandleCreateCheckoutSession(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Contains(t, output.Body.CheckoutURL, "checkout.paddle.com")

	mockRepo.AssertExpectations(t)
	mockService.AssertExpectations(t)
}

// TestHandlePreviewSubscriptionUpdate_TrialingUser tests preview update for trialing users
func TestHandlePreviewSubscriptionUpdate_TrialingUser(t *testing.T) {
	handler, mockService, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	subscriptionID := "sub_01h1234567890"
	user := &domain.User{
		ID:                   "user_123",
		Email:                "test@example.com",
		SubscriptionStatus:   "trialing", // Trialing user (paid subscription during trial)
		SubscriptionTier:     "premium",
		PaddleSubscriptionID: &subscriptionID,
	}

	input := &PreviewUpdateInput{}
	input.UserID = "user_123"
	input.Body.NewTier = "household_2"
	input.Body.BillingInterval = "month"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil).Once()
	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil).Once()

	mockService.On("PreviewUpdate", ctx, subscriptionID, mock.AnythingOfType("string")).
		Return("5.00", "USD", "2026-03-15", nil)

	output, err := handler.HandlePreviewSubscriptionUpdate(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "5.00", output.Body.ProrationAmount)
	assert.Equal(t, "USD", output.Body.Currency)
	assert.Equal(t, "2026-03-15", output.Body.NextChargeDate)

	mockRepo.AssertExpectations(t)
	mockService.AssertExpectations(t)
}

// TestHandleUpdateSubscription_TrialingUser tests subscription update for trialing users
func TestHandleUpdateSubscription_TrialingUser(t *testing.T) {
	handler, mockService, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	customerID := "ctm_01h1234567890"
	subscriptionID := "sub_01h1234567890"
	user := &domain.User{
		ID:                   "user_123",
		Email:                "test@example.com",
		SubscriptionTier:     "premium",
		SubscriptionStatus:   "trialing", // Trialing user
		PaddleCustomerID:     &customerID,
		PaddleSubscriptionID: &subscriptionID,
	}

	input := &UpdateSubscriptionInput{}
	input.UserID = "user_123"
	input.Body.NewTier = "household_2"
	input.Body.BillingInterval = "month"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil).Once()
	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil).Once()

	mockService.On("UpdateSubscription",
		ctx,
		subscriptionID,
		mock.AnythingOfType("string"),
		"prorated_immediately",
	).Return(&paddle.Subscription{
		ID:     subscriptionID,
		Status: paddle.SubscriptionStatusTrialing,
	}, nil)

	output, err := handler.HandleUpdateSubscription(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, subscriptionID, output.Body.SubscriptionID)

	mockRepo.AssertExpectations(t)
	mockService.AssertExpectations(t)
}

// TestHandleCancelSubscription_TrialingUser tests subscription cancellation for trialing users
func TestHandleCancelSubscription_TrialingUser(t *testing.T) {
	handler, mockService, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	customerID := "ctm_01h1234567890"
	subscriptionID := "sub_01h1234567890"
	user := &domain.User{
		ID:                   "user_123",
		Email:                "test@example.com",
		SubscriptionTier:     "premium",
		SubscriptionStatus:   "trialing", // Trialing user
		PaddleCustomerID:     &customerID,
		PaddleSubscriptionID: &subscriptionID,
	}

	input := &CancelSubscriptionInput{}
	input.UserID = "user_123"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil).Once()
	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil).Once()

	mockService.On("CancelSubscription", ctx, subscriptionID).Return(nil)

	mockService.On("GetSubscription", ctx, subscriptionID).
		Return(&paddle.Subscription{
			ID:                   subscriptionID,
			Status:               paddle.SubscriptionStatusActive,
			CurrentBillingPeriod: &paddle.TimePeriod{EndsAt: "2026-03-15"},
		}, nil)

	output, err := handler.HandleCancelSubscription(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Contains(t, output.Body.Message, "cancelled")
	assert.Equal(t, "2026-03-15", output.Body.EndsAt)

	mockRepo.AssertExpectations(t)
	mockService.AssertExpectations(t)
}
