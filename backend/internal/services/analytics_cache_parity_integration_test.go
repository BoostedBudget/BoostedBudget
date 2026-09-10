//go:build integration

// Package services — cache parity harness for Issue #1531 Task 11b.
//
// This is the acceptance gate for the entire caching layer: two independent adversarial design
// reviews recommended NOT shipping a cache over financial data at all, and the user overrode that
// on the condition the risk be neutralised by testing. This file is that neutralisation.
//
// One table (parityCase), one body (runParityCase), reused by three suites:
//   - TestAnalyticsCacheParity            — cache enabled, real Redis: cold-miss, warm-hit,
//     stale-money-after-mutation.
//   - TestAnalyticsCacheParity_CacheDisabled — same table, cache disabled via
//     EnvDisabledNamespaces: every result byte-identical to ground truth, zero Redis commands.
//   - TestAnalyticsCacheParity_RedisDown      — same table, Redis stopped mid-suite (a disposable
//     container, never the shared one): every result still correct, no error reaches the caller.
//
// Scope: the 13 `analytics`-namespace methods named in task-11-brief.md, expanded to 19 table rows
// (GetCashFlowHistory and GetNetWorthTrend each get their 3 grouping variants; GetSpendingByCategory
// gets its drill-down variant). `analytics_rows`, dashprefs, identity, the household-scope suite,
// the fail-safe suite, E2E, and CI are explicitly out of scope for this slice (Task 11c/d/e).
//
// Run:
//
//	DATABASE_URL=postgres://... CACHE_REDIS_TEST_URL=redis://localhost:6397/0 \
//	  go test ./internal/services/... -tags integration -race -p 1 -run TestAnalyticsCacheParity -v
package services

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/cache/cachetest"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// ─── parityCase: one cached read, and the domain-correct mutation that must move it ───────────

// parityCase is one cached-read/mutation pair. Seed, Call, and Mutate all close over whatever
// per-case state (account IDs, budget IDs, a fixed "now") the case needs — this file does not
// force a single shared fixture shape onto every method, since GetBudgetVsActual (reads a stored
// column) and GetNetWorthTrend (reads balance snapshots) have nothing in common with the
// transaction-based methods beyond "some Postgres state that AnalyticsService reads".
type parityCase struct {
	Name string
	// Seed inserts whatever Postgres state Call's ground-truth reading depends on.
	Seed func(t *testing.T, ctx context.Context, pool *pgxpool.Pool)
	// Call invokes the one cached method under test against svc and returns its response.
	Call func(t *testing.T, ctx context.Context, svc AnalyticsService) any
	// Mutate changes exactly the Postgres state Call reads, so that a second Call(uncachedSvc)
	// returns a DIFFERENT value. Step 6 of the harness body asserts this — a Mutate that doesn't
	// actually move the aggregate is a test-authoring bug, not a passing case.
	Mutate func(t *testing.T, ctx context.Context, pool *pgxpool.Pool)
	// Bump mints a fresh generation token for this case's scope.
	//
	// ⚠ It is NOT "the production invalidation hook", as this comment previously claimed — it is
	// supplied by the test (see bumpPersonal below). That is correct for THIS harness, whose job is
	// proving a cached read equals uncached ground truth; supplying the bump isolates the read path
	// from the question of who triggers it. But it means these cases cannot detect a production path
	// that forgets to bump. That property is covered elsewhere: 24 tests across 8 files in this
	// package drive real service methods with a real cache.Generations and assert the generation
	// moved (e.g. account_service_cache_invalidation_test.go,
	// household_cache_invalidation_test.go, transaction_sync_cache_invalidation_test.go), and
	// cmd/server/deletion_route_middleware_integration_test.go covers the HTTP-middleware side.
	//
	// Original note: mint a fresh generation token for this case's
	// scope. Every case in this slice is personal-scope (household scope is Task 11c/d), so this
	// is the same closure shape everywhere, but kept as a per-case field because a future
	// household-scope table will need a different scope per case.
	Bump func(ctx context.Context, gens *cache.Generations)
}

// ─── Seed helpers: raw SQL against the real schema, mirroring the proven pattern in
// transaction_service_search_integration_test.go and household_service_integration_test.go ──────

// parityFixture bundles the Postgres pool and one seeded user+account, shared by every case's
// closures within a single subtest run. Each subtest gets its OWN fixture (own userID, own
// account) so cases never contend for the same cache scope or the same rows.
type parityFixture struct {
	pool         *pgxpool.Pool
	userID       string
	accountID    string
	linkedItemID string
}

func newParityFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) *parityFixture {
	t.Helper()
	userID := uuid.New().String()
	linkedItemID := uuid.New().String()
	accountID := uuid.New().String()

	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "int-parity|"+userID, "int-parity-"+userID+"@example.com")
	require.NoError(t, err, "seed user")

	_, err = pool.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, linkedItemID, userID, "parity-item-"+linkedItemID)
	require.NoError(t, err, "seed linked_item")

	_, err = pool.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, 'Parity Test Checking', 'depository', 'active')
	`, accountID, linkedItemID, userID, "plaid-parity-acc-"+accountID)
	require.NoError(t, err, "seed account")

	t.Cleanup(func() {
		// FK cascades (users -> linked_items -> accounts -> transactions/balance_snapshots,
		// users -> budgets -> budget_categories) remove everything this fixture created.
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID) //nolint:errcheck
	})

	return &parityFixture{pool: pool, userID: userID, accountID: accountID, linkedItemID: linkedItemID}
}

func (f *parityFixture) seedTransaction(t *testing.T, ctx context.Context, amount float64, date time.Time, name, merchantName, pfcPrimary, pfcDetailed string) {
	t.Helper()
	id := uuid.New().String()
	_, err := f.pool.Exec(ctx, `
		INSERT INTO transactions (
			id, user_id, account_id, linked_item_id, source,
			external_transaction_id, external_transaction_id_blind_idx,
			amount, date, name, merchant_name, pfc_primary, pfc_detailed,
			effective_category_name,
			pending, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, 'plaid', $5, $6,
			$7, $8::date, $9, $10, $11, $12,
			$11,
			false, now(), now()
		)
	`, id, f.userID, f.accountID, f.linkedItemID, "ext-"+id, "blind-"+id,
		amount, date.Format("2006-01-02"), name, merchantName, pfcPrimary, pfcDetailed)
	require.NoError(t, err, "seed transaction %q", name)
}

func (f *parityFixture) seedBalanceSnapshot(t *testing.T, ctx context.Context, balance float64, at time.Time) {
	t.Helper()
	repo := repository.NewBalanceSnapshotRepository(f.pool)
	b := balance
	err := repo.Create(ctx, &domain.BalanceSnapshot{
		AccountID:      f.accountID,
		UserID:         f.userID,
		CurrentBalance: &b,
		CurrencyCode:   "USD",
		SnapshotAt:     at,
		Source:         domain.SnapshotSourceManual,
	})
	require.NoError(t, err, "seed balance snapshot")
}

// seedBudget inserts one active monthly budget with one budget_categories row, returning the
// budget ID so Mutate can update spent_amount on the exact row Call reads.
func (f *parityFixture) seedBudget(t *testing.T, ctx context.Context, period string, allocated, spent float64) string {
	t.Helper()
	budgetID := uuid.New().String()
	_, err := f.pool.Exec(ctx, `
		INSERT INTO budgets (id, series_id, user_id, name, period, start_date, amount, is_active)
		VALUES ($1, $1, $2, $3, $4, CURRENT_DATE, $5, true)
	`, budgetID, f.userID, "Parity Budget "+budgetID, period, allocated)
	require.NoError(t, err, "seed budget")

	catID := uuid.New().String()
	_, err = f.pool.Exec(ctx, `
		INSERT INTO budget_categories (id, budget_id, category_kind, pfc_primary, allocated_amount, spent_amount)
		VALUES ($1, $2, 'pfc', 'FOOD_AND_DRINK', $3, $4)
	`, catID, budgetID, allocated, spent)
	require.NoError(t, err, "seed budget_category")

	return budgetID
}

func (f *parityFixture) updateBudgetSpent(t *testing.T, ctx context.Context, budgetID string, spent float64) {
	t.Helper()
	_, err := f.pool.Exec(ctx, `UPDATE budget_categories SET spent_amount = $1 WHERE budget_id = $2`, spent, budgetID)
	require.NoError(t, err, "update budget_category.spent_amount")
}

// ─── The 19-row table (14 method groups; CashFlowHistory and NetWorthTrend each contribute 3
// grouping-variant rows) ─────────────────────────────────────────────────────────────────────

// buildParityCases constructs the table fresh for one subtest run. now is captured once so every
// closure in every case sees the identical instant — Seed, the three Call invocations, and Mutate
// all reason about the same "today", eliminating any drift between wall-clock reads.
func buildParityCases(t *testing.T, f *parityFixture, now time.Time) []parityCase {
	periodStart := now.AddDate(0, 0, -14)
	periodEnd := now.Add(time.Hour) // buffer past `now` so EndDate <= comparisons never clip today

	var cases []parityCase

	// ── GetCashFlowSummary ──────────────────────────────────────────────────────────────────
	cfsFilters := domain.CashFlowFilters{StartDate: periodStart, EndDate: periodEnd}
	cases = append(cases, parityCase{
		Name: "GetCashFlowSummary",
		Seed: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
			f.seedTransaction(t, ctx, 20.00, now.AddDate(0, 0, -3), "Groceries", "Local Grocer", "FOOD_AND_DRINK", "FOOD_AND_DRINK_GROCERIES")
		},
		Call: func(t *testing.T, ctx context.Context, svc AnalyticsService) any {
			resp, err := svc.GetCashFlowSummary(ctx, f.userID, cfsFilters)
			require.NoError(t, err)
			return resp
		},
		Mutate: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
			f.seedTransaction(t, ctx, 55.00, now.AddDate(0, 0, -2), "Electronics", "Gadget Store", "GENERAL_MERCHANDISE", "GENERAL_MERCHANDISE_OTHER")
		},
		Bump: bumpPersonal(f),
	})

	// ── GetCashFlowHistory ×3 groupings ─────────────────────────────────────────────────────
	for _, grouping := range []string{"daily", "weekly", "monthly"} {
		grouping := grouping
		cfhFilters := domain.CashFlowFilters{StartDate: periodStart, EndDate: periodEnd, Grouping: grouping}
		cases = append(cases, parityCase{
			Name: "GetCashFlowHistory/" + grouping,
			Seed: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
				f.seedTransaction(t, ctx, 12.00, now.AddDate(0, 0, -3), "Coffee", "Cafe", "FOOD_AND_DRINK", "FOOD_AND_DRINK_COFFEE")
			},
			Call: func(t *testing.T, ctx context.Context, svc AnalyticsService) any {
				resp, err := svc.GetCashFlowHistory(ctx, f.userID, cfhFilters)
				require.NoError(t, err)
				return resp
			},
			Mutate: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
				f.seedTransaction(t, ctx, 33.00, now.AddDate(0, 0, -1), "Lunch", "Diner", "FOOD_AND_DRINK", "FOOD_AND_DRINK_RESTAURANT")
			},
			Bump: bumpPersonal(f),
		})
	}

	// ── GetSpendingByCategory (no drill-down) ───────────────────────────────────────────────
	spcFilters := domain.SpendingFilters{StartDate: periodStart, EndDate: periodEnd}
	cases = append(cases, parityCase{
		Name: "GetSpendingByCategory",
		Seed: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
			f.seedTransaction(t, ctx, 40.00, now.AddDate(0, 0, -4), "Groceries", "Local Grocer", "FOOD_AND_DRINK", "FOOD_AND_DRINK_GROCERIES")
		},
		Call: func(t *testing.T, ctx context.Context, svc AnalyticsService) any {
			resp, err := svc.GetSpendingByCategory(ctx, f.userID, spcFilters)
			require.NoError(t, err)
			return resp
		},
		Mutate: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
			f.seedTransaction(t, ctx, 60.00, now.AddDate(0, 0, -2), "New Shoes", "Shoe Store", "GENERAL_MERCHANDISE", "GENERAL_MERCHANDISE_OTHER")
		},
		Bump: bumpPersonal(f),
	})

	// ── GetSpendingByCategory drill-down (Category set) ─────────────────────────────────────
	spcDrilldownFilters := domain.SpendingFilters{StartDate: periodStart, EndDate: periodEnd, Category: "FOOD_AND_DRINK"}
	cases = append(cases, parityCase{
		Name: "GetSpendingByCategory/drilldown",
		Seed: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
			f.seedTransaction(t, ctx, 15.00, now.AddDate(0, 0, -4), "Dinner", "Restaurant", "FOOD_AND_DRINK", "FOOD_AND_DRINK_RESTAURANT")
		},
		Call: func(t *testing.T, ctx context.Context, svc AnalyticsService) any {
			resp, err := svc.GetSpendingByCategory(ctx, f.userID, spcDrilldownFilters)
			require.NoError(t, err)
			return resp
		},
		Mutate: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
			// A different SUBcategory within the same primary category: moves the drill-down
			// breakdown (a new subcategory row / higher total) without touching any other case's
			// primary-category total.
			f.seedTransaction(t, ctx, 22.00, now.AddDate(0, 0, -1), "Groceries", "Grocer", "FOOD_AND_DRINK", "FOOD_AND_DRINK_GROCERIES")
		},
		Bump: bumpPersonal(f),
	})

	// ── GetSpendingByMerchant ────────────────────────────────────────────────────────────────
	spmFilters := domain.SpendingFilters{StartDate: periodStart, EndDate: periodEnd}
	cases = append(cases, parityCase{
		Name: "GetSpendingByMerchant",
		Seed: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
			f.seedTransaction(t, ctx, 8.00, now.AddDate(0, 0, -3), "Coffee", "Corner Cafe", "FOOD_AND_DRINK", "FOOD_AND_DRINK_COFFEE")
		},
		Call: func(t *testing.T, ctx context.Context, svc AnalyticsService) any {
			resp, err := svc.GetSpendingByMerchant(ctx, f.userID, spmFilters, 10)
			require.NoError(t, err)
			return resp
		},
		Mutate: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
			f.seedTransaction(t, ctx, 9.50, now.AddDate(0, 0, -1), "Coffee", "Corner Cafe", "FOOD_AND_DRINK", "FOOD_AND_DRINK_COFFEE")
		},
		Bump: bumpPersonal(f),
	})

	// ── GetSpendingTrends ────────────────────────────────────────────────────────────────────
	stFilters := domain.SpendingFilters{StartDate: periodStart, EndDate: periodEnd}
	cases = append(cases, parityCase{
		Name: "GetSpendingTrends",
		Seed: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
			f.seedTransaction(t, ctx, 18.00, now.AddDate(0, 0, -3), "Gas", "Gas Station", "TRANSPORTATION", "TRANSPORTATION_GAS")
		},
		Call: func(t *testing.T, ctx context.Context, svc AnalyticsService) any {
			resp, err := svc.GetSpendingTrends(ctx, f.userID, stFilters)
			require.NoError(t, err)
			return resp
		},
		Mutate: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
			f.seedTransaction(t, ctx, 27.00, now.AddDate(0, 0, -1), "Gas", "Gas Station", "TRANSPORTATION", "TRANSPORTATION_GAS")
		},
		Bump: bumpPersonal(f),
	})

	// ── GetRecurringSubscriptions ────────────────────────────────────────────────────────────
	// Seed 2 monthly-spaced occurrences (below the repo's HAVING COUNT(*) >= 3 threshold, so
	// wantOff is an empty list); Mutate adds the 3rd, completing an exact-30-day cadence
	// (avg_gap=30 in [25,40], stddev=0 < 10) so the merchant crosses into "recurring".
	cases = append(cases, parityCase{
		Name: "GetRecurringSubscriptions",
		Seed: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
			f.seedTransaction(t, ctx, 9.99, now.AddDate(0, 0, -60), "Streaming", "Streaming Co", "ENTERTAINMENT", "ENTERTAINMENT_TV_AND_MOVIES")
			f.seedTransaction(t, ctx, 9.99, now.AddDate(0, 0, -30), "Streaming", "Streaming Co", "ENTERTAINMENT", "ENTERTAINMENT_TV_AND_MOVIES")
		},
		Call: func(t *testing.T, ctx context.Context, svc AnalyticsService) any {
			resp, err := svc.GetRecurringSubscriptions(ctx, f.userID)
			require.NoError(t, err)
			// AsOfDate is set to time.Now() fresh on every uncached call, which would make
			// wantOff/wantOff2 (both computed via the uncached service, seconds apart) spuriously
			// unequal. It is deliberately excluded from the comparison; every other field is
			// compared as-is.
			resp.AsOfDate = ""
			return resp
		},
		Mutate: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
			f.seedTransaction(t, ctx, 9.99, now, "Streaming", "Streaming Co", "ENTERTAINMENT", "ENTERTAINMENT_TV_AND_MOVIES")
		},
		Bump: bumpPersonal(f),
	})

	// ── GetMonthOverMonthSpendingChange ──────────────────────────────────────────────────────
	cases = append(cases, parityCase{
		Name: "GetMonthOverMonthSpendingChange",
		Seed: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
			f.seedTransaction(t, ctx, 25.00, now, "Groceries", "Grocer", "FOOD_AND_DRINK", "FOOD_AND_DRINK_GROCERIES")
		},
		Call: func(t *testing.T, ctx context.Context, svc AnalyticsService) any {
			resp, err := svc.GetMonthOverMonthSpendingChange(ctx, f.userID, "FOOD_AND_DRINK", 3, nil, false, nil)
			require.NoError(t, err)
			return resp
		},
		Mutate: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
			f.seedTransaction(t, ctx, 30.00, now, "Groceries", "Grocer", "FOOD_AND_DRINK", "FOOD_AND_DRINK_GROCERIES")
		},
		Bump: bumpPersonal(f),
	})

	// ── GetNetWorthTrend ×3 groupings ────────────────────────────────────────────────────────
	for _, grouping := range []string{"daily", "weekly", "monthly"} {
		grouping := grouping
		cases = append(cases, parityCase{
			Name: "GetNetWorthTrend/" + grouping,
			Seed: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
				f.seedBalanceSnapshot(t, ctx, 1000.00, now.AddDate(0, 0, -10))
			},
			Call: func(t *testing.T, ctx context.Context, svc AnalyticsService) any {
				resp, err := svc.GetNetWorthTrend(ctx, f.userID, grouping, nil)
				require.NoError(t, err)
				return resp
			},
			Mutate: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
				// A later same-account snapshot, STRICTLY INSIDE the read window rather than at
				// its edge. bucketNetWorth takes the LAST observed value in each bucket, so a
				// day after the seed's either changes that bucket's net worth (same bucket) or
				// adds a bucket — different JSON for every grouping either way.
				//
				// ⚠ THE `-5` IS LOAD-BEARING; this mutation used to be written at `now` and was
				// then absorbed entirely (#1626, gap G18). The absorber is NOT the carry-forward
				// itself but the end bound introduced alongside it: GetAggregatedHistory now
				// compares `(bs.snapshot_at AT TIME ZONE 'UTC')::date <= $3::date` where it
				// previously compared the instants `bs.snapshot_at <= $3`, and pgx resolves that
				// parameter to a DATE, which it encodes from `time.Now()` in the PROCESS-LOCAL
				// zone. A snapshot written at UTC `now` therefore falls OUTSIDE the window
				// whenever the process's local date lags the UTC date — every evening in any
				// negative-offset zone — so both ground-truth reads returned the seed row alone
				// and the case could not test staleness at all. Verified: at `now` these three
				// cases fail under `TZ=Etc/GMT+12` with the step-6 self-check; at `-5` they pass
				// under UTC, Etc/GMT+12 and Asia/Tokyo alike, five days being far outside any
				// zone offset.
				f.seedBalanceSnapshot(t, ctx, 1500.00, now.AddDate(0, 0, -5))
			},
			Bump: bumpPersonal(f),
		})
	}

	// ── GetBudgetVsActual ────────────────────────────────────────────────────────────────────
	var budgetID string
	cases = append(cases, parityCase{
		Name: "GetBudgetVsActual",
		Seed: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
			budgetID = f.seedBudget(t, ctx, "monthly", 500.00, 50.00)
		},
		Call: func(t *testing.T, ctx context.Context, svc AnalyticsService) any {
			resp, err := svc.GetBudgetVsActual(ctx, f.userID, "monthly")
			require.NoError(t, err)
			return resp
		},
		Mutate: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
			// GetBudgetVsActual reads budget_categories.spent_amount directly — it is a
			// denormalized, app-maintained column (not computed live from transactions), so the
			// domain-correct mutation IS the column update, not a transaction insert.
			f.updateBudgetSpent(t, ctx, budgetID, 250.00)
		},
		Bump: bumpPersonal(f),
	})

	// ── GetSavingsRateForPeriod ──────────────────────────────────────────────────────────────
	cases = append(cases, parityCase{
		Name: "GetSavingsRateForPeriod",
		Seed: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
			f.seedTransaction(t, ctx, -1000.00, now.AddDate(0, 0, -10), "Payroll", "Employer Inc", "INCOME", "INCOME_WAGES")
		},
		Call: func(t *testing.T, ctx context.Context, svc AnalyticsService) any {
			resp, err := svc.GetSavingsRateForPeriod(ctx, f.userID, periodStart, periodEnd, nil)
			require.NoError(t, err)
			return resp
		},
		Mutate: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
			f.seedTransaction(t, ctx, 200.00, now.AddDate(0, 0, -2), "Rent", "Landlord", "RENT_AND_UTILITIES", "RENT_AND_UTILITIES_RENT")
		},
		Bump: bumpPersonal(f),
	})

	// ── GetAverageMonthlySpend ───────────────────────────────────────────────────────────────
	amsFilters := domain.SpendingFilters{StartDate: periodStart, EndDate: periodEnd}
	cases = append(cases, parityCase{
		Name: "GetAverageMonthlySpend",
		Seed: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
			f.seedTransaction(t, ctx, 100.00, now.AddDate(0, 0, -3), "Shopping", "Store", "GENERAL_MERCHANDISE", "GENERAL_MERCHANDISE_OTHER")
		},
		Call: func(t *testing.T, ctx context.Context, svc AnalyticsService) any {
			resp, err := svc.GetAverageMonthlySpend(ctx, f.userID, amsFilters)
			require.NoError(t, err)
			return resp
		},
		Mutate: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
			f.seedTransaction(t, ctx, 50.00, now.AddDate(0, 0, -1), "Shopping", "Store", "GENERAL_MERCHANDISE", "GENERAL_MERCHANDISE_OTHER")
		},
		Bump: bumpPersonal(f),
	})

	// ── GetCategoryTotalForPeriod ────────────────────────────────────────────────────────────
	cases = append(cases, parityCase{
		Name: "GetCategoryTotalForPeriod",
		Seed: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
			f.seedTransaction(t, ctx, 30.00, now.AddDate(0, 0, -3), "Groceries", "Grocer", "FOOD_AND_DRINK", "FOOD_AND_DRINK_GROCERIES")
		},
		Call: func(t *testing.T, ctx context.Context, svc AnalyticsService) any {
			resp, err := svc.GetCategoryTotalForPeriod(ctx, f.userID, "FOOD_AND_DRINK", periodStart, periodEnd)
			require.NoError(t, err)
			return resp
		},
		Mutate: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
			f.seedTransaction(t, ctx, 20.00, now.AddDate(0, 0, -1), "Restaurant", "Diner", "FOOD_AND_DRINK", "FOOD_AND_DRINK_RESTAURANT")
		},
		Bump: bumpPersonal(f),
	})

	// ── GetIncomeSourceBreakdown ─────────────────────────────────────────────────────────────
	isbFilters := domain.SpendingFilters{StartDate: periodStart, EndDate: periodEnd}
	cases = append(cases, parityCase{
		Name: "GetIncomeSourceBreakdown",
		Seed: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
			f.seedTransaction(t, ctx, -500.00, now.AddDate(0, 0, -5), "Payroll", "Employer Inc", "INCOME", "INCOME_WAGES")
		},
		Call: func(t *testing.T, ctx context.Context, svc AnalyticsService) any {
			resp, err := svc.GetIncomeSourceBreakdown(ctx, f.userID, isbFilters)
			require.NoError(t, err)
			return resp
		},
		Mutate: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
			f.seedTransaction(t, ctx, -200.00, now.AddDate(0, 0, -1), "Refund", "Employer Inc", "INCOME", "INCOME_WAGES")
		},
		Bump: bumpPersonal(f),
	})

	// ── GetCategoryHierarchy (#1626) ─────────────────────────────────────────────────────────
	// Direction is a METHOD ARGUMENT rather than a SpendingFilters field, so it is not covered by
	// EncodeSpendingFilters; this case exists partly to prove the wrapper caches and invalidates
	// correctly around that extra key component.
	chFilters := domain.SpendingFilters{StartDate: periodStart, EndDate: periodEnd}
	cases = append(cases, parityCase{
		Name: "GetCategoryHierarchy",
		Seed: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
			f.seedTransaction(t, ctx, 75.00, now.AddDate(0, 0, -5), "Groceries", "Market", "FOOD_AND_DRINK", "FOOD_AND_DRINK_GROCERIES")
		},
		Call: func(t *testing.T, ctx context.Context, svc AnalyticsService) any {
			resp, err := svc.GetCategoryHierarchy(ctx, f.userID, chFilters, domain.CategoryDirectionExpense)
			require.NoError(t, err)
			return resp
		},
		Mutate: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
			f.seedTransaction(t, ctx, 40.00, now.AddDate(0, 0, -1), "Coffee", "Cafe", "FOOD_AND_DRINK", "FOOD_AND_DRINK_COFFEE")
		},
		Bump: bumpPersonal(f),
	})

	return cases
}

// bumpPersonal returns a Bump closure for f's personal (user) scope — the only scope kind this
// slice's table exercises; household scope is Task 11c/d.
func bumpPersonal(f *parityFixture) func(ctx context.Context, gens *cache.Generations) {
	return func(ctx context.Context, gens *cache.Generations) {
		gens.Bump(ctx, cache.ScopeUser, f.userID, "test")
	}
}

// ─── JSON-equality helpers ──────────────────────────────────────────────────────────────────

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err, "marshal %T for comparison", v)
	return string(b)
}

// ─── Metrics-delta assertions (Task 11a's cache.Snapshot/Delta) ────────────────────────────

// assertDelta asserts that namespace ns's delta shows exactly the given hits/misses, that its
// other result counters are all zero, and that no OTHER namespace absorbed a stray hit/miss —
// the latter check is what makes this a proof about THIS operation, not merely "some namespace
// got hit".
func assertDelta(t *testing.T, d cache.CountsSnapshot, ns cache.Namespace, wantHits, wantMisses float64, label string) {
	t.Helper()
	got := d[ns]
	if got.Hits != wantHits || got.Misses != wantMisses {
		t.Fatalf("%s: namespace %s delta = %+v; want hits=%v misses=%v", label, ns, got, wantHits, wantMisses)
	}
	if got.Errors != 0 || got.BypassDisabled != 0 || got.BypassGenError != 0 || got.BypassCircuit != 0 || got.BypassTombstoned != 0 {
		t.Fatalf("%s: namespace %s delta = %+v; expected no bypass/error results on the happy path", label, ns, got)
	}
	for otherNS, c := range d {
		if otherNS == ns {
			continue
		}
		if c != (cache.NamespaceCounts{}) {
			t.Fatalf("%s: unexpected non-zero delta on namespace %s: %+v (expected all traffic on %s)", label, otherNS, c, ns)
		}
	}
}

// ─── The shared harness body ────────────────────────────────────────────────────────────────

// runParityCase executes the full 7-step body from task-11-brief.md against one case. gens is
// nil for the cache-disabled and redis-down suites, which don't assert on Bump/metrics the same
// way (see runParityCase's ns/checkMetrics parameters).
//
// ns is the namespace THIS case's method actually lands in. Task 11c generalized this from a
// hardcoded cache.NamespaceAnalytics: a case landing in the wrong namespace (e.g. an
// analytics_rows method whose key builder regressed to NamespaceAnalytics) would otherwise
// silently pass the hit/miss-count assertions while proving nothing about the namespace it
// claims to cover — assertDelta's "every OTHER namespace must be all-zero" check only catches
// that if the caller told it which namespace SHOULD have moved.
func runParityCase(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	uncachedSvc, cachedSvc AnalyticsService, gens *cache.Generations,
	tc parityCase, ns cache.Namespace, checkMetrics bool,
) {
	tc.Seed(t, ctx, pool)

	// Step 2: ground truth.
	wantOff := tc.Call(t, ctx, uncachedSvc)

	// Step 3: cold read — must equal ground truth, and (cache enabled) exactly 1 miss / 0 hits.
	before := cache.Snapshot()
	gotCold := tc.Call(t, ctx, cachedSvc)
	afterCold := cache.Snapshot()
	assert.JSONEq(t, mustJSON(t, wantOff), mustJSON(t, gotCold), "%s: cold read must equal ground truth", tc.Name)
	if checkMetrics {
		assertDelta(t, afterCold.Delta(before), ns, 0, 1, tc.Name+": cold read")
	}

	// Step 4: warm read — must equal ground truth, and exactly 1 hit / 0 additional misses. This
	// is the assertion the whole gate rests on: without it, a broken key builder that mints a
	// fresh key every call would make every case pass by permanently missing.
	beforeWarm := cache.Snapshot()
	gotWarm := tc.Call(t, ctx, cachedSvc)
	afterWarm := cache.Snapshot()
	assert.JSONEq(t, mustJSON(t, wantOff), mustJSON(t, gotWarm), "%s: warm read must equal ground truth", tc.Name)
	if checkMetrics {
		assertDelta(t, afterWarm.Delta(beforeWarm), ns, 1, 0, tc.Name+": warm read")
	}

	// Step 5: mutate the underlying data, then bump the generation. The bump comes from tc.Bump —
	// a TEST-authored closure (bumpPersonal), NOT the production invalidation hook; see the Bump
	// field's doc comment. So steps 5-7 prove "if something bumps, the cached read reflects the
	// mutation", not "the production path bumps".
	tc.Mutate(t, ctx, pool)
	if gens != nil {
		tc.Bump(ctx, gens)
	}

	// Step 6: the mutation must have genuinely moved the number — guards against a no-op Mutate
	// making step 7 pass trivially.
	wantOff2 := tc.Call(t, ctx, uncachedSvc)
	if mustJSON(t, wantOff) == mustJSON(t, wantOff2) {
		t.Fatalf("%s: Mutate did not change the ground-truth result — this case's mutation does not "+
			"provably move this aggregate; the case is unable to test staleness honestly", tc.Name)
	}

	// Step 7: the stale-money assertion. After Bump, the cached read must reflect the mutation,
	// not the value warmed in step 4.
	gotAfter := tc.Call(t, ctx, cachedSvc)
	assert.JSONEq(t, mustJSON(t, wantOff2), mustJSON(t, gotAfter), "%s: post-mutation cached read served stale data", tc.Name)
}

// ─── Suite 1: cache enabled, real Redis ─────────────────────────────────────────────────────

func TestAnalyticsCacheParity(t *testing.T) {
	pool := connectIntegrationDB(t)
	redisClient := cachetest.ConnectTestRedis(t)
	c := cache.NewCache(cache.Options{Client: redisClient})
	ctx := context.Background()

	for _, buildCase := range parityCaseBuilders() {
		name, build := buildCase.name, buildCase.build
		t.Run(name, func(t *testing.T) {
			// Deliberately no t.Parallel(): cache.Snapshot/Delta reads a process-global counter
			// (Task 11a's doc comment on metrics_snapshot.go) and is only meaningful with no
			// concurrent cache traffic between the before/after snapshots.
			f := newParityFixture(t, ctx, pool)
			analyticsRepo := repository.NewAnalyticsRepository(pool)
			balanceRepo := repository.NewBalanceSnapshotRepository(pool)
			inner := NewAnalyticsService(analyticsRepo)
			inner.SetBalanceSnapshotRepo(balanceRepo)
			cachedSvc := NewCachedAnalyticsService(inner, c)

			tc := build(t, f, time.Now().UTC())
			runParityCase(t, ctx, pool, inner, cachedSvc, c.Generations(), tc, cache.NamespaceAnalytics, true)
		})
	}
}

// ─── Suite 2: cache disabled (EnvDisabledNamespaces) ────────────────────────────────────────

// countingRedisClient wraps a real redisclient.Client and counts every data-path call. It embeds
// the interface so unmodified methods (Name, RedactedAddr, Ping, PoolStats, Close, RunScript)
// pass straight through; only the methods cache.go's GetOrLoad/Generations can actually invoke are
// overridden and counted.
type countingRedisClient struct {
	redisclient.Client
	mu    sync.Mutex
	calls int
}

func (c *countingRedisClient) count() {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
}

func (c *countingRedisClient) Get(ctx context.Context, key string) (string, error) {
	c.count()
	return c.Client.Get(ctx, key)
}
func (c *countingRedisClient) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	c.count()
	return c.Client.Set(ctx, key, value, ttl)
}
func (c *countingRedisClient) SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	c.count()
	return c.Client.SetNX(ctx, key, value, ttl)
}
func (c *countingRedisClient) MGet(ctx context.Context, keys ...string) ([]string, []bool, error) {
	c.count()
	return c.Client.MGet(ctx, keys...)
}
func (c *countingRedisClient) Del(ctx context.Context, keys ...string) error {
	c.count()
	return c.Client.Del(ctx, keys...)
}
func (c *countingRedisClient) Scan(ctx context.Context, pattern string) ([]string, error) {
	c.count()
	return c.Client.Scan(ctx, pattern)
}

var _ redisclient.Client = (*countingRedisClient)(nil)

// TestAnalyticsCacheParity_CacheDisabled reuses the same table with NamespaceAnalytics disabled
// via EnvDisabledNamespaces (config.CacheNamespacesDisabled's parsed form — Task 8). Every result
// must still equal ground truth, and — since this cache is constructed with Control: nil, there is
// no Redis-hosted control-key poller running against this client at all in this harness, so "zero
// data-path commands" and "zero commands, full stop" coincide here — the spy asserts the latter,
// which is the stronger and still-honest claim for this configuration.
func TestAnalyticsCacheParity_CacheDisabled(t *testing.T) {
	pool := connectIntegrationDB(t)
	realClient := cachetest.ConnectTestRedis(t)
	spy := &countingRedisClient{Client: realClient}
	c := cache.NewCache(cache.Options{
		Client:                spy,
		EnvDisabledNamespaces: map[cache.Namespace]bool{cache.NamespaceAnalytics: true},
	})
	ctx := context.Background()

	for _, buildCase := range parityCaseBuilders() {
		name, build := buildCase.name, buildCase.build
		t.Run(name, func(t *testing.T) {
			f := newParityFixture(t, ctx, pool)
			analyticsRepo := repository.NewAnalyticsRepository(pool)
			balanceRepo := repository.NewBalanceSnapshotRepository(pool)
			inner := NewAnalyticsService(analyticsRepo)
			inner.SetBalanceSnapshotRepo(balanceRepo)
			cachedSvc := NewCachedAnalyticsService(inner, c)

			require.False(t, c.Enabled(cache.NamespaceAnalytics), "sanity: namespace must actually be disabled")

			tc := build(t, f, time.Now().UTC())

			// Simplified body for the disabled suite: no cold/warm/hit-miss semantics apply (every
			// call is a bypass), so just prove byte-identical results before and after Mutate, and
			// zero Redis commands throughout. inner is ground truth in both suites (there is
			// nothing to bump: a disabled namespace never wrote anything for Bump to invalidate).
			tc.Seed(t, ctx, pool)

			wantOff := tc.Call(t, ctx, inner)
			got := tc.Call(t, ctx, cachedSvc)
			assert.JSONEq(t, mustJSON(t, wantOff), mustJSON(t, got), "%s: disabled-cache read must equal ground truth", tc.Name)

			tc.Mutate(t, ctx, pool)
			wantAfterMutate := tc.Call(t, ctx, inner)
			if mustJSON(t, wantOff) == mustJSON(t, wantAfterMutate) {
				t.Fatalf("%s: Mutate did not change the ground-truth result", tc.Name)
			}
			gotAfterMutate := tc.Call(t, ctx, cachedSvc)
			assert.JSONEq(t, mustJSON(t, wantAfterMutate), mustJSON(t, gotAfterMutate), "%s: disabled-cache read after mutation must equal ground truth", tc.Name)

			spy.mu.Lock()
			calls := spy.calls
			spy.mu.Unlock()
			assert.Zero(t, calls, "%s: cache-disabled path must issue zero Redis commands", tc.Name)
		})
	}
}

// ─── Suite 3: Redis down (disposable container, stopped mid-suite) ─────────────────────────

// TestAnalyticsCacheParity_RedisDown reuses the same table against a disposable Redis container
// (never the shared cache-test instance — stopping that would break every other integration test
// sharing it) that is stopped before any case runs. Every case must still return the correct,
// uncached result with no error surfacing to the caller.
func TestAnalyticsCacheParity_RedisDown(t *testing.T) {
	pool := connectIntegrationDB(t)
	container := cachetest.StartRedisContainer(t)

	redisClient, err := redisclient.New(redisclient.Options{URL: container.URL, Name: "cache-test-down"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = redisClient.Close() })
	c := cache.NewCache(cache.Options{Client: redisClient})
	ctx := context.Background()

	// Confirm the client is live before killing it, so a stop failure can't masquerade as "it was
	// never up in the first place".
	require.NoError(t, redisClient.Ping(ctx), "sanity: container must be reachable before Stop")
	container.Stop(t)

	for _, buildCase := range parityCaseBuilders() {
		name, build := buildCase.name, buildCase.build
		t.Run(name, func(t *testing.T) {
			f := newParityFixture(t, ctx, pool)
			analyticsRepo := repository.NewAnalyticsRepository(pool)
			balanceRepo := repository.NewBalanceSnapshotRepository(pool)
			inner := NewAnalyticsService(analyticsRepo)
			inner.SetBalanceSnapshotRepo(balanceRepo)
			cachedSvc := NewCachedAnalyticsService(inner, c)

			tc := build(t, f, time.Now().UTC())
			tc.Seed(t, ctx, pool)

			wantOff := tc.Call(t, ctx, inner)
			gotDown := tc.Call(t, ctx, cachedSvc)
			assert.JSONEq(t, mustJSON(t, wantOff), mustJSON(t, gotDown), "%s: read with Redis down must equal ground truth with no error", tc.Name)
		})
	}
}

// ─── parityCaseBuilders: table construction deferred to per-subtest invocation ──────────────

// caseBuilder defers buildParityCases's per-case construction until inside each t.Run, so every
// subtest gets its own fixture/closure state (buildParityCases takes the fixture and "now" as
// arguments) while the table's SHAPE (names, and which grouping variants exist) is fixed once.
type caseBuilder struct {
	name  string
	build func(t *testing.T, f *parityFixture, now time.Time) parityCase
}

// parityCaseBuilders returns one caseBuilder per table row, by first building the full table
// against a throwaway fixture/time (to enumerate names, which don't depend on fixture identity)
// and then wrapping each entry so it can be rebuilt for-real inside each subtest.
func parityCaseBuilders() []caseBuilder {
	// Names are fixed by construction (buildParityCases never varies them by fixture/time), so a
	// nil fixture and zero time are safe here — only used to read off .Name per row.
	template := buildParityCasesTemplate()
	out := make([]caseBuilder, len(template))
	for i, name := range template {
		i, name := i, name
		out[i] = caseBuilder{
			name: name,
			build: func(t *testing.T, f *parityFixture, now time.Time) parityCase {
				cases := buildParityCases(t, f, now)
				return cases[i]
			},
		}
	}
	return out
}

// buildParityCasesTemplate returns just the ordered case names, matching buildParityCases's
// construction order exactly (asserted by TestAnalyticsCacheParityCaseNames_MatchTemplate below).
func buildParityCasesTemplate() []string {
	names := []string{"GetCashFlowSummary"}
	for _, g := range []string{"daily", "weekly", "monthly"} {
		names = append(names, "GetCashFlowHistory/"+g)
	}
	names = append(names,
		"GetSpendingByCategory",
		"GetSpendingByCategory/drilldown",
		"GetSpendingByMerchant",
		"GetSpendingTrends",
		"GetRecurringSubscriptions",
		"GetMonthOverMonthSpendingChange",
	)
	for _, g := range []string{"daily", "weekly", "monthly"} {
		names = append(names, "GetNetWorthTrend/"+g)
	}
	names = append(names,
		"GetBudgetVsActual",
		"GetSavingsRateForPeriod",
		"GetAverageMonthlySpend",
		"GetCategoryTotalForPeriod",
		"GetIncomeSourceBreakdown",
		"GetCategoryHierarchy",
	)
	return names
}

// TestAnalyticsCacheParityCaseNames_MatchTemplate guards buildParityCasesTemplate against drifting
// out of sync with buildParityCases's real construction order — parityCaseBuilders' indexing trick
// depends on the two lining up exactly.
func TestAnalyticsCacheParityCaseNames_MatchTemplate(t *testing.T) {
	pool := connectIntegrationDB(t)
	ctx := context.Background()
	f := newParityFixture(t, ctx, pool)
	real := buildParityCases(t, f, time.Now().UTC())

	template := buildParityCasesTemplate()
	require.Len(t, real, len(template), "buildParityCases row count must match the template")
	for i, tc := range real {
		assert.Equal(t, template[i], tc.Name, "row %d name mismatch", i)
	}
}
