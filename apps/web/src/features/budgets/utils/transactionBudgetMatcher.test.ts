import { describe, it, expect } from 'bun:test'
import type { Transaction } from '../../transactions/types'
import type { BudgetCategory } from '../types'
import { matchTransactionToBudgetCategories } from './transactionBudgetMatcher'

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
    category_kind: 'pfc',
    pfc_primary: 'FOOD_AND_DRINK',
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
// matchTransactionToBudgetCategories - No Match Cases
// =============================================================================

describe('matchTransactionToBudgetCategories - no match cases', () => {
  it('should return null when categories array is empty', () => {
    const transaction = createTransaction({ pfc_primary: 'FOOD_AND_DRINK' })
    const result = matchTransactionToBudgetCategories(transaction, [])
    expect(result).toBeNull()
  })

  it('should return null when transaction has no pfc_primary and no override and no tags', () => {
    const transaction = createTransaction({ pfc_primary: null, tags: [] })
    const categories = [createBudgetCategory()]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).toBeNull()
  })

  it('should return null when transaction pfc_primary does not match any budget category', () => {
    const transaction = createTransaction({ pfc_primary: 'SHOPPING', pfc_detailed: 'SHOPPING_CLOTHING' })
    const categories = [createBudgetCategory({ pfc_primary: 'FOOD_AND_DRINK', match_logic: 'category_only' })]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).toBeNull()
  })
})

// =============================================================================
// matchTransactionToBudgetCategories - category_only Match Logic
// =============================================================================

