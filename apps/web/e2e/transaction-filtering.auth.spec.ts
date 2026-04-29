/**
 * Transaction Filtering E2E Tests
 *
 * Tests the transaction filtering user flow including:
 * - Filter panel interaction
 * - Date, account, category, type, amount, and status filters
 * - URL state synchronization
 * - Filter combinations
 * - Mobile responsiveness
 *
 * Issue #529: Transaction page filtering
 */

import { test, expect, type Page } from '@playwright/test'
import { acceptCookieConsent } from './helpers/auth'
import { mockAuthDependencies } from './helpers/api-mocks'

// Mock data for transactions
const mockTransactions = {
  transactions: [
    {
      id: 'txn-1',
      user_id: 'user-123',
      account_id: 'acc-1',
      linked_item_id: 'item-1',
      source: 'plaid',
      external_transaction_id: 'plaid-txn-1',
      amount: -25.50,
      date: '2025-11-15',
      name: 'Coffee Shop',
      merchant_name: 'Starbucks',
      pfc_primary: 'FOOD_AND_DRINK',
      pfc_detailed: 'FOOD_AND_DRINK_COFFEE',
      pending: false,
      tags: ['Food'],
      created_at: '2025-11-15T10:00:00Z',
      updated_at: '2025-11-15T10:00:00Z',
    },
    {
      id: 'txn-2',
      user_id: 'user-123',
      account_id: 'acc-2',
      linked_item_id: 'item-1',
      source: 'plaid',
      external_transaction_id: 'plaid-txn-2',
      amount: -150.00,
      date: '2025-11-14',
      name: 'Grocery Store',
      merchant_name: 'Whole Foods',
      pfc_primary: 'GENERAL_MERCHANDISE',
      pfc_detailed: 'GENERAL_MERCHANDISE_GROCERIES',
      pending: false,
      tags: [],
      created_at: '2025-11-14T10:00:00Z',
      updated_at: '2025-11-14T10:00:00Z',
    },
    {
      id: 'txn-3',
      user_id: 'user-123',
      account_id: 'acc-1',
      linked_item_id: 'item-1',
      source: 'plaid',
      external_transaction_id: 'plaid-txn-3',
      amount: 2500.00,
      date: '2025-11-10',
      name: 'Salary',
      merchant_name: null,
      pfc_primary: 'INCOME',
      pfc_detailed: 'INCOME_WAGES',
      pending: false,
      tags: [],
      created_at: '2025-11-10T10:00:00Z',
      updated_at: '2025-11-10T10:00:00Z',
    },
    {
      id: 'txn-4',
      user_id: 'user-123',
      account_id: 'acc-1',
      linked_item_id: 'item-1',
      source: 'plaid',
      external_transaction_id: 'plaid-txn-4',
      amount: -50.00,
      date: '2025-11-16',
      name: 'Pending Transaction',
      merchant_name: 'Amazon',
      pfc_primary: 'GENERAL_MERCHANDISE',
      pfc_detailed: 'GENERAL_MERCHANDISE_ONLINE_MARKETPLACES',
      pending: true,
      tags: [],
      created_at: '2025-11-16T10:00:00Z',
      updated_at: '2025-11-16T10:00:00Z',
    },
  ],
  total_count: 4,
  limit: 50,
  offset: 0,
  has_more: false,
}

const mockAccounts = {
  accounts: [
    {
      id: 'acc-1',
      linked_item_id: 'item-1',
      user_id: 'user-123',
      plaid_account_id: 'plaid-acc-1',
      name: 'Checking Account',
      official_name: 'Premium Checking',
      type: 'depository',
      subtype: 'checking',
      mask: '1234',
      current_balance: 5000.00,
      available_balance: 4500.00,
      currency_code: 'USD',
      institution_name: 'Chase',
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    },
    {
      id: 'acc-2',
      linked_item_id: 'item-1',
      user_id: 'user-123',
      plaid_account_id: 'plaid-acc-2',
      name: 'Savings Account',
      official_name: 'High Yield Savings',
      type: 'depository',
      subtype: 'savings',
      mask: '5678',
      current_balance: 10000.00,
      available_balance: 10000.00,
      currency_code: 'USD',
      institution_name: 'Chase',
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    },
  ],
  total_count: 2,
}

