//go:build integration

package repository

// Transaction repository EXPLAIN smoke tests.
// Verifies that the 4 expression+composite indexes created by migration
// 000074_add_transactions_keyset_indexes are used by the keyset pagination
// query for all sort key × direction combinations.
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
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func connectTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping EXPLAIN integration test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err, "connect to test database")
	t.Cleanup(pool.Close)
	return pool
}

// runExplain runs EXPLAIN on sql and returns the plan text.
func runExplain(ctx context.Context, t *testing.T, db *pgxpool.Pool, sql string, args ...any) string {
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

// TestTransactionKeyset_EXPLAIN verifies each of the 4 keyset indexes is used
// by the corresponding ORDER BY expression when enable_seqscan is off.
func TestTransactionKeyset_EXPLAIN(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()

	// Disable seq scan so the planner is forced to prefer an index if one applies.
	_, err := db.Exec(ctx, "SET enable_seqscan = OFF")
	require.NoError(t, err)
	defer db.Exec(ctx, "SET enable_seqscan = ON") //nolint:errcheck

	type tc struct {
		sortExpr    string // must match sortColumnMap exactly
		indexSuffix string // e.g. "date"
		orderDir    string // ASC or DESC
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
			sortExpr:    "COALESCE(merchant_name, name)",
			indexSuffix: "merchant",
			orderDir:    "DESC",
		},
		{
			sortExpr:    "COALESCE(merchant_name, name)",
			indexSuffix: "merchant",
			orderDir:    "ASC",
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
			firstPageSQL := fmt.Sprintf(
				`SELECT id FROM transactions WHERE user_id = $1 ORDER BY %s %s, id ASC LIMIT 51`,
				tc.sortExpr, tc.orderDir,
			)
			plan := runExplain(ctx, t, db, firstPageSQL, "smoke-test-user")
			expectedIdx := "idx_tx_keyset_" + tc.indexSuffix
			assert.True(t,
				strings.Contains(plan, "Index Scan") || strings.Contains(plan, "Bitmap Index Scan"),
				"Expected index scan for sort=%s dir=%s; plan:\n%s",
				tc.indexSuffix, tc.orderDir, plan,
			)
			assert.Contains(t, plan, expectedIdx,
				"Plan should reference %s; plan:\n%s", expectedIdx, plan)
		})
	}
}
