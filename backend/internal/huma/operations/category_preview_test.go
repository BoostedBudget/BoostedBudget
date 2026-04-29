package operations

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockCategoryPreviewService is a mock implementation of services.CategoryPreviewService
type mockCategoryPreviewService struct {
	response *domain.CategoryPreviewResponse
	err      error
}

func (m *mockCategoryPreviewService) PreviewCategoryMatches(ctx context.Context, userID string, req *domain.CategoryPreviewRequest) (*domain.CategoryPreviewResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}

// TestPreviewCategoryMatches_Success tests successful category preview.
func TestPreviewCategoryMatches_Success(t *testing.T) {
	mockService := &mockCategoryPreviewService{
		response: &domain.CategoryPreviewResponse{
			TransactionCount: 15,
			TotalAmount:      -250.50,
		},
	}

	handler := NewCategoryPreviewHandler(mockService)

	input := &PreviewCategoryMatchesInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body = domain.CategoryPreviewRequest{
		CategoryKind: "pfc",
		PFCPrimary:   ptrStr("FOOD_AND_DRINK"),
		MatchLogic:   domain.MatchLogicCategoryOnly,
		StartDate:    "2024-01-01",
		EndDate:      "2024-01-31",
	}

	output, err := handler.PreviewCategoryMatches(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, 15, output.Body.TransactionCount)
	assert.Equal(t, -250.50, output.Body.TotalAmount)
}

// TestPreviewCategoryMatches_WithTags tests category preview with tags.
func TestPreviewCategoryMatches_WithTags(t *testing.T) {
	mockService := &mockCategoryPreviewService{
		response: &domain.CategoryPreviewResponse{
			TransactionCount: 5,
			TotalAmount:      -100.00,
		},
	}

	handler := NewCategoryPreviewHandler(mockService)

	input := &PreviewCategoryMatchesInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body = domain.CategoryPreviewRequest{
		CategoryKind: "pfc",
		PFCPrimary:   ptrStr("FOOD_AND_DRINK"),
		Tags:         []string{"business-expense"},
		MatchLogic:   domain.MatchLogicAnd,
		StartDate:    "2024-01-01",
		EndDate:      "2024-01-31",
	}

	output, err := handler.PreviewCategoryMatches(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, 5, output.Body.TransactionCount)
}

// TestPreviewCategoryMatches_ValidationError tests validation error handling.
func TestPreviewCategoryMatches_ValidationError(t *testing.T) {
	mockService := &mockCategoryPreviewService{
		err: errors.New("invalid date range: start_date must be before end_date"),
	}

	handler := NewCategoryPreviewHandler(mockService)

	input := &PreviewCategoryMatchesInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body = domain.CategoryPreviewRequest{
		CategoryKind: "pfc",
		PFCPrimary:   ptrStr("FOOD_AND_DRINK"),
		MatchLogic:   domain.MatchLogicCategoryOnly,
		StartDate:    "2024-01-31",
		EndDate:      "2024-01-01", // End before start
	}

	output, err := handler.PreviewCategoryMatches(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid date range")
}

// TestPreviewCategoryMatches_EmptyCategories tests error when categories are empty.
func TestPreviewCategoryMatches_EmptyCategories(t *testing.T) {
	mockService := &mockCategoryPreviewService{
		err: errors.New("categories are required"),
	}

	handler := NewCategoryPreviewHandler(mockService)

	input := &PreviewCategoryMatchesInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body = domain.CategoryPreviewRequest{
		CategoryKind: "pfc",
		MatchLogic:   domain.MatchLogicCategoryOnly,
		StartDate:    "2024-01-01",
		EndDate:      "2024-01-31",
	}

	output, err := handler.PreviewCategoryMatches(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "categories are required")
}

// TestPreviewCategoryMatches_ZeroResults tests preview with no matching transactions.
func TestPreviewCategoryMatches_ZeroResults(t *testing.T) {
	mockService := &mockCategoryPreviewService{
		response: &domain.CategoryPreviewResponse{
			TransactionCount: 0,
			TotalAmount:      0,
		},
	}

	handler := NewCategoryPreviewHandler(mockService)

	input := &PreviewCategoryMatchesInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body = domain.CategoryPreviewRequest{
		CategoryKind: "pfc",
		PFCPrimary:   ptrStr("TRANSFER_IN"),
		MatchLogic:   domain.MatchLogicCategoryOnly,
		StartDate:    "2024-01-01",
		EndDate:      "2024-01-31",
	}

	output, err := handler.PreviewCategoryMatches(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, 0, output.Body.TransactionCount)
	assert.Equal(t, 0.0, output.Body.TotalAmount)
}

// TestCategoryPreviewRouteRegistration tests that routes are properly registered.
func TestCategoryPreviewRouteRegistration(t *testing.T) {
	mockService := &mockCategoryPreviewService{
		response: &domain.CategoryPreviewResponse{
			TransactionCount: 10,
			TotalAmount:      -150.00,
		},
	}

	_, api := humatest.New(t)
	handler := NewCategoryPreviewHandler(mockService)
	RegisterCategoryPreviewRoutes(api, handler)

	// Test POST /api/v1/budgets/categories/preview (without auth, should fail with 401)
	resp := api.Post("/api/v1/budgets/categories/preview", "application/json", map[string]interface{}{
		"categories":  [][]string{{"Food and Drink"}},
		"match_logic": "category_only",
		"start_date":  "2024-01-01",
		"end_date":    "2024-01-31",
	})
	assert.Equal(t, http.StatusUnauthorized, resp.Code)

	// Verify the response contains expected error message
	body := resp.Body.String()
	assert.Contains(t, body, "User not authenticated")
}

// TestCategoryPreviewResponseJSON tests JSON serialization of response.
func TestCategoryPreviewResponseJSON(t *testing.T) {
	mockService := &mockCategoryPreviewService{}

	_, api := humatest.New(t)
	handler := NewCategoryPreviewHandler(mockService)
	RegisterCategoryPreviewRoutes(api, handler)

	resp := api.Post("/api/v1/budgets/categories/preview", "application/json", map[string]interface{}{
		"categories":  [][]string{{"Food and Drink"}},
		"match_logic": "category_only",
		"start_date":  "2024-01-01",
		"end_date":    "2024-01-31",
	})
	assert.Equal(t, http.StatusUnauthorized, resp.Code)

	// Verify error response is valid JSON
	var errorResp map[string]interface{}
	err := json.Unmarshal(resp.Body.Bytes(), &errorResp)
	require.NoError(t, err, "Error response should be valid JSON")
}
