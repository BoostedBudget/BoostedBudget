package operations

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	humaerrors "github.com/maxnussbaum/BoostedBudget/internal/huma/errors"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	humamiddleware "github.com/maxnussbaum/BoostedBudget/internal/huma/middleware"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
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
		Resolved int `json:"resolved"`
		// OperatorAttested counts records IN THIS PAGE an operator has CLAIMED to have removed by hand
		// in Plaid's dashboard, verified by nothing (Issue #1497 item 2). ⚠ Deliberately its own number
		// and never summed into Resolved: one of these two counts is what Plaid told us, the other is
		// what a colleague told us, and a surface that adds them together can no longer say which.
		OperatorAttested int    `json:"operator_attested"`
		Limit            int    `json:"limit"`
		Offset           int    `json:"offset"`
		AsOf             string `json:"as_of"`
	}
}

// RecordPlaidOrphanedItemOperatorResolutionInput is the operator-attestation write. Issue #1497 item 2.
//
// ⚠ THE ATTESTER IS NOT IN THE BODY, AND MUST NEVER BE. It is taken from AdminAuthenticatedInput's
// ClerkUserID, i.e. from the verified JWT. A client-supplied attester would let one operator file a
// claim in a colleague's name, which destroys the ONLY mitigation an unverified attestation has: that
// a real, identified person is answerable for it.
type RecordPlaidOrphanedItemOperatorResolutionInput struct {
	inputs.AdminAuthenticatedInput
	ID   string                                           `path:"id" format:"uuid" doc:"plaid_orphaned_items.id of the record being attested"`
	Body RecordPlaidOrphanedItemOperatorResolutionRequest `required:"true"`
}

// RecordPlaidOrphanedItemOperatorResolutionRequest carries the one thing only the operator knows.
type RecordPlaidOrphanedItemOperatorResolutionRequest struct {
	// Note is REQUIRED, and its being required is a design decision rather than boilerplate. An
	// attestation is accepted on no evidence at all, so the only thing that makes it checkable later is
	// a human-readable account of what was actually done — without one, this endpoint would be a button
	// that makes a red badge go away. maxLength keeps a pasted log dump out of a column the admin table
	// renders inline.
	Note string `json:"note" minLength:"1" maxLength:"2000" required:"true" doc:"What you did at Plaid, in your own words. Recorded verbatim against your admin identity and shown on the orphaned-Items surface; it is the only way anyone can check this claim later."`
}

// RecordPlaidOrphanedItemOperatorResolutionOutput echoes exactly what was recorded.
//
// ⚠ IT DELIBERATELY DOES NOT RETURN admin_state. Deriving the row's new state here would mean either
// re-reading the row (a second query for a value the client is about to refetch anyway) or COMPUTING
// it from what this handler happens to know — and the second is the mistake: a row that was already
// proved-resolved reports "resolved", not "operator_attested", because proof beats claim
// (domain.PlaidOrphanedItem.AdminState). A guessed state on a compliance surface is exactly the class
// of confident-but-unchecked claim this feature exists to eliminate, so the client refetches the list
// and reads the server's derivation.
type RecordPlaidOrphanedItemOperatorResolutionOutput struct {
	Body struct {
		ID                 string `json:"id"`
		OperatorResolvedAt string `json:"operator_resolved_at"`
		OperatorResolvedBy string `json:"operator_resolved_by"`
		Note               string `json:"note"`
	}
}

// ListPlaidOrphanedItems returns Plaid Items stranded by a completed GDPR erasure, each labelled with
// what an operator must actually do about it.
//
// GET /api/v1/admin/plaid/orphaned-items
//
// ⚠ THERE IS DELIBERATELY NO "RETRY" ACTION, and Issue #1497 item 2 adding a write endpoint beside
// this one did not change that. The retry sweep already re-attempts every unresolved record with a
// live credential on its own daily cadence, so a retry button would duplicate it without changing any
// outcome; and for a record past its deadline there is nothing left to retry WITH, which is precisely
// what admin_state = "manual_intervention_required" is telling the operator. An action that could only
// ever be a no-op or a duplicate would be worse than absent: it would imply the system can still act
// when it cannot. The one write that DOES exist —
// RecordPlaidOrphanedItemOperatorResolution — is the opposite shape: it asks the system to record
// something only the human knows, rather than asking it to do something it cannot.
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
		case domain.OrphanedItemOperatorAttested:
			out.Body.OperatorAttested++
		}
		out.Body.Items = append(out.Body.Items, view)
	}
	out.Body.Total = total
	out.Body.Limit = input.Limit
	out.Body.Offset = input.Offset
	out.Body.AsOf = asOf.Format(time.RFC3339)
	return out, nil
}

