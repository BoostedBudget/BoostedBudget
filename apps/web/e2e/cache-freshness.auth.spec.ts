import { test, expect } from '@playwright/test'
import { acceptCookieConsent } from './helpers/auth'
import { apiClient } from './helpers/api/client'
import { captureNavigationJson } from './helpers/capture-navigation-response'

// Issue #1531 Task 11e: proves the Redis analytics cache serves FRESH data after a real,
// application-level write, end-to-end through the real running frontend + backend (not the Go
// integration harness Tasks 11b-d already cover at the service layer — this exercises the actual
// HTTP request path: Clerk auth, the cache-invalidation middleware, and a real browser's network
// stack).
//
// ⚠ GUARD FIRST (see assertCacheEnabledOrFail below). Without it every assertion below would pass
// against a cache-disabled backend and prove nothing about caching at all — see the brief's own
// warning. The public /health endpoint deliberately reports only `{status}` now (Task 6's security
// fix removed the component breakdown), so this reads the admin-gated
// /api/v1/admin/system/health endpoint instead, whose `cache_enabled` field this task added
// specifically so this guard has something real to check (see
// backend/internal/domain/system_health.go's CacheEnabled doc comment).
//
// SUBSTITUTE-PATH DOCUMENTATION (deliberate, not an implementation shortcut — mirrors
// household-share-exclusion.auth.spec.ts's own precedent for handling a brief written against a
// UI/API surface this codebase doesn't actually have): the brief asks for "adding a transaction
// moves dashboard totals" and "deleting it reverts the total". There is NO transaction-creation or
// transaction-deletion endpoint anywhere in this API — transactions arrive exclusively via Plaid
// sync, and there is no Plaid Sandbox Link E2E automation in this suite (confirmed precedent:
// household-share-exclusion.auth.spec.ts's own header). Fabricating one for this task would be
// out of scope and would not even exercise the real ingestion path.
//
// What IS real and reachable: PUT /api/v1/transactions/{id}/category (UpdateTransactionCategory).
// This spec moves a real, pre-seeded transaction ("Chipotle Mexican Grill", $45.00,
// FOOD_AND_DRINK/FOOD_AND_DRINK_RESTAURANTS — see apps/web/scripts/seed-e2e-data.sql) from its
// budgeted expense category into TRANSFER_OUT. The analytics repository EXCLUDES every
// `TRANSFER%` PFC primary from spending/cash-flow aggregation (see
// backend/internal/repository/analytics_repository.go's `effectiveTransferExpr()... NOT LIKE
// 'TRANSFER%'` guards) — so this single real write moves BOTH:
//   - GetSpendingByCategory's per-category total AND overall TotalSpent (the reports page) — the
//     literal "editing category moves spending-by-category" requirement.
//   - GetCashFlowSummary's Expenses total (the dashboard's Cash Flow Summary widget,
//     includeTransfers: false) — the "dashboard totals move" requirement's REAL analog: a
//     transaction that stops counting as an expense once its category no longer counts as one.
// The transaction is real, the write is real, the HTTP path is real, and the invalidation is the
// SAME blanket cache_invalidation.go middleware every other mutating endpoint goes through — this
// is not a weaker proof than "add/delete a transaction" would have been, it is the closest
// available REAL substitute. Reverting the category back to its original value at the end
// substitutes for "deleting it reverts the total" — the same class of proof (a second real,
// cache-invalidating write is proven to restore the original numbers immediately), via edit-revert
// rather than delete, again because delete does not exist as an endpoint.
//
// CROSS-ENDPOINT CONSISTENCY: both moved endpoints (GetCashFlowSummary, GetSpendingByCategory) sit
// under the SAME cache.NamespaceAnalytics generation, so this cannot detect a bump that reaches
// one NAMESPACE but not another (Task 11b's Go harness already covers per-namespace correctness at
// the unit level) — what this DOES catch, honestly, is a bump that is silently scoped to the wrong
// CACHE KEY within that shared generation (e.g. a key-builder bug that makes one of the two
// methods miss the invalidation while the other correctly reflects it), by asserting BOTH move by
// the exact same dollar amount from the SAME single write.

