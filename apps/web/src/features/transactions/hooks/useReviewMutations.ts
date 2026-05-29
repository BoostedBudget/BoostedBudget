// Mark transactions reviewed/unreviewed with cache invalidation + analytics (#1203).
import { useQueryClient } from '@tanstack/react-query'
import {
  useSetTransactionReviewed,
  useBulkSetTransactionReviewed,
} from '@boostedbudget/api-client/endpoints/transactions/transactions'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'

export interface UseReviewMutationsResult {
  setReviewed: (transactionID: string, reviewed: boolean) => Promise<void>
  bulkSetReviewed: (transactionIDs: string[], reviewed: boolean) => Promise<void>
  isPending: boolean
}

export function useReviewMutations(): UseReviewMutationsResult {
  const queryClient = useQueryClient()
  const trackEvent = useTrackEvent()

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: ['transactions'] })
  }

  const single = useSetTransactionReviewed()
  const bulk = useBulkSetTransactionReviewed()

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

  return {
    setReviewed,
    bulkSetReviewed,
    isPending: single.isPending || bulk.isPending,
  }
}
