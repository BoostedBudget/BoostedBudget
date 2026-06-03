package operations

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockPaddleTransactionRepository mocks PaddleTransactionRepository for admin endpoint tests.
type MockPaddleTransactionRepository struct {
	mock.Mock
}

func (m *MockPaddleTransactionRepository) Create(ctx context.Context, txn *domain.PaddleTransaction) error {
	return m.Called(ctx, txn).Error(0)
}

func (m *MockPaddleTransactionRepository) GetByTransactionID(ctx context.Context, transactionID string) (*domain.PaddleTransaction, error) {
	args := m.Called(ctx, transactionID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.PaddleTransaction), args.Error(1)
}

func (m *MockPaddleTransactionRepository) GetByUserID(ctx context.Context, userID string) ([]domain.PaddleTransaction, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.PaddleTransaction), args.Error(1)
}

func (m *MockPaddleTransactionRepository) GetFailedTransactions(ctx context.Context, limit, offset int) ([]domain.PaddleTransaction, int, error) {
	args := m.Called(ctx, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Int(1), args.Error(2)
	}
	return args.Get(0).([]domain.PaddleTransaction), args.Int(1), args.Error(2)
}

func setupAdminPaymentFailuresAPI(t *testing.T) (humatest.TestAPI, *MockPaddleTransactionRepository, *MockAdminService) {
	t.Helper()
	_, api := humatest.New(t)
	mockTxnRepo := new(MockPaddleTransactionRepository)
	mockAdminSvc := new(MockAdminService)

	cfg := &config.Config{Environment: "qa"}
	handler := NewAdminHandler(nil, nil, nil, mockAdminSvc, nil, nil, nil, cfg).
		WithPaddleTransactionRepo(mockTxnRepo)
	RegisterAdminPaymentFailuresOperations(api, handler)
	return api, mockTxnRepo, mockAdminSvc
}

// TestGetPaymentFailures_OperationRegistered verifies the endpoint is registered (returns non-404).
func TestGetPaymentFailures_OperationRegistered(t *testing.T) {
	api, _, mockAdminSvc := setupAdminPaymentFailuresAPI(t)

	clerkUserID := "user_admin123"
	mockAdminSvc.On("IsHardcodedAdmin", mock.Anything).Return(false)
	mockAdminSvc.On("HasRole", mock.Anything, clerkUserID, domain.RoleAdmin).Return(true, nil)

	ctx := context.WithValue(context.Background(), inputs.ClerkUserIDKey, clerkUserID)
	claims := &clerk.SessionClaims{Custom: map[string]interface{}{"email": "admin@example.com"}}
	ctx = context.WithValue(ctx, inputs.ClerkClaimsKey, claims)

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/admin/payments/failures", nil)
	res := httptest.NewRecorder()
	api.Adapter().ServeHTTP(res, req)

	assert.NotEqual(t, http.StatusNotFound, res.Code, "operation should be registered")
	assert.NotEqual(t, http.StatusMethodNotAllowed, res.Code, "GET method should be allowed")
}

// TestGetPaymentFailures_HandlerReturnsTransactions tests the handler directly (bypassing middleware).
func TestGetPaymentFailures_HandlerReturnsTransactions(t *testing.T) {
	mockTxnRepo := new(MockPaddleTransactionRepository)
	cfg := &config.Config{Environment: "qa"}
	handler := NewAdminHandler(nil, nil, nil, nil, nil, nil, nil, cfg).
		WithPaddleTransactionRepo(mockTxnRepo)

	now := time.Now()
	txn := domain.PaddleTransaction{
		ID:            "txn-uuid-1",
		UserID:        "user_abc",
		TransactionID: "txn_01h1234567890failed",
		Amount:        8.99,
		Currency:      "USD",
		Status:        "failed",
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	mockTxnRepo.On("GetFailedTransactions", mock.Anything, 20, 0).Return([]domain.PaddleTransaction{txn}, 1, nil)

	input := &GetPaymentFailuresInput{Limit: 20, Offset: 0}
	out, err := handler.GetPaymentFailures(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, 1, out.Body.Total)
	assert.Equal(t, 20, out.Body.Limit)
	assert.Equal(t, 0, out.Body.Offset)
	require.Len(t, out.Body.Transactions, 1)
	assert.Equal(t, "failed", out.Body.Transactions[0].Status)

	mockTxnRepo.AssertExpectations(t)
}

// TestGetPaymentFailures_WithPagination tests pagination parameters.
func TestGetPaymentFailures_WithPagination(t *testing.T) {
	mockTxnRepo := new(MockPaddleTransactionRepository)
	cfg := &config.Config{Environment: "qa"}
	handler := NewAdminHandler(nil, nil, nil, nil, nil, nil, nil, cfg).
		WithPaddleTransactionRepo(mockTxnRepo)

	mockTxnRepo.On("GetFailedTransactions", mock.Anything, 10, 20).Return([]domain.PaddleTransaction{}, 0, nil)

	input := &GetPaymentFailuresInput{Limit: 10, Offset: 20}
	out, err := handler.GetPaymentFailures(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, 0, out.Body.Total)
	assert.Equal(t, 10, out.Body.Limit)
	assert.Equal(t, 20, out.Body.Offset)
	assert.Empty(t, out.Body.Transactions)

	mockTxnRepo.AssertExpectations(t)
}

// TestGetPaymentFailures_RepoError tests error handling from repository.
func TestGetPaymentFailures_RepoError(t *testing.T) {
	mockTxnRepo := new(MockPaddleTransactionRepository)
	cfg := &config.Config{Environment: "qa"}
	handler := NewAdminHandler(nil, nil, nil, nil, nil, nil, nil, cfg).
		WithPaddleTransactionRepo(mockTxnRepo)

	mockTxnRepo.On("GetFailedTransactions", mock.Anything, 20, 0).Return(nil, 0, errors.New("db error"))

	input := &GetPaymentFailuresInput{Limit: 20, Offset: 0}
	out, err := handler.GetPaymentFailures(context.Background(), input)

	assert.Error(t, err)
	assert.Nil(t, out)
}

// TestGetPaymentFailures_NilRepo tests 503 response when repo is not configured.
func TestGetPaymentFailures_NilRepo(t *testing.T) {
	cfg := &config.Config{Environment: "qa"}
	handler := NewAdminHandler(nil, nil, nil, nil, nil, nil, nil, cfg)
	// Do NOT set paddleTransactionRepo

	input := &GetPaymentFailuresInput{Limit: 20, Offset: 0}
	out, err := handler.GetPaymentFailures(context.Background(), input)

	assert.Error(t, err)
	assert.Nil(t, out)
}
