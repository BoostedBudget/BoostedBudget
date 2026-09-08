// Custom category types matching backend domain models and API schemas
// Issue #345: Manual Transaction Category & Tag Editing
//
// #1609: the REQUEST types take parent_pfc_primary from the generated client rather than
// declaring it `string`. The field stopped being a cosmetic hint -- nesting a category under
// TRANSFER_IN or TRANSFER_OUT now removes its transactions from cash flow -- so the API
// advertises the 18 PFCv2 primaries (plus '' to clear) as an enum, and a hand-written `string`
// here would let a typo through the type system and be rejected by the server instead.
import type { CreateCustomCategoryRequestParentPfcPrimary } from '@boostedbudget/api-client/models/createCustomCategoryRequestParentPfcPrimary'
import type { UpdateCustomCategoryRequestParentPfcPrimary } from '@boostedbudget/api-client/models/updateCustomCategoryRequestParentPfcPrimary'

/**
 * User-defined custom category for transaction categorization
 * Matches backend domain.CustomCategory schema
 */
export interface CustomCategory {
  id: string
  user_id: string
  name: string
  parent_pfc_primary?: string | null // PFC v2 hint / nesting under a PFC primary (#1073)
  parent_id?: string | null // Nesting under a custom primary (#1203); mutually exclusive with parent_pfc_primary
  parent_name?: string | null // Display name of the custom parent (#1203), populated by the API
  color?: string | null // Hex color code (e.g., "#FF5733")
  icon?: string | null // Icon identifier (key into the IconPicker registry)
  usage_count: number
  created_at: string // ISO 8601 date-time
  updated_at: string // ISO 8601 date-time
}

/**
 * Request to create a new custom category
 */
export interface CreateCategoryRequest {
  name: string
  // Nesting under a PFC primary (#1073). Load-bearing since #1609: see the file header.
  parent_pfc_primary?: CreateCustomCategoryRequestParentPfcPrimary
  parent_id?: string // Nesting under a custom primary (#1203)
  color?: string // Hex color code (e.g., "#FF5733")
  icon?: string // Icon identifier
}

/**
 * Request to update an existing custom category
 */
export interface UpdateCategoryRequest {
  name?: string
  // Nesting under a PFC primary (#1073). Load-bearing since #1609: see the file header.
  parent_pfc_primary?: UpdateCustomCategoryRequestParentPfcPrimary
  parent_id?: string // Nesting under a custom primary (#1203); empty string clears the parent
  color?: string // Hex color code (e.g., "#FF5733")
  icon?: string // Icon identifier
}

/**
 * Live usage counts for a custom category (#1203), used for the delete-impact warning.
 */
export interface CategoryUsage {
  transaction_count: number
  budget_count: number
  rule_count: number
  child_count: number
}
