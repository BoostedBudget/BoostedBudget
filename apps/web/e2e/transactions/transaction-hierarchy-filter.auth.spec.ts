/**
 * Transaction Hierarchical Category Filter E2E Tests — Issue #1200
 *
 * Covers:
 *   1. Sub-category column shows the effective pfc_detailed label
 *   2. HierarchicalCategoryTreeSelect filter writes pfc_detaileds to URL params
 */

import { test, expect, type Page } from '@playwright/test'
import { acceptCookieConsent } from '../helpers/auth'
import { mockAuthDependencies } from '../helpers/api-mocks'

// ─── Shared mock transaction ─────────────────────────────────────────────────

const MOCK_TRANSACTION = {
  id: 'txn-test-1',
  user_id: 'user-123',
  account_id: 'acc-1',
  linked_item_id: 'item-1',
  source: 'plaid',
  external_transaction_id: 'plaid-test-1',
  amount: 5.50,
  date: '2026-01-15',
  name: 'Starbucks Coffee',
  merchant_name: 'Starbucks Coffee',
  // FOOD_AND_DRINK_COFFEE is the correct enum (pfcOptions.ts label: "Coffee")
  pfc_primary: 'FOOD_AND_DRINK',
  pfc_detailed: 'FOOD_AND_DRINK_COFFEE',
  category_source: 'plaid',
  pending: false,
  tags: [],
  tags_source: [],
  created_at: '2026-01-15T10:00:00Z',
  updated_at: '2026-01-15T10:00:00Z',
}

const MOCK_TRANSACTIONS_RESPONSE = {
  transactions: [MOCK_TRANSACTION],
  limit: 50,
  next_cursor: null,
}

// ─── Shared setup helpers ─────────────────────────────────────────────────────

async function mockTransactionAPIs(page: Page) {
  await mockAuthDependencies(page)

  await page.route('**/api/v1/transactions**', (route) => {
    if (route.request().method() === 'GET') {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(MOCK_TRANSACTIONS_RESPONSE),
      })
    } else {
      route.continue()
    }
  })

  await page.route('**/api/v1/user/tags**', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
  )
  await page.route('**/api/v1/user/categories**', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
  )
  await page.route('**/api/v1/categories**', (route) => {
    if (route.request().method() === 'GET') {
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
    } else {
      route.fallback()
    }
  })
  await page.route('**/api/v1/plaid/categories**', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
  )
  await page.route('**/api/v1/accounts**', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        accounts: [
          {
            id: 'acc-1',
            name: 'Checking',
            nickname: null,
            type: 'depository',
            mask: '0001',
            current_balance: 1000,
            custom_color: '#3b82f6',
            custom_emoji: null,
          },
        ],
      }),
    })
  )
}

// ─── Tests ────────────────────────────────────────────────────────────────────

test.describe('Transaction hierarchical category filter', () => {
  test.beforeEach(async ({ page }) => {
    await acceptCookieConsent(page)
  })

  test('Sub-category column displays effective pfc_detailed for transactions', async ({ page }) => {
    await mockTransactionAPIs(page)
    await page.goto('/transactions')
    await page.waitForLoadState('domcontentloaded')

    // Wait for the transaction row to appear
    await expect(page.getByText('Starbucks Coffee')).toBeVisible({ timeout: 10000 })

    // The Sub-category column renders getDetailedLabel('FOOD_AND_DRINK_COFFEE') = "Coffee"
    // (defined in pfcOptions.ts PFC_HIERARCHY under FOOD_AND_DRINK)
    await expect(page.getByText('Coffee', { exact: true })).toBeVisible({ timeout: 5000 })
  })

  test('sub-category column filter writes pfc_detaileds to URL via column dropdown', async ({ page }) => {
    await mockTransactionAPIs(page)
    await page.goto('/transactions')
    await page.waitForLoadState('domcontentloaded')
    await expect(page.getByText('Starbucks Coffee')).toBeVisible({ timeout: 10000 })

    // The Sub-category column header has a filter icon button (added in #1204)
    const subCatFilterBtn = page.getByRole('button', { name: /filter by sub-category/i })
    await expect(subCatFilterBtn).toBeVisible({ timeout: 5000 })
    await subCatFilterBtn.click()

    // Dropdown shows a searchable flat list of PFC detailed options
    await expect(page.getByPlaceholder(/search/i)).toBeVisible({ timeout: 3000 })

    // Select "Coffee" from the dropdown
    const coffeeCheckbox = page.getByRole('checkbox', { name: /^Coffee$/i })
    await expect(coffeeCheckbox).toBeVisible({ timeout: 3000 })
    await coffeeCheckbox.click()

    // URL should now contain pfc_detaileds=FOOD_AND_DRINK_COFFEE
    await expect(page).toHaveURL(/pfc_detaileds=FOOD_AND_DRINK_COFFEE/, { timeout: 5000 })

    // Sub-category filter button shows active styling
    await expect(subCatFilterBtn).toHaveClass(/column-filter__trigger--active/, { timeout: 3000 })
  })

  test('hierarchical category filter applies pfc_detaileds to transaction list', async ({ page }) => {
    await mockTransactionAPIs(page)
    await page.goto('/transactions')
    await page.waitForLoadState('domcontentloaded')

    // Wait for the transaction table to render
    await expect(page.getByText('Starbucks Coffee')).toBeVisible({ timeout: 10000 })

    // Open the HierarchicalCategoryTreeSelect filter — it renders as a Filter icon button
    // inside the Category column header with aria-label="Filter by category"
    const categoryFilterTrigger = page.getByRole('button', { name: 'Filter by category' })
    await expect(categoryFilterTrigger).toBeVisible({ timeout: 5000 })
    await categoryFilterTrigger.click()

    // The popover opens with a search input and tree of primary categories
    await expect(page.getByRole('textbox', { name: 'Search categories' })).toBeVisible({ timeout: 3000 })

    // Expand the Food & Drink primary using its data-testid expand toggle
    const expandToggle = page.locator('[data-testid="expand-toggle-FOOD_AND_DRINK"]')
    await expect(expandToggle).toBeVisible({ timeout: 3000 })
    await expandToggle.click()

    // Click the Coffee detailed sub-category checkbox
    const coffeeCheckbox = page.locator('[data-testid="detailed-checkbox-FOOD_AND_DRINK_COFFEE"]')
    await expect(coffeeCheckbox).toBeVisible({ timeout: 3000 })
    await coffeeCheckbox.click()

    // After selecting a detailed sub-category, the URL should contain pfc_detaileds=FOOD_AND_DRINK_COFFEE
    await expect(page).toHaveURL(/pfc_detaileds=FOOD_AND_DRINK_COFFEE/, { timeout: 5000 })

    // The active-dot indicator should appear on the filter trigger, confirming the filter is active
    const activeDot = page.locator('[data-testid="active-dot"]')
    await expect(activeDot).toBeVisible({ timeout: 3000 })
  })
})
