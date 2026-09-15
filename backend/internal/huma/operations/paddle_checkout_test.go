package operations

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"

	paddle "github.com/PaddleHQ/paddle-go-sdk/v4"
	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
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

func (m *mockPaddleCheckoutService) CancelSubscriptionImmediately(ctx context.Context, subscriptionID string) error {
	return nil
}

func (m *mockPaddleCheckoutService) ArchiveAndAnonymizeCustomer(ctx context.Context, customerID, anonymizedEmail string) error {
	return nil
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
func (m *mockProfileRepo) GetUsersWithExpiredTrials(ctx context.Context) ([]*domain.User, error) {
	return nil, nil
}
func (m *mockProfileRepo) GetUsersWithExpiringTrials(ctx context.Context, withinDays int) ([]*domain.User, error) {
	return nil, nil
}
func (m *mockProfileRepo) CancelDeletion(ctx context.Context, userID string) error { return nil }
func (m *mockProfileRepo) UpdateSubscriptionFromPaddle(ctx context.Context, userID, tier, status string, startedAt, endsAt *time.Time, subscriptionID *string) error {
	return nil
}
func (m *mockProfileRepo) CreateUserTx(ctx context.Context, tx pgx.Tx, user *domain.User) error {
	return nil
}
// RecordDeletionResidue (Issue #1504) records that a deletion performed an irreversible external
// effect and then did not complete. See repository.ProfileRepository for the contract.
func (m *mockProfileRepo) RecordDeletionResidue(ctx context.Context, userID string, effect repository.DeletionResidueEffect) error {
	args := m.Called(ctx, userID, effect)
	return args.Error(0)
}

func (m *mockProfileRepo) DeleteUserCascade(ctx context.Context, userID string, expectedDeletionRequestedAt *time.Time) (bool, []string, error) {
	return true, nil, nil
}

// CountUncleanedLinkedItems — Issue #1494. Only DeletionService's Plaid-less branch calls this;
// these tests never reach it. Zero is the "nothing outstanding" answer, i.e. the non-blocking one.
func (m *mockProfileRepo) CountUncleanedLinkedItems(ctx context.Context, userID string) (int, error) {
	return 0, nil
}

// HouseholdOwnerBlocksDeletion — round 15, finding D2. Only DeletionService's pre-cleanup
// precondition calls this; these tests never reach it, so it is a plain stub for the same reason as
// the one above. It returns FALSE (nothing blocks) so a test that ever DID reach it would exercise
// the proceed path rather than an unexplained abort.
func (m *mockProfileRepo) HouseholdOwnerBlocksDeletion(ctx context.Context, userID string) (bool, error) {
	return false, nil
}

// DeletionStillAuthorised — ExecuteDeletion's use-time authorisation recheck. Unreachable from these
// checkout tests; returns TRUE so a future caller gets the proceed path rather than a silent skip.
func (m *mockProfileRepo) DeletionStillAuthorised(ctx context.Context, userID string, kind repository.DeletionClaimKind, expectedDeletionRequestedAt *time.Time) (bool, error) {
	return true, nil
}
func (m *mockProfileRepo) MarkForDeletion(ctx context.Context, userID string, scheduledAt time.Time) error {
	return nil
}
func (m *mockProfileRepo) UpdateUser(ctx context.Context, user *domain.User) error { return nil }
func (m *mockProfileRepo) GetUsersWithActivePaddleSubscriptions(ctx context.Context) ([]*domain.User, error) {
	return nil, nil
}
func (m *mockProfileRepo) GetEffectiveSubscriptionStatusByUserID(ctx context.Context, userID string) (string, error) {
	return "", nil
}
func (m *mockProfileRepo) SetRetentionDates(ctx context.Context, userID string, guard repository.RetentionClockGuard, retentionMonths int) error {
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
func (m *mockProfileRepo) SetPastDueStartedAt(ctx context.Context, userID string, t time.Time) error {
	return nil
}
func (m *mockProfileRepo) ClearPastDueStartedAt(ctx context.Context, userID string) error {
	return nil
}
func (m *mockProfileRepo) GetUsersInPastDue(ctx context.Context) ([]domain.User, error) {
	return []domain.User{}, nil
}

// GetUsersNeedingRetentionClockRepair, RepairRetentionClock (Issue #1413 H5 residual) are not
// exercised by these Paddle checkout tests; stubbed to satisfy the widened ProfileRepository
// interface.
func (m *mockProfileRepo) GetUsersNeedingRetentionClockRepair(ctx context.Context) ([]*domain.User, error) {
	return []*domain.User{}, nil
}

func (m *mockProfileRepo) RepairRetentionClock(ctx context.Context, userID string, startedAt time.Time, retentionMonths int) (bool, error) {
	return false, nil
}

// GetUsersNeedingPlaidDisconnectRepair (Issue #1413 round-7 grace-window repair path) is not
// exercised by these Paddle checkout tests; stubbed to satisfy the widened ProfileRepository
// interface.
func (m *mockProfileRepo) GetUsersNeedingPlaidDisconnectRepair(ctx context.Context, gracePeriod time.Duration) ([]*domain.User, error) {
	return []*domain.User{}, nil
}

func (m *mockProfileRepo) ClaimPlaidDisconnectIfPastDue(ctx context.Context, userID string) (bool, error) {
	return false, nil
}

// CountRetentionDeletionsWithheld is Issue #1569 T1's withholding-count read. Never exercised by
// this suite: it exists so mockProfileRepo keeps satisfying repository.ProfileRepository.
func (m *mockProfileRepo) CountRetentionDeletionsWithheld(ctx context.Context) (repository.RetentionWithholdingCounts, error) {
	return repository.RetentionWithholdingCounts{}, nil
}

// ClaimForDeletion (Issue #1413 Codex review round 3) is not exercised by these Paddle checkout
// tests; stubbed to satisfy the widened ProfileRepository interface.
func (m *mockProfileRepo) ClaimForDeletion(ctx context.Context, userID string, kind repository.DeletionClaimKind) (bool, error) {
	return false, nil
}

func (m *mockProfileRepo) UpdateUserEmailTx(ctx context.Context, tx pgx.Tx, userID, plainEmail string) error {
	args := m.Called(ctx, tx, userID, plainEmail)
	return args.Error(0)
}

// setupTestCheckoutHandlerBase creates a handler with mock dependencies and an optional
// entitlement checker. Used by setupTestCheckoutHandler and any test that needs a comped
// user AND a wired (mocked) EntitlementRepo — see
// TestHandleCreateCheckoutSession_CompedUser_HouseholdCoverageGateRuns.
func setupTestCheckoutHandlerBase(t *testing.T, entitlementRepo checkoutEntitlementReader) (*PaddleCheckoutHandler, *mockPaddleCheckoutService, *mockProfileRepo) {
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
		PaddleService:   mockService,
		ProfileRepo:     mockRepo,
		Config:          cfg,
		TierConfig:      tierConfig,
		Logger:          logger,
		EntitlementRepo: entitlementRepo,
	})

	return handler, mockService, mockRepo
}

