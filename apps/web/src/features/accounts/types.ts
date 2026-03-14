/**
 * Account Types
 *
 * Re-exports generated types from OpenAPI spec for backward compatibility.
 * The backend API spec is the single source of truth.
 */

// Re-export generated types
export type { Account } from '@boostedbudget/api-client/models'

// Re-export generated request/response types with aliases
export type {
  UpdateAccountRequest,
  UpdateAccountOutputBody as UpdateAccountResponse,
  ReorderAccountsRequest,
  ReorderAccountsOutputBody as ReorderAccountsResponse,
  GetAccountsOutputBody as GetAccountsResponse,
} from '@boostedbudget/api-client/models'

// Local types for Plaid Link (not in OpenAPI spec)
export interface LinkTokenResponse {
  link_token: string
  expiration: string
  request_id: string
}

export interface ExchangePublicTokenRequest {
  public_token: string
  institution_id: string
  institution_name: string
}

export interface ExchangePublicTokenResponse {
  success: boolean
  item_id: string
  accounts_linked: number
}

// Valid icon identifiers (must match backend validation)
export const VALID_ACCOUNT_ICONS = [
  'wallet', 'piggy-bank', 'credit-card', 'bank', 'building',
  'briefcase', 'dollar', 'euro', 'pound', 'yen',
  'chart', 'trending-up', 'trending-down', 'lock', 'unlock',
  'star', 'heart', 'home', 'car', 'plane',
  'shopping-cart', 'gift', 'coffee', 'utensils', 'graduation-cap',
] as const

export type AccountIcon = typeof VALID_ACCOUNT_ICONS[number]

// Account order for reordering operations
export interface AccountOrder {
  account_id: string
  display_order: number
}

