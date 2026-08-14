import { test, expect } from '@playwright/test'
import { acceptCookieConsent } from './helpers/auth'
import { apiClient } from './helpers/api/client'

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
    testInfo.setTimeout(120_000)

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
        { timeout: 15000 }
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
    const txnBody: { transactions: Array<{ id: string; amount: number; pfc_primary?: string }> } =
      await txnResponse.json()
    const chipotle = txnBody.transactions.find((t) => t.amount === CHIPOTLE_AMOUNT)
    expect(
      chipotle,
      'Seeded "Chipotle Mexican Grill" ($45.00) transaction not found — run ' +
        'apps/web/scripts/seed-e2e-data.sql against the E2E database first.'
    ).toBeTruthy()
    const transactionId = chipotle!.id
    expect(chipotle!.pfc_primary).toBe(ORIGINAL_PFC_PRIMARY)

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
    .waitFor({ state: 'visible', timeout: 5000 })
    .then(() => true)
    .catch(() => false)
  if (!widgetAlreadyPresent) {
    await page.getByRole('button', { name: 'Edit' }).click()
    await page.getByRole('button', { name: 'Add Widget' }).click()
    const catalogDialog = page.getByRole('dialog')
    await expect(catalogDialog.getByRole('heading', { name: 'Add Widget' })).toBeVisible()
    await catalogDialog.getByText('Cash Flow Summary', { exact: true }).click()
    await expect(catalogDialog).not.toBeVisible({ timeout: 10000 })
    await page.getByRole('button', { name: 'Done' }).click()
    await page.waitForTimeout(500) // debounced layout save (useDashboardLayout SAVE_DEBOUNCE_MS)
  }

  // ONE deterministic reload, with the response promise set up immediately before it — this is
  // the only navigation whose response we read the body of, so there is no race between an
  // earlier navigation's response object being invalidated by a later one (which is exactly what
  // "page.reload() at the end of the widget-add branch, but the response promise was set up
  // before the FIRST goto" produced: a "No resource with given identifier found" Protocol error
  // when the widget-add branch's own reload discarded the original response before it was read).
  const responsePromise = page.waitForResponse(
    (r) => r.url().includes('/api/v1/analytics/cash-flow-summary') && r.request().method() === 'GET',
    { timeout: 15000 }
  )
  await page.reload()
  await page.waitForLoadState('domcontentloaded')

  const response = await responsePromise
  return response.json()
}

// captureSpendingByCategory navigates to /reports and returns the REAL
// GET /api/v1/analytics/spending-by-category response body the page's own fetch received.
// Always uses page.goto — see captureCashFlowSummary's doc comment for why (page may currently
// be on /dashboard from a prior call).
async function captureSpendingByCategory(
  page: import('@playwright/test').Page
): Promise<SpendingByCategoryResponse> {
  const responsePromise = page.waitForResponse(
    (r) =>
      r.url().includes('/api/v1/analytics/spending-by-category') && r.request().method() === 'GET',
    { timeout: 15000 }
  )

  await page.goto('/reports')
  await page.waitForLoadState('domcontentloaded')
  await acceptCookieConsent(page)

  const response = await responsePromise
  return response.json()
}
