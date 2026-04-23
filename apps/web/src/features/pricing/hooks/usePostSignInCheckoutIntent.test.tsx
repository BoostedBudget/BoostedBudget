import { describe, it, expect, beforeEach, mock, afterEach } from 'bun:test'
import React from 'react'
import { renderHook } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { usePostSignInCheckoutIntent } from './usePostSignInCheckoutIntent'
import { mockUseUser } from '@/test/mocks/clerk'
import {
  mockUseCreatePaddleCheckout,
  mockCreatePaddleCheckoutMutateAsync,
} from '@/test/mocks/subscription-api'

function createWrapper() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        <MemoryRouter>{children}</MemoryRouter>
      </QueryClientProvider>
    )
  }
}

const signedInUser = {
  isLoaded: true,
  isSignedIn: true,
  user: { id: 'user_123', publicMetadata: { subscription_tier: 'premium' } },
}

beforeEach(() => {
  mockUseUser.mockReturnValue({ isLoaded: true, isSignedIn: false, user: null })
  mockUseCreatePaddleCheckout.mockReturnValue({
    mutateAsync: mockCreatePaddleCheckoutMutateAsync,
    isPending: false,
    isSuccess: false,
    isError: false,
    error: null,
    reset: mock(() => {}),
  })
  mockCreatePaddleCheckoutMutateAsync.mockResolvedValue({
    checkout_url: 'https://checkout.paddle.com/txn_123',
    transaction_id: 'txn_123',
  })
  sessionStorage.clear()
  mockCreatePaddleCheckoutMutateAsync.mockClear()
})

afterEach(() => {
  sessionStorage.clear()
})

describe('usePostSignInCheckoutIntent', () => {
  it('does nothing when user is not signed in', () => {
    sessionStorage.setItem('checkout_intent', JSON.stringify({ tier: 'premium', billingInterval: 'month' }))
    renderHook(() => usePostSignInCheckoutIntent(), { wrapper: createWrapper() })
    expect(mockCreatePaddleCheckoutMutateAsync).not.toHaveBeenCalled()
  })

  it('does nothing when no intent is stored', () => {
    mockUseUser.mockReturnValue(signedInUser)
    renderHook(() => usePostSignInCheckoutIntent(), { wrapper: createWrapper() })
    expect(mockCreatePaddleCheckoutMutateAsync).not.toHaveBeenCalled()
  })

  it('consumes sessionStorage intent and initiates checkout when signed in', async () => {
    mockUseUser.mockReturnValue(signedInUser)
    sessionStorage.setItem('checkout_intent', JSON.stringify({ tier: 'premium', billingInterval: 'month' }))

    renderHook(() => usePostSignInCheckoutIntent(), { wrapper: createWrapper() })

    await new Promise((r) => setTimeout(r, 50))
    expect(mockCreatePaddleCheckoutMutateAsync).toHaveBeenCalledWith(
      expect.objectContaining({ data: expect.objectContaining({ tier: 'premium', billing_interval: 'month' }) })
    )
  })

  it('clears sessionStorage intent after consuming it', async () => {
    mockUseUser.mockReturnValue(signedInUser)
    sessionStorage.setItem('checkout_intent', JSON.stringify({ tier: 'premium', billingInterval: 'month' }))

    renderHook(() => usePostSignInCheckoutIntent(), { wrapper: createWrapper() })
    await new Promise((r) => setTimeout(r, 50))

    expect(sessionStorage.getItem('checkout_intent')).toBeNull()
  })

  it('ignores stale intent older than 10 minutes', async () => {
    mockUseUser.mockReturnValue(signedInUser)
    const staleTimestamp = Date.now() - 11 * 60 * 1000
    sessionStorage.setItem('checkout_intent', JSON.stringify({
      tier: 'premium',
      billingInterval: 'month',
      storedAt: staleTimestamp,
    }))

    renderHook(() => usePostSignInCheckoutIntent(), { wrapper: createWrapper() })
    await new Promise((r) => setTimeout(r, 50))

    expect(mockCreatePaddleCheckoutMutateAsync).not.toHaveBeenCalled()
    expect(sessionStorage.getItem('checkout_intent')).toBeNull()
  })

  it('processes fresh intent (within 10 minutes)', async () => {
    mockUseUser.mockReturnValue(signedInUser)
    const freshTimestamp = Date.now() - 2 * 60 * 1000
    sessionStorage.setItem('checkout_intent', JSON.stringify({
      tier: 'household_2',
      billingInterval: 'year',
      storedAt: freshTimestamp,
    }))

    renderHook(() => usePostSignInCheckoutIntent(), { wrapper: createWrapper() })
    await new Promise((r) => setTimeout(r, 50))

    expect(mockCreatePaddleCheckoutMutateAsync).toHaveBeenCalledWith(
      expect.objectContaining({ data: expect.objectContaining({ tier: 'household_2', billing_interval: 'year' }) })
    )
  })

  it('does not fire twice on StrictMode double-invoke (single-fire guard)', async () => {
    mockUseUser.mockReturnValue(signedInUser)
    sessionStorage.setItem('checkout_intent', JSON.stringify({ tier: 'premium', billingInterval: 'month' }))

    const { rerender } = renderHook(() => usePostSignInCheckoutIntent(), { wrapper: createWrapper() })
    rerender()
    await new Promise((r) => setTimeout(r, 50))

    expect(mockCreatePaddleCheckoutMutateAsync).toHaveBeenCalledTimes(1)
  })
})
