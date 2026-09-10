package repository

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
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

// TestCustomCategoryIDFromToken_RejectsAMalformedSuffix is #1626 gap G9.
//
// THE FOOTGUN, and it is a footgun rather than a live failure — recorded that way because the
// standing instruction on this branch is to action rather than defer. categoryTotalQuery(true)
// emits `cc.id = $5::uuid`, so the token's suffix is CAST by Postgres rather than compared as text,
// and an unparseable one fails the whole statement — which MapError classifies as a 500.
//
// It is not reachable from the API today: services.validateCategoryToken gates the one live caller
// with the same uuid.Parse and answers 400. But that gate protects the SERVICE, not the CAST. Any
// future repository caller — another analytics method, a job, a test helper — reaches
// GetCategoryTotalForPeriod directly and gets the 500 back, and nothing about the query's own shape
// says so. Parsing here makes the repository safe BY CONSTRUCTION: a malformed suffix is not a
// custom token, so the uuid subquery is never emitted and no uuid is ever bound.
//
// ⚠ WHAT IT MUST NOT DO is reject a VALID uuid naming a category that does not exist. That is a
// legitimate request answered by a zero total and the "Custom category" fallback label (see
// validateCategoryToken's own note). Only the SHAPE is checked, exactly as at the service layer, and
// with the SAME parser — uuid.Parse, not a regexp — so the gate cannot admit a spelling Postgres
// would then reject, nor reject one it would accept.
func TestCustomCategoryIDFromToken_RejectsAMalformedSuffix(t *testing.T) {
	valid := uuid.NewString()

	cases := []struct {
		name   string
		token  string
		wantID string
		wantOK bool
		why    string
	}{
		{
			name:   "a well-formed uuid suffix is a custom token",
			token:  "custom:" + valid,
			wantID: valid,
			wantOK: true,
			why:    "the whole point of the token; a valid uuid must still bind as $5",
		},
		{
			name:   "a uuid naming no existing row is STILL a custom token",
			token:  "custom:00000000-0000-4000-8000-000000000000",
			wantID: "00000000-0000-4000-8000-000000000000",
			wantOK: true,
			why: "existence is not this function's question — a tombstoned or absent category answers " +
				"with a zero total and the fallback label, which requires the uuid to be bound",
		},
		{
			name:   "an uppercase uuid is accepted, because Postgres accepts it",
			token:  "custom:" + strings.ToUpper(valid),
			wantID: strings.ToUpper(valid),
			wantOK: true,
			why:    "uuid.Parse is the same parser the ::uuid cast applies; rejecting here would 404 a valid id",
		},
		{
			name:   "a non-uuid suffix is NOT a custom token",
			token:  "custom:not-a-uuid",
			wantOK: false,
			why: "this is the gap: it used to be returned as a custom id and cast with $5::uuid, and " +
				"`invalid input syntax for type uuid` fails the whole statement as a 500",
		},
		{
			name:   "a suffix that is a uuid PREFIX is not a uuid",
			token:  "custom:" + valid[:8],
			wantOK: false,
			why:    "a truncated uuid is the shape a hand-built or truncated query parameter actually takes",
		},
		{
			name:   "a SQL-ish suffix is not a uuid",
			token:  "custom:1;DROP TABLE transactions",
			wantOK: false,
			why: "the suffix reaches the query only as a BOUND parameter, so this was never injection — " +
				"it was a 500. Rejecting it keeps the failure mode from being either",
		},
		{
			name:   "an empty suffix is not a custom token",
			token:  "custom:",
			wantOK: false,
			why:    "unchanged behaviour, pinned so the added parse cannot be read as having replaced it",
		},
		{
			name:   "a PFC enum is left entirely alone",
			token:  "FOOD_AND_DRINK",
			wantOK: false,
			why:    "no prefix, no parse, no uuid subquery — a PFC token is not a uuid and must not be judged as one",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotID, gotOK := customCategoryIDFromToken(tc.token)
			assert.Equal(t, tc.wantOK, gotOK, tc.why)
			assert.Equal(t, tc.wantID, gotID, tc.why)
		})
	}
}

// TestGetCategoryTotalForPeriod_MalformedCustomTokenBindsNoUUID is the behavioural half: it proves
// the repository METHOD, not just the helper, stops emitting the uuid cast and stops binding a
// fifth argument.
//
// ⚠ Asserted through the bound-argument COUNT rather than by inspecting the SQL, deliberately. A
// text assertion that the query lacks `$5::uuid` would be killed by the same mutation but would say
// nothing about what actually reaches Postgres — and four such text-only pins on this branch each
// turned out to be hiding a fixture gap. Four args means no uuid was bound; five means one was.
func TestGetCategoryTotalForPeriod_MalformedCustomTokenBindsNoUUID(t *testing.T) {
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
			AddRow(0.0, 0, nil))

	row, err := repo.GetCategoryTotalForPeriod(ctx, "user-1", "custom:not-a-uuid", start, end)
	require.NoError(t, err,
		"a malformed custom token must not reach Postgres as a uuid cast. It used to, and "+
			"`invalid input syntax for type uuid` failed the whole statement — a 500 for a "+
			"malformed input, from any repository caller that does not happen to sit behind "+
			"services.validateCategoryToken")
	require.NotNil(t, row)
	assert.InDelta(t, 0.0, row.Total, 0.001,
		"the token matches no transaction, so the ungrouped aggregate answers zero — the same answer "+
			"a text comparison against a nonexistent category always gave")
	require.NoError(t, mock.ExpectationsWereMet(),
		"exactly 4 bound args. A fifth would mean the uuid subquery was emitted and a malformed "+
			"value was bound to `cc.id = $5::uuid`")
}
