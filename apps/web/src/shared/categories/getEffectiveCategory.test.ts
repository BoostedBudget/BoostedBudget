import { describe, it, expect } from 'bun:test'
import { getEffectiveCategory, getEffectiveCategoryLabel } from './getEffectiveCategory'
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
  it('returns pfc custom override when category_override_kind is custom', () => {
    const tx: Transaction = {
      ...baseTransaction,
      category_override_kind: 'custom',
      custom_category_override_id: 'custom-uuid-123',
    }
    expect(getEffectiveCategory(tx)).toEqual({ kind: 'custom', custom_category_id: 'custom-uuid-123' })
  })

  it('returns pfc override when category_override_kind is pfc', () => {
    const tx: Transaction = {
      ...baseTransaction,
      category_override_kind: 'pfc',
      pfc_primary_override: 'FOOD_AND_DRINK',
      pfc_detailed_override: 'FOOD_AND_DRINK_RESTAURANTS',
    }
    expect(getEffectiveCategory(tx)).toEqual({
      kind: 'pfc',
      pfc_primary: 'FOOD_AND_DRINK',
      pfc_detailed: 'FOOD_AND_DRINK_RESTAURANTS',
    })
  })

  it('returns pfc override without detailed when only primary set', () => {
    const tx: Transaction = {
      ...baseTransaction,
      category_override_kind: 'pfc',
      pfc_primary_override: 'TRANSPORTATION',
    }
    expect(getEffectiveCategory(tx)).toEqual({
      kind: 'pfc',
      pfc_primary: 'TRANSPORTATION',
      pfc_detailed: undefined,
    })
  })

  it('falls through to pfc_primary when no override', () => {
    const tx: Transaction = {
      ...baseTransaction,
      pfc_primary: 'FOOD_AND_DRINK',
      pfc_detailed: 'FOOD_AND_DRINK_GROCERIES',
    }
    expect(getEffectiveCategory(tx)).toEqual({
      kind: 'pfc',
      pfc_primary: 'FOOD_AND_DRINK',
      pfc_detailed: 'FOOD_AND_DRINK_GROCERIES',
    })
  })

  it('returns pfc with only primary when pfc_detailed is absent', () => {
    const tx: Transaction = {
      ...baseTransaction,
      pfc_primary: 'INCOME',
    }
    expect(getEffectiveCategory(tx)).toEqual({
      kind: 'pfc',
      pfc_primary: 'INCOME',
      pfc_detailed: undefined,
    })
  })

  it('returns null when all sources are absent', () => {
    expect(getEffectiveCategory(baseTransaction)).toBeNull()
  })

  it('override takes priority over pfc_primary', () => {
    const tx: Transaction = {
      ...baseTransaction,
      category_override_kind: 'pfc',
      pfc_primary_override: 'TRANSPORTATION',
      pfc_primary: 'FOOD_AND_DRINK',
    }
    expect(getEffectiveCategory(tx)).toEqual({
      kind: 'pfc',
      pfc_primary: 'TRANSPORTATION',
      pfc_detailed: undefined,
    })
  })

  it('custom override takes priority over pfc_primary', () => {
    const tx: Transaction = {
      ...baseTransaction,
      category_override_kind: 'custom',
      custom_category_override_id: 'custom-uuid',
      pfc_primary: 'FOOD_AND_DRINK',
    }
    expect(getEffectiveCategory(tx)).toEqual({ kind: 'custom', custom_category_id: 'custom-uuid' })
  })

  it('ignores pfc override when pfc_primary_override is absent', () => {
    const tx: Transaction = {
      ...baseTransaction,
      category_override_kind: 'pfc',
      pfc_primary_override: null,
      pfc_primary: 'FOOD_AND_DRINK',
    }
    // No valid pfc_primary_override → falls through to pfc_primary
    expect(getEffectiveCategory(tx)).toEqual({
      kind: 'pfc',
      pfc_primary: 'FOOD_AND_DRINK',
      pfc_detailed: undefined,
    })
  })
})

describe('getEffectiveCategoryLabel', () => {
  it('returns "Uncategorized" for null', () => {
    expect(getEffectiveCategoryLabel(null)).toBe('Uncategorized')
  })

  it('returns custom_category_id for custom kind', () => {
    expect(getEffectiveCategoryLabel({ kind: 'custom', custom_category_id: 'my-id' })).toBe('my-id')
  })

  it('returns pfc_detailed when present', () => {
    expect(getEffectiveCategoryLabel({
      kind: 'pfc',
      pfc_primary: 'FOOD_AND_DRINK',
      pfc_detailed: 'FOOD_AND_DRINK_RESTAURANTS',
    })).toBe('FOOD_AND_DRINK_RESTAURANTS')
  })

  it('returns pfc_primary when no pfc_detailed', () => {
    expect(getEffectiveCategoryLabel({ kind: 'pfc', pfc_primary: 'INCOME' })).toBe('INCOME')
  })
})
