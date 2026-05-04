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
import { skipIfBackendDown } from './helpers/health'

test.describe('Transaction Visual Enhancements', () => {
  test.beforeEach(async ({ page }, testInfo) => {
    // TransactionCard is not rendered on /transactions (table view only). All tests
    // here are fixme until card view is integrated into the transactions page. #531
    test.fixme(true, 'TransactionCard not integrated into /transactions (table-view only) — re-enable when card view ships. See #531')
    await skipIfBackendDown(page, testInfo)
    await setupClerkTestingToken({ page })
    await page.goto('/transactions')
    await expect(page.locator('article[data-transaction-id]').first()).toBeVisible({ timeout: 10000 })
  })

  test.describe('Account Badge', () => {
    test('should display account badge when account is linked', async ({ page }) => {
      // Check for account badge on transaction cards
      const accountBadge = page.locator('.transaction-card__account-badge').first()

      await expect(accountBadge).toBeVisible()
      await expect(accountBadge).not.toBeEmpty()
    })

    test('should apply custom color to account badge', async ({ page }) => {
      const accountBadge = page.locator('.transaction-card__account-badge').first()

      await expect(accountBadge).toBeVisible()
      const style = await accountBadge.getAttribute('style')
      expect(style).toContain('background-color')
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
      const expenseArrow = page.locator('[data-testid="expense-arrow"]').first()
      await expect(expenseArrow).toBeVisible()
    })

    test('should display income arrow for positive amounts', async ({ page }) => {
      const incomeArrow = page.locator('[data-testid="income-arrow"]').first()
      await expect(incomeArrow).toBeVisible()
    })
  })

  test.describe('Enhanced Pending Badge', () => {
    test('should display clock icon in pending badge', async ({ page }) => {
      const pendingIcon = page.locator('[data-testid="pending-icon"]').first()
      await expect(pendingIcon).toBeVisible()
    })

    test('should display tooltip on pending badge', async ({ page }) => {
      const pendingBadge = page.locator('.transaction-card__pending-badge').first()
      const title = await pendingBadge.getAttribute('title')
      expect(title).toContain('pending')
    })
  })

  test.describe('Hover Quick Actions', () => {
    test('should show quick action buttons on hover', async ({ page }) => {
      const transactionCard = page.locator('article[data-transaction-id]').first()
      const quickActions = page.locator('.transaction-card__quick-actions').first()

      // Verify quick actions are hidden by default (display: none)
      await expect(quickActions).toHaveCSS('display', 'none')

      // Hover over the card
      await transactionCard.hover()

      // Quick actions should become visible (display: flex)
      await expect(quickActions).toHaveCSS('display', 'flex')
    })

    test('should have edit button with correct aria-label', async ({ page }) => {
      const transactionCard = page.locator('article[data-transaction-id]').first()

      // Hover to reveal quick actions
      await transactionCard.hover()

      const editButton = page.locator('button[aria-label="Edit transaction"]').first()
      await expect(editButton).toBeVisible()
    })

    test('should open editor when edit button is clicked', async ({ page }) => {
      const transactionCard = page.locator('article[data-transaction-id]').first()

      // Hover to reveal quick actions
      await transactionCard.hover()

      // Click edit button
      const editButton = page.locator('button[aria-label="Edit transaction"]').first()
      await editButton.click()

      await expect(page.locator('[role="dialog"]')).toBeVisible({ timeout: 5000 })
    })
  })

  test.describe('Visual Regression', () => {
    test.fixme(true, 'transaction-card__name/__amount/__date CSS classes not added to TransactionCard — see #531')
    test('transaction card layout is correct', async ({ page }) => {
      const transactionCard = page.locator('article[data-transaction-id]').first()
      await expect(transactionCard).toHaveCSS('position', 'relative')
      await expect(transactionCard.locator('.transaction-card__name')).toBeVisible()
      await expect(transactionCard.locator('.transaction-card__category')).toBeVisible()
      await expect(transactionCard.locator('.transaction-card__amount')).toBeVisible()
      await expect(transactionCard.locator('.transaction-card__date')).toBeVisible()
    })
  })
})
