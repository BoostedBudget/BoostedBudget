//go:build integration

package repository

// Transaction repository EXPLAIN smoke tests.
// Verifies that the 4 expression+composite indexes created by migration
// 000074_add_transactions_keyset_indexes are used by the keyset pagination
// query. Amount/date/name sort keys assert an index scan is chosen.
// Merchant-sort cases assert only SQL validity (plan builds without error)
// because no index covers the effective-merchant JOIN chain.
//
// Run with:
//   DATABASE_URL=postgres://... go test ./internal/repository/... -run TestTransactionKeyset_EXPLAIN -tags integration -v
//
// The test uses SET enable_seqscan = OFF so the planner is forced to pick an
// index if one is applicable, even on an empty table. A matching index exists
// but with a mismatched expression the planner falls back to a Seq Scan,
// which fails the assertion.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	pgx "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/database"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// rowQuerier is satisfied by *pgxpool.Pool and *pgxpool.Conn.
// Using a dedicated *pgxpool.Conn ensures planner SET commands and EXPLAIN
// queries share the same session — pool connections are not guaranteed to be the same.
type rowQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func connectTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping EXPLAIN integration test")
	}
	// Issue #1461: route through database.NewPoolFromURL (not a bare pgxpool.New) so this pool —
	// used by every repository-package integration test via this shared helper — pins session
	// TimeZone=UTC exactly like the production pool does. Without this, no integration test in
	// this package would ever exercise NewPool's TimeZone pin.
	pool, err := database.NewPoolFromURL(context.Background(), dsn)
	require.NoError(t, err, "connect to test database")
	t.Cleanup(pool.Close)
	return pool
}

// runExplain runs EXPLAIN on sql and returns the plan text.
func runExplain(ctx context.Context, t *testing.T, db rowQuerier, sql string, args ...any) string {
	t.Helper()
	rows, err := db.Query(ctx, "EXPLAIN "+sql, args...)
	require.NoError(t, err, "EXPLAIN query: %s", sql)
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var line string
		require.NoError(t, rows.Scan(&line))
		lines = append(lines, line)
	}
	require.NoError(t, rows.Err())
	return strings.Join(lines, "\n")
}

// TestChainExistsIndex_EXPLAIN verifies that the chain-expansion EXISTS leg
// in GetByUserIDKeysetUnified uses a user_account_id index rather than a Seq Scan.
// Accepts either idx_accounts_user_account_status (composite) or idx_accounts_user_account
// (single-col) — both are valid and statistics-driven.
//
// Like all EXPLAIN tests in this file, it runs against an empty table; the planner
// still produces a real access-path plan because enable_seqscan=OFF makes seq scans
// prohibitively expensive. The assertion proves index availability, not row-level behavior.
func TestChainExistsIndex_EXPLAIN(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()

	// Acquire a single connection so SET and EXPLAIN share the same session.
	conn, err := db.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()

	// Force index use (disable seq scan); a missing index produces a Seq Scan.
	_, err = conn.Exec(ctx, "SET enable_seqscan = OFF")
	require.NoError(t, err)
	defer conn.Exec(ctx, "SET enable_seqscan = ON") //nolint:errcheck

	// The correlated banking-branch shape from GetByUserIDKeysetUnified.
	// When account_id is NULL or replaced, the planner must use the user_account_id
	// index in the chain leg (second EXISTS) rather than a sequential scan.
	//
	// ⚠ This is a HAND-WRITTEN RESTATEMENT of two of the branch's three disjuncts, and it does
	// not carry retainedAccountVisibilityLeg at all (#1569 residual E5). It is kept because it
	// isolates the chain leg's index choice from everything else in a very large query — but it
	// cannot notice the emitted query changing. TestUnifiedFeed_EmittedQuery_EXPLAIN, at the end
	// of this file, plans the real thing.
	bankingChainSQL := `
SELECT t.id FROM transactions t
WHERE t.user_id = $1
  AND (
      EXISTS (SELECT 1 FROM accounts WHERE id = t.account_id AND status = 'active')
      OR (
          t.user_account_id IS NOT NULL
          AND EXISTS (
              SELECT 1 FROM accounts chain_a
              WHERE chain_a.user_account_id = t.user_account_id
                AND chain_a.status = 'active'
          )
      )
  )`

	plan := runExplain(ctx, t, conn, bankingChainSQL, "00000000-0000-0000-0000-000000000000")

	assert.True(t,
		strings.Contains(plan, "Index Scan") || strings.Contains(plan, "Bitmap Index Scan"),
		"Expected an index scan for the chain EXISTS leg; got Seq Scan (missing or misnamed index).\nPlan:\n%s", plan,
	)
	assert.True(t,
		strings.Contains(plan, "idx_accounts_user_account_status") ||
			strings.Contains(plan, "idx_accounts_user_account"),
		"Plan must use a user_account_id index (idx_accounts_user_account_status or idx_accounts_user_account).\nPlan:\n%s", plan,
	)
}

