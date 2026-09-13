import { describe, expect, it } from 'bun:test'
import { getCategoryPresentation } from './getCategoryPresentation'

describe('getCategoryPresentation', () => {
  it('separates curated primary and detailed labels ahead of server labels', () => {
    expect(getCategoryPresentation({ kind: 'pfc', pfc_primary: 'RENT_AND_UTILITIES', pfc_detailed: 'RENT_AND_UTILITIES_TELEPHONE', pfc_primary_display: 'Rent and Utilities', pfc_detailed_display: 'TELEPHONE' })).toEqual({ categoryLabel: 'Rent & Utilities', subcategoryLabel: 'Telephone', breadcrumb: 'Rent & Utilities › Telephone' })
  })
  it('keeps primary-only and uncategorized details empty', () => {
    expect(getCategoryPresentation({ kind: 'pfc', pfc_primary: 'TRAVEL' })).toEqual({ categoryLabel: 'Travel', subcategoryLabel: '—', breadcrumb: 'Travel' })
    expect(getCategoryPresentation(null)).toEqual({ categoryLabel: 'Uncategorized', subcategoryLabel: '—', breadcrumb: 'Uncategorized' })
  })
  it('uses corresponding server labels for unknown enums', () => {
    expect(getCategoryPresentation({ kind: 'pfc', pfc_primary: 'NEW_PRIMARY', pfc_detailed: 'NEW_PRIMARY_DETAIL', pfc_primary_display: 'New Category', pfc_detailed_display: 'New Detail' })).toEqual({ categoryLabel: 'New Category', subcategoryLabel: 'New Detail', breadcrumb: 'New Category › New Detail' })
  })
  it('formats unknown detail without duplicating its primary', () => {
    expect(getCategoryPresentation({ kind: 'pfc', pfc_primary: 'TRAVEL', pfc_detailed: 'TRAVEL_NEW_DETAIL' }).subcategoryLabel).toBe('New Detail')
  })
  const categories = [
    { id: 'root', name: 'Family' },
    { id: 'child', name: 'Kids', parent_id: 'root' },
    { id: 'leaf', name: 'School', parent_id: 'child' },
    { id: 'pfc-child', name: 'Family phones', parent_pfc_primary: 'RENT_AND_UTILITIES' },
    { id: 'pfc-leaf', name: 'Kids phones', parent_id: 'pfc-child' },
  ]
  for (const [id, categoryLabel, subcategoryLabel, breadcrumb] of [
    ['root', 'Family', '—', 'Family'],
    ['child', 'Family', 'Kids', 'Family › Kids'],
    ['leaf', 'Family', 'Kids › School', 'Family › Kids › School'],
    ['pfc-leaf', 'Rent & Utilities', 'Family phones › Kids phones', 'Rent & Utilities › Family phones › Kids phones'],
  ]) {
    it(`resolves custom ancestry for ${id}`, () => {
      expect(getCategoryPresentation({ kind: 'custom', custom_category_id: id }, categories)).toEqual({ categoryLabel, subcategoryLabel, breadcrumb })
    })
  }
  it('retains saved custom label or id when metadata is absent', () => {
    expect(getCategoryPresentation({ kind: 'custom', custom_category_id: 'missing', custom_category_display: 'Saved name' }).breadcrumb).toBe('Saved name')
    expect(getCategoryPresentation({ kind: 'custom', custom_category_id: 'missing' }).breadcrumb).toBe('missing')
  })
  it('terminates corrupt cyclic ancestry without duplicating names', () => {
    expect(getCategoryPresentation({ kind: 'custom', custom_category_id: 'cycle' }, [{ id: 'cycle', name: 'Cycle', parent_id: 'cycle' }]).breadcrumb).toBe('Cycle')
  })
})
