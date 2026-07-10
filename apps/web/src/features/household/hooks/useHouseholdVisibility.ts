/**
 * useHouseholdVisibility Hook
 *
 * Issue #1370: Household feature — hooks layer
 *
 * Wraps `useListMyAccountShares` (mapped into a `Record<user_account_id,
 * visibility>` for O(1) lookups) and `useSetHouseholdShareVisibility`.
 *
 * Query-key invalidation investigation (see also useAcceptInvite.ts, which
 * duplicates this same helper for its own accept-success invalidation):
 * `packages/api-client/src/endpoints/dashboard/dashboard.ts` and
 * `.../analytics/analytics.ts` define each scoped query's key as
 *   `[url, ...(params ? [params] : [])]`
 * i.e. the params element is conditionally spread in, not always present.
 * Calling e.g. `getGetBalanceSummaryQueryKey()` with NO arguments therefore
 * returns the 1-element array `[url]`, not `[url, undefined]`. TanStack
 * Query's `invalidateQueries` does prefix matching by default (`exact:
 * false`): a query cached as `[url, { scope: 'personal' }]` or
 * `[url, { scope: 'household' }]` both start with `[url]`, so invoking the
 * key helper with no params and invalidating on that key invalidates BOTH
 * scope variants in one call, regardless of which scopes are actually
 * cached. No manual key-slicing is needed — the generated helper's own
 * "no-params" behavior already produces the right prefix.
 */

import { useCallback, useMemo } from 'react'
import { useQueryClient, type QueryClient } from '@tanstack/react-query'
import {
  useListMyAccountShares,
  useSetHouseholdShareVisibility,
  getListMyAccountSharesQueryKey,
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
import type { UserAccountShareView } from '@boostedbudget/api-client/models'

export type ShareVisibility = 'shared' | 'hidden' | null

export interface UseHouseholdVisibilityResult {
  shares: UserAccountShareView[]
  visibilityByAccountId: Record<string, ShareVisibility>
  /** True only when the account's visibility is explicitly 'shared'; null and 'hidden' are both not-shared. */
  isShared: (userAccountId: string) => boolean
  isLoading: boolean
  error: Error | null
  refetch: () => Promise<void>
  setVisibility: (userAccountId: string, visibility: 'shared' | 'hidden') => Promise<void>
}

/**
 * Invalidates the 7 scoped dashboard/analytics queries so both the
 * `personal` and `household` scope variants refresh. See file-header comment
 * for why calling each key helper with no arguments is the correct approach.
 */
async function invalidateScopedDashboardAnalyticsQueries(queryClient: QueryClient): Promise<void> {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: getGetBalanceSummaryQueryKey() }),
    queryClient.invalidateQueries({ queryKey: getGetNetWorthQueryKey() }),
    queryClient.invalidateQueries({ queryKey: getGetCashFlowSummaryQueryKey() }),
    queryClient.invalidateQueries({ queryKey: getGetBalanceHistoryQueryKey() }),
    queryClient.invalidateQueries({ queryKey: getGetNetWorthHistoryQueryKey() }),
    queryClient.invalidateQueries({ queryKey: getGetCashFlowHistoryQueryKey() }),
    queryClient.invalidateQueries({ queryKey: getGetNetWorthTrendQueryKey() }),
  ])
}

export function useHouseholdVisibility(): UseHouseholdVisibilityResult {
  const queryClient = useQueryClient()

  const {
    data,
    isLoading,
    error: queryError,
    refetch: queryRefetch,
  } = useListMyAccountShares()

  const shares = useMemo(
    () => (data ?? []) as unknown as UserAccountShareView[],
    [data]
  )

  const error = queryError
    ? queryError instanceof Error
      ? queryError
      : new Error('Failed to fetch account shares')
    : null

  const visibilityByAccountId = useMemo(() => {
    const map: Record<string, ShareVisibility> = {}
    for (const share of shares) {
      // Preserve the raw value (including null) — see `isShared` for the
      // "null/hidden both mean not-shared" semantics used by consumers.
      map[share.user_account_id] = (share.visibility as ShareVisibility) ?? null
    }
    return map
  }, [shares])

  const isShared = useCallback(
    (userAccountId: string): boolean => visibilityByAccountId[userAccountId] === 'shared',
    [visibilityByAccountId]
  )

  const setVisibilityMutation = useSetHouseholdShareVisibility()

  const refetch = useCallback(async () => {
    await queryClient.invalidateQueries({ queryKey: getListMyAccountSharesQueryKey() })
    await queryRefetch()
  }, [queryClient, queryRefetch])

  const setVisibility = useCallback(
    async (userAccountId: string, visibility: 'shared' | 'hidden'): Promise<void> => {
      await setVisibilityMutation.mutateAsync({ id: userAccountId, data: { visibility } })
      await queryClient.invalidateQueries({ queryKey: getListMyAccountSharesQueryKey() })
      await invalidateScopedDashboardAnalyticsQueries(queryClient)
      await queryRefetch()
    },
    [setVisibilityMutation, queryClient, queryRefetch]
  )

  return {
    shares,
    visibilityByAccountId,
    isShared,
    isLoading,
    error,
    refetch,
    setVisibility,
  }
}
