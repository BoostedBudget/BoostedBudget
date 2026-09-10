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

func (m *MockTransactionStatsCalculator) GetMatchingTransactionStats(ctx context.Context, userID string, categoryKind string, pfcPrimary *string, pfcDetailed *string, customCategoryID *string, tags []string, matchLogic domain.MatchLogic, startDate, endDate time.Time) (int, float64, error) {
	args := m.Called(ctx, userID, categoryKind, pfcPrimary, pfcDetailed, customCategoryID, tags, matchLogic, startDate, endDate)
	return args.Int(0), args.Get(1).(float64), args.Error(2)
}

func (m *MockTransactionStatsCalculator) GetCategoryMatchBreakdown(ctx context.Context, userID string, categoryKind string, pfcPrimary *string, pfcDetailed *string, customCategoryID *string, startDate, endDate time.Time) ([]domain.CategoryPreviewBreakdownItem, int, float64, error) {
	args := m.Called(ctx, userID, categoryKind, pfcPrimary, pfcDetailed, customCategoryID, startDate, endDate)
	var children []domain.CategoryPreviewBreakdownItem
	if v := args.Get(0); v != nil {
		children = v.([]domain.CategoryPreviewBreakdownItem)
	}
	return children, args.Int(1), args.Get(2).(float64), args.Error(3)
}

// TestCategoryPreviewService_PreviewCategoryMatches_Success tests successful preview
func TestCategoryPreviewService_PreviewCategoryMatches_Success(t *testing.T) {
	mockCalc := new(MockTransactionStatsCalculator)
	service := NewCategoryPreviewService(mockCalc)

	ctx := context.Background()
	userID := "user-123"
	req := &domain.CategoryPreviewRequest{
		CategoryKind: "pfc",
		PFCPrimary:   ptrStr("FOOD_AND_DRINK"),
		Tags:         []string{},
		MatchLogic:   domain.MatchLogicCategoryOnly,
		StartDate:    "2024-01-01",
		EndDate:      "2024-01-31",
	}

	mockCalc.On("GetMatchingTransactionStats",
		ctx, userID, "pfc", ptrStr("FOOD_AND_DRINK"), (*string)(nil), (*string)(nil), []string{}, domain.MatchLogicCategoryOnly,
		mock.Anything, mock.Anything,
	).Return(5, 250.50, nil)
	mockCalc.On("GetCategoryMatchBreakdown",
		ctx, userID, "pfc", ptrStr("FOOD_AND_DRINK"), (*string)(nil), (*string)(nil),
		mock.Anything, mock.Anything,
	).Return(nil, 0, 0.0, nil)

	resp, err := service.PreviewCategoryMatches(ctx, userID, req)
	require.NoError(t, err)
	assert.Equal(t, 5, resp.TransactionCount)
	assert.Equal(t, 250.50, resp.TotalAmount)
	assert.Empty(t, resp.Children)

	mockCalc.AssertExpectations(t)
}

// TestCategoryPreviewService_PreviewCategoryMatches_WithChildBreakdown tests that a
// pfc_primary rollup discloses its nested custom-category children (Issue #1630 Task 5):
// a "2 txns, $615.28" total that includes a $423.20 "HOA" custom category should say so.
func TestCategoryPreviewService_PreviewCategoryMatches_WithChildBreakdown(t *testing.T) {
	mockCalc := new(MockTransactionStatsCalculator)
	service := NewCategoryPreviewService(mockCalc)

	ctx := context.Background()
	userID := "user-123"
	req := &domain.CategoryPreviewRequest{
		CategoryKind: "pfc",
		PFCPrimary:   ptrStr("RENT_AND_UTILITIES"),
		Tags:         []string{},
		MatchLogic:   domain.MatchLogicCategoryOnly,
		StartDate:    "2024-01-01",
		EndDate:      "2024-01-31",
	}

	mockCalc.On("GetMatchingTransactionStats",
		ctx, userID, "pfc", ptrStr("RENT_AND_UTILITIES"), (*string)(nil), (*string)(nil), []string{}, domain.MatchLogicCategoryOnly,
		mock.Anything, mock.Anything,
	).Return(2, 615.28, nil)
	mockCalc.On("GetCategoryMatchBreakdown",
		ctx, userID, "pfc", ptrStr("RENT_AND_UTILITIES"), (*string)(nil), (*string)(nil),
		mock.Anything, mock.Anything,
	).Return([]domain.CategoryPreviewBreakdownItem{
		{CategoryID: "cc-hoa", CategoryName: "HOA", TransactionCount: 1, TotalAmount: 423.20},
	}, 1, 192.08, nil)

	resp, err := service.PreviewCategoryMatches(ctx, userID, req)
	require.NoError(t, err)
	assert.Equal(t, 2, resp.TransactionCount)
	assert.Equal(t, 615.28, resp.TotalAmount)
	assert.Equal(t, 1, resp.DirectTransactionCount)
	assert.Equal(t, 192.08, resp.DirectAmount)
	require.Len(t, resp.Children, 1)
	assert.Equal(t, "HOA", resp.Children[0].CategoryName)
	assert.Equal(t, 423.20, resp.Children[0].TotalAmount)
	assert.Equal(t, 1, resp.Children[0].TransactionCount)

	mockCalc.AssertExpectations(t)
}

