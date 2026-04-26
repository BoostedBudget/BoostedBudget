/**
 * Transaction Budget Matcher Utility
 * Issue #499: Transaction-Budget Linking UI
 *
 * Provides client-side matching logic to determine which budget category
 * a transaction belongs to. Replicates backend JSONB matching behavior
 * to avoid N+1 API calls.
 */

import type { Transaction } from '../../transactions/types'
import type { BudgetCategory } from '../types'
import { getEffectiveCategory } from '@/shared/categories/getEffectiveCategory'

/**
 * Reason why a transaction matched a budget category
 */
export type MatchReason = 'category' | 'tag' | 'both'

/**
 * Result of matching a transaction to a budget category
 */
export interface BudgetCategoryMatch {
  category: BudgetCategory
  matchReason: MatchReason
}

/**
 * Parse transaction category from JSON string to array.
 * Handles null, undefined, empty strings, and invalid JSON gracefully.
 *
 * @param categoryJson - JSON string like '["Food and Drink", "Restaurants"]'
 * @returns Array of category strings, or empty array if invalid
 */
export function parseTransactionCategory(
  categoryJson: string | null | undefined
): string[] {
  if (!categoryJson) return []

  try {
    const parsed = JSON.parse(categoryJson)
    if (Array.isArray(parsed)) {
      return parsed
    }
    return []
  } catch {
    return []
  }
}

/**
 * Check if transaction category contains budget category using containment logic.
 * Transaction ["Food and Drink", "Restaurants"] contains budget ["Food and Drink"]
 * but NOT vice versa (budget is more specific).
 *
 * @param transactionCategory - Parsed transaction category array
 * @param budgetCategory - Budget category array to match against
 * @returns true if transaction category contains all budget category elements in order
 */
function categoryContains(
  transactionCategory: string[],
  budgetCategory: string[]
): boolean {
  // Empty budget category doesn't match anything
  if (budgetCategory.length === 0) return false

  // Transaction must have at least as many elements as budget
  if (transactionCategory.length < budgetCategory.length) return false

  // Check if budget category is a prefix of transaction category
  // e.g., ["Food"] matches ["Food", "Restaurants"]
  // e.g., ["Food", "Restaurants"] matches ["Food", "Restaurants"]
  return budgetCategory.every(
    (cat, index) => transactionCategory[index] === cat
  )
}

/**
 * Check if transaction tags overlap with budget tags.
 *
 * @param transactionTags - Transaction's tag array (may be undefined)
 * @param budgetTags - Budget category's tag array
 * @returns true if there's any tag overlap
 */
function tagsOverlap(
  transactionTags: string[] | undefined,
  budgetTags: string[]
): boolean {
  if (!transactionTags || transactionTags.length === 0) return false
  if (budgetTags.length === 0) return false

  return transactionTags.some((tag) => budgetTags.includes(tag))
}

/**
 * Match a transaction against a list of budget categories.
 * Returns the first matching category with the reason for the match.
 *
 * Matching logic options:
 * - category_only: Match only on category hierarchy
 * - tag_only: Match only on tag overlap
 * - and: Both category AND tags must match
 * - or: Either category OR tags match
 *
 * @param transaction - Transaction to match
 * @param categories - Budget categories to check against
 * @returns First matching category with reason, or null if no match
 */
export function matchTransactionToBudgetCategories(
  transaction: Transaction,
  categories: BudgetCategory[]
): BudgetCategoryMatch | null {
  if (!categories || categories.length === 0) return null

  const txCategory = getEffectiveCategory(transaction)
  const txTags = transaction.tags || []

  for (const budgetCat of categories) {
    const categoryMatches = categoryContains(txCategory, budgetCat.category)
    const tagMatches = tagsOverlap(txTags, budgetCat.tags)

    let isMatch
    let matchReason: MatchReason

    // Determine match based on match_logic
    switch (budgetCat.match_logic) {
      case 'category_only':
        isMatch = categoryMatches
        matchReason = 'category'
        break

      case 'tag_only':
        isMatch = tagMatches
        matchReason = 'tag'
        break

      case 'and':
        isMatch = categoryMatches && tagMatches
        matchReason = 'both'
        break

      case 'or':
        isMatch = categoryMatches || tagMatches
        // Determine reason based on what matched
        if (categoryMatches && tagMatches) {
          matchReason = 'both'
        } else if (categoryMatches) {
          matchReason = 'category'
        } else {
          matchReason = 'tag'
        }
        break

      default:
        // Default to category_only for unknown/empty values
        isMatch = categoryMatches
        matchReason = 'category'
    }

    if (isMatch) {
      return { category: budgetCat, matchReason }
    }
  }

  return null
}
