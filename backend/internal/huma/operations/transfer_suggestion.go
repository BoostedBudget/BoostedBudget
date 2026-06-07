// Package operations provides HTTP handler implementations for transfer suggestion endpoints.
// Issue #952: Notification Center — Transfer Suggestion API
// Issue #972: C3 — confirm-match MCP-ready write endpoint
package operations

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	humaerrors "github.com/maxnussbaum/BoostedBudget/internal/huma/errors"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// TransferSuggestionHandlerDeps holds optional dependencies for MCP write audit support.
type TransferSuggestionHandlerDeps struct {
	IdempotencyRepo repository.IdempotencyRepository
	DB              repository.DB
	MCPAuditRepo    repository.MCPAuditRepository
	MCPWorkerSecret string
}

// TransferSuggestionHandler handles transfer suggestion HTTP requests.
type TransferSuggestionHandler struct {
	service         services.TransferSuggestionService
	idempotencyRepo repository.IdempotencyRepository
	db              repository.DB
	mcpAuditRepo    repository.MCPAuditRepository
	mcpWorkerSecret string
	analytics       phclient.Analytics
}

// SetAnalytics sets the analytics client for event capture.
func (h *TransferSuggestionHandler) SetAnalytics(a phclient.Analytics) {
	h.analytics = a
}