// #1598. Every in-spec wait scales with the runner, from ONE constant.
//
// This spec's waits were calibrated on the Mac Mini pool. When the job moved to a 2-vCPU
// Ubicloud runner (#1598) they became marginal, and fixing them one at a time made the binding
// constraint hop: raising the whole-test budget from 120s just moved the failure to a 15s
// `waitForResponse`, which is what failed on run 33669941623 (the run also took 27m against a
// 17.4m baseline and produced 21 flakes across 16 unrelated specs, so the runner really is
// slower, not just this spec).
//
// A single multiplier keeps them from drifting apart again. It is NOT a licence to out-wait a
// real hang: the whole-test budget below still bounds the spec, and every wait here is for one
// specific network response or element, never a bare sleep.
const CI_WAIT_FACTOR = process.env.CI ? 3 : 1
const WAIT_RESPONSE_MS = 15_000 * CI_WAIT_FACTOR
const WAIT_ELEMENT_MS = 10_000 * CI_WAIT_FACTOR
// The already-persisted-widget probe. Missing it is not fatal but is EXPENSIVE: the spec falls
// through to the six-interaction Add-Widget dialog flow, which is what consumed the whole-test
// budget on run 33660693375. Waiting longer here is strictly cheaper than falling through.
const WAIT_WIDGET_MS = 5_000 * CI_WAIT_FACTOR
// Whole-test budget. ⚠ Deliberately NOT scaled by CI_WAIT_FACTOR — see the retraction below.
//
// ⚠ RETRACTION (run 33678171305). I first set this to 120_000 * CI_WAIT_FACTOR = 360_000, on the
// stated grounds that 120s "was calibrated on the Mac Mini and is not enough on the 2-vCPU
// runner". THAT WAS WRONG, and measurement refutes it: on the green run (33673717843) this test
// completed in 39.9s on that exact runner type, so 120s was already 3x the headroom it needs.
//
// The test does not need a bigger budget; it intermittently HANGS. A budget does not fix a hang,
// it only prices one — and tripling it tripled the price. One hung attempt burned 6.2 minutes
// instead of ~2.
//
// ⚠ Scale honestly: this describe block sets `retries: 0` (below), so that is 6.2 minutes ONCE,
// not x3. My first draft of this comment said "three attempts ~18.6 minutes, which blew the job's
// 45-minute cap" — both halves were wrong. Measuring the run instead: 39.5 minutes of test time
// total, with a cluster of ~31s durations across boosts-table, budget-hierarchy, url-sort,
// dashboard, transaction-filtering and savings — i.e. dozens of UNRELATED tests hitting the
// global 30s per-test timeout and retrying under `retries: 2`. That starvation blew the cap; this
// budget added about four minutes to it. Fixing this is still right (360s was 9x the measured
// healthy 39.9s) but it is not the headline.
//
// The budget is a HANG DETECTOR, not a slowness allowance. Slowness is absorbed by the
// per-action waits above, which are individually bounded; this only has to exceed the sum of the
// legitimate ones. 180_000 is 4.5x the measured healthy duration and leaves room for a couple of
// worst-case 45s responses, while capping a hung attempt at 3 minutes rather than 6.
const TEST_BUDGET_MS = process.env.CI ? 180_000 : 120_000

const CHIPOTLE_AMOUNT = 45.0
const ORIGINAL_PFC_PRIMARY = 'FOOD_AND_DRINK'
const ORIGINAL_PFC_DETAILED = 'FOOD_AND_DRINK_RESTAURANTS'
const MOVED_PFC_PRIMARY = 'TRANSFER_OUT'
const MOVED_PFC_DETAILED = 'TRANSFER_OUT_SAVINGS'

interface CashFlowSummaryResponse {
  expenses: number
  income: number
}

interface SpendingByCategoryResponse {
  total_spent: number
  categories: Array<{ category: string; total: number }>
}

