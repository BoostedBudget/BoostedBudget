// Plaid Link types matching backend OpenAPI schemas

/**
 * Response from POST /v1/plaid/link-token
 * Link token used to initialize Plaid Link in the frontend
 */
export interface LinkTokenResponse {
  link_token: string
  expiration: string // ISO 8601 date-time
  request_id: string
}

/**
 * Request body for POST /v1/plaid/exchange-token
 * Exchanges a public token from Plaid Link for account access
 */
export interface ExchangePublicTokenRequest {
  public_token: string
  institution_id: string
  institution_name: string
}

/**
 * Response from POST /v1/plaid/exchange-token
 * Result of linking bank accounts
 */
export interface ExchangePublicTokenResponse {
  success: boolean
  item_id: string
  accounts_linked: number
}

/**
 * Valid icon identifiers for account customization (Issue #307)
 */
export const VALID_ACCOUNT_ICONS = [
  'wallet', 'piggy-bank', 'credit-card', 'bank', 'building',
  'briefcase', 'dollar', 'euro', 'pound', 'yen',
  'chart', 'trending-up', 'trending-down', 'lock', 'unlock',
  'star', 'heart', 'home', 'car', 'plane',
  'shopping-cart', 'gift', 'coffee', 'utensils', 'graduation-cap',
] as const

export type AccountIcon = typeof VALID_ACCOUNT_ICONS[number]

/**
 * Bank account with details and institution information
 * Returned from GET /v1/accounts
 */
export interface Account {
  id: string
  linked_item_id: string
  user_id: string
  plaid_account_id: string
  name: string
  official_name?: string // Optional field from Plaid
  type: 'depository' | 'credit' | 'loan' | 'investment' | 'other'
  subtype?: string // checking, savings, credit card, etc.
  mask?: string // Last 4 digits
  current_balance?: number
  available_balance?: number
  currency_code: string
  institution_name?: string // From linked_items JOIN (optional)

  // Customization fields (Issue #307)
  nickname?: string // User-defined display name
  custom_color?: string // Hex color #RRGGBB
  custom_icon?: AccountIcon // Predefined icon identifier
  custom_emoji?: string // UTF-8 emoji
  display_order?: number // Sort order
  is_hidden?: boolean // Hidden from default views

  created_at: string // ISO 8601 date-time
  updated_at: string // ISO 8601 date-time
}

/**
 * Request body for PATCH /v1/accounts/:id (Issue #307)
 * All fields optional - omit to keep current value, empty string to clear
 */
export interface UpdateAccountRequest {
  nickname?: string | null
  custom_color?: string | null
  custom_icon?: AccountIcon | string | null
  custom_emoji?: string | null
  display_order?: number | null
  is_hidden?: boolean | null
}

/**
 * Response from PATCH /v1/accounts/:id (Issue #307)
 */
export interface UpdateAccountResponse {
  account: Account
}

/**
 * Single account order for reordering (Issue #307)
 */
export interface AccountOrder {
  account_id: string
  display_order: number
}

/**
 * Request body for PATCH /v1/accounts/reorder (Issue #307)
 */
export interface ReorderAccountsRequest {
  account_orders: AccountOrder[]
}

/**
 * Response from PATCH /v1/accounts/reorder (Issue #307)
 */
export interface ReorderAccountsResponse {
  success: boolean
}

/**
 * Response from GET /v1/accounts
 * List of all linked accounts for the authenticated user
 */
export interface GetAccountsResponse {
  accounts: Account[]
  total_count: number
}

/**
 * Monthly sync statistics for subscription tier limits
 * Response from GET /v1/sync-stats
 * Issue #242: Monthly sync limits
 */
export interface SyncStats {
  user_id: string
  month: string // YYYY-MM format
  accounts_synced: number
  accounts_limit: number // -1 for unlimited
  total_transactions: number
  can_sync_more: boolean
  reset_date: string // ISO 8601 date-time
}
