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

// ContributionProgressRecomputer triggers savings progress recomputation after a contribution write.
// Narrow interface (M20) to avoid importing the full SavingsProgressService.
type ContributionProgressRecomputer interface {
	RecomputeUser(ctx context.Context, userID string) error
}

// ContributionHandler handles savings goal contribution HTTP requests (M20).
type ContributionHandler struct {
	contribRepo     repository.SavingsContributionRepository
	progressService ContributionProgressRecomputer // optional; nil = skip goal recompute after write
}

// NewContributionHandler creates a new ContributionHandler.
// progressService is optional; pass nil to disable post-write goal recomputation.
func NewContributionHandler(
	contribRepo repository.SavingsContributionRepository,
	progressService ContributionProgressRecomputer,
) *ContributionHandler {
	return &ContributionHandler{
		contribRepo:     contribRepo,
		progressService: progressService,
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
func (h *ContributionHandler) LogContribution(ctx context.Context, input *LogContributionInput) (*LogContributionOutput, error) {
	if err := input.Body.Validate(); err != nil {
		return nil, huma.Error422UnprocessableEntity("invalid contribution", err)
	}

	contrib := &domain.SavingsGoalContribution{
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

	if err := h.contribRepo.Insert(ctx, input.UserID, contrib); err != nil {
		if err == domain.ErrSavingsGoalNotFound {
			return nil, huma.Error404NotFound("savings goal not found")
		}
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	// Recompute goal progress after logging a contribution (non-fatal if not wired).
	if h.progressService != nil {
		if err := h.progressService.RecomputeUser(ctx, input.UserID); err != nil {
			// Non-fatal — contribution was persisted; progress will be reconciled at next sync.
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

	// Recompute goal progress after confirmation (non-fatal if not wired).
	if h.progressService != nil {
		if err := h.progressService.RecomputeUser(ctx, input.UserID); err != nil {
			_ = err
		}
	}

	return &ConfirmContributionOutput{Body: confirmed}, nil
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

	// Recompute goal progress after deletion (non-fatal if not wired).
	if h.progressService != nil {
		if err := h.progressService.RecomputeUser(ctx, input.UserID); err != nil {
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
