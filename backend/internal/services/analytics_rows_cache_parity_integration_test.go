//go:build integration

// Package services — Issue #1531 Task 11c: cache parity for the `analytics_rows` namespace.
//
// This is the sibling of analytics_cache_parity_integration_test.go's 19-row `analytics`
// namespace table, covering the 4 methods Task 10a placed in `analytics_rows` instead: they
// return merchant names and transaction descriptions (raw PII, not aggregates), so the namespace
// carries a shorter 300s TTL band under a tighter GDPR posture than `analytics`'s 120s. See
// cache/namespace.go's NamespaceAnalyticsRows doc comment.
//
// Reuses 11b's parityCase/runParityCase/parityFixture/bumpPersonal/mustJSON/assertDelta exactly —
// the only change to shared code was generalizing runParityCase to take an explicit `ns
// cache.Namespace` parameter (previously hardcoded to cache.NamespaceAnalytics), because a case
// whose key builder regressed to the wrong namespace must be caught, not silently pass.
//
// Scope decision: this file implements ONLY the cache-enabled suite (cold-miss / warm-hit /
// stale-money-after-mutation), not CacheDisabled/RedisDown siblings. Those two suites in 11b
// exist to prove the cache INFRASTRUCTURE (EnvDisabledNamespaces bypass, Redis-down fail-open) is
// namespace-agnostic — they already exercise the shared cache.GetOrLoad code path that these 4
// methods run through too, and Task 11c's brief scoped this file to the 4 cases themselves, not a
// re-proof of infrastructure 11b already covers. If a future audit wants the belt-and-suspenders
// version, the pattern to copy is TestAnalyticsCacheParity_CacheDisabled /
// TestAnalyticsCacheParity_RedisDown in the sibling file, swapping in this file's table.
//
// Run:
//
//	DATABASE_URL=postgres://... CACHE_REDIS_TEST_URL=redis://localhost:6397/0 \
//	  go test ./internal/services/... -tags integration -race -p 1 -run TestAnalyticsRowsCacheParity -v
package services

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/cache/cachetest"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// seedMerchant inserts a merchants row (transactions.merchant_id's FK target) and returns its id.
// Left uncleaned deliberately: fk_transactions_merchant_id has no ON DELETE clause, and
// t.Cleanup runs LIFO — a cleanup registered here would fire BEFORE newParityFixture's user
// delete (registered earlier, so it unwinds later), which would still have transactions
// referencing this merchant and fail the DELETE. A stray merchants row per test run is harmless
// (merchants carries no PII beyond a display name, and this file gives every row a
// t.Name()-qualified unique name so cross-run collisions can't happen against the unique(name)
// constraint).
func (f *parityFixture) seedMerchant(t *testing.T, ctx context.Context, name string) string {
	t.Helper()
	id := uuid.New().String()
	_, err := f.pool.Exec(ctx, `INSERT INTO merchants (id, name) VALUES ($1, $2)`, id, name)
	require.NoError(t, err, "seed merchant %q", name)
	return id
}

// seedTransactionWithMerchantID is seedTransaction plus an explicit transactions.merchant_id —
// GetCategoryBreakdownForMerchant filters on effectiveMerchantIdExpr = COALESCE(merged-into,
// t.merchant_id), a UUID FK to merchants, not on merchant_name text.
func (f *parityFixture) seedTransactionWithMerchantID(t *testing.T, ctx context.Context, amount float64, date time.Time, name, merchantName, merchantID, pfcPrimary, pfcDetailed string) {
	t.Helper()
	id := uuid.New().String()
	_, err := f.pool.Exec(ctx, `
		INSERT INTO transactions (
			id, user_id, account_id, linked_item_id, source,
			external_transaction_id, external_transaction_id_blind_idx,
			amount, date, name, merchant_name, merchant_id, pfc_primary, pfc_detailed,
			effective_category_name,
			pending, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, 'plaid', $5, $6,
			$7, $8::date, $9, $10, $11, $12, $13,
			$12,
			false, now(), now()
		)
	`, id, f.userID, f.accountID, f.linkedItemID, "ext-"+id, "blind-"+id,
		amount, date.Format("2006-01-02"), name, merchantName, merchantID, pfcPrimary, pfcDetailed)
	require.NoError(t, err, "seed transaction %q with merchant_id", name)
}

