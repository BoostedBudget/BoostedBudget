/**
 * Investment Feed E2E Tests
 *
 * Tests investment activity rows appearing inline in the unified Transactions feed.
 * Uses mocked API responses (no live backend or QA dataset required).
 *
 * Issue #1299: Investment transactions in unified Transactions feed
 */

import { test, expect, type Page } from '@playwright/test'
import { acceptCookieConsent } from '../helpers/auth'
import { mockAuthDependencies } from '../helpers/api-mocks'

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

const BANKING_ROW = {
  id: 'txn-banking-1',
  user_id: 'user-123',
  account_id: 'acc-checking',
  linked_item_id: 'item-1',
  source: 'banking',
  amount: 45.99,
  date: '2025-11-15',
  name: 'Coffee Shop',
  merchant_name: 'Starbucks',
  pfc_primary: 'FOOD_AND_DRINK',
  pfc_detailed: 'FOOD_AND_DRINK_COFFEE',
  pfc_primary_display: 'Food And Drink',
  pfc_detailed_display: 'Coffee',
  pending: false,
  tags: [],
  created_at: '2025-11-15T10:00:00Z',
  updated_at: '2025-11-15T10:00:00Z',
  sort_date: '2025-11-15',
}

const INVESTMENT_ROW = {
  id: 'txn-invest-1',
  user_id: 'user-123',
  account_id: 'acc-brokerage',
  linked_item_id: 'item-2',
  source: 'investment',
  amount: -1500.0,
  date: '2025-11-14',
  name: 'Buy AAPL',
  ticker_symbol: 'AAPL',
  security_name: 'Apple Inc.',
  activity_type: 'buy',
  investment_quantity: 10,
  investment_price: 150.0,
  investment_fees: 0.99,
  pending: false,
  tags: [],
  created_at: '2025-11-14T10:00:00Z',
  updated_at: '2025-11-14T10:00:00Z',
  sort_date: '2025-11-14',
}

const MOCK_ACCOUNTS = {
  accounts: [
    {
      id: 'acc-checking',
      user_id: 'user-123',
      institution_name: 'Chase',
      name: 'Checking Account',
      nickname: 'My Checking',
      type: 'depository',
      subtype: 'checking',
      status: 'active',
      current_balance: 5000.0,
      available_balance: 4800.0,
      currency_code: 'USD',
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    },
    {
      id: 'acc-brokerage',
      user_id: 'user-123',
      institution_name: 'Vanguard',
      name: 'Brokerage Account',
      nickname: 'Brokerage ••1234',
      type: 'investment',
      subtype: 'brokerage',
      status: 'active',
      current_balance: 50000.0,
      available_balance: 50000.0,
      currency_code: 'USD',
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    },
  ],
  total_count: 2,
}

// ---------------------------------------------------------------------------
// Mock setup helper
// ---------------------------------------------------------------------------

