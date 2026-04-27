import { describe, it, expect } from 'bun:test'
import { getEffectiveCategory } from './getEffectiveCategory'
import type { Transaction } from '@/features/transactions/types'

const baseTransaction: Transaction = {
  id: 'tx-1',
  user_id: 'user-1',
  account_id: 'acct-1',
  linked_item_id: 'item-1',
  source: 'plaid',
  amount: 100,
  date: '2025-01-01',
  name: 'Test Transaction',
  pending: false,
  created_at: '2025-01-01T00:00:00Z',
  updated_at: '2025-01-01T00:00:00Z',
}

describe('getEffectiveCategory', () => {
  it('returns category_override when present', () => {
    const tx: Transaction = {
      ...baseTransaction,
      category_override: JSON.stringify(['Food And Drink', 'Groceries']),
    }
    expect(getEffectiveCategory(tx)).toEqual(['Food And Drink', 'Groceries'])
  })

  it('falls through to legacy category when no override', () => {
    const tx: Transaction = {
      ...baseTransaction,
      category: JSON.stringify(['Shops', 'Sporting Goods']),
    }
    expect(getEffectiveCategory(tx)).toEqual(['Shops', 'Sporting Goods'])
  })

  it('falls through to PFC primary_display when no override and no legacy category', () => {
    const tx: Transaction = {
      ...baseTransaction,
      personal_finance_category_data: {
        primary: 'FOOD_AND_DRINK',
        primary_display: 'Food And Drink',
      },
    }
    expect(getEffectiveCategory(tx)).toEqual(['Food And Drink'])
  })

  it('returns empty array when all sources are absent', () => {
    expect(getEffectiveCategory(baseTransaction)).toEqual([])
  })

  it('explicit-uncategorized guard: returns [] when category_source is manual and override is absent', () => {
    const tx: Transaction = {
      ...baseTransaction,
      category: JSON.stringify(['Shops']),
      category_source: 'manual',
      category_override: null,
    }
    expect(getEffectiveCategory(tx)).toEqual([])
  })

  it('explicit-uncategorized guard does NOT fire when override is present', () => {
    const tx: Transaction = {
      ...baseTransaction,
      category_source: 'manual',
      category_override: JSON.stringify(['Food And Drink']),
    }
    expect(getEffectiveCategory(tx)).toEqual(['Food And Drink'])
  })

  it('override takes priority over legacy category', () => {
    const tx: Transaction = {
      ...baseTransaction,
      category_override: JSON.stringify(['Transportation']),
      category: JSON.stringify(['Shops']),
    }
    expect(getEffectiveCategory(tx)).toEqual(['Transportation'])
  })

  it('override takes priority over PFC data', () => {
    const tx: Transaction = {
      ...baseTransaction,
      category_override: JSON.stringify(['Transportation']),
      personal_finance_category_data: {
        primary: 'FOOD_AND_DRINK',
        primary_display: 'Food And Drink',
      },
    }
    expect(getEffectiveCategory(tx)).toEqual(['Transportation'])
  })

  it('legacy category takes priority over PFC data', () => {
    const tx: Transaction = {
      ...baseTransaction,
      category: JSON.stringify(['Shops']),
      personal_finance_category_data: {
        primary: 'FOOD_AND_DRINK',
        primary_display: 'Food And Drink',
      },
    }
    expect(getEffectiveCategory(tx)).toEqual(['Shops'])
  })

  it('handles malformed override JSON gracefully', () => {
    const tx: Transaction = {
      ...baseTransaction,
      category_override: 'not-json',
      category: JSON.stringify(['Shops']),
    }
    expect(getEffectiveCategory(tx)).toEqual(['Shops'])
  })

  it('handles empty override array — falls through to legacy category when source is not manual', () => {
    const tx: Transaction = {
      ...baseTransaction,
      category_override: JSON.stringify([]),
      category: JSON.stringify(['Shops']),
    }
    expect(getEffectiveCategory(tx)).toEqual(['Shops'])
  })

  it('explicit-uncategorized guard fires with empty JSON array override when source is manual', () => {
    // category_override='[]' means user explicitly cleared — must NOT fall through to legacy category
    const tx: Transaction = {
      ...baseTransaction,
      category_source: 'manual',
      category_override: JSON.stringify([]),
      category: JSON.stringify(['Shops']),
    }
    expect(getEffectiveCategory(tx)).toEqual([])
  })

  it('handles null override — falls through to legacy category', () => {
    const tx: Transaction = {
      ...baseTransaction,
      category_override: null,
      category: JSON.stringify(['Shops']),
    }
    expect(getEffectiveCategory(tx)).toEqual(['Shops'])
  })
})
