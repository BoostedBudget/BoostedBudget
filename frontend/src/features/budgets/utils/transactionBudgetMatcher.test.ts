/**
 * Transaction Budget Matcher Utility Tests
 * Issue #499: Transaction-Budget Linking UI
 *
 * TDD RED Phase: Write comprehensive tests before implementation
 */

import { describe, it, expect } from 'vitest'
import type { Transaction } from '../../transactions/types'
import type { BudgetCategory } from '../types'
import {
  parseTransactionCategory,
  matchTransactionToBudgetCategories,
} from './transactionBudgetMatcher'

// =============================================================================
// Test Helpers
// =============================================================================

function createTransaction(overrides: Partial<Transaction> = {}): Transaction {
  return {
    id: 'tx-1',
    user_id: 'user-1',
    account_id: 'acc-1',
    linked_item_id: 'item-1',
    source: 'plaid',
    amount: -25.99,
    date: '2025-01-15',
    name: 'Test Transaction',
    pending: false,
    created_at: '2025-01-15T00:00:00Z',
    updated_at: '2025-01-15T00:00:00Z',
    ...overrides,
  }
}

function createBudgetCategory(overrides: Partial<BudgetCategory> = {}): BudgetCategory {
  return {
    id: 'cat-1',
    budget_id: 'budget-1',
    category: ['Food and Drink'],
    allocated_amount: 500,
    spent_amount: 250,
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
    tags: [],
    match_logic: 'category_only',
    ...overrides,
  }
}

// =============================================================================
// parseTransactionCategory Tests
// =============================================================================

describe('parseTransactionCategory', () => {
  it('should parse valid JSON category string to array', () => {
    const result = parseTransactionCategory('["Food and Drink", "Restaurants"]')
    expect(result).toEqual(['Food and Drink', 'Restaurants'])
  })

  it('should return empty array for null category', () => {
    const result = parseTransactionCategory(null)
    expect(result).toEqual([])
  })

  it('should return empty array for undefined category', () => {
    const result = parseTransactionCategory(undefined)
    expect(result).toEqual([])
  })

  it('should return empty array for empty string', () => {
    const result = parseTransactionCategory('')
    expect(result).toEqual([])
  })

  it('should return empty array for invalid JSON', () => {
    const result = parseTransactionCategory('not valid json')
    expect(result).toEqual([])
  })

  it('should return empty array for non-array JSON', () => {
    const result = parseTransactionCategory('{"foo": "bar"}')
    expect(result).toEqual([])
  })

  it('should handle single category in array', () => {
    const result = parseTransactionCategory('["Shopping"]')
    expect(result).toEqual(['Shopping'])
  })

  it('should handle deeply nested categories', () => {
    const result = parseTransactionCategory('["Food and Drink", "Restaurants", "Fast Food"]')
    expect(result).toEqual(['Food and Drink', 'Restaurants', 'Fast Food'])
  })
})

// =============================================================================
// matchTransactionToBudgetCategories - No Match Cases
// =============================================================================

describe('matchTransactionToBudgetCategories - no match cases', () => {
  it('should return null when categories array is empty', () => {
    const transaction = createTransaction({
      category: '["Food and Drink", "Restaurants"]',
    })
    const result = matchTransactionToBudgetCategories(transaction, [])
    expect(result).toBeNull()
  })

  it('should return null when transaction has null category and no tags', () => {
    const transaction = createTransaction({
      category: null,
      tags: [],
    })
    const categories = [createBudgetCategory()]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).toBeNull()
  })

  it('should return null when transaction category does not match any budget category', () => {
    const transaction = createTransaction({
      category: '["Shopping", "Clothing"]',
    })
    const categories = [
      createBudgetCategory({
        category: ['Food and Drink'],
        match_logic: 'category_only',
      }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).toBeNull()
  })
})

// =============================================================================
// matchTransactionToBudgetCategories - category_only Match Logic
// =============================================================================

