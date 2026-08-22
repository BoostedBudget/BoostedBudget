import { test, expect } from '@playwright/test'
import { acceptCookieConsent } from '../helpers/auth'
import { ensureBudgetExists } from '../helpers/api/budgets'
import { getAuthToken } from '../helpers/api/client'

test.describe('Budget list-to-detail navigation (#1051)', () => {
  // #1537: this describe's beforeAll navigates to /budgets and then waits up to 30s for Clerk to
  // hydrate. The suite-wide budget (playwright.config.ts:48) is `CI ? 30s : 15s`, so locally a 30s
  // inner wait sits inside a 15s hook and cannot finish — these tests failed deterministically in
  // two separate local runs, at 0ms, with '"beforeAll" hook timeout of 15000ms exceeded'.
  //
  // NOTE: test.describe.configure({ timeout }) does NOT work here — I tried it and the hook
  // still died at 15000ms. Playwright applies describe-level timeouts to TESTS; a beforeAll
  // hook's budget is only changed by calling test.setTimeout() INSIDE the hook, which is what
  // the first line of the hook below does.
  //
  // Scoped to this hook rather than raising the global budget: the global change would alter
  // timing for ~375 other tests to fix these, and the inner wait's documented intent (Clerk must
  // hydrate within 30s) is preserved either way. CI already passes at its 30s budget, so this is a
  // local-developer fix, not a CI one — introduced by 4d5a0545 (#1055), a hardening that made the
  // spec unrunnable locally.
  test.beforeAll(async ({ browser }) => {
    test.setTimeout(60_000) // see the note above: only this raises a beforeAll's budget
    const context = await browser.newContext({ storageState: 'e2e/.auth/user.json' })
    const page = await context.newPage()
    await page.goto('/budgets')
    await page.waitForLoadState('networkidle')
    await page.waitForFunction(
      () =>
        !!(window as unknown as { Clerk?: { session?: { id?: string } } }).Clerk?.session?.id,
      { timeout: 30000 }
    )
    const token = await getAuthToken(page)
    if (token) {
      await ensureBudgetExists(page)
    } else {
      console.warn('beforeAll: Clerk token unavailable — trusting SQL seed for fixture budget')
    }
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
