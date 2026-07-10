import { describe, it, expect, beforeEach, spyOn } from 'bun:test'
import { renderHook, waitFor, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import type { GetInvitePreviewResponseBody, AcceptInviteResponseBody } from '@boostedbudget/api-client/models'
import {
  getGetMyHouseholdQueryKey,
  getListHouseholdMembersQueryKey,
  getGetHouseholdEntitlementQueryKey,
  getListMyAccountSharesQueryKey,
} from '@boostedbudget/api-client/endpoints/household/household'
import { getGetBalanceSummaryQueryKey, getGetNetWorthQueryKey } from '@boostedbudget/api-client/endpoints/dashboard/dashboard'
import { getGetCashFlowSummaryQueryKey } from '@boostedbudget/api-client/endpoints/analytics/analytics'
import {
  mockUseGetHouseholdInvitePreview,
  mockUseAcceptHouseholdInvite,
  mockUseDeclineHouseholdInvite,
  mockAcceptHouseholdInviteMutateAsync,
  mockDeclineHouseholdInviteMutateAsync,
} from '@/test/mocks/household-api'
import { useAcceptInvite } from './useAcceptInvite'

function createWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
}

const mockPreview: GetInvitePreviewResponseBody = {
  household_name: 'The Smiths',
  invitee_email: 'invitee@example.com',
  expires_at: '2025-02-01T00:00:00Z',
}

const mockAcceptResponse: AcceptInviteResponseBody = {
  household_id: 'household-1',
  member: {
    id: 'member-1',
    household_id: 'household-1',
    user_id: 'user-2',
    role: 'member',
    status: 'active',
    joined_at: '2025-01-15T00:00:00Z',
    created_at: '2025-01-15T00:00:00Z',
    updated_at: '2025-01-15T00:00:00Z',
  },
}

