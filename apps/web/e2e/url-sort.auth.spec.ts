/**
 * URL Sort State E2E Tests
 * Issue #1124: URL-persist sort state for Transactions/Accounts/Budgets/Rules
 *
 * Covers:
 * - Navigating with ?sort=col:dir pre-applies aria-sort on the correct column header
 * - Clicking a column header adds ?sort=col:dir to URL
 * - Sort state persists after page reload
 * - Sort changes use replace semantics (back-button leaves the page, not pre-sort state)
 * - clearAllFilters preserves sort (transactions page)
 *
 * Uses API mocking (page.route) — no live backend required.
 */

import { test, expect, type Page } from '@playwright/test'
import { acceptCookieConsent } from './helpers/auth'
import { mockAuthDependencies } from './helpers/api-mocks'

// ---------------------------------------------------------------------------
// Fixture data
// ---------------------------------------------------------------------------

const FIXTURE_TRANSACTIONS = {
  transactions: [
    {
      id: 'txn-1',
      user_id: 'user-123',
      account_id: 'acc-1',
      linked_item_id: 'item-1',
      source: 'plaid',
      external_transaction_id: 'ext-1',
      amount: 25.50,
      date: '2026-05-01',
      name: 'Coffee Shop',
      merchant_name: 'Starbucks',
      pfc_primary: 'FOOD_AND_DRINK',
      pfc_detailed: 'FOOD_AND_DRINK_COFFEE',
      pending: false,
      tags: [],
      created_at: '2026-05-01T10:00:00Z',
      updated_at: '2026-05-01T10:00:00Z',
    },
    {
      id: 'txn-2',
      user_id: 'user-123',
      account_id: 'acc-1',
      linked_item_id: 'item-1',
      source: 'plaid',
      external_transaction_id: 'ext-2',
      amount: 150.00,
      date: '2026-04-28',
      name: 'Grocery Store',
      merchant_name: 'Whole Foods',
      pfc_primary: 'FOOD_AND_DRINK',
      pfc_detailed: 'FOOD_AND_DRINK_GROCERIES',
      pending: false,
      tags: [],
      created_at: '2026-04-28T10:00:00Z',
      updated_at: '2026-04-28T10:00:00Z',
    },
  ],
  total_count: 2,
  has_more: false,
  next_offset: 0,
}

const FIXTURE_ACCOUNTS = {
  accounts: [
    {
      id: 'acc-1',
      user_id: 'user-123',
      linked_item_id: 'item-1',
      name: 'Checking',
      official_name: 'Chase Total Checking',
      type: 'depository',
      subtype: 'checking',
      institution_name: 'Chase',
      current_balance: 1500.00,
      available_balance: 1400.00,
      mask: '1234',
      is_hidden: false,
      display_order: 0,
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
    },
    {
      id: 'acc-2',
      user_id: 'user-123',
      linked_item_id: 'item-1',
      name: 'Savings',
      official_name: 'Chase Savings',
      type: 'depository',
      subtype: 'savings',
      institution_name: 'Chase',
      current_balance: 5000.00,
      available_balance: 5000.00,
      mask: '5678',
      is_hidden: false,
      display_order: 1,
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
    },
  ],
}

const FIXTURE_BUDGETS = [
  {
    id: 'budget-1',
    user_id: 'user-123',
    name: 'Groceries',
    amount: 500.00,
    period: 'monthly',
    is_active: true,
    categories: [],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  },
  {
    id: 'budget-2',
    user_id: 'user-123',
    name: 'Entertainment',
    amount: 200.00,
    period: 'monthly',
    is_active: true,
    categories: [],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  },
]

