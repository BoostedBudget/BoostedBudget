import { describe, it, expect, beforeEach, spyOn } from 'bun:test'
import { renderHook, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import type { HouseholdMemberView } from '../types'
import {
  getListHouseholdMembersQueryKey,
  getGetHouseholdEntitlementQueryKey,
} from '@boostedbudget/api-client/endpoints/household/household'
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

  it('removeMember calls the mutation with the userId and invalidates listHouseholdMembers + getHouseholdEntitlement', async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const invalidateSpy = spyOn(queryClient, 'invalidateQueries')
    mockRemoveHouseholdMemberMutateAsync.mockResolvedValue(undefined)

    const { result } = renderHook(() => useHouseholdMembers(), { wrapper: createWrapper(queryClient) })

    await result.current.removeMember('user-2')

    expect(mockRemoveHouseholdMemberMutateAsync).toHaveBeenCalledWith({ userId: 'user-2' })

    await waitFor(() => {
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: getListHouseholdMembersQueryKey() })
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: getGetHouseholdEntitlementQueryKey() })
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
