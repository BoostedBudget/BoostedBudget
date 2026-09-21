/**
 * Tests for groupBudgetCategoriesByPrimary
 * Issue #1200: Multi-level category hierarchy — Task 7
 * TDD: RED phase — tests written before implementation
 */
import { describe, it, expect } from 'bun:test'
import type { BudgetCategory } from '../types'
import { groupBudgetCategoriesByPrimary } from './groupBudgetCategoriesByPrimary'

// Helper to create a minimal BudgetCategory for testing
const makePfcPrimary = (
  id: string,
  pfc_primary: string,
  created_at = '2024-01-01T00:00:00Z'
): BudgetCategory => ({
  id,
  budget_id: 'budget-1',
  category_kind: 'pfc',
  pfc_primary,
  pfc_detailed: undefined,
  pfc_primary_display: undefined,
  pfc_detailed_display: undefined,
  allocated_amount: 100,
  spent_amount: 50,
  tags: [],
  match_logic: 'category_only',
  created_at,
  updated_at: created_at,
})

const makePfcDetailed = (
  id: string,
  pfc_primary: string,
  pfc_detailed: string,
  created_at = '2024-01-01T00:00:00Z'
): BudgetCategory => ({
  id,
  budget_id: 'budget-1',
  category_kind: 'pfc',
  pfc_primary,
  pfc_detailed,
  pfc_primary_display: undefined,
  pfc_detailed_display: undefined,
  allocated_amount: 100,
  spent_amount: 50,
  tags: [],
  match_logic: 'category_only',
  created_at,
  updated_at: created_at,
})

const makeCustom = (
  id: string,
  custom_category_id: string,
  created_at = '2024-01-01T00:00:00Z'
): BudgetCategory => ({
  id,
  budget_id: 'budget-1',
  category_kind: 'custom',
  custom_category_id,
  allocated_amount: 100,
  spent_amount: 50,
  tags: [],
  match_logic: 'category_only',
  created_at,
  updated_at: created_at,
})

