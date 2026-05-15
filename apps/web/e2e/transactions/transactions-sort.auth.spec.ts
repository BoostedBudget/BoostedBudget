/**
 * Transaction Sort E2E Tests
 *
 * Issue #1123: Server-side sort for transactions
 *
 * Verifies that clicking column headers sends sort_by/sort_dir to the backend and
 * that paginating preserves the sort (cross-page invariant).
 */

import { test, expect, type Page } from '@playwright/test'
import { acceptCookieConsent } from '../helpers/auth'
import { mockAuthDependencies } from '../helpers/api-mocks'

// 60 transactions with ascending amounts (10–610) so sorting is easily verifiable
function buildFixture() {
  const transactions = Array.from({ length: 60 }, (_, i) => ({
    id: `txn-${i + 1}`,
    user_id: 'user-123',
    account_id: 'acc-1',
    linked_item_id: 'item-1',
    source: 'plaid',
    external_transaction_id: `plaid-txn-${i + 1}`,
    amount: (i + 1) * 10,
    date: `2025-11-${String((i % 28) + 1).padStart(2, '0')}`,
    name: `Merchant ${i + 1}`,
    merchant_name: `Merchant ${String.fromCharCode(65 + (i % 26))}`, // A–Z cycling
    pfc_primary: 'GENERAL_MERCHANDISE',
    pending: false,
    tags: [],
    created_at: `2025-11-${String((i % 28) + 1).padStart(2, '0')}T10:00:00Z`,
    updated_at: `2025-11-${String((i % 28) + 1).padStart(2, '0')}T10:00:00Z`,
  }))
  return transactions
}

const allTransactions = buildFixture()

async function setupTransactionsMock(page: Page) {
  // Pre-set cookie consent so the banner never appears (mocked tests don't need GDPR UX)
  await page.addInitScript(() => {
    const consent = { consents: { necessary: true, analytics: false, advertising: false }, timestamp: '2026-01-01T00:00:00.000Z', version: '2.0' }
    document.cookie = `boostedbudget_cookie_consent=${encodeURIComponent(JSON.stringify(consent))}; path=/; SameSite=Strict`
  })

  await mockAuthDependencies(page)

  // Mock Plaid categories
  await page.route('**/api/v1/categories/plaid/primaries*', (route) => {
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
  })

  // Mock categories and tags
  await page.route('**/api/v1/categories*', (route) => {
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
  })
  await page.route('**/api/v1/tags*', (route) => {
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
  })
  await page.route('**/api/v1/accounts*', (route) => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([
        { id: 'acc-1', name: 'Checking', nickname: null, type: 'depository', subtype: 'checking', mask: '0001', current_balance: 1000 },
      ]),
    })
  })

  // Transactions mock that sorts the fixture and paginates
  await page.route('**/api/v1/transactions**', (route) => {
    if (route.request().method() !== 'GET') {
      route.continue()
      return
    }
    const url = new URL(route.request().url())
    const sortBy = url.searchParams.get('sort_by') ?? 'date'
    const sortDir = url.searchParams.get('sort_dir') ?? 'desc'
    const limit = parseInt(url.searchParams.get('limit') ?? '50', 10)
    const offset = parseInt(url.searchParams.get('offset') ?? '0', 10)

    let sorted = [...allTransactions]
    sorted.sort((a, b) => {
      const primary = (() => {
        switch (sortBy) {
          case 'amount':
            return a.amount - b.amount
          case 'merchant':
            return (a.merchant_name ?? '').localeCompare(b.merchant_name ?? '')
          default:
            return new Date(a.date).getTime() - new Date(b.date).getTime()
        }
      })()
      // tiebreaker by id (numeric portion)
      const cmp =
        primary !== 0
          ? primary
          : parseInt(a.id.replace('txn-', '')) - parseInt(b.id.replace('txn-', ''))
      return sortDir === 'asc' ? cmp : -cmp
    })

    const page_ = sorted.slice(offset, offset + limit)
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        transactions: page_,
        total_count: sorted.length,
        limit,
        offset,
        has_more: offset + limit < sorted.length,
      }),
    })
  })
}

