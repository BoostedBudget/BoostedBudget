/**
 * DataTable column resize E2E — Transactions (#1172)
 * Covers: cell padding, live drag, persistence, reset, semantic colors
 */

import { test, expect, type Page } from '@playwright/test'
import { mockAuthDependencies } from '../helpers/api-mocks'
import { dragHandleInPhases } from '../helpers/datatable-resize'

function buildTransactions() {
  return Array.from({ length: 30 }, (_, i) => ({
    id: `txn-resize-${i}`,
    user_id: 'user-123',
    account_id: 'acc-1',
    linked_item_id: 'item-1',
    source: 'plaid',
    external_transaction_id: `plaid-resize-${i}`,
    // Alternating positive/negative amounts for red/green tests
    amount: i % 2 === 0 ? -(50 + i * 10) : 50 + i * 10,
    date: '2026-05-01',
    name: `Merchant Resize Test ${i}`,
    merchant_name: `Merchant ${i}`,
    pfc_primary: 'FOOD_AND_DRINK',
    pfc_detailed: 'FOOD_AND_DRINK_COFFEE',
    pending: false,
    tags: [],
    account: { id: 'acc-1', name: 'Checking', nickname: null, custom_color: '#3b82f6', custom_emoji: null },
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

  await mockAuthDependencies(page)

  await page.route('**/api/v1/transactions**', (route) => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ transactions: buildTransactions(), next_cursor: null, limit: 30 }),
    })
  })

  await page.route('**/api/v1/user/tags**', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
  )
  await page.route('**/api/v1/user/categories**', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
  )
  await page.route('**/api/v1/categories**', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
  )
  await page.route('**/api/v1/plaid/categories**', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
  )
  await page.route('**/api/v1/accounts**', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        accounts: [{ id: 'acc-1', name: 'Checking', nickname: null, type: 'depository', mask: '0001', current_balance: 1000 }],
      }),
    })
  )
}

async function waitForTable(page: Page) {
  await page.waitForSelector('table tbody tr', { timeout: 15000 })
}