// NewTransferSuggestionHandler creates a new TransferSuggestionHandler.
// deps are optional (pass nil/omit for spec-gen nil-stubs); when non-nil,
// MCP write operations are audited transactionally.
func NewTransferSuggestionHandler(service services.TransferSuggestionService, deps ...TransferSuggestionHandlerDeps) *TransferSuggestionHandler {
	h := &TransferSuggestionHandler{service: service}
	if len(deps) > 0 {
		h.idempotencyRepo = deps[0].IdempotencyRepo
		h.db = deps[0].DB
		h.mcpAuditRepo = deps[0].MCPAuditRepo
		h.mcpWorkerSecret = deps[0].MCPWorkerSecret
	}
	return h
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

// maskReservation clears matched_transaction_id from non-completed suggestions
// before including in API response. This is an internal detail (GAP AH).
func maskReservation(s *domain.TransferSuggestion) *domain.TransferSuggestion {
	if s != nil && s.Status != domain.SuggestionStatusCompleted {
		s.MatchedTransactionID = nil
	}
	return s
}

// --- Handler Methods ---

// GetTransferSuggestions handles GET /api/v1/transfer-suggestions.
// Returns all pending transfer suggestions for the authenticated user.
func (h *TransferSuggestionHandler) GetTransferSuggestions(ctx context.Context, input *GetTransferSuggestionsInput) (*GetTransferSuggestionsOutput, error) {
	suggestions, err := h.service.GetPendingSuggestions(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	if suggestions == nil {
		suggestions = []*domain.TransferSuggestion{}
	}

	for i := range suggestions {
		maskReservation(suggestions[i])
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
	output.Body.Suggestion = maskReservation(suggestion)
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
	output.Body.Suggestion = maskReservation(suggestion)
	return output, nil
}

// DeclineTransferSuggestionMatchInput is the input for declining a fuzzy-matched suggestion.
type DeclineTransferSuggestionMatchInput struct {
	inputs.AuthenticatedInput
	inputs.IdempotencyInput
	inputs.MCPContextInput
	ID string `path:"id" doc:"Transfer suggestion UUID to decline"`
}

// DeclineTransferSuggestionMatchOutput is the response for declining a transfer suggestion match.
type DeclineTransferSuggestionMatchOutput struct {
	Body struct {
		Message    string                     `json:"message"`
		Suggestion *domain.TransferSuggestion `json:"suggestion"`
	}
}

// ConfirmMatchBody is the request body for confirming a transfer suggestion match.
type ConfirmMatchBody struct {
	ActualAmount float64 `json:"actual_amount" minimum:"0.01" doc:"Actual amount transferred"`
}

// ConfirmTransferSuggestionMatchInput is the input for confirming a fuzzy-matched suggestion.
type ConfirmTransferSuggestionMatchInput struct {
	inputs.AuthenticatedInput
	inputs.IdempotencyInput
	inputs.MCPContextInput
	ID   string           `path:"id" doc:"Transfer suggestion UUID to confirm"`
	Body ConfirmMatchBody
}

// ConfirmTransferSuggestionMatchOutput is the response for confirming a transfer suggestion match.
type ConfirmTransferSuggestionMatchOutput struct {
	Body struct {
		Message    string                     `json:"message"`
		Suggestion *domain.TransferSuggestion `json:"suggestion"`
	}
}

// ConfirmMatch handles POST /api/v1/transfer-suggestions/{id}/confirm-match.
// Confirms a fuzzy-matched transfer suggestion with the actual transfer amount.
// Supports MCP audit path when X-MCP-Worker-Secret is present and valid.
func (h *TransferSuggestionHandler) ConfirmMatch(ctx context.Context, input *ConfirmTransferSuggestionMatchInput) (*ConfirmTransferSuggestionMatchOutput, error) {
	fireConfirmAnalytics := func() {
		if h.analytics != nil {
			h.analytics.Capture(input.UserID, phclient.EventTransferSuggestionValidated, map[string]interface{}{
				"suggestion_id": input.ID,
			})
		}
	}

	// buildOutput constructs the response and conditionally fires analytics.
	// GAP AS: fireAnalytics param prevents double-firing EventTransferSuggestionValidated
	// on MCP idempotency-key cache-hit replays. Event fires only on first execution.
	buildOutput := func(suggestion *domain.TransferSuggestion, fireAnalytics bool) *ConfirmTransferSuggestionMatchOutput {
		output := &ConfirmTransferSuggestionMatchOutput{}
		output.Body.Message = "Transfer suggestion confirmed"
		output.Body.Suggestion = suggestion
		if fireAnalytics {
			fireConfirmAnalytics()
		}
		return output
	}

	// MCP origin: use transactional audit with idempotency support.
	if input.IsMCPOrigin(h.mcpWorkerSecret) && h.db != nil && h.mcpAuditRepo != nil {
		tx, err := h.db.Begin(ctx)
		if err != nil {
			return nil, humaerrors.LoggingMapError(ctx, err)
		}
		defer rollbackMCPAuditTx(ctx, tx)

		if h.idempotencyRepo != nil && input.IdempotencyKey != "" {
			var suggestion *domain.TransferSuggestion
			cacheHit, err := h.idempotencyRepo.GetOrCreateInTx(ctx, tx, input.IdempotencyKey, input.UserID, "confirmTransferSuggestionMatch",
				func() (interface{}, error) {
					txSuggestionRepo := repository.NewTransferSuggestionRepository(tx)
					txSvc := services.NewTransferSuggestionService(txSuggestionRepo)
					return txSvc.ConfirmTransferSuggestionMatch(ctx, input.UserID, input.ID, input.Body.ActualAmount)
				}, &suggestion)
			if err != nil {
				return nil, humaerrors.LoggingMapError(ctx, err)
			}
			if cacheHit {
				// GAP AS: cache-hit replay — do NOT re-fire analytics (event already fired on first exec).
				return buildOutput(suggestion, false), nil
			}
			if err := recordMCPWriteAuditInTx(ctx, h.mcpAuditRepo, tx, input.UserID, input.MCPGrantID, "confirmTransferSuggestionMatch", input.IdempotencyKey); err != nil {
				return nil, humaerrors.LoggingMapError(ctx, err)
			}
			if err := tx.Commit(ctx); err != nil {
				return nil, humaerrors.LoggingMapError(ctx, err)
			}
			return buildOutput(suggestion, true), nil
		}

		txSuggestionRepo := repository.NewTransferSuggestionRepository(tx)
		txSvc := services.NewTransferSuggestionService(txSuggestionRepo)
		suggestion, err := txSvc.ConfirmTransferSuggestionMatch(ctx, input.UserID, input.ID, input.Body.ActualAmount)
		if err != nil {
			return nil, humaerrors.LoggingMapError(ctx, err)
		}
		if err := recordMCPWriteAuditInTx(ctx, h.mcpAuditRepo, tx, input.UserID, input.MCPGrantID, "confirmTransferSuggestionMatch", input.IdempotencyKey); err != nil {
			return nil, humaerrors.LoggingMapError(ctx, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, humaerrors.LoggingMapError(ctx, err)
		}
		return buildOutput(suggestion, true), nil
	}

	// Non-MCP path: call service directly.
	suggestion, err := h.service.ConfirmTransferSuggestionMatch(ctx, input.UserID, input.ID, input.Body.ActualAmount)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return buildOutput(suggestion, true), nil
}

// DeclineMatch handles POST /api/v1/transfer-suggestions/{id}/decline-match.
// Releases the fuzzy-match reservation on a pending/accepted suggestion so the inflow
// can be matched by a different suggestion. User chose "No, that wasn't my transfer."
// Supports MCP audit path when X-MCP-Worker-Secret is present and valid.
func (h *TransferSuggestionHandler) DeclineMatch(ctx context.Context, input *DeclineTransferSuggestionMatchInput) (*DeclineTransferSuggestionMatchOutput, error) {
	fireDeclineAnalytics := func() {
		if h.analytics != nil {
			h.analytics.Capture(input.UserID, phclient.EventTransferSuggestionValidated, map[string]interface{}{
				"suggestion_id": input.ID,
				"action":        "declined",
			})
		}
	}

	// buildOutput constructs the response and conditionally fires analytics.
	// GAP AS: fireAnalytics param prevents double-firing on MCP idempotency-key cache-hit replays.
	buildOutput := func(suggestion *domain.TransferSuggestion, fireAnalytics bool) *DeclineTransferSuggestionMatchOutput {
		output := &DeclineTransferSuggestionMatchOutput{}
		output.Body.Message = "Transfer match declined"
		output.Body.Suggestion = maskReservation(suggestion)
		if fireAnalytics {
			fireDeclineAnalytics()
		}
		return output
	}

	// MCP origin: use transactional audit with idempotency support.
	if input.IsMCPOrigin(h.mcpWorkerSecret) && h.db != nil && h.mcpAuditRepo != nil {
		tx, err := h.db.Begin(ctx)
		if err != nil {
			return nil, humaerrors.LoggingMapError(ctx, err)
		}
		defer rollbackMCPAuditTx(ctx, tx)

		if h.idempotencyRepo != nil && input.IdempotencyKey != "" {
			var suggestion *domain.TransferSuggestion
			cacheHit, err := h.idempotencyRepo.GetOrCreateInTx(ctx, tx, input.IdempotencyKey, input.UserID, "declineTransferSuggestionMatch",
				func() (interface{}, error) {
					txSuggestionRepo := repository.NewTransferSuggestionRepository(tx)
					txSvc := services.NewTransferSuggestionService(txSuggestionRepo)
					return txSvc.DeclineTransferSuggestionMatch(ctx, input.UserID, input.ID)
				}, &suggestion)
			if err != nil {
				return nil, humaerrors.LoggingMapError(ctx, err)
			}
			if cacheHit {
				// GAP AS: cache-hit replay — do not re-fire analytics.
				return buildOutput(suggestion, false), nil
			}
			if err := recordMCPWriteAuditInTx(ctx, h.mcpAuditRepo, tx, input.UserID, input.MCPGrantID, "declineTransferSuggestionMatch", input.IdempotencyKey); err != nil {
				return nil, humaerrors.LoggingMapError(ctx, err)
			}
			if err := tx.Commit(ctx); err != nil {
				return nil, humaerrors.LoggingMapError(ctx, err)
			}
			return buildOutput(suggestion, true), nil
		}

		txSuggestionRepo := repository.NewTransferSuggestionRepository(tx)
		txSvc := services.NewTransferSuggestionService(txSuggestionRepo)
		suggestion, err := txSvc.DeclineTransferSuggestionMatch(ctx, input.UserID, input.ID)
		if err != nil {
			return nil, humaerrors.LoggingMapError(ctx, err)
		}
		if err := recordMCPWriteAuditInTx(ctx, h.mcpAuditRepo, tx, input.UserID, input.MCPGrantID, "declineTransferSuggestionMatch", input.IdempotencyKey); err != nil {
			return nil, humaerrors.LoggingMapError(ctx, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, humaerrors.LoggingMapError(ctx, err)
		}
		return buildOutput(suggestion, true), nil
	}

	// Non-MCP path: call service directly.
	suggestion, err := h.service.DeclineTransferSuggestionMatch(ctx, input.UserID, input.ID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	return buildOutput(suggestion, true), nil
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

	// POST /api/v1/transfer-suggestions/{id}/confirm-match — confirm a fuzzy-match suggestion
	huma.Register(api, huma.Operation{
		OperationID:   "confirmTransferSuggestionMatch",
		Method:        http.MethodPost,
		Path:          "/api/v1/transfer-suggestions/{id}/confirm-match",
		Summary:       "Confirm transfer suggestion match",
		Description:   "Confirms a fuzzy-matched transfer suggestion with the actual transfer amount. For MCP use.",
		Tags:          []string{"TransferSuggestions"},
		Security:      []map[string][]string{{"clerk": {}}},
		DefaultStatus: http.StatusOK,
	}, handler.ConfirmMatch)

	// POST /api/v1/transfer-suggestions/{id}/decline-match — release fuzzy-match reservation
	huma.Register(api, huma.Operation{
		OperationID:   "declineTransferSuggestionMatch",
		Method:        http.MethodPost,
		Path:          "/api/v1/transfer-suggestions/{id}/decline-match",
		Summary:       "Decline transfer suggestion match",
		Description:   "Releases the fuzzy-match reservation on a pending/accepted suggestion. The inflow is freed for other suggestions.",
		Tags:          []string{"TransferSuggestions"},
		Security:      []map[string][]string{{"clerk": {}}},
		DefaultStatus: http.StatusOK,
	}, handler.DeclineMatch)
}
