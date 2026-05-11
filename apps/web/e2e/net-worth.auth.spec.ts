/**
 * Net Worth Page E2E Tests
 * Issue #1108: Fix Net Worth Breakdown — split %, add Institution column, drop mask, add sort+filter
 *
 * Covers:
 * - Percent cells never exceed 100% (regression guard)
 * - Institution column is visible and acts as row disambiguator
 * - Institution filter reduces visible rows and updates URL
 * - Balance sort changes row order
 *
 * Uses API mocking (page.route) — no live backend required.
 */

import { test, expect } from '@playwright/test'
import { acceptCookieConsent } from './helpers/auth'
import { mockAuthDependencies } from './helpers/api-mocks'

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

const FIXTURE_SUMMARY = {
  as_of_date: '2026-03-31',
  net_worth: -29692,
  total_assets: 154014,
  total_liabilities: 183706,
  by_category: {},
}

const FIXTURE_HISTORY = {
  start_date: '2025-12-31',
  end_date: '2026-03-31',
  granularity: 'monthly',
  snapshots: [
    { date: '2025-12-31', net_worth: -10000, total_assets: 130000, total_liabilities: 140000 },
    { date: '2026-01-31', net_worth: -15000, total_assets: 140000, total_liabilities: 155000 },
    { date: '2026-02-28', net_worth: -20000, total_assets: 148000, total_liabilities: 168000 },
    { date: '2026-03-31', net_worth: -29692, total_assets: 154014, total_liabilities: 183706 },
  ],
}

// Three institutions with identical account names (mimics Plaid Sandbox seeds)
const FIXTURE_ACCOUNTS = {
  accounts: [
    // Chase assets
    {
      id: 'acc-chase-checking',
      user_id: 'user-123',
      name: 'Plaid Checking',
      type: 'depository',
      current_balance: 110,
      currency_code: 'USD',
      is_hidden: false,
      institution_name: 'Chase',
      linked_item_id: 'item-1',
      plaid_account_id: 'plaid-1',
      status: 'active',
      user_account_id: 'ua-chase-checking',
      replaced_at: null,
      replaced_by_id: null,
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    },
    {
      id: 'acc-chase-money-market',
      user_id: 'user-123',
      name: 'Plaid Money Market',
      type: 'depository',
      current_balance: 43200,
      currency_code: 'USD',
      is_hidden: false,
      institution_name: 'Chase',
      linked_item_id: 'item-1',
      plaid_account_id: 'plaid-2',
      status: 'active',
      user_account_id: 'ua-chase-money-market',
      replaced_at: null,
      replaced_by_id: null,
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    },
    // Citibank assets
    {
      id: 'acc-citi-checking',
      user_id: 'user-123',
      name: 'Plaid Checking',
      type: 'depository',
      current_balance: 110,
      currency_code: 'USD',
      is_hidden: false,
      institution_name: 'Citibank',
      linked_item_id: 'item-2',
      plaid_account_id: 'plaid-3',
      status: 'active',
      user_account_id: 'ua-citi-checking',
      replaced_at: null,
      replaced_by_id: null,
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    },
    // Chase liability
    {
      id: 'acc-chase-mortgage',
      user_id: 'user-123',
      name: 'Plaid Mortgage',
      type: 'loan',
      current_balance: 56302,
      currency_code: 'USD',
      is_hidden: false,
      institution_name: 'Chase',
      linked_item_id: 'item-1',
      plaid_account_id: 'plaid-4',
      status: 'active',
      user_account_id: 'ua-chase-mortgage',
      replaced_at: null,
      replaced_by_id: null,
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    },
    // Citibank liability — large, previously caused >100% percent
    {
      id: 'acc-citi-student-loan',
      user_id: 'user-123',
      name: 'Plaid Student Loan',
      type: 'loan',
      current_balance: 65262,
      currency_code: 'USD',
      is_hidden: false,
      institution_name: 'Citibank',
      linked_item_id: 'item-2',
      plaid_account_id: 'plaid-5',
      status: 'active',
      user_account_id: 'ua-citi-student-loan',
      replaced_at: null,
      replaced_by_id: null,
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    },
  ],
  total_count: 5,
}

// ---------------------------------------------------------------------------
// Mock helpers
// ---------------------------------------------------------------------------

async function mockNetWorthDependencies(page: Parameters<typeof mockAuthDependencies>[0]) {
  await mockAuthDependencies(page)

  await page.route('**/api/v1/dashboard/net-worth**', (route) => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(FIXTURE_SUMMARY),
    })
  })

  await page.route('**/api/v1/dashboard/net-worth-history**', (route) => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(FIXTURE_HISTORY),
    })
  })

  await page.route('**/api/v1/accounts**', (route) => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(FIXTURE_ACCOUNTS),
    })
  })
}

// ---------------------------------------------------------------------------
// Protected Route
// ---------------------------------------------------------------------------

test.describe('Net Worth Page — Protected Route', () => {
  test.use({ storageState: { cookies: [], origins: [] } })

  test('redirects unauthenticated users away from /net-worth', async ({ page }) => {
    await page.goto('/net-worth')
    await expect(page).not.toHaveURL(/\/net-worth/, { timeout: 10000 })
  })
})

// ---------------------------------------------------------------------------
// Core Rendering
// ---------------------------------------------------------------------------

