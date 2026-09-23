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
  getGetHoldingsQueryKey,
  getGetPortfolioSummaryQueryKey,
} from '@boostedbudget/api-client/endpoints/investments/investments'
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

  it('setVisibility invalidates listMyAccountShares and all 9 scoped dashboard/analytics/investments query keys', async () => {
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
      // Issue #1706: holdings + portfolio summary are scope-dependent too — a
      // share-visibility change must refresh their household aggregates.
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: getGetHoldingsQueryKey() })
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: getGetPortfolioSummaryQueryKey() })
    })
  })

  it('setVisibility invalidation actually marks BOTH personal- and household-scope cache entries stale in a real QueryClient', async () => {
    // Finding #2 (review): the other invalidation tests in this file spy on
    // `invalidateQueries` and assert it was called with the key-getter's own
    // return value — that proves the call site uses the right key literal,
    // but not that TanStack Query's prefix-matching actually invalidates
    // BOTH `scope=personal` and `scope=household` cached variants. This test
    // seeds a real (unmocked) QueryClient cache with both scope variants for
    // one of the 9 scoped queries, triggers the real setVisibility mutation
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

  it('setVisibility invalidation marks BOTH personal- and household-scope investments cache entries stale in a real QueryClient', async () => {
    // Issue #1706 (independent review, MEDIUM): holdings and portfolio summary
    // became scope-dependent on this branch, so their cache entries are keyed
    // per scope. Without invalidating them here, toggling an account's share
    // visibility leaves the household Holdings and Portfolio Summary totals
    // stale for the full 5-minute staleTime (there is no refetch-on-focus).
    // This seeds a real QueryClient with both scope variants of each and
    // asserts the share change marks all four stale.
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockSetHouseholdShareVisibilityMutateAsync.mockResolvedValue(undefined)

    const personalHoldings = getGetHoldingsQueryKey({ scope: 'personal' })
    const householdHoldings = getGetHoldingsQueryKey({ scope: 'household' })
    const personalSummary = getGetPortfolioSummaryQueryKey({ scope: 'personal' })
    const householdSummary = getGetPortfolioSummaryQueryKey({ scope: 'household' })
    queryClient.setQueryData(personalHoldings, { holdings: [] })
    queryClient.setQueryData(householdHoldings, { holdings: [] })
    queryClient.setQueryData(personalSummary, { total_value: 100 })
    queryClient.setQueryData(householdSummary, { total_value: 250 })

    for (const key of [personalHoldings, householdHoldings, personalSummary, householdSummary]) {
      expect(queryClient.getQueryState(key)?.isInvalidated).toBe(false)
    }

    const { result } = renderHook(() => useHouseholdVisibility(), { wrapper: createWrapper(queryClient) })

    await act(async () => {
      await result.current.setVisibility('ua-1', 'hidden')
    })

    await waitFor(() => {
      for (const key of [personalHoldings, householdHoldings, personalSummary, householdSummary]) {
        expect(queryClient.getQueryState(key)?.isInvalidated).toBe(true)
      }
    })
  })

  it('suppresses retries on a 404 (solo user, no household) but retries other errors up to failureCount < 2', () => {
    // Finding #2 (final review): the backend returns 404 (ErrHouseholdNotFound)
    // for ListMyAccountShares when the caller isn't in a household — an
    // expected, common state for solo users, not a transient failure.
    // Without this, TanStack Query's default retry (3x) turns every
    // solo-user mount of a useHouseholdVisibility consumer (e.g.
    // AccountCustomizationModal, which calls the hook unconditionally) into
    // a retry storm. `useListMyAccountShares` is mocked at the module level
    // in this test file, so we assert the retry-suppression logic by
    // invoking the `query.retry` function this hook actually passes in,
    // rather than exercising real network retry behavior.
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    renderHook(() => useHouseholdVisibility(), { wrapper: createWrapper(queryClient) })

    const lastCallOptions = mockUseListMyAccountShares.mock.calls.at(-1)?.[0] as
      | { query?: { retry?: (failureCount: number, error: unknown) => boolean } }
      | undefined
    const retry = lastCallOptions?.query?.retry
    expect(retry).toBeInstanceOf(Function)

    expect(retry?.(0, { response: { status: 404 } })).toBe(false)
    expect(retry?.(1, { response: { status: 404 } })).toBe(false)
    expect(retry?.(0, { response: { status: 500 } })).toBe(true)
    expect(retry?.(2, { response: { status: 500 } })).toBe(false)
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