describe('groupBudgetCategoriesByPrimary', () => {
  // Test 1: Empty array
  it('returns empty array for empty input', () => {
    const result = groupBudgetCategoriesByPrimary([])
    expect(result).toEqual([])
  })

  // Test 2: Single primary-only PFC
  it('returns single group with no children for a primary-only PFC category', () => {
    const cat = makePfcPrimary('cat-1', 'FOOD_AND_DRINK')
    const result = groupBudgetCategoriesByPrimary([cat])
    expect(result).toHaveLength(1)
    expect(result[0].group).toBe(cat)
    expect(result[0].children).toEqual([])
  })

  // Test 3: Single detailed PFC with no parent → standalone
  it('returns standalone group for a detailed PFC category with no matching parent', () => {
    const cat = makePfcDetailed('cat-1', 'FOOD_AND_DRINK', 'FOOD_AND_DRINK_FAST_FOOD')
    const result = groupBudgetCategoriesByPrimary([cat])
    expect(result).toHaveLength(1)
    expect(result[0].group).toBe(cat)
    expect(result[0].children).toEqual([])
  })

  // Test 4: Primary + matching child
  it('groups a primary and its matching detailed child', () => {
    const parent = makePfcPrimary('parent-1', 'FOOD_AND_DRINK', '2024-01-01T00:00:00Z')
    const child = makePfcDetailed('child-1', 'FOOD_AND_DRINK', 'FOOD_AND_DRINK_RESTAURANTS', '2024-01-02T00:00:00Z')
    const result = groupBudgetCategoriesByPrimary([parent, child])
    expect(result).toHaveLength(1)
    expect(result[0].group).toBe(parent)
    expect(result[0].children).toEqual([child])
  })

  // Test 5: Primary + two matching children + one non-matching detailed
  it('groups two children under parent and leaves orphan as standalone', () => {
    const parent = makePfcPrimary('parent-1', 'FOOD_AND_DRINK', '2024-01-01T00:00:00Z')
    const child1 = makePfcDetailed('child-1', 'FOOD_AND_DRINK', 'FOOD_AND_DRINK_RESTAURANTS', '2024-01-02T00:00:00Z')
    const child2 = makePfcDetailed('child-2', 'FOOD_AND_DRINK', 'FOOD_AND_DRINK_FAST_FOOD', '2024-01-03T00:00:00Z')
    const orphan = makePfcDetailed('orphan-1', 'TRANSPORTATION', 'TRANSPORTATION_GAS', '2024-01-04T00:00:00Z')
    const result = groupBudgetCategoriesByPrimary([parent, child1, child2, orphan])

    expect(result).toHaveLength(2)
    // First group: parent with 2 children
    expect(result[0].group).toBe(parent)
    expect(result[0].children).toHaveLength(2)
    expect(result[0].children).toContain(child1)
    expect(result[0].children).toContain(child2)
    // Second group: orphan standalone
    expect(result[1].group).toBe(orphan)
    expect(result[1].children).toEqual([])
  })

  // Test 6: Custom category → standalone
  it('returns custom category as standalone group', () => {
    const cat = makeCustom('cat-1', 'my-custom-cat')
    const result = groupBudgetCategoriesByPrimary([cat])
    expect(result).toHaveLength(1)
    expect(result[0].group).toBe(cat)
    expect(result[0].children).toEqual([])
  })

  // Test 7: Mix of all three
  it('handles mix of primary+child, orphan, and custom correctly', () => {
    const parent = makePfcPrimary('parent-1', 'FOOD_AND_DRINK', '2024-01-01T00:00:00Z')
    const child = makePfcDetailed('child-1', 'FOOD_AND_DRINK', 'FOOD_AND_DRINK_RESTAURANTS', '2024-01-02T00:00:00Z')
    const orphan = makePfcDetailed('orphan-1', 'TRANSPORTATION', 'TRANSPORTATION_GAS', '2024-01-03T00:00:00Z')
    const custom = makeCustom('custom-1', 'my-custom', '2024-01-04T00:00:00Z')

    const result = groupBudgetCategoriesByPrimary([parent, child, orphan, custom])

    // 3 groups: parent+child group, orphan standalone, custom standalone
    expect(result).toHaveLength(3)

    const pfcGroup = result.find(g => g.group?.id === 'parent-1')
    expect(pfcGroup).toBeDefined()
    expect(pfcGroup!.children).toEqual([child])

    const orphanGroup = result.find(g => g.group?.id === 'orphan-1')
    expect(orphanGroup).toBeDefined()
    expect(orphanGroup!.children).toEqual([])

    const customGroup = result.find(g => g.group?.id === 'custom-1')
    expect(customGroup).toBeDefined()
    expect(customGroup!.children).toEqual([])
  })

  // Test 8: Multiple primaries each with children → separate groups
  it('creates separate groups for multiple primaries with children', () => {
    const parent1 = makePfcPrimary('parent-1', 'FOOD_AND_DRINK', '2024-01-01T00:00:00Z')
    const child1 = makePfcDetailed('child-1', 'FOOD_AND_DRINK', 'FOOD_AND_DRINK_RESTAURANTS', '2024-01-02T00:00:00Z')
    const parent2 = makePfcPrimary('parent-2', 'TRANSPORTATION', '2024-01-03T00:00:00Z')
    const child2 = makePfcDetailed('child-2', 'TRANSPORTATION', 'TRANSPORTATION_GAS', '2024-01-04T00:00:00Z')

    const result = groupBudgetCategoriesByPrimary([parent1, child1, parent2, child2])

    expect(result).toHaveLength(2)
    const group1 = result.find(g => g.group?.id === 'parent-1')
    expect(group1!.children).toEqual([child1])
    const group2 = result.find(g => g.group?.id === 'parent-2')
    expect(group2!.children).toEqual([child2])
  })

  // Test 9: Children sorted by created_at within group
  it('sorts children by created_at ASC within group', () => {
    const parent = makePfcPrimary('parent-1', 'FOOD_AND_DRINK', '2024-01-01T00:00:00Z')
    const childLater = makePfcDetailed('child-later', 'FOOD_AND_DRINK', 'FOOD_AND_DRINK_RESTAURANTS', '2024-01-03T00:00:00Z')
    const childEarlier = makePfcDetailed('child-earlier', 'FOOD_AND_DRINK', 'FOOD_AND_DRINK_FAST_FOOD', '2024-01-02T00:00:00Z')

    const result = groupBudgetCategoriesByPrimary([parent, childLater, childEarlier])

    expect(result).toHaveLength(1)
    expect(result[0].children[0].id).toBe('child-earlier')
    expect(result[0].children[1].id).toBe('child-later')
  })

  // Test 10: Groups sorted by their first item's created_at ASC
  it('sorts groups by their first item created_at ASC', () => {
    const parent2 = makePfcPrimary('parent-2', 'TRANSPORTATION', '2024-01-03T00:00:00Z')
    const parent1 = makePfcPrimary('parent-1', 'FOOD_AND_DRINK', '2024-01-01T00:00:00Z')

    const result = groupBudgetCategoriesByPrimary([parent2, parent1])

    expect(result[0].group!.id).toBe('parent-1')
    expect(result[1].group!.id).toBe('parent-2')
  })

  // Test 11: Custom categories appended after PFC groups
  it('appends custom categories after PFC groups', () => {
    const customEarly = makeCustom('custom-early', 'custom-a', '2024-01-01T00:00:00Z')
    const pfcLate = makePfcPrimary('pfc-late', 'FOOD_AND_DRINK', '2024-01-05T00:00:00Z')

    const result = groupBudgetCategoriesByPrimary([customEarly, pfcLate])

    // PFC groups first, then custom (even if custom has earlier created_at)
    expect(result[0].group!.id).toBe('pfc-late')
    expect(result[1].group!.id).toBe('custom-early')
  })
})

