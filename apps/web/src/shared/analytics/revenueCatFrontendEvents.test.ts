import { describe, it, expect } from 'bun:test'
import { ANALYTICS_EVENTS, normalizeAnalyticsEventPayload } from '@/shared/analytics'
import { isPiiKey } from '@/shared/lib/piiScrubbing'

/**
 * #1652 todo 11 (D4): every RevenueCat frontend event must survive the REAL
 * `normalizeAnalyticsEventPayload`. A wrong contract is dropped silently — `captureProductEvent`
 * returns early on a null normalization — so asserting `useTrackEvent` was called proves nothing
 * about whether the event ever leaves the browser. The payloads below mirror the exact objects the
 * production call sites pass; the `callSite` field names where each one lives.
 */
const RC_FRONTEND_EVENTS: ReadonlyArray<{
  event: string
  callSite: string
  payload?: Record<string, unknown>
}> = [
  {
    event: ANALYTICS_EVENTS.ADMIN_REVENUECAT_REPLAY_QUEUED,
    callSite: 'features/admin/components/RevenueCatPage.tsx:57',
  },
  {
    event: ANALYTICS_EVENTS.ADMIN_REVENUECAT_REFRESH_QUEUED,
    callSite: 'features/admin/components/RevenueCatUserPanel.tsx:53',
  },
  {
    event: ANALYTICS_EVENTS.ADMIN_REVENUECAT_RETRY_QUEUED,
    callSite: 'features/admin/components/RevenueCatUserPanel.tsx:58',
  },
  {
    event: ANALYTICS_EVENTS.ADMIN_REVENUECAT_GRANT_QUEUED,
    callSite: 'features/admin/components/RevenueCatUserPanel.tsx:63',
    payload: { grant_class: 'comp', tier: 'household_2', revoke: false },
  },
  {
    event: ANALYTICS_EVENTS.ADMIN_SUBSCRIPTION_CHANGE_QUEUED,
    callSite: 'features/admin/components/SubscriptionOverrideModal.tsx:157,212',
    payload: { action: 'grant', tier: 'premium' },
  },
  {
    event: ANALYTICS_EVENTS.CHECKOUT_STARTED,
    callSite: 'features/billing/hooks/useRevenueCatCheckout.ts:33',
  },
  {
    event: ANALYTICS_EVENTS.CHECKOUT_CANCELLED,
    callSite: 'features/billing/hooks/useRevenueCatCheckout.ts:34',
  },
  {
    event: ANALYTICS_EVENTS.CHECKOUT_CONFIRMATION_PENDING,
    callSite: 'features/billing/hooks/useRevenueCatCheckout.ts:35',
  },
  {
    event: ANALYTICS_EVENTS.CHECKOUT_COMPLETED,
    callSite: 'features/billing/hooks/useRevenueCatCheckout.ts:37',
  },
  {
    event: ANALYTICS_EVENTS.SUBSCRIPTION_MANAGED,
    callSite: 'features/billing/components/BillingPortalButton.tsx',
  },
]

describe('RevenueCat frontend event contracts (#1652 todo 11)', () => {
  it('asserts against a PII matcher that actually rejects PII keys', () => {
    expect(isPiiKey('user_email')).toBe(true)
    expect(isPiiKey('customer_amount')).toBe(true)
    expect(isPiiKey('account_id')).toBe(true)
    expect(isPiiKey('grant_class')).toBe(false)
    expect(isPiiKey('tier')).toBe(false)
  })

  for (const { event, callSite, payload } of RC_FRONTEND_EVENTS) {
    it(`${event} survives the real normalizer with no PII-bearing property (${callSite})`, () => {
      const normalized = normalizeAnalyticsEventPayload(event, payload)
      expect(normalized).not.toBeNull()
      for (const key of Object.keys(normalized ?? {})) {
        expect(isPiiKey(key)).toBe(false)
      }
    })
  }

  it('normalizes the bounded grant payload to exactly its three declared properties', () => {
    expect(
      normalizeAnalyticsEventPayload(ANALYTICS_EVENTS.ADMIN_REVENUECAT_GRANT_QUEUED, {
        grant_class: 'trial',
        tier: 'household_5',
        revoke: true,
      }),
    ).toEqual({ grant_class: 'trial', tier: 'household_5', revoke: true })
  })

  it('normalizes the subscription-change payload and drops an absent optional tier', () => {
    expect(
      normalizeAnalyticsEventPayload(ANALYTICS_EVENTS.ADMIN_SUBSCRIPTION_CHANGE_QUEUED, {
        action: 'revoke',
      }),
    ).toEqual({ action: 'revoke' })
  })

  it('strips unreviewed properties from the pre-existing checkout/managed events', () => {
    for (const event of [
      ANALYTICS_EVENTS.CHECKOUT_STARTED,
      ANALYTICS_EVENTS.CHECKOUT_COMPLETED,
      ANALYTICS_EVENTS.SUBSCRIPTION_MANAGED,
    ]) {
      expect(normalizeAnalyticsEventPayload(event, { anything: 'x' })).toEqual({})
    }
  })

  it('drops a wrong contract instead of delivering it (negative controls)', () => {
    expect(
      normalizeAnalyticsEventPayload(ANALYTICS_EVENTS.ADMIN_REVENUECAT_GRANT_QUEUED, {
        grant_class: 'comp',
        tier: 'not_a_tier',
        revoke: false,
      }),
    ).toBeNull()
    expect(
      normalizeAnalyticsEventPayload(ANALYTICS_EVENTS.ADMIN_REVENUECAT_GRANT_QUEUED, {
        grant_class: 'comp',
        tier: 'premium',
        revoke: false,
        reason: 'support correction',
      }),
    ).toBeNull()
    expect(
      normalizeAnalyticsEventPayload(ANALYTICS_EVENTS.ADMIN_SUBSCRIPTION_CHANGE_QUEUED, {
        action: 'upgrade',
      }),
    ).toBeNull()
    expect(
      normalizeAnalyticsEventPayload(ANALYTICS_EVENTS.ADMIN_REVENUECAT_REFRESH_QUEUED, {
        user_id: 'u-1',
      }),
    ).toBeNull()
  })
})