describe('useAcceptInvite', () => {
  beforeEach(() => {
    mockUseGetHouseholdInvitePreview.mockClear()
    mockUseAcceptHouseholdInvite.mockClear()
    mockUseDeclineHouseholdInvite.mockClear()
    mockAcceptHouseholdInviteMutateAsync.mockClear()
    mockDeclineHouseholdInviteMutateAsync.mockClear()

    mockUseGetHouseholdInvitePreview.mockReturnValue({
      data: mockPreview,
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve({}),
    })
    mockUseAcceptHouseholdInvite.mockReturnValue({
      mutateAsync: mockAcceptHouseholdInviteMutateAsync,
      isPending: false,
    })
    mockUseDeclineHouseholdInvite.mockReturnValue({
      mutateAsync: mockDeclineHouseholdInviteMutateAsync,
      isPending: false,
    })
  })

  it('returns the preview data', () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const { result } = renderHook(() => useAcceptInvite('token-1'), { wrapper: createWrapper(queryClient) })

    expect(result.current.preview).toEqual(mockPreview)
    expect(result.current.errorKind).toBeNull()
  })

  it('classifies a 404 preview error as not_found', () => {
    mockUseGetHouseholdInvitePreview.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: { response: { status: 404 } },
      refetch: () => Promise.resolve({}),
    })
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const { result } = renderHook(() => useAcceptInvite('bad-token'), { wrapper: createWrapper(queryClient) })

    expect(result.current.errorKind).toBe('not_found')
    expect(result.current.preview).toBeNull()
  })

  it('accept() calls the mutation with consent + hidden_account_ids and returns the result', async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockAcceptHouseholdInviteMutateAsync.mockResolvedValue(mockAcceptResponse)

    const { result } = renderHook(() => useAcceptInvite('token-1'), { wrapper: createWrapper(queryClient) })

    let returned: unknown
    await act(async () => {
      returned = await result.current.accept(['ua-hidden-1'])
    })

    expect(mockAcceptHouseholdInviteMutateAsync).toHaveBeenCalledWith({
      token: 'token-1',
      data: { consent: true, hidden_account_ids: ['ua-hidden-1'] },
    })
    expect(returned).toEqual(mockAcceptResponse)
  })

  it('accept() defaults hidden_account_ids to null when omitted', async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockAcceptHouseholdInviteMutateAsync.mockResolvedValue(mockAcceptResponse)

    const { result } = renderHook(() => useAcceptInvite('token-1'), { wrapper: createWrapper(queryClient) })

    await act(async () => {
      await result.current.accept()
    })

    expect(mockAcceptHouseholdInviteMutateAsync).toHaveBeenCalledWith({
      token: 'token-1',
      data: { consent: true, hidden_account_ids: null },
    })
  })

  it('accept() success invalidates getMyHousehold, listHouseholdMembers, getHouseholdEntitlement, listMyAccountShares, and the scoped dashboard/analytics queries', async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const invalidateSpy = spyOn(queryClient, 'invalidateQueries')
    mockAcceptHouseholdInviteMutateAsync.mockResolvedValue(mockAcceptResponse)

    const { result } = renderHook(() => useAcceptInvite('token-1'), { wrapper: createWrapper(queryClient) })

    await act(async () => {
      await result.current.accept()
    })

    await waitFor(() => {
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: getGetMyHouseholdQueryKey() })
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: getListHouseholdMembersQueryKey() })
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: getGetHouseholdEntitlementQueryKey() })
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: getListMyAccountSharesQueryKey() })
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: getGetBalanceSummaryQueryKey() })
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: getGetNetWorthQueryKey() })
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: getGetCashFlowSummaryQueryKey() })
    })
  })

  it('accept() failure with 403 sets errorKind to email_mismatch and rejects', async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockAcceptHouseholdInviteMutateAsync.mockRejectedValue({ response: { status: 403 } })

    const { result } = renderHook(() => useAcceptInvite('token-1'), { wrapper: createWrapper(queryClient) })

    await act(async () => {
      await expect(result.current.accept()).rejects.toBeDefined()
    })

    await waitFor(() => {
      expect(result.current.errorKind).toBe('email_mismatch')
    })
  })

  it('accept() failure with 409 sets errorKind to seats_full', async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockAcceptHouseholdInviteMutateAsync.mockRejectedValue({ response: { status: 409 } })

    const { result } = renderHook(() => useAcceptInvite('token-1'), { wrapper: createWrapper(queryClient) })

    await act(async () => {
      await expect(result.current.accept()).rejects.toBeDefined()
    })

    await waitFor(() => {
      expect(result.current.errorKind).toBe('seats_full')
    })
  })

  it('accept() failure with 404 sets errorKind to not_found', async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockAcceptHouseholdInviteMutateAsync.mockRejectedValue({ response: { status: 404 } })

    const { result } = renderHook(() => useAcceptInvite('token-1'), { wrapper: createWrapper(queryClient) })

    await act(async () => {
      await expect(result.current.accept()).rejects.toBeDefined()
    })

    await waitFor(() => {
      expect(result.current.errorKind).toBe('not_found')
    })
  })

  it('accept() failure with an unmapped status sets errorKind to unknown', async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockAcceptHouseholdInviteMutateAsync.mockRejectedValue(new Error('network down'))

    const { result } = renderHook(() => useAcceptInvite('token-1'), { wrapper: createWrapper(queryClient) })

    await act(async () => {
      await expect(result.current.accept()).rejects.toBeDefined()
    })

    await waitFor(() => {
      expect(result.current.errorKind).toBe('unknown')
    })
  })

  it('decline() calls the mutation with the token', async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockDeclineHouseholdInviteMutateAsync.mockResolvedValue(undefined)

    const { result } = renderHook(() => useAcceptInvite('token-1'), { wrapper: createWrapper(queryClient) })

    await act(async () => {
      await result.current.decline()
    })

    expect(mockDeclineHouseholdInviteMutateAsync).toHaveBeenCalledWith({ token: 'token-1' })
  })

  it('decline() failure with 404 sets errorKind to not_found', async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockDeclineHouseholdInviteMutateAsync.mockRejectedValue({ response: { status: 404 } })

    const { result } = renderHook(() => useAcceptInvite('token-1'), { wrapper: createWrapper(queryClient) })

    await act(async () => {
      await expect(result.current.decline()).rejects.toBeDefined()
    })

    await waitFor(() => {
      expect(result.current.errorKind).toBe('not_found')
    })
  })
})
