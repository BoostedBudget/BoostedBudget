// Package operations provides HTTP handler implementations for transfer suggestion endpoints.
// Issue #952: Notification Center — Transfer Suggestion API
package operations

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	humaerrors "github.com/maxnussbaum/BoostedBudget/internal/huma/errors"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// TransferSuggestionHandler handles transfer suggestion HTTP requests.
type TransferSuggestionHandler struct {
	service services.TransferSuggestionService
}

// NewTransferSuggestionHandler creates a new TransferSuggestionHandler.
func NewTransferSuggestionHandler(service services.TransferSuggestionService) *TransferSuggestionHandler {
	return &TransferSuggestionHandler{service: service}
}

// --- Input / Output types ---

// GetTransferSuggestionsInput is the input for listing pending transfer suggestions.
type GetTransferSuggestionsInput struct {
	inputs.AuthenticatedInput
}

// GetTransferSuggestionsOutput is the response for listing pending transfer suggestions.
type GetTransferSuggestionsOutput struct {
	Body []*domain.TransferSuggestion
}

// AcceptTransferSuggestionInput is the input for accepting a transfer suggestion.
type AcceptTransferSuggestionInput struct {
	inputs.AuthenticatedInput
	ID   string `path:"id" doc:"Transfer suggestion UUID"`
	Body struct {
		Notes *string `json:"notes,omitempty" doc:"Optional notes for accepting the suggestion"`
	}
}

// AcceptTransferSuggestionOutput is the response for accepting a transfer suggestion.
type AcceptTransferSuggestionOutput struct {
	Body struct {
		Message    string                    `json:"message"`
		Suggestion *domain.TransferSuggestion `json:"suggestion"`
	}
}

// RejectTransferSuggestionInput is the input for rejecting a transfer suggestion.
type RejectTransferSuggestionInput struct {
	inputs.AuthenticatedInput
	ID   string `path:"id" doc:"Transfer suggestion UUID"`
	Body struct {
		Notes *string `json:"notes,omitempty" doc:"Optional notes for rejecting the suggestion"`
	}
}

// RejectTransferSuggestionOutput is the response for rejecting a transfer suggestion.
type RejectTransferSuggestionOutput struct {
	Body struct {
		Message    string                    `json:"message"`
		Suggestion *domain.TransferSuggestion `json:"suggestion"`
	}
}

// --- Handler Methods ---

// GetTransferSuggestions handles GET /api/v1/transfer-suggestions.
// Returns all pending transfer suggestions for the authenticated user.
func (h *TransferSuggestionHandler) GetTransferSuggestions(ctx context.Context, input *GetTransferSuggestionsInput) (*GetTransferSuggestionsOutput, error) {
	suggestions, err := h.service.GetPendingSuggestions(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	// Return empty slice instead of nil so JSON serialises as [] not null.
	if suggestions == nil {
		suggestions = []*domain.TransferSuggestion{}
	}

	return &GetTransferSuggestionsOutput{Body: suggestions}, nil
}

// AcceptTransferSuggestion handles PATCH /api/v1/transfer-suggestions/{id}/accept.
func (h *TransferSuggestionHandler) AcceptTransferSuggestion(ctx context.Context, input *AcceptTransferSuggestionInput) (*AcceptTransferSuggestionOutput, error) {
	suggestion, err := h.service.AcceptSuggestion(ctx, input.UserID, input.ID, input.Body.Notes)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	output := &AcceptTransferSuggestionOutput{}
	output.Body.Message = "Transfer suggestion accepted"
	output.Body.Suggestion = suggestion
	return output, nil
}

// RejectTransferSuggestion handles PATCH /api/v1/transfer-suggestions/{id}/reject.
func (h *TransferSuggestionHandler) RejectTransferSuggestion(ctx context.Context, input *RejectTransferSuggestionInput) (*RejectTransferSuggestionOutput, error) {
	suggestion, err := h.service.RejectSuggestion(ctx, input.UserID, input.ID, input.Body.Notes)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	output := &RejectTransferSuggestionOutput{}
	output.Body.Message = "Transfer suggestion rejected"
	output.Body.Suggestion = suggestion
	return output, nil
}

// RegisterTransferSuggestionRoutes registers transfer suggestion routes with the Huma API.
func RegisterTransferSuggestionRoutes(api huma.API, handler *TransferSuggestionHandler) {
	// GET /api/v1/transfer-suggestions — list pending suggestions for authenticated user
	huma.Register(api, huma.Operation{
		OperationID: "getTransferSuggestions",
		Method:      http.MethodGet,
		Path:        "/api/v1/transfer-suggestions",
		Summary:     "Get pending transfer suggestions",
		Description: "Returns all pending transfer suggestions for the authenticated user.",
		Tags:        []string{"TransferSuggestions"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetTransferSuggestions)

	// PATCH /api/v1/transfer-suggestions/{id}/accept — accept a suggestion
	huma.Register(api, huma.Operation{
		OperationID: "acceptTransferSuggestion",
		Method:      http.MethodPatch,
		Path:        "/api/v1/transfer-suggestions/{id}/accept",
		Summary:     "Accept a transfer suggestion",
		Description: "Marks a pending transfer suggestion as accepted. Returns 404 if not found, 409 if already actioned.",
		Tags:        []string{"TransferSuggestions"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.AcceptTransferSuggestion)

	// PATCH /api/v1/transfer-suggestions/{id}/reject — reject a suggestion
	huma.Register(api, huma.Operation{
		OperationID: "rejectTransferSuggestion",
		Method:      http.MethodPatch,
		Path:        "/api/v1/transfer-suggestions/{id}/reject",
		Summary:     "Reject a transfer suggestion",
		Description: "Marks a pending transfer suggestion as rejected. Returns 404 if not found, 409 if already actioned.",
		Tags:        []string{"TransferSuggestions"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.RejectTransferSuggestion)
}
