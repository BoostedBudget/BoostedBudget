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
		// ⚠ THIS DESCRIPTION IS THE ONLY DOCUMENTATION THIS ENDPOINT HAS. There is no frontend
		// renderer for the annotation trail, so a consumer's entire understanding of change_source
		// comes from the text below — which makes an incomplete enumeration here a different class
		// of defect from an incomplete Go comment. It shipped naming two of five values; commit
		// 238a69c0 corrected the domain COMMENT and left this untouched, because only one of the two
		// had a test. TestAnnotationHistoryDescriptionEnumeratesEveryChangeSource now derives the
		// expected values from the domain constants (themselves pinned to pg_constraint by
		// TestAnnotationChangeSourcesMatchTheLiveCheck), so a sixth source fails on the day it is
		// declared. ⚠ Editing this string requires `make generate-openapi` — the published
		// api/openapi.yaml is what consumers read, not this file.
		Description: "Retrieves the before/after record of every change to a transaction's category, " +
			"review state or tags for the authenticated user, newest first. A row exists for each " +
			"write that actually changed a tracked value; re-writing the identical value records " +
			"nothing. Changes made to a transaction that has since been removed are still returned.\n\n" +
			"`change_source` answers *what was asked for*, and has five values. Two are the user " +
			"acting on their own transaction: 'category' — a recategorisation, which ALSO rewrites " +
			"`reviewed`/`reviewed_at` as a side effect, so a review timestamp can be lost by a write " +
			"the user thought was only about the category; and 'review' — a review-state write, " +
			"single or bulk, which leaves the category alone. 'tags' is a whole-array tag replace: " +
			"every tag dropped is gone the moment it returns and the previous set survives nowhere " +
			"else, so `prior_tags` here is the only record of it; category fields are unchanged on " +
			"these rows by construction.\n\n" +
			"The remaining two record changes the user did NOT ask for, which is exactly why they " +
			"are not filed under 'category'. 'settlement' is written when a pending charge posts and " +
			"the sync carries the manual category from the pending row onto the posted twin, " +
			"replacing whatever the twin had — it explains why the twin no longer agrees with the " +
			"rule_execution_history entry for the rule that categorised it moments earlier. " +
			"'erasure' is the one source whose cause is another user: when a different user " +
			"exercises GDPR erasure their custom categories are hard-deleted, and a surviving user's " +
			"transaction pointing at one has its override cleared. That row is the ONLY explanation " +
			"anywhere for a category changing under a user who did nothing — no other record of it " +
			"exists, and none is written for the erased user's own transactions, which are deleted " +
			"in the same database transaction.",
		Tags:     []string{"Transactions"},
		Security: []map[string][]string{{"clerk": {}}},
	}, handler.GetUserTransactionAnnotationHistory)
}
