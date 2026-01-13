// Transaction types matching backend domain models and API schemas

/**
 * Transaction source identifier
 * Indicates where the transaction data originated from
 */
export type TransactionSource = 'plaid' | 'manual' | 'teller' | 'finicity' | 'yodlee'

/**
 * Category source identifier
 * Indicates where the category came from
 */
export type CategorySource = 'plaid' | 'rule' | 'manual'

/**
 * Tag source tracking
 * Tracks where each tag on a transaction came from
 */
export interface TagSource {
  tag: string
  source: string // 'manual', 'rule', etc.
}

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
  // Issue #345: Category override fields
  category_override?: string | null // JSON array of user's chosen category
  category_source?: CategorySource
  category_override_date?: string | null // ISO 8601 date-time
  original_category?: string | null // Preserved Plaid category
  // Issue #345: Tags fields
  tags?: string[] // Array of tag names
  tags_source?: TagSource[] // Source tracking for each tag
  created_at: string // ISO 8601 date-time
  updated_at: string // ISO 8601 date-time
}

/**
 * Optional filters for querying transactions
 * All fields are optional - null/undefined means no filter
 */
export interface TransactionFilters {
  // Existing (deprecated single-value filters)
  account_id?: string
  start_date?: string // ISO 8601 date (YYYY-MM-DD)
  end_date?: string // ISO 8601 date (YYYY-MM-DD)
  category?: string // Partial match, case-insensitive (deprecated: use categories)
  search?: string // Search in name or merchant_name, case-insensitive
  pending?: boolean // Filter by pending status

  // Issue #529: Extended filter support
  account_ids?: string[] // Multiple account IDs
  categories?: string[] // Multiple categories
  min_amount?: number // Minimum absolute transaction amount
  max_amount?: number // Maximum absolute transaction amount
  amount_type?: 'income' | 'expense' | 'all' // Transaction type filter
}

/**
 * Date range preset options for quick filtering
 * Issue #529: Transaction page filtering
 */
export type DatePreset = '7d' | '30d' | '90d' | '1y' | 'all' | 'custom'

/**
 * Status filter options
 * Issue #529: Transaction page filtering
 */
export type StatusFilter = 'all' | 'posted' | 'pending'

/**
 * Response from GET /v1/transactions
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

/**
 * Request to update transaction tags
 * Issue #345: Manual tag editing
 */
export interface UpdateTransactionTagsRequest {
  tags: string[]
}

/**
 * Request to update transaction category
 * Issue #345: Manual category editing
 */
export interface UpdateTransactionCategoryRequest {
  category: string[]
}