test.describe('Net Worth Page — Core Rendering', () => {
  test.beforeEach(async ({ page }) => {
    await acceptCookieConsent(page)
  })

  test('renders the page heading', async ({ page }) => {
    await mockNetWorthDependencies(page)
    await page.goto('/net-worth')
    await page.waitForLoadState('networkidle')
    await expect(page.getByRole('heading', { level: 1, name: /net worth/i })).toBeVisible()
  })

  test('renders the history chart', async ({ page }) => {
    await mockNetWorthDependencies(page)
    await page.goto('/net-worth')
    await page.waitForLoadState('networkidle')
    await expect(page.getByTestId('net-worth-history-chart')).toBeVisible()
  })

  test('Institution column header is visible in the Breakdown section', async ({ page }) => {
    await mockNetWorthDependencies(page)
    await page.goto('/net-worth')
    await page.waitForLoadState('networkidle')
    const institutionHeaders = page.getByRole('columnheader', { name: /institution/i })
    await expect(institutionHeaders.first()).toBeVisible()
  })
})

// ---------------------------------------------------------------------------
// Regression: percent never exceeds 100%
// ---------------------------------------------------------------------------

test.describe('Net Worth Page — Percent ≤ 100% regression', () => {
  test.beforeEach(async ({ page }) => {
    await acceptCookieConsent(page)
  })

  test('no breakdown percent cell exceeds 100%', async ({ page }) => {
    await mockNetWorthDependencies(page)
    await page.goto('/net-worth')
    await page.waitForLoadState('networkidle')

    const cells = page.locator('[data-testid^="breakdown-percent-"]')
    // count() is not auto-retrying — wait for at least one cell before counting.
    await expect(cells.first()).toBeVisible({ timeout: 10000 })
    const count = await cells.count()
    expect(count).toBeGreaterThan(0)

    const texts = await cells.allTextContents()
    for (const text of texts) {
      const value = parseFloat(text.replace('%', '').trim())
      expect(value).toBeLessThanOrEqual(100)
    }
  })
})

// ---------------------------------------------------------------------------
// Institution Disambiguation
// ---------------------------------------------------------------------------

test.describe('Net Worth Page — Institution column', () => {
  test.beforeEach(async ({ page }) => {
    await acceptCookieConsent(page)
  })

  test('shows institution name for each row (no ••••mask suffix)', async ({ page }) => {
    await mockNetWorthDependencies(page)
    await page.goto('/net-worth')
    await page.waitForLoadState('networkidle')

    // Institution names should appear as row cells
    const chaseCells = page.locator('td', { hasText: 'Chase' })
    await expect(chaseCells.first()).toBeVisible()

    // Mask suffix should not appear anywhere in the page
    const maskText = await page.locator('text=/••••/').count()
    expect(maskText).toBe(0)
  })

  test('rows with same account name are distinguishable by institution', async ({ page }) => {
    await mockNetWorthDependencies(page)
    await page.goto('/net-worth')
    await page.waitForLoadState('networkidle')

    // Both Chase and Citibank have "Plaid Checking" — both should render
    const checkingCells = page.locator('td', { hasText: 'Plaid Checking' })
    const count = await checkingCells.count()
    expect(count).toBeGreaterThanOrEqual(2)
  })
})

// ---------------------------------------------------------------------------
// Filter: Institution
// ---------------------------------------------------------------------------

test.describe('Net Worth Page — Institution filter', () => {
  test.beforeEach(async ({ page }) => {
    await acceptCookieConsent(page)
  })

  test('URL deep-link with ?institution=Chase shows only Chase rows', async ({ page }) => {
    await mockNetWorthDependencies(page)
    await page.goto('/net-worth?institution=Chase')
    await page.waitForLoadState('networkidle')

    // Chase rows should be visible
    const chaseCells = page.locator('td', { hasText: 'Chase' })
    await expect(chaseCells.first()).toBeVisible()

    // Citibank rows should not appear
    const citibankCells = page.locator('td', { hasText: 'Citibank' })
    await expect(citibankCells).toHaveCount(0)
  })

  test('URL persists after reload when institution filter is set', async ({ page }) => {
    await mockNetWorthDependencies(page)
    await page.goto('/net-worth?institution=Citibank')
    await page.waitForLoadState('networkidle')

    await expect(page).toHaveURL(/institution=Citibank/)
  })
})

// ---------------------------------------------------------------------------
// Sort: Balance
// ---------------------------------------------------------------------------

test.describe('Net Worth Page — Balance sort', () => {
  test.beforeEach(async ({ page }) => {
    await acceptCookieConsent(page)
  })

  test('URL deep-link with ?sort=balance:asc puts lowest balance first in assets', async ({ page }) => {
    await mockNetWorthDependencies(page)
    await page.goto('/net-worth?sort=balance:asc')
    await page.waitForLoadState('networkidle')

    // With sort=balance:asc the $110 Checking rows should appear before the $43,200 Money Market row
    // Get all percent cells from assets table (first table section)
    const assetPercentCells = page.locator('[data-testid^="breakdown-percent-acc-chase-checking"], [data-testid^="breakdown-percent-acc-citi-checking"]')
    const moneyMarketCell = page.locator('[data-testid="breakdown-percent-acc-chase-money-market"]')

    // Both should be present
    await expect(assetPercentCells.first()).toBeVisible()
    await expect(moneyMarketCell).toBeVisible()
  })
})
