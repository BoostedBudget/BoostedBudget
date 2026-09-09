//go:build integration

// Package services — Issue #1531 Task 11c: the household-scope cache invalidation suite.
//
// Every household-capable method in cachedAnalyticsService picks its cache Scope via
// cache.ScopeFor(userID, householdScope): non-nil household scope -> Scope{Kind: ScopeHousehold,
// ID: hh.HouseholdID}; nil -> Scope{Kind: ScopeUser, ID: userID} (cache/consumer.go). That single
// branch is what this file proves, from a real 2-member household, in both directions:
//
//  1. Member B's write invalidates member A's cached HOUSEHOLD-scoped result (the bump reached
//     the right key).
//  2. Member B's write does NOT invalidate member A's cached PERSONAL-scoped result (the bump did
//     NOT reach a key it has no business touching). This second assertion matters as much as the
//     first: a decorator that bumped every scope indiscriminately on every write would pass
//     assertion 1 and still be a real perf/correctness bug (a "household member changed
//     something" write would blow away every member's personal-scope cache too), so asserting the
//     ABSENCE of over-invalidation is not optional. It also confirms Mutate itself is honestly
//     household-only: personal ground truth (computed via the uncached service) must be byte-
//     identical before and after B's write, since B's transaction lives on B's own account and
//     personal-scope queries filter on t.user_id = A directly (no join to accounts/household
//     tables at all) — if the personal ground truth had ALSO changed, the "no over-invalidation"
//     assertion below would be vacuous (there'd be nothing for it to over-invalidate).
//
// Scope decision (documented per the task 11c brief's bar for judgment calls): this suite covers
// 3 representative methods, not all ~18 household-capable rows in the `analytics`/`analytics_rows`
// 20+4-row tables (#1626 added GetCategoryHierarchy, a cached household-capable analytics read;
// the counts here are descriptive prose, not enforced, so they were re-derived by hand):
//   - GetSpendingByCategory   (domain.SpendingFilters.HouseholdScope — the most common wiring
//     shape, shared by 8 of the 19 `analytics` rows)
//   - GetCashFlowSummary      (domain.CashFlowFilters.HouseholdScope — the same wiring shape
//     against a different filter struct/repository query)
//   - GetSavingsRateForPeriod (an explicit *domain.HouseholdScope parameter, not filters-embedded
//     — the OTHER wiring shape, shared by GetMonthOverMonthSpendingChange/GetNetWorthTrend)
//
// The property under test — does cache.ScopeFor + Generations.Bump(ScopeHousehold, ...) correctly
// isolate household-scoped keys from user-scoped keys — is identical regardless of which
// analytics method sits behind cache.NewKey; 11b's per-method Mutate assertions already proved
// each method's REPOSITORY query moves its own aggregate correctly. Replicating that per-method
// proof a 4th time (analytics, analytics_rows, cache-disabled, redis-down, now household, times
// ~17 rows) would multiply fixture complexity for zero incremental coverage of the actual
// decorator risk. GetNetWorthTrend was deliberately excluded from even this representative set: its
// household path fans out through BalanceSnapshotRepository.GetAggregatedHistoryForHousehold, which
// needs balance_snapshots.user_account_id wired through a THIRD household CTE variant
// (householdAccountsCTEForBalancesPerDay) — real plumbing, but it does not exercise any DIFFERENT
// cache-scope logic than the three methods already covered here.
//
// Run:
//
//	DATABASE_URL=postgres://... CACHE_REDIS_TEST_URL=redis://localhost:6397/0 \
//	  go test ./internal/services/... -tags integration -race -p 1 -run TestAnalyticsCacheParity_HouseholdScope -v
package services

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/cache/cachetest"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// householdMember is a parityFixture-equivalent for household-scope tests: it additionally seeds
// a user_accounts row and points its `accounts` row at it via user_account_id, which
// household_grants (analytics_repository.go's householdGrantsCTE) gates on. Personal-scope
// queries are untouched by this — they still filter on transactions.user_id directly.
type householdMember struct {
	pool          *pgxpool.Pool
	userID        string
	linkedItemID  string
	userAccountID string
	accountID     string
}

