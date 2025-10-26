package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockTransactionRepository is a mock implementation of TransactionRepository for testing
type MockTransactionRepository struct {
	mock.Mock
}

func (m *MockTransactionRepository) Create(ctx context.Context, transaction *domain.Transaction) error {
	args := m.Called(ctx, transaction)
	return args.Error(0)
}

func (m *MockTransactionRepository) CreateBatch(ctx context.Context, transactions []domain.Transaction) error {
	args := m.Called(ctx, transactions)
	return args.Error(0)
}

func (m *MockTransactionRepository) Update(ctx context.Context, transaction *domain.Transaction) error {
	args := m.Called(ctx, transaction)
	return args.Error(0)
}

func (m *MockTransactionRepository) DeleteByExternalID(ctx context.Context, source domain.TransactionSource, externalID string, userID string) error {
	args := m.Called(ctx, source, externalID, userID)
	return args.Error(0)
}

func (m *MockTransactionRepository) GetByUserID(ctx context.Context, userID string, limit, offset int) ([]domain.Transaction, error) {
	args := m.Called(ctx, userID, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.Transaction), args.Error(1)
}

func (m *MockTransactionRepository) GetByAccountID(ctx context.Context, accountID string) ([]domain.Transaction, error) {
	args := m.Called(ctx, accountID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.Transaction), args.Error(1)
}

func (m *MockTransactionRepository) GetBySource(ctx context.Context, source domain.TransactionSource, userID string) ([]domain.Transaction, error) {
	args := m.Called(ctx, source, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.Transaction), args.Error(1)
}

func (m *MockTransactionRepository) GetByUserIDWithFilters(ctx context.Context, userID string, filters domain.TransactionFilters, limit, offset int) ([]domain.Transaction, error) {
	args := m.Called(ctx, userID, filters, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.Transaction), args.Error(1)
}

func (m *MockTransactionRepository) CountByUserID(ctx context.Context, userID string, filters domain.TransactionFilters) (int, error) {
	args := m.Called(ctx, userID, filters)
	return args.Int(0), args.Error(1)
}

func (m *MockTransactionRepository) GetByExternalID(ctx context.Context, source domain.TransactionSource, externalID string, userID string) (*domain.Transaction, error) {
	args := m.Called(ctx, source, externalID, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Transaction), args.Error(1)
}

// TestTransactionService_ListTransactions_EnforcesMaxLimit tests that the service enforces a maximum limit of 100
func TestTransactionService_ListTransactions_EnforcesMaxLimit(t *testing.T) {
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo)
	ctx := context.Background()

	userID := "user-123"
	filters := domain.TransactionFilters{}

	// Attempt to request more than 100 transactions
	_, err := service.ListTransactions(ctx, userID, filters, 150, 0)

	assert.Error(t, err)
	assert.Equal(t, ErrInvalidLimit, err)
}

// TestTransactionService_ListTransactions_EnforcesNonNegativeOffset tests that negative offsets are rejected
func TestTransactionService_ListTransactions_EnforcesNonNegativeOffset(t *testing.T) {
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo)
	ctx := context.Background()

	userID := "user-123"
	filters := domain.TransactionFilters{}

	// Attempt to use negative offset
	_, err := service.ListTransactions(ctx, userID, filters, 50, -10)

	assert.Error(t, err)
	assert.Equal(t, ErrInvalidOffset, err)
}

// TestTransactionService_ListTransactions_ValidatesDateRange tests that start date must be before end date
func TestTransactionService_ListTransactions_ValidatesDateRange(t *testing.T) {
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo)
	ctx := context.Background()

	userID := "user-123"
	startDate := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	filters := domain.TransactionFilters{
		StartDate: &startDate,
		EndDate:   &endDate,
	}

	// Attempt to use invalid date range (start after end)
	_, err := service.ListTransactions(ctx, userID, filters, 50, 0)

	assert.Error(t, err)
	assert.Equal(t, ErrInvalidDateRange, err)
}

