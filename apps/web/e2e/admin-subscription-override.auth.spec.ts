/**
 * Admin Subscription Override E2E Tests (Issue #1414 — Task 15)
 *
 * The #1414 removal flow end to end: an admin opens the Override modal from the users
 * table, sees the current override state, and can remove an active override behind a
 * type-to-confirm dialog. The removal endpoint is deliberately POST (not DELETE) so the
 * audited reason reliably reaches the handler — the body assertion in the removal test
 * is the point of that design choice, not incidental.
 *
 * Note: this mocks the admin responses rather than expecting a live admin surface — the same
 * approach admin-sort.auth.spec.ts and admin-users-search.auth.spec.ts already use. The
 * reason is now purely that the local E2E run has no admin-role Clerk user, not the
 * environment: Issue #1499 removed the admin middleware's environment check, so the admin
 * API is mounted in every environment and gated on the caller's admin role alone.
 */

import { test, expect, type Page } from '@playwright/test'
import { mockAuthDependencies } from './helpers/api-mocks'

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

const FIXTURE_USER = {
  id: 'user-override-1',
  email: 'override-target@example.com',
  subscription_tier: 'household_2',
  subscription_status: 'active',
  effective_status: 'active',
  primary_region: 'us-east-1',
  last_login_at: '2026-05-01T10:00:00Z',
  created_at: '2026-01-01T00:00:00Z',
  linked_accounts_count: 1,
  boosts_count: 0,
  referrals_count: 0,
  is_admin: false,
  is_active: true,
  manual_tier_override: 'household_2',
  manual_tier_ends_at: null,
}

const DETAIL_WITH_OVERRIDE = {
  active_overrides_count: 0,
  base_tier: 'premium',
  effective_status: 'active',
  effective_tier: 'household_2',
  features: {
    advanced_reports: false,
    api_access: false,
    custom_categories: true,
    custom_tags: true,
    priority_support: false,
  },
  limits: { transaction_history_days: 730 },
  manual_tier_override: 'household_2',
  resource_overrides: [],
  subscription_status: 'active',
}

const DETAIL_NO_OVERRIDE = {
  ...DETAIL_WITH_OVERRIDE,
  effective_tier: 'premium',
  manual_tier_override: undefined,
}

// ---------------------------------------------------------------------------
// Mock helpers
// ---------------------------------------------------------------------------

async function setupAdmin(page: Page) {
  await page.addInitScript(() => {
    const consent = {
      consents: { necessary: true, analytics: false, advertising: false },
      timestamp: '2026-01-01T00:00:00.000Z',
      version: '2.0',
    }
    document.cookie = `boostedbudget_cookie_consent=${encodeURIComponent(
      JSON.stringify(consent)
    )}; path=/; SameSite=Strict`
    localStorage.setItem('boosted-admin-mode', 'true')
  })
  await mockAuthDependencies(page, { isAdmin: true })
  await page.route('**/api/v1/regions*', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ regions: {}, default_region: 'us-east-1' }),
    })
  )
}

async function mockUsersList(page: Page, users: unknown[]) {
  await page.route('**/api/v1/admin/users*', (route) => {
    if (route.request().method() === 'GET') {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          users,
          total_count: users.length,
          page: 1,
          total_pages: 1,
          has_more: false,
        }),
      })
    } else {
      route.continue()
    }
  })
}

async function mockSubscriptionDetail(page: Page, userId: string, detail: unknown) {
  await page.route(`**/api/v1/admin/users/${userId}/subscription*`, (route) => {
    if (route.request().method() === 'GET') {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(detail),
      })
    } else {
      route.continue()
    }
  })
}

async function openOverrideModal(page: Page) {
  await page.goto('/admin/users')
  await page.waitForLoadState('networkidle')
  await page.getByRole('button', { name: 'Override subscription' }).click()
  await expect(page.getByRole('dialog', { name: 'Subscription Override' })).toBeVisible()
}

