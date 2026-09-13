/**
 * Transaction Category Editor E2E Tests — PFC curated source
 *
 * Verifies that the "Change category" dropdown in the TransactionEditor
 * shows all 18 curated PFCv2 primaries (including ones the test user has
 * zero transaction history for), closing the original regression from Issue #1062
 * where only manually-created custom categories appeared.
 *
 * Issue #1064: PFC test coverage for AutoCategorizationRuleEditor + e2e specs
 */

import { test, expect, type Page } from '@playwright/test'
import { acceptCookieConsent } from '../helpers/auth'
import { mockAuthDependencies, ALL_PFC_PRIMARIES, FOOD_AND_DRINK_DETAILED } from '../helpers/api-mocks'

// One transaction with only legacy category data (simulates a user whose
// transaction history does NOT include Loan Payments or Government And Non Profit)
const mockTransactions = {
  transactions: [
    {
      id: 'txn-1',
      user_id: 'user-123',
      account_id: 'acc-1',
      linked_item_id: 'item-1',
      source: 'plaid',
      external_transaction_id: 'plaid-txn-1',
      amount: 25.50,
      date: '2025-11-15',
      name: 'Starbucks',
      merchant_name: 'Starbucks',
      pfc_primary: 'FOOD_AND_DRINK',
      pfc_detailed: 'FOOD_AND_DRINK_COFFEE',
      pending: false,
      tags: [],
      created_at: '2025-11-15T10:00:00Z',
      updated_at: '2025-11-15T10:00:00Z',
    },
  ],
  total_count: 1,
  limit: 50,
  offset: 0,
  has_more: false,
}

async function mockCategoryEditorAPIs(page: Page) {
  await mockAuthDependencies(page)

  await page.route('**/api/v1/transactions**', (route) => {
    if (route.request().method() === 'GET') {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(mockTransactions),
      })
    } else {
      route.continue()
    }
  })

  // Curated PFC primaries endpoint — all 18 (this is the source of truth we're testing)
  await page.route('**/api/v1/categories/plaid-primaries*', (route) => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(ALL_PFC_PRIMARIES),
    })
  })

  // User custom categories — empty (brand-new user scenario).
  // Orval's useGetUserCategories hits GET /api/v1/categories (the function name
  // contains "User" but the URL does not). Mock the exact URL.
  await page.route('**/api/v1/categories', (route) => {
    if (route.request().method() === 'GET') {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([]),
      })
    } else {
      route.fallback()
    }
  })

  await page.route('**/api/v1/tags*', (route) => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([]),
    })
  })

  await page.route('**/api/v1/accounts*', (route) => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ accounts: [] }),
    })
  })
}

test.describe('Transaction Category Editor — PFC curated source', () => {
  test.beforeEach(async ({ page }) => {
    await acceptCookieConsent(page)
    await page.route('**/api/v1/categories/plaid-detailed', route => route.fulfill({ json: FOOD_AND_DRINK_DETAILED }))
  })

  test('Change category dropdown shows all 18 PFCv2 primaries including unobserved ones', async ({
    page,
  }) => {
    await mockCategoryEditorAPIs(page)
    await page.goto('/transactions')
    await page.waitForLoadState('domcontentloaded')

    // Wait for transaction row to appear
    await expect(page.getByText('Starbucks')).toBeVisible({ timeout: 10000 })

    // Open the transaction editor via the edit button
    await page.getByRole('button', { name: 'Edit transaction' }).first().click()

    // Wait for the editor to open
    await expect(page.getByRole('button', { name: 'Change category' })).toBeVisible({ timeout: 5000 })

    // Open the category dropdown
    await page.getByRole('button', { name: 'Change category' }).click()

    // Assert the dropdown is open with a listbox
    const listbox = page.getByRole('listbox')
    await expect(listbox).toBeVisible()

    // Count all options (role=option) — should include Uncategorized + 18 PFC primaries
    const options = listbox.getByRole('option')
    const optionCount = await options.count()
    expect(optionCount).toBeGreaterThanOrEqual(18)

    // Assert curated primaries that the test user has ZERO transaction history for
    // (proving the curated source, not observed-only, is used)
    await expect(listbox.getByRole('option', { name: 'Loan Payments' })).toBeVisible()
    await expect(listbox.getByRole('option', { name: 'Government And Non Profit', exact: true })).toBeVisible()
    await expect(listbox.getByRole('option', { name: 'Loan Disbursements' })).toBeVisible()

    // Reset is an explicit action, outside selectable category identities.
    await expect(page.getByRole('button', { name: 'Reset category override' })).toBeVisible()
  })

  test('Change category dropdown shows custom categories merged with curated primaries', async ({
    page,
  }) => {
    await mockAuthDependencies(page)

    await page.route('**/api/v1/transactions**', (route) => {
      if (route.request().method() === 'GET') {
        route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(mockTransactions),
        })
      } else {
        route.continue()
      }
    })

    await page.route('**/api/v1/categories/plaid-primaries*', (route) => {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(ALL_PFC_PRIMARIES),
      })
    })

    // Equal display names retain distinct bank/custom identities.
    // Orval's useGetUserCategories hits GET /api/v1/categories (not /categories/user).
    await page.route('**/api/v1/categories', (route) => {
      if (route.request().method() === 'GET') {
        route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([
            { id: 'cat-1', name: 'My Custom', color: '#FF0000' },
            { id: 'cat-2', name: 'Food And Drink', color: '#00FF00' }, // overlaps PFC primary
          ]),
        })
      } else {
        route.fallback()
      }
    })

    await page.route('**/api/v1/tags*', (route) => {
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
    })
    await page.route('**/api/v1/accounts*', (route) => {
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ accounts: [] }) })
    })

    await page.goto('/transactions')
    await page.waitForLoadState('domcontentloaded')
    await expect(page.getByText('Starbucks')).toBeVisible({ timeout: 10000 })

    await page.getByRole('button', { name: 'Edit transaction' }).first().click()
    await expect(page.getByRole('button', { name: 'Change category' })).toBeVisible({ timeout: 5000 })
    await page.getByRole('button', { name: 'Change category' }).click()

    const listbox = page.getByRole('listbox')
    await expect(listbox).toBeVisible()

    // Custom category is present
    await expect(listbox.getByRole('option', { name: 'My Custom' })).toBeVisible()

    await expect(listbox.getByRole('option', { name: 'Food And Drink', exact: true })).toBeVisible()
    await expect(listbox.getByRole('option', { name: 'Food And Drink Custom', exact: true })).toBeVisible()
  })
})
