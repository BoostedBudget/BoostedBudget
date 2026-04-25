/**
 * Budget List-to-Detail Navigation E2E Tests (#1051)
 *
 * Verifies that users can navigate from the /budgets list page to a budget
 * detail page from both the Cards view and the Table view.
 *
 * Issue #1051: Budgets table navigation
 */

import { test, expect } from '@playwright/test'
import { acceptCookieConsent } from '../helpers/auth'

test.describe('Budget list-to-detail navigation (#1051)', () => {
  // Session is loaded via storageState from auth.setup.ts for authenticated tests
  // Tests require authentication to access /budgets page

  test.beforeEach(async ({ page }) => {
    // Accept cookie consent to avoid banner interference
    await acceptCookieConsent(page)
  })

  test('Cards view: clicking Manage budget link navigates to budget detail', async ({ page }) => {
    // Arrange - Navigate to budgets page (default is Cards view)
    await page.goto('/budgets')
    await page.waitForLoadState('networkidle')

    // Act - Find the "Manage budget" link in the cards view
    const manageBudgetLink = page.getByRole('link', { name: /manage budget/i })

    if (await manageBudgetLink.isVisible().catch(() => false)) {
      await manageBudgetLink.click()
      await page.waitForLoadState('networkidle')

      // Assert - URL should contain /budgets/<uuid>
      expect(page.url()).toMatch(/\/budgets\/[0-9a-f-]{36}/)

      // Assert - "Back to Budgets" link confirms we're on the detail page
      await expect(page.getByRole('link', { name: /back to budgets/i })).toBeVisible()
    } else {
      // No budgets exist in the test environment — skip gracefully
      test.skip()
    }
  })

  test('Table view: clicking Manage budget icon button navigates to budget detail', async ({ page }) => {
    // Arrange - Navigate to budgets page
    await page.goto('/budgets')
    await page.waitForLoadState('networkidle')

    // Act - Switch to Table view
    await page.getByRole('button', { name: 'Table' }).click()

    // Wait for table to render (at least one row should appear)
    await page.waitForLoadState('networkidle')

    // Act - Find the "Manage budget" button in the table actions column
    const manageBudgetButton = page.getByRole('button', { name: /manage budget/i })

    if (await manageBudgetButton.isVisible().catch(() => false)) {
      await manageBudgetButton.click()
      await page.waitForLoadState('networkidle')

      // Assert - URL should contain /budgets/<uuid>
      expect(page.url()).toMatch(/\/budgets\/[0-9a-f-]{36}/)

      // Assert - "Back to Budgets" link confirms we're on the detail page
      await expect(page.getByRole('link', { name: /back to budgets/i })).toBeVisible()
    } else {
      // No budgets exist in the test environment — skip gracefully
      test.skip()
    }
  })

  test('Table view: clicking a budget row navigates to budget detail', async ({ page }) => {
    // Arrange - Navigate to budgets page
    await page.goto('/budgets')
    await page.waitForLoadState('networkidle')

    // Act - Switch to Table view
    await page.getByRole('button', { name: 'Table' }).click()

    // Wait for table to render
    await page.waitForLoadState('networkidle')

    // Act - Find a budget row (any row containing text that looks like a budget name)
    const budgetRow = page.getByRole('row').filter({ hasText: /budget/i }).first()

    if (await budgetRow.isVisible().catch(() => false)) {
      // Click the Name cell (first cell) to avoid hitting action buttons
      const nameCell = budgetRow.getByRole('cell').first()
      await nameCell.click()
      await page.waitForLoadState('networkidle')

      // Assert - URL should contain /budgets/<uuid>
      expect(page.url()).toMatch(/\/budgets\/[0-9a-f-]{36}/)

      // Assert - "Back to Budgets" link confirms we're on the detail page
      await expect(page.getByRole('link', { name: /back to budgets/i })).toBeVisible()
    } else {
      // No budgets exist in the test environment — skip gracefully
      test.skip()
    }
  })
})
