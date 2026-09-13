import { test, expect } from '@playwright/test'
import { acceptCookieConsent } from '../helpers/auth'
import { apiClient } from '../helpers/api/client'
import { mockAuthDependencies } from '../helpers/api-mocks'

test('shared group editing, plan activity, privacy and keyboard drilldown (#1637 #1627)', async ({ page }) => {
  test.setTimeout(90_000)
  await acceptCookieConsent(page)
  await page.goto('/budgets')
  await page.waitForFunction(() => !!(window as unknown as { Clerk?: { session?: unknown } }).Clerk?.session)
  const client = await apiClient(page)
  const created = await client.post('/api/v1/budgets', {
    name: `Grouped activity ${Date.now()}`, period: 'monthly', start_date: `${new Date().toISOString().slice(0, 8)}01`,
    amount: 1000, expected_income: 1000, currency_code: 'USD', income_sources: [{ type: 'manual', value: '200', description: 'Declared only' }],
    categories: [{ category_kind: 'remainder', allocated_amount: 100, match_logic: 'category_only', tags: [] }],
  })
  expect(created.ok()).toBeTruthy()
  const budget = await created.json() as { id: string }
  try {
    await page.goto(`/budgets/${budget.id}`)
    await expect(page.getByRole('heading', { name: 'Budget Plan and Activity' })).toBeVisible()
    await page.getByRole('button', { name: 'Add Category', exact: true }).click()
    await page.getByRole('button', { name: 'Shared group', exact: true }).click()
    await page.getByLabel('Group name', { exact: true }).fill('Flexible essentials')
    await page.getByLabel('Include Uncategorized').check()
    await page.getByRole('spinbutton', { name: /amount/i }).fill('300')
    await page.getByRole('button', { name: 'Add', exact: true }).click()
    await expect(page.getByRole('dialog')).toHaveCount(0)
    const table = page.getByRole('table', { name: 'Budget allocation and activity' })
    await expect(table.getByRole('row').filter({ hasText: 'Flexible essentials' })).toBeVisible()
    const reportResponse = await client.get(`/api/v1/budgets/${budget.id}/plan-vs-actual`)
    expect(reportResponse.ok()).toBeTruthy()
    const report = await reportResponse.json()
    expect(report.income.manual_declared).toBe(200)
    expect(report.income.observed).toBe(0)
    expect(report.totals.allocated).toBe(400)
    await expect(page.locator('.budget-category-card').filter({ hasText: 'Flexible essentials' }).getByText('$300.00', { exact: true }).first()).toBeVisible()
    const group = report.lines.find((line: { label: string }) => line.label === 'Flexible essentials')
    expect(group).toBeTruthy()
    await expect(table.getByText('Flexible essentials')).toHaveAttribute('data-ph-mask')
    await page.getByRole('button', { name: 'Hide amounts', exact: true }).click()
    await expect(page.getByText('Chart hidden in privacy mode.')).toBeVisible()
    await page.getByRole('button', { name: 'Show amounts', exact: true }).click()
    const view = table.getByRole('row').filter({ hasText: 'Flexible essentials' }).getByRole('button', { name: /activity$/ })
    await view.focus()
    await page.keyboard.press('Enter')
    await expect(page.getByRole('dialog', { name: 'Allocation activity' })).toBeVisible()
    await page.keyboard.press('Escape')
    await page.setViewportSize({ width: 390, height: 844 })
    await expect(table).toBeVisible()
    await expect.poll(async () => page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth), { timeout: 5_000 }).toBeLessThanOrEqual(0)
  } finally {
    // Retaining delete of this test-created budget only; history is not purged.
    const removed = await client.delete(`/api/v1/budgets/${budget.id}`)
    expect(removed.ok()).toBeTruthy()
  }
})

test('euro group uses the same currency in its card and activity report', async ({ page }) => {
  await acceptCookieConsent(page)
  await mockAuthDependencies(page)
  // Display-only fixture: the shared authenticated user's real ledger is USD.
  // No foreign-currency financial rows or budgets are written for this assertion.
  const id = 'euro-budget-display'
  const category = { id: 'euro-group', budget_id: id, category_kind: 'group', group_name: 'Euro essentials', members: [{ category_kind: 'uncategorized' }], allocated_amount: 300, spent_amount: 50, tags: [], match_logic: 'category_only', created_at: '2026-09-01T00:00:00Z', updated_at: '2026-09-01T00:00:00Z' }
  await page.route(`**/api/v1/budgets/${id}`, route => route.fulfill({ json: {
    id, user_id: 'display-owner', name: 'Euro display', period: 'monthly', start_date: '2026-09-01', amount: 1000, expected_income: 1000, income_amount: 0, income_sources: [], currency_code: 'EUR', is_active: true, categories: [category],
  } }))
  await page.route(`**/api/v1/budgets/${id}/plan-vs-actual`, route => route.fulfill({ json: {
    budget_id: id, currency_code: 'EUR', eligibility: 'eligible', completeness: 'complete', reasons: [], period_start: '2026-09-01', period_end: '2026-09-30', actual_through: '2026-09-12', as_of: '2026-09-12T12:00:00Z',
    income: { expected: 1000, observed: 0, manual_declared: 0 },
    lines: [{ id: category.id, budget_category_id: category.id, kind: 'expense', label: category.group_name, planned: 300, actual: 50, variance: -250, completeness: 'complete', reasons: [] }],
    totals: { allocated: 300, unallocated: 700, overallocated: 0, actual: 50, unbudgeted: 0 },
  } }))
  await page.goto(`/budgets/${id}`)
  const card = page.locator('.budget-category-card').filter({ hasText: 'Euro essentials' })
  await expect(card.getByText('€300.00', { exact: true }).first()).toBeVisible()
  await expect(card.getByText('€50.00', { exact: true }).first()).toBeVisible()
  const table = page.getByRole('table', { name: 'Budget allocation and activity' })
  await expect(table.getByText('€300.00', { exact: true })).toBeVisible()
  await expect(table.getByText('€50.00', { exact: true })).toBeVisible()
  await expect(card).not.toContainText('$')
})
