/**
 * Active-Subscriber Billing Management E2E Tests (Issue #1456, updated by #1652)
 *
 * `/billing/manage` has E2E coverage for the complimentary (comped) view only
 * (comped-account.auth.spec.ts:163-184), which asserts *Change Plan* and
 * *Cancel Subscription* are ABSENT for a comped user. This file covers the opposite state:
 * a user with a real, active paid subscription.
 *
 * #1652 pivoted billing self-service to a single "Manage subscription" action that opens the
 * provider's own portal (Paddle stays merchant of record). The in-app Change Plan / Cancel
 * Subscription mutations are gone, so this spec now asserts the resolved management link and
 * the billing details (next-billing date + amount) that the comped view deliberately suppresses.
 *
 * Uses API mocking (page.route) — no live Paddle subscription required. Modeled directly
 * on comped-account.auth.spec.ts: same mocking helpers, role-based selectors, and the
 * `.auth.spec.ts` naming that Playwright projects actually pick up.
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

const PORTAL_URL = 'https://sandbox-vendors.paddle.com/subscription-portal/test'

async function mockTiers(page: Page) {
  await page.route('**/api/v1/subscription/tiers*', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(TIERS_FIXTURE),
    })
  )
}

/**
 * Mocks `GET /subscription/current` for a real, active, paid subscriber:
 * `status: 'active'`, `is_comped: false`, with a concrete `next_billing_date` and
 * `amount` — precisely the fields the comped view (comped-account.auth.spec.ts:179-180)
 * asserts are absent.
 */
async function mockActiveSubscriptionCurrent(page: Page) {
  await page.route('**/api/v1/subscription/current*', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        status: 'active',
        is_comped: false,
        tier: 'premium',
        billing_interval: 'month',
        currency: 'USD',
        cancel_at_period_end: false,
        subscription_id: 'sub_test_active_123',
        next_billing_date: '2026-08-31',
        amount: '8.99',
      }),
    })
  )
}

async function mockBillingPortal(page: Page) {
  await page.route('**/api/v1/subscription/billing-portal*', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ portal_url: PORTAL_URL }),
    })
  )
}

test.describe('Active-subscriber billing management', () => {
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

    await mockAuthDependencies(page, { subscriptionStatus: 'active', isComped: false })
    await mockTiers(page)
    await mockActiveSubscriptionCurrent(page)
    await mockBillingPortal(page)

    await page.goto('/billing/manage')
    await page.waitForLoadState('networkidle')
    await expect(page.getByRole('heading', { name: 'Manage Subscription' })).toBeVisible()
  })

  test('shows the resolved Manage subscription link for an active paid subscriber', async ({
    page,
  }) => {
    const manage = page.getByRole('link', { name: 'Manage subscription' })
    await expect(manage).toBeVisible()
    await expect(manage).toHaveAttribute('href', PORTAL_URL)
    await expect(manage).toHaveAttribute('target', '_blank')
  })

  test('does not render in-app Change Plan or Cancel Subscription controls', async ({ page }) => {
    await expect(page.getByRole('button', { name: 'Change Plan' })).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'Cancel Subscription' })).toHaveCount(0)
  })

  test('renders the next-billing date and amount (asserted absent in the comped view)', async ({
    page,
  }) => {
    // Scoped to the BillingDetails region: "USD 8.99" also appears in
    // CurrentSubscriptionDisplay's pricing block, so an unscoped getByText() would be a
    // strict-mode violation. BillingDetails is the component the comped-account spec
    // asserts is never rendered for a comped user (comped-account.auth.spec.ts:176-180) —
    // this is the presence counterpart of that absence assertion.
    const billingDetails = page.getByRole('region', { name: 'Billing details' })
    await expect(billingDetails.getByText('Next Billing Date:')).toBeVisible()
    await expect(billingDetails.getByText('2026-08-31')).toBeVisible()
    await expect(billingDetails.getByText('Amount:', { exact: true })).toBeVisible()
    await expect(billingDetails.getByText('USD 8.99')).toBeVisible()
  })
})
