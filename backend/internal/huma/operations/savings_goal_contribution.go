package operations

// savings_goal_contribution.go — Issue #1300 M20: D6 source-a contribution API
//
// Provides four endpoints for managing savings goal contributions:
//   - POST   /api/v1/savings-goals/{goalID}/contributions        — manually log a contribution
//   - GET    /api/v1/savings-goals/{goalID}/contributions        — list all contributions for a goal
//   - GET    /api/v1/savings-goals/deleted/{goalID}/contributions — the retained ledger of a
//     goal the user has deleted (#1569 T2; the live listing above excludes tombstoned goals)
//   - POST   /api/v1/savings-goals/{goalID}/contributions/{id}/confirm — confirm a proposed contribution
//   - DELETE /api/v1/savings-goals/{goalID}/contributions/{id}   — remove a contribution

import (
	"context"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	humaerrors "github.com/maxnussbaum/BoostedBudget/internal/huma/errors"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// =============================================================================
// ContributionHandler
// =============================================================================

// ContributionProgressRecomputer triggers savings progress recomputation for a specific goal
// after a contribution write. Narrow interface (M20/R3-2) to avoid importing the full
// SavingsProgressService. Uses RecomputeGoalByID rather than RecomputeUser so that completed
// goals (excluded from RecomputeUser's active-only scan) are also kept in sync.
type ContributionProgressRecomputer interface {
	RecomputeGoalByID(ctx context.Context, userID, goalID string) error
}

// ContributionGoalReader loads a savings goal for ownership + basis checks (R3-3/C8).
// Narrow interface to avoid importing the full SavingsGoalService.
type ContributionGoalReader interface {
	GetByIDForUser(ctx context.Context, id, userID string) (*domain.SavingsGoal, error)
}

// ContributionHandler handles savings goal contribution HTTP requests (M20).
type ContributionHandler struct {
	contribRepo     repository.SavingsContributionRepository
	progressService ContributionProgressRecomputer // optional; nil = skip goal recompute after write
	goalReader      ContributionGoalReader         // optional; nil = skip basis gate (tests only)

	// auditLogRepo records the savings_goal_contribution.history_purged entry INSIDE
	// DeleteContribution's own transaction (#1569 round 17 task 6, ADR explicit-destruction
	// control 2). Wired by SetAuditLogRepository rather than a fourth constructor parameter — same
	// setter pattern as savingsGoalService.auditLogRepo, and it keeps the ~25 existing
	// NewContributionHandler call sites untouched.
	//
	// ⚠ REQUIRED FOR DeleteContribution, and this comment said "nil = no audit row" until #1569
	// round 17 corrected it. Since round 17 task 11 the handler REFUSES — repository.
	// ErrAuditLogRepositoryNotWired, mapped to a 500 — rather than destroying a contribution with
	// no trail. Every OTHER handler method is unaffected and still works with this nil, which is
	// why the ~25 constructor call sites did not have to change.
	//
	// ⚠ contribRepo.DeleteContribution is STILL nil-tolerant one layer down: it skips the audit
	// write when the sink is nil, so the hardening integration tests can pass nil. The check in
	// this handler is the only thing that makes the control mandatory.
	auditLogRepo repository.AuditLogRepository
}

// SetAuditLogRepository wires the audit-log repository used by DeleteContribution. See the field.
func (h *ContributionHandler) SetAuditLogRepository(ar repository.AuditLogRepository) {
	h.auditLogRepo = ar
}

// NewContributionHandler creates a new ContributionHandler.
// progressService is optional; pass nil to disable post-write goal recomputation.
// goalReader is optional; pass nil only in tests that pre-validate the goal externally.
func NewContributionHandler(
	contribRepo repository.SavingsContributionRepository,
	progressService ContributionProgressRecomputer,
	goalReader ContributionGoalReader,
) *ContributionHandler {
	return &ContributionHandler{
		contribRepo:     contribRepo,
		progressService: progressService,
		goalReader:      goalReader,
	}
}

// =============================================================================
// Input / Output types
// =============================================================================

// LogContributionInput is the request body for manually logging a contribution.
type LogContributionInput struct {
	inputs.AuthenticatedInput
	GoalID string `path:"goalID" doc:"Savings goal ID"`
	Body   domain.LogContributionRequest
}

// LogContributionOutput is the response for a newly created contribution.
type LogContributionOutput struct {
	Body *domain.SavingsGoalContribution
}

// ListContributionsInput is the request for listing contributions for a goal.
type ListContributionsInput struct {
	inputs.AuthenticatedInput
	GoalID string `path:"goalID" doc:"Savings goal ID"`
}

// ListContributionsOutput wraps the list of contributions.
type ListContributionsOutput struct {
	Body []*domain.SavingsGoalContribution
}

// ListDeletedGoalContributionsInput is the request for listing a TOMBSTONED goal's contributions.
type ListDeletedGoalContributionsInput struct {
	inputs.AuthenticatedInput
	GoalID string `path:"goalID" doc:"ID of a savings goal the user has already deleted"`
}

// DeletedGoalContributionsBody is the payload of the deleted-goal ledger.
//
// It carries the total alongside the entries because the deleted-goal surface has nothing else to
// derive it from: savings_goals.current_amount is frozen at whatever the last recompute wrote, and
// no live read will ever touch a tombstoned goal again.
type DeletedGoalContributionsBody struct {
	Contributions  []*domain.SavingsGoalContribution `json:"contributions" doc:"Every contribution still logged against the deleted goal, newest first. A contribution the user explicitly deleted earlier was destroyed at that point and is not here"`
	TotalConfirmed float64                           `json:"total_confirmed" doc:"Sum of the confirmed contributions; proposed and rejected rows are excluded"`
}

// ListDeletedGoalContributionsOutput wraps the deleted goal's retained ledger.
type ListDeletedGoalContributionsOutput struct {
	Body DeletedGoalContributionsBody
}

// ConfirmContributionInput is the request for confirming a proposed contribution.
type ConfirmContributionInput struct {
	inputs.AuthenticatedInput
	GoalID string `path:"goalID" doc:"Savings goal ID"`
	ID     string `path:"id" doc:"Contribution ID"`
}

// ConfirmContributionOutput is the response for a confirmed contribution.
type ConfirmContributionOutput struct {
	Body *domain.SavingsGoalContribution
}

// RejectContributionInput is the request for rejecting a proposed contribution.
type RejectContributionInput struct {
	inputs.AuthenticatedInput
	GoalID string `path:"goalID" doc:"Savings goal ID"`
	ID     string `path:"id" doc:"Contribution ID"`
}

// RejectContributionOutput is the response for a rejected contribution.
type RejectContributionOutput struct {
	Body *domain.SavingsGoalContribution
}

// DeleteContributionInput is the request for removing a contribution.
type DeleteContributionInput struct {
	inputs.AuthenticatedInput
	GoalID string `path:"goalID" doc:"Savings goal ID"`
	ID     string `path:"id" doc:"Contribution ID"`
}

// =============================================================================
// Handler methods
// =============================================================================

// assertLedgerIsLive refuses a ledger MUTATION on a goal whose current_amount is not derived from
// the contribution ledger.
//
// #1625 Task 10. LogContribution had this gate; Confirm and Reject did not, so on a goal switched
// off the contributions basis they returned 200 while RecomputeGoalByID early-returned — the row
// changed status, the goal did not move, and the caller was told it worked. One helper rather than
// three copies, so the three endpoints cannot come to disagree about what the precondition is.
//
// ⚠ DeleteContribution deliberately does NOT call this, and the original justification for that
// was overstated. "Permanently undeletable" is wrong: a user could switch the goal back to the
// contributions basis and delete the row then. The accurate reason is narrower and still holds —
// gating Delete would make deleting your own row require an unrelated, side-effecting change to
// the goal's configuration first, to remove data the basis has no bearing on. Confirm and Reject
// genuinely cannot take effect on a manual goal; a delete does not have that failure mode.
//
// ⚠ "A delete always does exactly what it says" was FALSIFIED (#1625): the repository looked the
// contribution up by its own id alone, so a same-user DELETE through goal A's URL could destroy
// goal B's contribution row — a hard delete of financial history the caller never named. Fixed by
// binding goalID into DeleteContribution's WHERE, the same way Confirm/Reject were bound; see that
// method's doc comment. The claim above is now true only because of that binding, not because
// deletes are inherently free of this failure mode.
//
// `verb` completes the sentence, so each endpoint's message describes what IT could not do.
func (h *ContributionHandler) assertLedgerIsLive(ctx context.Context, goalID, userID, verb string) error {
	if h.goalReader == nil {
		return nil
	}
	goal, err := h.goalReader.GetByIDForUser(ctx, goalID, userID)
	if err != nil {
		if err == domain.ErrSavingsGoalNotFound {
			return huma.Error404NotFound("savings goal not found")
		}
		return humaerrors.LoggingMapError(ctx, err)
	}
	if goal.ProgressBasis == domain.ProgressBasisContributions {
		return nil
	}
	// #1625: the original message was the bare string "basis mismatch", naming neither the
	// offending value nor the remedy — so an agent hitting it on a brand-new goal had no way to
	// proceed. Say what is wrong and exactly how to fix it.
	// ⚠ #1625 final review I4: the remedy has to be TRUE for the basis the caller is actually on.
	// "or update current_amount directly instead" is only available on the MANUAL basis:
	// savingsGoalService.UpdateGoal refuses a caller-supplied current_amount whenever the TARGET
	// basis is not manual, with a 400. Telling a balance / account_percentage caller to do it sent
	// them straight into another 4xx — the "an agent hitting this had no way to proceed" failure
	// this message was rewritten to fix, one step further along.
	remedy := fmt.Sprintf("Set progress_basis to %q (PUT /api/v1/savings-goals/%s) first.",
		domain.ProgressBasisContributions, goalID)
	if goal.ProgressBasis == domain.ProgressBasisManual {
		remedy = fmt.Sprintf(
			"Set progress_basis to %q (PUT /api/v1/savings-goals/%s) first, or update current_amount "+
				"directly on this goal instead.",
			domain.ProgressBasisContributions, goalID)
	} else {
		remedy += " current_amount cannot be set by hand on this basis either — it is derived from " +
			"the linked account, and PUT /api/v1/savings-goals/{id} refuses a supplied value with a 400."
	}
	return huma.Error422UnprocessableEntity(
		fmt.Sprintf(
			"this goal's progress_basis is %q, so its current_amount is not tracked through the "+
				"contribution ledger and contributions cannot be %s it. %s",
			goal.ProgressBasis, verb, remedy,
		),
		domain.ErrContributionBasisMismatch)
}

// LogContribution handles POST /api/v1/savings-goals/{goalID}/contributions.
// Manually logs a confirmed contribution for the goal (D6 source-a / M20).
//
// R3-3 (C8): rejects if goal.progress_basis != "contributions" — manual contributions
// only move progress for contributions-basis goals.
// R3-1 (C2): if the log links a transaction_id, the write goes through
// UpsertFromTransaction (FOR UPDATE lock + capacity check) so a manual log cannot
// over-allocate a transaction. Un-linked logs use Insert (no capacity to consume).
// R3-2: calls RecomputeGoal(goalID) not RecomputeUser so completed goals are recomputed.
func (h *ContributionHandler) LogContribution(ctx context.Context, input *LogContributionInput) (*LogContributionOutput, error) {
	if err := input.Body.Validate(); err != nil {
		return nil, huma.Error422UnprocessableEntity("invalid contribution", err)
	}

	// R3-3 (C8): load goal and gate on contributions basis.
	if err := h.assertLedgerIsLive(ctx, input.GoalID, input.UserID, "logged against"); err != nil {
		return nil, err
	}

	var contrib *domain.SavingsGoalContribution

	if input.Body.TransactionID != nil {
		// R4-1 (C2+C3): txn-linked manual log — use InsertManualFromTransaction which:
		// - acquires FOR UPDATE + capacity check (C2, same as UpsertFromTransaction)
		// - stores managed_by_reconciliation=false, confirmed_by_user=true, caller's occurred_on (C3/D6)
		// - RETURNING the real persisted row (not a fabricated uuid)
		// UpsertFromTransaction is intentionally NOT used here — it hardcodes reconciler
		// metadata (managed=true, CURRENT_DATE) which is wrong for user-entered rows.
		var err error
		contrib, err = h.contribRepo.InsertManualFromTransaction(
			ctx,
			input.UserID,
			input.GoalID,
			*input.Body.TransactionID,
			input.Body.Amount,
			input.Body.OccurredOn,
		)
		if err != nil {
			switch err {
			case domain.ErrSavingsGoalNotFound:
				return nil, huma.Error404NotFound("savings goal not found")
			case domain.ErrContributionNotFound:
				return nil, huma.Error404NotFound("transaction not found")
			case domain.ErrContributionOverAllocated:
				return nil, huma.Error422UnprocessableEntity("over-allocated", err)
			case domain.ErrContributionAlreadyConfirmed:
				// A confirmed or rejected contribution already occupies this (transaction, goal) slot.
				// The ON CONFLICT WHERE status='proposed' guard prevents overwriting it (Codex R4-1 follow-up).
				return nil, huma.Error422UnprocessableEntity("a contribution for this transaction already exists", err)
			default:
				return nil, humaerrors.LoggingMapError(ctx, err)
			}
		}
	} else {
		// Un-linked manual log: no transaction capacity to consume → plain insert.
		contrib = &domain.SavingsGoalContribution{
			ID:                      uuid.New().String(),
			SavingsGoalID:           input.GoalID,
			Amount:                  input.Body.Amount,
			Source:                  "manual",
			Status:                  "confirmed",
			ManagedByReconciliation: false,
			ConfirmedByUser:         true,
			OccurredOn:              input.Body.OccurredOn,
		}
		if err := h.contribRepo.Insert(ctx, input.UserID, contrib); err != nil {
			if err == domain.ErrSavingsGoalNotFound {
				return nil, huma.Error404NotFound("savings goal not found")
			}
			return nil, humaerrors.LoggingMapError(ctx, err)
		}
	}

	// R3-2: recompute the specific goal (not the whole user) so completed goals are also kept in sync.
	if h.progressService != nil {
		if err := h.progressService.RecomputeGoalByID(ctx, input.UserID, input.GoalID); err != nil {
			// Non-fatal — contribution was persisted; progress reconciled at next sync.
			_ = err
		}
	}

	return &LogContributionOutput{Body: contrib}, nil
}

// ListContributions handles GET /api/v1/savings-goals/{goalID}/contributions.
// Returns all contributions (confirmed + proposed) ordered newest-first.
func (h *ContributionHandler) ListContributions(ctx context.Context, input *ListContributionsInput) (*ListContributionsOutput, error) {
	contribs, err := h.contribRepo.ListByGoal(ctx, input.UserID, input.GoalID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	return &ListContributionsOutput{Body: contribs}, nil
}

// ListDeletedGoalContributions handles
// GET /api/v1/savings-goals/deleted/{goalID}/contributions (Issue #1569 T2).
//
// The DELETE endpoint promises that "every contribution still logged against the goal is
// retained" — still logged, because deleting one contribution explicitly is a hard delete.
// This is the read that makes that promise checkable: without it the rows survive in the table and
// are visible to nobody, which is the "retention that reads as deletion" failure #1569 is named
// for — and it lands on hand-logged amounts no provider can ever supply again.
//
// A DISTINCT ROUTE, not a flag on the live listing, on purpose:
//   - deleted goals already have their own surface (GET /savings-goals/deleted, rendered by
//     DeletedGoalsPanel, from which the purge verb is reached) and this belongs beside it;
//   - the live path stays incapable of returning a tombstoned goal, so nothing can widen by
//     accident later — a query flag would put that decision one boolean away in every caller;
//   - the response shape differs (it carries the total), which a flag could not express cleanly.
//
// Tenancy is enforced in SQL by the repository, identically to the live listing.
func (h *ContributionHandler) ListDeletedGoalContributions(ctx context.Context, input *ListDeletedGoalContributionsInput) (*ListDeletedGoalContributionsOutput, error) {
	contribs, err := h.contribRepo.ListByDeletedGoal(ctx, input.UserID, input.GoalID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	total, err := h.contribRepo.SumConfirmedByDeletedGoal(ctx, input.UserID, input.GoalID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	if contribs == nil {
		// A nil slice marshals as `null`, which the client cannot tell from a failed read — the
		// same ambiguity this endpoint exists to remove.
		contribs = []*domain.SavingsGoalContribution{}
	}
	return &ListDeletedGoalContributionsOutput{Body: DeletedGoalContributionsBody{
		Contributions:  contribs,
		TotalConfirmed: total,
	}}, nil
}

// ConfirmContribution handles POST /api/v1/savings-goals/{goalID}/contributions/{id}/confirm.
// Promotes a proposed contribution to confirmed status. Re-checks allocation capacity under lock.
func (h *ContributionHandler) ConfirmContribution(ctx context.Context, input *ConfirmContributionInput) (*ConfirmContributionOutput, error) {
	if err := h.assertLedgerIsLive(ctx, input.GoalID, input.UserID, "confirmed against"); err != nil {
		return nil, err
	}

	confirmed, err := h.contribRepo.ConfirmContribution(ctx, input.UserID, input.GoalID, input.ID)
	if err != nil {
		switch err {
		case domain.ErrContributionNotFound:
			return nil, huma.Error404NotFound("contribution not found")
		case domain.ErrContributionAlreadyConfirmed:
			return nil, huma.Error409Conflict("contribution is already confirmed")
		case domain.ErrContributionAlreadyRejected:
			return nil, huma.Error409Conflict("contribution was already rejected and cannot be confirmed")
		case domain.ErrContributionOverAllocated:
			return nil, huma.Error422UnprocessableEntity("over-allocated", err)
		default:
			return nil, humaerrors.LoggingMapError(ctx, err)
		}
	}

	// R3-2: recompute the specific goal (not the whole user) so completed goals are also kept in sync.
	if h.progressService != nil {
		if err := h.progressService.RecomputeGoalByID(ctx, input.UserID, input.GoalID); err != nil {
			// Non-fatal — contribution was persisted; progress reconciled at next sync.
			_ = err
		}
	}

	return &ConfirmContributionOutput{Body: confirmed}, nil
}

// RejectContribution handles POST /api/v1/savings-goals/{goalID}/contributions/{id}/reject.
// Sets a proposed contribution to 'rejected' status (sticky tombstone — R3-7).
// The row is retained so UpsertProposedFromTransaction won't re-propose the same transfer-in
// on future syncs (the partial-unique-index key is preserved as a tombstone).
// Only proposed rows can be rejected; confirmed rows must be deleted.
func (h *ContributionHandler) RejectContribution(ctx context.Context, input *RejectContributionInput) (*RejectContributionOutput, error) {
	if err := h.assertLedgerIsLive(ctx, input.GoalID, input.UserID, "rejected on"); err != nil {
		return nil, err
	}

	rejected, err := h.contribRepo.RejectContribution(ctx, input.UserID, input.GoalID, input.ID)
	if err != nil {
		switch err {
		case domain.ErrContributionNotFound:
			return nil, huma.Error404NotFound("contribution not found")
		case domain.ErrContributionNotProposed:
			return nil, huma.Error409Conflict("only proposed contributions can be rejected")
		default:
			return nil, humaerrors.LoggingMapError(ctx, err)
		}
	}

	// R3-2: recompute the specific goal (non-fatal).
	if h.progressService != nil {
		if err := h.progressService.RecomputeGoalByID(ctx, input.UserID, input.GoalID); err != nil {
			_ = err
		}
	}

	return &RejectContributionOutput{Body: rejected}, nil
}

// DeleteContribution handles DELETE /api/v1/savings-goals/{goalID}/contributions/{id}.
// PERMANENTLY DESTROYS a contribution row: the user may delete proposed and confirmed rows.
//
// 404 now also covers three deliberate refusals — a tombstoned goal, a tombstoned contribution and
// a 'rejected' sticky tombstone — none of which the caller can see in any product surface. See the
// repository method's doc comment (#1569 round 17 task 6) for why "not found" is the honest answer
// for all three and what the refusal does NOT protect.
func (h *ContributionHandler) DeleteContribution(ctx context.Context, input *DeleteContributionInput) (*struct{}, error) {
	// Control 2 is not optional: refuse rather than destroy un-audited. See
	// repository.ErrAuditLogRepositoryNotWired.
	if h.auditLogRepo == nil {
		return nil, humaerrors.LoggingMapError(ctx, repository.ErrAuditLogRepositoryNotWired)
	}

	if err := h.contribRepo.DeleteContribution(ctx, input.UserID, input.GoalID, input.ID, h.auditLogRepo); err != nil {
		if err == domain.ErrContributionNotFound {
			return nil, huma.Error404NotFound("contribution not found")
		}
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	// R3-2: recompute the specific goal (not the whole user) so completed goals are also kept in sync.
	if h.progressService != nil {
		if err := h.progressService.RecomputeGoalByID(ctx, input.UserID, input.GoalID); err != nil {
			// Non-fatal — contribution was deleted; progress reconciled at next sync.
			_ = err
		}
	}

	return nil, nil
}

// =============================================================================
// Route registration
// =============================================================================

// RegisterContributionRoutes registers the contribution API endpoints.
func RegisterContributionRoutes(api huma.API, handler *ContributionHandler) {
	// POST /api/v1/savings-goals/{goalID}/contributions — manually log
	huma.Register(api, huma.Operation{
		OperationID: "logSavingsGoalContribution",
		Method:      http.MethodPost,
		Path:        "/api/v1/savings-goals/{goalID}/contributions",
		Summary:     "Log a savings goal contribution",
		Description: "Manually records a confirmed contribution for a savings goal (D6 source-a). " +
			"Optionally links to an existing transaction. Triggers goal progress recomputation. " +
			"Requires the goal's progress_basis to be \"contributions\". " +
			"⚠ BREAKING (#1625): the request body rejects unknown fields, and the phantom \"notes\" " +
			"field was removed — a client that previously sent it was silently ignored and now gets " +
			"a 422. Drop the field; contribution notes were never stored.",
		Tags:     []string{"Savings Goals", "Contributions"},
		Security: []map[string][]string{{"clerk": {}}},
	}, handler.LogContribution)

	// GET /api/v1/savings-goals/{goalID}/contributions — list
	huma.Register(api, huma.Operation{
		OperationID: "listSavingsGoalContributions",
		Method:      http.MethodGet,
		Path:        "/api/v1/savings-goals/{goalID}/contributions",
		Summary:     "List savings goal contributions",
		Description: "Returns all confirmed and proposed contributions for a savings goal, newest first. " +
			"Includes archived contributions (closed periods) which do not count toward the goal's progress. " +
			"Returns an empty list for a goal the user has DELETED — the contributions still logged " +
			"against it are retained and are read through " +
			"GET /savings-goals/deleted/{goalID}/contributions instead (#1569).",
		Tags:        []string{"Savings Goals", "Contributions"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.ListContributions)

	// GET /api/v1/savings-goals/deleted/{goalID}/contributions — deleted goal's retained ledger
	huma.Register(api, huma.Operation{
		OperationID: "listDeletedSavingsGoalContributions",
		Method:      http.MethodGet,
		Path:        "/api/v1/savings-goals/deleted/{goalID}/contributions",
		Summary:     "List a deleted savings goal's retained contributions",
		Description: "Returns the contributions retained under a goal the user has DELETED, newest first, " +
			"with their confirmed total (Issue #1569). Deleting a goal tombstones it and keeps every " +
			"contribution STILL logged against it — including amounts the user entered by hand, which no " +
			"provider can supply again. A contribution the user explicitly deleted earlier via DELETE " +
			"/savings-goals/{goalID}/contributions/{id} was destroyed then and is not listed here. This " +
			"is the only endpoint that can read the retained ones back; the live " +
			"GET /savings-goals/{goalID}/contributions deliberately returns nothing for a deleted goal. " +
			"Returns an empty list for a goal that is not deleted. Pair it with " +
			"GET /savings-goals/deleted, and with POST /savings-goals/{id}/purge to destroy them for good.",
		Tags:     []string{"Savings Goals", "Contributions"},
		Security: []map[string][]string{{"clerk": {}}},
	}, handler.ListDeletedGoalContributions)

	// POST /api/v1/savings-goals/{goalID}/contributions/{id}/confirm — confirm proposed
	huma.Register(api, huma.Operation{
		OperationID: "confirmSavingsGoalContribution",
		Method:      http.MethodPost,
		Path:        "/api/v1/savings-goals/{goalID}/contributions/{id}/confirm",
		Summary:     "Confirm a proposed savings goal contribution",
		Description: "Promotes a proposed contribution to confirmed status. Re-checks allocation capacity " +
			"under a transaction-level lock before committing (Codex C2).",
		Tags:     []string{"Savings Goals", "Contributions"},
		Security: []map[string][]string{{"clerk": {}}},
	}, handler.ConfirmContribution)

	// POST /api/v1/savings-goals/{goalID}/contributions/{id}/reject — sticky reject tombstone
	huma.Register(api, huma.Operation{
		OperationID: "rejectSavingsGoalContribution",
		Method:      http.MethodPost,
		Path:        "/api/v1/savings-goals/{goalID}/contributions/{id}/reject",
		Summary:     "Reject a proposed savings goal contribution",
		Description: "Sets a proposed contribution to 'rejected' status. The row is retained as a sticky " +
			"tombstone so future Plaid syncs do not re-propose the same transfer-in (R3-7). " +
			"Only proposed contributions can be rejected; confirmed contributions must be deleted.",
		Tags:     []string{"Savings Goals", "Contributions"},
		Security: []map[string][]string{{"clerk": {}}},
	}, handler.RejectContribution)

	// DELETE /api/v1/savings-goals/{goalID}/contributions/{id} — remove
	huma.Register(api, huma.Operation{
		OperationID:   "deleteSavingsGoalContribution",
		Method:        http.MethodDelete,
		Path:          "/api/v1/savings-goals/{goalID}/contributions/{id}",
		Summary:       "Delete a savings goal contribution",
		Description:   "PERMANENTLY DESTROYS a contribution (proposed or confirmed) and triggers goal progress recomputation. This is a hard delete, not a tombstone, and it is one of the sanctioned explicit-destruction exceptions under the retain-by-default rule (#1569): a hand-logged amount erased here cannot be recovered and no provider can supply it again. Deleting the GOAL does not do this — that retains every contribution still logged against it.",
		Tags:          []string{"Savings Goals", "Contributions"},
		Security:      []map[string][]string{{"clerk": {}}},
		DefaultStatus: http.StatusNoContent,
	}, handler.DeleteContribution)
}