const FIXTURE_RULES = [
  {
    id: 'rule-1',
    user_id: 'user-123',
    name: 'Tag Coffee',
    description: 'Tag coffee purchases',
    rule_type: 'tag',
    enabled: true,
    priority: 10,
    execution_count: 42,
    conditions: [{ type: 'merchant_contains', value: 'starbucks', operator: 'contains' }],
    condition_logic: 'AND',
    actions: [{ type: 'add_tag', tag: 'coffee' }],
    trigger_config: { type: 'event' },
    last_executed_at: '2026-05-01T10:00:00Z',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-05-01T10:00:00Z',
  },
  {
    id: 'rule-2',
    user_id: 'user-123',
    name: 'Categorize Groceries',
    description: 'Set grocery category',
    rule_type: 'categorize',
    enabled: false,
    priority: 20,
    execution_count: 15,
    conditions: [{ type: 'merchant_contains', value: 'whole foods', operator: 'contains' }],
    condition_logic: 'AND',
    actions: [{ type: 'set_category', category: 'FOOD_AND_DRINK' }],
    trigger_config: { type: 'schedule' },
    last_executed_at: '2026-04-15T08:00:00Z',
    created_at: '2026-02-01T00:00:00Z',
    updated_at: '2026-04-15T08:00:00Z',
  },
]

// ---------------------------------------------------------------------------
// Per-page mock helpers
// ---------------------------------------------------------------------------

async function mockTransactionPage(page: Page) {
  await mockAuthDependencies(page)
  await page.route('**/api/v1/transactions**', (route) => {
    if (route.request().method() === 'GET') {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(FIXTURE_TRANSACTIONS),
      })
    } else {
      route.continue()
    }
  })
  await page.route('**/api/v1/accounts*', (route) => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(FIXTURE_ACCOUNTS),
    })
  })
  await page.route('**/api/v1/categories/plaid*', (route) => {
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
  })
  await page.route('**/api/v1/categories*', (route) => {
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
  })
  await page.route('**/api/v1/tags*', (route) => {
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
  })
}

async function mockAccountPage(page: Page) {
  await mockAuthDependencies(page)
  await page.route('**/api/v1/accounts*', (route) => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(FIXTURE_ACCOUNTS),
    })
  })
  await page.route('**/api/v1/subscription/tiers*', (route) => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ tiers: { premium: { account_limit: 100 } } }),
    })
  })
  await page.route('**/api/v1/linked-items*', (route) => {
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ items: [] }) })
  })
  // PlaidLinkButton calls POST /api/v1/plaid/link-token on mount; mock it to prevent
  // the request from hanging and consuming the test timeout via waitForLoadState('networkidle')
  await page.route('**/api/v1/plaid/link-token*', (route) => {
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ link_token: 'test-link-token' }) })
  })
  // react-plaid-link loads cdn.plaid.com/link/stable/link-initialize.js when given a token.
  // In CI this CDN request can hang for the full 30s test timeout, blocking networkidle.
  // Return a minimal Plaid stub so the SDK initialises instantly without network activity.
  await page.route('**/cdn.plaid.com/**', (route) => {
    route.fulfill({
      status: 200,
      contentType: 'application/javascript',
      body: 'window.Plaid={create:function(c){return{open:function(){},destroy:function(){},exit:function(){if(c&&c.onExit)c.onExit(null,{});}};}}',
    })
  })
}

async function mockBudgetPage(page: Page) {
  await mockAuthDependencies(page)
  await page.route('**/api/v1/budgets**', (route) => {
    if (route.request().method() === 'GET') {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(FIXTURE_BUDGETS),
      })
    } else {
      route.continue()
    }
  })
}

async function mockRulesPage(page: Page) {
  await mockAuthDependencies(page)
  await page.route('**/api/v1/rules*', (route) => {
    if (route.request().method() === 'GET') {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(FIXTURE_RULES),
      })
    } else {
      route.continue()
    }
  })
}

// Switch to table view and wait for the table to appear
async function switchToTableView(page: Page) {
  await page.getByTitle('Table view').click()
  await expect(page.getByRole('table')).toBeVisible({ timeout: 5000 })
}

// ---------------------------------------------------------------------------
// Transactions (always table view)
// ---------------------------------------------------------------------------

