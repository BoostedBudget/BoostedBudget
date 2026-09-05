/**
 * useSavingsGoalFreshness
 *
 * Issue #1569 (Task 13, E14): maps goal id -> `freshness` from the basis-aware progress
 * summary, so a card can explain a frozen balance-derived amount.
 *
 * The summary endpoint was built for MCP/AI consumption and had no frontend consumer at all,
 * which is why `freshness` read as dead surface. It is a SECONDARY signal here: the goals list
 * renders from its own query, and a summary that is loading, errored, or absent simply yields
 * an empty map — no card is annotated and nothing blocks.
 */

import { useMemo } from 'react'
import { useGetSavingsProgressSummary } from '@boostedbudget/api-client/endpoints/savings-goals/savings-goals'
import type { SavingsGoalProgressItemFreshness } from '@boostedbudget/api-client/models'

export function useSavingsGoalFreshness(): Record<string, SavingsGoalProgressItemFreshness> {
  // No staleTime override: whether an account is disconnected changes at most a few times in a
  // user's lifetime, so it has no business being refetched more eagerly than anything else on
  // the page. An earlier 30 s override made this rarely-changing flag the most aggressively
  // refetched query here; the app-wide default is the right cadence.
  const { data } = useGetSavingsProgressSummary()

  return useMemo(() => {
    const byGoalId: Record<string, SavingsGoalProgressItemFreshness> = {}
    for (const item of data?.goals ?? []) {
      if (item?.goal_id && item.freshness) {
        byGoalId[item.goal_id] = item.freshness
      }
    }
    return byGoalId
  }, [data])
}
