import { describe, it, expect, beforeEach, afterEach, mock, spyOn } from 'bun:test'
import React from 'react'
import { renderHook } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router'
import { usePostSignInCheckoutIntent } from './usePostSignInCheckoutIntent'
import { mockUseUser } from '@/test/mocks/clerk'
import { revenueCatCheckout } from '../../billing/revenueCatRuntime'

const mockStartCheckout = mock(async () => {})
let restoreStartCheckout = () => {}

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
  sessionStorage.clear()
  const startSpy = spyOn(revenueCatCheckout, 'start').mockImplementation(mockStartCheckout)
  restoreStartCheckout = () => { startSpy.mockRestore() }
  mockStartCheckout.mockClear()
})

afterEach(() => {
  restoreStartCheckout()
  sessionStorage.clear()
})

describe('usePostSignInCheckoutIntent', () => {
  it('does nothing when user is not signed in', () => {
    sessionStorage.setItem('checkout_intent', JSON.stringify({ tier: 'premium', billingInterval: 'month' }))
    renderHook(() => usePostSignInCheckoutIntent(), { wrapper: createWrapper() })
    expect(mockStartCheckout).not.toHaveBeenCalled()
  })

  it('does nothing when no intent is stored', () => {
    mockUseUser.mockReturnValue(signedInUser)
    renderHook(() => usePostSignInCheckoutIntent(), { wrapper: createWrapper() })
    expect(mockStartCheckout).not.toHaveBeenCalled()
  })

  it('consumes sessionStorage intent and initiates checkout when signed in', async () => {
    mockUseUser.mockReturnValue(signedInUser)
    sessionStorage.setItem('checkout_intent', JSON.stringify({ tier: 'premium', billingInterval: 'month' }))

    renderHook(() => usePostSignInCheckoutIntent(), { wrapper: createWrapper() })

    await new Promise((r) => setTimeout(r, 50))
    expect(mockStartCheckout).toHaveBeenCalledTimes(1)
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

    expect(mockStartCheckout).not.toHaveBeenCalled()
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

    expect(mockStartCheckout).toHaveBeenCalledTimes(1)
  })

  it('does not fire twice on StrictMode double-invoke (single-fire guard)', async () => {
    mockUseUser.mockReturnValue(signedInUser)
    sessionStorage.setItem('checkout_intent', JSON.stringify({ tier: 'premium', billingInterval: 'month' }))

    const { rerender } = renderHook(() => usePostSignInCheckoutIntent(), { wrapper: createWrapper() })
    rerender()
    await new Promise((r) => setTimeout(r, 50))

    expect(mockStartCheckout).toHaveBeenCalledTimes(1)
  })
})