// TestTransactionKeyset_EXPLAIN verifies each of the 4 keyset indexes is used
// by the corresponding ORDER BY expression when enable_seqscan is off.
func TestTransactionKeyset_EXPLAIN(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()

	// Acquire a single connection so all SET commands and EXPLAIN queries share
	// the same session — pool connections are not guaranteed to be the same.
	conn, err := db.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()

	// Disable seq scan and explicit sort so the planner must use an index that provides
	// the required ORDER BY directly (i.e. the keyset composite index).
	_, err = conn.Exec(ctx, "SET enable_seqscan = OFF")
	require.NoError(t, err)
	defer conn.Exec(ctx, "SET enable_seqscan = ON") //nolint:errcheck
	_, err = conn.Exec(ctx, "SET enable_sort = OFF")
	require.NoError(t, err)
	defer conn.Exec(ctx, "SET enable_sort = ON") //nolint:errcheck

	type tc struct {
		sortExpr    string // ORDER BY expression
		indexSuffix string // e.g. "date"
		orderDir    string // ASC or DESC
		skipAssert  bool   // for merchant: EFF JOIN has no matching index
	}

	cases := []tc{
		{
			sortExpr:    "COALESCE(authorized_date, date)",
			indexSuffix: "date",
			orderDir:    "DESC",
		},
		{
			sortExpr:    "COALESCE(authorized_date, date)",
			indexSuffix: "date",
			orderDir:    "ASC",
		},
		{
			// Merchant sort uses the effective merchant name JOIN expression (mno/mno_t chain).
			// No index covers this JOIN, so skip index assertions; runExplain's require.NoError
			// serves as the regression guard (JOIN must be syntactically valid SQL).
			sortExpr:    effectiveMerchantNameExpr,
			indexSuffix: "merchant",
			orderDir:    "DESC",
			skipAssert:  true,
		},
		{
			sortExpr:    effectiveMerchantNameExpr,
			indexSuffix: "merchant",
			orderDir:    "ASC",
			skipAssert:  true,
		},
		{
			sortExpr:    "amount",
			indexSuffix: "amount",
			orderDir:    "DESC",
		},
		{
			sortExpr:    "amount",
			indexSuffix: "amount",
			orderDir:    "ASC",
		},
		{
			sortExpr:    "COALESCE(effective_category_name, effective_pfc_primary, pfc_primary)",
			indexSuffix: "category",
			orderDir:    "DESC",
		},
		{
			sortExpr:    "COALESCE(effective_category_name, effective_pfc_primary, pfc_primary)",
			indexSuffix: "category",
			orderDir:    "ASC",
		},
	}

	for _, tc := range cases {
		tc := tc
		name := fmt.Sprintf("%s_%s", tc.indexSuffix, strings.ToLower(tc.orderDir))
		t.Run(name, func(t *testing.T) {
			// First-page form: no keyset predicate, just ORDER BY + LIMIT.
			// For merchant sort, inject merchantSortJoins to resolve the effectiveMerchantNameExpr aliases.
			joins := ""
			if tc.indexSuffix == "merchant" {
				joins = " " + merchantSortJoins
			}
			// JOINs go between "FROM transactions t" and WHERE, mirroring production
			// (transaction_repository.go). Placing them after WHERE causes SQLSTATE 42601.
			firstPageSQL := fmt.Sprintf(
				`SELECT t.id FROM transactions t%s WHERE t.user_id = $1 ORDER BY %s %s, t.id ASC LIMIT 51`,
				joins, tc.sortExpr, tc.orderDir,
			)
			plan := runExplain(ctx, t, conn, firstPageSQL, "00000000-0000-0000-0000-000000000000")

			if tc.skipAssert {
				return // runExplain's require.NoError is the only regression guard
			}

			assert.True(t,
				strings.Contains(plan, "Index Scan") ||
					strings.Contains(plan, "Bitmap Index Scan") ||
					strings.Contains(plan, "Index Only Scan"),
				"Expected index scan for sort=%s dir=%s; plan:\n%s",
				tc.indexSuffix, tc.orderDir, plan,
			)
			expectedIdx := "idx_tx_keyset_" + tc.indexSuffix
			assert.Contains(t, plan, expectedIdx,
				"Plan should reference %s; plan:\n%s", expectedIdx, plan)
		})
	}
}