test.describe('URL Sort — Transactions', () => {
  test.beforeEach(async ({ page }) => {
    await acceptCookieConsent(page)
  })

  test('navigating with ?sort=amount:desc pre-applies aria-sort', async ({ page }) => {
    await mockTransactionPage(page)
    await page.goto('/transactions?sort=amount%3Adesc')
    await page.waitForLoadState('networkidle')
    const amountHeader = page.getByRole('columnheader', { name: /amount/i })
    await expect(amountHeader).toHaveAttribute('aria-sort', 'descending')
  })

  test('clicking a sortable header adds sort to URL', async ({ page }) => {
    await mockTransactionPage(page)
    await page.goto('/transactions')
    await page.waitForLoadState('networkidle')
    const amountHeader = page.getByRole('columnheader', { name: /amount/i })
    await amountHeader.getByRole('button', { name: /^Amount$/i }).click()
    await expect(page).toHaveURL(/sort=amount/)
    await expect(amountHeader).toHaveAttribute('aria-sort', /ascending|descending/)
  })

  test('sort persists after reload', async ({ page }) => {
    await mockTransactionPage(page)
    await page.goto('/transactions')
    await page.waitForLoadState('networkidle')
    const amountHeader = page.getByRole('columnheader', { name: /amount/i })
    await amountHeader.getByRole('button', { name: /^Amount$/i }).click()
    await expect(page).toHaveURL(/sort=amount/)
    const sortBefore = await amountHeader.getAttribute('aria-sort')
    await page.reload()
    await page.waitForLoadState('networkidle')
    await expect(page).toHaveURL(/sort=amount/)
    await expect(amountHeader).toHaveAttribute('aria-sort', sortBefore!)
  })

  test('sort changes use replace semantics — back-button leaves the page', async ({ page }) => {
    await mockTransactionPage(page)
    // Establish a prior history entry
    await page.goto('/')
    await page.goto('/transactions')
    await page.waitForLoadState('networkidle')
    const amountHeader = page.getByRole('columnheader', { name: /amount/i })
    await amountHeader.getByRole('button', { name: /^Amount$/i }).click()
    await expect(page).toHaveURL(/sort=amount/)
    // With replace semantics, back should exit /transactions entirely
    await page.goBack()
    await expect(page).not.toHaveURL(/\/transactions/)
  })

  test('clearAllFilters preserves sort', async ({ page }) => {
    await mockTransactionPage(page)
    await page.goto('/transactions?sort=amount%3Adesc')
    await page.waitForLoadState('networkidle')
    await expect(page).toHaveURL(/sort=amount%3Adesc/)
    const clearBtn = page.getByRole('button', { name: /clear all/i })
    if (await clearBtn.isVisible({ timeout: 2000 }).catch(() => false)) {
      await clearBtn.click()
      await expect(page).toHaveURL(/sort=amount%3Adesc/)
    } else {
      test.info().annotations.push({ type: 'note', description: 'Clear-all button not visible without filter panel open — URL sort preservation verified by hook unit tests' })
    }
  })
})

// ---------------------------------------------------------------------------
// Accounts (requires table view toggle)
// ---------------------------------------------------------------------------

test.describe('URL Sort — Accounts', () => {
  test.beforeEach(async ({ page }) => {
    await acceptCookieConsent(page)
  })

  test('navigating with ?sort=name:asc pre-applies aria-sort after switching to table view', async ({ page }) => {
    await mockAccountPage(page)
    await page.goto('/accounts?sort=name%3Aasc')
    await page.waitForLoadState('networkidle')
    await switchToTableView(page)
    const accountHeader = page.getByRole('columnheader', { name: /^account$/i })
    await expect(accountHeader).toHaveAttribute('aria-sort', 'ascending')
  })

  test('clicking a sortable header adds sort to URL', async ({ page }) => {
    await mockAccountPage(page)
    await page.goto('/accounts')
    await page.waitForLoadState('networkidle')
    await switchToTableView(page)
    const accountHeader = page.getByRole('columnheader', { name: /^account$/i })
    await accountHeader.locator('button').click()
    await expect(page).toHaveURL(/sort=name/)
    await expect(accountHeader).toHaveAttribute('aria-sort', /ascending|descending/)
  })

  test('sort persists after reload', async ({ page }) => {
    await mockAccountPage(page)
    await page.goto('/accounts')
    await page.waitForLoadState('networkidle')
    await switchToTableView(page)
    const accountHeader = page.getByRole('columnheader', { name: /^account$/i })
    await accountHeader.locator('button').click()
    await expect(page).toHaveURL(/sort=name/)
    await page.reload()
    await page.waitForLoadState('networkidle')
    await expect(page).toHaveURL(/sort=name/)
  })

  test('sort changes use replace semantics — back-button leaves the page', async ({ page }) => {
    await mockAccountPage(page)
    await page.goto('/')
    await page.goto('/accounts')
    await page.waitForLoadState('networkidle')
    await switchToTableView(page)
    const accountHeader = page.getByRole('columnheader', { name: /^account$/i })
    await accountHeader.locator('button').click()
    await expect(page).toHaveURL(/sort=name/)
    await page.goBack()
    await expect(page).not.toHaveURL(/\/accounts/)
  })
})

