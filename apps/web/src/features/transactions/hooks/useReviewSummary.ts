/**
 * useReviewSummary — fetches the transaction review summary from
 * GET /api/v1/transactions/review-summary.
 *
 * Used by:
 *   - ReviewInboxPage header (counts + top unreviewed categories)
 *   - Navigation badge (unreviewed_count)
 *
 * Query key: ['review-summary'] — invalidated by every review/category mutation
 * in useReviewMutations so the badge and page header stay in sync.
 */

import { useGetTransactionReviewSummary } from '@boostedbudget/api-client/endpoints/transactions/transactions'
import type { ReviewSummaryResponse } from '@boostedbudget/api-client/models/reviewSummaryResponse'

export type { ReviewSummaryResponse }

export interface UseReviewSummaryResult {
  summary: ReviewSummaryResponse | undefined
  isLoading: boolean
  isError: boolean
}

export function useReviewSummary(): UseReviewSummaryResult {
  const query = useGetTransactionReviewSummary(
    {},
    {
      query: {
        queryKey: ['review-summary'],
        staleTime: 60_000, // 1 min — nav badge can tolerate brief staleness
        refetchOnWindowFocus: true,
      },
    },
  )

  return {
    summary: query.data,
    isLoading: query.isLoading,
    isError: query.isError,
  }
}