// recordingDB is a DB that answers nothing and remembers everything. It exists so a test can get
// at the EXACT SQL and args a repository method emits — pgxmock's matcher hands over the SQL but
// not the arguments, and the arguments are what an EXPLAIN of the real statement needs.
type recordingDB struct {
	sql  string
	args []any
	// aggSQL/aggArgs capture the QueryRow the whole-set totals aggregate issues ahead of the page
	// query (#1626 Phase 2). It is recorded separately rather than overwriting sql because BOTH
	// statements need planning: the aggregate's first version was syntactically invalid, and no
	// pgxmock shape test could have told us — this is the test that would have.
	aggSQL  string
	aggArgs []any
}

func (d *recordingDB) Begin(context.Context) (pgx.Tx, error) { return nil, errRecordingDB }
func (d *recordingDB) Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errRecordingDB
}
func (d *recordingDB) Query(_ context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	d.sql, d.args = sql, args
	return nil, errRecordingDB
}
func (d *recordingDB) QueryRow(_ context.Context, sql string, args ...interface{}) pgx.Row {
	d.aggSQL, d.aggArgs = sql, args
	return zeroRow{}
}

// zeroRow lets the totals aggregate SUCCEED with zeroes, so recording continues on to the page
// query. Failing here would stop the method before it emits the statement this test primarily
// exists to plan — and both statements need capturing.
type zeroRow struct{}

func (zeroRow) Scan(dest ...interface{}) error {
	for _, d := range dest {
		switch v := d.(type) {
		case *int:
			*v = 0
		case *float64:
			*v = 0
		}
	}
	return nil
}

func (d *recordingDB) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }

var errRecordingDB = errors.New("recordingDB: no results, by design")

