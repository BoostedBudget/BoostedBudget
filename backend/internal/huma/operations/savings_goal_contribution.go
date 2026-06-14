package operations

// savings_goal_contribution.go — Issue #1300 M20: D6 source-a contribution API
//
// Provides four endpoints for managing savings goal contributions:
//   - POST   /api/v1/savings-goals/{goalID}/contributions        — manually log a contribution
//   - GET    /api/v1/savings-goals/{goalID}/contributions        — list all contributions for a goal
//   - POST   /api/v1/savings-goals/{goalID}/contributions/{id}/confirm — confirm a proposed contribution
//   - DELETE /api/v1/savings-goals/{goalID}/contributions/{id}   — remove a contribution

import (
	"context"
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
	if h.goalReader != nil {
		goal, err := h.goalReader.GetByIDForUser(ctx, input.GoalID, input.UserID)
		if err != nil {
			if err == domain.ErrSavingsGoalNotFound {
				return nil, huma.Error404NotFound("savings goal not found")
			}
			return nil, humaerrors.LoggingMapError(ctx, err)
		}
		if goal.ProgressBasis != domain.ProgressBasisContributions {
			return nil, huma.Error422UnprocessableEntity("basis mismatch",
				domain.ErrContributionBasisMismatch)
		}
	}

	var contrib *domain.SavingsGoalContribution

	if input.Body.TransactionID != nil {
		// R3-1 (C2): txn-linked → capacity-safe path (FOR UPDATE + cap check).
		if err := h.contribRepo.UpsertFromTransaction(
			ctx,
			input.UserID,
			input.GoalID,
			*input.Body.TransactionID,
			input.Body.Amount,
			"manual",
		); err != nil {
			switch err {
			case domain.ErrSavingsGoalNotFound:
				return nil, huma.Error404NotFound("savings goal not found")
			case domain.ErrContributionNotFound:
				return nil, huma.Error404NotFound("transaction not found")
			case domain.ErrContributionOverAllocated:
				return nil, huma.Error422UnprocessableEntity("over-allocated", err)
			default:
				return nil, humaerrors.LoggingMapError(ctx, err)
			}
		}
		// Build a response struct representing the upserted row.
		contrib = &domain.SavingsGoalContribution{
			ID:                      uuid.New().String(),
			SavingsGoalID:           input.GoalID,
			TransactionID:           input.Body.TransactionID,
			Amount:                  input.Body.Amount,
			Source:                  "manual",
			Status:                  "confirmed",
			ManagedByReconciliation: false,
			ConfirmedByUser:         true,
			OccurredOn:              input.Body.OccurredOn,
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

// ConfirmContribution handles POST /api/v1/savings-goals/{goalID}/contributions/{id}/confirm.
// Promotes a proposed contribution to confirmed status. Re-checks allocation capacity under lock.
func (h *ContributionHandler) ConfirmContribution(ctx context.Context, input *ConfirmContributionInput) (*ConfirmContributionOutput, error) {
	confirmed, err := h.contribRepo.ConfirmContribution(ctx, input.UserID, input.ID)
	if err != nil {
		switch err {
		case domain.ErrContributionNotFound:
			return nil, huma.Error404NotFound("contribution not found")
		case domain.ErrContributionAlreadyConfirmed:
			return nil, huma.Error409Conflict("contribution is already confirmed")
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
	rejected, err := h.contribRepo.RejectContribution(ctx, input.UserID, input.ID)
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
// Removes a contribution row. The user may delete both proposed and confirmed rows.
func (h *ContributionHandler) DeleteContribution(ctx context.Context, input *DeleteContributionInput) (*struct{}, error) {
	if err := h.contribRepo.DeleteContribution(ctx, input.UserID, input.ID); err != nil {
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
			"Optionally links to an existing transaction. Triggers goal progress recomputation.",
		Tags:     []string{"Savings Goals", "Contributions"},
		Security: []map[string][]string{{"clerk": {}}},
	}, handler.LogContribution)

	// GET /api/v1/savings-goals/{goalID}/contributions — list
	huma.Register(api, huma.Operation{
		OperationID: "listSavingsGoalContributions",
		Method:      http.MethodGet,
		Path:        "/api/v1/savings-goals/{goalID}/contributions",
		Summary:     "List savings goal contributions",
		Description: "Returns all confirmed and proposed contributions for a savings goal, newest first.",
		Tags:        []string{"Savings Goals", "Contributions"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.ListContributions)

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
		Description:   "Removes a contribution (proposed or confirmed). Triggers goal progress recomputation.",
		Tags:          []string{"Savings Goals", "Contributions"},
		Security:      []map[string][]string{{"clerk": {}}},
		DefaultStatus: http.StatusNoContent,
	}, handler.DeleteContribution)
}
