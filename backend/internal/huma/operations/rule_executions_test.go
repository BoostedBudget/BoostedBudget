package operations

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// Issue #1569 Task 5 (#1572) — the automation audit trail's read path.

type mockRuleExecutionHistoryService struct {
	mock.Mock
}

func (m *mockRuleExecutionHistoryService) GetUserExecutionHistory(
	ctx context.Context, userID string, limit, offset int,
) ([]*domain.RuleExecution, int, error) {
	args := m.Called(ctx, userID, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Int(1), args.Error(2)
	}
	return args.Get(0).([]*domain.RuleExecution), args.Int(1), args.Error(2)
}

var _ services.RuleExecutionHistoryService = (*mockRuleExecutionHistoryService)(nil)

func strp(s string) *string { return &s }

// TestGetUserRuleExecutions_ReturnsTheTrail covers the handler contract.
func TestGetUserRuleExecutions_ReturnsTheTrail(t *testing.T) {
	svc := new(mockRuleExecutionHistoryService)
	handler := NewRuleExecutionHandler(svc)

	rows := []*domain.RuleExecution{{
		ID:                 "exec-1",
		RuleID:             strp("rule-1"),
		TransactionID:      strp("tx-1"),
		UserID:             "user-1572",
		RuleName:           strp("Coffee is Food"),
		ActionType:         domain.RuleExecutionActionCategory,
		Result:             domain.RuleExecutionResultApplied,
		DefinitionSnapshot: json.RawMessage(`{"name":"Coffee is Food"}`),
	}}
	svc.On("GetUserExecutionHistory", mock.Anything, "user-1572", 20, 0).Return(rows, 1, nil)

	out, err := handler.GetUserRuleExecutions(context.Background(), &GetUserRuleExecutionsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1572"},
		Limit:              20,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, out.Body.Total)
	require.Len(t, out.Body.Executions, 1)
	assert.Equal(t, "exec-1", out.Body.Executions[0].ID)
	svc.AssertExpectations(t)
}

// TestGetUserRuleExecutions_ScopesToTheAuthenticatedUser: the only user id that reaches the service
// is the authenticated one. Nothing in the input can redirect it.
func TestGetUserRuleExecutions_ScopesToTheAuthenticatedUser(t *testing.T) {
	svc := new(mockRuleExecutionHistoryService)
	handler := NewRuleExecutionHandler(svc)

	svc.On("GetUserExecutionHistory", mock.Anything, "user-owner", 20, 0).
		Return([]*domain.RuleExecution{}, 0, nil)

	_, err := handler.GetUserRuleExecutions(context.Background(), &GetUserRuleExecutionsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-owner"},
		Limit:              20,
	})
	require.NoError(t, err)

	// ⚠ AssertExpectations FIRST (D8). Without it the loop below asserts nothing at all when the
	// service was never called — a vacuous pass, and the exact shape this project keeps finding.
	svc.AssertExpectations(t)
	require.NotEmpty(t, svc.Calls, "the handler must actually have reached the service")
	for _, c := range svc.Calls {
		assert.Equal(t, "user-owner", c.Arguments.String(1),
			"the only tenancy filter is the authenticated user id")
	}
}