// setupTestCheckoutHandler creates a handler with mock dependencies (no entitlement checker)
func setupTestCheckoutHandler(t *testing.T) (*PaddleCheckoutHandler, *mockPaddleCheckoutService, *mockProfileRepo) {
	t.Helper()
	return setupTestCheckoutHandlerBase(t, nil)
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

// ─── Household covered-member checkout guard (#1367 decision #9) ────────────

type mockCoverageCheckerForCheckout struct{ mock.Mock }

func (m *mockCoverageCheckerForCheckout) IsCoveredByHousehold(ctx context.Context, userID string) (bool, error) {
	args := m.Called(ctx, userID)
	return args.Bool(0), args.Error(1)
}

func TestHandleCreateCheckoutSession_CoveredByHousehold_Rejected(t *testing.T) {
	mockService := new(mockPaddleCheckoutService)
	mockRepo := new(mockProfileRepo)
	checker := &mockCoverageCheckerForCheckout{}
	tierConfig, err := config.NewTierConfig("")
	require.NoError(t, err)
	handler := NewPaddleCheckoutHandler(PaddleCheckoutHandlerDeps{
		PaddleService:   mockService,
		ProfileRepo:     mockRepo,
		Config:          &config.Config{FrontendURL: "https://app.example.com"},
		TierConfig:      tierConfig,
		EntitlementRepo: checker,
	})
	ctx := contextWithUserID("user_123")

	// A covered member's own subscription_status is whatever it was before joining —
	// here "inactive" (lapsed) — so only the coverage check can catch the overlap.
	user := &domain.User{ID: "user_123", Email: "test@example.com", SubscriptionStatus: "inactive"}
	input := &CreateCheckoutInput{}
	input.UserID = "user_123"
	input.Body.Tier = "premium"
	input.Body.BillingInterval = "month"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)
	checker.On("IsCoveredByHousehold", ctx, "user_123").Return(true, nil)

	output, err := handler.HandleCreateCheckoutSession(ctx, input)

	require.Error(t, err)
	require.Nil(t, output)
	statusErr, ok := err.(huma.StatusError)
	require.True(t, ok, "expected huma.StatusError")
	assert.Equal(t, http.StatusConflict, statusErr.GetStatus())
}

func TestHandleCreateCheckoutSession_NotCoveredByHousehold_Allowed(t *testing.T) {
	mockService := new(mockPaddleCheckoutService)
	mockRepo := new(mockProfileRepo)
	checker := &mockCoverageCheckerForCheckout{}
	tierConfig, err := config.NewTierConfig("")
	require.NoError(t, err)
	handler := NewPaddleCheckoutHandler(PaddleCheckoutHandlerDeps{
		PaddleService:   mockService,
		ProfileRepo:     mockRepo,
		Config:          &config.Config{FrontendURL: "https://app.example.com"},
		TierConfig:      tierConfig,
		EntitlementRepo: checker,
	})
	ctx := contextWithUserID("user_123")

	customerID := "ctm_01h1234567890"
	user := &domain.User{ID: "user_123", Email: "test@example.com", SubscriptionStatus: "inactive", PaddleCustomerID: &customerID}
	input := &CreateCheckoutInput{}
	input.UserID = "user_123"
	input.Body.Tier = "premium"
	input.Body.BillingInterval = "month"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)
	checker.On("IsCoveredByHousehold", ctx, "user_123").Return(false, nil)
	mockService.On("CreateCheckoutSession", ctx, customerID, mock.Anything, user.Email, mock.Anything, user.ID).
		Return("https://checkout.paddle.com/...", "txn_123", nil)

	output, err := handler.HandleCreateCheckoutSession(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
}