// TestTransactionService_ListTransactions_SanitizesSearchInput tests SQL injection prevention
func TestTransactionService_ListTransactions_SanitizesSearchInput(t *testing.T) {
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo)
	ctx := context.Background()

	userID := "user-123"
	maliciousSearch := "'; DROP TABLE transactions; --"
	filters := domain.TransactionFilters{
		Search: &maliciousSearch,
	}

	// Mock repository should receive sanitized search string
	mockRepo.On("GetByUserIDWithFilters", ctx, userID, mock.MatchedBy(func(f domain.TransactionFilters) bool {
		// Check that search input was sanitized (should not contain SQL injection patterns)
		if f.Search == nil {
			return false
		}
		// Should have dangerous characters removed or escaped
		return !containsSQLInjection(*f.Search)
	}), 50, 0).Return([]domain.Transaction{}, nil)

	mockRepo.On("CountByUserID", ctx, userID, mock.Anything).Return(0, nil)

	result, err := service.ListTransactions(ctx, userID, filters, 50, 0)

	require.NoError(t, err)
	assert.NotNil(t, result)
	mockRepo.AssertExpectations(t)
}

// TestTransactionService_ListTransactions_ReturnsTransactionsArray tests successful transaction retrieval
func TestTransactionService_ListTransactions_ReturnsTransactionsArray(t *testing.T) {
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo)
	ctx := context.Background()

	userID := "user-123"
	filters := domain.TransactionFilters{}

	// Mock transactions
	externalID := "txn-1"
	mockTransactions := []domain.Transaction{
		{
			ID:                    "id-1",
			UserID:                userID,
			AccountID:             "account-1",
			LinkedItemID:          "item-1",
			Source:                domain.SourcePlaid,
			ExternalTransactionID: &externalID,
			Amount:                -25.50,
			Date:                  time.Now(),
			Name:                  "Grocery Store",
		},
	}

	mockRepo.On("GetByUserIDWithFilters", ctx, userID, filters, 50, 0).Return(mockTransactions, nil)
	mockRepo.On("CountByUserID", ctx, userID, filters).Return(1, nil)

	result, err := service.ListTransactions(ctx, userID, filters, 50, 0)

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 1, len(result.Transactions))
	assert.Equal(t, "id-1", result.Transactions[0].ID)
	mockRepo.AssertExpectations(t)
}

// TestTransactionService_ListTransactions_IncludesPaginationMetadata tests that response includes pagination metadata
func TestTransactionService_ListTransactions_IncludesPaginationMetadata(t *testing.T) {
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo)
	ctx := context.Background()

	userID := "user-123"
	filters := domain.TransactionFilters{}
	limit := 50
	offset := 0
	totalCount := 150

	// Mock 50 transactions returned
	mockTransactions := make([]domain.Transaction, 50)
	for i := 0; i < 50; i++ {
		externalID := "txn-1"
		mockTransactions[i] = domain.Transaction{
			ID:                    "id-" + string(rune(i)),
			UserID:                userID,
			ExternalTransactionID: &externalID,
		}
	}

	mockRepo.On("GetByUserIDWithFilters", ctx, userID, filters, limit, offset).Return(mockTransactions, nil)
	mockRepo.On("CountByUserID", ctx, userID, filters).Return(totalCount, nil)

	result, err := service.ListTransactions(ctx, userID, filters, limit, offset)

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, totalCount, result.TotalCount)
	assert.Equal(t, limit, result.Limit)
	assert.Equal(t, offset, result.Offset)
	assert.True(t, result.HasMore) // 150 total, 50 returned, so there's more
	mockRepo.AssertExpectations(t)
}

