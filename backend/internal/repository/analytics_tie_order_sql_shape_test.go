package repository

// analytics_tie_order_sql_shape_test.go — #1569 gap task G4, issue #1585.
//
// The behavioural half of #1585 lives in analytics_tie_order_determinism_integration_test.go: it
// seeds deliberate ties and proves the six spending reads return them in the same, alphabetical
// order under four different planner configurations.
//
// ⚠ That test CANNOT pin the merchant reads' third sort leg — the grouped merchant key — and this
// was measured, not assumed. Removing the leg leaves the relative order of two same-named merchant
// groups UNDEFINED, and undefined is not the same as wrong: on this Postgres the HashAggregate
// happened to emit them in the very order the key would have imposed, so the mutant survived five
// consecutive runs of a fixture holding FOUR groups all rendering the name "Unknown". A semantic
// test can only observe the order the executor chose, never the guarantee that it had to.
//
// So the leg is pinned structurally instead: the merchant reads must ORDER BY the same expression
// they GROUP BY. That is checkable, deterministic, and fails the moment the leg is dropped.
//
// The second test here is the forward-looking half: a NEW spending read that ships `ORDER BY total
// DESC` with no tiebreaker reintroduces #1585 silently, and neither the integration test (which
// only knows the six reads that exist today) nor a reviewer's memory would catch it.

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// squashWS renders a SQL fragment as a single space-separated line so the assertions compare
// meaning rather than the indentation the query happens to be built with.
func squashWS(s string) string { return strings.Join(strings.Fields(s), " ") }

// finalOrderBy returns the query's last ORDER BY clause, squashed, with any trailing LIMIT removed.
func finalOrderBy(t *testing.T, sql string) string {
	t.Helper()
	i := strings.LastIndex(sql, "ORDER BY")
	require.GreaterOrEqual(t, i, 0, "query has no ORDER BY at all:\n%s", sql)
	clause := squashWS(sql[i+len("ORDER BY"):])
	if j := strings.Index(clause, "LIMIT"); j >= 0 {
		clause = strings.TrimSpace(clause[:j])
	}
	return clause
}

// finalGroupBy returns the query's last GROUP BY clause, squashed, up to the ORDER BY that follows.
func finalGroupBy(t *testing.T, sql string) string {
	t.Helper()
	i := strings.LastIndex(sql, "GROUP BY")
	require.GreaterOrEqual(t, i, 0, "query has no GROUP BY at all:\n%s", sql)
	rest := sql[i+len("GROUP BY"):]
	if j := strings.Index(rest, "ORDER BY"); j >= 0 {
		rest = rest[:j]
	}
	return squashWS(rest)
}

