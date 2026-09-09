package operations

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	humaerrors "github.com/maxnussbaum/BoostedBudget/internal/huma/errors"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// SavingsGoalHandler handles savings goal-related HTTP requests.
// Issue #28: Savings goals
type SavingsGoalHandler struct {
	service         services.SavingsGoalService
	progressService services.SavingsProgressService // optional; nil disables progress-summary endpoint
	idempotencyRepo repository.IdempotencyRepository // optional; nil disables dedup
	db              repository.DB
	mcpAuditRepo    repository.MCPAuditRepository
	accountGetter   services.AccountGetter
	mcpWorkerSecret string
}

type SavingsGoalHandlerDeps struct {
	ProgressService services.SavingsProgressService
	IdempotencyRepo repository.IdempotencyRepository
	DB              repository.DB
	MCPAuditRepo    repository.MCPAuditRepository
	AccountGetter   services.AccountGetter
	MCPWorkerSecret string
}

// NewSavingsGoalHandler creates a new SavingsGoalHandler instance.
// deps are optional (pass nil/omit for spec-gen nil-stubs); when non-nil,
// createSavingsGoal requests with an Idempotency-Key header are deduplicated.
func NewSavingsGoalHandler(service services.SavingsGoalService, deps ...SavingsGoalHandlerDeps) *SavingsGoalHandler {
	h := &SavingsGoalHandler{service: service}
	if len(deps) > 0 {
		h.progressService = deps[0].ProgressService
		h.idempotencyRepo = deps[0].IdempotencyRepo
		h.db = deps[0].DB
		h.mcpAuditRepo = deps[0].MCPAuditRepo
		h.accountGetter = deps[0].AccountGetter
		h.mcpWorkerSecret = deps[0].MCPWorkerSecret
	}
	return h
}

// --- Input/Output Types ---

// CreateSavingsGoalInput represents the input for creating a savings goal.
type CreateSavingsGoalInput struct {
	inputs.AuthenticatedInput
	inputs.IdempotencyInput // reads Idempotency-Key header; empty = no deduplication
	inputs.MCPContextInput
	Body domain.CreateSavingsGoalRequest
}

// CreateSavingsGoalOutput represents the response for creating a savings goal.
type CreateSavingsGoalOutput struct {
	Body *domain.SavingsGoal
}

// GetSavingsGoalsInput represents the input for getting all savings goals.
type GetSavingsGoalsInput struct {
	inputs.AuthenticatedInput
}

// GetSavingsGoalsOutput represents the response for getting all savings goals.
type GetSavingsGoalsOutput struct {
	Body []*domain.SavingsGoal
}

// GetDeletedSavingsGoalsInput represents the input for listing deleted savings goals.
// Issue #1569, Task 12.
type GetDeletedSavingsGoalsInput struct {
	inputs.AuthenticatedInput
}

// GetDeletedSavingsGoalsOutput represents the response for listing deleted savings goals.
type GetDeletedSavingsGoalsOutput struct {
	Body []*domain.SavingsGoal
}

// GetActiveSavingsGoalsInput represents the input for getting active savings goals.
type GetActiveSavingsGoalsInput struct {
	inputs.AuthenticatedInput
}

// GetActiveSavingsGoalsOutput represents the response for getting active savings goals.
type GetActiveSavingsGoalsOutput struct {
	Body []*domain.SavingsGoal
}

// GetSavingsGoalInput represents the input for getting a specific savings goal.
type GetSavingsGoalInput struct {
	inputs.AuthenticatedInput
	ID string `path:"id" doc:"Savings goal ID"`
}

// GetSavingsGoalOutput represents the response for getting a savings goal.
type GetSavingsGoalOutput struct {
	Body *domain.SavingsGoal
}

// UpdateSavingsGoalInput represents the input for updating a savings goal.
type UpdateSavingsGoalInput struct {
	inputs.AuthenticatedInput
	ID   string `path:"id" doc:"Savings goal ID"`
	Body domain.UpdateSavingsGoalRequest
}