// TestHandleCreateCheckoutSession_RawEntitlementExistsButOwnerReadOnly_Allowed is the
// regression test for a real Codex-review finding: checking the RAW subscription_entitlements
// row (source='household') instead of EFFECTIVE coverage traps a member once their
// household owner goes read-only (past_due/cancelled) — the raw row still exists (Phase 7
// owns cleaning it up), so a naive raw check would keep blocking checkout even though the
// member is no longer actually covered and legitimately needs to self-subscribe.
func TestHandleCreateCheckoutSession_RawEntitlementExistsButOwnerReadOnly_Allowed(t *testing.T) {
	mockService := new(mockPaddleCheckoutService)
	mockRepo := new(mockProfileRepo)
	checker := &mockCoverageCheckerForCheckout{}
	tierConfig, err := config.NewTierConfig("")
	require.NoError(t, err)
	handler := NewPaddleCheckoutHandler(PaddleCheckoutHandlerDeps{
		PaddleService:   mockService,
		ProfileRepo:     mockRepo,
		Config:          &config.Config{FrontendURL: "https://app.example.com"},
		TierConfig:      tierConfig,
		EntitlementRepo: checker,
	})
	ctx := contextWithUserID("user_123")

	customerID := "ctm_01h1234567890"
	user := &domain.User{ID: "user_123", Email: "test@example.com", SubscriptionStatus: "inactive", PaddleCustomerID: &customerID}
	input := &CreateCheckoutInput{}
	input.UserID = "user_123"
	input.Body.Tier = "premium"
	input.Body.BillingInterval = "month"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)
	// IsCoveredByHousehold already resolves this correctly to false (owner read-only
	// reverts the member to "own" per ResolveEffectiveEntitlement) even though a raw
	// source='household' entitlement row still exists in the DB.
	checker.On("IsCoveredByHousehold", ctx, "user_123").Return(false, nil)
	mockService.On("CreateCheckoutSession", ctx, customerID, mock.Anything, user.Email, mock.Anything, user.ID).
		Return("https://checkout.paddle.com/...", "txn_123", nil)

	output, err := handler.HandleCreateCheckoutSession(ctx, input)

	require.NoError(t, err, "a member no longer effectively covered must be able to self-subscribe")
	require.NotNil(t, output)
}

func TestHandleCreateCheckoutSession_NoEntitlementRepoConfigured_StillWorks(t *testing.T) {
	// Backward compatibility: handlers constructed without EntitlementRepo (nil) must
	// keep working — the guard is skipped, not a panic.
	handler, mockService, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	customerID := "ctm_01h1234567890"
	user := &domain.User{ID: "user_123", Email: "test@example.com", SubscriptionStatus: "inactive", PaddleCustomerID: &customerID}
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

// TestHandleCreateCheckoutSession_CompedUser_RawInactive_AllowedToCheckout is a #1413 pin test
// AND this issue's AC 4 ("a comped user can still purchase a real subscription"). The duplicate-
// purchase guard (paddle_checkout.go:262-270) deliberately reads the RAW user.SubscriptionStatus,
// never ComputeEffectiveStatus(). A comped user's real Paddle subscription lapsed (raw
// "inactive") while their override forces effective status to "active" — if this guard read
// effective status, a comped user could never reach checkout, and would be hard-locked out of
// ever buying a real subscription the moment the comp lapses. This test proves checkout still
// succeeds for exactly that user.
func TestHandleCreateCheckoutSession_CompedUser_RawInactive_AllowedToCheckout(t *testing.T) {
	handler, mockService, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	customerID := "ctm_01h1234567890"
	override := "household_2"
	futureEnd := time.Now().Add(30 * 24 * time.Hour)
	user := &domain.User{
		ID:                 "user_123",
		Email:              "test@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "inactive", // real subscription lapsed
		PaddleCustomerID:   &customerID,
		ManualTierOverride: &override,
		ManualTierEndsAt:   &futureEnd, // admin comp, still active -> effective status is "active"
	}

	input := &CreateCheckoutInput{}
	input.UserID = "user_123"
	input.Body.Tier = "premium"
	input.Body.BillingInterval = "year"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)
	mockService.On("CreateCheckoutSession", ctx, customerID, mock.Anything, user.Email, mock.Anything, user.ID).
		Return("https://checkout.paddle.com/...", "txn_456", nil)

	output, err := handler.HandleCreateCheckoutSession(ctx, input)

	require.NoError(t, err, "a comped user with a lapsed real subscription must still reach "+
		"checkout — the guard must read raw status, not the comp-forced effective status")
	require.NotNil(t, output)
	assert.Contains(t, output.Body.CheckoutURL, "checkout.paddle.com")

	mockRepo.AssertExpectations(t)
	mockService.AssertExpectations(t)
}

// TestHandleCreateCheckoutSession_CompedUser_RawCancelled_AllowedToCheckout is the sibling of
// the above with raw status "cancelled" instead of "inactive" — the other read-only raw status
// duplicate-purchase applies to for a comped user whose real subscription was cancelled before
// the comp was granted.
func TestHandleCreateCheckoutSession_CompedUser_RawCancelled_AllowedToCheckout(t *testing.T) {
	handler, mockService, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	customerID := "ctm_01h1234567890"
	override := "household_3"
	user := &domain.User{
		ID:                 "user_123",
		Email:              "test@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "cancelled", // real subscription cancelled
		PaddleCustomerID:   &customerID,
		ManualTierOverride: &override,
		ManualTierEndsAt:   nil, // permanent comp -> effective status is "active"
	}

	input := &CreateCheckoutInput{}
	input.UserID = "user_123"
	input.Body.Tier = "premium"
	input.Body.BillingInterval = "month"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)
	mockService.On("CreateCheckoutSession", ctx, customerID, mock.Anything, user.Email, mock.Anything, user.ID).
		Return("https://checkout.paddle.com/...", "txn_789", nil)

	output, err := handler.HandleCreateCheckoutSession(ctx, input)

	require.NoError(t, err, "a comped user with a cancelled real subscription must still reach "+
		"checkout — the guard must read raw status, not the comp-forced effective status")
	require.NotNil(t, output)
	assert.Contains(t, output.Body.CheckoutURL, "checkout.paddle.com")

	mockRepo.AssertExpectations(t)
	mockService.AssertExpectations(t)
}

