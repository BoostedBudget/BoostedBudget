package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Issue #497: Category Preview Service Tests
// =============================================================================

// MockTransactionStatsCalculator is a mock implementation of TransactionStatsCalculator
type MockTransactionStatsCalculator struct {
	mock.Mock
}

func (m *MockTransactionStatsCalculator) GetMatchingTransactionStats(ctx context.Context, userID string, categories [][]string, tags []string, matchLogic domain.MatchLogic, startDate, endDate time.Time) (int, float64, error) {
	args := m.Called(ctx, userID, categories, tags, matchLogic, startDate, endDate)
	return args.Int(0), args.Get(1).(float64), args.Error(2)
}

// TestCategoryPreviewService_PreviewCategoryMatches_Success tests successful preview
func TestCategoryPreviewService_PreviewCategoryMatches_Success(t *testing.T) {
	mockCalc := new(MockTransactionStatsCalculator)
	service := NewCategoryPreviewService(mockCalc)

	ctx := context.Background()
	userID := "user-123"
	req := &domain.CategoryPreviewRequest{
		Categories: [][]string{{"Food and Drink", "Restaurants"}},
		Tags:       []string{},
		MatchLogic: domain.MatchLogicCategoryOnly,
		StartDate:  "2024-01-01",
		EndDate:    "2024-01-31",
	}

	expectedStartDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	expectedEndDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)

	mockCalc.On("GetMatchingTransactionStats",
		ctx, userID, req.Categories, req.Tags, req.MatchLogic,
		expectedStartDate, expectedEndDate,
	).Return(5, 250.50, nil)

	resp, err := service.PreviewCategoryMatches(ctx, userID, req)
	require.NoError(t, err)
	assert.Equal(t, 5, resp.TransactionCount)
	assert.Equal(t, 250.50, resp.TotalAmount)

	mockCalc.AssertExpectations(t)
}

// TestCategoryPreviewService_PreviewCategoryMatches_MultipleCategories tests multiple categories
func TestCategoryPreviewService_PreviewCategoryMatches_MultipleCategories(t *testing.T) {
	mockCalc := new(MockTransactionStatsCalculator)
	service := NewCategoryPreviewService(mockCalc)

	ctx := context.Background()
	userID := "user-123"
	req := &domain.CategoryPreviewRequest{
		Categories: [][]string{
			{"Food and Drink", "Restaurants"},
			{"Food and Drink", "Groceries"},
		},
		Tags:       []string{},
		MatchLogic: domain.MatchLogicCategoryOnly,
		StartDate:  "2024-01-01",
		EndDate:    "2024-01-31",
	}

	mockCalc.On("GetMatchingTransactionStats",
		ctx, userID, req.Categories, req.Tags, req.MatchLogic,
		mock.Anything, mock.Anything,
	).Return(12, 650.00, nil)

	resp, err := service.PreviewCategoryMatches(ctx, userID, req)
	require.NoError(t, err)
	assert.Equal(t, 12, resp.TransactionCount)
	assert.Equal(t, 650.00, resp.TotalAmount)

	mockCalc.AssertExpectations(t)
}

// TestCategoryPreviewService_PreviewCategoryMatches_TagOnly tests tag_only match logic
func TestCategoryPreviewService_PreviewCategoryMatches_TagOnly(t *testing.T) {
	mockCalc := new(MockTransactionStatsCalculator)
	service := NewCategoryPreviewService(mockCalc)

	ctx := context.Background()
	userID := "user-123"
	req := &domain.CategoryPreviewRequest{
		Categories: [][]string{},
		Tags:       []string{"Essential", "groceries"},
		MatchLogic: domain.MatchLogicTagOnly,
		StartDate:  "2024-01-01",
		EndDate:    "2024-01-31",
	}

	mockCalc.On("GetMatchingTransactionStats",
		ctx, userID, req.Categories, req.Tags, req.MatchLogic,
		mock.Anything, mock.Anything,
	).Return(8, 400.00, nil)

	resp, err := service.PreviewCategoryMatches(ctx, userID, req)
	require.NoError(t, err)
	assert.Equal(t, 8, resp.TransactionCount)
	assert.Equal(t, 400.00, resp.TotalAmount)

	mockCalc.AssertExpectations(t)
}

