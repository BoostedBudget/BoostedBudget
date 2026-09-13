package services

import (
	"context"
	"errors"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type atomicBudgetSyncFixture struct {
	MockBudgetSyncRepository
	calls int
	err   error
}

func (f *atomicBudgetSyncFixture) RecalculateBudgetActuals(context.Context, string, string, bool) (*domain.Budget, error) {
	f.calls++
	return nil, f.err
}
func TestBudgetAtomicSyncDelegatesEligibilityAndPropagatesBatchFailure(t *testing.T) {
	repo := &atomicBudgetSyncFixture{}
	income := new(MockBudgetIncomeCalculator)
	svc := NewBudgetSyncService(repo, income, new(MockBudgetSpentCalculator), testSyncServiceLogger())
	b := &domain.Budget{ID: "budget", UserID: "user", Period: domain.BudgetPeriodMonthly, StartDate: time.Now().UTC().AddDate(0, 0, -1)}
	require.NoError(t, svc.RecalculateBudget(context.Background(), b))
	require.Equal(t, 1, repo.calls, "eligibility must be resolved after the repository locks fresh configuration")
	b.IsActive = true
	repo.err = errors.New("incomplete savings")
	require.ErrorContains(t, svc.RecalculateBudget(context.Background(), b), "incomplete savings")
	require.Equal(t, 2, repo.calls)
}
