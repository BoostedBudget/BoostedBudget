/**
 * DataTable column resize E2E — Accounts (#1172)
 * Covers: drag non-pinned columns, persistence, right-pinned sticky positioning
 */

import { test, expect, type Page } from '@playwright/test'
import { mockAuthDependencies } from './helpers/api-mocks'
import { dragHandleInPhases } from './helpers/datatable-resize'

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
    })
  })

  test('table-layout is fixed on accounts page', async ({ page }) => {
    await mockDependencies(page)
    await page.goto('/accounts')
    await switchToTableView(page)

    const tableLayout = await page.locator('table').first().evaluate((el) => getComputedStyle(el).tableLayout)
    expect(tableLayout).toBe('fixed')
  })

  test('drag name column; body cells track live (mid-drag)', async ({ page }) => {
    await mockDependencies(page)
    await page.goto('/accounts')
    await switchToTableView(page)

    const nameTh = page.locator('th').filter({ hasText: 'Account' }).first()
    await expect(nameTh).toBeVisible()

    const handle = nameTh.locator('[data-testid="resize-handle"]')
    await expect(handle).toBeVisible()

    // Account column is 2nd col: institution, account, type, mask, balance
    const bodyCell = page.locator('table tbody tr').first().locator('td').nth(1)
    const initialWidth = await bodyCell.evaluate((el) => el.getBoundingClientRect().width)

    const { midWidth, finalWidth } = await dragHandleInPhases(page, handle, {
      dx: 50,
      cellLocator: bodyCell,
    })

    expect(midWidth).toBeGreaterThan(initialWidth + 10)
    expect(finalWidth).toBeGreaterThan(initialWidth + 30)
  })

  test('right-pinned balance column stays at viewport right edge during horizontal scroll', async ({ page }) => {
    await mockDependencies(page)
    // Use 900px — above md breakpoint (768px) so max-md:relative doesn't override sticky
    await page.setViewportSize({ width: 900, height: 800 })
    await page.goto('/accounts')
    await switchToTableView(page)

    const balanceTh = page.locator('th').filter({ hasText: 'Balance' }).first()
    await expect(balanceTh).toBeVisible()

    const tableWrapper = page.locator('.overflow-x-auto').first()
    if (await tableWrapper.count() > 0) {
      await tableWrapper.evaluate((el) => { el.scrollLeft = 200 })
    } else {
      await page.evaluate(() => window.scrollBy(200, 0))
    }
    await page.waitForTimeout(100)

    const balanceTd = page.locator('table tbody tr').first().locator('td[data-pinned="right"]').first()
    if (await balanceTd.count() > 0) {
      const position = await balanceTd.evaluate((el) => getComputedStyle(el).position)
      expect(position).toBe('sticky')
    } else {
      const position = await balanceTh.evaluate((el) => getComputedStyle(el).position)
      expect(position).toBe('sticky')
    }
  })

  test('resized width persists across reload', async ({ page }) => {
    await mockDependencies(page)
    await page.goto('/accounts')
    await switchToTableView(page)

    const nameTh = page.locator('th').filter({ hasText: 'Account' }).first()
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