// buildAnalyticsRowsCases constructs the 4-row analytics_rows table, mirroring
// buildParityCases's shape and doc comments exactly.
func buildAnalyticsRowsCases(t *testing.T, f *parityFixture, now time.Time) []parityCase {
	periodStart := now.AddDate(0, 0, -14)
	periodEnd := now.Add(time.Hour)

	var cases []parityCase

	// ── GetTopExpense ────────────────────────────────────────────────────────────────────────
	cases = append(cases, parityCase{
		Name: "GetTopExpense",
		Seed: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
			f.seedTransaction(t, ctx, 50.00, now.AddDate(0, 0, -3), "Electronics", "Gadget Store", "GENERAL_MERCHANDISE", "GENERAL_MERCHANDISE_OTHER")
		},
		Call: func(t *testing.T, ctx context.Context, svc AnalyticsService) any {
			resp, err := svc.GetTopExpense(ctx, f.userID, periodStart, periodEnd)
			require.NoError(t, err)
			return resp
		},
		Mutate: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
			// Larger and later: becomes the new single largest expense in the window.
			f.seedTransaction(t, ctx, 120.00, now.AddDate(0, 0, -1), "New Laptop", "Computer Store", "GENERAL_MERCHANDISE", "GENERAL_MERCHANDISE_ELECTRONICS")
		},
		Bump: bumpPersonal(f),
	})

	// ── GetLargestTransactionsForPeriod ──────────────────────────────────────────────────────
	ltpFilters := domain.SpendingFilters{StartDate: periodStart, EndDate: periodEnd}
	cases = append(cases, parityCase{
		Name: "GetLargestTransactionsForPeriod",
		Seed: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
			f.seedTransaction(t, ctx, 80.00, now.AddDate(0, 0, -4), "Furniture", "Furniture Store", "GENERAL_MERCHANDISE", "GENERAL_MERCHANDISE_OTHER")
		},
		Call: func(t *testing.T, ctx context.Context, svc AnalyticsService) any {
			resp, err := svc.GetLargestTransactionsForPeriod(ctx, f.userID, ltpFilters, 5)
			require.NoError(t, err)
			return resp
		},
		Mutate: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
			f.seedTransaction(t, ctx, 150.00, now.AddDate(0, 0, -1), "Appliance", "Appliance Store", "GENERAL_MERCHANDISE", "GENERAL_MERCHANDISE_OTHER")
		},
		Bump: bumpPersonal(f),
	})

	// ── GetUnusualSpend ───────────────────────────────────────────────────────────────────────
	// Baseline: 3 same-category transactions inside [baselineStart, periodStart) with a small,
	// non-zero spread (10, 10, 12 -> mean 10.667, population stddev ~0.943) — STDDEV_POP(...)=0
	// would NULLIF to NULL and the repo's `b.stddev_amount IS NOT NULL` guard would exclude the
	// category entirely, so identical baseline amounts would silently produce an always-empty
	// result regardless of what Mutate does. Ground truth #1 has NO transaction inside the
	// analysis window [periodStart, periodEnd] at all, so the result is empty; Mutate adds one
	// deep in the window at 10x the baseline mean, which clears mean+2*stddev by a wide margin.
	cases = append(cases, parityCase{
		Name: "GetUnusualSpend",
		Seed: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
			f.seedTransaction(t, ctx, 10.00, now.AddDate(0, 0, -20), "Groceries", "Grocer", "FOOD_AND_DRINK", "FOOD_AND_DRINK_GROCERIES")
			f.seedTransaction(t, ctx, 10.00, now.AddDate(0, 0, -25), "Groceries", "Grocer", "FOOD_AND_DRINK", "FOOD_AND_DRINK_GROCERIES")
			f.seedTransaction(t, ctx, 12.00, now.AddDate(0, 0, -30), "Groceries", "Grocer", "FOOD_AND_DRINK", "FOOD_AND_DRINK_GROCERIES")
		},
		Call: func(t *testing.T, ctx context.Context, svc AnalyticsService) any {
			resp, err := svc.GetUnusualSpend(ctx, f.userID, periodStart, periodEnd)
			require.NoError(t, err)
			return resp
		},
		Mutate: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
			f.seedTransaction(t, ctx, 100.00, now.AddDate(0, 0, -2), "Groceries", "Grocer", "FOOD_AND_DRINK", "FOOD_AND_DRINK_GROCERIES")
		},
		Bump: bumpPersonal(f),
	})

	// ── GetCategoryBreakdownForMerchant ──────────────────────────────────────────────────────
	var merchantID string
	cbfmFilters := domain.SpendingFilters{StartDate: periodStart, EndDate: periodEnd}
	cases = append(cases, parityCase{
		Name: "GetCategoryBreakdownForMerchant",
		Seed: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
			// Name must be unique across separate `go test` invocations, not just within one run:
			// t.Name() is STABLE across runs (it's the subtest path, not a per-run nonce), and this
			// row is deliberately left uncleaned (see seedMerchant's doc comment) — a name keyed only
			// on t.Name() collides with a leftover row from any prior run against the same DB and
			// fails merchants_name_unique on the INSERT, a false failure with nothing wrong in the
			// case under test.
			merchantID = f.seedMerchant(t, ctx, "Merchant "+t.Name()+"-"+uuid.New().String())
			f.seedTransactionWithMerchantID(t, ctx, 40.00, now.AddDate(0, 0, -4), "Groceries", "The Merchant", merchantID, "FOOD_AND_DRINK", "FOOD_AND_DRINK_GROCERIES")
		},
		Call: func(t *testing.T, ctx context.Context, svc AnalyticsService) any {
			resp, err := svc.GetCategoryBreakdownForMerchant(ctx, f.userID, merchantID, cbfmFilters)
			require.NoError(t, err)
			return resp
		},
		Mutate: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
			// A second category from the SAME merchant: moves both the per-category totals and
			// every existing category's percentage (percentages are of a shared TotalSpend).
			f.seedTransactionWithMerchantID(t, ctx, 30.00, now.AddDate(0, 0, -1), "Electronics", "The Merchant", merchantID, "GENERAL_MERCHANDISE", "GENERAL_MERCHANDISE_OTHER")
		},
		Bump: bumpPersonal(f),
	})

	return cases
}

