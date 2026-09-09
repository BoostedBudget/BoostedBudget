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

// TestGetCategoryTotalForPeriod_CustomCategory_BindsFiveArgsWhenDisplayIsNull covers the custom-
// token path with a NULL category_display cell (the scalar subquery having nothing to return).
//
// ⚠ RENAMED (#1626 Phase 2 pgxmock sweep): this test used to be named
// ...NullDisplayScansToEmptyString and claimed to prove the sql.NullString scan tolerates NULL.
// It does not, and cannot: pgxmock scans a `nil` AddRow cell into a plain Go `string` WITHOUT
// error (confirmed by mutation — reverting GetCategoryTotalForPeriod's `sql.NullString` to a plain
// `string` leaves this test green), so it cannot distinguish the current, correct scan from a
// reverted, broken one. What it DOES prove, and the reason it survives rather than being deleted,
// is real: pgxmock's WithArgs enforces arg COUNT, so require.NoError below only passes if the
// custom-token branch bound exactly five args (the fifth being the bound uuid — see the
// "custom-token form binds a FIFTH arg" test above). The genuine NULL-tolerance proof — that a real
// Postgres NULL from this exact subquery shape does not 500 — lives in
// TestAnalyticsReads_NullCategoryDisplay_ToleratedBySqlNullStringReads
// (analytics_null_category_display_integration_test.go, build tag integration, real Postgres).
func TestGetCategoryTotalForPeriod_CustomCategory_BindsFiveArgsWhenDisplayIsNull(t *testing.T) {
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
	require.NoError(t, err, "the custom-token branch must bind exactly five args even when the "+
		"scalar subquery's result is NULL — a fourth-arg-only call would fail this WithArgs match")
	require.NotNil(t, row)
	assert.Empty(t, row.CategoryDisplay)
}

// TestGetCategoryTotalForPeriod_PFCToken_TakesFourArgs is the PFC-token counterpart to the
// custom-token test above: it exercises the actual repository call (not just the query builder,
// which TestGetCategoryTotalForPeriodQuery_NeverUsesMaxOnADenormalizedName below already pins),
// confirming a PFC token binds exactly 4 args — no fifth uuid arg, because there is no
// custom_categories row to look up.
//
// ⚠ RENAMED (#1626 Phase 2 pgxmock sweep): dropped "AndNullDisplay" from the name. The
// `assert.Empty(t, row.CategoryDisplay)` below is real (a mismatched arg count would already have
// failed require.NoError via WithArgs), but it cannot prove NULL TOLERANCE any more than the test
// above can — pgxmock never errors scanning a nil cell into a plain string, sql.NullString or not.
// The real proof, against real Postgres, is
// TestAnalyticsReads_NullCategoryDisplay_ToleratedBySqlNullStringReads's "PFC token" subtest
// (analytics_null_category_display_integration_test.go).
func TestGetCategoryTotalForPeriod_PFCToken_TakesFourArgs(t *testing.T) {
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