func TestSpendingReads_OrderByCarriesTheDocumentedTiebreaker(t *testing.T) {
	ctx := context.Background()
	const userID = "11111111-1111-1111-1111-111111111111"

	personal := domain.SpendingFilters{
		StartDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC),
	}
	household := personal
	household.HouseholdScope = &domain.HouseholdScope{
		HouseholdID:   "22222222-2222-2222-2222-222222222222",
		MemberUserIDs: []string{userID},
	}

	merchantKey := squashWS(merchantGroupKeyExpr)

	// All six spending reads, each with the ORDER BY it must produce. Personal and household are
	// listed separately and spelled out in full on purpose: a divergence between the two members of
	// a pair is how a defect hid on this branch before, so the expectations are not shared.
	cases := []struct {
		name          string
		call          func(AnalyticsRepository) error
		wantOrderBy   string
		wantGroupedBy string // merchant reads only: the ORDER BY tail must be this same expression
		nargs         int    // bound parameters, so pgxmock's WithArgs matches the real call
	}{
		{
			name:  "GetSpendingByCategory personal",
			nargs: 3,
			call: func(r AnalyticsRepository) error {
				_, err := r.GetSpendingByCategory(ctx, userID, personal)
				return err
			},
			wantOrderBy: "total DESC, category_display ASC",
		},
		{
			name:  "getSpendingByCategoryHousehold",
			nargs: 5,
			call: func(r AnalyticsRepository) error {
				_, err := r.GetSpendingByCategory(ctx, userID, household)
				return err
			},
			wantOrderBy: "total DESC, category_display ASC",
		},
		{
			name:  "GetSpendingByMerchant personal",
			nargs: 4,
			call: func(r AnalyticsRepository) error {
				_, err := r.GetSpendingByMerchant(ctx, userID, personal, 10)
				return err
			},
			wantOrderBy:   "total DESC, merchant_name ASC, " + merchantKey + " ASC",
			wantGroupedBy: merchantKey,
		},
		{
			name:  "getSpendingByMerchantHousehold",
			nargs: 6,
			call: func(r AnalyticsRepository) error {
				_, err := r.GetSpendingByMerchant(ctx, userID, household, 10)
				return err
			},
			wantOrderBy:   "total DESC, merchant_name ASC, " + merchantKey + " ASC",
			wantGroupedBy: merchantKey,
		},
		{
			name:        "GetSpendingTrends personal",
			nargs:       3,
			call:        func(r AnalyticsRepository) error { _, err := r.GetSpendingTrends(ctx, userID, personal); return err },
			wantOrderBy: "period_start ASC, total DESC, category_display ASC",
		},
		{
			name:        "getSpendingTrendsHousehold",
			nargs:       5,
			call:        func(r AnalyticsRepository) error { _, err := r.GetSpendingTrends(ctx, userID, household); return err },
			wantOrderBy: "period_start ASC, total DESC, category_display ASC",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mock, repo, captured := setupAnalyticsMockDBCapturingSQL(t)
			defer mock.Close()
			anyArgs := make([]interface{}, tc.nargs)
			for i := range anyArgs {
				anyArgs[i] = pgxmock.AnyArg()
			}
			mock.ExpectQuery(`SELECT`).WithArgs(anyArgs...).WillReturnRows(pgxmock.NewRows([]string{"a"}))

			require.NoError(t, tc.call(repo))
			require.Len(t, *captured, 1, "expected exactly one statement to be issued")
			sql := (*captured)[0]

			require.Equal(t, tc.wantOrderBy, finalOrderBy(t, sql),
				"%s must break ties deterministically (#1585); equal-total rows are otherwise returned in whatever order the plan produces", tc.name)

			if tc.wantGroupedBy != "" {
				require.Equal(t, tc.wantGroupedBy, finalGroupBy(t, sql),
					"%s must ORDER BY the very expression it GROUPs BY — the merchant display name COALESCEs to the literal 'Unknown', so several distinct merchant keys tie on the name and only the key separates them", tc.name)
			}
		})
	}
}

// orderByTotalRe finds every ORDER BY in analytics_repository.go that sorts on `total`.
var orderByTotalRe = regexp.MustCompile(`(?m)^\s*ORDER BY .*\btotal DESC\b.*$`)

// tiebreakerLegRe matches the tiebreaker legs #1585 sanctioned: alphabetical by the read's name
// column, and (merchant only) the grouped merchant key that follows it.
var tiebreakerLegRe = regexp.MustCompile(`\b(category_display|merchant_name)\s+ASC\b`)

// TestNoSpendingReadOrdersByTotalWithoutATiebreaker is the forward-looking guard. The six reads
// #1585 fixed are pinned by name above; this one fires on the SEVENTH — a new spending read that
// ships `ORDER BY total DESC` alone and silently reintroduces plan-dependent tie order.
//
// It reads the source rather than the executed SQL because a read nobody has written a test for is
// precisely the one that needs catching, and such a read issues no SQL for a matcher to capture.
func TestNoSpendingReadOrdersByTotalWithoutATiebreaker(t *testing.T) {
	src, err := os.ReadFile("analytics_repository.go")
	require.NoError(t, err, "read analytics_repository.go")

	found := orderByTotalRe.FindAllString(string(src), -1)
	require.Len(t, found, 6,
		"expected the six spending reads #1585 fixed; if a read was added or removed, extend "+
			"TestSpendingReads_OrderByCarriesTheDocumentedTiebreaker to name it explicitly. Found:\n%s",
		strings.Join(found, "\n"))

	for _, line := range found {
		require.Regexp(t, tiebreakerLegRe, line,
			"an ORDER BY on `total` with no tiebreaker returns equal-total rows in plan-dependent "+
				"order (#1585). Add the alphabetical leg — and, for a merchant-shaped read, the "+
				"grouped merchant key after it. Offending clause: %s", squashWS(line))
	}
}