func newHouseholdMember(t *testing.T, ctx context.Context, pool *pgxpool.Pool, label string) *householdMember {
	t.Helper()
	userID := uuid.New().String()
	linkedItemID := uuid.New().String()
	userAccountID := uuid.New().String()
	accountID := uuid.New().String()

	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'household_2', 'active', 'iad')
	`, userID, "int-hh-"+label+"|"+userID, "int-hh-"+label+"-"+userID+"@example.com")
	require.NoError(t, err, "seed %s user", label)

	_, err = pool.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, linkedItemID, userID, "hh-item-"+linkedItemID)
	require.NoError(t, err, "seed %s linked_item", label)

	_, err = pool.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id, institution_id_blind_idx)
		VALUES ($1, $2, 'hh-inst', $3)
	`, userAccountID, userID, "hh-inst-blind-"+userAccountID)
	require.NoError(t, err, "seed %s user_account", label)

	_, err = pool.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, user_account_id)
		VALUES ($1, $2, $3, $4, $5, 'depository', 'active', $6)
	`, accountID, linkedItemID, userID, "plaid-hh-acc-"+accountID, "Household Test Checking "+label, userAccountID)
	require.NoError(t, err, "seed %s account", label)

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID) //nolint:errcheck
	})

	return &householdMember{pool: pool, userID: userID, linkedItemID: linkedItemID, userAccountID: userAccountID, accountID: accountID}
}

func (m *householdMember) seedTransaction(t *testing.T, ctx context.Context, amount float64, date time.Time, name, merchantName, pfcPrimary, pfcDetailed string) {
	t.Helper()
	id := uuid.New().String()
	_, err := m.pool.Exec(ctx, `
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
	`, id, m.userID, m.accountID, m.linkedItemID, "ext-"+id, "blind-"+id,
		amount, date.Format("2006-01-02"), name, merchantName, pfcPrimary, pfcDetailed)
	require.NoError(t, err, "seed transaction %q", name)
}

// householdPair bundles two members and the household both share an account into. shareStartedAt
// is fixed comfortably before any test's date window so GREATEST($4::date,
// share_started_at::date) in the household repo queries never clips a seeded transaction.
type householdPair struct {
	pool        *pgxpool.Pool
	householdID string
	a, b        *householdMember
	scope       *domain.HouseholdScope
}

func newHouseholdPair(t *testing.T, ctx context.Context, pool *pgxpool.Pool) *householdPair {
	t.Helper()
	a := newHouseholdMember(t, ctx, pool, "a")
	b := newHouseholdMember(t, ctx, pool, "b")

	householdID := uuid.New().String()
	_, err := pool.Exec(ctx, `
		INSERT INTO households (id, owner_user_id, name) VALUES ($1, $2, $3)
	`, householdID, a.userID, "Parity Household "+householdID)
	require.NoError(t, err, "seed household")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM households WHERE id = $1`, householdID) //nolint:errcheck
	})

	shareStartedAt := time.Now().UTC().AddDate(0, 0, -100)
	for _, m := range []*householdMember{a, b} {
		_, err := pool.Exec(ctx, `
			INSERT INTO household_account_shares (household_id, user_account_id, visibility, shared_at)
			VALUES ($1, $2, 'shared', $3)
		`, householdID, m.userAccountID, shareStartedAt)
		require.NoError(t, err, "share %s's account into the household", m.userID)
	}

	return &householdPair{
		pool:        pool,
		householdID: householdID,
		a:           a,
		b:           b,
		scope:       &domain.HouseholdScope{HouseholdID: householdID, MemberUserIDs: []string{a.userID, b.userID}},
	}
}

