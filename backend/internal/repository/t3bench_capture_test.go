//go:build t3bench

// Package-local measurement harness for #1569 T3 / #1582. Never built by
// `go test ./...` nor by `-tags integration`: it needs a hand-seeded 200k-row
// household that the ordinary fixtures do not create. Run it with:
//
//	T3BENCH_URL=postgres://... go test -tags t3bench -run TestT3Bench ./internal/repository/ -v
//
// testdata/t3bench_seed.sql builds that database and documents its shape. Set
// T3BENCH_EXPLAIN=1 to also print EXPLAIN (ANALYZE, BUFFERS) for each of the nine reads.
// The DB it points at MUST be a throwaway — it is never cleaned up.
//
// ⚠ THERE IS NO CI PERF GUARD, AND THIS TEST IS NOT ONE. Nothing in .github/workflows, the
// Makefiles or scripts/ sets `-tags t3bench` (checked 2026-08-26), so CI never builds this file and
// no threshold is ever compared. It is a measurement instrument you point at a database by hand,
// and the numbers recorded in analytics_repository.go are only as current as the last time a
// person ran it.
//
// ⚠ It used to `t.Skip` when T3BENCH_URL was unset. That is fail-open — the same green-by-absence
// shape #1569 spent five rounds removing from six other guards — and it is worse than it looks:
// `go test` DISCARDS a passing package's output unless -v is passed, so even a stderr banner on
// the skip path is invisible in the exact invocation most likely to hide it. So a missing
// T3BENCH_URL is now a FATAL, not a skip. Asking for `-tags t3bench` IS asking to benchmark;
// having measured nothing is a failure of that request, and it cannot be mistaken for a pass.
// This is safe precisely because no automated caller sets the tag — if one ever does, it must
// supply a seeded T3BENCH_URL, which is the point.
//
// Last run by a human: 2026-08-26, commit 3d21e077, Apple M4 / 10 cores / 16 GiB / macOS 15.3.1,
// against Postgres 17.11 in Docker on localhost:5434. Seed loaded clean against the schema of that
// day (no rot). See householdGrantsCTE's round-5 table for what it produced.
package repository

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// capturingDB records the exact SQL + args the repository sends, then delegates.
type capturingDB struct {
	inner *pgxpool.Pool
	sql   string
	args  []interface{}
}

func (c *capturingDB) Begin(ctx context.Context) (pgx.Tx, error) { return c.inner.Begin(ctx) }
func (c *capturingDB) Exec(ctx context.Context, sql string, a ...interface{}) (pgconn.CommandTag, error) {
	return c.inner.Exec(ctx, sql, a...)
}
func (c *capturingDB) Query(ctx context.Context, sql string, a ...interface{}) (pgx.Rows, error) {
	c.sql, c.args = sql, a
	return c.inner.Query(ctx, sql, a...)
}
func (c *capturingDB) QueryRow(ctx context.Context, sql string, a ...interface{}) pgx.Row {
	c.sql, c.args = sql, a
	return c.inner.QueryRow(ctx, sql, a...)
}
func (c *capturingDB) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	return c.inner.SendBatch(ctx, b)
}

const (
	benchHousehold = "00000000-0000-0000-0000-0000000000aa"
	benchUser1     = "00000000-0000-0000-0000-000000000001"
)

func benchScope() *domain.HouseholdScope {
	return &domain.HouseholdScope{
		HouseholdID: benchHousehold,
		MemberUserIDs: []string{
			"00000000-0000-0000-0000-000000000001",
			"00000000-0000-0000-0000-000000000002",
			"00000000-0000-0000-0000-000000000003",
		},
	}
}

