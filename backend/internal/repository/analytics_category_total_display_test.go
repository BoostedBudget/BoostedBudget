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

// TestGetCategoryTotalForPeriod_CustomCategory_NullDisplayScansToEmptyString covers the other
// reachable custom-token state: the category still exists (or the row is a tombstoned/gone
// custom_categories id) but the scalar subquery has nothing to return, so category_display comes
// back NULL. The repository must not error on that — it is scanned through sql.NullString and
// normalised to "", leaving the placeholder decision to resolveCategoryDisplay in the service.
func TestGetCategoryTotalForPeriod_CustomCategory_NullDisplayScansToEmptyString(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewAnalyticsRepository(mock)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	const catID = "9f1c2b3a-0000-4000-8000-000000000002"

	mock.ExpectQuery(`(?is)FROM transactions t`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"total", "transaction_count", "category_display"}).
			AddRow(250.0, 1, nil))

	row, err := repo.GetCategoryTotalForPeriod(ctx, "user-1", "custom:"+catID, start, end)
	require.NoError(t, err)
	require.NotNil(t, row)
	assert.Empty(t, row.CategoryDisplay)
}

// TestGetCategoryTotalForPeriod_PFCToken_TakesFourArgsAndNullDisplay is the PFC-token counterpart
// to the custom-token test above: it exercises the actual repository call (not just the query
// builder, which TestGetCategoryTotalForPeriodQuery_NeverUsesMaxOnADenormalizedName below already
// pins), confirming a PFC token binds exactly 4 args — no fifth uuid arg, because there is no
// custom_categories row to look up — and that its display column comes back NULL by construction.
func TestGetCategoryTotalForPeriod_PFCToken_TakesFourArgsAndNullDisplay(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewAnalyticsRepository(mock)
	ctx := context.Background()
	start := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`(?is)FROM transactions t`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"total", "transaction_count", "category_display"}).
			AddRow(450.0, 18, nil))

	row, err := repo.GetCategoryTotalForPeriod(ctx, "user-1", "FOOD_AND_DRINK", start, end)
	require.NoError(t, err)
	require.NotNil(t, row)
	assert.InDelta(t, 450.0, row.Total, 0.001)
	assert.Equal(t, 18, row.TransactionCount)
	assert.Empty(t, row.CategoryDisplay)
	require.NoError(t, mock.ExpectationsWereMet(),
		"a PFC token must bind exactly 4 args — a fifth would mean a uuid arg leaked in for a "+
			"token with no custom_categories row to bind it to")
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
