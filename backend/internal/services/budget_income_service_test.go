package services

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// =============================================================================
// Issue #492: Budget Income Service Tests
// =============================================================================

// MockIncomeTransactionGetter is a mock implementation for testing
type MockIncomeTransactionGetter struct {
	mock.Mock
}

func (m *MockIncomeTransactionGetter) GetIncomeSumByCategory(ctx context.Context, userID string, category string, startDate, endDate time.Time) (float64, error) {
	args := m.Called(ctx, userID, category, startDate, endDate)
	return args.Get(0).(float64), args.Error(1)
}

func (m *MockIncomeTransactionGetter) GetIncomeSumByTag(ctx context.Context, userID string, tag string, startDate, endDate time.Time) (float64, error) {
	args := m.Called(ctx, userID, tag, startDate, endDate)
	return args.Get(0).(float64), args.Error(1)
}

func (m *MockIncomeTransactionGetter) GetByUserIDWithFilters(ctx context.Context, userID string, filters domain.TransactionFilters, limit, offset int) ([]domain.Transaction, error) {
	args := m.Called(ctx, userID, filters, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.Transaction), args.Error(1)
}

// Helper to create a test logger
func testIncomeServiceLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
}

// =============================================================================
// CalculateBudgetIncome Tests
// =============================================================================

// TestBudgetIncomeService_CalculateBudgetIncome_PlaidCategory tests income calculation with Plaid category source
func TestBudgetIncomeService_CalculateBudgetIncome_PlaidCategory(t *testing.T) {
	mockRepo := new(MockIncomeTransactionGetter)
	logger := testIncomeServiceLogger()
	service := NewBudgetIncomeService(mockRepo, logger)

	ctx := context.Background()
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)

	budget := &domain.Budget{
		UserID: "user-123",
		IncomeSources: []domain.IncomeSource{
			{Type: domain.IncomeSourceTypePlaidCategory, Value: "Paycheck"},
		},
	}

	mockRepo.On("GetIncomeSumByCategory", ctx, "user-123", "Paycheck", startDate, endDate).
		Return(5000.00, nil)

	income, err := service.CalculateBudgetIncome(ctx, budget, startDate, endDate)

	assert.NoError(t, err)
	assert.Equal(t, 5000.00, income)
	mockRepo.AssertExpectations(t)
}

// TestBudgetIncomeService_CalculateBudgetIncome_Tag tests income calculation with tag source
func TestBudgetIncomeService_CalculateBudgetIncome_Tag(t *testing.T) {
	mockRepo := new(MockIncomeTransactionGetter)
	logger := testIncomeServiceLogger()
	service := NewBudgetIncomeService(mockRepo, logger)

	ctx := context.Background()
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)

	budget := &domain.Budget{
		UserID: "user-123",
		IncomeSources: []domain.IncomeSource{
			{Type: domain.IncomeSourceTypeTag, Value: "bonus"},
		},
	}

	mockRepo.On("GetIncomeSumByTag", ctx, "user-123", "bonus", startDate, endDate).
		Return(1000.00, nil)

	income, err := service.CalculateBudgetIncome(ctx, budget, startDate, endDate)

	assert.NoError(t, err)
	assert.Equal(t, 1000.00, income)
	mockRepo.AssertExpectations(t)
}

// TestBudgetIncomeService_CalculateBudgetIncome_Manual tests income calculation with manual source
func TestBudgetIncomeService_CalculateBudgetIncome_Manual(t *testing.T) {
	mockRepo := new(MockIncomeTransactionGetter)
	logger := testIncomeServiceLogger()
	service := NewBudgetIncomeService(mockRepo, logger)

	ctx := context.Background()
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)

	budget := &domain.Budget{
		UserID: "user-123",
		IncomeSources: []domain.IncomeSource{
			{Type: domain.IncomeSourceTypeManual, Value: "1500.50", Description: "Side gig"},
		},
	}

	income, err := service.CalculateBudgetIncome(ctx, budget, startDate, endDate)

	assert.NoError(t, err)
	assert.Equal(t, 1500.50, income)
	// No repository calls for manual sources
	mockRepo.AssertNotCalled(t, "GetIncomeSumByCategory")
	mockRepo.AssertNotCalled(t, "GetIncomeSumByTag")
}

