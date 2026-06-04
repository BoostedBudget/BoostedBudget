// Mark transactions reviewed/unreviewed with cache invalidation + analytics (#1203, #1210).
import { useQueryClient } from '@tanstack/react-query'
import {
  useSetTransactionReviewed,
  useBulkSetTransactionReviewed,
  useBulkUpdateTransactionCategory,
} from '@boostedbudget/api-client/endpoints/transactions/transactions'
import type { BulkUpdateTransactionCategoryInputBody } from '@boostedbudget/api-client/models/bulkUpdateTransactionCategoryInputBody'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'

export type BulkRecategorizeRequest = Omit<BulkUpdateTransactionCategoryInputBody, 'transaction_ids'>

export interface UseReviewMutationsResult {
  setReviewed: (transactionID: string, reviewed: boolean) => Promise<void>
  bulkSetReviewed: (transactionIDs: string[], reviewed: boolean) => Promise<void>
  bulkRecategorize: (transactionIDs: string[], request: BulkRecategorizeRequest) => Promise<{ requested: number; updated: number }>
  isPending: boolean
}

export function useReviewMutations(): UseReviewMutationsResult {
  const queryClient = useQueryClient()
  const trackEvent = useTrackEvent()

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: ['transactions'] })
    queryClient.invalidateQueries({ queryKey: ['review-summary'] })
  }

  const single = useSetTransactionReviewed()
  const bulk = useBulkSetTransactionReviewed()
  const bulkCat = useBulkUpdateTransactionCategory()

  const setReviewed = async (transactionID: string, reviewed: boolean): Promise<void> => {
    await single.mutateAsync({ id: transactionID, data: { reviewed } })
    invalidate()
    trackEvent(ANALYTICS_EVENTS.TRANSACTION_REVIEWED, { reviewed, count: 1 })
  }

  const bulkSetReviewed = async (transactionIDs: string[], reviewed: boolean): Promise<void> => {
    if (transactionIDs.length === 0) return
    await bulk.mutateAsync({ data: { transaction_ids: transactionIDs, reviewed } })
    invalidate()
    trackEvent(ANALYTICS_EVENTS.TRANSACTION_REVIEWED, { reviewed, count: transactionIDs.length })
  }

  const bulkRecategorize = async (
    transactionIDs: string[],
    request: BulkRecategorizeRequest,
  ): Promise<{ requested: number; updated: number }> => {
    if (transactionIDs.length === 0) return { requested: 0, updated: 0 }
    const result = await bulkCat.mutateAsync({
      data: { ...request, transaction_ids: transactionIDs },
    })
    invalidate()
    trackEvent(ANALYTICS_EVENTS.REVIEW_BULK_RECATEGORIZED, {
      count: transactionIDs.length,
      kind: request.kind,
    })
    return result as { requested: number; updated: number }
  }

  return {
    setReviewed,
    bulkSetReviewed,
    bulkRecategorize,
    isPending: single.isPending || bulk.isPending || bulkCat.isPending,
  }
}
