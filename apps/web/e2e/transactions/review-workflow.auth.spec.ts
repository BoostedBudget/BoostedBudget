import { test, expect, type Page, type Locator } from '@playwright/test'
import { acceptCookieConsent } from '../helpers/auth'
import { mockAuthDependencies, ALL_PFC_PRIMARIES, FOOD_AND_DRINK_DETAILED } from '../helpers/api-mocks'

const categoryId = '11111111-1111-4111-8111-111111111111'
const leafId = '22222222-2222-4222-8222-222222222222'
const categories = [
  { id: categoryId, name: 'Private family', parent_pfc_primary: 'RENT_AND_UTILITIES', color: '#336699' },
  { id: leafId, name: 'Private child', parent_id: categoryId, color: '#336699' },
  { id: '33333333-3333-4333-8333-333333333333', name: 'Travel', color: '#336699' },
]
const initial = {
  id: 'txn-phone', user_id: 'user-123', account_id: 'acc-private', linked_item_id: 'item-1', source: 'banking',
  amount: 42.37, date: '2026-09-01', name: 'Private description', merchant_name: 'Private merchant',
  pfc_primary: 'RENT_AND_UTILITIES', pfc_detailed: 'RENT_AND_UTILITIES_TELEPHONE',
  pending: false, reviewed: false, tags: ['Private tag'], created_at: '', updated_at: '',
  category_override_kind: null as string | null, custom_category_override_id: null as string | null,
  pfc_primary_override: null as string | null, pfc_detailed_override: null as string | null,
}

/** Exact routes own all transaction writes; mutable state makes invalidations observable. */
async function fixture(page: Page) {
  await acceptCookieConsent(page)
  await mockAuthDependencies(page)
  const state = {
    rows: [{ ...initial }, { ...initial, id: 'txn-second', merchant_name: 'Second merchant', amount: -12.34 }],
    writes: [] as { id: string; action: string; body: Record<string, unknown> }[],
    lists: [] as URL[],
    release: undefined as (() => void) | undefined,
    holdNext: false,
  }
  await page.route('**/api/v1/categories**', route => {
    const path = new URL(route.request().url()).pathname
    if (route.request().method() !== 'GET') return route.fulfill({ status: 405 })
    const json = path.endsWith('/plaid-primaries') ? ALL_PFC_PRIMARIES
      : path.endsWith('/plaid-detailed') ? [...FOOD_AND_DRINK_DETAILED,
        { primary_enum: 'RENT_AND_UTILITIES', primary_display: 'Rent And Utilities', enum: 'RENT_AND_UTILITIES_TELEPHONE', display: 'Telephone' }]
      : categories
    return route.fulfill({ json })
  })
  await page.route('**/api/v1/tags*', route => route.fulfill({ json: [{ id: 'tag-private', name: 'Private tag', color: '#336699' }] }))
  await page.route('**/api/v1/accounts*', route => route.fulfill({ json: { accounts: [{ id: 'acc-private', name: 'Private account', type: 'depository', status: 'active', current_balance: 1000 }], has_more: false } }))
  await page.route('**/api/v1/transactions**', async route => {
    const request = route.request()
    const url = new URL(request.url())
    if (request.method() === 'GET' && url.pathname === '/api/v1/transactions/review-summary') {
      return route.fulfill({ json: { unreviewed_count: state.rows.filter(row => !row.reviewed).length, top_categories: [] } })
    }
    if (request.method() === 'GET' && url.pathname === '/api/v1/transactions') {
      state.lists.push(url)
      const reviewed = url.searchParams.get('reviewed')
      const rows = state.rows.filter(row => reviewed === null || row.reviewed === (reviewed === 'true'))
      return route.fulfill({ json: { transactions: rows, total_count: rows.length, has_more: false, next_cursor: null } })
    }
    const match = url.pathname.match(/^\/api\/v1\/transactions\/(txn-[\w-]+)\/(category|tags|review)$/)
    if (match && ['PUT', 'PATCH'].includes(request.method())) {
      const [, id, action] = match
      const body = request.postDataJSON()
      state.writes.push({ id, action, body })
      if (state.holdNext) {
        state.holdNext = false
        await new Promise<void>(resolve => { state.release = resolve })
      }
      const row = state.rows.find(row => row.id === id)!
      if (action === 'review') row.reviewed = body.reviewed
      if (action === 'tags') row.tags = body.tags
      if (action === 'category') {
        row.category_override_kind = body.kind === 'clear' ? null : body.kind
        row.custom_category_override_id = body.custom_category_id ?? null
        row.pfc_primary_override = body.pfc_primary ?? null
        row.pfc_detailed_override = body.pfc_detailed ?? null
      }
      return route.fulfill({ json: row })
    }
    return route.fulfill({ status: 404, json: { message: 'Unexpected fixture request' } })
  })
  return state
}