// TestCategoryPreviewService_PreviewCategoryMatches_AndLogic tests AND match logic
func TestCategoryPreviewService_PreviewCategoryMatches_AndLogic(t *testing.T) {
	mockCalc := new(MockTransactionStatsCalculator)
	service := NewCategoryPreviewService(mockCalc)

	ctx := context.Background()
	userID := "user-123"
	req := &domain.CategoryPreviewRequest{
		Categories: [][]string{{"Food and Drink", "Restaurants"}},
		Tags:       []string{"Essential"},
		MatchLogic: domain.MatchLogicAnd,
		StartDate:  "2024-01-01",
		EndDate:    "2024-01-31",
	}

	mockCalc.On("GetMatchingTransactionStats",
		ctx, userID, req.Categories, req.Tags, req.MatchLogic,
		mock.Anything, mock.Anything,
	).Return(3, 150.00, nil)

	resp, err := service.PreviewCategoryMatches(ctx, userID, req)
	require.NoError(t, err)
	assert.Equal(t, 3, resp.TransactionCount)
	assert.Equal(t, 150.00, resp.TotalAmount)

	mockCalc.AssertExpectations(t)
}

// TestCategoryPreviewService_PreviewCategoryMatches_OrLogic tests OR match logic
func TestCategoryPreviewService_PreviewCategoryMatches_OrLogic(t *testing.T) {
	mockCalc := new(MockTransactionStatsCalculator)
	service := NewCategoryPreviewService(mockCalc)

	ctx := context.Background()
	userID := "user-123"
	req := &domain.CategoryPreviewRequest{
		Categories: [][]string{{"Food and Drink", "Restaurants"}},
		Tags:       []string{"Essential"},
		MatchLogic: domain.MatchLogicOr,
		StartDate:  "2024-01-01",
		EndDate:    "2024-01-31",
	}

	mockCalc.On("GetMatchingTransactionStats",
		ctx, userID, req.Categories, req.Tags, req.MatchLogic,
		mock.Anything, mock.Anything,
	).Return(10, 500.00, nil)

	resp, err := service.PreviewCategoryMatches(ctx, userID, req)
	require.NoError(t, err)
	assert.Equal(t, 10, resp.TransactionCount)
	assert.Equal(t, 500.00, resp.TotalAmount)

	mockCalc.AssertExpectations(t)
}

// TestCategoryPreviewService_PreviewCategoryMatches_NilRequest tests nil request handling
func TestCategoryPreviewService_PreviewCategoryMatches_NilRequest(t *testing.T) {
	mockCalc := new(MockTransactionStatsCalculator)
	service := NewCategoryPreviewService(mockCalc)

	ctx := context.Background()
	userID := "user-123"

	resp, err := service.PreviewCategoryMatches(ctx, userID, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request is required")
}

// TestCategoryPreviewService_PreviewCategoryMatches_InvalidMatchLogic tests invalid match logic
func TestCategoryPreviewService_PreviewCategoryMatches_InvalidMatchLogic(t *testing.T) {
	mockCalc := new(MockTransactionStatsCalculator)
	service := NewCategoryPreviewService(mockCalc)

	ctx := context.Background()
	userID := "user-123"
	req := &domain.CategoryPreviewRequest{
		Categories: [][]string{{"Food"}},
		Tags:       []string{},
		MatchLogic: "invalid_logic",
		StartDate:  "2024-01-01",
		EndDate:    "2024-01-31",
	}

	resp, err := service.PreviewCategoryMatches(ctx, userID, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "invalid match_logic")
}

// TestCategoryPreviewService_PreviewCategoryMatches_InvalidStartDate tests invalid start date
func TestCategoryPreviewService_PreviewCategoryMatches_InvalidStartDate(t *testing.T) {
	mockCalc := new(MockTransactionStatsCalculator)
	service := NewCategoryPreviewService(mockCalc)

	ctx := context.Background()
	userID := "user-123"
	req := &domain.CategoryPreviewRequest{
		Categories: [][]string{{"Food"}},
		Tags:       []string{},
		MatchLogic: domain.MatchLogicCategoryOnly,
		StartDate:  "invalid-date",
		EndDate:    "2024-01-31",
	}

	resp, err := service.PreviewCategoryMatches(ctx, userID, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "invalid start_date format")
}

// TestCategoryPreviewService_PreviewCategoryMatches_InvalidEndDate tests invalid end date
func TestCategoryPreviewService_PreviewCategoryMatches_InvalidEndDate(t *testing.T) {
	mockCalc := new(MockTransactionStatsCalculator)
	service := NewCategoryPreviewService(mockCalc)

	ctx := context.Background()
	userID := "user-123"
	req := &domain.CategoryPreviewRequest{
		Categories: [][]string{{"Food"}},
		Tags:       []string{},
		MatchLogic: domain.MatchLogicCategoryOnly,
		StartDate:  "2024-01-01",
		EndDate:    "invalid-date",
	}

	resp, err := service.PreviewCategoryMatches(ctx, userID, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "invalid end_date format")
}

// TestCategoryPreviewService_PreviewCategoryMatches_NoCategoriesForCategoryOnly tests validation
func TestCategoryPreviewService_PreviewCategoryMatches_NoCategoriesForCategoryOnly(t *testing.T) {
	mockCalc := new(MockTransactionStatsCalculator)
	service := NewCategoryPreviewService(mockCalc)

	ctx := context.Background()
	userID := "user-123"
	req := &domain.CategoryPreviewRequest{
		Categories: [][]string{},
		Tags:       []string{},
		MatchLogic: domain.MatchLogicCategoryOnly,
		StartDate:  "2024-01-01",
		EndDate:    "2024-01-31",
	}

	resp, err := service.PreviewCategoryMatches(ctx, userID, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "at least one category is required")
}

// TestCategoryPreviewService_PreviewCategoryMatches_NoTagsForTagOnly tests validation
func TestCategoryPreviewService_PreviewCategoryMatches_NoTagsForTagOnly(t *testing.T) {
	mockCalc := new(MockTransactionStatsCalculator)
	service := NewCategoryPreviewService(mockCalc)

	ctx := context.Background()
	userID := "user-123"
	req := &domain.CategoryPreviewRequest{
		Categories: [][]string{},
		Tags:       []string{},
		MatchLogic: domain.MatchLogicTagOnly,
		StartDate:  "2024-01-01",
		EndDate:    "2024-01-31",
	}

	resp, err := service.PreviewCategoryMatches(ctx, userID, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "at least one tag is required")
}

// TestCategoryPreviewService_PreviewCategoryMatches_AndLogicValidation tests AND logic validation
func TestCategoryPreviewService_PreviewCategoryMatches_AndLogicValidation(t *testing.T) {
	mockCalc := new(MockTransactionStatsCalculator)
	service := NewCategoryPreviewService(mockCalc)

	ctx := context.Background()
	userID := "user-123"

	// Test missing categories
	req1 := &domain.CategoryPreviewRequest{
		Categories: [][]string{},
		Tags:       []string{"Essential"},
		MatchLogic: domain.MatchLogicAnd,
		StartDate:  "2024-01-01",
		EndDate:    "2024-01-31",
	}

	resp, err := service.PreviewCategoryMatches(ctx, userID, req1)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "at least one category is required")

	// Test missing tags
	req2 := &domain.CategoryPreviewRequest{
		Categories: [][]string{{"Food"}},
		Tags:       []string{},
		MatchLogic: domain.MatchLogicAnd,
		StartDate:  "2024-01-01",
		EndDate:    "2024-01-31",
	}

	resp, err = service.PreviewCategoryMatches(ctx, userID, req2)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "at least one tag is required")
}

