import { test, expect, type Page } from '@playwright/test'
import { acceptCookieConsent } from '../helpers/auth'
import { apiClient, getAuthToken } from '../helpers/api/client'
import { ensureBudgetExists } from '../helpers/api/budgets'

// Matches both "Create budget" (header) and "Create your first budget" (empty state)
const openCreateBudgetDialog = (page: Page) =>
  page.getByRole('button', { name: /create.*budget/i }).first().click()

// Unique name per run so CI retries don't hit the UNIQUE(user_id, name) constraint
const RUN_BUDGET_NAME = `E2E Income Test Budget ${Date.now()}`

test.describe('Budget income unification (#1048)', () => {
  test.beforeAll(async ({ browser }) => {
    const context = await browser.newContext({ storageState: 'e2e/.auth/user.json' })
    const page = await context.newPage()
    await page.goto('/budgets')
    await page.waitForLoadState('networkidle')
    // Hard assertion: Clerk session must hydrate within 30s; failure surfaces as auth error, not a selector miss.
    await page.waitForFunction(
      () =>
        !!(window as unknown as { Clerk?: { session?: { id?: string } } }).Clerk?.session?.id,
      { timeout: 30000 }
    )
    const token = await getAuthToken(page)
    if (token) {
      await ensureBudgetExists(page)
    } else {
      // JWT not yet available with email claims — SQL seed already created E2E Monthly Budget.
      console.warn('beforeAll: Clerk token unavailable — trusting SQL seed for fixture budget')
    }
    await context.close()
  })

  test.afterAll(async ({ browser }) => {
    // Delete the per-run budget created by the creation test to keep the DB clean for retries.
    const context = await browser.newContext({ storageState: 'e2e/.auth/user.json' })
    const page = await context.newPage()
    await page.goto('/budgets')
    await page.waitForLoadState('networkidle')
    const client = await apiClient(page).catch(() => null)
    if (client) {
      const listResp = await client.get('/api/v1/budgets').catch(() => null)
      if (listResp && listResp.ok()) {
        const body = await listResp.json()
        const budgets: Array<{ id: string; name: string }> = body.budgets ?? body ?? []
        const target = budgets.find((b) => b.name === RUN_BUDGET_NAME)
        if (target) await client.delete(`/api/v1/budgets/${target.id}`).catch(() => {})
      }
    }
    await context.close()
  })

  test.beforeEach(async ({ page }) => {
    await acceptCookieConsent(page)
  })

  test('Create Budget dialog has no tabs', async ({ page }) => {
    await page.goto('/budgets')
    await page.waitForLoadState('networkidle')

    // Matches both "Create budget" (header) and "Create your first budget" (empty state)
    await page.getByRole('button', { name: /create.*budget/i }).first().click()

    await expect(page.getByRole('dialog', { name: /create budget/i })).toBeVisible()
    await expect(page.getByRole('tab')).toHaveCount(0)
    await expect(page.getByLabel(/expected income/i)).toBeVisible()
  })

  test('Create budget with expected income and tracking source', async ({ page }) => {
    await page.goto('/budgets')
    await page.waitForLoadState('networkidle')

    // Matches both "Create budget" (header) and "Create your first budget" (empty state)
    await page.getByRole('button', { name: /create.*budget/i }).first().click()

    await expect(page.getByRole('dialog', { name: /create budget/i })).toBeVisible()

    await page.getByLabel(/budget name|name/i).fill(RUN_BUDGET_NAME)
    await page.getByLabel(/expected income/i).fill('5000')

    const trackingButton = page.getByRole('button', { name: /track.*income|tracking source/i })
    if (await trackingButton.isVisible()) {
      await trackingButton.click()
      await page.waitForTimeout(300)
    }

    await page.getByRole('button', { name: /^create budget$/i }).click()

    await page.waitForLoadState('networkidle')
    await expect(page.getByText(RUN_BUDGET_NAME)).toBeVisible()
  })

  test('Budget list shows budget cards with income progress', async ({ page }) => {
    // Pin to cards view before navigation so localStorage state from other tests can't interfere.
    await page.addInitScript(() => window.localStorage.setItem('budgetsViewMode', 'cards'))
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
    if (await cancelButton.isVisible()) {
      await cancelButton.click()
    } else {
      await page.keyboard.press('Escape')
    }

    await expect(dialog).not.toBeVisible()
  })
})
