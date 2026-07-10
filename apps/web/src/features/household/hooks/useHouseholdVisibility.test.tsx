import { describe, it, expect, beforeEach, spyOn } from 'bun:test'
import { renderHook, waitFor, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import type { UserAccountShareView } from '@boostedbudget/api-client/models'
import { getListMyAccountSharesQueryKey } from '@boostedbudget/api-client/endpoints/household/household'
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
  mockUseListMyAccountShares,
  mockUseSetHouseholdShareVisibility,
  mockRefetchMyAccountShares,
  mockSetHouseholdShareVisibilityMutateAsync,
} from '@/test/mocks/household-api'
import { useHouseholdVisibility } from './useHouseholdVisibility'

function createWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
}

const mockShares: UserAccountShareView[] = [
  { user_account_id: 'ua-1', visibility: 'shared' },
  { user_account_id: 'ua-2', visibility: 'hidden' },
  { user_account_id: 'ua-3', visibility: null },
]

describe('useHouseholdVisibility', () => {
  beforeEach(() => {
    mockUseListMyAccountShares.mockClear()
    mockUseSetHouseholdShareVisibility.mockClear()
    mockRefetchMyAccountShares.mockClear()
    mockSetHouseholdShareVisibilityMutateAsync.mockClear()

    mockUseListMyAccountShares.mockReturnValue({
      data: mockShares,
      isLoading: false,
      error: null,
      refetch: mockRefetchMyAccountShares,
    })
    mockUseSetHouseholdShareVisibility.mockReturnValue({
      mutateAsync: mockSetHouseholdShareVisibilityMutateAsync,
      isPending: false,
    })
  })

  it('maps shares to a Record keyed by user_account_id', () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const { result } = renderHook(() => useHouseholdVisibility(), { wrapper: createWrapper(queryClient) })

    expect(result.current.visibilityByAccountId['ua-1']).toBe('shared')
    expect(result.current.visibilityByAccountId['ua-2']).toBe('hidden')
    expect(result.current.visibilityByAccountId['ua-3']).toBeNull()
  })

  it('treats a null visibility as not-shared (excluded from the household aggregate)', () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const { result } = renderHook(() => useHouseholdVisibility(), { wrapper: createWrapper(queryClient) })

    expect(result.current.isShared('ua-3')).toBe(false)
    expect(result.current.isShared('ua-2')).toBe(false)
    expect(result.current.isShared('ua-1')).toBe(true)
    // An account with no share record at all is also not-shared.
    expect(result.current.isShared('unknown-account')).toBe(false)
  })

  it('setVisibility calls the mutation with the correct id/data shape', async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockSetHouseholdShareVisibilityMutateAsync.mockResolvedValue(undefined)

    const { result } = renderHook(() => useHouseholdVisibility(), { wrapper: createWrapper(queryClient) })

    await act(async () => {
      await result.current.setVisibility('ua-3', 'shared')
    })

    expect(mockSetHouseholdShareVisibilityMutateAsync).toHaveBeenCalledWith({
      id: 'ua-3',
      data: { visibility: 'shared' },
    })
  })

  it('setVisibility invalidates listMyAccountShares and all 7 scoped dashboard/analytics query keys', async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const invalidateSpy = spyOn(queryClient, 'invalidateQueries')
    mockSetHouseholdShareVisibilityMutateAsync.mockResolvedValue(undefined)

    const { result } = renderHook(() => useHouseholdVisibility(), { wrapper: createWrapper(queryClient) })

    await act(async () => {
      await result.current.setVisibility('ua-1', 'hidden')
    })

    await waitFor(() => {
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: getListMyAccountSharesQueryKey() })
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: getGetBalanceSummaryQueryKey() })
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: getGetNetWorthQueryKey() })
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: getGetCashFlowSummaryQueryKey() })
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: getGetBalanceHistoryQueryKey() })
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: getGetNetWorthHistoryQueryKey() })
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: getGetCashFlowHistoryQueryKey() })
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: getGetNetWorthTrendQueryKey() })
    })
  })

  it('setVisibility invalidation actually marks BOTH personal- and household-scope cache entries stale in a real QueryClient', async () => {
    // Finding #2 (review): the other invalidation tests in this file spy on
    // `invalidateQueries` and assert it was called with the key-getter's own
    // return value — that proves the call site uses the right key literal,
    // but not that TanStack Query's prefix-matching actually invalidates
    // BOTH `scope=personal` and `scope=household` cached variants. This test
    // seeds a real (unmocked) QueryClient cache with both scope variants for
    // one of the 7 scoped queries, triggers the real setVisibility mutation
    // path, and asserts both cache entries are now marked stale — proving
    // the real prefix-match semantics, not just a call-arg assertion.
    //
    // NOTE: uses getGetNetWorthQueryKey rather than
    // getGetBalanceSummaryQueryKey — this test file's suite runs under the
    // project's global bunfig.toml preloads, which include
    // `src/test/mocks/dashboard-api.ts`. That file calls `mock.module()` on
    // the dashboard endpoints module for ALL tests process-wide (bun:test
    // module mocks are global, not scoped to a file), and its
    // `getGetBalanceSummaryQueryKey`/`getGetBalanceHistoryQueryKey` stubs
    // always append a `params` element (even `undefined`) instead of
    // conditionally spreading it — breaking the real prefix-match shape this
    // test needs to exercise. `getGetNetWorthQueryKey` is untouched by that
    // mock (and by analytics-api.ts's equivalent), so it resolves to the
    // real generated implementation here.
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockSetHouseholdShareVisibilityMutateAsync.mockResolvedValue(undefined)

    const personalKey = getGetNetWorthQueryKey({ scope: 'personal' })
    const householdKey = getGetNetWorthQueryKey({ scope: 'household' })
    queryClient.setQueryData(personalKey, { total_balance: 100 })
    queryClient.setQueryData(householdKey, { total_balance: 250 })

    // Sanity check: neither entry starts out invalidated.
    expect(queryClient.getQueryState(personalKey)?.isInvalidated).toBe(false)
    expect(queryClient.getQueryState(householdKey)?.isInvalidated).toBe(false)

    const { result } = renderHook(() => useHouseholdVisibility(), { wrapper: createWrapper(queryClient) })

    await act(async () => {
      await result.current.setVisibility('ua-1', 'hidden')
    })

    await waitFor(() => {
      expect(queryClient.getQueryState(personalKey)?.isInvalidated).toBe(true)
      expect(queryClient.getQueryState(householdKey)?.isInvalidated).toBe(true)
    })
  })

  it('propagates setVisibility failures without invalidating', async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const invalidateSpy = spyOn(queryClient, 'invalidateQueries')
    mockSetHouseholdShareVisibilityMutateAsync.mockRejectedValue(new Error('forbidden'))

    const { result } = renderHook(() => useHouseholdVisibility(), { wrapper: createWrapper(queryClient) })

    await act(async () => {
      await expect(result.current.setVisibility('ua-1', 'hidden')).rejects.toThrow('forbidden')
    })
    expect(invalidateSpy).not.toHaveBeenCalled()
  })
})
