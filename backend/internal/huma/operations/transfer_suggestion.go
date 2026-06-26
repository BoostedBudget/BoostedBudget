// Package operations provides HTTP handler implementations for transfer suggestion endpoints.
// Issue #952: Notification Center — Transfer Suggestion API
// Issue #972: C3 — confirm-match MCP-ready write endpoint
package operations

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strconv"

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

// GetTransferSuggestionsInput is the input for listing transfer suggestions.
// When Status is empty, returns pending-only (backward compat with notification center).
type GetTransferSuggestionsInput struct {
	inputs.AuthenticatedInput
	// Status is an optional comma-separated list of suggestion statuses to filter by.
	// Valid values: pending, accepted, rejected, expired, completed.
	// When omitted, returns all pending suggestions (backward compat).
	Status string `query:"status" doc:"Comma-separated list of statuses to filter by (e.g. completed,rejected,expired). Omit for pending-only." required:"false"`
	// Limit is the maximum number of suggestions to return (1-100, default 20).
	Limit int `query:"limit" default:"20" minimum:"1" maximum:"100" doc:"Maximum number of suggestions to return."`
	// Offset is the number of suggestions to skip for pagination (default 0).
	Offset int `query:"offset" default:"0" minimum:"0" doc:"Number of suggestions to skip for pagination."`
}

// TransferSuggestionListResponse is the paginated envelope for listing transfer suggestions.
type TransferSuggestionListResponse struct {
	Items  []*domain.TransferSuggestion `json:"items" doc:"List of transfer suggestions"`
	Total  int                          `json:"total" doc:"Total number of matching suggestions across all pages"`
	Limit  int                          `json:"limit" doc:"Maximum results per page as requested"`
	Offset int                          `json:"offset" doc:"Number of results skipped"`
}

// GetTransferSuggestionsOutput is the response for listing transfer suggestions.
type GetTransferSuggestionsOutput struct {
	Body TransferSuggestionListResponse
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
// When no status param is provided, returns pending suggestions (backward compat with
// notification center). When status is provided as a CSV, filters by those statuses
// and returns a paginated envelope with total count.
func (h *TransferSuggestionHandler) GetTransferSuggestions(ctx context.Context, input *GetTransferSuggestionsInput) (*GetTransferSuggestionsOutput, error) {
	var items []*domain.TransferSuggestion
	var total int

	if input.Status == "" {
		// Legacy path: no status param → pending only (notification center compat).
		pending, err := h.service.GetPendingSuggestions(ctx, input.UserID)
		if err != nil {
			return nil, humaerrors.LoggingMapError(ctx, err)
		}
		if pending == nil {
			pending = []*domain.TransferSuggestion{}
		}
		items = pending
		total = len(pending)
	} else {
		// Filtered path: parse and validate the CSV status list.
		statuses, err := parseSuggestionStatuses(input.Status)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity(err.Error())
		}
		filtered, t, err := h.service.GetSuggestionsByStatus(ctx, input.UserID, statuses, input.Limit, input.Offset)
		if err != nil {
			return nil, humaerrors.LoggingMapError(ctx, err)
		}
		if filtered == nil {
			filtered = []*domain.TransferSuggestion{}
		}
		items = filtered
		total = t
	}

	for i := range items {
		// Compute server-derived fields BEFORE masking clears matched_transaction_id.
		if ms := items[i].ComputeMatchState(); ms != "" {
			items[i].MatchState = &ms
		}
		maskReservation(items[i])
	}

	limit := input.Limit
	if limit == 0 {
		limit = 20
	}

	return &GetTransferSuggestionsOutput{Body: TransferSuggestionListResponse{
		Items:  items,
		Total:  total,
		Limit:  limit,
		Offset: input.Offset,
	}}, nil
}

// parseSuggestionStatuses parses a comma-separated status string and validates each value.
// Returns a 422-ready error if any value is unknown.
func parseSuggestionStatuses(csv string) ([]domain.SuggestionStatus, error) {
	parts := splitCSV(csv)
	statuses := make([]domain.SuggestionStatus, 0, len(parts))
	for _, p := range parts {
		s := domain.SuggestionStatus(p)
		if !s.IsValid() {
			return nil, fmt.Errorf("invalid status %q: must be one of pending, accepted, rejected, expired, completed", p)
		}
		statuses = append(statuses, s)
	}
	return statuses, nil
}

