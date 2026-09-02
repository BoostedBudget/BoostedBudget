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

// Issue #1569 Task 5 (#1572) — the automation audit trail's endpoint.
//
// Without this, adding the missing INSERT would have shipped something with no observable effect:
// rule_execution_history had no repository, no service and no route, so a row written to it could
// not be seen by anyone. Mirrors GET /api/v1/boost-executions, which solved the same problem for
// boost_executions in task 14.

// RuleExecutionHandler serves the account-wide automation history.
type RuleExecutionHandler struct {
	service services.RuleExecutionHistoryService
}

// NewRuleExecutionHandler creates a RuleExecutionHandler.
func NewRuleExecutionHandler(service services.RuleExecutionHistoryService) *RuleExecutionHandler {
	return &RuleExecutionHandler{service: service}
}

// GetUserRuleExecutionsInput is the account-wide automation history request.
//
// Deliberately has NO id path parameter, for the same reason GetUserBoostExecutionsInput does not:
// fk_rule_execution_history_rule_id is ON DELETE SET NULL and rule deletion is a HARD delete, so an
// execution whose rule has been deleted can only ever be reached by a read keyed on the user.
type GetUserRuleExecutionsInput struct {
	inputs.AuthenticatedInput
	Limit  int `query:"limit" default:"20" minimum:"1" maximum:"100" doc:"Maximum number of results"`
	Offset int `query:"offset" default:"0" minimum:"0" doc:"Number of results to skip"`
}

// GetUserRuleExecutionsOutput is the account-wide automation history response.
type GetUserRuleExecutionsOutput struct {
	Body *domain.RuleExecutionsResponse
}

// GetUserRuleExecutions handles GET /api/v1/rule-executions.
//
// Tenancy: there is deliberately no rule-ownership check, because a deleted rule has no owner left
// to check. Scoping is the authenticated user id from AuthenticatedInput, passed straight to the
// service, which filters on the execution row's own denormalised user_id. Nothing client-supplied
// reaches that filter.
func (h *RuleExecutionHandler) GetUserRuleExecutions(
	ctx context.Context,
	input *GetUserRuleExecutionsInput,
) (*GetUserRuleExecutionsOutput, error) {
	if h.service == nil {
		return nil, huma.Error500InternalServerError("Rule execution history service not available")
	}

	executions, total, err := h.service.GetUserExecutionHistory(ctx, input.UserID, input.Limit, input.Offset)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	if executions == nil {
		executions = []*domain.RuleExecution{}
	}

	return &GetUserRuleExecutionsOutput{
		Body: &domain.RuleExecutionsResponse{
			Executions: executions,
			Total:      total,
			Limit:      input.Limit,
			Offset:     input.Offset,
		},
	}, nil
}

// RegisterRuleExecutionRoutes registers the automation history route.
// Note: this route requires the Clerk middleware to be applied.
//
// ⚠ THE PATH IS A SIBLING OF /api/v1/auto-categorization, NOT A CHILD, and that is deliberate.
// /api/v1/auto-categorization/{id} already exists, so /api/v1/auto-categorization/executions would
// share a prefix with a {param} sibling. Production is humaecho.NewV4 (Echo v4's radix tree, where a
// static segment beats a param in either registration order), but humatest — which every router
// test in this package uses — is registration-order sensitive, both measured in
// TestRouterPrecedence_ProductionEchoAlwaysPrefersTheLiteralSegment and
// TestRouterPrecedence_HumatestIsRegistrationOrderSensitive (cmd/server). Choosing the hyphenated
// sibling sidesteps the difference entirely and matches the existing convention
// (/api/v1/boost-executions, /api/v1/savings-goals, /api/v1/user-accounts).
//
// Reachability is asserted at the ROUTER level by
// TestRuleExecutionRoutes_PathIsReachableAndNotSwallowedByARuleIDWildcard — a handler test would
// not have been evidence that the route resolves.
func RegisterRuleExecutionRoutes(api huma.API, handler *RuleExecutionHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "getUserRuleExecutions",
		Method:      http.MethodGet,
		Path:        "/api/v1/rule-executions",
		Summary:     "Get automation rule execution history",
		Description: "Retrieves categorization-rule executions for the authenticated user, " +
			"newest first. A row exists for each time a rule actually CHANGED a transaction's " +
			"category or tags; a rule that matched but changed nothing is counted in the rule's " +
			"match_count and is not recorded here. Executions whose rule has since been deleted " +
			"are still returned, carrying rule_id = null plus rule_name and definition_snapshot " +
			"captured at execution time. One class is withheld: an execution against a pending " +
			"transaction the provider removed when it settled, whose posted twin carries its own " +
			"execution — showing both would report one charge as two automation runs. Executions " +
			"against a transaction the provider reversed AFTER it posted ARE returned, because " +
			"that transaction has no twin and is itself still surfacable.",
		Tags:     []string{"Auto-Categorization"},
		Security: []map[string][]string{{"clerk": {}}},
	}, handler.GetUserRuleExecutions)
}
