//go:build integration

package repository

// Issue #1569 Task 7 — smoke coverage for the ~25 analytics queries the tombstone filter touched.
//
// WHY THIS EXISTS. The `AND t.deleted_at IS NULL` predicate was added to every transactions read in
// this package by a mechanical pass over the SQL literals. That pass produced THREE syntactically
// broken queries (transaction_repository.GetByAccountID, merchant_repository.GetDuplicateSuggestions,
// admin_user_repository.GetUserDetail), each of which compiled fine and failed only when Postgres
// parsed it. Two of the three were caught by pre-existing integration tests; the third was caught
// because a fourth query happened to be covered. Most analytics queries had no integration coverage
// at all, so a fourth break would have shipped.
//
// The assertion is deliberately shallow — every method runs, personal AND household scope, against a
// real schema, and none returns an error. It is a PARSE-AND-EXECUTE guard, not a correctness test:
// it does not prove any aggregate is right, only that the query Postgres receives is well-formed and
// that the columns it names exist. The behavioural proof that tombstones are excluded lives in
// TestTombstone_HiddenFromEveryRead_Integration.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

func TestAnalyticsQueriesParseWithTombstoneFilter_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userID, "analytics-1569-"+userID+"@example.com")
	require.NoError(t, err, "seed user")

	repo := NewAnalyticsRepository(tx)

	end := time.Now()
	start := end.AddDate(0, -3, 0)

	// Both scopes are exercised: the household variants are separate SQL with their own
	// household_accounts CTE, and the mechanical pass edited them independently.
	scopes := map[string]*domain.HouseholdScope{
		"personal":  nil,
		"household": {HouseholdID: uuid.New().String(), MemberUserIDs: []string{userID}},
	}

	for name, scope := range scopes {
		t.Run(name, func(t *testing.T) {
			cashFlow := domain.CashFlowFilters{
				StartDate: start, EndDate: end, Grouping: "monthly",
				AccountTypes: []string{"depository"}, HouseholdScope: scope,
			}
			spending := domain.SpendingFilters{
				StartDate: start, EndDate: end,
				AccountTypes: []string{"depository"}, HouseholdScope: scope,
			}

			_, err := repo.GetCashFlowAggregation(ctx, userID, cashFlow)
			require.NoError(t, err, "GetCashFlowAggregation")

			_, err = repo.GetCashFlowGrouped(ctx, userID, cashFlow)
			require.NoError(t, err, "GetCashFlowGrouped")

			_, err = repo.GetSpendingByCategory(ctx, userID, spending)
			require.NoError(t, err, "GetSpendingByCategory")

			_, err = repo.GetSpendingByMerchant(ctx, userID, spending, 10)
			require.NoError(t, err, "GetSpendingByMerchant")

			_, err = repo.GetSpendingTrends(ctx, userID, spending)
			require.NoError(t, err, "GetSpendingTrends")

			_, err = repo.GetAverageMonthlySpend(ctx, userID, spending)
			require.NoError(t, err, "GetAverageMonthlySpend")

			_, err = repo.GetSavingsRateForPeriod(ctx, userID, start, end, scope)
			require.NoError(t, err, "GetSavingsRateForPeriod")

			_, err = repo.GetMonthOverMonthSpending(ctx, userID, start, "FOOD_AND_DRINK", spending)
			require.NoError(t, err, "GetMonthOverMonthSpending")

			_, err = repo.GetIncomeSourceBreakdown(ctx, userID, spending)
			require.NoError(t, err, "GetIncomeSourceBreakdown")
		})
	}

	// Personal-only methods (no household variant exists for these).
	spending := domain.SpendingFilters{StartDate: start, EndDate: end}

	_, err = repo.GetCategoryTotalForPeriod(ctx, userID, "FOOD_AND_DRINK", start, end)
	require.NoError(t, err, "GetCategoryTotalForPeriod")

	_, err = repo.GetTopExpense(ctx, userID, start, end)
	require.NoError(t, err, "GetTopExpense")

	_, err = repo.GetRecurringSubscriptions(ctx, userID, start)
	require.NoError(t, err, "GetRecurringSubscriptions")

	_, err = repo.GetLargestTransactionsForPeriod(ctx, userID, spending, 5)
	require.NoError(t, err, "GetLargestTransactionsForPeriod")

	_, err = repo.GetCategoryBreakdownForMerchant(ctx, userID, uuid.New().String(), spending)
	require.NoError(t, err, "GetCategoryBreakdownForMerchant")

	_, err = repo.GetUnusualSpend(ctx, userID, start.AddDate(0, -3, 0), start, end, 2.0)
	require.NoError(t, err, "GetUnusualSpend")

	// Non-analytics readers the same pass edited and that had no integration coverage.
	custom := NewCustomCategoryRepository(tx)
	_, err = custom.GetCategoryUsage(ctx, userID, uuid.New().String())
	require.NoError(t, err, "CustomCategoryRepository.GetCategoryUsage")

	txnRepo := NewTransactionRepository(tx, newTestFieldEncryptor(t))
	_, err = txnRepo.GetDistinctPlaidCategories(ctx, userID)
	require.NoError(t, err, "TransactionRepository.GetDistinctPlaidCategories")

	_, err = txnRepo.GetIncomeSumByCategory(ctx, userID, "INCOME", start, end)
	require.NoError(t, err, "TransactionRepository.GetIncomeSumByCategory")

	_, err = txnRepo.GetIncomeSumByTag(ctx, userID, "salary", start, end)
	require.NoError(t, err, "TransactionRepository.GetIncomeSumByTag")

	_, err = txnRepo.GetInflowTransactionsForAccount(ctx, userID, uuid.New().String(), start, end)
	require.NoError(t, err, "TransactionRepository.GetInflowTransactionsForAccount")

	pfcPrimary := "FOOD_AND_DRINK"
	budgetCat := &domain.BudgetCategory{
		CategoryKind: "pfc", PFCPrimary: &pfcPrimary, MatchLogic: domain.MatchLogicCategoryOnly,
	}
	_, err = txnRepo.GetSpentSumForBudgetCategory(ctx, userID, budgetCat, start, end)
	require.NoError(t, err, "TransactionRepository.GetSpentSumForBudgetCategory (category_only)")

	_, _, _, err = txnRepo.GetTransactionsForBudgetCategory(ctx, userID, budgetCat, start, end, 10, 0)
	require.NoError(t, err, "TransactionRepository.GetTransactionsForBudgetCategory (category_only)")

	tagCat := &domain.BudgetCategory{
		CategoryKind: "pfc", PFCPrimary: &pfcPrimary, MatchLogic: domain.MatchLogicTagOnly,
		Tags: []string{"holiday"},
	}
	_, err = txnRepo.GetSpentSumForBudgetCategory(ctx, userID, tagCat, start, end)
	require.NoError(t, err, "GetSpentSumForBudgetCategory (tag_only)")

	andCat := &domain.BudgetCategory{
		CategoryKind: "pfc", PFCPrimary: &pfcPrimary, MatchLogic: domain.MatchLogicAnd,
		Tags: []string{"holiday"},
	}
	_, err = txnRepo.GetSpentSumForBudgetCategory(ctx, userID, andCat, start, end)
	require.NoError(t, err, "GetSpentSumForBudgetCategory (and)")

	orCat := &domain.BudgetCategory{
		CategoryKind: "pfc", PFCPrimary: &pfcPrimary, MatchLogic: domain.MatchLogicOr,
		Tags: []string{"holiday"},
	}
	_, err = txnRepo.GetSpentSumForBudgetCategory(ctx, userID, orCat, start, end)
	require.NoError(t, err, "GetSpentSumForBudgetCategory (or)")

	_, _, err = txnRepo.GetMatchingTransactionStats(
		ctx, userID, "pfc", &pfcPrimary, nil, nil, []string{"holiday"}, domain.MatchLogicOr, start, end)
	require.NoError(t, err, "GetMatchingTransactionStats (or)")

	_, _, err = txnRepo.GetMatchingTransactionStats(
		ctx, userID, "pfc", &pfcPrimary, nil, nil, nil, domain.MatchLogicCategoryOnly, start, end)
	require.NoError(t, err, "GetMatchingTransactionStats (category_only)")

	_, _, err = txnRepo.GetMatchingTransactionStats(
		ctx, userID, "pfc", &pfcPrimary, nil, nil, []string{"holiday"}, domain.MatchLogicTagOnly, start, end)
	require.NoError(t, err, "GetMatchingTransactionStats (tag_only)")

	_, _, err = txnRepo.GetMatchingTransactionStats(
		ctx, userID, "pfc", &pfcPrimary, nil, nil, []string{"holiday"}, domain.MatchLogicAnd, start, end)
	require.NoError(t, err, "GetMatchingTransactionStats (and)")
}
