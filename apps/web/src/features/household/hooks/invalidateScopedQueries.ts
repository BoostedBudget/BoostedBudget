/**
 * invalidateScopedDashboardAnalyticsQueries
 *
 * Issue #1370: Household feature — hooks layer (shared helper)
 *
 * Query-key invalidation investigation:
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
 *
 * Shared by useHouseholdVisibility.ts (visibility toggle changes what's
 * shared into the household aggregate) and useAcceptInvite.ts (joining a
 * household changes almost everything the user sees).
 */

import type { QueryClient } from '@tanstack/react-query'
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

/**
 * Invalidates the 9 scoped dashboard/analytics/investments queries so both the
 * `personal` and `household` scope variants refresh. See file-header comment
 * for why calling each key helper with no arguments is the correct approach.
 *
 * The two investments keys (#1706) are scope-dependent for the same reason the
 * dashboard/analytics keys are: holdings and portfolio summary accept
 * `scope=personal|household`, so a share-visibility change must refresh their
 * household aggregates rather than leave them stale.
 */
export async function invalidateScopedDashboardAnalyticsQueries(queryClient: QueryClient): Promise<void> {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: getGetBalanceSummaryQueryKey() }),
    queryClient.invalidateQueries({ queryKey: getGetNetWorthQueryKey() }),
    queryClient.invalidateQueries({ queryKey: getGetCashFlowSummaryQueryKey() }),
    queryClient.invalidateQueries({ queryKey: getGetBalanceHistoryQueryKey() }),
    queryClient.invalidateQueries({ queryKey: getGetNetWorthHistoryQueryKey() }),
    queryClient.invalidateQueries({ queryKey: getGetCashFlowHistoryQueryKey() }),
    queryClient.invalidateQueries({ queryKey: getGetNetWorthTrendQueryKey() }),
    queryClient.invalidateQueries({ queryKey: getGetHoldingsQueryKey() }),
    queryClient.invalidateQueries({ queryKey: getGetPortfolioSummaryQueryKey() }),
  ])
}
