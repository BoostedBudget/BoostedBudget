/**
 * Comped Account E2E Tests (Issue #1413/#1414 — Task 15)
 *
 * A user whose raw `subscription_status` is `inactive` but who has a live admin comp
 * (`is_comped: true`, no `comp_ends_at` — a permanent comp) must be treated as a fully
 * active user everywhere the raw status would otherwise say "expired":
 *   - /dashboard: full write access, no "subscription has expired" banner
 *   - /profile: a Complimentary badge, never a destructive "inactive" badge
 *   - /pricing: no frozen-account banner
 *   - /billing/manage: the complimentary view, with no next-billing date and no amount
 *     (the billing page computes `next_billing_date ?? current_period_end`, so a
 *     regression in `is_comped` resolution would show a real user a charge they are
 *     not being billed for)
 *
 * Uses API mocking (page.route) — no live backend subscription state required.
 */

import { test, expect, type Page } from '@playwright/test'
import { mockAuthDependencies } from './helpers/api-mocks'

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

const TIERS_FIXTURE = {
  tiers: {
    premium: {
      name: 'premium',
      display_name: 'Premium',
      description: 'For individuals',
      pricing: {
        price_monthly: 8.99,
        price_yearly: 89.99,
        mobile_price_yearly: 89.99,
        users_included: 1,
      },
      features: {
        priority_support: false,
        advanced_reports: false,
        api_access: false,
        custom_categories: true,
        custom_tags: true,
      },
      limits: { transaction_history_days: 730 },
    },
  },
}

async function mockTiers(page: Page) {
  await page.route('**/api/v1/subscription/tiers*', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(TIERS_FIXTURE),
    })
  )
}

async function mockRegions(page: Page) {
  await page.route('**/api/v1/regions*', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ regions: {}, default_region: 'us-east-1' }),
    })
  )
}

/**
 * Mocks `GET /subscription/current` the way the brief specifies for a comped user:
 * `status: 'active'` + `is_comped: true`, with `next_billing_date`, `current_period_end`,
 * and `amount` deliberately absent (permanent comp, nothing to bill).
 */
async function mockCompedSubscriptionCurrent(page: Page) {
  await page.route('**/api/v1/subscription/current*', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        status: 'active',
        is_comped: true,
        tier: 'premium',
        billing_interval: 'month',
        currency: 'USD',
        cancel_at_period_end: false,
        subscription_id: '',
        // next_billing_date, current_period_end, amount: deliberately absent
      }),
    })
  )
}

test.describe('Comped account', () => {
  test.beforeEach(async ({ page }) => {
    await page.addInitScript(() => {
      const consent = {
        consents: { necessary: true, analytics: false, advertising: false },
        timestamp: '2026-01-01T00:00:00.000Z',
        version: '2.0',
      }
      document.cookie = `boostedbudget_cookie_consent=${encodeURIComponent(
        JSON.stringify(consent)
      )}; path=/; SameSite=Strict`
    })
  })

  test('reaches /dashboard with full write access and no subscription-expired banner', async ({
    page,
  }) => {
    await mockAuthDependencies(page, { subscriptionStatus: 'inactive', isComped: true })

    await page.goto('/dashboard')
    await page.waitForLoadState('networkidle')

    await expect(page.getByRole('heading', { name: 'Dashboard' })).toBeVisible()
    await expect(page.getByText(/subscription has expired/i)).not.toBeVisible()

    // Not read-only: ReadOnlyGuard renders its children un-wrapped (no inert container)
    // when write access is present, so the Edit button is a real, clickable button.
    const editButton = page.getByRole('button', { name: 'Edit' })
    await expect(editButton).toBeVisible()
    await expect(editButton).toBeEnabled()
    await editButton.click()
    await expect(page.getByRole('button', { name: 'Add Widget' })).toBeVisible()
  })

  test('shows a Complimentary badge on /profile, never a destructive inactive badge', async ({
    page,
  }) => {
    await mockAuthDependencies(page, { subscriptionStatus: 'inactive', isComped: true })
    await mockTiers(page)
    await mockRegions(page)

    await page.goto('/profile')
    await page.waitForLoadState('networkidle')

    await expect(page.getByText('Complimentary')).toBeVisible()
    await expect(page.getByText('Inactive', { exact: true })).not.toBeVisible()
  })

  test('shows no frozen-account banner on /pricing', async ({ page }) => {
    await mockAuthDependencies(page, { subscriptionStatus: 'inactive', isComped: true })
    await mockTiers(page)
    await mockCompedSubscriptionCurrent(page)

    await page.goto('/pricing')
    await page.waitForLoadState('networkidle')

    await expect(page.getByText(/complimentary/i).first()).toBeVisible()
    await expect(page.getByText(/your subscription has expired/i)).not.toBeVisible()
  })

  test('billing/manage shows the complimentary view with no next-billing date and no amount', async ({
    page,
  }) => {
    await mockAuthDependencies(page, { subscriptionStatus: 'inactive', isComped: true })
    await mockTiers(page)
    await mockCompedSubscriptionCurrent(page)

    await page.goto('/billing/manage')
    await page.waitForLoadState('networkidle')

    await expect(page.getByRole('heading', { name: 'Manage Subscription' })).toBeVisible()
    await expect(page.getByText(/complimentary premium|complimentary access/i)).toBeVisible()

    // BillingDetails (the component that renders "Next Billing Date:" / "Amount:") is never
    // rendered for a comped user — asserting its labels are absent is what would catch a
    // regression where a comped user falls through to the regular billed-subscription view.
    await expect(page.getByText('Next Billing Date:')).not.toBeVisible()
    await expect(page.getByText('Amount:', { exact: true })).not.toBeVisible()
    await expect(page.getByRole('button', { name: 'Change Plan' })).not.toBeVisible()
    await expect(page.getByRole('button', { name: 'Cancel Subscription' })).not.toBeVisible()
    await expect(page.getByRole('button', { name: 'View Plans' })).toBeVisible()
  })
})
