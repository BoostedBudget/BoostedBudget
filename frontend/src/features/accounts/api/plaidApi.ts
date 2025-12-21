import { apiClient } from '../../../api/axiosConfig'
import type {
  LinkTokenResponse,
  ExchangePublicTokenRequest,
  ExchangePublicTokenResponse,
  GetAccountsResponse,
  UpdateAccountRequest,
  UpdateAccountResponse,
  ReorderAccountsRequest,
  ReorderAccountsResponse,
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
  const response = await apiClient.post<LinkTokenResponse>('/v1/plaid/link-token')
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
    '/v1/plaid/exchange-token',
    data
  )
  return response.data
}

/**
 * Fetch all linked bank accounts for the authenticated user
 * GET /api/v1/accounts
 *
 * Returns all accounts with institution information from linked_items.
 * Includes account details like balances, type, name, and institution.
 * Accounts are sorted by display_order (nulls last), then created_at.
 *
 * @param includeHidden - If true, includes hidden accounts. Default: false (Issue #307)
 * @returns List of accounts with total count
 * @throws Error if request fails (401 unauthorized, 500 server error)
 */
export async function fetchAccounts(includeHidden = false): Promise<GetAccountsResponse> {
  const params = includeHidden ? { include_hidden: 'true' } : {}
  const response = await apiClient.get<GetAccountsResponse>('/v1/accounts', { params })
  return response.data
}

/**
 * Delete a single bank account
 * DELETE /v1/accounts/:id
 *
 * Deletes the specified account with ownership validation.
 * Only the account owner can delete their account.
 * Other accounts from the same institution remain linked.
 *
 * @param accountId - UUID of the account to delete
 * @throws Error if request fails (401 unauthorized, 404 not found, 500 server error)
 */
export async function deleteAccount(accountId: string): Promise<void> {
  await apiClient.delete(`/v1/accounts/${accountId}`)
}

/**
 * Disconnect an institution and delete all associated accounts
 * DELETE /v1/linked-items/:id
 *
 * This endpoint:
 * 1. Validates ownership of the linked item
 * 2. Calls Plaid /item/remove to stop monthly billing
 * 3. Deletes the linked item from database
 * 4. CASCADE deletes all associated accounts
 *
 * @param linkedItemId - UUID of the linked item (institution) to disconnect
 * @throws Error if request fails (401 unauthorized, 404 not found, 500 server error)
 */
export async function disconnectInstitution(linkedItemId: string): Promise<void> {
  await apiClient.delete(`/v1/linked-items/${linkedItemId}`)
}

/**
 * Update account customization fields (Issue #307)
 * PATCH /v1/accounts/:id
 *
 * Supports partial updates - only include fields to update.
 * Set a field to null/empty string to clear it.
 * Validates:
 * - nickname: max 255 characters
 * - custom_color: hex format #RRGGBB
 * - custom_icon: one of predefined icons
 * - custom_emoji: max 10 characters
 *
 * @param accountId - UUID of the account to update
 * @param data - Partial update fields
 * @returns Updated account with all fields
 * @throws Error if request fails (400 validation error, 401 unauthorized, 404 not found, 500 server error)
 */
export async function updateAccount(
  accountId: string,
  data: UpdateAccountRequest
): Promise<UpdateAccountResponse> {
  const response = await apiClient.patch<UpdateAccountResponse>(
    `/v1/accounts/${accountId}`,
    data
  )
  return response.data
}

/**
 * Bulk update display order for multiple accounts (Issue #307)
 * PATCH /v1/accounts/reorder
 *
 * Used for drag-and-drop reordering. All accounts must belong
 * to the authenticated user. Order values should be sequential
 * starting from 0.
 *
 * @param accountOrders - Array of account IDs with their new display order
 * @returns Success status
 * @throws Error if request fails (400 empty list, 401 unauthorized, 404 account not found, 500 server error)
 */
export async function reorderAccounts(
  accountOrders: ReorderAccountsRequest['account_orders']
): Promise<ReorderAccountsResponse> {
  const response = await apiClient.patch<ReorderAccountsResponse>(
    '/v1/accounts/reorder',
    { account_orders: accountOrders }
  )
  return response.data
}