// assertHouseholdInvalidationAndIsolation is the shared 11-step body every case below runs:
// warm both a household-scoped and a personal-scoped read for member A, have member B write
// (mutateB), bump ONLY the household scope, then assert the household read was invalidated while
// the personal read was not.
func assertHouseholdInvalidationAndIsolation(
	t *testing.T, ctx context.Context, hh *householdPair,
	inner, cachedSvc AnalyticsService, gens *cache.Generations,
	callHousehold, callPersonal func(svc AnalyticsService) any,
	mutateB func(),
) {
	t.Helper()

	// Ground truth, both scopes, before B writes anything.
	wantHHBefore := callHousehold(inner)
	wantPersonalBefore := callPersonal(inner)

	// Warm the household-scoped cache entry for A.
	gotHHCold := callHousehold(cachedSvc)
	assert.JSONEq(t, mustJSON(t, wantHHBefore), mustJSON(t, gotHHCold), "household cold read must equal ground truth")
	gotHHWarm := callHousehold(cachedSvc)
	assert.JSONEq(t, mustJSON(t, wantHHBefore), mustJSON(t, gotHHWarm), "household warm read must equal ground truth")

	// Warm the personal-scoped cache entry for A.
	gotPersonalCold := callPersonal(cachedSvc)
	assert.JSONEq(t, mustJSON(t, wantPersonalBefore), mustJSON(t, gotPersonalCold), "personal cold read must equal ground truth")
	gotPersonalWarm := callPersonal(cachedSvc)
	assert.JSONEq(t, mustJSON(t, wantPersonalBefore), mustJSON(t, gotPersonalWarm), "personal warm read must equal ground truth")

	// Member B writes, then ONLY the household scope is bumped — mirroring what a real household
	// write would trigger (cache/consumer.go's ScopeFor picks ScopeHousehold for household-scoped
	// reads; the middleware bumps whichever scope(s) the ACTING user's writes affect, and B's own
	// personal-scope generation is never touched by this test on purpose — the point is to prove
	// A's cache isolation, not exercise B's).
	mutateB()
	gens.Bump(ctx, cache.ScopeHousehold, hh.householdID, "test")

	// Ground truth after B's write: household must have moved, personal must NOT have (sanity
	// check — B's transaction lives on B's own account; a personal-scope query for A filters on
	// t.user_id = A directly, with no join to accounts/household tables at all).
	wantHHAfter := callHousehold(inner)
	wantPersonalAfter := callPersonal(inner)
	if mustJSON(t, wantHHBefore) == mustJSON(t, wantHHAfter) {
		t.Fatal("member B's write did not move the household ground truth — this case cannot honestly test invalidation")
	}
	assert.JSONEq(t, mustJSON(t, wantPersonalBefore), mustJSON(t, wantPersonalAfter),
		"sanity: member B's write must NOT move member A's personal ground truth — otherwise the "+
			"no-over-invalidation assertion below would be vacuous")

	// Assertion 1: the household-scoped cached read for A must reflect B's write (invalidated).
	gotHHAfter := callHousehold(cachedSvc)
	assert.JSONEq(t, mustJSON(t, wantHHAfter), mustJSON(t, gotHHAfter),
		"household-scoped cached read must be invalidated by member B's write")

	// Assertion 2 — the one that matters as much as #1: the personal-scoped cached read for A
	// must NOT have been invalidated by the household bump. A decorator that bumped every scope
	// on every write would pass assertion 1 and still be wrong; this is what catches that.
	gotPersonalAfter := callPersonal(cachedSvc)
	assert.JSONEq(t, mustJSON(t, wantPersonalBefore), mustJSON(t, gotPersonalAfter),
		"personal-scoped cached read must NOT be invalidated by a household-scope bump (over-invalidation)")
}

