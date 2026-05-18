/**
 * DataTable Sticky Header E2E Tests
 * Issue #1132: Add stickyHeader and stickyHeaderTopOffset props to DataTable.
 *
 * Covers:
 * - thead pins during vertical scroll in a virtualized scroll container (/transactions)
 * - thead pins below the global 64px app header on a document-scroll page (/accounts)
 * - filter dropdown (Radix Popover portaled to body) is visible when opened in a sticky header
 *
 * Uses API mocking (page.route) — no live backend or seeded QA data required.
 *
 * Run with: bun run test:e2e -- --grep 'sticky-header' --reporter=line
 */

import { test, expect } from '@playwright/test'
import type { Page } from '@playwright/test'
import { acceptCookieConsent } from './helpers/auth'
import { mockAuthDependencies } from './helpers/api-mocks'

// ---------------------------------------------------------------------------
// Mock helpers — transactions page
// ---------------------------------------------------------------------------

/** Build a page of 50 minimal transactions for scroll tests. */
function buildTransactionPage(
  pageIndex: number,
  count: number,
  nextCursor: string | null
) {
  const transactions = Array.from({ length: count }, (_, i) => {
    const idx = pageIndex * count + i
    return {
      id: `txn-sticky-${pageIndex}-${i}`,
      user_id: 'user-123',
      account_id: 'acc-1',
      linked_item_id: 'item-1',
      source: 'plaid',
      external_transaction_id: `plaid-sticky-${idx}`,
      amount: 10 + idx,
      date: '2026-05-01',
      name: `Sticky Transaction ${idx}`,
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

const TXN_PAGE_0 = buildTransactionPage(0, 50, null)

async function mockTransactionsDependencies(page: Page) {
  await mockAuthDependencies(page)

  await page.route('**/api/v1/transactions**', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(TXN_PAGE_0),
    })
  )

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
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ accounts: [] }),
    })
  )
}

// ---------------------------------------------------------------------------
// Mock helpers — accounts page
// ---------------------------------------------------------------------------

/** Build a list of accounts with enough items to require scrolling. */
function buildAccounts(count: number) {
  return Array.from({ length: count }, (_, i) => ({
    id: `acc-sticky-${i}`,
    user_id: 'user-123',
    linked_item_id: 'item-1',
    linked_item_status: 'active',
    name: `Account ${i}`,
    official_name: `Official Account ${i}`,
    nickname: null,
    mask: `${1000 + i}`,
    type: 'depository',
    subtype: i % 2 === 0 ? 'checking' : 'savings',
    institution_name: `Bank ${i % 3}`,
    current_balance: 1000 + i * 100,
    is_hidden: false,
    display_order: i,
    custom_color: null,
    custom_emoji: null,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    replaced_by_account_id: null,
  }))
}

async function mockAccountsDependencies(page: Page) {
  await mockAuthDependencies(page)

  await page.route('**/api/v1/accounts**', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ accounts: buildAccounts(20) }),
    })
  )

  await page.route('**/api/v1/subscription/tiers**', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        tiers: [
          {
            id: 'premium',
            name: 'Premium',
            max_accounts: 50,
            price: 8.99,
          },
        ],
      }),
    })
  )
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test.describe('DataTable — Sticky Header', () => {
  test.beforeEach(async ({ page }) => {
    await acceptCookieConsent(page)
  })

  // ── Test 1: virtualized container (transactions page) ─────────────────────
  test('thead pins during vertical scroll on /transactions', async ({ page }) => {
    await mockTransactionsDependencies(page)
    await page.goto('/transactions')
    await page.waitForSelector('[data-testid="virtual-scroll-container"]', { timeout: 15000 })

    const thead = page.locator('table thead').first()
    const scrollContainer = page.locator('[data-testid="virtual-scroll-container"]')

    const before = await thead.boundingBox()
    expect(before).not.toBeNull()

    // Scroll 600px inside the virtual container
    await scrollContainer.evaluate((el) => el.scrollTo(0, 600))
    await page.waitForTimeout(100)

    const after = await thead.boundingBox()
    expect(after).not.toBeNull()

    // Sticky thead must not move — allow 2px tolerance for sub-pixel rendering
    expect(Math.abs(after!.y - before!.y)).toBeLessThan(2)
  })

  // ── Test 2: non-virtualized document scroll (accounts page) ──────────────
  test('thead pins below global header on /accounts', async ({ page }) => {
    await mockAccountsDependencies(page)
    await page.goto('/accounts')

    // Accounts page defaults to card view — switch to table view via the
    // ListViewToggle button (title="Table view", aria-pressed="false").
    const tableToggle = page.locator('button[title="Table view"]')
    await expect(tableToggle).toBeVisible({ timeout: 10000 })
    await tableToggle.click()

    // Wait for the DataTable to appear
    const thead = page.locator('table thead').first()
    await expect(thead).toBeVisible({ timeout: 10000 })

    // Scroll the document 800px to push content up
    await page.evaluate(() => window.scrollTo(0, 800))
    await page.waitForTimeout(100)

    const box = await thead.boundingBox()
    expect(box).not.toBeNull()

    // With stickyHeaderTopOffset="4rem" (64px), the thead top must be
    // at ~64px from viewport top (allow 4px tolerance for borders/rounding).
    expect(box!.y).toBeGreaterThanOrEqual(60)
    // Confirm it is actually pinned and visible (not scrolled off-screen)
    expect(box!.y).toBeLessThan(200)
  })

  // ── Test 3: filter dropdown is not clipped by sticky thead ───────────────
  test('filter dropdown is visible when opened in a sticky-header table', async ({ page }) => {
    await mockTransactionsDependencies(page)
    await page.goto('/transactions')
    await page.waitForSelector('[data-testid="virtual-scroll-container"]', { timeout: 15000 })

    // Find the first filter trigger button inside the table header.
    // ColumnFilterDropdown renders: <button aria-haspopup="listbox" ...>
    const filterButton = page.locator('table thead button[aria-haspopup="listbox"]').first()
    await expect(filterButton).toBeVisible({ timeout: 10000 })
    await filterButton.click()

    // The PopoverContent is portaled to document.body (Radix Popover) so it
    // should not be clipped by the sticky thead stacking context.
    const listbox = page.getByRole('listbox').first()
    await expect(listbox).toBeVisible()

    // The listbox must appear below the thead's bottom edge (not hidden behind it)
    const listboxBox = await listbox.boundingBox()
    const theadBox = await page.locator('table thead').first().boundingBox()
    expect(listboxBox).not.toBeNull()
    expect(theadBox).not.toBeNull()
    expect(listboxBox!.y).toBeGreaterThan(theadBox!.y)
  })
})
