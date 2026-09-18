import type { Page } from '@playwright/test'
import type { PrivacySettings } from '@boostedbudget/api-client/models'

/**
 * Mock endpoints required for any authenticated page to render through PremiumRoute.
 *
 * PremiumRoute calls GET /api/v1/user/profile before rendering its children. If this
 * endpoint is not mocked, a dead backend causes all mocked tests to fail even when
 * their actual data APIs are fully intercepted.
 *
 * Call this from every test file that uses page.route() to mock data APIs.
 *
 * Issue #1413: `effectiveStatus` / `isComped` / `compTier` / `compEndsAt` let a spec present a
 * comped or frozen user without hand-rolling the profile route. All four are additive with
 * defaults matching pre-#1413 behavior (`isComped: false`, the rest absent from the JSON body via
 * `JSON.stringify` dropping `undefined`) — existing callers are unaffected.
 */
export async function mockAuthDependencies(
  page: Page,
  overrides?: {
    subscriptionStatus?: string
    isAdmin?: boolean
    effectiveStatus?: string
    isComped?: boolean
    compTier?: string
    compEndsAt?: string
  }
) {
  const {
    subscriptionStatus = 'active',
    isAdmin = false,
    effectiveStatus,
    isComped = false,
    compTier,
    compEndsAt,
  } = overrides ?? {}

  // Authenticated startup reads server-authoritative privacy settings. Keep PUT support for
  // specs that exercise explicit changes from the privacy settings page.
  const privacy: PrivacySettings = {
    id: 'privacy-123',
    user_id: 'user-123',
    gdpr_necessary_consent: true,
    gdpr_analytics_consent: false,
    gdpr_consent_timestamp: '2026-01-01T00:00:00Z',
    marketing_emails_enabled: false,
    product_updates_enabled: true,
    security_alerts_enabled: true,
    data_sharing_enabled: false,
    include_in_aggregate_stats: true,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }
  await page.route(url => url.pathname === '/api/v1/user/privacy', route => {
    const method = route.request().method()
    if (method !== 'GET' && method !== 'PUT') return route.fallback()
    if (method === 'PUT') {
      const updates = route.request().postDataJSON() as Record<string, unknown> | null
      for (const field of [
        'gdpr_necessary_consent', 'gdpr_analytics_consent',
        'marketing_emails_enabled', 'product_updates_enabled', 'security_alerts_enabled',
        'data_sharing_enabled', 'include_in_aggregate_stats',
      ] as const) {
        if (typeof updates?.[field] === 'boolean') privacy[field] = updates[field]
      }
    }
    return route.fulfill({ status: 200, json: privacy })
  })

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
        effective_status: effectiveStatus,
        is_comped: isComped,
        comp_tier: compTier,
        comp_ends_at: compEndsAt,
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

  // Issue #952: NotificationBell is now in the Header on all authenticated pages.
  // Mock notification endpoints to prevent unmocked API calls causing retries/re-renders.
  await page.route('**/api/v1/notifications/unread/count*', (route) => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ count: 0 }),
    })
  })

  await page.route('**/api/v1/notifications*', (route) => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([]),
    })
  })
}

/**
 * All 18 PFCv2 primary categories — curated, canonical source for e2e tests.
 * Used in category-editor.auth.spec.ts and rule-editor-pfc.auth.spec.ts.
 */
export const ALL_PFC_PRIMARIES = [
  { enum: 'BANK_FEES', display: 'Bank Fees' },
  { enum: 'ENTERTAINMENT', display: 'Entertainment' },
  { enum: 'FOOD_AND_DRINK', display: 'Food And Drink' },
  { enum: 'GENERAL_MERCHANDISE', display: 'General Merchandise' },
  { enum: 'GENERAL_SERVICES', display: 'General Services' },
  { enum: 'GOVERNMENT_AND_NON_PROFIT', display: 'Government And Non Profit' },
  { enum: 'HOME_IMPROVEMENT', display: 'Home Improvement' },
  { enum: 'INCOME', display: 'Income' },
  { enum: 'LOAN_DISBURSEMENTS', display: 'Loan Disbursements' },
  { enum: 'LOAN_PAYMENTS', display: 'Loan Payments' },
  { enum: 'MEDICAL', display: 'Medical' },
  { enum: 'OTHER', display: 'Other' },
  { enum: 'PERSONAL_CARE', display: 'Personal Care' },
  { enum: 'RENT_AND_UTILITIES', display: 'Rent And Utilities' },
  { enum: 'TRANSFER_IN', display: 'Transfer In' },
  { enum: 'TRANSFER_OUT', display: 'Transfer Out' },
  { enum: 'TRANSPORTATION', display: 'Transportation' },
  { enum: 'TRAVEL', display: 'Travel' },
]

/**
 * Subset of PFCv2 detailed categories for cascading selector tests.
 * Used in rule-editor-pfc.auth.spec.ts.
 */
export const FOOD_AND_DRINK_DETAILED = [
  { primary_enum: 'FOOD_AND_DRINK', primary_display: 'Food And Drink', enum: 'FOOD_AND_DRINK_COFFEE', display: 'Coffee' },
  { primary_enum: 'FOOD_AND_DRINK', primary_display: 'Food And Drink', enum: 'FOOD_AND_DRINK_FAST_FOOD', display: 'Fast Food' },
  { primary_enum: 'FOOD_AND_DRINK', primary_display: 'Food And Drink', enum: 'FOOD_AND_DRINK_GROCERIES', display: 'Groceries' },
  { primary_enum: 'FOOD_AND_DRINK', primary_display: 'Food And Drink', enum: 'FOOD_AND_DRINK_RESTAURANT', display: 'Restaurant' },
  { primary_enum: 'TRANSPORTATION', primary_display: 'Transportation', enum: 'TRANSPORTATION_GAS', display: 'Gas' },
  { primary_enum: 'TRANSPORTATION', primary_display: 'Transportation', enum: 'TRANSPORTATION_TAXIS_AND_RIDE_SHARES', display: 'Taxis And Ride Shares' },
]
