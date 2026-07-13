import { describe, it, expect, beforeEach } from 'bun:test'
import { renderHook, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import type { Household, HouseholdEntitlementSummary } from '../types'
import type { GetMyHouseholdResponseBody } from '@boostedbudget/api-client/models'
import {
  mockUseGetMyHousehold,
  mockUseGetHouseholdEntitlement,
  mockRefetchMyHousehold,
  mockRefetchHouseholdEntitlement,
} from '@/test/mocks/household-api'
import { useHousehold } from './useHousehold'

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
}

const mockHousehold: Household = {
  id: 'household-1',
  name: 'The Smiths',
  owner_user_id: 'user-1',
  created_at: '2025-01-01T00:00:00Z',
  updated_at: '2025-01-01T00:00:00Z',
}

const mockEntitlement: HouseholdEntitlementSummary = {
  is_owner: true,
  seats_active: 2,
  seats_pending: 1,
  source: 'household',
  tier: 'household_3',
  users_included: 3,
}

const mockResponseBody: GetMyHouseholdResponseBody = {
  household: mockHousehold,
  summary: mockEntitlement,
}

describe('useHousehold', () => {
  beforeEach(() => {
    mockUseGetMyHousehold.mockClear()
    mockUseGetHouseholdEntitlement.mockClear()
    mockRefetchMyHousehold.mockClear()
    mockRefetchHouseholdEntitlement.mockClear()

    // Sensible defaults; individual tests override via mockReturnValue.
    mockUseGetMyHousehold.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: null,
      refetch: mockRefetchMyHousehold,
    })
    mockUseGetHouseholdEntitlement.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: null,
      refetch: mockRefetchHouseholdEntitlement,
    })
  })

  it('returns isInHousehold: false and no error when useGetMyHousehold 404s (no household yet)', () => {
    mockUseGetMyHousehold.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: { response: { status: 404 } },
      refetch: mockRefetchMyHousehold,
    })
    mockUseGetHouseholdEntitlement.mockReturnValue({
      data: mockEntitlement,
      isLoading: false,
      error: null,
      refetch: mockRefetchHouseholdEntitlement,
    })

    const { result } = renderHook(() => useHousehold(), { wrapper: createWrapper() })

    expect(result.current.isInHousehold).toBe(false)
    expect(result.current.household).toBeNull()
    expect(result.current.error).toBeNull()
    // Entitlement is independent of membership and should still be surfaced.
    expect(result.current.seatsUsed).toBe(2)
    expect(result.current.seatsTotal).toBe(3)
    expect(result.current.isOwner).toBe(true)
  })

  it('returns the household and derived seat data when a household exists', () => {
    mockUseGetMyHousehold.mockReturnValue({
      data: mockResponseBody,
      isLoading: false,
      error: null,
      refetch: mockRefetchMyHousehold,
    })
    mockUseGetHouseholdEntitlement.mockReturnValue({
      data: mockEntitlement,
      isLoading: false,
      error: null,
      refetch: mockRefetchHouseholdEntitlement,
    })

    const { result } = renderHook(() => useHousehold(), { wrapper: createWrapper() })

    expect(result.current.isInHousehold).toBe(true)
    expect(result.current.household).toEqual(mockHousehold)
    expect(result.current.summary).toEqual(mockEntitlement)
    expect(result.current.seatsUsed).toBe(2)
    expect(result.current.seatsTotal).toBe(3)
    expect(result.current.isOwner).toBe(true)
    expect(result.current.error).toBeNull()
  })

  it('surfaces a real (non-404) household query error', () => {
    const error = { response: { status: 500 } }
    mockUseGetMyHousehold.mockReturnValue({
      data: undefined,
      isLoading: false,
      error,
      refetch: mockRefetchMyHousehold,
    })

    const { result } = renderHook(() => useHousehold(), { wrapper: createWrapper() })

    expect(result.current.error).not.toBeNull()
    expect(result.current.isInHousehold).toBe(false)
  })

  it('reports isLoading true while either query is loading', () => {
    mockUseGetMyHousehold.mockReturnValue({
      data: undefined,
      isLoading: true,
      error: null,
      refetch: mockRefetchMyHousehold,
    })

    const { result } = renderHook(() => useHousehold(), { wrapper: createWrapper() })

    expect(result.current.isLoading).toBe(true)
  })

  it('defaults seatsUsed/seatsTotal/isOwner to falsy values when entitlement has not loaded yet', () => {
    const { result } = renderHook(() => useHousehold(), { wrapper: createWrapper() })

    expect(result.current.seatsUsed).toBe(0)
    expect(result.current.seatsTotal).toBe(0)
    expect(result.current.isOwner).toBe(false)
  })

  it('refetch invalidates both query keys and refetches both queries', async () => {
    mockUseGetMyHousehold.mockReturnValue({
      data: mockResponseBody,
      isLoading: false,
      error: null,
      refetch: mockRefetchMyHousehold,
    })
    mockUseGetHouseholdEntitlement.mockReturnValue({
      data: mockEntitlement,
      isLoading: false,
      error: null,
      refetch: mockRefetchHouseholdEntitlement,
    })

    const { result } = renderHook(() => useHousehold(), { wrapper: createWrapper() })

    await result.current.refetch()

    await waitFor(() => {
      expect(mockRefetchMyHousehold).toHaveBeenCalled()
      expect(mockRefetchHouseholdEntitlement).toHaveBeenCalled()
    })
  })
})
