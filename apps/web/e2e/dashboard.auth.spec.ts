/**
 * Dashboard E2E Tests
 *
 * Tests critical user flows for the Balance Tracking Dashboard,
 * focusing on integration points (auth, API, interactions) rather
 * than component logic (covered in unit tests).
 *
 * Issue #353: Balance Tracking Dashboard
 */

import { test, expect, type Page } from '@playwright/test'
import { acceptCookieConsent } from './helpers/auth'
import { mockAuthDependencies } from './helpers/api-mocks'

// Mock data for dashboard APIs
const mockBalanceSummary = {
  as_of_date: '2025-11-15',
  total_balance: 15000.5,
  by_type: {
    depository: {
      balance: 5000.0,
      account_count: 2,
      accounts: [
        {
          id: 'acc-1',
          name: 'Checking Account',
          mask: '1234',
          balance: 3000.0,
          institution_name: 'Chase',
        },
        {
          id: 'acc-2',
          name: 'Savings Account',
          mask: '5678',
          balance: 2000.0,
          institution_name: 'Chase',
        },
      ],
    },
    credit: {
      balance: -2000.0,
      account_count: 1,
      accounts: [
        {
          id: 'acc-3',
          name: 'Credit Card',
          mask: '9999',
          balance: -2000.0,
          institution_name: 'AmEx',
        },
      ],
    },
    investment: {
      balance: 12000.5,
      account_count: 1,
      accounts: [
        {
          id: 'acc-4',
          name: 'Investment Account',
          mask: '7777',
          balance: 12000.5,
          institution_name: 'Vanguard',
        },
      ],
    },
  },
  period_comparison: {
    previous_balance: 14000.0,
    change_amount: 1000.5,
    change_percent: 7.15,
  },
}

const mockBalanceHistory = {
  start_date: '2025-10-16',
  end_date: '2025-11-15',
  granularity: 'daily',
  snapshots: [
    {
      date: '2025-10-16',
      balance: 14000.0,
      by_type: {
        depository: 4500.0,
        credit: -1500.0,
        investment: 11000.0,
      },
    },
    {
      date: '2025-10-17',
      balance: 14200.0,
      by_type: {
        depository: 4600.0,
        credit: -1600.0,
        investment: 11200.0,
      },
    },
    {
      date: '2025-10-18',
      balance: 14100.0,
      by_type: {
        depository: 4550.0,
        credit: -1550.0,
        investment: 11100.0,
      },
    },
    {
      date: '2025-11-14',
      balance: 14800.0,
      by_type: {
        depository: 4900.0,
        credit: -1900.0,
        investment: 11800.0,
      },
    },
    {
      date: '2025-11-15',
      balance: 15000.5,
      by_type: {
        depository: 5000.0,
        credit: -2000.0,
        investment: 12000.5,
      },
    },
  ],
}

// Mock account types - Format must match AccountTypesResponse interface (Issue #582)
const mockAccountTypes = {
  types: [
    {
      key: 'depository',
      label: 'Checking & Savings',
      count: 2,
      total_balance: 5000.0,
    },
    {
      key: 'credit',
      label: 'Credit Cards',
      count: 1,
      total_balance: -2000.0,
    },
    {
      key: 'investment',
      label: 'Investments',
      count: 1,
      total_balance: 12000.5,
    },
  ],
}

/**
 * Helper function to mock dashboard API endpoints
 *
 * @param page - Playwright Page object
 * @param overrides - Optional overrides for default mock data
 */
