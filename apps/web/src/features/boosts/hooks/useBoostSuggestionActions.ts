// Issue #1267: Boosts page — accept/reject/confirm-match/cancel with invalidation + analytics
import { useQueryClient } from '@tanstack/react-query'
import {
  useAcceptTransferSuggestion,
  useRejectTransferSuggestion,
  useConfirmTransferSuggestionMatch,
  useCancelTransferSuggestion,
  getGetTransferSuggestionsQueryKey,
} from '@boostedbudget/api-client/endpoints/transfer-suggestions/transfer-suggestions'
import { getGetSavingsImpactMeQueryKey } from '@boostedbudget/api-client/endpoints/savings-impact/savings-impact'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'

export interface UseBoostSuggestionActionsResult {
  /** Accept a pending suggestion */
  accept: (suggestionId: string) => void
  /** Reject a pending suggestion */
  reject: (suggestionId: string) => void
  /** Confirm that an awaiting-match suggestion has been fulfilled (requires a fuzzy-match reservation) */
  confirmMatch: (suggestionId: string, actualAmount: number) => void
  /** Cancel a pending/accepted suggestion, transitioning it to rejected so it leaves the Awaiting tab */
  cancelAccepted: (suggestionId: string) => void
  /** True while any mutation is in-flight */
  isPending: boolean
  /** Last error from any mutation, or null */
  error: Error | null
}

export function useBoostSuggestionActions(): UseBoostSuggestionActionsResult {
  const queryClient = useQueryClient()
  const trackEvent = useTrackEvent()

  const acceptMutation = useAcceptTransferSuggestion({
    mutation: {
      onSuccess: (data) => {
        queryClient.invalidateQueries({ queryKey: getGetTransferSuggestionsQueryKey() })
        trackEvent(ANALYTICS_EVENTS.TRANSFER_SUGGESTION_ACCEPTED, {
          amount: data?.suggestion?.amount,
          source: 'boosts_page',
        })
      },
      onError: (err) => console.error('[Boosts] accept failed:', err),
    },
  })

  const rejectMutation = useRejectTransferSuggestion({
    mutation: {
      onSuccess: () => {
        queryClient.invalidateQueries({ queryKey: getGetTransferSuggestionsQueryKey() })
        trackEvent(ANALYTICS_EVENTS.TRANSFER_SUGGESTION_REJECTED, { source: 'boosts_page' })
      },
      onError: (err) => console.error('[Boosts] reject failed:', err),
    },
  })

  const confirmMatchMutation = useConfirmTransferSuggestionMatch({
    mutation: {
      onSuccess: () => {
        queryClient.invalidateQueries({ queryKey: getGetTransferSuggestionsQueryKey() })
        // Also refresh the summary header — completed count + total moved change after a confirmed match
        queryClient.invalidateQueries({ queryKey: getGetSavingsImpactMeQueryKey() })
        trackEvent(ANALYTICS_EVENTS.TRANSFER_SUGGESTION_MATCH_CONFIRMED, { source: 'boosts_page' })
      },
      onError: (err) => console.error('[Boosts] confirm-match failed:', err),
    },
  })

  const cancelMutation = useCancelTransferSuggestion({
    mutation: {
      onSuccess: () => {
        queryClient.invalidateQueries({ queryKey: getGetTransferSuggestionsQueryKey() })
        trackEvent(ANALYTICS_EVENTS.TRANSFER_SUGGESTION_REJECTED, { source: 'boosts_page' })
      },
      onError: (err) => console.error('[Boosts] cancel-accepted failed:', err),
    },
  })

  const mutations = [acceptMutation, rejectMutation, confirmMatchMutation, cancelMutation]

  return {
    accept: (suggestionId: string) => acceptMutation.mutate({ id: suggestionId, data: {} }),
    reject: (suggestionId: string) => rejectMutation.mutate({ id: suggestionId, data: {} }),
    confirmMatch: (suggestionId: string, actualAmount: number) =>
      confirmMatchMutation.mutate({ id: suggestionId, data: { actual_amount: actualAmount } }),
    cancelAccepted: (suggestionId: string) => cancelMutation.mutate({ id: suggestionId, data: {} }),
    isPending: mutations.some((m) => m.isPending),
    error: mutations.map((m) => m.error).find(Boolean) ?? null,
  }
}