func analyticsRowsCaseNames() []string {
	return []string{
		"GetTopExpense",
		"GetLargestTransactionsForPeriod",
		"GetUnusualSpend",
		"GetCategoryBreakdownForMerchant",
	}
}

// TestAnalyticsRowsCacheParity is the analytics_rows-namespace counterpart of
// TestAnalyticsCacheParity: same 7-step body (runParityCase), same real-Redis setup, but
// asserting cache.NamespaceAnalyticsRows instead of cache.NamespaceAnalytics — the whole point of
// this file per the Task 11c brief's warning that a case landing in the wrong namespace would
// otherwise silently pass.
func TestAnalyticsRowsCacheParity(t *testing.T) {
	pool := connectIntegrationDB(t)
	redisClient := cachetest.ConnectTestRedis(t)
	c := cache.NewCache(cache.Options{Client: redisClient})
	ctx := context.Background()

	for _, name := range analyticsRowsCaseNames() {
		name := name
		t.Run(name, func(t *testing.T) {
			// Deliberately no t.Parallel() — cache.Snapshot/Delta reads a process-global counter.
			f := newParityFixture(t, ctx, pool)
			analyticsRepo := repository.NewAnalyticsRepository(pool)
			balanceRepo := repository.NewBalanceSnapshotRepository(pool)
			inner := NewAnalyticsService(analyticsRepo)
			inner.SetBalanceSnapshotRepo(balanceRepo)
			cachedSvc := NewCachedAnalyticsService(inner, c)

			cases := buildAnalyticsRowsCases(t, f, time.Now().UTC())
			var tc parityCase
			for _, c2 := range cases {
				if c2.Name == name {
					tc = c2
				}
			}
			require.NotEmpty(t, tc.Name, "case %q must exist in buildAnalyticsRowsCases", name)

			runParityCase(t, ctx, pool, inner, cachedSvc, c.Generations(), tc, cache.NamespaceAnalyticsRows, true)
		})
	}
}

// TestAnalyticsRowsCacheParityCaseNames_MatchTemplate guards analyticsRowsCaseNames against
// drifting out of sync with buildAnalyticsRowsCases's real construction order, mirroring
// TestAnalyticsCacheParityCaseNames_MatchTemplate in the sibling file.
func TestAnalyticsRowsCacheParityCaseNames_MatchTemplate(t *testing.T) {
	pool := connectIntegrationDB(t)
	ctx := context.Background()
	f := newParityFixture(t, ctx, pool)
	real := buildAnalyticsRowsCases(t, f, time.Now().UTC())

	names := analyticsRowsCaseNames()
	require.Len(t, real, len(names), "buildAnalyticsRowsCases row count must match the template")
	for i, tc := range real {
		require.Equal(t, names[i], tc.Name, "row %d name mismatch", i)
	}
}