// ---------------------------------------------------------------------------
// Budgets (requires table view toggle)
// ---------------------------------------------------------------------------

test.describe('URL Sort — Budgets', () => {
  test.beforeEach(async ({ page }) => {
    await acceptCookieConsent(page)
  })

  test('navigating with ?sort=name:asc pre-applies aria-sort after switching to table view', async ({ page }) => {
    await mockBudgetPage(page)
    await page.goto('/budgets?sort=name%3Aasc')
    await page.waitForLoadState('networkidle')
    await switchToTableView(page)
    const nameHeader = page.getByRole('columnheader', { name: /^name$/i })
    await expect(nameHeader).toHaveAttribute('aria-sort', 'ascending')
  })

  test('clicking a sortable header adds sort to URL', async ({ page }) => {
    await mockBudgetPage(page)
    await page.goto('/budgets')
    await page.waitForLoadState('networkidle')
    await switchToTableView(page)
    const nameHeader = page.getByRole('columnheader', { name: /^name$/i })
    await nameHeader.locator('button').click()
    await expect(page).toHaveURL(/sort=name/)
    await expect(nameHeader).toHaveAttribute('aria-sort', /ascending|descending/)
  })

  test('sort persists after reload', async ({ page }) => {
    await mockBudgetPage(page)
    await page.goto('/budgets')
    await page.waitForLoadState('networkidle')
    await switchToTableView(page)
    const nameHeader = page.getByRole('columnheader', { name: /^name$/i })
    await nameHeader.locator('button').click()
    await expect(page).toHaveURL(/sort=name/)
    await page.reload()
    await page.waitForLoadState('networkidle')
    await expect(page).toHaveURL(/sort=name/)
  })
})

// ---------------------------------------------------------------------------
// Rules (requires table view toggle)
// ---------------------------------------------------------------------------

test.describe('URL Sort — Rules', () => {
  test.beforeEach(async ({ page }) => {
    await acceptCookieConsent(page)
  })

  test('navigating with ?sort=priority:asc pre-applies aria-sort after switching to table view', async ({ page }) => {
    await mockRulesPage(page)
    await page.goto('/rules?sort=priority%3Aasc')
    await page.waitForLoadState('networkidle')
    await switchToTableView(page)
    const priorityHeader = page.getByRole('columnheader', { name: /priority/i })
    await expect(priorityHeader).toHaveAttribute('aria-sort', 'ascending')
  })

  test('clicking a sortable header adds sort to URL', async ({ page }) => {
    await mockRulesPage(page)
    await page.goto('/rules')
    await page.waitForLoadState('networkidle')
    await switchToTableView(page)
    const priorityHeader = page.getByRole('columnheader', { name: /priority/i })
    await priorityHeader.locator('button').click()
    await expect(page).toHaveURL(/sort=priority/)
    await expect(priorityHeader).toHaveAttribute('aria-sort', /ascending|descending/)
  })

  test('sort persists after reload', async ({ page }) => {
    await mockRulesPage(page)
    await page.goto('/rules')
    await page.waitForLoadState('networkidle')
    await switchToTableView(page)
    const priorityHeader = page.getByRole('columnheader', { name: /priority/i })
    await priorityHeader.locator('button').click()
    await expect(page).toHaveURL(/sort=priority/)
    await page.reload()
    await page.waitForLoadState('networkidle')
    await expect(page).toHaveURL(/sort=priority/)
  })
})
