/**
 * Admin Sort E2E Smoke Tests
 * Issue #1148: Server-side sort for admin tables (UsersPage, WebhooksPage, DeadLetterPage)
 *
 * Covers (2 tests per page = 6 total):
 * - Clicking a sortable column header adds ?sort=col:dir to the URL
 * - Clicking a sort header while on page 2 resets to page 1
 * - Navigating with a pre-sorted URL pre-applies aria-sort on the correct header
 * - Tri-state cycling: asc → desc → cleared
 *
 * Uses API mocking (page.route) — no live backend required.
 */

import { test, expect, type Page } from '@playwright/test'
import { mockAuthDependencies } from './helpers/api-mocks'

// ---------------------------------------------------------------------------
// Fixture data
// ---------------------------------------------------------------------------

const FIXTURE_USERS = {
  users: [
    {
      id: 'user-admin-1',
      email: 'alice@example.com',
      subscription_tier: 'premium',
      subscription_status: 'active',
      primary_region: 'us-east-1',
      last_login_at: '2026-05-01T10:00:00Z',
      created_at: '2026-01-01T00:00:00Z',
      linked_accounts_count: 2,
      automation_rules_count: 1,
      referrals_count: 0,
      is_admin: false,
      manual_tier_override: null,
      manual_tier_ends_at: null,
    },
  ],
  total_count: 1,
  page: 1,
  total_pages: 1,
  has_more: false,
}

const FIXTURE_WEBHOOKS = {
  Events: [
    {
      id: 'wh-1',
      event_type: 'subscription.created',
      event_id: 'evt-abc-123',
      processed: true,
      processing_error: null,
      processing_attempts: 1,
      created_at: '2026-05-01T10:00:00Z',
    },
  ],
  Total: 1,
  Page: 1,
  TotalPages: 1,
  HasMore: false,
}

const FIXTURE_WEBHOOK_STATS = {
  Total: 1,
  Processed: 1,
  Failed: 0,
  AvgProcessingTimeMs: 50,
}

const FIXTURE_DLQ_JOBS = {
  jobs: [
    {
      id: 1,
      original_job_id: 99,
      kind: 'plaid_sync',
      queue: 'default',
      user_id: 'user-123',
      args: {},
      errors: [],
      max_attempts: 5,
      failed_at: '2026-05-01T10:00:00Z',
      created_at: '2026-05-01T10:00:00Z',
      retried_at: null,
      retry_job_id: null,
      notes: null,
      notified: false,
    },
  ],
  total: 1,
  page: 1,
  total_pages: 1,
  has_more: false,
}

const FIXTURE_DLQ_STATS = {
  total: 1,
  retried: 0,
  last_24_hours: 1,
  by_kind: { plaid_sync: 1 },
}

// ---------------------------------------------------------------------------
// Per-page mock helpers
// ---------------------------------------------------------------------------

async function mockUsersPage(page: Page) {
  await mockAuthDependencies(page, { isAdmin: true })
  await page.route('**/api/v1/regions*', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ regions: {}, default_region: 'us-east-1' }),
    })
  )
  await page.route('**/api/v1/admin/users*', (route) => {
    if (route.request().method() === 'GET') {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(FIXTURE_USERS),
      })
    } else {
      route.continue()
    }
  })
}

async function mockWebhooksPage(page: Page) {
  await mockAuthDependencies(page, { isAdmin: true })
  await page.route('**/api/v1/admin/webhooks/paddle/stats*', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(FIXTURE_WEBHOOK_STATS),
    })
  )
  await page.route('**/api/v1/admin/webhooks/paddle*', (route) => {
    if (route.request().method() !== 'GET' || route.request().url().includes('/stats')) {
      route.continue()
      return
    }
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(FIXTURE_WEBHOOKS),
    })
  })
}

async function mockDeadLetterPage(page: Page) {
  await mockAuthDependencies(page, { isAdmin: true })
  await page.route('**/api/v1/admin/dead-letter-jobs/stats*', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(FIXTURE_DLQ_STATS),
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
      body: JSON.stringify(FIXTURE_DLQ_JOBS),
    })
  })
}

// ---------------------------------------------------------------------------
// Admin Sort — UsersPage
// ---------------------------------------------------------------------------

