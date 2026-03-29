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
  parent_category?: string | null // For hierarchical categories
  color?: string | null // Hex color code (e.g., "#FF5733")
  icon?: string | null // Icon identifier
  usage_count: number
  created_at: string // ISO 8601 date-time
  updated_at: string // ISO 8601 date-time
}

/**
 * Request to create a new custom category
 */
export interface CreateCategoryRequest {
  name: string
  parent_category?: string // Parent category for hierarchy
  color?: string // Hex color code (e.g., "#FF5733")
  icon?: string // Icon identifier
}

/**
 * Request to update an existing custom category
 */
export interface UpdateCategoryRequest {
  name?: string
  parent_category?: string // Parent category for hierarchy
  color?: string // Hex color code (e.g., "#FF5733")
  icon?: string // Icon identifier
}
