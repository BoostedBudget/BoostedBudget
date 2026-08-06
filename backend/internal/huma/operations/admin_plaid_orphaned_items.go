package operations

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	humaerrors "github.com/maxnussbaum/BoostedBudget/internal/huma/errors"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	humamiddleware "github.com/maxnussbaum/BoostedBudget/internal/huma/middleware"
)

// ListPlaidOrphanedItemsInput is the query for the orphaned-Plaid-Item admin listing. Issue #1497.
//
// Offset pagination with a small default, deliberately: migration 000134 expects this table to hold
// single digits of rows (it takes a permanently-unremovable Plaid Item AND a completed erasure to make
// one), so this is a human reading a short work list, not a sweep that must not skip anything. The
// sweeps use keyset pagination for that reason and this does not — see
// plaidOrphanedItemRepository.List on why they are deliberately different.
type ListPlaidOrphanedItemsInput struct {
	inputs.AdminAuthenticatedInput
	Limit  int `query:"limit" default:"50" minimum:"1" maximum:"200" doc:"Number of records to return"`
	Offset int `query:"offset" default:"0" minimum:"0" doc:"Number of records to skip"`
}

// ListPlaidOrphanedItemsOutput is the response body.
//
// The counts are computed server-side rather than left to the client, because the two states call for
// OPPOSITE human action — one needs an operator in Plaid's dashboard, the other needs them to do
// nothing — and a client that derived them from a paged slice would report the page's counts as the
// table's. Conflating the two states is how an Item silently rots.
type ListPlaidOrphanedItemsOutput struct {
	Body struct {
		Items []domain.PlaidOrphanedItemAdminView `json:"items"`
		Total int                                 `json:"total"`
		// ManualInterventionRequired counts records IN THIS PAGE that an operator must finish by hand in
		// Plaid: nothing automated will remove them, and each one may still be billing.
		ManualInterventionRequired int `json:"manual_intervention_required"`
		// AutoRetrying counts records IN THIS PAGE that a live credential and a running sweep are still
		// working on. No operator action — acting now would duplicate work in flight.
		AutoRetrying int `json:"auto_retrying"`
		// Resolved counts records IN THIS PAGE whose Item was proved removed. History, not work.
		Resolved int    `json:"resolved"`
		Limit    int    `json:"limit"`
		Offset   int    `json:"offset"`
		AsOf     string `json:"as_of"`
	}
}

// ListPlaidOrphanedItems returns Plaid Items stranded by a completed GDPR erasure, each labelled with
// what an operator must actually do about it.
//
// GET /api/v1/admin/plaid/orphaned-items
//
// READ-ONLY, AND THERE IS DELIBERATELY NO "RETRY" ACTION. The retry sweep already re-attempts every
// unresolved record with a live credential on its own daily cadence, so a button would duplicate it
// without changing any outcome; and for a record past its deadline there is nothing left to retry WITH,
// which is precisely what admin_state = "manual_intervention_required" is telling the operator. An
// action that could only ever be a no-op or a duplicate would be worse than absent: it would imply the
// system can still act when it cannot.
func (h *AdminHandler) ListPlaidOrphanedItems(ctx context.Context, input *ListPlaidOrphanedItemsInput) (*ListPlaidOrphanedItemsOutput, error) {
	if h.plaidOrphanedItemRepo == nil {
		return nil, huma.Error503ServiceUnavailable("orphaned Plaid item repository not available")
	}

	records, total, err := h.plaidOrphanedItemRepo.List(ctx, input.Limit, input.Offset)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to list orphaned Plaid items: %w", err))
	}

	// ONE instant for the whole response. Every state in this payload is a liveness/staleness decision
	// against a clock (see domain.PlaidOrphanedItem.AdminState), and deriving each row against its own
	// time.Now() would let two rows in one response disagree about where the boundary is — the coupling
	// Issue #1466 threaded asOf everywhere to remove. It is echoed back as as_of so a reader can tell
	// which instant the labels were computed for.
	asOf := time.Now().UTC()

	out := &ListPlaidOrphanedItemsOutput{}
	out.Body.Items = make([]domain.PlaidOrphanedItemAdminView, 0, len(records))
	for _, rec := range records {
		view := rec.AdminView(asOf)
		switch view.AdminState {
		case domain.OrphanedItemManualInterventionRequired:
			out.Body.ManualInterventionRequired++
		case domain.OrphanedItemAutoRetrying:
			out.Body.AutoRetrying++
		case domain.OrphanedItemResolved:
			out.Body.Resolved++
		}
		out.Body.Items = append(out.Body.Items, view)
	}
	out.Body.Total = total
	out.Body.Limit = input.Limit
	out.Body.Offset = input.Offset
	out.Body.AsOf = asOf.Format(time.RFC3339)
	return out, nil
}

// RegisterAdminPlaidOrphanedItemOperations registers the orphaned-Plaid-Item admin listing.
//
// Called from RegisterAdminRoutes, and named *Operations rather than *Routes to match
// RegisterAdminDeadLetterOperations — cmd/server's route-registration drift test matches only
// `Register…Routes(`, and a second `…Routes` name registered from inside RegisterAdminRoutes would look
// to it like a group main.go had failed to call.
func RegisterAdminPlaidOrphanedItemOperations(api huma.API, handler *AdminHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "listAdminPlaidOrphanedItems",
		Method:      http.MethodGet,
		Path:        "/api/v1/admin/plaid/orphaned-items",
		Summary:     "List orphaned Plaid Items (admin)",
		Description: "Returns Plaid Items whose local record was deleted by a completed GDPR erasure before " +
			"removal could be proved, labelled with the action required: 'auto_retrying' (an automated retry " +
			"holds a live credential — no action), 'manual_intervention_required' (remove the Item in Plaid's " +
			"dashboard using plaid_item_id, or it may keep billing), or 'resolved' (history). Never returns the " +
			"retained access token. Requires admin role.",
		Tags:     []string{"Admin"},
		Security: []map[string][]string{{"clerk": {}}},
		Middlewares: huma.Middlewares{
			humamiddleware.NewAdminMiddleware(handler.config, handler.adminService),
		},
	}, handler.ListPlaidOrphanedItems)
}