// TestUnifiedFeed_EmittedQuery_EXPLAIN is #1569 residual E5.
//
// TestChainExistsIndex_EXPLAIN above plans a HAND-WRITTEN restatement of the banking branch. That
// was already a weak link — the restatement can drift from the query without anything noticing —
// and #1569 broke it outright: task 2 added retainedAccountVisibilityLeg, a third disjunct that
// joins account_status_history, and the restatement does not contain it. So the one new access
// path this issue introduced into the feed query was the one path no EXPLAIN test covered.
//
// This test takes the text and the arguments straight out of GetByUserIDKeysetUnified via a
// recording DB, so there is nothing to keep in sync: whatever the method emits is what gets
// planned. Two things are then asserted, and the first matters as much as the second — EXPLAIN
// of a real statement also proves the emitted SQL parses and type-checks end to end, which no
// pgxmock shape test can do (pgxmock matches the string and never executes it).
func TestUnifiedFeed_EmittedQuery_EXPLAIN(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()

	rec := &recordingDB{}
	repo := NewTransactionRepository(rec, newTestFieldEncryptor(t))
	_, _, _, err := repo.GetByUserIDKeysetUnified(ctx, "00000000-0000-0000-0000-000000000000",
		domain.TransactionFilters{IncludeInvestmentActivity: true}, nil, 10)
	require.ErrorIs(t, err, errRecordingDB, "the recording DB must be the only thing that failed")
	require.NotEmpty(t, rec.sql, "no SQL captured — GetByUserIDKeysetUnified stopped calling Query")

	// Guard against the failure mode this test exists to end: planning a statement that no longer
	// contains the leg under test, and passing.
	require.Contains(t, rec.sql, "account_status_history h",
		"the emitted feed query no longer carries retainedAccountVisibilityLeg — either the leg "+
			"moved or this test is now planning something else entirely")
	require.Contains(t, rec.sql, retainedAccountVisibilityLegs("t", "t.date"),
		"the banking branch must carry the leg VERBATIM as the helper renders it")
	require.Contains(t, rec.sql, retainedAccountVisibilityLegs("it", "it.date"),
		"so must the investment branch")

	conn, err := db.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()

	_, err = conn.Exec(ctx, "SET enable_seqscan = OFF")
	require.NoError(t, err)
	defer conn.Exec(ctx, "SET enable_seqscan = ON") //nolint:errcheck

	plan := runExplain(ctx, t, conn, rec.sql, rec.args...)

	assert.NotContains(t, plan, "Seq Scan on account_status_history",
		"the retained-visibility leg must reach account_status_history by index (idx_ash_account_from, "+
			"migration 000151); a Seq Scan here is a full-table scan per candidate transaction.\nPlan:\n%s", plan)
	assert.Contains(t, plan, "account_status_history",
		"the plan must actually include the retained-visibility leg.\nPlan:\n%s", plan)

	// And the whole-set totals aggregate, which is a SECOND statement this method emits (#1626
	// Phase 2). Planning it is not optional bonus coverage: its first version failed to parse, and
	// only executing it against Postgres could say so. Because it WRAPS the page query rather than
	// restating its predicates, a plan for it also re-proves the page query still type-checks.
	require.NotEmpty(t, rec.aggSQL,
		"no aggregate captured — GetByUserIDKeysetUnified stopped computing first-page totals")
	// ⚠ WHAT IS SHARED IS THE PREDICATE-BEARING UNION BODY, NOT THE PAGE QUERY (#1626 Phase 2,
	// review finding X3). This assertion used to strip only the LIMIT and require the aggregate to
	// contain everything else — which enshrined the page's `ORDER BY sort_date, source_disc, id`
	// inside the aggregate, so an unfiltered first page sorted every banking AND investment row
	// merely to COUNT and SUM them, and then the page query sorted again. The drift-safety was the
	// right call and is unchanged: both statements are still derived from ONE string, so a predicate
	// cannot drift between them. Only the presentation ordering stopped leaking into the aggregate.
	//
	// Both boundaries are stripped from the page text here, and the page must carry both, so this
	// cannot pass by the clauses quietly disappearing.
	orderAt := strings.LastIndex(rec.sql, "ORDER BY sort_date")
	require.Positive(t, orderAt, "the page query must carry the presentation ORDER BY")
	limitAt := strings.LastIndex(rec.sql, "LIMIT $")
	require.Greater(t, limitAt, orderAt, "the page query's LIMIT must follow its ORDER BY")
	// Asserted as a bool rather than require.Contains because both strings are ~7 KB and a Contains
	// failure prints them twice, which buries the message that matters.
	unionBody := strings.TrimSpace(rec.sql[:orderAt])
	require.True(t, strings.Contains(rec.aggSQL, unionBody),
		"the aggregate must WRAP the shared union body verbatim, not restate it — a restatement is "+
			"free to drift, and a total that disagrees with its rows is worse than no total (#1626)")
	require.NotContains(t, rec.aggSQL, "ORDER BY sort_date",
		"the aggregate must NOT inherit the page's presentation ordering: COUNT(*) and SUM(amount) "+
			"are order-independent, so sorting the whole filtered set to compute them is pure cost "+
			"paid on the busiest list in the app (#1626 X3)")
	require.Len(t, rec.aggArgs, len(rec.args)-1,
		"the aggregate must bind every page arg EXCEPT the LIMIT: it has no LIMIT clause, and the "+
			"extended protocol rejects a parameter the statement does not reference")

	aggPlan := runExplain(ctx, t, conn, rec.aggSQL, rec.aggArgs...)
	assert.NotContains(t, aggPlan, "Seq Scan on account_status_history",
		"the aggregate inherits the page query's access paths and must not lose them.\nPlan:\n%s", aggPlan)

	// ⚠ THE PLAN ASSERTION IS A PAIR, and the positive half is what makes the negative half mean
	// something. Asserting only "no Sort in the aggregate" would also pass if the page query lost
	// its ordering, or if this test started planning something else entirely; requiring the SAME
	// predicate set to plan WITH a Sort for the page and WITHOUT one for the aggregate is the
	// difference the finding is about, and neither half holds on its own.
	assert.Contains(t, plan, "Sort",
		"the PAGE query must still sort — it is the ordering the list presents.\nPlan:\n%s", plan)
	assert.NotContains(t, aggPlan, "Sort",
		"the aggregate's plan must contain no Sort at all: it is a COUNT and a SUM over a UNION ALL "+
			"and has nothing to order.\nPlan:\n%s", aggPlan)
}