// TestCategoryPreviewService_PreviewCategoryMatches_RepositoryError tests repository error handling
func TestCategoryPreviewService_PreviewCategoryMatches_RepositoryError(t *testing.T) {
	mockCalc := new(MockTransactionStatsCalculator)
	service := NewCategoryPreviewService(mockCalc)

	ctx := context.Background()
	userID := "user-123"
	req := &domain.CategoryPreviewRequest{
		Categories: [][]string{{"Food"}},
		Tags:       []string{},
		MatchLogic: domain.MatchLogicCategoryOnly,
		StartDate:  "2024-01-01",
		EndDate:    "2024-01-31",
	}

	mockCalc.On("GetMatchingTransactionStats",
		ctx, userID, req.Categories, req.Tags, req.MatchLogic,
		mock.Anything, mock.Anything,
	).Return(0, 0.0, errors.New("database error"))

	resp, err := service.PreviewCategoryMatches(ctx, userID, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "database error")

	mockCalc.AssertExpectations(t)
}

// TestCategoryPreviewService_PreviewCategoryMatches_ZeroResults tests zero results case
func TestCategoryPreviewService_PreviewCategoryMatches_ZeroResults(t *testing.T) {
	mockCalc := new(MockTransactionStatsCalculator)
	service := NewCategoryPreviewService(mockCalc)

	ctx := context.Background()
	userID := "user-123"
	req := &domain.CategoryPreviewRequest{
		Categories: [][]string{{"Nonexistent", "Category"}},
		Tags:       []string{},
		MatchLogic: domain.MatchLogicCategoryOnly,
		StartDate:  "2024-01-01",
		EndDate:    "2024-01-31",
	}

	mockCalc.On("GetMatchingTransactionStats",
		ctx, userID, req.Categories, req.Tags, req.MatchLogic,
		mock.Anything, mock.Anything,
	).Return(0, 0.0, nil)

	resp, err := service.PreviewCategoryMatches(ctx, userID, req)
	require.NoError(t, err)
	assert.Equal(t, 0, resp.TransactionCount)
	assert.Equal(t, 0.0, resp.TotalAmount)

	mockCalc.AssertExpectations(t)
}
