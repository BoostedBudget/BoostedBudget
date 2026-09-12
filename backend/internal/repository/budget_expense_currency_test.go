package repository

import (
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestBudgetPreviewRejectsUnverifiableCurrencyTotals(t *testing.T) {
	category := domain.BudgetCategory{ID: "other", CategoryKind: "remainder"}
	for _, currency := range []string{"EUR", ""} {
		t.Run(currency, func(t *testing.T) {
			_, err := budgetExpensePreview([]domain.BudgetCategory{category}, category, []domain.BudgetExpenseFact{{ID: "usd", Amount: 100, CurrencyCode: "USD"}, {ID: "foreign", Amount: 100, CurrencyCode: currency}})
			require.Error(t, err)
		})
	}
}