test.describe('DataTable resize — Transactions', () => {
  test.beforeEach(async ({ page }) => {
    // Clear table-prefs on the FIRST navigation only (not on reload).
    // sessionStorage persists across reloads within a tab, so we use it as a
    // one-time flag to prevent the persistence tests from clearing sizing on reload.
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

  test('cell padding is non-zero after CSS cascade-layer fix', async ({ page }) => {
    await mockDependencies(page)
    await page.goto('/transactions')
    await waitForTable(page)

    const td = page.locator('table tbody td').first()
    await expect(td).toBeVisible()

    const paddingLeft = await td.evaluate((el) => getComputedStyle(el).paddingLeft)
    const paddingTop = await td.evaluate((el) => getComputedStyle(el).paddingTop)

    expect(paddingLeft).not.toBe('0px')
    expect(paddingTop).not.toBe('0px')
  })

  test('table-layout is fixed', async ({ page }) => {
    await mockDependencies(page)
    await page.goto('/transactions')
    await waitForTable(page)

    const tableLayout = await page.locator('table').first().evaluate((el) => getComputedStyle(el).tableLayout)
    expect(tableLayout).toBe('fixed')
  })

  test('resize handle drag moves body cells live (columnResizeMode: onChange)', async ({ page }) => {
    await mockDependencies(page)
    await page.goto('/transactions')
    await waitForTable(page)

    // Locate the description column header (id='merchant', header='Description') and its resize handle
    const merchantTh = page.locator('th').filter({ hasText: 'Description' }).first()
    await expect(merchantTh).toBeVisible()

    const handle = merchantTh.locator('[data-testid="resize-handle"]')
    await expect(handle).toBeVisible()

    // Fix #1193: Virtual <td> cells now have inline width styles from TanStack column sizing.
    const initialWidth = await merchantTh.evaluate((el) => el.getBoundingClientRect().width)

    const { midWidth, finalWidth } = await dragHandleInPhases(page, handle, {
      dx: 60,
      cellLocator: merchantTh,
    })

    expect(midWidth).toBeGreaterThan(initialWidth + 10)
    expect(finalWidth).toBeGreaterThan(initialWidth + 40)

    // Fix #1193: virtual <td> width must match <th> (merchant is column index 1).
    // Hard assertion — if no absolute rows exist, virtualization is broken (not a skip).
    const firstVirtualRow = page.locator('tbody tr[style*="position: absolute"]').first()
    await expect(firstVirtualRow).toBeVisible()
    const merchantTd = firstVirtualRow.locator('td').nth(1)
    const tdWidth = await merchantTd.evaluate((el) => el.getBoundingClientRect().width)
    // Tolerance: <th> BCR includes browser table-layout redistribution of surplus space;
    // <td> BCR = exact inline style value (getSize()). Redistribution is ~3px at CI viewport.
    // Pre-fix diffs were 100-250px so 10px still catches regressions.
    expect(Math.abs(tdWidth - finalWidth)).toBeLessThan(10)
  })

  test('resized column width persists across reload', async ({ page }) => {
    await mockDependencies(page)
    await page.goto('/transactions')
    await waitForTable(page)

    const merchantTh = page.locator('th').filter({ hasText: 'Description' }).first()
    const handle = merchantTh.locator('[data-testid="resize-handle"]')
    await expect(handle).toBeVisible()

    const handleBox = await handle.boundingBox()
    const startX = handleBox!.x + handleBox!.width / 2
    const startY = handleBox!.y + handleBox!.height / 2

    await page.mouse.move(startX, startY)
    await page.mouse.down()
    await page.mouse.move(startX + 60, startY, { steps: 10 })
    await page.mouse.up()

    // Fix #1193: Virtual <td> width must match <th> before and after reload.
    // Hard assertions — if no absolute rows exist, virtualization is broken (not a skip).
    const widthAfterDrag = await merchantTh.evaluate((el) => el.getBoundingClientRect().width)
    const firstVirtualRow = page.locator('tbody tr[style*="position: absolute"]').first()
    await expect(firstVirtualRow).toBeVisible()
    const merchantTdBeforeReload = firstVirtualRow.locator('td').nth(1)
    const tdWidthBefore = await merchantTdBeforeReload.evaluate((el) => el.getBoundingClientRect().width)
    // Tolerance: <th> BCR includes browser table-layout redistribution (~3px at CI viewport).
    // Pre-fix diffs were 100-250px so 10px still catches regressions.
    expect(Math.abs(tdWidthBefore - widthAfterDrag)).toBeLessThan(10)

    // Reload and re-mock
    await mockDependencies(page)
    await page.reload()
    await waitForTable(page)

    const widthAfterReload = await page
      .locator('th').filter({ hasText: 'Description' }).first()
      .evaluate((el) => el.getBoundingClientRect().width)

    expect(Math.abs(widthAfterReload - widthAfterDrag)).toBeLessThan(5)

    const firstVirtualRowAfterReload = page.locator('tbody tr[style*="position: absolute"]').first()
    await expect(firstVirtualRowAfterReload).toBeVisible()
    const merchantTdAfterReload = firstVirtualRowAfterReload.locator('td').nth(1)
    const tdWidthAfter = await merchantTdAfterReload.evaluate((el) => el.getBoundingClientRect().width)
    // Tolerance: <th> BCR includes browser table-layout redistribution (~3px at CI viewport).
    // Pre-fix diffs were 100-250px so 10px still catches regressions.
    expect(Math.abs(tdWidthAfter - widthAfterReload)).toBeLessThan(10)
  })

  test('"Reset column widths" restores defaultSize', async ({ page }) => {
    await mockDependencies(page)
    await page.goto('/transactions')
    await waitForTable(page)

    // Drag merchant +60px to create a sizing override
    const merchantTh = page.locator('th').filter({ hasText: 'Description' }).first()
    const handle = merchantTh.locator('[data-testid="resize-handle"]')
    const handleBox = await handle.boundingBox()
    const startX = handleBox!.x + handleBox!.width / 2
    const startY = handleBox!.y + handleBox!.height / 2
    await page.mouse.move(startX, startY)
    await page.mouse.down()
    await page.mouse.move(startX + 60, startY, { steps: 10 })
    await page.mouse.up()

    // Fix #1193: Virtual <td> widths from column sizing state must persist
    const widthAfterDrag = await merchantTh.evaluate((el) => el.getBoundingClientRect().width)

    // Open Columns menu
    await page.locator('button', { hasText: 'Columns' }).click()
    const resetButton = page.locator('button[role="menuitem"]', { hasText: 'Reset column widths' })
    await expect(resetButton).toBeVisible()
    await expect(resetButton).toBeEnabled()
    await resetButton.click()

    await page.waitForTimeout(100)
    const widthAfterReset = await merchantTh.evaluate((el) => el.getBoundingClientRect().width)
    expect(widthAfterReset).toBeLessThan(widthAfterDrag - 40)

    const firstVirtualRow = page.locator('tbody tr[style*="position: absolute"]').first()
    await expect(firstVirtualRow).toBeVisible()
    const merchantTd = firstVirtualRow.locator('td').nth(1)
    const tdWidthAfterReset = await merchantTd.evaluate((el) => el.getBoundingClientRect().width)
    // Tolerance: after reset all columns return to defaultSize; table surplus is larger so
    // browser redistributes ~21px to each <th>. <td> BCR = exact inline style. 30px covers this.
    // Pre-fix diffs were 100-250px so 30px still catches regressions.
    expect(Math.abs(tdWidthAfterReset - widthAfterReset)).toBeLessThan(30)
  })

  test('amount columns show semantic red/green colors', async ({ page }) => {
    await mockDependencies(page)
    await page.goto('/transactions')
    await waitForTable(page)

    // The amount column is pinned right (data-pinned="right"). On the real /transactions
    // page, onEdit/onCategorize add an extra actions <td> at the end of each row, so
    // td.last() is the actions cell — use data-pinned="right" to reliably target amount.
    // First row: amount = -(50) which is inflow (negative in Plaid convention) → green.
    // Second row: amount = 60 which is outflow → red. Either is a valid color assertion.
    const amountCell = page.locator('table tbody tr').first().locator('td[data-pinned="right"]').first()
    const color = await amountCell.evaluate((el) => {
      const span = el.querySelector('span[class*="text-red"], span[class*="text-green"]')
      return span ? getComputedStyle(span).color : null
    })
    expect(color).not.toBeNull()
  })

  test('account chip uses token-based classes (dark-mode safe)', async ({ page }) => {
    await mockDependencies(page)
    await page.goto('/transactions')
    await waitForTable(page)

    const chip = page.locator('table tbody td span.rounded-md').first()
    if (await chip.count() > 0) {
      const className = await chip.getAttribute('class') ?? ''
      expect(className).not.toContain('text-gray-800')
      expect(
        className.includes('text-foreground') ||
        className.includes('text-muted-foreground')
      ).toBe(true)
    }
  })

  test('no scroll-jump during rapid column drag on virtualized table', async ({ page }) => {
    await page.addInitScript(() => {
      for (let i = localStorage.length - 1; i >= 0; i--) {
        const key = localStorage.key(i)
        if (key?.startsWith('bb:table-prefs:')) localStorage.removeItem(key)
      }
    })
    await page.addInitScript(() => {
      const consent = {
        consents: { necessary: true, analytics: false, advertising: false },
        timestamp: '2026-01-01T00:00:00.000Z',
        version: '2.0',
      }
      document.cookie = `boostedbudget_cookie_consent=${encodeURIComponent(JSON.stringify(consent))}; path=/; SameSite=Strict`
    })
    await mockAuthDependencies(page)
    await page.route('**/api/v1/transactions**', (route) => {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          transactions: Array.from({ length: 50 }, (_, i) => ({
            id: `txn-virt-${i}`,
            user_id: 'user-123',
            account_id: 'acc-1',
            linked_item_id: 'item-1',
            source: 'plaid',
            external_transaction_id: `virt-${i}`,
            amount: -(50 + i * 5),
            date: '2026-05-01',
            description: `Transaction ${i}`,
            merchant_name: `Merchant ${i}`,
            category_id: 'cat-1',
            pending: false,
            currency_code: 'USD',
          })),
          next_cursor: 'cursor-page-2',
          limit: 50,
        }),
      })
    })
    await page.route('**/api/v1/user/tags**', (route) =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
    )
    await page.route('**/api/v1/user/categories**', (route) =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
    )
    await page.route('**/api/v1/categories**', (route) =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
    )
    await page.route('**/api/v1/plaid/categories**', (route) =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
    )
    await page.route('**/api/v1/accounts**', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          accounts: [{ id: 'acc-1', name: 'Checking', nickname: null, type: 'depository', mask: '0001', current_balance: 1000 }],
        }),
      })
    )
    await page.goto('/transactions')
    await waitForTable(page)

    const merchantTh = page.locator('th').filter({ hasText: 'Description' }).first()
    const handle = merchantTh.locator('[data-testid="resize-handle"]')
    if (await handle.count() === 0) {
      // Skip if handle not present (e.g. coarse-pointer detected)
      return
    }

    // Measure scroll position before drag
    const scrollContainer = page.locator('[data-virtualized-scroll]').first()
    const hasVirtualScroll = await scrollContainer.count() > 0

    // Perform rapid drag (20 steps over 80px)
    const handleBox = await handle.boundingBox()
    if (!handleBox) return
    const startX = handleBox.x + handleBox.width / 2
    const startY = handleBox.y + handleBox.height / 2

    const scrollBefore = hasVirtualScroll
      ? await scrollContainer.evaluate((el) => el.scrollTop)
      : 0

    await page.mouse.move(startX, startY)
    await page.mouse.down()
    await page.mouse.move(startX + 80, startY, { steps: 20 })
    await page.mouse.up()

    const scrollAfter = hasVirtualScroll
      ? await scrollContainer.evaluate((el) => el.scrollTop)
      : 0

    // Scroll position must not jump more than 8px during the drag
    expect(Math.abs(scrollAfter - scrollBefore)).toBeLessThan(8)
  })
})