// TestHandleCreateCheckoutSession_CompedUser_HouseholdCoverageGateRuns is the requirements-gap
// analysis (PR #1443) item 4 fix: both comped-checkout pin tests above
// (...RawInactive_AllowedToCheckout and its RawCancelled sibling) use setupTestCheckoutHandler,
// which wires EntitlementRepo as nil — so the household-coverage gate at paddle_checkout.go:262-270
// is SKIPPED entirely (the `if h.entitlementRepo != nil` guard never even evaluates the check) on
// both, never actually exercised for a comped user. Harmless in production today — an admin
// override named e.g. "household_2" never creates a real household_members row, so
// IsCoveredByHousehold always resolves false for a comped user regardless — but that was unproven
// by any test. This variant wires a real (mocked) checker via setupTestCheckoutHandlerBase and
// asserts the checker's expectation is actually satisfied (mock.AssertExpectations fails an
// un-called On(...) expectation), so this test cannot pass by the gate silently being skipped
// again — unlike its two RawInactive/RawCancelled siblings, which would pass identically whether
// or not the gate ran at all.
func TestHandleCreateCheckoutSession_CompedUser_HouseholdCoverageGateRuns(t *testing.T) {
	checker := &mockCoverageCheckerForCheckout{}
	handler, mockService, mockRepo := setupTestCheckoutHandlerBase(t, checker)
	ctx := contextWithUserID("user_123")

	customerID := "ctm_01h1234567890"
	override := "household_2"
	futureEnd := time.Now().Add(30 * 24 * time.Hour)
	user := &domain.User{
		ID:                 "user_123",
		Email:              "test@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "inactive", // real subscription lapsed
		PaddleCustomerID:   &customerID,
		ManualTierOverride: &override,
		ManualTierEndsAt:   &futureEnd, // admin comp, still active -> effective status is "active"
	}

	input := &CreateCheckoutInput{}
	input.UserID = "user_123"
	input.Body.Tier = "premium"
	input.Body.BillingInterval = "month"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)
	checker.On("IsCoveredByHousehold", ctx, "user_123").Return(false, nil)
	mockService.On("CreateCheckoutSession", ctx, customerID, mock.Anything, user.Email, mock.Anything, user.ID).
		Return("https://checkout.paddle.com/...", "txn_comp_gate", nil)

	output, err := handler.HandleCreateCheckoutSession(ctx, input)

	require.NoError(t, err, "a comped user not covered by a household must still reach checkout")
	require.NotNil(t, output)
	checker.AssertExpectations(t) // the pin: fails if IsCoveredByHousehold was never called
	mockRepo.AssertExpectations(t)
	mockService.AssertExpectations(t)
}

// --- Context-aware pricing: HandleGetCurrentSubscription extended status tests ---

// TestHandleGetCurrentSubscription_CancelledWithScheduledCancel_ReturnsCancelAtPeriodEnd verifies
// that a cancelled user with a scheduled Paddle cancel returns cancel_at_period_end=true and scheduled_change_at.
func TestHandleGetCurrentSubscription_CancelledWithScheduledCancel_ReturnsCancelAtPeriodEnd(t *testing.T) {
	handler, mockService, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	customerID := "ctm_01h1234567890"
	subscriptionID := "sub_01h1234567890"
	user := &domain.User{
		ID:                   "user_123",
		Email:                "test@example.com",
		SubscriptionTier:     "premium",
		SubscriptionStatus:   "cancelled",
		PaddleCustomerID:     &customerID,
		PaddleSubscriptionID: &subscriptionID,
	}

	input := &GetCurrentSubscriptionInput{}
	input.UserID = "user_123"

	effectiveAt := "2026-05-01T00:00:00Z"
	cancelAction := paddle.ScheduledChangeActionCancel

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil).Once()
	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil).Once()
	mockService.On("GetSubscription", ctx, subscriptionID).Return(&paddle.Subscription{
		ID:              subscriptionID,
		Status:          paddle.SubscriptionStatusActive,
		BillingCycle:    paddle.Duration{Interval: "month", Frequency: 1},
		CurrencyCode:    "USD",
		ScheduledChange: &paddle.SubscriptionScheduledChange{Action: cancelAction, EffectiveAt: effectiveAt},
		Items:           []paddle.SubscriptionItem{{Price: paddle.Price{UnitPrice: paddle.Money{Amount: "8.99"}}}},
	}, nil)

	output, err := handler.HandleGetCurrentSubscription(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.True(t, output.Body.CancelAtPeriodEnd, "expected cancel_at_period_end=true when scheduled cancel exists")
	assert.Equal(t, effectiveAt, output.Body.ScheduledChangeAt)
	assert.Equal(t, subscriptionID, output.Body.SubscriptionID)

	mockRepo.AssertExpectations(t)
	mockService.AssertExpectations(t)
}

// TestHandleGetCurrentSubscription_CancelledNoScheduledChange_ReturnsData verifies
// that a cancelled user without ScheduledChange still returns subscription data with cancel_at_period_end=false.
func TestHandleGetCurrentSubscription_CancelledNoScheduledChange_ReturnsData(t *testing.T) {
	handler, mockService, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	customerID := "ctm_01h1234567890"
	subscriptionID := "sub_01h1234567890"
	user := &domain.User{
		ID:                   "user_123",
		Email:                "test@example.com",
		SubscriptionTier:     "premium",
		SubscriptionStatus:   "cancelled",
		PaddleCustomerID:     &customerID,
		PaddleSubscriptionID: &subscriptionID,
	}

	input := &GetCurrentSubscriptionInput{}
	input.UserID = "user_123"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil).Once()
	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil).Once()
	mockService.On("GetSubscription", ctx, subscriptionID).Return(&paddle.Subscription{
		ID:              subscriptionID,
		Status:          paddle.SubscriptionStatusCanceled,
		BillingCycle:    paddle.Duration{Interval: "month", Frequency: 1},
		CurrencyCode:    "USD",
		ScheduledChange: nil,
	}, nil)

	output, err := handler.HandleGetCurrentSubscription(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.False(t, output.Body.CancelAtPeriodEnd)
	assert.Empty(t, output.Body.ScheduledChangeAt)
	assert.Equal(t, subscriptionID, output.Body.SubscriptionID)

	mockRepo.AssertExpectations(t)
	mockService.AssertExpectations(t)
}