describe('matchTransactionToBudgetCategories - category_only match logic', () => {
  it('should match when transaction pfc_primary matches budget pfc_primary', () => {
    const transaction = createTransaction({ pfc_primary: 'FOOD_AND_DRINK', pfc_detailed: 'FOOD_AND_DRINK_RESTAURANTS' })
    const categories = [createBudgetCategory({ pfc_primary: 'FOOD_AND_DRINK', match_logic: 'category_only' })]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).not.toBeNull()
    expect(result?.category.id).toBe('cat-1')
    expect(result?.matchReason).toBe('category')
  })

  it('should match when both pfc_primary and pfc_detailed match exactly', () => {
    const transaction = createTransaction({ pfc_primary: 'FOOD_AND_DRINK', pfc_detailed: 'FOOD_AND_DRINK_RESTAURANTS' })
    const categories = [
      createBudgetCategory({
        pfc_primary: 'FOOD_AND_DRINK',
        pfc_detailed: 'FOOD_AND_DRINK_RESTAURANTS',
        match_logic: 'category_only',
      }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).not.toBeNull()
    expect(result?.matchReason).toBe('category')
  })

  it('should NOT match when budget pfc_detailed is more specific than transaction', () => {
    const transaction = createTransaction({ pfc_primary: 'FOOD_AND_DRINK' })
    const categories = [
      createBudgetCategory({
        pfc_primary: 'FOOD_AND_DRINK',
        pfc_detailed: 'FOOD_AND_DRINK_RESTAURANTS',
        match_logic: 'category_only',
      }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).toBeNull()
  })

  it('should match using pfc_primary_override when category_override_kind is pfc', () => {
    const transaction = createTransaction({
      pfc_primary: 'SHOPPING',
      category_override_kind: 'pfc',
      pfc_primary_override: 'FOOD_AND_DRINK',
      pfc_detailed_override: 'FOOD_AND_DRINK_RESTAURANTS',
    })
    const categories = [createBudgetCategory({ pfc_primary: 'FOOD_AND_DRINK', match_logic: 'category_only' })]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).not.toBeNull()
    expect(result?.matchReason).toBe('category')
  })

  it('should prefer pfc_primary_override over pfc_primary when override is set', () => {
    const transaction = createTransaction({
      pfc_primary: 'FOOD_AND_DRINK',
      category_override_kind: 'pfc',
      pfc_primary_override: 'SHOPPING',
    })
    const foodCategory = createBudgetCategory({ id: 'cat-food', pfc_primary: 'FOOD_AND_DRINK', match_logic: 'category_only' })
    const shoppingCategory = createBudgetCategory({ id: 'cat-shopping', pfc_primary: 'SHOPPING', match_logic: 'category_only' })
    const result = matchTransactionToBudgetCategories(transaction, [foodCategory, shoppingCategory])
    expect(result).not.toBeNull()
    expect(result?.category.id).toBe('cat-shopping')
  })

  it('should ignore tags when match_logic is category_only', () => {
    const transaction = createTransaction({ pfc_primary: 'FOOD_AND_DRINK', tags: ['dining'] })
    const categories = [
      createBudgetCategory({
        pfc_primary: 'SHOPPING',
        tags: ['dining'],
        match_logic: 'category_only',
      }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).toBeNull()
  })

  it('should match custom category when category_override_kind is custom', () => {
    const transaction = createTransaction({
      pfc_primary: 'FOOD_AND_DRINK',
      category_override_kind: 'custom',
      custom_category_override_id: 'custom-uuid-abc',
    })
    const categories = [
      createBudgetCategory({
        category_kind: 'custom',
        pfc_primary: undefined,
        custom_category_id: 'custom-uuid-abc',
        match_logic: 'category_only',
      }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).not.toBeNull()
    expect(result?.matchReason).toBe('category')
  })
})

// =============================================================================
// matchTransactionToBudgetCategories - tag_only Match Logic
// =============================================================================

describe('matchTransactionToBudgetCategories - tag_only match logic', () => {
  it('should match when transaction has any budget tag', () => {
    const transaction = createTransaction({ pfc_primary: 'SHOPPING', tags: ['dining', 'work'] })
    const categories = [
      createBudgetCategory({ pfc_primary: 'FOOD_AND_DRINK', tags: ['dining'], match_logic: 'tag_only' }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).not.toBeNull()
    expect(result?.matchReason).toBe('tag')
  })

  it('should NOT match when transaction has no matching tags', () => {
    const transaction = createTransaction({ pfc_primary: 'FOOD_AND_DRINK', tags: ['entertainment'] })
    const categories = [
      createBudgetCategory({ pfc_primary: 'FOOD_AND_DRINK', tags: ['dining'], match_logic: 'tag_only' }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).toBeNull()
  })

  it('should NOT match when transaction has no tags', () => {
    const transaction = createTransaction({ pfc_primary: 'FOOD_AND_DRINK', tags: [] })
    const categories = [
      createBudgetCategory({ pfc_primary: 'FOOD_AND_DRINK', tags: ['dining'], match_logic: 'tag_only' }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).toBeNull()
  })

  it('should NOT match when transaction has undefined tags', () => {
    const transaction = createTransaction({ pfc_primary: 'FOOD_AND_DRINK', tags: undefined })
    const categories = [
      createBudgetCategory({ pfc_primary: 'FOOD_AND_DRINK', tags: ['dining'], match_logic: 'tag_only' }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).toBeNull()
  })

  it('should ignore category when match_logic is tag_only', () => {
    const transaction = createTransaction({ pfc_primary: 'FOOD_AND_DRINK', tags: ['dining'] })
    const categories = [
      createBudgetCategory({ pfc_primary: 'SHOPPING', tags: ['dining'], match_logic: 'tag_only' }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).not.toBeNull()
    expect(result?.matchReason).toBe('tag')
  })

  it('should match when any of multiple budget tags overlap', () => {
    const transaction = createTransaction({ tags: ['groceries'] })
    const categories = [
      createBudgetCategory({ tags: ['dining', 'groceries', 'takeout'], match_logic: 'tag_only' }),
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
    const transaction = createTransaction({ pfc_primary: 'FOOD_AND_DRINK', tags: ['dining'] })
    const categories = [
      createBudgetCategory({ pfc_primary: 'FOOD_AND_DRINK', tags: ['dining'], match_logic: 'and' }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).not.toBeNull()
    expect(result?.matchReason).toBe('both')
  })

  it('should NOT match when only category matches', () => {
    const transaction = createTransaction({ pfc_primary: 'FOOD_AND_DRINK', tags: ['entertainment'] })
    const categories = [
      createBudgetCategory({ pfc_primary: 'FOOD_AND_DRINK', tags: ['dining'], match_logic: 'and' }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).toBeNull()
  })

  it('should NOT match when only tags match', () => {
    const transaction = createTransaction({ pfc_primary: 'SHOPPING', tags: ['dining'] })
    const categories = [
      createBudgetCategory({ pfc_primary: 'FOOD_AND_DRINK', tags: ['dining'], match_logic: 'and' }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).toBeNull()
  })

  it('should NOT match when budget has no tags configured', () => {
    const transaction = createTransaction({ pfc_primary: 'FOOD_AND_DRINK', tags: ['dining'] })
    const categories = [
      createBudgetCategory({ pfc_primary: 'FOOD_AND_DRINK', tags: [], match_logic: 'and' }),
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
    const transaction = createTransaction({ pfc_primary: 'FOOD_AND_DRINK', tags: ['entertainment'] })
    const categories = [
      createBudgetCategory({ pfc_primary: 'FOOD_AND_DRINK', tags: ['dining'], match_logic: 'or' }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).not.toBeNull()
    expect(result?.matchReason).toBe('category')
  })

  it('should match when tags match (category does not)', () => {
    const transaction = createTransaction({ pfc_primary: 'SHOPPING', tags: ['dining'] })
    const categories = [
      createBudgetCategory({ pfc_primary: 'FOOD_AND_DRINK', tags: ['dining'], match_logic: 'or' }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).not.toBeNull()
    expect(result?.matchReason).toBe('tag')
  })

  it('should match with "both" reason when both category and tags match', () => {
    const transaction = createTransaction({ pfc_primary: 'FOOD_AND_DRINK', tags: ['dining'] })
    const categories = [
      createBudgetCategory({ pfc_primary: 'FOOD_AND_DRINK', tags: ['dining'], match_logic: 'or' }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).not.toBeNull()
    expect(result?.matchReason).toBe('both')
  })

  it('should NOT match when neither category nor tags match', () => {
    const transaction = createTransaction({ pfc_primary: 'SHOPPING', tags: ['entertainment'] })
    const categories = [
      createBudgetCategory({ pfc_primary: 'FOOD_AND_DRINK', tags: ['dining'], match_logic: 'or' }),
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
    const transaction = createTransaction({ pfc_primary: 'FOOD_AND_DRINK', pfc_detailed: 'FOOD_AND_DRINK_RESTAURANTS' })
    const categories = [
      createBudgetCategory({ id: 'cat-1', pfc_primary: 'FOOD_AND_DRINK', match_logic: 'category_only' }),
      createBudgetCategory({ id: 'cat-2', pfc_primary: 'FOOD_AND_DRINK', pfc_detailed: 'FOOD_AND_DRINK_RESTAURANTS', match_logic: 'category_only' }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).not.toBeNull()
    expect(result?.category.id).toBe('cat-1')
  })

  it('should handle transaction with empty tags array', () => {
    const transaction = createTransaction({ pfc_primary: 'FOOD_AND_DRINK', tags: [] })
    const categories = [createBudgetCategory({ pfc_primary: 'FOOD_AND_DRINK', tags: [], match_logic: 'category_only' })]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).not.toBeNull()
    expect(result?.matchReason).toBe('category')
  })

  it('should handle budget category with empty tags array', () => {
    const transaction = createTransaction({ pfc_primary: 'FOOD_AND_DRINK', tags: ['dining'] })
    const categories = [createBudgetCategory({ pfc_primary: 'FOOD_AND_DRINK', tags: [], match_logic: 'category_only' })]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).not.toBeNull()
  })

  it('should default to category matching when match_logic is invalid', () => {
    const transaction = createTransaction({ pfc_primary: 'FOOD_AND_DRINK' })
    const categories = [
      createBudgetCategory({ pfc_primary: 'FOOD_AND_DRINK', match_logic: '' as never }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).not.toBeNull()
    expect(result?.matchReason).toBe('category')
  })

  it('should work with case-sensitive pfc_primary matching', () => {
    const transaction = createTransaction({ pfc_primary: 'FOOD_AND_DRINK' })
    const categories = [
      createBudgetCategory({ pfc_primary: 'food_and_drink', match_logic: 'category_only' }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).toBeNull()
  })

  it('should work with case-sensitive tag matching', () => {
    const transaction = createTransaction({ pfc_primary: null, tags: ['Dining'] })
    const categories = [
      createBudgetCategory({ tags: ['dining'], match_logic: 'tag_only' }),
    ]
    const result = matchTransactionToBudgetCategories(transaction, categories)
    expect(result).toBeNull()
  })
})
