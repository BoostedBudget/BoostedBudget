import { describe, it, expect, beforeEach, spyOn } from 'bun:test'
import { renderHook, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import type { HouseholdMemberView } from '../types'
import {
  getListHouseholdMembersQueryKey,
  getGetHouseholdEntitlementQueryKey,
  getGetMyHouseholdQueryKey,
} from '@boostedbudget/api-client/endpoints/household/household'
import {
  getGetBalanceSummaryQueryKey,
  getGetBalanceHistoryQueryKey,
  getGetNetWorthQueryKey,
  getGetNetWorthHistoryQueryKey,
} from '@boostedbudget/api-client/endpoints/dashboard/dashboard'
import {
  getGetCashFlowSummaryQueryKey,
  getGetCashFlowHistoryQueryKey,
  getGetNetWorthTrendQueryKey,
} from '@boostedbudget/api-client/endpoints/analytics/analytics'
import {
  mockUseListHouseholdMembers,
  mockUseRemoveHouseholdMember,
  mockRefetchHouseholdMembers,
  mockRemoveHouseholdMemberMutateAsync,
} from '@/test/mocks/household-api'
import { useHouseholdMembers } from './useHouseholdMembers'

function createWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
}

const mockMembers: HouseholdMemberView[] = [
  { user_id: 'user-1', role: 'owner', status: 'active', joined_at: '2025-01-01T00:00:00Z' },
  { user_id: 'user-2', role: 'member', status: 'active', joined_at: '2025-02-01T00:00:00Z' },
]

describe('useHouseholdMembers', () => {
  beforeEach(() => {
    mockUseListHouseholdMembers.mockClear()
    mockUseRemoveHouseholdMember.mockClear()
    mockRefetchHouseholdMembers.mockClear()
    mockRemoveHouseholdMemberMutateAsync.mockClear()

    mockUseListHouseholdMembers.mockReturnValue({
      data: mockMembers,
      isLoading: false,
      error: null,
      refetch: mockRefetchHouseholdMembers,
    })
    mockUseRemoveHouseholdMember.mockReturnValue({
      mutateAsync: mockRemoveHouseholdMemberMutateAsync,
      isPending: false,
    })
  })

  it('returns the list of members', () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const { result } = renderHook(() => useHouseholdMembers(), { wrapper: createWrapper(queryClient) })

    expect(result.current.members).toEqual(mockMembers)
    expect(result.current.isLoading).toBe(false)
    expect(result.current.error).toBeNull()
  })

  it('defaults members to an empty array when data is undefined', () => {
    mockUseListHouseholdMembers.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: null,
      refetch: mockRefetchHouseholdMembers,
    })
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const { result } = renderHook(() => useHouseholdMembers(), { wrapper: createWrapper(queryClient) })

    expect(result.current.members).toEqual([])
  })

  it('removeMember calls the mutation with the userId and invalidates listHouseholdMembers + getHouseholdEntitlement + getMyHousehold + the 9 scoped dashboard/analytics/investments queries', async () => {
    // Finding #1 (final review): removeMember is used for BOTH an owner
    // removing another member AND self-leave (LeaveHouseholdDialog calling
    // removeMember(ownUserId)). getMyHousehold must be invalidated so
    // useHousehold()'s cached isInHousehold flips to false on self-leave —
    // otherwise stale in-household UI (Household tab, HouseholdCard,
    // ScopeSegmentedControl's reset-on-leave effect) never updates. The
    // scoped dashboard/analytics queries must also be invalidated so an
    // owner's household-scope view stops over-counting the removed member's
    // shared balances.
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const invalidateSpy = spyOn(queryClient, 'invalidateQueries')
    mockRemoveHouseholdMemberMutateAsync.mockResolvedValue(undefined)

    const { result } = renderHook(() => useHouseholdMembers(), { wrapper: createWrapper(queryClient) })

    await result.current.removeMember('user-2')

    expect(mockRemoveHouseholdMemberMutateAsync).toHaveBeenCalledWith({ userId: 'user-2' })

    await waitFor(() => {
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: getListHouseholdMembersQueryKey() })
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: getGetHouseholdEntitlementQueryKey() })
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: getGetMyHouseholdQueryKey() })
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: getGetBalanceSummaryQueryKey() })
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: getGetNetWorthQueryKey() })
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: getGetCashFlowSummaryQueryKey() })
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: getGetBalanceHistoryQueryKey() })
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: getGetNetWorthHistoryQueryKey() })
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: getGetCashFlowHistoryQueryKey() })
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: getGetNetWorthTrendQueryKey() })
    })
  })

  it('removeMember invalidation actually marks the cached getMyHousehold entry stale in a real QueryClient (self-leave regression)', async () => {
    // Finding #1 (final review): proves the fix at the cache level, not just
    // via a call-arg spy — seeds a real QueryClient with a cached
    // getMyHousehold entry (the shape useHousehold() would have after a
    // member loads their household), triggers the real removeMember
    // mutation path (mirroring LeaveHouseholdDialog calling
    // removeMember(ownUserId)), and asserts the cache entry is now stale so
    // useHousehold()'s next read/refetch will see isInHousehold flip false.
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockRemoveHouseholdMemberMutateAsync.mockResolvedValue(undefined)

    const myHouseholdKey = getGetMyHouseholdQueryKey()
    queryClient.setQueryData(myHouseholdKey, {
      household: { id: 'household-1', name: 'The Smiths' },
      summary: { is_owner: false, seats_active: 2, seats_pending: 0, source: 'household', tier: 'household_3', users_included: 3 },
    })
    expect(queryClient.getQueryState(myHouseholdKey)?.isInvalidated).toBe(false)

    const { result } = renderHook(() => useHouseholdMembers(), { wrapper: createWrapper(queryClient) })

    await result.current.removeMember('user-1')

    await waitFor(() => {
      expect(queryClient.getQueryState(myHouseholdKey)?.isInvalidated).toBe(true)
    })
  })

  it('propagates removeMember failures without invalidating', async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const invalidateSpy = spyOn(queryClient, 'invalidateQueries')
    const error = new Error('Cannot remove household owner')
    mockRemoveHouseholdMemberMutateAsync.mockRejectedValue(error)

    const { result } = renderHook(() => useHouseholdMembers(), { wrapper: createWrapper(queryClient) })

    await expect(result.current.removeMember('user-1')).rejects.toThrow('Cannot remove household owner')
    expect(invalidateSpy).not.toHaveBeenCalled()
  })
})