// UpdateSavingsGoalOutput represents the response for updating a savings goal.
type UpdateSavingsGoalOutput struct {
	Body *domain.SavingsGoal
}

// DeleteSavingsGoalInput represents the input for deleting a savings goal.
type DeleteSavingsGoalInput struct {
	inputs.AuthenticatedInput
	ID string `path:"id" doc:"Savings goal ID"`
}

// PurgeSavingsGoalInput represents the input for permanently destroying an already-deleted
// savings goal and every contribution under it. Issue #1569, Task 8.
type PurgeSavingsGoalInput struct {
	inputs.AuthenticatedInput
	ID string `path:"id" doc:"Savings goal ID"`
}

// GetSavingsProgressSummaryInput represents the input for the progress-summary analytics endpoint.
type GetSavingsProgressSummaryInput struct {
	inputs.AuthenticatedInput
	inputs.MCPContextInput
}

// GetSavingsProgressSummaryOutput represents the response for the progress-summary analytics endpoint.
type GetSavingsProgressSummaryOutput struct {
	Body *domain.SavingsProgressSummary
}

// --- Handler Methods ---

// CreateGoal handles POST /api/v1/savings-goals requests.
// Creates a new savings goal for the authenticated user.
func (h *SavingsGoalHandler) CreateGoal(ctx context.Context, input *CreateSavingsGoalInput) (*CreateSavingsGoalOutput, error) {
	// MCP origin: use transactional audit with atomic idempotency (R6-SG: mirror tags/categories).
	if input.IsMCPOrigin(h.mcpWorkerSecret) && h.db != nil && h.mcpAuditRepo != nil && h.accountGetter != nil {
		tx, err := h.db.Begin(ctx)
		if err != nil {
			return nil, humaerrors.LoggingMapError(ctx, err)
		}
		defer rollbackMCPAuditTx(ctx, tx)

		if h.idempotencyRepo != nil && input.IdempotencyKey != "" {
			var goal *domain.SavingsGoal
			cacheHit, err := h.idempotencyRepo.GetOrCreateInTx(ctx, tx, input.IdempotencyKey, input.UserID, "createSavingsGoal",
				func() (interface{}, error) {
					txGoalRepo := repository.NewSavingsGoalRepository(tx)
					txGoalService := services.NewSavingsGoalService(txGoalRepo, h.accountGetter, slog.Default())
					// #1625 Task 17: wired over the SAME tx. This call only creates, so the ledger
					// is unused today — but an unwired service REFUSES every progress_basis change
					// (repository.ErrUnguardedBasisChange), and leaving the hole here would turn
					// the day somebody adds an update beside this line into a defect instead of a
					// compile-time complaint. Pinned by
					// TestEverySavingsGoalServiceConstructionWiresTheBasisSwitchLedger.
					txGoalService.SetBasisSwitchContribRepo(repository.NewSavingsContributionRepository(tx))
					return txGoalService.CreateGoal(ctx, input.UserID, &input.Body)
				}, &goal)
			if err != nil {
				return nil, humaerrors.LoggingMapError(ctx, err)
			}
			if cacheHit {
				return &CreateSavingsGoalOutput{Body: goal}, nil
			}
			if err := recordMCPWriteAuditInTx(ctx, h.mcpAuditRepo, tx, input.UserID, input.MCPGrantID, "createSavingsGoal", input.IdempotencyKey); err != nil {
				return nil, humaerrors.LoggingMapError(ctx, err)
			}
			if err := tx.Commit(ctx); err != nil {
				return nil, humaerrors.LoggingMapError(ctx, err)
			}
			return &CreateSavingsGoalOutput{Body: goal}, nil
		}

		txGoalRepo := repository.NewSavingsGoalRepository(tx)
		txGoalService := services.NewSavingsGoalService(txGoalRepo, h.accountGetter, slog.Default())
		// Same reasoning as the idempotent branch above — see that comment.
		txGoalService.SetBasisSwitchContribRepo(repository.NewSavingsContributionRepository(tx))
		goal, err := txGoalService.CreateGoal(ctx, input.UserID, &input.Body)
		if err != nil {
			return nil, humaerrors.LoggingMapError(ctx, err)
		}

		if err := recordMCPWriteAuditInTx(ctx, h.mcpAuditRepo, tx, input.UserID, input.MCPGrantID, "createSavingsGoal", input.IdempotencyKey); err != nil {
			return nil, humaerrors.LoggingMapError(ctx, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, humaerrors.LoggingMapError(ctx, err)
		}
		return &CreateSavingsGoalOutput{Body: goal}, nil
	}

	// Idempotency dedup without MCP audit (non-MCP path)
	if h.idempotencyRepo != nil && input.IdempotencyKey != "" {
		var goal *domain.SavingsGoal
		_, err := h.idempotencyRepo.GetOrCreate(ctx, input.IdempotencyKey, input.UserID, "createSavingsGoal",
			func() (interface{}, error) {
				return h.service.CreateGoal(ctx, input.UserID, &input.Body)
			}, &goal)
		if err != nil {
			return nil, humaerrors.LoggingMapError(ctx, err)
		}
		return &CreateSavingsGoalOutput{Body: goal}, nil
	}

	// No dedup
	goal, err := h.service.CreateGoal(ctx, input.UserID, &input.Body)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	return &CreateSavingsGoalOutput{Body: goal}, nil
}

// GetGoals handles GET /api/v1/savings-goals requests.
// Retrieves all savings goals for the authenticated user.
func (h *SavingsGoalHandler) GetGoals(ctx context.Context, input *GetSavingsGoalsInput) (*GetSavingsGoalsOutput, error) {
	goals, err := h.service.GetGoals(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	// Return empty array instead of null
	if goals == nil {
		goals = []*domain.SavingsGoal{}
	}

	return &GetSavingsGoalsOutput{Body: goals}, nil
}

// GetDeletedGoals handles GET /api/v1/savings-goals/deleted requests.
//
// The counterpart to POST /savings-goals/{id}/purge (Task 8). That endpoint refuses unless the
// goal is already deleted, which made it unreachable: no other read in the codebase can return a
// tombstoned goal, so nothing could tell a client which ids satisfy the precondition. Issue
// #1569, Task 12.
func (h *SavingsGoalHandler) GetDeletedGoals(ctx context.Context, input *GetDeletedSavingsGoalsInput) (*GetDeletedSavingsGoalsOutput, error) {
	goals, err := h.service.GetDeletedGoals(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	if goals == nil {
		goals = []*domain.SavingsGoal{}
	}
	return &GetDeletedSavingsGoalsOutput{Body: goals}, nil
}

// GetActiveGoals handles GET /api/v1/savings-goals/active requests.
// Retrieves all active (not completed) savings goals for the authenticated user.
func (h *SavingsGoalHandler) GetActiveGoals(ctx context.Context, input *GetActiveSavingsGoalsInput) (*GetActiveSavingsGoalsOutput, error) {
	goals, err := h.service.GetActiveGoals(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	// Return empty array instead of null
	if goals == nil {
		goals = []*domain.SavingsGoal{}
	}

	return &GetActiveSavingsGoalsOutput{Body: goals}, nil
}

// GetGoal handles GET /api/v1/savings-goals/:id requests.
// Retrieves a specific savings goal by ID.
func (h *SavingsGoalHandler) GetGoal(ctx context.Context, input *GetSavingsGoalInput) (*GetSavingsGoalOutput, error) {
	goal, err := h.service.GetGoal(ctx, input.UserID, input.ID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &GetSavingsGoalOutput{Body: goal}, nil
}

// UpdateGoal handles PUT /api/v1/savings-goals/:id requests.
// Updates a savings goal's properties.
func (h *SavingsGoalHandler) UpdateGoal(ctx context.Context, input *UpdateSavingsGoalInput) (*UpdateSavingsGoalOutput, error) {
	goal, err := h.service.UpdateGoal(ctx, input.UserID, input.ID, &input.Body)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &UpdateSavingsGoalOutput{Body: goal}, nil
}

// DeleteGoal handles DELETE /api/v1/savings-goals/:id requests.
// Removes a savings goal from the user's collection.
func (h *SavingsGoalHandler) DeleteGoal(ctx context.Context, input *DeleteSavingsGoalInput) (*struct{}, error) {
	err := h.service.DeleteGoal(ctx, input.UserID, input.ID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return nil, nil
}

// PurgeGoal handles POST /api/v1/savings-goals/:id/purge requests.
// Permanently destroys an already-deleted goal and every contribution under it. Issue #1569,
// Task 8: DELETE is now non-destructive, and this is the second, explicit step that actually
// erases — it returns 409 on a goal the user has not deleted first.
func (h *SavingsGoalHandler) PurgeGoal(ctx context.Context, input *PurgeSavingsGoalInput) (*struct{}, error) {
	if err := h.service.PurgeGoal(ctx, input.UserID, input.ID); err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	return nil, nil
}

// GetProgressSummary handles GET /api/v1/savings-goals/progress-summary requests.
// Returns basis-aware savings progress for all active goals plus portfolio totals.
// Tagged Analytics so MCP/LLM consumers get a single-round-trip aggregate (Codex C11).
func (h *SavingsGoalHandler) GetProgressSummary(ctx context.Context, input *GetSavingsProgressSummaryInput) (*GetSavingsProgressSummaryOutput, error) {
	if h.progressService == nil {
		return nil, huma.Error503ServiceUnavailable("progress service not configured")
	}
	summary, err := h.progressService.GetProgressSummary(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	return &GetSavingsProgressSummaryOutput{Body: summary}, nil
}

// RegisterSavingsGoalRoutes registers savings goal routes with the Huma API.
// Note: These routes require the Clerk middleware to be applied.
func RegisterSavingsGoalRoutes(api huma.API, handler *SavingsGoalHandler) {
	// POST /api/v1/savings-goals - Create savings goal
	huma.Register(api, huma.Operation{
		OperationID:   "createSavingsGoal",
		Method:        http.MethodPost,
		Path:          "/api/v1/savings-goals",
		Summary:       "Create savings goal",
		Description:   "Creates a new savings goal for the authenticated user.",
		Tags:          []string{"Savings Goals"},
		Security:      []map[string][]string{{"clerk": {}}},
		DefaultStatus: http.StatusCreated,
	}, handler.CreateGoal)

	// GET /api/v1/savings-goals - Get all savings goals
	huma.Register(api, huma.Operation{
		OperationID: "getSavingsGoals",
		Method:      http.MethodGet,
		Path:        "/api/v1/savings-goals",
		Summary:     "Get savings goals",
		Description: "Retrieves all savings goals for the authenticated user.",
		Tags:        []string{"Savings Goals"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetGoals)

	// GET /api/v1/savings-goals/active - Get active savings goals (must be before :id route)
	huma.Register(api, huma.Operation{
		OperationID: "getActiveSavingsGoals",
		Method:      http.MethodGet,
		Path:        "/api/v1/savings-goals/active",
		Summary:     "Get active savings goals",
		Description: "Retrieves all active (not completed) savings goals for the authenticated user.",
		Tags:        []string{"Savings Goals"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetActiveGoals)

	// GET /api/v1/savings-goals/deleted - List deleted goals (#1569 Task 12).
	//
	// The neighbouring registrations say "(must be before :id route)". That is FALSE for this
	// router — a static segment beats a parameter whatever the registration order, verified by
	// moving this block after {id} and still getting 200 — so the claim is not repeated here.
	// The property is held by TestSavingsGoalDeletedRoute_IsItsOwnOperation, not by
	// where these lines happen to sit. It is nonetheless placed with the other collection routes
	// for readability.
	huma.Register(api, huma.Operation{
		OperationID: "getDeletedSavingsGoals",
		Method:      http.MethodGet,
		Path:        "/api/v1/savings-goals/deleted",
		Summary:     "Get deleted savings goals",
		Description: "Lists savings goals the user has deleted. Deleting a goal retains it and every contribution STILL logged against it (Issue #1569) — a contribution the user explicitly deleted earlier via DELETE /savings-goals/{goalID}/contributions/{id} was destroyed then and does not come back here. This endpoint is how the client shows what was kept and offers POST /savings-goals/{id}/purge, which refuses unless the goal is already deleted. Each entry carries deleted_at.",
		Tags:        []string{"Savings Goals"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetDeletedGoals)

	// GET /api/v1/savings-goals/progress-summary - Basis-aware progress analytics (must be before :id route)
	huma.Register(api, huma.Operation{
		OperationID: "getSavingsProgressSummary",
		Method:      http.MethodGet,
		Path:        "/api/v1/savings-goals/progress-summary",
		Summary:     "Get savings progress summary",
		Description: "Returns basis-aware savings progress for all active goals plus portfolio totals. " +
			"Each goal exposes progress_basis, computed_from, freshness, and confirmed_contributions_total " +
			"so AI/MCP consumers can interpret mixed-basis current_amount values correctly (Issue #1300 Codex C11).",
		Tags:     []string{"Savings Goals", "Analytics"},
		Security: []map[string][]string{{"clerk": {}}},
	}, handler.GetProgressSummary)

	// GET /api/v1/savings-goals/:id - Get specific savings goal
	huma.Register(api, huma.Operation{
		OperationID: "getSavingsGoal",
		Method:      http.MethodGet,
		Path:        "/api/v1/savings-goals/{id}",
		Summary:     "Get savings goal",
		Description: "Retrieves a specific savings goal by ID.",
		Tags:        []string{"Savings Goals"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetGoal)

	// PUT /api/v1/savings-goals/:id - Update savings goal
	huma.Register(api, huma.Operation{
		OperationID: "updateSavingsGoal",
		Method:      http.MethodPut,
		Path:        "/api/v1/savings-goals/{id}",
		Summary:     "Update savings goal",
		Description: "Updates a savings goal's properties.",
		Tags:        []string{"Savings Goals"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.UpdateGoal)

	// DELETE /api/v1/savings-goals/:id - Delete savings goal
	huma.Register(api, huma.Operation{
		OperationID:   "deleteSavingsGoal",
		Method:        http.MethodDelete,
		Path:          "/api/v1/savings-goals/{id}",
		Summary:       "Delete savings goal",
		Description:   "Removes a savings goal from the user's collection. Destroys nothing ON ITS OWN — every contribution still logged against the goal is retained and stays readable at GET /savings-goals/deleted/{goalID}/contributions (the live GET /savings-goals/{goalID}/contributions returns nothing once the goal is deleted). It does not resurrect a contribution the user had already deleted explicitly, which was destroyed at that point. Use POST /savings-goals/{id}/purge to permanently erase a deleted goal and its remaining contributions.",
		Tags:          []string{"Savings Goals"},
		Security:      []map[string][]string{{"clerk": {}}},
		DefaultStatus: http.StatusNoContent,
	}, handler.DeleteGoal)

	// POST /api/v1/savings-goals/:id/purge - Purge savings goal (Issue #1569, Task 8)
	huma.Register(api, huma.Operation{
		OperationID:   "purgeSavingsGoal",
		Method:        http.MethodPost,
		Path:          "/api/v1/savings-goals/{id}/purge",
		Summary:       "Purge savings goal",
		Description:   "Permanently destroys a savings goal and every contribution logged against it — including amounts the user entered by hand, which no provider can supply again. This cannot be undone. Refuses with 409 unless the goal has already been deleted (DELETE /savings-goals/{id} first); that two-step is the confirmation.",
		Tags:          []string{"Savings Goals"},
		Security:      []map[string][]string{{"clerk": {}}},
		DefaultStatus: http.StatusNoContent,
	}, handler.PurgeGoal)
}