// TestCategoryPreviewService_PreviewCategoryMatches_TagOnly tests tag_only match logic
func TestCategoryPreviewService_PreviewCategoryMatches_TagOnly(t *testing.T) {
	mockCalc := new(MockTransactionStatsCalculator)
	service := NewCategoryPreviewService(mockCalc)

	ctx := context.Background()
	userID := "user-123"
	req := &domain.CategoryPreviewRequest{
		CategoryKind: "",
		Tags:         []string{"Essential", "groceries"},
		MatchLogic:   domain.MatchLogicTagOnly,
		StartDate:    "2024-01-01",
		EndDate:      "2024-01-31",
	}

	mockCalc.On("GetMatchingTransactionStats",
		ctx, userID, "", (*string)(nil), (*string)(nil), (*string)(nil), []string{"Essential", "groceries"}, domain.MatchLogicTagOnly,
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
		CategoryKind: "pfc",
		PFCPrimary:   ptrStr("FOOD_AND_DRINK"),
		Tags:         []string{"Essential"},
		MatchLogic:   domain.MatchLogicAnd,
		StartDate:    "2024-01-01",
		EndDate:      "2024-01-31",
	}

	mockCalc.On("GetMatchingTransactionStats",
		ctx, userID, "pfc", ptrStr("FOOD_AND_DRINK"), (*string)(nil), (*string)(nil), []string{"Essential"}, domain.MatchLogicAnd,
		mock.Anything, mock.Anything,
	).Return(3, 150.00, nil)
	mockCalc.On("GetCategoryMatchBreakdown",
		ctx, userID, "pfc", ptrStr("FOOD_AND_DRINK"), (*string)(nil), (*string)(nil),
		mock.Anything, mock.Anything,
	).Return(nil, 0, 0.0, nil)

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
		CategoryKind: "pfc",
		PFCPrimary:   ptrStr("FOOD_AND_DRINK"),
		Tags:         []string{"Essential"},
		MatchLogic:   domain.MatchLogicOr,
		StartDate:    "2024-01-01",
		EndDate:      "2024-01-31",
	}

	mockCalc.On("GetMatchingTransactionStats",
		ctx, userID, "pfc", ptrStr("FOOD_AND_DRINK"), (*string)(nil), (*string)(nil), []string{"Essential"}, domain.MatchLogicOr,
		mock.Anything, mock.Anything,
	).Return(10, 500.00, nil)
	mockCalc.On("GetCategoryMatchBreakdown",
		ctx, userID, "pfc", ptrStr("FOOD_AND_DRINK"), (*string)(nil), (*string)(nil),
		mock.Anything, mock.Anything,
	).Return(nil, 0, 0.0, nil)

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
		CategoryKind: "pfc",
		PFCPrimary:   ptrStr("FOOD_AND_DRINK"),
		Tags:         []string{},
		MatchLogic:   "invalid_logic",
		StartDate:    "2024-01-01",
		EndDate:      "2024-01-31",
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
		CategoryKind: "pfc",
		PFCPrimary:   ptrStr("FOOD_AND_DRINK"),
		Tags:         []string{},
		MatchLogic:   domain.MatchLogicCategoryOnly,
		StartDate:    "invalid-date",
		EndDate:      "2024-01-31",
	}

	resp, err := service.PreviewCategoryMatches(ctx, userID, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "start_date")
}