// TestGetUserRuleExecutions_EmptyTrailIsAnArrayNotNull — a user with no automation history must get
// `"executions": []`, not `null`. The distinction matters to a client deciding between "no
// automation has run" and "the field is missing".
func TestGetUserRuleExecutions_EmptyTrailIsAnArrayNotNull(t *testing.T) {
	svc := new(mockRuleExecutionHistoryService)
	handler := NewRuleExecutionHandler(svc)

	svc.On("GetUserExecutionHistory", mock.Anything, "user-empty", 20, 0).
		Return(nil, 0, nil)

	out, err := handler.GetUserRuleExecutions(context.Background(), &GetUserRuleExecutionsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-empty"},
		Limit:              20,
	})
	require.NoError(t, err)
	require.NotNil(t, out.Body.Executions)

	encoded, err := json.Marshal(out.Body)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"executions":[]`)
}

func TestGetUserRuleExecutions_ServiceErrorIsMapped(t *testing.T) {
	svc := new(mockRuleExecutionHistoryService)
	handler := NewRuleExecutionHandler(svc)

	svc.On("GetUserExecutionHistory", mock.Anything, "user-1572", 20, 0).
		Return(nil, 0, errors.New("database is on fire"))

	_, err := handler.GetUserRuleExecutions(context.Background(), &GetUserRuleExecutionsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1572"},
		Limit:              20,
	})
	require.Error(t, err)
}

func TestGetUserRuleExecutions_NilServiceIs500(t *testing.T) {
	handler := NewRuleExecutionHandler(nil)
	_, err := handler.GetUserRuleExecutions(context.Background(), &GetUserRuleExecutionsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1572"},
		Limit:              20,
	})
	require.Error(t, err)
}

// TestRuleExecutionRoutes_PathIsReachableAndNotSwallowedByARuleIDWildcard proves at the ROUTER
// level that the endpoint resolves. A handler test above calls the function directly and never
// touches the router, so it is not evidence that any request can reach it.
//
// The rule routes are registered FIRST here, on purpose, so /api/v1/auto-categorization/{id} is in
// the tree before the history route. That is the adversarial order: humatest.New's router is
// registration-order sensitive (measured in cmd/server's
// TestRouterPrecedence_HumatestIsRegistrationOrderSensitive), so a history path registered under
// /api/v1/auto-categorization/ would be swallowed by the {id} wildcard here. It is a sibling
// instead, so it cannot be.
//
// ⚠ Production is humaecho.NewV4, not humatest — do not read a humatest verdict as production
// routing. See TestRouterPrecedence_ProductionEchoAlwaysPrefersTheLiteralSegment.
func TestRuleExecutionRoutes_PathIsReachableAndNotSwallowedByARuleIDWildcard(t *testing.T) {
	execSvc := new(mockRuleExecutionHistoryService)
	execSvc.On("GetUserExecutionHistory", mock.Anything, "user-routing", 20, 0).
		Return([]*domain.RuleExecution{{ID: "exec-1", UserID: "user-routing"}}, 1, nil)

	// A permissive stub for the WRONG handler, so a misrouted request produces this test's own
	// assertion message rather than an opaque panic or 404.
	ruleSvc := &routingSpyRuleService{
		mockCategorizationRuleService: mockCategorizationRuleService{getErr: errors.New("rule not found")},
	}

	_, api := humatest.New(t)
	api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		next(huma.WithValue(ctx, inputs.UserIDKey, "user-routing"))
	})

	RegisterCategorizationRuleRoutes(api, NewCategorizationRuleHandler(ruleSvc))
	RegisterRuleExecutionRoutes(api, NewRuleExecutionHandler(execSvc))

	resp := api.Get("/api/v1/rule-executions")
	require.Equal(t, 200, resp.Code, "body: %s", resp.Body.String())
	assert.Contains(t, resp.Body.String(), `"total":1`)

	assert.Zero(t, ruleSvc.getRuleByIDCalls,
		"the /rule-executions request was dispatched to GetRuleByID — the literal path was "+
			"swallowed by the /auto-categorization/{id} wildcard")
	execSvc.AssertExpectations(t)
}

// routingSpyRuleService records whether the rule-by-id handler ran, which is how this file tells a
// correctly routed request from one swallowed by a {param} sibling.
type routingSpyRuleService struct {
	mockCategorizationRuleService
	getRuleByIDCalls int
}

func (s *routingSpyRuleService) GetRuleByID(ctx context.Context, userID, ruleID string) (*domain.CategorizationRule, error) {
	s.getRuleByIDCalls++
	return s.mockCategorizationRuleService.GetRuleByID(ctx, userID, ruleID)
}