describe('groupBudgetCategoriesByPrimary — custom nesting linkage (#1686)', () => {
  // Test: custom category whose ROW linkage names a budgeted primary-only PFC parent nests there
  it('nests custom category under its PFC parent group when parent budget exists', () => {
    const pfcParent = makePfcPrimary('pfc-food', 'FOOD_AND_DRINK', '2024-01-01T00:00:00Z')
    const custom: BudgetCategory = {
      ...makeCustom('custom-1', 'custom-uuid-a', '2024-01-02T00:00:00Z'),
      custom_parent_pfc_primary: 'FOOD_AND_DRINK',
    }

    const result = groupBudgetCategoriesByPrimary([pfcParent, custom])

    // 1 group: pfc parent with the custom as a child
    expect(result).toHaveLength(1)
    expect(result[0].group!.id).toBe('pfc-food')
    expect(result[0].children).toHaveLength(1)
    expect(result[0].children[0].id).toBe('custom-1')
  })

  // Test: custom category without a row linkage stays standalone
  it('keeps custom category as standalone when its row linkage is absent', () => {
    const pfcParent = makePfcPrimary('pfc-food', 'FOOD_AND_DRINK', '2024-01-01T00:00:00Z')
    const custom = makeCustom('custom-1', 'custom-uuid-b', '2024-01-02T00:00:00Z')

    const result = groupBudgetCategoriesByPrimary([pfcParent, custom])

    // 2 groups: pfc parent alone, custom standalone
    expect(result).toHaveLength(2)
    expect(result[0].group!.id).toBe('pfc-food')
    expect(result[0].children).toHaveLength(0)
    expect(result[1].group!.id).toBe('custom-1')
    expect(result[1].children).toHaveLength(0)
  })

  // Test: custom category whose row linkage names a PFC primary with no budget row → standalone
  it('renders custom category as standalone when its row linkage names no budgeted PFC parent', () => {
    const custom: BudgetCategory = {
      ...makeCustom('custom-1', 'custom-uuid-c', '2024-01-01T00:00:00Z'),
      custom_parent_pfc_primary: 'FOOD_AND_DRINK',
    }

    // No PFC parent budget for FOOD_AND_DRINK in the categories list
    const result = groupBudgetCategoriesByPrimary([custom])

    expect(result).toHaveLength(1)
    expect(result[0].group!.id).toBe('custom-1')
    expect(result[0].children).toHaveLength(0)
  })

  // Test: row linkage names a primary that is NOT the budgeted one → standalone (divergent pair)
  it('does not nest a custom whose row linkage names a different primary than the budgeted row', () => {
    const pfcParent = makePfcPrimary('pfc-food', 'FOOD_AND_DRINK', '2024-01-01T00:00:00Z')
    const custom: BudgetCategory = {
      ...makeCustom('custom-1', 'custom-uuid-divergent', '2024-01-02T00:00:00Z'),
      custom_parent_pfc_primary: 'TRANSPORTATION',
    }

    const result = groupBudgetCategoriesByPrimary([pfcParent, custom])

    expect(result).toHaveLength(2)
    expect(result.find(g => g.group?.id === 'pfc-food')!.children).toEqual([])
    expect(result.find(g => g.group?.id === 'custom-1')).toBeDefined()
  })

  // Test: backward compat — a custom with no row linkage is always standalone
  it('renders custom categories as standalone when no row linkage is present (backward compat)', () => {
    const pfcParent = makePfcPrimary('pfc-food', 'FOOD_AND_DRINK', '2024-01-01T00:00:00Z')
    const custom = makeCustom('custom-1', 'custom-uuid-d', '2024-01-02T00:00:00Z')

    const result = groupBudgetCategoriesByPrimary([pfcParent, custom])

    expect(result).toHaveLength(2)
    expect(result[0].group!.id).toBe('pfc-food')
    expect(result[1].group!.id).toBe('custom-1')
    expect(result[1].children).toHaveLength(0)
  })

  // Test: multiple customs, some with a row linkage, some without
  it('nests only customs with matching PFC parent; leaves others standalone', () => {
    const pfcFood = makePfcPrimary('pfc-food', 'FOOD_AND_DRINK', '2024-01-01T00:00:00Z')
    const customWithParent: BudgetCategory = {
      ...makeCustom('custom-with-parent', 'uuid-with', '2024-01-02T00:00:00Z'),
      custom_parent_pfc_primary: 'FOOD_AND_DRINK',
    }
    const customNoParent = makeCustom('custom-no-parent', 'uuid-without', '2024-01-03T00:00:00Z')

    const result = groupBudgetCategoriesByPrimary([pfcFood, customWithParent, customNoParent])

    // 2 groups: [pfc-food with customWithParent as child], [customNoParent standalone]
    expect(result).toHaveLength(2)
    const foodGroup = result.find(g => g.group?.id === 'pfc-food')
    expect(foodGroup!.children).toHaveLength(1)
    expect(foodGroup!.children[0].id).toBe('custom-with-parent')

    const noParentGroup = result.find(g => g.group?.id === 'custom-no-parent')
    expect(noParentGroup!.children).toHaveLength(0)
  })

  it('nests a custom sub-allocation under its budgeted custom parent (#1203)', () => {
    const base = {
      budget_id: 'budget-1',
      category_kind: 'custom' as const,
      allocated_amount: 100,
      spent_amount: 50,
      tags: [],
      match_logic: 'category_only' as const,
      created_at: '2024-01-01T00:00:00Z',
      updated_at: '2024-01-01T00:00:00Z',
    }
    const parent: BudgetCategory = { ...base, id: 'p', custom_category_id: 'cust-parent' }
    const child: BudgetCategory = {
      ...base,
      id: 'c',
      custom_category_id: 'cust-child',
      custom_parent_id: 'cust-parent',
      created_at: '2024-01-02T00:00:00Z',
    }
    const result = groupBudgetCategoriesByPrimary([parent, child])
    const parentGroup = result.find(g => g.group?.id === 'p')
    expect(parentGroup).toBeDefined()
    expect(parentGroup!.children.map(c => c.id)).toEqual(['c'])
    // The child is not also a standalone group.
    expect(result.find(g => g.group?.id === 'c')).toBeUndefined()
  })
})

