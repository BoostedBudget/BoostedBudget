// Custom category types matching backend domain models and API schemas
// Issue #345: Manual Transaction Category & Tag Editing

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
  parent_pfc_primary?: string // PFC v2 primary enum hint / nesting under a PFC primary (#1073)
  parent_id?: string // Nesting under a custom primary (#1203)
  color?: string // Hex color code (e.g., "#FF5733")
  icon?: string // Icon identifier
}

/**
 * Request to update an existing custom category
 */
export interface UpdateCategoryRequest {
  name?: string
  parent_pfc_primary?: string // PFC v2 primary enum hint / nesting under a PFC primary (#1073)
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
