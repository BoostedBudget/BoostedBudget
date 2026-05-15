import { describe, it, expect, beforeEach } from 'bun:test'
import React from 'react'
import { renderHook } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { useSubscriptionManagement } from './useSubscriptionManagement'
import {
  mockUseGetCurrentSubscription,
  mockUseUpdatePaddleSubscription,
  mockUseCancelPaddleSubscription,
  mockUseGetBillingPortalUrl,
  mockUpdateSubscriptionMutateAsync,
  mockCancelSubscriptionMutateAsync,
} from '@/test/mocks/subscription-api'

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

const defaultMutationState = {
  isPending: false,
  isSuccess: false,
  isError: false,
  error: null,
  reset: () => {},
}

describe('useSubscriptionManagement', () => {
  beforeEach(() => {
    mockUseGetCurrentSubscription.mockReturnValue(defaultQueryState)
    mockUseUpdatePaddleSubscription.mockReturnValue({
      mutateAsync: mockUpdateSubscriptionMutateAsync,
      ...defaultMutationState,
    })
    mockUseCancelPaddleSubscription.mockReturnValue({
      mutateAsync: mockCancelSubscriptionMutateAsync,
      ...defaultMutationState,
    })
    mockUseGetBillingPortalUrl.mockReturnValue(defaultQueryState)
    mockUpdateSubscriptionMutateAsync.mockClear()
    mockCancelSubscriptionMutateAsync.mockClear()
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

  describe('updateSubscription', () => {
    it('includes current_billing_interval in mutation payload when provided', async () => {
      mockUpdateSubscriptionMutateAsync.mockResolvedValue({})
      const { result } = renderHook(() => useSubscriptionManagement(), {
        wrapper: createWrapper(),
      })

      await result.current.updateSubscription({
        newTier: 'household_2',
        billingInterval: 'year',
        currentBillingInterval: 'month',
      })

      expect(mockUpdateSubscriptionMutateAsync).toHaveBeenCalledWith({
        data: expect.objectContaining({
          new_tier: 'household_2',
          billing_interval: 'year',
          current_billing_interval: 'month',
        }),
      })
    })

    it('omits current_billing_interval when not provided', async () => {
      mockUpdateSubscriptionMutateAsync.mockResolvedValue({})
      const { result } = renderHook(() => useSubscriptionManagement(), {
        wrapper: createWrapper(),
      })

      await result.current.updateSubscription({
        newTier: 'premium',
        billingInterval: 'month',
      })

      const call = mockUpdateSubscriptionMutateAsync.mock.calls[0]?.[0]
      expect(call.data.current_billing_interval).toBeUndefined()
    })
  })
})
