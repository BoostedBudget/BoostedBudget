package operations

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	humaerrors "github.com/maxnussbaum/BoostedBudget/internal/huma/errors"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
)

type GetBudgetPlanInput struct {
	inputs.AuthenticatedInput
	ID string `path:"id" doc:"Budget ID"`
}
type GetBudgetPlanOutput struct {
	Body *domain.BudgetPlanVsActualResponse
}

func (h *BudgetHandler) GetPlanVsActual(ctx context.Context, input *GetBudgetPlanInput) (*GetBudgetPlanOutput, error) {
	if h.planService == nil {
		return nil, huma.Error503ServiceUnavailable("Budget activity is unavailable")
	}
	report, err := h.planService.GetPlanVsActual(ctx, input.UserID, input.ID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	return &GetBudgetPlanOutput{Body: report}, nil
}

func registerBudgetPlanRoute(api huma.API, h *BudgetHandler) {
	huma.Register(api, huma.Operation{OperationID: "getBudgetPlanVsActual", Method: http.MethodGet, Path: "/api/v1/budgets/{id}/plan-vs-actual", Summary: "Get budget plan and activity", Description: "Compare the current budget plan with posted activity in its eligible current period. Unknown actuals remain null. This read does not synchronize or run automation.", Tags: []string{"Budgets"}, Security: []map[string][]string{{"clerk": {}}}}, h.GetPlanVsActual)
}
