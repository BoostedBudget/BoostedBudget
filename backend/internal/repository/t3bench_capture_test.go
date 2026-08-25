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
		t.Skip("T3BENCH_URL not set")
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