async function mockDashboardAPIs(
  page: Page,
  overrides: {
    summary?: typeof mockBalanceSummary | null
    history?: typeof mockBalanceHistory | null
    accountTypes?: typeof mockAccountTypes | null
    summaryStatus?: number
    historyStatus?: number
    accountTypesStatus?: number
  } = {}
) {
  const {
    summary = mockBalanceSummary,
    history = mockBalanceHistory,
    accountTypes = mockAccountTypes,
    summaryStatus = 200,
    historyStatus = 200,
    accountTypesStatus = 200,
  } = overrides

  // Mock auth-dependent endpoints (PremiumRoute blocks rendering until /user/profile resolves)
  await mockAuthDependencies(page)

  // Mock balance summary endpoint
  await page.route('**/api/v1/dashboard/summary**', (route) => {
    route.fulfill({
      status: summaryStatus,
      contentType: 'application/json',
      body: JSON.stringify(summary),
    })
  })

  // Mock balance history endpoint
  await page.route('**/api/v1/dashboard/history**', (route) => {
    route.fulfill({
      status: historyStatus,
      contentType: 'application/json',
      body: JSON.stringify(history),
    })
  })

  // Mock account types endpoint
  await page.route('**/api/v1/dashboard/account-types**', (route) => {
    route.fulfill({
      status: accountTypesStatus,
      contentType: 'application/json',
      body: JSON.stringify(accountTypes),
    })
  })

  // Mock dashboard preferences endpoint (CORRECT FORMAT - Issue #582)
  // Format must match DashboardPreferencesResponse interface from types/index.ts
  await page.route('**/api/v1/dashboard/preferences*', (route) => {
    if (route.request().method() === 'GET') {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          layout: {
            widgets: [
              { i: 'balance-overview-1', type: 'balance-overview', x: 0, y: 0, w: 12, h: 2, minW: 4, minH: 2 },
              { i: 'balance-history-1', type: 'balance-history', x: 0, y: 2, w: 12, h: 4, minW: 4, minH: 4 },
              { i: 'account-cards-1', type: 'account-cards', x: 0, y: 6, w: 12, h: 3, minW: 4, minH: 3 },
            ],
            version: 4,
          },
          updated_at: '2025-01-01T00:00:00Z',
        }),
      })
    } else {
      // Handle PUT for saving preferences
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true }),
      })
    }
  })

  // Mock default layout endpoint (CORRECT FORMAT - Issue #582)
  // Format must match DashboardLayout interface from types/index.ts
  await page.route('**/api/v1/dashboard/default-layout*', (route) => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        widgets: [
          { i: 'balance-overview-1', type: 'balance-overview', x: 0, y: 0, w: 12, h: 2, minW: 4, minH: 2 },
          { i: 'balance-history-1', type: 'balance-history', x: 0, y: 2, w: 12, h: 4, minW: 4, minH: 4 },
          { i: 'account-cards-1', type: 'account-cards', x: 0, y: 6, w: 12, h: 3, minW: 4, minH: 3 },
        ],
        version: 4,
      }),
    })
  })
}

// ============================================================================
// Non-Authenticated Tests (Always Run)
// ============================================================================

test.describe('Dashboard - Protected Route', () => {
  // Override storageState to ensure no session is loaded for these tests
  test.use({ storageState: { cookies: [], origins: [] } })

  test('should redirect unauthenticated users away from dashboard', async ({
    page,
  }) => {
    // No Clerk auth token - this test verifies unauthenticated redirect behavior
    // Act - Navigate to dashboard without authentication
    await page.goto('/dashboard')

    // Assert - Should redirect away from dashboard (to home or Clerk)
    // Using Playwright's auto-retry assertion to handle redirect timing
    await expect(page).not.toHaveURL(/\/dashboard/, { timeout: 10000 })
  })

  test('should show dashboard in navigation only when authenticated', async ({
    page,
  }) => {
    // No Clerk auth token - this test verifies unauthenticated state
    // Arrange - Start unauthenticated
    await page.goto('/')
    await page.waitForLoadState('networkidle')

    // Assert - Dashboard link should not be visible
    const dashboardLinkBefore = page.getByRole('link', { name: /dashboard/i })
    await expect(dashboardLinkBefore).not.toBeVisible()

    // Note: Testing authenticated state requires Clerk credentials (see authenticated tests below)
  })
})

