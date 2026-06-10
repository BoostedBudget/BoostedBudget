// Issue #1267: Boosts page — per-tab transfer suggestion fetcher
import { useGetTransferSuggestions } from '@boostedbudget/api-client/endpoints/transfer-suggestions/transfer-suggestions'
import type { GetTransferSuggestionsParams } from '@boostedbudget/api-client/models'
import type { TransferSuggestion } from '@boostedbudget/api-client/models'

export interface UseBoostSuggestionsResult {
  items: TransferSuggestion[]
  total: number
  isLoading: boolean
  error: Error | null
  refetch: () => void
}

export function useBoostSuggestions(params: GetTransferSuggestionsParams): UseBoostSuggestionsResult {
  const { data, isLoading, error, refetch } = useGetTransferSuggestions(params)

  return {
    items: data?.items ?? [],
    total: data?.total ?? 0,
    isLoading,
    error: error as Error | null,
    refetch,
  }
}