test.describe('Admin subscription override', () => {
  test.beforeEach(async ({ page }) => {
    await setupAdmin(page)
  })

  test('opens the Override modal from the users table and sees the current override', async ({
    page,
  }) => {
    await mockUsersList(page, [FIXTURE_USER])
    await mockSubscriptionDetail(page, FIXTURE_USER.id, DETAIL_WITH_OVERRIDE)

    await openOverrideModal(page)

    await expect(page.getByText('Manual override active')).toBeVisible()
    await expect(page.getByRole('button', { name: 'Remove Override' })).toBeVisible()
  })

  test('does not render Remove Override when no override exists', async ({ page }) => {
    await mockUsersList(page, [{ ...FIXTURE_USER, manual_tier_override: null }])
    await mockSubscriptionDetail(page, FIXTURE_USER.id, DETAIL_NO_OVERRIDE)

    await openOverrideModal(page)

    await expect(page.getByRole('button', { name: 'Save Override' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Remove Override' })).not.toBeVisible()
  })

  test('removes an override: reason reaches the POST body, success toast, table refetches', async ({
    page,
  }) => {
    let usersListCalls = 0
    await page.route('**/api/v1/admin/users*', (route) => {
      if (route.request().method() !== 'GET') {
        route.continue()
        return
      }
      usersListCalls++
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          users: [FIXTURE_USER],
          total_count: 1,
          page: 1,
          total_pages: 1,
          has_more: false,
        }),
      })
    })
    await mockSubscriptionDetail(page, FIXTURE_USER.id, DETAIL_WITH_OVERRIDE)

    let capturedBody: unknown = null
    await page.route(
      `**/api/v1/admin/users/${FIXTURE_USER.id}/subscription/tier/clear`,
      (route) => {
        capturedBody = route.request().postDataJSON()
        route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            stage: 'pending',
            message: 'Complimentary access removal queued. Paid access and trials are unchanged.',
          }),
        })
      }
    )

    await page.goto('/admin/users')
    await page.waitForLoadState('networkidle')
    const callsBeforeRemoval = usersListCalls

    await page.getByRole('button', { name: 'Override subscription' }).click()
    await expect(page.getByRole('dialog', { name: 'Subscription Override' })).toBeVisible()

    await page.getByLabel(/reason/i).fill('Support case #123')
    await page.getByRole('button', { name: 'Remove Override' }).click()

    const confirmDialog = page.getByRole('alertdialog')
    await expect(confirmDialog).toBeVisible()
    await confirmDialog.getByLabel(/type "REMOVE" to confirm/i).fill('REMOVE')
    await confirmDialog.getByRole('button', { name: 'Remove Override' }).click()

    await expect(page.getByText('Complimentary access removal queued. Paid access and trials are unchanged.')).toBeVisible()

    // Point 3's highest-value assertion: the reason must reach the request BODY (the whole
    // reason the endpoint is POST rather than DELETE), not just the URL/method.
    expect(capturedBody).toEqual({ reason: 'Support case #123' })

    await expect.poll(() => usersListCalls).toBeGreaterThan(callsBeforeRemoval)
  })

  test('dismissing the removal confirmation issues no request', async ({ page }) => {
    await mockUsersList(page, [FIXTURE_USER])
    await mockSubscriptionDetail(page, FIXTURE_USER.id, DETAIL_WITH_OVERRIDE)

    let clearRequested = false
    await page.route(
      `**/api/v1/admin/users/${FIXTURE_USER.id}/subscription/tier/clear`,
      (route) => {
        clearRequested = true
        route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            stage: 'pending',
            message: 'Complimentary access removal queued. Paid access and trials are unchanged.',
          }),
        })
      }
    )

    await openOverrideModal(page)

    await page.getByLabel(/reason/i).fill('Support case #123')
    await page.getByRole('button', { name: 'Remove Override' }).click()

    const confirmDialog = page.getByRole('alertdialog')
    await expect(confirmDialog).toBeVisible()
    await confirmDialog.getByRole('button', { name: 'Cancel' }).click()
    await expect(confirmDialog).not.toBeVisible()

    expect(clearRequested).toBe(false)
  })
})
