import { describe, it, expect, beforeEach } from 'bun:test'
import React from 'react'
import { renderHook } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { useSubscriptionManagement } from './useSubscriptionManagement'
import { mockUseAuth, createMockClerkAuth } from '@/test/mocks/clerk'
import {
  mockUseGetCurrentSubscription,
  mockUseGetBillingPortalUrl,
} from '@/test/mocks/subscription-api'
import { mockUseGetUserProfile } from '@/test/mocks/profile-api'

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  })
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
}

const defaultQueryState = {
  data: undefined,
  isLoading: false,
  error: null,
  refetch: () => Promise.resolve({ data: undefined, error: null }),
}

describe('useSubscriptionManagement', () => {
  beforeEach(() => {
    mockUseGetCurrentSubscription.mockReturnValue(defaultQueryState)
    mockUseGetBillingPortalUrl.mockClear()
    mockUseGetBillingPortalUrl.mockReturnValue(defaultQueryState)
    // Non-comped by default (the hook also consults the profile-derived presentation as a
    // resilience fallback for comped users, Issue #1413).
    mockUseGetUserProfile.mockClear()
    mockUseGetUserProfile.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve(),
    })
  })

  describe('Loading state', () => {
    it('returns isLoadingSubscription true when query is loading', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        ...defaultQueryState,
        isLoading: true,
      })

      const { result } = renderHook(() => useSubscriptionManagement(), {
        wrapper: createWrapper(),
      })

      expect(result.current.isLoadingSubscription).toBe(true)
    })
  })

  describe('404 / hasNoSubscription', () => {
    it('returns hasNoSubscription true when error status is 404', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        ...defaultQueryState,
        error: { status: 404, message: 'Not Found' },
      })

      const { result } = renderHook(() => useSubscriptionManagement(), {
        wrapper: createWrapper(),
      })

      expect(result.current.hasNoSubscription).toBe(true)
    })

    it('returns hasNoSubscription true when error response.status is 404', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        ...defaultQueryState,
        error: { response: { status: 404 }, message: 'Not Found' },
      })

      const { result } = renderHook(() => useSubscriptionManagement(), {
        wrapper: createWrapper(),
      })

      expect(result.current.hasNoSubscription).toBe(true)
    })

    it('returns subscriptionError null when error is 404', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        ...defaultQueryState,
        error: { status: 404, message: 'Not Found' },
      })

      const { result } = renderHook(() => useSubscriptionManagement(), {
        wrapper: createWrapper(),
      })

      expect(result.current.subscriptionError).toBeNull()
    })

    it('returns hasNoSubscription false when error is not 404', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        ...defaultQueryState,
        error: { status: 500, message: 'Server Error' },
      })

      const { result } = renderHook(() => useSubscriptionManagement(), {
        wrapper: createWrapper(),
      })

      expect(result.current.hasNoSubscription).toBe(false)
    })

    it('returns hasNoSubscription false when no error', () => {
      mockUseGetCurrentSubscription.mockReturnValue(defaultQueryState)

      const { result } = renderHook(() => useSubscriptionManagement(), {
        wrapper: createWrapper(),
      })

      expect(result.current.hasNoSubscription).toBe(false)
    })
  })

  describe('Error normalization — no raw HTTP codes', () => {
    it('maps 401 to friendly sign-in message', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        ...defaultQueryState,
        error: { status: 401, message: 'Unauthorized' },
      })

      const { result } = renderHook(() => useSubscriptionManagement(), {
        wrapper: createWrapper(),
      })

      expect(result.current.subscriptionError?.message).toBe(
        'Please sign in to manage your subscription'
      )
      expect(result.current.subscriptionError?.message).not.toContain('401')
    })

    it('maps 403 to friendly permission message', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        ...defaultQueryState,
        error: { status: 403, message: 'Forbidden' },
      })

      const { result } = renderHook(() => useSubscriptionManagement(), {
        wrapper: createWrapper(),
      })

      expect(result.current.subscriptionError?.message).not.toContain('403')
      expect(result.current.subscriptionError?.message).toContain('permission')
    })

    it('maps 500 to friendly service error message', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        ...defaultQueryState,
        error: { status: 500, message: 'Internal Server Error' },
      })

      const { result } = renderHook(() => useSubscriptionManagement(), {
        wrapper: createWrapper(),
      })

      expect(result.current.subscriptionError?.message).not.toContain('500')
      expect(result.current.subscriptionError?.message).not.toContain('Internal Server Error')
    })

    it('maps network errors to friendly message', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        ...defaultQueryState,
        error: { message: 'Network request failed' },
      })

      const { result } = renderHook(() => useSubscriptionManagement(), {
        wrapper: createWrapper(),
      })

      expect(result.current.subscriptionError?.message).toBe(
        'Network error. Please check your connection.'
      )
    })

    it('returns null error when no error', () => {
      mockUseGetCurrentSubscription.mockReturnValue(defaultQueryState)

      const { result } = renderHook(() => useSubscriptionManagement(), {
        wrapper: createWrapper(),
      })

      expect(result.current.subscriptionError).toBeNull()
    })
  })

  describe('Subscription data', () => {
    it('exposes subscription data from query', () => {
      const mockSub = { status: 'active', tier: 'premium', amount: '8.99', currency: 'USD' }
      mockUseGetCurrentSubscription.mockReturnValue({
        ...defaultQueryState,
        data: mockSub,
      })

      const { result } = renderHook(() => useSubscriptionManagement(), {
        wrapper: createWrapper(),
      })

      expect(result.current.subscription).toEqual(mockSub)
    })

    it('exposes refetchSubscription function', () => {
      const { result } = renderHook(() => useSubscriptionManagement(), {
        wrapper: createWrapper(),
      })

      expect(typeof result.current.refetchSubscription).toBe('function')
    })
  })

  describe('Billing portal query gate', () => {
    const portalResponse = { data: { portal_url: 'https://portal.paddle.com/123' }, isLoading: false, error: null, refetch: () => Promise.resolve() }

    it('enables billing portal query for active subscriptions', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        ...defaultQueryState,
        data: { status: 'active', tier: 'premium' },
      })
      mockUseGetBillingPortalUrl.mockReturnValue(portalResponse)

      const { result } = renderHook(() => useSubscriptionManagement(), {
        wrapper: createWrapper(),
      })

      expect(result.current.billingPortalUrl).toBe('https://portal.paddle.com/123')
    })

    it('enables billing portal query for trialing subscriptions', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        ...defaultQueryState,
        data: { status: 'trialing', tier: 'premium' },
      })
      mockUseGetBillingPortalUrl.mockReturnValue(portalResponse)

      const { result } = renderHook(() => useSubscriptionManagement(), {
        wrapper: createWrapper(),
      })

      expect(result.current.billingPortalUrl).toBe('https://portal.paddle.com/123')
    })

    it('enables billing portal query for cancelled subscriptions', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        ...defaultQueryState,
        data: { status: 'cancelled', tier: 'premium' },
      })
      mockUseGetBillingPortalUrl.mockReturnValue(portalResponse)

      const { result } = renderHook(() => useSubscriptionManagement(), {
        wrapper: createWrapper(),
      })

      expect(result.current.billingPortalUrl).toBe('https://portal.paddle.com/123')
    })

    it('enables billing portal query for past_due subscriptions', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        ...defaultQueryState,
        data: { status: 'past_due', tier: 'premium' },
      })
      mockUseGetBillingPortalUrl.mockReturnValue(portalResponse)

      const { result } = renderHook(() => useSubscriptionManagement(), {
        wrapper: createWrapper(),
      })

      expect(result.current.billingPortalUrl).toBe('https://portal.paddle.com/123')
    })

    it('exposes undefined billingPortalUrl when portal query has no data', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        ...defaultQueryState,
        data: { status: 'active', tier: 'premium' },
      })
      mockUseGetBillingPortalUrl.mockReturnValue(defaultQueryState)

      const { result } = renderHook(() => useSubscriptionManagement(), {
        wrapper: createWrapper(),
      })

      expect(result.current.billingPortalUrl).toBeUndefined()
    })
  })

  describe('billing portal error state', () => {
    it('normalizes a portal-URL failure instead of leaving a dead control', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        ...defaultQueryState,
        data: { status: 'active', tier: 'premium', subscription_id: 'sub_123' },
      })
      mockUseGetBillingPortalUrl.mockReturnValue({
        ...defaultQueryState,
        error: { status: 503, message: 'Service Unavailable' },
      })

      const { result } = renderHook(() => useSubscriptionManagement(), {
        wrapper: createWrapper(),
      })

      expect(result.current.billingPortalError?.message).toBe(
        'Subscription service temporarily unavailable'
      )
      expect(result.current.billingPortalError?.message).not.toContain('503')
    })

    it('returns null billingPortalError when the portal resolves', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        ...defaultQueryState,
        data: { status: 'active', tier: 'premium', subscription_id: 'sub_123' },
      })
      mockUseGetBillingPortalUrl.mockReturnValue({
        ...defaultQueryState,
        data: { portal_url: 'https://portal.paddle.com/123' },
      })

      const { result } = renderHook(() => useSubscriptionManagement(), {
        wrapper: createWrapper(),
      })

      expect(result.current.billingPortalError).toBeNull()
      expect(result.current.billingPortalUrl).toBe('https://portal.paddle.com/123')
    })
  })

  // Issue #1413: admin-comped users get real access even though there's no Paddle subscription.
  describe('Comped resilience (#1413)', () => {
    it('partitions billing reads by Clerk identity after account switching', () => {
      mockUseAuth.mockReturnValue(createMockClerkAuth({ userId: 'user_first' }))
      const hook = renderHook(() => useSubscriptionManagement(), { wrapper: createWrapper() })
      const getKey = () => (mockUseGetCurrentSubscription.mock.calls.at(-1) as unknown as [{ query: { queryKey: unknown } }])?.[0]?.query?.queryKey
      const firstKey = getKey()
      mockUseAuth.mockReturnValue(createMockClerkAuth({ userId: 'user_second' }))
      hook.rerender()
      const secondKey = getKey()
      expect(secondKey).not.toEqual(firstKey)
    })
    it('allows billing portal recovery for a paused paid subscription', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        ...defaultQueryState,
        data: { status: 'paused', tier: 'premium', subscription_id: 'sub_paused' },
      } as unknown as ReturnType<typeof mockUseGetCurrentSubscription>)
      renderHook(() => useSubscriptionManagement(), { wrapper: createWrapper() })
      expect(mockUseGetBillingPortalUrl).toHaveBeenLastCalledWith({ query: expect.objectContaining({ enabled: true }) })
    })
    it('keeps the paid billing portal available when complimentary access coexists', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        ...defaultQueryState,
        data: { status: 'active', tier: 'household_2', subscription_id: 'sub_paid', is_comped: true },
      } as unknown as ReturnType<typeof mockUseGetCurrentSubscription>)
      renderHook(() => useSubscriptionManagement(), { wrapper: createWrapper() })
      expect(mockUseGetBillingPortalUrl).toHaveBeenLastCalledWith({ query: expect.objectContaining({ enabled: true }) })
    })
    it('returns hasNoSubscription false for a comped user even when the endpoint 404s (profile-derived)', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        ...defaultQueryState,
        error: { status: 404, message: 'Not Found' },
      })
      mockUseGetUserProfile.mockReturnValue({
        data: {
          id: 'user-1',
          subscription_status: 'inactive',
          subscription_tier: 'premium',
          is_comped: true,
        },
        isLoading: false,
        error: null,
        refetch: () => Promise.resolve(),
      })

      const { result } = renderHook(() => useSubscriptionManagement(), {
        wrapper: createWrapper(),
      })

      expect(result.current.hasNoSubscription).toBe(false)
      expect(result.current.isComped).toBe(true)
      expect(result.current.subscriptionError).toBeNull()
    })

    it('returns isComped true when the subscription payload itself reports is_comped', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        ...defaultQueryState,
        data: {
          status: 'active',
          tier: 'premium',
          is_comped: true,
          comp_ends_at: '2026-08-01T00:00:00Z',
        },
      })

      const { result } = renderHook(() => useSubscriptionManagement(), {
        wrapper: createWrapper(),
      })

      expect(result.current.isComped).toBe(true)
      expect(result.current.compEndsAt).toBe('2026-08-01T00:00:00Z')
    })

    it('does not enable the billing-portal query for a comped user', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        ...defaultQueryState,
        data: { status: 'active', tier: 'premium', is_comped: true },
      })

      renderHook(() => useSubscriptionManagement(), { wrapper: createWrapper() })

      expect(mockUseGetBillingPortalUrl).toHaveBeenCalledWith(
        expect.objectContaining({ query: expect.objectContaining({ enabled: false }) })
      )
    })

    it('regression: non-comped 404 still returns hasNoSubscription true', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        ...defaultQueryState,
        error: { status: 404, message: 'Not Found' },
      })

      const { result } = renderHook(() => useSubscriptionManagement(), {
        wrapper: createWrapper(),
      })

      expect(result.current.hasNoSubscription).toBe(true)
      expect(result.current.isComped).toBe(false)
    })
  })
})