async function mockInvestmentFeedAPIs(
  page: Page,
  overrides: { transactions?: object[]; includeInvestment?: boolean } = {}
) {
  const { transactions = [BANKING_ROW, INVESTMENT_ROW] } = overrides

  await mockAuthDependencies(page)

  // Transactions endpoint — honours include_investment_activity param
  await page.route('**/api/v1/transactions**', (route) => {
    const url = new URL(route.request().url())
    const includeInvestment = url.searchParams.get('include_investment_activity')

    let rows = transactions
    if (includeInvestment === 'false') {
      rows = transactions.filter((t) => (t as { source: string }).source !== 'investment')
    }

    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        transactions: rows,
        total_count: rows.length,
        limit: 50,
        offset: 0,
        has_more: false,
      }),
    })
  })

  await page.route('**/api/v1/accounts*', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(MOCK_ACCOUNTS) })
  )

  // Stub remaining endpoints the page requires
  await page.route('**/api/v1/tags*', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ tags: [], total_count: 0 }) })
  )
  await page.route('**/api/v1/categories/plaid*', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
  )
  await page.route('**/api/v1/categories*', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ categories: [], total_count: 0 }) })
  )
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test.describe('Investment feed — unified Transactions page (#1299)', () => {
  test.beforeEach(async ({ page }) => {
    await mockInvestmentFeedAPIs(page)
    await page.goto('/transactions')
    await acceptCookieConsent(page)
    await page.waitForLoadState('networkidle')
  })

  // AC1: Investment badge renders
  test('investment row shows "Investment" badge', async ({ page }) => {
    await expect(page.getByText('Investment')).toBeVisible({ timeout: 10000 })
  })

  // AC2: Merchant column shows ticker · security_name
  test('investment row merchant column shows ticker · security_name', async ({ page }) => {
    await expect(page.getByText('AAPL · Apple Inc.')).toBeVisible({ timeout: 10000 })
  })

  // AC3: Activity type chip renders
  test('investment row category column shows activity-type chip', async ({ page }) => {
    // ActivityTypeChip for 'buy' renders the text "Buy"
    await expect(page.getByText('Buy')).toBeVisible({ timeout: 10000 })
  })

  // AC6/AC7: Toggle hides investment rows
  test('toggling "Include investment activity" OFF hides investment rows', async ({ page }) => {
    // Expand filter panel
    await page.getByRole('button', { name: /filters/i }).click()

    // Verify investment row is visible before toggling
    await expect(page.getByText('AAPL · Apple Inc.')).toBeVisible()

    // Toggle OFF
    const toggle = page.getByRole('switch', { name: /include investment activity/i })
    await toggle.click()

    // Investment row should disappear; banking row should remain
    await expect(page.getByText('AAPL · Apple Inc.')).not.toBeVisible()
    await expect(page.getByText('Starbucks')).toBeVisible()
  })

  // AC5: Clicking investment row opens read-only drawer
  test('clicking investment row opens read-only detail drawer', async ({ page }) => {
    // Click the investment row (ticker cell)
    await page.getByText('AAPL · Apple Inc.').click()

    // Drawer should open with investment detail
    const drawer = page.getByRole('dialog', { name: /investment activity/i })
    await expect(drawer).toBeVisible({ timeout: 5000 })

    // No edit or save buttons in drawer (read-only)
    await expect(drawer.getByRole('button', { name: /edit/i })).toHaveCount(0)
    await expect(drawer.getByRole('button', { name: /save/i })).toHaveCount(0)

    // Account shows resolved nickname, not raw UUID
    await expect(drawer.getByTestId('account-value')).toContainText('Brokerage ••1234')
  })
})

// ---------------------------------------------------------------------------
// Dark-mode smoke (AC13)
// ---------------------------------------------------------------------------

test.describe('Investment feed — dark mode (#1299)', () => {
  test('investment badge, chip, and drawer render in dark mode', async ({ page }) => {
    // Pre-set dark mode before page load so ThemeProvider applies it on mount
    await page.addInitScript(() => {
      localStorage.setItem('bb-theme', 'dark')
    })

    await mockInvestmentFeedAPIs(page)
    await page.goto('/transactions')
    await acceptCookieConsent(page)
    await page.waitForLoadState('networkidle')

    // ThemeProvider sets 'dark' class on <html>
    await expect(page.locator('html')).toHaveClass(/dark/)

    // Key investment elements still render in dark mode
    await expect(page.getByText('Investment')).toBeVisible({ timeout: 10000 })
    await expect(page.getByText('AAPL · Apple Inc.')).toBeVisible()
    await expect(page.getByText('Buy')).toBeVisible()

    // Drawer opens in dark mode
    await page.getByText('AAPL · Apple Inc.').click()
    const drawer = page.getByRole('dialog', { name: /investment activity/i })
    await expect(drawer).toBeVisible({ timeout: 5000 })
  })
})