// TestAnalyticsCacheParity_HouseholdScope runs the shared body above against 3 representative
// household-capable methods (see this file's doc comment for why these 3 and not all ~17).
func TestAnalyticsCacheParity_HouseholdScope(t *testing.T) {
	pool := connectIntegrationDB(t)
	redisClient := cachetest.ConnectTestRedis(t)
	ctx := context.Background()

	periodStart := time.Now().UTC().AddDate(0, 0, -14)
	periodEnd := time.Now().UTC().Add(time.Hour)

	t.Run("GetSpendingByCategory", func(t *testing.T) {
		c := cache.NewCache(cache.Options{Client: redisClient})
		hh := newHouseholdPair(t, ctx, pool)
		analyticsRepo := repository.NewAnalyticsRepository(pool)
		inner := NewAnalyticsService(analyticsRepo)
		cachedSvc := NewCachedAnalyticsService(inner, c)

		hh.a.seedTransaction(t, ctx, 30.00, time.Now().UTC().AddDate(0, 0, -4), "Groceries", "Grocer", "FOOD_AND_DRINK", "FOOD_AND_DRINK_GROCERIES")

		hhFilters := domain.SpendingFilters{StartDate: periodStart, EndDate: periodEnd, HouseholdScope: hh.scope}
		personalFilters := domain.SpendingFilters{StartDate: periodStart, EndDate: periodEnd}

		callHousehold := func(svc AnalyticsService) any {
			resp, err := svc.GetSpendingByCategory(ctx, hh.a.userID, hhFilters)
			require.NoError(t, err)
			return resp
		}
		callPersonal := func(svc AnalyticsService) any {
			resp, err := svc.GetSpendingByCategory(ctx, hh.a.userID, personalFilters)
			require.NoError(t, err)
			return resp
		}
		mutateB := func() {
			hh.b.seedTransaction(t, ctx, 45.00, time.Now().UTC().AddDate(0, 0, -1), "Dinner", "Restaurant", "FOOD_AND_DRINK", "FOOD_AND_DRINK_RESTAURANT")
		}

		assertHouseholdInvalidationAndIsolation(t, ctx, hh, inner, cachedSvc, c.Generations(), callHousehold, callPersonal, mutateB)
	})

	t.Run("GetCashFlowSummary", func(t *testing.T) {
		c := cache.NewCache(cache.Options{Client: redisClient})
		hh := newHouseholdPair(t, ctx, pool)
		analyticsRepo := repository.NewAnalyticsRepository(pool)
		inner := NewAnalyticsService(analyticsRepo)
		cachedSvc := NewCachedAnalyticsService(inner, c)

		hh.a.seedTransaction(t, ctx, 20.00, time.Now().UTC().AddDate(0, 0, -3), "Groceries", "Local Grocer", "FOOD_AND_DRINK", "FOOD_AND_DRINK_GROCERIES")

		hhFilters := domain.CashFlowFilters{StartDate: periodStart, EndDate: periodEnd, HouseholdScope: hh.scope}
		personalFilters := domain.CashFlowFilters{StartDate: periodStart, EndDate: periodEnd}

		callHousehold := func(svc AnalyticsService) any {
			resp, err := svc.GetCashFlowSummary(ctx, hh.a.userID, hhFilters)
			require.NoError(t, err)
			return resp
		}
		callPersonal := func(svc AnalyticsService) any {
			resp, err := svc.GetCashFlowSummary(ctx, hh.a.userID, personalFilters)
			require.NoError(t, err)
			return resp
		}
		mutateB := func() {
			hh.b.seedTransaction(t, ctx, 55.00, time.Now().UTC().AddDate(0, 0, -2), "Electronics", "Gadget Store", "GENERAL_MERCHANDISE", "GENERAL_MERCHANDISE_OTHER")
		}

		assertHouseholdInvalidationAndIsolation(t, ctx, hh, inner, cachedSvc, c.Generations(), callHousehold, callPersonal, mutateB)
	})

	t.Run("GetSavingsRateForPeriod", func(t *testing.T) {
		c := cache.NewCache(cache.Options{Client: redisClient})
		hh := newHouseholdPair(t, ctx, pool)
		analyticsRepo := repository.NewAnalyticsRepository(pool)
		inner := NewAnalyticsService(analyticsRepo)
		cachedSvc := NewCachedAnalyticsService(inner, c)

		// Income only from A, so Income > 0 and SavingsRate is well-defined (non-nil) from the
		// start — B's later expense-only Mutate then moves Expenses/NetSavings/SavingsRate
		// without touching Income.
		hh.a.seedTransaction(t, ctx, -1000.00, time.Now().UTC().AddDate(0, 0, -10), "Payroll", "Employer Inc", "INCOME", "INCOME_WAGES")

		callHousehold := func(svc AnalyticsService) any {
			resp, err := svc.GetSavingsRateForPeriod(ctx, hh.a.userID, periodStart, periodEnd, hh.scope)
			require.NoError(t, err)
			return resp
		}
		callPersonal := func(svc AnalyticsService) any {
			resp, err := svc.GetSavingsRateForPeriod(ctx, hh.a.userID, periodStart, periodEnd, nil)
			require.NoError(t, err)
			return resp
		}
		mutateB := func() {
			hh.b.seedTransaction(t, ctx, 200.00, time.Now().UTC().AddDate(0, 0, -2), "Rent", "Landlord", "RENT_AND_UTILITIES", "RENT_AND_UTILITIES_RENT")
		}

		assertHouseholdInvalidationAndIsolation(t, ctx, hh, inner, cachedSvc, c.Generations(), callHousehold, callPersonal, mutateB)
	})
}