// TestTransactionService_ListTransactions_HasMoreFalseWhenNoMoreResults tests has_more flag when all results returned
func TestTransactionService_ListTransactions_HasMoreFalseWhenNoMoreResults(t *testing.T) {
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo)
	ctx := context.Background()

	userID := "user-123"
	filters := domain.TransactionFilters{}
	limit := 50
	offset := 100
	totalCount := 120

	// Mock 20 transactions returned (last page)
	mockTransactions := make([]domain.Transaction, 20)
	for i := 0; i < 20; i++ {
		externalID := "txn-1"
		mockTransactions[i] = domain.Transaction{
			ID:                    "id-" + string(rune(i)),
			UserID:                userID,
			ExternalTransactionID: &externalID,
		}
	}

	mockRepo.On("GetByUserIDWithFilters", ctx, userID, filters, limit, offset).Return(mockTransactions, nil)
	mockRepo.On("CountByUserID", ctx, userID, filters).Return(totalCount, nil)

	result, err := service.ListTransactions(ctx, userID, filters, limit, offset)

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, totalCount, result.TotalCount)
	assert.False(t, result.HasMore) // offset=100 + returned=20 = 120, no more results
	mockRepo.AssertExpectations(t)
}

// TestTransactionService_ListTransactions_HandlesRepositoryError tests error handling from repository
func TestTransactionService_ListTransactions_HandlesRepositoryError(t *testing.T) {
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo)
	ctx := context.Background()

	userID := "user-123"
	filters := domain.TransactionFilters{}

	// Mock repository returning an error
	mockRepo.On("GetByUserIDWithFilters", ctx, userID, filters, 50, 0).Return(nil, repository.ErrTransactionNotFound)

	result, err := service.ListTransactions(ctx, userID, filters, 50, 0)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to get transactions")
	mockRepo.AssertExpectations(t)
}

// TestTransactionService_ListTransactions_HandlesCountError tests error handling from count operation
func TestTransactionService_ListTransactions_HandlesCountError(t *testing.T) {
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo)
	ctx := context.Background()

	userID := "user-123"
	filters := domain.TransactionFilters{}

	mockRepo.On("GetByUserIDWithFilters", ctx, userID, filters, 50, 0).Return([]domain.Transaction{}, nil)
	mockRepo.On("CountByUserID", ctx, userID, filters).Return(0, errors.New("database connection error"))

	result, err := service.ListTransactions(ctx, userID, filters, 50, 0)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to count transactions")
	mockRepo.AssertExpectations(t)
}

// TestTransactionService_ListTransactions_DefaultsLimit tests that default limit of 50 is applied
func TestTransactionService_ListTransactions_DefaultsLimit(t *testing.T) {
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo)
	ctx := context.Background()

	userID := "user-123"
	filters := domain.TransactionFilters{}

	// Pass limit=0, should default to 50
	mockRepo.On("GetByUserIDWithFilters", ctx, userID, filters, 50, 0).Return([]domain.Transaction{}, nil)
	mockRepo.On("CountByUserID", ctx, userID, filters).Return(0, nil)

	result, err := service.ListTransactions(ctx, userID, filters, 0, 0)

	require.NoError(t, err)
	assert.Equal(t, 50, result.Limit)
	mockRepo.AssertExpectations(t)
}

// Helper function to detect SQL injection patterns
func containsSQLInjection(s string) bool {
	dangerousPatterns := []string{
		"DROP TABLE",
		"DELETE FROM",
		"INSERT INTO",
		"UPDATE ",
		"';",
		"--",
		"/*",
		"*/",
		"UNION SELECT",
		"OR 1=1",
	}

	for _, pattern := range dangerousPatterns {
		if len(s) >= len(pattern) {
			for i := 0; i <= len(s)-len(pattern); i++ {
				// Case-insensitive comparison
				match := true
				for j := 0; j < len(pattern); j++ {
					c1 := s[i+j]
					c2 := pattern[j]
					if c1 >= 'a' && c1 <= 'z' {
						c1 = c1 - 32 // Convert to uppercase
					}
					if c2 >= 'a' && c2 <= 'z' {
						c2 = c2 - 32
					}
					if c1 != c2 {
						match = false
						break
					}
				}
				if match {
					return true
				}
			}
		}
	}
	return false
}
