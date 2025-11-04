import { apiClient } from '../../../api/axiosConfig'
import type { GetTransactionsResponse, FetchTransactionsParams } from '../types'

/**
 * Fetch transactions for the authenticated user
 * GET /api/v1/transactions
 *
 * Returns paginated list of transactions with optional filtering.
 * Supports filters for account, date range, category, search term, and pending status.
 *
 * @param params - Optional parameters for pagination and filtering
 * @param params.limit - Number of transactions to return (1-100, default 50)
 * @param params.offset - Number of transactions to skip for pagination (default 0)
 * @param params.filters - Optional filters for account, dates, category, search, pending status
 * @returns Paginated list of transactions with metadata (total_count, has_more)
 * @throws Error if request fails (401 unauthorized, 400 invalid parameters, 500 server error)
 */
export async function fetchTransactions(
  params?: FetchTransactionsParams
): Promise<GetTransactionsResponse> {
  // Construct query parameters, only including defined values
  const queryParams: Record<string, string | number | boolean> = {}

  // Pagination parameters
  if (params?.limit !== undefined) {
    queryParams.limit = params.limit
  }
  if (params?.offset !== undefined) {
    queryParams.offset = params.offset
  }

  // Filter parameters - only include if defined and non-empty
  if (params?.filters) {
    const { filters } = params

    if (filters.account_id !== undefined && filters.account_id !== '') {
      queryParams.account_id = filters.account_id
    }
    if (filters.start_date !== undefined && filters.start_date !== '') {
      queryParams.start_date = filters.start_date
    }
    if (filters.end_date !== undefined && filters.end_date !== '') {
      queryParams.end_date = filters.end_date
    }
    if (filters.category !== undefined && filters.category !== '') {
      queryParams.category = filters.category
    }
    if (filters.search !== undefined && filters.search !== '') {
      queryParams.search = filters.search
    }
    if (filters.pending !== undefined) {
      queryParams.pending = filters.pending
    }
  }

  const response = await apiClient.get<GetTransactionsResponse>('/v1/transactions', {
    params: queryParams,
  })

  return response.data
}
