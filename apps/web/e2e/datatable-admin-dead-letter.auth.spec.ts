/**
 * DataTable column resize E2E — Admin Dead Letter Queue (#1172)
 * Covers: admin resizing, live drag tracking, persistence
 */

import { test, expect, type Page } from '@playwright/test'
import { mockAuthDependencies } from './helpers/api-mocks'
import { dragHandleInPhases } from './helpers/datatable-resize'

function buildDeadLetterJobs(count: number) {
  const kinds = ['transaction_sync', 'webhook_delivery', 'rule_evaluation']
  const statuses = ['failed', 'retrying', 'dead']
  return Array.from({ length: count }, (_, i) => ({
    id: `dlq-resize-${i}`,
    kind: kinds[i % kinds.length],
    queue: `default`,
    user_id: `user-${i % 3}`,
    failed_at: '2026-05-01T10:00:00Z',
    last_error: `Error message ${i}`,
    attempt_count: (i % 3) + 1,
    status: statuses[i % statuses.length],
    notes: i % 2 === 0 ? `Note for job ${i}` : '',
    created_at: '2026-05-01T10:00:00Z',
    updated_at: '2026-05-01T10:00:00Z',
  }))
}

async function mockDependencies(page: Page) {
  await page.addInitScript(() => {
    const consent = {
      consents: { necessary: true, analytics: false, advertising: false },
      timestamp: '2026-01-01T00:00:00.000Z',
      version: '2.0',
    }
    document.cookie = `boostedbudget_cookie_consent=${encodeURIComponent(JSON.stringify(consent))}; path=/; SameSite=Strict`
  })

  // Mark user as admin so AdminRoute allows access
  await mockAuthDependencies(page, { isAdmin: true })

  const jobs = buildDeadLetterJobs(15)

  await page.route('**/api/v1/admin/dead-letter-jobs/stats*', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        total: jobs.length,
        retried: 5,
        last_24_hours: 3,
        by_kind: { transaction_sync: 8, webhook_delivery: 4, rule_evaluation: 3 },
      }),
    })
  )

  await page.route('**/api/v1/admin/dead-letter-jobs*', (route) => {
    if (route.request().method() !== 'GET' || route.request().url().includes('/stats')) {
      route.continue()
      return
    }
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ jobs, total: jobs.length }),
    })
  })
}

async function waitForTable(page: Page) {
  await page.waitForSelector('table tbody tr', { timeout: 15000 })
}

test.describe('DataTable resize — Admin Dead Letter Queue', () => {
  test.beforeEach(async ({ page }) => {
    // Clear table-prefs on the FIRST navigation only — sessionStorage persists across
    // reloads within a tab so we use it as a one-time flag.
    await page.addInitScript(() => {
      if (!sessionStorage.getItem('e2e-prefs-cleared')) {
        for (let i = localStorage.length - 1; i >= 0; i--) {
          const key = localStorage.key(i)
          if (key?.startsWith('bb:table-prefs:')) localStorage.removeItem(key)
        }
        sessionStorage.setItem('e2e-prefs-cleared', '1')
      }
      // AdminRoute also checks localStorage 'boosted-admin-mode' === 'true'
      localStorage.setItem('boosted-admin-mode', 'true')
    })
  })

  test('table-layout is fixed on admin dead-letter page', async ({ page }) => {
    await mockDependencies(page)
    await page.goto('/admin/dead-letter')
    await waitForTable(page)

    const tableLayout = await page.locator('table').first().evaluate((el) => getComputedStyle(el).tableLayout)
    expect(tableLayout).toBe('fixed')
  })

  test('drag kind column; body cells track in sync (mid-drag)', async ({ page }) => {
    await mockDependencies(page)
    await page.goto('/admin/dead-letter')
    await waitForTable(page)

    const kindTh = page.locator('th').filter({ hasText: 'Kind' }).first()
    await expect(kindTh).toBeVisible()

    const handle = kindTh.locator('[data-testid="resize-handle"]')
    await expect(handle).toBeVisible()

    const bodyCell = page.locator('table tbody tr').first().locator('td').first()
    const initialWidth = await bodyCell.evaluate((el) => el.getBoundingClientRect().width)

    const { midWidth, finalWidth } = await dragHandleInPhases(page, handle, { dx: 60 })

    expect(midWidth).toBeGreaterThan(initialWidth + 10)
    expect(finalWidth).toBeGreaterThan(initialWidth + 30)
  })

  test('resized width persists across reload', async ({ page }) => {
    await mockDependencies(page)
    await page.goto('/admin/dead-letter')
    await waitForTable(page)

    const kindTh = page.locator('th').filter({ hasText: 'Kind' }).first()
    const handle = kindTh.locator('[data-testid="resize-handle"]')
    const handleBox = await handle.boundingBox()
    const startX = handleBox!.x + handleBox!.width / 2
    const startY = handleBox!.y + handleBox!.height / 2

    await page.mouse.move(startX, startY)
    await page.mouse.down()
    await page.mouse.move(startX + 50, startY, { steps: 10 })
    await page.mouse.up()

    const widthAfterDrag = await page.locator('table tbody tr').first().locator('td').first()
      .evaluate((el) => el.getBoundingClientRect().width)

    await mockDependencies(page)
    await page.reload()
    await waitForTable(page)

    const widthAfterReload = await page.locator('table tbody tr').first().locator('td').first()
      .evaluate((el) => el.getBoundingClientRect().width)

    expect(Math.abs(widthAfterReload - widthAfterDrag)).toBeLessThan(5)
  })
})
