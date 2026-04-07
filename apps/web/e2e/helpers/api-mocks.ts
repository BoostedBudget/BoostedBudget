import type { Page } from '@playwright/test'

/**
 * Mock endpoints required for any authenticated page to render through PremiumRoute.
 *
 * PremiumRoute calls GET /api/v1/user/profile before rendering its children. If this
 * endpoint is not mocked, a dead backend causes all mocked tests to fail even when
 * their actual data APIs are fully intercepted.
 *
 * Call this from every test file that uses page.route() to mock data APIs.
 */
export async function mockAuthDependencies(
  page: Page,
  overrides?: {
    subscriptionStatus?: string
    isAdmin?: boolean
  }
) {
  const { subscriptionStatus = 'active', isAdmin = false } = overrides ?? {}

  await page.route('**/api/v1/user/profile*', (route) => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        id: 'user-123',
        clerk_user_id: 'user_test123',
        email: 'test@example.com',
        subscription_status: subscriptionStatus,
        subscription_tier: 'premium',
        primary_region: 'us-east-1',
        created_at: '2025-01-01T00:00:00Z',
        updated_at: '2025-01-01T00:00:00Z',
      }),
    })
  })

  await page.route('**/api/v1/admin/check-role*', (route) => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ is_admin: isAdmin }),
    })
  })
}