// TestHandleGetCurrentSubscription_PastDueReturnsData verifies past_due users get subscription data.
func TestHandleGetCurrentSubscription_PastDueReturnsData(t *testing.T) {
	handler, mockService, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	customerID := "ctm_01h1234567890"
	subscriptionID := "sub_01h1234567890"
	user := &domain.User{
		ID:                   "user_123",
		Email:                "test@example.com",
		SubscriptionTier:     "premium",
		SubscriptionStatus:   "past_due",
		PaddleCustomerID:     &customerID,
		PaddleSubscriptionID: &subscriptionID,
	}

	input := &GetCurrentSubscriptionInput{}
	input.UserID = "user_123"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil).Once()
	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil).Once()
	mockService.On("GetSubscription", ctx, subscriptionID).Return(&paddle.Subscription{
		ID:              subscriptionID,
		Status:          paddle.SubscriptionStatusPastDue,
		BillingCycle:    paddle.Duration{Interval: "month", Frequency: 1},
		CurrencyCode:    "USD",
		ScheduledChange: nil,
	}, nil)

	output, err := handler.HandleGetCurrentSubscription(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, subscriptionID, output.Body.SubscriptionID)
	assert.Equal(t, "past_due", output.Body.Status)

	mockRepo.AssertExpectations(t)
	mockService.AssertExpectations(t)
}

// TestHandleGetCurrentSubscription_InactiveReturns404 verifies inactive users get 404 (not 400).
func TestHandleGetCurrentSubscription_InactiveReturns404(t *testing.T) {
	handler, _, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	user := &domain.User{
		ID:                   "user_123",
		Email:                "test@example.com",
		SubscriptionTier:     "premium",
		SubscriptionStatus:   "inactive",
		PaddleCustomerID:     nil,
		PaddleSubscriptionID: nil,
	}

	input := &GetCurrentSubscriptionInput{}
	input.UserID = "user_123"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)

	output, err := handler.HandleGetCurrentSubscription(ctx, input)

	require.Error(t, err)
	assert.Nil(t, output)
	var humaErr huma.StatusError
	require.True(t, errors.As(err, &humaErr), "expected huma.StatusError")
	assert.Equal(t, 404, humaErr.GetStatus())

	mockRepo.AssertExpectations(t)
}

// TestHandleGetCurrentSubscription_NilCurrentBillingPeriodDoesNotPanic verifies nil CurrentBillingPeriod
// (returned by Paddle for canceled/past_due subscriptions) does not cause a nil pointer panic.
func TestHandleGetCurrentSubscription_NilCurrentBillingPeriodDoesNotPanic(t *testing.T) {
	handler, mockService, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	customerID := "ctm_01h1234567890"
	subscriptionID := "sub_01h1234567890"
	user := &domain.User{
		ID:                   "user_123",
		Email:                "test@example.com",
		SubscriptionTier:     "premium",
		SubscriptionStatus:   "cancelled",
		PaddleCustomerID:     &customerID,
		PaddleSubscriptionID: &subscriptionID,
	}

	input := &GetCurrentSubscriptionInput{}
	input.UserID = "user_123"

	cancelAction := paddle.ScheduledChangeActionCancel
	effectiveAt := "2026-05-01T00:00:00Z"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil).Once()
	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil).Once()
	mockService.On("GetSubscription", ctx, subscriptionID).Return(&paddle.Subscription{
		ID:                   subscriptionID,
		Status:               paddle.SubscriptionStatusCanceled,
		BillingCycle:         paddle.Duration{Interval: "month", Frequency: 1},
		CurrencyCode:         "USD",
		CurrentBillingPeriod: nil,
		ScheduledChange:      &paddle.SubscriptionScheduledChange{Action: cancelAction, EffectiveAt: effectiveAt},
	}, nil)

	assert.NotPanics(t, func() {
		_, _ = handler.HandleGetCurrentSubscription(ctx, input)
	})

	mockRepo.AssertExpectations(t)
	mockService.AssertExpectations(t)
}

// TestHandleGetCurrentSubscription_CompedInactive_Returns200NotFound verifies that a comped
// user with a raw "inactive" status (no Paddle subscription) gets a synthetic 200 response
// instead of the 404 a non-comped inactive user would receive (Issue #1413).
func TestHandleGetCurrentSubscription_CompedInactive_Returns200NotFound(t *testing.T) {
	handler, _, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	override := "household_2"
	endsAt := time.Now().Add(30 * 24 * time.Hour)
	user := &domain.User{
		ID:                   "user_123",
		Email:                "test@example.com",
		SubscriptionTier:     "premium",
		SubscriptionStatus:   "inactive",
		PaddleSubscriptionID: nil,
		ManualTierOverride:   &override,
		ManualTierEndsAt:     &endsAt,
	}

	input := &GetCurrentSubscriptionInput{}
	input.UserID = "user_123"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)

	output, err := handler.HandleGetCurrentSubscription(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "active", output.Body.Status)
	assert.Equal(t, "household_2", output.Body.Tier)
	assert.Empty(t, output.Body.SubscriptionID)
	assert.True(t, output.Body.IsComped)
	require.NotEmpty(t, output.Body.CompEndsAt)
	parsed, parseErr := time.Parse(time.RFC3339, output.Body.CompEndsAt)
	require.NoError(t, parseErr, "comp_ends_at must be RFC 3339")
	assert.WithinDuration(t, endsAt, parsed, time.Second)
	assert.Equal(t, "USD", output.Body.Currency)
	assert.False(t, output.Body.CancelAtPeriodEnd)
	assert.Empty(t, output.Body.BillingInterval)

	mockRepo.AssertExpectations(t)
}

