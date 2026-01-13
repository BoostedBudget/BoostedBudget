/**
 * Category Utilities
 *
 * Utilities for converting between Plaid category enum format and human-readable format.
 * Issue #402: Fix Category Dropdowns in Auto-Categorization Rule Editor
 */

/**
 * Converts Plaid category enum (e.g., "FOOD_AND_DRINK") to human-readable format
 * (e.g., "Food And Drink")
 *
 * @param plaidCategory - The Plaid category enum string (e.g., "FOOD_AND_DRINK")
 * @returns Human-readable format (e.g., "Food And Drink")
 */
export function formatPlaidCategory(plaidCategory: string): string {
  if (!plaidCategory) return ''

  return plaidCategory
    .split('_')
    .map(word => word.charAt(0).toUpperCase() + word.slice(1).toLowerCase())
    .join(' ')
}

/**
 * Converts human-readable format back to Plaid enum format
 * (e.g., "Food And Drink" → "FOOD_AND_DRINK")
 *
 * @param displayName - The human-readable category name (e.g., "Food And Drink")
 * @returns Plaid enum format (e.g., "FOOD_AND_DRINK")
 */
export function toPlaidCategoryEnum(displayName: string): string {
  if (!displayName) return ''

  return displayName.toUpperCase().replace(/\s+/g, '_')
}
