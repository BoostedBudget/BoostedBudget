package services

import (
	"context"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/require"
)

type budgetPlanReaderFunc func(context.Context, string, string, time.Time) (*domain.BudgetPlanVsActualResponse, error)

func (f budgetPlanReaderFunc) GetPlanVsActual(ctx context.Context, userID, budgetID string, asOf time.Time) (*domain.BudgetPlanVsActualResponse, error) {
	return f(ctx, userID, budgetID, asOf)
}

func TestBudgetPlanServiceUsesAuthenticatedOwnerAndOneAsOf(t *testing.T) {
	before := time.Now()
	called := false
	reader := budgetPlanReaderFunc(func(_ context.Context, userID, budgetID string, asOf time.Time) (*domain.BudgetPlanVsActualResponse, error) {
		called = true
		require.Equal(t, "owner", userID)
		require.Equal(t, "budget", budgetID)
		require.False(t, asOf.Before(before))
		require.False(t, asOf.After(time.Now()))
		return nil, domain.ErrBudgetNotFound
	})
	_, err := NewBudgetPlanService(reader).GetPlanVsActual(context.Background(), "owner", "budget")
	require.ErrorIs(t, err, domain.ErrBudgetNotFound)
	require.True(t, called)
}