// RecordPlaidOrphanedItemOperatorResolution records an operator's UNVERIFIED claim that they removed a
// stranded Plaid Item by hand in Plaid's own dashboard. Issue #1497 item 2.
//
// POST /api/v1/admin/plaid/orphaned-items/{id}/operator-resolution
//
// ⚠ WHY THIS EXISTS AS ITS OWN THING RATHER THAN AS A "MARK RESOLVED" BUTTON. Before this endpoint, an
// operator who did the manual work at Plaid had nowhere to say so: the row kept reporting
// manual_intervention_required forever, and the only column that could have silenced it — resolved_at —
// means "an automated /item/remove PROVED the Item gone", which the operator's action does not
// establish. So the honest options were "leave a permanent false work item" or "write a false proof",
// and this endpoint is the third one: record the claim AS a claim.
//
// WHAT IT THEREFORE DOES NOT DO, all enforced in the repository and pinned by tests there: it does not
// stamp resolved_at, does not purge or touch the retained credential, does not stamp token_purged_at or
// plaid_removed_at, does not touch the retry attempt count, and does NOT remove the row from the
// automated retry sweep's selection (see plaidOrphanedItemRepository.GetRetryablePage for that decision
// and its argument). The row also stays fully visible on the listing above — a claim that hid the
// evidence for itself would be worthless.
//
// NEITHER NO-OP IS ANSWERED WITH A 200. A missing record is 404 and an already-attested one is 409,
// because the caller is a human who just wrote a note: replying "done" while discarding it is how
// somebody comes to believe a record exists that does not. That is the exact silence Y1 removed from
// the dead-letter retry surface.
func (h *AdminHandler) RecordPlaidOrphanedItemOperatorResolution(
	ctx context.Context,
	input *RecordPlaidOrphanedItemOperatorResolutionInput,
) (*RecordPlaidOrphanedItemOperatorResolutionOutput, error) {
	if h.plaidOrphanedItemRepo == nil {
		return nil, huma.Error503ServiceUnavailable("orphaned Plaid item repository not available")
	}
	// Belt and braces beside the resolver, which already refuses a request with no Clerk identity: an
	// anonymous attestation violates migration 000137's CHECK constraint (tightened by 000139 to reject a
	// blank attester as well as a NULL one), and a 23514 surfacing as a
	// 500 would hide what is actually wrong. See the input type's comment on why the attester is never
	// taken from the body.
	if input.ClerkUserID == "" {
		return nil, huma.Error401Unauthorized("an operator attestation must record the identity of the operator making it")
	}

	// The schema's minLength counts characters, so "   " passes it. An attestation whose note is only
	// whitespace is the unexplained claim this endpoint exists to avoid producing, so it is refused here
	// and the trimmed value is what gets stored.
	note := strings.TrimSpace(input.Body.Note)
	if note == "" {
		return nil, huma.Error422UnprocessableEntity(
			"an operator attestation must say what was actually done at Plaid — it is unverified, so the note is the only way anyone can check it later")
	}

	// ONE instant, stamped by the application rather than by the database's NOW(), for the reason #1466
	// threaded asOf through every liveness predicate in this feature: the attestation timestamp is read
	// back by a state derivation that is tested at its boundaries, and a clock read inside SQL is not
	// controllable from a test.
	asOf := time.Now().UTC()

	err := h.plaidOrphanedItemRepo.RecordOperatorResolution(ctx, input.ID, input.ClerkUserID, note, asOf)
	switch {
	case errors.Is(err, repository.ErrOrphanedItemNotFound):
		return nil, huma.Error404NotFound("no orphaned Plaid Item record with that id")
	case errors.Is(err, repository.ErrOrphanedItemAlreadyAttested):
		// 409 and an explicit statement that THIS note was not stored. First-wins keeps "who said this,
		// and when" answerable, so the earlier attestation stands and the later one has to be told.
		return nil, huma.Error409Conflict(
			"this record already carries an operator attestation; the first one stands and your note was NOT recorded — read the existing attestation on the record before acting")
	case err != nil:
		return nil, humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to record the operator attestation for orphaned Plaid item %s: %w", input.ID, err))
	}

	out := &RecordPlaidOrphanedItemOperatorResolutionOutput{}
	out.Body.ID = input.ID
	out.Body.OperatorResolvedAt = asOf.Format(time.RFC3339)
	out.Body.OperatorResolvedBy = input.ClerkUserID
	out.Body.Note = note
	return out, nil
}