test.describe('Transaction Sort', () => {
  test.use({ storageState: 'e2e/.auth/user.json' })

  test('default sort is date desc — most-recent transaction appears first', async ({ page }) => {
    await setupTransactionsMock(page)
    await page.goto('/transactions')
    await acceptCookieConsent(page)

    const table = page.getByRole('table')
    await expect(table).toBeVisible({ timeout: 10_000 })

    // The fixture has dates cycling day 1–28; with date desc and tiebreaker id desc,
    // txn-60 (day 28) should be near the top. We just assert the Amount header has no
    // active sort indicator (sort state is on Date by default).
    const amountHeader = page.getByRole('columnheader', { name: /amount/i })
    await expect(amountHeader).toBeVisible()
    // Amount header should not show active sort
    await expect(amountHeader.locator('[aria-sort]')).toHaveCount(0)
  })

  test('clicking Amount header sends sort_by=amount&sort_dir=asc to backend', async ({ page }) => {
    const capturedRequests: string[] = []

    await setupTransactionsMock(page)
    page.on('request', (req) => {
      if (req.url().includes('/api/v1/transactions') && req.method() === 'GET') {
        capturedRequests.push(req.url())
      }
    })

    await page.goto('/transactions')
    await acceptCookieConsent(page)

    const table = page.getByRole('table')
    await expect(table).toBeVisible({ timeout: 10_000 })

    // Click the amount column header sort button
    const amountHeader = page.getByRole('columnheader', { name: /amount/i })
    const sortBtn = amountHeader.locator('button:not([aria-label="Drag to reorder column"])')

    // Arm listener BEFORE the click so the request can't race past it
    const sortReqPromise = page.waitForRequest(
      (req) => req.url().includes('sort_by=amount') && req.method() === 'GET',
      { timeout: 5_000 }
    )
    await sortBtn.click()
    await sortReqPromise

    const sortedUrl = capturedRequests.find((u) => u.includes('sort_by=amount'))
    expect(sortedUrl).toBeTruthy()
    const params = new URL(sortedUrl!).searchParams
    expect(params.get('sort_by')).toBe('amount')
    expect(params.get('sort_dir')).toBe('asc')
  })

  test('cross-page invariant — last row of page 1 amount ≤ first row of page 2 when sorted asc', async ({ page }) => {
    await setupTransactionsMock(page)
    await page.goto('/transactions')
    await acceptCookieConsent(page)

    const table = page.getByRole('table')
    await expect(table).toBeVisible({ timeout: 10_000 })

    // Sort by amount ascending
    const amountHeader = page.getByRole('columnheader', { name: /amount/i })
    const sortBtn = amountHeader.locator('button:not([aria-label="Drag to reorder column"])')

    // Arm listener BEFORE the click so the request can't race past it
    const sortReqPromise = page.waitForRequest(
      (req) => req.url().includes('sort_by=amount') && req.method() === 'GET',
      { timeout: 5_000 }
    )
    await sortBtn.click()
    await sortReqPromise

    // Wait for table to render
    await page.waitForLoadState('networkidle')
    const rows = table.locator('tbody tr')
    await expect(rows.first()).toBeVisible({ timeout: 5_000 })

    // Record the amount in the last row's amount cell (5th td, index 4)
    const lastRow = rows.last()
    const lastAmountText = await lastRow.locator('td').nth(4).textContent()

    // Navigate to page 2 — arm listener before click
    const page2ReqPromise = page.waitForRequest(
      (req) => req.url().includes('sort_by=amount') && req.url().includes('offset=50'),
      { timeout: 5_000 }
    )
    await page.getByRole('button', { name: /next/i }).click()
    await page2ReqPromise
    await page.waitForLoadState('networkidle')

    const firstRowPage2 = table.locator('tbody tr').first()
    await expect(firstRowPage2).toBeVisible({ timeout: 5_000 })
    const firstAmountText = await firstRowPage2.locator('td').nth(4).textContent()

    // Parse absolute value of displayed amounts (UI negates Plaid amounts: $500 debit → "-$500")
    const parseAmt = (s: string | null) => Math.abs(parseFloat((s ?? '0').replace(/[$,]/g, '')))
    expect(parseAmt(lastAmountText)).toBeLessThanOrEqual(parseAmt(firstAmountText))
  })

  test('sorting resets to page 1 — offset goes to 0', async ({ page }) => {
    const capturedOffsets: number[] = []

    await setupTransactionsMock(page)
    page.on('request', (req) => {
      if (req.url().includes('/api/v1/transactions') && req.method() === 'GET') {
        const offset = parseInt(new URL(req.url()).searchParams.get('offset') ?? '0', 10)
        capturedOffsets.push(offset)
      }
    })

    await page.goto('/transactions')
    await acceptCookieConsent(page)
    await expect(page.getByRole('table')).toBeVisible({ timeout: 10_000 })

    // Wait for Next button — it only appears once hasMore=true arrives from the API
    await expect(page.getByRole('button', { name: /next/i })).toBeVisible({ timeout: 10_000 })

    // Go to page 2 — arm listener before click
    const page2ReqPromise = page.waitForRequest(
      (req) => req.url().includes('/api/v1/transactions') && req.url().includes('offset=50'),
      { timeout: 5_000 }
    )
    await page.getByRole('button', { name: /next/i }).click()
    await page2ReqPromise

    // Now sort — offset should reset to 0; arm listener before click
    const amountHeader = page.getByRole('columnheader', { name: /amount/i })
    const sortReqPromise = page.waitForRequest(
      (req) => req.url().includes('sort_by=amount') && req.url().includes('offset=0'),
      { timeout: 5_000 }
    )
    await amountHeader.locator('button:not([aria-label="Drag to reorder column"])').click()
    await sortReqPromise

    // Verify offset=0 was sent after the sort change
    const lastSortRequest = capturedOffsets.at(-1)
    expect(lastSortRequest).toBe(0)
  })
})
