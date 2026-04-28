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
 * Matches backend domain.Transaction schema (PFC v2 canonical)
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
  // PFC v2 canonical fields
  pfc_primary?: string | null
  pfc_detailed?: string | null
  pfc_confidence_level?: string | null
  payment_channel?: string | null // 'online', 'in store', 'other'
  pending: boolean
  user_notes?: string | null
  // Category override (discriminated union)
  category_override_kind?: string | null // null | 'pfc' | 'custom'
  pfc_primary_override?: string | null
  pfc_detailed_override?: string | null
  custom_category_override_id?: string | null
  category_source?: CategorySource
  category_override_date?: string | null // ISO 8601 date-time
  // Tags
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
  account_id?: string
  start_date?: string // ISO 8601 date (YYYY-MM-DD)
  end_date?: string // ISO 8601 date (YYYY-MM-DD)
  search?: string // Search in name or merchant_name, case-insensitive
  pending?: boolean // Filter by pending status
  account_ids?: string[] // Multiple account IDs
  pfc_primaries?: string[] // PFC primary enum values
  min_amount?: number // Minimum absolute transaction amount
  max_amount?: number // Maximum absolute transaction amount
  amount_type?: 'income' | 'expense' | 'all' // Transaction type filter
}

/**
 * Date range preset options for quick filtering
 * Issue #529: Transaction page filtering
 * Issue #953: Standardized — now uses shared DatePreset from shared/dates
 */
export type { DatePreset } from '@/shared/dates'

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
 * Request to update transaction category (PFC v2)
 * Discriminated union: kind determines which fields are required
 */
export interface UpdateTransactionCategoryRequest {
  kind: 'pfc' | 'custom' | 'clear'
  pfc_primary?: string // required when kind='pfc'
  pfc_detailed?: string // optional when kind='pfc'
  custom_category_id?: string // required when kind='custom'
}