async function openEditor(page: Page) {
  await page.getByRole('button', { name: 'Edit transaction', exact: true }).first().click()
  return page.getByRole('dialog', { name: 'Edit Transaction' })
}
async function masked(locator: Locator) {
  await expect(locator).toBeVisible()
  expect(await locator.evaluate(element => !!element.closest('[data-ph-mask]'))).toBe(true)
}

for (const target of ['search', 'new category name'] as const) {
  test(`Escape from category ${target} preserves the shared editor draft`, async ({ page }) => {
    const state = await fixture(page)
    await page.goto('/transactions')
    const dialog = await openEditor(page)
    await dialog.getByRole('button', { name: 'Remove Private tag' }).click()
    await dialog.getByRole('button', { name: 'Change category' }).click()
    const search = dialog.getByRole('combobox', { name: 'Search categories' })
    await expect(search).toBeFocused()
    if (target === 'new category name') {
      await dialog.getByRole('button', { name: /create new category/i }).click()
      const name = dialog.getByRole('textbox', { name: 'Category name' })
      await name.fill('Unsaved category')
      await name.press('Escape')
    } else {
      await search.fill('telephone')
      await search.press('Escape')
    }
    await expect(dialog).toBeVisible()
    await expect(search).not.toBeVisible()
    await expect(dialog.getByRole('button', { name: 'Change category' })).toBeFocused()
    await expect(dialog.getByText('No tags', { exact: true })).toBeVisible()
    await expect(dialog.getByRole('button', { name: 'Save', exact: true })).toBeEnabled()
    expect(state.writes).toHaveLength(0)
    await dialog.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(dialog).not.toBeVisible()
    expect(state.writes).toEqual([{ id: 'txn-phone', action: 'tags', body: { tags: [] } }])
    await openEditor(page)
    await page.keyboard.press('Escape')
    await expect(dialog).not.toBeVisible()
  })
}

