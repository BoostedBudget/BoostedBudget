import { test, expect } from '@playwright/test'
import { acceptCookieConsent } from '../helpers/auth'
import { ensureBudgetExists } from '../helpers/api/budgets'

test.describe('Budget list-to-detail navigation (#1051)', () => {
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

  test('Cards view: clicking Manage budget link navigates to budget detail', async ({ page }) => {
    await page.goto('/budgets')
    await page.waitForLoadState('networkidle')

    const manageBudgetLink = page.getByRole('link', { name: /manage budget/i }).first()
    await expect(manageBudgetLink).toBeVisible()

    await manageBudgetLink.click()
    await page.waitForLoadState('networkidle')

    expect(page.url()).toMatch(/\/budgets\/[0-9a-f-]{36}/)
    await expect(page.getByRole('link', { name: /back to budgets/i })).toBeVisible()
  })

  test('Table view: clicking Manage budget icon button navigates to budget detail', async ({ page }) => {
    await page.goto('/budgets')
    await page.waitForLoadState('networkidle')

    await page.getByRole('button', { name: 'Table' }).click()
    await page.waitForLoadState('networkidle')

    const manageBudgetButton = page.getByRole('button', { name: /manage budget/i }).first()
    await expect(manageBudgetButton).toBeVisible()

    await manageBudgetButton.click()
    await page.waitForLoadState('networkidle')

    expect(page.url()).toMatch(/\/budgets\/[0-9a-f-]{36}/)
    await expect(page.getByRole('link', { name: /back to budgets/i })).toBeVisible()
  })

  test('Table view: clicking a budget row navigates to budget detail', async ({ page }) => {
    await page.goto('/budgets')
    await page.waitForLoadState('networkidle')

    await page.getByRole('button', { name: 'Table' }).click()
    await page.waitForLoadState('networkidle')

    const budgetRow = page.getByRole('row').nth(1)
    await expect(budgetRow).toBeVisible()

    await budgetRow.getByRole('cell').first().click()
    await page.waitForLoadState('networkidle')

    expect(page.url()).toMatch(/\/budgets\/[0-9a-f-]{36}/)
    await expect(page.getByRole('link', { name: /back to budgets/i })).toBeVisible()
  })
})
