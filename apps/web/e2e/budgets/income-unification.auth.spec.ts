import { test, expect, type Page } from '@playwright/test'
import { acceptCookieConsent } from '../helpers/auth'
import { ensureBudgetExists } from '../helpers/api/budgets'

// Matches both "Create budget" (header) and "Create your first budget" (empty state)
const openCreateBudgetDialog = (page: Page) =>
  page.getByRole('button', { name: /create.*budget/i }).first().click()

test.describe('Budget income unification (#1048)', () => {
  test.beforeAll(async ({ browser }) => {
    const context = await browser.newContext({ storageState: 'e2e/.auth/user.json' })
    const page = await context.newPage()
    await page.goto('/budgets')
    await page.waitForLoadState('networkidle')
    await page
      .waitForFunction(
        () =>
          !!(window as unknown as { Clerk?: { session?: { id?: string } } }).Clerk?.session?.id,
        { timeout: 15000 }
      )
      .catch(() => {})
    await ensureBudgetExists(page)
    await context.close()
  })

  test.beforeEach(async ({ page }) => {
    await acceptCookieConsent(page)
  })

  test('Create Budget dialog has no tabs', async ({ page }) => {
    await page.goto('/budgets')
    await page.waitForLoadState('networkidle')

    await openCreateBudgetDialog(page)

    await expect(page.getByRole('dialog', { name: /create budget/i })).toBeVisible()
    await expect(page.getByRole('tab')).toHaveCount(0)
    await expect(page.getByLabel(/expected income/i)).toBeVisible()
  })

  test('Create budget with expected income and tracking source', async ({ page }) => {
    await page.goto('/budgets')
    await page.waitForLoadState('networkidle')

    await openCreateBudgetDialog(page)

    await expect(page.getByRole('dialog', { name: /create budget/i })).toBeVisible()

    await page.getByLabel(/budget name|name/i).fill('E2E Income Test Budget')
    await page.getByLabel(/expected income/i).fill('5000')

    const trackingButton = page.getByRole('button', { name: /track.*income|tracking source/i })
    if (await trackingButton.isVisible().catch(() => false)) {
      await trackingButton.click()
      await page.waitForTimeout(300)
    }

    await page.getByRole('button', { name: /^create budget$/i }).click()

    await page.waitForLoadState('networkidle')
    await expect(page.getByText('E2E Income Test Budget')).toBeVisible()
  })

  test('Budget list shows budget cards with income progress', async ({ page }) => {
    await page.goto('/budgets')
    await page.waitForLoadState('networkidle')

    const firstBudgetCard = page.locator('[data-testid="budget-card"]').first()
    await expect(firstBudgetCard).toBeVisible()

    await expect(firstBudgetCard.getByText('Income', { exact: true })).toBeVisible()
  })

  test('Budget detail page displays Expected Income information', async ({ page }) => {
    await page.goto('/budgets')
    await page.waitForLoadState('networkidle')

    const budgetLink = page.locator('a, button').filter({ hasText: /manage|view|edit/i }).first()
    await expect(budgetLink).toBeVisible()
    await budgetLink.click()
    await page.waitForLoadState('networkidle')

    await expect(page.getByText(/expected income/i)).toBeVisible()
  })

  test('Soft warning when allocations exceed expected income', async ({ page }) => {
    await page.goto('/budgets')
    await page.waitForLoadState('networkidle')

    const editButton = page.locator('button, a').filter({ hasText: /edit|manage budget|allocate/i }).first()
    await expect(editButton).toBeVisible()
    await editButton.click()
    await page.waitForLoadState('networkidle')

    const addCategoryButton = page.getByRole('button', { name: /add category/i })
    await expect(addCategoryButton).toBeVisible()
    await addCategoryButton.click()
    await page.waitForTimeout(500)

    const amountInput = page.locator('input[type="number"]').first()
    await expect(amountInput).toBeVisible()
    await amountInput.fill('9999')
    await page.waitForTimeout(300)

    const warningLocator = page
      .locator('[role="status"], [role="alert"], .warning, .error')
      .filter({ hasText: /exceed|over|more than/i })
    await expect(warningLocator).toBeVisible()
    await expect(warningLocator).toContainText(/exceed/i)

    await expect(page.getByRole('button', { name: /^(add|save|confirm)$/i })).not.toBeDisabled()
  })

  test('Dialog closes when cancel button clicked', async ({ page }) => {
    await page.goto('/budgets')
    await page.waitForLoadState('networkidle')

    await openCreateBudgetDialog(page)

    const dialog = page.getByRole('dialog', { name: /create budget/i })
    await expect(dialog).toBeVisible()

    const cancelButton = page.getByRole('button', { name: /cancel|close/i })
    if (await cancelButton.isVisible().catch(() => false)) {
      await cancelButton.click()
    } else {
      await page.keyboard.press('Escape')
    }

    await expect(dialog).not.toBeVisible()
  })
})
