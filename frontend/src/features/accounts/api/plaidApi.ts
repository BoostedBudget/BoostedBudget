import { apiClient } from '../../../api/axiosConfig'
import type {
  LinkTokenResponse,
  ExchangePublicTokenRequest,
  ExchangePublicTokenResponse,
} from '../types'

/**
 * Fetch a Plaid Link token from the backend
 * POST /api/v1/plaid/link-token
 *
 * The link token is used to initialize Plaid Link in the frontend.
 * Token is valid for 30 minutes.
 *
 * @returns Link token data including token, expiration, and request ID
 * @throws Error if request fails (401 unauthorized, 500 server error, network error)
 */
export async function fetchLinkToken(): Promise<LinkTokenResponse> {
  const response = await apiClient.post<LinkTokenResponse>('/api/v1/plaid/link-token')
  return response.data
}

/**
 * Exchange a Plaid public token for account access
 * POST /api/v1/plaid/exchange-token
 *
 * This endpoint:
 * 1. Validates subscription tier account limits
 * 2. Exchanges public token for access token via Plaid API
 * 3. Fetches account information from Plaid
 * 4. Encrypts and stores access token
 * 5. Creates linked item and account records in database
 *
 * @param data - Public token and institution information from Plaid Link
 * @returns Exchange result with success status, item ID, and accounts linked count
 * @throws Error if request fails (400 invalid token, 403 limit exceeded, 500 server error, 503 Plaid unavailable)
 */
export async function exchangePublicToken(
  data: ExchangePublicTokenRequest
): Promise<ExchangePublicTokenResponse> {
  const response = await apiClient.post<ExchangePublicTokenResponse>(
    '/api/v1/plaid/exchange-token',
    data
  )
  return response.data
}