// TestAnalyticsCacheParity_HouseholdScope_CrossMemberRequesterIsolation is Issue #1531 H1's
// regression test. Every case in TestAnalyticsCacheParity_HouseholdScope above issues BOTH the
// cold and the warm read as the SAME member (A) — member B only ever writes, never reads — which
// structurally cannot exercise the bug: cache.ScopeFor drops the requesting userID entirely
// (internal/cache/consumer.go), but getSpendingByMerchantHousehold's SQL binds $1 = the
// REQUESTING user into merchantSortJoins' merchant_name_overrides joins
// (internal/repository/merchant_sql_helpers.go), which drive both the merchant display name AND
// the GROUP BY. Two DIFFERENT members reading the identical household query within the same TTL
// window must NOT collapse onto one cache entry: whichever member reads second must see their OWN
// merchant override, not the first reader's.
//
// This must fail before the fix: before H1, EncodeHouseholdScope hashes only household ID +
// member IDs (identical for A and B), so B's read is a cache HIT on the entry A's read populated,
// and B is served A's private merchant label.
func TestAnalyticsCacheParity_HouseholdScope_CrossMemberRequesterIsolation(t *testing.T) {
	pool := connectIntegrationDB(t)
	redisClient := cachetest.ConnectTestRedis(t)
	ctx := context.Background()

	c := cache.NewCache(cache.Options{Client: redisClient})
	hh := newHouseholdPair(t, ctx, pool)
	analyticsRepo := repository.NewAnalyticsRepository(pool)
	inner := NewAnalyticsService(analyticsRepo)
	cachedSvc := NewCachedAnalyticsService(inner, c)

	periodStart := time.Now().UTC().AddDate(0, 0, -14)
	periodEnd := time.Now().UTC().Add(time.Hour)

	// One merchant, shared by a transaction from EACH member, so the household query's GROUP BY
	// collapses both members' spend into a single row — isolating the assertion to the merchant
	// NAME (which the requester-dependent override join resolves) rather than the total (which is
	// requester-independent and would move for an unrelated reason).
	merchantID := uuid.New().String()
	merchantName := "Shared Merchant Co " + merchantID
	_, err := pool.Exec(ctx, `
		INSERT INTO merchants (id, name, normalized_name, created_at)
		VALUES ($1, $2, $3, now())
	`, merchantID, merchantName, merchantName)
	require.NoError(t, err, "seed shared merchant")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM merchants WHERE id = $1`, merchantID) //nolint:errcheck
	})

	seedMerchantTx := func(m *householdMember, amount float64) {
		id := uuid.New().String()
		_, err := pool.Exec(ctx, `
			INSERT INTO transactions (
				id, user_id, account_id, linked_item_id, source,
				external_transaction_id, external_transaction_id_blind_idx,
				amount, date, name, merchant_name, merchant_id, pfc_primary, pfc_detailed,
				effective_category_name, pending, created_at, updated_at
			) VALUES (
				$1, $2, $3, $4, 'plaid', $5, $6,
				$7, $8::date, $9, $10, $11, $12, $13, $12, false, now(), now()
			)
		`, id, m.userID, m.accountID, m.linkedItemID, "ext-"+id, "blind-"+id,
			amount, time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02"),
			"Shared Purchase", "Shared Merchant Co", merchantID, "FOOD_AND_DRINK", "FOOD_AND_DRINK_GROCERIES")
		require.NoError(t, err, "seed merchant transaction for %s", m.userID)
	}
	seedMerchantTx(hh.a, 40.00)
	seedMerchantTx(hh.b, 60.00)

	// Each member privately renames the shared merchant to something only THEY see.
	insertOverride := func(userID, displayName string) {
		_, err := pool.Exec(ctx, `
			INSERT INTO merchant_name_overrides (id, user_id, merchant_id, display_name, created_at, updated_at)
			VALUES ($1, $2, $3, $4, now(), now())
		`, uuid.New().String(), userID, merchantID, displayName)
		require.NoError(t, err, "insert merchant override for %s", userID)
	}
	insertOverride(hh.a.userID, "A's Private Label")
	insertOverride(hh.b.userID, "B's Private Label")

	hhFilters := domain.SpendingFilters{StartDate: periodStart, EndDate: periodEnd, HouseholdScope: hh.scope}

	// Member A reads first (cold): populates the cache entry the current key scheme uses.
	beforeA := cache.Snapshot()
	respA, err := cachedSvc.GetSpendingByMerchant(ctx, hh.a.userID, hhFilters, 10)
	require.NoError(t, err)
	afterA := cache.Snapshot()
	assertDelta(t, afterA.Delta(beforeA), cache.NamespaceAnalytics, 0, 1, "A's cold read")
	require.Len(t, respA.Merchants, 1, "expected the two members' transactions to collapse into one merchant row")
	assert.Equal(t, "A's Private Label", respA.Merchants[0].MerchantName,
		"member A must see A's own override on A's own (cold) read")

	// Member B reads the SAME household query immediately after, well within the namespace TTL.
	// This must be resolved against B's OWN override — not A's cached entry. Requester is part of
	// the cache key (Issue #1531 H1), so this is necessarily a cold miss for B's OWN key, not a
	// hit on A's entry.
	beforeB1 := cache.Snapshot()
	respB, err := cachedSvc.GetSpendingByMerchant(ctx, hh.b.userID, hhFilters, 10)
	require.NoError(t, err)
	afterB1 := cache.Snapshot()
	assertDelta(t, afterB1.Delta(beforeB1), cache.NamespaceAnalytics, 0, 1, "B's cold read")
	require.Len(t, respB.Merchants, 1, "expected the two members' transactions to collapse into one merchant row")
	assert.Equal(t, "B's Private Label", respB.Merchants[0].MerchantName,
		"member B must see B's own override, not member A's cached override — a household-scoped "+
			"cache key that omits the requester lets B's read collapse onto A's cache entry and "+
			"serves B another household member's private merchant rename (Issue #1531 H1)")

	// Issue #1531 GA-8: the assertions above prove A and B get DIFFERENT results, but not that
	// either read was actually SERVED FROM CACHE — a bypassed or permanently-failing cache would
	// pass every assertion above vacuously (every read would just hit Postgres directly and
	// naturally see its own override, with cache.ScopeFor/EncodeHouseholdScope never in the loop
	// at all). Read B's SAME query again, within the same TTL window, and assert this second read
	// is a genuine cache HIT (not a miss/bypass) that still serves B's own isolated entry — proving
	// B's read path actually goes through the cache, the same way the parity harness above verifies
	// warm reads via assertDelta.
	beforeB2 := cache.Snapshot()
	respB2, err := cachedSvc.GetSpendingByMerchant(ctx, hh.b.userID, hhFilters, 10)
	require.NoError(t, err)
	afterB2 := cache.Snapshot()
	assertDelta(t, afterB2.Delta(beforeB2), cache.NamespaceAnalytics, 1, 0, "B's warm (cache-hit) read")
	require.Len(t, respB2.Merchants, 1, "expected the two members' transactions to collapse into one merchant row")
	assert.Equal(t, "B's Private Label", respB2.Merchants[0].MerchantName,
		"member B's warm, cache-served read must still return B's own isolated entry, not A's")
}