const mockPlaidCategories = ['Food and Drink', 'Shopping', 'Income', 'Transportation']

const mockTags = [
  {
    id: 'tag-1',
    user_id: 'user-123',
    name: 'Food',
    color: '#FF5733',
    usage_count: 1,
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
  },
]

const mockCategories = [
  {
    id: 'cat-1',
    user_id: 'user-123',
    name: 'Custom Category',
    color: '#FF5733',
    usage_count: 0,
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
  },
]

/**
 * Helper function to mock all transaction filtering APIs
 */
async function mockFilteringAPIs(
  page: Page,
  overrides: {
    transactions?: typeof mockTransactions
    accounts?: typeof mockAccounts
    plaidCategories?: typeof mockPlaidCategories
    transactionsStatus?: number
  } = {}
) {
  const {
    transactions = mockTransactions,
    accounts = mockAccounts,
    plaidCategories = mockPlaidCategories,
    transactionsStatus = 200,
  } = overrides

  // Mock auth-dependent endpoints (PremiumRoute blocks rendering until /user/profile resolves)
  await mockAuthDependencies(page)

  // Mock transactions endpoint with filter handling
  await page.route('**/api/v1/transactions**', (route) => {
    if (route.request().method() === 'GET') {
      const url = new URL(route.request().url())
      let filteredTransactions = [...transactions.transactions]

      // Apply account_ids filter
      const accountIds = url.searchParams.get('account_ids')
      if (accountIds) {
        const ids = accountIds.split(',')
        filteredTransactions = filteredTransactions.filter((t) => ids.includes(t.account_id))
      }

      // Apply amount_type filter
      const amountType = url.searchParams.get('amount_type')
      if (amountType === 'income') {
        filteredTransactions = filteredTransactions.filter((t) => t.amount > 0)
      } else if (amountType === 'expense') {
        filteredTransactions = filteredTransactions.filter((t) => t.amount < 0)
      }

      // Apply pending filter
      const pending = url.searchParams.get('pending')
      if (pending === 'true') {
        filteredTransactions = filteredTransactions.filter((t) => t.pending)
      } else if (pending === 'false') {
        filteredTransactions = filteredTransactions.filter((t) => !t.pending)
      }

      // Apply min_amount filter
      const minAmount = url.searchParams.get('min_amount')
      if (minAmount) {
        const min = parseFloat(minAmount)
        filteredTransactions = filteredTransactions.filter((t) => Math.abs(t.amount) >= min)
      }

      // Apply max_amount filter
      const maxAmount = url.searchParams.get('max_amount')
      if (maxAmount) {
        const max = parseFloat(maxAmount)
        filteredTransactions = filteredTransactions.filter((t) => Math.abs(t.amount) <= max)
      }

      route.fulfill({
        status: transactionsStatus,
        contentType: 'application/json',
        body: JSON.stringify({
          ...transactions,
          transactions: filteredTransactions,
          total_count: filteredTransactions.length,
        }),
      })
    } else {
      route.continue()
    }
  })

  // Mock accounts endpoint (wildcard to match query params)
  await page.route('**/api/v1/accounts*', (route) => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(accounts),
    })
  })

  // Mock Plaid categories endpoint (wildcard to match query params)
  await page.route('**/api/v1/categories/plaid*', (route) => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(plaidCategories),
    })
  })

  // Mock tags endpoint (wildcard to match query params)
  await page.route('**/api/v1/tags*', (route) => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(mockTags),
    })
  })

  // Mock categories endpoint (wildcard to match query params)
  await page.route('**/api/v1/categories*', (route) => {
    if (route.request().method() === 'GET') {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(mockCategories),
      })
    } else {
      route.continue()
    }
  })
}

// ============================================================================
// Non-Authenticated Tests
// ============================================================================

test.describe('Transaction Filtering - Protected Route', () => {
  // Override storageState to ensure no session is loaded for these tests
  test.use({ storageState: { cookies: [], origins: [] } })

  test('should redirect unauthenticated users away from transactions page', async ({
    page,
  }) => {
    // No Clerk auth token - this test verifies unauthenticated redirect behavior
    await page.goto('/transactions')
    await expect(page).not.toHaveURL(/\/transactions/, { timeout: 10000 })
  })
})

