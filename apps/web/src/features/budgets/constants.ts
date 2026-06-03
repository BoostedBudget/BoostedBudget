/**
 * Constants for Budgets Feature
 * Issue #584: Unified Card/Table View System
 */

// =============================================================================
// Local Storage Keys
// =============================================================================

export const STORAGE_KEY_LIST_VIEW = 'budgets-list-view'

// =============================================================================
// Route Helpers
// =============================================================================

// Issue #1051: Navigate to the budget detail page
export function getBudgetDetailPath(id: string): string {
  return `/budgets/${id}`
}
