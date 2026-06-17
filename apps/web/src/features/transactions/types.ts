// Transaction types matching backend domain models and API schemas
import type { Account } from '@/features/accounts/types'

/**
 * Transaction source identifier (provider level)
 * Indicates which data provider the transaction came from
 */
export type TransactionSource = 'plaid' | 'manual' | 'teller' | 'finicity' | 'yodlee'

/**
 * Investment activity type — mirrors backend InvestmentActivity domain.
 */
export type ActivityType = 'buy' | 'sell' | 'dividend' | 'reinvest' | 'fee' | 'cash' | 'other'

/**
 * Feed row source discriminator — identifies the row kind in the unified activity feed.
 * Distinct from TransactionSource (provider-level) which identifies the data provider.
 */
export type FeedRowSource = 'banking' | 'investment'

/**
 * Category source identifier
 * Indicates where the category came from
 */
export type CategorySource = 'plaid' | 'rule' | 'manual'

/**
 * Tag source tracking — identifies where each tag originated (manual, rule, etc.)
 */
export interface TagSource {
  tag: string
  source: string
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
  amount: number // PFCv2: Positive=outflow (spending), Negative=inflow (refund/income)
  date: string // ISO 8601 date (YYYY-MM-DD)
  authorized_date?: string | null // ISO 8601 date
  name: string
  merchant_name?: string | null
  // PFC v2 canonical fields
  pfc_primary?: string | null
  pfc_detailed?: string | null
  pfc_primary_display?: string | null
  pfc_detailed_display?: string | null
  pfc_confidence_level?: string | null
  pfc_category_icon_url?: string | null
  payment_channel?: string | null // 'online', 'in store', 'other'
  pending: boolean
  user_notes?: string | null
  // Category override (discriminated union)
  category_override_kind?: string | null // null | 'pfc' | 'custom'
  pfc_primary_override?: string | null
  pfc_detailed_override?: string | null
  pfc_primary_override_display?: string | null
  pfc_detailed_override_display?: string | null
  custom_category_override_id?: string | null
  custom_category_override_display?: string | null
  category_source?: CategorySource
  category_override_date?: string | null // ISO 8601 date-time
  // Review state (#1203)
  reviewed?: boolean
  reviewed_at?: string | null // ISO 8601 date-time
  // Tags
  tags?: string[] // Array of tag names
  tags_source?: TagSource[] // Source tracking for each tag
  created_at: string // ISO 8601 date-time
  updated_at: string // ISO 8601 date-time
}

/**
 * Banking transaction row in the unified feed
 * Mirrors ActivityItem from the API with source discriminator narrowed to 'banking'.
 * All fields correspond to ActivityItem in packages/api-client/src/models/activityItem.ts.
 */
export interface BankingTransaction {
  source: 'banking'
  id: string
  user_id: string
  account_id: string
  linked_item_id?: string | null
  amount: number
  date: string
  name: string
  merchant_name?: string | null
  merchant_id?: string | null
  pending: boolean
  authorized_date?: string | null
  payment_channel?: string | null
  pfc_primary?: string | null
  pfc_detailed?: string | null
  pfc_primary_display?: string | null
  pfc_detailed_display?: string | null
  pfc_confidence_level?: string | null
  pfc_category_icon_url?: string | null
  pfc_primary_override?: string | null
  pfc_detailed_override?: string | null
  pfc_primary_override_display?: string | null
  pfc_detailed_override_display?: string | null
  effective_pfc_primary?: string | null
  effective_pfc_detailed?: string | null
  effective_category_name?: string | null
  category_override_kind?: string | null
  custom_category_override_id?: string | null
  custom_category_override_display?: string | null
  external_transaction_id?: string | null
  reviewed?: boolean
  reviewed_at?: string | null
  user_notes?: string | null
  tags?: string[] | null
  data_source?: string | null
  sort_date?: string
  created_at: string
  updated_at: string
}

/**
 * Investment activity row in the unified feed
 * Mirrors ActivityItem from the API with source discriminator narrowed to 'investment'.
 * Investment-specific fields are OPTIONAL to match the generated ActivityItem model (the
 * backend returns nullable pointers for all investment-specific fields). Render sites must
 * coalesce with '?? "—"' guards; the sparse-row test asserts no "undefined" leaks through.
 *
 * T1: `account` is included so the embedded Account object (from the API) survives the
 * ActivityItem→FeedRow cast and resolves the accountsMap loading-race in the drawer.
 * T2: investment fields made optional, matching the generated ActivityItem contract.
 */
export interface InvestmentActivity {
  source: 'investment'
  id: string
  user_id: string
  account_id: string
  /** Embedded Account object from the ActivityItem API response (resolves loading-race). */
  account?: Account | null
  linked_item_id?: string | null
  amount: number
  date: string
  name: string
  // Investment-specific fields — optional (backend returns nullable pointers)
  ticker_symbol?: string | null
  security_name?: string | null
  activity_type?: ActivityType | null
  investment_quantity?: number | null
  investment_price?: number | null
  investment_fees?: number | null
  // Investment-specific optional fields
  investment_subtype?: string | null
  security_id?: string | null
  // Common optional fields
  pending?: boolean
  authorized_date?: string | null
  sort_date?: string
  data_source?: string | null
  user_notes?: string | null
  tags?: string[] | null
  created_at: string
  updated_at: string
}

/**
 * Unified feed row — discriminated union of banking and investment activity.
 * Use isInvestmentRow() to narrow to InvestmentActivity.
 */
export type FeedRow = BankingTransaction | InvestmentActivity

/**
 * Type guard: narrows FeedRow to InvestmentActivity when source === 'investment'
 */
export function isInvestmentRow(t: FeedRow): t is InvestmentActivity {
  return t.source === 'investment'
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
  reviewed?: boolean // Filter by review state (#1203): true=reviewed, false=unreviewed inbox
  category_sources?: CategorySource[] // Filter by category source (#1210): plaid|rule|manual
  account_ids?: string[] // Multiple account IDs
  pfc_primaries?: string[] // PFC primary enum values
  pfc_detaileds?: string[] // PFC detailed enum values (OR with pfc_primaries)
  custom_category_ids?: string[] // Custom category UUIDs (#1203); parents roll up children
  min_amount?: number // Minimum absolute transaction amount
  max_amount?: number // Maximum absolute transaction amount
  amount_type?: 'income' | 'expense' | 'all' // Transaction type filter
  merchant_id?: string // Filter by merchant UUID
  // Issue #1299: Investment transactions in unified feed
  include_investment_activity?: boolean // default: true (absence means true)
  activity_types?: ActivityType[] // Filter by investment activity type
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
