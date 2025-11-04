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
  created_at: string // ISO 8601 date-time
  updated_at: string // ISO 8601 date-time
}

/**
 * Response from GET /v1/accounts
 * List of all linked accounts for the authenticated user
 */
export interface GetAccountsResponse {
  accounts: Account[]
  total_count: number
}