// splitCSV splits a comma-separated string into trimmed, non-empty parts.
func splitCSV(s string) []string {
	var parts []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			part := ""
			for j := start; j < i; j++ {
				if s[j] != ' ' {
					part += string(s[j])
				}
			}
			if part != "" {
				parts = append(parts, part)
			}
			start = i + 1
		}
	}
	return parts
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
	// GAP CG: round actual_amount to 2dp once so that the idempotency-key fold and the
	// service call always use the same canonical value. Without this, sub-cent inputs like
	// 250.751 and 250.754 fold to the same ":250.75" key but call the service with
	// different raw floats, causing the second call's cached body to reflect the wrong amount.
	input.Body.ActualAmount = math.Round(input.Body.ActualAmount*100) / 100

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
			// GAP BB/BC: scope the key to (suggestion ID, actual_amount) so that:
			//   • same base key targeting different suggestions → different entries (BB)
			//   • same base key + same suggestion + different actual_amount → different
			//     entries, forcing re-validation instead of replaying a cached body (BC)
			// The ":" delimiter is safe: suggestion IDs are UUIDs (no ":" chars), and
			// FormatFloat with 'f' precision 2 only emits digits and ".".
			scopedKey := input.IdempotencyKey + ":" + input.ID +
				":" + strconv.FormatFloat(input.Body.ActualAmount, 'f', 2, 64)
			cacheHit, err := h.idempotencyRepo.GetOrCreateInTx(ctx, tx, scopedKey, input.UserID, "confirmTransferSuggestionMatch",
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
			// GAP BB: scope the key to the suggestion ID (mirrors confirm path above).
			// ":" delimiter is safe because suggestion IDs are UUIDs (no ":" chars).
			scopedKey := input.IdempotencyKey + ":" + input.ID
			cacheHit, err := h.idempotencyRepo.GetOrCreateInTx(ctx, tx, scopedKey, input.UserID, "declineTransferSuggestionMatch",
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

// CancelTransferSuggestionInput is the input for cancelling a pending/accepted suggestion.
type CancelTransferSuggestionInput struct {
	inputs.AuthenticatedInput
	ID   string `path:"id" doc:"Transfer suggestion UUID to cancel"`
	Body struct {
		Notes *string `json:"notes,omitempty" doc:"Optional notes for the cancellation"`
	}
}

// CancelTransferSuggestionOutput is the response for cancelling a transfer suggestion.
type CancelTransferSuggestionOutput struct {
	Body struct {
		Message    string                     `json:"message"`
		Suggestion *domain.TransferSuggestion `json:"suggestion"`
	}
}

// CancelSuggestion handles POST /api/v1/transfer-suggestions/{id}/cancel.
// Transitions a pending or accepted suggestion to rejected so it leaves the Awaiting Match tab.
// Distinct from decline-match, which only clears the fuzzy-match reservation.
func (h *TransferSuggestionHandler) CancelSuggestion(ctx context.Context, input *CancelTransferSuggestionInput) (*CancelTransferSuggestionOutput, error) {
	suggestion, err := h.service.CancelAcceptedSuggestion(ctx, input.UserID, input.ID, input.Body.Notes)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	output := &CancelTransferSuggestionOutput{}
	output.Body.Message = "Transfer suggestion cancelled"
	output.Body.Suggestion = maskReservation(suggestion)
	return output, nil
}

// RegisterTransferSuggestionRoutes registers transfer suggestion routes with the Huma API.
func RegisterTransferSuggestionRoutes(api huma.API, handler *TransferSuggestionHandler) {
	// GET /api/v1/transfer-suggestions — list suggestions for authenticated user (filterable by status)
	huma.Register(api, huma.Operation{
		OperationID: "getTransferSuggestions",
		Method:      http.MethodGet,
		Path:        "/api/v1/transfer-suggestions",
		Summary:     "List transfer suggestions (filter by status)",
		Description: "Returns transfer suggestions for the authenticated user. " +
			"Use the `status` query parameter (comma-separated) to filter by status. " +
			"Valid values: pending, accepted, completed, rejected, expired. " +
			"When omitted, returns only pending suggestions (backward-compatible default). " +
			"Supports `limit` (1–100, default 20) and `offset` pagination.",
		Tags:     []string{"TransferSuggestions"},
		Security: []map[string][]string{{"clerk": {}}},
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

	// POST /api/v1/transfer-suggestions/{id}/cancel — cancel a pending/accepted suggestion
	huma.Register(api, huma.Operation{
		OperationID:   "cancelTransferSuggestion",
		Method:        http.MethodPost,
		Path:          "/api/v1/transfer-suggestions/{id}/cancel",
		Summary:       "Cancel a transfer suggestion",
		Description:   "Cancels a pending or accepted suggestion, transitioning it to rejected so it leaves the Awaiting Match tab. Distinct from decline-match (which only clears the fuzzy-match reservation without changing status).",
		Tags:          []string{"TransferSuggestions"},
		Security:      []map[string][]string{{"clerk": {}}},
		DefaultStatus: http.StatusOK,
	}, handler.CancelSuggestion)
}