test.describe('Admin sort — UsersPage', () => {
  test.beforeEach(async ({ page }) => {
    await page.addInitScript(() => {
      const consent = {
        consents: { necessary: true, analytics: false, advertising: false },
        timestamp: '2026-01-01T00:00:00.000Z',
        version: '2.0',
      }
      document.cookie = `boostedbudget_cookie_consent=${encodeURIComponent(JSON.stringify(consent))}; path=/; SameSite=Strict`
      localStorage.setItem('boosted-admin-mode', 'true')
    })
  })

  test('clicking Tier column header sets sort in URL', async ({ page }) => {
    await mockUsersPage(page)
    await page.goto('/admin/users')
    await page.waitForLoadState('networkidle')
    const tierHeader = page.getByRole('columnheader', { name: /Tier/i })
    await expect(tierHeader).toBeVisible()
    await tierHeader.getByRole('button', { name: /^Tier$/i }).click()
    await expect(page).toHaveURL(/sort=subscription_tier/)
    await expect(tierHeader).toHaveAttribute('aria-sort', /ascending|descending/)
  })

  test('sorting on page 2 resets page param', async ({ page }) => {
    await mockUsersPage(page)
    await page.goto('/admin/users?page=2')
    await page.waitForLoadState('networkidle')
    const tierHeader = page.getByRole('columnheader', { name: /Tier/i })
    await expect(tierHeader).toBeVisible()
    await tierHeader.getByRole('button', { name: /^Tier$/i }).click()
    await expect(page).toHaveURL(/sort=subscription_tier/)
    await expect(page).not.toHaveURL(/[?&]page=/)
  })
})

// ---------------------------------------------------------------------------
// Admin Sort — WebhooksPage
// ---------------------------------------------------------------------------

test.describe('Admin sort — WebhooksPage', () => {
  test.beforeEach(async ({ page }) => {
    await page.addInitScript(() => {
      const consent = {
        consents: { necessary: true, analytics: false, advertising: false },
        timestamp: '2026-01-01T00:00:00.000Z',
        version: '2.0',
      }
      document.cookie = `boostedbudget_cookie_consent=${encodeURIComponent(JSON.stringify(consent))}; path=/; SameSite=Strict`
      localStorage.setItem('boosted-admin-mode', 'true')
    })
  })

  test('clicking Attempts column header sets sort in URL', async ({ page }) => {
    await mockWebhooksPage(page)
    await page.goto('/admin/webhooks')
    await page.waitForLoadState('networkidle')
    const attemptsHeader = page.getByRole('columnheader', { name: /Attempts/i })
    await expect(attemptsHeader).toBeVisible()
    await attemptsHeader.getByRole('button', { name: /^Attempts$/i }).click()
    await expect(page).toHaveURL(/sort=attempts/)
    await expect(attemptsHeader).toHaveAttribute('aria-sort', /ascending|descending/)
  })

  test('navigating with ?sort=created_at:desc pre-applies aria-sort on Created header', async ({ page }) => {
    await mockWebhooksPage(page)
    await page.goto('/admin/webhooks?sort=created_at%3Adesc')
    await page.waitForLoadState('networkidle')
    const createdHeader = page.getByRole('columnheader', { name: /Created/i })
    await expect(createdHeader).toBeVisible()
    await expect(createdHeader).toHaveAttribute('aria-sort', 'descending')
    await expect(page).toHaveURL(/sort=created_at/)
  })
})

// ---------------------------------------------------------------------------
// Admin Sort — DeadLetterPage
// ---------------------------------------------------------------------------

test.describe('Admin sort — DeadLetterPage', () => {
  test.beforeEach(async ({ page }) => {
    await page.addInitScript(() => {
      const consent = {
        consents: { necessary: true, analytics: false, advertising: false },
        timestamp: '2026-01-01T00:00:00.000Z',
        version: '2.0',
      }
      document.cookie = `boostedbudget_cookie_consent=${encodeURIComponent(JSON.stringify(consent))}; path=/; SameSite=Strict`
      localStorage.setItem('boosted-admin-mode', 'true')
    })
  })

  test('clicking Kind column header sets sort in URL', async ({ page }) => {
    await mockDeadLetterPage(page)
    await page.goto('/admin/dead-letter')
    await page.waitForLoadState('networkidle')
    const kindHeader = page.getByRole('columnheader', { name: /Kind/i })
    await expect(kindHeader).toBeVisible()
    await kindHeader.getByRole('button', { name: /^Kind$/i }).click()
    await expect(page).toHaveURL(/sort=kind/)
    await expect(kindHeader).toHaveAttribute('aria-sort', /ascending|descending/)
  })

  test('tri-state: second click → desc, third click → clears sort', async ({ page }) => {
    await mockDeadLetterPage(page)
    await page.goto('/admin/dead-letter?sort=failed_at%3Aasc')
    await page.waitForLoadState('networkidle')
    const failedAtHeader = page.getByRole('columnheader', { name: /Failed At/i })
    await expect(failedAtHeader).toBeVisible()
    await expect(failedAtHeader).toHaveAttribute('aria-sort', 'ascending')
    // Second click on an already-sorted-asc column → descending
    await failedAtHeader.getByRole('button', { name: /^Failed At$/i }).click()
    await expect(page).toHaveURL(/sort=failed_at(%3A|:)desc/)
    await expect(failedAtHeader).toHaveAttribute('aria-sort', 'descending')
    // Third click → sort cleared
    await failedAtHeader.getByRole('button', { name: /^Failed At$/i }).click()
    await expect(page).not.toHaveURL(/sort=/)
  })
})
