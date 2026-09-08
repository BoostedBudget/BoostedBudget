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

// Local (non-re-exported) import of the same type, needed below: `export type { X } from 'y'`
// forwards X without introducing a usable local binding for this file's own type expressions.
import type { UpdateAccountRequest } from '@boostedbudget/api-client/models'

/**
 * Local widening of the generated `UpdateAccountRequest` PATCH payload type.
 *
 * `exclude_investment_sync` (#1618b) lives on `UpdateUserAccountRequest` — the wire type
 * `useAccountCustomization`'s `updateAccount` actually sends when an account has a
 * `user_account_id` (the common case) — but NOT on `UpdateAccountRequest`, the fallback
 * account-endpoint PATCH body used when it doesn't. `AccountCustomizationModal`'s local
 * `changes` state has to carry the field regardless of which endpoint eventually handles the
 * write, so this widens `UpdateAccountRequest` rather than importing either generated type
 * directly. See `useAccountCustomization.ts`'s own `PartialUpdateAccountRequest` for the
 * sibling type on the consuming side — keep the two in sync if either gains a field.
 */
export type AccountCustomizationChanges = Partial<UpdateAccountRequest> & {
  exclude_investment_sync?: boolean | null
}

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
  /** The Plaid provider's item id — NOT the same value as any account's `linked_item_id`. */
  item_id: string
  /**
   * The internal linked_items.id — matches `Account.linked_item_id` (Issue
   * #1370 Task 9). Use THIS field, not `item_id`, to find which accounts
   * were just linked after a post-link accounts refetch.
   */
  linked_item_id: string
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

