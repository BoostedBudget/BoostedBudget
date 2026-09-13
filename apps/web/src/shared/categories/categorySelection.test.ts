import { describe, expect, it } from 'bun:test'
import { categorySelectionEqual, seedCategorySelection } from './categorySelection'
import type { Transaction } from '@/features/transactions/types'

describe('typed category selection', () => {
  it('seeds unknown custom UUIDs without metadata or display-name inference', () => {
    expect(seedCategorySelection({ category_override_kind: 'custom', custom_category_override_id: 'uuid', custom_category_override_display: 'Income', pfc_primary: 'INCOME' } as Transaction)).toEqual({ kind: 'custom', custom_category_id: 'uuid' })
  })
  it('preserves both override enums and excludes display properties from the request', () => {
    expect(seedCategorySelection({ category_override_kind: 'pfc', pfc_primary_override: 'UNKNOWN', pfc_detailed_override: 'UNKNOWN_DETAIL', pfc_primary_override_display: 'Unknown' } as Transaction)).toEqual({ kind: 'pfc', pfc_primary: 'UNKNOWN', pfc_detailed: 'UNKNOWN_DETAIL' })
  })
  it('uses the bank category when no override exists and clear when none exists', () => {
    expect(seedCategorySelection({ pfc_primary: 'INCOME' } as Transaction)).toEqual({ kind: 'pfc', pfc_primary: 'INCOME', pfc_detailed: undefined })
    expect(seedCategorySelection({} as Transaction)).toEqual({ kind: 'clear' })
  })
  it('compares identity and detailed enums instead of labels or property ordering', () => {
    expect(categorySelectionEqual({ kind: 'pfc', pfc_primary: 'INCOME' }, { pfc_primary: 'INCOME', kind: 'pfc' })).toBe(true)
    expect(categorySelectionEqual({ kind: 'custom', custom_category_id: 'INCOME' }, { kind: 'pfc', pfc_primary: 'INCOME' })).toBe(false)
    expect(categorySelectionEqual({ kind: 'pfc', pfc_primary: 'INCOME' }, { kind: 'pfc', pfc_primary: 'INCOME', pfc_detailed: 'INCOME_WAGES' })).toBe(false)
  })
})