func TestT3Bench(t *testing.T) {
	url := os.Getenv("T3BENCH_URL")
	if url == "" {
		// Loud on purpose: a silent skip here reads as "the benchmark passed".
		fmt.Fprint(os.Stderr, "\n"+
			"################################################################################\n"+
			"# T3BENCH DID NOT RUN. T3BENCH_URL is unset, so NOTHING was measured.           #\n"+
			"# This is NOT a perf guard and measures nothing without a URL. The timings in   #\n"+
			"# analytics_repository.go are unverified by this run. To actually measure:      #\n"+
			"#   createdb t3bench && pg_dump -s <migrated-db> | psql t3bench \\               #\n"+
			"#     && psql t3bench -f internal/repository/testdata/t3bench_seed.sql          #\n"+
			"#   T3BENCH_URL=postgres://.../t3bench go test -tags t3bench -run TestT3Bench \\ #\n"+
			"#     ./internal/repository/ -v                                                 #\n"+
			"################################################################################\n\n")
		t.Fatal("T3BENCH_URL not set — NOTHING MEASURED (see the banner above). " +
			"This is a hard failure on purpose: -tags t3bench asks for a benchmark, and a skip " +
			"here would be indistinguishable from a passing perf check.")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()

	start := time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)
	cf := domain.CashFlowFilters{StartDate: start, EndDate: end, HouseholdScope: benchScope(), Grouping: "monthly"}
	sf := domain.SpendingFilters{StartDate: start, EndDate: end, HouseholdScope: benchScope()}

	cases := []struct {
		name string
		run  func(r AnalyticsRepository) error
	}{
		{"CashFlowAggregation", func(r AnalyticsRepository) error {
			_, e := r.GetCashFlowAggregation(ctx, benchUser1, cf)
			return e
		}},
		{"CashFlowGrouped", func(r AnalyticsRepository) error {
			_, e := r.GetCashFlowGrouped(ctx, benchUser1, cf)
			return e
		}},
		{"SpendingByCategory", func(r AnalyticsRepository) error {
			_, e := r.GetSpendingByCategory(ctx, benchUser1, sf)
			return e
		}},
		{"SpendingByMerchant", func(r AnalyticsRepository) error {
			_, e := r.GetSpendingByMerchant(ctx, benchUser1, sf, 20)
			return e
		}},
		{"SpendingTrends", func(r AnalyticsRepository) error {
			_, e := r.GetSpendingTrends(ctx, benchUser1, sf)
			return e
		}},
		{"AverageMonthlySpend", func(r AnalyticsRepository) error {
			_, e := r.GetAverageMonthlySpend(ctx, benchUser1, sf)
			return e
		}},
		{"SavingsRateForPeriod", func(r AnalyticsRepository) error {
			_, e := r.GetSavingsRateForPeriod(ctx, benchUser1, start, end, benchScope())
			return e
		}},
		{"MonthOverMonthSpending", func(r AnalyticsRepository) error {
			_, e := r.GetMonthOverMonthSpending(ctx, benchUser1, start, "", sf)
			return e
		}},
		{"IncomeSourceBreakdown", func(r AnalyticsRepository) error {
			_, e := r.GetIncomeSourceBreakdown(ctx, benchUser1, sf)
			return e
		}},
	}

	iters := 5
	explainOnly := os.Getenv("T3BENCH_EXPLAIN") != ""
	for _, tc := range cases {
		cap := &capturingDB{inner: pool}
		repo := NewAnalyticsRepository(cap)

		if err := tc.run(repo); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if cap.sql == "" {
			t.Fatalf("%s: no SQL captured", tc.name)
		}

		var best, total time.Duration
		for i := 0; i < iters; i++ {
			t0 := time.Now()
			if err := tc.run(repo); err != nil {
				t.Fatalf("%s iter %d: %v", tc.name, i, err)
			}
			d := time.Since(t0)
			total += d
			if best == 0 || d < best {
				best = d
			}
		}
		fmt.Printf("\n=== %s  best=%v mean=%v (n=%d)\n", tc.name, best, total/time.Duration(iters), iters)

		if explainOnly {
			rows, err := pool.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS, SETTINGS) "+cap.sql, cap.args...)
			if err != nil {
				t.Fatalf("%s explain: %v", tc.name, err)
			}
			var lines []string
			for rows.Next() {
				var l string
				if err := rows.Scan(&l); err != nil {
					t.Fatal(err)
				}
				lines = append(lines, l)
			}
			rows.Close()
			fmt.Println(strings.Join(lines, "\n"))
		}
	}
}
