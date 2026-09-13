package operations

import (
	"context"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/stretchr/testify/require"
)

type budgetPlanServiceFunc func(context.Context, string, string) (*domain.BudgetPlanVsActualResponse, error)

func (f budgetPlanServiceFunc) GetPlanVsActual(ctx context.Context, userID, budgetID string) (*domain.BudgetPlanVsActualResponse, error) {
	return f(ctx, userID, budgetID)
}

func TestBudgetPlanHandlerRegistersReadOnlyRouteAndPreservesOwnership(t *testing.T) {
	service := budgetPlanServiceFunc(func(_ context.Context, userID, budgetID string) (*domain.BudgetPlanVsActualResponse, error) {
		require.Equal(t, "owner", userID)
		require.Equal(t, "budget", budgetID)
		return &domain.BudgetPlanVsActualResponse{BudgetID: budgetID}, nil
	})
	// Nil sync and write services make accidental recalculation impossible.
	h := NewBudgetHandler(nil, nil, nil, BudgetHandlerDeps{PlanService: service})
	_, api := humatest.New(t)
	RegisterBudgetRoutes(api, h)
	require.NotNil(t, api.OpenAPI().Paths["/api/v1/budgets/{id}/plan-vs-actual"].Get)
	result, err := h.GetPlanVsActual(context.Background(), &GetBudgetPlanInput{AuthenticatedInput: inputs.AuthenticatedInput{UserID: "owner"}, ID: "budget"})
	require.NoError(t, err)
	require.Equal(t, "budget", result.Body.BudgetID)
}

func TestBudgetPlanSchemaCollectionsAreNotNullable(t *testing.T){
	_,api:=humatest.New(t)
	RegisterBudgetRoutes(api,NewBudgetHandler(nil,nil,nil))
	for _,entry:=range []struct{model,field string}{{"BudgetPlanVsActualResponse","lines"},{"BudgetPlanVsActualResponse","reasons"},{"BudgetPlanLine","reasons"},{"BudgetPlanIncome","reasons"}}{
		schema:=api.OpenAPI().Components.Schemas.Map()[entry.model].Properties[entry.field]
		require.False(t,schema.Nullable,entry.model+"."+entry.field)
	}
}
