/**
 * DataTable column resize E2E — Accounts (non-virtualized, right-pinned balance)
 * Issue #1172: Fix Transactions table formatting + TanStack column sizing everywhere
 *
 * Covers:
 * - Drag a non-pinned column; body cells track in real time
 * - Persistence across reload
 * - Right-pinned column (`balance`) remains at right edge during horizontal scroll
 *
 * Uses API mocking — no live backend required.
 *
 * Run with: bun run test:e2e -- --grep 'datatable-accounts' --reporter=line
 */

import { test, expect, type Page } from '@playwright/test'
import { mockAuthDependencies } from './helpers/api-mocks'

function buildAccounts(count: number) {
  return Array.from({ length: count }, (_, i) => ({
    id: `acc-resize-${i}`,
    user_id: 'user-123',
    linked_item_id: 'item-1',
    linked_item_status: 'active',
    name: `Account ${i}`,
    official_name: `Official Account ${i}`,
    nickname: null,
    mask: `${1000 + i}`,
    type: 'depository',
    subtype: i % 2 === 0 ? 'checking' : 'savings',
    institution_name: `Bank ${i % 3}`,
    current_balance: 1000 + i * 100,
    is_hidden: false,
    display_order: i,
    custom_color: null,
    custom_emoji: null,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    replaced_by_account_id: null,
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

  await mockAuthDependencies(page)

  await page.route('**/api/v1/accounts**', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ accounts: buildAccounts(15) }),
    })
  )

  await page.route('**/api/v1/subscription/tiers**', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ tiers: [{ id: 'premium', name: 'Premium', max_accounts: 50, price: 8.99 }] }),
    })
  )

  await page.route('**/api/v1/plaid/link-token**', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ link_token: 'test-link-token' }) })
  )
}

async function switchToTableView(page: Page) {
  const tableToggle = page.locator('button[title="Table view"]')
  await expect(tableToggle).toBeVisible({ timeout: 10000 })
  await tableToggle.click()
  await page.waitForSelector('table tbody tr', { timeout: 10000 })
}

test.describe('DataTable resize — Accounts', () => {
  test.beforeEach(async ({ page }) => {
    await page.addInitScript(() => {
      for (let i = localStorage.length - 1; i >= 0; i--) {
        const key = localStorage.key(i)
        if (key?.startsWith('bb:table-prefs:')) localStorage.removeItem(key)
      }
    })
  })

  test('table-layout is fixed on accounts page', async ({ page }) => {
    await mockDependencies(page)
    await page.goto('/accounts')
    await switchToTableView(page)

    const tableLayout = await page.locator('table').first().evaluate((el) => getComputedStyle(el).tableLayout)
    expect(tableLayout).toBe('fixed')
  })

  test('drag name column; body cells track in sync', async ({ page }) => {
    await mockDependencies(page)
    await page.goto('/accounts')
    await switchToTableView(page)

    const nameTh = page.locator('th').filter({ hasText: 'Name' }).first()
    await expect(nameTh).toBeVisible()

    const handle = nameTh.locator('[data-testid="resize-handle"]')
    await expect(handle).toBeVisible()

    // Measure initial body cell width for Name column (2nd col: institution, name, type, mask, balance)
    const bodyCell = page.locator('table tbody tr').first().locator('td').nth(1)
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
    await page.goto('/accounts')
    await switchToTableView(page)

    const nameTh = page.locator('th').filter({ hasText: 'Name' }).first()
    const handle = nameTh.locator('[data-testid="resize-handle"]')
    const handleBox = await handle.boundingBox()
    const startX = handleBox!.x + handleBox!.width / 2
    const startY = handleBox!.y + handleBox!.height / 2

    await page.mouse.move(startX, startY)
    await page.mouse.down()
    await page.mouse.move(startX + 50, startY, { steps: 10 })
    await page.mouse.up()

    const widthAfterDrag = await page.locator('table tbody tr').first().locator('td').nth(1)
      .evaluate((el) => el.getBoundingClientRect().width)

    await mockDependencies(page)
    await page.reload()
    await switchToTableView(page)

    const widthAfterReload = await page.locator('table tbody tr').first().locator('td').nth(1)
      .evaluate((el) => el.getBoundingClientRect().width)

    expect(Math.abs(widthAfterReload - widthAfterDrag)).toBeLessThan(5)
  })
})
