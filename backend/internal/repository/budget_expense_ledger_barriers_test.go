package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// budgetExpenseFactRows builds the 13-column row set readBudgetExpenseFacts scans, carrying one
// posted expense fact that matches a FOOD_AND_DRINK pfc line and whose savings provenance cannot
// be interpreted. CalculateBudgetExpenseLedger therefore withholds that line (Amount nil, Reason
// set) instead of publishing a figure it cannot trust — the exact shape every sibling read barrier
// must report.
func budgetExpenseFactRows() *pgxmock.Rows {
	return pgxmock.NewRows([]string{
		"id", "amount", "pfc_primary", "pfc_detailed", "custom_category_override_id",
		"parent_id", "parent_pfc_primary", "grandparent_pfc_primary", "currency_code",
		"tags", "savings_amount", "reason", "custom_category_name",
	}).AddRow(
		"txn-1", 100.0, "FOOD_AND_DRINK", "", "", "", "", "", "USD",
		[]string{}, 0.0, "legacy_savings_origin", "",
	)
}

// TestBudgetExpenseLedgerBarriersCarrySentinel pins the #1685 contract for the six sibling read
// barriers: each must wrap domain.ErrBudgetActualsIncomplete through
// domain.NewBudgetActualsIncompleteError, so the HTTP mapper can recover the machine-readable
// reason via errors.As and answer 422-with-reason instead of an opaque 500. A bare
// fmt.Errorf("budget actuals incomplete: …") satisfies neither errors.Is nor errors.As.
func TestBudgetExpenseLedgerBarriersCarrySentinel(t *testing.T) {
	requireIncomplete := func(t *testing.T, err error, reason string) {
		t.Helper()
		require.ErrorIs(t, err, domain.ErrBudgetActualsIncomplete,
			"the barrier must wrap the sentinel so the mapper answers 422 instead of 500")
		var incomplete *domain.BudgetActualsIncompleteError
		require.ErrorAs(t, err, &incomplete,
			"the mapper recovers the machine-readable reason through errors.As")
		require.Equal(t, []string{reason}, incomplete.Reasons)
	}

	// budget_expense_ledger.go:18 and :24 — the two reasons budgetScalarCurrency can raise. All
	// four of its call sites (:33, :159, :199, transaction_repository.go:2549) return this error
	// unchanged, so pinning the source pins every caller.
	t.Run("budgetScalarCurrency reports unknown_currency", func(t *testing.T) {
		_, err := budgetScalarCurrency([]string{""}, "")
		requireIncomplete(t, err, "unknown_currency")
	})

	t.Run("budgetScalarCurrency reports mixed_currency", func(t *testing.T) {
		_, err := budgetScalarCurrency([]string{"USD", "EUR"}, "")
		requireIncomplete(t, err, "mixed_currency")
	})

	// budget_expense_ledger.go:38 — budgetExpensePreview's withheld-line barrier.
	t.Run("budgetExpensePreview reports the withheld line reason", func(t *testing.T) {
		food := "FOOD_AND_DRINK"
		category := domain.BudgetCategory{ID: "food", CategoryKind: "pfc", PFCPrimary: &food}
		_, err := budgetExpensePreview([]domain.BudgetCategory{category}, category, []domain.BudgetExpenseFact{
			{ID: "txn-1", Amount: 100, CurrencyCode: "USD", PFCPrimary: food, Reason: "legacy_savings_origin"},
		})
		requireIncomplete(t, err, "legacy_savings_origin")
	})

	// transaction_repository.go:2532, :2553 and :2629 — the three repository reads that consume
	// the ledger. Each is driven through a mocked fact read so the barrier itself is exercised,
	// not a hand-built ledger.
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, -1)
	food := "FOOD_AND_DRINK"
	for _, tc := range []struct {
		name string
		// expectTx mirrors GetTransactionsForBudgetCategory's own isolation wrapper: a pool that
		// can BeginTx gets a REPEATABLE READ READ ONLY transaction around the read.
		expectTx bool
		call     func(repo TransactionRepository) error
	}{
		{
			name: "GetSpentSumForBudgetCategory",
			call: func(repo TransactionRepository) error {
				_, err := repo.GetSpentSumForBudgetCategory(context.Background(), "user-1",
					&domain.BudgetCategory{ID: "food", CategoryKind: "pfc", PFCPrimary: &food}, start, end)
				return err
			},
		},
		{
			name: "GetMatchingTransactionStats",
			call: func(repo TransactionRepository) error {
				_, _, err := repo.GetMatchingTransactionStats(context.Background(), "user-1", "pfc",
					&food, nil, nil, nil, domain.MatchLogicCategoryOnly, start, end)
				return err
			},
		},
		{
			name:     "GetTransactionsForBudgetCategory",
			expectTx: true,
			call: func(repo TransactionRepository) error {
				_, _, _, err := repo.GetTransactionsForBudgetCategory(context.Background(), "user-1",
					&domain.BudgetCategory{ID: "food", CategoryKind: "pfc", PFCPrimary: &food}, start, end, 10, 0)
				return err
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()
			if tc.expectTx {
				mock.ExpectBegin()
				mock.ExpectExec(regexp.QuoteMeta("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY")).
					WillReturnResult(pgxmock.NewResult("SET", 0))
			}
			mock.ExpectQuery(regexp.QuoteMeta(budgetExpenseFactsQuery)).
				WithArgs("user-1", start, end).
				WillReturnRows(budgetExpenseFactRows())
			if tc.expectTx {
				mock.ExpectRollback()
			}
			requireIncomplete(t, tc.call(NewTransactionRepository(mock, nil)), "legacy_savings_origin")
		})
	}
}