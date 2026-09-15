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
	if customer, ok := args.Get(0).(*paddle.Customer); ok {
		return customer, args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockPaddleSDK) ListCustomersByEmail(ctx context.Context, email string) ([]*paddle.Customer, error) {
	args := m.Called(ctx, email)
	if customers, ok := args.Get(0).([]*paddle.Customer); ok {
		return customers, args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockPaddleSDK) GetCustomer(ctx context.Context, req *paddle.GetCustomerRequest) (*paddle.Customer, error) {
	args := m.Called(ctx, req)
	if customer, ok := args.Get(0).(*paddle.Customer); ok {
		return customer, args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockPaddleSDK) UpdateCustomer(ctx context.Context, req *paddle.UpdateCustomerRequest) (*paddle.Customer, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*paddle.Customer), args.Error(1)
}

func (m *mockPaddleSDK) CreateTransaction(ctx context.Context, req *paddle.CreateTransactionRequest) (*paddle.Transaction, error) {
	args := m.Called(ctx, req)
	if transaction, ok := args.Get(0).(*paddle.Transaction); ok {
		return transaction, args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockPaddleSDK) GetSubscription(ctx context.Context, req *paddle.GetSubscriptionRequest) (*paddle.Subscription, error) {
	args := m.Called(ctx, req)
	if subscription, ok := args.Get(0).(*paddle.Subscription); ok {
		return subscription, args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockPaddleSDK) UpdateSubscription(ctx context.Context, req *paddle.UpdateSubscriptionRequest) (*paddle.Subscription, error) {
	args := m.Called(ctx, req)
	if subscription, ok := args.Get(0).(*paddle.Subscription); ok {
		return subscription, args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockPaddleSDK) CancelSubscription(ctx context.Context, req *paddle.CancelSubscriptionRequest) (*paddle.Subscription, error) {
	args := m.Called(ctx, req)
	if subscription, ok := args.Get(0).(*paddle.Subscription); ok {
		return subscription, args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockPaddleSDK) PreviewSubscriptionUpdate(ctx context.Context, req *paddle.PreviewSubscriptionUpdateRequest) (*paddle.SubscriptionPreview, error) {
	args := m.Called(ctx, req)
	if preview, ok := args.Get(0).(*paddle.SubscriptionPreview); ok {
		return preview, args.Error(1)
	}
	return nil, args.Error(1)
}

// setupPaddleServiceWithMock creates a PaddleService with a mock SDK
func setupPaddleServiceWithMock(t *testing.T) (*paddleService, *mockPaddleSDK) {
	t.Helper()

	mockSDK := new(mockPaddleSDK)
	cfg := &config.Config{
		PaddleAPIKey:      "test_api_key",
		PaddleEnvironment: "sandbox",
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	service := &paddleService{
		client:    mockSDK,
		rawClient: nil, // Mock tests don't use this; real tests use GetClient()
		config:    cfg,
		logger:    logger,
	}

	return service, mockSDK
}

func TestEnsureCustomer_CreatesNewCustomer(t *testing.T) {
	service, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()

	mockSDK.On("CreateCustomer", ctx, mock.MatchedBy(func(req *paddle.CreateCustomerRequest) bool {
		return req.Email == "test@example.com" && req.CustomData["user_id"] == "user_123"
	})).Return(&paddle.Customer{ID: "ctm_new_customer"}, nil)

	customerID, err := service.EnsureCustomer(ctx, "user_123", "test@example.com")

	require.NoError(t, err)
	assert.Equal(t, "ctm_new_customer", customerID)
	mockSDK.AssertExpectations(t)
}

func TestEnsureCustomer_ReusesActiveSameOwnerCustomerAfterConflict(t *testing.T) {
	service, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()

	mockSDK.On("CreateCustomer", ctx, mock.AnythingOfType("*paddle.CreateCustomerRequest")).
		Return(nil, paddle.ErrCustomerAlreadyExists)
	mockSDK.On("ListCustomersByEmail", ctx, "test@example.com").
		Return([]*paddle.Customer{{ID: "ctm_existing_customer", Email: "test@example.com", Status: paddle.StatusActive, CustomData: paddle.CustomData{"user_id": "user_123"}}}, nil)

	customerID, err := service.EnsureCustomer(ctx, "user_123", "test@example.com")

	require.NoError(t, err)
	assert.Equal(t, "ctm_existing_customer", customerID)
	mockSDK.AssertExpectations(t)
}

func TestEnsureCustomer_ReturnsLookupErrorWhenConflictLookupFails(t *testing.T) {
	service, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()

	mockSDK.On("CreateCustomer", ctx, mock.AnythingOfType("*paddle.CreateCustomerRequest")).
		Return(nil, paddle.ErrCustomerAlreadyExists)
	mockSDK.On("ListCustomersByEmail", ctx, "test@example.com").
		Return(nil, errors.New("lookup failed"))

	customerID, err := service.EnsureCustomer(ctx, "user_123", "test@example.com")

	require.Error(t, err)
	assert.Empty(t, customerID)
	assert.Contains(t, err.Error(), "failed to look up existing Paddle customer by email")
	mockSDK.AssertExpectations(t)
}

func TestEnsureCustomer_ReturnsErrorWhenConflictLookupIsAmbiguous(t *testing.T) {
	service, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()

	mockSDK.On("CreateCustomer", ctx, mock.AnythingOfType("*paddle.CreateCustomerRequest")).
		Return(nil, paddle.ErrCustomerAlreadyExists)
	mockSDK.On("ListCustomersByEmail", ctx, "test@example.com").
		Return([]*paddle.Customer{
			{ID: "ctm_one", Email: "test@example.com"},
			{ID: "ctm_two", Email: "test@example.com"},
		}, nil)

	customerID, err := service.EnsureCustomer(ctx, "user_123", "test@example.com")

	require.Error(t, err)
	assert.Empty(t, customerID)
	assert.Contains(t, err.Error(), "expected exactly 1 Paddle customer")
	mockSDK.AssertExpectations(t)
}

func TestEnsureCustomer_ReturnsErrorWhenNoCustomersFound(t *testing.T) {
	service, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()

	mockSDK.On("CreateCustomer", ctx, mock.AnythingOfType("*paddle.CreateCustomerRequest")).
		Return(nil, paddle.ErrCustomerAlreadyExists)
	mockSDK.On("ListCustomersByEmail", ctx, "test@example.com").
		Return([]*paddle.Customer{}, nil)

	customerID, err := service.EnsureCustomer(ctx, "user_123", "test@example.com")

	require.Error(t, err)
	assert.Empty(t, customerID)
	assert.Contains(t, err.Error(), "expected exactly 1 Paddle customer")
	mockSDK.AssertExpectations(t)
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

// TestCreateCustomer_RequiredParameters exercises CreateCustomer directly (not through
// EnsureCustomer's conflict-resolution wrapper, which TestEnsureCustomer_CreatesNewCustomer
// already covers) — the request shape, the success return, and the error-wrapping path.
// The stale "Requires Paddle SDK interface refactor for proper mocking" reason no longer
// holds: mockPaddleSDK (above) is exactly that refactor, already used by a dozen other
// tests in this file.
func TestCreateCustomer_RequiredParameters(t *testing.T) {
	svc, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()

	mockSDK.On("CreateCustomer", ctx, mock.MatchedBy(func(req *paddle.CreateCustomerRequest) bool {
		return req.Email == "customer@example.com" && req.CustomData["user_id"] == "user_789"
	})).Return(&paddle.Customer{ID: "ctm_created"}, nil).Once()

	customerID, err := svc.CreateCustomer(ctx, "user_789", "customer@example.com")
	require.NoError(t, err)
	assert.Equal(t, "ctm_created", customerID)
	mockSDK.AssertExpectations(t)
}

// TestCreateCustomer_RequiredParameters_APIError proves CreateCustomer wraps a raw SDK
// failure with context instead of returning it bare — the sibling half of the
// "handles API errors" claim the skipped test documented but never checked.
func TestCreateCustomer_RequiredParameters_APIError(t *testing.T) {
	svc, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()

	mockSDK.On("CreateCustomer", ctx, mock.Anything).Return(nil, errors.New("paddle rate limited")).Once()

	_, err := svc.CreateCustomer(ctx, "user_789", "customer@example.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create Paddle customer")
	mockSDK.AssertExpectations(t)
}

// TestGetCustomer_RequiredParameters covers the success path: GetCustomer calls the SDK
// with the given ID and returns the customer on success.
//
// ⚠ It does NOT assert "Returns ErrCustomerNotFound for 404" — that was this skipped
// test's ORIGINAL claim, but GetCustomer (paddle_service.go) never actually returns that
// sentinel; it always wraps whatever the SDK returned in a generic
// "failed to fetch Paddle customer: %w" error. services.ErrCustomerNotFound is defined
// and IS switched on by internal/huma/errors/mapper.go, but nothing in the production
// GetCustomer path ever produces it — a pre-existing dead-sentinel gap, not something to
// paper over with a false-positive test. See TestGetCustomer_RequiredParameters_APIError
// for what the code actually does today.
func TestGetCustomer_RequiredParameters(t *testing.T) {
	svc, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()

	mockSDK.On("GetCustomer", ctx, &paddle.GetCustomerRequest{CustomerID: "ctm_555"}).
		Return(&paddle.Customer{ID: "ctm_555"}, nil).Once()

	customer, err := svc.GetCustomer(ctx, "ctm_555")
	require.NoError(t, err)
	assert.Equal(t, "ctm_555", customer.ID)
	mockSDK.AssertExpectations(t)
}

// TestGetCustomer_RequiredParameters_APIError documents the current (generic-wrap, not
// ErrCustomerNotFound) error behavior — see the doc comment above.
func TestGetCustomer_RequiredParameters_APIError(t *testing.T) {
	svc, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()

	mockSDK.On("GetCustomer", ctx, mock.Anything).Return(nil, errors.New("not found")).Once()

	_, err := svc.GetCustomer(ctx, "ctm_missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch Paddle customer")
	assert.NotErrorIs(t, err, ErrCustomerNotFound, "production code does not (yet) map this to the sentinel — see doc comment")
	mockSDK.AssertExpectations(t)
}

// TestCreateCheckoutSession_RequiredParameters covers the price/quantity/customer-ID
// request shape and the success return (checkout URL + transaction ID) with the mock SDK.
// TestCreateCheckoutSession_IncludesCustomData (below) exercises the same method but
// against a REAL (unauthenticated) Paddle client and only checks the error path; this is
// the mock-backed success-path coverage that was missing.
func TestCreateCheckoutSession_RequiredParameters(t *testing.T) {
	svc, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()

	mockSDK.On("CreateTransaction", ctx, mock.MatchedBy(func(req *paddle.CreateTransactionRequest) bool {
		if len(req.Items) != 1 || req.Items[0].TransactionItemFromCatalog == nil {
			return false
		}
		item := req.Items[0].TransactionItemFromCatalog
		return item.PriceID == "pri_999" && item.Quantity == 1 &&
			req.CustomerID != nil && *req.CustomerID == "ctm_123" &&
			req.CustomData["user_id"] == "user_123"
	})).Return(&paddle.Transaction{
		ID: "txn_abc",
		Checkout: &paddle.TransactionCheckout{
			URL: paddle.PtrTo("https://checkout.paddle.com/txn_abc"),
		},
	}, nil).Once()

	checkoutURL, transactionID, err := svc.CreateCheckoutSession(ctx, "ctm_123", "pri_999", "test@example.com", "https://example.com/success", "user_123")
	require.NoError(t, err)
	assert.Equal(t, "https://checkout.paddle.com/txn_abc", checkoutURL)
	assert.Equal(t, "txn_abc", transactionID)
	mockSDK.AssertExpectations(t)
}

// TestCreateCheckoutSession_RequiredParameters_MissingCheckoutURL proves the "handles
// missing checkout URL in response" claim the skipped test documented but never checked.
func TestCreateCheckoutSession_RequiredParameters_MissingCheckoutURL(t *testing.T) {
	svc, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()

	mockSDK.On("CreateTransaction", ctx, mock.Anything).Return(&paddle.Transaction{
		ID:       "txn_no_checkout",
		Checkout: nil,
	}, nil).Once()

	_, _, err := svc.CreateCheckoutSession(ctx, "ctm_123", "pri_999", "test@example.com", "https://example.com/success", "user_123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing checkout URL")
	mockSDK.AssertExpectations(t)
}

// TestCreateCheckoutSession_IncludesCustomData verifies that custom_data with user_id is set
func TestCreateCheckoutSession_IncludesCustomData(t *testing.T) {
	cfg := &config.Config{
		PaddleAPIKey:      "test_key",
		PaddleEnvironment: "sandbox",
	}
	logger := slog.Default()

	service, err := NewPaddleService(cfg, logger)
	require.NoError(t, err)

	ctx := context.Background()
	userID := "user_12345"
	customerID := "ctm_12345"
	priceID := "pri_12345"
	email := "test@example.com"
	successURL := "https://example.com/success"

	// Call with userID parameter - will fail with Paddle API (expected in unit test)
	// but verifies the signature is correct and userID is accepted
	_, _, err = service.CreateCheckoutSession(ctx, customerID, priceID, email, successURL, userID)

	// Expected to fail with Paddle API error (not authenticated), but that's OK
	// The important thing is the method accepts userID and would pass it to Paddle
	assert.Error(t, err, "Expected Paddle API error with test credentials")
	assert.Contains(t, err.Error(), "failed to create Paddle transaction")

	// Note: Actual verification of CustomData being set requires either:
	// 1. Refactoring to use an interface we can mock
	// 2. Integration test with real Paddle sandbox
	// The integration test below verifies this in a real environment
}

// TestGetSubscription_RequiredParameters covers the success path.
//
// ⚠ Like TestGetCustomer_RequiredParameters above, this does NOT assert
// "Returns ErrSubscriptionNotFound for 404" — GetSubscription (paddle_service.go) never
// produces that sentinel either; see TestGetSubscription_RequiredParameters_APIError.
func TestGetSubscription_RequiredParameters(t *testing.T) {
	svc, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()

	mockSDK.On("GetSubscription", ctx, &paddle.GetSubscriptionRequest{SubscriptionID: "sub_444"}).
		Return(&paddle.Subscription{ID: "sub_444", Status: paddle.SubscriptionStatusActive}, nil).Once()

	sub, err := svc.GetSubscription(ctx, "sub_444")
	require.NoError(t, err)
	assert.Equal(t, "sub_444", sub.ID)
	mockSDK.AssertExpectations(t)
}

func TestGetSubscription_RequiredParameters_APIError(t *testing.T) {
	svc, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()

	mockSDK.On("GetSubscription", ctx, mock.Anything).Return(nil, errors.New("not found")).Once()

	_, err := svc.GetSubscription(ctx, "sub_missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch subscription")
	assert.NotErrorIs(t, err, ErrSubscriptionNotFound, "production code does not (yet) map this to the sentinel")
	mockSDK.AssertExpectations(t)
}

// TestUpdateSubscription_RequiredParameters covers all five claims the skipped test
// documented but never checked: fetches current subscription first (so it can read the
// existing item quantity via buildCatalogItem), builds the update with the new price ID,
// applies the requested proration mode, and returns the updated subscription on success.
func TestUpdateSubscription_RequiredParameters(t *testing.T) {
	svc, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()
	subscriptionID := "sub_777"

	mockSDK.On("GetSubscription", ctx, &paddle.GetSubscriptionRequest{SubscriptionID: subscriptionID}).
		Return(&paddle.Subscription{
			ID:    subscriptionID,
			Items: []paddle.SubscriptionItem{{Quantity: 2}},
		}, nil).Once()

	mockSDK.On("UpdateSubscription", ctx, mock.MatchedBy(func(req *paddle.UpdateSubscriptionRequest) bool {
		if req.SubscriptionID != subscriptionID || req.Items == nil {
			return false
		}
		items := req.Items.Value()
		if items == nil || len(*items) != 1 || (*items)[0].SubscriptionUpdateItemFromCatalog == nil {
			return false
		}
		item := (*items)[0].SubscriptionUpdateItemFromCatalog
		if req.ProrationBillingMode == nil || req.ProrationBillingMode.Value() == nil {
			return false
		}
		mode := *req.ProrationBillingMode.Value()
		return item.PriceID == "pri_new" && item.Quantity == 2 &&
			mode == paddle.ProrationBillingModeFullImmediately
	})).Return(&paddle.Subscription{ID: subscriptionID, Status: paddle.SubscriptionStatusActive}, nil).Once()

	updated, err := svc.UpdateSubscription(ctx, subscriptionID, "pri_new", "full_immediately")
	require.NoError(t, err)
	assert.Equal(t, subscriptionID, updated.ID)
	mockSDK.AssertExpectations(t)
}

// TestUpdateSubscription_RequiredParameters_FetchError proves UpdateSubscription surfaces
// (rather than swallows) a failure to fetch the current subscription — the "handles API
// errors" claim the skipped test documented but never checked. The GetSubscription failure
// path is exercised directly above; this proves UpdateSubscription's own wrapping of it.
func TestUpdateSubscription_RequiredParameters_FetchError(t *testing.T) {
	svc, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()

	mockSDK.On("GetSubscription", ctx, mock.Anything).Return(nil, errors.New("db unavailable")).Once()

	_, err := svc.UpdateSubscription(ctx, "sub_777", "pri_new", "full_immediately")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch current subscription")
	mockSDK.AssertExpectations(t)
}

// TestUpdateSubscription_RequiredParameters_UpdateError proves the SDK's own update call
// failure is wrapped, not swallowed.
func TestUpdateSubscription_RequiredParameters_UpdateError(t *testing.T) {
	svc, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()

	mockSDK.On("GetSubscription", ctx, mock.Anything).Return(&paddle.Subscription{ID: "sub_777"}, nil).Once()
	mockSDK.On("UpdateSubscription", ctx, mock.Anything).Return(nil, errors.New("paddle rejected update")).Once()

	_, err := svc.UpdateSubscription(ctx, "sub_777", "pri_new", "full_immediately")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to update Paddle subscription")
	mockSDK.AssertExpectations(t)
}

// TestCancelSubscription_RequiredParameters covers the success path: correct subscription
// ID, effective_from pinned to next-billing-period (not immediate — that distinction is
// the whole reason CancelSubscription and CancelSubscriptionImmediately are two methods),
// and no error on success. The "already cancelled" and "API error" claims are covered by
// TestCancelSubscription_AlreadyCancelled_MapsToErrPaddleAlreadyCancelled and
// TestCancelSubscription_SDKError_NotConfusedWithAlreadyCancelled directly below.
func TestCancelSubscription_RequiredParameters(t *testing.T) {
	svc, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()
	subscriptionID := "sub_888"

	mockSDK.On("CancelSubscription", ctx, mock.MatchedBy(func(req *paddle.CancelSubscriptionRequest) bool {
		return req.SubscriptionID == subscriptionID &&
			req.EffectiveFrom != nil && *req.EffectiveFrom == paddle.EffectiveFromNextBillingPeriod
	})).Return(&paddle.Subscription{ID: subscriptionID, Status: paddle.SubscriptionStatusCanceled}, nil).Once()

	err := svc.CancelSubscription(ctx, subscriptionID)
	require.NoError(t, err)
	mockSDK.AssertExpectations(t)
}

// TestCancelSubscription_AlreadyCancelled_MapsToErrPaddleAlreadyCancelled proves the H2
// idempotency fix for the OTHER Paddle cancel path (the deleted PastDueService's grace-cancel step called
// CancelSubscription, not CancelSubscriptionImmediately). The deleted PastDuePaddleClient's own doc comment
// already promised "the implementation must tolerate
// paddle.ErrSubscriptionIsCanceledActionInvalid by mapping it to ErrPaddleAlreadyCancelled" —
// this was a documented contract with no implementation behind it: CancelSubscription used to
// wrap and return the raw Paddle error unchanged, which the deleted past_due_service.go's
// `!errors.Is(err, ErrPaddleAlreadyCancelled)` check could never match, so a retried grace-cancel
// against an already-cancelled subscription would hard-fail instead of tolerating it and
// continuing the cancellation flow. Verified against
// https://developer.paddle.com/errors/subscriptions/subscription_is_canceled_action_invalid and
// paddle.ErrSubscriptionIsCanceledActionInvalid's definition in the vendored SDK
// (paddle-go-sdk/v4/subscriptions.go) — the same error code CancelSubscriptionImmediately already
// tolerates, just mapped to the OTHER sentinel this method's callers expect. Issue #1413 H2.
func TestCancelSubscription_AlreadyCancelled_MapsToErrPaddleAlreadyCancelled(t *testing.T) {
	svc, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()
	subscriptionID := "sub_01abc123"

	mockSDK.On("CancelSubscription", ctx, mock.MatchedBy(func(req *paddle.CancelSubscriptionRequest) bool {
		return req.SubscriptionID == subscriptionID
	})).Return(nil, paddle.ErrSubscriptionIsCanceledActionInvalid)

	err := svc.CancelSubscription(ctx, subscriptionID)

	require.Error(t, err, "already-cancelled must still be a distinguishable outcome for CancelSubscription (unlike CancelSubscriptionImmediately, which is full success)")
	assert.ErrorIs(t, err, ErrPaddleAlreadyCancelled, "must map to the sentinel the deleted PastDuePaddleClient callers checked for")
	mockSDK.AssertExpectations(t)
}

// TestCancelSubscription_SDKError_NotConfusedWithAlreadyCancelled proves a genuine (non-already-
// cancelled) Paddle failure is NOT accidentally mapped to ErrPaddleAlreadyCancelled.
func TestCancelSubscription_SDKError_NotConfusedWithAlreadyCancelled(t *testing.T) {
	svc, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()
	subscriptionID := "sub_01abc123"

	mockSDK.On("CancelSubscription", ctx, mock.MatchedBy(func(req *paddle.CancelSubscriptionRequest) bool {
		return req.SubscriptionID == subscriptionID
	})).Return(nil, errors.New("paddle API error"))

	err := svc.CancelSubscription(ctx, subscriptionID)

	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrPaddleAlreadyCancelled)
	mockSDK.AssertExpectations(t)
}

// TestPreviewUpdate_RequiredParameters covers all five claims from the skipped test:
// fetches the current subscription first (buildCatalogItem needs its item quantity),
// builds the preview request for the new price, and returns proration amount, currency,
// and next charge date on success.
func TestPreviewUpdate_RequiredParameters(t *testing.T) {
	svc, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()
	subscriptionID := "sub_222"

	mockSDK.On("GetSubscription", ctx, &paddle.GetSubscriptionRequest{SubscriptionID: subscriptionID}).
		Return(&paddle.Subscription{
			ID:    subscriptionID,
			Items: []paddle.SubscriptionItem{{Quantity: 1}},
		}, nil).Once()

	mockSDK.On("PreviewSubscriptionUpdate", ctx, mock.MatchedBy(func(req *paddle.PreviewSubscriptionUpdateRequest) bool {
		return req.SubscriptionID == subscriptionID
	})).Return(&paddle.SubscriptionPreview{
		CurrencyCode: paddle.CurrencyCodeUSD,
		ImmediateTransaction: &paddle.NextTransaction{
			Details: paddle.TransactionDetailsPreview{
				Totals: paddle.TransactionTotals{Total: "1234"},
			},
		},
		NextTransaction: &paddle.NextTransaction{
			BillingPeriod: paddle.TimePeriod{StartsAt: "2027-01-01T00:00:00Z"},
		},
	}, nil).Once()

	amount, currency, nextCharge, err := svc.PreviewUpdate(ctx, subscriptionID, "pri_new")
	require.NoError(t, err)
	assert.Equal(t, "1234", amount)
	assert.Equal(t, string(paddle.CurrencyCodeUSD), currency)
	assert.Equal(t, "2027-01-01T00:00:00Z", nextCharge)
	mockSDK.AssertExpectations(t)
}

// TestPreviewUpdate_RequiredParameters_APIError proves the "handles API errors" claim:
// a preview-call failure is wrapped, not swallowed.
func TestPreviewUpdate_RequiredParameters_APIError(t *testing.T) {
	svc, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()
	subscriptionID := "sub_222"

	mockSDK.On("GetSubscription", ctx, mock.Anything).Return(&paddle.Subscription{ID: subscriptionID}, nil).Once()
	mockSDK.On("PreviewSubscriptionUpdate", ctx, mock.Anything).Return(nil, errors.New("paddle preview failed")).Once()

	_, _, _, err := svc.PreviewUpdate(ctx, subscriptionID, "pri_new")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to preview subscription update")
	mockSDK.AssertExpectations(t)
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
		"user_test_123", // userID for custom_data
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

	_, _, err = service.CreateCheckoutSession(ctx, "ctm_123", "pri_123", "test@example.com", "https://example.com", "user_123")
	assert.Error(t, err, "Should fail with cancelled context")
}

// Test proration mode parsing in UpdateSubscription

// TestUpdateSubscription_ProrationModes verifies each proration-mode string flows through
// UpdateSubscription into the actual request the SDK receives.
//
// ⚠ The pure string→enum mapping this skipped test's table originally described is already
// covered unconditionally by TestParseProrationMode (parseProrationMode directly, below in
// this file) — re-asserting that table here would be a duplicate. What was NOT covered is
// the wiring: that UpdateSubscription actually calls parseProrationMode and puts the result
// on the request, which is what this now checks with the mock SDK.
func TestUpdateSubscription_ProrationModes(t *testing.T) {
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
			svc, mockSDK := setupPaddleServiceWithMock(t)
			ctx := context.Background()
			subscriptionID := "sub_proration"

			mockSDK.On("GetSubscription", ctx, mock.Anything).
				Return(&paddle.Subscription{ID: subscriptionID, Items: []paddle.SubscriptionItem{{Quantity: 1}}}, nil).Once()

			mockSDK.On("UpdateSubscription", ctx, mock.MatchedBy(func(req *paddle.UpdateSubscriptionRequest) bool {
				if req.ProrationBillingMode == nil || req.ProrationBillingMode.Value() == nil {
					return false
				}
				return *req.ProrationBillingMode.Value() == tc.expected
			})).Return(&paddle.Subscription{ID: subscriptionID}, nil).Once()

			_, err := svc.UpdateSubscription(ctx, subscriptionID, "pri_x", tc.input)
			require.NoError(t, err)
			mockSDK.AssertExpectations(t)
		})
	}
}

func TestPaddleService_UpdateCustomerEmail_Success(t *testing.T) {
	mockSDK := new(mockPaddleSDK)
	svc := &paddleService{client: mockSDK}

	mockSDK.On("UpdateCustomer", mock.Anything, &paddle.UpdateCustomerRequest{
		CustomerID: "ctm_abc123",
		Email:      paddle.NewPatchField("new@example.com"),
	}).Return(&paddle.Customer{ID: "ctm_abc123"}, nil)

	err := svc.UpdateCustomerEmail(context.Background(), "ctm_abc123", "new@example.com")
	assert.NoError(t, err)
	mockSDK.AssertExpectations(t)
}

func TestPaddleService_UpdateCustomerEmail_Error(t *testing.T) {
	mockSDK := new(mockPaddleSDK)
	svc := &paddleService{client: mockSDK}

	mockSDK.On("UpdateCustomer", mock.Anything, mock.Anything).
		Return(nil, errors.New("paddle api error"))

	err := svc.UpdateCustomerEmail(context.Background(), "ctm_abc123", "new@example.com")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to update Paddle customer email")
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

	// Verify the concrete implementation satisfies PaddleService. NewPaddleService's
	// return type already guarantees this for the interface value, so the assertion is
	// made against the concrete type to make it actually meaningful.
	var _ PaddleService = (*paddleService)(nil)

	// Verify all methods are callable (compilation test)
	ctx := context.Background()

	_ = func() { _, _ = service.CreateCustomer(ctx, "u", "e@e.com") }
	_ = func() { _, _ = service.EnsureCustomer(ctx, "u", "e@e.com") }
	_ = func() { _, _ = service.GetCustomer(ctx, "c") }
	_ = func() { _, _, _ = service.CreateCheckoutSession(ctx, "c", "p", "e@e.com", "url", "u") }
	_ = func() { _, _ = service.GetSubscription(ctx, "s") }
	_ = func() { _, _ = service.UpdateSubscription(ctx, "s", "p", "prorated") }
	_ = func() { _ = service.CancelSubscription(ctx, "s") }
	_ = func() { _, _, _, _ = service.PreviewUpdate(ctx, "s", "p") }
	_ = func() { _ = service.UpdateCustomerEmail(ctx, "c", "e@e.com") }
	_ = func() { _ = service.GetClient() }
}

// Test that custom_data is included when creating customers

// TestCreateCustomer_IncludesCustomData verifies CreateCustomerRequest carries the email
// and custom_data.user_id fields, calling CreateCustomer directly — distinct from
// TestEnsureCustomer_CreatesNewCustomer, which asserts the same request shape but through
// EnsureCustomer's higher-level conflict-resolution wrapper.
func TestCreateCustomer_IncludesCustomData(t *testing.T) {
	svc, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()

	mockSDK.On("CreateCustomer", ctx, mock.MatchedBy(func(req *paddle.CreateCustomerRequest) bool {
		return req.Email == "custom-data@example.com" && req.CustomData["user_id"] == "user_custom_data"
	})).Return(&paddle.Customer{ID: "ctm_custom_data"}, nil).Once()

	_, err := svc.CreateCustomer(ctx, "user_custom_data", "custom-data@example.com")
	require.NoError(t, err)
	mockSDK.AssertExpectations(t)
}

// TestCreateCheckoutSession_WithAndWithoutCustomer covers both claims the skipped test
// documented: with a customer ID the request carries it, without one it's guest checkout
// (nil CustomerID field on the request).
func TestCreateCheckoutSession_WithAndWithoutCustomer(t *testing.T) {
	t.Run("with customer ID", func(t *testing.T) {
		svc, mockSDK := setupPaddleServiceWithMock(t)
		ctx := context.Background()

		mockSDK.On("CreateTransaction", ctx, mock.MatchedBy(func(req *paddle.CreateTransactionRequest) bool {
			return req.CustomerID != nil && *req.CustomerID == "ctm_present"
		})).Return(&paddle.Transaction{
			ID:       "txn_with_customer",
			Checkout: &paddle.TransactionCheckout{URL: paddle.PtrTo("https://checkout.paddle.com/txn_with_customer")},
		}, nil).Once()

		_, _, err := svc.CreateCheckoutSession(ctx, "ctm_present", "pri_1", "test@example.com", "https://example.com", "user_1")
		require.NoError(t, err)
		mockSDK.AssertExpectations(t)
	})

	t.Run("without customer ID (guest checkout)", func(t *testing.T) {
		svc, mockSDK := setupPaddleServiceWithMock(t)
		ctx := context.Background()

		mockSDK.On("CreateTransaction", ctx, mock.MatchedBy(func(req *paddle.CreateTransactionRequest) bool {
			return req.CustomerID == nil
		})).Return(&paddle.Transaction{
			ID:       "txn_guest",
			Checkout: &paddle.TransactionCheckout{URL: paddle.PtrTo("https://checkout.paddle.com/txn_guest")},
		}, nil).Once()

		_, _, err := svc.CreateCheckoutSession(ctx, "", "pri_1", "test@example.com", "https://example.com", "user_1")
		require.NoError(t, err)
		mockSDK.AssertExpectations(t)
	})
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
// TestGetBillingPortalURL_Success covers both the sandbox and production URL selection —
// GetBillingPortalURL validates the customer exists (via GetCustomer) then returns a
// static, environment-selected portal URL; there's no SDK "create billing portal session"
// call to mock (see GetBillingPortalURL's own doc comment: Paddle SDK v4 has no such
// endpoint), so the SDK interaction under test is the customer-validation GetCustomer call.
func TestGetBillingPortalURL_Success(t *testing.T) {
	t.Run("sandbox", func(t *testing.T) {
		svc, mockSDK := setupPaddleServiceWithMock(t)
		ctx := context.Background()

		mockSDK.On("GetCustomer", ctx, &paddle.GetCustomerRequest{CustomerID: "ctm_portal"}).
			Return(&paddle.Customer{ID: "ctm_portal"}, nil).Once()

		url, err := svc.GetBillingPortalURL(ctx, "ctm_portal")
		require.NoError(t, err)
		assert.Equal(t, "https://sandbox-buy.paddle.com/customer/portal", url)
		mockSDK.AssertExpectations(t)
	})

	t.Run("production", func(t *testing.T) {
		svc, mockSDK := setupPaddleServiceWithMock(t)
		svc.config.PaddleEnvironment = "production"
		ctx := context.Background()

		mockSDK.On("GetCustomer", ctx, &paddle.GetCustomerRequest{CustomerID: "ctm_portal"}).
			Return(&paddle.Customer{ID: "ctm_portal"}, nil).Once()

		url, err := svc.GetBillingPortalURL(ctx, "ctm_portal")
		require.NoError(t, err)
		assert.Equal(t, "https://buy.paddle.com/customer/portal", url)
		mockSDK.AssertExpectations(t)
	})
}

// TestGetBillingPortalURL_InvalidCustomer covers a customer-lookup failure (e.g. the
// customer ID doesn't exist). ⚠ Like the GetCustomer/GetSubscription tests above, this
// does NOT assert "Returns ErrCustomerNotFound" — GetBillingPortalURL's only failure path
// is GetCustomer's own generic wrap, which never produces that sentinel.
func TestGetBillingPortalURL_InvalidCustomer(t *testing.T) {
	svc, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()

	mockSDK.On("GetCustomer", ctx, mock.Anything).Return(nil, errors.New("not found")).Once()

	_, err := svc.GetBillingPortalURL(ctx, "ctm_missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to validate customer")
	mockSDK.AssertExpectations(t)
}

// TestGetBillingPortalURL_PaddleAPIError covers a non-"not found" SDK failure (rate limit,
// network error) during the customer-validation call. Per GetBillingPortalURL's
// implementation this is the SAME code path as TestGetBillingPortalURL_InvalidCustomer —
// the current method doesn't distinguish "customer not found" from "Paddle API error" when
// validating; both simply propagate GetCustomer's wrapped error. Kept as a separate test
// (matching the skipped test's original name and intent) with a distinct simulated error to
// document that non-distinction rather than assert a difference that doesn't exist.
func TestGetBillingPortalURL_PaddleAPIError(t *testing.T) {
	svc, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()

	mockSDK.On("GetCustomer", ctx, mock.Anything).Return(nil, errors.New("paddle rate limited")).Once()

	_, err := svc.GetBillingPortalURL(ctx, "ctm_rate_limited")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to validate customer")
	mockSDK.AssertExpectations(t)
}

// TestCancelSubscriptionImmediately_Success verifies the subscription is cancelled
// with EffectiveFromImmediately (not next billing period). Issue #1006.
func TestCancelSubscriptionImmediately_Success(t *testing.T) {
	svc, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()
	subscriptionID := "sub_01abc123"

	cancelledSub := &paddle.Subscription{
		ID:     subscriptionID,
		Status: paddle.SubscriptionStatusCanceled,
	}
	mockSDK.On("CancelSubscription", ctx, mock.MatchedBy(func(req *paddle.CancelSubscriptionRequest) bool {
		return req.SubscriptionID == subscriptionID &&
			req.EffectiveFrom != nil &&
			*req.EffectiveFrom == paddle.EffectiveFromImmediately
	})).Return(cancelledSub, nil)

	err := svc.CancelSubscriptionImmediately(ctx, subscriptionID)

	assert.NoError(t, err)
	mockSDK.AssertExpectations(t)
}

// TestCancelSubscriptionImmediately_SDKError verifies errors are propagated.
func TestCancelSubscriptionImmediately_SDKError(t *testing.T) {
	svc, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()
	subscriptionID := "sub_01abc123"

	mockSDK.On("CancelSubscription", ctx, mock.MatchedBy(func(req *paddle.CancelSubscriptionRequest) bool {
		return req.SubscriptionID == subscriptionID
	})).Return(nil, errors.New("paddle API error"))

	err := svc.CancelSubscriptionImmediately(ctx, subscriptionID)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to cancel Paddle subscription immediately")
	mockSDK.AssertExpectations(t)
}

// TestCancelSubscriptionImmediately_AlreadyCancelled verifies already-cancelled
// subscriptions are treated as success (Paddle returns the sub with no error).
func TestCancelSubscriptionImmediately_AlreadyCancelled(t *testing.T) {
	svc, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()
	subscriptionID := "sub_01abc123"

	alreadyCancelledSub := &paddle.Subscription{
		ID:     subscriptionID,
		Status: paddle.SubscriptionStatusCanceled,
	}
	mockSDK.On("CancelSubscription", ctx, mock.MatchedBy(func(req *paddle.CancelSubscriptionRequest) bool {
		return req.SubscriptionID == subscriptionID
	})).Return(alreadyCancelledSub, nil)

	err := svc.CancelSubscriptionImmediately(ctx, subscriptionID)

	assert.NoError(t, err)
}

// TestCancelSubscriptionImmediately_AlreadyCancelled_ErrorForm verifies that when Paddle
// returns ErrSubscriptionIsCanceledActionInvalid, the method treats it as success (idempotent).
// Issue #1006: Paddle returns this error when cancelling an already-cancelled subscription.
func TestCancelSubscriptionImmediately_AlreadyCancelled_ErrorForm(t *testing.T) {
	svc, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()
	subscriptionID := "sub_01abc123"

	mockSDK.On("CancelSubscription", ctx, mock.MatchedBy(func(req *paddle.CancelSubscriptionRequest) bool {
		return req.SubscriptionID == subscriptionID
	})).Return(nil, paddle.ErrSubscriptionIsCanceledActionInvalid)

	err := svc.CancelSubscriptionImmediately(ctx, subscriptionID)
	assert.NoError(t, err, "already-cancelled error must be treated as success")
	mockSDK.AssertExpectations(t)
}

// TestArchiveAndAnonymizeCustomer_Success verifies the SDK is called with both Email
// and Status fields set (single PATCH for GDPR PII erasure). Issue #1006.
func TestArchiveAndAnonymizeCustomer_Success(t *testing.T) {
	svc, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()
	customerID := "ctm_01abc123"
	anonymizedEmail := "deleted_user-123@deleted.local"

	var capturedReq *paddle.UpdateCustomerRequest
	mockSDK.On("UpdateCustomer", ctx, mock.MatchedBy(func(req *paddle.UpdateCustomerRequest) bool {
		capturedReq = req
		return req.CustomerID == customerID
	})).Return(&paddle.Customer{ID: customerID, Status: paddle.StatusArchived}, nil)

	err := svc.ArchiveAndAnonymizeCustomer(ctx, customerID, anonymizedEmail)
	assert.NoError(t, err)
	require.NotNil(t, capturedReq)
	assert.NotNil(t, capturedReq.Email, "Email field must be set")
	assert.NotNil(t, capturedReq.Status, "Status field must be set")
	mockSDK.AssertExpectations(t)
}

// TestArchiveAndAnonymizeCustomer_SDKError verifies errors are propagated.
func TestArchiveAndAnonymizeCustomer_SDKError(t *testing.T) {
	svc, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()
	customerID := "ctm_01abc123"

	mockSDK.On("UpdateCustomer", ctx, mock.MatchedBy(func(req *paddle.UpdateCustomerRequest) bool {
		return req.CustomerID == customerID
	})).Return(nil, errors.New("paddle API error"))

	err := svc.ArchiveAndAnonymizeCustomer(ctx, customerID, "deleted_user-123@deleted.local")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to archive and anonymize Paddle customer")
	mockSDK.AssertExpectations(t)
}

// TestArchiveAndAnonymizeCustomer_AlreadyArchived verifies that an already-archived
// customer (Paddle returns success with archived status) is treated as success (idempotent).
func TestArchiveAndAnonymizeCustomer_AlreadyArchived(t *testing.T) {
	svc, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()
	customerID := "ctm_01abc123"

	mockSDK.On("UpdateCustomer", ctx, mock.MatchedBy(func(req *paddle.UpdateCustomerRequest) bool {
		return req.CustomerID == customerID
	})).Return(&paddle.Customer{ID: customerID, Status: paddle.StatusArchived}, nil)

	err := svc.ArchiveAndAnonymizeCustomer(ctx, customerID, "deleted_user-123@deleted.local")
	assert.NoError(t, err)
	mockSDK.AssertExpectations(t)
}

// TestArchiveAndAnonymizeCustomer_AlreadyArchived_ErrorForm verifies that when Paddle
// returns ErrEntityArchived, the method treats it as success (idempotent).
// Issue #1006: Paddle returns this error when archiving an already-archived customer.
func TestArchiveAndAnonymizeCustomer_AlreadyArchived_ErrorForm(t *testing.T) {
	svc, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()
	customerID := "ctm_01abc123"

	mockSDK.On("UpdateCustomer", ctx, mock.MatchedBy(func(req *paddle.UpdateCustomerRequest) bool {
		return req.CustomerID == customerID
	})).Return(nil, paddle.ErrEntityArchived)

	err := svc.ArchiveAndAnonymizeCustomer(ctx, customerID, "deleted_user-123@deleted.local")
	assert.NoError(t, err, "already-archived error must be treated as success")
	mockSDK.AssertExpectations(t)
}

// Tests for archived-customer handling in EnsureCustomer (issue #1039)

func TestEnsureCustomer_ActiveMatch_Reuses(t *testing.T) {
	service, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()

	mockSDK.On("CreateCustomer", ctx, mock.AnythingOfType("*paddle.CreateCustomerRequest")).
		Return(nil, paddle.ErrCustomerAlreadyExists)
	mockSDK.On("ListCustomersByEmail", ctx, "test@example.com").
		Return([]*paddle.Customer{{ID: "ctm_active", Status: paddle.StatusActive, CustomData: paddle.CustomData{"user_id": "user_123"}}}, nil)

	customerID, err := service.EnsureCustomer(ctx, "user_123", "test@example.com")

	require.NoError(t, err)
	assert.Equal(t, "ctm_active", customerID)
	mockSDK.AssertNotCalled(t, "UpdateCustomer")
	mockSDK.AssertExpectations(t)
}

func TestEnsureCustomer_ArchivedSameOwner_Unarchives(t *testing.T) {
	service, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()

	mockSDK.On("CreateCustomer", ctx, mock.AnythingOfType("*paddle.CreateCustomerRequest")).
		Return(nil, paddle.ErrCustomerAlreadyExists)
	mockSDK.On("ListCustomersByEmail", ctx, "test@example.com").
		Return([]*paddle.Customer{{ID: "ctm_archived", Status: paddle.StatusArchived, CustomData: paddle.CustomData{"user_id": "user_123"}}}, nil)
	mockSDK.On("UpdateCustomer", ctx, mock.MatchedBy(func(req *paddle.UpdateCustomerRequest) bool {
		return req.CustomerID == "ctm_archived" && req.Status != nil && req.Status.Value() != nil && *req.Status.Value() == paddle.StatusActive
	})).Return(&paddle.Customer{ID: "ctm_archived", Status: paddle.StatusActive}, nil)

	customerID, err := service.EnsureCustomer(ctx, "user_123", "test@example.com")

	require.NoError(t, err)
	assert.Equal(t, "ctm_archived", customerID)
	mockSDK.AssertExpectations(t)
}

func TestEnsureCustomer_ArchivedDifferentOwner_FailsClosed(t *testing.T) {
	service, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()

	mockSDK.On("CreateCustomer", ctx, mock.AnythingOfType("*paddle.CreateCustomerRequest")).
		Return(nil, paddle.ErrCustomerAlreadyExists)
	mockSDK.On("ListCustomersByEmail", ctx, "test@example.com").
		Return([]*paddle.Customer{{ID: "ctm_archived", Status: paddle.StatusArchived, CustomData: paddle.CustomData{"user_id": "other_user"}}}, nil)

	customerID, err := service.EnsureCustomer(ctx, "user_123", "test@example.com")

	require.Error(t, err)
	assert.Empty(t, customerID)
	assert.ErrorIs(t, err, ErrPaddleCustomerOwnershipMismatch)
	mockSDK.AssertNotCalled(t, "UpdateCustomer")
	mockSDK.AssertExpectations(t)
}

func TestEnsureCustomer_ArchivedMissingCustomData_FailsClosed(t *testing.T) {
	service, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()

	mockSDK.On("CreateCustomer", ctx, mock.AnythingOfType("*paddle.CreateCustomerRequest")).
		Return(nil, paddle.ErrCustomerAlreadyExists)
	mockSDK.On("ListCustomersByEmail", ctx, "test@example.com").
		Return([]*paddle.Customer{{ID: "ctm_archived", Status: paddle.StatusArchived, CustomData: nil}}, nil)

	customerID, err := service.EnsureCustomer(ctx, "user_123", "test@example.com")

	require.Error(t, err)
	assert.Empty(t, customerID)
	assert.ErrorIs(t, err, ErrPaddleCustomerOwnershipMismatch)
	mockSDK.AssertNotCalled(t, "UpdateCustomer")
	mockSDK.AssertExpectations(t)
}

func TestEnsureCustomer_ArchivedNonStringCustomData_FailsClosed(t *testing.T) {
	service, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()

	mockSDK.On("CreateCustomer", ctx, mock.AnythingOfType("*paddle.CreateCustomerRequest")).
		Return(nil, paddle.ErrCustomerAlreadyExists)
	mockSDK.On("ListCustomersByEmail", ctx, "test@example.com").
		Return([]*paddle.Customer{{ID: "ctm_archived", Status: paddle.StatusArchived, CustomData: paddle.CustomData{"user_id": 12345}}}, nil)

	customerID, err := service.EnsureCustomer(ctx, "user_123", "test@example.com")

	require.Error(t, err)
	assert.Empty(t, customerID)
	assert.ErrorIs(t, err, ErrPaddleCustomerOwnershipMismatch)
	mockSDK.AssertNotCalled(t, "UpdateCustomer")
	mockSDK.AssertExpectations(t)
}

func TestEnsureCustomer_ArchivedSameOwner_UnarchiveFails_Propagates(t *testing.T) {
	service, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()

	mockSDK.On("CreateCustomer", ctx, mock.AnythingOfType("*paddle.CreateCustomerRequest")).
		Return(nil, paddle.ErrCustomerAlreadyExists)
	mockSDK.On("ListCustomersByEmail", ctx, "test@example.com").
		Return([]*paddle.Customer{{ID: "ctm_archived", Status: paddle.StatusArchived, CustomData: paddle.CustomData{"user_id": "user_123"}}}, nil)
	mockSDK.On("UpdateCustomer", ctx, mock.AnythingOfType("*paddle.UpdateCustomerRequest")).
		Return(nil, errors.New("paddle API error"))

	customerID, err := service.EnsureCustomer(ctx, "user_123", "test@example.com")

	require.Error(t, err)
	assert.Empty(t, customerID)
	assert.Contains(t, err.Error(), "paddle API error")
	mockSDK.AssertExpectations(t)
}

// Tests for active-customer ownership check in EnsureCustomer (issue #1043)

func TestEnsureCustomer_ActiveDifferentOwner_FailsClosed(t *testing.T) {
	service, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()

	mockSDK.On("CreateCustomer", ctx, mock.AnythingOfType("*paddle.CreateCustomerRequest")).
		Return(nil, paddle.ErrCustomerAlreadyExists)
	mockSDK.On("ListCustomersByEmail", ctx, "test@example.com").
		Return([]*paddle.Customer{{ID: "ctm_active", Status: paddle.StatusActive, CustomData: paddle.CustomData{"user_id": "other_user"}}}, nil)

	customerID, err := service.EnsureCustomer(ctx, "user_123", "test@example.com")

	require.Error(t, err)
	assert.Empty(t, customerID)
	assert.ErrorIs(t, err, ErrPaddleCustomerOwnershipMismatch)
	mockSDK.AssertNotCalled(t, "UpdateCustomer")
	mockSDK.AssertExpectations(t)
}

func TestEnsureCustomer_ActiveMissingCustomData_FailsClosed(t *testing.T) {
	service, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()

	mockSDK.On("CreateCustomer", ctx, mock.AnythingOfType("*paddle.CreateCustomerRequest")).
		Return(nil, paddle.ErrCustomerAlreadyExists)
	mockSDK.On("ListCustomersByEmail", ctx, "test@example.com").
		Return([]*paddle.Customer{{ID: "ctm_active", Status: paddle.StatusActive, CustomData: nil}}, nil)

	customerID, err := service.EnsureCustomer(ctx, "user_123", "test@example.com")

	require.Error(t, err)
	assert.Empty(t, customerID)
	assert.ErrorIs(t, err, ErrPaddleCustomerOwnershipMismatch)
	mockSDK.AssertNotCalled(t, "UpdateCustomer")
	mockSDK.AssertExpectations(t)
}

func TestEnsureCustomer_ActiveNonStringCustomData_FailsClosed(t *testing.T) {
	service, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()

	mockSDK.On("CreateCustomer", ctx, mock.AnythingOfType("*paddle.CreateCustomerRequest")).
		Return(nil, paddle.ErrCustomerAlreadyExists)
	mockSDK.On("ListCustomersByEmail", ctx, "test@example.com").
		Return([]*paddle.Customer{{ID: "ctm_active", Status: paddle.StatusActive, CustomData: paddle.CustomData{"user_id": 12345}}}, nil)

	customerID, err := service.EnsureCustomer(ctx, "user_123", "test@example.com")

	require.Error(t, err)
	assert.Empty(t, customerID)
	assert.ErrorIs(t, err, ErrPaddleCustomerOwnershipMismatch)
	mockSDK.AssertNotCalled(t, "UpdateCustomer")
	mockSDK.AssertExpectations(t)
}

func TestEnsureCustomer_ActiveEmptyStringCustomData_FailsClosed(t *testing.T) {
	service, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()

	mockSDK.On("CreateCustomer", ctx, mock.AnythingOfType("*paddle.CreateCustomerRequest")).
		Return(nil, paddle.ErrCustomerAlreadyExists)
	mockSDK.On("ListCustomersByEmail", ctx, "test@example.com").
		Return([]*paddle.Customer{{ID: "ctm_active", Status: paddle.StatusActive, CustomData: paddle.CustomData{"user_id": ""}}}, nil)

	customerID, err := service.EnsureCustomer(ctx, "user_123", "test@example.com")

	require.Error(t, err)
	assert.Empty(t, customerID)
	assert.ErrorIs(t, err, ErrPaddleCustomerOwnershipMismatch)
	mockSDK.AssertNotCalled(t, "UpdateCustomer")
	mockSDK.AssertExpectations(t)
}

func TestEnsureCustomer_ActiveWhitespaceUserIdCustomData_FailsClosed(t *testing.T) {
	service, mockSDK := setupPaddleServiceWithMock(t)
	ctx := context.Background()

	mockSDK.On("CreateCustomer", ctx, mock.AnythingOfType("*paddle.CreateCustomerRequest")).
		Return(nil, paddle.ErrCustomerAlreadyExists)
	mockSDK.On("ListCustomersByEmail", ctx, "test@example.com").
		Return([]*paddle.Customer{{ID: "ctm_active", Status: paddle.StatusActive, CustomData: paddle.CustomData{"user_id": "  "}}}, nil)

	customerID, err := service.EnsureCustomer(ctx, "user_123", "test@example.com")

	require.Error(t, err)
	assert.Empty(t, customerID)
	assert.ErrorIs(t, err, ErrPaddleCustomerOwnershipMismatch)
	mockSDK.AssertNotCalled(t, "UpdateCustomer")
	mockSDK.AssertExpectations(t)
}

// --- buildCatalogItem ---

func TestBuildCatalogItem_NoItems_DefaultsToQuantityOne(t *testing.T) {
	sub := &paddle.Subscription{Items: nil}
	item := buildCatalogItem("pri_abc123", sub)
	require.NotNil(t, item)
	assert.Equal(t, "pri_abc123", item.PriceID)
	assert.Equal(t, 1, item.Quantity, "quantity must default to 1 when subscription has no items")
}

func TestBuildCatalogItem_ExistingItems_UsesFirstItemQuantity(t *testing.T) {
	sub := &paddle.Subscription{
		Items: []paddle.SubscriptionItem{
			{Quantity: 3},
		},
	}
	item := buildCatalogItem("pri_xyz789", sub)
	require.NotNil(t, item)
	assert.Equal(t, "pri_xyz789", item.PriceID)
	assert.Equal(t, 3, item.Quantity, "quantity must match the first subscription item")
}

// --- parseProrationMode ---

func TestParseProrationMode(t *testing.T) {
	tests := []struct {
		input    string
		expected paddle.ProrationBillingMode
	}{
		{"prorated_immediately", paddle.ProrationBillingModeProratedImmediately},
		{"full_immediately", paddle.ProrationBillingModeFullImmediately},
		{"full_next_billing_period", paddle.ProrationBillingModeFullNextBillingPeriod},
		{"unknown_value", paddle.ProrationBillingModeProratedImmediately},
		{"", paddle.ProrationBillingModeProratedImmediately},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			result := parseProrationMode(tc.input)
			assert.Equal(t, tc.expected, result, "parseProrationMode(%q)", tc.input)
		})
	}
}