test.describe('Cache freshness after a real transaction-category write (#1531)', () => {
  test.describe.configure({ retries: 0 })

  test('editing a transaction category immediately moves cash-flow expenses and spending-by-category, consistently, and reverting restores both', async ({
    page,
  }, testInfo) => {
    // Whole-test budget. This spec is unusually navigation-heavy for one test: six full page
    // loads (three goto + three reload, deliberately never SPA soft-navs — see
    // captureCashFlowSummary's header comment) against a real backend, plus a PUT and a revert.
    //
    // 120_000 was calibrated on the Mac Mini runner and is NOT enough on the 2-vCPU Ubicloud
    // runner this job moved to in #1598: the test ran 2.1m there and died mid-`locator.click`
    // with "Test timeout of 120000ms exceeded", twice out of two runs, while the same commit
    // passed on mac. The budget was the binding constraint, not any single action — even the
    // finally-block's revert PUT inherited the already-expired deadline and logged
    // "category cleanup failed (non-fatal)", leaving the fixture moved.
    //
    // Locally the whole spec file completes in ~49s, so 300_000 on CI is roughly 2x the observed
    // CI cost rather than an arbitrary round number. Kept at 120_000 locally so a genuine hang
    // still surfaces quickly on a dev machine.
    testInfo.setTimeout(TEST_BUDGET_MS)

    // ⚠ The actual reason a hang could cost 6 minutes: Playwright's default actionTimeout is 0,
    // meaning NO per-action limit, so a single `.click()` or `.waitFor()` blocks until the
    // whole-test budget expires. That is why run 33678171305 shows nothing but a 30s
    // notification poll for six minutes — the page was alive and the test was simply waiting,
    // unbounded, with no error to point at.
    //
    // Bounding it per action does two things a budget cannot: it caps what one hang costs, and it
    // fails with a message naming the locator that never resolved instead of an anonymous
    // whole-test timeout. Set here rather than in playwright.config.ts because config is shared
    // by every spec and this is the one with a known intermittent hang; widening it globally is a
    // separate decision.
    page.setDefaultTimeout(WAIT_ELEMENT_MS)

    // A page.goto is required BEFORE any apiClient() call: apiClient reads the auth token from
    // window.Clerk.session in the browser context (helpers/api/client.ts), which doesn't exist
    // until a page has actually loaded the app — a bare storageState with no navigation yet
    // produces a token-less (401) request. Rehydrating Clerk's session from the saved
    // storageState (rather than a live sign-in, as auth.setup.ts does) takes a moment longer than
    // domcontentloaded — mirror auth.setup.ts's own explicit wait for window.Clerk.session.id
    // rather than relying on apiClient's retry loop alone to out-wait it.
    await page.goto('/dashboard')
    await page.waitForLoadState('domcontentloaded')
    await acceptCookieConsent(page)
    try {
      await page.waitForFunction(
        () => {
          const clerk = (window as unknown as { Clerk?: { session?: { id?: string } } }).Clerk
          return clerk?.session?.id != null
        },
        { timeout: WAIT_RESPONSE_MS }
      )
    } catch {
      // Fall through to apiClient's own retry loop, which will surface a clear failure below if
      // the session genuinely never hydrates.
    }
    const client = await apiClient(page)

    // =========================================================================
    // GUARD — see file header. A non-200 or cache_enabled !== true here means the rest of this
    // spec would prove nothing, so this fails loudly rather than skipping quietly.
    // =========================================================================
    const healthResponse = await client.get('/api/v1/admin/system/health')
    expect(
      healthResponse.ok(),
      `GET /api/v1/admin/system/health failed (${healthResponse.status()}): ` +
        `${await healthResponse.text()}. The E2E user must be an admin (see ` +
        `config/admin_emails.yaml's non_production_admin_emails) with ENVIRONMENT=qa for this ` +
        `guard to run at all.`
    ).toBe(true)
    const health: { cache_enabled: boolean } = await healthResponse.json()
    expect(
      health.cache_enabled,
      'Cache is reported disabled on /api/v1/admin/system/health — every assertion below would ' +
        'pass against a cache-disabled backend and prove nothing. Not running.'
    ).toBe(true)

    // =========================================================================
    // ARRANGE — find the real seeded transaction and confirm its starting category, so the
    // "moved by exactly $45" assertions below aren't quietly comparing against the wrong baseline.
    // =========================================================================
    const txnResponse = await client.get('/api/v1/transactions?search=Chipotle&limit=10')
    expect(txnResponse.ok(), `GET transactions search failed: ${await txnResponse.text()}`).toBe(
      true
    )
    const txnBody: {
      transactions: Array<{ id: string; amount: number; pfc_primary?: string; date: string }>
    } = await txnResponse.json()
    const chipotle = txnBody.transactions.find((t) => t.amount === CHIPOTLE_AMOUNT)
    expect(
      chipotle,
      'Seeded "Chipotle Mexican Grill" ($45.00) transaction not found — run ' +
        'apps/web/scripts/seed-e2e-data.sql against the E2E database first.'
    ).toBeTruthy()
    const transactionId = chipotle!.id
    expect(chipotle!.pfc_primary).toBe(ORIGINAL_PFC_PRIMARY)

    // =========================================================================
    // ⚠ SHARED FIXTURE ROW. cache-household-visibility.auth.spec.ts moves and reverts this SAME
    // $45 transaction. Two specs interleaving their move/revert around each other's baselines
    // would produce a zero delta with nothing wrong in the cache — the same false signal this
    // guard exists to prevent. It is not reachable today on either path: CI runs
    // `workers: 1` (playwright.config.ts), and locally that spec `test.skip`s itself unless
    // TEST_CLERK_EMAIL_2 is set, which .env.e2e does not set. Setting a second identity locally
    // (workers: 2) makes the hazard live — isolate the row or serialize the two specs before
    // relying on a local run in that configuration.
    //
    // PREMISE GUARD — the moved transaction must fall inside the window the widget will read.
    //
    // Every Expenses assertion below is a subtraction across two reads of the SAME window, so a
    // transaction OUTSIDE that window yields a difference of exactly $0 — indistinguishable from
    // a stale cache, and reported as one by the "serving stale data" message on the first
    // Expenses assertion. This guard exists because that is not hypothetical: on 2026-09-02 the
    // fixture's
    // `CURRENT_DATE - INTERVAL '2 days'` dated this row 2026-08-31 while the widget requested
    // September, and the spec failed claiming the dashboard served stale data when the cache was
    // working correctly.
    //
    // The window is not ours to pick. captureCashFlowSummary reads the response the widget's own
    // fetch received, and DashboardContext defaults `timeframe` to 'this_month' in plain useState
    // with no URL or localStorage persistence — so the `page.reload()` inside that helper always
    // returns to the current calendar month, whatever we set beforehand. The fixture complies
    // instead: seed-e2e-data.sql clamps this row into the current month (see its comment).
    //
    // ⚠ UTC getters, deliberately — NOT getFullYear()/getMonth(). The clock that decides the
    // window is the BROWSER's, and the browser is pinned to UTC (playwright.config.ts
    // `timezoneId: 'UTC'`, added by #1569 T8-FIX) while this Node process runs in the runner's
    // local zone. Using local getters here compares against a clock nothing else in the system
    // uses: at 2026-09-01T00:30Z a UTC browser and a UTC database both say September while a
    // New York runner still says August, and the guard would fail a fixture that is correct.
    // The seed names UTC explicitly for the same reason (see seed-e2e-data.sql), so all three
    // clocks agree by construction rather than by coincidence.
    //
    // Date-only comparison on the ISO prefix; `date` is serialized from a Go time.Time.
    const now = new Date()
    const currentMonthPrefix = `${now.getUTCFullYear()}-${String(now.getUTCMonth() + 1).padStart(2, '0')}`
    expect(
      chipotle!.date.slice(0, 7),
      `Seeded Chipotle transaction is dated ${chipotle!.date} — outside the current UTC month ` +
        `(${currentMonthPrefix}), which is the only window the Cash Flow Summary widget requests. ` +
        'Moving its category cannot change that window\'s Expenses total, so every assertion ' +
        'below would report a $0 delta as cache staleness. This is a FIXTURE problem, not a cache ' +
        'problem: fix the date expression for e2e-txn-002 in apps/web/scripts/seed-e2e-data.sql.'
    ).toBe(currentMonthPrefix)

    try {
      // =========================================================================
      // BASELINE (before mutation) — real network round trips, real UI navigation.
      // =========================================================================
      const dashboardCashFlowBaseline = await captureCashFlowSummary(page)
      const reportsCategoryBaseline = await captureSpendingByCategory(page)

      const baselineFoodCategory = reportsCategoryBaseline.categories.find(
        (c) => c.category === ORIGINAL_PFC_PRIMARY
      )
      expect(
        baselineFoodCategory,
        'FOOD_AND_DRINK category missing from the baseline spending-by-category response'
      ).toBeTruthy()

      // =========================================================================
      // ACT — the crux mutation. Real HTTP PUT, going through the real blanket
      // cache-invalidation middleware (internal/huma/middleware/cache_invalidation.go).
      // =========================================================================
      const moveResponse = await client.put(`/api/v1/transactions/${transactionId}/category`, {
        kind: 'pfc',
        pfc_primary: MOVED_PFC_PRIMARY,
        pfc_detailed: MOVED_PFC_DETAILED,
      })
      expect(moveResponse.ok(), `category move failed: ${await moveResponse.text()}`).toBe(true)

      // =========================================================================
      // ASSERT — cache freshness. Fresh navigations (not SPA soft nav) so React Query's
      // in-memory cache doesn't mask a backend-cache staleness bug with a client-cache hit.
      // =========================================================================
      const dashboardCashFlowAfterMove = await captureCashFlowSummary(page)
      const reportsCategoryAfterMove = await captureSpendingByCategory(page)

      expect(
        dashboardCashFlowBaseline.expenses - dashboardCashFlowAfterMove.expenses,
        'Cash-flow-summary Expenses did not drop by exactly the moved transaction amount ' +
          '(TRANSFER_OUT is excluded from expense totals) — the dashboard is serving stale data.'
      ).toBeCloseTo(CHIPOTLE_AMOUNT, 2)

      const foodCategoryAfterMove = reportsCategoryAfterMove.categories.find(
        (c) => c.category === ORIGINAL_PFC_PRIMARY
      )
      const foodTotalAfterMove = foodCategoryAfterMove?.total ?? 0
      expect(
        (baselineFoodCategory!.total - foodTotalAfterMove),
        'FOOD_AND_DRINK spending-by-category total did not drop by exactly the moved amount — ' +
          'the reports page is serving stale data.'
      ).toBeCloseTo(CHIPOTLE_AMOUNT, 2)
      expect(
        reportsCategoryBaseline.total_spent - reportsCategoryAfterMove.total_spent,
        'spending-by-category TotalSpent did not drop by exactly the moved amount.'
      ).toBeCloseTo(CHIPOTLE_AMOUNT, 2)

      // CROSS-ENDPOINT CONSISTENCY (see file header): both real endpoints must move by the
      // identical amount from this ONE write — a bump that silently missed one of the two would
      // show a mismatch here even though each individual assertion above passed.
      const cashFlowDelta = dashboardCashFlowBaseline.expenses - dashboardCashFlowAfterMove.expenses
      const categoryDelta = reportsCategoryBaseline.total_spent - reportsCategoryAfterMove.total_spent
      expect(
        cashFlowDelta,
        'Dashboard cash-flow-summary and the reports page spending-by-category moved by ' +
          'DIFFERENT amounts from the same single write — a cache bump reached one endpoint but ' +
          'not the other.'
      ).toBeCloseTo(categoryDelta, 2)

      // =========================================================================
      // REVERT — substitutes for "deleting it reverts the total" (see file header). Runs inside
      // this try block (not only in `finally`) so its own assertions are part of the test's pass
      // criteria, not merely best-effort cleanup — the `finally` below is a SEPARATE, non-fatal
      // safety net for when an assertion above already failed.
      // =========================================================================
      const revertResponse = await client.put(`/api/v1/transactions/${transactionId}/category`, {
        kind: 'pfc',
        pfc_primary: ORIGINAL_PFC_PRIMARY,
        pfc_detailed: ORIGINAL_PFC_DETAILED,
      })
      expect(revertResponse.ok(), `category revert failed: ${await revertResponse.text()}`).toBe(
        true
      )

      const dashboardCashFlowAfterRevert = await captureCashFlowSummary(page)
      const reportsCategoryAfterRevert = await captureSpendingByCategory(page)

      expect(
        dashboardCashFlowAfterRevert.expenses,
        'Cash-flow-summary Expenses did not revert to baseline after undoing the category move.'
      ).toBeCloseTo(dashboardCashFlowBaseline.expenses, 2)
      expect(
        reportsCategoryAfterRevert.total_spent,
        'spending-by-category TotalSpent did not revert to baseline after undoing the category move.'
      ).toBeCloseTo(reportsCategoryBaseline.total_spent, 2)
    } finally {
      // Best-effort safety net (mirrors household-share-exclusion.auth.spec.ts's cleanup style):
      // guarantees the seeded fixture is never left mutated for a LATER run even if an assertion
      // above threw before the in-test revert ran.
      try {
        await client.put(`/api/v1/transactions/${transactionId}/category`, {
          kind: 'pfc',
          pfc_primary: ORIGINAL_PFC_PRIMARY,
          pfc_detailed: ORIGINAL_PFC_DETAILED,
        })
      } catch (cleanupError) {
        console.warn(`[cache-freshness] category cleanup failed (non-fatal): ${cleanupError}`)
      }
    }
  })
})

