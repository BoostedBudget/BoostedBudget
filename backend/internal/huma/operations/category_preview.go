package operations

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// =============================================================================
// Issue #497: Category Preview Handler
// =============================================================================

// CategoryPreviewHandler handles category preview HTTP requests.
type CategoryPreviewHandler struct {
	service services.CategoryPreviewService
}

// NewCategoryPreviewHandler creates a new CategoryPreviewHandler instance.
func NewCategoryPreviewHandler(service services.CategoryPreviewService) *CategoryPreviewHandler {
	return &CategoryPreviewHandler{service: service}
}

// --- Input/Output Types ---

// PreviewCategoryMatchesInput represents the input for previewing category matches.
type PreviewCategoryMatchesInput struct {
	inputs.AuthenticatedInput
	Body domain.CategoryPreviewRequest
}

// PreviewCategoryMatchesOutput represents the response for category preview.
type PreviewCategoryMatchesOutput struct {
	Body *domain.CategoryPreviewResponse
}

// --- Handler Methods ---

// PreviewCategoryMatches handles POST /api/v1/budgets/categories/preview
// Returns the count and total amount of transactions matching the given category config.
func (h *CategoryPreviewHandler) PreviewCategoryMatches(ctx context.Context, input *PreviewCategoryMatchesInput) (*PreviewCategoryMatchesOutput, error) {
	resp, err := h.service.PreviewCategoryMatches(ctx, input.UserID, &input.Body)
	if err != nil {
		// Return validation errors as 400 Bad Request
		return nil, huma.Error400BadRequest(err.Error())
	}

	return &PreviewCategoryMatchesOutput{Body: resp}, nil
}

// RegisterCategoryPreviewRoutes registers category preview routes with the Huma API.
// Note: These routes require the Clerk middleware to be applied.
func RegisterCategoryPreviewRoutes(api huma.API, handler *CategoryPreviewHandler) {
	// POST /api/v1/budgets/categories/preview - Preview category matches
	huma.Register(api, huma.Operation{
		OperationID: "previewCategoryMatches",
		Method:      http.MethodPost,
		Path:        "/api/v1/budgets/categories/preview",
		Summary:     "Preview category matches",
		Description: "Returns the count and total amount of transactions matching the given category configuration within the date range.",
		Tags:        []string{"Budget"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.PreviewCategoryMatches)
}
