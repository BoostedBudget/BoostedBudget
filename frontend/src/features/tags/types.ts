// User tag types matching backend domain models and API schemas
// Issue #345: Manual Transaction Category & Tag Editing

/**
 * User-defined tag for transaction organization
 * Matches backend domain.UserTag schema
 */
export interface UserTag {
  id: string
  user_id: string
  name: string
  color?: string | null // Hex color code (e.g., "#FF5733")
  usage_count: number
  created_at: string // ISO 8601 date-time
  updated_at: string // ISO 8601 date-time
}

/**
 * Request to create a new tag
 */
export interface CreateTagRequest {
  name: string
  color?: string // Hex color code (e.g., "#FF5733")
}

/**
 * Request to update an existing tag
 */
export interface UpdateTagRequest {
  name?: string
  color?: string // Hex color code (e.g., "#FF5733")
}