// listPlaidOrphanedItemsDescription is the listing endpoint's published description — the text that
// lands in api/openapi.yaml and, from there, in the generated client and in whatever an operator or
// an integrator reads before they act on a row.
//
// ⚠ IT IS BUILT BY INTERPOLATING domain's OWN AdminState CONSTANTS, NOT BY TYPING THEIR NAMES, and
// that is the whole reason this is a variable rather than a string literal inline in the Register
// call. The hand-typed version enumerated THREE of the four states: `operator_attested` was added to
// the derivation (Issue #1497 item 2) and to all three consumers, and this sentence — the only
// description of the enumeration anybody outside the codebase can read — was left saying there were
// three. A published enumeration that is missing a value is worse than none, because a client that
// switches on it has no reason to think a fourth exists.
//
// The order is the operator's, not domain.AdminState's priority order: what needs doing, then what
// needs nothing. Adding a state to domain.OrphanedItemAdminState without adding a clause here is
// caught by TestListPlaidOrphanedItemsDescription_EnumeratesEveryAdminState.
var listPlaidOrphanedItemsDescription = fmt.Sprintf(
	"Returns Plaid Items whose local record was deleted by a completed GDPR erasure before removal "+
		"could be proved, labelled with the action required: '%s' (remove the Item in Plaid's dashboard "+
		"using plaid_item_id, or it may keep billing), '%s' (an automated retry holds a live credential "+
		"— no action), '%s' (an admin has stated they removed it by hand; this is an UNVERIFIED claim, "+
		"resolved_at is NOT set, and the automated sweep keeps re-attempting the removal precisely "+
		"because it may yet disprove the claim), or '%s' (Plaid confirmed the removal — history, not "+
		"work). Never returns the retained access token. Requires admin role.",
	domain.OrphanedItemManualInterventionRequired,
	domain.OrphanedItemAutoRetrying,
	domain.OrphanedItemOperatorAttested,
	domain.OrphanedItemResolved,
)

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
		Description: listPlaidOrphanedItemsDescription,
		Tags:        []string{"Admin"},
		Security: []map[string][]string{{"clerk": {}}},
		Middlewares: huma.Middlewares{
			humamiddleware.NewAdminMiddleware(handler.config, handler.adminService),
		},
	}, handler.ListPlaidOrphanedItems)

	huma.Register(api, huma.Operation{
		OperationID: "recordAdminPlaidOrphanedItemOperatorResolution",
		Method:      http.MethodPost,
		Path:        "/api/v1/admin/plaid/orphaned-items/{id}/operator-resolution",
		Summary:     "Attest that an orphaned Plaid Item was removed by hand (admin)",
		Description: "Records that YOU removed this Plaid Item yourself in Plaid's dashboard. This is an " +
			"UNVERIFIED attestation, not a proved removal: it is stored against your admin identity with " +
			"your note, moves the record to admin_state 'operator_attested' so nobody is asked to redo the " +
			"work, and deliberately does NOT set resolved_at (which means Plaid itself confirmed the " +
			"removal), does NOT destroy the retained access token, and does NOT stop the automated retry " +
			"sweep — the sweep is what can eventually confirm your claim. First-wins: if the record is " +
			"already attested this returns 409 and your note is not stored. Requires admin role.",
		Tags:     []string{"Admin"},
		Security: []map[string][]string{{"clerk": {}}},
		Middlewares: huma.Middlewares{
			humamiddleware.NewAdminMiddleware(handler.config, handler.adminService),
		},
	}, handler.RecordPlaidOrphanedItemOperatorResolution)
}
