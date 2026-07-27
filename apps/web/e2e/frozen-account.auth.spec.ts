/**
 * Frozen Account E2E Tests (Issue #1413/#1414 — Task 15)
 *
 * Closes the zero-coverage gap for read-only accounts: a genuinely `inactive`, NOT comped
 * user must be frozen everywhere — expired banner, disabled write controls — and a status
 * the frontend doesn't recognise must never leave the user stranded: it has to bounce them
 * out to /pricing rather than silently granting (or half-granting) access.
 *
 * Uses API mocking (page.route) — no live backend subscription state required.
 */

import { test, expect, type Page } from '@playwright/test'
import { mockAuthDependencies } from './helpers/api-mocks'

async function mockTiers(page: Page) {
  await page.route('**/api/v1/subscription/tiers*', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
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
      }),
    })
  )
}

/** No subscription on file — the ordinary state for a genuinely inactive user. */
async function mockNoSubscriptionCurrent(page: Page) {
  await page.route('**/api/v1/subscription/current*', (route) =>
    route.fulfill({
      status: 404,
      contentType: 'application/json',
      body: JSON.stringify({ error: 'not found' }),
    })
  )
}

test.describe('Frozen account', () => {
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

  test('sees the expired banner on /dashboard', async ({ page }) => {
    await mockAuthDependencies(page, { subscriptionStatus: 'inactive' })

    await page.goto('/dashboard')
    await page.waitForLoadState('networkidle')

    await expect(page.getByRole('alert')).toContainText(/your subscription has expired/i)
  })

  test('write controls are disabled/inert via ReadOnlyGuard on /dashboard', async ({ page }) => {
    await mockAuthDependencies(page, { subscriptionStatus: 'inactive' })

    await page.goto('/dashboard')
    await page.waitForLoadState('networkidle')

    // ReadOnlyGuard wraps write controls in an `inert` container carrying a descriptive
    // aria-label/title — this is the actual mechanism that disables them (mouse, keyboard,
    // and assistive technology alike), so assert on it directly rather than a CSS class.
    const guardWrapper = page.locator('[title="Resubscribe to customize your dashboard"]').first()
    await expect(guardWrapper).toBeVisible()
    await expect(guardWrapper).toHaveAttribute('inert', '')
    await expect(guardWrapper.locator('[aria-disabled="true"]')).toBeVisible()

    // `inert` removes the subtree from the accessibility tree entirely, so the Edit button
    // is not reachable via an accessible role query while read-only.
    await expect(page.getByRole('button', { name: 'Edit' })).toHaveCount(0)
  })

  test('an unrecognised subscription status redirects to /pricing and shows the frozen banner', async ({
    page,
  }) => {
    await mockAuthDependencies(page, { subscriptionStatus: 'some_future_status_xyz' })
    await mockTiers(page)
    await mockNoSubscriptionCurrent(page)

    await page.goto('/dashboard')

    await expect(page).toHaveURL(/\/pricing/)
    await expect(page.getByText(/your subscription has expired/i)).toBeVisible()
  })
})