test('returning through SPA navigation refreshes an inactive unreviewed transaction cache', async ({ page }) => {
  const state = await fixture(page)
  await page.goto('/transactions?reviewed=false')
  const row = page.getByRole('row').filter({ hasText: 'Private merchant' })
  await expect(row).toBeVisible()
  // Header links retain the QueryClient; a full navigation would hide this bug.
  await page.locator('a[href="/review"]').first().click()
  await expect(page).toHaveURL(/\/review$/)
  await expect(page.getByRole('heading', { name: 'Review Inbox', exact: true })).toBeVisible()
  await row.click()
  await page.getByRole('button', { name: 'Edit details', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Edit Transaction' })
  await dialog.getByRole('button', { name: 'Mark reviewed', exact: true }).click()
  await expect(dialog).not.toBeVisible()
  await expect(row).not.toBeVisible()
  expect(state.rows[0].reviewed).toBe(true)
  const requestsBeforeReturn = state.lists.length
  // POP back to the same filtered route without remounting QueryClient. Visiting
  // All first and toggling the filter would independently trigger a stale fetch.
  await page.goBack()
  await expect.poll(() => new URL(page.url()).searchParams.get('reviewed')).toBe('false')
  await expect(page.getByRole('heading', { name: 'Transactions', exact: true })).toBeVisible()
  await expect(row).not.toBeVisible()
  await expect(page.getByRole('row').filter({ hasText: 'Second merchant' })).toBeVisible()
  expect(state.lists.slice(requestsBeforeReturn).some(url => url.searchParams.get('reviewed') === 'false')).toBe(true)
})

for (const width of [1280, 390]) {
  test(`review filter distinguishes omitted/false/true and clears independently at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 840 })
    const state = await fixture(page)
    await page.goto('/transactions')
    await expect.poll(() => state.lists.length).toBeGreaterThan(0)
    expect(state.lists.at(-1)!.searchParams.has('reviewed')).toBe(false)
    await page.getByRole('button', { name: /^Filters/ }).click()
    const group = page.getByRole('group', { name: 'Review status', exact: true })
    await expect(group.getByRole('button', { name: 'All', exact: true })).toHaveAttribute('aria-pressed', 'true')
    await page.getByRole('button', { name: 'Posted', exact: true }).click()
    await expect.poll(() => new URL(page.url()).searchParams.get('pending')).toBe('false')
    await expect.poll(() => state.lists.at(-1)?.searchParams.get('pending')).toBe('false')
    for (const [label, value] of [['Unreviewed', 'false'], ['Reviewed', 'true'], ['All', null]] as const) {
      await group.getByRole('button', { name: label, exact: true }).click()
      await expect.poll(() => new URL(page.url()).searchParams.get('reviewed')).toBe(value)
      // Returning to a visited filter can use a fresh cached page without another GET.
      await expect.poll(() => state.lists.some(url => url.searchParams.get('reviewed') === value && url.searchParams.get('pending') === 'false')).toBe(true)
      expect(new URL(page.url()).searchParams.get('pending')).toBe('false')
    }
    await group.getByRole('button', { name: 'Unreviewed', exact: true }).click()
    if (width < 768) await page.getByRole('button', { name: 'Apply', exact: true }).click()
    else await page.getByRole('button', { name: /^Filters/ }).click()
    await page.getByRole('button', { name: 'Remove Review status filter', exact: true }).click()
    await expect.poll(() => new URL(page.url()).searchParams.has('reviewed')).toBe(false)
    expect(new URL(page.url()).searchParams.get('pending')).toBe('false')
    await page.reload()
    await expect.poll(() => state.lists.at(-1)?.searchParams.has('reviewed')).toBe(false)
    expect(state.lists.at(-1)?.searchParams.get('pending')).toBe('false')
  })
}

test('Telephone hierarchy, exact enum/custom saves and explicit bank reset survive refetch', async ({ page }) => {
  const state = await fixture(page)
  await page.goto('/transactions')
  const row = page.getByRole('row').filter({ hasText: 'Private merchant' })
  await expect(row.getByRole('cell', { name: 'Rent & Utilities', exact: true })).toBeVisible()
  await expect(row.getByRole('cell', { name: 'Telephone', exact: true })).toBeVisible()
  await masked(row.getByRole('cell', { name: 'Private merchant' }))
  await masked(row.getByRole('cell', { name: 'Private account' }))
  await masked(row.getByRole('cell', { name: /42\.37/ }))
  let dialog = await openEditor(page)
  await dialog.getByRole('button', { name: 'Change category' }).click()
  await page.getByRole('combobox', { name: 'Search categories' }).fill('coffee')
  await page.getByRole('option', { name: 'Coffee', exact: true }).click()
  await dialog.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(dialog).not.toBeVisible()
  expect(state.writes.at(-1)).toEqual({ id: 'txn-phone', action: 'category', body: { kind: 'pfc', pfc_primary: 'FOOD_AND_DRINK', pfc_detailed: 'FOOD_AND_DRINK_COFFEE' } })
  await expect(row.getByRole('cell', { name: 'Coffee', exact: true })).toBeVisible()
  dialog = await openEditor(page)
  await dialog.getByRole('button', { name: 'Change category' }).click()
  await page.getByRole('combobox', { name: 'Search categories' }).fill('Travel')
  await expect(page.getByRole('option', { name: 'Travel', exact: true })).toBeVisible()
  await expect(page.getByRole('option', { name: 'Travel Custom', exact: true })).toBeVisible()
  await page.getByRole('combobox', { name: 'Search categories' }).fill('Private child')
  await masked(page.getByRole('option', { name: 'Private child Custom' }).getByText('Private child', { exact: true }))
  await page.getByRole('option', { name: 'Private child Custom' }).click()
  await expect(dialog.getByText('Rent And Utilities › Private family › Private child', { exact: true })).toBeVisible()
  await dialog.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(dialog).not.toBeVisible()
  expect(state.writes.at(-1)?.body).toEqual({ kind: 'custom', custom_category_id: leafId })
  await masked(row.getByRole('cell', { name: 'Private family › Private child', exact: true }))
  dialog = await openEditor(page)
  await dialog.getByRole('button', { name: 'Reset category override' }).click()
  await dialog.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(dialog).not.toBeVisible()
  expect(state.writes.at(-1)?.body).toEqual({ kind: 'clear' })
  await expect(row.getByRole('cell', { name: 'Telephone', exact: true })).toBeVisible()
})

test('Review uses shared editor, blocks pending dismissal and retains approve Undo after queue refresh', async ({ page }) => {
  const state = await fixture(page)
  await page.goto('/review')
  const row = page.getByRole('row').filter({ hasText: 'Private merchant' })
  await expect(row.getByRole('cell', { name: 'Rent & Utilities', exact: true })).toBeVisible()
  await expect(row.getByRole('cell', { name: 'Telephone', exact: true })).toBeVisible()
  await row.click()
  const edit = page.getByRole('button', { name: 'Edit details', exact: true })
  await edit.focus()
  await page.keyboard.press('Enter')
  const dialog = page.getByRole('dialog', { name: 'Edit Transaction' })
  await expect(dialog).toBeVisible()
  expect(state.writes).toHaveLength(0)
  for (const text of ['Private merchant', 'Private description', 'Private account', 'Private tag']) await masked(dialog.getByText(text, { exact: true }))
  await masked(dialog.getByText(/42\.37/))
  const leakedAttributes = await dialog.evaluate(element => {
    const privateText = ['Private merchant', 'Private description', 'Private account', 'Private tag']
    return [...element.querySelectorAll('*')].flatMap(node => [...node.attributes]
      .filter(attribute => privateText.some(value => attribute.value.includes(value)))
      .map(attribute => attribute.name))
  })
  expect(leakedAttributes).toEqual([])
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click()
  await expect(edit).toBeFocused()
  await page.keyboard.press('c')
  await expect(page.getByRole('button', { name: 'Change category' })).toBeVisible()
  await page.getByRole('button', { name: 'Cancel', exact: true }).click()
  await edit.click()
  state.holdNext = true
  await dialog.getByRole('button', { name: 'Mark reviewed', exact: true }).click()
  await expect.poll(() => state.writes.length).toBe(1)
  await page.keyboard.press('Escape')
  await expect(dialog).toBeVisible()
  await expect(dialog.getByRole('button', { name: 'Close', exact: true })).toBeDisabled()
  await expect(dialog.getByText('Private merchant', { exact: true })).toBeVisible()
  // Simulate another client reviewing the row while our request is pending.
  // Reconnecting a stale query produces a real GET and removes it from this queue.
  const listCount = state.lists.length
  state.rows[0].reviewed = true
  await page.clock.setFixedTime(new Date(Date.now() + 6 * 60_000))
  await page.evaluate(() => window.dispatchEvent(new Event('offline')))
  await page.evaluate(() => window.dispatchEvent(new Event('online')))
  await expect.poll(() => state.lists.length).toBeGreaterThan(listCount)
  await expect(row).not.toBeVisible()
  await expect(dialog.getByText('Private merchant', { exact: true })).toBeVisible()
  await expect(dialog.getByText('Second merchant', { exact: true })).not.toBeVisible()
  state.release!()
  await expect(dialog).not.toBeVisible()
  await expect(row).not.toBeVisible()
  await page.getByRole('row').filter({ hasText: 'Second merchant' }).click()
  await page.getByRole('button', { name: 'Approve: mark reviewed and advance' }).click()
  await expect(page.getByText('All caught up!', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Undo', exact: true }).click()
  await expect(page.getByRole('row').filter({ hasText: 'Second merchant' })).toBeVisible()
  expect(state.writes.map(write => [write.id, write.body.reviewed])).toEqual([['txn-phone', true], ['txn-second', true], ['txn-second', false]])
})

for (const width of [1280, 390]) for (const theme of ['light', 'dark']) {
  test(`shared editor and expanded mobile triage fit ${width}px ${theme}`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width, height: 740 })
    await page.addInitScript(value => localStorage.setItem('bb-theme', value), theme)
    await fixture(page)
    await page.goto('/review')
    if (width >= 768) await page.getByRole('row').filter({ hasText: 'Private merchant' }).click()
    await page.getByRole('button', { name: 'Edit details', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Edit Transaction' })
    await dialog.getByRole('button', { name: 'Change category' }).click()
    const bounds = await dialog.boundingBox()
    expect(bounds!.width).toBeLessThanOrEqual(width >= 768 ? 674 : width)
    expect(bounds!.height).toBeLessThanOrEqual(740 * 0.9 + 2)
    const save = dialog.getByRole('button', { name: 'Save', exact: true })
    const footer = await save.boundingBox()
    expect(footer!.y + footer!.height).toBeLessThan(740)
    expect(await dialog.locator('.min-h-0.overflow-y-auto').evaluate(element => element.scrollHeight > element.clientHeight)).toBe(true)
    const editorScreenshot = testInfo.outputPath(`1638-editor-${width}-${theme}.png`)
    await page.screenshot({ path: editorScreenshot })
    await testInfo.attach('shared editor', { path: editorScreenshot, contentType: 'image/png' })
    await dialog.getByRole('button', { name: 'Cancel', exact: true }).click()
    if (width < 768) {
      await page.getByRole('button', { name: 'Recategorize transaction' }).click()
      await page.getByRole('button', { name: 'Change category' }).click()
      const scroller = page.locator('div.fixed').filter({ has: page.getByRole('button', { name: 'Close triage' }) })
      expect(await scroller.evaluate(element => element.scrollHeight > element.clientHeight)).toBe(true)
      // Vertical scrolling must never pan the card horizontally or clip its actions.
      expect(await scroller.evaluate(element => element.scrollWidth - element.clientWidth)).toBeLessThanOrEqual(1)
      const frame = await scroller.boundingBox()
      for (const name of ['Approve: mark reviewed and advance', 'Recategorize transaction', 'Skip: advance without marking', 'Select transaction for bulk action', 'Edit details']) {
        const action = page.getByRole('button', { name, exact: true })
        const box = await action.boundingBox()
        expect(box!.x).toBeGreaterThanOrEqual(frame!.x)
        expect(box!.x + box!.width).toBeLessThanOrEqual(frame!.x + frame!.width)
      }
      const reset = page.getByRole('button', { name: 'Reset category override' })
      await reset.scrollIntoViewIfNeeded()
      await reset.click()
      await page.getByRole('button', { name: 'Apply', exact: true }).scrollIntoViewIfNeeded()
      const triageScreenshot = testInfo.outputPath(`1638-triage-${width}-${theme}.png`)
      await page.screenshot({ path: triageScreenshot })
      await testInfo.attach('expanded mobile triage', { path: triageScreenshot, contentType: 'image/png' })
    }
  })
}