// TestHandleGetCurrentSubscription_CompedPermanent_CompEndsAtOmitted verifies a permanent
// comp (no ManualTierEndsAt) omits comp_ends_at from the synthetic response entirely.
func TestHandleGetCurrentSubscription_CompedPermanent_CompEndsAtOmitted(t *testing.T) {
	handler, _, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	override := "household_2"
	user := &domain.User{
		ID:                 "user_123",
		Email:              "test@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "inactive",
		ManualTierOverride: &override,
		ManualTierEndsAt:   nil, // permanent
	}

	input := &GetCurrentSubscriptionInput{}
	input.UserID = "user_123"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)

	output, err := handler.HandleGetCurrentSubscription(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.True(t, output.Body.IsComped)
	assert.Empty(t, output.Body.CompEndsAt)

	mockRepo.AssertExpectations(t)
}

// TestHandleGetCurrentSubscription_CompedWithLivePaddleSubscription verifies that a comp
// layered on top of a real (e.g. past_due) Paddle subscription returns the real Paddle data
// plus is_comped/comp_ends_at — billing truth is preserved, the comp is annotated.
func TestHandleGetCurrentSubscription_CompedWithLivePaddleSubscription(t *testing.T) {
	handler, mockService, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	customerID := "ctm_01h1234567890"
	subscriptionID := "sub_01h1234567890"
	override := "household_2"
	endsAt := time.Now().Add(10 * 24 * time.Hour)
	user := &domain.User{
		ID:                   "user_123",
		Email:                "test@example.com",
		SubscriptionTier:     "premium",
		SubscriptionStatus:   "past_due",
		PaddleCustomerID:     &customerID,
		PaddleSubscriptionID: &subscriptionID,
		ManualTierOverride:   &override,
		ManualTierEndsAt:     &endsAt,
	}

	input := &GetCurrentSubscriptionInput{}
	input.UserID = "user_123"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil).Once()
	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil).Once()
	mockService.On("GetSubscription", ctx, subscriptionID).Return(&paddle.Subscription{
		ID:              subscriptionID,
		Status:          paddle.SubscriptionStatusPastDue,
		BillingCycle:    paddle.Duration{Interval: "month", Frequency: 1},
		CurrencyCode:    "USD",
		ScheduledChange: nil,
	}, nil)

	output, err := handler.HandleGetCurrentSubscription(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	// Billing truth preserved: real subscription ID and raw Paddle status, unchanged.
	assert.Equal(t, subscriptionID, output.Body.SubscriptionID)
	assert.Equal(t, "past_due", output.Body.Status)
	// Comp annotated on top.
	assert.True(t, output.Body.IsComped)
	require.NotEmpty(t, output.Body.CompEndsAt)
	parsed, parseErr := time.Parse(time.RFC3339, output.Body.CompEndsAt)
	require.NoError(t, parseErr, "comp_ends_at must be RFC 3339")
	assert.WithinDuration(t, endsAt, parsed, time.Second)

	mockRepo.AssertExpectations(t)
	mockService.AssertExpectations(t)
}

// TestHandleGetCurrentSubscription_CompedTrialUser_ReturnsCompNotTrial verifies a comp
// dominates a trial: a comped user with raw status "trial" gets the comp response, not the
// trial branch.
func TestHandleGetCurrentSubscription_CompedTrialUser_ReturnsCompNotTrial(t *testing.T) {
	handler, _, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	override := "household_2"
	endsAt := time.Now().Add(20 * 24 * time.Hour)
	trialEndsAt := time.Now().Add(45 * 24 * time.Hour)
	user := &domain.User{
		ID:                 "user_123",
		Email:              "test@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "trial",
		TrialEndsAt:        &trialEndsAt,
		ManualTierOverride: &override,
		ManualTierEndsAt:   &endsAt,
	}

	input := &GetCurrentSubscriptionInput{}
	input.UserID = "user_123"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)

	output, err := handler.HandleGetCurrentSubscription(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "active", output.Body.Status)
	assert.True(t, output.Body.IsComped)
	assert.Empty(t, output.Body.TrialEndsAt, "comp dominates trial: trial fields must not be populated")
	assert.Nil(t, output.Body.TrialDaysRemaining)

	mockRepo.AssertExpectations(t)
}

// TestHandleGetCurrentSubscription_CompedInactive_NoFakeUpcomingCharge verifies the
// synthetic comped response never populates next_billing_date, current_period_end, or
// amount — the frontend computes next_billing_date ?? current_period_end, so any value
// there would render as a fake upcoming charge for someone who isn't being charged.
func TestHandleGetCurrentSubscription_CompedInactive_NoFakeUpcomingCharge(t *testing.T) {
	handler, _, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	override := "household_2"
	endsAt := time.Now().Add(30 * 24 * time.Hour)
	user := &domain.User{
		ID:                 "user_123",
		Email:              "test@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "inactive",
		ManualTierOverride: &override,
		ManualTierEndsAt:   &endsAt,
	}

	input := &GetCurrentSubscriptionInput{}
	input.UserID = "user_123"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)

	output, err := handler.HandleGetCurrentSubscription(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Empty(t, output.Body.NextBillingDate)
	assert.Empty(t, output.Body.CurrentPeriodEnd)
	assert.Empty(t, output.Body.Amount)

	mockRepo.AssertExpectations(t)
}