// TestBudgetIncomeService_CalculateBudgetIncome_MixedSources tests income calculation with multiple source types
func TestBudgetIncomeService_CalculateBudgetIncome_MixedSources(t *testing.T) {
	mockRepo := new(MockIncomeTransactionGetter)
	logger := testIncomeServiceLogger()
	service := NewBudgetIncomeService(mockRepo, logger)

	ctx := context.Background()
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)

	budget := &domain.Budget{
		UserID: "user-123",
		IncomeSources: []domain.IncomeSource{
			{Type: domain.IncomeSourceTypePlaidCategory, Value: "Paycheck"},
			{Type: domain.IncomeSourceTypeTag, Value: "bonus"},
			{Type: domain.IncomeSourceTypeManual, Value: "500.00"},
		},
	}

	mockRepo.On("GetIncomeSumByCategory", ctx, "user-123", "Paycheck", startDate, endDate).
		Return(5000.00, nil)
	mockRepo.On("GetIncomeSumByTag", ctx, "user-123", "bonus", startDate, endDate).
		Return(1000.00, nil)

	income, err := service.CalculateBudgetIncome(ctx, budget, startDate, endDate)

	assert.NoError(t, err)
	assert.Equal(t, 6500.00, income) // 5000 + 1000 + 500
	mockRepo.AssertExpectations(t)
}

// TestBudgetIncomeService_CalculateBudgetIncome_EmptySources tests with no income sources
func TestBudgetIncomeService_CalculateBudgetIncome_EmptySources(t *testing.T) {
	mockRepo := new(MockIncomeTransactionGetter)
	logger := testIncomeServiceLogger()
	service := NewBudgetIncomeService(mockRepo, logger)

	ctx := context.Background()
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)

	budget := &domain.Budget{
		UserID:        "user-123",
		IncomeSources: []domain.IncomeSource{},
	}

	income, err := service.CalculateBudgetIncome(ctx, budget, startDate, endDate)

	assert.NoError(t, err)
	assert.Equal(t, 0.0, income)
}

// TestBudgetIncomeService_CalculateBudgetIncome_InvalidManualValue tests invalid manual value handling
func TestBudgetIncomeService_CalculateBudgetIncome_InvalidManualValue(t *testing.T) {
	mockRepo := new(MockIncomeTransactionGetter)
	logger := testIncomeServiceLogger()
	service := NewBudgetIncomeService(mockRepo, logger)

	ctx := context.Background()
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)

	budget := &domain.Budget{
		UserID: "user-123",
		IncomeSources: []domain.IncomeSource{
			{Type: domain.IncomeSourceTypeManual, Value: "not-a-number"},
			{Type: domain.IncomeSourceTypeManual, Value: "100.00"}, // Valid one after invalid
		},
	}

	income, err := service.CalculateBudgetIncome(ctx, budget, startDate, endDate)

	assert.NoError(t, err)
	assert.Equal(t, 100.00, income) // Only the valid manual value is counted
}

// TestBudgetIncomeService_CalculateBudgetIncome_NegativeManualValue tests negative manual value handling
func TestBudgetIncomeService_CalculateBudgetIncome_NegativeManualValue(t *testing.T) {
	mockRepo := new(MockIncomeTransactionGetter)
	logger := testIncomeServiceLogger()
	service := NewBudgetIncomeService(mockRepo, logger)

	ctx := context.Background()
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)

	budget := &domain.Budget{
		UserID: "user-123",
		IncomeSources: []domain.IncomeSource{
			{Type: domain.IncomeSourceTypeManual, Value: "-500.00"}, // Negative, should be skipped
			{Type: domain.IncomeSourceTypeManual, Value: "200.00"},  // Positive
		},
	}

	income, err := service.CalculateBudgetIncome(ctx, budget, startDate, endDate)

	assert.NoError(t, err)
	assert.Equal(t, 200.00, income) // Only positive value counted
}

