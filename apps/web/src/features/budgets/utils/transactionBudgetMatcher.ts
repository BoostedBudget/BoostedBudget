/**
 * Transaction Budget Matcher Utility
 * Issue #499: Transaction-Budget Linking UI
 *
 * Provides client-side matching logic to determine which budget category
 * a transaction belongs to. Replicates backend SQL matching behavior.
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
 * Check if a transaction's effective category matches a budget category.
 * Uses PFC v2 discriminated union: either pfc or custom kind.
 */
function effectiveCategoryMatches(transaction: Transaction, budgetCat: BudgetCategory): boolean {
  const effective = getEffectiveCategory(transaction)
  if (!effective) return false

  if (budgetCat.category_kind === 'pfc' && effective.kind === 'pfc') {
    if (!budgetCat.pfc_primary) return false
    if (effective.pfc_primary !== budgetCat.pfc_primary) return false
    if (budgetCat.pfc_detailed && effective.pfc_detailed !== budgetCat.pfc_detailed) return false
    return true
  }

  if (budgetCat.category_kind === 'custom' && effective.kind === 'custom') {
    return !!(budgetCat.custom_category_id && effective.custom_category_id === budgetCat.custom_category_id)
  }

  return false
}

/**
 * Check if transaction tags overlap with budget tags.
 */
function tagsOverlap(transactionTags: string[] | undefined, budgetTags: string[]): boolean {
  if (!transactionTags || transactionTags.length === 0) return false
  if (budgetTags.length === 0) return false
  return transactionTags.some((tag) => budgetTags.includes(tag))
}

/**
 * Match a transaction against a list of budget categories.
 * Returns the first matching category with the reason for the match.
 */
export function matchTransactionToBudgetCategories(
  transaction: Transaction,
  categories: BudgetCategory[]
): BudgetCategoryMatch | null {
  if (!categories || categories.length === 0) return null

  const txTags = transaction.tags || []

  for (const budgetCat of categories) {
    const catMatches = effectiveCategoryMatches(transaction, budgetCat)
    const tagMatches = tagsOverlap(txTags, budgetCat.tags)

    let isMatch: boolean
    let matchReason: MatchReason

    switch (budgetCat.match_logic) {
      case 'category_only':
        isMatch = catMatches
        matchReason = 'category'
        break

      case 'tag_only':
        isMatch = tagMatches
        matchReason = 'tag'
        break

      case 'and':
        isMatch = catMatches && tagMatches
        matchReason = 'both'
        break

      case 'or':
        isMatch = catMatches || tagMatches
        if (catMatches && tagMatches) {
          matchReason = 'both'
        } else if (catMatches) {
          matchReason = 'category'
        } else {
          matchReason = 'tag'
        }
        break

      default:
        isMatch = catMatches
        matchReason = 'category'
    }

    if (isMatch) {
      return { category: budgetCat, matchReason }
    }
  }

  return null
}
