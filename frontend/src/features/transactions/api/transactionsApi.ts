import { apiClient } from '../../../api/axiosConfig'
import type {
  GetTransactionsResponse,
  FetchTransactionsParams,
  Transaction,
  UpdateTransactionTagsRequest,
  UpdateTransactionCategoryRequest,
} from '../types'

/**
 * Fetch transactions for the authenticated user
 * GET /api/v1/transactions
 *
 * Returns paginated list of transactions with optional filtering.
 * Supports filters for account(s), date range, category/categories, search term,
 * pending status, amount range, and transaction type.
 *
 * @param params - Optional parameters for pagination and filtering
 * @param params.limit - Number of transactions to return (1-100, default 50)
 * @param params.offset - Number of transactions to skip for pagination (default 0)
 * @param params.filters - Optional filters for transactions
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

    // Single account ID (deprecated, use account_ids)
    if (filters.account_id !== undefined && filters.account_id !== '') {
      queryParams.account_id = filters.account_id
    }

    // Multiple account IDs (Issue #529)
    if (filters.account_ids !== undefined && filters.account_ids.length > 0) {
      queryParams.account_ids = filters.account_ids.join(',')
    }

    // Date range
    if (filters.start_date !== undefined && filters.start_date !== '') {
      queryParams.start_date = filters.start_date
    }
    if (filters.end_date !== undefined && filters.end_date !== '') {
      queryParams.end_date = filters.end_date
    }

    // Single category (deprecated, use categories)
    if (filters.category !== undefined && filters.category !== '') {
      queryParams.category = filters.category
    }

    // Multiple categories (Issue #529)
    if (filters.categories !== undefined && filters.categories.length > 0) {
      queryParams.categories = filters.categories.join(',')
    }

    // Search
    if (filters.search !== undefined && filters.search !== '') {
      queryParams.search = filters.search
    }

    // Pending status
    if (filters.pending !== undefined) {
      queryParams.pending = filters.pending
    }

    // Amount range (Issue #529)
    if (filters.min_amount !== undefined) {
      queryParams.min_amount = filters.min_amount
    }
    if (filters.max_amount !== undefined) {
      queryParams.max_amount = filters.max_amount
    }

    // Transaction type (Issue #529)
    if (filters.amount_type !== undefined && filters.amount_type !== 'all') {
      queryParams.amount_type = filters.amount_type
    }
  }

  const response = await apiClient.get<GetTransactionsResponse>('/v1/transactions', {
    params: queryParams,
  })

  return response.data
}

/**
 * Get a single transaction by ID
 * GET /api/v1/transactions/:id
 */
export async function fetchTransactionById(transactionId: string): Promise<Transaction> {
  const response = await apiClient.get<Transaction>(`/v1/transactions/${transactionId}`)
  return response.data
}

/**
 * Update transaction tags
 * PUT /api/v1/transactions/:id/tags
 */
export async function updateTransactionTags(
  transactionId: string,
  request: UpdateTransactionTagsRequest
): Promise<Transaction> {
  const response = await apiClient.put<Transaction>(`/v1/transactions/${transactionId}/tags`, request)
  return response.data
}

/**
 * Update transaction category
 * PUT /api/v1/transactions/:id/category
 */
export async function updateTransactionCategory(
  transactionId: string,
  request: UpdateTransactionCategoryRequest
): Promise<Transaction> {
  const response = await apiClient.put<Transaction>(`/v1/transactions/${transactionId}/category`, request)
  return response.data
}