// TestBudgetIncomeService_CalculateBudgetIncome_RepositoryError tests error propagation
func TestBudgetIncomeService_CalculateBudgetIncome_RepositoryError(t *testing.T) {
	mockRepo := new(MockIncomeTransactionGetter)
	logger := testIncomeServiceLogger()
	service := NewBudgetIncomeService(mockRepo, logger)

	ctx := context.Background()
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)

	budget := &domain.Budget{
		UserID: "user-123",
		IncomeSources: []domain.IncomeSource{
			{Type: domain.IncomeSourceTypePlaidCategory, Value: "Paycheck"},
		},
	}

	mockRepo.On("GetIncomeSumByCategory", ctx, "user-123", "Paycheck", startDate, endDate).
		Return(0.0, errors.New("database connection failed"))

	income, err := service.CalculateBudgetIncome(ctx, budget, startDate, endDate)

	assert.Error(t, err)
	assert.Equal(t, 0.0, income)
	assert.Contains(t, err.Error(), "failed to calculate income for category Paycheck")
	mockRepo.AssertExpectations(t)
}

// =============================================================================
// GetMatchingIncomeTransactions Tests
// =============================================================================

// TestBudgetIncomeService_GetMatchingIncomeTransactions_PlaidCategory tests matching by category
func TestBudgetIncomeService_GetMatchingIncomeTransactions_PlaidCategory(t *testing.T) {
	mockRepo := new(MockIncomeTransactionGetter)
	logger := testIncomeServiceLogger()
	service := NewBudgetIncomeService(mockRepo, logger)

	ctx := context.Background()
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)

	incomeSources := []domain.IncomeSource{
		{Type: domain.IncomeSourceTypePlaidCategory, Value: "Paycheck"},
	}

	paycheckCategory := `["Paycheck"]`
	transactions := []domain.Transaction{
		{ID: "tx-1", UserID: "user-123", Amount: 5000.00, Category: &paycheckCategory},        // Match
		{ID: "tx-2", UserID: "user-123", Amount: -50.00, Category: &paycheckCategory},         // Negative, excluded
		{ID: "tx-3", UserID: "user-123", Amount: 100.00, Category: incomeTestStrPtr(`["Other"]`)}, // Wrong category
		{ID: "tx-4", UserID: "user-123", Amount: 2500.00, CategoryOverride: &paycheckCategory}, // Match via override
	}

	mockRepo.On("GetByUserIDWithFilters", ctx, "user-123", mock.Anything, 10000, 0).
		Return(transactions, nil)

	result, err := service.GetMatchingIncomeTransactions(ctx, "user-123", incomeSources, startDate, endDate)

	assert.NoError(t, err)
	assert.Len(t, result, 2)
	mockRepo.AssertExpectations(t)
}

// TestBudgetIncomeService_GetMatchingIncomeTransactions_Tag tests matching by tag
func TestBudgetIncomeService_GetMatchingIncomeTransactions_Tag(t *testing.T) {
	mockRepo := new(MockIncomeTransactionGetter)
	logger := testIncomeServiceLogger()
	service := NewBudgetIncomeService(mockRepo, logger)

	ctx := context.Background()
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)

	incomeSources := []domain.IncomeSource{
		{Type: domain.IncomeSourceTypeTag, Value: "salary"},
	}

	transactions := []domain.Transaction{
		{ID: "tx-1", UserID: "user-123", Amount: 5000.00, Tags: []string{"salary"}},         // Match
		{ID: "tx-2", UserID: "user-123", Amount: -50.00, Tags: []string{"salary"}},          // Negative, excluded
		{ID: "tx-3", UserID: "user-123", Amount: 100.00, Tags: []string{"bonus"}},           // Wrong tag
		{ID: "tx-4", UserID: "user-123", Amount: 2500.00, Tags: []string{"salary", "main"}}, // Match with multiple tags
	}

	mockRepo.On("GetByUserIDWithFilters", ctx, "user-123", mock.Anything, 10000, 0).
		Return(transactions, nil)

	result, err := service.GetMatchingIncomeTransactions(ctx, "user-123", incomeSources, startDate, endDate)

	assert.NoError(t, err)
	assert.Len(t, result, 2)
	mockRepo.AssertExpectations(t)
}

