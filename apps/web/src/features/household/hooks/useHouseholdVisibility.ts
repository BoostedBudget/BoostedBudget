/**
 * useHouseholdVisibility Hook
 *
 * Issue #1370: Household feature — hooks layer
 *
 * Wraps `useListMyAccountShares` (mapped into a `Record<user_account_id,
 * visibility>` for O(1) lookups) and `useSetHouseholdShareVisibility`.
 *
 * Query-key invalidation investigation (see also useAcceptInvite.ts, which
 * shares the same `invalidateScopedDashboardAnalyticsQueries` helper for its
 * own accept-success invalidation):
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
 * "no-params" behavior already produces the right prefix. See
 * `invalidateScopedQueries.ts` for the shared helper implementing this.
 *
 * 404 retry suppression (final-review fix): the backend returns 404
 * (ErrHouseholdNotFound) for `ListMyAccountShares` when the caller isn't in a
 * household at all — an expected, common state (solo users), not a
 * transient failure. Without suppressing retries here, every solo-user
 * consumer of this hook (e.g. AccountCustomizationModal, which calls it
 * unconditionally whenever the modal opens) fires a request that TanStack
 * Query retries up to 3x by default on every mount. Mirrors the
 * `is404Error`-based retry-suppression pattern in useHousehold.ts.
 */

import { useCallback, useMemo } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import {
  useListMyAccountShares,
  useSetHouseholdShareVisibility,
  getListMyAccountSharesQueryKey,
} from '@boostedbudget/api-client/endpoints/household/household'
import type { UserAccountShareView } from '@boostedbudget/api-client/models'
import { invalidateScopedDashboardAnalyticsQueries } from './invalidateScopedQueries'
import { getErrorStatus } from './errorStatus'

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

export function useHouseholdVisibility(): UseHouseholdVisibilityResult {
  const queryClient = useQueryClient()

  const {
    data,
    isLoading,
    error: queryError,
    refetch: queryRefetch,
  } = useListMyAccountShares({
    query: {
      retry: (failureCount, error) => {
        if (getErrorStatus(error) === 404) return false
        return failureCount < 2
      },
    },
  })

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
