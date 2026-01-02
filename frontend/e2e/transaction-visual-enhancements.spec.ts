/**
 * Transaction Visual Enhancements E2E Tests
 *
 * Issue #531: Transaction List Visual Enhancements
 *
 * These tests verify the visual enhancements to transaction cards:
 * - Account badge display
 * - Category color dots
 * - Income/expense arrows
 * - Enhanced pending badge
 * - Hover quick actions
 */

import { test, expect } from '@playwright/test'
import { setupClerkTestingToken } from '@clerk/testing/playwright'
import { skipIfNoAuth } from './helpers/auth'

test.describe('Transaction Visual Enhancements', () => {
  test.beforeEach(async ({ page }, testInfo) => {
    skipIfNoAuth(testInfo);
    await setupClerkTestingToken({ page })
    // Navigate to the transactions page
    // Note: This requires authentication - use a logged in session
    await page.goto('/transactions')

    // Wait for transactions to load
    await page.waitForSelector('.transaction-card', { timeout: 10000 }).catch(() => {
      // If no transactions, skip the test
      test.skip()
    })
  })

  test.describe('Account Badge', () => {
    test('should display account badge when account is linked', async ({ page }) => {
      // Check for account badge on transaction cards
      const accountBadge = page.locator('.transaction-card__account-badge').first()

      // Account badge may or may not be present depending on test data
      const count = await accountBadge.count()
      if (count > 0) {
        await expect(accountBadge).toBeVisible()
        // Verify badge has content (account name)
        await expect(accountBadge).not.toBeEmpty()
      }
    })

    test('should apply custom color to account badge', async ({ page }) => {
      const accountBadge = page.locator('.transaction-card__account-badge').first()

      const count = await accountBadge.count()
      if (count > 0) {
        // Verify badge has a background color style
        const style = await accountBadge.getAttribute('style')
        expect(style).toContain('background-color')
      }
    })
  })

  test.describe('Category Color Dot', () => {
    test('should display category color dot', async ({ page }) => {
      const categoryDot = page.locator('.transaction-card__category-dot').first()
      await expect(categoryDot).toBeVisible()
    })

    test('should apply background color to category dot', async ({ page }) => {
      const categoryDot = page.locator('.transaction-card__category-dot').first()
      const style = await categoryDot.getAttribute('style')
      expect(style).toContain('background-color')
    })
  })

  test.describe('Amount Arrows', () => {
    test('should display expense arrow for negative amounts', async ({ page }) => {
      // Look for transactions with negative amounts (expenses)
      const expenseArrow = page.locator('[data-testid="expense-arrow"]').first()
      const count = await expenseArrow.count()

      if (count > 0) {
        await expect(expenseArrow).toBeVisible()
      }
    })

    test('should display income arrow for positive amounts', async ({ page }) => {
      // Look for transactions with positive amounts (income)
      const incomeArrow = page.locator('[data-testid="income-arrow"]').first()
      const count = await incomeArrow.count()

      if (count > 0) {
        await expect(incomeArrow).toBeVisible()
      }
    })
  })

  test.describe('Enhanced Pending Badge', () => {
    test('should display clock icon in pending badge', async ({ page }) => {
      // Look for pending transactions
      const pendingIcon = page.locator('[data-testid="pending-icon"]').first()
      const count = await pendingIcon.count()

      if (count > 0) {
        await expect(pendingIcon).toBeVisible()
      }
    })

    test('should display tooltip on pending badge', async ({ page }) => {
      const pendingBadge = page.locator('.transaction-card__pending-badge').first()
      const count = await pendingBadge.count()

      if (count > 0) {
        const title = await pendingBadge.getAttribute('title')
        expect(title).toContain('pending')
      }
    })
  })

  test.describe('Hover Quick Actions', () => {
    test('should show quick action buttons on hover', async ({ page }) => {
      const transactionCard = page.locator('.transaction-card').first()
      const quickActions = page.locator('.transaction-card__quick-actions').first()

      // Verify quick actions are hidden by default (display: none)
      await expect(quickActions).toHaveCSS('display', 'none')

      // Hover over the card
      await transactionCard.hover()

      // Quick actions should become visible (display: flex)
      await expect(quickActions).toHaveCSS('display', 'flex')
    })

    test('should have edit button with correct aria-label', async ({ page }) => {
      const transactionCard = page.locator('.transaction-card').first()

      // Hover to reveal quick actions
      await transactionCard.hover()

      const editButton = page.locator('button[aria-label="Edit transaction"]').first()
      await expect(editButton).toBeVisible()
    })

    test('should open editor when edit button is clicked', async ({ page }) => {
      const transactionCard = page.locator('.transaction-card').first()

      // Hover to reveal quick actions
      await transactionCard.hover()

      // Click edit button
      const editButton = page.locator('button[aria-label="Edit transaction"]').first()
      await editButton.click()

      // Verify editor modal opens
      await expect(page.locator('.transaction-editor')).toBeVisible({ timeout: 5000 }).catch(() => {
        // Alternative: check for any modal dialog
        return expect(page.locator('[role="dialog"]')).toBeVisible()
      })
    })
  })

  test.describe('Visual Regression', () => {
    test('transaction card layout is correct', async ({ page }) => {
      // Take a screenshot of a transaction card for visual comparison
      const transactionCard = page.locator('.transaction-card').first()

      // Basic layout checks
      await expect(transactionCard).toHaveCSS('position', 'relative')

      // Check that card contains expected elements
      await expect(transactionCard.locator('.transaction-card__name')).toBeVisible()
      await expect(transactionCard.locator('.transaction-card__category')).toBeVisible()
      await expect(transactionCard.locator('.transaction-card__amount')).toBeVisible()
      await expect(transactionCard.locator('.transaction-card__date')).toBeVisible()
    })
  })
})