// TestBudgetIncomeService_GetMatchingIncomeTransactions_ManualSourceIgnored tests that manual sources don't match transactions
func TestBudgetIncomeService_GetMatchingIncomeTransactions_ManualSourceIgnored(t *testing.T) {
	mockRepo := new(MockIncomeTransactionGetter)
	logger := testIncomeServiceLogger()
	service := NewBudgetIncomeService(mockRepo, logger)

	ctx := context.Background()
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)

	incomeSources := []domain.IncomeSource{
		{Type: domain.IncomeSourceTypeManual, Value: "1000.00"}, // Manual sources don't match transactions
	}

	transactions := []domain.Transaction{
		{ID: "tx-1", UserID: "user-123", Amount: 5000.00, Tags: []string{"salary"}},
	}

	mockRepo.On("GetByUserIDWithFilters", ctx, "user-123", mock.Anything, 10000, 0).
		Return(transactions, nil)

	result, err := service.GetMatchingIncomeTransactions(ctx, "user-123", incomeSources, startDate, endDate)

	assert.NoError(t, err)
	assert.Len(t, result, 0) // No matches because manual sources don't match transactions
	mockRepo.AssertExpectations(t)
}

// TestBudgetIncomeService_GetMatchingIncomeTransactions_Deduplication tests that transactions matching multiple sources are deduplicated
func TestBudgetIncomeService_GetMatchingIncomeTransactions_Deduplication(t *testing.T) {
	mockRepo := new(MockIncomeTransactionGetter)
	logger := testIncomeServiceLogger()
	service := NewBudgetIncomeService(mockRepo, logger)

	ctx := context.Background()
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)

	incomeSources := []domain.IncomeSource{
		{Type: domain.IncomeSourceTypePlaidCategory, Value: "Paycheck"},
		{Type: domain.IncomeSourceTypeTag, Value: "salary"},
	}

	paycheckCategory := `["Paycheck"]`
	transactions := []domain.Transaction{
		// This transaction matches BOTH category and tag sources
		{ID: "tx-1", UserID: "user-123", Amount: 5000.00, Category: &paycheckCategory, Tags: []string{"salary"}},
	}

	mockRepo.On("GetByUserIDWithFilters", ctx, "user-123", mock.Anything, 10000, 0).
		Return(transactions, nil)

	result, err := service.GetMatchingIncomeTransactions(ctx, "user-123", incomeSources, startDate, endDate)

	assert.NoError(t, err)
	assert.Len(t, result, 1) // Should appear only once despite matching multiple sources
	mockRepo.AssertExpectations(t)
}

// TestBudgetIncomeService_GetMatchingIncomeTransactions_RepositoryError tests error handling
func TestBudgetIncomeService_GetMatchingIncomeTransactions_RepositoryError(t *testing.T) {
	mockRepo := new(MockIncomeTransactionGetter)
	logger := testIncomeServiceLogger()
	service := NewBudgetIncomeService(mockRepo, logger)

	ctx := context.Background()
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)

	incomeSources := []domain.IncomeSource{
		{Type: domain.IncomeSourceTypePlaidCategory, Value: "Paycheck"},
	}

	mockRepo.On("GetByUserIDWithFilters", ctx, "user-123", mock.Anything, 10000, 0).
		Return(nil, errors.New("database error"))

	result, err := service.GetMatchingIncomeTransactions(ctx, "user-123", incomeSources, startDate, endDate)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to get transactions")
	mockRepo.AssertExpectations(t)
}

// Helper function to create string pointer for income service tests
func incomeTestStrPtr(s string) *string {
	return &s
}
