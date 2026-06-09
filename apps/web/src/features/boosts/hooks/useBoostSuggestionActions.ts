// Issue #1267: Boosts page — accept/reject/confirm-match/decline-match with invalidation + analytics
import { useQueryClient } from '@tanstack/react-query'
import {
  useAcceptTransferSuggestion,
  useRejectTransferSuggestion,
  useConfirmTransferSuggestionMatch,
  useDeclineTransferSuggestionMatch,
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
  /** Confirm that an awaiting-match suggestion has been fulfilled */
  confirmMatch: (suggestionId: string, actualAmount: number) => void
  /** Decline a fuzzy-match prompt for an awaiting-match suggestion */
  declineMatch: (suggestionId: string) => void
  /** True while any of the four mutations is in-flight */
  isPending: boolean
  /** Last error from any mutation, or null */
  error: Error | null
}

export function useBoostSuggestionActions(): UseBoostSuggestionActionsResult {
  const queryClient = useQueryClient()
  const trackEvent = useTrackEvent()

  const invalidateSuggestions = () =>
    queryClient.invalidateQueries({ queryKey: getGetTransferSuggestionsQueryKey() })

  const invalidateSavingsImpact = () =>
    queryClient.invalidateQueries({ queryKey: getGetSavingsImpactMeQueryKey() })

  const acceptMutation = useAcceptTransferSuggestion({
    mutation: {
      onSuccess: (data) => {
        invalidateSuggestions()
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
        invalidateSuggestions()
        trackEvent(ANALYTICS_EVENTS.TRANSFER_SUGGESTION_REJECTED, { source: 'boosts_page' })
      },
      onError: (err) => console.error('[Boosts] reject failed:', err),
    },
  })

  const confirmMatchMutation = useConfirmTransferSuggestionMatch({
    mutation: {
      onSuccess: () => {
        invalidateSuggestions()
        // Also refresh the summary header — completed count + total moved change after a confirmed match
        invalidateSavingsImpact()
        trackEvent(ANALYTICS_EVENTS.TRANSFER_SUGGESTION_MATCH_CONFIRMED, { source: 'boosts_page' })
      },
      onError: (err) => console.error('[Boosts] confirm-match failed:', err),
    },
  })

  const declineMatchMutation = useDeclineTransferSuggestionMatch({
    mutation: {
      onSuccess: () => {
        invalidateSuggestions()
        trackEvent(ANALYTICS_EVENTS.TRANSFER_SUGGESTION_MATCH_DECLINED, { source: 'boosts_page' })
      },
      onError: (err) => console.error('[Boosts] decline-match failed:', err),
    },
  })

  const mutations = [acceptMutation, rejectMutation, confirmMatchMutation, declineMatchMutation]

  return {
    accept: (suggestionId: string) => acceptMutation.mutate({ id: suggestionId, data: {} }),
    reject: (suggestionId: string) => rejectMutation.mutate({ id: suggestionId, data: {} }),
    confirmMatch: (suggestionId: string, actualAmount: number) =>
      confirmMatchMutation.mutate({ id: suggestionId, data: { actual_amount: actualAmount } }),
    declineMatch: (suggestionId: string) => declineMatchMutation.mutate({ id: suggestionId }),
    isPending: mutations.some((m) => m.isPending),
    error: mutations.map((m) => m.error).find(Boolean) ?? null,
  }
}
