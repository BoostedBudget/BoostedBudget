package operations

import (
	"context"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestBudgetDrilldownUsesCurrentUTCClippedPeriod(t *testing.T) {
	b := createTestBudget()
	b.StartDate = time.Now().UTC().AddDate(-1, 0, 0)
	b.EndDate = nil
	repo := &mockTransactionRepoForBudget{}
	h := NewBudgetHandler(&mockBudgetService{budget: b}, &mockBudgetSyncService{}, services.NewTransactionService(repo, &mockCustomCategoryRepoForBudget{}))
	input := &GetCategoryTransactionsInput{AuthenticatedInput: inputs.AuthenticatedInput{UserID: b.UserID}, ID: b.ID, CategoryID: b.Categories[0].ID, Limit: 20}
	_, err := h.GetCategoryTransactions(context.Background(), input)
	require.NoError(t, err)
	period := domain.CurrentBudgetActualsRange(b, time.Now())
	require.Equal(t, period.Start, repo.start)
	require.Equal(t, period.ActualThrough, repo.end)
	b.IsActive = false
	out, err := h.GetCategoryTransactions(context.Background(), input)
	require.NoError(t, err)
	require.Empty(t, out.Body.Transactions)
	require.Equal(t, 1, repo.calls)
}

func TestBudgetUnbudgetedDrilldownUsesOwnedBudgetContext(t *testing.T) {
	b := createTestBudget()
	repo := &mockTransactionRepoForBudget{totalCount: 1, totalAmount: 40}
	h := NewBudgetHandler(&mockBudgetService{budget: b}, &mockBudgetSyncService{}, services.NewTransactionService(repo, &mockCustomCategoryRepoForBudget{}))
	out, err := h.GetCategoryTransactions(context.Background(), &GetCategoryTransactionsInput{AuthenticatedInput: inputs.AuthenticatedInput{UserID: b.UserID}, ID: b.ID, CategoryID: "unbudgeted", Limit: 20})
	require.NoError(t, err)
	require.Equal(t, 40.0, out.Body.TotalAmount)
	require.Equal(t, 1, repo.calls)
}