// TestHandleGetCurrentSubscription_CompedNoSubscriptionID_Returns200 verifies that a comped
// user whose raw status is a Paddle-lookup status (e.g. "active") but who has no
// PaddleSubscriptionID (getActiveSubscriptionID fails) also gets the synthetic 200, not a 404.
func TestHandleGetCurrentSubscription_CompedNoSubscriptionID_Returns200(t *testing.T) {
	handler, _, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	override := "household_3"
	user := &domain.User{
		ID:                   "user_123",
		Email:                "test@example.com",
		SubscriptionTier:     "premium",
		SubscriptionStatus:   "active",
		PaddleSubscriptionID: nil, // no Paddle subscription — getActiveSubscriptionID fails
		ManualTierOverride:   &override,
		ManualTierEndsAt:     nil,
	}

	input := &GetCurrentSubscriptionInput{}
	input.UserID = "user_123"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil).Once()
	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil).Once()

	output, err := handler.HandleGetCurrentSubscription(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "active", output.Body.Status)
	assert.Equal(t, "household_3", output.Body.Tier)
	assert.True(t, output.Body.IsComped)
	assert.Empty(t, output.Body.SubscriptionID)

	mockRepo.AssertExpectations(t)
}

// TestHandleGetCurrentSubscription_NotComped_InactiveStillReturns404 verifies non-comped
// paths are unchanged: a non-comped inactive user still gets a 404, not the synthetic 200.
func TestHandleGetCurrentSubscription_NotComped_InactiveStillReturns404(t *testing.T) {
	handler, _, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	user := &domain.User{
		ID:                   "user_123",
		Email:                "test@example.com",
		SubscriptionTier:     "premium",
		SubscriptionStatus:   "inactive",
		PaddleCustomerID:     nil,
		PaddleSubscriptionID: nil,
	}

	input := &GetCurrentSubscriptionInput{}
	input.UserID = "user_123"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)

	output, err := handler.HandleGetCurrentSubscription(ctx, input)

	require.Error(t, err)
	assert.Nil(t, output)
	var humaErr huma.StatusError
	require.True(t, errors.As(err, &humaErr), "expected huma.StatusError")
	assert.Equal(t, 404, humaErr.GetStatus())

	mockRepo.AssertExpectations(t)
}

// TestHandleGetCurrentSubscription_NotComped_IsCompedFalseInResponse verifies non-comped
// users hitting the real Paddle-lookup path get is_comped=false and comp_ends_at empty in
// the response (existing matrix — response now carries the new fields, defaulted).
func TestHandleGetCurrentSubscription_NotComped_IsCompedFalseInResponse(t *testing.T) {
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
		PaddleSubscriptionID: &subscriptionID,
	}

	input := &GetCurrentSubscriptionInput{}
	input.UserID = "user_123"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil).Once()
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
	assert.False(t, output.Body.IsComped)
	assert.Empty(t, output.Body.CompEndsAt)

	mockRepo.AssertExpectations(t)
	mockService.AssertExpectations(t)
}

// TestHandleGetCurrentSubscription_MalformedOverride_NotComped is the RED test for Issue #1413
// round-5 (adversarial review finding): a manual_tier_override naming NO configured tier
// ('not_a_tier' — garbage, not a real tier name in config/subscription_tiers.yaml) must NOT
// produce the synthetic active-comp payload, even though the raw override is non-expired
// (future-dated). Before this fix, isComped was computed via
// user.HasActiveManualOverride() alone — "non-empty and not expired" — with no check that the
// override names a real tier, so GET /subscription/current could report a working comp for a
// corrupt override the read-only middleware would then 403 on write (the exact
// representation/access mismatch this fix closes).
func TestHandleGetCurrentSubscription_MalformedOverride_NotComped(t *testing.T) {
	handler, _, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	override := "not_a_tier"
	endsAt := time.Now().Add(30 * 24 * time.Hour) // future-dated: not expired
	user := &domain.User{
		ID:                   "user_123",
		Email:                "test@example.com",
		SubscriptionTier:     "premium",
		SubscriptionStatus:   "inactive",
		PaddleSubscriptionID: nil,
		ManualTierOverride:   &override,
		ManualTierEndsAt:     &endsAt,
	}

	input := &GetCurrentSubscriptionInput{}
	input.UserID = "user_123"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)

	output, err := handler.HandleGetCurrentSubscription(ctx, input)

	// A non-comped inactive user with no Paddle subscription must 404, not get a synthetic
	// 200 comp payload.
	require.Error(t, err)
	assert.Nil(t, output)
	var humaErr huma.StatusError
	require.True(t, errors.As(err, &humaErr), "expected huma.StatusError")
	assert.Equal(t, 404, humaErr.GetStatus())

	mockRepo.AssertExpectations(t)
}

