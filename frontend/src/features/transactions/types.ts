// Transaction types matching backend domain models and API schemas

/**
 * Transaction source identifier
 * Indicates where the transaction data originated from
 */
export type TransactionSource = 'plaid' | 'manual' | 'teller' | 'finicity' | 'yodlee'

/**
 * Transaction record from any provider
 * Matches backend domain.Transaction schema
 */
export interface Transaction {
  id: string
  user_id: string
  account_id: string
  linked_item_id: string
  source: TransactionSource
  external_transaction_id?: string | null
  external_metadata?: string | null
  amount: number // Positive=credit, Negative=debit
  date: string // ISO 8601 date (YYYY-MM-DD)
  authorized_date?: string | null // ISO 8601 date
  name: string
  merchant_name?: string | null
  category?: string | null // JSON array of category strings
  personal_finance_category?: string | null // JSON object with detailed categorization
  payment_channel?: string | null // 'online', 'in store', 'other'
  pending: boolean
  user_notes?: string | null
  created_at: string // ISO 8601 date-time
  updated_at: string // ISO 8601 date-time
}

/**
 * Optional filters for querying transactions
 * All fields are optional - null/undefined means no filter
 */
export interface TransactionFilters {
  account_id?: string
  start_date?: string // ISO 8601 date (YYYY-MM-DD)
  end_date?: string // ISO 8601 date (YYYY-MM-DD)
  category?: string // Partial match, case-insensitive
  search?: string // Search in name or merchant_name, case-insensitive
  pending?: boolean // Filter by pending status
}

/**
 * Response from GET /api/v1/transactions
 * Paginated list of transactions with metadata
 */
export interface GetTransactionsResponse {
  transactions: Transaction[]
  total_count: number
  limit: number
  offset: number
  has_more: boolean
}

/**
 * Parameters for fetching transactions
 * Used by API and hooks
 */
export interface FetchTransactionsParams {
  filters?: TransactionFilters
  limit?: number // 1-100, default 50
  offset?: number // Default 0
}
