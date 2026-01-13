// Shared matching logic types used across features
// Issue #495: Extract common matching infrastructure

/**
 * Match logic for combining category and tag matching.
 * Used by: budget category matching, potentially future categorization rule enhancements
 *
 * Matches backend domain.MatchLogic in backend/internal/domain/budget.go
 */
export type MatchLogic = 'category_only' | 'tag_only' | 'and' | 'or'

/**
 * Human-readable labels for MatchLogic dropdown/display
 */
export const MATCH_LOGIC_LABELS: Record<MatchLogic, string> = {
  category_only: 'Match by category only',
  tag_only: 'Match by tags only',
  and: 'Match category AND tags',
  or: 'Match category OR tags',
}

/**
 * Descriptions for each match logic option (for UI tooltips)
 */
export const MATCH_LOGIC_DESCRIPTIONS: Record<MatchLogic, string> = {
  category_only: 'Transactions must match the specified category hierarchy',
  tag_only: 'Transactions must have at least one of the specified tags',
  and: 'Transactions must match BOTH category AND have at least one tag',
  or: 'Transactions matching category OR having any tag will count',
}

/**
 * Type guard to check if a string is a valid MatchLogic
 */
export function isMatchLogic(value: string): value is MatchLogic {
  return ['category_only', 'tag_only', 'and', 'or'].includes(value)
}

/**
 * Default match logic for budget categories
 */
export const DEFAULT_MATCH_LOGIC: MatchLogic = 'category_only'
