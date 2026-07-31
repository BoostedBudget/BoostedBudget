import { describe, it, expect, beforeEach } from 'bun:test'
import { renderHook } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import React from 'react'
import type { User } from '@boostedbudget/api-client/models'
import {
  mockUseGetUserProfile,
  mockRefetchUserProfile,
} from '@/test/mocks/profile-api'
import { useSubscriptionPresentation } from './useSubscriptionPresentation'
import { presentationFromSubscription } from '../lib/subscriptionPresentation'

const baseUser: User = {
  id: 'user-123',
  clerk_user_id: 'user_123',
  email: 'test@example.com',
  email_verified: true,
  primary_region: 'iad',
  subscription_tier: 'premium',
  subscription_status: 'inactive',
  had_trial: true,
  is_active: true,
  is_deleted: false,
  created_at: '2025-01-01T00:00:00Z',
  updated_at: '2025-01-15T10:00:00Z',
  timezone: 'America/New_York',
  timezone_source: 'default',
}

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
    },
  })
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    )
  }
}

describe('useSubscriptionPresentation', () => {
  beforeEach(() => {
    mockUseGetUserProfile.mockClear()
    mockRefetchUserProfile.mockClear()
  })

  it('derives a comped presentation when the profile reports is_comped', () => {
    mockUseGetUserProfile.mockReturnValue({
      data: { ...baseUser, is_comped: true } as User,
      isLoading: false,
      error: null,
      refetch: mockRefetchUserProfile,
    })

    const { result } = renderHook(() => useSubscriptionPresentation(), {
      wrapper: createWrapper(),
    })

    expect(result.current.kind).toBe('comped')
    expect(result.current.hasWriteAccess).toBe(true)
  })

  it('derives an inactive presentation when the profile reports inactive and not comped', () => {
    mockUseGetUserProfile.mockReturnValue({
      data: { ...baseUser, subscription_status: 'inactive', is_comped: false } as User,
      isLoading: false,
      error: null,
      refetch: mockRefetchUserProfile,
    })

    const { result } = renderHook(() => useSubscriptionPresentation(), {
      wrapper: createWrapper(),
    })

    expect(result.current.kind).toBe('inactive')
    expect(result.current.hasWriteAccess).toBe(false)
  })

  it('fails closed to a safe default while the profile is still loading', () => {
    mockUseGetUserProfile.mockReturnValue({
      data: undefined,
      isLoading: true,
      error: null,
      refetch: mockRefetchUserProfile,
    })

    const { result } = renderHook(() => useSubscriptionPresentation(), {
      wrapper: createWrapper(),
    })

    expect(result.current.kind).toBe('unknown')
    expect(result.current.hasWriteAccess).toBe(false)
  })
})

describe('presentationFromSubscription', () => {
  it('maps status: active + is_comped to comped', () => {
    const result = presentationFromSubscription({
      amount: '8.99',
      billing_interval: 'month',
      cancel_at_period_end: false,
      currency: 'usd',
      status: 'active',
      subscription_id: 'sub_123',
      tier: 'premium',
      is_comped: true,
    })

    expect(result.kind).toBe('comped')
    expect(result.hasWriteAccess).toBe(true)
  })

  it('treats a 404 payload as inactive', () => {
    const result = presentationFromSubscription(undefined, { notFound: true })

    expect(result.kind).toBe('inactive')
    expect(result.hasWriteAccess).toBe(false)
  })

  it('treats an undefined payload (no error signaled) as inactive', () => {
    const result = presentationFromSubscription(undefined)

    expect(result.kind).toBe('inactive')
  })
})
