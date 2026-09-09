package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// The list endpoint returned only a page, so nothing in the product could answer "what does this
// filtered set come to" — which made the reports Sankey's click-through claim ("this node's amount
// equals the filtered list's total") unverifiable by construction, not merely untested (#1626
// Phase 2, gap 2).
//
// These tests pin the two properties that make the total trustworthy rather than decorative:
//
//  1. It is computed ONLY on the first page. Re-aggregating the whole filtered set on every page of
//     the busiest list in the app would be real cost for a number that does not change as the user
//     pages, so a cursored call must issue exactly ONE query.
//  2. It describes the whole SET, not the page.
//
// The third and most important property — that the aggregate uses the SAME predicate as the rows —
// cannot be observed here: two SQL strings agreeing is not the same as two result sets agreeing.
// TestFeedTotals_AgreeWithTheRowsTheyDescribe_Integration is where that is actually checked, and it
// is the reason both implementations reuse their page query's own WHERE fragment and arg slice
// rather than rebuilding the predicate.
//
// Falsifiability, checked by mutation rather than assumed: deleting the `if cursor == nil` guard
// fails (1); deriving the totals from the returned page instead of the aggregate fails (2).

// aggregateShapeRe deliberately requires BOTH halves — a count and a summed amount. Matching only
// "some extra query happened" would keep passing if the aggregate silently stopped summing.
var aggregateShapeRe = regexp.MustCompile(`(?is)SELECT\s+COUNT\(\*\)\s*,\s*COALESCE\(SUM\(`)

// expectFeedTotalsAggregate registers the whole-set aggregate that every first-page keyset read now
// issues ahead of its page query. Exported as a helper because four pre-existing first-page tests
// need it too, and a copied literal in five places is how a query-shape change becomes five edits.
func expectFeedTotalsAggregate(mock pgxmock.PgxPoolIface, count int, amount float64, args ...interface{}) {
	q := mock.ExpectQuery(aggregateShapeRe.String())
	if len(args) > 0 {
		q = q.WithArgs(args...)
	}
	q.WillReturnRows(pgxmock.NewRows([]string{"count", "sum"}).AddRow(count, amount))
}

func TestBankingKeysetFirstPage_ReturnsWholeSetTotals(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()
	userID := "user-feed-totals-1"

	// Nine rows in the set, two returned in the page: a total derived from the page instead of the
	// aggregate cannot pass this.
	expectFeedTotalsAggregate(mock, 9, 1234.56, userID)

	now := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	rows := pgxmock.NewRows(transactionSelectColumns).
		AddRow(makeKeysetRow("00000000-0000-0000-0000-000000000001", userID, -10.00, now, nil)...).
		AddRow(makeKeysetRow("00000000-0000-0000-0000-000000000002", userID, -20.00, now, nil)...)
	mock.ExpectQuery(`(?is)FROM transactions t`).
		WithArgs(userID, 3).
		WillReturnRows(rows)

	txs, _, totals, err := repo.GetByUserIDKeyset(ctx, userID, domain.TransactionFilters{}, nil, 2)
	require.NoError(t, err)
	require.Len(t, txs, 2)

	require.NotNil(t, totals, "the first page must carry whole-set totals — without them the Sankey parity claim is unmeasurable")
	assert.Equal(t, 9, totals.Count, "the count must describe the whole filtered set, not the returned page")
	assert.InDelta(t, 1234.56, totals.Amount, 0.001)

	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBankingKeysetCursoredPage_SkipsTheAggregateEntirely(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()
	userID := "user-feed-totals-2"

	// ONE expectation, and pgxmock is ordered and exhaustive by default: an aggregate on this path
	// would arrive first and fail to match the page query's shape.
	mock.ExpectQuery(`(?is)FROM transactions t`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows(transactionSelectColumns))

	sv := "2026-01-01"
	cursor := &domain.TransactionCursor{ID: uuid.New(), SV: &sv, K: "date", D: "desc"}

	_, _, totals, err := repo.GetByUserIDKeyset(ctx, userID, domain.TransactionFilters{}, cursor, 3)
	require.NoError(t, err)

	assert.Nil(t, totals, "a cursored page must not re-aggregate the whole set; nil means 'not computed', never 'zero'")
	require.NoError(t, mock.ExpectationsWereMet())
}
