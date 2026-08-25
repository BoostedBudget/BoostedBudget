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

// Issue #1569 Task 6 (#1579) — the annotation trail's endpoint.
//
// The user's ruling on #1579 was "record before/after, smallest change, NO NEW UI". This route is
// not a UI: it is the only way the recorded before/after can be observed at all, and without it the
// capture would be a table nobody can read — precisely the state task 5 found rule_execution_history
// in. No frontend surface is added.

// TransactionAnnotationHistoryHandler serves the account-wide annotation-overwrite history.
type TransactionAnnotationHistoryHandler struct {
	service services.TransactionAnnotationHistoryService
}

// NewTransactionAnnotationHistoryHandler creates a TransactionAnnotationHistoryHandler.
func NewTransactionAnnotationHistoryHandler(service services.TransactionAnnotationHistoryService) *TransactionAnnotationHistoryHandler {
	return &TransactionAnnotationHistoryHandler{service: service}
}

// GetUserTransactionAnnotationHistoryInput is the account-wide annotation history request.
//
// Deliberately has NO transaction id path parameter. A change can outlive its transaction — the
// row carries no FK, so a purged transaction leaves its history behind — and a per-transaction
// route would make exactly those rows unreachable.
type GetUserTransactionAnnotationHistoryInput struct {
	inputs.AuthenticatedInput
	Limit  int `query:"limit" default:"20" minimum:"1" maximum:"100" doc:"Maximum number of results"`
	Offset int `query:"offset" default:"0" minimum:"0" doc:"Number of results to skip"`
}

// GetUserTransactionAnnotationHistoryOutput is the annotation history response.
type GetUserTransactionAnnotationHistoryOutput struct {
	Body *domain.TransactionAnnotationHistoryResponse
}

// GetUserTransactionAnnotationHistory handles GET /api/v1/transaction-annotation-history.
//
// Tenancy is the authenticated user id from AuthenticatedInput, passed straight to the service,
// which filters on the history row's own denormalised user_id. Nothing client-supplied reaches that
// filter — there is no id parameter for it to reach.
func (h *TransactionAnnotationHistoryHandler) GetUserTransactionAnnotationHistory(
	ctx context.Context,
	input *GetUserTransactionAnnotationHistoryInput,
) (*GetUserTransactionAnnotationHistoryOutput, error) {
	if h.service == nil {
		return nil, huma.Error500InternalServerError("Transaction annotation history service not available")
	}

	changes, total, err := h.service.GetUserAnnotationHistory(ctx, input.UserID, input.Limit, input.Offset)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	if changes == nil {
		changes = []*domain.TransactionAnnotationChange{}
	}

	return &GetUserTransactionAnnotationHistoryOutput{
		Body: &domain.TransactionAnnotationHistoryResponse{
			Changes: changes,
			Total:   total,
			Limit:   input.Limit,
			Offset:  input.Offset,
		},
	}, nil
}

// RegisterTransactionAnnotationHistoryRoutes registers the annotation history route.
// Note: this route requires the Clerk middleware to be applied.
//
// ⚠ THE PATH IS A TOP-LEVEL SIBLING, NOT A CHILD OF /api/v1/transactions, and that is deliberate.
// /api/v1/transactions/{id} already exists, so /api/v1/transactions/annotation-history would share
// a prefix with a {param} sibling. Production is humaecho.NewV4 (Echo v4's radix tree prefers a
// static segment in either registration order) but humatest — which every router test in this
// package uses — is registration-order sensitive; both are measured in
// TestRouterPrecedence_ProductionEchoAlwaysPrefersTheLiteralSegment and
// TestRouterPrecedence_HumatestIsRegistrationOrderSensitive (cmd/server). The hyphenated sibling
// sidesteps the difference and matches the convention already set by /api/v1/rule-executions,
// /api/v1/boost-executions and /api/v1/savings-goals.
//
// Reachability is asserted at the ROUTER level by
// TestTransactionAnnotationHistoryRoutes_PathIsReachableAndNotSwallowedByATransactionIDWildcard —
// a handler test is not evidence that a route resolves.
func RegisterTransactionAnnotationHistoryRoutes(api huma.API, handler *TransactionAnnotationHistoryHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "getUserTransactionAnnotationHistory",
		Method:      http.MethodGet,
		Path:        "/api/v1/transaction-annotation-history",
		Summary:     "Get transaction annotation change history",
		Description: "Retrieves the before/after record of every category or review-state overwrite " +
			"the authenticated user made on a transaction, newest first. A row exists for each write " +
			"that actually changed a tracked value; re-writing the identical value records nothing. " +
			"change_source distinguishes a recategorisation ('category') — which also rewrites the " +
			"review state as a side effect — from a review-state write ('review'). Changes made to a " +
			"transaction that has since been removed are still returned.",
		Tags:     []string{"Transactions"},
		Security: []map[string][]string{{"clerk": {}}},
	}, handler.GetUserTransactionAnnotationHistory)
}
