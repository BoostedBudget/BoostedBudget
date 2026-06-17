/**
 * Account Detail Page E2E Tests
 * Issue #1298: Show holdings on investment account detail — Accounts page integration
 *
 * Covers:
 * - Clicking an investment account from the table view navigates to the detail page
 *   with a Holdings section and an activity deep-link
 * - Non-investment accounts show account header only (no Holdings, no activity link)
 *
 * Uses API mocking (page.route) — no live backend required.
 */

import { test, expect, type Page } from '@playwright/test'
import { mockAuthDependencies } from './helpers/api-mocks'

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

const INVESTMENT_ACCOUNT = {
  id: 'acc-invest-e2e-1',
  user_id: 'user-123',
  linked_item_id: 'item-invest',
  linked_item_status: 'active',
  name: 'Vanguard Brokerage',
  official_name: 'Vanguard Brokerage Account',
  nickname: null,
  mask: '4321',
  type: 'investment',
  subtype: 'brokerage',
  institution_name: 'Vanguard',
  current_balance: 50000,
  available_balance: null,
  currency_code: 'USD',
  is_hidden: false,
  display_order: 0,
  custom_color: null,
  custom_emoji: null,
  custom_icon: null,
  status: 'active',
  plaid_account_id: 'plaid-invest-1',
  replaced_by_account_id: null,
  created_at: '2025-01-01T00:00:00Z',
  updated_at: '2025-01-01T00:00:00Z',
}

const DEPOSITORY_ACCOUNT = {
  id: 'acc-depository-e2e-1',
  user_id: 'user-123',
  linked_item_id: 'item-depository',
  linked_item_status: 'active',
  name: 'Chase Checking',
  official_name: 'Chase Total Checking',
  nickname: null,
  mask: '0000',
  type: 'depository',
  subtype: 'checking',
  institution_name: 'Chase',
  current_balance: 2500,
  available_balance: 2300,
  currency_code: 'USD',
  is_hidden: false,
  display_order: 1,
  custom_color: null,
  custom_emoji: null,
  custom_icon: null,
  status: 'active',
  plaid_account_id: 'plaid-depository-1',
  replaced_by_account_id: null,
  created_at: '2025-01-01T00:00:00Z',
  updated_at: '2025-01-01T00:00:00Z',
}

const FIXTURE_HOLDINGS = {
  count: 1,
  holdings: [
    {
      id: 'holding-1',
      security_id: 'sec-1',
      security_name: 'Apple Inc',
      ticker_symbol: 'AAPL',
      security_type: 'equity',
      quantity: 10,
      institution_price: 175,
      institution_value: 1750,
      cost_basis: 1500,
      gain_loss: 250,
      gain_loss_pct: 16.67,
      account_id: INVESTMENT_ACCOUNT.id,
      day_change: 5,
      day_change_pct: 0.29,
      user_id: 'user-123',
    },
  ],
}

// ---------------------------------------------------------------------------
// Mock helpers
// ---------------------------------------------------------------------------

async function mockAccountDetailDependencies(page: Page) {
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
      body: JSON.stringify({ accounts: [INVESTMENT_ACCOUNT, DEPOSITORY_ACCOUNT] }),
    })
  )

  await page.route('**/api/v1/investments/holdings**', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(FIXTURE_HOLDINGS),
    })
  )

  await page.route('**/api/v1/subscription/tiers**', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        tiers: [{ id: 'premium', name: 'Premium', max_accounts: 50, price: 8.99 }],
      }),
    })
  )

  await page.route('**/api/v1/plaid/link-token**', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ link_token: 'test-link-token' }),
    })
  )
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test.describe('Account Detail Page (#1298)', () => {
  test('investment account — row click navigates to detail page with Holdings section and activity link', async ({
    page,
  }) => {
    await mockAccountDetailDependencies(page)
    await page.goto('/accounts')

    // Switch to table view so onRowClick fires on row click
    const tableToggle = page.locator('button[title="Table view"]')
    await expect(tableToggle).toBeVisible({ timeout: 10000 })
    await tableToggle.click()
    await page.waitForSelector('table tbody tr', { timeout: 10000 })

    // Click the Vanguard row by content — avoids accidentally clicking a skeleton row
    const investRow = page.locator('table tbody tr').filter({ hasText: 'Vanguard' })
    await expect(investRow).toBeVisible({ timeout: 10000 })
    await investRow.click()

    // URL should change to /accounts/:id
    await page.waitForURL(`**/${INVESTMENT_ACCOUNT.id}`, { timeout: 10000 })
    expect(page.url()).toContain(`/accounts/${INVESTMENT_ACCOUNT.id}`)

    // Account header is visible — exact match avoids matching the official_name substring
    await expect(page.getByText('Vanguard', { exact: true })).toBeVisible({ timeout: 10000 })

    // Holdings section heading present
    await expect(page.getByText('Holdings')).toBeVisible()

    // Holdings table renders a real holding row (validates fixture field names)
    await expect(page.getByText('AAPL')).toBeVisible()
    await expect(page.getByText('Apple Inc')).toBeVisible()

    // Activity deep-link present with exact expected href
    const activityLink = page.getByRole('link', { name: /view investment activity/i })
    await expect(activityLink).toBeVisible()
    const href = await activityLink.getAttribute('href')
    expect(href).toBe(
      `/transactions?tab=investment-activity&account_id=${INVESTMENT_ACCOUNT.id}`
    )
  })

  test('non-investment account — detail page shows header only, no Holdings or activity link', async ({
    page,
  }) => {
    await mockAccountDetailDependencies(page)
    // Navigate directly to depository account detail page
    await page.goto(`/accounts/${DEPOSITORY_ACCOUNT.id}`)
    await page.waitForLoadState('networkidle')

    // Account header is visible — exact match avoids matching the official_name substring
    await expect(page.getByText('Chase', { exact: true })).toBeVisible({ timeout: 10000 })

    // Holdings section heading must NOT be in the DOM at all
    await expect(page.getByText('Holdings')).toHaveCount(0)

    // Activity link must NOT be in the DOM at all
    await expect(
      page.getByRole('link', { name: /view investment activity/i })
    ).toHaveCount(0)
  })

  test('investment account detail — dark theme class applied and key elements render', async ({
    page,
  }) => {
    // Pre-set dark mode before page load so ThemeProvider applies it on mount
    await page.addInitScript(() => {
      localStorage.setItem('bb-theme', 'dark')
    })

    await mockAccountDetailDependencies(page)
    await page.goto(`/accounts/${INVESTMENT_ACCOUNT.id}`)
    await page.waitForLoadState('networkidle')

    // ThemeProvider sets 'dark' class on <html>
    await expect(page.locator('html')).toHaveClass(/dark/)

    // Key elements still render in dark mode — exact match avoids matching official_name substring
    await expect(page.getByText('Vanguard', { exact: true })).toBeVisible({ timeout: 10000 })
    await expect(page.getByText('Holdings')).toBeVisible()
    await expect(
      page.getByRole('link', { name: /view investment activity/i })
    ).toBeVisible()
  })
})
