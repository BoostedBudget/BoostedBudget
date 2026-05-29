import { describe, it, expect } from 'bun:test'
import { buildCategoryHierarchy, eligibleCustomParents } from './buildCategoryHierarchy'
import type { CustomCategory } from '../types'

function cat(partial: Partial<CustomCategory> & { id: string; name: string }): CustomCategory {
  return {
    user_id: 'u1',
    usage_count: 0,
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
    ...partial,
  }
}

describe('buildCategoryHierarchy', () => {
  it('groups top-level customs with their children, alphabetically', () => {
    const cats = [
      cat({ id: 'p', name: 'Food' }),
      cat({ id: 'c2', name: 'Fine Dining', parent_id: 'p' }),
      cat({ id: 'c1', name: 'Fast Food', parent_id: 'p' }),
      cat({ id: 'a', name: 'Apps' }),
    ]
    const { topLevel } = buildCategoryHierarchy(cats)
    // Alphabetical: Apps, Food
    expect(topLevel.map((g) => g.parent.name)).toEqual(['Apps', 'Food'])
    const food = topLevel.find((g) => g.parent.id === 'p')!
    expect(food.children.map((c) => c.name)).toEqual(['Fast Food', 'Fine Dining'])
  })

  it('groups customs nested under a PFC primary', () => {
    const cats = [
      cat({ id: 'm', name: 'Meal Prep', parent_pfc_primary: 'FOOD_AND_DRINK' }),
      cat({ id: 'b', name: 'Bars', parent_pfc_primary: 'FOOD_AND_DRINK' }),
    ]
    const { pfcGroups } = buildCategoryHierarchy(cats)
    expect(pfcGroups).toHaveLength(1)
    expect(pfcGroups[0].pfcPrimary).toBe('FOOD_AND_DRINK')
    expect(pfcGroups[0].categories.map((c) => c.name)).toEqual(['Bars', 'Meal Prep'])
  })

  it('keeps the three nesting modes separate', () => {
    const cats = [
      cat({ id: 'top', name: 'Top' }),
      cat({ id: 'child', name: 'Child', parent_id: 'top' }),
      cat({ id: 'pfc', name: 'PfcNested', parent_pfc_primary: 'TRAVEL' }),
    ]
    const { topLevel, pfcGroups } = buildCategoryHierarchy(cats)
    expect(topLevel).toHaveLength(1)
    expect(topLevel[0].children.map((c) => c.id)).toEqual(['child'])
    expect(pfcGroups).toHaveLength(1)
  })
})

describe('eligibleCustomParents', () => {
  it('returns only top-level customs and excludes self', () => {
    const cats = [
      cat({ id: 'top1', name: 'Top1' }),
      cat({ id: 'top2', name: 'Top2' }),
      cat({ id: 'child', name: 'Child', parent_id: 'top1' }),
      cat({ id: 'pfc', name: 'PfcNested', parent_pfc_primary: 'TRAVEL' }),
    ]
    const eligible = eligibleCustomParents(cats, 'top2')
    expect(eligible.map((c) => c.id)).toEqual(['top1'])
  })
})