// captureCashFlowSummary navigates to /dashboard (adding the Cash Flow Summary widget via the
// real "Add Widget" catalog flow if a prior run hasn't already persisted it — mirrors
// household-share-exclusion.auth.spec.ts's Account Cards pattern) and returns the REAL
// GET /api/v1/analytics/cash-flow-summary response body the widget's own fetch received.
//
// Always uses page.goto (never page.reload): the page may currently be on /reports (from a prior
// captureSpendingByCategory call) — page.reload() would re-load WHATEVER page is currently
// active, not necessarily /dashboard. goto to the same URL is still a full, fresh navigation
// (a real network round trip, not an SPA soft-nav), so it is never a weaker freshness check than
// reload would have been.
async function captureCashFlowSummary(
  page: import('@playwright/test').Page
): Promise<CashFlowSummaryResponse> {
  await page.goto('/dashboard')
  await page.waitForLoadState('domcontentloaded')
  await acceptCookieConsent(page)

  // A bare `.isVisible()` right after domcontentloaded is a snapshot, not a wait — it can return
  // false purely because the SPA hasn't finished its first render/data-fetch yet, even when the
  // widget genuinely IS already persisted in the layout from an earlier call in this same test.
  // A false "absent" here leads into WidgetCatalog's "already added" no-op path (see
  // household-share-exclusion.auth.spec.ts's own note on that behavior), whose "Add"-click never
  // closes the dialog — so this must be a real wait, not an instantaneous check.
  const widgetHeading = page.getByText('Cash Flow Summary', { exact: true })
  const widgetAlreadyPresent = await widgetHeading
    // CI-aware: a bare 5s was the reason CI took the expensive branch at all. On the second
    // call the widget IS already persisted in the saved layout, but after a cold `page.reload()`
    // the dashboard has to resolve profile + layout + accounts before any widget renders, and on
    // the 2-vCPU runner that regularly exceeds 5s. Missing the wait sends the test into the
    // Add-Widget dialog flow — six more interactions — which is what consumed the whole-test
    // budget above. Waiting longer here is strictly cheaper than falling through.
    .waitFor({ state: 'visible', timeout: WAIT_WIDGET_MS })
    .then(() => true)
    .catch(() => false)
  if (!widgetAlreadyPresent) {
    await page.getByRole('button', { name: 'Edit' }).click()
    await page.getByRole('button', { name: 'Add Widget' }).click()
    const catalogDialog = page.getByRole('dialog')
    await expect(catalogDialog.getByRole('heading', { name: 'Add Widget' })).toBeVisible()
    await catalogDialog.getByText('Cash Flow Summary', { exact: true }).click()
    await expect(catalogDialog).not.toBeVisible({ timeout: WAIT_ELEMENT_MS })
    await page.getByRole('button', { name: 'Done' }).click()
    await page.waitForTimeout(500) // debounced layout save (useDashboardLayout SAVE_DEBOUNCE_MS)
  }

  // Arm before reloading, but accept only requests issued after the destination commits.
  // A response from widget preparation can otherwise arrive during this reload.
  const body = await captureNavigationJson<CashFlowSummaryResponse>(page, {
    destinationPath: '/dashboard',
    apiPath: '/api/v1/analytics/cash-flow-summary',
    timeout: WAIT_RESPONSE_MS,
    navigate: () => page.reload({ waitUntil: 'commit', timeout: WAIT_ELEMENT_MS }),
  })
  await expect(page).toHaveURL(/\/dashboard/)
  await page.waitForLoadState('domcontentloaded')

  return body
}

// captureSpendingByCategory navigates to the canonical spending tab and returns the REAL
// GET /api/v1/analytics/spending-by-category response body the page's own fetch received.
// #1300 made /reports a client redirect; capture the destination document directly.
// Always uses page.goto — see captureCashFlowSummary's doc comment for why (page may currently
// be on /dashboard from a prior call).
async function captureSpendingByCategory(
  page: import('@playwright/test').Page
): Promise<SpendingByCategoryResponse> {
  const body = await captureNavigationJson<SpendingByCategoryResponse>(page, {
    destinationPath: '/insights',
    apiPath: '/api/v1/analytics/spending-by-category',
    timeout: WAIT_RESPONSE_MS,
    navigate: () => page.goto('/insights?tab=spending', { waitUntil: 'commit', timeout: WAIT_ELEMENT_MS }),
  })
  await expect(page).toHaveURL(url => url.pathname === '/insights' && url.searchParams.get('tab') === 'spending')
  await page.waitForLoadState('domcontentloaded')
  await acceptCookieConsent(page)

  return body
}
