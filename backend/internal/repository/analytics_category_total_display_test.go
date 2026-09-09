package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// GetCategoryTotalForPeriod resolved its display name with MAX(t.effective_category_name), and the
// comment defending that choice was WRONG in a way that mattered (#1626 Phase 2, gap item 2).
//
// It argued: the WHERE pins one category, so MAX picks that category's name rather than choosing
// between rivals. True of the CATEGORY, false of the NAME. effective_category_name is denormalised
// onto each transaction and refreshed ASYNCHRONOUSLY by a River job after a rename, so mid-refresh
// the rows of one category legitimately carry BOTH the old and the new name — and MAX is
// lexicographic. It does not return the stale name, or the fresh one; it returns whichever sorts
// higher. "Alpha" renamed to "Zulu" reports Zulu, and "Zulu" renamed to "Alpha" reports Zulu too.
//
// A one-row QueryRow aggregate cannot select cc.name directly without grouping, and grouping risks
// multiple rows in ghost states, so the fix is a SCALAR SUBQUERY: still exactly one row, resolved
// from custom_categories, which holds the ONE current name.
//
// ⚠ The subquery is emitted only for a custom token, and the uuid is BOUND, not interpolated. An
// unconditional `substring($4 from 8)::uuid` would be a cast Postgres may evaluate before the
// guarding LIKE, turning a PFC-token call into an invalid-input-syntax error.

var (
	maxDisplayRe = regexp.MustCompile(`(?i)MAX\s*\(\s*t\.effective_category_name\s*\)`)
	ccNameSubqRe = regexp.MustCompile(`(?is)SELECT\s+cc\.name\s+FROM\s+custom_categories\s+cc`)
)

func TestGetCategoryTotalForPeriod_CustomCategory_ResolvesTheCurrentName(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewAnalyticsRepository(mock)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	const catID = "8f14e45f-ceea-467a-9f47-ab1c1e2d3f40"

	mock.ExpectQuery(`(?is)FROM transactions t`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"total", "transaction_count", "category_display"}).
			AddRow(150.0, 3, "Coffee runs"))

	row, err := repo.GetCategoryTotalForPeriod(ctx, "user-1", "custom:"+catID, start, end)
	require.NoError(t, err)
	require.NotNil(t, row)
	assert.Equal(t, "Coffee runs", row.CategoryDisplay)
	require.NoError(t, mock.ExpectationsWereMet(),
		"the custom-token form binds a FIFTH arg — the bare uuid — because the id must be a "+
			"parameter, not a substring cast inside the SQL")
}

// TestGetCategoryTotalForPeriodQuery_NeverUsesMaxOnADenormalizedName is the shape half, and it is
// the one that pins the FIX rather than the plumbing: it inspects the emitted SQL directly, so a
// revert to MAX() fails here even if every behavioural test still passes on a single-name fixture.
func TestGetCategoryTotalForPeriodQuery_NeverUsesMaxOnADenormalizedName(t *testing.T) {
	custom := categoryTotalQuery(true)
	assert.NotRegexp(t, maxDisplayRe, custom,
		"MAX() over the denormalized name is lexicographic, so mid-rename it returns an ARBITRARY "+
			"name — not a stale one. Resolve from custom_categories instead")
	assert.Regexp(t, ccNameSubqRe, custom,
		"a custom category's display must come from custom_categories, which holds the one current name")

	pfc := categoryTotalQuery(false)
	assert.NotRegexp(t, maxDisplayRe, pfc)
	assert.NotRegexp(t, ccNameSubqRe, pfc,
		"a PFC token has no custom_categories row; emitting the subquery would force a uuid cast "+
			"on a value that is not a uuid")
}
