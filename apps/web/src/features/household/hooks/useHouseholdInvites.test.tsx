import { describe, it, expect, beforeEach, spyOn } from 'bun:test'
import { renderHook, waitFor, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import type { HouseholdInvite } from '../types'
import {
  getListHouseholdInvitesQueryKey,
  getGetHouseholdEntitlementQueryKey,
} from '@boostedbudget/api-client/endpoints/household/household'
import {
  mockUseListHouseholdInvites,
  mockUseCreateHouseholdInvite,
  mockUseRevokeHouseholdInvite,
  mockRefetchHouseholdInvites,
  mockCreateHouseholdInviteMutateAsync,
  mockRevokeHouseholdInviteMutateAsync,
} from '@/test/mocks/household-api'
import { useHouseholdInvites } from './useHouseholdInvites'

function createWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
}

const mockInvites: HouseholdInvite[] = [
  {
    id: 'invite-1',
    household_id: 'household-1',
    invitee_email: 'a@example.com',
    inviter_user_id: 'user-1',
    status: 'pending',
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
    expires_at: '2025-01-08T00:00:00Z',
  },
]

describe('useHouseholdInvites', () => {
  beforeEach(() => {
    mockUseListHouseholdInvites.mockClear()
    mockUseCreateHouseholdInvite.mockClear()
    mockUseRevokeHouseholdInvite.mockClear()
    mockRefetchHouseholdInvites.mockClear()
    mockCreateHouseholdInviteMutateAsync.mockClear()
    mockRevokeHouseholdInviteMutateAsync.mockClear()

    mockUseListHouseholdInvites.mockReturnValue({
      data: mockInvites,
      isLoading: false,
      error: null,
      refetch: mockRefetchHouseholdInvites,
    })
    mockUseCreateHouseholdInvite.mockReturnValue({
      mutateAsync: mockCreateHouseholdInviteMutateAsync,
      isPending: false,
    })
    mockUseRevokeHouseholdInvite.mockReturnValue({
      mutateAsync: mockRevokeHouseholdInviteMutateAsync,
      isPending: false,
    })
  })

  it('returns the list of invites', () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const { result } = renderHook(() => useHouseholdInvites(), { wrapper: createWrapper(queryClient) })

    expect(result.current.invites).toEqual(mockInvites)
    expect(result.current.actionErrorMessage).toBeNull()
  })

  it('createInvite calls the mutation and invalidates listHouseholdInvites + getHouseholdEntitlement', async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const invalidateSpy = spyOn(queryClient, 'invalidateQueries')
    const newInvite = { invite: mockInvites[0], invite_link: 'https://app/invite/abc' }
    mockCreateHouseholdInviteMutateAsync.mockResolvedValue(newInvite)

    const { result } = renderHook(() => useHouseholdInvites(), { wrapper: createWrapper(queryClient) })

    let returned: unknown
    await act(async () => {
      returned = await result.current.createInvite('new@example.com')
    })

    expect(mockCreateHouseholdInviteMutateAsync).toHaveBeenCalledWith({ data: { email: 'new@example.com' } })
    expect(returned).toEqual(newInvite)
    await waitFor(() => {
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: getListHouseholdInvitesQueryKey() })
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: getGetHouseholdEntitlementQueryKey() })
    })
  })

  it('surfaces a friendly error for a duplicate pending invite (409)', async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockCreateHouseholdInviteMutateAsync.mockRejectedValue({
      response: {
        status: 409,
        data: { detail: 'A pending invite already exists for this email' },
      },
    })

    const { result } = renderHook(() => useHouseholdInvites(), { wrapper: createWrapper(queryClient) })

    await act(async () => {
      await expect(result.current.createInvite('dup@example.com')).rejects.toBeDefined()
    })

    await waitFor(() => {
      expect(result.current.actionErrorMessage).toBe('A pending invite already exists for this email')
    })
  })

  it('surfaces a friendly error for seats-full (409)', async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockCreateHouseholdInviteMutateAsync.mockRejectedValue({
      response: {
        status: 409,
        data: { detail: 'No seats available on this plan' },
      },
    })

    const { result } = renderHook(() => useHouseholdInvites(), { wrapper: createWrapper(queryClient) })

    await act(async () => {
      await expect(result.current.createInvite('someone@example.com')).rejects.toBeDefined()
    })

    await waitFor(() => {
      expect(result.current.actionErrorMessage).toBe('No seats available on this plan')
    })
  })

  it('falls back to a generic message for an unmapped error', async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockCreateHouseholdInviteMutateAsync.mockRejectedValue(new Error('network down'))

    const { result } = renderHook(() => useHouseholdInvites(), { wrapper: createWrapper(queryClient) })

    await act(async () => {
      await expect(result.current.createInvite('someone@example.com')).rejects.toBeDefined()
    })

    await waitFor(() => {
      expect(result.current.actionErrorMessage).toBe('Something went wrong. Please try again.')
    })
  })

  it('revokeInvite calls the mutation and invalidates the same keys', async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const invalidateSpy = spyOn(queryClient, 'invalidateQueries')
    mockRevokeHouseholdInviteMutateAsync.mockResolvedValue(undefined)

    const { result } = renderHook(() => useHouseholdInvites(), { wrapper: createWrapper(queryClient) })

    await act(async () => {
      await result.current.revokeInvite('invite-1')
    })

    expect(mockRevokeHouseholdInviteMutateAsync).toHaveBeenCalledWith({ id: 'invite-1' })
    await waitFor(() => {
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: getListHouseholdInvitesQueryKey() })
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: getGetHouseholdEntitlementQueryKey() })
    })
  })

  it('regenerateInvite revokes then creates sequentially (not in parallel)', async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const callOrder: string[] = []
    const newInvite = { invite: { ...mockInvites[0], id: 'invite-2' }, invite_link: 'https://app/invite/def' }

    mockRevokeHouseholdInviteMutateAsync.mockImplementation(async () => {
      callOrder.push('revoke-start')
      await new Promise((resolve) => setTimeout(resolve, 5))
      callOrder.push('revoke-end')
    })
    mockCreateHouseholdInviteMutateAsync.mockImplementation(async () => {
      callOrder.push('create-start')
      return newInvite
    })

    const { result } = renderHook(() => useHouseholdInvites(), { wrapper: createWrapper(queryClient) })

    let returned: unknown
    await act(async () => {
      returned = await result.current.regenerateInvite('invite-1', 'a@example.com')
    })

    // The create must not start until revoke has fully finished — proves
    // sequential (await-then-await), not Promise.all-style parallel execution.
    expect(callOrder).toEqual(['revoke-start', 'revoke-end', 'create-start'])
    expect(mockRevokeHouseholdInviteMutateAsync).toHaveBeenCalledWith({ id: 'invite-1' })
    expect(mockCreateHouseholdInviteMutateAsync).toHaveBeenCalledWith({ data: { email: 'a@example.com' } })
    expect(returned).toEqual(newInvite)
  })

  it('regenerateInvite propagates a revoke failure without calling create', async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockRevokeHouseholdInviteMutateAsync.mockRejectedValue(new Error('revoke failed'))

    const { result } = renderHook(() => useHouseholdInvites(), { wrapper: createWrapper(queryClient) })

    await act(async () => {
      await expect(result.current.regenerateInvite('invite-1', 'a@example.com')).rejects.toThrow('revoke failed')
    })
    expect(mockCreateHouseholdInviteMutateAsync).not.toHaveBeenCalled()
  })
})