// TestHandleGetCurrentSubscription_MalformedOverride_TrialUser_TierFallsBackToRaw is the RED
// test for Issue #1413 (this task — tier axis). This is the trial-user branch specifically: it
// is only reached when `!isComped` (see the `if user.SubscriptionStatus == "trial" && !isComped`
// guard just above it in HandleGetCurrentSubscription), so isComped is already correctly false
// here — but before this fix, out.Body.Tier was set via user.ComputeEffectiveTier() (unvalidated)
// instead of reusing that already-computed, already-correct !isComped fact, so the malformed
// override's raw garbage string still leaked into the response's tier field even though
// is_comped correctly read false.
func TestHandleGetCurrentSubscription_MalformedOverride_TrialUser_TierFallsBackToRaw(t *testing.T) {
	handler, _, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	override := "not_a_tier"
	endsAt := time.Now().Add(30 * 24 * time.Hour) // future-dated: not expired
	trialEndsAt := time.Now().Add(45 * 24 * time.Hour)
	user := &domain.User{
		ID:                 "user_123",
		Email:              "test@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "trial",
		TrialEndsAt:        &trialEndsAt,
		ManualTierOverride: &override,
		ManualTierEndsAt:   &endsAt,
	}

	input := &GetCurrentSubscriptionInput{}
	input.UserID = "user_123"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)

	output, err := handler.HandleGetCurrentSubscription(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "trial", output.Body.Status)
	assert.False(t, output.Body.IsComped)
	assert.Equal(t, "premium", output.Body.Tier, "a malformed override must fall back to the raw tier, not the garbage override string")

	mockRepo.AssertExpectations(t)
}

// TestHandleGetCurrentSubscription_MalformedOverride_ActiveWithPaddleSub_TierFallsBackToRaw is
// the RED test for Issue #1413 (this task — tier axis) at the real-Paddle-lookup branch: a
// malformed override on a user with a genuine, active Paddle subscription must not leak into
// out.Body.Tier — before this fix, this line called user.ComputeEffectiveTier() (unvalidated)
// even though isComped (computed just above, and correctly false here) already reflects the
// rejection.
func TestHandleGetCurrentSubscription_MalformedOverride_ActiveWithPaddleSub_TierFallsBackToRaw(t *testing.T) {
	handler, mockService, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	override := "not_a_tier"
	endsAt := time.Now().Add(30 * 24 * time.Hour)
	subscriptionID := "sub_01h1234567890"
	user := &domain.User{
		ID:                   "user_123",
		Email:                "test@example.com",
		SubscriptionTier:     "premium",
		SubscriptionStatus:   "active",
		PaddleSubscriptionID: &subscriptionID,
		ManualTierOverride:   &override,
		ManualTierEndsAt:     &endsAt,
	}

	input := &GetCurrentSubscriptionInput{}
	input.UserID = "user_123"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil).Once()
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
	assert.False(t, output.Body.IsComped)
	assert.Equal(t, "premium", output.Body.Tier, "a malformed override must fall back to the raw tier, not the garbage override string")

	mockRepo.AssertExpectations(t)
	mockService.AssertExpectations(t)
}

// TestHandleGetCurrentSubscription_ValidOverride_StillComped is the positive control demanded
// alongside the RED test above: the tier-name validation added for the malformed-override fix
// must not become over-tight and start hiding legitimate comps. A genuinely valid, configured
// override tier ("premium") must still produce the synthetic active-comp payload.
func TestHandleGetCurrentSubscription_ValidOverride_StillComped(t *testing.T) {
	handler, _, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	override := "premium"
	user := &domain.User{
		ID:                   "user_123",
		Email:                "test@example.com",
		SubscriptionTier:     "premium",
		SubscriptionStatus:   "inactive",
		PaddleSubscriptionID: nil,
		ManualTierOverride:   &override,
		ManualTierEndsAt:     nil, // permanent comp
	}

	input := &GetCurrentSubscriptionInput{}
	input.UserID = "user_123"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)

	output, err := handler.HandleGetCurrentSubscription(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.True(t, output.Body.IsComped, "a validly-configured override tier must still report is_comped=true")
	assert.Equal(t, "active", output.Body.Status)
	assert.Equal(t, "premium", output.Body.Tier)

	mockRepo.AssertExpectations(t)
}

// --- Context-aware pricing: HandleGetBillingPortalURL extended status tests ---

// TestHandleGetBillingPortalURL_AllowsCancelled verifies cancelled users can get billing portal URL.
func TestHandleGetBillingPortalURL_AllowsCancelled(t *testing.T) {
	handler, mockService, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	customerID := "ctm_cancelled_123"
	subscriptionID := "sub_cancelled_123"
	user := &domain.User{
		ID:                   "user_123",
		Email:                "test@example.com",
		SubscriptionStatus:   "cancelled",
		PaddleCustomerID:     &customerID,
		PaddleSubscriptionID: &subscriptionID,
	}

	input := &GetBillingPortalURLInput{}
	input.UserID = "user_123"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)
	mockService.On("GetBillingPortalURL", ctx, customerID).Return("https://buy.paddle.com/customer/portal", nil)

	output, err := handler.HandleGetBillingPortalURL(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.NotEmpty(t, output.Body.PortalURL)

	mockRepo.AssertExpectations(t)
	mockService.AssertExpectations(t)
}

// TestHandleGetBillingPortalURL_AllowsPastDue verifies past_due users can get billing portal URL.
func TestHandleGetBillingPortalURL_AllowsPastDue(t *testing.T) {
	handler, mockService, mockRepo := setupTestCheckoutHandler(t)
	ctx := contextWithUserID("user_123")

	customerID := "ctm_pastdue_123"
	subscriptionID := "sub_pastdue_123"
	user := &domain.User{
		ID:                   "user_123",
		Email:                "test@example.com",
		SubscriptionStatus:   "past_due",
		PaddleCustomerID:     &customerID,
		PaddleSubscriptionID: &subscriptionID,
	}

	input := &GetBillingPortalURLInput{}
	input.UserID = "user_123"

	mockRepo.On("GetUserByID", ctx, "user_123").Return(user, nil)
	mockService.On("GetBillingPortalURL", ctx, customerID).Return("https://buy.paddle.com/customer/portal", nil)

	output, err := handler.HandleGetBillingPortalURL(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.NotEmpty(t, output.Body.PortalURL)

	mockRepo.AssertExpectations(t)
	mockService.AssertExpectations(t)
}
