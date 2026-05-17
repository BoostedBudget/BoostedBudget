/**
 * Transactions Page — Infinite Scroll E2E Tests
 * Issue #1128: Cursor-based pagination + virtual scroll for transactions.
 *
 * Covers:
 * - Scrolling a virtualized table accumulates unique row IDs across pages
 * - No duplicate rows across page boundaries
 * - aria-live region is present for accessible status announcements
 * - Applying a filter resets the visible rows to only the first page
 *
 * Uses API mocking (page.route) — no live backend or seeded QA data required.
 *
 * Run with: bun run test:e2e -- --grep 'infinite-scroll' --reporter=line
 */

import { test, expect } from '@playwright/test'
import type { Page } from '@playwright/test'
import { acceptCookieConsent } from './helpers/auth'
import { mockAuthDependencies } from './helpers/api-mocks'
import { collectVirtualizedRowIds } from './utils/virtualized-table'

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

/** Build a page of transactions with sequential IDs for easy duplicate detection. */
function buildTransactionPage(
  pageIndex: number,
  count: number,
  nextCursor: string | null
) {
  const transactions = Array.from({ length: count }, (_, i) => {
    const idx = pageIndex * count + i
    return {
      id: `txn-page${pageIndex}-${i}-${idx}`,
      user_id: 'user-123',
      account_id: 'acc-1',
      linked_item_id: 'item-1',
      source: 'plaid',
      external_transaction_id: `plaid-txn-${idx}`,
      amount: 25.5 + idx,
      date: '2026-05-01',
      name: `Transaction ${idx}`,
      merchant_name: `Merchant ${idx}`,
      pfc_primary: 'FOOD_AND_DRINK',
      pfc_detailed: 'FOOD_AND_DRINK_COFFEE',
      pending: false,
      tags: [],
      created_at: '2026-05-01T10:00:00Z',
      updated_at: '2026-05-01T10:00:00Z',
    }
  })
  return { transactions, next_cursor: nextCursor, limit: count }
}

const PAGE_0 = buildTransactionPage(0, 50, 'cursor-page-1')
const PAGE_1 = buildTransactionPage(1, 50, 'cursor-page-2')
const PAGE_2 = buildTransactionPage(2, 50, null) // last page

const FILTERED_PAGE = {
  transactions: [
    {
      id: 'txn-filter-1',
      user_id: 'user-123',
      account_id: 'acc-1',
      linked_item_id: 'item-1',
      source: 'plaid',
      external_transaction_id: 'plaid-filtered-1',
      amount: 5.0,
      date: '2026-05-01',
      name: 'Filtered Result',
      merchant_name: 'Filter Merchant',
      pfc_primary: 'FOOD_AND_DRINK',
      pfc_detailed: 'FOOD_AND_DRINK_COFFEE',
      pending: false,
      tags: [],
      created_at: '2026-05-01T10:00:00Z',
      updated_at: '2026-05-01T10:00:00Z',
    },
  ],
  next_cursor: null,
  total: 1,
}

// ---------------------------------------------------------------------------
// Mock helpers
// ---------------------------------------------------------------------------

/**
 * Set up all mocks needed for the /transactions page to render.
 * The transactions endpoint is cursor-aware: the mock inspects the `cursor`
 * query param to return the correct page of data.
 */
async function mockTransactionsDependencies(page: Page) {
  await mockAuthDependencies(page)

  // Transactions cursor pagination
  await page.route('**/api/v1/transactions**', (route) => {
    const url = new URL(route.request().url())
    const cursor = url.searchParams.get('cursor')
    const search = url.searchParams.get('search')

    // When a search/filter is applied, return a small filtered result set
    if (search) {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(FILTERED_PAGE),
      })
    }

    if (cursor === 'cursor-page-1') {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(PAGE_1),
      })
    }

    if (cursor === 'cursor-page-2') {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(PAGE_2),
      })
    }

    // No cursor — first page
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(PAGE_0),
    })
  })

  // Supporting endpoints called by TransactionsPage on mount
  await page.route('**/api/v1/user/tags**', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
  )

  await page.route('**/api/v1/user/categories**', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
  )

  await page.route('**/api/v1/plaid/categories/primary**', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
  )

  await page.route('**/api/v1/accounts**', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ accounts: [] }) })
  )
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test.describe('Transactions Page — Protected Route', () => {
  test.use({ storageState: { cookies: [], origins: [] } })

  test('redirects unauthenticated users away from /transactions', async ({ page }) => {
    await page.goto('/transactions')
    await expect(page).not.toHaveURL(/\/transactions/, { timeout: 10000 })
  })
})

test.describe('Transactions Page — Infinite Scroll', () => {
  test.beforeEach(async ({ page }) => {
    await acceptCookieConsent(page)
  })

  test('scrolling loads more transactions without duplicates', async ({ page }) => {
    await mockTransactionsDependencies(page)
    await page.goto('/transactions')
    await page.waitForSelector('[data-testid="virtual-scroll-container"]', { timeout: 15000 })

    const ids = await collectVirtualizedRowIds(page, {
      scrollContainerSelector: '[data-testid="virtual-scroll-container"]',
      rowSelector: '[data-row-id]',
      rowIdAttribute: 'data-row-id',
      scrollStepPx: 9999,
      maxScrolls: 5,
      requestUrlPattern: '**/api/v1/transactions**',
    })

    // Should have seen at least 50 unique rows (one full mocked page)
    expect(ids.length).toBeGreaterThan(50)

    // No duplicates — Set size must equal array length
    expect(new Set(ids).size).toBe(ids.length)
  })

  test('aria-live region is present for accessible status announcements', async ({ page }) => {
    await mockTransactionsDependencies(page)
    await page.goto('/transactions')
    await page.waitForLoadState('networkidle')

    // DataTable renders an sr-only aria-live region for column reorder announcements.
    // The transactions page may also render its own status region.
    const liveRegion = page.locator('[aria-live="polite"]').first()
    await expect(liveRegion).toBeAttached()
  })

  test('filter change discards prior page data', async ({ page }) => {
    await mockTransactionsDependencies(page)
    await page.goto('/transactions')
    await page.waitForSelector('[data-testid="virtual-scroll-container"]', { timeout: 15000 })

    // Scroll to trigger loading page 2
    const container = page.locator('[data-testid="virtual-scroll-container"]')
    await container.evaluate((el) => { el.scrollTop += 2000 })
    await page.waitForTimeout(500)

    // Apply a search filter — this should invalidate the infinite query and
    // reset to only the first (filtered) page
    const searchInput = page.getByLabel('Search transactions')
    await searchInput.fill('filtered-search-term')
    await page.waitForTimeout(600)

    // After filtering, we should see only the filtered rows, not 100+ from 2 pages
    const visibleIds = await page.evaluate(() =>
      Array.from(document.querySelectorAll('[data-row-id]')).map((el) =>
        (el as HTMLElement).getAttribute('data-row-id') ?? ''
      )
    )

    // The filter returns 1 result — no page-2 rows should persist
    expect(visibleIds.length).toBeLessThan(50)
  })

  test('virtual scroll container renders with correct testid', async ({ page }) => {
    await mockTransactionsDependencies(page)
    await page.goto('/transactions')

    await expect(
      page.locator('[data-testid="virtual-scroll-container"]')
    ).toBeVisible({ timeout: 15000 })
  })
})