// ============================================================================
// Authenticated Tests
// ============================================================================

test.describe('Transaction Filtering - Authenticated User', () => {
  test.beforeEach(async ({ page }) => {
    // Session is loaded via storageState from auth.setup.ts
    await acceptCookieConsent(page)
  })

  // ==========================================================================
  // Filter Panel Tests
  // ==========================================================================

  test.describe('Filter Panel', () => {
    test('should show filter panel with expand toggle', async ({ page }) => {
      await mockFilteringAPIs(page)
      await page.goto('/transactions')
      await page.waitForLoadState('domcontentloaded')

      // Issue #615: Panel renamed to "Date & Status" (Account/Category/Type/Amount moved to table headers)
      await expect(page.getByText('Date & Status')).toBeVisible()
    })

    test('should expand filter panel when clicking toggle', async ({ page }) => {
      await mockFilteringAPIs(page)
      await page.goto('/transactions')
      await page.waitForLoadState('domcontentloaded')

      // Click to expand (Issue #615: renamed from "Filters")
      await page.getByText('Date & Status').click()

      // Filter controls should be visible (only Date Range and Status per Issue #615)
      await expect(page.getByText('Date Range')).toBeVisible()
      await expect(page.getByText('Status', { exact: true })).toBeVisible()
    })

    test('should collapse filter panel when clicking toggle again', async ({ page }) => {
      await mockFilteringAPIs(page)
      await page.goto('/transactions')
      await page.waitForLoadState('domcontentloaded')

      // Expand
      await page.getByText('Date & Status').click()
      await expect(page.getByText('Date Range')).toBeVisible()

      // Collapse
      await page.getByText('Date & Status').click()
      await expect(page.getByText('Date Range')).not.toBeVisible()
    })

    // SKIPPED: Amount type filter moved to inline table headers (Issue #615)
    test.skip('should show active filter count badge', async ({ page }) => {
      await mockFilteringAPIs(page)
      await page.goto('/transactions?amount_type=income')
      await page.waitForLoadState('networkidle')

      // Should show badge with count
      const badge = page.locator('text=Filters').locator('..').getByText('1')
      await expect(badge).toBeVisible()
    })
  })

  // ==========================================================================
  // Type Filter Tests (SKIPPED: Issue #615 - moved to inline table headers)
  // ==========================================================================

  test.describe.skip('Type Filter', () => {
    // SKIPPED: Income/Expenses filter moved from panel to inline table headers (Issue #615)
    // These tests need to be rewritten to interact with the table header dropdowns
    test('should filter to income only', async ({ page }) => {
      await mockFilteringAPIs(page)
      await page.goto('/transactions')
      await page.waitForLoadState('networkidle')

      // Expand filters
      await page.getByText('Filters').click()

      // Click Income button
      await page.getByRole('button', { name: 'Income' }).click()

      // Wait for filtered results
      await expect(page.getByText('Salary')).toBeVisible()
      await expect(page.getByText('Starbucks')).not.toBeVisible()
    })

    test('should filter to expenses only', async ({ page }) => {
      await mockFilteringAPIs(page)
      await page.goto('/transactions')
      await page.waitForLoadState('networkidle')

      // Expand filters
      await page.getByText('Filters').click()

      // Click Expenses button
      await page.getByRole('button', { name: 'Expenses' }).click()

      // Wait for filtered results
      await expect(page.getByText('Starbucks')).toBeVisible()
      await expect(page.getByText('Salary')).not.toBeVisible()
    })

    test('should show all when clicking All', async ({ page }) => {
      await mockFilteringAPIs(page)
      await page.goto('/transactions?amount_type=income')
      await page.waitForLoadState('networkidle')

      // Expand filters
      await page.getByText('Filters').click()

      // Click All button
      await page.getByRole('button', { name: 'All', exact: true }).first().click()

      // All transactions should be visible
      await expect(page.getByText('Starbucks')).toBeVisible()
      await expect(page.getByText('Salary')).toBeVisible()
    })
  })

  // ==========================================================================
  // Status Filter Tests
  // ==========================================================================

  test.describe('Status Filter', () => {
    test('should filter to pending only', async ({ page }) => {
      await mockFilteringAPIs(page)
      await page.goto('/transactions')
      await page.waitForLoadState('domcontentloaded')

      // Expand filters (Issue #615: renamed to "Date & Status")
      await page.getByText('Date & Status').click()

      // Click Pending button
      await page.getByRole('button', { name: 'Pending' }).click()

      // Wait for filtered results
      await expect(page.getByText('Amazon')).toBeVisible()
      await expect(page.getByText('Starbucks')).not.toBeVisible()
    })

    test('should filter to posted only', async ({ page }) => {
      await mockFilteringAPIs(page)
      await page.goto('/transactions')
      await page.waitForLoadState('domcontentloaded')

      // Expand filters (Issue #615: renamed to "Date & Status")
      await page.getByText('Date & Status').click()

      // Click Posted button
      await page.getByRole('button', { name: 'Posted' }).click()

      // Wait for filtered results
      await expect(page.getByText('Starbucks')).toBeVisible()
      await expect(page.getByText('Amazon')).not.toBeVisible()
    })
  })

  // ==========================================================================
  // URL State Sync Tests
  // ==========================================================================

  test.describe('URL State Sync', () => {
    test('should update URL when status filter is applied', async ({ page }) => {
      await mockFilteringAPIs(page)
      await page.goto('/transactions')
      await page.waitForLoadState('domcontentloaded')

      // Expand filters (Issue #615: renamed to "Date & Status")
      await page.getByText('Date & Status').click()

      // Apply status filter (Status filter remains in panel)
      await page.getByRole('button', { name: 'Posted' }).click()

      // URL should contain filter param
      await expect(page).toHaveURL(/pending=false/)
    })

    // SKIPPED: amount_type filter moved to inline table headers (Issue #615)
    test.skip('should restore filters from URL on page load', async ({ page }) => {
      await mockFilteringAPIs(page)
      await page.goto('/transactions?amount_type=income')
      await page.waitForLoadState('networkidle')

      // Should show only income transactions
      await expect(page.getByText('Salary')).toBeVisible()
      await expect(page.getByText('Starbucks')).not.toBeVisible()
    })

    test('should persist status filter after page reload', async ({ page }) => {
      await mockFilteringAPIs(page)
      await page.goto('/transactions')
      await page.waitForLoadState('domcontentloaded')

      // Expand and apply status filter (Issue #615: Type filter moved inline)
      await page.getByText('Date & Status').click()
      await page.getByRole('button', { name: 'Posted' }).click()

      // Reload page
      await page.reload()
      await page.waitForLoadState('domcontentloaded')

      // Filter should still be applied - Posted transactions visible
      await expect(page.getByText('Starbucks')).toBeVisible()
      // Pending transactions (Amazon) should be filtered out
      await expect(page.getByText('Amazon')).not.toBeVisible()
    })
  })

  // ==========================================================================
  // Clear Filters Tests
  // ==========================================================================

  test.describe('Clear Filters', () => {
    test('should show clear all button when filters are active', async ({ page }) => {
      await mockFilteringAPIs(page)
      // Use status filter which is still in panel (Issue #615: amount_type moved inline)
      await page.goto('/transactions?pending=false')
      await page.waitForLoadState('domcontentloaded')

      // Panel should show "Clear all" link when filters active and collapsed
      await expect(page.getByText(/clear all/i)).toBeVisible()
    })

    test('should clear all filters when clicking clear button', async ({ page }) => {
      await mockFilteringAPIs(page)
      // Use status filter which is still in panel (Issue #615: amount_type moved inline)
      await page.goto('/transactions?pending=false')
      await page.waitForLoadState('domcontentloaded')

      // Click clear
      await page.getByText(/clear all/i).first().click()

      // All transactions should be visible including pending
      await expect(page.getByText('Starbucks')).toBeVisible()
      await expect(page.getByText('Amazon')).toBeVisible() // Pending transaction

      // URL should be clean
      await expect(page).not.toHaveURL(/pending/)
    })
  })

  // ==========================================================================
  // Filter Pills Tests (SKIPPED: Issue #615 - Type filter moved to inline table headers)
  // ==========================================================================

  test.describe.skip('Filter Pills', () => {
    // SKIPPED: Type filter pills no longer exist - filter moved to inline table headers (Issue #615)
    // The panel now only shows Date and Status pills (tested via Status Filter tests)
    test('should show filter pill when filter is active and panel collapsed', async ({ page }) => {
      await mockFilteringAPIs(page)
      await page.goto('/transactions?amount_type=income')
      await page.waitForLoadState('networkidle')

      // Panel should be collapsed by default
      await expect(page.getByText('Date Range')).not.toBeVisible()

      // Filter pill should be visible
      await expect(page.getByText('Type:')).toBeVisible()
      await expect(page.getByText('Income')).toBeVisible()
    })

    test('should remove filter when clicking pill remove button', async ({ page }) => {
      await mockFilteringAPIs(page)
      await page.goto('/transactions?amount_type=income')
      await page.waitForLoadState('networkidle')

      // Find and click the remove button on the pill
      const pill = page.locator('text=Type:').locator('..')
      await pill.getByRole('button').click()

      // All transactions should be visible
      await expect(page.getByText('Starbucks')).toBeVisible()
      await expect(page.getByText('Salary')).toBeVisible()
    })
  })

  // ==========================================================================
  // Combined Filters Tests (SKIPPED: Issue #615 - Type filter moved to inline table headers)
  // ==========================================================================

  test.describe.skip('Combined Filters', () => {
    // SKIPPED: Expenses/Income filter moved to inline table headers (Issue #615)
    // Combined filter tests need rewriting to use Status + Date instead of Type + Status
    test('should apply multiple filters together (AND behavior)', async ({ page }) => {
      await mockFilteringAPIs(page)
      await page.goto('/transactions')
      await page.waitForLoadState('networkidle')

      // Expand filters
      await page.getByText('Filters').click()

      // Apply expense filter
      await page.getByRole('button', { name: 'Expenses' }).click()

      // Apply posted filter
      await page.getByRole('button', { name: 'Posted' }).click()

      // Should show only posted expenses
      await expect(page.getByText('Starbucks')).toBeVisible()
      await expect(page.getByText('Whole Foods')).toBeVisible()
      await expect(page.getByText('Salary')).not.toBeVisible() // income
      await expect(page.getByText('Amazon')).not.toBeVisible() // pending
    })
  })

  // ==========================================================================
  // Empty State Tests
  // ==========================================================================

  test.describe('Empty State', () => {
    test('should show empty state with filter message when no results', async ({ page }) => {
      await mockFilteringAPIs(page, {
        transactions: {
          transactions: [],
          total_count: 0,
          limit: 50,
          offset: 0,
          has_more: false,
        },
      })
      await page.goto('/transactions?amount_type=income')
      await page.waitForLoadState('domcontentloaded')

      // Should show empty state with filter hint
      await expect(page.getByText(/no transactions found/i)).toBeVisible()
      await expect(page.getByText(/adjusting your filters/i)).toBeVisible()
    })

    test('should show clear filters button in empty state', async ({ page }) => {
      await mockFilteringAPIs(page, {
        transactions: {
          transactions: [],
          total_count: 0,
          limit: 50,
          offset: 0,
          has_more: false,
        },
      })
      await page.goto('/transactions?amount_type=income')
      await page.waitForLoadState('domcontentloaded')

      // Should have clear filters button
      await expect(page.getByRole('button', { name: /clear all filters/i })).toBeVisible()
    })
  })

  // ==========================================================================
  // Date Preset Tests
  // ==========================================================================

  test.describe('Date Presets', () => {
    test('should show date preset options', async ({ page }) => {
      await mockFilteringAPIs(page)
      await page.goto('/transactions')
      await page.waitForLoadState('domcontentloaded')

      // Dismiss cookie consent if it reappears after navigation
      await acceptCookieConsent(page)

      // Expand filters (Issue #615: renamed to "Date & Status")
      await page.getByText('Date & Status').click()

      // Date presets should be visible (Issue #953: migrated to shared calendar-aligned presets)
      // ToggleGroup renders radio items (Radix single toggle group), not buttons
      await expect(page.getByRole('radio', { name: 'This Week' })).toBeVisible()
      await expect(page.getByRole('radio', { name: 'This Month' })).toBeVisible()
      await expect(page.getByRole('radio', { name: 'Last Month' })).toBeVisible()
      await expect(page.getByRole('radio', { name: 'Last 3 Months' })).toBeVisible()
      await expect(page.getByRole('radio', { name: 'All Time' })).toBeVisible()
      await expect(page.getByRole('radio', { name: 'Custom', exact: true })).toBeVisible()
    })

    test('should apply date preset and update URL', async ({ page }) => {
      await mockFilteringAPIs(page)
      await page.goto('/transactions')
      await page.waitForLoadState('domcontentloaded')

      // Dismiss cookie consent if it reappears after navigation
      await acceptCookieConsent(page)

      // Expand filters (Issue #615: renamed to "Date & Status")
      await page.getByText('Date & Status').click()

      // Click This Month preset (Issue #953: migrated from '30 days' to calendar-aligned presets)
      await page.getByRole('radio', { name: 'This Month' }).click()

      // URL should contain date params
      await expect(page).toHaveURL(/start_date=/)
      await expect(page).toHaveURL(/end_date=/)
    })

    test('should show custom date inputs when Custom is selected', async ({ page }) => {
      await mockFilteringAPIs(page)
      // Navigate with arbitrary dates that don't match any known preset.
      // inferPreset() returns 'custom' when start_date/end_date are set but don't match
      // a standard range, which causes DateRangePicker to render Start/End Date inputs.
      // Note: clicking the "Custom" ToggleGroupItem alone cannot trigger this — setDatePreset('custom')
      // in useTransactionFilters is intentionally a no-op (the user sets dates manually).
      await page.goto('/transactions?start_date=2020-01-01&end_date=2020-12-31')
      await expect(page.getByText('Starbucks')).toBeVisible()

      // Dismiss cookie consent if it reappears after navigation
      await acceptCookieConsent(page)

      // Expand filters (Issue #615: renamed to "Date & Status")
      await page.getByText('Date & Status').click()

      // Custom date inputs should appear because the inferred preset is 'custom'
      await expect(page.getByText('Start Date')).toBeVisible()
      await expect(page.getByText('End Date')).toBeVisible()
    })
  })

  // ==========================================================================
  // Transaction Count Display Tests
  // ==========================================================================

  test.describe('Transaction Count Display', () => {
    // SKIPPED: amount_type filter moved to inline table headers (Issue #615)
    test.skip('should show filtered count when filters active', async ({ page }) => {
      await mockFilteringAPIs(page)
      await page.goto('/transactions?amount_type=income')
      await page.waitForLoadState('networkidle')

      // Should show "Showing X of Y transactions" format
      await expect(page.getByText(/showing.*of.*transaction/i)).toBeVisible()
    })

    // SKIPPED: Transaction count display format may vary based on implementation
    test.skip('should show total count when no filters active', async ({ page }) => {
      await mockFilteringAPIs(page)
      await page.goto('/transactions')
      await page.waitForLoadState('networkidle')

      // Should show "X transactions total" format
      await expect(page.getByText(/4.*transactions.*total/i)).toBeVisible()
    })
  })

  // ==========================================================================
  // Performance Tests (SKIPPED: Issue #615 - Tests use Type filter moved to inline headers)
  // ==========================================================================

  test.describe.skip('Performance', () => {
    // SKIPPED: Performance tests rely on Type filter (Expenses) which moved to inline headers
    // These tests need rewriting to use Status filter or Date presets instead
    /**
     * Generate mock transactions for performance testing
     */
    function generateMockTransactions(count: number) {
      const transactions = []
      for (let i = 0; i < count; i++) {
        transactions.push({
          id: `txn-perf-${i}`,
          user_id: 'user-123',
          account_id: i % 2 === 0 ? 'acc-1' : 'acc-2',
          linked_item_id: 'item-1',
          source: 'plaid',
          external_transaction_id: `plaid-txn-${i}`,
          amount: i % 3 === 0 ? 100 + i : -(50 + i),
          date: `2025-${String((i % 12) + 1).padStart(2, '0')}-15`,
          name: `Transaction ${i}`,
          merchant_name: `Merchant ${i}`,
          pfc_primary: i % 2 === 0 ? 'FOOD_AND_DRINK' : 'GENERAL_MERCHANDISE',
          pending: i % 5 === 0,
          tags: [],
          created_at: '2025-01-01T00:00:00Z',
          updated_at: '2025-01-01T00:00:00Z',
        })
      }
      return transactions
    }

    test('should handle 1000 transactions with acceptable performance', async ({ page }) => {
      // Generate 1000 mock transactions
      const largeTransactionSet = generateMockTransactions(1000)

      // Mock API with large dataset
      await page.route('**/api/v1/transactions*', async (route) => {
        const url = new URL(route.request().url())
        const amountType = url.searchParams.get('amount_type')

        // Filter based on amount_type if provided
        let filtered = largeTransactionSet
        if (amountType === 'expense') {
          filtered = largeTransactionSet.filter((t) => t.amount < 0)
        } else if (amountType === 'income') {
          filtered = largeTransactionSet.filter((t) => t.amount > 0)
        }

        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            transactions: filtered.slice(0, 50), // Paginate to 50
            total_count: filtered.length,
            limit: 50,
            offset: 0,
            has_more: filtered.length > 50,
          }),
        })
      })

      // Mock other required APIs (wildcard to match query params)
      await page.route('**/api/v1/accounts*', async (route) => {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ accounts: mockAccounts }),
        })
      })

      await page.route('**/api/v1/categories/plaid*', async (route) => {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ categories: mockCategories }),
        })
      })

      await page.route('**/api/v1/tags*', async (route) => {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ tags: [] }),
        })
      })

      // Navigate to transactions page
      await page.goto('/transactions')
      await page.waitForLoadState('networkidle')

      // Verify initial load shows 1000 transactions
      await expect(page.getByText('1000 transactions total')).toBeVisible()

      // Measure filter application time
      const filterStart = Date.now()

      // Expand filters and click Expenses
      await page.getByText('Filters').click()
      await page.getByRole('button', { name: 'Expenses' }).click()

      // Wait for filtered response
      await page.waitForResponse(
        (response) =>
          response.url().includes('/api/v1/transactions') &&
          response.url().includes('amount_type=expense')
      )

      const filterDuration = Date.now() - filterStart

      // Verify filter was applied (should show "Showing X of Y transactions")
      await expect(page.getByText(/showing.*of.*transaction/i)).toBeVisible()

      // Log performance metric (useful for debugging)
      console.log(`Filter application took ${filterDuration}ms`)

      // Performance assertion: Filter should complete within 500ms
      // (relaxed from 100ms to account for network simulation and rendering)
      expect(filterDuration).toBeLessThan(500)
    })

    test('should render initial page with 1000 transactions within performance budget', async ({
      page,
    }) => {
      // Generate 1000 mock transactions
      const largeTransactionSet = generateMockTransactions(1000)

      // Mock API with large dataset
      await page.route('**/api/v1/transactions*', async (route) => {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            transactions: largeTransactionSet.slice(0, 50),
            total_count: 1000,
            limit: 50,
            offset: 0,
            has_more: true,
          }),
        })
      })

      // Mock other required APIs (wildcard to match query params)
      await page.route('**/api/v1/accounts*', async (route) => {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ accounts: mockAccounts }),
        })
      })

      await page.route('**/api/v1/categories/plaid*', async (route) => {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ categories: mockCategories }),
        })
      })

      await page.route('**/api/v1/tags*', async (route) => {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ tags: [] }),
        })
      })

      // Measure page load time
      const loadStart = Date.now()

      await page.goto('/transactions')
      await page.waitForLoadState('networkidle')

      // Wait for transactions to render
      await expect(page.getByText('1000 transactions total')).toBeVisible()

      const loadDuration = Date.now() - loadStart

      // Log performance metric
      console.log(`Initial page load with 1000 transactions took ${loadDuration}ms`)

      // Page should load within 2000ms (generous budget for CI environments)
      expect(loadDuration).toBeLessThan(2000)
    })
  })
})