// ============================================================================
// Authenticated Tests (Conditional Skip Pattern)
// ============================================================================

test.describe('Dashboard - Authenticated User', () => {
  test.beforeEach(async ({ page }) => {
    // Session is loaded via storageState from auth.setup.ts
    await acceptCookieConsent(page)
  })

  // ==========================================================================
  // Happy Path Tests
  // ==========================================================================

  // SKIPPED: Issue #615 - Dashboard API mocking and selectors need verification
  test.describe.skip('Happy Path', () => {
    test('should display dashboard with balance overview, chart, and account cards', async ({
      page,
    }) => {
      // Arrange - Mock dashboard APIs
      await mockDashboardAPIs(page)

      // Act - Navigate to dashboard
      await page.goto('/dashboard')
      await page.waitForLoadState('networkidle')

      // Assert - Balance Overview visible
      await expect(page.getByText('Total Balance')).toBeVisible()
      await expect(page.getByText('$15,000.50')).toBeVisible()

      // Assert - Balance History Chart visible
      await expect(page.getByText('Balance History')).toBeVisible()

      // Assert - Account cards visible (checking for account type labels)
      await expect(
        page.getByText('Checking & Savings', { exact: false })
      ).toBeVisible()
      await expect(
        page.getByText('Credit Cards', { exact: false })
      ).toBeVisible()
      await expect(
        page.getByText('Investments', { exact: false })
      ).toBeVisible()
    })
  })

  // ==========================================================================
  // Interactive Features Tests
  // ==========================================================================

  // SKIPPED: Issue #615 - Dashboard interactive features need selector verification
  test.describe.skip('Interactive Features', () => {
    test('should update chart when timeframe filter changes to 90 days', async ({
      page,
    }) => {
      // Arrange - Mock dashboard APIs
      await mockDashboardAPIs(page)
      await page.goto('/dashboard')
      await page.waitForLoadState('networkidle')

      // Re-mock history API to verify filter parameter
      await page.route('**/api/v1/dashboard/history**', (route) => {
        // API call with timeframe parameter (implementation verified by route interception)
        route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(mockBalanceHistory),
        })
      })

      // Act - Click 90 days filter button
      const ninetyDaysButton = page.getByRole('button', { name: /90.*days/i })
      await ninetyDaysButton.click()
      await page.waitForTimeout(500) // Wait for API call

      // Assert - Button should be selected (uses Tailwind bg-blue-600 for selected state)
      await expect(ninetyDaysButton).toHaveClass(/bg-blue-600/)
    })

    test('should filter by account type when filter checkbox clicked', async ({
      page,
    }) => {
      // Arrange - Mock dashboard APIs
      await mockDashboardAPIs(page)
      await page.goto('/dashboard')
      await page.waitForLoadState('networkidle')

      // Act - Click account type filter checkbox (AccountTypeFilter uses checkboxes, not buttons)
      // Find the checkbox by its label text
      const depositoryCheckbox = page.getByRole('checkbox', {
        name: /checking.*savings/i,
      })
      await depositoryCheckbox.click()
      await page.waitForTimeout(500)

      // Assert - Checkbox should be checked
      await expect(depositoryCheckbox).toBeChecked()

      // Assert - Chart should update (verify chart is still visible)
      await expect(page.getByText('Balance History')).toBeVisible()
    })

    test('should toggle chart between total balance and type breakdown', async ({
      page,
    }) => {
      // Arrange - Mock dashboard APIs
      await mockDashboardAPIs(page)
      await page.goto('/dashboard')
      await page.waitForLoadState('networkidle')

      // Act - Find and click the breakdown toggle button
      const toggleButton = page.getByRole('button', {
        name: /show breakdown|show total/i,
      })
      const initialText = await toggleButton.textContent()

      await toggleButton.click()
      await page.waitForTimeout(500)

      // Assert - Button text should change
      const newText = await toggleButton.textContent()
      expect(newText).not.toBe(initialText)

      // Assert - Chart should still be visible
      await expect(page.getByText('Balance History')).toBeVisible()
    })

    test('should apply multiple filters together (timeframe + account type)', async ({
      page,
    }) => {
      // Arrange - Mock dashboard APIs
      await mockDashboardAPIs(page)
      await page.goto('/dashboard')
      await page.waitForLoadState('networkidle')

      // Act - Apply account type filter (uses checkbox)
      const depositoryCheckbox = page.getByRole('checkbox', {
        name: /checking.*savings/i,
      })
      await depositoryCheckbox.click()
      await page.waitForTimeout(300)

      // Act - Apply timeframe filter (uses button)
      const ninetyDaysButton = page.getByRole('button', { name: /90.*days/i })
      await ninetyDaysButton.click()
      await page.waitForTimeout(300)

      // Assert - Both filters should be active
      await expect(depositoryCheckbox).toBeChecked()
      await expect(ninetyDaysButton).toHaveClass(/bg-blue-600/)

      // Assert - Dashboard should still display data
      await expect(page.getByText('Total Balance')).toBeVisible()
      await expect(page.getByText('Balance History')).toBeVisible()
    })
  })

  // ==========================================================================
  // Error States Tests
  // ==========================================================================

  // SKIPPED: Issue #615 - Dashboard error state UI needs verification
  test.describe.skip('Error States', () => {
    test('should show error message and allow retry when API fails', async ({
      page,
    }) => {
      // Arrange - Mock dashboard APIs to fail
      await mockDashboardAPIs(page, {
        summaryStatus: 500,
        historyStatus: 500,
        accountTypesStatus: 500,
      })

      // Act - Navigate to dashboard
      await page.goto('/dashboard')
      await page.waitForLoadState('networkidle')

      // Assert - Error message should be visible
      await expect(
        page.getByText(/failed to load|error|something went wrong/i)
      ).toBeVisible()

      // Assert - Retry button should be visible
      const retryButton = page.getByRole('button', { name: /try again|retry/i })
      await expect(retryButton).toBeVisible()

      // Act - Mock APIs to succeed and click retry
      await mockDashboardAPIs(page) // Success mocks
      await retryButton.click()
      await page.waitForLoadState('networkidle')

      // Assert - Dashboard should load successfully
      await expect(page.getByText('Total Balance')).toBeVisible()
      await expect(page.getByText('$15,000.50')).toBeVisible()
    })
  })

  // ==========================================================================
  // Empty States Tests
  // ==========================================================================

  test.describe('Empty States', () => {
    // TODO: This test requires investigation - mocking empty data structures
    // causes the ErrorBoundary to trigger. Options to fix:
    // 1. Use a real test user with no accounts (requires test data setup)
    // 2. Fix widget components to handle edge cases in mocked data
    // 3. Mock at the component level instead of API level
    // Skipping for now as the main PR (#691) is about navigation fixes.
    test.fixme('should show "No Data Available" message for new users', async ({
      page,
    }) => {
      // Arrange - Mock dashboard APIs with empty data structures
      await mockDashboardAPIs(page, {
        summary: { as_of_date: '2025-01-01', total_balance: 0, by_type: {} },
        history: { start_date: '2025-01-01', end_date: '2025-01-01', granularity: 'daily', snapshots: [] },
        accountTypes: { types: [] },
      })

      // Act - Navigate to dashboard
      await page.goto('/dashboard')
      await page.waitForLoadState('networkidle')

      // Assert - Empty state message should be visible
      await expect(
        page.getByText(/no (data|history|account|accounts).*available|no accounts found|connect.*account/i)
      ).toBeVisible({ timeout: 10000 })

      // Assert - Should suggest connecting an account
      const connectLink = page.getByRole('link', {
        name: /connect|link account/i,
      })
      if ((await connectLink.count()) > 0) {
        await expect(connectLink).toBeVisible()
      }
    })
  })
})
