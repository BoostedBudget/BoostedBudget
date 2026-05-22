/**
 * DataTable column resize E2E — Admin Dead Letter Queue
 * Issue #1172: Fix Transactions table formatting + TanStack column sizing everywhere
 *
 * Covers:
 * - Admin consumer that previously wired only visibility (now also wires sizing)
 * - Drag a column; body cells track (columnResizeMode: 'onChange')
 * - Persistence: resized width survives page reload
 *
 * Uses API mocking with isAdmin: true — no live backend required.
 *
 * Run with: bun run test:e2e -- --grep 'datatable-admin-dead-letter' --reporter=line
 */

import { test, expect, type Page } from '@playwright/test'
import { mockAuthDependencies } from './helpers/api-mocks'

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

  // Register the more-specific /stats route FIRST — Playwright matches in
  // registration order (last-registered wins for overlapping globs), so register
  // the narrow pattern first to ensure the jobs route never intercepts it.
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

  // Register the jobs list route after /stats to avoid it swallowing the stats URL.
  // The handler guards against non-GET methods to allow retry/delete POSTs to pass through.
  await page.route('**/api/v1/admin/dead-letter-jobs*', (route) => {
    if (route.request().method() !== 'GET') {
      route.continue()
      return
    }
    // Don't intercept /stats (belt-and-suspenders in case Playwright routing order flips)
    if (route.request().url().includes('/stats')) {
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
    await page.addInitScript(() => {
      for (let i = localStorage.length - 1; i >= 0; i--) {
        const key = localStorage.key(i)
        if (key?.startsWith('bb:table-prefs:')) localStorage.removeItem(key)
      }
    })
  })

  test('table-layout is fixed on admin dead-letter page', async ({ page }) => {
    await mockDependencies(page)
    await page.goto('/admin/dead-letter')
    await waitForTable(page)

    const tableLayout = await page.locator('table').first().evaluate((el) => getComputedStyle(el).tableLayout)
    expect(tableLayout).toBe('fixed')
  })

  test('drag kind column; body cells track in sync', async ({ page }) => {
    await mockDependencies(page)
    await page.goto('/admin/dead-letter')
    await waitForTable(page)

    const kindTh = page.locator('th').filter({ hasText: 'Kind' }).first()
    await expect(kindTh).toBeVisible()

    const handle = kindTh.locator('[data-testid="resize-handle"]')
    await expect(handle).toBeVisible()

    // First body cell (kind column)
    const bodyCell = page.locator('table tbody tr').first().locator('td').first()
    const initialWidth = await bodyCell.evaluate((el) => el.getBoundingClientRect().width)

    const handleBox = await handle.boundingBox()
    expect(handleBox).not.toBeNull()
    const startX = handleBox!.x + handleBox!.width / 2
    const startY = handleBox!.y + handleBox!.height / 2

    await page.mouse.move(startX, startY)
    await page.mouse.down()
    await page.mouse.move(startX + 50, startY, { steps: 10 })
    await page.mouse.up()

    const newWidth = await bodyCell.evaluate((el) => el.getBoundingClientRect().width)
    expect(newWidth).toBeGreaterThan(initialWidth + 30)
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