describe('groupBudgetCategoriesByPrimary — orphan detailed rows must not adopt customs (#1686)', () => {
  // Issue #1686: a detailed row with no matching primary-only row is a SIBLING of any custom
  // sharing its pfc_primary, never its parent. The custom must render top-level.
  it('does not nest a custom under an orphan detailed row sharing its pfc_primary', () => {
    const orphan = makePfcDetailed(
      'orphan-1',
      'RENT_AND_UTILITIES',
      'RENT_AND_UTILITIES_GAS_AND_ELECTRICITY'
    )
    const custom: BudgetCategory = {
      ...makeCustom('custom-1', 'hoa-uuid'),
      custom_parent_pfc_primary: 'RENT_AND_UTILITIES',
    }

    const result = groupBudgetCategoriesByPrimary([orphan, custom])

    const customGroup = result.find(g => g.group?.id === 'custom-1')
    expect(customGroup).toBeDefined()
    expect(customGroup!.children).toEqual([])

    const orphanGroup = result.find(g => g.group?.id === 'orphan-1')
    expect(orphanGroup).toBeDefined()
    expect(orphanGroup!.children).toEqual([])
    expect(orphanGroup!.children.map(c => c.id)).not.toContain('custom-1')
  })
})

describe('groupBudgetCategoriesByPrimary — containment gates (#1686)', () => {
  it('does not nest a custom whose match_logic is "or" under a category_only parent', () => {
    const parent = makePfcPrimary('pfc-food', 'FOOD_AND_DRINK', '2024-01-01T00:00:00Z')
    const custom: BudgetCategory = {
      ...makeCustom('custom-1', 'uuid-or', '2024-01-02T00:00:00Z'),
      match_logic: 'or',
      custom_parent_pfc_primary: 'FOOD_AND_DRINK',
    }

    const result = groupBudgetCategoriesByPrimary([parent, custom])

    const customGroup = result.find(g => g.group?.id === 'custom-1')
    expect(customGroup).toBeDefined()
    expect(result.find(g => g.group?.id === 'pfc-food')!.children).toEqual([])
  })

  it('does not nest a custom whose match_logic is "tag_only" under a category_only parent', () => {
    const parent = makePfcPrimary('pfc-food', 'FOOD_AND_DRINK', '2024-01-01T00:00:00Z')
    const custom: BudgetCategory = {
      ...makeCustom('custom-1', 'uuid-tag-only', '2024-01-02T00:00:00Z'),
      match_logic: 'tag_only',
      custom_parent_pfc_primary: 'FOOD_AND_DRINK',
    }

    const result = groupBudgetCategoriesByPrimary([parent, custom])

    const customGroup = result.find(g => g.group?.id === 'custom-1')
    expect(customGroup).toBeDefined()
    expect(result.find(g => g.group?.id === 'pfc-food')!.children).toEqual([])
  })

  it('does not nest a custom whose row linkage is absent', () => {
    const parent = makePfcPrimary('pfc-food', 'FOOD_AND_DRINK', '2024-01-01T00:00:00Z')
    const custom = makeCustom('custom-1', 'uuid-map-only', '2024-01-02T00:00:00Z')

    const result = groupBudgetCategoriesByPrimary([parent, custom])

    const customGroup = result.find(g => g.group?.id === 'custom-1')
    expect(customGroup).toBeDefined()
    expect(result.find(g => g.group?.id === 'pfc-food')!.children).toEqual([])
  })

  it('does not nest a contained child under a non-containing parent (tag_only)', () => {
    const parent: BudgetCategory = {
      ...makePfcPrimary('pfc-food', 'FOOD_AND_DRINK', '2024-01-01T00:00:00Z'),
      match_logic: 'tag_only',
    }
    const custom: BudgetCategory = {
      ...makeCustom('custom-1', 'uuid-tag-parent', '2024-01-02T00:00:00Z'),
      custom_parent_pfc_primary: 'FOOD_AND_DRINK',
    }

    const result = groupBudgetCategoriesByPrimary([parent, custom])

    const customGroup = result.find(g => g.group?.id === 'custom-1')
    expect(customGroup).toBeDefined()
    expect(result.find(g => g.group?.id === 'pfc-food')!.children).toEqual([])
  })

  it('nests a genuinely contained child (category_only parent, "and" child, row linkage present)', () => {
    const parent = makePfcPrimary('pfc-food', 'FOOD_AND_DRINK', '2024-01-01T00:00:00Z')
    const custom: BudgetCategory = {
      ...makeCustom('custom-1', 'uuid-and', '2024-01-02T00:00:00Z'),
      match_logic: 'and',
      custom_parent_pfc_primary: 'FOOD_AND_DRINK',
    }

    const result = groupBudgetCategoriesByPrimary([parent, custom])

    expect(result.find(g => g.group?.id === 'pfc-food')!.children.map(c => c.id)).toEqual([
      'custom-1',
    ])
    expect(result.find(g => g.group?.id === 'custom-1')).toBeUndefined()
  })

  it('nests a grandchild under a budgeted PFC root when its direct parent has no budget row', () => {
    const pfcRoot = makePfcPrimary('pfc-root', 'FOOD_AND_DRINK', '2024-01-01T00:00:00Z')
    const grandchild: BudgetCategory = {
      ...makeCustom('grandchild-1', 'uuid-grandchild', '2024-01-02T00:00:00Z'),
      custom_parent_id: 'unbudgeted-parent-uuid',
      custom_grandparent_pfc_primary: 'FOOD_AND_DRINK',
    }

    const result = groupBudgetCategoriesByPrimary([pfcRoot, grandchild])

    expect(result.find(g => g.group?.id === 'pfc-root')!.children.map(c => c.id)).toEqual([
      'grandchild-1',
    ])
    expect(result.find(g => g.group?.id === 'grandchild-1')).toBeUndefined()
  })
})