// TestCategoryPreviewService_PreviewCategoryMatches_InvalidEndDate tests invalid end date
func TestCategoryPreviewService_PreviewCategoryMatches_InvalidEndDate(t *testing.T) {
	mockCalc := new(MockTransactionStatsCalculator)
	service := NewCategoryPreviewService(mockCalc)

	ctx := context.Background()
	userID := "user-123"
	req := &domain.CategoryPreviewRequest{
		CategoryKind: "pfc",
		PFCPrimary:   ptrStr("FOOD_AND_DRINK"),
		Tags:         []string{},
		MatchLogic:   domain.MatchLogicCategoryOnly,
		StartDate:    "2024-01-01",
		EndDate:      "invalid-date",
	}

	resp, err := service.PreviewCategoryMatches(ctx, userID, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "end_date")
}

// TestCategoryPreviewService_PreviewCategoryMatches_NoCategoryKindForCategoryOnly tests validation
func TestCategoryPreviewService_PreviewCategoryMatches_NoCategoryKindForCategoryOnly(t *testing.T) {
	mockCalc := new(MockTransactionStatsCalculator)
	service := NewCategoryPreviewService(mockCalc)

	ctx := context.Background()
	userID := "user-123"
	req := &domain.CategoryPreviewRequest{
		CategoryKind: "",
		Tags:         []string{},
		MatchLogic:   domain.MatchLogicCategoryOnly,
		StartDate:    "2024-01-01",
		EndDate:      "2024-01-31",
	}

	resp, err := service.PreviewCategoryMatches(ctx, userID, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "category_kind is required")
}

// TestCategoryPreviewService_PreviewCategoryMatches_NoTagsForTagOnly tests validation
func TestCategoryPreviewService_PreviewCategoryMatches_NoTagsForTagOnly(t *testing.T) {
	mockCalc := new(MockTransactionStatsCalculator)
	service := NewCategoryPreviewService(mockCalc)

	ctx := context.Background()
	userID := "user-123"
	req := &domain.CategoryPreviewRequest{
		CategoryKind: "",
		Tags:         []string{},
		MatchLogic:   domain.MatchLogicTagOnly,
		StartDate:    "2024-01-01",
		EndDate:      "2024-01-31",
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

	// Test missing category_kind
	req1 := &domain.CategoryPreviewRequest{
		CategoryKind: "",
		Tags:         []string{"Essential"},
		MatchLogic:   domain.MatchLogicAnd,
		StartDate:    "2024-01-01",
		EndDate:      "2024-01-31",
	}

	resp, err := service.PreviewCategoryMatches(ctx, userID, req1)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "category_kind is required")

	// Test missing tags
	req2 := &domain.CategoryPreviewRequest{
		CategoryKind: "pfc",
		PFCPrimary:   ptrStr("FOOD_AND_DRINK"),
		Tags:         []string{},
		MatchLogic:   domain.MatchLogicAnd,
		StartDate:    "2024-01-01",
		EndDate:      "2024-01-31",
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
		CategoryKind: "pfc",
		PFCPrimary:   ptrStr("FOOD_AND_DRINK"),
		Tags:         []string{},
		MatchLogic:   domain.MatchLogicCategoryOnly,
		StartDate:    "2024-01-01",
		EndDate:      "2024-01-31",
	}

	mockCalc.On("GetMatchingTransactionStats",
		ctx, userID, "pfc", ptrStr("FOOD_AND_DRINK"), (*string)(nil), (*string)(nil), []string{}, domain.MatchLogicCategoryOnly,
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
		CategoryKind: "pfc",
		PFCPrimary:   ptrStr("TRANSFER_IN"),
		Tags:         []string{},
		MatchLogic:   domain.MatchLogicCategoryOnly,
		StartDate:    "2024-01-01",
		EndDate:      "2024-01-31",
	}

	mockCalc.On("GetMatchingTransactionStats",
		ctx, userID, "pfc", ptrStr("TRANSFER_IN"), (*string)(nil), (*string)(nil), []string{}, domain.MatchLogicCategoryOnly,
		mock.Anything, mock.Anything,
	).Return(0, 0.0, nil)
	mockCalc.On("GetCategoryMatchBreakdown",
		ctx, userID, "pfc", ptrStr("TRANSFER_IN"), (*string)(nil), (*string)(nil),
		mock.Anything, mock.Anything,
	).Return(nil, 0, 0.0, nil)

	resp, err := service.PreviewCategoryMatches(ctx, userID, req)
	require.NoError(t, err)
	assert.Equal(t, 0, resp.TransactionCount)
	assert.Equal(t, 0.0, resp.TotalAmount)

	mockCalc.AssertExpectations(t)
}
