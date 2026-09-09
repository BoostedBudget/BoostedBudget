package repository

import (
	"context"
	"testing"

	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// The unified feed UNIONs banking transactions with investment_transactions. Investment rows carry
// NO category at all, so any category filter must zero that branch — otherwise asking for
// "Food and Drink" returns every investment row in the period, because an unfiltered UNION arm
// contributes everything it has.
//
// ⚠ WHY THIS IS A POSITIVE ASSERTION (#1626 Phase 2, item 5). The obvious test is
// "assert the query does NOT contain investment rows", which is the assert-absent idiom and the
// wrong tool here: the requirement is the PRESENCE of a specific guard, and an absent-shaped
// assertion passes just as happily against a query that lost the investment arm entirely, or
// against one that never ran. pgxmock's regex is a POSITIVE matcher — the expectation fails unless
// the emitted SQL actually contains both halves — so the guard has to be there for the call to
// succeed at all.
//
// Both halves are required in one pattern deliberately. `AND FALSE` alone would match a query that
// zeroes the branch for an unrelated reason (a merchant filter, a category-source filter, a
// banking-only sort), so it would keep passing if the category leg stopped zeroing anything.
func TestUnifiedFeed_CategoryFilter_ZeroesTheInvestmentBranch(t *testing.T) {
	cases := []struct {
		name    string
		filters domain.TransactionFilters
		// argCount is the total bind count, and it is asserted rather than wildcarded: a category
		// filter BINDS parameters, so a widening of the filter group that forgot to thread its
		// args would shift every later $N and produce a query that is wrong in a way no shape
		// regex could see.
		argCount int
	}{
		{
			name:     "pfc primaries",
			filters:  domain.TransactionFilters{IncludeInvestmentActivity: true, PFCPrimaries: []string{"FOOD_AND_DRINK"}},
			argCount: 3, // user id, the enum, limit+1
		},
		{
			name:     "custom category ids",
			filters:  domain.TransactionFilters{IncludeInvestmentActivity: true, CustomCategoryIDs: []string{"11111111-1111-1111-1111-111111111111"}},
			argCount: 3,
		},
		{
			// #1626 Phase 2's own leg. It binds NOTHING — the predicate is two IS NULL tests — so
			// this case also pins that a bind-free leg still zeroes the branch. A guard keyed off
			// "did we bind a category arg?" would miss it.
			name:     "uncategorized only",
			filters:  domain.TransactionFilters{IncludeInvestmentActivity: true, Uncategorized: true},
			argCount: 2, // user id, limit+1
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))

			anyArgs := make([]interface{}, tc.argCount)
			for i := range anyArgs {
				anyArgs[i] = pgxmock.AnyArg()
			}
			// The totals aggregate runs first on a first page and binds every arg but the limit.
			expectFeedTotalsAggregate(mock, 0, 0, anyArgs[:tc.argCount-1]...)

			// ONE pattern, BOTH halves, in the order the investment arm emits them: the category
			// predicate lands in the banking arm above, and `AND FALSE` closes the investment arm
			// below it.
			mock.ExpectQuery(`(?is)FROM investment_transactions it.*AND FALSE`).
				WithArgs(anyArgs...).
				WillReturnRows(pgxmock.NewRows([]string{"id"}))

			_, _, _, err = repo.GetByUserIDKeysetUnified(
				context.Background(), "00000000-0000-0000-0000-000000000000", tc.filters, nil, 10)
			require.NoError(t, err)
			require.NoError(t, mock.ExpectationsWereMet(),
				"a category filter must zero the investment UNION arm; without it, filtering by a "+
					"category returns every investment row in the period")
		})
	}
}