describe('matchTransactionToBudgetCategories - category_only match logic', () => {
  it('should match when transaction category contains budget category', () => {
    const transaction = createTransaction({
      category: '["Food and Drink", "Restaurants"]',
    })
    const categories = [
      createBudgetCategory({
        category: ['Food and Drink'],
        match_logic: 'category_only',
      }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).not.toBeNull()
    expect(result?.category.id).toBe('cat-1')
    expect(result?.matchReason).toBe('category')
  })

  it('should match when transaction category exactly matches budget category', () => {
    const transaction = createTransaction({
      category: '["Food and Drink", "Restaurants"]',
    })
    const categories = [
      createBudgetCategory({
        category: ['Food and Drink', 'Restaurants'],
        match_logic: 'category_only',
      }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).not.toBeNull()
    expect(result?.matchReason).toBe('category')
  })

  it('should NOT match when budget category is more specific than transaction', () => {
    // Transaction: ["Food and Drink"]
    // Budget: ["Food and Drink", "Restaurants"]
    // Should NOT match because transaction doesn't have "Restaurants"
    const transaction = createTransaction({
      category: '["Food and Drink"]',
    })
    const categories = [
      createBudgetCategory({
        category: ['Food and Drink', 'Restaurants'],
        match_logic: 'category_only',
      }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).toBeNull()
  })

  it('should match using category_override when present', () => {
    const transaction = createTransaction({
      category: '["Shopping"]', // Original category
      category_override: '["Food and Drink", "Restaurants"]', // User override
    })
    const categories = [
      createBudgetCategory({
        category: ['Food and Drink'],
        match_logic: 'category_only',
      }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).not.toBeNull()
    expect(result?.matchReason).toBe('category')
  })

  it('should prefer category_override over category when both exist', () => {
    const transaction = createTransaction({
      category: '["Food and Drink"]', // Would match food budget
      category_override: '["Shopping"]', // Override to shopping
    })
    const foodCategory = createBudgetCategory({
      id: 'cat-food',
      category: ['Food and Drink'],
      match_logic: 'category_only',
    })
    const shoppingCategory = createBudgetCategory({
      id: 'cat-shopping',
      category: ['Shopping'],
      match_logic: 'category_only',
    })
    const result = matchTransactionToBudgetCategories(transaction, [foodCategory, shoppingCategory])
    expect(result).not.toBeNull()
    expect(result?.category.id).toBe('cat-shopping')
  })

  it('should ignore tags when match_logic is category_only', () => {
    const transaction = createTransaction({
      category: '["Food and Drink"]',
      tags: ['dining'], // Has matching tag, but should be ignored
    })
    const categories = [
      createBudgetCategory({
        category: ['Shopping'], // Different category
        tags: ['dining'],
        match_logic: 'category_only', // Only match on category
      }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).toBeNull() // Should not match because category doesn't match
  })
})

// =============================================================================
// matchTransactionToBudgetCategories - tag_only Match Logic
// =============================================================================

describe('matchTransactionToBudgetCategories - tag_only match logic', () => {
  it('should match when transaction has any budget tag', () => {
    const transaction = createTransaction({
      category: '["Shopping"]',
      tags: ['dining', 'work'],
    })
    const categories = [
      createBudgetCategory({
        category: ['Food and Drink'],
        tags: ['dining'],
        match_logic: 'tag_only',
      }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).not.toBeNull()
    expect(result?.matchReason).toBe('tag')
  })

  it('should NOT match when transaction has no matching tags', () => {
    const transaction = createTransaction({
      category: '["Food and Drink"]', // Matches category, but using tag_only
      tags: ['entertainment'],
    })
    const categories = [
      createBudgetCategory({
        category: ['Food and Drink'],
        tags: ['dining'],
        match_logic: 'tag_only',
      }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).toBeNull()
  })

  it('should NOT match when transaction has no tags', () => {
    const transaction = createTransaction({
      category: '["Food and Drink"]',
      tags: [],
    })
    const categories = [
      createBudgetCategory({
        category: ['Food and Drink'],
        tags: ['dining'],
        match_logic: 'tag_only',
      }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).toBeNull()
  })

  it('should NOT match when transaction has undefined tags', () => {
    const transaction = createTransaction({
      category: '["Food and Drink"]',
      tags: undefined,
    })
    const categories = [
      createBudgetCategory({
        category: ['Food and Drink'],
        tags: ['dining'],
        match_logic: 'tag_only',
      }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).toBeNull()
  })

  it('should ignore category when match_logic is tag_only', () => {
    const transaction = createTransaction({
      category: '["Food and Drink"]', // Matches category
      tags: ['dining'],
    })
    const categories = [
      createBudgetCategory({
        category: ['Shopping'], // Different category
        tags: ['dining'], // Same tag
        match_logic: 'tag_only',
      }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).not.toBeNull() // Should match on tag only
    expect(result?.matchReason).toBe('tag')
  })

  it('should match when any of multiple budget tags overlap', () => {
    const transaction = createTransaction({
      tags: ['groceries'],
    })
    const categories = [
      createBudgetCategory({
        tags: ['dining', 'groceries', 'takeout'],
        match_logic: 'tag_only',
      }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).not.toBeNull()
    expect(result?.matchReason).toBe('tag')
  })
})

// =============================================================================
// matchTransactionToBudgetCategories - and Match Logic
// =============================================================================

describe('matchTransactionToBudgetCategories - and match logic', () => {
  it('should match when BOTH category AND tags match', () => {
    const transaction = createTransaction({
      category: '["Food and Drink", "Restaurants"]',
      tags: ['dining'],
    })
    const categories = [
      createBudgetCategory({
        category: ['Food and Drink'],
        tags: ['dining'],
        match_logic: 'and',
      }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).not.toBeNull()
    expect(result?.matchReason).toBe('both')
  })

  it('should NOT match when only category matches', () => {
    const transaction = createTransaction({
      category: '["Food and Drink", "Restaurants"]',
      tags: ['entertainment'], // Different tag
    })
    const categories = [
      createBudgetCategory({
        category: ['Food and Drink'],
        tags: ['dining'],
        match_logic: 'and',
      }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).toBeNull()
  })

  it('should NOT match when only tags match', () => {
    const transaction = createTransaction({
      category: '["Shopping"]', // Different category
      tags: ['dining'],
    })
    const categories = [
      createBudgetCategory({
        category: ['Food and Drink'],
        tags: ['dining'],
        match_logic: 'and',
      }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).toBeNull()
  })

  it('should NOT match when budget has no tags configured', () => {
    const transaction = createTransaction({
      category: '["Food and Drink"]',
      tags: ['dining'],
    })
    const categories = [
      createBudgetCategory({
        category: ['Food and Drink'],
        tags: [], // No tags configured
        match_logic: 'and',
      }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).toBeNull()
  })
})

// =============================================================================
// matchTransactionToBudgetCategories - or Match Logic
// =============================================================================

describe('matchTransactionToBudgetCategories - or match logic', () => {
  it('should match when category matches (tags do not)', () => {
    const transaction = createTransaction({
      category: '["Food and Drink", "Restaurants"]',
      tags: ['entertainment'], // Different tag
    })
    const categories = [
      createBudgetCategory({
        category: ['Food and Drink'],
        tags: ['dining'],
        match_logic: 'or',
      }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).not.toBeNull()
    expect(result?.matchReason).toBe('category')
  })

  it('should match when tags match (category does not)', () => {
    const transaction = createTransaction({
      category: '["Shopping"]', // Different category
      tags: ['dining'],
    })
    const categories = [
      createBudgetCategory({
        category: ['Food and Drink'],
        tags: ['dining'],
        match_logic: 'or',
      }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).not.toBeNull()
    expect(result?.matchReason).toBe('tag')
  })

  it('should match with "both" reason when both category and tags match', () => {
    const transaction = createTransaction({
      category: '["Food and Drink", "Restaurants"]',
      tags: ['dining'],
    })
    const categories = [
      createBudgetCategory({
        category: ['Food and Drink'],
        tags: ['dining'],
        match_logic: 'or',
      }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).not.toBeNull()
    expect(result?.matchReason).toBe('both')
  })

  it('should NOT match when neither category nor tags match', () => {
    const transaction = createTransaction({
      category: '["Shopping"]',
      tags: ['entertainment'],
    })
    const categories = [
      createBudgetCategory({
        category: ['Food and Drink'],
        tags: ['dining'],
        match_logic: 'or',
      }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).toBeNull()
  })
})

// =============================================================================
// matchTransactionToBudgetCategories - Edge Cases
// =============================================================================

describe('matchTransactionToBudgetCategories - edge cases', () => {
  it('should return first matching category when multiple match', () => {
    const transaction = createTransaction({
      category: '["Food and Drink", "Restaurants"]',
    })
    const categories = [
      createBudgetCategory({
        id: 'cat-1',
        category: ['Food and Drink'],
        match_logic: 'category_only',
      }),
      createBudgetCategory({
        id: 'cat-2',
        category: ['Food and Drink', 'Restaurants'],
        match_logic: 'category_only',
      }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).not.toBeNull()
    expect(result?.category.id).toBe('cat-1') // First match
  })

  it('should handle transaction with empty tags array', () => {
    const transaction = createTransaction({
      category: '["Food and Drink"]',
      tags: [],
    })
    const categories = [
      createBudgetCategory({
        category: ['Food and Drink'],
        tags: [],
        match_logic: 'category_only',
      }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).not.toBeNull()
    expect(result?.matchReason).toBe('category')
  })

  it('should handle budget category with empty tags array', () => {
    const transaction = createTransaction({
      category: '["Food and Drink"]',
      tags: ['dining'],
    })
    const categories = [
      createBudgetCategory({
        category: ['Food and Drink'],
        tags: [], // Empty tags
        match_logic: 'category_only',
      }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).not.toBeNull() // Category matches
  })

  it('should handle budget category with empty category array', () => {
    const transaction = createTransaction({
      category: '["Food and Drink"]',
      tags: ['dining'],
    })
    const categories = [
      createBudgetCategory({
        category: [], // Empty category
        tags: ['dining'],
        match_logic: 'tag_only',
      }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).not.toBeNull() // Tags match
    expect(result?.matchReason).toBe('tag')
  })

  it('should handle malformed category JSON gracefully', () => {
    const transaction = createTransaction({
      category: '{not valid}',
    })
    const categories = [
      createBudgetCategory({
        category: ['Food and Drink'],
        match_logic: 'category_only',
      }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).toBeNull()
  })

  it('should default to category_only when match_logic is undefined/empty', () => {
    const transaction = createTransaction({
      category: '["Food and Drink"]',
    })
    const categories = [
      createBudgetCategory({
        category: ['Food and Drink'],
        match_logic: '' as never, // Simulate invalid/empty value
      }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).not.toBeNull()
    expect(result?.matchReason).toBe('category')
  })

  it('should work with case-sensitive category matching', () => {
    const transaction = createTransaction({
      category: '["Food and Drink"]',
    })
    const categories = [
      createBudgetCategory({
        category: ['food and drink'], // Different case
        match_logic: 'category_only',
      }),
    ]
    // Categories are case-sensitive (matching backend behavior)
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).toBeNull()
  })

  it('should work with case-sensitive tag matching', () => {
    const transaction = createTransaction({
      category: null,
      tags: ['Dining'], // Capital D
    })
    const categories = [
      createBudgetCategory({
        category: [],
        tags: ['dining'], // Lowercase
        match_logic: 'tag_only',
      }),
    ]
    // Tags are case-sensitive (matching backend behavior)
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).toBeNull()
  })
})
