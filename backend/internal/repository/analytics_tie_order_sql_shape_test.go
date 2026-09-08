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
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
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

	// All eight spending reads, each with the ORDER BY it must produce. The income pair joined this
	// universe in #1626: it previously ordered by ABS(SUM(t.amount)) with no tiebreaker at all, so
	// the alias-based guard below could not see it. Personal and household are
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
			name:  "GetIncomeSourceBreakdown personal",
			nargs: 3,
			call: func(r AnalyticsRepository) error {
				_, err := r.GetIncomeSourceBreakdown(ctx, userID, personal)
				return err
			},
			wantOrderBy: "total DESC, category_display ASC",
		},
		{
			name:  "getIncomeSourceBreakdownHousehold",
			nargs: 5,
			call: func(r AnalyticsRepository) error {
				_, err := r.GetIncomeSourceBreakdown(ctx, userID, household)
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

// ── The forward-looking guard ────────────────────────────────────────────────────────────────────
//
// #1569 round 5 (Codex F5): this guard used to match a single LINE of source text
// (`^\s*ORDER BY .*total DESC.*$`). A seventh read written as
//
//	ORDER BY
//	  total DESC
//
// was invisible to it, so it found exactly the six reads that already existed and PASSED — it could
// not see the next instance, which is the only instance it exists for. It now folds the file's SQL
// string expressions through the package's shared folder (sql_literal_folding_test.go: a `+`
// concatenation is joined into one string, an operand it cannot evaluate becomes a word-character-
// free placeholder) and squashes whitespace before matching, so neither a line break nor `+`
// assembly hides a clause any more.
//
// ⚠ Stated scope boundary, so the limit is honest rather than silent. The guard sees SQL that is
// literal in analytics_repository.go, including across `+`. It does NOT see:
//   - a clause assembled across separate statements (`query += "ORDER BY total"` then
//     `query += " DESC"`), because those two literals never meet in one expression;
//   - a clause whose ORDER BY text arrives from a variable, a call, or a `fmt.Sprintf` verb — the
//     folder renders those as its placeholder, which deliberately does NOT read as `total DESC`;
//   - a read in any other file.
//
// Those shapes are reachable only by writing an ORDER BY in a way no read in this file uses. A read
// that ever needs one must instead be pinned by name in
// TestSpendingReads_OrderByCarriesTheDocumentedTiebreaker above, which observes the executed SQL
// and has no such blind spot.

// orderByClauseTerminators end an ORDER BY clause at paren depth zero.
var orderByClauseTerminators = []string{"LIMIT ", "OFFSET ", "UNION ", "FETCH ", "ORDER BY "}

// orderByClauses returns every ORDER BY clause in one folded query, whitespace-squashed, stopping
// at the trailing LIMIT/OFFSET/UNION or at the close of an enclosing subquery.
func orderByClauses(sql string) []string {
	s := squashWS(sql)
	const kw = "ORDER BY "
	var out []string
	for i := 0; ; {
		j := strings.Index(s[i:], kw)
		if j < 0 {
			return out
		}
		start := i + j + len(kw)
		end, depth := len(s), 0
	scan:
		for k := start; k < len(s); k++ {
			switch s[k] {
			case '(':
				depth++
				continue
			case ')':
				if depth == 0 {
					end = k
					break scan
				}
				depth--
				continue
			}
			if depth != 0 || (k != start && s[k-1] != ' ') {
				continue
			}
			for _, term := range orderByClauseTerminators {
				if strings.HasPrefix(s[k:], term) {
					end = k
					break scan
				}
			}
		}
		out = append(out, strings.TrimSpace(s[start:end]))
		i = start
	}
}

// orderByTotalRe recognises a folded ORDER BY clause that sorts on `total`.
var orderByTotalRe = regexp.MustCompile(`\btotal DESC\b`)

// tiebreakerLegRe matches the tiebreaker legs #1585 sanctioned: alphabetical by the read's name
// column, and (merchant only) the grouped merchant key that follows it.
var tiebreakerLegRe = regexp.MustCompile(`\b(category_display|merchant_name)\s+ASC\b`)

// spendingOrderByTotalClauses folds analytics_repository.go and returns every ORDER BY clause in it
// that sorts on `total`, each prefixed with the source line the query starts on.
// analyticsSQLFiles are the files whose SQL this guard reads.
//
// #1626 added the second entry, and the reason is a hole this guard had: it parsed exactly one
// file, so a new spending read that landed in ANY other file escaped the inventory silently — no
// count change, no failure, no tiebreaker enforced. The hierarchy read is in its own file precisely
// because it is a self-contained subject, and it would have slipped straight past.
var analyticsSQLFiles = []string{
	"analytics_repository.go",
	"analytics_category_path.go",
}

func spendingOrderByTotalClauses(t *testing.T) []string {
	t.Helper()

	var found []string
	for _, name := range analyticsSQLFiles {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, name, nil, 0)
		require.NoError(t, err, "parse %s", name)

		for _, expr := range sqlStringExprs(file) {
			for _, clause := range orderByClauses(expr.SQL) {
				if orderByTotalRe.MatchString(clause) {
					found = append(found,
						name+":"+strconv.Itoa(fset.Position(expr.Pos).Line)+
							"  ORDER BY "+clause)
				}
			}
		}
	}
	return found
}

// TestNoSpendingReadOrdersByTotalWithoutATiebreaker is the forward-looking guard. The six reads
// #1585 fixed are pinned by name above; this one fires on the SEVENTH — a new spending read that
// ships `ORDER BY total DESC` alone and silently reintroduces plan-dependent tie order.
//
// It reads the source rather than the executed SQL because a read nobody has written a test for is
// precisely the one that needs catching, and such a read issues no SQL for a matcher to capture.
//
// Both assertions are `assert`, not `require`: a newly added untiebreakered read trips the
// inventory count AND the tiebreaker check, and seeing both messages is how the reader tells
// "somebody added a read" apart from "somebody changed one".
func TestNoSpendingReadOrdersByTotalWithoutATiebreaker(t *testing.T) {
	found := spendingOrderByTotalClauses(t)

	assert.Len(t, found, 9,
		"expected the six spending reads #1585 fixed, plus the income pair #1626 brought into scope, "+
			"plus the ONE literal the hierarchy pair shares (both its personal and household reads "+
			"order through categoryHierarchyGroupAndOrder, so one clause covers two reads and they "+
			"cannot drift apart); if a read was added or removed, extend "+
			"TestSpendingReads_OrderByCarriesTheDocumentedTiebreaker to name it explicitly. Found:\n%s",
		strings.Join(found, "\n"))

	for _, clause := range found {
		assert.Regexp(t, tiebreakerLegRe, clause,
			"an ORDER BY on `total` with no tiebreaker returns equal-total rows in plan-dependent "+
				"order (#1585). Add the alphabetical leg — and, for a merchant-shaped read, the "+
				"grouped merchant key after it. Offending clause: %s", clause)
	}
}

// TestIncomeReadsSelectTheCategoryDisplayColumn is the structural half of #1626 bug A.
//
// GetIncomeSourceBreakdown used to select ONLY the grouping key. For a custom-categorised income
// row that key is the synthetic token `custom:<uuid>`, and the service then ran it through
// PFCPrimaryDisplayName, which misses the registry map and title-cases the raw token — so the
// Sankey rendered a UUID as an income source's name. The expense sibling never had the bug because
// it selects effectiveCategoryDisplayExpr() alongside the key.
//
// This is pinned structurally rather than behaviourally because the defect is an ABSENT column:
// a semantic test can assert the name that comes back, but only the SQL shape can assert that the
// read asks for a display at all, which is what regressed.
func TestIncomeReadsSelectTheCategoryDisplayColumn(t *testing.T) {
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

	cases := []struct {
		name  string
		args  domain.SpendingFilters
		nargs int
	}{
		{name: "GetIncomeSourceBreakdown personal", args: personal, nargs: 3},
		{name: "getIncomeSourceBreakdownHousehold", args: household, nargs: 5},
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

			_, err := repo.GetIncomeSourceBreakdown(ctx, userID, tc.args)
			require.NoError(t, err)
			require.Len(t, *captured, 1, "expected exactly one statement to be issued")
			sql := squashWS((*captured)[0])

			require.Contains(t, sql, effectiveCategoryDisplayExpr()+" AS category_display",
				"%s must select the display column beside the grouping key, or a custom income "+
					"category reaches the API as the raw `custom:<uuid>` token (#1626)", tc.name)
			require.Contains(t, finalGroupBy(t, (*captured)[0]), "category_display",
				"%s must GROUP BY the display it selects", tc.name)
		})
	}
}
