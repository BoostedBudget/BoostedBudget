// Plaid Link types matching backend OpenAPI schemas

/**
 * Response from POST /api/v1/plaid/link-token
 * Link token used to initialize Plaid Link in the frontend
 */
export interface LinkTokenResponse {
  link_token: string
  expiration: string // ISO 8601 date-time
  request_id: string
}

/**
 * Request body for POST /api/v1/plaid/exchange-token
 * Exchanges a public token from Plaid Link for account access
 */
export interface ExchangePublicTokenRequest {
  public_token: string
  institution_id: string
  institution_name: string
}

/**
 * Response from POST /api/v1/plaid/exchange-token
 * Result of linking bank accounts
 */
export interface ExchangePublicTokenResponse {
  success: boolean
  item_id: string
  accounts_linked: number
}
